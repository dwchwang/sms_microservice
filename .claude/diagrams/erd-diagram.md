# 🗄️ Mô hình dữ liệu — PostgreSQL · Redis · Elasticsearch

> Cập nhật: 21/07/2026 · Nguồn: `deployments/docker/postgres/init.sql` + các file `internal/model`, `keys.go`.

---

## 1. Toàn cảnh — dữ liệu nằm ở đâu

```mermaid
graph TB
    subgraph PG["🐘 PostgreSQL 17 — 3 database tách rời"]
        subgraph D1["identity_db · identity_user"]
            T1["roles"]
            T2["permissions"]
            T3["role_permissions"]
            T4["users"]
        end
        subgraph D2["server_db · server_user_v2"]
            T5["servers"]
            T6["api_idempotency"]
        end
        subgraph D3["report_db · report_user_v2"]
            T7["report_jobs"]
            T8["daily_snapshots"]
        end
    end

    subgraph RD["⚡ Redis 8"]
        K1["db0 — auth<br/>blacklist · login attempts"]
        K2["db1 — projection · status<br/>queue · stream · cache"]
    end

    subgraph ES["🔍 Elasticsearch 8.12"]
        E1["server-status-logs-YYYY.MM.DD<br/>1 doc = 1 lượt ping"]
    end
```

Không có khoá ngoại nào bắc qua ranh giới database. `daily_snapshots.server_id` và `servers.server_id` trùng giá trị nhưng **không** có FK — đó là chủ đích của database-per-service.

---

## 2. identity_db — RBAC

```mermaid
erDiagram
    ROLES ||--o{ USERS : "gán cho"
    ROLES ||--o{ ROLE_PERMISSIONS : "sở hữu"
    PERMISSIONS ||--o{ ROLE_PERMISSIONS : "được tham chiếu qua scope"

    ROLES {
        uuid id PK
        varchar name UK "admin · operator · viewer"
        text description
        timestamptz created_at
        timestamptz updated_at
    }

    PERMISSIONS {
        uuid id PK
        varchar scope UK "server:create, report:send, ..."
        text description
        timestamptz created_at
    }

    ROLE_PERMISSIONS {
        uuid id PK
        uuid role_id FK "ON DELETE CASCADE"
        varchar scope "UNIQUE(role_id, scope)"
        timestamptz created_at
    }

    USERS {
        uuid id PK
        varchar email UK
        varchar password_hash "500 ký tự — chừa chỗ Argon2id"
        varchar full_name
        uuid role_id FK
        boolean is_active
        timestamptz last_login_at
        timestamptz created_at
        timestamptz updated_at
        timestamptz deleted_at "soft delete"
    }
```

`role_permissions.scope` là **chuỗi**, không phải FK trỏ `permissions.id`. Cách này cho phép thêm scope mới trong code trước khi kịp seed vào bảng `permissions`.

---

## 3. server_db — nguồn sự thật về server

```mermaid
erDiagram
    SERVERS {
        uuid id PK
        varchar server_id "UNIQUE TOÀN CỤC, kể cả bản ghi đã xoá"
        varchar server_name "UNIQUE chỉ trên bản ghi còn sống"
        varchar status "ON · OFF · UNKNOWN"
        timestamptz status_changed_at
        bigint status_version "= round_id · chống ghi đè ngược"
        varchar last_status_event_id
        inet ipv4 "kiểu inet — LIKE phải qua host()"
        int tcp_port "1..65535"
        varchar os
        int cpu_cores
        int ram_gb
        int disk_gb
        varchar location
        text description
        timestamptz created_at
        timestamptz updated_at
        timestamptz deleted_at "soft delete"
    }

    API_IDEMPOTENCY {
        varchar actor_id PK
        varchar endpoint PK
        varchar idempotency_key PK
        varchar request_hash "SHA-256 thân request"
        varchar state "processing · completed · failed"
        int status_code
        jsonb response_body
        timestamptz expires_at
        timestamptz created_at
    }
```

### Hai index UNIQUE khác nhau — đây là gốc rễ của hành vi import

```mermaid
graph TB
    A["ux_servers_server_id<br/>UNIQUE(server_id)<br/><b>KHÔNG</b> có WHERE"]
    B["ux_servers_active_name<br/>UNIQUE(server_name)<br/>WHERE deleted_at IS NULL"]

    A --> A1["server_id đã xoá mềm<br/>VẪN chiếm chỗ"]
    A1 --> A2["Import không thể chèn dòng thứ hai<br/>⇒ phải dùng ON CONFLICT DO UPDATE<br/>WHERE servers.deleted_at IS NOT NULL<br/>để HỒI SINH bản ghi"]

    B --> B1["Tên của server đã xoá được<br/>giải phóng, dùng lại thoải mái"]
```

| Tình huống import | `server_id` đã tồn tại | Kết quả |
|-------------------|------------------------|---------|
| bản ghi **còn sống** | có | `WHERE` không khớp → không trả về từ `RETURNING` → báo **trùng** |
| bản ghi **đã xoá mềm** | có | `WHERE` khớp → ghi đè + `deleted_at = NULL` → báo **thành công** (hồi sinh) |
| chưa từng có | không | INSERT thường → **thành công** |

Cột `ipv4` kiểu `inet`, nên lọc theo tiền tố phải viết `host(ipv4) LIKE ?` — `ipv4 LIKE ?` sẽ lỗi `operator does not exist: inet ~~ unknown`.

---

## 4. report_db — kết tinh và vết gửi mail

```mermaid
erDiagram
    DAILY_SNAPSHOTS {
        varchar server_id PK
        date date PK "ranh giới ngày theo giờ VN"
        varchar server_name "tên TẠI NGÀY ĐÓ, không phải hiện tại"
        int on_checks
        int actual_checks "số lượt thực đo"
        int expected_checks "số lượt đáng lẽ phải đo"
        numeric uptime_pct "NULL khi actual_checks = 0"
        varchar last_status "ON/OFF cuối ngày · NULL khi no-data"
        timestamptz created_at
    }

    REPORT_JOBS {
        uuid id PK
        varchar report_type "daily · on-demand"
        varchar requester_id
        varchar idempotency_key
        date start_at
        date end_at
        varchar recipient_email
        varchar state "6 trạng thái"
        jsonb response_json "summary đã sinh"
        varchar smtp_message_id "RFC 5322 Message-ID"
        text error_message
        timestamptz created_at
        timestamptz updated_at
        timestamptz sent_at
    }
```

### Ba con số đo lường, ba câu hỏi khác nhau

```mermaid
graph LR
    A["expected_checks<br/>server sống bao nhiêu phút<br/>trong ngày ÷ 60"]
    B["actual_checks<br/>thực tế ES ghi nhận<br/>bao nhiêu lượt"]
    C["on_checks<br/>trong đó bao nhiêu lượt<br/>kết quả là ON"]

    A --> D["coverage = Σactual / Σexpected<br/><b>Hệ thống giám sát có khoẻ không?</b>"]
    B --> D
    B --> E["uptime = on / actual<br/><b>Server có khoẻ không?</b>"]
    C --> E
```

`uptime_pct = NULL` (no-data) khác hoàn toàn `uptime_pct = 0`:

| | Ý nghĩa | Vào `AVG()`? | Đếm vào |
|---|---|---|---|
| `NULL` | **không ai đo được** | không | `servers_no_data` |
| `0` | **server chết cả ngày** | có | `servers_uptime0` |

Server mới tạo lúc 18:00 chỉ có `expected_checks = 360` chứ không phải 1440 — nếu không, việc tạo server sẽ trông như một sự cố giám sát.

---

## 5. Redis keyspace

```mermaid
graph TB
    subgraph DB0["db0 — auth-service"]
        A1["blacklist:jti:{jti} → 1<br/>TTL = thời gian còn lại của token"]
        A2["login:attempts:{email} → n<br/>TTL cửa sổ chống brute-force"]
    end

    subgraph DB1["db1 — monitor + cache"]
        subgraph OWN1["✍️ server-service GHI"]
            B1["server:monitor-target:{id} (HASH)<br/>server_name · ipv4 · tcp_port"]
            B2["server:monitor-target:ids (SET)<br/>toàn bộ server_id"]
            B3["server:monitor-target:ready<br/>cờ 'projection đã dựng xong'"]
        end
        subgraph OWN2["✍️ monitor-service GHI"]
            C1["monitor:round:lock:{round}<br/>SETNX · TTL 120s"]
            C2["monitor:round:current → round_id"]
            C3["monitor:ping:queue:{round} (LIST)<br/>TTL 120s"]
            C4["monitor:status:{id} (HASH)<br/>status · last_checked_at · latency_ms<br/>round_id · total_checks · on_checks"]
            C5["monitor:uptime:index (ZSET)<br/>score = % uptime trọn đời"]
            C6["stream:monitor.status (STREAM)<br/>MAXLEN ~100000"]
        end
    end
```

### Ai ghi, ai đọc — và vì sao chiều ngược lại không tồn tại

```mermaid
graph LR
    SS["server-service"] -->|"GHI"| P["server:monitor-target:*"]
    P -->|"ĐỌC"| MS["monitor-service"]

    MS -->|"GHI"| S["monitor:status:*<br/>monitor:uptime:index"]
    MS -->|"GHI"| ST["stream:monitor.status"]

    S -->|"ĐỌC (dashboard)"| SS
    ST -.->|"XREADGROUP"| SS

    SS -->|"XOÁ khi server bị xoá"| S
```

Khi xoá server, server-service phải tự dọn `monitor:status:{id}` và gỡ khỏi `monitor:uptime:index` — nếu không, server đã chết vẫn tiếp tục được tính điểm trong bảng xếp hạng "10 server tệ nhất" mãi mãi.

**Chính sách eviction:** `volatile-lru` chứ không phải `allkeys-lru`. Chỉ cache mới có TTL; số đếm uptime, status, projection và stream **không** có TTL, và xoá bất kỳ thứ nào trong đó là mất dữ liệu không tái tạo được. AOF `everysec` giữ số đếm qua lần restart.

> ⚠️ Khi debug bằng `redis-cli`, nhớ `-n 1` — dữ liệu giám sát nằm ở db1, không phải db0.

---

## 6. Elasticsearch — fact thô

```mermaid
graph LR
    A["1 lượt ping"] --> B["1 document"]
    B --> C["server-status-logs-YYYY.MM.DD<br/>ngày đặt tên theo <b>UTC</b>"]

    D["Field:<br/>server_id · server_name · status<br/>checked_at · round_id<br/>latency_ms · error_code"]
    C --- D

    C -->|"composite agg + after_key<br/>terms agg KHÔNG phân trang nổi<br/>10.000 bucket"| E["snapshot job 00:30 VN"]
```

| Đại lượng | Giá trị |
|-----------|---------|
| Document/ngày | 10.000 server × 1440 vòng ≈ **14,4 triệu** |
| Ghi | bulk, 1000 doc hoặc 5 giây |
| Đọc | **1 lần/ngày** bởi snapshot job |
| Retry | 3 lần, backoff tăng dần, rồi **drop** |

Index đặt tên theo ngày **UTC** trong khi báo cáo cắt ngày theo **UTC+7**, nên một ngày VN trải trên **hai** index UTC. Truy vấn dùng wildcard `server-status-logs-*` với bộ lọc `checked_at` theo khoảng thời gian tuyệt đối, nên chuyện này không gây lệch.

---

## 7. Bảng tra nhanh: một sự kiện chạm vào những gì

| Hành động | PostgreSQL | Redis | Elasticsearch |
|-----------|-----------|-------|---------------|
| Tạo server | `INSERT servers` | `HSET` target + `SADD` ids | — |
| Xoá server | `deleted_at = now()` | `DEL` target, `SREM` ids, `DEL` status, `ZREM` uptime | — |
| Import 10k | `INSERT ... ON CONFLICT` | ghi hàng loạt target | — |
| Một lượt ping | — | Lua: status + counter + (stream nếu đổi) | +1 document |
| Đổi trạng thái | `UPDATE servers.status` (qua consumer) | `XADD` stream | +1 document |
| Snapshot 00:30 | `UPSERT daily_snapshots` | — | composite agg |
| Gửi báo cáo | `INSERT report_jobs` + đổi state | — | — |
