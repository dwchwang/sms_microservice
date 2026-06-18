# Bảo mật: Flow xác thực JWT & Phân quyền RBAC

Để đáp ứng tiêu chí bảo mật (Error handling, JWT Auth, Scope), hệ thống VCS-SMS tách rời hoàn toàn việc cấp phát thẻ (Auth) và việc kiểm tra vé (Gateway).

## 1. Stateless vs Stateful

Hệ thống cũ thường dùng Session lưu trong Database (Stateful). Mỗi lần user có request, hệ thống phải query DB xem Session này của ai.
Với Microservice, ta dùng **JWT (JSON Web Token) - Stateless**.
- JWT là một chuỗi mã hóa bao gồm 3 phần (Header, Payload, Signature).
- Payload chứa thông tin public: `user_id`, `role`, và các `scopes` (quyền hạn).
- Khi người dùng gửi request lên kèm JWT, API Gateway chỉ cần dùng "Khóa bí mật" (Secret Key) để tính toán toán học. Nếu hợp lệ, nó tin tưởng 100% nội dung Payload mà KHÔNG CẦN CHỌC VÀO DATABASE.
- Nhờ vậy, API Gateway chạy cực kỳ mượt mà, không trở thành nút thắt cổ chai (bottleneck) về DB.

## 2. Refresh Token Flow

JWT có nhược điểm: Nếu nó bị lộ, kẻ gian có thể dùng vĩnh viễn. Để an toàn, Access Token (JWT) được cấp thời hạn sống (TTL) rất ngắn, thường là 15-30 phút.

Vậy người dùng phải đăng nhập lại mỗi 30 phút? KHÔNG.
Giải pháp là **Refresh Token Flow**:
1. Đăng nhập thành công -> Auth Service trả về 2 token: `AccessToken` (TTL: 15m) và `RefreshToken` (TTL: 7 ngày).
2. Khi `AccessToken` hết hạn, App/Web dưới client tự động âm thầm gửi `RefreshToken` lên API `/auth/refresh`.
3. Auth Service kiểm tra `RefreshToken` trong DB/Redis. Nếu đúng, cấp lại cặp Token mới.
4. Điều này giúp bảo mật cao nhưng không hy sinh trải nghiệm người dùng.

## 3. Vấn đề Đăng xuất (Logout) & JWT Blacklist

Vì JWT là Stateless, nó không được lưu trong DB. Vậy khi user bấm Đăng xuất, làm sao để vô hiệu hóa token đó ngay lập tức (dù nó còn hạn 10 phút nữa)?

**Giải pháp: Redis Blacklist**
- Trong Payload của JWT, ta chèn thêm một trường `jti` (JWT ID - một chuỗi UUID duy nhất cho từng token).
- Khi user gọi API `/auth/logout`, hệ thống lấy `jti` này và lưu vào **Redis** với giá trị `auth:blacklist:<jti>` = 1. Cài đặt TTL cho key này trong Redis bằng chính thời gian sống còn lại của JWT.
- Tại API Gateway, trước khi chấp nhận JWT, nó sẽ ngó qua Redis xem cái `jti` này có nằm trong sổ đen (Blacklist) không. Có thì từ chối (401 Unauthorized). Redis check key cực nhanh (dưới 1ms) nên không ảnh hưởng hiệu năng.

## 4. RBAC (Role-Based Access Control) & Scope Validation

Trong bảng `users` ta có cột `role_id` (admin, operator, viewer).
Nhưng trong Code, ta **KHÔNG NÊN** hardcode kiểm tra `if user.Role == "admin"`. Phân quyền cứng như vậy rất khó mở rộng sau này.

**Giải pháp: Role -> Scopes mapping**
Trong DB có bảng `role_permissions`.
- Role `admin` -> có các scope: `server:create`, `server:read`, `server:update`, `server:delete`, `server:import`, `server:export`, `monitor:view`, `report:view`, `report:send`, `user:manage`.
- Role `operator` -> có các scope vận hành: `server:create`, `server:read`, `server:update`, `server:import`, `server:export`, `monitor:view`, `report:view`, `report:send`.
- Role `viewer` -> có các scope: `server:read`, `server:export`, `report:view`.

Khi `auth-service` cấp JWT, nó query DB lấy toàn bộ list scopes của role đó, nén vào trong JWT (ví dụ mảng `["server:read", "report:view"]`).

**Tại API Gateway:**
Mỗi route khai báo cần scope gì. 
Ví dụ `POST /api/v1/servers` yêu cầu scope `server:create`.
API Gateway đọc JWT, thấy token này chỉ có mảng `["server:read"]`. Thiếu `server:create`. Lập tức Gateway chặn lại và trả về HTTP 403 Forbidden. Backend không bao giờ phải thấy request trái phép này.

## 5. Security Context Injection

Sau khi Gateway xác minh JWT hợp lệ, nó sẽ chuyển tiếp (proxy) request tới service bên trong (ví dụ `server-service`).
Nhưng `server-service` cần biết request này do ai gửi để lưu log `created_by`. Làm sao biết nếu nó không đọc JWT?
- API Gateway sẽ đính kèm thông tin vào **HTTP Headers** nội bộ: `X-User-ID: <uuid>`, `X-User-Scopes: ...`
- Backend service chỉ việc đọc Header tĩnh này để biết user hiện tại là ai. Đây là pattern chuẩn trong Microservice.
