# VCS SMS — Frontend (web)

Dashboard quản lý 10.000 server. Next.js 16 (App Router) + TypeScript + Tailwind v4, áp dụng
design language Vercel (`DESIGN-vercel.md`). Phủ toàn bộ 17 endpoint của API Gateway.

## Tech stack

- **Next.js 16** App Router (client-rendered dashboard sau JWT)
- **Tailwind v4** — design tokens map từ `DESIGN-vercel.md` (`src/app/globals.css`)
- **shadcn-style primitives** (Radix) — `src/components/ui`
- **TanStack Query v5** — fetch/cache/invalidation (`src/lib/api/hooks.ts`)
- **React Hook Form + Zod** — form & validation (`src/lib/api/schemas.ts`)
- **axios** — client + interceptor refresh JWT (`src/lib/api/client.ts`)
- **Recharts** — biểu đồ báo cáo
- **Zustand** — auth state (`src/store/auth.ts`)

## Chạy local

```bash
cp .env.example .env.local      # chỉnh NEXT_PUBLIC_API_BASE_URL nếu cần
npm install
npm run dev                     # http://localhost:3000
```

> Yêu cầu API Gateway chạy tại `http://localhost:8080`. CORS gateway đã cho phép
> `http://localhost:3000` sẵn (`api-gateway/config/config.go`).

Đăng nhập bằng tài khoản seed: **`admin` / `Admin@123456`**.

## Build & Docker

```bash
npm run build                   # standalone output
# hoặc qua compose (từ thư mục server-management-system):
docker compose up -d web        # service vcs-sms-web, cổng 3000
```

## Cấu trúc

```
src/
├── app/
│   ├── (public)/login · register        # public auth
│   ├── (app)/                           # layout có guard + shell
│   │   ├── page.tsx                     # dashboard tổng quan + monitor widget
│   │   ├── servers/ · servers/[server_id]
│   │   ├── reports/ · users/ · profile/
│   ├── 403 · error.tsx · not-found.tsx
├── components/ ui · common · shell · servers · reports
├── lib/api  client · endpoints · hooks · schemas · types
├── providers  query · auth
└── store/auth.ts
```

## Phân quyền (gate UI theo scope thật từ DB)

| | admin | operator | viewer |
|---|:-:|:-:|:-:|
| Xem server / báo cáo | ✅ | ✅ | ✅ |
| Sửa server | ✅ | ✅ | ❌ |
| Tạo / Import server | ✅ | ✅ | ❌ |
| Export server | ✅ | ✅ | ✅ |
| Xoá server | ✅ | ❌ | ❌ |
| Xem monitor hệ thống | ✅ | ✅ | ❌ |
| Gửi báo cáo email | ✅ | ✅ | ❌ |
| Quản lý người dùng | ✅ | ❌ | ❌ |

UI **ẩn** action không có scope; backend vẫn chặn 403 (lớp bảo vệ thật).

## Ghi chú tích hợp backend

- **Server list** trả key `servers` (không phải `items`); **users list** trả `items`.
- Response bọc `{ status, code, message, data, meta }`; `code` = HTTP status. Payload ở `data.data`.
- **Export**: filename đọc từ header `Content-Disposition` (gateway đã expose header này;
  nếu không, FE tự sinh `servers_export_<timestamp>.xlsx`).
- **Monitor widget**: cần scope `monitor:view`. Đã seed cho admin/operator trong migration
  `000002_create_role_permissions.up.sql` và được reconcile bằng `000004_reconcile_role_permissions.up.sql`.
