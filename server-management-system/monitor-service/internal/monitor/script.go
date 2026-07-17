package monitor

import "github.com/redis/go-redis/v9"

// Return codes from statusScript.
const (
	statusSkippedStale = 0 // an older or replayed round; nothing written
	statusChanged      = 1 // written, and status.changed published
	statusUnchanged    = 2 // written, same status as before
)

// statusScript writes current status and publishes status.changed in one step,
// so there is no window where Redis and the stream disagree.
//
// KEYS[1] monitor:status:{server_id}   KEYS[2] stream:monitor.status
// ARGV: server_id, status, checked_at (RFC3339), latency_ms, round_id
var statusScript = redis.NewScript(`
local status_key = KEYS[1]
local stream_key = KEYS[2]
local server_id  = ARGV[1]
local new_status = ARGV[2]
local checked_at = ARGV[3]
local latency    = ARGV[4]
local round_id   = tonumber(ARGV[5])

local old_status = redis.call('HGET', status_key, 'status')
-- HGET yields false when the field is absent, so a first check reads -1.
local old_round = tonumber(redis.call('HGET', status_key, 'round_id') or '-1')

if round_id <= old_round then
  return 0
end

redis.call('HSET', status_key,
  'status', new_status,
  'last_checked_at', checked_at,
  'latency_ms', latency,
  'round_id', round_id)

-- old_status is false on a first check: UNKNOWN -> ON/OFF is a real transition.
if old_status == false or old_status ~= new_status then
  redis.call('XADD', stream_key, 'MAXLEN', '~', '100000', '*',
    'schema_version', '1',
    'event_type', 'status.changed',
    'event_id', server_id .. ':' .. round_id,
    'server_id', server_id,
    'status', new_status,
    'changed_at', checked_at,
    'checked_at', checked_at,
    'status_version', tostring(round_id))
  return 1
end

return 2
`)
