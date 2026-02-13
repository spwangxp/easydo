package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"easydo-server/internal/middleware"
	"easydo-server/internal/models"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Helper functions for extracting values from map
func getFloat64(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case float64:
			return val
		case int:
			return float64(val)
		case string:
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				return f
			}
		}
	}
	return 0
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case float64:
			return int(val)
		case int:
			return val
		case string:
			if i, err := strconv.Atoi(val); err == nil {
				return i
			}
		}
	}
	return 0
}

func getInt64(m map[string]interface{}, key string) int64 {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case float64:
			return int64(val)
		case int:
			return int64(val)
		case int64:
			return val
		case string:
			if i, err := strconv.ParseInt(val, 10, 64); err == nil {
				return i
			}
		}
	}
	return 0
}

type AgentHandler struct {
	DB *gorm.DB
}

func NewAgentHandler() *AgentHandler {
	return &AgentHandler{
		DB: models.DB,
	}
}

// generateToken generates a random secure token for agent authentication
func generateToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// RegisterAgent registers a new agent
func (h *AgentHandler) RegisterAgent(c *gin.Context) {
	// Use map to avoid struct validation issues
	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "参数错误: " + err.Error(),
		})
		return
	}

	// Extract required fields manually
	name, _ := req["name"].(string)
	host, _ := req["host"].(string)
	if name == "" || host == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "参数错误: name 和 host 是必填字段",
		})
		return
	}

	// Extract optional fields with defaults
	port := int(getInt(req, "port"))
	labels, _ := req["labels"].(string)
	tags, _ := req["tags"].(string)
	os, _ := req["os"].(string)
	arch, _ := req["arch"].(string)
	hostname, _ := req["hostname"].(string)
	ipAddress, _ := req["ip_address"].(string)
	cpuCores := int(getInt(req, "cpu_cores"))
	memoryTotal := int64(getFloat64(req, "memory_total"))
	diskTotal := int64(getFloat64(req, "disk_total"))
	token, _ := req["token"].(string)

	// 如果提供了token，验证是否是已批准的老agent
	if token != "" {
		var existingAgent models.Agent
		if err := h.DB.Where("token = ? AND registration_status = ?", token, models.AgentRegistrationStatusApproved).First(&existingAgent).Error; err == nil {
			// 老agent重新注册，更新信息
			h.DB.Model(&existingAgent).Updates(map[string]interface{}{
				"name":          name,
				"host":          host,
				"port":          port,
				"labels":        labels,
				"tags":          tags,
				"os":            os,
				"arch":          arch,
				"hostname":      hostname,
				"ip_address":    ipAddress,
				"cpu_cores":     cpuCores,
				"memory_total":  memoryTotal,
				"disk_total":    diskTotal,
				"last_heart_at": time.Now().Unix(),
			})

			c.JSON(http.StatusOK, gin.H{
				"code": 200,
				"message": "Agent信息已更新",
				"data": gin.H{
					"agent_id":           existingAgent.ID,
					"name":               existingAgent.Name,
					"status":             existingAgent.Status,
					"registration_status": existingAgent.RegistrationStatus,
				},
			})
			return
		}
	}

	// 新agent注册，不生成token，等待管理员批准
	// 生成注册密钥，用于后续获取token
	registerKey, _ := generateToken()

	agent := &models.Agent{
		Name:              name,
		Host:              host,
		Port:              port,
		Token:             "",
		RegisterKey:       registerKey,
		Status:            models.AgentStatusOffline,
		RegistrationStatus: models.AgentRegistrationStatusPending,
		Labels:            labels,
		Tags:              tags,
		OS:                os,
		Arch:              arch,
		Hostname:          hostname,
		IPAddress:         ipAddress,
		CPUCores:          cpuCores,
		MemoryTotal:       memoryTotal,
		DiskTotal:         diskTotal,
		LastHeartAt:       time.Now().Unix(),
	}

	if err := h.DB.Create(agent).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "注册Agent失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"message": "注册申请已提交，等待管理员审批",
		"data": gin.H{
			"agent_id":           agent.ID,
			"name":               agent.Name,
			"status":             agent.Status,
			"registration_status": agent.RegistrationStatus,
			"register_key":       registerKey,
		},
	})
}

// GetAgentList returns list of agents
func (h *AgentHandler) GetAgentList(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	status := c.Query("status")
	labels := c.Query("labels")

	var agents []models.Agent
	var total int64

	query := h.DB.Model(&models.Agent{})

	if status != "" {
		query = query.Where("status = ?", status)
	}

	if labels != "" {
		query = query.Where("labels LIKE ?", "%"+labels+"%")
	}

	query.Count(&total)

	offset := (page - 1) * pageSize
	query.Preload("Owner").Offset(offset).Limit(pageSize).Find(&agents)

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"list":  agents,
			"total": total,
			"page":  page,
			"size":  pageSize,
		},
	})
}

// GetAgentDetail returns agent details
func (h *AgentHandler) GetAgentDetail(c *gin.Context) {
	id := c.Param("id")

	var agent models.Agent
	if err := h.DB.Preload("Owner").First(&agent, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Agent不存在",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"id":                   agent.ID,
			"name":                 agent.Name,
			"host":                 agent.Host,
			"port":                 agent.Port,
			"token":                agent.Token,
			"register_key":         agent.RegisterKey,
			"status":               agent.Status,
			"registration_status":  agent.RegistrationStatus,
			"approved_at":          agent.ApprovedAt,
			"approved_by":          agent.ApprovedBy,
			"approved_remark":      agent.ApprovedRemark,
			"labels":               agent.Labels,
			"tags":                 agent.Tags,
			"version":              agent.Version,
			"os":                   agent.OS,
			"arch":                 agent.Arch,
			"cpu_cores":            agent.CPUCores,
			"memory_total":         agent.MemoryTotal,
			"disk_total":           agent.DiskTotal,
			"hostname":             agent.Hostname,
			"ip_address":           agent.IPAddress,
			"last_heart_at":        agent.LastHeartAt,
			"heartbeat_interval":   agent.HeartbeatInterval,
			"owner_id":             agent.OwnerID,
			"owner":                agent.Owner,
			"created_at":           agent.CreatedAt,
			"updated_at":           agent.UpdatedAt,
		},
	})
}

// UpdateAgent updates agent information
func (h *AgentHandler) UpdateAgent(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Name              string `json:"name"`
		Labels            string `json:"labels"`
		Tags              string `json:"tags"`
		Status            string `json:"status"`
		HeartbeatInterval int    `json:"heartbeat_interval"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "参数错误",
		})
		return
	}

	var agent models.Agent
	if err := h.DB.First(&agent, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Agent不存在",
		})
		return
	}

	updates := make(map[string]interface{})
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Labels != "" {
		updates["labels"] = req.Labels
	}
	if req.Tags != "" {
		updates["tags"] = req.Tags
	}
	if req.Status != "" {
		updates["status"] = req.Status
	}
	// 支持更新心跳周期（仅当值大于0时）
	if req.HeartbeatInterval > 0 {
		updates["heartbeat_interval"] = req.HeartbeatInterval
	}

	if len(updates) > 0 {
		h.DB.Model(&agent).Updates(updates)
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "更新成功",
	})
}

// DeleteAgent deletes an agent
func (h *AgentHandler) DeleteAgent(c *gin.Context) {
	id := c.Param("id")

	var taskIDs []uint64
	h.DB.Model(&models.AgentTask{}).Where("agent_id = ?", id).Pluck("id", &taskIDs)

	if len(taskIDs) > 0 {
		h.DB.Where("task_id IN ?", taskIDs).Delete(&models.TaskExecution{})
		h.DB.Where("task_id IN ?", taskIDs).Delete(&models.AgentLog{})
	}

	h.DB.Where("agent_id = ?", id).Delete(&models.AgentTask{})
	h.DB.Where("agent_id = ?", id).Delete(&models.AgentHeartbeat{})

	if err := h.DB.Delete(&models.Agent{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除Agent失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "删除成功",
	})
}

// Heartbeat handles agent heartbeat
func (h *AgentHandler) Heartbeat(c *gin.Context) {
	// Read raw body
	body, _ := c.GetRawData()

	// Use interface{} to accept both string and number
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "参数错误: " + err.Error(),
		})
		return
	}

	// Extract agent_id (can be string or number)
	agentIDVal := req["agent_id"]
	var agentIDStr string
	switch v := agentIDVal.(type) {
	case string:
		agentIDStr = v
	case float64:
		agentIDStr = fmt.Sprintf("%d", int(v))
	case int:
		agentIDStr = fmt.Sprintf("%d", v)
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "agent_id格式错误",
		})
		return
	}

	// Validate agent_id is provided
	if agentIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "agent_id不能为空",
		})
		return
	}

	// Convert agent_id string to uint
	agentID, err := strconv.ParseUint(agentIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "agent_id格式错误",
		})
		return
	}

	// Get token from request (can be missing or empty)
	token, _ := req["token"].(string)

	fmt.Printf("[DEBUG] Heartbeat: agent_id=%d, token_provided=%v, token_length=%d\n", agentID, token != "", len(token))

	var agent models.Agent
	if err := h.DB.Where("id = ?", agentID).First(&agent).Error; err != nil {
		fmt.Printf("[DEBUG] Heartbeat: agent not found, id=%d\n", agentID)
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Agent不存在",
		})
		return
	}

	fmt.Printf("[DEBUG] Heartbeat: agent found, id=%d, db_token_len=%d, db_status=%s, db_reg_status=%s\n", 
		agent.ID, len(agent.Token), agent.Status, agent.RegistrationStatus)

	agentTimestamp := getInt64(req, "timestamp")
	if agentTimestamp == 0 {
		agentTimestamp = time.Now().Unix()
	}

	// If token is provided, verify it (full authentication)
	if token != "" {
		fmt.Printf("[DEBUG] Heartbeat: comparing tokens, request=%s, db=%s, match=%v\n", 
			token, agent.Token, agent.Token == token)
		if agent.Token != token {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "认证失败",
			})
			return
		}

		// Check if agent is approved for full heartbeat
		fmt.Printf("[DEBUG] Heartbeat: checking registration status, current=%s, expected=%s\n", 
			agent.RegistrationStatus, models.AgentRegistrationStatusApproved)
		if agent.RegistrationStatus != models.AgentRegistrationStatusApproved {
			c.JSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "Agent未通过审批，无法发送心跳",
			})
			return
		}
	} else {
		// No token: allow pending agents to report initial online status
		// This enables the workflow: register -> report online -> approve -> get token
		if agent.RegistrationStatus == models.AgentRegistrationStatusApproved {
			c.JSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "已批准的Agent必须提供token",
			})
			return
		}
		// Pending agents can report online status without token
	}

	newSuccessCount := agent.ConsecutiveSuccess + 1
	if newSuccessCount > 3 {
		newSuccessCount = 3
	}

	updates := map[string]interface{}{
		"last_heart_at":       agentTimestamp,
		"consecutive_success": newSuccessCount,
		"consecutive_failures": 0,
	}

	// Mark agent as online after first successful heartbeat (reduce from 3 to 1)
	// This allows the workflow: register -> send heartbeat -> approve immediately
	if agent.Status != models.AgentStatusOnline && newSuccessCount >= 1 {
		updates["status"] = models.AgentStatusOnline
		fmt.Printf("Agent %d status updated to online\n", agentID)
	}

	h.DB.Model(&agent).Updates(updates)

	// Store heartbeat in WebSocket handler's memory (keep last 50 per agent)
	newHeartbeat := models.AgentHeartbeat{
		AgentID:      agentID,
		Timestamp:    agentTimestamp,
		CPUUsage:     getFloat64(req, "cpu_usage"),
		MemoryUsage:  getFloat64(req, "memory_usage"),
		DiskUsage:    getFloat64(req, "disk_usage"),
		LoadAvg:      getString(req, "load_avg"),
		TasksRunning: getInt(req, "tasks_running"),
	}

	wsHandler := NewWebSocketHandler()
	wsHandler.storeHeartbeat(agentID, newHeartbeat)

	// Get pending tasks for this agent (only if approved)
	var pendingTasks []models.AgentTask
	if agent.RegistrationStatus == models.AgentRegistrationStatusApproved {
		h.DB.Where("agent_id = ? AND status = ?", agentID, models.TaskStatusPending).Find(&pendingTasks)
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"status":            "ok",
			"server_time":       time.Now().Unix(),
			"pending_tasks":     len(pendingTasks),
			"heartbeat_interval": agent.HeartbeatInterval,
		},
	})
}

// GetAgentHeartbeats returns heartbeat history for an agent (from memory)
func (h *AgentHandler) GetAgentHeartbeats(c *gin.Context) {
	id := c.Param("id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "100"))

	agentID, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的Agent ID",
		})
		return
	}

	// Get heartbeats from WebSocket handler's shared memory
	wsHandler := NewWebSocketHandler()
	heartbeats, total := wsHandler.GetHeartbeats(agentID, page, pageSize)

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"list":  heartbeats,
			"total": total,
			"page":  page,
			"size":  pageSize,
		},
	})
}

// SelectAgent returns available agents matching criteria
func (h *AgentHandler) SelectAgent(c *gin.Context) {
	var req struct {
		Labels  string `json:"labels"`  // Required labels
		Tags    string `json:"tags"`    // Optional tags
		Exclude []uint64 `json:"exclude"` // Agent IDs to exclude
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "参数错误",
		})
		return
	}

	query := h.DB.Model(&models.Agent{}).Where("status = ?", models.AgentStatusOnline)
	query = query.Where("registration_status = ?", models.AgentRegistrationStatusApproved)

	if len(req.Exclude) > 0 {
		query = query.Where("id NOT IN ?", req.Exclude)
	}

	// TODO: Implement label matching logic
	// For now, return any online agent
	var agents []models.Agent
	query.Find(&agents)

	if len(agents) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"code": 200,
			"data": nil,
			"message": "没有可用的Agent",
		})
		return
	}

	// Simple load balancing: select agent with least running tasks
	bestAgent := &agents[0]
	minTasks := int64(999999)

	for _, agent := range agents {
		var runningTasks int64
		h.DB.Model(&models.AgentTask{}).Where("agent_id = ? AND status = ?", agent.ID, models.TaskStatusRunning).Count(&runningTasks)
		if runningTasks < minTasks {
			minTasks = runningTasks
			bestAgent = &agent
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": bestAgent,
	})
}

// GetAgentSelf allows an agent to fetch its own info using agent token
func (h *AgentHandler) GetAgentSelf(c *gin.Context) {
	// Read raw body for flexible parsing
	body, _ := c.GetRawData()

	// Use interface{} to accept both string and number for agent_id
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "参数错误: " + err.Error(),
		})
		return
	}

	// Extract agent_id (can be string or number)
	agentIDVal := req["agent_id"]
	var agentID uint
	switch v := agentIDVal.(type) {
	case float64:
		agentID = uint(v)
	case int:
		agentID = uint(v)
	case string:
		if parsed, err := strconv.ParseUint(v, 10, 64); err == nil {
			agentID = uint(parsed)
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "agent_id格式错误",
		})
		return
	}

	// Get token (can be empty for pending agents)
	token, _ := req["token"].(string)

	// Find agent
	var agent models.Agent
	if err := h.DB.Where("id = ?", agentID).First(&agent).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Agent不存在",
		})
		return
	}

	// Verify agent status
	// If token is provided, verify it and return full info
	if token != "" {
		if agent.Token != token {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "认证失败",
			})
			return
		}
		// Token verified, return full agent info including token and config
		c.JSON(http.StatusOK, gin.H{
			"code": 200,
			"data": gin.H{
				"id":                  agent.ID,
				"name":                agent.Name,
				"status":              agent.Status,
				"registration_status": agent.RegistrationStatus,
				"token":               agent.Token,
				"heartbeat_interval":  agent.HeartbeatInterval,
			},
		})
		return
	}

	// No token: check for register_key (for agents waiting to fetch their token)
	registerKey, _ := req["register_key"].(string)
	if registerKey != "" && agent.RegistrationStatus == models.AgentRegistrationStatusApproved {
		// Verify register key
		if agent.RegisterKey != registerKey {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "注册密钥无效",
			})
			return
		}
		// Register key verified, return token
		c.JSON(http.StatusOK, gin.H{
			"code": 200,
			"data": gin.H{
				"id":                  agent.ID,
				"name":                agent.Name,
				"status":              agent.Status,
				"registration_status": agent.RegistrationStatus,
				"token":               agent.Token,
				"heartbeat_interval":  agent.HeartbeatInterval,
			},
		})
		return
	}

	// No token and no valid register key: allow agent to check its registration status
	// This enables the workflow: register -> wait for approval -> check status -> get token
	if agent.RegistrationStatus == models.AgentRegistrationStatusApproved {
		// Approved but no token: agent needs to get token via register key
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "Agent已批准，需要注册密钥获取token",
		})
		return
	}

	// Agent is pending or rejected, return status without token
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"id":                  agent.ID,
			"name":                agent.Name,
			"status":              agent.Status,
			"registration_status": agent.RegistrationStatus,
		},
	})
}

// GetPendingAgents returns list of pending agents
func (h *AgentHandler) GetPendingAgents(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))

	var agents []models.Agent
	var total int64

	query := h.DB.Model(&models.Agent{}).Where("registration_status = ?", models.AgentRegistrationStatusPending)
	query.Count(&total)

	offset := (page - 1) * pageSize
	query.Preload("Owner").Offset(offset).Limit(pageSize).Order("created_at DESC").Find(&agents)

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"list":  agents,
			"total": total,
			"page":  page,
			"size":  pageSize,
		},
	})
}

// ApproveAgent approves a pending agent registration
func (h *AgentHandler) ApproveAgent(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Remark string `json:"remark"`
	}
	c.ShouldBindJSON(&req)

	var agent models.Agent
	if err := h.DB.First(&agent, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Agent不存在",
		})
		return
	}

	if agent.RegistrationStatus != models.AgentRegistrationStatusPending {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "该Agent不是待接纳状态",
		})
		return
	}

	// Check if agent is online (sending heartbeats)
	if agent.Status != models.AgentStatusOnline {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Agent离线，无法批准。请等待Agent上线后再试。",
		})
		return
	}

	// 生成token
	token, err := generateToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "生成令牌失败",
		})
		return
	}

	approvedBy := middleware.GetCurrentUserID(c)
	now := time.Now().Unix()

	h.DB.Model(&agent).Updates(map[string]interface{}{
		"registration_status": models.AgentRegistrationStatusApproved,
		"token":               token,
		"status":              models.AgentStatusOnline,
		"approved_at":         now,
		"approved_by":         approvedBy,
		"approved_remark":     req.Remark,
	})

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"message": "接纳成功，Token已自动下发",
		"data": gin.H{
			"token": token,
		},
	})
}

// RejectAgent rejects a pending agent registration
func (h *AgentHandler) RejectAgent(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Remark string `json:"remark" binding:"required"`
	}
	c.ShouldBindJSON(&req)

	var agent models.Agent
	if err := h.DB.First(&agent, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Agent不存在",
		})
		return
	}

	if agent.RegistrationStatus != models.AgentRegistrationStatusPending {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "该Agent不是待接纳状态",
		})
		return
	}

	approvedBy := middleware.GetCurrentUserID(c)
	now := time.Now().Unix()

	h.DB.Model(&agent).Updates(map[string]interface{}{
		"registration_status": models.AgentRegistrationStatusRejected,
		"status":              models.AgentStatusOffline,
		"approved_at":         now,
		"approved_by":         approvedBy,
		"approved_remark":     req.Remark,
	})

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "已拒绝该Agent注册申请",
	})
}

// RefreshAgentToken refreshes an agent's token
func (h *AgentHandler) RefreshAgentToken(c *gin.Context) {
	id := c.Param("id")

	var agent models.Agent
	if err := h.DB.First(&agent, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Agent不存在",
		})
		return
	}

	if agent.RegistrationStatus != models.AgentRegistrationStatusApproved {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "该Agent不是已批准状态",
		})
		return
	}

	// 生成新token
	token, err := generateToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "生成令牌失败",
		})
		return
	}

	// 保存新token
	h.DB.Model(&agent).Updates(map[string]interface{}{
		"token": token,
	})

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"message": "Token已刷新",
		"data": gin.H{
			"token": token,
		},
	})
}

// RemoveAgent removes an agent (revokes approval, keeps record)
func (h *AgentHandler) RemoveAgent(c *gin.Context) {
	id := c.Param("id")

	var agent models.Agent
	if err := h.DB.First(&agent, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Agent不存在",
		})
		return
	}

	if agent.RegistrationStatus != models.AgentRegistrationStatusApproved {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "只有已接纳的执行器才能移除",
		})
		return
	}

	// Revoke approval: set status to pending and clear token
	h.DB.Model(&agent).Updates(map[string]interface{}{
		"registration_status": models.AgentRegistrationStatusPending,
		"token":               "",
		"status":              models.AgentStatusOffline,
	})

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "执行器已移除，需要重新注册并审批",
	})
}
