package hidb

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// --- 基礎 Key-Value 操作 ---

// RedisGet 取得字串值
func (d *Database) RedisGet(ctx context.Context, key string) (string, error) {
	if d.redisDB == nil {
		return "", fmt.Errorf("redis not initialized")
	}
	return d.redisDB.Get(ctx, key).Result()
}

// RedisSet 設定字串值（含 TTL，0 表示不過期）
func (d *Database) RedisSet(ctx context.Context, key, value string, ttl time.Duration) error {
	if d.redisDB == nil {
		return fmt.Errorf("redis not initialized")
	}
	return d.redisDB.Set(ctx, key, value, ttl).Err()
}

// RedisDel 刪除一或多個 key
func (d *Database) RedisDel(ctx context.Context, keys ...string) (int64, error) {
	if d.redisDB == nil {
		return 0, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.Del(ctx, keys...).Result()
}

// RedisExists 檢查 key 是否存在（回傳存在的數量）
func (d *Database) RedisExists(ctx context.Context, keys ...string) (int64, error) {
	if d.redisDB == nil {
		return 0, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.Exists(ctx, keys...).Result()
}

// RedisExpire 設定 key 的 TTL
func (d *Database) RedisExpire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if d.redisDB == nil {
		return false, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.Expire(ctx, key, ttl).Result()
}

// RedisTTL 取得 key 的剩餘 TTL
func (d *Database) RedisTTL(ctx context.Context, key string) (time.Duration, error) {
	if d.redisDB == nil {
		return 0, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.TTL(ctx, key).Result()
}

// --- JSON 序列化操作 ---

// RedisSetJSON 將任意 struct 序列化為 JSON 後存入 Redis
func (d *Database) RedisSetJSON(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	if d.redisDB == nil {
		return fmt.Errorf("redis not initialized")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	return d.redisDB.Set(ctx, key, data, ttl).Err()
}

// RedisGetJSON 從 Redis 取得 JSON 並反序列化到指定 struct
func (d *Database) RedisGetJSON(ctx context.Context, key string, dest interface{}) error {
	if d.redisDB == nil {
		return fmt.Errorf("redis not initialized")
	}
	data, err := d.redisDB.Get(ctx, key).Bytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

// --- Hash 操作 ---

// RedisHSet 設定 hash 欄位
func (d *Database) RedisHSet(ctx context.Context, key string, values ...interface{}) error {
	if d.redisDB == nil {
		return fmt.Errorf("redis not initialized")
	}
	return d.redisDB.HSet(ctx, key, values...).Err()
}

// RedisHGet 取得 hash 欄位值
func (d *Database) RedisHGet(ctx context.Context, key, field string) (string, error) {
	if d.redisDB == nil {
		return "", fmt.Errorf("redis not initialized")
	}
	return d.redisDB.HGet(ctx, key, field).Result()
}

// RedisHGetAll 取得 hash 全部欄位
func (d *Database) RedisHGetAll(ctx context.Context, key string) (map[string]string, error) {
	if d.redisDB == nil {
		return nil, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.HGetAll(ctx, key).Result()
}

// RedisHDel 刪除 hash 欄位
func (d *Database) RedisHDel(ctx context.Context, key string, fields ...string) (int64, error) {
	if d.redisDB == nil {
		return 0, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.HDel(ctx, key, fields...).Result()
}

// --- List 操作 ---

// RedisLPush 從左側推入 list
func (d *Database) RedisLPush(ctx context.Context, key string, values ...interface{}) (int64, error) {
	if d.redisDB == nil {
		return 0, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.LPush(ctx, key, values...).Result()
}

// RedisRPush 從右側推入 list
func (d *Database) RedisRPush(ctx context.Context, key string, values ...interface{}) (int64, error) {
	if d.redisDB == nil {
		return 0, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.RPush(ctx, key, values...).Result()
}

// RedisLPop 從左側取出 list
func (d *Database) RedisLPop(ctx context.Context, key string) (string, error) {
	if d.redisDB == nil {
		return "", fmt.Errorf("redis not initialized")
	}
	return d.redisDB.LPop(ctx, key).Result()
}

// RedisLRange 取得 list 指定範圍
func (d *Database) RedisLRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	if d.redisDB == nil {
		return nil, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.LRange(ctx, key, start, stop).Result()
}

// RedisLLen 取得 list 長度
func (d *Database) RedisLLen(ctx context.Context, key string) (int64, error) {
	if d.redisDB == nil {
		return 0, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.LLen(ctx, key).Result()
}

// --- Set 操作 ---

// RedisSAdd 新增 set 成員
func (d *Database) RedisSAdd(ctx context.Context, key string, members ...interface{}) (int64, error) {
	if d.redisDB == nil {
		return 0, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.SAdd(ctx, key, members...).Result()
}

// RedisSMembers 取得 set 全部成員
func (d *Database) RedisSMembers(ctx context.Context, key string) ([]string, error) {
	if d.redisDB == nil {
		return nil, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.SMembers(ctx, key).Result()
}

// RedisSIsMember 檢查是否為 set 成員
func (d *Database) RedisSIsMember(ctx context.Context, key string, member interface{}) (bool, error) {
	if d.redisDB == nil {
		return false, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.SIsMember(ctx, key, member).Result()
}

// RedisSRem 移除 set 成員
func (d *Database) RedisSRem(ctx context.Context, key string, members ...interface{}) (int64, error) {
	if d.redisDB == nil {
		return 0, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.SRem(ctx, key, members...).Result()
}

// --- Sorted Set 操作 ---

// RedisZAdd 新增 sorted set 成員
func (d *Database) RedisZAdd(ctx context.Context, key string, members ...redis.Z) (int64, error) {
	if d.redisDB == nil {
		return 0, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.ZAdd(ctx, key, members...).Result()
}

// RedisZRangeWithScores 取得 sorted set 指定範圍（含分數）
func (d *Database) RedisZRangeWithScores(ctx context.Context, key string, start, stop int64) ([]redis.Z, error) {
	if d.redisDB == nil {
		return nil, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.ZRangeWithScores(ctx, key, start, stop).Result()
}

// RedisZRem 移除 sorted set 成員
func (d *Database) RedisZRem(ctx context.Context, key string, members ...interface{}) (int64, error) {
	if d.redisDB == nil {
		return 0, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.ZRem(ctx, key, members...).Result()
}

// RedisZScore 取得 sorted set 成員分數
func (d *Database) RedisZScore(ctx context.Context, key string, member string) (float64, error) {
	if d.redisDB == nil {
		return 0, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.ZScore(ctx, key, member).Result()
}

// --- 原子操作 ---

// RedisIncr 原子遞增
func (d *Database) RedisIncr(ctx context.Context, key string) (int64, error) {
	if d.redisDB == nil {
		return 0, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.Incr(ctx, key).Result()
}

// RedisDecr 原子遞減
func (d *Database) RedisDecr(ctx context.Context, key string) (int64, error) {
	if d.redisDB == nil {
		return 0, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.Decr(ctx, key).Result()
}

// RedisIncrBy 原子遞增指定值
func (d *Database) RedisIncrBy(ctx context.Context, key string, value int64) (int64, error) {
	if d.redisDB == nil {
		return 0, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.IncrBy(ctx, key, value).Result()
}

// --- SetNX（分散式鎖基礎） ---

// RedisSetNX 僅在 key 不存在時設定（用於分散式鎖）
func (d *Database) RedisSetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	if d.redisDB == nil {
		return false, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.SetNX(ctx, key, value, ttl).Result()
}

// --- Pub/Sub ---

// RedisPublish 發布訊息到 channel
func (d *Database) RedisPublish(ctx context.Context, channel string, message interface{}) (int64, error) {
	if d.redisDB == nil {
		return 0, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.Publish(ctx, channel, message).Result()
}

// RedisSubscribe 訂閱 channel（回傳 PubSub，呼叫端需 Close）
func (d *Database) RedisSubscribe(ctx context.Context, channels ...string) (*redis.PubSub, error) {
	if d.redisDB == nil {
		return nil, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.Subscribe(ctx, channels...), nil
}

// --- Pipeline（批次操作） ---

// RedisPipeline 建立 pipeline 批次執行多個命令
func (d *Database) RedisPipeline() (redis.Pipeliner, error) {
	if d.redisDB == nil {
		return nil, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.Pipeline(), nil
}

// RedisTxPipeline 建立交易式 pipeline（MULTI/EXEC）
func (d *Database) RedisTxPipeline() (redis.Pipeliner, error) {
	if d.redisDB == nil {
		return nil, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.TxPipeline(), nil
}

// --- Scan（遊標迭代） ---

// RedisScan 使用 SCAN 命令遊標迭代 key（不阻塞伺服器）
func (d *Database) RedisScan(ctx context.Context, cursor uint64, match string, count int64) ([]string, uint64, error) {
	if d.redisDB == nil {
		return nil, 0, fmt.Errorf("redis not initialized")
	}
	return d.redisDB.Scan(ctx, cursor, match, count).Result()
}

// --- 輔助方法 ---

// RedisIsNil 檢查 error 是否為 redis.Nil（key 不存在）
func RedisIsNil(err error) bool {
	return err == redis.Nil
}
