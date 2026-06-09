# Chiến lược Cơ sở dữ liệu: Shared Instance, Separate Schemas

Trong hệ thống VCS-SMS, chúng ta sử dụng PostgreSQL 17 làm cơ sở dữ liệu quan hệ chính. Quyết định quan trọng nhất ở đây là sử dụng **Shared Instance, Separate Logical Schemas** (Chung 1 server vật lý, nhưng tách schema logic).

## 1. Khái niệm Schema trong PostgreSQL

Trong MySQL, một "Database" thường đồng nghĩa với một tập hợp các bảng.
Nhưng trong PostgreSQL, cấu trúc có 3 tầng: **Database Server (Instance)** > **Database** > **Schema** > **Table**.

Thay vì tạo 5 Database hoàn toàn riêng biệt (ví dụ `vcs_auth_db`, `vcs_server_db`), chúng ta tạo **1 Database duy nhất** tên là `vcs_sms`. Bên trong Database đó, chúng ta tạo ra 5 **Schemas**:
- `auth_schema`
- `server_schema`
- `monitor_schema`
- `report_schema`
- `fileio_schema`

## 2. Tại sao lại dùng cách này?

### 2.1. Đạt được ranh giới (Boundaries) của Microservice
Quy tắc vàng của Microservice là: **Mỗi service phải sở hữu dữ liệu của riêng nó**. Service A không được chọc trực tiếp vào Database của Service B.

Bằng cách tạo các schema riêng và **DB Users riêng**, ta thiết lập được ranh giới này ở cấp độ quyền hạn (Permissions):
- User `auth_user` chỉ có quyền trên `auth_schema`.
- User `report_user` chỉ có quyền trên `report_schema`.
Nếu lập trình viên vô tình viết một câu query từ `report-service` cố gắng `DELETE FROM auth_schema.users`, Postgres sẽ chặn lại ngay lập tức (Permission Denied).

### 2.2. Nhẹ tài nguyên (Resource Efficiency)
Mỗi một Database trong PostgreSQL đều duy trì một tập hợp các background worker processes và shared memory riêng. Chạy 5 Postgres databases riêng biệt sẽ tiêu tốn RAM và CPU overhead đáng kể, trong khi số lượng bảng của chúng ta không quá nhiều.
Dùng Shared Instance giúp tối ưu tài nguyên cho một hệ thống quy mô vừa, rất phù hợp khi chạy bằng Docker trên máy local hoặc server nhỏ.

### 2.3. Giải quyết bài toán Cross-Schema Read (Đọc chéo)
Đôi khi, nguyên tắc "Không chọc DB của nhau" gây ra khó khăn lớn về hiệu năng.
Ví dụ: `monitor-service` cần danh sách IP của 10.000 servers để ping. Danh sách này nằm ở bảng `servers` của `server-service`.
- **Cách Microservice chuẩn mực**: `monitor-service` gọi HTTP API sang `server-service` yêu cầu danh sách. Nhược điểm: Truyền 10.000 bản ghi qua mạng HTTP rất chậm, tốn băng thông, và phụ thuộc (nếu `server-service` chết, `monitor-service` cũng chết theo).
- **Cách Shared Schema giải quyết**: Vì cùng nằm trên 1 Database vật lý, ta có thể `GRANT SELECT` (Cấp quyền Đọc) trên bảng `server_schema.servers` cho `monitor_user`. Khi đó `monitor-service` có thể query thẳng vào bảng servers với tốc độ của mạng nội bộ DB.
**Lưu ý:** Chỉ cấp quyền ĐỌC (SELECT), quyền GHI (INSERT/UPDATE/DELETE) vẫn thuộc độc quyền của `server-service`. Điều này giữ được sự nhất quán dữ liệu (Data Integrity).

## 3. Quản lý Connection Pool

Khi ứng dụng Go kết nối với DB bằng GORM, nó sẽ duy trì một "Pool" các kết nối mở sẵn. Vì chúng ta chia thành 5 services, sẽ có 5 Connection Pools độc lập đập vào cùng 1 Postgres server. Cần cấu hình `MaxOpenConns` trong Go sao cho tổng số connection của 5 service không vượt quá giới hạn cấu hình của Postgres (thường là 100).

## 4. Bổ trợ bằng Redis và Elasticsearch

Postgres không phải công cụ vạn năng:
- **Redis (Ver 8)** được dùng cho dữ liệu siêu nhanh, siêu ngắn hạn: Rate Limiting, Distributed Lock (Khóa phân tán), JWT Blacklist, Caching API.
- **Elasticsearch (Ver 8)** được dùng cho dữ liệu Log cực lớn (Time-series data). Mỗi phút 10.000 server sinh ra 10.000 bản ghi status. Khi chạy liên tục 24/24, 1 ngày có thể lên tới 14.4 triệu bản ghi. Ở chế độ demo (chỉ chạy khi bật hệ thống), số lượng bản ghi sẽ tỷ lệ thuận với thời gian chạy thực tế. Đổ dữ liệu này vào Postgres sẽ làm nó phình to và chậm chạp. Elasticsearch sinh ra để giải quyết bài toán index và aggregate (tính toán thống kê) dữ liệu log khổng lồ này với tốc độ mili-giây.
