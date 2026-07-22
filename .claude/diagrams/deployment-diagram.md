# 🚀 Sơ đồ triển khai — Docker Compose

> Cập nhật: 21/07/2026 · Nguồn: `server-management-system/docker-compose.yml`

---

## 1. Toàn cảnh 10 container trên mạng `vcs-network`

```mermaid
graph TB
    subgraph HOST["Máy host — cổng mở ra ngoài"]
        P1["localhost:3000 → web"]
        P2["localhost:8080 → traefik"]
        P3["localhost:5432 → postgres"]
        P4["localhost:6379 → redis"]
        P5["localhost:9200 → elasticsearch"]
    end

    subgraph NET["Docker bridge network: vcs-network"]
        subgraph TIER1["Tầng biên"]
            WEB["vcs-sms-web<br/>Next.js :3000"]
            TRF["vcs-sms-traefik<br/>:8080"]
        end

        subgraph TIER2["Tầng ứng dụng — chỉ expose, KHÔNG publish"]
            AU["vcs-sms-auth :8081"]
            SV["vcs-sms-server :8082"]
            MO["vcs-sms-monitor :8083"]
            RP["vcs-sms-report :8084"]
        end

        subgraph TIER3["Tầng dữ liệu"]
            PG["vcs-sms-postgres<br/>postgres:17-alpine"]
            RD["vcs-sms-redis<br/>redis:8-alpine"]
            ES["vcs-sms-elasticsearch<br/>8.12.0"]
        end

        SIM["vcs-sms-tcp-simulator<br/>10.000 listener<br/>giới hạn 256MB / 1 CPU"]
    end

    P1 --> WEB
    P2 --> TRF
    P3 --> PG
    P4 --> RD
    P5 --> ES

    WEB --> TRF
    TRF --> AU
    TRF --> SV
    TRF --> RP

    AU --> PG
    AU --> RD
    SV --> PG
    SV --> RD
    MO --> RD
    MO --> ES
    MO --> SIM
    RP --> PG
    RP --> ES
    RP --> SV
```

---

## 2. Thứ tự khởi động — `depends_on` + healthcheck

```mermaid
graph LR
    subgraph W["Chờ healthy"]
        PG["postgres<br/>pg_isready"]
        RD["redis<br/>redis-cli ping"]
        ES["elasticsearch<br/>_cluster/health"]
        SIM["tcp-simulator<br/>nc -z :9001"]
    end

    PG --> AU["auth-service"]
    RD --> AU
    PG --> SV["server-service"]
    RD --> SV
    RD --> MO["monitor-service"]
    ES --> MO
    SIM --> MO
    PG --> RP["report-service"]
    ES --> RP
    SV -->|"service_started"| RP

    AU --> TRF["traefik"]
    SV --> TRF
    RP --> TRF
    TRF --> WEB["web"]
```

| Phụ thuộc | Kiểu | Vì sao |
|-----------|------|--------|
| monitor → tcp-simulator | `service_healthy` | ping vào cổng chưa mở thì mọi server đều báo OFF sai |
| report → server-service | `service_started` (không phải healthy) | chỉ snapshot job cần, mà nó chạy lúc 00:30 |
| monitor → **không** có postgres | — | Monitoring hoàn toàn không đụng PostgreSQL |

---

## 3. Volume và tính bền dữ liệu

```mermaid
graph TB
    subgraph NAMED["Named volume — sống qua docker compose down"]
        V1["postgres_data<br/>→ /var/lib/postgresql/data"]
        V2["redis_data<br/>→ /data (AOF)"]
        V3["es_data<br/>→ elasticsearch/data"]
    end

    subgraph BIND["Bind mount — chỉ đọc"]
        B1["./deployments/docker/postgres/init.sql<br/>→ docker-entrypoint-initdb.d/<br/>CHỈ chạy khi volume RỖNG"]
        B2["./deployments/docker/postgres/seed_10k_servers.sql<br/>→ /seed/ · KHÔNG tự chạy"]
        B3["./deployments/traefik/*.yml"]
    end

    subgraph LOGS["Bind mount — ghi log"]
        L1["./logs/auth · server · monitor · report"]
        L2["./logs/traefik"]
    end
```

> ⚠️ `init.sql` chỉ chạy **lần đầu**, khi `postgres_data` còn rỗng. Sửa file này rồi restart sẽ **không** có tác dụng — phải `docker compose down -v` (mất toàn bộ dữ liệu) hoặc chạy tay bằng `psql`.

---

## 4. Cấu hình Redis — vì sao đúng như thế

```
redis-server --requirepass ...
  --maxmemory 512mb
  --maxmemory-policy volatile-lru      ← KHÔNG phải allkeys-lru
  --appendonly yes --appendfsync everysec
```

```mermaid
graph TB
    A["volatile-lru:<br/>CHỈ evict key CÓ TTL"]

    A --> B["✅ Được phép evict:<br/>cache danh sách (có TTL)<br/>round lock, ping queue (TTL 120s)"]
    A --> C["🚫 KHÔNG BAO GIỜ bị evict:<br/>monitor:status:* (counter trọn đời)<br/>monitor:uptime:index<br/>server:monitor-target:*<br/>stream:monitor.status"]

    C --> D["Vì sao: mất counter là mất VĨNH VIỄN.<br/>Mất projection thì Monitoring<br/>dừng hẳn cho tới khi rebuild."]

    E["appendonly yes:<br/>counter sống qua restart"]
```

Nếu dùng `allkeys-lru`, Redis có thể xoá `monitor:status:*` khi thiếu bộ nhớ — và số đếm uptime trọn đời không có cách nào tính lại.

---

## 5. Phân tách database — một Postgres, ba DB, ba user

```mermaid
graph TB
    subgraph PG["container postgres"]
        subgraph DB1["identity_db"]
            U1["identity_user"]
        end
        subgraph DB2["server_db"]
            U2["server_user_v2"]
        end
        subgraph DB3["report_db"]
            U3["report_user_v2"]
        end
    end

    AU["auth-service"] -->|"chỉ user này"| U1
    SV["server-service"] --> U2
    RP["report-service"] --> U3

    RP -.->|"❌ KHÔNG có quyền<br/>phải gọi HTTP"| U2
```

Ranh giới được cưỡng chế bằng **quyền của DB user**, không phải bằng quy ước. Report Service *không thể* `SELECT` bảng `servers` kể cả khi có người viết code như vậy — nó buộc phải gọi `GET /internal/servers`.

---

## 6. Vận hành thường ngày

```bash
# Khởi động toàn bộ
docker compose up -d

# Nạp 10.000 server test (không tự chạy)
docker exec vcs-sms-postgres psql -U vcs_admin -d server_db -f /seed/seed_10k_servers.sql

# Dựng lại projection để Monitoring nhìn thấy target
docker exec vcs-sms-server /app/server-service rebuild-monitor-cache

# Xem chỉ số giám sát
docker exec vcs-sms-monitor wget -qO- localhost:8083/metrics | grep vcs_monitor

# Chạy lại snapshot của một ngày cụ thể
docker exec vcs-sms-report wget -qO- --post-data='' \
  http://localhost:8084/internal/snapshots/2026-07-17

# Soi Redis — NHỚ -n 1
docker exec vcs-sms-redis redis-cli -a "$REDIS_PASSWORD" -n 1 dbsize
```

**Ba lỗi vận hành hay gặp:**

| Triệu chứng | Nguyên nhân | Cách xử lý |
|------------|-------------|------------|
| `redis-cli` cho ra 0 key | đang xem db0, dữ liệu ở db1 | thêm `-n 1` |
| Log Monitoring: *"target projection not ready"* | Redis bị xoá sạch, projection mất | chạy `rebuild-monitor-cache` |
| `make seed` báo *"input device is not a TTY"* | Makefile dùng `docker exec -it` | gọi `docker exec` không kèm `-it` |

---

## 7. Ước lượng tài nguyên (10.000 server)

| Container | RAM | Ghi chú |
|-----------|-----|---------|
| elasticsearch | ~1 GB | heap cố định 512MB (`ES_JAVA_OPTS`) |
| postgres | ~256 MB | 10k dòng servers + ~10k snapshot/ngày |
| redis | ≤ 512 MB | `maxmemory` chặn cứng |
| tcp-simulator | ≤ 256 MB | giới hạn qua `deploy.resources` |
| 4 service Go | ~100 MB/cái | monitor cao nhất — 200 goroutine + buffer 50k fact |
| web (Next.js) | ~150 MB | |

**Tải mỗi ngày:** 10.000 server × 1.440 vòng = **14,4 triệu** lượt ping và **14,4 triệu** document ES. Đây chính là lý do Report Service đọc `daily_snapshots` (10.000 dòng) chứ không truy vấn thẳng Elasticsearch mỗi lần cần báo cáo.
