-- Leaky bucket (policing mode) rate limiter.
-- Expects 'now' in seconds to match the leak_rate unit.
-- KEYS[1]: bucket key
-- ARGV[1]: capacity
-- ARGV[2]: leak_rate (requests drained per second)
-- ARGV[3]: now (seconds)
-- Returns: { allowed, remaining, retry_after }

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
local retry_after = 0

if level + 1 <= capacity then
  level = level + 1
  remaining = math.max(0, math.floor(capacity - level))
  allowed = 1
else
  retry_after = math.ceil((level - capacity + 1) / leak_rate)
  if retry_after < 1 then retry_after = 1 end
end

valkey.call('HSET', key, 'level', tostring(level), 'last_leak', tostring(now))
valkey.call('EXPIRE', key, math.ceil(capacity / leak_rate) * 2)

return { allowed, remaining, retry_after }
