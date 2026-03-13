package services

import (
	"context"
	"easydo-server/internal/config"
	"easydo-server/internal/models"
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
	DB               *gorm.DB
	stopChan         chan struct{}
	agentCheckTicker *time.Ticker
}

const agentOfflineLeaderLockKey = "easydo:cron:agent-offline:leader"

func agentOfflineLeaderKey() string {
	return agentOfflineLeaderLockKey
}

func agentOfflineLeaderTTL() time.Duration {
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
				for _, member := range members {
					recipientID := member.UserID
					message := models.Message{
						WorkspaceID: agent.WorkspaceID,
						RecipientID: &recipientID,
						Type:        "agent_offline",
						Title:       "Agent离线告警",
						Content:     messageContent,
					}
					c.DB.Create(&message)
				}
			} else {
				var admins []models.User
				c.DB.Where("role = ?", "admin").Find(&admins)
				for _, admin := range admins {
					recipientID := admin.ID
					message := models.Message{
						Type:        "agent_offline",
						Title:       "Agent离线告警",
						Content:     messageContent,
						RecipientID: &recipientID,
					}
					c.DB.Create(&message)
				}
			}
		}
	}
}

func (c *Cron) StopAgentOfflineChecker() {
	if c.agentCheckTicker != nil {
		c.agentCheckTicker.Stop()
	}
	close(c.stopChan)
}
