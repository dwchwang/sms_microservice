# Phase H: Report Service — HA & Scale-out

> **Mục tiêu:** Chạy report-service ở 2–3 instance (hoặc nhiều hơn) mà (a) không gửi email
> trùng, (b) không chạy snapshot trùng, (c) khi instance đang chạy job bị chết thì instance
> khác tiếp quản trong vài phút mà không cần người can thiệp.
>
> **Prerequisite:** R1–R7 hoàn tất. Stack đang chạy `report-service` ở `replicas: 1`.
>
> **Tài liệu tham chiếu:** `design.md` §9 (Reporting), `docs/09-flow-reporting-email.md`.

---

## Bối cảnh — vì sao cần phase này

report-service hiện có 3 điểm hỏng khi `replicas > 1`:

| # | Lỗi | Vị trí | Cần scale mới lộ? |
|---|---|---|:---:|
| H-1 | Cron chạy trên **mọi** replica → daily report gửi N email/ngày | `cmd/main.go:71-77`, `internal/infrastructure/scheduler/cron.go:76-91` | Có |
| H-2 | Snapshot job chạy N lần đồng thời → N× ES aggregation, N× upsert 10.000 row, lock contention | `internal/infrastructure/scheduler/cron.go:67-74` | Có |
| H-3 | `Idempotency-Key` được đọc và **lưu** nhưng không có ai tra cứu, không có UNIQUE constraint → retry = 2 email | `internal/handler/report_handler.go:70` → `internal/service/send_service.go:63` → `init.sql:233` | **Không** |

Ghi chú về H-2: dữ liệu **không sai** khi chạy trùng — `Upsert` dùng `ON CONFLICT (server_id, date)
DO UPDATE` và mọi instance tính cùng một window đã đóng nên ghi cùng giá trị. Vấn đề thuần là
lãng phí và lock contention. Comment ở `internal/repository/es_uptime_repository.go:25-26`
(`// Only the snapshot job calls it, once a day.`) chính là giả định đang bị phá.

Phần HTTP (`GET /summary`, `GET /:id`) đã stateless thật — không cache in-memory, không session.
Không cần sửa gì để scale phần này.

---

## Checklist tổng quan

- [x] **H.1** Bảng `cron_runs` + claim atomic (leader election qua PostgreSQL)
- [x] **H.2** Viết lại scheduler: reconcile loop + heartbeat thay cron bắn-thẳng
- [x] **H.3** Chống gửi trùng cho daily report (tái dùng state machine `report_jobs`)
- [x] **H.4** Idempotency thật cho `POST /api/v1/reports`
- [x] **H.5** Migration cho DB đang chạy + cập nhật `init.sql`
- [x] **H.6** `docker-compose.yml` + `docker-stack.yml`: replicas, healthcheck, rolling update
- [x] **H.7** Verify đa-instance trên Docker Compose (Swarm: chưa)

---

## Quyết định thiết kế & đánh đổi

Ghi lại để người đọc sau không tưởng là chọn bừa.

| # | Quyết định | Lý do |
|---|---|---|
| 1 | **Điều phối bằng PostgreSQL, không phải Redis** | report-service hiện **không hề kết nối Redis** (`go.mod` không có `go-redis`, `Config` không có `RedisConfig`). Thêm Redis chỉ để lock một job chạy 1 lần/ngày là thêm dependency cho một lần dùng. Postgres đã có sẵn, và claim-row **bền hơn** lock có TTL: không có cửa sổ nào lock hết hạn rồi job chạy lại. |
| 2 | **Claim theo `run_date` (ngày dữ liệu), không theo giờ bắn** | `snapshot` cho ngày 2026-07-23 chỉ được chạy đúng một lần trong đời. Nếu đổi `REPORT_SNAPSHOT_CRON`, key không đổi ⇒ không chạy lại nhầm. |
| 3 | **Reconcile loop mỗi phút thay `cron.Cron` bắn thẳng** | `robfig/cron` chỉ bắn đúng thời điểm. Nếu cả 3 instance đều down lúc 00:30, job **mất luôn**. Reconcile loop hỏi "job của hôm nay xong chưa?" nên instance nào lên trước sẽ chạy bù. Đây mới thật sự là HA. `robfig/cron` vẫn giữ lại — chỉ dùng để **parse** biểu thức, không dùng làm trigger. |
| 4 | **Heartbeat + steal-if-stale, không dùng timeout cố định** | Snapshot 10.000 server có thể chạy vài phút. Timeout cố định thì hoặc quá ngắn (cướp job đang khoẻ ⇒ chạy trùng) hoặc quá dài (HA phục hồi chậm). Heartbeat 30s + stale 3 phút tách rời hai chuyện đó. |
| 5 | **`tickInterval` / `heartbeatInterval` / `staleAfter` là hằng số, không phải config** | Cùng lý do với `RoundSeconds` của Monitoring (refactor-README §Quyết định lệch #3): hai instance cấu hình lệch nhau sẽ phá chính cơ chế mà nó dựng lên. |
| 6 | **Daily report retry dựa trên state của `report_jobs`, không phải cron_runs** | Instance chết **sau** khi gửi mail nhưng **trước** khi ghi `done` ⇒ instance khác cướp job ⇒ gửi email lần 2. `report_jobs` đã có sẵn state machine phân biệt "email đã lên dây chưa" (`sending` được set **trước** `sender.Send()` tại `send_service.go:90-104`). Tái dùng nó thay vì phát minh cơ chế mới. |
| 7 | **Biểu thức cron bị giới hạn ở "mỗi ngày một lần"** | Mô hình claim là 1 row / (job, ngày). Cron kiểu `*/5 * * * *` sẽ không có nghĩa. Hai job hiện tại đều là daily nên đây không phải giới hạn thật; ghi rõ ra để không ai đặt nhầm. |
| 8 | **Không tự retry `delivery_unknown`** | Giữ nguyên nguyên tắc đã có ở `model/report_job.go:16-18`. |

**Phương án đã cân nhắc và loại:** tách riêng service `report-scheduler` (`replicas: 1`) khỏi
`report-service` (`replicas: N`) — chỉ ~5 dòng code, nhưng scheduler thành single point of failure.
Đạt scale mà **không** đạt HA, nên không đáp ứng yêu cầu.

---

## H.1. Bảng `cron_runs` + claim atomic

### DDL

```sql
CREATE TABLE IF NOT EXISTS cron_runs (
    job_name      VARCHAR(50)  NOT NULL,
    run_date      DATE         NOT NULL,
    state         VARCHAR(20)  NOT NULL CHECK (state IN ('running','done','failed')),
    owner         VARCHAR(255) NOT NULL,
    started_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    heartbeat_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    finished_at   TIMESTAMPTZ,
    error_message TEXT,
    PRIMARY KEY (job_name, run_date)
);
```

### File mới: `internal/model/cron_run.go`

```go
const (
    CronRunning = "running"
    CronDone    = "done"
    CronFailed  = "failed"
)

const (
    JobSnapshot    = "snapshot"
    JobDailyReport = "daily_report"
)

type CronRun struct { ... }   // map 1-1 với DDL trên
func (CronRun) TableName() string { return "cron_runs" }
```

### File mới: `internal/repository/cron_run_repository.go`

```go
type CronRunRepository interface {
    TryClaim(ctx context.Context, job string, date time.Time, owner string, staleAfter time.Duration) (bool, error)
    Heartbeat(ctx context.Context, job string, date time.Time, owner string) (bool, error)
    MarkDone(ctx context.Context, job string, date time.Time, owner string) error
    MarkFailed(ctx context.Context, job string, date time.Time, owner, errMsg string) error
    IsDone(ctx context.Context, job string, date time.Time) (bool, error)
}
```

**`TryClaim` — toàn bộ cơ chế điều phối nằm ở một câu SQL:**

```sql
INSERT INTO cron_runs (job_name, run_date, state, owner, started_at, heartbeat_at)
VALUES (?, ?, 'running', ?, NOW(), NOW())
ON CONFLICT (job_name, run_date) DO UPDATE
   SET state         = 'running',
       owner         = EXCLUDED.owner,
       started_at    = NOW(),
       heartbeat_at  = NOW(),
       finished_at   = NULL,
       error_message = NULL
 WHERE cron_runs.state = 'failed'
    OR (cron_runs.state = 'running'
        AND cron_runs.heartbeat_at < NOW() - (? * INTERVAL '1 second'))
RETURNING job_name
```

- Không có row trả về ⇒ instance khác đang giữ, hoặc đã `done` ⇒ bỏ qua lượt này.
- `state='done'` không bao giờ khớp `WHERE` ⇒ không bao giờ chạy lại.
- Dùng `Raw(...).Scan(&out)` rồi kiểm `len(out) == 1`. **Không** dựa vào `RowsAffected` của
  `Exec` với `ON CONFLICT` — ngữ nghĩa của nó không rõ ràng giữa các driver.

**`Heartbeat`** — có guard `owner`, và trả về `false` khi mất quyền:

```sql
UPDATE cron_runs SET heartbeat_at = NOW()
 WHERE job_name = ? AND run_date = ? AND owner = ? AND state = 'running'
```

`RowsAffected == 0` nghĩa là job đã bị instance khác cướp (hoặc đã kết thúc) ⇒ runner phải
**huỷ context của job đang chạy** để hai instance không cùng ghi. Đây là điểm dễ bỏ sót nhất
của toàn phase.

`MarkDone` / `MarkFailed` cũng phải có `AND owner = ?` vì lý do tương tự.

---

## H.2. Viết lại scheduler

### File sửa: `internal/infrastructure/scheduler/cron.go`

Bỏ `cron.Cron` làm trigger. Giữ `robfig/cron` chỉ để `cron.ParseStandard(expr)` lấy ra
`cron.Schedule`, dùng để tính "hôm nay job này đến giờ chưa".

```go
const (
    tickInterval      = time.Minute
    heartbeatInterval = 30 * time.Second
    staleAfter        = 3 * time.Minute   // = 6 nhịp heartbeat
)

type job struct {
    name     string
    schedule cron.Schedule
    run      func(ctx context.Context, date time.Time) error
    // dependsOn để trống, hoặc tên job phải xong trước.
    dependsOn string
}
```

**Vòng lặp chính** (`Run(ctx)`), mỗi `tickInterval`:

```
với mỗi job:
    fireAt   := schedule.Next(startOfDay(now) - 1ns)       // lần bắn đầu tiên trong ngày
    nếu now < fireAt                        → bỏ qua (chưa tới giờ)
    runDate  := startOfDay(now).AddDate(0,0,-1)            // cả 2 job đều báo cáo "hôm qua"
    nếu job.dependsOn != "" và !IsDone(dependsOn, runDate) → bỏ qua (đợi lượt sau)
    nếu !TryClaim(job.name, runDate, hostname, staleAfter) → bỏ qua
    chạy runWithHeartbeat(job, runDate)
```

- Chỉ reconcile **lần bắn của ngày hôm nay**. Nếu toàn bộ cụm down quá 24h, ngày bị mất phải
  chạy tay qua `POST /internal/snapshots/:date` (endpoint này đã có sẵn ở `cmd/main.go:104-117`).
- `daily_report` khai báo `dependsOn: JobSnapshot` ⇒ bỏ hẳn việc phụ thuộc vào chênh lệch giờ
  cron (09:30 vs 10:00) để đảm bảo thứ tự.

### File mới: `internal/infrastructure/scheduler/leader.go`

`runWithHeartbeat(ctx, job, date)`:

1. Tạo `jobCtx, cancel := context.WithCancel(ctx)`.
2. Goroutine heartbeat mỗi `heartbeatInterval`: gọi `repo.Heartbeat(...)`; nếu trả `false` thì
   `cancel()` và log `Warn` — quyền sở hữu đã mất.
3. Chạy `job.run(jobCtx, date)`.
4. `err == nil` → `MarkDone`; ngược lại → `MarkFailed(err.Error())` để lượt reconcile sau
   thử lại (vì `TryClaim` khớp `state = 'failed'`).

### File sửa: `cmd/main.go`

- Thay `cron.Register(...)` + `cron.Start()` bằng `scheduler.Run(runCtx)` trong goroutine,
  theo đúng pattern `runCtx/stopRun/wg` mà `monitor-service/cmd/main.go:75-81` đang dùng.
- Lấy `hostname` làm `owner` (giống `server-service/cmd/main.go:98-102`).
- Shutdown: `stopRun()` rồi chờ `wg` với timeout — job đang chạy sẽ bị huỷ và để lại
  `state='running'` với heartbeat cũ; instance khác nhặt sau `staleAfter`. Đây là hành vi mong muốn.

---

## H.3. Chống gửi trùng cho daily report

Vấn đề còn lại sau H.2: instance A claim `daily_report`, gửi email xong, **chết trước khi
`MarkDone`**. Sau 3 phút instance B cướp job và gửi email lần hai.

Giải: trước khi gửi, tra `report_jobs` xem ngày đó đã có lần gửi nào chưa.

### File sửa: `internal/repository/report_job_repository.go`

Thêm:

```go
FindLatestDaily(ctx context.Context, date time.Time) (*model.ReportJob, error)
// WHERE report_type='daily' AND start_at=? AND end_at=? ORDER BY created_at DESC LIMIT 1
```

### Quy tắc quyết định

| State của `report_jobs` (daily, ngày đó) | Hành động | Vì sao |
|---|---|---|
| không có row | Gửi | Chưa từng thử |
| `processing`, `generated` | Gửi | `sending` chưa được set ⇒ email chắc chắn chưa lên dây |
| `failed` | Gửi | `recordSend` chỉ trả `failed` khi lỗi xảy ra **trước** `DATA` (`smtp_sender.go:144-160`) |
| `sending` | **Không gửi**, `MarkDone`, log `Warn` | Email có thể đã đi — `SetState(sending)` chạy trước `sender.Send()` |
| `sent` | **Không gửi**, `MarkDone` | Đã xong |
| `delivery_unknown` | **Không gửi**, `MarkDone`, log `Error` | Nguyên tắc có sẵn: không bao giờ retry mù |

Đặt logic này trong hàm `run` của job `daily_report` ở `scheduler/cron.go`, **không** đặt trong
`sendService.Send()` — `Send()` phục vụ cả on-demand và phải giữ nguyên ngữ nghĩa "gọi là gửi".

---

## H.4. Idempotency thật cho `POST /api/v1/reports`

Đây là lỗi tồn tại sẵn, độc lập với scale, nhưng scale làm nó không thể vá bằng cache in-memory.

### DDL

```sql
CREATE UNIQUE INDEX IF NOT EXISTS ux_report_jobs_idem
    ON report_jobs (requester_id, idempotency_key)
    WHERE idempotency_key <> '';
```

Partial index ⇒ request không gửi header vẫn tạo job bình thường (giữ nguyên hành vi hiện tại).

### File sửa: `internal/repository/report_job_repository.go`

```go
FindByIdempotency(ctx context.Context, requesterID, key string) (*model.ReportJob, error)
```

### File sửa: `internal/service/send_service.go`

Trong `Send()`, **trước** khi tạo job:

```
nếu key != "":
    existing := FindByIdempotency(requesterID, key)
    nếu tìm thấy:
        nếu (start, end, recipient) khác với existing → trả ErrIdempotencyConflict
        ngược lại                                     → replay: trả job cũ, KHÔNG gửi lại
```

Và **sau** `jobs.Create(job)`, bắt lỗi unique-violation (`23505`):

```
nếu lỗi là unique violation trên ux_report_jobs_idem:
    đọc lại bằng FindByIdempotency → replay
```

Bước thứ hai này mới là bước làm cho nó **đúng với N replica**: hai request cùng key rơi vào
hai instance cùng lúc thì check-then-act đơn thuần vẫn lọt. Ràng buộc UNIQUE ở DB là trọng tài
duy nhất, code chỉ dịch lỗi của nó thành response.

### File sửa: `internal/service/errors.go`, `internal/handler/report_handler.go`

- Thêm sentinel `ErrIdempotencyConflict`.
- Map trong `handleReportError`: `→ 409` với code `apperrors.CodeReportIdempotency`.

> Code `CodeReportIdempotency = "REPORT_IDEMPOTENCY_CONFLICT"` **đã có sẵn** ở
> `shared/errors/codes.go:29` và đã map sang HTTP 409 ở dòng 52 — chỉ là chưa ai dùng.
> Không cần thêm code mới.

---

## H.5. Migration cho DB đang chạy

Repo **không có thư mục `migrations/`** (đã xoá ở R7) và `init.sql` chỉ chạy khi volume
`postgres_data` còn trống. Cụm đang chạy sẽ **không** tự nhận DDL mới.

- [ ] Thêm DDL của H.1 và H.4 vào `deployments/docker/postgres/init.sql`, trong khối `\c report_db`,
      đặt sau `daily_snapshots` — cho lần cài mới.
- [ ] Tạo `deployments/docker/postgres/migrate_report_ha.sql` chứa đúng 2 lệnh
      (`CREATE TABLE IF NOT EXISTS cron_runs`, `CREATE UNIQUE INDEX IF NOT EXISTS ux_report_jobs_idem`)
      để chạy trên cụm đang sống:
      ```bash
      docker exec -i $(docker ps -qf name=vcs-sms_postgres) \
        psql -U report_user_v2 -d report_db < deployments/docker/postgres/migrate_report_ha.sql
      ```
- [ ] Cả hai file phải idempotent (`IF NOT EXISTS`) để chạy lại không hỏng.

**Cảnh báo:** `CREATE UNIQUE INDEX` sẽ **thất bại** nếu `report_jobs` đang có sẵn bản ghi trùng
`(requester_id, idempotency_key)`. Kiểm tra trước:

```sql
SELECT requester_id, idempotency_key, COUNT(*)
FROM report_jobs WHERE idempotency_key <> ''
GROUP BY 1, 2 HAVING COUNT(*) > 1;
```

---

## H.6. Cập nhật deployment

### `docker-stack.yml` — service `report-service`

```yaml
    healthcheck:
      test: ["CMD", "/app/bin/report-service", "healthcheck"]
      interval: 10s
      timeout: 3s
      retries: 3
      start_period: 20s
    deploy:
      replicas: 3
      update_config:
        order: start-first
        parallelism: 1
        delay: 10s
      restart_policy:
        condition: any
```

- Hiện **không service nào trong stack có `healthcheck`**. Không có nó, Swarm VIP sẽ route
  request vào replica chưa kết nối xong Postgres/ES ⇒ 502 lúc rolling update.
- `report-service/Dockerfile` dùng `gcr.io/distroless/static-debian12:nonroot` — **không có
  shell, không có `wget`/`curl`**. Nên healthcheck phải do chính binary thực hiện.

### File sửa: `cmd/main.go` — subcommand `healthcheck`

Dùng lại đúng pattern `os.Args[1]` mà `server-service/cmd/main.go:74-77` đang dùng cho
`rebuild-monitor-cache`. Đặt ở **đầu** `main()`, trước cả `LoadConfig()`:

```go
if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
    res, err := http.Get("http://127.0.0.1:" + port + "/health")
    if err != nil || res.StatusCode != http.StatusOK {
        os.Exit(1)
    }
    os.Exit(0)
}
```

`port` đọc thẳng từ env `APP_PORT` (mặc định `8084`) — không gọi `LoadConfig()` để healthcheck
không kết nối DB và không ghi log.

### Traefik — không cần đổi gì

`deployments/traefik/dynamic.yml` khai báo `http://report-service:8084` qua file provider.
Trong Swarm, tên đó phân giải ra **VIP của service**, và IPVS tự chia tải sang mọi replica.
Scale ra 3 instance là trong suốt với Traefik.

Lưu ý: **không** thêm `retry` middleware cho router `report-api`. Retry một `POST /reports`
đã đi tới SMTP sẽ gửi email lần hai; H.4 chặn được nếu client gửi `Idempotency-Key`, nhưng
đừng tạo thêm nguồn retry ở tầng hạ tầng.

### `.env.example`

Không thêm biến mới (xem quyết định #5). Cập nhật comment cho `REPORT_SNAPSHOT_CRON` /
`REPORT_DAILY_CRON` nói rõ: **chỉ chấp nhận biểu thức chạy một lần mỗi ngày.**

---

## H.7. Verify

### Unit test

| File | Ca cần phủ |
|---|---|
| `repository/cron_run_repository_test.go` | `TryClaim` lần đầu → true; lần hai cùng ngày → false; sau `MarkFailed` → true; `running` + heartbeat cũ hơn `staleAfter` → true; `running` + heartbeat tươi → false; `done` → false |
| `scheduler/cron_test.go` | Chưa tới giờ → không claim; `dependsOn` chưa `done` → không claim; `runDate` luôn là hôm qua theo `Asia/Ho_Chi_Minh` |
| `scheduler/leader_test.go` | `Heartbeat` trả false → job context bị cancel; `run` lỗi → `MarkFailed`; `run` ok → `MarkDone` |
| `service/send_service_test.go` | Cùng key + cùng payload → replay, `sender.Send` **không** được gọi; cùng key + payload khác → `ErrIdempotencyConflict`; `Create` trả 23505 → đọc lại và replay |
| `scheduler` (daily dedupe) | 6 state của `report_jobs` → gửi / không gửi theo đúng bảng ở H.3 |

Đặt clock injectable (`now func() time.Time`) như `reportService` đã làm — nếu không, test sẽ
pass hôm nay và hỏng ngày mai (bug #6 trong refactor-README).

### Verify thật trên cụm 3 node

- [ ] `docker service scale vcs-sms_report-service=3` → `docker service ps` thấy 3 task Running
- [ ] Đặt `REPORT_SNAPSHOT_CRON` về vài phút tới, chờ → **đúng 1 row** `cron_runs`
      (`job_name='snapshot'`), `state='done'`, và **chỉ 1 replica** có log "Daily snapshot written"
- [ ] Daily report → **đúng 1 email** trong hộp thư, **đúng 1 row** `report_jobs` `state='sent'`
- [ ] **HA:** trong lúc snapshot đang chạy, `docker kill` đúng container đang giữ claim.
      Trong ~3 phút, replica khác phải claim lại và chạy xong; `cron_runs.owner` đổi tên
- [ ] **Không gửi trùng:** kill replica ngay sau khi `report_jobs.state='sent'`.
      Replica khác cướp claim nhưng **không** gửi email thứ hai; `cron_runs` về `done`
- [ ] **Idempotency:** `POST /api/v1/reports` hai lần cùng `Idempotency-Key` (qua Traefik nên
      rơi vào hai replica khác nhau) → cùng một `job_id`, **1 email**
- [ ] Cùng key + `end_date` khác → **409** `REPORT_IDEMPOTENCY_CONFLICT`
- [ ] **Bù ngày bị mất:** dừng cả 3 replica trước giờ snapshot, qua giờ đó mới bật lại
      → job vẫn chạy trong vòng 1 phút kể từ khi replica đầu tiên lên
- [ ] Rolling update (`docker service update --force`) không sinh 502 ở `GET /reports/summary`
- [ ] `go build ./... && go vet ./... && go test ./...` xanh ở cả 6 module

---

## Kết quả verify thật (Docker Compose, 24/07/2026)

Chạy trên volume `postgres_data` **có sẵn** — tức là đúng đường nâng cấp thật, không phải
cài mới. `migrate_report_ha.sql` được áp lên DB đang sống.

| Hạng mục | Kết quả |
|---|---|
| Build/vet/test 6 module | ✅ 0 lỗi, 0 test fail |
| Healthcheck trên distroless | ✅ container `(healthy)`; `wget` không tồn tại nên binary tự probe |
| Trước migration | Service **không crash** — log `relation "cron_runs" does not exist` rồi tick tiếp |
| Sau migration | `snapshot` → 10.000 row `daily_snapshots`; `daily_report` → email gửi thật `state=sent` |
| Thứ tự `dependsOn` | `daily_report` bắt đầu **4ms** sau khi `snapshot` xong, cùng một tick — không phải đợi 1 phút |
| **Scale 3 replica** | Xoá `cron_runs` → **đúng 1 owner** claim cả 2 job; 2 replica còn lại không log gì |
| **Chống gửi trùng** | `daily_report` claim lại thấy `report_jobs.state='sent'` → `"already attempted; not sending again"`, `report_jobs` vẫn **đúng 1 row** |
| **HA — owner treo** | Cắm row `running` với heartbeat cũ 20 phút → replica sống cướp và chạy xong |
| **HA — owner chết thật** | `docker kill` replica đang giữ quyền → owner đổi sang `a8326c76734d`, cả 2 job xong |
| **Chạy bù** | Cron `30 9 * * *`, chạy lúc 11:19 giờ VN (trễ ~2h) vẫn thực thi — điều cron bắn-thẳng không làm được |
| Idempotency replay | Cùng key 2 lần → **cùng `job_id`**, `report_jobs` chỉ 1 row, 1 email |
| Idempotency conflict | Cùng key + đổi `recipient_email` → **409**; đổi `start_date`/`end_date` → **409** `REPORT_IDEMPOTENCY_CONFLICT` |
| Integration test `cron_runs` | 9/9 PASS trên PostgreSQL thật (DB scratch riêng, không đụng `report_db`) |
| Traefik + 3 replica | `GET /reports/summary` × 6 → 200 hết; file provider không cần sửa |

### Còn lại

- [ ] Deploy Swarm: build + push lại image (`docker-stack.yml:183` đang ghim `vcs-sms-report:1.0.0`)
- [ ] Chạy `migrate_report_ha.sql` trên Postgres của cụm Swarm
- [ ] Verify rolling update (`docker service update --force`) không sinh 502

---

## Thứ tự thực hiện đề xuất

1. **H.4 trước tiên** — độc lập với phần còn lại, sửa lỗi đang hỏng ngay ở production hiện tại,
   và có thể release riêng ở `replicas: 1`.
2. H.5 (migration) — cần trước khi code H.1 chạy được.
3. H.1 → H.2 → H.3 — làm liền mạch, giữa chừng hệ thống chưa chạy đúng đa-instance.
4. H.6 — chỉ scale lên 3 sau khi H.1–H.3 đã verify ở `replicas: 1`.

Trong suốt bước 1–3, giữ `replicas: 1`. Chỉ đổi sang 3 ở bước cuối.

---

## Ngoài phạm vi (ghi lại, không làm ở phase này)

- **Job `report_jobs` mồ côi ở `processing`/`sending`** khi replica bị kill giữa request
  on-demand. Không có reaper. Sau H.3, các job *daily* mồ côi không còn gây gửi trùng nữa;
  job on-demand mồ côi chỉ là rác hiển thị. Nếu muốn dọn: thêm một job `reap_stale_jobs`
  vào cùng cơ chế claim, đánh `failed` cho row `processing` quá 1 giờ.
- **Dọn `cron_runs` cũ.** Mỗi ngày 2 row ⇒ ~730 row/năm. Không cần dọn.
