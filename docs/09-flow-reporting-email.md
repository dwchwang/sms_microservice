# 09 — Flow: dashboard, báo cáo & email

> Hai thứ khác nhau, hai nguồn khác nhau:
>
> | | Dashboard `/servers/uptime` | Báo cáo email |
> |---|---|---|
> | Câu hỏi | "Hôm nay hệ thống thế nào?" | "Ngày X→Y hệ thống thế nào?" |
> | Nguồn | Bộ đếm **ngày hôm nay** trong **Redis** | **`daily_snapshots`** |
> | Có ngay không | **Có**, mọi lúc | Chỉ những ngày đã snapshot |
> | Kỳ | Ngày hiện tại theo giờ VN, tự reset lúc nửa đêm | Khoảng ngày chỉ định, chỉ ngày đã kết thúc |

---

## 0. Dashboard — uptime của ngày hôm nay

```text
GET /api/v1/servers/uptime      scope: server:stats
```

Vấn đề: uptime là tỉ lệ **theo thời gian**, mà `/servers` chỉ có trạng thái *hiện tại*.
Đọc ES mỗi lần mở dashboard thì vi phạm nguyên tắc "ES chỉ có một người đọc" và tốn kém;
chờ snapshot đêm thì hôm nay không có gì để xem.

Giải pháp: **đếm ngay trong Lua script** vốn đã chạy mỗi lần check.

```lua
-- Sau version guard, nên replay/round cũ không làm phồng số đếm.
-- Reset khi sang ngày mới (day là YYYY-MM-DD theo Asia/Ho_Chi_Minh, do Go truyền vào).
if redis.call('HGET', status_key, 'day') ~= day then
  redis.call('HSET', status_key, 'day', day, 'day_total', 0, 'day_on', 0)
end
local total = redis.call('HINCRBY', status_key, 'day_total', 1)
local ons = tonumber(redis.call('HGET', status_key, 'day_on') or '0')
if new_status == 'ON' then ons = redis.call('HINCRBY', status_key, 'day_on', 1) end
redis.call('ZADD', 'monitor:uptime:index', (ons / total) * 100, server_id)
```

Chi phí: **0 round-trip thêm** — script đã `HSET` lên đúng key đó rồi.

### Vì sao reset theo ngày chứ không luỹ kế trọn đời

Bản đầu đếm trọn đời, và dashboard mất ý nghĩa sau vài ngày chạy: một server chết cả hôm
nay vẫn hiện 92% nhờ lịch sử tuần trước, còn AOF mang con số đó qua mọi lần restart. Muốn
thấy tình hình thật phải xoá key thủ công — nghĩa là dashboard không tự đúng được.

Với field `day`, lần check đầu tiên của ngày mới đặt lại `day_total`/`day_on` về 0. Toàn bộ
ZSET tự làm mới trong **một round** sau nửa đêm.

Ngày là ngày **giờ Việt Nam** và do Go tính rồi truyền vào `ARGV[6]`, không phải Lua tự lấy:
`redis.call('TIME')` trả UTC, dùng nó sẽ làm bộ đếm reset lúc 7 giờ sáng VN.

> Trên Redis đã chạy từ trước, `monitor:status:{id}` có thể còn `total_checks`/`on_checks`
> — field của bản cũ còn sót trong AOF, không code nào đọc hay ghi nữa. Tên JSON
> `total_checks`/`on_checks` trong response được giữ nguyên cho hợp đồng với frontend,
> nhưng giá trị lấy từ `day_total`/`day_on`.

> **Vì sao không đếm trong PostgreSQL như hệ thống cũ:** mỗi check là một UPDATE →
> 10.000 UPDATE/phút, và Monitoring sẽ phải phát event **mỗi lần check** thay vì chỉ
> khi status đổi. `server:list:version` nhảy liên tục và **cache chết** — đúng thứ
> mục 4 của [01](./01-architecture-overview.md) tồn tại để tránh.

Đọc ra, không cái nào scale theo số server:

| Số liệu | Lệnh Redis |
|---|---|
| `servers_uptime_100` | `ZCOUNT idx 100 100` |
| `servers_uptime_0` | `ZCOUNT idx 0 0` |
| `servers_uptime_partial` | `ZCOUNT idx (0 (100` |
| `top_10_lowest_uptime` | `ZRANGE idx 0 9 WITHSCORES` + `HMGET day_total day_on` |
| `servers_no_data` | `total_servers − ZCARD idx` |
| `avg_uptime_pct` | `ZRANGE idx 0 -1 WITHSCORES`, lấy trung bình score |
| `servers_on/off/unknown` | `/servers/stats` (PostgreSQL, cache 10s) |

Đo thật với 10.001 server: **0,125s**, cache `server:uptime:cache` TTL 10s.

`servers_no_data` được **trừ ra chứ không đoán**: server vừa tạo chưa qua round nào thì
không có mặt trong ZSET, và nó phải được đếm riêng thay vì bị coi là uptime 0%.

`server_name` của 10 server tệ nhất lấy từ **PostgreSQL**, không từ Redis — PostgreSQL là
chủ sở hữu của tên. Cái giá: 10 truy vấn `FindByServerID` cho đúng 10 dòng.

`avg_uptime_pct` là **trung bình các phần trăm từng server**, không phải tỉ lệ
`Σon / Σtotal` toàn hệ thống. Đây là cùng định nghĩa với `avg_uptime_pct` của report, để hai
con số so sánh được với nhau.

### Ràng buộc Redis

Bộ đếm của **hôm nay** là dữ liệu không tái tạo được từ PostgreSQL, nên:

- `--appendonly yes --appendfsync everysec` — restart giữa ngày không mất số đếm.
- `--maxmemory-policy volatile-lru` chứ **không** `allkeys-lru`: chỉ cache và các key round
  mới có TTL. Bộ đếm, status, target projection, `server:list:version` và stream đều không
  TTL → được bảo vệ.
- Xóa server phải `ZREM` khỏi index và `DEL monitor:status:{id}`, nếu không server đã
  xóa vẫn chấm điểm trong dashboard mãi mãi (hai key đó không có TTL để tự dọn).

Nếu Redis mất sạch dữ liệu, bộ đếm reset về 0 và uptime của hôm nay tính lại từ đầu — nhưng
nó sẽ đúng lại sau vài round, và lịch sử thật vẫn nằm nguyên ở `daily_snapshots`. Mất
`server:monitor-target:*` nghiêm trọng hơn nhiều: Monitoring **dừng hẳn** cho tới khi có
người chạy `rebuild-monitor-cache`.

---

## 1. Vì sao có snapshot

Đề bài yêu cầu dùng Elasticsearch để tính uptime. Nhưng nếu **mỗi lần bấm nút report**
lại aggregate 14,4 triệu document thì: report chậm, ES chết là report chết, và không
thể giữ dữ liệu thô quá vài ngày.

Giải pháp: aggregate **một lần mỗi ngày** (`REPORT_SNAPSHOT_CRON`, mặc định 00:30 giờ VN)
cho ngày hôm trước, cô đọng vào `daily_snapshots`. Report chỉ đọc bảng đó.

```text
ES giữ raw fact 7 ngày (ILM xóa)
daily_snapshots giữ 10.000 row/ngày → nhiều năm
```

ES vẫn là thứ **tính** uptime bằng composite aggregation — chỉ là tính một lần trong đêm
thay vì lúc có người bấm nút. **Job snapshot là consumer duy nhất của ES.**

---

## 2. Job snapshot (một lần mỗi ngày)

```text
0. Scheduler: due? → TryClaim("snapshot", hôm_qua) → thắng thì mới chạy (xem §7)

1. Population ← GET /internal/servers của server-service (cursor, ~10 request)
     tham số BẮT BUỘC, cả hai RFC3339:
       created_before = 00:00 ngày KẾ TIẾP
       deleted_after  = 00:00 ngày cần snapshot
     → server tồn tại trong ngày: created_at < created_before
                                  AND (deleted_at IS NULL OR deleted_at > deleted_after)
     → phân trang bằng next_cursor = server_id cuối, tối đa 1000 dòng/trang

2. Measured ← composite aggregation trên ES cho [00:00, 24:00) hôm qua
     by_server (composite, size 1000, phân trang bằng after_key)
       ├─ doc_count            → actual_checks
       ├─ on_checks  (filter status=ON) → on_checks
       └─ last_fact  (top_hits sort checked_at desc, size 1)
             → server_name + last_status

3. LEFT JOIN: population ⟕ measured
     có fact  → uptime_pct = on_checks / actual_checks × 100
     không có → uptime_pct = NULL   (no_data)

4. expected_checks theo lifecycle từng server

5. UPSERT daily_snapshots ON CONFLICT (server_id, date) DO UPDATE   ← chạy lại được
```

**Population đọc từ server-service, không suy ra từ fact.** Server không ai ping được
vẫn phải xuất hiện trong report — và nó chỉ làm được vậy nếu sự tồn tại của nó được
biết **độc lập với việc đo**. Đây là toàn bộ lý do có `servers_no_data`.

`composite` chứ không `terms`: `terms` không phân trang được cho 10.000 bucket.

Truy vấn ES **không** dùng wildcard `server-status-logs-*`: `IndexPattern()` liệt kê đúng
tên các index mà cửa sổ thời gian chạm tới, nối bằng dấu phẩy. Wildcard bắt ES fan-out qua
mọi index từng ghi; liệt kê tên chỉ chạm hai index (một ngày VN trải trên hai ngày UTC).
`WithIgnoreUnavailable(true)` + `WithAllowNoIndices(true)` xử lý trường hợp một trong hai đã
bị ILM xoá.

Chạy lại thủ công (chỉ network nội bộ, **không** cần claim `cron_runs`):

```bash
docker exec vcs-sms-traefik wget -qO- --post-data='' \
  http://report-service:8084/internal/snapshots/2026-07-23
# → {"Date":"2026-07-23T00:00:00+07:00","Servers":10000,"ServersNoData":0,
#    "CoveragePct":18.88888888888889}
```

Endpoint này bỏ qua leader election có chủ đích: nó là công cụ của người vận hành, và
`UPSERT` vốn idempotent nên chạy lại không hỏng gì.

> Response trả về `snapshot.Result` thô, nên key là **PascalCase** (`Date`, `Servers`,
> `ServersNoData`, `CoveragePct`) chứ không snake_case như các endpoint public — struct đó
> không có json tag. Đây là endpoint nội bộ cho người vận hành đọc bằng mắt, không phải
> hợp đồng API.

---

## 3. Công thức và bất biến

```text
uptime_pct   = on_checks / actual_checks × 100      (mỗi server có data)
coverage_pct = Σ actual_checks / Σ expected_checks × 100
```

| Field | Ý nghĩa |
|---|---|
| `total_servers` | Tổng server thuộc population |
| `servers_on_at_end_at` / `servers_off_at_end_at` | Số server có status **cuối kỳ** là ON/OFF |
| `avg_uptime_pct` | Trung bình uptime trên các server **có data** |
| `servers_uptime_100` / `_partial` / `_0` | Phân bố uptime |
| `servers_no_data` | Thuộc population nhưng không có fact nào |
| `coverage_pct` | Đo được bao nhiêu phần so với lẽ ra phải đo |
| `top_10_lowest_uptime` | 10 server uptime thấp nhất (loại no_data) |

Ba bất biến để bắt lỗi sớm:

```text
total_servers = servers_uptime_100 + servers_uptime_partial + servers_uptime_0 + servers_no_data
total_servers = servers_on_at_end_at + servers_off_at_end_at + servers_no_data
avg_uptime_pct chỉ tính trên (total_servers - servers_no_data) server
```

> **Bug đã sửa:** truy vấn `Totals` từng trộn `COUNT(DISTINCT server_id)` (đếm server)
> với `COUNT(*) FILTER (...)` (đếm **row** = server-day). Với cửa sổ 1 ngày thì số row
> tình cờ bằng số server nên không ai thấy. Với cửa sổ nhiều ngày, một server up 100%
> suốt 3 ngày cộng **3** vào `servers_uptime_100` trong khi `total_servers` chỉ tính
> nó **1** lần → `servers_uptime_partial` **âm**, và `top_10` trả cùng một server nhiều
> lần. Nay truy vấn gộp per-server (`GROUP BY server_id`) rồi mới phân loại.

### `servers_on_at_end_at` — vì sao tên dài như vậy

Đề bài hỏi "số lượng server On/Off". Hai field này trả lời đúng câu đó, nhưng tên cố
ý dài để không ai hiểu nhầm:

> Số ON/OFF là **ảnh chụp tại một thời điểm** — ở đây là cuối kỳ report. Nó không đại
> diện cho cả khoảng thời gian. Một server ON lúc 23:59 có thể đã OFF suốt 8 tiếng
> trước đó. Chỉ số phản ánh đúng chất lượng một khoảng thời gian là **uptime %**.

---

## 4. Coverage — vì sao không giấu

```text
coverage_pct < REPORT_COVERAGE_THRESHOLD (mặc định 95%)  → degraded = true
```

Report **nói ra** phần mình không đo được thay vì lặng lẽ tính trên số liệu thiếu.
Server `no_data` góp vào mẫu số `expected_checks` nên nó **không biến mất** khỏi
coverage — nếu loại nó ra, mất 30% dữ liệu vẫn hiện coverage 100%.

---

## 5. Đọc report

```text
GET /api/v1/reports/summary?start_date=&end_date=
  ├─ RequireScope("report:view")
  ├─ ParseRange:
  │     sai format          → REPORT_INVALID_RANGE
  │     start > end         → REPORT_INVALID_RANGE
  │     end >= hôm nay      → REPORT_INVALID_RANGE  (ngày chưa kết thúc thì chưa có nghĩa)
  │     quá REPORT_MAX_RANGE_DAYS → REPORT_INVALID_RANGE
  ├─ MissingDates(start, end) → có ngày thiếu snapshot?
  │     → REPORT_DATA_UNAVAILABLE, NÊU TÊN NGÀY
  └─ Totals + CountByLastStatus(end) + LowestUptime
```

Thiếu snapshot thì **từ chối và nêu tên ngày**, chứ không lặng lẽ tính trung bình
trên một cái lỗ.

Report **chỉ đọc `daily_snapshots`**, không chạm ES. Nhờ vậy ES chết không làm hỏng
report của những ngày đã snapshot.

---

## 6. Gửi email

```text
POST /api/v1/reports          RequireScope("report:send")
  │
  ├─ Tạo report_jobs (state=processing)   ← row có TRƯỚC khi gửi
  ├─ Summary → state=generated, lưu response_json
  ├─ Render HTML → state=sending
  ├─ SMTP (net/smtp, STARTTLS, Gmail)
  └─ state cuối:
        thành công                        → sent + smtp_message_id
        lỗi TRƯỚC khi body lên dây        → failed
        lỗi SAU khi body lên dây          → delivery_unknown
```

### `delivery_unknown` — vì sao cần state thứ ba

Nếu lỗi xảy ra **sau** khi body đã đi, không ai biết mail có tới hay không. Gọi nó là
`failed` rồi retry có thể gửi **hai lần**; gọi nó là `sent` thì có thể mail chẳng bao
giờ tới. Ghi nhận sự thật là "không biết", kèm `smtp_message_id` để operator tự tra
hộp thư Sent.

Đây là lý do dùng `net/smtp` thay `gomail`: `gomail.DialAndSend` chỉ trả `error`,
không cho biết lỗi xảy ra trước hay sau khi body lên dây.

### Message-ID tự sinh

Dòng `250` của Gmail mang **queue ID**, không phải Message-ID. Tự sinh Message-ID theo
RFC 5322 mới đúng mục đích tra cứu hộp thư Sent.

### Envelope vs display name

`SMTP_FROM=VCS-SMS <a@gmail.com>` phải được `mail.ParseAddress` tách ra: envelope
`MAIL FROM` chỉ nhận **địa chỉ trần**. Nhét cả display name vào sẽ làm hỏng cả
envelope lẫn domain trong Message-ID.

### Recipient allowlist

`SMTP_RECIPIENT_DOMAINS` rỗng = **cho gửi tới bất kỳ ai** → hệ thống có thể bị lợi
dụng làm mail relay. Phải set trước khi lên production.

---

## 7. Báo cáo tự động hằng ngày — và cơ chế chống chạy trùng

```text
REPORT_SNAPSHOT_CRON = 30 0 * * *     → snapshot ngày hôm qua
REPORT_DAILY_CRON    = 0 10 * * *     → gửi report cho REPORT_DAILY_RECIPIENT
```

Cả hai theo giờ `Asia/Ho_Chi_Minh`. Snapshot trước, gửi mail sau — khoảng cách đủ rộng để
snapshot lỗi thì còn kịp chạy lại thủ công trước giờ gửi.

`REPORT_DAILY_RECIPIENT` rỗng = **không đăng ký job gửi mail** (chỉ log `Warn` lúc khởi
động). Job snapshot vẫn chạy bình thường.

### Scheduler *reconcile* mỗi phút, không nổ đúng khoảnh khắc cron

`robfig/cron` chỉ được dùng để **parse biểu thức**, không để đăng ký callback. Vòng lặp
thật là: mỗi 60 giây, với mỗi job, hỏi ba câu.

```text
tick 60s
  └─ due(schedule, now)?          giờ nổ HÔM NAY đã qua chưa
       ├─ chưa → bỏ qua
       └─ rồi
            └─ dependsOn done?    daily_report cần IsDone("snapshot", hôm_qua)
                 ├─ chưa → bỏ qua
                 └─ rồi
                      └─ TryClaim(job, hôm_qua, hostname, staleAfter=3m)
                           ├─ thua → bỏ qua (replica khác đang/đã làm)
                           └─ thắng → runClaimed: chạy job + heartbeat 30s song song
```

Nổ theo callback thì một replica boot lúc 10:05 sẽ **không bao giờ** biết job 10:00 chưa ai
làm. Reconcile thì nó thấy `due` = true, `cron_runs` chưa có dòng `done` cho ngày đó, và tự
nhận việc. Đây là điều làm cho một lần deploy giữa giờ cron không mất báo cáo.

`due()` chỉ xét **fire của hôm nay**: một ngày bị bỏ hoàn toàn (service tắt cả ngày) thì
không tự bù — phải gọi `POST /internal/snapshots/{date}`.

### Vì sao cần `cron_runs`

`report-service` chạy nhiều replica (`docker-stack.yml`: `replicas: 3`) và **mọi** replica
đều chạy scheduler. Không có trọng tài thì 10:00 sáng có ba email giống nhau bay ra.

Trọng tài là **PRIMARY KEY `(job_name, run_date)`** của bảng `cron_runs` — không có lock
nào khác. `run_date` là *ngày dữ liệu* (luôn là hôm qua), nên một job chỉ chạy đúng một lần
cho mỗi ngày, bất kể bao nhiêu replica hay bao nhiêu lần restart.

| State | Claim lại được? | Vì sao |
|---|:---:|---|
| `done` | ❌ | Xong rồi — đây là thứ chặn gửi hai lần |
| `failed` | ✅ ngay tick sau | Lỗi tạm thời tự khỏi mà không cần can thiệp |
| `running` | ✅ nếu `heartbeat_at` cũ hơn 3 phút | Phân biệt job chạy chậm với replica đã chết |

`heartbeat` chạy hai chiều: nó làm mới claim mỗi 30 giây, **và** khi phát hiện claim đã bị
cướp (`RowsAffected = 0`) nó **cancel context của job đang chạy**. Không có chiều thứ hai,
một replica treo mạng 4 phút rồi hồi phục sẽ tiếp tục ghi vào cùng `run_date` mà replica mới
đang xử lý.

Mọi `MarkDone`/`MarkFailed`/`Heartbeat` đều có `WHERE owner = ?`, nên replica đã mất claim
không ghi đè được kết quả của replica đang giữ.

### Claim một mình vẫn chưa đủ cho việc gửi mail

Claim bảo đảm "cùng lúc chỉ một replica chạy". Nó **không** nói gì về "lần chạy trước đã làm
tới đâu" — và một replica có thể chết **sau** khi SMTP đã nhận body nhưng **trước** khi ghi
kết quả. Vì vậy `runDailyReport` còn một chốt nữa:

```text
FindLatestDaily(hôm_qua)
  ├─ chưa có job nào                       → gửi
  ├─ có, state ∈ {processing, generated, failed}  → gửi lại (body chưa lên dây)
  └─ có, state ∈ {sending, sent, delivery_unknown} → log WARN, KHÔNG gửi
```

`sending` **không** resendable, dù nghe như "chưa gửi xong": nó được ghi *trước* khi gọi
SMTP, nên một replica chết đúng khoảng đó thì không ai biết mail đã đi hay chưa. Đoán "chưa"
là cách gửi hai lần.

### Trạng thái vận hành có thể tra được

```bash
docker exec vcs-sms-postgres psql -U vcs_admin -d report_db -c \
  "SELECT job_name, run_date, state, owner, finished_at, error_message
     FROM cron_runs ORDER BY run_date DESC LIMIT 10;"
```

Đây là lý do chọn một dòng PostgreSQL thay vì Redis lock: lock hết TTL là mất mọi dấu vết,
còn `cron_runs` để lại `owner`, `started_at`, `finished_at`, `error_message` — đúng những
thứ cần khi 10:00 sáng mai không có email.
