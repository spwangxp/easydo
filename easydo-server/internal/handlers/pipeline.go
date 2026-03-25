package handlers

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/smtp"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"easydo-server/internal/models"
	"easydo-server/internal/services"
	"easydo-server/pkg/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PipelineHandler struct {
	DB *gorm.DB
}

const pipelineRunTriggerTypeDeploymentRequest = "deployment_request"

func pipelineManagementQuery(db *gorm.DB, includeHidden bool) *gorm.DB {
	if includeHidden {
		return db
	}
	return db.Where("management_hidden = ?", false)
}

type pipelineTaskTypeResponse struct {
	Type            string                       `json:"type"`
	Category        string                       `json:"category"`
	ExecMode        string                       `json:"exec_mode"`
	CredentialSlots []pipelineTaskCredentialSlot `json:"credential_slots"`
}

type pipelineTaskCredentialSlot struct {
	Slot              string                      `json:"slot"`
	Label             string                      `json:"label"`
	Required          bool                        `json:"required"`
	AllowedTypes      []models.CredentialType     `json:"allowed_types"`
	AllowedCategories []models.CredentialCategory `json:"allowed_categories"`
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

func (h *PipelineHandler) GetPipelineTaskTypes(c *gin.Context) {
	keys := make([]string, 0, len(pipelineTaskDefinitions))
	for taskType := range pipelineTaskDefinitions {
		keys = append(keys, taskType)
	}
	sort.Strings(keys)

	result := make([]pipelineTaskTypeResponse, 0, len(keys))
	for _, taskType := range keys {
		def := pipelineTaskDefinitions[taskType]
		slots := make([]pipelineTaskCredentialSlot, 0, len(def.CredentialSlots))
		for _, slot := range def.CredentialSlots {
			slots = append(slots, pipelineTaskCredentialSlot{
				Slot:              slot.Slot,
				Label:             slot.Label,
				Required:          slot.Required,
				AllowedTypes:      slot.AllowedTypes,
				AllowedCategories: slot.AllowedCategories,
			})
		}
		result = append(result, pipelineTaskTypeResponse{
			Type:            def.CanonicalType,
			Category:        def.Category,
			ExecMode:        def.ExecMode,
			CredentialSlots: slots,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": result,
	})
}

type pipelineCredentialBinding struct {
	NodeID       string
	TaskType     string
	Slot         taskCredentialSlot
	CredentialID uint64
}

func (h *PipelineHandler) validatePipelineCredentialBindings(config *PipelineConfig, userID uint64, role string, pipelineID uint64, workspaceID uint64) ([]models.PipelineCredentialRef, error) {
	if config == nil || len(config.Nodes) == 0 {
		return nil, nil
	}

	refs := make([]models.PipelineCredentialRef, 0)
	bindings := make([]pipelineCredentialBinding, 0)

	for i := range config.Nodes {
		node := &config.Nodes[i]
		canonical, def, ok := getPipelineTaskDefinition(node.Type)
		if !ok {
			continue
		}

		nodeCfg := normalizePipelineNodeConfig(node.Type, canonical, node.getNodeConfig())
		node.Config = nodeCfg
		node.Params = nil
		node.Type = canonical

		if len(def.CredentialSlots) == 0 {
			continue
		}

		rawBindings, _ := nodeCfg["credentials"].(map[string]interface{})
		if rawBindings != nil {
			for key := range rawBindings {
				if _, slotOK := def.findCredentialSlot(key); !slotOK {
					return nil, fmt.Errorf("节点 '%s' 的任务类型 '%s' 不支持凭据槽位 '%s'", node.ID, canonical, key)
				}
			}
		}

		for _, slot := range def.CredentialSlots {
			var bindingRaw interface{}
			if rawBindings != nil {
				bindingRaw = rawBindings[slot.Slot]
			}

			credentialID, hasBinding := extractCredentialIDFromBinding(bindingRaw)
			if !hasBinding {
				if slot.Required {
					return nil, fmt.Errorf("节点 '%s' 缺少必填凭据槽位 '%s'", node.ID, slot.Slot)
				}
				continue
			}

			bindings = append(bindings, pipelineCredentialBinding{
				NodeID:       node.ID,
				TaskType:     canonical,
				Slot:         slot,
				CredentialID: credentialID,
			})
			refs = append(refs, models.PipelineCredentialRef{
				PipelineID:     pipelineID,
				NodeID:         node.ID,
				TaskType:       canonical,
				CredentialSlot: slot.Slot,
				CredentialID:   credentialID,
			})
		}
	}

	if len(bindings) == 0 {
		return refs, nil
	}

	credentialIDs := make([]uint64, 0, len(bindings))
	seen := make(map[uint64]struct{})
	for _, binding := range bindings {
		if _, exists := seen[binding.CredentialID]; exists {
			continue
		}
		seen[binding.CredentialID] = struct{}{}
		credentialIDs = append(credentialIDs, binding.CredentialID)
	}

	var credentials []models.Credential
	if err := applyCredentialReadScope(h.DB.Model(&models.Credential{}), userID, role).
		Where("workspace_id = ?", workspaceID).
		Where("id IN ?", credentialIDs).
		Find(&credentials).Error; err != nil {
		return nil, err
	}

	credentialMap := make(map[uint64]models.Credential, len(credentials))
	for _, credential := range credentials {
		credentialMap[credential.ID] = credential
	}

	for _, binding := range bindings {
		credential, exists := credentialMap[binding.CredentialID]
		if !exists {
			return nil, fmt.Errorf("节点 '%s' 绑定的凭据 #%d 不存在或无权限访问", binding.NodeID, binding.CredentialID)
		}
		if !credential.IsUsable() {
			return nil, fmt.Errorf("节点 '%s' 绑定的凭据 '%s' 不是可用状态", binding.NodeID, credential.Name)
		}
		if !binding.Slot.allowsType(credential.Type) {
			return nil, fmt.Errorf("节点 '%s' 的槽位 '%s' 不支持凭据类型 '%s'", binding.NodeID, binding.Slot.Slot, credential.Type)
		}
		if !binding.Slot.allowsCategory(credential.Category) {
			return nil, fmt.Errorf("节点 '%s' 的槽位 '%s' 不支持凭据分类 '%s'", binding.NodeID, binding.Slot.Slot, credential.Category)
		}
		decrypted, err := services.NewCredentialEncryptionService().DecryptCredentialData(credential.EncryptedPayload)
		if err != nil {
			return nil, fmt.Errorf("节点 '%s' 的槽位 '%s' 凭据解密失败: %w", binding.NodeID, binding.Slot.Slot, err)
		}
		if err := validateTaskCredentialPayload(binding.TaskType, binding.Slot, credential, decrypted); err != nil {
			return nil, fmt.Errorf("节点 '%s' 的槽位 '%s' %w", binding.NodeID, binding.Slot.Slot, err)
		}
	}

	return refs, nil
}

func (h *PipelineHandler) parseAndValidatePipelineConfig(rawConfig string, userID uint64, role string, pipelineID uint64, workspaceID uint64) (PipelineConfig, []models.PipelineCredentialRef, string, error) {
	var config PipelineConfig
	if err := json.Unmarshal([]byte(rawConfig), &config); err != nil {
		return PipelineConfig{}, nil, "流水线配置JSON解析失败: " + err.Error(), err
	}

	if valid, errMsg := config.ValidateDAG(); !valid {
		return PipelineConfig{}, nil, errMsg, errors.New(errMsg)
	}
	if valid, errMsg := config.ValidateTaskTypes(); !valid {
		return PipelineConfig{}, nil, errMsg, errors.New(errMsg)
	}

	refs, err := h.validatePipelineCredentialBindings(&config, userID, role, pipelineID, workspaceID)
	if err != nil {
		return PipelineConfig{}, nil, "流水线凭据配置无效: " + err.Error(), err
	}

	return config, refs, "", nil
}

func (h *PipelineHandler) replacePipelineCredentialRefs(tx *gorm.DB, pipelineID uint64, refs []models.PipelineCredentialRef) error {
	if err := tx.Where("pipeline_id = ?", pipelineID).Delete(&models.PipelineCredentialRef{}).Error; err != nil {
		return err
	}
	if len(refs) == 0 {
		return nil
	}
	for i := range refs {
		refs[i].PipelineID = pipelineID
	}
	return tx.Create(&refs).Error
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
	workspaceID := c.GetUint64("workspace_id")
	includeHidden := strings.EqualFold(strings.TrimSpace(c.Query("include_publish_owned")), "true")

	query := pipelineManagementQuery(h.DB.Model(&models.Pipeline{}), includeHidden).Where("workspace_id = ?", workspaceID)

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
	pipelineManagementQuery(h.DB.Model(&models.Pipeline{}), includeHidden).Where("workspace_id = ?", workspaceID).Count(&allCount)
	pipelineManagementQuery(h.DB.Model(&models.Pipeline{}), includeHidden).Where("workspace_id = ? AND owner_id = ?", workspaceID, userID).Count(&createdCount)
	pipelineManagementQuery(h.DB.Model(&models.Pipeline{}), includeHidden).Where("workspace_id = ? AND is_favorite = ?", workspaceID, true).Count(&favoritedCount)

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

	pipelineIDs := make([]uint64, 0, len(pipelines))
	for _, pipeline := range pipelines {
		pipelineIDs = append(pipelineIDs, pipeline.ID)
	}

	lastRunByPipeline := make(map[uint64]models.PipelineRun, len(pipelines))
	if len(pipelineIDs) > 0 {
		lastRunSubQuery := regularPipelineRunsQuery(h.DB.Model(&models.PipelineRun{})).
			Select("pipeline_id, MAX(build_number) AS max_build_number").
			Where("pipeline_id IN ?", pipelineIDs).
			Group("pipeline_id")

		var lastRuns []models.PipelineRun
		regularPipelineRunsQuery(h.DB.Model(&models.PipelineRun{})).
			Joins("JOIN (?) latest ON latest.pipeline_id = pipeline_runs.pipeline_id AND latest.max_build_number = pipeline_runs.build_number", lastRunSubQuery).
			Where("pipeline_runs.pipeline_id IN ?", pipelineIDs).
			Order("pipeline_runs.id DESC").
			Find(&lastRuns)

		for _, run := range lastRuns {
			if _, exists := lastRunByPipeline[run.PipelineID]; !exists {
				lastRunByPipeline[run.PipelineID] = run
			}
		}
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
		if lastRun, ok := lastRunByPipeline[p.ID]; ok && lastRun.ID > 0 {
			run := lastRun
			pwb.LastBuild = &run
			pwb.LatestRunner = run.TriggerUser
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

func regularPipelineRunsQuery(db *gorm.DB) *gorm.DB {
	return db.Where("(trigger_type IS NULL OR trigger_type = '' OR trigger_type <> ?)", pipelineRunTriggerTypeDeploymentRequest)
}

func (h *PipelineHandler) GetPipelineDetail(c *gin.Context) {
	id := c.Param("id")
	workspaceID := c.GetUint64("workspace_id")

	var pipeline models.Pipeline
	if err := h.DB.Preload("Owner").Preload("Project").Where("id = ? AND workspace_id = ?", id, workspaceID).First(&pipeline).Error; err != nil {
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

	userID := c.GetUint64("user_id")
	role := c.GetString("role")
	workspaceID := c.GetUint64("workspace_id")
	if !userCanWriteWorkspaceResource(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权在当前工作空间创建流水线"})
		return
	}
	if req.ProjectID != 0 && !projectBelongsToWorkspace(h.DB, req.ProjectID, workspaceID) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "项目不属于当前工作空间"})
		return
	}
	credentialRefs := make([]models.PipelineCredentialRef, 0)

	if req.Config != "" {
		config, refs, errMsg, err := h.parseAndValidatePipelineConfig(req.Config, userID, role, 0, workspaceID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": errMsg,
			})
			return
		}
		normalizedConfig, _ := json.Marshal(config)
		req.Config = string(normalizedConfig)
		credentialRefs = refs
	}

	pipeline := &models.Pipeline{
		Name:        req.Name,
		Description: req.Description,
		WorkspaceID: workspaceID,
		ProjectID:   req.ProjectID,
		Environment: req.Environment,
		Config:      req.Config,
		OwnerID:     userID,
	}

	createQuery := h.DB
	if req.ProjectID == 0 {
		createQuery = createQuery.Omit("ProjectID")
	}
	if err := createQuery.Create(pipeline).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建流水线失败",
		})
		return
	}
	if err := h.replacePipelineCredentialRefs(h.DB, pipeline.ID, credentialRefs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建流水线失败: 同步凭据引用失败: " + err.Error(),
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

	workspaceID := c.GetUint64("workspace_id")
	// 先查询流水线是否存在
	var pipeline models.Pipeline
	if err := h.DB.Where("id = ? AND workspace_id = ?", id, workspaceID).First(&pipeline).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "流水线不存在",
		})
		return
	}

	userID := c.GetUint64("user_id")
	role := c.GetString("role")
	if !userCanWriteWorkspaceResource(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权修改该流水线"})
		return
	}
	credentialRefs := make([]models.PipelineCredentialRef, 0)

	if req.Config != "" {
		config, refs, errMsg, err := h.parseAndValidatePipelineConfig(req.Config, userID, role, pipeline.ID, workspaceID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": errMsg,
			})
			return
		}
		normalizedConfig, _ := json.Marshal(config)
		req.Config = string(normalizedConfig)
		credentialRefs = refs
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

	// 仅更新显式传入字段，避免将数据库中的 NULL project_id 回写为 0
	if len(updates) > 0 {
		if err := h.DB.Model(&models.Pipeline{}).Where("id = ?", pipeline.ID).Updates(updates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "更新流水线失败: " + err.Error(),
			})
			return
		}
		if req.Config != "" {
			if err := h.replacePipelineCredentialRefs(h.DB, pipeline.ID, credentialRefs); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code":    500,
					"message": "更新流水线失败: 同步凭据引用失败: " + err.Error(),
				})
				return
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "更新成功",
	})
}

func (h *PipelineHandler) DeletePipeline(c *gin.Context) {
	id := c.Param("id")
	workspaceID := c.GetUint64("workspace_id")
	userID := c.GetUint64("user_id")
	role := c.GetString("role")
	if !userCanManageWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权删除该流水线"})
		return
	}
	var pipeline models.Pipeline
	if err := h.DB.Where("id = ? AND workspace_id = ?", id, workspaceID).First(&pipeline).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "流水线不存在"})
		return
	}

	// 先删除关联的运行记录
	h.DB.Where("pipeline_id = ?", id).Delete(&models.PipelineRun{})
	h.DB.Where("pipeline_id = ?", id).Delete(&models.PipelineCredentialRef{})

	if err := h.DB.Delete(&pipeline).Error; err != nil {
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
	workspaceID := c.GetUint64("workspace_id")

	var pipeline models.Pipeline
	if err := h.DB.Where("id = ? AND workspace_id = ?", id, workspaceID).First(&pipeline).Error; err != nil {
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

	if valid, errMsg := config.ValidateTaskTypes(); !valid {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": errMsg,
		})
		return
	}

	triggerUserID := c.GetUint64("user_id")
	triggerRole := c.GetString("role")
	triggerUsername := c.GetString("username")
	if triggerUsername == "" {
		triggerUsername = "system"
	}

	if _, err := h.validatePipelineCredentialBindings(&config, triggerUserID, triggerRole, pipeline.ID, workspaceID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "流水线凭据配置无效: " + err.Error(),
		})
		return
	}

	run, buildNumber, err := h.launchPipelineRun(pipeline, config, pipelineRunTriggerContext{
		TriggerType:     "manual",
		TriggerUser:     triggerUsername,
		TriggerUserID:   triggerUserID,
		TriggerUserRole: triggerRole,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 500,
			"msg":  "创建运行记录失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"run_id":       run.ID,
			"build_number": buildNumber,
			"status":       run.Status,
		},
	})
}

// CancelPipelineRun cancels a running pipeline run and marks non-terminal tasks as cancelled.
// Only runs in queued, pending, or running state can be cancelled.
func (h *PipelineHandler) CancelPipelineRun(c *gin.Context) {
	id := c.Param("id")
	runID := c.Param("run_id")
	workspaceID := c.GetUint64("workspace_id")

	pipelineID, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的流水线ID",
		})
		return
	}

	if !pipelineBelongsToWorkspace(h.DB, pipelineID, workspaceID) {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "流水线不存在",
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

	var run models.PipelineRun
	if err := h.DB.Where("id = ? AND pipeline_id = ? AND workspace_id = ?", runIDNum, pipelineID, workspaceID).First(&run).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "运行记录不存在",
		})
		return
	}

	switch run.Status {
	case models.PipelineRunStatusQueued, models.PipelineRunStatusPending, models.PipelineRunStatusRunning:
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": fmt.Sprintf("运行状态 '%s' 不支持取消操作", run.Status),
		})
		return
	}

	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var tasks []models.AgentTask
		if err := tx.Where("pipeline_run_id = ? AND status NOT IN ?",
			runIDNum,
			[]string{
				models.TaskStatusExecuteSuccess,
				models.TaskStatusExecuteFailed,
				models.TaskStatusScheduleFailed,
				models.TaskStatusCancelled,
			}).Find(&tasks).Error; err != nil {
			return fmt.Errorf("查询任务失败: %w", err)
		}

		now := time.Now().Unix()
		for i := range tasks {
			task := &tasks[i]
			if !models.IsTaskStatusTransitionAllowed(task.Status, models.TaskStatusCancelled) {
				continue
			}

			updates := map[string]interface{}{
				"status":   models.TaskStatusCancelled,
				"end_time": now,
			}
			if task.StartTime > 0 {
				updates["duration"] = int(now - task.StartTime)
			}

			if err := tx.Model(task).Updates(updates).Error; err != nil {
				return fmt.Errorf("更新任务 %d 状态失败: %w", task.ID, err)
			}

			task.Status = models.TaskStatusCancelled
			task.EndTime = now
			if task.StartTime > 0 {
				task.Duration = int(now - task.StartTime)
			}
			syncLiveTaskStateFromTask(task, "")

			SharedWebSocketHandler().BroadcastTaskStatus(
				runIDNum,
				task.ID,
				task.NodeID,
				models.TaskStatusCancelled,
				0,
				"任务已被取消",
				"",
			)
		}

		duration := 0
		if run.StartTime > 0 {
			duration = int(now - run.StartTime)
		}

		runUpdates := map[string]interface{}{
			"status":   models.PipelineRunStatusCancelled,
			"end_time": now,
			"duration": duration,
		}
		if err := tx.Model(&run).Updates(runUpdates).Error; err != nil {
			return fmt.Errorf("更新运行状态失败: %w", err)
		}

		return nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "取消流水线运行失败: " + err.Error(),
		})
		return
	}

	run.Status = models.PipelineRunStatusCancelled
	run.EndTime = time.Now().Unix()
	if run.StartTime > 0 {
		run.Duration = int(run.EndTime - run.StartTime)
	}
	syncLiveRunStateFromRun(&run)
	syncDeploymentStateFromRun(h.DB, &run)

	SharedWebSocketHandler().BroadcastRunStatus(runIDNum, models.PipelineRunStatusCancelled, "流水线运行已取消")

	emitPipelineRunTerminalNotification(h.DB, &run, NotificationEventTypePipelineRunCancelled)

	if run.AgentID > 0 {
		updateAgentStatusByPipelineConcurrency(h.DB, run.AgentID)
	}

	go h.scheduleQueuedPipelineRuns(h.DB)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "流水线运行已取消",
	})
}

func (h *PipelineHandler) createPipelineRunWithUniqueBuildNumber(run *models.PipelineRun) (int, error) {
	if h == nil || h.DB == nil || run == nil {
		return 0, fmt.Errorf("invalid pipeline run create context")
	}

	for attempt := 0; attempt < 5; attempt++ {
		buildNumber := 1
		err := h.DB.Transaction(func(tx *gorm.DB) error {
			var lastRun models.PipelineRun
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("pipeline_id = ?", run.PipelineID).
				Order("build_number DESC").
				First(&lastRun).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if lastRun.ID > 0 {
				buildNumber = lastRun.BuildNumber + 1
			}
			run.BuildNumber = buildNumber
			return tx.Create(run).Error
		})
		if err == nil {
			return buildNumber, nil
		}
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "unique") || strings.Contains(lower, "duplicate") {
			continue
		}
		return 0, err
	}

	return 0, fmt.Errorf("failed to allocate unique build number after retries")
}

// PipelineNode represents a node in the pipeline configuration
type PipelineNode struct {
	ID            string                 `json:"id"`
	Type          string                 `json:"type"` // git_clone/npm/maven/gradle/docker/unit/integration/e2e/coverage/lint/ssh/kubernetes/docker-run/shell/sleep/email/webhook/in_app
	Name          string                 `json:"name"`
	X             *float64               `json:"x,omitempty"`
	Y             *float64               `json:"y,omitempty"`
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
		// A directed acyclic graph must contain at least one entry node.
		// If every connected node has incoming edges, there is a cycle.
		return false, "流水线配置无效：检测到循环依赖"
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
			WorkspaceID:   run.WorkspaceID,
			PipelineRunID: run.ID,
			NodeID:        node.ID,
			TaskType:      taskType,
			Name:          node.Name,
			Params:        h.jsonEncode(nodeConfig),
			Script:        script,
			WorkDir:       workDir,
			EnvVars:       envVars,
			Status:        models.TaskStatusQueued,
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
func (h *PipelineHandler) executePipelineTasks(pipeline models.Pipeline, run *models.PipelineRun, config PipelineConfig, triggerUserID uint64, triggerRole string) {
	// 检查配置有效性
	if config.Nodes == nil || len(config.Nodes) == 0 {
		h.updateRunStatus(run.ID, models.PipelineRunStatusFailed, "流水线配置无效：节点列表为空")
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

	// 是否存在需要 Agent 执行的节点
	hasAgentNode := false
	for _, node := range config.Nodes {
		if isAgentPipelineTaskType(node.Type) {
			hasAgentNode = true
			break
		}
	}

	// 选择执行 Agent（仅当存在 Agent 任务时）
	agentID := run.AgentID
	if hasAgentNode {
		if agentID == 0 {
			agentID = h.selectAgentForPipeline(h.DB, pipeline.WorkspaceID)
		}
		if agentID == 0 {
			h.updateRunStatus(run.ID, models.PipelineRunStatusFailed, "没有可用的Agent")
			return
		}
		h.DB.Model(run).Update("agent_id", agentID)
		run.AgentID = agentID
	}

	// 找出入度为0的初始节点
	resolver := NewVariableResolver()
	envVars := BuildGlobalEnvVars(&pipeline, run)
	resolver.SetEnvVars(envVars)

	for nodeID, degree := range inDegree {
		if degree == 0 {
			// 创建初始节点任务
			node := nodeMap[nodeID]
			if node != nil {
				success, _ := h.executeNodeWithAgent(h.DB, pipeline, run, node, nodeMap, resolver, agentID, triggerUserID, triggerRole)
				if !success {
					h.updateRunStatus(run.ID, models.PipelineRunStatusFailed, "初始化任务失败（凭据权限或解析错误）")
					return
				}
				if isServerPipelineTaskType(node.Type) {
					var completedTasks []models.AgentTask
					h.DB.Where("pipeline_run_id = ?", run.ID).Find(&completedTasks)
					SharedWebSocketHandler().triggerDownstreamTasks(run.ID, completedTasks)
					SharedWebSocketHandler().checkAndUpdatePipelineStatus(run.ID)
				}
			}
		}
	}

	// 任务执行由 agent 通过 WebSocket 驱动，下游任务由 triggerDownstreamTasks 在任务完成时触发
}

func (h *PipelineHandler) selectAgentForPipeline(db *gorm.DB, workspaceID uint64) uint64 {
	return selectAgentWithPipelineCapacity(db, workspaceID)
}

func resolveTaskMaxRetries(nodeConfig map[string]interface{}) int {
	if nodeConfig == nil {
		return 0
	}

	raw, exists := nodeConfig["retry_count"]
	if !exists {
		return 0
	}

	retries := toInt(raw)
	if retries < 0 {
		return 0
	}
	return retries
}

func createAgentTaskWithExplicitMaxRetries(db *gorm.DB, task *models.AgentTask) error {
	expectedMaxRetries := task.MaxRetries

	if err := db.Create(task).Error; err != nil {
		return err
	}

	// Existing schemas may still have max_retries default=3; enforce explicit 0 when configured.
	if expectedMaxRetries == 0 {
		if err := db.Model(task).Update("max_retries", 0).Error; err != nil {
			return err
		}
		task.MaxRetries = 0
	}

	return nil
}

func (h *PipelineHandler) executeNodeWithAgent(db *gorm.DB, pipeline models.Pipeline, run *models.PipelineRun, node *PipelineNode, nodeMap map[string]*PipelineNode, resolver *VariableResolver, agentID uint64, triggerUserID uint64, triggerRole string) (bool, map[string]interface{}) {
	canonicalType, def, ok := getPipelineTaskDefinition(node.Type)
	if !ok {
		fmt.Printf("Unsupported task type: %s\n", node.Type)
		return false, nil
	}

	nodeConfig := normalizePipelineNodeConfig(node.Type, canonicalType, node.getNodeConfig())
	if resolver != nil {
		resolvedConfig, err := resolver.ResolveNodeConfig(nodeConfig)
		if err == nil {
			nodeConfig = normalizePipelineNodeConfig(node.Type, canonicalType, resolvedConfig)
		}
	}
	if err := resolveResourceBackedNodeConfig(db, canonicalType, run.WorkspaceID, nodeConfig); err != nil {
		fmt.Printf("Failed to resolve resource-backed config for node %s: %v\n", node.ID, err)
		return false, nil
	}

	if err := h.injectCredentialEnv(db, canonicalType, def, nodeConfig, run, triggerUserID, triggerRole); err != nil {
		fmt.Printf("Failed to inject credential for node %s: %v\n", node.ID, err)
		return false, nil
	}

	timeout := node.Timeout
	if timeout <= 0 {
		if v := toInt(nodeConfig["timeout"]); v > 0 {
			timeout = v
		} else {
			timeout = 3600
		}
	}
	maxRetries := resolveTaskMaxRetries(nodeConfig)

	if def.ExecMode == taskExecModeServer {
		success, errMsg := h.executeServerTask(db, run, node, canonicalType, nodeConfig, timeout)
		if !success {
			fmt.Printf("Server task failed: node=%s type=%s err=%s\n", node.ID, canonicalType, errMsg)
			return false, nil
		}
		return true, nil
	}

	if agentID == 0 {
		return false, nil
	}

	var agent models.Agent
	if err := db.First(&agent, agentID).Error; err != nil {
		return false, nil
	}
	if agent.Status != models.AgentStatusOnline && agent.Status != models.AgentStatusBusy {
		return false, nil
	}

	_, script, err := renderPipelineAgentScript(canonicalType, nodeConfig)
	if err != nil {
		fmt.Printf("Failed to render task script for node %s: %v\n", node.ID, err)
		return false, nil
	}
	if resolver != nil && strings.TrimSpace(script) != "" {
		resolvedScript, resolveErr := resolver.ResolveVariables(script)
		if resolveErr == nil {
			script = resolvedScript
		}
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

	repoURL, repoBranch, repoCommit, repoPath := "", "", "", ""
	if canonicalType == "git_clone" {
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

	var task models.AgentTask
	result := db.Where("pipeline_run_id = ? AND node_id = ?", run.ID, node.ID).First(&task)
	if result.Error != nil {
		task = models.AgentTask{
			AgentID:       agentID,
			WorkspaceID:   run.WorkspaceID,
			PipelineRunID: run.ID,
			NodeID:        node.ID,
			TaskType:      canonicalType,
			Name:          node.Name,
			Params:        h.jsonEncode(nodeConfig),
			Script:        script,
			WorkDir:       workDir,
			EnvVars:       envVars,
			Status:        models.TaskStatusQueued,
			Timeout:       timeout,
			MaxRetries:    maxRetries,
			RepoURL:       repoURL,
			RepoBranch:    repoBranch,
			RepoCommit:    repoCommit,
			RepoPath:      repoPath,
		}
		if err := createAgentTaskWithExplicitMaxRetries(db, &task); err != nil {
			fmt.Printf("Failed to create task for node %s: %v\n", node.ID, err)
			return false, nil
		}
	} else {
		task.AgentID = agentID
		task.TaskType = canonicalType
		task.Params = h.jsonEncode(nodeConfig)
		task.Script = script
		task.WorkDir = workDir
		task.EnvVars = envVars
		task.Timeout = timeout
		task.Status = models.TaskStatusQueued
		task.MaxRetries = maxRetries
		task.RepoURL = repoURL
		task.RepoBranch = repoBranch
		task.RepoCommit = repoCommit
		task.RepoPath = repoPath
		if err := db.Save(&task).Error; err != nil {
			fmt.Printf("Failed to update task %d: %v\n", task.ID, err)
			return false, nil
		}
	}

	_ = SharedWebSocketHandler().sendTaskAssign(task)
	return true, nil
}

// executeNode executes a single node
// Returns (success, taskOutputs) - success indicates if execution was successful,
// taskOutputs contains the outputs generated by the task for downstream tasks
func (h *PipelineHandler) executeNode(db *gorm.DB, pipeline models.Pipeline, run *models.PipelineRun, node *PipelineNode, nodeMap map[string]*PipelineNode, resolver *VariableResolver) (bool, map[string]interface{}) {
	agentID := uint64(0)
	if isAgentPipelineTaskType(node.Type) {
		agentID = h.selectAgentForPipeline(db, pipeline.WorkspaceID)
		if agentID == 0 {
			return false, nil
		}
	}
	return h.executeNodeWithAgent(db, pipeline, run, node, nodeMap, resolver, agentID, 0, "")
}

func parseCredentialID(v interface{}) (uint64, bool) {
	switch val := v.(type) {
	case float64:
		if val <= 0 {
			return 0, false
		}
		return uint64(val), true
	case int:
		if val <= 0 {
			return 0, false
		}
		return uint64(val), true
	case int64:
		if val <= 0 {
			return 0, false
		}
		return uint64(val), true
	case uint64:
		if val == 0 {
			return 0, false
		}
		return val, true
	case string:
		if strings.TrimSpace(val) == "" {
			return 0, false
		}
		id, err := strconv.ParseUint(val, 10, 64)
		if err != nil || id == 0 {
			return 0, false
		}
		return id, true
	default:
		return 0, false
	}
}

func sanitizeEnvKey(key string) string {
	normalized := strings.ToUpper(strings.TrimSpace(key))
	re := regexp.MustCompile(`[^A-Z0-9]+`)
	normalized = re.ReplaceAllString(normalized, "_")
	normalized = strings.Trim(normalized, "_")
	return normalized
}

func extractCredentialIDFromBinding(raw interface{}) (uint64, bool) {
	if raw == nil {
		return 0, false
	}

	if id, ok := parseCredentialID(raw); ok {
		return id, true
	}

	binding, ok := raw.(map[string]interface{})
	if !ok {
		return 0, false
	}
	return parseCredentialID(binding["credential_id"])
}

func slotEnvPrefix(slot string) string {
	slotKey := sanitizeEnvKey(slot)
	if slotKey == "" {
		slotKey = "CREDENTIAL"
	}
	return "EASYDO_CRED_" + slotKey + "_"
}

func mergeNodeEnv(nodeConfig map[string]interface{}) map[string]interface{} {
	envMap := make(map[string]interface{})
	if existing, ok := nodeConfig["env"].(map[string]interface{}); ok {
		for k, v := range existing {
			envMap[k] = v
		}
	}
	return envMap
}

func pickCredentialSecretValue(secrets map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		value, exists := secrets[key]
		if !exists || value == nil {
			continue
		}
		if val := strings.TrimSpace(convertToString(value)); val != "" && val != "null" {
			return val
		}
	}
	return ""
}

func pickCredentialBoolValue(secrets map[string]interface{}, keys ...string) (bool, bool) {
	for _, key := range keys {
		value, exists := secrets[key]
		if !exists || value == nil {
			continue
		}
		switch v := value.(type) {
		case bool:
			return v, true
		case string:
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "1", "true", "yes", "y", "on":
				return true, true
			case "0", "false", "no", "n", "off":
				return false, true
			}
		case float64:
			return v != 0, true
		case int:
			return v != 0, true
		case int64:
			return v != 0, true
		}
	}
	return false, false
}

func ensureHeadersMap(config map[string]interface{}) map[string]interface{} {
	if headers, ok := config["headers"].(map[string]interface{}); ok {
		return headers
	}
	headers := make(map[string]interface{})
	config["headers"] = headers
	return headers
}

func applyServerCredentialConfig(taskType string, slot taskCredentialSlot, credential models.Credential, decrypted map[string]interface{}, nodeConfig map[string]interface{}) {
	switch taskType {
	case "email":
		if slot.Slot != "smtp_auth" {
			return
		}

		if strings.TrimSpace(toString(nodeConfig["smtp_username"])) == "" {
			nodeConfig["smtp_username"] = pickCredentialSecretValue(decrypted, "username", "client_id")
		}
		if strings.TrimSpace(toString(nodeConfig["smtp_password"])) == "" {
			nodeConfig["smtp_password"] = pickCredentialSecretValue(decrypted, "password", "token", "client_secret", "secret_access_key")
		}

	case "webhook":
		if slot.Slot == "webhook_mtls" {
			if strings.TrimSpace(toString(nodeConfig["tls_client_cert"])) == "" {
				nodeConfig["tls_client_cert"] = pickCredentialSecretValue(decrypted, "cert_pem", "client_cert", "certificate")
			}
			if strings.TrimSpace(toString(nodeConfig["tls_client_key"])) == "" {
				nodeConfig["tls_client_key"] = pickCredentialSecretValue(decrypted, "key_pem", "private_key", "client_key")
			}
			if strings.TrimSpace(toString(nodeConfig["tls_ca_cert"])) == "" {
				nodeConfig["tls_ca_cert"] = pickCredentialSecretValue(decrypted, "ca_cert", "ca_bundle", "ca")
			}
			if strings.TrimSpace(toString(nodeConfig["tls_server_name"])) == "" {
				nodeConfig["tls_server_name"] = pickCredentialSecretValue(decrypted, "server_name", "tls_server_name")
			}
			if _, exists := nodeConfig["tls_insecure_skip_verify"]; !exists {
				if value, ok := pickCredentialBoolValue(decrypted, "insecure_skip_verify", "skip_verify"); ok {
					nodeConfig["tls_insecure_skip_verify"] = value
				}
			}
			return
		}

		if slot.Slot != "webhook_auth" {
			return
		}

		headers := ensureHeadersMap(nodeConfig)
		if current := strings.TrimSpace(toString(headers["Authorization"])); current != "" {
			return
		}

		switch credential.Type {
		case models.TypeToken:
			token := pickCredentialSecretValue(decrypted, "token", "access_token")
			if token == "" {
				return
			}
			tokenType := strings.ToLower(strings.TrimSpace(pickCredentialSecretValue(decrypted, "token_type")))
			if tokenType == "basic" {
				username := pickCredentialSecretValue(decrypted, "username", "client_id")
				raw := username + ":" + token
				headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(raw))
				return
			}
			headers["Authorization"] = "Bearer " + token

		case models.TypePassword:
			username := pickCredentialSecretValue(decrypted, "username")
			password := pickCredentialSecretValue(decrypted, "password")
			if username == "" || password == "" {
				return
			}
			raw := username + ":" + password
			headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(raw))

		case models.TypeOAuth2:
			token := pickCredentialSecretValue(decrypted, "access_token")
			if token != "" {
				headers["Authorization"] = "Bearer " + token
			}
		}
	}
}

func validateTaskCredentialPayload(taskType string, slot taskCredentialSlot, credential models.Credential, decrypted map[string]interface{}) error {
	missing := func(fields ...string) error {
		return fmt.Errorf("missing required payload for credential type '%s': %s", credential.Type, strings.Join(fields, ", "))
	}
	hasAny := func(keys ...string) bool {
		return pickCredentialSecretValue(decrypted, keys...) != ""
	}
	requireAll := func(keys ...string) error {
		missingKeys := make([]string, 0)
		for _, key := range keys {
			if !hasAny(key) {
				missingKeys = append(missingKeys, key)
			}
		}
		if len(missingKeys) > 0 {
			return missing(missingKeys...)
		}
		return nil
	}

	switch taskType {
	case "git_clone":
		if slot.Slot != "repo_auth" {
			return nil
		}
		switch credential.Type {
		case models.TypeSSHKey:
			return requireAll("private_key")
		case models.TypeToken:
			if !hasAny("token", "access_token") {
				return missing("token")
			}
		case models.TypePassword:
			return requireAll("username", "password")
		}

	case "docker", "docker-run":
		switch slot.Slot {
		case "registry_auth":
			switch credential.Type {
			case models.TypeToken:
				if !hasAny("token", "access_token") {
					return missing("token")
				}
			case models.TypePassword:
				return requireAll("username", "password")
			}
		case "ssh_auth":
			switch credential.Type {
			case models.TypeSSHKey:
				return requireAll("private_key")
			case models.TypePassword:
				return requireAll("username", "password")
			}
		default:
			return nil
		}

	case "ssh":
		if slot.Slot != "ssh_auth" {
			return nil
		}
		switch credential.Type {
		case models.TypeSSHKey:
			return requireAll("private_key")
		case models.TypePassword:
			return requireAll("username", "password")
		}

	case "kubernetes":
		if slot.Slot != "cluster_auth" {
			return nil
		}
		if hasAny("kubeconfig") {
			return nil
		}
		switch credential.Type {
		case models.TypeToken:
			if !hasAny("server", "api_server") || !hasAny("token", "access_token") {
				return missing("server", "token")
			}
		case models.TypeCert:
			if !hasAny("server", "api_server") || !hasAny("cert_pem") || !hasAny("key_pem") {
				return missing("server", "cert_pem", "key_pem")
			}
		}

	case "email":
		if slot.Slot != "smtp_auth" {
			return nil
		}
		switch credential.Type {
		case models.TypePassword:
			return requireAll("username", "password")
		case models.TypeToken:
			if !hasAny("username", "client_id") || !hasAny("token", "access_token", "client_secret") {
				return missing("username", "token")
			}
		}

	case "webhook":
		switch slot.Slot {
		case "webhook_auth":
			switch credential.Type {
			case models.TypeToken:
				if !hasAny("token", "access_token") {
					return missing("token")
				}
			case models.TypePassword:
				return requireAll("username", "password")
			case models.TypeOAuth2:
				if !hasAny("access_token") {
					return missing("access_token")
				}
			}
		case "webhook_mtls":
			if credential.Type == models.TypeCert {
				return requireAll("cert_pem", "key_pem")
			}
		}
	}

	return nil
}

func resolveResourceBackedNodeConfig(db *gorm.DB, canonicalType string, workspaceID uint64, nodeConfig map[string]interface{}) error {
	if db == nil || nodeConfig == nil || canonicalType != "docker-run" {
		return nil
	}
	resourceID := toUint64Value(nodeConfig["target_resource_id"])
	if resourceID == 0 {
		return nil
	}
	var resource models.Resource
	if err := db.Where("workspace_id = ?", workspaceID).First(&resource, resourceID).Error; err != nil {
		return fmt.Errorf("target resource not found: %w", err)
	}
	if resource.Type != models.ResourceTypeVM {
		return fmt.Errorf("target resource %d is not a VM resource", resource.ID)
	}
	host, port := parseEndpointHostPort(resource.Endpoint)
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("target VM resource %d endpoint is empty", resource.ID)
	}
	nodeConfig["host"] = host
	if strings.TrimSpace(port) != "" {
		nodeConfig["port"] = port
	} else if toInt(nodeConfig["port"]) <= 0 {
		nodeConfig["port"] = 22
	}

	var bindings []models.ResourceCredentialBinding
	if err := db.Where("workspace_id = ? AND resource_id = ?", workspaceID, resource.ID).Order("created_at ASC, id ASC").Find(&bindings).Error; err != nil {
		return err
	}
	if len(bindings) == 0 {
		return nil
	}
	bindingByPurpose := make(map[string]uint64, len(bindings))
	for _, binding := range bindings {
		if binding.CredentialID == 0 {
			continue
		}
		if _, exists := bindingByPurpose[binding.Purpose]; !exists {
			bindingByPurpose[binding.Purpose] = binding.CredentialID
		}
	}
	resourceCredentialID := deploymentResourceCredentialID(resource.Type, bindingByPurpose)
	if resourceCredentialID == 0 {
		return nil
	}
	credentials, _ := nodeConfig["credentials"].(map[string]interface{})
	if credentials == nil {
		credentials = make(map[string]interface{})
		nodeConfig["credentials"] = credentials
	}
	credentials["ssh_auth"] = map[string]interface{}{"credential_id": resourceCredentialID}
	return nil
}

func toUint64Value(v interface{}) uint64 {
	switch val := v.(type) {
	case uint64:
		return val
	case uint:
		return uint64(val)
	case uint32:
		return uint64(val)
	case int:
		if val > 0 {
			return uint64(val)
		}
	case int64:
		if val > 0 {
			return uint64(val)
		}
	case float64:
		if val > 0 {
			return uint64(val)
		}
	case json.Number:
		i, _ := val.Int64()
		if i > 0 {
			return uint64(i)
		}
	case string:
		parsed, err := strconv.ParseUint(strings.TrimSpace(val), 10, 64)
		if err == nil {
			return parsed
		}
	}
	return 0
}

func (h *PipelineHandler) injectCredentialEnv(db *gorm.DB, canonicalType string, def pipelineTaskDefinition, nodeConfig map[string]interface{}, run *models.PipelineRun, userID uint64, role string) error {
	if nodeConfig == nil || len(def.CredentialSlots) == 0 {
		return nil
	}

	rawBindings, _ := nodeConfig["credentials"].(map[string]interface{})
	injectEnv := def.ExecMode == taskExecModeAgent
	var envMap map[string]interface{}
	if injectEnv {
		envMap = mergeNodeEnv(nodeConfig)
	}

	for _, slot := range def.CredentialSlots {
		var bindingRaw interface{}
		if rawBindings != nil {
			bindingRaw = rawBindings[slot.Slot]
		}

		credentialID, hasBinding := extractCredentialIDFromBinding(bindingRaw)
		if !hasBinding {
			if slot.Required {
				return fmt.Errorf("credential slot '%s' is required", slot.Slot)
			}
			continue
		}

		var credential models.Credential
		if err := db.First(&credential, credentialID).Error; err != nil {
			return fmt.Errorf("credential not found for slot '%s': %w", slot.Slot, err)
		}
		if !canReadCredential(db, &credential, userID, role) && !canUseDeploymentBoundCredential(db, &credential, run, slot.Slot) {
			return fmt.Errorf("access denied for credential slot '%s'", slot.Slot)
		}
		if !credential.IsUsable() {
			return fmt.Errorf("credential in slot '%s' is not active", slot.Slot)
		}

		if !slot.allowsType(credential.Type) {
			return fmt.Errorf("credential type '%s' is not allowed for slot '%s'", credential.Type, slot.Slot)
		}
		if !slot.allowsCategory(credential.Category) {
			return fmt.Errorf("credential category '%s' is not allowed for slot '%s'", credential.Category, slot.Slot)
		}

		decrypted, err := services.NewCredentialEncryptionService().
			DecryptCredentialData(credential.EncryptedPayload)
		if err != nil {
			return fmt.Errorf("failed to decrypt credential in slot '%s': %w", slot.Slot, err)
		}
		if err := validateTaskCredentialPayload(canonicalType, slot, credential, decrypted); err != nil {
			return fmt.Errorf("slot '%s' %w", slot.Slot, err)
		}

		if injectEnv {
			prefix := slotEnvPrefix(slot.Slot)
			credentialJSON, _ := json.Marshal(decrypted)
			envMap[prefix+"ID"] = strconv.FormatUint(credential.ID, 10)
			envMap[prefix+"TYPE"] = string(credential.Type)
			envMap[prefix+"CATEGORY"] = string(credential.Category)
			envMap[prefix+"JSON"] = string(credentialJSON)

			for k, v := range decrypted {
				envKey := sanitizeEnvKey(k)
				if envKey == "" {
					continue
				}
				envMap[prefix+envKey] = convertToString(v)
			}
		}

		applyServerCredentialConfig(canonicalType, slot, credential, decrypted, nodeConfig)

		now := time.Now().Unix()
		db.Model(&credential).Updates(map[string]interface{}{
			"last_used_at": now,
			"used_count":   credential.UsedCount + 1,
		})
		detailJSON, _ := json.Marshal(map[string]interface{}{
			"task_type": slot.Slot,
			"run_id":    run.ID,
			"node_slot": slot.Slot,
		})
		db.Create(&models.CredentialEvent{
			CredentialID: credential.ID,
			Action:       models.CredentialEventUsed,
			ActorType:    "pipeline_run",
			ActorID:      run.ID,
			Result:       "success",
			DetailJSON:   string(detailJSON),
		})
	}

	if injectEnv && len(envMap) > 0 {
		nodeConfig["env"] = envMap
	}
	return nil
}

func (h *PipelineHandler) executeServerTask(db *gorm.DB, run *models.PipelineRun, node *PipelineNode, canonicalType string, nodeConfig map[string]interface{}, timeout int) (bool, string) {
	start := time.Now().Unix()
	serverTaskAgentID := h.resolveServerTaskAgentID(db, run)
	if serverTaskAgentID == 0 {
		return false, "无法分配服务端任务执行记录"
	}

	var task models.AgentTask
	result := db.Where("pipeline_run_id = ? AND node_id = ?", run.ID, node.ID).First(&task)
	if result.Error != nil {
		task = models.AgentTask{
			AgentID:       serverTaskAgentID,
			WorkspaceID:   run.WorkspaceID,
			PipelineRunID: run.ID,
			NodeID:        node.ID,
			TaskType:      canonicalType,
			Name:          node.Name,
			Params:        h.jsonEncode(nodeConfig),
			Status:        models.TaskStatusRunning,
			StartTime:     start,
			Timeout:       timeout,
			MaxRetries:    0,
		}
		if err := createAgentTaskWithExplicitMaxRetries(db, &task); err != nil {
			return false, "创建服务端任务失败: " + err.Error()
		}
	} else {
		task.AgentID = serverTaskAgentID
		task.TaskType = canonicalType
		task.Params = h.jsonEncode(nodeConfig)
		task.Status = models.TaskStatusRunning
		task.StartTime = start
		task.EndTime = 0
		task.ErrorMsg = ""
		task.Timeout = timeout
		if err := db.Save(&task).Error; err != nil {
			return false, "更新服务端任务失败: " + err.Error()
		}
	}
	syncLiveTaskStateFromTask(&task, "")
	SharedWebSocketHandler().BroadcastTaskStatus(run.ID, task.ID, task.NodeID, models.TaskStatusRunning, 0, "", "")
	logger := newTaskProcessLogger(task)
	logger.Step(fmt.Sprintf("开始执行 %s 任务", canonicalType))

	success := false
	errMsg := ""
	switch canonicalType {
	case "email":
		success, errMsg = h.executeEmailTask(logger, nodeConfig)
	case "webhook":
		success, errMsg = h.executeWebhookTask(logger, nodeConfig)
	case "in_app":
		success, errMsg = h.executeInAppTask(logger, db, run, node, nodeConfig)
	default:
		errMsg = "不支持的服务端任务类型"
	}

	end := time.Now().Unix()
	duration := int(end - start)
	updates := map[string]interface{}{
		"end_time":  end,
		"duration":  duration,
		"status":    models.TaskStatusExecuteSuccess,
		"error_msg": "",
	}
	if !success {
		updates["status"] = models.TaskStatusExecuteFailed
		updates["error_msg"] = errMsg
		logger.Error(errMsg)
	} else {
		logger.Info(fmt.Sprintf("任务执行完成 duration=%ds", duration))
	}
	db.Model(&task).Updates(updates)
	task.EndTime = end
	task.Duration = duration
	task.ErrorMsg = errMsg
	if success {
		task.Status = models.TaskStatusExecuteSuccess
	} else {
		task.Status = models.TaskStatusExecuteFailed
	}
	syncLiveTaskStateFromTask(&task, "")
	_ = agentFileLogs.FinishTask(task.ID, task.RetryCount+1)
	SharedWebSocketHandler().BroadcastTaskStatus(run.ID, task.ID, task.NodeID, updates["status"].(string), 0, errMsg, "")

	return success, errMsg
}

func (h *PipelineHandler) resolveServerTaskAgentID(db *gorm.DB, run *models.PipelineRun) uint64 {
	if run != nil && run.AgentID > 0 {
		return run.AgentID
	}

	var existing models.Agent
	if err := db.Select("id").Order("id ASC").First(&existing).Error; err == nil && existing.ID > 0 {
		return existing.ID
	}

	systemAgent := models.Agent{
		Name:               "__server_task__",
		Host:               "server.internal",
		Port:               0,
		Token:              "__server_task__",
		Status:             models.AgentStatusOffline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
		ApprovedAt:         time.Now().Unix(),
		LastHeartAt:        time.Now().Unix(),
		HeartbeatInterval:  10,
	}
	if err := db.Where("name = ? AND host = ?", systemAgent.Name, systemAgent.Host).FirstOrCreate(&systemAgent).Error; err != nil {
		return 0
	}
	return systemAgent.ID
}

// executeEmailTask executes email notification task (Server side)
func (h *PipelineHandler) executeEmailTask(logger *taskProcessLogger, config map[string]interface{}) (bool, string) {
	toList := parseCommaSeparatedList(toString(config["to"]))
	ccList := parseCommaSeparatedList(toString(config["cc"]))
	recipients := append([]string{}, toList...)
	recipients = append(recipients, ccList...)
	if len(recipients) == 0 {
		return false, "email.to 不能为空"
	}

	subject := toString(config["subject"])
	if strings.TrimSpace(subject) == "" {
		subject = "EasyDo 流水线通知"
	}
	body := toString(config["body"])
	bodyType := strings.ToLower(strings.TrimSpace(toString(config["body_type"])))
	if bodyType != "html" {
		bodyType = "text"
	}

	smtpHost := strings.TrimSpace(toString(config["smtp_host"]))
	if smtpHost == "" {
		return false, "smtp_host 不能为空"
	}
	smtpPort := toInt(config["smtp_port"])
	if smtpPort <= 0 {
		smtpPort = 25
	}
	username := strings.TrimSpace(toString(config["smtp_username"]))
	password := toString(config["smtp_password"])
	from := strings.TrimSpace(toString(config["from"]))
	if from == "" {
		from = username
	}
	if from == "" {
		return false, "from 不能为空"
	}
	logger.Command(fmt.Sprintf("smtp send host=%s port=%d recipients=%d from=%s subject=%s", smtpHost, smtpPort, len(recipients), from, subject))

	contentType := "text/plain; charset=UTF-8"
	if bodyType == "html" {
		contentType = "text/html; charset=UTF-8"
	}

	msg := bytes.NewBuffer(nil)
	msg.WriteString("From: " + from + "\r\n")
	msg.WriteString("To: " + strings.Join(toList, ",") + "\r\n")
	if len(ccList) > 0 {
		msg.WriteString("Cc: " + strings.Join(ccList, ",") + "\r\n")
	}
	msg.WriteString("Subject: " + subject + "\r\n")
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: " + contentType + "\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(body)

	addr := net.JoinHostPort(smtpHost, strconv.Itoa(smtpPort))
	var auth smtp.Auth
	if username != "" {
		auth = smtp.PlainAuth("", username, password, smtpHost)
	}
	if err := smtp.SendMail(addr, auth, from, recipients, msg.Bytes()); err != nil {
		return false, "邮件发送失败: " + err.Error()
	}
	logger.Info(fmt.Sprintf("邮件发送成功 recipients=%d", len(recipients)))
	return true, ""
}

func (h *PipelineHandler) executeWebhookTask(logger *taskProcessLogger, config map[string]interface{}) (bool, string) {
	url := strings.TrimSpace(toString(config["url"]))
	if url == "" {
		return false, "webhook.url 不能为空"
	}

	method := strings.ToUpper(strings.TrimSpace(toString(config["method"])))
	if method == "" {
		method = http.MethodPost
	}

	timeout := toInt(config["timeout"])
	if timeout <= 0 {
		timeout = 10
	}
	logger.Command(fmt.Sprintf("webhook %s %s timeout=%ds", method, sanitizeTaskLogPreview(url, 400), timeout))

	var payload []byte
	bodyVal := config["body"]
	switch v := bodyVal.(type) {
	case string:
		body := strings.TrimSpace(v)
		if body == "" {
			payload = []byte(`{}`)
		} else if json.Valid([]byte(body)) {
			payload = []byte(body)
		} else {
			payload, _ = json.Marshal(map[string]interface{}{"message": body})
		}
	case map[string]interface{}, []interface{}:
		payload, _ = json.Marshal(v)
	default:
		payload = []byte(`{}`)
	}
	logger.Info("request_body=" + sanitizeTaskLogPreview(string(payload), 600))

	req, err := http.NewRequest(method, url, bytes.NewReader(payload))
	if err != nil {
		return false, "构造 webhook 请求失败: " + err.Error()
	}
	req.Header.Set("Content-Type", "application/json")

	if headersMap, ok := config["headers"].(map[string]interface{}); ok {
		for k, v := range headersMap {
			key := strings.TrimSpace(k)
			if key == "" {
				continue
			}
			req.Header.Set(key, toString(v))
		}
	} else if headersJSON := strings.TrimSpace(toString(config["headers_json"])); headersJSON != "" {
		var headers map[string]string
		if err := json.Unmarshal([]byte(headersJSON), &headers); err == nil {
			for k, v := range headers {
				key := strings.TrimSpace(k)
				if key == "" {
					continue
				}
				req.Header.Set(key, v)
			}
		}
	}
	logger.Info("request_headers=" + sanitizeTaskLogPreview(fmt.Sprintf("%v", req.Header), 600))

	tlsConfig, err := buildWebhookTLSConfig(config)
	if err != nil {
		return false, "webhook TLS 配置无效: " + err.Error()
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if tlsConfig != nil {
		transport.TLSClientConfig = tlsConfig
	}

	client := &http.Client{
		Timeout:   time.Duration(timeout) * time.Second,
		Transport: transport,
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, "webhook 调用失败: " + err.Error()
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	logger.Info(fmt.Sprintf("response_status=%d", resp.StatusCode))
	logger.Info("response_body=" + sanitizeTaskLogPreview(string(respBody), 600))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Sprintf("webhook 响应失败: status=%d body=%s", resp.StatusCode, string(respBody))
	}
	return true, ""
}

func buildWebhookTLSConfig(config map[string]interface{}) (*tls.Config, error) {
	clientCertPEM := strings.TrimSpace(toString(config["tls_client_cert"]))
	clientKeyPEM := strings.TrimSpace(toString(config["tls_client_key"]))
	caCertPEM := strings.TrimSpace(toString(config["tls_ca_cert"]))
	serverName := strings.TrimSpace(toString(config["tls_server_name"]))
	insecureSkip := false
	switch v := config["tls_insecure_skip_verify"].(type) {
	case bool:
		insecureSkip = v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "y", "on":
			insecureSkip = true
		}
	case float64:
		insecureSkip = v != 0
	case int:
		insecureSkip = v != 0
	case int64:
		insecureSkip = v != 0
	}

	if clientCertPEM == "" && clientKeyPEM != "" {
		return nil, fmt.Errorf("tls_client_key provided without tls_client_cert")
	}
	if clientCertPEM != "" && clientKeyPEM == "" {
		return nil, fmt.Errorf("tls_client_cert provided without tls_client_key")
	}

	if clientCertPEM == "" && caCertPEM == "" && serverName == "" && !insecureSkip {
		return nil, nil
	}

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}
	if insecureSkip {
		tlsConfig.InsecureSkipVerify = true
	}
	if serverName != "" {
		tlsConfig.ServerName = serverName
	}

	if caCertPEM != "" {
		rootCAs, err := x509.SystemCertPool()
		if err != nil || rootCAs == nil {
			rootCAs = x509.NewCertPool()
		}
		if ok := rootCAs.AppendCertsFromPEM([]byte(caCertPEM)); !ok {
			return nil, fmt.Errorf("invalid tls_ca_cert PEM")
		}
		tlsConfig.RootCAs = rootCAs
	}

	if clientCertPEM != "" && clientKeyPEM != "" {
		certificate, err := tls.X509KeyPair([]byte(clientCertPEM), []byte(clientKeyPEM))
		if err != nil {
			return nil, fmt.Errorf("invalid client certificate pair: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}

	return tlsConfig, nil
}

func (h *PipelineHandler) executeInAppTask(logger *taskProcessLogger, db *gorm.DB, run *models.PipelineRun, node *PipelineNode, config map[string]interface{}) (bool, string) {
	title := strings.TrimSpace(toString(config["title"]))
	if title == "" {
		title = "流水线站内信通知"
	}
	content := strings.TrimSpace(toString(config["content"]))
	if content == "" {
		content = fmt.Sprintf("流水线运行 #%d 的节点 %s 已触发站内信通知", run.ID, node.Name)
	}
	priority := toInt(config["priority"])

	metadata := map[string]interface{}{
		"pipeline_run_id": run.ID,
		"node_id":         node.ID,
		"node_name":       node.Name,
	}
	if customMetadata := strings.TrimSpace(toString(config["metadata_json"])); customMetadata != "" {
		var merged map[string]interface{}
		if err := json.Unmarshal([]byte(customMetadata), &merged); err == nil {
			for k, v := range merged {
				metadata[k] = v
			}
		}
	}
	logger.Command(fmt.Sprintf("in_app notify recipient=%d title=%s", run.TriggerUserID, title))
	logger.Info("message_preview=" + sanitizeTaskLogPreview(content, 400))

	if run.TriggerUserID == 0 {
		return true, ""
	}
	metadata["priority"] = priority
	if err := emitSystemInboxNotification(db, run.WorkspaceID, run.TriggerUserID, title, content, metadata, fmt.Sprintf("pipeline-in-app-node:%d:%s", run.ID, node.ID)); err != nil {
		return false, "站内信创建失败: " + err.Error()
	}
	logger.Info("站内信创建成功")
	return true, ""
}

func parseCommaSeparatedList(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r'
	})
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		item := strings.TrimSpace(p)
		if item == "" {
			continue
		}
		result = append(result, item)
	}
	return result
}

func toString(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case json.Number:
		return val.String()
	case fmt.Stringer:
		return val.String()
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", val)
	}
}

// buildTaskScript builds the execution script based on node type and config
func (h *PipelineHandler) buildTaskScript(node *PipelineNode, config map[string]interface{}) string {
	_, script, err := renderPipelineAgentScript(node.Type, config)
	if err != nil {
		return ""
	}
	return script
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
		updates["error_msg"] = errorMsg
	}

	h.DB.Model(&run).Updates(updates)
	run.Status = status
	run.EndTime = now
	run.Duration = duration
	run.ErrorMsg = errorMsg
	syncLiveRunStateFromRun(&run)
	syncDeploymentStateFromRun(h.DB, &run)

	// Broadcast run status to frontend clients
	wsHandler := SharedWebSocketHandler()
	wsHandler.BroadcastRunStatus(runID, status, errorMsg)

	if status == models.PipelineRunStatusSuccess || status == models.PipelineRunStatusFailed || status == models.PipelineRunStatusCancelled {
		switch status {
		case models.PipelineRunStatusSuccess:
			emitPipelineRunTerminalNotification(h.DB, &run, NotificationEventTypePipelineRunSucceeded)
		case models.PipelineRunStatusFailed:
			emitPipelineRunTerminalNotification(h.DB, &run, NotificationEventTypePipelineRunFailed)
		case models.PipelineRunStatusCancelled:
			emitPipelineRunTerminalNotification(h.DB, &run, NotificationEventTypePipelineRunCancelled)
		}
		if run.AgentID > 0 {
			updateAgentStatusByPipelineConcurrency(h.DB, run.AgentID)
		}
		go h.scheduleQueuedPipelineRuns(h.DB)
	}
}

func (h *PipelineHandler) GetPipelineRuns(c *gin.Context) {
	id := c.Param("id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	workspaceID := c.GetUint64("workspace_id")
	pipelineID, _ := strconv.ParseUint(id, 10, 64)
	if !pipelineBelongsToWorkspace(h.DB, pipelineID, workspaceID) {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "流水线不存在"})
		return
	}

	var runs []models.PipelineRun
	var total int64

	regularPipelineRunsQuery(h.DB.Model(&models.PipelineRun{})).Where("workspace_id = ? AND pipeline_id = ?", workspaceID, pipelineID).Count(&total)

	offset := (page - 1) * pageSize
	regularPipelineRunsQuery(h.DB).Where("workspace_id = ? AND pipeline_id = ?", workspaceID, pipelineID).Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&runs)

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
	workspaceID := c.GetUint64("workspace_id")

	var pipeline models.Pipeline
	if err := h.DB.Where("id = ? AND workspace_id = ?", id, workspaceID).First(&pipeline).Error; err != nil {
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
	workspaceID := c.GetUint64("workspace_id")
	pipelineID, _ := strconv.ParseUint(id, 10, 64)
	if !pipelineBelongsToWorkspace(h.DB, pipelineID, workspaceID) {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "流水线不存在"})
		return
	}

	var totalRuns, successfulRuns, failedRuns int64
	var avgDuration float64

	regularPipelineRunsQuery(h.DB.Model(&models.PipelineRun{})).Where("workspace_id = ? AND pipeline_id = ?", workspaceID, pipelineID).Count(&totalRuns)
	regularPipelineRunsQuery(h.DB.Model(&models.PipelineRun{})).Where("workspace_id = ? AND pipeline_id = ? AND status = ?", workspaceID, pipelineID, models.PipelineRunStatusSuccess).Count(&successfulRuns)
	regularPipelineRunsQuery(h.DB.Model(&models.PipelineRun{})).Where("workspace_id = ? AND pipeline_id = ? AND status = ?", workspaceID, pipelineID, models.PipelineRunStatusFailed).Count(&failedRuns)

	// 计算平均耗时
	var totalDuration int64
	regularPipelineRunsQuery(h.DB.Model(&models.PipelineRun{})).Select("COALESCE(SUM(duration), 0)").Where("workspace_id = ? AND pipeline_id = ? AND duration > 0", workspaceID, pipelineID).Scan(&totalDuration)
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
	workspaceID := c.GetUint64("workspace_id")

	var run models.PipelineRun
	if err := h.DB.Preload("Pipeline").Where("workspace_id = ?", workspaceID).First(&run, runID).Error; err != nil {
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
	if liveRunState, err := utils.GetLiveRunState(c.Request.Context(), run.ID); err == nil && liveRunState != nil {
		run.Status = liveRunState.Status
		run.Duration = liveRunState.Duration
		run.ErrorMsg = liveRunState.ErrorMsg
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
	workspaceID := c.GetUint64("workspace_id")

	// 验证运行记录存在且属于指定流水线
	var run models.PipelineRun
	if err := h.DB.Where("workspace_id = ?", workspaceID).First(&run, runID).Error; err != nil {
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
	h.DB.Where("workspace_id = ? AND pipeline_run_id = ?", workspaceID, runID).Preload("Agent").Order("created_at ASC").Find(&tasks)
	if liveTaskStates, complete, err := utils.GetLiveTaskStatesForRun(c.Request.Context(), run.ID); err == nil && complete && len(liveTaskStates) > 0 {
		byTaskID := make(map[uint64]utils.LiveTaskState, len(liveTaskStates))
		for _, state := range liveTaskStates {
			byTaskID[state.TaskID] = state
		}
		for i := range tasks {
			if state, ok := byTaskID[tasks[i].ID]; ok {
				tasks[i].Status = state.Status
				tasks[i].StartTime = state.StartTime
				tasks[i].EndTime = state.EndTime
				tasks[i].Duration = state.Duration
				tasks[i].ExitCode = state.ExitCode
				tasks[i].ErrorMsg = state.ErrorMsg
				if tasks[i].Agent == nil && state.AgentName != "" {
					tasks[i].Agent = &models.Agent{Name: state.AgentName}
				}
			}
		}
	}

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
			if run.Status == models.PipelineRunStatusQueued || run.Status == models.PipelineRunStatusPending || run.Status == models.PipelineRunStatusRunning {
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
	taskIDStr := c.DefaultQuery("task_id", "")
	workspaceID := c.GetUint64("workspace_id")

	// 验证运行记录存在且属于指定流水线
	var run models.PipelineRun
	if err := h.DB.Where("workspace_id = ?", workspaceID).First(&run, runID).Error; err != nil {
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

	var taskID uint64
	if taskIDStr != "" {
		taskID, err = strconv.ParseUint(taskIDStr, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "无效的任务ID",
			})
			return
		}
	}

	logs, err := agentFileLogs.QueryRunLogs(runIDNum, taskID, level, source)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "读取日志失败: " + err.Error(),
		})
		return
	}
	var tasks []models.AgentTask
	if err := h.DB.Where("workspace_id = ? AND pipeline_run_id = ?", workspaceID, runIDNum).Find(&tasks).Error; err == nil {
		taskHandler := NewTaskHandler()
		for _, task := range tasks {
			if models.IsTerminalTaskStatus(task.Status) {
				continue
			}
			liveLogs, liveErr := taskHandler.fetchCrossServerLiveTaskLogs(c.Request.Context(), task, 0)
			if liveErr == nil && len(liveLogs) > 0 {
				logs = append(logs, liveLogs...)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"list":  logs,
			"total": len(logs),
		},
	})
}
