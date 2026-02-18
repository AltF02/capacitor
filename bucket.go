package capacitor

import "github.com/valkey-io/valkey-go"

// IMPORTANT: This script expects 'now' in the same time unit as leak_rate
// (if leak_rate is per second, now should be in seconds, not milliseconds).
const luaLeakyBucket = `
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local leak_rate = tonumber(ARGV[2])
local now = tonumber(ARGV[3])

local data = valkey.call('HGETALL', key)
local level = 0
local last_leak = now

if #data > 0 then
  local fields = {}
  for i = 1, #data, 2 do
    fields[data[i]] = data[i + 1]
  end
  level = tonumber(fields['level']) or 0
  last_leak = tonumber(fields['last_leak']) or now
end

local elapsed = now - last_leak
local leaked = elapsed * leak_rate
level = math.max(0, level - leaked)

local allowed = 0
local remaining = math.max(0, math.floor(capacity - level))

if level + 1 <= capacity then
  level = level + 1
  remaining = math.max(0, math.floor(capacity - level))
  allowed = 1
end

valkey.call('HSET', key, 'level', tostring(level), 'last_leak', tostring(now))
valkey.call('EXPIRE', key, math.ceil(capacity / leak_rate) * 2)

return { allowed, remaining }
`

var leakyBucketScript = valkey.NewLuaScript(luaLeakyBucket)
