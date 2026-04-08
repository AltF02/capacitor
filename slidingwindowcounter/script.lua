-- Sliding-window counter rate limiter.
-- Uses two fixed-window counters with a weighted average.
-- KEYS use hash tags ({baseKey}) for Redis Cluster compatibility.
-- KEYS[1]: previous window key
-- KEYS[2]: current window key
-- ARGV[1]: limit
-- ARGV[2]: window (seconds)
-- ARGV[3]: now (seconds)
-- Returns: { allowed, remaining, retry_after }

local prev_key = KEYS[1]
local curr_key = KEYS[2]
local limit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local now = tonumber(ARGV[3])

local window_num = math.floor(now / window)
local elapsed = (now / window) - window_num

local prev_count = tonumber(valkey.call('GET', prev_key)) or 0
local curr_count = tonumber(valkey.call('GET', curr_key)) or 0

local estimated = math.floor(prev_count * (1 - elapsed) + curr_count)

local allowed = 0
local remaining = 0
local retry_after = 0

if estimated < limit then
    curr_count = valkey.call('INCR', curr_key)
    if curr_count == 1 then
        valkey.call('EXPIRE', curr_key, window * 2)
    end
    remaining = math.max(0, limit - curr_count)
    allowed = 1
else
    remaining = 0
    local ttl = valkey.call('PTTL', curr_key)
    retry_after = math.ceil(ttl / 1000)
    if retry_after < 1 then retry_after = 1 end
end

return { allowed, remaining, retry_after }
