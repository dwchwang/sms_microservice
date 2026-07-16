# Phase 7: Frontend & UI — Dashboard quản lý 10.000 server (Vercel Design Language)

> **Mục tiêu:** Xây dựng web frontend hoàn chỉnh cho toàn bộ 17 endpoints đã test thành công, áp dụng design language Vercel (`DESIGN-vercel.md`).
> **Thời gian:** 5–7 buổi (chia 8 sub-phase)
> **Prerequisite:** Phase 1–6 hoàn tất, API Gateway `:8080` chạy OK, đã có user admin seed.
> **Điểm đạt được:** Một dashboard production-grade, RBAC-aware, phủ 100% chức năng backend, đúng brand Vercel.

---

## Quyết định kiến trúc (đã chốt)

| Hạng mục | Lựa chọn |
|----------|----------|
| **Framework** | Next.js 15 (App Router) + React 19 + TypeScript |
| **Styling** | Tailwind CSS v4 (design tokens từ `DESIGN-vercel.md`) |
| **Component kit** | shadcn/ui (Radix primitives, re-skin theo Vercel) |
| **Data fetching** | TanStack Query v5 (cache, retry, invalidation) |
| **Forms** | React Hook Form + Zod (validation mirror OpenAPI) |
| **HTTP** | axios (instance + interceptors cho JWT/refresh) |
| **Charts** | Recharts (report uptime, daily breakdown) |
| **Icons** | lucide-react |
| **Font** | `geist` package (Geist Sans + Geist Mono — đúng brand) |
| **Vị trí** | `server-management-system/web/` |
| **Deploy** | Container `vcs-sms-web` trong `docker-compose.yml`, gọi gateway `:8080` |
| **State global** | Zustand (auth state, UI prefs) — phần còn lại để TanStack Query lo |

---

## Checklist tổng quan Phase 7

- [ ] **7.1** Scaffold Next.js + Tailwind + design tokens + Geist font
- [ ] **7.2** Design system layer — tokens, primitives (Button, Card, Input, Badge, Table…)
- [ ] **7.3** API layer — axios client, JWT/refresh interceptor, TanStack Query hooks, Zod schemas
- [ ] **7.4** Auth flow — Login/Register page, AuthProvider, protected layout, RBAC gating
- [ ] **7.5** App shell — Sidebar + Topbar + breadcrumb, responsive
- [ ] **7.6** Servers module — list (filter/sort/page), detail, create, edit, delete, import, export
- [ ] **7.7** Reports module — summary dashboard (charts) + send-email form
- [ ] **7.8** Users & Profile module — user list, change role, profile page
- [ ] **7.9** Monitor widget + Dashboard home (overview KPIs)
- [ ] **7.10** Polish — loading skeletons, empty states, toasts, error boundary, a11y, responsive QA
- [ ] **7.11** Dockerfile + compose integration + env config
- [ ] **7.12** README FE + verify end-to-end

---

## 7.0. Bản đồ endpoint → màn hình (nguồn sự thật)

Tất cả endpoint dưới prefix `http://localhost:8080/api/v1`. Cột "Scope" quyết định **gating UI**.

| # | Method | Endpoint | Màn hình / Hành động FE | Scope yêu cầu |
|---|--------|----------|--------------------------|---------------|
| 1 | POST | `/auth/register` | Trang Register (public) | — |
| 2 | POST | `/auth/login` | Trang Login (public) | — |
| 3 | POST | `/auth/refresh` | Interceptor tự gọi khi 401 | — |
| 4 | POST | `/auth/logout` | Nút Logout (topbar menu) | authenticated |
| 5 | GET | `/auth/profile` | Trang Profile + load AuthProvider | authenticated |
| 6 | POST | `/servers` | Modal/Drawer "Tạo server" | `server:create` |
| 7 | GET | `/servers` | Bảng danh sách server (filter/sort/page) | `server:read` |
| 8 | GET | `/servers/{id}` | Trang chi tiết server | `server:read` |
| 9 | PUT | `/servers/{id}` | Form "Sửa server" | `server:update` |
| 10 | DELETE | `/servers/{id}` | Nút xóa + confirm modal | `server:delete` |
| 11 | POST | `/servers/import` | Drawer "Import Excel" (upload .xlsx) | `server:import` |
| 12 | GET | `/servers/import/{job_id}` | Poll tiến độ job (progress + kết quả) | `server:import` |
| 13 | POST | `/servers/export` | Nút "Export Excel" (download file) | `server:export` |
| 14 | GET | `/reports/summary` | Dashboard báo cáo (charts + KPI) | `report:view` |
| 15 | POST | `/reports` | Form "Gửi báo cáo qua email" | `report:send` |
| 16 | GET | `/auth/users` | Bảng quản lý người dùng | `user:manage` |
| 17 | PUT | `/auth/users/{id}/role` | Dropdown đổi role trong bảng user | `user:manage` |
| — | GET | `/monitor/status` | Widget trạng thái monitor service | `monitor:view`* |

> *Lưu ý:* `/monitor/status` chỉ trả **metadata service** (check_interval, worker_count, ES index…), **không phải** status từng server. Trạng thái On/Off realtime của mỗi server lấy từ field `status` trong `/servers` (Monitor service cập nhật DB qua health-check 60s).
>
> ⚠️ **ĐÃ XÁC THỰC CODE:** scope `monitor:view` **KHÔNG được seed cho bất kỳ role nào** (`migrations/auth/000002_create_role_permissions.up.sql`). Gateway lại bắt buộc scope này cho `/monitor/status` → endpoint **trả 403 cho MỌI user, kể cả admin**. ⇒ Phải xử lý backend trước (chọn 1): (a) seed `monitor:view` cho admin, hoặc (b) đổi route monitor sang chỉ cần `authenticated`. **Nếu chưa fix, KHÔNG build monitor widget** — hoặc build nhưng ẩn khi 403.

### Ma trận quyền theo Role — ⚠️ ĐÃ XÁC THỰC từ DB seed (KHÁC `architecture.md`)

> Nguồn sự thật: `migrations/auth/000002_create_role_permissions.up.sql` (KHÔNG phải §8.2 của architecture.md — tài liệu đó ghi sai scope của Operator).
>
> **Scope thật mỗi role:**
> - **admin** (9): `server:create`, `server:read`, `server:update`, `server:delete`, `server:import`, `server:export`, `report:view`, `report:send`, `user:manage`
> - **operator** (3): `server:read`, `server:update`, `report:view`
> - **viewer** (2): `server:read`, `report:view`

| Chức năng | Admin | Operator | Viewer |
|-----------|:-----:|:--------:|:------:|
| Xem server list/detail | ✅ | ✅ | ✅ |
| Tạo server | ✅ | ❌ | ❌ |
| Sửa server | ✅ | ✅ | ❌ |
| Xóa server | ✅ | ❌ | ❌ |
| Import Excel | ✅ | ❌ | ❌ |
| Export Excel | ✅ | ❌ | ❌ |
| Xem báo cáo | ✅ | ✅ | ✅ |
| Gửi báo cáo email | ✅ | ❌ | ❌ |
| Quản lý user / đổi role | ✅ | ❌ | ❌ |

> FE phải **ẩn (không chỉ disable)** các action không có scope; backend vẫn là lớp bảo vệ thật (403). FE đọc `scopes[]` từ `/auth/profile`.
> Thực tế chỉ **admin** mới thấy hầu hết action ghi-dữ-liệu; operator chỉ sửa server + xem báo cáo; viewer chỉ đọc. Test bằng tài khoản seed: **`admin` / `Admin@123456`**.

---

## 7.1. Scaffold + Design Tokens

**Khởi tạo:**
```bash
cd server-management-system
npx create-next-app@latest web --ts --tailwind --app --eslint --src-dir --import-alias "@/*"
cd web
npm i @tanstack/react-query axios zod react-hook-form @hookform/resolvers \
      zustand recharts lucide-react geist sonner clsx tailwind-merge \
      @radix-ui/react-dialog @radix-ui/react-dropdown-menu @radix-ui/react-select \
      @radix-ui/react-tooltip @radix-ui/react-tabs @radix-ui/react-avatar
npx shadcn@latest init
```

**Map `DESIGN-vercel.md` → Tailwind theme** (`src/app/globals.css` với `@theme` của Tailwind v4):

```css
@theme {
  /* Surfaces */
  --color-canvas: #ffffff;
  --color-canvas-soft: #fafafa;
  --color-canvas-soft-2: #f5f5f5;
  --color-primary: #171717;     /* ink — CTA + dark bands */
  --color-on-primary: #ffffff;

  /* Text */
  --color-ink: #171717;
  --color-body: #4d4d4d;
  --color-mute: #888888;

  /* Lines */
  --color-hairline: #ebebeb;
  --color-hairline-strong: #a1a1a1;

  /* Semantic */
  --color-link: #0070f3;
  --color-success: #0070f3;
  --color-error: #ee0000;
  --color-error-soft: #f7d4d6;
  --color-warning: #f5a623;
  --color-warning-soft: #ffefcf;

  /* Brand gradient stops (hero only) */
  --color-grad-develop-start: #007cf0;
  --color-grad-develop-end:   #00dfd8;
  --color-grad-preview-start: #7928ca;
  --color-grad-preview-end:   #ff0080;
  --color-grad-ship-start:    #ff4d4d;
  --color-grad-ship-end:      #f9cb28;

  /* Radius */
  --radius-sm: 6px;   /* in-app buttons, inputs (geist-radius) */
  --radius-md: 8px;   /* cards */
  --radius-lg: 12px;  /* large cards */
  --radius-pill: 100px;

  /* Font */
  --font-sans: var(--font-geist-sans);
  --font-mono: var(--font-geist-mono);
}
```

**Quy ước brand bắt buộc (từ DESIGN-vercel.md "Do's & Don'ts"):**
- App UI dùng **radius 6px** cho button/input/dropdown (KHÔNG dùng pill 100px trong app — pill chỉ cho marketing/auth hero CTA).
- Headline weight tối đa **600**, sentence-case, negative tracking.
- Mono (`Geist Mono`) chỉ cho: `server_id`, IPv4, code, label kỹ thuật, eyebrow. Body luôn sans.
- Shadow **stacked** + inset hairline ring (Level 1–5), không drop-shadow nặng.
- Trạng thái: dùng `success`(on) / `mute`(off) / `warning`(pending) / `error`(failed) — không thêm accent thứ 6.
- Mesh gradient **chỉ** ở hero auth/landing + empty-state lớn, không thu nhỏ thành icon.

---

## 7.2. Design System Layer (primitives)

Thư mục `src/components/ui/` — shadcn re-skin theo token. Các primitive cần build/điều chỉnh:

| Primitive | Map DESIGN-vercel | Ghi chú |
|-----------|-------------------|---------|
| `Button` | `button-primary-sm` / `button-secondary-sm` | variants: `primary` (ink), `secondary` (white+hairline), `ghost`, `destructive` (error); size sm/md; radius 6px in-app |
| `Card` | `card-marketing` / `card-soft` | Level 2–3 stacked shadow + inset ring |
| `Input` / `Textarea` | `form-input` (h40) / `form-input-sm` (h32) | hairline border, focus ring `link` |
| `Select` | dropdown menu `canvas-soft-2` | Radix Select |
| `Badge` / `StatusPill` | `badge-secondary` | màu theo status (on/off/pending/failed) |
| `Table` | `ex-data-table-cell` | header `caption-mono` uppercase, body `body-sm`, row border hairline |
| `Dialog` / `Drawer` | `ex-modal-card` (Level 5) | dùng cho create/edit/import/confirm |
| `Tabs` | `tab-ghost` (pill-sm 64px) | filter tabs |
| `Toast` | `ex-toast` (sonner) | success/error feedback |
| `Skeleton` | canvas-soft shimmer | loading state bảng/cards |
| `EmptyState` | `ex-empty-state-card` | empty list + mesh gradient nhẹ |
| `Pagination` | hairline buttons | server-side paging |
| `Avatar` / `RoleBadge` | — | hiển thị role màu phân biệt |

**KPI/Stat card** (cho dashboard): `display-md` số liệu + `caption-mono` label, Level 3 shadow.

---

## 7.3. API Layer

**`src/lib/api/client.ts`** — axios instance:
- `baseURL = process.env.NEXT_PUBLIC_API_BASE_URL` (default `http://localhost:8080/api/v1`).
- Request interceptor: gắn `Authorization: Bearer <access_token>`.
- Response interceptor: nếu `401` → thử `POST /auth/refresh` với `refresh_token` (single-flight, queue request đang chờ) → retry; fail thì logout + redirect `/login`.
- Chuẩn hóa lỗi: parse `ApiErrorResponse` (`{status, code, message, errors[], meta}`) → ném `ApiError` có `fieldErrors` để form hiển thị.

> ⚠️ **ĐÃ XÁC THỰC envelope thật** (`shared/response/response.go`):
> - Mọi response bọc trong `{ status, code, message, data, meta }`. **`code` = HTTP status code** (200/401/403/404/409/422/429/500) — **KHÔNG phải mã 5 chữ số** (ví dụ `42201` trong OpenAPI chỉ là minh họa, không phải giá trị thật). ⇒ Error handling FE switch theo HTTP status (hoặc `body.code`), không parse mã 5 số.
> - Payload thật nằm ở **`response.data.data`** (axios `.data` rồi `.data` của envelope).
> - **Field errors:** mảng `errors[]` mỗi phần tử `{ field, code, message }` — `code` ở đây là string (`INVALID_FORMAT`…). Map `field → message` vào React Hook Form.

> ⚠️ **ĐÃ XÁC THỰC shape danh sách — KHÔNG đồng nhất giữa 2 service:**
> - `GET /servers` → `data` = `{ servers: ServerResponse[], total, page, page_size, total_pages }` — **key là `servers`**, KHÔNG phải `items` (dù OpenAPI ghi `items`). `total` là số (int64).
> - `GET /auth/users` → `data` = `{ items: UserResponse[], total, page, page_size, total_pages }` — key là **`items`**.
> ⇒ Viết 2 type riêng, đừng giả định chung key.

> ⚠️ **ĐÃ XÁC THỰC `UserResponse`** (`auth-service/internal/dto/response.go`): chỉ có `id, username, email, full_name, role, scopes[], is_active, created_at`. **KHÔNG có `last_login_at`** (dù OpenAPI ghi). ⇒ Bỏ cột "last login" khỏi bảng Users & trang Profile, hoặc đề xuất BE bổ sung field.

> ⚠️ **`ServerResponse`** dùng con trỏ cho số → field `os, cpu_cores, ram_gb, disk_gb, location, description` là `omitempty` (có thể vắng mặt trong JSON). FE phải xử lý `undefined`/null (hiển thị "—").

**Token storage:** access token trong memory (Zustand) + refresh token trong cookie `httpOnly`-style (do là SPA gọi gateway, lưu localStorage có XSS risk → khuyến nghị lưu refresh ở cookie qua route handler Next; nếu đơn giản hóa: localStorage, ghi chú rủi ro). Quyết định mặc định: **localStorage cho cả 2** (dashboard nội bộ), có TODO nâng cấp cookie httpOnly.

**Zod schemas** (`src/lib/api/schemas.ts`) — mirror OpenAPI:
- `loginSchema`, `registerSchema` (username≥3, password≥8, email).
- `createServerSchema`: `server_id` regex `^[A-Z0-9\-_]+$` (3–100), `server_name` (3–255), `ipv4` format, cpu_cores≥1, ram/disk≥0.
- `updateServerSchema` (partial + `status: on|off`).
- `sendReportSchema` (start_date, end_date, email).
- `updateRoleSchema` (`admin|operator|viewer`).

**TanStack Query hooks** (`src/lib/api/hooks/`):
- Auth: `useLogin`, `useRegister`, `useLogout`, `useProfile`.
- Servers: `useServers(params)`, `useServer(id)`, `useCreateServer`, `useUpdateServer`, `useDeleteServer`.
- Import: `useImportServers` (mutation upload), `useImportJob(jobId)` (`refetchInterval` khi status `pending|processing`, dừng khi `completed|failed`).
- Export: `useExportServers` (mutation → blob → trigger download).
- Reports: `useReportSummary(start,end)`, `useSendReport`.
- Users: `useUsers(page)`, `useUpdateUserRole`.
- Monitor: `useMonitorStatus`.

Query keys chuẩn hóa + invalidation: tạo/sửa/xóa/import server → invalidate `['servers']`.

---

## 7.4. Auth Flow & RBAC

**Pages (route group `(public)`):**
- `/login` — `ex-auth-form-card` trên `canvas-soft`, hero mesh gradient half-band. Form: username + password. Link → register. CTA pill `button-primary`.
- `/register` — form username/email/password/full_name. Sau đăng ký thành công (role mặc định `viewer`) → toast + redirect `/login`.

**AuthProvider** (`src/providers/auth-provider.tsx`):
- Khi mount: nếu có token → gọi `/auth/profile` lấy `{id, username, role, scopes[]}` → set Zustand store.
- Expose `hasScope(scope)`, `hasAnyScope([])`, `user`, `role`.

**Route protection:**
- Route group `(app)` bọc bởi `layout.tsx` kiểm tra auth → chưa login redirect `/login`.
- `<Can scope="server:create">…</Can>` component gate UI element.
- Middleware Next (`middleware.ts`) chặn truy cập route cứng (vd `/users` cần `user:manage`) — fallback `/403` page.

---

## 7.5. App Shell

**Layout** (`(app)/layout.tsx`): Sidebar trái + Topbar + content.

```
┌──────────────────────────────────────────────┐
│ Topbar: logo · breadcrumb ··· [monitor dot] [profile▼] │  h64
├────────────┬─────────────────────────────────┤
│ Sidebar    │  Page content (max-w 1400, gutters 24)        │
│ (240px)    │                                               │
│ ▸ Dashboard│                                               │
│ ▸ Servers  │   ex-app-shell-row: active = left ink bar     │
│ ▸ Reports  │                                               │
│ ▸ Users*   │                                               │
│ ────────── │                                               │
│ profile    │                                               │
└────────────┴─────────────────────────────────┘
```

- Sidebar item: `ex-app-shell-row`, active state = thanh `primary` bên trái + nền `canvas-soft`.
- Nav item gate theo scope: "Users" chỉ hiện với `user:manage`.
- Topbar: monitor health dot (xanh `success`/đỏ `error` từ `/monitor/status`), profile dropdown (Profile / Logout).
- Responsive: <960px Sidebar → drawer hamburger (overlay), nội dung 1-up.

---

## 7.6. Servers Module (trọng tâm — quản lý 10K server)

### 7.6.1. `/servers` — Danh sách
- **Toolbar:** ô search `server_name` (debounce 400ms), filter `status` (tab-ghost: All/On/Off), filter `location`, `os`, `ipv4`; nút `[+ Tạo server]` (gate `server:create`), `[Import]` (gate `server:import`), `[Export]` (gate `server:export`).
- **Bảng** (`ex-data-table-cell`): cột `server_id`(mono) · `server_name` · `StatusPill` · `ipv4`(mono) · `os` · `location` · `cpu_cores` · `ram_gb` · actions (xem/sửa/xóa theo scope).
- Header cột clickable → `sort_by` + `sort_order` (icon mũi tên); đồng bộ vào URL query.
- **Pagination server-side:** `page`/`page_size` (20 default), hiển thị `total`, `total_pages` — quan trọng vì 10K rows, KHÔNG load hết.
- URL là nguồn sự thật của filter/sort/page (shareable, back-button OK).
- States: skeleton rows khi loading, EmptyState khi 0 kết quả, error banner khi fail.

### 7.6.2. `/servers/[server_id]` — Chi tiết
- Header: `server_name` (`display-md`) + `StatusPill` + `server_id` mono badge.
- Card thông số: 2 cột grid (ipv4, os, cpu, ram, disk, location, created_at, updated_at).
- Nút `[Sửa]` (gate `server:update`), `[Xóa]` (gate `server:delete`).

### 7.6.3. Create / Edit (Dialog/Drawer)
- `ex-modal-card`, React Hook Form + Zod.
- Create: tất cả field; hiển thị inline field-errors map từ `errors[].field` (vd `ipv4 INVALID_FORMAT`, 409 trùng `server_id`).
- Edit: `server_id` **disabled** (không cho sửa), thêm toggle `status on/off`.
- Sau thành công → toast + invalidate `['servers']` + đóng modal.

### 7.6.4. Delete
- Confirm Dialog (destructive): "Xóa server SRV-xxxxx?" → `DELETE` → toast + refresh.

### 7.6.5. Import Excel (Drawer)
- Dropzone .xlsx (validate client: ext + ≤10MB). Upload → nhận `job_id` (202).
- **Progress view:** poll `GET /servers/import/{job_id}` mỗi 2s; progress bar theo status `pending → processing → completed/failed`.
- Khi `completed`: hiển thị `total_rows / success_count / failed_count`, 2 tab: **Success list** + **Failed list** (`row_number`, `server_id`, `error_reason`). Nút "Tải lại danh sách" → invalidate servers.

### 7.6.6. Export Excel
- Nút `[Export]` mở popover lấy filter hiện tại (status/name/ipv4/location/os/sort) → `POST /servers/export` → nhận blob `.xlsx` → đọc `Content-Disposition` filename → trigger download. Loading spinner trên nút.

---

## 7.7. Reports Module

### 7.7.1. `/reports` — Summary dashboard (gate `report:view`)
- **Date range picker** (start_date, end_date) → `GET /reports/summary`.
- **KPI row** (4 stat cards): Total servers · Servers On · Servers Off · Avg uptime %.
- **Chart 1** — Daily breakdown: Recharts stacked area/bar (on/off theo ngày) + line uptime%.
- **Chart 2** — Donut On/Off tỉ lệ.
- **Bảng** Low-uptime servers (top N): `server_id` · `server_name` · `uptime%` (highlight `warning`/`error` nếu thấp).
- Empty/loading states; cache theo `report:summary:{start}:{end}`.

### 7.7.2. Send report email (gate `report:send`)
- Nút `[Gửi qua email]` → Dialog form: start_date, end_date, email (Zod). Submit `POST /reports`.
- Success: toast "Report sent successfully" + hiển thị `report_id`. Xử lý 500 (SMTP fail) → error toast rõ ràng.

---

## 7.8. Users & Profile Module

### 7.8.1. `/users` — Quản lý người dùng (gate `user:manage`, Admin only)
- Bảng: `username` · `full_name` · `email` · `RoleBadge` · `is_active` · `last_login_at`.
- Cột Role: dropdown đổi role (`admin/operator/viewer`) → `PUT /auth/users/{id}/role`.
- Chặn tự đổi role của chính mình (disable row của current user; backend cũng trả 400).
- Pagination (`page`, `page_size`).

### 7.8.2. `/profile` — Hồ sơ cá nhân
- Card thông tin từ `/auth/profile`: username, email, full_name, role, scopes (chip list mono), is_active, last_login_at, created_at.
- Nút Logout.

---

## 7.9. Dashboard Home + Monitor Widget

### `/` (Dashboard home)
- **Hero band** nhẹ (mesh gradient) chào mừng + tên user.
- **KPI overview:** 3 stat: Total / On / Off — lấy `total` từ `GET /servers?page_size=1` (toàn bộ), `status=on`, `status=off`. (Tối ưu: đề xuất BE thêm `/servers/stats` để 1 call.)
- **Monitor widget:** `GET /monitor/status` → card hiển thị check_interval, worker_count, ES index, redis_available (dot xanh/đỏ). Gate `monitor:view` (ẩn nếu không có).
- **Quick actions:** shortcut tới Servers / Reports / Import theo scope.

---

## 7.10. Polish & Non-functional

- **Loading:** skeleton cho mọi bảng/card; spinner trên nút mutation.
- **Empty states:** `ex-empty-state-card` cho list rỗng (servers, users, low-uptime).
- **Error handling:** global Error Boundary + toast cho mutation lỗi; field-level errors map từ `ErrorResponse.errors[]`; trang `/403`, `/404`, `/500`.
- **Rate limit (429):** interceptor hiển thị toast "Quá nhiều yêu cầu, thử lại sau".
- **Responsive:** breakpoints theo DESIGN-vercel (mobile <600, tablet 600–959, desktop 960+); bảng → horizontal scroll / card-stack ở mobile; sidebar → drawer.
- **A11y:** focus ring `link`, aria-label nút icon, contrast ink/body đạt WCAG AA, keyboard nav cho Dialog/Select (Radix lo sẵn).
- **Mono discipline:** chỉ `server_id`/IPv4/scopes/code dùng Geist Mono.

---

## 7.11. Dockerfile + Compose

**`web/Dockerfile`** (multi-stage, Next standalone output):
```dockerfile
FROM node:22-alpine AS deps
WORKDIR /app
COPY package*.json ./
RUN npm ci
FROM node:22-alpine AS build
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
ENV NEXT_PUBLIC_API_BASE_URL=http://localhost:8080/api/v1
RUN npm run build
FROM node:22-alpine AS run
WORKDIR /app
ENV NODE_ENV=production
COPY --from=build /app/.next/standalone ./
COPY --from=build /app/.next/static ./.next/static
COPY --from=build /app/public ./public
EXPOSE 3000
CMD ["node", "server.js"]
```
- `next.config.ts`: `output: 'standalone'`.
- Thêm service `vcs-sms-web` vào `docker-compose.yml` (port `3000:3000`, `depends_on: api-gateway`, env `NEXT_PUBLIC_API_BASE_URL`).
- **CORS origins:** ✅ đã xác thực — gateway default `CORS_ALLOWED_ORIGINS` đã gồm `http://localhost:3000,http://localhost:5173` (`api-gateway/config/config.go:80`). **Không cần sửa** cho dev local.
- ⚠️ **CORS expose headers (CẦN FIX BE cho Export):** `cors.go:29` hiện expose `X-Request-ID, X-RateLimit-*` — **thiếu `Content-Disposition`**. Trình duyệt sẽ KHÔNG đọc được filename từ header export. ⇒ 2 lựa chọn: (a) thêm `Content-Disposition` vào `Access-Control-Expose-Headers` ở gateway (1 dòng), hoặc (b) FE tự sinh filename `servers_export_<timestamp>.xlsx` (fileio dùng đúng pattern này — `generator.go:116`). **Khuyến nghị (a).**

---

## 7.12. Folder Structure (đề xuất)

```
web/
├── src/
│   ├── app/
│   │   ├── (public)/login/page.tsx
│   │   ├── (public)/register/page.tsx
│   │   ├── (app)/layout.tsx           # shell + auth guard
│   │   ├── (app)/page.tsx             # dashboard home
│   │   ├── (app)/servers/page.tsx
│   │   ├── (app)/servers/[server_id]/page.tsx
│   │   ├── (app)/reports/page.tsx
│   │   ├── (app)/users/page.tsx
│   │   ├── (app)/profile/page.tsx
│   │   ├── 403/page.tsx · 404 · error.tsx
│   │   ├── layout.tsx · globals.css   # tokens + Geist font
│   ├── components/
│   │   ├── ui/                         # shadcn primitives (re-skin)
│   │   ├── shell/                      # Sidebar, Topbar, Breadcrumb
│   │   ├── servers/                    # ServerTable, ServerForm, ImportDrawer, ExportButton
│   │   ├── reports/                    # SummaryCharts, SendReportDialog
│   │   ├── users/                      # UserTable, RoleSelect
│   │   └── common/                     # Can, StatusPill, EmptyState, KpiCard
│   ├── lib/
│   │   ├── api/client.ts · schemas.ts · hooks/*
│   │   └── utils.ts (cn, formatters)
│   ├── providers/ (auth, query, theme)
│   ├── store/ (zustand auth + ui)
│   └── middleware.ts
├── public/ · Dockerfile · next.config.ts · tailwind config
```

---

## Thứ tự thực hiện đề xuất (sub-phase)

1. **7.1 + 7.2 + 7.3** — nền tảng (scaffold, tokens, primitives, API client). *Không có nền này thì không build page được.*
2. **7.4 + 7.5** — auth + app shell (đăng nhập được, thấy layout).
3. **7.6** — Servers module (chức năng cốt lõi, lớn nhất).
4. **7.7** — Reports.
5. **7.8 + 7.9** — Users, Profile, Dashboard home, Monitor widget.
6. **7.10** — Polish.
7. **7.11 + 7.12** — Docker + verify e2e.

---

## Rủi ro & điểm cần xác nhận — ✅ ĐÃ XÁC THỰC CODE

| # | Vấn đề | Trạng thái xác thực | Hành động |
|---|--------|---------------------|-----------|
| 1 | **Scope `monitor:view`** không seed cho role nào → `/monitor/status` 403 cho mọi user | ✅ Confirmed (`000002...up.sql`) | **BE fix trước** (seed admin scope HOẶC đổi route → authenticated). Nếu không: FE ẩn monitor widget khi 403. |
| 2 | **CORS Expose-Headers thiếu `Content-Disposition`** | ✅ Confirmed (`cors.go:29`) | BE thêm 1 dòng, hoặc FE tự sinh filename. |
| 3 | **CORS origins** đã gồm `localhost:3000` | ✅ Confirmed (`config.go:80`) — KHÔNG cần sửa | — |
| 4 | **Server list key = `servers`** (không phải `items`) | ✅ Confirmed (`server-service/dto/response.go:23`) | FE parse đúng key. |
| 5 | **`code` = HTTP status** (không phải mã 5 số) | ✅ Confirmed (`response.go:44`) | Error handling theo HTTP status. |
| 6 | **`UserResponse` thiếu `last_login_at`** | ✅ Confirmed (`auth-service/dto/response.go`) | Bỏ cột last-login hoặc BE bổ sung. |
| 7 | **RBAC Operator** thực tế chỉ 3 scope (docs ghi 7) | ✅ Confirmed (`000002...up.sql`) | Dùng matrix đã sửa ở §7.0. |
| 8 | **Import max size 10MB**, chỉ `.xlsx` | ✅ Confirmed (`config.go:85`, `import_service.go:98-105`) | FE validate client: ext `.xlsx` + ≤10MB. |
| 9 | **Rate limit 100 req / 60s / IP** | ✅ Confirmed (`config.go:71-72`) | Interceptor xử lý 429. |
| 10 | **Đếm On/Off dashboard**: chỉ có `total` trong list | — | Gọi list với `status=on`/`off`, `page_size=1`, đọc `total`. Hoặc đề xuất BE `/servers/stats`. |
| 11 | **Refresh token storage** | — | Mặc định localStorage (ghi chú XSS); nâng cấp cookie httpOnly nếu production. |

> **Khuyến nghị thứ tự:** trước khi build monitor widget & export, xin chốt với BE việc fix #1 (monitor:view) và #2 (Content-Disposition). Đây là 2 fix backend nhỏ (mỗi cái ~1 dòng). Các phần còn lại FE tự xử lý được.

---

> **Tài liệu liên quan:**
> - [DESIGN-vercel.md](../../DESIGN-vercel.md) — design language nguồn
> - [api-spec.yaml](../../docs/api-spec.yaml) — 17 endpoints
> - [architecture.md](../../docs/architecture.md) — kiến trúc backend, RBAC, infra
