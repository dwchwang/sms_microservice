# 📐 Bộ sơ đồ hệ thống VCS-SMS

> Cập nhật: 24/07/2026 — đối chiếu lại với mã nguồn và với hệ thống đang chạy
> (`docker compose up`, 10.000 server, 455 unit test xanh).

## Danh mục

| # | File | Nội dung | Dùng khi nào |
|---|------|----------|--------------|
| 1 | [architecture-diagram.md](architecture-diagram.md) | Context → Container → Luồng dữ liệu tổng thể | Giới thiệu hệ thống, trả lời "cái gì nói chuyện với cái gì" |
| 2 | [component-diagram.md](component-diagram.md) | Bên trong từng service (handler → service → repo → hạ tầng) | Onboarding code, tìm chỗ sửa |
| 3 | [erd-diagram.md](erd-diagram.md) | 3 PostgreSQL DB + Redis keyspace + Elasticsearch index | Thiết kế/di trú dữ liệu |
| 4 | [sequence-diagrams.md](sequence-diagrams.md) | 8 luồng nghiệp vụ quan trọng theo thời gian | Debug một luồng cụ thể |
| 5 | [state-diagram.md](state-diagram.md) | FSM: trạng thái server, report job, import row, vòng ping | Hiểu điều kiện chuyển trạng thái |
| 6 | [deployment-diagram.md](deployment-diagram.md) | 10 container Docker Compose, port, volume, phụ thuộc | Vận hành, triển khai |
| 7 | [use-case-diagram.md](use-case-diagram.md) | 3 role × 13 scope × endpoint | Phân quyền, kiểm thử RBAC |

## Năm ý tưởng kiến trúc phải nắm trước khi đọc sơ đồ

1. **Database-per-service** — `identity_db`, `server_db`, `report_db` tách rời. Không service nào đọc DB của service khác; muốn dữ liệu thì gọi HTTP nội bộ (Report → `GET /internal/servers`).

2. **Redis là ranh giới giữa Server Service và Monitoring** — Server Service *ghi* projection `server:monitor-target:*`; Monitoring *đọc* projection đó, *ghi* `monitor:status:*` và stream `stream:monitor.status`; Server Service *tiêu thụ* stream để cập nhật `servers.status` trong PostgreSQL. Hai chiều, không service nào gọi HTTP tới service kia.

3. **Ba tầng dữ liệu uptime, ba mục đích khác nhau**
   - **Redis** (`monitor:status:*`, `monitor:uptime:index`) — số đếm **theo ngày hiện tại (giờ VN)**, tự reset khi sang ngày mới; phục vụ dashboard *thời gian thực*.
   - **Elasticsearch** (`server-status-logs-YYYY.MM.DD`) — **fact thô** mỗi lượt ping, chỉ snapshot job đọc, 1 lần/ngày.
   - **PostgreSQL** (`daily_snapshots`) — **kết tinh theo ngày**, là nguồn duy nhất của mọi báo cáo và email.

4. **Múi giờ** — Monitoring và Elasticsearch làm việc bằng **UTC**; Report Service quy đổi sang **`Asia/Ho_Chi_Minh` (UTC+7)** cho mọi ranh giới ngày và mọi lịch cron. Bộ đếm uptime trong Redis cũng cắt ngày theo giờ VN (field `day` trong `monitor:status:{id}`).

5. **Mọi replica đều chạy, một replica mới làm** — cùng một khuôn mẫu xuất hiện ở hai chỗ. Monitoring giành `SETNX monitor:round:lock:{round}` để nạp queue (kẻ thua **vẫn** ping). Report Service giành một dòng trong `cron_runs` để chạy cron (kẻ thua không làm gì). Nhờ vậy thêm replica không nhân đôi công việc và không cần thành phần điều phối nào.

## Quy ước ký hiệu trong các sơ đồ

| Ký hiệu | Ý nghĩa |
|---------|---------|
| Mũi tên nét liền | Gọi đồng bộ (HTTP / lệnh Redis / SQL) |
| Mũi tên nét đứt | Bất đồng bộ (Redis Stream, buffer, cron) |
| `[R]` / `[W]` | Chỉ đọc / có ghi |
| 🔒 | Yêu cầu JWT qua Traefik ForwardAuth |
| ⏰ | Do cron kích hoạt, không do người dùng |
