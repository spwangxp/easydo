package services

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"easydo-server/internal/config"
	"easydo-server/internal/models"
	"easydo-server/pkg/utils"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
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

func TestComputeNextScheduleTime_UsesStoredTimezone(t *testing.T) {
	base := time.Date(2026, time.March, 20, 0, 0, 0, 0, time.UTC)

	next, err := ComputeNextScheduleTime("0 9 * * *", "Asia/Shanghai", base)
	if err != nil {
		t.Fatalf("compute next schedule time failed: %v", err)
	}

	expected := time.Date(2026, time.March, 20, 1, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Fatalf("next=%s, want %s", next.Format(time.RFC3339), expected.Format(time.RFC3339))
	}
}

func TestComputeNextScheduleTime_RejectsInvalidCron(t *testing.T) {
	_, err := ComputeNextScheduleTime("invalid cron", "UTC", time.Now().UTC())
	if err == nil {
		t.Fatalf("expected invalid cron error")
	}
}

func openCronNotificationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "_", " ", "_", ":", "_").Replace(t.Name()))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite db failed: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db failed: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	if err := db.AutoMigrate(&models.User{}, &models.Workspace{}, &models.WorkspaceMember{}, &models.Agent{}, &models.NotificationEvent{}, &models.NotificationAudience{}, &models.Notification{}, &models.InboxMessage{}, &models.NotificationDelivery{}, &models.NotificationPreference{}); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}
	return db
}

func TestCheckAgentOfflineEmitsCanonicalNotifications(t *testing.T) {
	db := openCronNotificationTestDB(t)
	owner := models.User{Username: "cron-owner", Role: "user", Status: "active", Email: "cron-owner@example.com"}
	if err := owner.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: "cron-ws", Slug: "cron-ws", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: owner.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}).Error; err != nil {
		t.Fatalf("create workspace member failed: %v", err)
	}
	agent := models.Agent{Name: "cron-agent", Host: "127.0.0.1", Port: 22, Status: models.AgentStatusOnline, RegistrationStatus: models.AgentRegistrationStatusApproved, WorkspaceID: workspace.ID, ScopeType: models.AgentScopeWorkspace, LastHeartAt: time.Now().Add(-time.Minute).Unix(), HeartbeatInterval: 10}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	cron := &Cron{DB: db}
	cron.checkAgentOffline()

	var eventCount int64
	if err := db.Model(&models.NotificationEvent{}).Where("family = ? AND event_type = ?", "agent.lifecycle", "agent.offline").Count(&eventCount).Error; err != nil {
		t.Fatalf("count notification events failed: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("agent offline notification event count=%d, want 1", eventCount)
	}
	var inboxCount int64
	if err := db.Model(&models.InboxMessage{}).Where("recipient_id = ?", owner.ID).Count(&inboxCount).Error; err != nil {
		t.Fatalf("count inbox messages failed: %v", err)
	}
	if inboxCount != 1 {
		t.Fatalf("agent offline inbox count=%d, want 1", inboxCount)
	}
}
