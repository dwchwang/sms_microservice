# Pattern Đồng thời cao (High-Concurrency): Worker Pool & Distributed Lock

Điểm ăn tiền nhất và cũng khó nhất của bài toán (2.0 điểm) là: **Kiểm tra trạng thái (Health Check) 10.000 server định kỳ mỗi phút**.

Nếu code không khéo, việc mở 10.000 kết nối TCP cùng 1 lúc sẽ làm cạn kiệt tài nguyên máy chủ (socket exhaustion, OOM), hoặc mất quá 1 phút mới quét xong, dẫn tới job bị chồng chéo.

Dưới đây là cách VCS-SMS giải quyết bài toán này.

## 1. Pattern Worker Pool trong Golang

Golang có `Goroutine` rất nhẹ, bạn có thể `go checkServer()` 10.000 lần một lúc. Tuy nhiên, việc bung (fan-out) quá nhiều goroutine đồng thời có thể gây sốc cho network interface card (NIC) và CPU context switching.

**Giải pháp: Worker Pool**
Thay vì mở 10.000 luồng, chúng ta chỉ tạo cố định một nhóm công nhân (Worker Pool) gồm **100 goroutines**.
- Ta có một băng chuyền (Channel trong Go) tên là `jobs`. Ta thả 10.000 server vào băng chuyền này.
- 100 công nhân sẽ liên tục đứng chực ở băng chuyền. Ai rảnh thì lấy 1 server ra ping. Ping xong lại lấy tiếp.
- Cứ như vậy, tại bất kỳ thời điểm nào, cũng chỉ có tối đa 100 kết nối TCP đang mở. Tài nguyên được kiểm soát chặt chẽ (Throttling).

```text
               /-- Worker 1 --\ 
10.000 Servers --- Worker 2 --- (xử lý TCP/Simulator) ---> [Kết quả Channel]
(Channel jobs) \-- Worker N --/
```

Với timeout TCP là 5 giây. Trong trường hợp xấu nhất toàn bộ server đều sập (chờ đủ 5s mới fail):
1 Worker xử lý được 60s / 5s = 12 servers/phút.
100 Workers xử lý được 100 * 12 = 1.200 servers/phút (Vẫn chưa đủ nếu timeout liên tục).
Thực tế, kết nối thành công hoặc lỗi rớt mạng thường trả về trong <100ms. Nhưng để an toàn cho 10.000 servers, hệ thống cho phép cấu hình số lượng worker qua `.env` (ví dụ tăng lên 500 workers).

## 2. Distributed Lock (Khóa phân tán) với Redis

Hệ thống được thiết kế Microservice, tức là bạn có thể chạy 3 instances của `monitor-service` cùng lúc để tăng tính sẵn sàng (High Availability).
Tuy nhiên, cả 3 instances đều có Scheduler Cron job kích hoạt mỗi phút một lần. Nếu không cẩn thận, cả 3 sẽ cùng quét 10.000 server, tạo ra 30.000 kết nối -> Dư thừa và sai lệch dữ liệu log.

**Giải pháp: Redis Distributed Lock**
Trước khi thực hiện vòng quét định kỳ, service phải "giành quyền" thực thi bằng cách tạo một chìa khóa (Key) trong Redis.

1. Instance A tới Redis nói: "Cho tôi tạo key `health-check-lock` với TTL 90 giây". Redis trả lời: "OK, key chưa tồn tại, anh được phép chạy".
2. Cùng lúc đó, Instance B tới Redis đòi tạo key `health-check-lock`. Redis trả lời: "Key đã có chủ (bị khóa), anh không được phép chạy".
3. Instance B sẽ bỏ qua vòng quét lần này, đi ngủ. Chỉ 1 mình Instance A quét 10.000 servers.

Khóa có **TTL (Time to Live) 90 giây**. Nếu Instance A đang quét dở bị crash (cúp điện), sau 90 giây khóa sẽ tự bốc hơi. Phút tiếp theo Instance B sẽ có thể giành quyền quét tiếp. Đây là cơ chế tránh Deadlock (khóa vĩnh viễn).

## 3. TCP Simulator Pool: Giả lập 10.000 Server Thật

Vấn đề thực tế khi làm bài: Bạn lấy đâu ra 10.000 IP thật đang sống để test? Nếu toàn IP ảo, kết nối TCP sẽ luôn bị timeout, hệ thống sẽ chậm và kết quả 100% Offline (chẳng có gì thú vị để vẽ biểu đồ báo cáo).

**Giải pháp: TCP Simulator Pool**
Thay vì sử dụng 2 mode riêng biệt (TCP cho thật, Simulator cho giả lập), VCS-SMS kết hợp cả hai thành **một giải pháp thống nhất**:

1. **TCP Simulator Service** (`tcp-simulator`): Một chương trình Go duy nhất, chạy trong Docker, quản lý **10.000 TCP listeners** — mỗi listener đại diện cho 1 "fake server".
2. **Math Engine**: Cứ mỗi **30 giây**, Math Engine tính toán xem mỗi server nên ở trạng thái ON hay OFF dựa trên:
   - `uptime_rate` (VD: 0.95 = 95% khả năng ON)
   - Biến thiên hàm Sin theo giờ trong ngày (tạo pattern trồi sụt realistic)
   - Offset riêng cho mỗi server (để không phải tất cả cùng ON/OFF 1 lúc)
3. **Mở/Đóng Port Động**: Nếu server nên ON → mở TCP port (chấp nhận kết nối rồi đóng ngay). Nếu server nên OFF → đóng TCP port (connection refused).
4. **Monitor Service dùng TCP Connect thật**: `net.DialTimeout("tcp", "tcp-simulator:9001", 5s)`. Port mở = ON ✅. Port đóng = OFF ❌.

**Tại sao cách này ưu việt hơn?**
- Monitor Service chạy **y hệt production** — code TCPChecker không hề biết đây là server giả. Nó chỉ biết: "Ping TCP thành công thì ON, thất bại thì OFF".
- Test được toàn bộ: worker pool, timeout handling, error handling, batch write Elasticsearch.
- Trạng thái On/Off vẫn có pattern trồi sụt realistic nhờ công thức toán học.
- Chỉ thêm **1 container** (~100-256MB RAM) thay vì 10.000 containers.

```text
                                    TCP Simulator Service
                                   ┌────────────────────┐
                /-- Worker 1 --\    │  Port 9001: OPEN ✅ │
10.000 Servers --- Worker 2 --- ──▶│  Port 9002: CLOSED❌│
(Channel jobs) \-- Worker N --/    │  Port 9003: OPEN ✅ │
                                   │  ...                │
   Monitor Service                 │  Math Engine: mỗi  │
   (100 Workers, TCP Connect)      │  30s tính On/Off   │
                                   └────────────────────┘
```

## 4. Ghi lô lớn (Bulk Index) vào Elasticsearch

Sau khi thu được 10.000 kết quả, ta không gọi Database 10.000 lần.
Ta gom chúng thành 1 mảng (Batch) và gửi 1 request duy nhất tới Elasticsearch gọi là **Bulk API**.
Đồng thời, ghi đè 10.000 trạng thái hiện tại (Current Status) vào Redis bằng pipeline, giúp cho API xem danh sách server lấy trạng thái cực nhanh. PG Database chỉ được gọi `Batch Update` cho những server nào THỰC SỰ THAY ĐỔI trạng thái so với phút trước (VD: đang ON tự nhiên OFF) để tiết kiệm I/O.
