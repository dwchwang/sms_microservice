# 🔄 Server State Diagram — Trạng thái ON/OFF

> **Ngày tạo:** 09/06/2026
> **Mô tả:** Sơ đồ trạng thái của một server trong hệ thống VCS-SMS.

---

## State Machine — 1 Server

```mermaid
stateDiagram-v2
    [*] --> Offline: Server được tạo
    Offline --> Online: Health-check ON<br/>(TCP port mở, accept connection)
    Online --> Offline: Health-check OFF<br/>(TCP port đóng, connection refused)
    Offline --> [*]: Server bị xóa (soft delete)

    note right of Online
        🟢 TCP Simulator: MỞ port
        Monitor: net.DialTimeout → OK
        Status cập nhật trong PG + Redis
        Event "server.status.changed" → Kafka
    end note

    note left of Offline
        🔴 TCP Simulator: ĐÓNG port
        Monitor: net.DialTimeout → Refused
        Status cập nhật trong PG + Redis
        Event "server.status.changed" → Kafka
    end note
```

---

## State Transition Triggers

| Từ | Đến | Trigger | Actor |
|----|-----|---------|-------|
| `[*]` | `Offline` | Server được tạo (POST /servers hoặc Import Excel) | Admin |
| `Offline` | `Online` | Health-check trả về ON (TCP port mở) | Monitor Service |
| `Online` | `Offline` | Health-check trả về OFF (TCP port đóng) | Monitor Service |
| `Offline` | `[*]` | Server bị xóa (DELETE /servers/:id) | Admin |

---

## TCP Simulator: Math Engine quyết định On/Off

```mermaid
flowchart TD
    Start(["⏰ Tick mỗi 30 giây"]) --> ReadUptime["Đọc uptime_rate từ config<br/>VD: 0.95 = 95%"]
    ReadUptime --> HourVar["Tính hourlyVariation<br/>sin(hour × π / 12) × 0.05"]
    HourVar --> PhaseVar["Tính serverPhase<br/>sin((hour+phase) × π / 24) × 0.02"]
    PhaseVar --> Calc["effectiveRate = uptimeRate + hourVar + phaseVar<br/>clamp(0, 1)"]
    Calc --> Roll{"🎲 rand() < effectiveRate ?"}
    
    Roll -->|"✅ YES (95%)"| ShouldOn["Server nên ON"]
    Roll -->|"❌ NO (5%)"| ShouldOff["Server nên OFF"]
    
    ShouldOn --> CheckOn{"Đang ON chưa?"}
    CheckOn -->|"Chưa"| OpenPort["🔓 Mở TCP port<br/>net.Listen()"]
    CheckOn -->|"Rồi"| NoOp1["Không làm gì"]
    
    ShouldOff --> CheckOff{"Đang OFF chưa?"}
    CheckOff -->|"Chưa"| ClosePort["🔒 Đóng TCP port<br/>listener.Close()"]
    CheckOff -->|"Rồi"| NoOp2["Không làm gì"]
    
    OpenPort --> Wait30s["Chờ 30 giây"]
    ClosePort --> Wait30s
    NoOp1 --> Wait30s
    NoOp2 --> Wait30s
    Wait30s --> Start
```

---

## Monitor Service: Check Flow mỗi 60 giây

```mermaid
flowchart TD
    Cron["⏰ Cron: mỗi 60 giây"] --> Lock{"Redis Lock?"}
    Lock -->|"❌ Đã khóa"| Skip["Bỏ qua chu kỳ này"]
    Lock -->|"✅ Nhận khóa"| Fetch["SELECT 10.000 servers<br/>từ server_schema"]
    
    Fetch --> FanOut["Fan-out 10.000 jobs<br/>vào channel"]
    FanOut --> Workers["100 Workers xử lý"]
    
    Workers --> TCPDial["net.DialTimeout(tcp,<br/>tcp-simulator:port, 5s)"]
    TCPDial --> Check{"Kết nối thành công?"}
    
    Check -->|"✅ Có"| ResultOn["Status = 'on'<br/>response_time_ms = elapsed"]
    Check -->|"❌ Không"| ResultOff["Status = 'off'<br/>response_time_ms = 0"]
    
    ResultOn --> Compare["So sánh với Redis<br/>server:status:{id}"]
    ResultOff --> Compare
    
    Compare --> HasChanged{"Thay đổi?"}
    HasChanged -->|"✅ Có"| ChangeList["Thêm vào statusChanges[]"]
    HasChanged -->|"❌ Không"| UpdateRedis["SET Redis<br/>server:status:{id} = new"]
    
    ChangeList --> UpdateRedis
    UpdateRedis --> MoreWork{"Còn server?"}
    MoreWork -->|"✅ Còn"| Workers
    MoreWork -->|"❌ Hết"| Batch
    
    Batch["📦 Batch Processing"] --> ES["Bulk Index 10K docs<br/>→ Elasticsearch"]
    ES --> PG["Batch UPDATE status<br/>→ PostgreSQL<br/>(chỉ server thay đổi)"]
    PG --> Kafka["Publish Events<br/>→ Kafka"]
    Kafka --> Release["Giải phóng Redis Lock"]
```

---

## Uptime Calculation

```mermaid
flowchart LR
    subgraph "Uptime 1 Server"
        TotalChecks["🔢 Tổng số lần check<br/>đã thực hiện"] --> Formula
        OnChecks["✅ Số lần ON"] --> Formula
        Formula["📐 Uptime = ON / Total × 100%"] --> Result["📊 VD: 456/480 = 95.0%"]
    end

    subgraph "Uptime Toàn Hệ Thống"
        AllServers["📊 Uptime của từng server"] --> AvgFormula
        AvgFormula["📐 AVG(tất cả Uptime)"] --> AvgResult["📊 VD: 97.85%"]
    end
```
