-- Fixed-window counter rate limiter.
-- KEYS[1]: window counter key
-- ARGV[1]: limit
-- ARGV[2]: window (seconds)
-- Returns: { allowed, remaining, retry_after }

local key = KEYS[1]
local limit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])

local count = valkey.call('INCR', key)
if count == 1 then
    valkey.call('PEXPIRE', key, window * 1000)
end

local ttl = valkey.call('PTTL', key)
local allowed = 0
local remaining = 0
local retry_after = 0

if count <= limit then
    allowed = 1
    remaining = limit - count
else
    remaining = 0
    retry_after = math.ceil(ttl / 1000)
    if retry_after < 1 then retry_after = 1 end
end

return { allowed, remaining, retry_after }
