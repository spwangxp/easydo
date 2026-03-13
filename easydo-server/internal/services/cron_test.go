package services

import (
	"context"
	"testing"
	"time"

	"easydo-server/internal/config"
	"easydo-server/pkg/utils"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setupCronRedis(t *testing.T, serverID string) *miniredis.Miniredis {
	t.Helper()
	config.Init()
	config.Config.Set("server.id", serverID)

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}
	utils.RedisClient = redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() {
		_ = utils.RedisClient.Close()
		mini.Close()
	})
	return mini
}

func TestTryAcquireAgentOfflineLeadership_AllowsSingleLeader(t *testing.T) {
	setupCronRedis(t, "server-a")
	leader := &Cron{}
	follower := &Cron{}
	ctx := context.Background()

	ok, err := leader.tryAcquireAgentOfflineLeadership(ctx)
	if err != nil {
		t.Fatalf("leader acquire failed: %v", err)
	}
	if !ok {
		t.Fatal("expected first leader acquisition to succeed")
	}

	config.Config.Set("server.id", "server-b")
	ok, err = follower.tryAcquireAgentOfflineLeadership(ctx)
	if err != nil {
		t.Fatalf("follower acquire failed: %v", err)
	}
	if ok {
		t.Fatal("expected second server acquisition to fail while lease is held")
	}
}

func TestTryAcquireAgentOfflineLeadership_RenewsExistingLeaderLease(t *testing.T) {
	mini := setupCronRedis(t, "server-a")
	cron := &Cron{}
	ctx := context.Background()

	ok, err := cron.tryAcquireAgentOfflineLeadership(ctx)
	if err != nil {
		t.Fatalf("initial acquire failed: %v", err)
	}
	if !ok {
		t.Fatal("expected initial acquisition to succeed")
	}

	mini.FastForward(20 * time.Second)
	ok, err = cron.tryAcquireAgentOfflineLeadership(ctx)
	if err != nil {
		t.Fatalf("renew acquire failed: %v", err)
	}
	if !ok {
		t.Fatal("expected same leader to renew lease")
	}
	if ttl := mini.TTL(agentOfflineLeaderKey()); ttl <= 0 {
		t.Fatalf("expected positive ttl after renewal, got %v", ttl)
	}
}
