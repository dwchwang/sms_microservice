# 🔀 Sơ đồ trạng thái — các máy trạng thái trong hệ thống

> Cập nhật: 24/07/2026

| # | Máy trạng thái | Nơi lưu |
|---|---|---|
| [1](#1-trạng-thái-server) | Trạng thái server | `servers.status` + `monitor:status:{id}` |
| [2](#2-trạng-thái-report-job) | Report job | `report_jobs.state` |
| [3](#3-kết-cục-của-một-dòng-trong-file-import) | Một dòng file import | chỉ trong response |
| [4](#4-vòng-đời-một-round-giám-sát) | Round giám sát | Redis (lock · queue · current) |
| [5](#5-factbuffer--dưới-áp-lực) | FactBuffer | bộ nhớ process |
| [6](#6-sức-khoẻ-dữ-liệu-báo-cáo) | Sức khoẻ dữ liệu báo cáo | `daily_snapshots` |
| [7](#7-cron-run--ai-được-chạy-job-hôm-nay) | Cron run (leader election) | `cron_runs.state` |

---

## 1. Trạng thái server

```mermaid
stateDiagram-v2
    [*] --> UNKNOWN: tạo server / import<br/>status_version = 0

    UNKNOWN --> ON: lần ping đầu thành công<br/>Lua trả về 1
    UNKNOWN --> OFF: lần ping đầu thất bại<br/>Lua trả về 1

    ON --> OFF: TCP dial hỏng<br/>XADD status.changed
    OFF --> ON: TCP dial được<br/>XADD status.changed

    ON --> ON: vẫn ON — Lua trả về 2<br/>KHÔNG đẩy stream
    OFF --> OFF: vẫn OFF — Lua trả về 2<br/>KHÔNG đẩy stream

    ON --> [*]: xoá mềm
    OFF --> [*]: xoá mềm
    UNKNOWN --> [*]: xoá mềm

    note right of UNKNOWN
        Lần check đầu tiên (chưa có old_status)
        ĐƯỢC TÍNH là một chuyển đổi thật:
        UNKNOWN → ON/OFF vẫn đẩy stream.
    end note
```

**`UNKNOWN` tồn tại ở hai tình huống hoàn toàn khác nhau:**

| Tình huống | Ý nghĩa |
|-----------|---------|
| Server vừa tạo, chưa qua vòng ping nào | tạm thời, sẽ hết trong ≤ 60 giây |
| Server tồn tại nhưng Monitoring chết | báo động — trạng thái đang mục nát |

**Trạng thái nằm ở hai nơi và có thể lệch nhau trong chốc lát:**

```mermaid
graph LR
    A["monitor:status:{id}<br/>trong Redis"] -->|"XADD (chỉ khi ĐỔI)"| B["stream"]
    B -->|"consumer, độ trễ dưới giây"| C["servers.status<br/>trong PostgreSQL"]

    A -.->|"nguồn NHANH<br/>dùng cho dashboard"| D["/servers/uptime"]
    A -.->|"last_checked_at, đọc tươi"| E
    C -.->|"nguồn BỀN<br/>dùng cho danh sách + export"| E["/servers"]
```

Redis luôn đi trước một chút. Đó là chủ đích: dashboard cần nhanh, danh sách cần bền và query được bằng SQL.

`GET /servers` lấy **cả hai**: `status` từ PostgreSQL (qua cache), còn
`last_status_check` đọc tươi từ Redis lúc serialize. Field thứ hai đổi mỗi phút, nên nhét
nó vào cache entry sẽ buộc bump version mỗi 60 giây và cache thành vô dụng.

**Xoá server phải dọn cả hai nơi.** `servers.deleted_at = NOW()` là chưa đủ:
`TargetProjection.Delete` còn phải `ZREM monitor:uptime:index` và
`DEL monitor:status:{id}`. Thiếu bước đó, một server đã xoá vẫn tiếp tục chấm điểm trong
bảng "10 server tệ nhất" của dashboard mãi mãi — vì cả hai key ấy **không có TTL**.

---

## 2. Trạng thái report job

```mermaid
stateDiagram-v2
    [*] --> processing: Create(job)<br/>dòng job có TRƯỚC khi gửi mail

    processing --> generated: Summary() OK<br/>lưu response_json
    processing --> failed: Summary() lỗi<br/>(thiếu snapshot / DB hỏng)

    generated --> sending: Render() OK
    generated --> failed: Render() lỗi

    sending --> sent: SMTP 250 OK<br/>lưu Message-ID + sent_at
    sending --> failed: lỗi SMTP RÕ RÀNG<br/>(535 sai mật khẩu, người nhận bị chặn)
    sending --> delivery_unknown: lỗi MẬP MỜ<br/>(đứt kết nối sau DATA)

    sent --> [*]
    failed --> [*]
    delivery_unknown --> [*]

    note right of delivery_unknown
        KHÔNG BAO GIỜ tự retry.
        Thư có thể đã tới nơi;
        retry mù sẽ gửi hai lần.
        Message-ID được giữ lại để
        người vận hành tra hộp Sent.
    end note
```

| Trạng thái | Người nhận có nhận được thư? | Hành động tiếp theo |
|-----------|------------------------------|---------------------|
| `sent` | ✅ chắc chắn | không cần làm gì |
| `failed` | ❌ chắc chắn không | sửa nguyên nhân rồi gửi lại — an toàn |
| `delivery_unknown` | ❓ không ai biết | **tra Message-ID trước**, rồi mới quyết định |

`ErrRecipientNotAllowed` là ngoại lệ duy nhất được trả ngược lên thành lỗi HTTP — người dùng cần biết ngay địa chỉ đó không nằm trong `SMTP_RECIPIENT_DOMAINS`.

---

## 3. Kết cục của một dòng trong file import

```mermaid
stateDiagram-v2
    [*] --> parsed: excel.Parser đọc dòng

    parsed --> failed_invalid: parser đánh dấu không hợp lệ<br/>(thiếu cột, sai kiểu)
    parsed --> failed_cidr: IP ngoài CIDR allowlist<br/>SERVER_IP_NOT_ALLOWED
    parsed --> skipped_infile: trùng id/tên NGAY TRONG FILE<br/>(bản đầu tiên thắng)
    parsed --> skipped_name: tên đã bị server còn sống chiếm
    parsed --> candidate: qua hết mọi vòng lọc

    candidate --> succeeded_new: server_id chưa từng có<br/>INSERT
    candidate --> succeeded_revived: server_id đã XOÁ MỀM<br/>ON CONFLICT DO UPDATE<br/>deleted_at = NULL
    candidate --> skipped_duplicate: server_id CÒN SỐNG<br/>WHERE không khớp<br/>⇒ vắng trong RETURNING

    failed_invalid --> [*]
    failed_cidr --> [*]
    skipped_infile --> [*]
    skipped_name --> [*]
    succeeded_new --> [*]
    succeeded_revived --> [*]
    skipped_duplicate --> [*]
```

Một dòng hỏng **không bao giờ** làm hỏng cả request; chỉ file hỏng mới bị từ chối (`ErrImportFileRejected`). Kết quả trả về gộp thành ba nhóm: `succeeded`, `failed`, `skipped_duplicate`.

---

## 4. Vòng đời một round giám sát

```mermaid
stateDiagram-v2
    [*] --> tick: chạm ranh giới 60 giây<br/>(theo đồng hồ REDIS)

    tick --> đo_vòng_trước: QueueDepth(round trước)<br/>→ chỉ số checks_missing
    đo_vòng_trước --> giành_lock: SETNX round lock

    giành_lock --> thua: đã có instance khác giữ
    giành_lock --> thắng: nhận được lock

    thua --> [*]: bình thường — instance này<br/>VẪN ping từ queue người khác nạp

    thắng --> kiểm_ready: EXISTS target:ready
    kiểm_ready --> bỏ_qua: projection chưa dựng xong
    kiểm_ready --> nạp_queue: sẵn sàng

    bỏ_qua --> [*]: vòng dở dang sẽ báo cáo sai<br/>→ thà bỏ hẳn vòng này

    nạp_queue --> công_bố: SSCAN + RPUSH + EXPIRE
    công_bố --> đang_chạy: SET round:current<br/>(đặt CUỐI CÙNG)

    đang_chạy --> cạn: 200 worker BRPOP đến hết queue
    đang_chạy --> tràn: hết 60s mà queue chưa cạn

    cạn --> [*]: Sampler ghi round_duration
    tràn --> [*]: checks_missing > 0<br/>⚠ cần thêm worker/instance

    note right of công_bố
        round:current đặt SAU CÙNG:
        worker thấy round nào thì queue
        của round đó phải đã nạp đầy.
    end note
```

**Chuyển vòng không cần cơ chế nào cả.** Worker đọc lại `monitor:round:current` ở *mỗi* vòng lặp và không bao giờ ghi nhớ round. Vòng mới bắt đầu → lần lặp kế tiếp tự động rút từ queue mới, phần thừa của queue cũ hết hạn theo TTL 120 giây.

---

## 5. FactBuffer — dưới áp lực

```mermaid
stateDiagram-v2
    [*] --> rỗng

    rỗng --> tích_luỹ: Add(fact)
    tích_luỹ --> tích_luỹ: chưa đủ 1000 và chưa tới 5s
    tích_luỹ --> đang_flush: đủ 1000 fact HOẶC ticker 5s

    đang_flush --> rỗng: ES bulk OK
    đang_flush --> thử_lại: bulk lỗi (tối đa 3 lần,<br/>backoff 500ms tăng dần)
    thử_lại --> rỗng: lần thử sau thành công
    thử_lại --> bỏ_batch: hết 3 lần vẫn lỗi

    bỏ_batch --> rỗng: dropped += len(batch)<br/>es_bulk_failure_total++

    tích_luỹ --> bỏ_fact: buffer chạm capacity (50.000)
    bỏ_fact --> tích_luỹ: dropped++, fact bị vứt

    note right of bỏ_fact
        Có chủ đích: khi ES sập lâu,
        thà mất fact còn hơn phình bộ nhớ
        đến khi process chết.
        Coverage giảm là hồi phục được.
        OOM thì không.
    end note
```

Hệ quả có thể quan sát được: ES sập → `actual_checks` trong snapshot giảm → `coverage_pct` giảm → nếu xuống dưới 95% thì email tự gắn cờ ⚠ **CẢNH BÁO** ngay trên đầu.

---

## 6. Sức khoẻ dữ liệu báo cáo

```mermaid
stateDiagram-v2
    [*] --> kiểm_tra: yêu cầu báo cáo khoảng [start, end]

    kiểm_tra --> từ_chối: MissingDates() trả về ngày thiếu
    kiểm_tra --> đủ_dữ_liệu: mọi ngày đều có snapshot

    từ_chối --> [*]: ErrDataUnavailable + liệt kê ngày thiếu<br/>KHÔNG lấy trung bình vắt qua lỗ hổng

    đủ_dữ_liệu --> bình_thường: coverage ≥ 95%
    đủ_dữ_liệu --> suy_giảm: coverage < 95%

    bình_thường --> [*]: gửi mail bình thường
    suy_giảm --> [*]: gửi mail CÓ banner ⚠ CẢNH BÁO<br/>"số liệu uptime có thể không đầy đủ"
```

Ba mức phản ứng, tương ứng ba mức độ nghiêm trọng:

| Tình trạng | Phản ứng |
|-----------|----------|
| Mất vài fact | im lặng, coverage nhích xuống |
| Coverage < `REPORT_COVERAGE_THRESHOLD` (95%) | vẫn gửi, **có cảnh báo** trong email (`degraded = true`) |
| Thiếu hẳn snapshot một ngày | **từ chối gửi**, nêu rõ ngày nào thiếu |

---

## 7. Cron run — ai được chạy job hôm nay

Một dòng `cron_runs` cho mỗi cặp `(job_name, run_date)`. Đây là toàn bộ cơ chế
leader election của `report-service`, và nó không dùng thành phần ngoài nào —
chỉ ràng buộc PRIMARY KEY của PostgreSQL.

```mermaid
stateDiagram-v2
    [*] --> running: TryClaim thắng<br/>INSERT ... 'running', owner = tôi

    running --> running: Heartbeat mỗi 30s<br/>UPDATE heartbeat_at WHERE owner = tôi
    running --> done: MarkDone WHERE owner = tôi
    running --> failed: MarkFailed WHERE owner = tôi

    failed --> running: TryClaim của tick sau<br/>ON CONFLICT ... WHERE state = 'failed'
    running --> running: bị CƯỚP — heartbeat_at cũ hơn 3 phút<br/>owner đổi sang replica khác

    done --> [*]: điểm dừng duy nhất

    note right of done
        'done' KHÔNG bao giờ được claim lại.
        Đây là thứ bảo đảm một job chỉ
        chạy đúng một lần cho mỗi ngày dữ liệu.
    end note

    note left of failed
        'failed' KHÔNG khoá vĩnh viễn:
        tick kế tiếp thử lại được.
        Lỗi tạm thời (ES nghẹt, DB timeout)
        tự khỏi mà không cần ai can thiệp.
    end note
```

**Ba trạng thái, và điều gì làm mỗi trạng thái khác nhau:**

| State | Claim lại được? | Ý nghĩa |
|---|:---:|---|
| `running` | chỉ khi `heartbeat_at` cũ hơn `staleAfter` = 3 phút | có replica đang làm, **hoặc** replica đó đã chết |
| `failed` | ✅ ngay tick sau | đã thử và lỗi; thử lại là đúng |
| `done` | ❌ không bao giờ | xong rồi |

### Vì sao cần cả heartbeat, không chỉ TTL

`running` một mình là mơ hồ: không phân biệt được **job chạy chậm** (snapshot 14,4 triệu
document có thể mất vài phút) với **replica đã chết**. `heartbeat_at` biến sự khác biệt đó
thành một phép so sánh: quá 6 nhịp (3 phút) không cập nhật thì coi là mồ côi.

Chiều còn lại cũng quan trọng: khi `Heartbeat` trả về `RowsAffected = 0` — nghĩa là claim
đã bị cướp — `beat()` **cancel context của job đang chạy**. Không có bước này, một replica
bị treo mạng 4 phút rồi hồi phục sẽ tiếp tục ghi vào cùng `run_date` mà replica mới đang xử lý.

### Vì sao mọi lệnh ghi đều có `WHERE owner = ?`

```mermaid
graph LR
    A["replica A: claim, chạy chậm"] --> B["mạng đứt 4 phút"]
    B --> C["replica B cướp claim<br/>owner = B"]
    C --> D["B chạy xong → MarkDone(owner=B) ✅"]
    A --> E["A hồi phục, job xong,<br/>gọi MarkDone(owner=A)"]
    E --> F["0 row — KHÔNG ghi đè<br/>vì owner hiện tại là B"]
```

Không có guard này, A sẽ ghi `done` lên một dòng mà thực ra B đang giữ, và nếu B lỗi thì
kết quả là một job `done` giả.

Một trường hợp nữa được xử lý riêng: khi service **shutdown giữa job**,
`runClaimed` cố ý **không** ghi gì cả và để claim tự hết hiệu lực sau 3 phút. Ghi `failed`
lúc đó sẽ đúng về hình thức nhưng nói sai sự thật — job không thất bại, nó chỉ bị dừng.
