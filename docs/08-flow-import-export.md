# 08 — Flow: import / export Excel

> FileIO Service **không còn tồn tại**. Import/export là adapter của server-service,
> vì bản chất import là bulk mutation lên aggregate Server. Tách thành service riêng
> chỉ làm tăng độ phức tạp mà không đem lại gì.

---

## 1. Import

```text
POST /api/v1/servers/import        Idempotency-Key: <bắt buộc>
Content-Type: multipart/form-data  (file .xlsx)
  │
  ├─ RequireScope("server:import")
  ├─ Idempotency: cùng key+body → replay, không import lần hai
  │
  ├─ Parse Excel → []ParsedRow          ← lỗi file → 400, TOÀN BỘ bị từ chối
  │
  ├─ Validate từng dòng:
  │     dòng không hợp lệ        → failed
  │     IP ngoài CIDR allowlist  → failed (SERVER_IP_NOT_ALLOWED)
  │     trùng ID/tên TRONG file  → skipped_duplicate  (dòng đầu tiên thắng)
  │
  ├─ Lọc tên đã tồn tại (FindExistingNames) → skipped_duplicate
  │
  ├─ INSERT theo batch 500:
  │     INSERT ... ON CONFLICT (server_id) DO NOTHING RETURNING server_id
  │     ├─ ID trả về    → succeeded + sync target projection
  │     ├─ ID vắng mặt  → skipped_duplicate (đã tồn tại)
  │     └─ đụng unique tên → retry TỪNG DÒNG (một dòng xấu không làm hỏng cả batch)
  │
  ├─ Có dòng nào succeeded? → INCR server:list:version
  └─ 200 + báo cáo ba nhóm
```

### Nguyên tắc: lỗi dòng không làm hỏng request

Chỉ **lỗi file** mới từ chối cả request. Một dòng sai chỉ khiến dòng đó `failed`, các
dòng còn lại vẫn vào. Import 10.000 dòng mà hỏng vì dòng thứ 3.000 là hành vi tệ.

### Response

```json
{
  "total_rows": 9,
  "succeeded":         { "count": 2, "items": ["SRV-01", "SRV-02"] },
  "failed":            { "count": 3, "items": [
      { "row": 4, "server_id": "SRV-04", "reason": "SERVER_IP_NOT_ALLOWED" }
  ]},
  "skipped_duplicate": { "count": 4, "items": ["SRV-05", "SRV-06"] },
  "duration_ms": 812
}
```

Ba nhóm **tách bạch**, không gộp: "trùng nên bỏ qua" khác hẳn "sai nên từ chối", và
người import cần biết mình đang gặp cái nào.

### Optional lưu NULL, không phải 0

`cpu_cores`/`ram_gb`/`disk_gb` bỏ trống trong file → ghi **NULL**. Ghi `0` vừa vi phạm
CHECK constraint, vừa nói dối rằng máy có 0 core.

---

## 2. Export

```text
POST /api/v1/servers/export      ← POST vì filter có thể dài, nhưng đây là thao tác ĐỌC
  │
  ├─ RequireScope("server:export")
  ├─ KHÔNG có idempotency (đọc thì cần gì)
  │
  ├─ Lặp FindAll theo page (page_size 100) cho tới hết
  ├─ Sinh .xlsx
  │     mỗi ô: bắt đầu bằng = + - @ tab CR  → prefix dấu '
  └─ Trả file
```

### Formula injection

Excel coi ô bắt đầu bằng `=`, `+`, `-`, `@` là **công thức**. Một server tên
`=cmd|'/c calc'!A1` sẽ chạy lệnh khi ai đó mở file export. Prefix `'` biến nó lại
thành text.

Chốt này áp cho **mọi** ô, kể cả các cột trông vô hại như `location`.

### Vì sao 100 query cho 10.000 server

`FindAll` cap `page_size` ở 100 để bảo vệ list API công khai, và export dùng chung nó
(không viết SQL riêng cho export — hai đường đọc cùng một dữ liệu sẽ lệch nhau theo
thời gian). Đặt 1.000 sẽ bị clamp âm thầm xuống 100 — hằng số nói dối còn tệ hơn.

Cái giá: export 10.000 server tốn 100 query, mỗi query lặp lại một `COUNT(*)`. Nếu
đo thấy chậm thật, hướng đúng là chuyển cap xuống tầng validate DTO — nhưng đó là
đổi hành vi của list API, phải cân nhắc riêng.

---

## 3. Cột file Excel

| Cột | Bắt buộc | Ghi chú |
|---|---|---|
| `server_id` | ✅ | Unique toàn cục, kể cả với server đã xóa |
| `server_name` | ✅ | Unique trong các server đang sống |
| `ipv4` | ✅ | Phải nằm trong `SERVER_ALLOWED_CIDRS` |
| `tcp_port` | ✅ | 1–65535 |
| `os` | ❌ | Rỗng → NULL |
| `cpu_cores`, `ram_gb`, `disk_gb` | ❌ | Rỗng → NULL; nếu có phải > 0 |
| `location`, `description` | ❌ | Rỗng → NULL |

`status` **không** phải cột import được. Nó chỉ đến từ monitoring.
