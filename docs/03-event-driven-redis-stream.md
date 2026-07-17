# 03 — Event-driven với Redis Stream

> Thay thế `03-event-driven-kafka.md`. Hệ thống **không còn dùng Kafka**.

---

## 1. Vì sao bỏ Kafka

Kafka giải quyết bài toán: nhiều producer, nhiều consumer group độc lập, replay dài
hạn, throughput hàng trăm nghìn msg/s, ordering theo partition.

Bài toán thực tế ở đây:

| Yếu tố | Con số thật |
|---|---|
| Loại event | **1** (`status.changed`) |
| Producer | **1** (monitor-service) |
| Consumer group | **1** (`server-svc`) |
| Tần suất | Vài chục event/ngày (chỉ khi status **đổi**) |

Không có yếu tố nào cần tới Kafka. Đổi lại phải nuôi broker + KRaft controller,
~1GB RAM, và thêm một thành phần nữa có thể chết. Redis thì **đã có sẵn** trong hệ
thống cho cache và target projection.

Redis Stream cung cấp đúng những gì cần: append-only log, consumer group, ACK,
pending list, `XAUTOCLAIM` để tiếp quản việc của consumer chết.

---

## 2. Event `status.changed`

```text
stream:monitor.status   (MAXLEN ~ 100000)

schema_version  1
event_type      status.changed
event_id        {server_id}:{round_id}
server_id       SRV-00002
status          ON | OFF          ← UNKNOWN không bao giờ là transition hợp lệ
changed_at      RFC3339
checked_at      RFC3339
status_version  {round_id}
```

> **Contract:** `changed_at` phải là **RFC3339**. Lệch format → consumer coi mọi event
> là malformed, **ACK rồi vứt đi im lặng**, chỉ để lại log Error.

---

## 3. Lua script — vì sao phải atomic

Nếu tách HSET và XADD thành hai lệnh, sẽ có một khoảnh khắc Redis status nói một
đằng còn stream nói một nẻo. Process chết đúng lúc đó → hai nguồn lệch nhau vĩnh viễn.

```lua
local old_status = redis.call('HGET', status_key, 'status')
local old_round  = tonumber(redis.call('HGET', status_key, 'round_id') or '-1')

if round_id <= old_round then
  return 0                          -- event cũ hoặc replay: không làm gì
end

redis.call('HSET', status_key,
  'status', new_status, 'last_checked_at', checked_at,
  'latency_ms', latency, 'round_id', round_id)

if old_status == false or old_status ~= new_status then
  redis.call('XADD', stream_key, 'MAXLEN', '~', '100000', '*', ...)
  return 1                          -- có transition
end

return 2                            -- status không đổi: KHÔNG phát event
```

### `old_status == false`, không phải `== nil`

Redis Lua trả về **`false`** cho field không tồn tại, **không phải `nil`**. Viết
`old_status == nil` thì điều kiện vĩnh viễn sai → event đầu tiên (`UNKNOWN → ON/OFF`)
không bao giờ phát → server mới **kẹt `UNKNOWN` mãi mãi**.

### Ba mã trả về

| Mã | Ý nghĩa |
|---|---|
| `0` | Round cũ hoặc replay — không ghi gì |
| `1` | Đã ghi, và đã phát `status.changed` |
| `2` | Đã ghi, status không đổi — **không** phát event |

Mã `2` là lý do cache sống: 10.000 ping/phút hầu như đều trả `2`.

---

## 4. Consumer phía server-service

```text
Group: server-svc      Consumer: {hostname}
XREADGROUP  ">"  COUNT 100  BLOCK 2s
XAUTOCLAIM  mỗi 30s, MinIdle 60s   ← tiếp quản việc của consumer đã chết
```

### Version guard

```sql
UPDATE servers SET status=?, status_changed_at=?, status_version=?, last_status_event_id=?
WHERE server_id=? AND status_version < ?
```

`RowsAffected = 0` nghĩa là event cũ, hoặc server đã bị xóa → vẫn ACK, không bump
cache version.

### Xử lý lỗi

| Tình huống | Hành động | Lý do |
|---|---|---|
| Event không parse được | **ACK rồi bỏ** | Nếu không, nó redeliver vĩnh viễn |
| DB lỗi | **Không ACK** | Để pending, consumer khác `XAUTOCLAIM` |
| Event cũ/trùng | ACK, không bump | Version guard đã lo |

### Phục hồi khi mất consumer group

Redis restart không persistence, `FLUSHDB`, hoặc `allkeys-lru` evict key stream →
`XREADGROUP` trả `NOGROUP` mãi mãi trong khi monitor vẫn ghi. Nếu chỉ tạo group một
lần lúc boot thì **status trong PostgreSQL đứng im vĩnh viễn**, và chỉ có log báo.

Consumer tự phát hiện `NOGROUP` và tạo lại group (~4s):

| Thời điểm | Vị trí tạo group | Lý do |
|---|---|---|
| Boot lần đầu | `$` | Không replay lịch sử chẳng ai chờ |
| Phục hồi sau mất group | **`0`** | Monitoring **chỉ** phát event khi status đổi. Bỏ qua event cũ sẽ để server kẹt trạng thái sai đến tận lần đổi tiếp theo. Replay an toàn nhờ version guard. |

---

## 5. Bump cache version

```text
Consumer apply cả batch
  → có ít nhất 1 row thực sự đổi?  → INCR server:list:version  (một lần cho cả batch)
  → không row nào đổi?             → không bump
```

Đây là chỗ luận điểm §4 của [01](./01-architecture-overview.md) thành hiện thực:
qua trọn một round 10.000 server, `list:version` **đứng nguyên**, trong khi
`last_checked_at` vẫn nhích và ES vẫn nhận thêm document.
