# 01 — Tổng quan kiến trúc

> Hệ thống hiện tại: **4 service + Traefik + Redis Stream + database-per-service**.
> Đây là bản đọc nhanh; đặc tả đầy đủ nằm ở `design.md`.

---

## 1. Bức tranh tổng thể

```text
Client / Web (Next.js :3000)
        │
        ▼
   Traefik :8080  ──ForwardAuth──► auth-service /internal/verify
        │
        ├─ /api/v1/auth      → auth-service    (identity_db)
        ├─ /api/v1/servers   → server-service  (server_db)
        └─ /api/v1/reports   → report-service  (report_db)

monitor-service  — không có endpoint public
        đọc Redis target projection → ping TCP → Redis status
        → Lua XADD → stream:monitor.status → server-service consumer
        → Elasticsearch (health fact, ILM giữ 7 ngày)
```

10 container: `postgres`, `redis`, `elasticsearch`, `tcp-simulator`, `traefik`,
`auth-service`, `server-service`, `monitor-service`, `report-service`, `web`.

Chỉ **Traefik (8080)** và **web (3000)** publish ra host. Bốn service ứng dụng chỉ
sống trên network nội bộ — lý do ở [05-security-jwt-rbac.md](./05-security-jwt-rbac.md).

---

## 2. Bốn service và trách nhiệm

| Service | Sở hữu dữ liệu | Trách nhiệm |
|---|---|---|
| **auth-service** | `identity_db` | Đăng nhập, JWT, RBAC scope, ForwardAuth cho Traefik |
| **server-service** | `server_db` | CRUD server, import/export Excel, target projection, consume status event |
| **monitor-service** | *không có DB riêng* | Ping TCP theo round, ghi status vào Redis, ghi health fact vào ES |
| **report-service** | `report_db` | Snapshot 00:30 hằng ngày, báo cáo uptime, gửi email |

**monitor-service không có PostgreSQL.** Input là Redis target projection, output là
Redis status + Elasticsearch. Nhờ vậy nó scale ngang tự do — thêm instance không
đụng gì tới database.

---

## 3. Ba quyết định định hình kiến trúc

### 3.1 Database-per-service

Không còn schema dùng chung. Mỗi service một database, không service nào đọc bảng
của service khác. Cần dữ liệu của nhau thì gọi API nội bộ hoặc đọc projection.

→ [02-database-strategy.md](./02-database-strategy.md)

### 3.2 Redis Stream thay Kafka

Bài toán chỉ có **một** loại event (`status.changed`), một producer, một consumer
group. Kafka (broker + KRaft controller + ~1GB RAM) là chi phí vận hành cố định mà
không đổi lấy được gì. Redis thì đã có sẵn trong hệ thống cho cache và projection.

→ [03-event-driven-redis-stream.md](./03-event-driven-redis-stream.md)

### 3.3 Round-based monitoring

Mỗi phút là một **round** có ID xác định: `round_id = unix_time / 60`, lấy từ
**Redis TIME** chứ không phải đồng hồ máy. Scheduler nạp queue một lần cho mỗi
round; mọi instance cùng `BRPOP` từ queue đó.

→ [04-high-concurrency-worker-pool.md](./04-high-concurrency-worker-pool.md)

---

## 4. Vì sao status không làm hỏng cache

Đây là luận điểm trung tâm của thiết kế mới.

Monitoring ping 10.000 server **mỗi phút**, nhưng chỉ phát event khi status **thực sự
đổi** — chuyện xảy ra vài chục lần/ngày, không phải 10.000 lần/phút. Lua script so
sánh status cũ/mới ngay trong Redis và chỉ `XADD` khi khác.

Hệ quả: `server:list:version` gần như đứng yên → cache list/detail có tỉ lệ hit rất
cao. Ở thiết kế cũ (event mỗi lần check) cache bị vô hiệu liên tục nên vô dụng.

`last_checked_at` **không** nằm trong cache entry — nó được đọc tươi từ Redis
`monitor:status:{id}` lúc trả response. Nhờ vậy nó nhích mỗi phút mà không phải bump
version, và cache vẫn hit.

---

## 5. Đường đi của một status change

```text
1. monitor worker BRPOP server_id từ monitor:ping:queue:{round_id}
2. HGETALL server:monitor-target:{server_id} → ipv4, tcp_port, server_name
3. TCP connect → ON/OFF + latency
4. Lua script (atomic):
     - round_id <= round_id cũ?  → bỏ qua (chống out-of-order)
     - HSET monitor:status:{id}  status, last_checked_at, latency_ms, round_id
     - status đổi?               → XADD stream:monitor.status
5. server-service consumer (group "server-svc") đọc event:
     - UPDATE servers ... WHERE server_id=? AND status_version < ?
     - INCR server:list:version  (chỉ khi thực sự có row đổi)
6. Song song: health fact → bulk buffer → Elasticsearch (index theo ngày)
```

Bước 4 gộp HSET và XADD trong **một** Lua script: không có khoảnh khắc nào Redis
status và stream mâu thuẫn nhau.

---

## 6. Các tính chất chịu lỗi

| Tình huống | Hành vi |
|---|---|
| Elasticsearch chết | Ping vẫn chạy, status vẫn đúng; chỉ snapshot đêm nay thiếu data |
| Redis mất stream/group | Consumer tự phát hiện `NOGROUP`, tạo lại group tại `0` và replay (~4s) |
| Event trùng / cũ | Version guard `status_version <` làm mọi apply idempotent |
| Event hỏng | ACK rồi bỏ, không redeliver vô hạn |
| Monitor instance chết | Instance khác vẫn `BRPOP` từ cùng queue |
| Scheduler mất lock | Bình thường — chỉ một instance nạp queue, mọi instance đều ping |
| Bulk ES bị từ chối | `Write` trả lỗi → retry; doc `_id` tất định nên không nhân bản |

---

## 7. Tài liệu liên quan

| File | Nội dung |
|---|---|
| [02-database-strategy.md](./02-database-strategy.md) | Database-per-service, 3 DB |
| [03-event-driven-redis-stream.md](./03-event-driven-redis-stream.md) | Redis Stream, Lua, version guard |
| [04-high-concurrency-worker-pool.md](./04-high-concurrency-worker-pool.md) | Round, worker pool, sizing |
| [05-security-jwt-rbac.md](./05-security-jwt-rbac.md) | JWT, ForwardAuth, scope |
| [06-flow-server-crud.md](./06-flow-server-crud.md) | CRUD, cache-aside, projection |
| [07-flow-health-check.md](./07-flow-health-check.md) | Health check đầu-cuối |
| [08-flow-import-export.md](./08-flow-import-export.md) | Import/export Excel |
| [09-flow-reporting-email.md](./09-flow-reporting-email.md) | Snapshot, report, email |
