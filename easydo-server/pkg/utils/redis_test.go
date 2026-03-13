package utils

import (
	"context"
	"sort"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestGetActiveLogKeys_ReturnsAllKeysWithoutBlockingPatternAssumptions(t *testing.T) {
	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}
	defer mini.Close()

	previousRedis := RedisClient
	RedisClient = redis.NewClient(&redis.Options{Addr: mini.Addr()})
	defer func() {
		_ = RedisClient.Close()
		RedisClient = previousRedis
	}()

	ctx := context.Background()
	if err := RedisClient.Set(ctx, LogBufferPrefix+"1:2", "a", 0).Err(); err != nil {
		t.Fatalf("set first log key failed: %v", err)
	}
	if err := RedisClient.Set(ctx, LogBufferPrefix+"3:4", "b", 0).Err(); err != nil {
		t.Fatalf("set second log key failed: %v", err)
	}
	if err := RedisClient.Set(ctx, "other:key", "c", 0).Err(); err != nil {
		t.Fatalf("set non log key failed: %v", err)
	}

	keys, err := GetActiveLogKeys()
	if err != nil {
		t.Fatalf("get active log keys failed: %v", err)
	}
	sort.Strings(keys)

	if len(keys) != 2 {
		t.Fatalf("expected 2 log keys, got %d (%v)", len(keys), keys)
	}
	if keys[0] != LogBufferPrefix+"1:2" || keys[1] != LogBufferPrefix+"3:4" {
		t.Fatalf("unexpected keys: %v", keys)
	}
}
