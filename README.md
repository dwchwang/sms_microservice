# 🖥️ VCS Server Management System (VCS-SMS)

Hệ thống quản lý và giám sát tập trung **10.000 server**: CRUD, import/export Excel,
health check TCP mỗi 60 giây, báo cáo uptime và gửi email hằng ngày.

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://go.dev/)
[![Traefik](https://img.shields.io/badge/Traefik-v3.0-24A1C1?logo=traefikproxy)](https://traefik.io/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-17-336791?logo=postgresql)](https://www.postgresql.org/)
[![Redis](https://img.shields.io/badge/Redis-8-DC382D?logo=redis)](https://redis.io/)
[![Elasticsearch](https://img.shields.io/badge/Elasticsearch-8.12-005571?logo=elasticsearch)](https://www.elastic.co/)
[![Next.js](https://img.shields.io/badge/Next.js-16-000000?logo=nextdotjs)](https://nextjs.org/)
[![Tests](https://img.shields.io/badge/tests-455%20passing-success)](#-kiểm-thử)

---

## Mục lục

- [Hệ thống làm gì](#-hệ-thống-làm-gì)
- [Kiến trúc](#️-kiến-trúc)
- [Bốn ý tưởng cốt lõi](#-bốn-ý-tưởng-cốt-lõi)
- [Quick start](#-quick-start)
- [Cấu hình](#️-cấu-hình)
- [API](#-api)
- [Cấu trúc thư mục](#-cấu-trúc-thư-mục)
- [Kiểm thử](#-kiểm-thử)
- [Vận hành](#-vận-hành)
- [Xử lý sự cố](#-xử-lý-sự-cố)
- [Tài liệu](#-tài-liệu)

---

## 🎯 Hệ thống làm gì

| Nhóm | Chức năng |
|---|---|
| **Giám sát** | TCP health check 10.000 server **mỗi 60 giây**, ghi health fact vào Elasticsearch |
| **Quản lý server** | CRUD với filter / sort / pagination, soft delete, cache-aside |
| **Excel** | Import đồng bộ (báo cáo 3 nhóm kết quả), export theo đúng bộ lọc đang áp dụng |
| **Báo cáo** | Uptime theo kỳ, ON/OFF cuối kỳ, top 10 tệ nhất, **coverage** — gửi email HTML + đính kèm `.xlsx` |
| **Dashboard** | Uptime *ngày hôm nay* theo thời gian thực, không cần chờ snapshot |
| **Bảo mật** | JWT HS256 + Argon2id, 3 role × **13 scope** ánh xạ 1-1 theo endpoint |
| **Observability** | 7 metric Prometheus, log JSON có rotate, request ID xuyên suốt |
| **HA** | Cả 4 service scale ngang được; leader election cho cron của Report Service |

---

## 🏗️ Kiến trúc

**4 microservice + Traefik ForwardAuth + Redis Stream + database-per-service.**

```
                    ┌──────────────────────┐
   Browser ────────▶│  web · Next.js :3000 │
                    └──────────┬───────────┘
                               ▼
                    ┌──────────────────────────────────────┐
                    │  traefik :8080                       │
                    │  cors → forward-auth → rate-limit    │
                    └───┬──────────┬──────────┬────────────┘
        ForwardAuth ····┘          │          │
        /internal/verify           │          │
              ┌────────────────────▼──┐  ┌────▼──────────────┐  ┌──────────────────┐
              │ auth-service   :8081  │  │ server-service    │  │ report-service   │
              │ identity_db           │  │ :8082  server_db  │  │ :8084  report_db │
              └───────────┬───────────┘  └────┬─────────┬────┘  └───┬──────────┬───┘
                          │                   │         │           │          │
                          │      GET /internal/servers ◀┼───────────┘          │
                          ▼                   ▼         ▼                      ▼
                    ┌──────────────────────────────────────────────────────────────┐
                    │  PostgreSQL 17  ·  Redis 8  ·  Elasticsearch 8.12            │
                    └──────────────────────────────────────────────────────────────┘
                                            ▲          ▲
                          target projection │          │ status + stream + facts
                                            │   ┌──────┴──────────────────┐
                                            └───│ monitor-service  :8083  │
                                                │ KHÔNG có PostgreSQL     │
                                                │ KHÔNG có endpoint public│
                                                └──────────┬──────────────┘
                                                           ▼  TCP connect
                                                ┌──────────────────────────┐
                                                │ tcp-simulator 9001-19000 │
                                                └──────────────────────────┘
```

### Bốn service và trách nhiệm

| Service | Sở hữu dữ liệu | Trách nhiệm | Replica |
|---|---|---|:---:|
| **auth-service** | `identity_db` | Login, JWT, RBAC scope, ForwardAuth cho Traefik | 2 |
| **server-service** | `server_db` | CRUD, import/export Excel, target projection, consume status event | 2 |
| **monitor-service** | *không có DB* | Ping TCP theo round, ghi status + bộ đếm uptime vào Redis, ghi fact vào ES | 3 |
| **report-service** | `report_db` | Snapshot ngày hôm qua, báo cáo uptime, gửi email (có leader election) | 3 |

> **monitor-service không có PostgreSQL và không có endpoint public.** Input là Redis target
> projection, output là Redis status + Elasticsearch. Toàn bộ trao đổi với server-service đi
> **qua Redis**, không service nào gọi HTTP tới service kia trên đường monitoring.

📐 [Bộ sơ đồ đầy đủ](.claude/diagrams/README.md) — kiến trúc, thành phần, ERD, tuần tự,
trạng thái, triển khai, use case.

---

## 💡 Bốn ý tưởng cốt lõi

**1. Chỉ phát event khi status *thực sự* đổi.** Monitoring ping 10.000 server mỗi phút,
nhưng Lua script so status cũ/mới ngay trong Redis và chỉ `XADD` khi khác — chuyện xảy ra
vài chục lần/ngày, không phải 10.000 lần/phút. Nhờ vậy `server:list:version` gần như đứng
yên và **cache có tỉ lệ hit rất cao**. Đây là luận điểm trung tâm của thiết kế; ở bản cũ
(event mỗi lần check) cache bị vô hiệu liên tục nên vô dụng.

**2. Round-based monitoring với đồng hồ chung.** `round_id = redis_time_unix / 60`, lấy từ
**Redis TIME** chứ không phải `time.Now()` của từng máy. Một instance thắng
`SETNX monitor:round:lock:{round}` thì nạp queue; **mọi** instance đều `BRPOP` từ queue đó
nên thêm instance là thêm năng lực ping, không nhân đôi công việc.

**3. Ba tầng dữ liệu uptime, ba mục đích.**

| Tầng | Phạm vi | Ai đọc |
|---|---|---|
| **Redis** (`monitor:uptime:index`) | ngày hôm nay (giờ VN), tự reset lúc nửa đêm | dashboard realtime |
| **Elasticsearch** (`server-status-logs-*`) | mỗi lượt ping, ILM giữ 7 ngày | **chỉ** snapshot job, 1 lần/ngày |
| **PostgreSQL** (`daily_snapshots`) | mỗi ngày đã kết thúc | mọi báo cáo và email |

**4. Báo cáo phơi bày phần nó không đo được.** `coverage_pct = Σactual / Σexpected` trả lời
"**hệ thống giám sát** có khoẻ không", tách biệt với `uptime_pct` trả lời "**server** có
khoẻ không". Server không ai ping được có `uptime_pct = NULL` (không phải 0) và vẫn góp vào
mẫu số `expected_checks`. Thiếu hẳn snapshot một ngày thì báo cáo **bị từ chối kèm tên
ngày**, không lấy trung bình vắt qua lỗ hổng.

---

## 🚀 Quick start

### Yêu cầu

- **Docker** 24+ với **Docker Compose** v2+
- **RAM** ≥ 4GB (khuyến nghị 8GB — Elasticsearch chiếm ~1GB)
- **Disk** ~5GB cho image + volume
- **Go** 1.25+ và **Make** — chỉ cần khi dev local hoặc chạy test

### 1. Cấu hình

```bash
cd server-management-system
cp .env.example .env
```

Sửa `.env` — bốn biến bắt buộc:

```env
JWT_SECRET=chuoi-ngau-nhien-it-nhat-32-ky-tu   # < 32 ký tự → service TỪ CHỐI khởi động
SMTP_USERNAME=your-email@gmail.com
SMTP_PASSWORD=your-16-char-app-password        # Gmail App Password, KHÔNG phải mật khẩu thường
REPORT_DAILY_RECIPIENT=admin@company.com       # rỗng = không đăng ký job gửi mail
```

### 2. Khởi động

```bash
docker compose up -d --build   # 10 container
docker compose ps              # postgres/redis/es/tcp-simulator/report phải healthy
```

### 3. Seed 10.000 server — **hai lệnh, không phải một**

```bash
make seed             # nạp 10.000 server vào server_db
make rebuild-cache    # BẮT BUỘC — dựng Redis target projection
```

> ⚠️ **Thiếu `rebuild-cache` thì không server nào được ping.** Monitoring không đọc
> PostgreSQL: nó đọc một *projection* trong Redis, và projection chỉ được dựng bởi lệnh
> này. Dấu hiệu: log monitor liên tục báo `target projection not ready; skipping round`,
> và mọi server đứng ở `UNKNOWN`.

### 4. Truy cập

| Thành phần | Địa chỉ |
|---|---|
| **Web UI** | http://localhost:3000 |
| **API** (qua Traefik) | http://localhost:8080/api/v1 |
| Elasticsearch | http://localhost:9200 |
| PostgreSQL | `localhost:5432` |
| Redis | `localhost:6379` |

🔑 Tài khoản admin seed sẵn: **`admin@vcs.com` / `Admin@123456`**

### 5. Xác nhận hệ thống hoạt động

```bash
# Đăng nhập (LƯU Ý: email, không phải username)
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@vcs.com","password":"Admin@123456"}' \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["data"]["access_token"])')

# Sau ~60 giây kể từ rebuild-cache, unknown phải về 0
curl -s http://localhost:8080/api/v1/servers/stats \
  -H "Authorization: Bearer $TOKEN"
# → {"data":{"total":10000,"on":8910,"off":1090,"unknown":0}}

# Uptime của hôm nay
curl -s http://localhost:8080/api/v1/servers/uptime \
  -H "Authorization: Bearer $TOKEN"
```

> Traefik chỉ định tuyến `/api/v1/auth`, `/api/v1/servers`, `/api/v1/reports`. Endpoint
> `/health` của từng service **không** publish ra ngoài, nên `curl :8080/health` sẽ trả 404 —
> dùng `docker compose ps` để xem sức khoẻ container.

### Dev local (chỉ hạ tầng trong Docker)

```bash
make dev-up                       # chỉ postgres, redis, elasticsearch, tcp-simulator
cd auth-service && go run ./cmd   # rồi tương tự cho từng service
```

---

## ⚙️ Cấu hình

Toàn bộ biến nằm trong `server-management-system/.env` (mẫu: `.env.example`).

### Những biến đáng chú ý

| Biến | Mặc định | Ý nghĩa |
|---|---|---|
| `JWT_SECRET` | — | **≥ 32 ký tự**, không thì service thoát ngay khi khởi động |
| `JWT_ACCESS_EXPIRY_MINUTES` | `15` | TTL access token |
| `JWT_REFRESH_EXPIRY_DAYS` | `7` | TTL refresh token (có rotation, dùng một lần) |
| `SERVER_CIDR_ALLOWLIST` | `10.0.0.0/8,172.16.0.0/12,192.168.0.0/16` | Dải IP được phép làm target. **Rỗng = chặn tất cả** (fail closed) |
| `MONITOR_WORKER_COUNT` | `200` | Goroutine ping mỗi instance. Sizing: `targets × timeout / 60s` + headroom |
| `MONITOR_TCP_TIMEOUT` | `3000` | ms |
| `MONITOR_FACT_CAPACITY` | `50000` | Fact được buffer trước khi **drop** khi ES sập |
| `MONITOR_TCP_DIAL_HOST` | `tcp-simulator` | Ghi đè host đích để simulator đứng thay 10.000 địa chỉ. **Rỗng ở production** |
| `REPORT_SNAPSHOT_CRON` | `30 0 * * *` | Cô đọng dữ liệu hôm qua (giờ `Asia/Ho_Chi_Minh`) |
| `REPORT_DAILY_CRON` | `0 10 * * *` | Gửi báo cáo hôm qua |
| `REPORT_DAILY_RECIPIENT` | — | **Rỗng = không đăng ký job gửi mail** (snapshot vẫn chạy) |
| `REPORT_MAX_RANGE_DAYS` | `31` | Khoảng báo cáo tối đa |
| `REPORT_COVERAGE_THRESHOLD` | `95.0` | Dưới ngưỡng → email gắn cờ `degraded` |
| `SMTP_RECIPIENT_DOMAINS` | *(rỗng)* | Domain được nhận mail. **Rỗng = gửi tới bất kỳ ai** — đừng để vậy ở production |
| `REDIS_DB` | `0` auth · `1` các service khác | Auth dùng keyspace riêng |

> **Chu kỳ round 60 giây không cấu hình được.** `round_id = floor(unix_time / 60)` phải
> khớp trên mọi instance, nên nó là hằng số trong code chứ không phải biến môi trường.

### Docker secret (Swarm)

`docker-stack.yml` dùng cặp `X` / `X_FILE`: nếu `REDIS_PASSWORD_FILE` được set thì giá trị
lấy từ nội dung file. Cầu nối là `shared/pkg/confighelper.GetStringSecret` — không dòng
code config nào phải biết mình đang chạy Compose hay Swarm.

### Trước khi lên production

| Việc | Vì sao |
|---|---|
| Đổi `JWT_SECRET` | Giá trị trong `.env.example` là công khai |
| Đặt `SMTP_RECIPIENT_DOMAINS` | Để rỗng thì hệ thống có thể bị lợi dụng làm mail relay |
| Đổi mật khẩu Postgres / Redis | Mặc định là công khai |
| Tắt `POST /api/v1/auth/register` | Nếu không muốn ai cũng tự tạo tài khoản |
| Siết `SERVER_CIDR_ALLOWLIST` | Càng rộng, hệ thống càng giống công cụ quét cổng nội mạng |
| Xem lại CORS trong `deployments/traefik/dynamic.yml` | Đang cho phép cả một dải LAN |

---

## 📡 API

**16 endpoint public** (qua Traefik) + 4 endpoint nội bộ.
📖 Đặc tả đầy đủ: [`docs/api-spec.yaml`](docs/api-spec.yaml) (OpenAPI 3.0.3)

### Auth — `/api/v1/auth`

| Method | Path | Scope |
|---|---|---|
| POST | `/register` | Public — luôn gán role `viewer` |
| POST | `/login` | Public — đăng nhập bằng **email** |
| POST | `/refresh` | Public — rotation, refresh token dùng một lần |
| POST | `/logout` | Authenticated |
| GET | `/profile` | Authenticated |
| GET | `/users` | `user:list` |
| PUT | `/users/{user_id}/role` | `user:manage_role` |

### Servers — `/api/v1/servers`

| Method | Path | Scope | Ghi chú |
|---|---|---|---|
| POST | `` | `server:create` | **Cần `Idempotency-Key`** |
| GET | `` | `server:list` | filter / sort / paginate, `page_size` cap 100 |
| GET | `/stats` | `server:stats` | ON/OFF/UNKNOWN **hiện tại**, cache 10s |
| GET | `/uptime` | `server:stats` | Uptime **hôm nay** (giờ VN), cache 10s |
| GET | `/{server_id}` | `server:view` | |
| PUT | `/{server_id}` | `server:update` | `server_id` không đổi được |
| DELETE | `/{server_id}` | `server:delete` | Soft delete |
| POST | `/import` | `server:import` | **Cần `Idempotency-Key`**, đồng bộ, ≤ 10.000 dòng |
| POST | `/export` | `server:export` | POST vì filter dài; là thao tác **đọc** |

### Reports — `/api/v1/reports`

| Method | Path | Scope | Ghi chú |
|---|---|---|---|
| GET | `/summary` | `report:view` | `end_date` phải là ngày **đã kết thúc** |
| POST | `` | `report:send` | `Idempotency-Key` **tuỳ chọn** |
| GET | `/{id}` | `report:view_detail` | |

### Nội bộ — không publish qua Traefik

| Method | Path | Dùng cho |
|---|---|---|
| GET | `/internal/verify` | Traefik ForwardAuth |
| GET | `/internal/servers` | Report Service đọc population |
| POST | `/internal/snapshots/{date}` | Chạy lại snapshot một ngày |
| GET | `/metrics`, `/health` | Prometheus, healthcheck |

### RBAC — 3 role × 13 scope

| Role | Scope |
|---|---|
| **admin** | tất cả 13 |
| **operator** | viewer + `server:create/update/delete/import/export` + `report:send` + `report:view_detail` |
| **viewer** | `server:list`, `server:view`, `server:stats`, `report:view` |

Quan hệ **bao hàm chặt**: `viewer ⊂ operator ⊂ admin`. Scope ánh xạ **1-1 theo endpoint**,
không gộp thành `read`/`write` — thêm endpoint mới là thêm scope mới.

**Hai tầng, hai câu hỏi khác nhau:** Traefik ForwardAuth trả lời *"token này có hợp lệ
không"*; `RequireScope` trong service trả lời *"user này được làm việc này không"*. Traefik
không biết endpoint nào cần scope nào, nên thiếu tầng hai thì một token `viewer` hợp lệ sẽ
xoá được server.

> Scope được nhúng thẳng vào JWT lúc login, nên `/internal/verify` không cần truy vấn DB.
> Cái giá: **đổi role chỉ có hiệu lực khi token cũ hết hạn hoặc user đăng nhập lại**.

---

## 📁 Cấu trúc thư mục

```
sms_microservice/
├── README.md                      ← file này
├── design.md                      đặc tả thiết kế đầy đủ
├── refactor.md                    đối chiếu chi tiết kiến trúc cũ ↔ mới
├── docs/                          tài liệu (xem mục Tài liệu)
├── .claude/
│   ├── diagrams/                  7 bộ sơ đồ Mermaid
│   ├── plan/ · process/ · test/   kế hoạch & nhật ký từng phase
│   └── conventions/
└── server-management-system/
    ├── docker-compose.yml         10 container, máy đơn
    ├── docker-compose.dev.yml     chỉ hạ tầng, cho dev local
    ├── docker-stack.yml           Docker Swarm, nhiều replica + secret
    ├── Makefile                   build · test · seed · rebuild-cache · logs
    │
    ├── shared/                    module dùng chung (6 Go module trong monorepo)
    │   ├── middleware/            RequestID · Logger · AuthFromForwardAuth · RequireScope
    │   ├── pkg/jwt/               Generate / Validate token
    │   ├── pkg/confighelper/      đọc *_FILE cho Docker secret
    │   ├── timezone/              Asia/Ho_Chi_Minh — dùng bởi cả monitor và report
    │   └── logger/ response/ errors/ validator/
    │
    ├── auth-service/              :8081 — identity_db
    ├── server-service/            :8082 — server_db
    │   └── internal/
    │       ├── handler/           CRUD · import · export · idempotency · internal
    │       ├── service/           business logic + CIDR validator
    │       └── infrastructure/
    │           ├── projection/    ✍️ ghi Redis target projection
    │           ├── consumer/      📖 consume stream:monitor.status
    │           ├── status/        📖 đọc monitor:status:* và uptime index
    │           └── excel/         parser (10 cột) + generator (14 cột)
    ├── monitor-service/           :8083 — KHÔNG có DB
    │   └── internal/
    │       ├── monitor/           scheduler · worker pool · fact buffer · sampler
    │       ├── infrastructure/
    │       │   ├── redisstore/    keys · redis_ops · Lua statusScript
    │       │   ├── pinger/        TCP connect + phân loại lỗi
    │       │   └── metrics/       7 metric Prometheus
    │       └── repository/        bulk ghi health fact vào ES
    ├── report-service/            :8084 — report_db
    │   └── internal/
    │       ├── infrastructure/
    │       │   ├── scheduler/     cron.go + leader.go — claim & heartbeat
    │       │   ├── snapshot/      population ⟕ facts → daily_snapshots
    │       │   └── email/         net/smtp STARTTLS + template
    │       ├── client/            gọi GET /internal/servers
    │       └── repository/        snapshot · report_job · cron_run · ES aggregator
    ├── tcp-simulator/             giả lập 10.000 listener bằng 1 container
    ├── web/                       Next.js 16 + React 19 + Tailwind 4
    └── deployments/
        ├── docker/postgres/       init.sql · seed_10k_servers.sql · migrate_report_ha.sql
        └── traefik/               traefik.yml (static) · dynamic.yml (router + middleware)
```

**Monorepo, 6 Go module:** `shared` + 4 service + `tcp-simulator`. Đổi contract chung chỉ
cần một commit.

---

## 🧪 Kiểm thử

```bash
make test        # toàn bộ 6 module
make coverage    # sinh báo cáo HTML từng module
make mocks       # sinh lại mock bằng mockery
```

**455 test, 6/6 module xanh** (`go build` + `go vet` + `go test`):

| Module | Test |
|---|---:|
| shared | 22 |
| auth-service | 85 |
| server-service | 178 |
| monitor-service | 53 |
| report-service | 102 |
| tcp-simulator | 15 |

### Integration test — tự skip khi không có hạ tầng

`sqlmock` chỉ **so khớp chuỗi SQL**, không chạy SQL, nên nó không phát hiện được truy vấn
đúng cú pháp mà **sai ngữ nghĩa**. Vì vậy các truy vấn aggregate và phần ILM/mapping có
integration test chạy trên PostgreSQL và Elasticsearch thật; chúng tự `skip` khi biến môi
trường không được set, nên `go test ./...` vẫn xanh khi không có hạ tầng:

```bash
REPORT_TEST_DATABASE_URL="postgres://…"     go test ./internal/repository/ -run Integration
MONITOR_TEST_ES_URL="http://localhost:9200" go test ./internal/infrastructure/database/ -run Integration
```

---

## 🔧 Vận hành

> ⚠️ **Bốn image service Go là distroless** (`gcr.io/distroless/static-debian12:nonroot`):
> không shell, không `wget`, không `curl`, không package manager. Mọi lệnh chẩn đoán phải
> gọi **binary trực tiếp**, hoặc đi từ container `traefik` (Alpine, có `wget`).

```bash
# Dựng lại target projection — sau seed, hoặc sau khi Redis mất dữ liệu
docker compose exec server-service /app/server-service rebuild-monitor-cache

# Đọc 7 metric của Monitoring
docker exec vcs-sms-traefik wget -qO- http://monitor-service:8083/metrics | grep vcs_monitor

# Chạy lại snapshot của một ngày
docker exec vcs-sms-traefik wget -qO- --post-data='' \
  http://report-service:8084/internal/snapshots/2026-07-23

# Soi Redis — -n 1 cho dữ liệu giám sát, -n 0 cho auth
docker exec vcs-sms-redis redis-cli -a "$REDIS_PASSWORD" --no-auth-warning -n 1 \
  hgetall monitor:status:SRV-00001

# Tra lịch sử cron (leader election của report-service)
docker exec vcs-sms-postgres psql -U vcs_admin -d report_db \
  -c "SELECT job_name, run_date, state, owner, finished_at FROM cron_runs ORDER BY run_date DESC LIMIT 5;"

# Log
docker compose logs -f monitor-service
docker compose exec report-service … / docker compose logs -f report-service
```

### 7 metric Prometheus

| Metric | Ý nghĩa | Báo động khi |
|---|---|---|
| `vcs_monitor_round_duration_seconds` | Round bắt đầu → queue cạn | tiến sát 60s |
| `vcs_monitor_targets_expected` | Target đã nạp vào queue | lệch số server thật |
| `vcs_monitor_checks_completed_total` | Ping instance này hoàn thành | — |
| **`vcs_monitor_checks_missing`** | **Việc chưa kịp ping lúc round kết thúc** | **> 0 kéo dài → thiếu worker** |
| `vcs_monitor_queue_depth` | Độ sâu queue hiện tại | không về 0 |
| `vcs_monitor_tcp_latency_seconds` | Histogram TCP connect | đuôi phân phối tăng |
| `vcs_monitor_es_bulk_failure_total` | Batch bulk bị bỏ sau retry | > 0 |

`checks_missing` là **tín hiệu duy nhất** báo thiếu worker. Không có nó, hệ thống ping thiếu
server mà không ai biết — không lỗi, không exception, chỉ là vài server im lặng không được
đo. Cách xử lý: tăng `MONITOR_WORKER_COUNT` hoặc thêm instance.

### Số đo thật (10.000 server, 1 instance, 200 goroutine)

| Chỉ số | Giá trị |
|---|---|
| `round_duration` trung bình | **3,5s** (ngân sách 60s) |
| `checks_missing` | 0 |
| `tcp_latency` trung bình | 4,3ms |
| Rebuild target projection | 1,8s |
| `GET /servers/uptime` (10.000 server) | ~0,125s |
| Elasticsearch | ~14,4 triệu doc/ngày nếu chạy liên tục, ILM giữ 7 ngày |

Công thức sizing `10.000 × 3s / 60s = 500` goroutine (+20% → 600) là **cận trên xấu nhất**,
giả định mọi check tốn trọn 3s timeout. Khi server trả lời bình thường (~4ms), 200 goroutine
trên 1 instance đã thừa.

### Triển khai Swarm

```bash
docker stack deploy -c docker-stack.yml vcs-sms
docker service scale vcs-sms_monitor-service=5
docker service logs -f vcs-sms_report-service
```

Postgres / Redis / Elasticsearch / Traefik bị ghim vào manager node
(`placement.constraints: [node.role == manager]`) vì dùng named volume local và bind-mount
config từ repo. Bản stack này scale **tầng ứng dụng**, không scale tầng dữ liệu.

---

## 🩺 Xử lý sự cố

| Triệu chứng | Nguyên nhân | Cách khắc phục |
|---|---|---|
| **Mọi server đều `UNKNOWN` và không đổi** | Thiếu marker `ready` — chưa dựng target projection | `make rebuild-cache` |
| Log monitor: `target projection not ready; skipping round` | Redis bị xoá sạch, hoặc chưa từng seed | `make rebuild-cache` |
| `redis-cli` trả 0 key | Đang xem db0; dữ liệu giám sát ở **db1** | thêm `-n 1` |
| `exec: "wget": executable file not found` | Image service là distroless | gọi từ `vcs-sms-traefik`, hoặc gọi binary trực tiếp |
| `docker exec vcs-sms-report …` → *No such container* | `report-service` **không có** `container_name` (để `--scale` chạy được) | `docker compose exec report-service …` |
| `curl :8080/health` → 404 | Traefik chỉ route `/api/v1/{auth,servers,reports}` | dùng `docker compose ps` |
| Mọi server `OFF` khi dev | `MONITOR_TCP_DIAL_HOST` không trỏ tới `tcp-simulator` | biến này đã set trong compose; kiểm tcp-simulator có `healthy` |
| `400` — *"Idempotency-Key header is required"* | Thiếu header ở `POST /servers` hoặc `/servers/import` | thêm `Idempotency-Key: <uuid>` |
| `422 SERVER_IP_NOT_ALLOWED` | IPv4 ngoài `SERVER_CIDR_ALLOWLIST` | sửa IP hoặc mở rộng allowlist |
| `422 REPORT_DATA_UNAVAILABLE` | Có ngày chưa snapshot | `POST /internal/snapshots/{date}` cho ngày thiếu |
| `coverage_pct` rất thấp | Monitoring không chạy đủ 24 giờ của ngày đó | **đúng như thiết kế** — báo cáo đang phơi ra phần nó không đo được |
| Email không gửi được | Dùng mật khẩu Gmail thường thay vì App Password | tạo App Password 16 ký tự |
| Bảng `cron_runs` không tồn tại | DB volume cũ, `init.sql` chỉ chạy khi volume rỗng | chạy `deployments/docker/postgres/migrate_report_ha.sql` thủ công |
| Trình duyệt chặn request nhưng `curl` vẫn chạy | Lỗi CORS — `curl` không gửi preflight | kiểm origin trong `deployments/traefik/dynamic.yml` |
| Service không start | Port conflict, thiếu `.env`, hoặc `JWT_SECRET` < 32 ký tự | `docker compose logs <service>` |
| `make seed` báo *"input device is not a TTY"* | Makefile dùng `docker exec -it` | gọi `docker exec` không kèm `-it` |

---

## 📚 Tài liệu

### Báo cáo

| Tài liệu | Nội dung |
|---|---|
| [Mô tả & thiết kế hệ thống](docs/BaoCao-MoTa-ThietKe-HeThong.md) | Báo cáo thiết kế đầy đủ — 14 mục |
| [Hướng dẫn sử dụng](docs/BaoCao-HuongDanSuDung.md) | Từng tính năng, kèm `curl` đã chạy thật |
| [OpenAPI Spec](docs/api-spec.yaml) | OpenAPI 3.0.3 — 16 endpoint |

### Kiến trúc theo chủ đề

| # | Tài liệu | Nội dung |
|---|---|---|
| 01 | [Tổng quan kiến trúc](docs/01-architecture-overview.md) | 4 service, quyết định định hình hệ thống, tính chịu lỗi |
| 02 | [Chiến lược dữ liệu](docs/02-database-strategy.md) | Database-per-service, Redis keyspace, ES, cache-aside |
| 03 | [Event-driven Redis Stream](docs/03-event-driven-redis-stream.md) | Vì sao bỏ Kafka, Lua atomic, version guard |
| 04 | [Concurrency & worker pool](docs/04-high-concurrency-worker-pool.md) | Round, sizing, Redis pool, 7 metric |
| 05 | [Bảo mật](docs/05-security-jwt-rbac.md) | JWT, ForwardAuth, RBAC, CORS, Argon2id, idempotency |
| 06 | [Flow: CRUD server](docs/06-flow-server-crud.md) | Cache-aside, projection, stats & uptime |
| 07 | [Flow: health check](docs/07-flow-health-check.md) | Một round đầu-cuối, Lua, consumer |
| 08 | [Flow: import/export](docs/08-flow-import-export.md) | Ba nhóm kết quả, hồi sinh row, formula injection |
| 09 | [Flow: báo cáo & email](docs/09-flow-reporting-email.md) | Dashboard, snapshot, coverage, FSM gửi mail, cron HA |

### Sơ đồ — [`.claude/diagrams/`](.claude/diagrams/README.md)

Kiến trúc · Thành phần · ERD · Tuần tự (8 luồng) · Trạng thái (7 FSM) · Triển khai · Use case

### Tài liệu lịch sử

`docs/report.md`, `docs/user-guide.md` và `docs/brainstorm_vcs_sms.md` mô tả kiến trúc
**Checkpoint 1** (5 service + API Gateway tự viết + Kafka + shared schema) và **không còn
đúng**. Mỗi file có banner cảnh báo ở đầu; giữ lại để đối chiếu quá trình refactor.
`refactor.md` là bản đối chiếu chi tiết cũ ↔ mới.

---

## 🔑 License

MIT — VCS Server Management System © 2026 · Chương trình đào tạo VCS Passport
