# 07 — Flow: health check

> Từ lúc scheduler nạp round tới lúc `status` trong PostgreSQL đổi.

---

## 1. Toàn cảnh một round

```text
──── t=0s ─────────────────────────────────────────────────
Scheduler (instance thắng lock):
  SETNX monitor:round:lock:{round_id}  TTL 120s
  kiểm tra server:monitor-target:ready
  SSCAN ids → RPUSH monitor:ping:queue:{round_id}   (10.000 phần tử)
  EXPIRE queue 120s
  SET monitor:round:current {round_id}

──── t=0..~30s ─────────────────────────────────────────────
600 goroutine (3 instance × 200), mọi instance:
  round = GET monitor:round:current
  id    = BRPOP monitor:ping:queue:{round}  (timeout 1s)
  target= HGETALL server:monitor-target:{id}   → nil thì bỏ qua (đã xóa)
  ON/OFF, latency = TCP connect(ipv4, tcp_port)  timeout 3s
  Lua script → 0 (cũ) | 1 (đổi) | 2 (không đổi)
  fact → bulk buffer → ES

──── t=60s ─────────────────────────────────────────────────
Round tiếp theo. Scheduler đo LLEN queue của round cũ → checks_missing.
Queue cũ hết hạn sau 120s.
```

---

## 2. TCP check

```text
(&net.Dialer{Timeout: MONITOR_TCP_TIMEOUT}).DialContext(ctx, "tcp", ipv4:tcp_port)
  thành công → ON,  latency = thời gian connect, conn.Close() ngay
  thất bại   → OFF, error_code phân loại
```

Chỉ là **TCP connect**, không gửi payload, không đọc gì. Câu hỏi là "cổng có mở
không", và connect trả lời đúng câu đó với chi phí thấp nhất.

Dùng `DialContext` chứ không `net.DialTimeout`: worker phải huỷ được lệnh dial đang treo
khi service nhận SIGTERM, nếu không shutdown phải chờ trọn `MONITOR_TCP_TIMEOUT`.

**Bốn mã lỗi** (`error_code` trong health fact — rỗng khi ON):

| Mã | Khi nào |
|---|---|
| `TIMEOUT` | `net.Error.Timeout()`, hoặc `context.Canceled` / `DeadlineExceeded` |
| `DNS_ERROR` | lỗi là `*net.DNSError` |
| `CONNECTION_REFUSED` | lỗi là `*net.OpError` (bao gồm cả refused thật) |
| `DIAL_ERROR` | mọi thứ còn lại |

Mã được **phân loại chứ không nuốt**: chúng vào Elasticsearch như một `keyword`, nên
`terms` agg trên `error_code` cho biết ngay là "10.000 server OFF" do fleet chết thật hay
do DNS trong container hỏng.

`MONITOR_TCP_DIAL_HOST` ghi đè **host** đích (giữ nguyên port) để TCP Simulator đứng thay
10.000 địa chỉ trên một hostname khi dev. Rỗng trong production. Đây là lý do seed script
đặt `tcp_port = 9000 + i`: port chính là thứ định danh server trên simulator.

---

## 3. Lua script — trái tim của flow

```text
KEYS: monitor:status:{server_id}, stream:monitor.status, monitor:uptime:index
ARGV: server_id, status, checked_at(RFC3339), latency_ms, round_id, day(VN)

round_id <= old_round?            → return 0    (không ghi gì)
HSET status, last_checked_at, latency_ms, round_id
field 'day' khác ARGV day?        → HSET day, day_total=0, day_on=0
HINCRBY day_total (+ day_on nếu ON) → ZADD uptime:index (day_on/day_total)*100
old_status == false (lần đầu)?    → XADD, return 1
old_status ~= new_status?         → XADD, return 1
                                    return 2    (không phát event)
```

Ba KEYS, không hai: `monitor:uptime:index` là KEYS[3]. Bộ đếm nằm **sau** chốt chặn round
nên một round phát lại không thổi phồng số, và **trước** phần XADD nên nó chạy cho *mọi*
lượt check, không chỉ lượt có transition.

Năm trường hợp và kết quả:

| Trường hợp | Mã | Bộ đếm ngày có tăng? |
|---|---|:---:|
| Check đầu tiên (`UNKNOWN → ON`) | `1` | ✅ |
| Status không đổi (`ON → ON`) | `2` | ✅ |
| Transition thật (`ON → OFF`) | `1` | ✅ |
| Round cũ tới muộn | `0` | ❌ |
| Replay đúng round đã ghi | `0` | ❌ |

Trường hợp `2` chiếm gần như toàn bộ 10.000 ping mỗi phút. Đó là lý do
`server:list:version` đứng yên và cache sống — trong khi `day_total` vẫn nhích đều và
dashboard uptime vẫn tươi.

---

## 4. Consumer đưa status vào PostgreSQL

```text
XREADGROUP group=server-svc consumer={hostname} ">" COUNT 100 BLOCK 2s
  │
  ├─ parse event → sai format? ACK rồi bỏ (kèm log Error)
  ├─ UPDATE servers SET status=?, status_changed_at=?, status_version=?
  │     WHERE server_id=? AND status_version < ?
  │     ├─ RowsAffected > 0 → đánh dấu cần bump
  │     └─ RowsAffected = 0 → event cũ hoặc server đã xóa; vẫn ACK
  ├─ XACK cả batch
  └─ có row đổi? → INCR server:list:version (một lần cho cả batch)
```

`XAUTOCLAIM` chạy mỗi 30s, tiếp quản message pending quá 60s của consumer đã chết.

---

## 5. Health fact vào Elasticsearch

```text
Fact { server_id, server_name, status, checked_at, round_id, latency_ms, error_code }
  → FactBuffer (flush khi đủ 1000 hoặc mỗi 5s)
  → _bulk vào index server-status-logs-YYYY.MM.DD
      _id = "{server_id}:{round_id}"     ← tất định
```

**ES là đường nhánh.** Một lần check vẫn được tính là đã xảy ra kể cả khi fact bị mất
— status trong Redis/PostgreSQL vẫn đúng. ES chết chỉ làm snapshot đêm nay thiếu data.

`server_name` được denormalize vào fact vì Monitoring không đọc PostgreSQL, mà report
lịch sử cần "tên tại thời điểm check" chứ không phải tên hiện tại.

---

## 6. Vì sao `checks_missing` quan trọng nhất

```text
checks_missing = LLEN monitor:ping:queue:{round_id}  đo lúc round kết thúc
```

Nếu worker không kịp ping hết queue trong 60s, phần dư nằm lại và biến mất cùng
queue khi nó hết hạn. Không có metric này thì hệ thống **ping thiếu server mà không
ai biết** — không có lỗi, không có exception, chỉ là vài server im lặng không được đo.

`checks_missing > 0` liên tục = thêm worker hoặc thêm instance.

---

## 7. Bảng lỗi

| Tình huống | Hành vi | Nhìn thấy ở đâu |
|---|---|---|
| Thiếu marker `ready` | Bỏ qua cả round | Log Warn + `targets_expected` = 0 |
| Server bị xóa giữa round | Bỏ qua im lặng | — |
| `tcp_port` trong hash hỏng | Lỗi cho riêng target đó | Log Error |
| Redis chết | Worker backoff 1s rồi thử lại | Log Error |
| ES chết | Buffer giữ rồi drop khi đầy | `es_bulk_failure`, coverage giảm |
| Mất consumer group | Tự tạo lại tại `0`, replay | Log Warn (~4s) |
| `changed_at` sai format | **ACK rồi vứt im lặng** | Chỉ có log Error |
