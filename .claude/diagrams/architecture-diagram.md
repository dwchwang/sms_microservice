# 🏗️ System Architecture Diagram — VCS-SMS

> **Ngày tạo:** 09/06/2026
> **Mô tả:** Sơ đồ kiến trúc tổng quan hệ thống VCS Server Management System.

---

## High-Level System Architecture

```mermaid
graph TB
    subgraph "Client Layer"
        Client["👤 Client<br/>(Postman / cURL / Frontend)"]
    end

    subgraph "Gateway Layer :8080"
        GW["🚪 API Gateway (Gin)<br/>JWT + Rate Limit + Reverse Proxy"]
    end

    subgraph "Business Services"
        AUTH["🔐 Auth Service<br/>:8081 | auth_schema"]
        SERVER["🖥️ Server Service<br/>:8082 | server_schema"]
        MONITOR["📡 Monitor Service<br/>:8083 | monitor_schema"]
        REPORT["📊 Report Service<br/>:8084 | report_schema"]
        FILEIO["📁 File I/O Service<br/>:8085 | fileio_schema"]
    end

    subgraph "Infrastructure"
        TCPSIM["🎭 TCP Simulator<br/>:9001-19000<br/>10.000 Fake Servers"]
    end

    subgraph "Message Broker"
        KAFKA["📨 Apache Kafka :9092<br/>7 Topics"]
    end

    subgraph "Data Layer"
        PG["🐘 PostgreSQL 17 :5432<br/>5 Schemas"]
        REDIS["⚡ Redis 8 :6379<br/>Cache / Lock / Blacklist"]
        ES["🔍 Elasticsearch 8 :9200<br/>server-status-logs"]
    end

    subgraph "External"
        SMTP["📧 Gmail SMTP<br/>smtp.gmail.com:587"]
    end

    Client -->|"REST API"| GW

    GW -->|"JWT + Scope"| AUTH
    GW -->|"JWT + Scope"| SERVER
    GW -->|"JWT + Scope"| MONITOR
    GW -->|"JWT + Scope"| REPORT
    GW -->|"JWT + Scope"| FILEIO

    AUTH --> PG
    AUTH --> REDIS

    SERVER --> PG
    SERVER --> REDIS
    SERVER -->|"server.created/updated/deleted"| KAFKA

    MONITOR --> PG
    MONITOR --> REDIS
    MONITOR -->|"server.health.batch<br/>server.status.changed"| KAFKA
    MONITOR -->|"Bulk Index 10K docs/60s"| ES
    MONITOR -.->|"TCP Connect<br/>net.DialTimeout"| TCPSIM

    REPORT --> PG
    REPORT -->|"Aggregation Query"| ES
    REPORT -->|"Consume health.batch"| KAFKA
    REPORT --> SMTP

    FILEIO --> PG
    FILEIO -->|"import.job.created"| KAFKA

    style Client fill:#e1f5fe
    style GW fill:#fff3e0
    style AUTH fill:#f3e5f5
    style SERVER fill:#e8f5e9
    style MONITOR fill:#fce4ec
    style REPORT fill:#e0f2f1
    style FILEIO fill:#fff8e1
    style TCPSIM fill:#ffccbc
    style KAFKA fill:#e0e0e0
    style PG fill:#bbdefb
    style REDIS fill:#ffcdd2
    style ES fill:#c8e6c9
    style SMTP fill:#d1c4e9
```

---

## Service Communication Matrix

| From → To | Auth | Server | Monitor | Report | FileIO | TCP Sim | Kafka | PG | Redis | ES | SMTP |
|-----------|:---:|:------:|:-------:|:------:|:------:|:-------:|:-----:|:--:|:-----:|:--:|:----:|
| **API Gateway** | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ✅ | ❌ | ❌ |
| **Auth** | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ | ❌ | ❌ |
| **Server** | ❌ | — | ❌ | ❌ | ❌ | ❌ | ✅(P) | ✅ | ✅ | ❌ | ❌ |
| **Monitor** | ❌ | ❌ | — | ❌ | ❌ | ✅(TCP) | ✅(P) | ✅(R) | ✅ | ✅ | ❌ |
| **Report** | ❌ | ❌ | ❌ | — | ❌ | ❌ | ✅(C) | ✅ | ✅ | ✅ | ✅ |
| **FileIO** | ❌ | ❌ | ❌ | ❌ | — | ❌ | ✅(P+C) | ✅(RW) | ❌ | ❌ | ❌ |
| **TCP Simulator** | ❌ | ❌ | ❌ | ❌ | ❌ | — | ❌ | ❌ | ❌ | ❌ | ❌ |

> **Legend:** P = Producer | C = Consumer | R = Read-only | RW = Read/Write

---

## Data Flow Summary

```mermaid
flowchart LR
    subgraph "1️⃣ Health Check (60s cycle)"
        M1["Monitor"] -->|"net.DialTimeout"| T1["TCP Simulator"]
        T1 -->|"Accept / Refuse"| M1
        M1 -->|"Bulk Index"| E1["Elasticsearch"]
        M1 -->|"Batch Update"| P1["PostgreSQL"]
        M1 -->|"Publish Events"| K1["Kafka"]
    end

    subgraph "2️⃣ Report (Daily 08:00)"
        R1["Report"] -->|"Aggregation"| E2["Elasticsearch"]
        E2 -->|"Uptime Data"| R1
        R1 -->|"Save Snapshot"| P2["PostgreSQL"]
        R1 -->|"Send HTML Email"| S1["Gmail SMTP"]
    end

    subgraph "3️⃣ Import Excel (Async)"
        F1["FileIO"] -->|"Upload .xlsx"| F1
        F1 -->|"Publish Job"| K2["Kafka"]
        K2 -->|"Consume Job"| F1
        F1 -->|"INSERT Servers"| P3["PostgreSQL"]
        F1 -->|"server.created"| K3["Kafka"]
    end
```

---

## Key Design Decisions

| # | Decision | Rationale |
|---|----------|-----------|
| 1 | **TCP Simulator Pool** | 1 Go container quản lý 10K TCP listeners, mở/đóng port theo Math Engine. Monitor ping TCP thật 100% |
| 2 | **Self-built API Gateway (Gin)** | Full control JWT, Rate Limiting, Reverse Proxy. Nhẹ hơn Kong/Traefik |
| 3 | **Shared Postgres, Separate Schemas** | 1 DB vật lý, 5 schemas riêng. Loose coupling + nhẹ máy |
| 4 | **Monorepo** | 1 docker-compose.yml, shared libs, quản lý tập trung |
| 5 | **Design First** | DB Schema → OpenAPI → Sequence → Code |
