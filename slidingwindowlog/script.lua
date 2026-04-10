-- Sliding-window log rate limiter.
-- Records exact timestamps in a sorted set for a true rolling window.
-- KEYS[1]: sorted set key
-- ARGV[1]: limit
-- ARGV[2]: window (seconds)
-- ARGV[3]: now (seconds)
-- ARGV[4]: unique member identifier
-- Returns: { allowed, remaining, retry_after }

local key = KEYS[1]
local limit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local member = ARGV[4]

local window_start = now - window
valkey.call('ZREMRANGEBYSCORE', key, '-inf', window_start)

local count = valkey.call('ZCARD', key)

local allowed = 0
local remaining = 0
local retry_after = 0

if count < limit then
    valkey.call('ZADD', key, now, member)
    valkey.call('EXPIRE', key, math.ceil(window) + 1)
    count = count + 1
    allowed = 1
    remaining = limit - count
else
    remaining = 0
    local oldest = valkey.call('ZRANGE', key, 0, 0, 'WITHSCORES')
    if #oldest >= 2 then
        local oldest_time = tonumber(oldest[2])
        retry_after = math.ceil(oldest_time + window - now)
        if retry_after < 1 then retry_after = 1 end
    else
        retry_after = math.ceil(window)
        if retry_after < 1 then retry_after = 1 end
    end
end

return { allowed, remaining, retry_after }
