# Luồng vận hành: Import & Export Excel (Bất đồng bộ)

Bài toán Import 5000 server bằng file Excel chứa đựng rủi ro cực lớn về hiệu năng (Timeout). VCS-SMS giải quyết bằng cơ chế Xử lý nền (Background Job) kết hợp với Kafka.

## 1. Luồng Import Excel (Asynchronous)

Hãy tưởng tượng bạn ra ngân hàng làm thủ tục, thay vì đứng chờ nhân viên làm xong mới được đi về, bạn bốc số, đưa hồ sơ rồi đi uống cà phê. Lát sau có tin nhắn báo hồ sơ đã duyệt xong. Đó chính là Asynchronous (Bất đồng bộ).

1. **Upload File**: Người dùng chọn file `.xlsx` chứa hàng ngàn server và bấm Import. File bay thẳng vào API Gateway rồi truyền tới `fileio-service`.
2. **Khởi tạo Job**: `fileio-service` lập tức tạo một bản ghi "Hồ sơ" trong bảng `import_jobs` với trạng thái là `PENDING` (Đang chờ xử lý). Nó sinh ra một mã `Job_ID` (ví dụ: JOB-999).
3. **Phản hồi ngay lập tức**: Service trả ngay mã `JOB-999` về cho màn hình của người dùng. Màn hình người dùng tắt vòng xoay loading, hiện thanh tiến trình (Progress bar) và chuyển sang trạng thái chờ. Người dùng có thể đi thao tác màn hình khác.
4. **Đẩy việc vào Kafka**: Cùng lúc đó, `fileio-service` ném một sự kiện `import.job.created` lên Kafka (gửi kèm file hoặc đường dẫn file).
5. **Xử lý nền**: Consumer nền của chính `fileio-service` nhặt event từ Kafka. Nó mở file Excel đã lưu trong `uploads/`, đọc từng dòng, validate `server_id`, `server_name`, IPv4 và các trường tài nguyên.
6. **Lưu trữ hàng loạt**: `fileio-service` dùng quyền cross-schema INSERT để ghi các dòng hợp lệ vào `server_schema.servers`, đồng thời ghi kết quả từng dòng vào `fileio_schema.import_job_details`.
7. **Publish server.created**: Với mỗi server import thành công, `fileio-service` bắn event `server.created`. `monitor-service` nghe event này để tạo cấu hình health-check cho server mới.
8. **Cập nhật tiến độ**: Trong quá trình xử lý, service cập nhật `total_rows`, `success_count`, `failed_count`, `started_at`, `completed_at` và `error_message` vào bảng `import_jobs`.
9. **Hoàn tất**: Khi đọc hết file, công nhân đổi trạng thái Job thành `COMPLETED` hoặc `FAILED`. Cache danh sách server trong Redis được invalidate bằng pattern `server:detail:*` và `servers:list:*`.
10. **Tracking**: Trong lúc hệ thống xử lý ngầm, giao diện Web gọi `GET /api/v1/servers/import/{job_id}` để lấy tiến độ, danh sách dòng thành công và danh sách dòng lỗi.

## 2. Luồng Export Excel (Synchronous)

Ngược lại với Import, việc Export (Tải về file Excel) thường đòi hỏi phải trả file ngay lập tức về trình duyệt (Download).

1. Người dùng bấm nút "Export", có thể đính kèm theo các bộ lọc (ví dụ: Chỉ xuất các Server đang OFF).
2. Yêu cầu truyền tới `fileio-service`.
3. Service này sử dụng quyền Đọc chéo (Cross-schema SELECT) để móc danh sách server từ Database lên.
4. Nó sử dụng thư viện `excelize` để tự động tạo một file Excel ảo trong bộ nhớ RAM, vẽ các cột, đổ dữ liệu từng dòng vào.
5. Sau khi file Excel thành hình trong RAM, hệ thống cấu hình HTTP Headers dạng `Content-Type: application/vnd.openxmlformats-officedocument.spreadsheetml.sheet` và `Content-Disposition: attachment; filename="servers.xlsx"`.
6. Nó phun luồng byte (stream) của file thẳng về trình duyệt của người dùng. Một popup Tải về sẽ hiện lên trên máy của Admin.

Bằng cách giới hạn bộ lọc hoặc làm phân trang (Pagination) ngầm, việc Export được kiểm soát bộ nhớ chặt chẽ để tránh làm sập service khi người dùng lỡ tay bấm Export toàn bộ dữ liệu hệ thống cùng lúc.
