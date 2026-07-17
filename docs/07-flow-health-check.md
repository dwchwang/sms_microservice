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
net.DialTimeout("tcp", ipv4:tcp_port, 3s)
  thành công → ON,  latency = thời gian connect
  thất bại   → OFF, error_code = TIMEOUT | REFUSED | ...
```

Chỉ là **TCP connect**, không gửi payload, không đọc gì. Câu hỏi là "cổng có mở
không", và connect trả lời đúng câu đó với chi phí thấp nhất.

`MONITOR_TCP_DIAL_HOST` cho phép ghi đè IP đích để TCP Simulator trả lời thay khi dev.

---

## 3. Lua script — trái tim của flow

```text
KEYS: monitor:status:{server_id}, stream:monitor.status
ARGV: server_id, status, checked_at(RFC3339), latency_ms, round_id

round_id <= old_round?            → return 0    (không ghi gì)
HSET status, last_checked_at, latency_ms, round_id
old_status == false (lần đầu)?    → XADD, return 1
old_status ~= new_status?         → XADD, return 1
                                    return 2    (không phát event)
```

Năm trường hợp và kết quả:

| Trường hợp | Mã |
|---|---|
| Check đầu tiên (`UNKNOWN → ON`) | `1` |
| Status không đổi (`ON → ON`) | `2` |
| Round cũ tới muộn | `0` |
| Replay đúng round đã ghi | `0` |
| Transition thật (`ON → OFF`) | `1` |

Trường hợp `2` chiếm gần như toàn bộ 10.000 ping mỗi phút. Đó là lý do
`server:list:version` đứng yên và cache sống.

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
