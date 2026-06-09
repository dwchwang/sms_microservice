# Luồng vận hành: Health-Check 10.000 Server

Đây là trái tim của hệ thống, hoạt động bền bỉ, lặp đi lặp lại không ngừng nghỉ để theo dõi "nhịp tim" của 10.000 máy chủ.

## 0. TCP Simulator — "10.000 Server Ảo" Chạy Thật

Trước khi Monitor Service bắt đầu hoạt động, cần hiểu cơ chế giả lập 10.000 server:

**Bài toán:** Bạn lấy đâu ra 10.000 máy chủ thật đang chạy để ping? Không có server thật → TCP luôn fail → 100% Offline → Biểu đồ vô nghĩa.

**Giải pháp: TCP Simulator Service** — Một chương trình Go duy nhất, chạy trong Docker, quản lý 10.000 TCP listeners. Mỗi listener đại diện cho 1 "fake server".

1. Mỗi server được gán 1 port riêng (SRV-00001 → port 9001, SRV-10000 → port 19000).
2. Cứ mỗi **30 giây**, Math Engine tính toán xem mỗi server nên ON hay OFF:
   - Dựa trên `uptime_rate` (VD: 0.95 = 95% khả năng ON).
   - Cộng thêm biến thiên theo hàm Sin theo giờ (tạo pattern trồi sụt ban ngày/ban đêm).
   - Mỗi server có phase riêng (để không phải tất cả ON/OFF cùng lúc).
3. Nếu server nên **ON** → **mở TCP port** (chấp nhận kết nối rồi đóng ngay).
4. Nếu server nên **OFF** → **đóng TCP port** (connection refused).

**Kết quả:** Monitor Service ping TCP tới `tcp-simulator:9001` — nếu port đang mở thì "à, server này ON", nếu bị refused thì "server này OFF". Monitor Service **không hề biết đây là server giả** — code TCPChecker chạy y hệt production.

## 1. Chuẩn bị (Trước khi chạy)

Bộ máy `monitor-service` sở hữu một Đồng hồ đếm nhịp (Cron Scheduler) được đặt lịch cứ đúng **60 giây (1 phút)** là reo chuông một lần.

Khi chuông reo:
1. Nó chạy ra hỏi Redis: "Cho tôi xin cái chìa khóa (Lock)". Redis cấp khóa. Nếu có 1 bản sao `monitor-service` thứ 2 cũng chạy ra xin khóa, nó sẽ bị Redis từ chối để đảm bảo chỉ 1 người được làm việc.
2. Nó kết nối qua Postgres (với quyền đọc chéo Schema), lấy về danh sách toàn bộ 10.000 server đang hoạt động (không bị xóa). Mỗi server có `ipv4 = "tcp-simulator"` và `tcp_port` riêng (9001–19000).

## 2. Quá trình "Ping" (Worker Pool)

Thay vì 1 người chạy đi kiểm tra 10.000 nhà, `monitor-service` phái ra **100 công nhân (Workers)** làm việc song song.

Tất cả 10.000 servers đều được check bằng **TCP Connect thật**:
- Công nhân sẽ gọi `net.DialTimeout("tcp", "tcp-simulator:9001", 5s)` — mở kết nối TCP thật tới TCP Simulator Service.
- Nếu TCP Simulator đang mở port đó (server ON theo toán học): kết nối **thành công** ✅ → server được tính là **ON**.
- Nếu TCP Simulator đã đóng port đó (server OFF theo toán học): kết nối **bị từ chối** ❌ → server bị tính là **OFF**.
- Thời gian phản hồi (response time) cũng được đo thật (thường < 5ms trong Docker network).

## 3. Tổng hợp và Ghi nhận Kết quả (Batch Processing)

Khi 100 công nhân đã báo cáo xong toàn bộ 10.000 kết quả, hệ thống bắt đầu xử lý hậu kỳ. Đây là bước phải làm cực kỳ tối ưu để không làm "sập" Database.

**Bước 3.1: So sánh trạng thái**
Hệ thống cầm 10.000 kết quả mới đi so sánh với 10.000 kết quả của phút trước (đang lưu sẵn trên RAM/Redis).
Nó phát hiện ra: "À, 9.990 server trạng thái vẫn không đổi. Chỉ có 10 server vừa từ ON chuyển sang OFF (bị sập)".

**Bước 3.2: Phát sóng sự kiện thay đổi (Alerting Foundation)**
Với 10 server bị thay đổi trạng thái đó, nó lập tức ném 10 tin nhắn lên Kafka (kênh `server.status.changed`). Hệ thống Alert hoặc màn hình Frontend có thể nhận sự kiện này để kêu bíp bíp, chớp đỏ báo động ngay lập tức theo thời gian thực (Real-time).

**Bước 3.3: Cập nhật PostgreSQL cực nhẹ**
Hệ thống tạo ra 1 câu lệnh SQL duy nhất (Batch Update) gửi xuống PostgreSQL để cập nhật chữ "on" thành "off" cho đúng 10 server bị thay đổi kia. Bỏ qua 9.990 server không đổi. Database thở phào nhẹ nhõm. Cập nhật xong, nó lưu lại trạng thái mới nhất vào Redis cho phút sau so sánh.

**Bước 3.4: Đổ Log vào Elasticsearch (Lưu trữ Big Data)**
Mặc dù DB chỉ cập nhật 10 server, nhưng **Lịch sử nhịp tim (Log)** của cả 10.000 server đều phải được ghi lại để vẽ biểu đồ và tính Uptime.
Hệ thống đóng gói 10.000 bản ghi này thành 1 gói hàng khổng lồ (Bulk Request), gửi 1 lần duy nhất sang Elasticsearch. Elasticsearch (công cụ chuyên trị Big Data) sẽ nuốt trọn 10.000 bản ghi này trong vài chục mili-giây và đánh index (chỉ mục) để sau này tìm kiếm siêu tốc.

Vòng lặp kết thúc, các công nhân nghỉ ngơi chờ chuông reo ở phút tiếp theo.
