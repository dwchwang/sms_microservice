# Luồng vận hành: Báo cáo Thống kê & Gửi Email

Yêu cầu cực kỳ quan trọng của dự án là phải có báo cáo định kỳ. Người quản trị cần biết được tổng quan "sức khỏe" của toàn bộ trung tâm dữ liệu mà không cần phải nhìn chằm chằm vào màn hình. VCS-SMS sử dụng sự kết hợp giữa Elasticsearch và Gmail SMTP để thực hiện việc này.

## 1. Bài toán Aggregation (Thống kê gộp)

Để tính được tỷ lệ Uptime của 1 server, công thức là:
`Uptime = (Số lần check trả về ON / Tổng số lần health-check đã thực hiện) * 100%`.

**Cơ chế Demo:** Vì hệ thống demo chỉ chạy khi được bật lên (không chạy 24/24), công thức không chia cố định cho 1440 phút. Thay vào đó, uptime được tính dựa trên số lần check thực tế đã diễn ra. Ví dụ: hệ thống chạy được 8 tiếng (480 lần check), server có 456 lần ON → Uptime = 456/480 = 95.0%. Cách tính này linh hoạt, luôn cho ra tỷ lệ chính xác dù hệ thống chạy 1 tiếng hay 10 tiếng.

Khi hệ thống chạy đủ lâu, tổng số bản ghi log có thể lên tới hàng triệu dòng. Để làm điều này cho 10.000 server, nếu dùng SQL truyền thống đếm (COUNT) số dòng này, máy chủ sẽ bị treo.

Đây là lúc sức mạnh của **Elasticsearch** (Search Engine) tỏa sáng.
- `report-service` gửi một câu truy vấn Aggregation cực kỳ tối ưu sang Elasticsearch: "Hãy gom nhóm theo `server_id`, đếm tổng số bản ghi và đếm số bản ghi có status = 'on'".
- Elasticsearch (vốn sử dụng cấu trúc Inverted Index và phân tán) sẽ đếm và tính toán hàng triệu dòng này chỉ trong vài chục mili-giây.

## 2. Luồng Cron Báo Cáo Hàng Ngày (Daily Report Job)

1. **Chuông reo**: 8:00 sáng mỗi ngày, Cronjob trong `report-service` tự động kích hoạt.
2. **Tính toán**: Nó gửi truy vấn tới Elasticsearch để tính Uptime ngày hôm qua cho toàn bộ 10.000 server. 
3. **Lưu Snapshot (Chụp nhanh)**: Để tránh ngày mai admin lại bấm xem báo cáo của ngày hôm qua khiến hệ thống phải nhờ Elasticsearch tính lại hàng triệu dòng, `report-service` khôn ngoan lưu ngay kết quả vừa tính xong (Tổng server, Tỷ lệ Uptime trung bình toàn hệ thống, Top 10 server tệ nhất) vào bảng `daily_snapshots` trong PostgreSQL. Dữ liệu này trở thành dạng tóm tắt tĩnh (đã tính xong). Các lần xem sau chỉ mất 1 mili-giây móc từ DB ra.
4. **Tạo giao diện Email**: Hệ thống có sẵn một khuôn mẫu (HTML Template) đẹp mắt. Nó nhúng các con số vừa tính được vào các thẻ HTML, tô đỏ các server bị offline nhiều.
5. **Gửi Email qua SMTP**: Nó sử dụng cấu hình Gmail SMTP (với tính năng App Password bảo mật của Google) để kết nối tới cổng 587 của Google. Nó gửi gói HTML vừa tạo cùng tiêu đề "Báo cáo sức khỏe Server ngày X" tới danh sách email quản trị viên.
6. Tiếng "Ting", quản trị viên nhận được email trên điện thoại.

## 3. Luồng Báo Cáo Chủ Động (On-Demand)

Đôi khi sếp muốn xem báo cáo ngay lập tức từ ngày 1 đến ngày 15 thay vì chờ email định kỳ.
1. Sếp vào giao diện, chọn khoảng ngày, nhập email và bấm "Gửi báo cáo".
2. Hệ thống gọi API `POST /api/v1/reports`.
3. `report-service` tạo một bản ghi `report_jobs` trạng thái `pending`, sau đó chuyển sang `processing`.
4. Service tính summary ngay trong request bằng Elasticsearch aggregation. Với khoảng thời gian hợp lệ tối đa 90 ngày, kết quả có thể được cache trong Redis để các lần gọi sau nhanh hơn.
5. Tổng hợp xong, service render HTML template và gửi email qua Gmail SMTP tới địa chỉ người dùng nhập.
6. Nếu gửi thành công, `report_jobs` được cập nhật `completed`, lưu `sent_at`, tổng server, số on/off và uptime trung bình; API trả về `report_id`, `status`, `message` và `summary`. Nếu lỗi ở bước tính toán, render template hoặc SMTP, job được đánh dấu `failed` kèm `error_message`.
