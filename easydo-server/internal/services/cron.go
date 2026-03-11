package services

import (
	"easydo-server/internal/models"
	"fmt"
	"sync"
	"time"

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
				c.checkAgentOffline()
			case <-c.stopChan:
				return
			}
		}
	}()
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
