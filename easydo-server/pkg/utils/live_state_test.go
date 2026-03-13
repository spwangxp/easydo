package utils

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestLiveTaskState_RoundTripAndGraceTTL(t *testing.T) {
	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}
	defer mini.Close()

	previousRedis := RedisClient
	RedisClient = redis.NewClient(&redis.Options{Addr: mini.Addr()})
	defer func() {
		if RedisClient != nil {
			_ = RedisClient.Close()
		}
		RedisClient = previousRedis
	}()

	ctx := context.Background()
	state := LiveTaskState{
		TaskID:         11,
		RunID:          22,
		Status:         "running",
		Seq:            1,
		OwnerServerID:  "server-a",
		AgentSessionID: "session-a",
	}
	if err := SaveLiveTaskState(ctx, state, false); err != nil {
		t.Fatalf("save live task state failed: %v", err)
	}
	stored, err := GetLiveTaskState(ctx, state.TaskID)
	if err != nil {
		t.Fatalf("get live task state failed: %v", err)
	}
	if stored == nil || stored.Status != "running" || stored.Seq != 1 {
		t.Fatalf("unexpected live task state: %+v", stored)
	}
	if ttl := mini.TTL(liveTaskStateKey(state.TaskID)); ttl <= 0 {
		t.Fatalf("expected positive live ttl, got %v", ttl)
	}

	state.Status = "execute_success"
	state.Seq = 2
	state.IsTerminal = true
	if err := SaveLiveTaskState(ctx, state, true); err != nil {
		t.Fatalf("save terminal task state failed: %v", err)
	}
	if ttl := mini.TTL(liveTaskStateKey(state.TaskID)); ttl <= 0 {
		t.Fatalf("expected positive terminal grace ttl, got %v", ttl)
	}
	mini.FastForward(liveStateTerminalGraceTTL() + time.Second)
	stored, err = GetLiveTaskState(ctx, state.TaskID)
	if err != nil {
		t.Fatalf("get terminal task state after expiry failed: %v", err)
	}
	if stored != nil {
		t.Fatalf("expected terminal live task state to expire, got %+v", stored)
	}
}

func TestLiveRunState_RoundTrip(t *testing.T) {
	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}
	defer mini.Close()

	previousRedis := RedisClient
	RedisClient = redis.NewClient(&redis.Options{Addr: mini.Addr()})
	defer func() {
		if RedisClient != nil {
			_ = RedisClient.Close()
		}
		RedisClient = previousRedis
	}()

	ctx := context.Background()
	state := LiveRunState{RunID: 33, Status: "running", Seq: 4, UpdatedAt: time.Now().Unix()}
	if err := SaveLiveRunState(ctx, state, false); err != nil {
		t.Fatalf("save live run state failed: %v", err)
	}
	stored, err := GetLiveRunState(ctx, state.RunID)
	if err != nil {
		t.Fatalf("get live run state failed: %v", err)
	}
	if stored == nil || stored.Status != "running" || stored.Seq != 4 {
		t.Fatalf("unexpected live run state: %+v", stored)
	}
}
