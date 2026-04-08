-- Token bucket rate limiter.
-- KEYS[1]: bucket key
-- ARGV[1]: capacity (max tokens)
-- ARGV[2]: refill_rate (tokens per second)
-- ARGV[3]: now (seconds)
-- Returns: { allowed, remaining, retry_after }

local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local refill_rate = tonumber(ARGV[2])
local now = tonumber(ARGV[3])

local data = valkey.call('HGETALL', key)
local tokens = capacity
local last_refill = now

if #data > 0 then
  local fields = {}
  for i = 1, #data, 2 do
    fields[data[i]] = data[i + 1]
  end
  tokens = tonumber(fields['tokens']) or capacity
  last_refill = tonumber(fields['last_refill']) or now
end

local elapsed = now - last_refill
tokens = math.min(capacity, tokens + elapsed * refill_rate)

local allowed = 0
local remaining = math.floor(tokens)
local retry_after = 0

if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
  remaining = math.floor(tokens)
else
  retry_after = math.ceil(1 / refill_rate)
  if retry_after < 1 then retry_after = 1 end
end

valkey.call('HSET', key, 'tokens', tostring(tokens), 'last_refill', tostring(now))
valkey.call('EXPIRE', key, math.ceil(capacity / refill_rate) + 1)

return { allowed, remaining, retry_after }
