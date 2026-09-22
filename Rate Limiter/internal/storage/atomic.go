package storage
import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

// AtomicCounterStorage provides distributed, single-operation counter updates.
type AtomicCounterStorage interface {
	Storage
	CheckAndSet(key string, limit int, windowStart int64, windowSeconds int64) (bool, int, error)
	CheckAndSetCount(key string, limit int, windowStart int64, windowSeconds int64) (bool, int, error)
	CheckAndSetTokenBucket(key string, limit int, windowSeconds int64) (bool, int, error)
	CheckAndSetLeakyBucket(key string, limit int, windowSeconds int64) (bool, int, error)
}

func atomicFixedWindow(client redis.UniversalClient, ctx context.Context, key string, limit int, windowStart, windowSeconds int64) (bool, int, error) {
	script := `
local data = redis.call('GET', KEYS[1])
local record
if data then record = cjson.decode(data) else record = {WindowStart=tonumber(ARGV[2]), Count=0} end
if tonumber(record.WindowStart) ~= tonumber(ARGV[2]) then record.WindowStart=tonumber(ARGV[2]); record.Count=0 end
if tonumber(record.Count) >= tonumber(ARGV[1]) then return {0, tonumber(record.Count)} end
record.Count=tonumber(record.Count)+1
redis.call('SET', KEYS[1], cjson.encode(record), 'EX', tonumber(ARGV[3]))
return {1, record.Count}`
	result, err := client.Eval(ctx, script, []string{key}, limit, windowStart, windowSeconds).Result()
	return parseAtomicResultAtomic(result, "fixed-window", err)
}

func atomicSlidingCounter(client redis.UniversalClient, ctx context.Context, key string, limit int, windowStart, windowSeconds int64) (bool, int, error) {
	now := time.Now().Unix()
	script := `
local data = redis.call('GET', KEYS[1])
local current = 0
if data then
local record=cjson.decode(data)
local storedStart=tonumber(record.WindowStart)
local storedCount=tonumber(record.Count) or 0
if storedStart == tonumber(ARGV[2]) then current=storedCount
elseif storedStart == tonumber(ARGV[2])-tonumber(ARGV[3]) then
local ratio=(tonumber(ARGV[4])-storedStart)/tonumber(ARGV[3])
if ratio<0 then ratio=0 end
if ratio>1 then ratio=1 end
current=math.floor(storedCount*(1-ratio))
end
end
if current >= tonumber(ARGV[1]) then return {0,current} end
current=current+1
redis.call('SET', KEYS[1], cjson.encode({WindowStart=tonumber(ARGV[2]),Count=current}), 'EX', tonumber(ARGV[3])*2)
return {1,current}`
	result, err := client.Eval(ctx, script, []string{key}, limit, windowStart, windowSeconds, now).Result()
	return parseAtomicResultAtomic(result, "sliding-window", err)
}

func parseAtomicResultAtomic(result interface{}, name string, err error) (bool, int, error) {
	if err != nil {
	return false, 0, err
	}
	values, ok := result.([]interface{})
	if !ok || len(values) < 2 {
	return false, 0, fmt.Errorf("unexpected Redis %s response", name)
	}
	allowed, okAllowed := values[0].(int64)
	count, okCount := values[1].(int64)
	if !okAllowed || !okCount {
	return false, 0, fmt.Errorf("invalid Redis %s response", name)
	}
	return allowed == 1, int(count), nil
}

func atomicBucket(client redis.UniversalClient, ctx context.Context, key string, limit int, windowSeconds int64, leaky bool) (bool, int, error) {
	now := time.Now().Unix()
	mode := int64(0)
	if leaky {
	mode = 1
	}
	script := `
local data = redis.call('GET', KEYS[1])
local level = tonumber(ARGV[1])
local now = tonumber(ARGV[2])
local leaky = tonumber(ARGV[5])
local capacity = tonumber(ARGV[3])
local window = tonumber(ARGV[4])
if not data and leaky == 0 then level = capacity end
if data then
local record = cjson.decode(data)
level = tonumber(record.Count) or 0
local elapsed = now - tonumber(record.WindowStart)
if elapsed < 0 then elapsed = 0 end
if leaky == 0 then
level = math.min(capacity, level + math.floor((capacity / window) * elapsed))
else
level = math.max(0, level - math.floor((capacity / window) * elapsed))
end
end
if level < 1 and leaky == 0 then return {0, 0} end
if leaky == 1 and level + 1 > capacity then return {0, level} end
if leaky == 0 then level = level - 1 else level = level + 1 end
redis.call('SET', KEYS[1], cjson.encode({WindowStart=now, Count=level}), 'EX', window * 2)
return {1, level}`
	result, err := client.Eval(ctx, script, []string{key}, 0, now, limit, windowSeconds, mode).Result()
	return parseAtomicResultAtomic(result, "bucket", err)
}

func (r *RedisStorage) CheckAndSetTokenBucket(key string, limit int, windowSeconds int64) (bool, int, error) {
	return atomicBucket(r.client, r.ctx, key, limit, windowSeconds, false)
}

func (r *RedisStorage) CheckAndSetLeakyBucket(key string, limit int, windowSeconds int64) (bool, int, error) {
	return atomicBucket(r.client, r.ctx, key, limit, windowSeconds, true)
}
