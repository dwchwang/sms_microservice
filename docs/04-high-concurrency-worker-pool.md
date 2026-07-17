# 04 — Concurrency: round, worker pool, sizing

> Bài toán: ping **10.000 server mỗi 60 giây**, TCP timeout 3s, nhiều instance chạy
> song song mà không dẫm chân nhau.

---

## 1. Round là gì

```text
round_id = redis_time_unix / 60
```

Mỗi phút là một round có ID xác định. Ba tính chất quan trọng:

**Lấy từ Redis TIME, không phải `time.Now()`.** Nhiều instance mà dùng đồng hồ máy
thì lệch nhau vài giây là đã tính ra `round_id` khác nhau → nạp/đọc queue khác nhau
→ vỡ toàn bộ cơ chế. Redis là đồng hồ chung.

**`RoundSeconds` là hằng số, không phải config.** Hai instance cấu hình khác nhau sẽ
cho `round_id` không thống nhất. Đây là lý do `MONITOR_CHECK_INTERVAL` bị bỏ khỏi `.env`.

**Scheduler neo vào ranh giới round, không dùng ticker cố định.** Mỗi vòng nó đọc lại
Redis TIME và ngủ tới ranh giới kế tiếp. Ticker cố định sẽ chạy theo pha lúc process
boot — queue nạp giữa chừng round, và một tick trễ >60s làm Go bỏ tick, mất nguyên một
round mà `checks_missing` không thấy vì queue chưa từng tồn tại.

---

## 2. Scheduler và worker pool

```text
Mỗi instance chạy CẢ HAI:

Scheduler (mỗi round):
  SETNX monitor:round:lock:{round_id}   ← chỉ 1 instance thắng
  thua lock → không làm gì, bình thường
  thắng lock:
    kiểm tra server:monitor-target:ready   ← thiếu marker → BỎ QUA round
    SSCAN server:monitor-target:ids → RPUSH monitor:ping:queue:{round_id}
    EXPIRE queue 120s
    SET monitor:round:current {round_id}   ← đặt CUỐI CÙNG

Worker pool (200 goroutine, MỌI instance):
  vòng lặp: đọc monitor:round:current → BRPOP queue của round đó
```

**Lock chỉ dành cho scheduler. Mọi instance đều ping.** Thua lock không có nghĩa là
ngồi không — worker của instance đó vẫn `BRPOP` từ queue mà instance thắng đã nạp.
Đây là cách công việc tự chia đều: ai rảnh thì pop.

**`SET monitor:round:current` đặt cuối cùng** để worker nhìn thấy round mới là chắc
chắn tìm được queue đã nạp xong.

**Worker không nhớ round.** Mỗi vòng lặp nó đọc lại `current`. Round mới bắt đầu thì
vòng sau nó pop từ queue mới, còn queue cũ tự hết hạn. Đó là toàn bộ cơ chế chuyển round.

### Marker `ready`

Monitoring **bỏ qua mọi round** nếu thiếu `server:monitor-target:ready`. Marker này
chỉ do lệnh rebuild đặt. Vì vậy: **sau khi seed hoặc khôi phục dữ liệu, luôn chạy
`make rebuild-cache`**, nếu không Monitor sẽ không ping gì cả.

---

## 3. Sizing worker

```text
Mỗi check tốn tối đa 3s (TCP timeout).
Một goroutine làm được 60s / 3s = 20 check mỗi round.
10.000 server / 20 = 500 goroutine.
+20% headroom → 600 goroutine → 3 instance × 200 goroutine.
```

**"Worker" ở đây là goroutine, không phải process hay container.** 200 goroutine chờ
I/O trong một process Go là chuyện bình thường — mỗi goroutine chỉ tốn vài KB stack.
Hệ thống **không** cần 500 container.

### Redis pool phải phủ hết số worker

`BRPOP` **giữ connection suốt thời gian block**. Nếu pool nhỏ hơn số worker thì:

- Số worker chạy thật bị chặn ở kích thước pool, không phải `MONITOR_WORKER_COUNT`.
- Scheduler phải xếp hàng sau worker cho **từng** lệnh SSCAN/RPUSH → nạp queue chậm hàng chục giây.

go-redis mặc định `PoolSize = 10 × GOMAXPROCS` (= 80 trên máy 8 CPU) — **nhỏ hơn 200**.
Code đặt `PoolSize = worker + 16`, phần dư dành cho scheduler, sampler và fact buffer.

### Số đo thật (10.001 server, 1 instance, 200 goroutine)

| Chỉ số | Giá trị |
|---|---|
| `round_duration` trung bình | **3,5s** (ngân sách 60s) |
| `checks_missing` | 0 |
| `tcp_latency` trung bình | 4,3ms |
| Rebuild projection | 1,7s |

Công thức 600 goroutine là **cận trên xấu nhất**, giả định mọi check tốn trọn 3s timeout.
Khi server trả lời bình thường (~4ms), 200 goroutine trên 1 instance đã thừa. Con số 600
vẫn đúng cho tình huống phần lớn server chết.

---

## 4. Bảy metric bắt buộc

Monitor expose `/metrics` (Prometheus) trên port nội bộ 8083.

| Metric | Ý nghĩa |
|---|---|
| `vcs_monitor_round_duration_seconds` | Thời gian từ lúc round bắt đầu tới khi queue cạn |
| `vcs_monitor_targets_expected` | Số target scheduler nạp vào queue |
| `vcs_monitor_checks_completed_total` | Số ping instance này hoàn thành |
| **`vcs_monitor_checks_missing`** | **`LLEN` queue đo lúc round kết thúc — việc chưa kịp ping** |
| `vcs_monitor_queue_depth` | Độ sâu queue hiện tại |
| `vcs_monitor_tcp_latency_seconds` | Histogram độ trễ TCP connect |
| `vcs_monitor_es_bulk_failure_total` | Số batch bulk bị bỏ sau khi retry hết |

**`checks_missing` là tín hiệu duy nhất báo thiếu worker.** Không có nó, hệ thống ping
thiếu server mà không ai biết. `checks_missing > 0` liên tục = cần thêm worker hoặc
thêm instance.

---

## 5. Bulk buffer có giới hạn

```text
flushSize     1000 fact
flushInterval 5s
capacity      MONITOR_FACT_CAPACITY (mặc định 50.000)
retry         3 lần, backoff tăng dần
```

Buffer **có trần**. ES outage kéo dài thì nó **drop fact** chứ không phình tới lúc
process chết. Report sẽ hiển thị coverage giảm — chuyện đó khắc phục được; OOM thì không.

> **Lỗi thầm lặng đã sửa:** `_bulk` của Elasticsearch trả **HTTP 200 kể cả khi từng
> document bị từ chối** (ví dụ `429 es_rejected_execution_exception` lúc tải cao — đúng
> kịch bản 10.000 server). Chỉ xét HTTP status thì fact biến mất không dấu vết và
> retry không bao giờ chạy. Code hiện đọc cờ `errors` trong body và trả lỗi để retry
> hoạt động — `_id` tất định đảm bảo retry không nhân bản document.

---

## 6. Tính đúng đắn dưới song song

| Rủi ro | Vì sao không thành vấn đề |
|---|---|
| `SSCAN` trả trùng phần tử | Server bị ping 2 lần trong round → Lua trả `0` (round không lớn hơn) và ES ghi đè cùng `_id`. Vô hại. |
| Hai instance cùng pop một server | `BRPOP` là atomic — chỉ một bên nhận được |
| Server bị xóa giữa round | `GetTarget` trả nil → bỏ qua |
| Ping xong nhưng round đã qua | Lua so `round_id` → trả `0`, không ghi |
| Instance chết giữa round | Việc còn lại nằm trong queue, instance khác pop tiếp |
