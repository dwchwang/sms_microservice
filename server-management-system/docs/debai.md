# CHƯƠNG TRÌNH ĐÀO TẠO VCS PASSPORT
## Đề bài project: Server Management System (VCS-SMS)

---

## 1. Đề bài
Công ty VCS hiện tại đang có khoảng **10.000 server**. Hãy xây dựng 1 hệ thống quản lý danh sách các server này.

* **Mô hình sơ lược:** Người dùng (User) tương tác qua API với hệ thống *VCS Server Management System* để kiểm tra trạng thái (Status) định kỳ hoặc chủ động của danh sách các Server cần quản lý (Server 1, Server 2,...).
* **Note:** * Các thành phần trong VCS-SMS do bạn tự định nghĩa và thiết kế.
    * Định nghĩa thế nào là server On/Off cũng do bạn tự quyết định.

---

## 2. Yêu cầu

### 2.1. Yêu cầu chức năng

#### A. Thông tin cơ bản của một Server
Một server sẽ gồm một số thông tin cơ bản sau:
* `server_id`: Mã định danh của 1 server (Không trùng nhau).
* `server_name`: Tên server (Không trùng nhau).
* `status`: Trạng thái On/Off của server.
* `created_time`: Thời gian tạo server.
* `last_updated`: Thời gian cập nhật cuối cùng.
* `ipv4`: Thông tin IPv4 của server.
* *Các thông tin thêm bạn cần tự định nghĩa để phù hợp với yêu cầu tính năng.*

#### B. Các chức năng thực hiện với hệ thống

##### 1. Kiểm tra trạng thái của server (2.0 điểm)
* VCS-SMS định kỳ kiểm tra trạng thái server trong danh sách cần quản lý và cập nhật status về hệ thống tập trung.

##### 2. Quản lý server
* **Tạo Server (0.25 điểm):**
    * *Mô tả:* Cho phép người dùng tạo 1 server với đầy đủ thông tin.
    * *Input:* Thông tin Server.
    * *Output:* Kết quả tạo server.
* **View server (0.25 điểm):**
    * *Mô tả:* Lấy ra danh sách server. Có thể kèm filter nếu người dùng nhập vào. Danh sách được phân trang và có hỗ trợ sort (sắp xếp) theo trường nào đó.
    * *Input:* Filter (optional), From/to (thông tin phân trang), Sort (trường và thứ tự sort).
    * *Output:* Số lượng server phù hợp và Danh sách server phù hợp.
* **Update server (0.25 điểm):**
    * *Mô tả:* Cập nhật thông tin 1 server. *Không cho phép cập nhật trường `server_id`*.
    * *Input:* `server_id` (Server cần cập nhật) và `update_data` (các thông tin cần update cho server).
    * *Output:* Thông tin server sau khi cập nhật.
* **Delete Server (0.25 điểm):**
    * *Mô tả:* Xóa 1 server khỏi danh sách cần quản trị.
    * *Input:* `server_id` (Server cần xóa).
    * *Output:* Kết quả xóa.

* **Import Servers (0.5 điểm):**
    * *Mô tả:* Cho phép tạo 1 danh sách nhiều server từ file Excel. Bỏ qua các `server_id` hoặc `server_name` đã tồn tại.
    * *Input:* File cần Import.
    * *Output:* Số lượng & Danh sách `server_id`/`server_name` đã import thành công; Số lượng & Danh sách `server_id`/`server_name` import thất bại.
* **Export Servers (0.5 điểm):**
    * *Mô tả:* Cho phép export 1 danh sách server ra file Excel.
    * *Input:* Filter (optional), From/to (thông tin phân trang), Sort (trường và thứ tự sort).
    * *Output:* File Excel chứa thông tin danh sách Server phù hợp.

##### 3. Báo cáo (1.0 điểm)
* **Báo cáo định kỳ (0.5 điểm):** Định kỳ 1 ngày 1 lần gửi thông tin Email cho quản trị với thông tin báo cáo tình trạng server của ngày trước đó, bao gồm:
    * Số lượng server trong hệ thống.
    * Số lượng server On.
    * Số lượng server Off.
    * Tỉ lệ thời gian Uptime trung bình của các server.
* **API Báo cáo chủ động (0.5 điểm):** Xây dựng API để có thể chủ động report thông tin trên.
    * *Input:* Ngày bắt đầu và ngày kết thúc muốn report, email quản trị.
    * *Output:* Kết quả và email gửi đến quản trị.

---

### 2.2. Yêu cầu phi chức năng (5.0 điểm)

#### Quy tắc code:
* Sử dụng **OpenAPI** *(0.5 điểm)*
* Sử dụng **Unit Test** với Code coverage đạt **>= 90%** *(0.5 điểm)*
* Sử dụng các thư viện thao tác với Database để **chống SQL Injection** *(0.5 điểm)*
* Output của API phải **định nghĩa rõ các trường hợp thành công, lỗi** với mã code và thông tin mô tả rõ ràng *(0.5 điểm)*
* Ghi **log ra file** và có **logrotate** *(0.5 điểm)*

#### Xác thực / Phân quyền:
* Tất cả API đều phải được xác thực/phân quyền bằng **JWT** *(0.5 điểm)*
* Mỗi API có 1 **scope** riêng.

#### Công nghệ sử dụng:
* Sử dụng **Elasticsearch** để tính toán thời gian Uptime của 1 server *(1.0 điểm)*
* Database sử dụng **Postgres**.
* Có sử dụng **Redis Cache** để Optimize performance cho các nghiệp vụ mà bạn thấy cần thiết *(0.5 điểm)*
* Các công nghệ khác tùy bạn thêm vào nếu cần thiết và không giới hạn *(0.5 điểm)*

---

## 2.3. Các tài liệu cần gửi
* Tài liệu mô tả và thiết kế hệ thống.
* Hướng dẫn sử dụng (bao gồm ảnh chụp màn hình các tính năng đã yêu cầu ở trên).
* Link repo trên GitHub (yêu cầu push code lên GitHub và cung cấp link).