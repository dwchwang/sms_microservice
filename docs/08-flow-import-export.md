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
  │     (header phải đúng thứ tự; tối đa MaxDataRows = 10.000 dòng dữ liệu)
  │
  ├─ Validate từng dòng:
  │     dòng không hợp lệ        → failed
  │     IP ngoài CIDR allowlist  → failed (SERVER_IP_NOT_ALLOWED)
  │     trùng ID/tên TRONG file  → skipped_duplicate  (dòng đầu tiên thắng)
  │
  ├─ Lọc tên đã tồn tại (FindExistingNames) → skipped_duplicate
  │
  ├─ INSERT theo batch 500:
  │     INSERT ... ON CONFLICT (server_id) DO UPDATE
  │       SET server_name = EXCLUDED.server_name, ipv4 = EXCLUDED.ipv4, ...,
  │           status_version = 0, status_changed_at = NULL, deleted_at = NULL
  │     WHERE servers.deleted_at IS NOT NULL       ← chỉ HỒI SINH row đã xoá mềm
  │     RETURNING server_id
  │     ├─ ID trả về    → succeeded + sync target projection
  │     ├─ ID vắng mặt  → skipped_duplicate (row CÒN SỐNG, WHERE không khớp)
  │     └─ đụng unique tên → retry TỪNG DÒNG (một dòng xấu không làm hỏng cả batch)
  │
  ├─ Có dòng nào succeeded? → INCR server:list:version
  └─ 200 + báo cáo ba nhóm
```

### `DO UPDATE ... WHERE deleted_at IS NOT NULL`, không phải `DO NOTHING`

`ux_servers_server_id` là `UNIQUE(server_id)` **không có mệnh đề `WHERE`**, nên một
`server_id` đã xoá mềm **vẫn chiếm chỗ**. Với `DO NOTHING`, import lại đúng file cũ sau
khi xoá 5 server sẽ báo trùng cả 10.000 và 5 server đó không bao giờ quay lại được.

`DO UPDATE ... WHERE servers.deleted_at IS NOT NULL` giải quyết cả hai chiều bằng một câu:

| `server_id` trong DB | `WHERE` khớp? | Trong `RETURNING`? | Báo cáo |
|---|:---:|:---:|---|
| chưa từng có | — (INSERT thường) | ✅ | **succeeded** |
| đã xoá mềm | ✅ | ✅ | **succeeded** (hồi sinh, `deleted_at = NULL`) |
| còn sống | ❌ | ❌ | **skipped_duplicate** |

Hồi sinh cũng đặt lại `status_version = 0` và `status_changed_at = NULL`: server mới chưa
được ping, nên nó phải bắt đầu lại từ `UNKNOWN` chứ không mang theo version của lần sống trước.

`ON CONFLICT` chỉ nhận **một** conflict target, nên clash theo `server_name` được lọc
trước bằng `FindExistingNames`, và phần lọt lưới (race) được bắt bằng
`IsUniqueViolation(err, "ux_servers_active_name")` rồi retry từng dòng.

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
  "skipped_duplicate": { "count": 4, "items": ["SRV-05", "SRV-06"] }
}
```

`items` của cả ba nhóm luôn là **array, không bao giờ `null`** — frontend map trực tiếp
không cần kiểm. `failed` là nhóm duy nhất mang `row` và `reason`, vì đó là nhóm duy nhất mà
người import cần sửa file.

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

### File import — 10 cột, đúng thứ tự này

```text
server_id · server_name · ipv4 · tcp_port · os
cpu_cores · ram_gb · disk_gb · location · description
```

| Cột | Bắt buộc | Ghi chú |
|---|---|---|
| `server_id` | ✅ | Unique toàn cục, kể cả với server đã xóa |
| `server_name` | ✅ | Unique trong các server đang sống |
| `ipv4` | ✅ | Phải nằm trong `SERVER_CIDR_ALLOWLIST` |
| `tcp_port` | ✅ | 1–65535 |
| `os` | ❌ | Rỗng → NULL |
| `cpu_cores`, `ram_gb`, `disk_gb` | ❌ | Rỗng → NULL; nếu có phải > 0 |
| `location`, `description` | ❌ | Rỗng → NULL |

Header sai thứ tự hoặc sai tên → **từ chối cả file** (`ErrImportFileRejected` → 400). Đây
là lỗi *file*, không phải lỗi dòng: đọc tiếp một file lệch cột sẽ ghi `ipv4` vào cột `os`.

`status` **không** phải cột import được. Nó chỉ đến từ monitoring.

### File export — 14 cột, khác file import

```text
server_id · server_name · status · last_status_check · ipv4 · tcp_port · os
cpu_cores · ram_gb · disk_gb · location · description · created_at · updated_at
```

Bốn cột thêm (`status`, `last_status_check`, `created_at`, `updated_at`) là những thứ **hệ
thống sinh ra**, không phải người dùng nhập. Vì vậy file export **không** dùng lại được làm
file import trực tiếp — đó là chủ đích: import mà nhận `status` thì client sẽ tự đặt được
trạng thái server.

Sheet đặt tên `Servers`; hàng header có style riêng (chữ trắng trên nền `4472C4`).
