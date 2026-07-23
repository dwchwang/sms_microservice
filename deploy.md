# VCS-SMS — Hướng dẫn deploy lên Docker Swarm (3 node: 1 manager + 2 worker)

> File stack: `server-management-system/docker-stack.yml`
> Docker Hub: `baohoang2411/vcs-sms-*`
> Lệnh chạy **trên manager** trừ khi ghi rõ `[worker]`. Trong repo dùng thư mục
> `server-management-system/`.

---

## 0. Trạng thái — đã làm tới đâu

Đã xong (không phải làm lại):

- [x] Sửa `docker-stack.yml` §A.1 — DB password inline khớp `init.sql`, bỏ `postgres_password` khỏi 3 app service
- [x] Sửa `docker-stack.yml` §A.2 — SMTP (username/from/recipient/cron 09:30)
- [x] Đổi tên image trong stack sang `baohoang2411/vcs-sms-*:latest`
- [x] Ghim postgres/redis/elasticsearch/traefik vào node manager (state + config node-local)
- [x] Bỏ bind-mount log của 4 app service → log ra stdout (chạy được trên mọi node)
- [x] Build 7 image
- [x] Push 7 image lên Docker Hub (`baohoang2411/vcs-sms-*`)
- [x] Xác minh App Password Gmail đăng nhập OK

Còn phải tự làm: **§1 → §5** bên dưới.

---

## A. (Tham khảo) Các sửa đổi trong stack — ĐÃ ÁP DỤNG

> Ghi lại để hiểu vì sao stack như hiện tại. Không cần làm lại.

- **A.1** DB user password do `init.sql` đặt cố định (`identity_pass_secret`,
  `server_pass_secret_v2`, `report_pass_secret_v2`) → đặt thẳng vào env service, `postgres_password`
  chỉ còn dùng cho service `postgres`.
- **A.2** report-service: `SMTP_USERNAME`/`SMTP_FROM` = `baohoangbh02411@gmail.com`,
  `REPORT_DAILY_RECIPIENT`, `REPORT_SNAPSHOT_CRON="30 9 * * *"`, `REPORT_DAILY_CRON="0 10 * * *"`.
- **A.3** Image = `baohoang2411/vcs-sms-*:latest` (public → node tự pull, không cần login).
- **A.4** placement: postgres/redis/elasticsearch/traefik ghim `node.role == manager`.
- **A.5** 4 app service không còn bind-mount log; xem log bằng `docker service logs`.

---

## 1. Chuẩn bị 3 node

- [ ] Cài Docker Engine trên cả 3 node (1 manager + 2 worker), cùng mạng LAN
- [ ] Copy repo lên **node manager** (cần cho bind mount của traefik config + init.sql + seed)
- [ ] Trên **manager** (vì elasticsearch ghim ở manager): bật `vm.max_map_count`
  ```bash
  sudo sysctl -w vm.max_map_count=262144
  echo 'vm.max_map_count=262144' | sudo tee -a /etc/sysctl.conf
  ```
- [ ] Trên **manager**, trong `server-management-system/`, tạo thư mục log cho traefik
  ```bash
  mkdir -p logs/traefik
  ```

---

## 2. Khởi tạo swarm + join worker

- [ ] Trên **manager** (thay IP LAN thật của manager):
  ```bash
  docker swarm init --advertise-addr <MANAGER_IP>
  docker swarm join-token worker      # copy nguyên lệnh in ra
  ```
- [ ] `[worker]` chạy trên **từng** worker lệnh vừa copy:
  ```bash
  docker swarm join --token <TOKEN> <MANAGER_IP>:2377
  ```
- [ ] Trên **manager** kiểm tra đủ 3 node Ready/Active:
  ```bash
  docker node ls        # 1 Leader (manager) + 2 worker
  ```

---

## 3. Tạo Docker secret (chỉ trên manager — swarm tự phân phối)

- [ ] Tạo 4 secret (dùng stdin để không lưu vào shell history):
  ```bash
  printf '%s' 'MK_POSTGRES_MANH'    | docker secret create postgres_password -
  printf '%s' 'MK_REDIS_MANH'       | docker secret create redis_password -
  printf '%s' 'CHUOI_JWT_64_KY_TU'  | docker secret create jwt_secret -
  printf '%s' 'axfjjalzothswhrb'    | docker secret create smtp_password -   # Gmail App Password
  ```
- [ ] `docker secret ls` → thấy đủ 4

> Secret không sửa tại chỗ được. Đổi giá trị: gỡ service dùng nó → `docker secret rm` → tạo lại.
> `postgres_password` chỉ đặt password superuser `vcs_admin`, và chỉ có tác dụng khi volume
> `postgres_data` còn trống (init lần đầu).

---

## 4. Deploy stack (trên manager, trong thư mục repo)

- [ ] Deploy:
  ```bash
  docker stack deploy -c docker-stack.yml vcs
  ```
- [ ] Chờ tất cả service `1/1`:
  ```bash
  watch docker stack services vcs
  ```
- [ ] Xem phân bố node (postgres/redis/es/traefik ở manager; app service rải các node):
  ```bash
  docker stack ps vcs
  ```

> Swarm bỏ qua `depends_on`; auth/server/report có thể restart vài lần trong lúc chờ
> Postgres/Redis/ES sẵn sàng — bình thường, đợi ~30–60s để hội tụ.
> Image public → worker tự pull, **không cần `docker login`** trên worker.

---

## 5. Sau deploy

- [ ] **Bắt buộc — dựng target projection + marker ready** (thiếu nó monitor bỏ mọi round).
  server-service có thể nằm ở worker; tìm node rồi exec ở node đó:
  ```bash
  docker service ps vcs_server-service --filter desired-state=running --format '{{.Node}}'
  # SSH sang node đó (hoặc nếu là manager thì chạy luôn):
  docker exec $(docker ps -q -f name=vcs_server-service) /app/server-service rebuild-monitor-cache
  ```
- [ ] (Tùy chọn) Seed 10.000 server test — postgres ở manager:
  ```bash
  docker exec $(docker ps -q -f name=vcs_postgres) \
    sh -c 'psql -U vcs_admin -d server_db -f /seed/seed_10k_servers.sql'
  ```
  rồi chạy lại `rebuild-monitor-cache` ở bước trên.
- [ ] Verify qua ingress mesh (gọi được qua IP **bất kỳ** node nào):
  ```bash
  curl http://<ANY_NODE_IP>:8080/health
  curl -X POST http://<ANY_NODE_IP>:8080/api/v1/auth/login \
    -H 'Content-Type: application/json' \
    -d '{"email":"admin@vcs.com","password":"Admin@123456"}'
  ```
- [ ] Đổi mật khẩu admin mặc định
- [ ] Gửi thử 1 report → job `sent`, email tới hộp thư

---

## 6. ⚠️ Web — build lại nếu truy cập UI từ máy khác

Image web đã **bake `NEXT_PUBLIC_API_BASE_URL=http://localhost:8080`** lúc build. Truy cập
`http://<node-ip>:3000` từ máy khác thì trình duyệt gọi API ở `localhost` của **máy người
dùng** → sai. Cần build lại với URL thật rồi push + update:

```bash
docker build -f web/Dockerfile \
  --build-arg NEXT_PUBLIC_API_BASE_URL=http://<MANAGER_IP>:8080/api/v1 \
  -t baohoang2411/vcs-sms-web:latest ./web
docker push baohoang2411/vcs-sms-web:latest
docker service update --image baohoang2411/vcs-sms-web:latest --force vcs_web
```

(Chỉ cần khi dùng giao diện web từ máy khác; test bằng curl thì bỏ qua.)

---

## 7. Vận hành

**Xem log** (app service log ra stdout):
```bash
docker service logs -f vcs_monitor-service
docker service logs -f vcs_auth-service
```

**Update image sau khi push tag mới:**
```bash
docker service update --image baohoang2411/vcs-sms-server:latest --force vcs_server-service
# hoặc deploy lại cả stack:
docker stack deploy -c docker-stack.yml vcs
```

**Rollback:** `docker service rollback vcs_server-service`

**Gỡ stack** (secret và named volume KHÔNG bị xóa theo):
```bash
docker stack rm vcs
```

**Ghi chú topo:**
- postgres/redis/elasticsearch/traefik ghim manager (named volume + bind-mount config là
  node-local). Dữ liệu nằm trên manager → backup Postgres ở manager là đủ (design §15.4).
- 4 app service + web + tcp-simulator rải trên 3 node; cổng 8080/3000 (và 5432/6379/9200)
  truy cập qua ingress mesh từ mọi node IP.
- 🖧 tcp-simulator là test harness — bỏ khỏi stack khi deploy production thật (khi đó
  `ipv4/tcp_port` trỏ server thật); nhớ sửa/gỡ `MONITOR_TCP_DIAL_HOST` của monitor.
- 🔒 Prod nên bỏ publish cổng hạ tầng 5432/6379/9200 (chỉ để 8080 + 3000), và đặt
  `SMTP_RECIPIENT_DOMAINS` để tránh bị lợi dụng làm mail relay.

---

## 8. Checklist tổng

Đã xong:
- [x] Sửa stack (DB password, SMTP, image, placement, log)
- [x] Build + push 7 image lên Docker Hub

Còn làm:
- [ ] §1 Chuẩn bị 3 node (Docker, repo lên manager, `vm.max_map_count`, `logs/traefik`)
- [ ] §2 `swarm init` + 2 worker `swarm join` + `docker node ls` thấy 3 node
- [ ] §3 Tạo 4 secret + `docker secret ls`
- [ ] §4 `docker stack deploy` + mọi service `1/1`
- [ ] §5 `rebuild-monitor-cache` + verify `/health` + login
- [ ] §6 (nếu dùng UI từ máy khác) build lại web với `NEXT_PUBLIC_API_BASE_URL` đúng
- [ ] Đổi mật khẩu admin mặc định + gửi thử report
