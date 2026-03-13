package handlers

import (
	"easydo-server/internal/models"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ProjectHandler struct {
	DB *gorm.DB
}

func NewProjectHandler() *ProjectHandler {
	return &ProjectHandler{DB: models.DB}
}

// extractPipelineIDs 从 Pipeline 列表中提取 ID
func extractPipelineIDs(pipelines []models.Pipeline) []uint64 {
	ids := make([]uint64, 0, len(pipelines))
	for _, p := range pipelines {
		ids = append(ids, p.ID)
	}
	return ids
}

func (h *ProjectHandler) GetProjectList(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	keyword := c.Query("keyword")
	tab := c.DefaultQuery("tab", "all")

	var projects []models.Project
	var total int64
	workspaceID := c.GetUint64("workspace_id")

	query := h.DB.Model(&models.Project{}).Where("workspace_id = ?", workspaceID)

	// Tab 过滤
	userID := c.GetUint64("user_id")
	switch tab {
	case "created":
		// 我创建的：只显示当前用户创建的项目
		query = query.Where("owner_id = ?", userID)
	case "favorited":
		// 我收藏的：只显示收藏的项目
		query = query.Where("is_favorited = ?", 1)
	case "all":
		// 所有项目：不做额外过滤
	default:
		// 默认显示所有项目
	}

	if keyword != "" {
		query = query.Where("name LIKE ?", "%"+keyword+"%")
	}

	query.Count(&total)

	offset := (page - 1) * pageSize
	// 排序规则：收藏的在前，按修改时间降序（无修改时间则按创建时间）
	query.Preload("Owner").
		Preload("Workspace").
		Order("is_favorited DESC, COALESCE(updated_at, created_at) DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&projects)

	type ProjectWithStats struct {
		models.Project
		PipelineCount   int       `json:"pipeline_count"`    // 流水线条数
		LatestRunner    string    `json:"latest_runner"`     // 最新执行人
		LatestRunTime   time.Time `json:"latest_run_time"`   // 最新执行时间
		LatestRunStatus string    `json:"latest_run_status"` // 最新执行结果
	}

	projectIDs := make([]uint64, 0, len(projects))
	for _, project := range projects {
		projectIDs = append(projectIDs, project.ID)
	}

	type projectPipelineCountRow struct {
		ProjectID     uint64
		PipelineCount int64
	}
	pipelineCountByProject := make(map[uint64]int64, len(projects))
	if len(projectIDs) > 0 {
		var countRows []projectPipelineCountRow
		h.DB.Model(&models.Pipeline{}).
			Select("project_id, COUNT(*) AS pipeline_count").
			Where("workspace_id = ? AND project_id IN ?", workspaceID, projectIDs).
			Group("project_id").
			Scan(&countRows)
		for _, row := range countRows {
			pipelineCountByProject[row.ProjectID] = row.PipelineCount
		}
	}

	type latestProjectRunRow struct {
		ProjectID   uint64
		TriggerUser string
		CreatedAt   time.Time
		Status      string
	}
	latestRunByProject := make(map[uint64]latestProjectRunRow, len(projects))
	if len(projectIDs) > 0 {
		var latestRows []latestProjectRunRow
		latestRunSubQuery := h.DB.Table("pipeline_runs AS pr").
			Select("p.project_id, MAX(pr.created_at) AS latest_created_at").
			Joins("JOIN pipelines p ON p.id = pr.pipeline_id").
			Where("p.workspace_id = ? AND p.project_id IN ?", workspaceID, projectIDs).
			Group("p.project_id")

		h.DB.Table("pipeline_runs AS pr").
			Select("p.project_id, pr.trigger_user, pr.created_at, pr.status").
			Joins("JOIN pipelines p ON p.id = pr.pipeline_id").
			Joins("JOIN (?) latest ON latest.project_id = p.project_id AND latest.latest_created_at = pr.created_at", latestRunSubQuery).
			Where("p.workspace_id = ? AND p.project_id IN ?", workspaceID, projectIDs).
			Order("pr.id DESC").
			Scan(&latestRows)

		for _, row := range latestRows {
			if _, exists := latestRunByProject[row.ProjectID]; !exists {
				latestRunByProject[row.ProjectID] = row
			}
		}
	}

	result := make([]ProjectWithStats, 0, len(projects))
	for _, p := range projects {
		pws := ProjectWithStats{
			Project: p,
		}
		pws.PipelineCount = int(pipelineCountByProject[p.ID])
		if latestRun, ok := latestRunByProject[p.ID]; ok {
			pws.LatestRunner = latestRun.TriggerUser
			pws.LatestRunTime = latestRun.CreatedAt
			pws.LatestRunStatus = latestRun.Status
		}

		result = append(result, pws)
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"list":  result,
			"total": total,
			"page":  page,
			"size":  pageSize,
		},
	})
}

func (h *ProjectHandler) GetProjectDetail(c *gin.Context) {
	id := c.Param("id")

	var project models.Project
	workspaceID := c.GetUint64("workspace_id")
	if err := h.DB.Preload("Owner").Where("id = ? AND workspace_id = ?", id, workspaceID).First(&project).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "项目不存在",
		})
		return
	}

	// 获取关联的流水线
	var pipelines []models.Pipeline
	h.DB.Where("project_id = ? AND workspace_id = ?", id, workspaceID).Find(&pipelines)

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"project":   project,
			"pipelines": pipelines,
		},
	})
}

func (h *ProjectHandler) CreateProject(c *gin.Context) {
	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
		Color       string `json:"color"`
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
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权在当前工作空间创建项目"})
		return
	}

	project := &models.Project{
		Name:        req.Name,
		Description: req.Description,
		Color:       req.Color,
		WorkspaceID: workspaceID,
		OwnerID:     userID,
	}

	if err := h.DB.Create(project).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建项目失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": project,
	})
}

func (h *ProjectHandler) UpdateProject(c *gin.Context) {
	idStr := c.Param("id")

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Color       string `json:"color"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "参数错误",
		})
		return
	}

	workspaceID := c.GetUint64("workspace_id")
	userID := c.GetUint64("user_id")
	role := c.GetString("role")
	var project models.Project
	if err := h.DB.Where("id = ? AND workspace_id = ?", idStr, workspaceID).First(&project).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "项目不存在"})
		return
	}
	if !userCanWriteWorkspaceResource(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权修改该项目"})
		return
	}
	updates := map[string]interface{}{}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Description != "" {
		updates["description"] = req.Description
	}
	if req.Color != "" {
		updates["color"] = req.Color
	}

	if len(updates) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"code":    200,
			"message": "没有需要更新的字段",
		})
		return
	}

	if err := h.DB.Model(&project).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新项目失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "更新成功",
	})
}

func (h *ProjectHandler) DeleteProject(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的项目ID",
		})
		return
	}

	workspaceID := c.GetUint64("workspace_id")
	userID := c.GetUint64("user_id")
	role := c.GetString("role")
	if !userCanManageWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权删除该项目"})
		return
	}
	var project models.Project
	if err := h.DB.Where("id = ? AND workspace_id = ?", id, workspaceID).First(&project).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "项目不存在"})
		return
	}

	// 先删除关联的流水线运行记录
	var pipelines []models.Pipeline
	h.DB.Where("project_id = ? AND workspace_id = ?", id, workspaceID).Find(&pipelines)
	for _, p := range pipelines {
		h.DB.Where("pipeline_id = ?", p.ID).Delete(&models.PipelineRun{})
	}

	// 删除关联的流水线
	h.DB.Where("project_id = ? AND workspace_id = ?", id, workspaceID).Delete(&models.Pipeline{})

	// 删除关联的部署记录
	h.DB.Where("project_id = ?", id).Delete(&models.DeployRecord{})

	if err := h.DB.Delete(&project).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除项目失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "删除成功",
	})
}

func (h *ProjectHandler) ToggleFavorite(c *gin.Context) {
	idStr := c.Param("id")

	var project models.Project
	workspaceID := c.GetUint64("workspace_id")
	if err := h.DB.Where("id = ? AND workspace_id = ?", idStr, workspaceID).First(&project).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "项目不存在",
		})
		return
	}

	// 切换收藏状态
	newStatus := 0
	if !project.IsFavorited {
		newStatus = 1
	}

	// 使用 raw SQL 更新避免 GORM 问题
	result := h.DB.Exec("UPDATE projects SET is_favorited = ? WHERE id = ?", newStatus, idStr)

	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "操作失败: " + result.Error.Error(),
		})
		return
	}

	if newStatus == 1 {
		c.JSON(http.StatusOK, gin.H{
			"code":    200,
			"message": "收藏成功",
		})
	} else {
		c.JSON(http.StatusOK, gin.H{
			"code":    200,
			"message": "取消收藏",
		})
	}
}
