# 📋 SMS Implementation Plan — Master Overview

> **Quy trình:** Design First → DB Schema → OpenAPI → Sequence Diagrams → Code → Test → Deploy
>
> **Kiến trúc:** Microservices (5 services + 1 Gateway + 1 TCP Simulator) | Monorepo | Shared Postgres (5 Schemas)

---

## Tổng quan Phases

| Phase | Tên | Thời gian | Mục tiêu chính |
|-------|-----|-----------|----------------|
| **[Phase 0](./phase-0-foundation.md)** | Foundation & Design | Tuần 1 | Setup monorepo, Docker, shared libs, DB schema, OpenAPI spec, TCP Simulator setup, Seed 10K servers |
| **[Phase 1](./phase-1-auth-server.md)** | Auth + Server Service | Tuần 2 | JWT auth, RBAC, CRUD server, API Gateway |
| **[Phase 2](./phase-2-monitor.md)** | Monitor + TCP Simulator | Tuần 3 | TCP Simulator service, Health-check scheduler, Elasticsearch, Worker Pool |
| **[Phase 3](./phase-3-report.md)** | Report Service | Tuần 4 | Uptime aggregation, Email, Daily cron, On-demand API |
| **[Phase 4](./phase-4-fileio.md)** | File I/O Service | Tuần 5 | Import/Export Excel, Async Kafka job |
| **[Phase 5](./phase-5-polish.md)** | Polish & Documentation | Tuần 6 | Test coverage, Docs, Deploy |

---

## Dependency Graph giữa các Phase

```
Phase 0 (Foundation + TCP Simulator Setup)
  │
  ├──→ Phase 1 (Auth + Server)
  │         │
  │         ├──→ Phase 2 (Monitor + TCP Simulator) ──→ Phase 3 (Report)
  │         │
  │         └──→ Phase 4 (File I/O)
  │
  └──────────────────────────────────────→ Phase 5 (Polish)
```

## Checklist nhanh — Điểm số mapping

| Điểm | Chức năng | Phase |
|------|-----------|-------|
| 2.0 | Kiểm tra trạng thái định kỳ | Phase 2 |
| 0.25 | Tạo Server | Phase 1 |
| 0.25 | View Server | Phase 1 |
| 0.25 | Update Server | Phase 1 |
| 0.25 | Delete Server | Phase 1 |
| 0.5 | Import Servers (Excel) | Phase 4 |
| 0.5 | Export Servers (Excel) | Phase 4 |
| 0.5 | Báo cáo định kỳ (Email) | Phase 3 |
| 0.5 | API Báo cáo chủ động | Phase 3 |
| 0.5 | OpenAPI | Phase 0 + mỗi Phase |
| 0.5 | Unit Test ≥ 90% | Mỗi Phase + Phase 5 |
| 0.5 | Chống SQL Injection (GORM) | Phase 1 |
| 0.5 | Error handling rõ ràng | Phase 0 (shared) |
| 0.5 | Log + logrotate | Phase 0 (shared) |
| 0.5 | JWT Auth + Scope | Phase 1 |
| 1.0 | Elasticsearch (Uptime) | Phase 2 + Phase 3 |
| 0.5 | Redis Cache | Phase 1 + Phase 2 |
| 0.5 | Công nghệ khác (Kafka, Docker) | Phase 0 + mỗi Phase |
| **10.0** | **Tổng** | |
