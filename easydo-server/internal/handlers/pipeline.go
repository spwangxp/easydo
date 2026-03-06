package handlers

import (
	"easydo-server/internal/models"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type PipelineHandler struct {
	DB *gorm.DB
}

func NewPipelineHandler() *PipelineHandler {
	return &PipelineHandler{DB: models.DB}
}

// getEnvironmentText 返回环境的中文显示文本
func getEnvironmentText(env string) string {
	switch env {
	case "development":
		return "开发环境"
	case "testing":
		return "测试环境"
	case "production":
		return "生产环境"
	default:
		return env
	}
}

func (h *PipelineHandler) GetPipelineList(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	keyword := c.Query("keyword")
	projectID := c.Query("project_id")
	environment := c.Query("environment")
	tab := c.DefaultQuery("tab", "all")

	var pipelines []models.Pipeline
	var total int64

	query := h.DB.Model(&models.Pipeline{})

	if keyword != "" {
		query = query.Where("name LIKE ?", "%"+keyword+"%")
	}

	if projectID != "" {
		pid, err := strconv.ParseUint(projectID, 10, 64)
		if err == nil {
			query = query.Where("project_id = ?", pid)
		}
	}

	if environment != "" {
		query = query.Where("environment = ?", environment)
	}

	userID := c.GetUint64("user_id")
	if tab == "created" {
		query = query.Where("owner_id = ?", userID)
	} else if tab == "favorited" {
		query = query.Where("is_favorite = ?", true)
	} else if tab == "frequent" {
		// 常用：显示用户创建的和收藏的
		query = query.Where("owner_id = ? OR is_favorite = ?", userID, true)
	}

	// 计算各tab的数量
	var allCount, createdCount, favoritedCount int64
	h.DB.Model(&models.Pipeline{}).Count(&allCount)
	h.DB.Model(&models.Pipeline{}).Where("owner_id = ?", userID).Count(&createdCount)
	h.DB.Model(&models.Pipeline{}).Where("is_favorite = ?", true).Count(&favoritedCount)

	query.Count(&total)

	offset := (page - 1) * pageSize
	// 按更新时间降序排序（从近到远）
	query.Preload("Owner").Preload("Project").Order("updated_at DESC").Offset(offset).Limit(pageSize).Find(&pipelines)

	// 为每个流水线获取最近构建信息
	type PipelineWithLastBuild struct {
		models.Pipeline
		LastBuild       *models.PipelineRun `json:"last_build"`
		LastEditor      string              `json:"last_editor"`      // 最后编辑人员
		LastEditorID    uint64              `json:"last_editor_id"`   // 最后编辑人员ID
		LatestRunner    string              `json:"latest_runner"`    // 最新构建人员
		EnvironmentText string              `json:"environment_text"` // 环境显示文本
		ProjectName     string              `json:"project_name"`     // 项目名称
	}

	result := make([]PipelineWithLastBuild, 0, len(pipelines))
	for _, p := range pipelines {
		pwb := PipelineWithLastBuild{
			Pipeline:        p,
			LastEditorID:    p.OwnerID,
			EnvironmentText: getEnvironmentText(p.Environment),
		}
		// 获取最后编辑人员
		if p.Owner != nil {
			pwb.LastEditor = p.Owner.Username
		}
		// 获取项目名称
		if p.Project != nil {
			pwb.ProjectName = p.Project.Name
		}
		// 获取最近一次构建记录
		var lastRun models.PipelineRun
		h.DB.Where("pipeline_id = ?", p.ID).Order("build_number DESC").First(&lastRun)
		if lastRun.ID > 0 {
			lastRun.CreatedAt = time.Now() // 使用当前时间作为模拟
			pwb.LastBuild = &lastRun
			pwb.LatestRunner = lastRun.TriggerUser
		}
		result = append(result, pwb)
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"list":  result,
			"total": total,
			"page":  page,
			"size":  pageSize,
			"tab_counts": gin.H{
				"all":       allCount,
				"created":   createdCount,
				"favorited": favoritedCount,
			},
		},
	})
}

func (h *PipelineHandler) GetPipelineDetail(c *gin.Context) {
	id := c.Param("id")

	var pipeline models.Pipeline
	if err := h.DB.Preload("Owner").Preload("Project").First(&pipeline, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "流水线不存在",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": pipeline,
	})
}

func (h *PipelineHandler) CreatePipeline(c *gin.Context) {
	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
		ProjectID   uint64 `json:"project_id"`
		Environment string `json:"environment"`
		Config      string `json:"config"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "参数错误",
		})
		return
	}

	// Validate DAG if config is provided
	if req.Config != "" {
		var config PipelineConfig
		if err := json.Unmarshal([]byte(req.Config), &config); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "流水线配置JSON解析失败: " + err.Error(),
			})
			return
		}

		if valid, errMsg := config.ValidateDAG(); !valid {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": errMsg,
			})
			return
		}
	}

	userID := c.GetUint64("user_id")

	pipeline := &models.Pipeline{
		Name:        req.Name,
		Description: req.Description,
		ProjectID:   req.ProjectID,
		Environment: req.Environment,
		Config:      req.Config,
		OwnerID:     userID,
	}

	// 如果 project_id 为 0，设置 为 NULL（不创建外键关联）
	if req.ProjectID == 0 {
		// 使用原始 SQL 设置 NULL
		err := h.DB.Exec("INSERT INTO pipelines (created_at, updated_at, name, description, config, project_id, owner_id, environment, is_public, is_favorite) VALUES (?, ?, ?, ?, ?, NULL, ?, ?, ?, ?)",
			time.Now(), time.Now(), req.Name, req.Description, req.Config, userID, req.Environment, false, false).Error
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "创建流水线失败: " + err.Error(),
			})
			return
		}

		// 获取刚创建的流水线
		var createdPipeline models.Pipeline
		h.DB.Where("name = ? AND owner_id = ?", req.Name, userID).Order("id DESC").First(&createdPipeline)

		c.JSON(http.StatusOK, gin.H{
			"code": 200,
			"data": createdPipeline,
		})
		return
	}

	if err := h.DB.Create(pipeline).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建流水线失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": pipeline,
	})
}

func (h *PipelineHandler) UpdatePipeline(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Environment string `json:"environment"`
		Config      string `json:"config"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "参数错误: " + err.Error(),
		})
		return
	}

	// 先查询流水线是否存在
	var pipeline models.Pipeline
	if err := h.DB.First(&pipeline, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "流水线不存在",
		})
		return
	}

	// Validate DAG if config is being updated
	if req.Config != "" {
		var config PipelineConfig
		if err := json.Unmarshal([]byte(req.Config), &config); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "流水线配置JSON解析失败: " + err.Error(),
			})
			return
		}

		if valid, errMsg := config.ValidateDAG(); !valid {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": errMsg,
			})
			return
		}
	}

	// 逐个更新字段
	updates := make(map[string]interface{})
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Description != "" {
		updates["description"] = req.Description
	}
	if req.Environment != "" {
		updates["environment"] = req.Environment
	}
	if req.Config != "" {
		updates["config"] = req.Config
	}

	// 使用Save方法更新
	if len(updates) > 0 {
		if name, ok := updates["name"].(string); ok {
			pipeline.Name = name
		}
		if desc, ok := updates["description"].(string); ok {
			pipeline.Description = desc
		}
		if env, ok := updates["environment"].(string); ok {
			pipeline.Environment = env
		}
		if config, ok := updates["config"].(string); ok {
			pipeline.Config = config
		}

		if err := h.DB.Save(&pipeline).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "更新流水线失败: " + err.Error(),
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "更新成功",
	})
}

func (h *PipelineHandler) DeletePipeline(c *gin.Context) {
	id := c.Param("id")

	// 先删除关联的运行记录
	h.DB.Where("pipeline_id = ?", id).Delete(&models.PipelineRun{})

	if err := h.DB.Delete(&models.Pipeline{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除流水线失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "删除成功",
	})
}

func (h *PipelineHandler) RunPipeline(c *gin.Context) {
	id := c.Param("id")

	var pipeline models.Pipeline
	if err := h.DB.First(&pipeline, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "流水线不存在",
		})
		return
	}

	// 解析流水线配置，检查是否有需要 Agent 执行的节点
	var config PipelineConfig
	if err := json.Unmarshal([]byte(pipeline.Config), &config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "流水线配置解析失败: " + err.Error(),
		})
		return
	}

	// 检查是否有需要 Agent 执行的节点（shell/docker/git_clone/agent）
	hasAgentNode := false
	for _, node := range config.Nodes {
		if node.Type == "shell" || node.Type == "docker" || node.Type == "git_clone" || node.Type == "agent" {
			hasAgentNode = true
			break
		}
	}

	// 如果有需要 Agent 执行的节点，检查是否有可用的 Agent
	if hasAgentNode {
		var availableAgents []models.Agent
		h.DB.Where("status = ? AND registration_status = ?",
			models.AgentStatusOnline,
			models.AgentRegistrationStatusApproved).Find(&availableAgents)

		if len(availableAgents) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "没有可用的执行器，请确认至少有一个执行器在线且已接纳后再运行流水线。您可以前往「执行器」页面查看和管理执行器状态。",
			})
			return
		}
	}

	// 获取最新的构建号
	var lastRun models.PipelineRun
	h.DB.Where("pipeline_id = ?", id).Order("build_number DESC").First(&lastRun)

	buildNumber := 1
	if lastRun.ID > 0 {
		buildNumber = lastRun.BuildNumber + 1
	}

	// 创建新的构建记录，保存配置快照
	run := &models.PipelineRun{
		PipelineID:  pipeline.ID,
		BuildNumber: buildNumber,
		Status:      "running",
		TriggerType: "manual",
		TriggerUser: "demo",
		StartTime:   time.Now().Unix(),
		Config:      pipeline.Config, // 保存配置快照
	}

	// 创建记录
	if err := h.DB.Create(run).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 500,
			"msg":  "创建运行记录失败: " + err.Error(),
		})
		return
	}

	// 检查 ID 是否已设置
	if run.ID == 0 {
		// 尝试获取刚创建的记录
		var latestRun models.PipelineRun
		h.DB.Where("pipeline_id = ? AND build_number = ?", pipeline.ID, buildNumber).Order("id DESC").First(&latestRun)
		if latestRun.ID > 0 {
			run.ID = latestRun.ID
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "无法获取运行记录 ID",
			})
			return
		}
	}

	// 异步执行流水线任务
	go h.executePipelineTasks(pipeline, run, config)

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"run_id":       run.ID,
			"build_number": buildNumber,
		},
	})
}

// PipelineNode represents a node in the pipeline configuration
type PipelineNode struct {
	ID            string                 `json:"id"`
	Type          string                 `json:"type"` // shell, docker, git_clone, email, agent
	Name          string                 `json:"name"`
	Config        map[string]interface{} `json:"config,omitempty"` // 新格式：config
	Params        map[string]interface{} `json:"params,omitempty"` // 旧格式兼容：params
	Timeout       int                    `json:"timeout"`
	IgnoreFailure bool                   `json:"ignore_failure"` // If true, current node can execute even if upstream fails
}

// getNodeConfig returns the node configuration, supporting both config and params
func (n *PipelineNode) getNodeConfig() map[string]interface{} {
	// 优先使用 config（新格式）
	if n.Config != nil && len(n.Config) > 0 {
		return n.Config
	}
	// 兼容 params（旧格式）
	if n.Params != nil && len(n.Params) > 0 {
		return n.Params
	}
	return make(map[string]interface{})
}

// PipelineConfig represents the pipeline configuration
// 支持新旧两种格式：
// - 新格式 (version: "2.0"): nodes + edges
// - 旧格式: nodes + connections
type PipelineConfig struct {
	Version     string               `json:"version"`
	Nodes       []PipelineNode       `json:"nodes"`
	Edges       []PipelineEdge       `json:"edges"`       // 新格式
	Connections []PipelineConnection `json:"connections"` // 旧格式兼容
}

// PipelineEdge represents an edge in the pipeline DAG (新格式)
type PipelineEdge struct {
	From          string `json:"from"`
	To            string `json:"to"`
	IgnoreFailure bool   `json:"ignore_failure"` // If true, downstream can execute even if upstream fails
}

// PipelineConnection represents a connection between nodes (旧格式兼容)
type PipelineConnection struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
}

// getEdges returns edges in unified format
// 将 connections 转换为 edges 格式以统一处理
func (c *PipelineConfig) getEdges() []PipelineEdge {
	// 如果有 edges（新格式），直接返回
	if len(c.Edges) > 0 {
		return c.Edges
	}

	// 如果有 connections（旧格式），转换为 edges
	if len(c.Connections) > 0 {
		edges := make([]PipelineEdge, len(c.Connections))
		for i, conn := range c.Connections {
			edges[i] = PipelineEdge{
				From: conn.From,
				To:   conn.To,
			}
		}
		return edges
	}

	return nil
}

// ValidateDAG validates that the pipeline configuration forms a valid DAG
// Returns (isValid, errorMessage)
func (c *PipelineConfig) ValidateDAG() (bool, string) {
	// Check if nodes exist
	if len(c.Nodes) == 0 {
		return false, "流水线配置无效：节点列表为空"
	}

	// Check for duplicate node IDs
	nodeIDSet := make(map[string]bool)
	for _, node := range c.Nodes {
		if node.ID == "" {
			return false, "流水线配置无效：节点ID不能为空"
		}
		if nodeIDSet[node.ID] {
			return false, fmt.Sprintf("流水线配置无效：节点ID '%s' 重复", node.ID)
		}
		nodeIDSet[node.ID] = true
	}

	edges := c.getEdges()

	if len(c.Nodes) == 1 && len(edges) == 0 {
		return true, ""
	}

	if len(c.Nodes) > 1 && len(edges) == 0 {
		return false, "流水线配置无效：多节点流水线必须包含依赖边"
	}

	adjacency := make(map[string][]string)
	inDegree := make(map[string]int)
	originalInDegree := make(map[string]int)

	for _, node := range c.Nodes {
		inDegree[node.ID] = 0
		originalInDegree[node.ID] = 0
		adjacency[node.ID] = []string{}
	}

	nodesInEdges := make(map[string]bool)

	// Process edges
	for _, edge := range edges {
		// Verify source and target nodes exist
		if !nodeIDSet[edge.From] {
			return false, fmt.Sprintf("流水线配置无效：边引用的源节点 '%s' 不存在", edge.From)
		}
		if !nodeIDSet[edge.To] {
			return false, fmt.Sprintf("流水线配置无效：边引用的目标节点 '%s' 不存在", edge.To)
		}

		// Check for self-referencing edges
		if edge.From == edge.To {
			return false, fmt.Sprintf("流水线配置无效：节点 '%s' 不能自引用", edge.From)
		}

		// Check for duplicate edges
		edgeKey := edge.From + "->" + edge.To
		if _, exists := adjacency[edge.From]; exists {
			for _, existing := range adjacency[edge.From] {
				if existing == edge.To {
					return false, fmt.Sprintf("流水线配置无效：边 '%s' 重复", edgeKey)
				}
			}
		}

		// Add edge to adjacency list
		adjacency[edge.From] = append(adjacency[edge.From], edge.To)
		inDegree[edge.To]++
		originalInDegree[edge.To]++
		nodesInEdges[edge.From] = true
		nodesInEdges[edge.To] = true
	}

	entryNodes := []string{}
	for nodeID, degree := range originalInDegree {
		if degree == 0 && nodesInEdges[nodeID] {
			entryNodes = append(entryNodes, nodeID)
		}
	}

	if len(entryNodes) == 0 && len(nodesInEdges) > 0 {
		return false, "流水线配置无效：没有起始任务（所有任务都有前置依赖）"
	}

	isolatedNodes := []string{}
	for _, node := range c.Nodes {
		if !nodesInEdges[node.ID] {
			isolatedNodes = append(isolatedNodes, node.ID)
		}
	}

	if len(isolatedNodes) > 0 {
		return false, fmt.Sprintf("流水线配置无效：存在孤立节点（未连接到依赖图）: %v", isolatedNodes)
	}

	exitNodes := []string{}
	for _, node := range c.Nodes {
		if len(adjacency[node.ID]) == 0 {
			exitNodes = append(exitNodes, node.ID)
		}
	}

	if len(exitNodes) == 0 {
		return false, "流水线配置无效：没有结束任务（所有任务都有后置依赖）"
	}

	processedCount := 0
	queue := append([]string{}, entryNodes...)
	tempInDegree := make(map[string]int)
	for k, v := range originalInDegree {
		tempInDegree[k] = v
	}

	for len(queue) > 0 {
		nodeID := queue[0]
		queue = queue[1:]
		processedCount++

		for _, neighbor := range adjacency[nodeID] {
			tempInDegree[neighbor]--
			if tempInDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	if processedCount != len(c.Nodes) {
		return false, "流水线配置无效：检测到循环依赖"
	}

	reachable := make(map[string]bool)
	bfsQueue := append([]string{}, entryNodes...)
	for _, nodeID := range entryNodes {
		reachable[nodeID] = true
	}

	for len(bfsQueue) > 0 {
		current := bfsQueue[0]
		bfsQueue = bfsQueue[1:]

		for _, neighbor := range adjacency[current] {
			if !reachable[neighbor] {
				reachable[neighbor] = true
				bfsQueue = append(bfsQueue, neighbor)
			}
		}
	}

	unreachableNodes := []string{}
	for _, node := range c.Nodes {
		if !reachable[node.ID] {
			unreachableNodes = append(unreachableNodes, node.ID)
		}
	}

	if len(unreachableNodes) > 0 {
		return false, fmt.Sprintf("流水线配置无效：存在不可达节点: %v", unreachableNodes)
	}

	return true, ""
}

func (h *PipelineHandler) createAllNodeTasks(pipeline models.Pipeline, run *models.PipelineRun, config PipelineConfig, nodeMap map[string]*PipelineNode) {
	for i := range config.Nodes {
		node := &config.Nodes[i]
		nodeConfig := node.getNodeConfig()

		script := h.buildTaskScript(node, nodeConfig)

		workDir := ""
		if wd, ok := nodeConfig["working_dir"].(string); ok {
			workDir = wd
		}

		envVars := ""
		if env, ok := nodeConfig["env"].(map[string]interface{}); ok && len(env) > 0 {
			envMap := make(map[string]string)
			for k, v := range env {
				if s, ok := v.(string); ok {
					envMap[k] = s
				}
			}
			if len(envMap) > 0 {
				envData, _ := json.Marshal(envMap)
				envVars = string(envData)
			}
		}

		timeout := node.Timeout
		if timeout <= 0 {
			timeout = 3600
		}

		taskType := node.Type
		if taskType == "agent" || taskType == "custom" {
			taskType = "shell"
		}

		task := &models.AgentTask{
			PipelineRunID: run.ID,
			NodeID:        node.ID,
			TaskType:      taskType,
			Name:          node.Name,
			Params:        h.jsonEncode(nodeConfig),
			Script:        script,
			WorkDir:       workDir,
			EnvVars:       envVars,
			Status:        models.TaskStatusPending,
			Timeout:       timeout,
		}

		if err := h.DB.Create(task).Error; err != nil {
			fmt.Printf("Failed to create task for node %s: %v\n", node.ID, err)
		}
	}
}

// executePipelineTasks starts pipeline execution asynchronously.
// Initial tasks are created for nodes with inDegree=0 and pushed to agent via WebSocket.
// Downstream tasks are created and pushed when upstream tasks complete.
func (h *PipelineHandler) executePipelineTasks(pipeline models.Pipeline, run *models.PipelineRun, config PipelineConfig) {
	// 检查配置有效性
	if config.Nodes == nil || len(config.Nodes) == 0 {
		h.updateRunStatus(run.ID, "failed", "流水线配置无效：节点列表为空")
		return
	}

	// 构建节点映射
	nodeMap := make(map[string]*PipelineNode)
	for i := range config.Nodes {
		nodeMap[config.Nodes[i].ID] = &config.Nodes[i]
	}

	// 构建依赖图并计算入度
	inDegree := make(map[string]int)
	graph := make(map[string][]string)

	for _, node := range config.Nodes {
		inDegree[node.ID] = 0
	}

	// 获取边列表（兼容新旧格式）
	edges := config.getEdges()

	for _, edge := range edges {
		graph[edge.From] = append(graph[edge.From], edge.To)
		inDegree[edge.To]++
	}

	// 选择执行 Agent
	agentID := h.selectAgentForPipeline(h.DB)
	if agentID == 0 {
		h.updateRunStatus(run.ID, "failed", "没有可用的Agent")
		return
	}

	h.DB.Model(run).Update("agent_id", agentID)

	// 找出入度为0的初始节点
	resolver := NewVariableResolver()
	envVars := BuildGlobalEnvVars(&pipeline, run)
	resolver.SetEnvVars(envVars)

	for nodeID, degree := range inDegree {
		if degree == 0 {
			// 创建初始节点任务
			node := nodeMap[nodeID]
			if node != nil {
				h.executeNodeWithAgent(h.DB, pipeline, run, node, nodeMap, resolver, agentID)
			}
		}
	}

	// 任务执行由 agent 通过 WebSocket 驱动，下游任务由 triggerDownstreamTasks 在任务完成时触发
}

func (h *PipelineHandler) selectAgentForPipeline(db *gorm.DB) uint64 {
	var agents []models.Agent
	db.Where("status = ? AND registration_status = ?",
		models.AgentStatusOnline,
		models.AgentRegistrationStatusApproved).Find(&agents)

	if len(agents) == 0 {
		return 0
	}

	var selectedAgent uint64
	minTasks := int64(999999)

	for _, agent := range agents {
		var runningTasks int64
		db.Model(&models.AgentTask{}).Where("agent_id = ? AND status = ?", agent.ID, models.TaskStatusRunning).Count(&runningTasks)
		if runningTasks < minTasks {
			minTasks = runningTasks
			selectedAgent = agent.ID
		}
	}

	return selectedAgent
}

func (h *PipelineHandler) executeNodeWithAgent(db *gorm.DB, pipeline models.Pipeline, run *models.PipelineRun, node *PipelineNode, nodeMap map[string]*PipelineNode, resolver *VariableResolver, agentID uint64) (bool, map[string]interface{}) {
	taskType := node.Type

	if taskType == "email" {
		success := h.executeEmailTask(db, run, node)
		return success, nil
	}

	if taskType != "shell" && taskType != "docker" && taskType != "git_clone" && taskType != "agent" && taskType != "custom" {
		return true, nil
	}

	if taskType == "agent" || taskType == "custom" {
		taskType = "shell"
	}

	var agent models.Agent
	if err := db.First(&agent, agentID).Error; err != nil {
		return false, nil
	}

	if agent.Status != models.AgentStatusOnline && agent.Status != models.AgentStatusBusy {
		return false, nil
	}

	nodeConfig := node.getNodeConfig()
	if resolver != nil {
		resolvedConfig, err := resolver.ResolveNodeConfig(nodeConfig)
		if err == nil {
			nodeConfig = resolvedConfig
		}
	}

	script := h.buildTaskScript(node, nodeConfig)
	if resolver != nil && script != "" {
		resolvedScript, err := resolver.ResolveVariables(script)
		if err == nil {
			script = resolvedScript
		}
	}

	timeout := node.Timeout
	if timeout <= 0 {
		timeout = 3600
	}

	workDir := ""
	if wd, ok := nodeConfig["working_dir"].(string); ok {
		workDir = wd
	}

	envVars := ""
	if env, ok := nodeConfig["env"].(map[string]interface{}); ok {
		envJSON, _ := json.Marshal(env)
		envVars = string(envJSON)
	}

	maxRetries := 0
	if retryVal, ok := nodeConfig["retry_count"].(float64); ok && retryVal > 0 {
		maxRetries = int(retryVal)
	}

	repoURL := ""
	repoBranch := ""
	repoCommit := ""
	repoPath := ""

	if taskType == "git_clone" {
		if repo, ok := nodeConfig["repository"].(map[string]interface{}); ok {
			if url, ok := repo["url"].(string); ok {
				repoURL = url
			}
			if branch, ok := repo["branch"].(string); ok {
				repoBranch = branch
			}
			if commit, ok := repo["commit_id"].(string); ok {
				repoCommit = commit
			}
			if targetDir, ok := repo["target_dir"].(string); ok {
				repoPath = targetDir
			}
		}
	}

	// 检查任务是否已存在
	var task models.AgentTask
	result := db.Where("pipeline_run_id = ? AND node_id = ?", run.ID, node.ID).First(&task)

	if result.Error != nil {
		// 任务不存在，创建新任务
		task = models.AgentTask{
			AgentID:       agentID,
			PipelineRunID: run.ID,
			NodeID:        node.ID,
			TaskType:      taskType,
			Name:          node.Name,
			Params:        h.jsonEncode(nodeConfig),
			Script:        script,
			WorkDir:       workDir,
			EnvVars:       envVars,
			Status:        models.TaskStatusPending,
			Timeout:       timeout,
			MaxRetries:    maxRetries,
			RepoURL:       repoURL,
			RepoBranch:    repoBranch,
			RepoCommit:    repoCommit,
			RepoPath:      repoPath,
		}
		if err := db.Create(&task).Error; err != nil {
			fmt.Printf("Failed to create task for node %s: %v\n", node.ID, err)
			return false, nil
		}
		fmt.Printf("Created task %d for node %s\n", task.ID, node.ID)
	} else {
		// 任务已存在，更新任务信息
		task.AgentID = agentID
		task.Script = script
		task.WorkDir = workDir
		task.EnvVars = envVars
		task.Timeout = timeout
		task.Status = models.TaskStatusPending
		task.MaxRetries = maxRetries
		if err := db.Save(&task).Error; err != nil {
			fmt.Printf("Failed to update task %d: %v\n", task.ID, err)
			return false, nil
		}
		fmt.Printf("Updated existing task %d for node %s\n", task.ID, node.ID)
	}

	// Immediately dispatch task via WebSocket if agent is connected.
	// If not connected, task remains pending and will be pushed on next heartbeat/connect.
	_ = SharedWebSocketHandler().sendTaskAssign(task)

	return true, nil
}

// executeNode executes a single node
// Returns (success, taskOutputs) - success indicates if execution was successful,
// taskOutputs contains the outputs generated by the task for downstream tasks
func (h *PipelineHandler) executeNode(db *gorm.DB, pipeline models.Pipeline, run *models.PipelineRun, node *PipelineNode, nodeMap map[string]*PipelineNode, resolver *VariableResolver) (bool, map[string]interface{}) {
	// 判断任务类型：Agent执行（shell/docker/git_clone） vs Server执行（email）
	taskType := node.Type

	// Server 直接执行的任务类型
	if taskType == "email" {
		success := h.executeEmailTask(db, run, node)
		return success, nil
	}

	// 需要通过 Agent 执行的任务类型（shell/docker/git_clone/agent）
	// 支持的任务类型包括：shell, docker, git_clone, agent, custom
	if taskType != "shell" && taskType != "docker" && taskType != "git_clone" && taskType != "agent" && taskType != "custom" {
		// 未知类型，视为不需要执行，直接返回成功
		return true, nil
	}

	// 如果是 agent 或 custom 类型，按 shell 类型处理
	if taskType == "agent" || taskType == "custom" {
		taskType = "shell"
	}

	// 选择 Agent
	var agent models.Agent
	var agentID uint64

	// 自动选择在线且已批准的 Agent
	var agents []models.Agent
	db.Where("status = ? AND registration_status = ?",
		models.AgentStatusOnline,
		models.AgentRegistrationStatusApproved).Find(&agents)

	if len(agents) == 0 {
		return false, nil
	}

	// 选择负载最小的 Agent
	agentID = agents[0].ID
	minTasks := int64(999999)

	for _, a := range agents {
		var runningTasks int64
		db.Model(&models.AgentTask{}).Where("agent_id = ? AND status = ?", a.ID, models.TaskStatusRunning).Count(&runningTasks)
		if runningTasks < minTasks {
			minTasks = runningTasks
			agentID = a.ID
		}
	}

	if err := db.First(&agent, agentID).Error; err != nil {
		return false, nil
	}

	if agent.Status != models.AgentStatusOnline && agent.Status != models.AgentStatusBusy {
		return false, nil
	}

	// 从节点配置中提取任务参数
	config := node.getNodeConfig()

	// 使用变量解析器解析配置中的变量引用
	if resolver != nil {
		resolvedConfig, err := resolver.ResolveNodeConfig(config)
		if err == nil {
			config = resolvedConfig
		}
	}

	// 获取脚本内容
	script := h.buildTaskScript(node, config)

	// 解析脚本中的变量引用
	if resolver != nil && script != "" {
		resolvedScript, err := resolver.ResolveVariables(script)
		if err == nil {
			script = resolvedScript
		}
	}

	// 获取超时时间
	timeout := node.Timeout
	if timeout <= 0 {
		timeout = 3600 // 默认1小时
	}

	// 获取工作目录
	workDir := ""
	if wd, ok := config["working_dir"].(string); ok {
		workDir = wd
	}

	// 获取环境变量
	envVars := ""
	if env, ok := config["env"].(map[string]interface{}); ok {
		envJSON, _ := json.Marshal(env)
		envVars = string(envJSON)
	}

	// 获取仓库信息（用于 git_clone 任务）
	repoURL := ""
	repoBranch := ""
	repoCommit := ""
	repoPath := ""

	if taskType == "git_clone" {
		if repo, ok := config["repository"].(map[string]interface{}); ok {
			if url, ok := repo["url"].(string); ok {
				repoURL = url
			}
			if branch, ok := repo["branch"].(string); ok {
				repoBranch = branch
			}
			if commit, ok := repo["commit_id"].(string); ok {
				repoCommit = commit
			}
			if targetDir, ok := repo["target_dir"].(string); ok {
				repoPath = targetDir
			}
		}
	}

	var task models.AgentTask
	result := db.Where("pipeline_run_id = ? AND node_id = ?", run.ID, node.ID).First(&task)

	if result.Error != nil {
		task = models.AgentTask{
			AgentID:       agentID,
			PipelineRunID: run.ID,
			NodeID:        node.ID,
			TaskType:      taskType,
			Name:          node.Name,
			Params:        h.jsonEncode(config),
			Script:        script,
			WorkDir:       workDir,
			EnvVars:       envVars,
			Status:        models.TaskStatusPending,
			Timeout:       timeout,
			RepoURL:       repoURL,
			RepoBranch:    repoBranch,
			RepoCommit:    repoCommit,
			RepoPath:      repoPath,
		}
		if err := db.Create(&task).Error; err != nil {
			return false, nil
		}
	} else {
		task.AgentID = agentID
		task.Status = models.TaskStatusPending
		task.Script = script
		task.WorkDir = workDir
		task.EnvVars = envVars
		task.Timeout = timeout
		if err := db.Save(&task).Error; err != nil {
			return false, nil
		}
	}

	// 更新 Agent 状态
	db.Model(&agent).Update("status", models.AgentStatusBusy)

	// 更新 PipelineRun 绑定的 Agent
	db.Model(run).Update("agent_id", agentID)

	// 等待任务完成（带超时）
	timeoutChan := time.After(time.Duration(timeout+30) * time.Second)
	tickChan := time.Tick(5 * time.Second)

	for {
		select {
		case <-timeoutChan:
			db.Model(task).Updates(map[string]interface{}{
				"status":    models.TaskStatusFailed,
				"error_msg": "任务执行超时",
				"end_time":  time.Now().Unix(),
			})
			db.Model(&agent).Update("status", models.AgentStatusOnline)
			return false, nil

		case <-tickChan:
			var currentTask models.AgentTask
			db.First(&currentTask, task.ID)

			if currentTask.Status == models.TaskStatusSuccess {
				db.Model(&agent).Update("status", models.AgentStatusOnline)
				// 构建任务输出
				taskOutputs := h.buildTaskOutputs(taskType, &currentTask)
				return true, taskOutputs
			}

			if currentTask.Status == models.TaskStatusFailed {
				db.Model(&agent).Update("status", models.AgentStatusOnline)
				return false, nil
			}
		}
	}
}

// executeEmailTask executes email notification task (Server side)
func (h *PipelineHandler) executeEmailTask(db *gorm.DB, run *models.PipelineRun, node *PipelineNode) bool {
	config := node.Config
	if config == nil {
		return true // 没有配置，视为成功
	}

	// TODO: 实现邮件发送逻辑
	// 这里可以调用现有的邮件服务
	// 目前直接返回成功

	return true
}

// buildTaskScript builds the execution script based on node type and config
func (h *PipelineHandler) buildTaskScript(node *PipelineNode, config map[string]interface{}) string {
	taskType := node.Type

	switch taskType {
	case "shell", "custom", "agent":
		// Shell/custom/agent 任务：直接使用脚本内容
		if script, ok := config["script"].(string); ok {
			return script
		}
		return ""

	case "docker":
		// Docker 任务：构建 Docker 命令
		var script strings.Builder

		imageName := ""
		if v, ok := config["image_name"].(string); ok {
			imageName = v
		}

		imageTag := "latest"
		if v, ok := config["image_tag"].(string); ok {
			imageTag = v
		}

		dockerfile := "./Dockerfile"
		if v, ok := config["dockerfile"].(string); ok {
			dockerfile = v
		}

		context := "."
		if v, ok := config["context"].(string); ok {
			context = v
		}

		script.WriteString(fmt.Sprintf("docker build -t %s:%s -f %s %s\n", imageName, imageTag, dockerfile, context))

		// 如果需要推送
		if push, ok := config["push"].(bool); ok && push {
			registry := ""
			if v, ok := config["registry"].(string); ok {
				registry = v
			}
			if registry != "" {
				script.WriteString(fmt.Sprintf("docker tag %s:%s %s/%s:%s\n", imageName, imageTag, registry, imageName, imageTag))
				script.WriteString(fmt.Sprintf("docker push %s/%s:%s\n", registry, imageName, imageTag))
			}
		}

		return script.String()

	case "git_clone":
		// Git 任务：构建 Git 命令
		var script strings.Builder

		repoURL := ""
		branch := "main"
		targetDir := ""
		depth := 0

		if repo, ok := config["repository"].(map[string]interface{}); ok {
			if url, ok := repo["url"].(string); ok {
				repoURL = url
			}
			if b, ok := repo["branch"].(string); ok {
				branch = b
			}
			if dir, ok := repo["target_dir"].(string); ok {
				targetDir = dir
			}
			if d, ok := repo["depth"].(float64); ok {
				depth = int(d)
			}
		}

		if repoURL == "" {
			return ""
		}

		// 构建 git clone 命令
		script.WriteString("set -e\n")

		// 删除已存在的目标目录（如果存在）
		if targetDir != "" {
			script.WriteString(fmt.Sprintf("rm -rf %s\n", targetDir))
			script.WriteString(fmt.Sprintf("mkdir -p %s\n", targetDir))
			script.WriteString(fmt.Sprintf("cd %s\n", targetDir))
		}

		// 执行 clone
		if depth > 0 {
			script.WriteString(fmt.Sprintf("git clone --depth %d -b %s %s .\n", depth, branch, repoURL))
		} else {
			script.WriteString(fmt.Sprintf("git clone -b %s %s .\n", branch, repoURL))
		}

		// 如果指定了 commit
		if commit, ok := config["repository"].(map[string]interface{}); ok {
			if commitID, ok := commit["commit_id"].(string); ok && commitID != "" {
				script.WriteString(fmt.Sprintf("git checkout %s\n", commitID))
			}
		}

		return script.String()

	default:
		return ""
	}
}

// buildTaskOutputs builds the output map for a completed task based on task type
func (h *PipelineHandler) buildTaskOutputs(taskType string, task *models.AgentTask) map[string]interface{} {
	outputs := map[string]interface{}{
		"status":    task.Status,
		"exit_code": task.ExitCode,
		"duration":  task.Duration,
	}

	// Parse ResultData if available
	if task.ResultData != "" {
		var resultData map[string]interface{}
		if err := json.Unmarshal([]byte(task.ResultData), &resultData); err == nil {
			for k, v := range resultData {
				outputs[k] = v
			}
		}
	}

	// Add type-specific outputs
	switch taskType {
	case "git_clone":
		outputs["url"] = task.RepoURL
		outputs["branch"] = task.RepoBranch
		outputs["commit_id"] = task.RepoCommit
		outputs["checkout_path"] = task.RepoPath

	case "shell":
		// Shell outputs are already in ResultData

	case "docker":
		// Docker outputs are already in ResultData
	}

	return outputs
}

// jsonEncode encodes map to JSON string
func (h *PipelineHandler) jsonEncode(v interface{}) string {
	data, _ := json.Marshal(v)
	return string(data)
}

// updateRunStatus updates the run status and broadcasts to frontend
func (h *PipelineHandler) updateRunStatus(runID uint64, status, errorMsg string) {
	now := time.Now().Unix()

	var run models.PipelineRun
	h.DB.First(&run, runID)

	// 计算实际 duration：基于所有任务的最大 end_time 和最小 start_time
	var maxEndTime int64 = 0
	var minStartTime int64 = 0
	var totalDuration int64 = 0
	var taskCount int64 = 0

	var tasks []models.AgentTask
	h.DB.Where("pipeline_run_id = ?", runID).Find(&tasks)

	for _, task := range tasks {
		if task.StartTime > 0 {
			if minStartTime == 0 || task.StartTime < minStartTime {
				minStartTime = task.StartTime
			}
		}
		if task.EndTime > 0 {
			if task.EndTime > maxEndTime {
				maxEndTime = task.EndTime
			}
		}
		if task.Duration > 0 {
			totalDuration += int64(task.Duration)
			taskCount++
		}
	}

	// 更新 duration：如果有任务duration，使用任务总耗时；否则使用整体时间差
	var duration int
	if taskCount > 0 && totalDuration > 0 {
		// 使用所有任务的总耗时
		duration = int(totalDuration)
	} else if minStartTime > 0 && maxEndTime > 0 {
		// 使用整体时间差
		duration = int(maxEndTime - minStartTime)
	} else if run.StartTime > 0 {
		// 使用当前时间与开始时间的差值
		duration = int(now - run.StartTime)
	}

	updates := map[string]interface{}{
		"status":   status,
		"end_time": now,
		"duration": duration,
	}

	if errorMsg != "" {
		// Store error message in stage field
		if len(errorMsg) > 64 {
			errorMsg = errorMsg[:64]
		}
		updates["stage"] = errorMsg
	}

	h.DB.Model(&run).Updates(updates)

	// Broadcast run status to frontend clients
	wsHandler := SharedWebSocketHandler()
	wsHandler.BroadcastRunStatus(runID, status, errorMsg)
}

func (h *PipelineHandler) GetPipelineRuns(c *gin.Context) {
	id := c.Param("id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))

	var runs []models.PipelineRun
	var total int64

	h.DB.Model(&models.PipelineRun{}).Where("pipeline_id = ?", id).Count(&total)

	offset := (page - 1) * pageSize
	h.DB.Where("pipeline_id = ?", id).Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&runs)

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"list":  runs,
			"total": total,
			"page":  page,
			"size":  pageSize,
		},
	})
}

func (h *PipelineHandler) ToggleFavorite(c *gin.Context) {
	id := c.Param("id")

	var pipeline models.Pipeline
	if err := h.DB.First(&pipeline, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "流水线不存在",
		})
		return
	}

	pipeline.IsFavorite = !pipeline.IsFavorite
	h.DB.Model(&pipeline).Update("is_favorite", pipeline.IsFavorite)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "操作成功",
	})
}

func (h *PipelineHandler) GetPipelineStatistics(c *gin.Context) {
	id := c.Param("id")

	var totalRuns, successfulRuns, failedRuns int64
	var avgDuration float64

	h.DB.Model(&models.PipelineRun{}).Where("pipeline_id = ?", id).Count(&totalRuns)
	h.DB.Model(&models.PipelineRun{}).Where("pipeline_id = ? AND status = ?", id, "success").Count(&successfulRuns)
	h.DB.Model(&models.PipelineRun{}).Where("pipeline_id = ? AND status = ?", id, "failed").Count(&failedRuns)

	// 计算平均耗时
	var totalDuration int64
	h.DB.Model(&models.PipelineRun{}).Where("pipeline_id = ? AND duration > 0", id).Pluck("duration", &totalDuration)
	if totalRuns > 0 {
		avgDuration = float64(totalDuration) / float64(totalRuns) / 60 // 转换为分钟
	}

	successRate := float64(0)
	if totalRuns > 0 {
		successRate = float64(successfulRuns) * 100 / float64(totalRuns)
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"total_runs":      totalRuns,
			"successful_runs": successfulRuns,
			"failed_runs":     failedRuns,
			"success_rate":    math.Round(successRate*100) / 100,
			"avg_duration":    math.Round(avgDuration*100) / 100,
		},
	})
}

func (h *PipelineHandler) GetPipelineTestReports(c *gin.Context) {
	id := c.Param("id")

	// 返回模拟的测试报告数据 (暂时忽略 id 参数)
	_ = id
	reports := []gin.H{
		{
			"id":       1,
			"name":     "单元测试",
			"total":    120,
			"passed":   115,
			"failed":   5,
			"skipped":  0,
			"duration": 120,
			"run_time": time.Now().Add(-1 * time.Hour).Format("2006-01-02 15:04:05"),
		},
		{
			"id":       2,
			"name":     "集成测试",
			"total":    30,
			"passed":   28,
			"failed":   2,
			"skipped":  0,
			"duration": 300,
			"run_time": time.Now().Add(-2 * time.Hour).Format("2006-01-02 15:04:05"),
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"list":  reports,
			"total": len(reports),
		},
	})
}

func (h *PipelineHandler) GetRunDetail(c *gin.Context) {
	id := c.Param("id")
	runID := c.Param("run_id")

	var run models.PipelineRun
	if err := h.DB.Preload("Pipeline").First(&run, runID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "运行记录不存在",
		})
		return
	}

	// 确保运行记录属于指定流水线
	if fmt.Sprintf("%d", run.PipelineID) != id {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "运行记录不存在",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": run,
	})
}

// GetRunTasks returns all tasks for a pipeline run, including nodes that haven't been executed
// If a node hasn't been executed yet but its upstream tasks failed without IgnoreFailure,
// it will be marked as "not_executed"
func (h *PipelineHandler) GetRunTasks(c *gin.Context) {
	id := c.Param("id")
	runID := c.Param("run_id")

	// 验证运行记录存在且属于指定流水线
	var run models.PipelineRun
	if err := h.DB.First(&run, runID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "运行记录不存在",
		})
		return
	}

	if fmt.Sprintf("%d", run.PipelineID) != id {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "运行记录不存在",
		})
		return
	}

	// 获取所有已执行的任务
	var tasks []models.AgentTask
	h.DB.Where("pipeline_run_id = ?", runID).Preload("Agent").Order("created_at ASC").Find(&tasks)

	// 构建 NodeID -> Task 映射
	taskMap := make(map[string]*models.AgentTask)
	for i := range tasks {
		taskMap[tasks[i].NodeID] = &tasks[i]
	}

	// 解析流水线配置
	var config PipelineConfig
	if run.Config != "" {
		if err := json.Unmarshal([]byte(run.Config), &config); err != nil {
			// 配置解析失败，返回已执行的任务
			c.JSON(http.StatusOK, gin.H{
				"code": 200,
				"data": gin.H{
					"list":  tasks,
					"total": len(tasks),
				},
			})
			return
		}
	}

	// 如果没有配置节点，返回已执行的任务
	if len(config.Nodes) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"code": 200,
			"data": gin.H{
				"list":  tasks,
				"total": len(tasks),
			},
		})
		return
	}

	// 构建依赖图：NodeID -> Upstream NodeIDs
	upstreamMap := make(map[string][]string)
	downstreamMap := make(map[string][]string) // For reverse lookup

	for _, edge := range config.getEdges() {
		upstreamMap[edge.To] = append(upstreamMap[edge.To], edge.From)
		downstreamMap[edge.From] = append(downstreamMap[edge.From], edge.To)
	}

	// 获取边的 IgnoreFailure 设置
	edgeIgnoreFailure := make(map[string]bool) // "from->to" -> ignoreFailure
	for _, edge := range config.getEdges() {
		edgeIgnoreFailure[edge.From+"->"+edge.To] = edge.IgnoreFailure
	}

	// 获取节点的 IgnoreFailure 设置
	nodeIgnoreFailure := make(map[string]bool)
	for _, node := range config.Nodes {
		nodeIgnoreFailure[node.ID] = node.IgnoreFailure
	}

	// 判断任务是否应该被跳过（基于前置任务状态）
	// 返回值: shouldSkip (true = 暂未执行), canNeverExecute (true = 因为前置任务失败且未设置IgnoreFailure)
	canNeverExecuteMap := make(map[string]bool)
	shouldSkipMap := make(map[string]bool)

	// 递归检查节点是否应该被跳过
	var checkSkip func(nodeID string, visited map[string]bool) (bool, bool)
	checkSkip = func(nodeID string, visited map[string]bool) (bool, bool) {
		if visited[nodeID] {
			return false, false // 避免循环依赖
		}
		visited[nodeID] = true

		// 如果已经有结果，直接返回
		if skip, ok := shouldSkipMap[nodeID]; ok {
			return skip, canNeverExecuteMap[nodeID]
		}

		// 获取前置任务
		upstreams := upstreamMap[nodeID]

		// 如果没有前置任务（起始节点），需要检查是否已执行
		if len(upstreams) == 0 {
			if _, exists := taskMap[nodeID]; exists {
				shouldSkipMap[nodeID] = false
				canNeverExecuteMap[nodeID] = false
				return false, false
			}
			// 起始节点未执行，可能是因为流水线刚开始或被跳过
			// 检查流水线运行状态
			if run.Status == "pending" || run.Status == "running" {
				// 流水线还在运行中，起始节点暂未执行是正常的
				shouldSkipMap[nodeID] = true
				canNeverExecuteMap[nodeID] = false
				return true, false
			}
			// 流水线已结束，起始节点未执行说明被跳过了
			shouldSkipMap[nodeID] = true
			canNeverExecuteMap[nodeID] = false
			return true, false
		}

		hasBlockingFailure := false
		for _, upstreamID := range upstreams {
			_, upstreamBlocking := checkSkip(upstreamID, visited)
			if upstreamBlocking {
				hasBlockingFailure = true
			}
		}

		edgeIgnoreFail := false
		for _, upstreamID := range upstreams {
			if edgeIgnoreFailure[upstreamID+"->"+nodeID] {
				edgeIgnoreFail = true
				break
			}
		}

		// 获取当前节点的 IgnoreFailure 设置
		nodeIgnoreFail := nodeIgnoreFailure[nodeID]

		// 判断当前节点是否可以执行
		if hasBlockingFailure && !edgeIgnoreFail && !nodeIgnoreFail {
			// 前置任务失败且未设置IgnoreFailure，当前节点无法执行
			shouldSkipMap[nodeID] = true
			canNeverExecuteMap[nodeID] = true
			return true, true
		}

		if _, exists := taskMap[nodeID]; exists {
			shouldSkipMap[nodeID] = false
			canNeverExecuteMap[nodeID] = false
			return false, false
		}

		// 任务未执行，且前置任务都跳过了
		shouldSkipMap[nodeID] = true
		canNeverExecuteMap[nodeID] = false
		return true, false
	}

	// 构建结果列表
	type TaskResponse struct {
		models.AgentTask
		DisplayStatus string `json:"display_status"` // 显示状态：包含 "暂未执行"
	}

	result := make([]TaskResponse, 0, len(config.Nodes))

	for _, node := range config.Nodes {
		tr := TaskResponse{
			DisplayStatus: "",
		}

		if task, exists := taskMap[node.ID]; exists {
			// 任务已执行，使用实际状态
			tr.AgentTask = *task
			tr.DisplayStatus = task.Status
		} else {
			// 任务未执行，判断原因
			visited := make(map[string]bool)
			shouldSkip, canNeverExecute := checkSkip(node.ID, visited)

			// 创建一个虚拟的任务对象
			tr.AgentTask = models.AgentTask{
				NodeID:   node.ID,
				Name:     node.Name,
				TaskType: node.Type,
				Status:   "not_executed",
			}

			if canNeverExecute {
				tr.DisplayStatus = "blocked" // 被阻塞（前置任务失败）
			} else if shouldSkip {
				tr.DisplayStatus = "not_executed" // 暂未执行
			} else {
				tr.DisplayStatus = "not_executed"
			}
		}

		result = append(result, tr)
	}

	// 按节点在配置中的顺序返回
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"list":  result,
			"total": len(result),
		},
	})
}

func (h *PipelineHandler) GetRunLogs(c *gin.Context) {
	id := c.Param("id")
	runID := c.Param("run_id")
	level := c.DefaultQuery("level", "")
	source := c.DefaultQuery("source", "")

	// 验证运行记录存在且属于指定流水线
	var run models.PipelineRun
	if err := h.DB.First(&run, runID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "运行记录不存在",
		})
		return
	}

	if fmt.Sprintf("%d", run.PipelineID) != id {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "运行记录不存在",
		})
		return
	}

	runIDNum, err := strconv.ParseUint(runID, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的运行ID",
		})
		return
	}

	logs, err := agentFileLogs.QueryRunLogs(runIDNum, level, source)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "读取日志失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"list":  logs,
			"total": len(logs),
		},
	})
}
