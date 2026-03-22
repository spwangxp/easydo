package services

import (
	"context"
	"easydo-server/internal/config"
	"easydo-server/internal/models"
	internalnotifications "easydo-server/internal/notifications"
	"easydo-server/pkg/utils"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

var (
	CronService *Cron
	once        sync.Once
)

type Cron struct {
	DB                    *gorm.DB
	stopChan              chan struct{}
	agentCheckTicker      *time.Ticker
	notificationTicker    *time.Ticker
	pipelineTriggerTicker *time.Ticker
}

const agentOfflineLeaderLockKey = "easydo:cron:agent-offline:leader"
const notificationDeliveryLeaderLockKey = "easydo:cron:notification-delivery:leader"
const pipelineTriggerLeaderLockKey = "easydo:cron:pipeline-trigger:leader"

type PipelineTriggerRunner interface {
	EvaluateScheduledPipelineTriggers(now time.Time) int
}

func agentOfflineLeaderKey() string {
	return agentOfflineLeaderLockKey
}

func agentOfflineLeaderTTL() time.Duration {
	return 30 * time.Second
}

func pipelineTriggerLeaderKey() string {
	return pipelineTriggerLeaderLockKey
}

func pipelineTriggerLeaderTTL() time.Duration {
	return 30 * time.Second
}

func notificationDeliveryLeaderKey() string {
	return notificationDeliveryLeaderLockKey
}

func notificationDeliveryLeaderTTL() time.Duration {
	return 30 * time.Second
}

func GetCronService(db *gorm.DB) *Cron {
	once.Do(func() {
		CronService = &Cron{
			DB:       db,
			stopChan: make(chan struct{}),
		}
	})
	return CronService
}

// StartAgentOfflineChecker 启动Agent离线检测
func (c *Cron) StartAgentOfflineChecker() {
	c.agentCheckTicker = time.NewTicker(10 * time.Second)

	go func() {
		for {
			select {
			case <-c.agentCheckTicker.C:
				ok, err := c.tryAcquireAgentOfflineLeadership(context.Background())
				if err == nil && ok {
					c.checkAgentOffline()
				}
			case <-c.stopChan:
				return
			}
		}
	}()
}

func (c *Cron) StartPipelineTriggerEvaluator(runner PipelineTriggerRunner) {
	if runner == nil {
		return
	}
	c.pipelineTriggerTicker = time.NewTicker(10 * time.Second)
	go func() {
		for {
			select {
			case <-c.pipelineTriggerTicker.C:
				ok, err := c.tryAcquirePipelineTriggerLeadership(context.Background())
				if err == nil && ok {
					runner.EvaluateScheduledPipelineTriggers(time.Now().UTC())
				}
			case <-c.stopChan:
				return
			}
		}
	}()
}

func (c *Cron) StartNotificationDeliveryProcessor() {
	c.notificationTicker = time.NewTicker(5 * time.Second)
	go func() {
		for {
			select {
			case <-c.notificationTicker.C:
				ok, err := c.tryAcquireNotificationDeliveryLeadership(context.Background())
				if err == nil && ok {
					_, _ = internalnotifications.DispatchPendingEmailDeliveries(c.DB, time.Now().UTC(), 50)
				}
			case <-c.stopChan:
				return
			}
		}
	}()
}

func (c *Cron) tryAcquireAgentOfflineLeadership(ctx context.Context) (bool, error) {
	if utils.RedisClient == nil {
		return false, fmt.Errorf("redis client is not initialized")
	}
	owner := config.Config.GetString("server.id")
	if owner == "" {
		return false, fmt.Errorf("server.id is required for cron leadership")
	}
	key := agentOfflineLeaderKey()
	ttl := agentOfflineLeaderTTL()

	ok, err := utils.RedisClient.SetNX(ctx, key, owner, ttl).Result()
	if err != nil {
		return false, err
	}
	if ok {
		return true, nil
	}

	currentOwner, err := utils.RedisClient.Get(ctx, key).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if currentOwner != owner {
		return false, nil
	}
	if err := utils.RedisClient.Expire(ctx, key, ttl).Err(); err != nil {
		return false, err
	}
	return true, nil
}

func (c *Cron) tryAcquirePipelineTriggerLeadership(ctx context.Context) (bool, error) {
	if utils.RedisClient == nil {
		return false, fmt.Errorf("redis client is not initialized")
	}
	owner := config.Config.GetString("server.id")
	if owner == "" {
		return false, fmt.Errorf("server.id is required for cron leadership")
	}
	key := pipelineTriggerLeaderKey()
	ttl := pipelineTriggerLeaderTTL()
	ok, err := utils.RedisClient.SetNX(ctx, key, owner, ttl).Result()
	if err != nil {
		return false, err
	}
	if ok {
		return true, nil
	}
	currentOwner, err := utils.RedisClient.Get(ctx, key).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if currentOwner != owner {
		return false, nil
	}
	if err := utils.RedisClient.Expire(ctx, key, ttl).Err(); err != nil {
		return false, err
	}
	return true, nil
}

func (c *Cron) tryAcquireNotificationDeliveryLeadership(ctx context.Context) (bool, error) {
	if utils.RedisClient == nil {
		return false, fmt.Errorf("redis client is not initialized")
	}
	owner := config.Config.GetString("server.id")
	if owner == "" {
		return false, fmt.Errorf("server.id is required for cron leadership")
	}
	key := notificationDeliveryLeaderKey()
	ttl := notificationDeliveryLeaderTTL()
	ok, err := utils.RedisClient.SetNX(ctx, key, owner, ttl).Result()
	if err != nil {
		return false, err
	}
	if ok {
		return true, nil
	}
	currentOwner, err := utils.RedisClient.Get(ctx, key).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if currentOwner != owner {
		return false, nil
	}
	if err := utils.RedisClient.Expire(ctx, key, ttl).Err(); err != nil {
		return false, err
	}
	return true, nil
}

func (c *Cron) checkAgentOffline() {
	// 获取所有在线的agent
	var onlineAgents []models.Agent
	c.DB.Model(&models.Agent{}).
		Where("status = ? AND registration_status = ?", models.AgentStatusOnline, models.AgentRegistrationStatusApproved).
		Find(&onlineAgents)

	for _, agent := range onlineAgents {
		// 计算超时阈值：3个心跳周期
		// heartbeat_interval默认10秒，3个周期=30秒
		timeoutSeconds := int64(agent.HeartbeatInterval * 3)
		if timeoutSeconds < 30 {
			timeoutSeconds = 30 // 最小30秒
		}

		offlineThreshold := time.Now().Unix() - timeoutSeconds

		// 如果超过3个心跳周期没有收到心跳，标记为离线
		if agent.LastHeartAt < offlineThreshold {
			c.DB.Model(&agent).Updates(map[string]interface{}{
				"status":               models.AgentStatusOffline,
				"consecutive_success":  0,
				"consecutive_failures": 3,
			})

			messageContent := fmt.Sprintf("执行器 [%s] 已离线（超过 %d 秒未收到心跳）", agent.Name, timeoutSeconds)
			if agent.ScopeType == models.AgentScopeWorkspace && agent.WorkspaceID > 0 {
				var members []models.WorkspaceMember
				c.DB.Where("workspace_id = ? AND status = ?", agent.WorkspaceID, models.WorkspaceMemberStatusActive).Find(&members)
				userRecipients := make([]uint64, 0, len(members))
				for _, member := range members {
					userRecipients = append(userRecipients, member.UserID)
				}
				_, _ = internalnotifications.Emit(c.DB, internalnotifications.EventInput{
					WorkspaceID:      agent.WorkspaceID,
					Family:           internalnotifications.FamilyAgentLifecycle,
					EventType:        internalnotifications.EventTypeAgentOffline,
					ResourceType:     models.NotificationResourceTypeAgent,
					ResourceID:       agent.ID,
					ActorType:        "system",
					Title:            "Agent离线告警",
					Content:          messageContent,
					IdempotencyKey:   fmt.Sprintf("agent-offline:%d:%d", agent.ID, offlineThreshold),
					PermissionPolicy: internalnotifications.PermissionPolicyWorkspaceMember,
					Channels:         []string{models.NotificationChannelInApp, models.NotificationChannelEmail},
					UserRecipients:   userRecipients,
				})
			} else {
				var admins []models.User
				c.DB.Where("role = ?", "admin").Find(&admins)
				userRecipients := make([]uint64, 0, len(admins))
				for _, admin := range admins {
					userRecipients = append(userRecipients, admin.ID)
				}
				_, _ = internalnotifications.Emit(c.DB, internalnotifications.EventInput{
					Family:           internalnotifications.FamilyAgentLifecycle,
					EventType:        internalnotifications.EventTypeAgentOffline,
					ResourceType:     models.NotificationResourceTypeAgent,
					ResourceID:       agent.ID,
					ActorType:        "system",
					Title:            "Agent离线告警",
					Content:          messageContent,
					IdempotencyKey:   fmt.Sprintf("agent-offline:%d:%d", agent.ID, offlineThreshold),
					PermissionPolicy: internalnotifications.PermissionPolicyPlatformAdmin,
					Channels:         []string{models.NotificationChannelInApp, models.NotificationChannelEmail},
					UserRecipients:   userRecipients,
				})
			}
		}
	}
}

func (c *Cron) StopAgentOfflineChecker() {
	if c.agentCheckTicker != nil {
		c.agentCheckTicker.Stop()
	}
	if c.notificationTicker != nil {
		c.notificationTicker.Stop()
	}
	if c.pipelineTriggerTicker != nil {
		c.pipelineTriggerTicker.Stop()
	}
	close(c.stopChan)
}
