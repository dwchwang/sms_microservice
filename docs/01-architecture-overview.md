# Kiến trúc Microservice & API Gateway

Tài liệu này giải thích lý do đằng sau các quyết định kiến trúc tổng quan của hệ thống VCS-SMS, bao gồm Microservice, Monorepo và API Gateway.

## 1. Tại sao lại là Microservices?

Với một hệ thống quản lý 10.000 server, tải trọng (workload) của các chức năng là rất khác nhau:
- **Monitor Service**: Hoạt động liên tục, định kỳ mỗi phút quét 10.000 server. Đây là service chịu tải tính toán (CPU) và mạng (Network I/O) nặng nhất.
- **TCP Simulator Service**: Service hạ tầng hỗ trợ, quản lý 10.000 TCP listeners, mở/đóng port động theo công thức toán học để tạo mục tiêu cho Monitor Service ping TCP thật. Đây không phải business service mà là công cụ giả lập server vật lý.
- **Server Service (CRUD)**: Tải thấp, chỉ thỉnh thoảng có thao tác thêm/sửa/xóa từ người dùng.
- **Report & File I/O**: Tải bất chợt (spiky workload), cần nhiều RAM để parse/gen file Excel hoặc gom dữ liệu lớn.

**Lợi ích khi chia Microservice:**
- **Mở rộng độc lập (Independent Scaling)**: Khi số lượng server tăng lên 50.000, ta chỉ cần scale (tăng số lượng instance) cho `Monitor Service` và `TCP Simulator` mà không cần cấp thêm RAM/CPU lãng phí cho `Auth` hay `File I/O`.
- **Cô lập lỗi (Fault Isolation)**: Nếu tính năng Import Excel bị lỗi out-of-memory và crash service, nó chỉ làm sập `File I/O Service`. Monitor Service vẫn tiếp tục ping server bình thường, hệ thống cốt lõi không bị ảnh hưởng.
- **Bảo mật**: `Auth Service` chứa logic nhạy cảm về mật khẩu có thể được bảo vệ chặt chẽ, các service khác không hề biết cấu trúc bảng Users.

## 2. Chiến lược Monorepo

Dù chia thành 5 microservices, toàn bộ code được đặt trong cùng một Git Repository (Monorepo) thay vì 5 repos rời rạc (Polyrepo).

**Lợi ích của Monorepo trong dự án này:**
- **Quản lý phiên bản thống nhất**: Không lo tình trạng service A dùng thư viện Kafka ver 1.0, service B dùng ver 2.0 gây lỗi không tương thích.
- **Thư mục `shared/` (Shared Library)**: Dễ dàng chia sẻ các cấu trúc dữ liệu chung (Errors, Logger chuẩn, Kafka Interface) mà không cần phải publish thành một thư viện Go package lên Github/Gitlab rồi kéo về. Mọi thay đổi ở `shared` lập tức khả dụng cho toàn bộ service.
- **Deploy dễ dàng**: Chỉ cần 1 file `docker-compose.yml` duy nhất ở root là có thể dựng toàn bộ hệ thống lên môi trường local để test.

## 3. Pattern API Gateway tự viết bằng Gin

Hệ thống của chúng ta sử dụng một API Gateway đứng trước 5 microservices. Thay vì dùng các giải pháp thương mại/mã nguồn mở đồ sộ như Kong hay Traefik, chúng ta tự viết một Gateway nhỏ gọn bằng Gin framework.

### Trách nhiệm của API Gateway:
1. **Entry Point Duy Nhất**: Client (Frontend, Mobile, Postman) chỉ gọi vào một port duy nhất (8080). Gateway sẽ đóng vai trò như một "nhân viên điều phối", định tuyến (route) request tới đúng service (`/api/v1/auth/` -> Auth Service, `/api/v1/servers/` -> Server Service). Nhờ đó client không cần biết topology (địa chỉ IP/Port) của các service bên dưới.
2. **Xác thực tập trung (Authentication Offloading)**: Thay vì cả 5 service đều phải nhúng logic giải mã JWT, Gateway sẽ làm việc này. Nó parse JWT, kiểm tra tính hợp lệ, lấy ra `user_id` và các quyền (`scopes`), sau đó nhúng (inject) thông tin này vào HTTP Header trước khi forward xuống service dưới.
3. **Chống Spam (Rate Limiting)**: Sử dụng Redis để giới hạn (ví dụ 100 requests/phút/IP), chặn đứng các cuộc tấn công DDoS ở ngay vòng gửi xe, bảo vệ các backend services.
4. **Log tập trung**: Sinh ra một `request_id` duy nhất và ghi log thời gian response cho toàn bộ hệ thống.

**Tóm lại:** API Gateway giúp các microservice backend nhẹ hơn: phần kiểm tra JWT, rate limit và scope được xử lý tập trung ở gateway, còn các service phía sau tập trung vào nghiệp vụ như tạo server, tính toán uptime, import/export và gửi báo cáo.
