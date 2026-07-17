# 09 — Flow: dashboard, báo cáo & email

> Hai thứ khác nhau, hai nguồn khác nhau:
>
> | | Dashboard `/reports` | Báo cáo email |
> |---|---|---|
> | Câu hỏi | "Hiện giờ hệ thống thế nào?" | "Ngày X→Y hệ thống thế nào?" |
> | Nguồn | Bộ đếm luỹ kế trong **Redis** | **`daily_snapshots`** |
> | Có ngay không | **Có**, mọi lúc | Chỉ những ngày đã snapshot (00:30) |
> | Kỳ | Từ lúc server bắt đầu được giám sát | Khoảng ngày chỉ định |

---

## 0. Dashboard — uptime luỹ kế

```text
GET /api/v1/servers/uptime      scope: server:stats
```

Vấn đề: uptime là tỉ lệ **lịch sử**, mà `/servers` chỉ có trạng thái *hiện tại*.
Đọc ES mỗi lần mở dashboard thì vi phạm mục 12.4 (ES chết là dashboard chết) và
tốn kém; chờ snapshot 00:30 thì hôm nay không có gì để xem.

Giải pháp: **đếm ngay trong Lua script** vốn đã chạy mỗi lần check.

```lua
-- Sau version guard, nên replay/event cũ không làm phồng số đếm
local total = redis.call('HINCRBY', status_key, 'total_checks', 1)
if new_status == 'ON' then ons = redis.call('HINCRBY', status_key, 'on_checks', 1) end
redis.call('ZADD', 'monitor:uptime:index', (ons / total) * 100, server_id)
```

Chi phí: **0 round-trip thêm** — script đã `HSET` lên đúng key đó rồi.

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
| `top_10_lowest_uptime` | `ZRANGE idx 0 9 WITHSCORES` |
| `servers_on/off/unknown` | `/servers/stats` (PostgreSQL, cache 10s) |

Đo thật với 10.001 server: **0,125s**, cache 10s.

### Ràng buộc Redis

Bộ đếm là dữ liệu **không tái tạo được từ PostgreSQL**, nên:

- `--appendonly yes` — restart không mất số đếm.
- `--maxmemory-policy volatile-lru` chứ **không** `allkeys-lru`: chỉ cache mới có TTL.
  Bộ đếm, status, target projection và stream đều không TTL → được bảo vệ.
- Xóa server phải `ZREM` khỏi index và `DEL monitor:status:{id}`, nếu không server đã
  xóa vẫn chấm điểm trong dashboard mãi mãi.

Nếu Redis mất sạch dữ liệu, bộ đếm reset về 0 và uptime tính lại từ đầu — lịch sử thật
vẫn nằm nguyên ở `daily_snapshots`.

---

## 1. Vì sao có snapshot

Đề bài yêu cầu dùng Elasticsearch để tính uptime. Nhưng nếu **mỗi lần bấm nút report**
lại aggregate 14,4 triệu document thì: report chậm, ES chết là report chết, và không
thể giữ dữ liệu thô quá vài ngày.

Giải pháp: aggregate **một lần lúc 00:30** cho ngày hôm trước, cô đọng vào
`daily_snapshots`. Report chỉ đọc bảng đó.

```text
ES giữ raw fact 7 ngày (ILM xóa)
daily_snapshots giữ 10.000 row/ngày → nhiều năm
```

ES vẫn là thứ **tính** uptime bằng composite aggregation — chỉ là tính lúc 00:30
thay vì lúc có người bấm nút. **Job snapshot là consumer duy nhất của ES.**

---

## 2. Job snapshot (00:30 hằng ngày)

```text
1. Population ← GET /internal/servers của server-service (cursor, ~10 request)
     server tồn tại trong ngày: created_at < end AND (deleted_at IS NULL OR > start)

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

Chạy lại thủ công: `POST /internal/snapshots/{date}` (chỉ network nội bộ).

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

## 7. Báo cáo tự động hằng ngày

```text
REPORT_SNAPSHOT_CRON = 30 0 * * *     → snapshot ngày hôm qua
REPORT_DAILY_CRON    = 0 10 * * *     → gửi report cho REPORT_DAILY_RECIPIENT
```

Snapshot chạy 00:30, gửi mail 10:00 — khoảng cách đủ rộng để snapshot lỗi thì còn kịp
chạy lại thủ công trước giờ gửi.
