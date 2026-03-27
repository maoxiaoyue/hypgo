package hidb

import (
	"testing"

	"github.com/redis/go-redis/v9"
)

// TestRedisNilGuard 驗證所有 Redis 方法在 redisDB 為 nil 時不 panic
func TestRedisNilGuard(t *testing.T) {
	db := &Database{} // redisDB 為 nil

	ctx := t.Context()

	if _, err := db.RedisGet(ctx, "key"); err == nil {
		t.Error("RedisGet should error when redis not initialized")
	}
	if err := db.RedisSet(ctx, "key", "val", 0); err == nil {
		t.Error("RedisSet should error")
	}
	if _, err := db.RedisDel(ctx, "key"); err == nil {
		t.Error("RedisDel should error")
	}
	if _, err := db.RedisExists(ctx, "key"); err == nil {
		t.Error("RedisExists should error")
	}
	if _, err := db.RedisExpire(ctx, "key", 0); err == nil {
		t.Error("RedisExpire should error")
	}
	if _, err := db.RedisTTL(ctx, "key"); err == nil {
		t.Error("RedisTTL should error")
	}
	if err := db.RedisSetJSON(ctx, "key", "val", 0); err == nil {
		t.Error("RedisSetJSON should error")
	}
	if err := db.RedisGetJSON(ctx, "key", nil); err == nil {
		t.Error("RedisGetJSON should error")
	}
	if err := db.RedisHSet(ctx, "key", "f", "v"); err == nil {
		t.Error("RedisHSet should error")
	}
	if _, err := db.RedisHGet(ctx, "key", "f"); err == nil {
		t.Error("RedisHGet should error")
	}
	if _, err := db.RedisHGetAll(ctx, "key"); err == nil {
		t.Error("RedisHGetAll should error")
	}
	if _, err := db.RedisHDel(ctx, "key", "f"); err == nil {
		t.Error("RedisHDel should error")
	}
	if _, err := db.RedisLPush(ctx, "key", "v"); err == nil {
		t.Error("RedisLPush should error")
	}
	if _, err := db.RedisRPush(ctx, "key", "v"); err == nil {
		t.Error("RedisRPush should error")
	}
	if _, err := db.RedisLPop(ctx, "key"); err == nil {
		t.Error("RedisLPop should error")
	}
	if _, err := db.RedisLRange(ctx, "key", 0, -1); err == nil {
		t.Error("RedisLRange should error")
	}
	if _, err := db.RedisLLen(ctx, "key"); err == nil {
		t.Error("RedisLLen should error")
	}
	if _, err := db.RedisSAdd(ctx, "key", "v"); err == nil {
		t.Error("RedisSAdd should error")
	}
	if _, err := db.RedisSMembers(ctx, "key"); err == nil {
		t.Error("RedisSMembers should error")
	}
	if _, err := db.RedisSIsMember(ctx, "key", "v"); err == nil {
		t.Error("RedisSIsMember should error")
	}
	if _, err := db.RedisSRem(ctx, "key", "v"); err == nil {
		t.Error("RedisSRem should error")
	}
	if _, err := db.RedisZAdd(ctx, "key", redis.Z{Score: 1, Member: "m"}); err == nil {
		t.Error("RedisZAdd should error")
	}
	if _, err := db.RedisZRangeWithScores(ctx, "key", 0, -1); err == nil {
		t.Error("RedisZRangeWithScores should error")
	}
	if _, err := db.RedisZRem(ctx, "key", "m"); err == nil {
		t.Error("RedisZRem should error")
	}
	if _, err := db.RedisZScore(ctx, "key", "m"); err == nil {
		t.Error("RedisZScore should error")
	}
	if _, err := db.RedisIncr(ctx, "key"); err == nil {
		t.Error("RedisIncr should error")
	}
	if _, err := db.RedisDecr(ctx, "key"); err == nil {
		t.Error("RedisDecr should error")
	}
	if _, err := db.RedisIncrBy(ctx, "key", 1); err == nil {
		t.Error("RedisIncrBy should error")
	}
	if _, err := db.RedisSetNX(ctx, "key", "val", 0); err == nil {
		t.Error("RedisSetNX should error")
	}
	if _, err := db.RedisPublish(ctx, "ch", "msg"); err == nil {
		t.Error("RedisPublish should error")
	}
	if _, err := db.RedisSubscribe(ctx, "ch"); err == nil {
		t.Error("RedisSubscribe should error")
	}
	if _, err := db.RedisPipeline(); err == nil {
		t.Error("RedisPipeline should error")
	}
	if _, err := db.RedisTxPipeline(); err == nil {
		t.Error("RedisTxPipeline should error")
	}
	if _, _, err := db.RedisScan(ctx, 0, "*", 10); err == nil {
		t.Error("RedisScan should error")
	}
}

// TestRedisIsNil 驗證 RedisIsNil 工具函式
func TestRedisIsNil(t *testing.T) {
	if !RedisIsNil(redis.Nil) {
		t.Error("RedisIsNil(redis.Nil) should return true")
	}
	if RedisIsNil(nil) {
		t.Error("RedisIsNil(nil) should return false")
	}
}
