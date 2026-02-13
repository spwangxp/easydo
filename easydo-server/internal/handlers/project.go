package handlers

import (
	"easydo-server/internal/models"
	"net/http"
	"strconv"
	"strings"
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
	
	query := h.DB.Model(&models.Project{})
	
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
		Order("is_favorited DESC, COALESCE(updated_at, created_at) DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&projects)
	
	// 为每个项目添加流水线统计信息
	type ProjectWithStats struct {
		models.Project
		PipelineCount     int       `json:"pipeline_count"`      // 流水线条数
		LatestRunner      string    `json:"latest_runner"`       // 最新执行人
		LatestRunTime     time.Time `json:"latest_run_time"`     // 最新执行时间
		LatestRunStatus   string    `json:"latest_run_status"`   // 最新执行结果
	}
	
	result := make([]ProjectWithStats, 0, len(projects))
	for _, p := range projects {
		pws := ProjectWithStats{
			Project: p,
		}
		
		// 获取项目的流水线数量
		var pipelineCount int64
		h.DB.Model(&models.Pipeline{}).Where("project_id = ?", p.ID).Count(&pipelineCount)
		pws.PipelineCount = int(pipelineCount)
		
		// 获取项目下所有流水线中最近一次运行的执行信息
		// 先获取该项目下所有流水线ID
		var pipelines []models.Pipeline
		h.DB.Where("project_id = ?", p.ID).Pluck("id", &pipelines)
		
		if len(pipelines) > 0 {
			// 获取项目下所有流水线中最近一次运行的记录
			var latestRun models.PipelineRun
			h.DB.Where("pipeline_id IN (?)", 
				h.DB.Model(&models.Pipeline{}).Select("id").Where("project_id = ?", p.ID)).
				Order("created_at DESC").
				First(&latestRun)
			
			if latestRun.ID > 0 {
				pws.LatestRunner = latestRun.TriggerUser
				pws.LatestRunTime = latestRun.CreatedAt
				pws.LatestRunStatus = latestRun.Status
			}
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
	if err := h.DB.Preload("Owner").First(&project, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "项目不存在",
		})
		return
	}
	
	// 获取关联的流水线
	var pipelines []models.Pipeline
	h.DB.Where("project_id = ?", id).Find(&pipelines)
	
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
	
	project := &models.Project{
		Name:        req.Name,
		Description: req.Description,
		Color:       req.Color,
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
	
	// 构建更新SQL
	updates := []string{}
	if req.Name != "" {
		updates = append(updates, "name='"+req.Name+"'")
	}
	if req.Description != "" {
		updates = append(updates, "description='"+req.Description+"'")
	}
	if req.Color != "" {
		updates = append(updates, "color='"+req.Color+"'")
	}
	
	if len(updates) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"code":    200,
			"message": "没有需要更新的字段",
		})
		return
	}
	
	sql := "UPDATE projects SET " + strings.Join(updates, ",") + " WHERE id=" + idStr
	result := h.DB.Exec(sql)
	
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新项目失败: " + result.Error.Error(),
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
	
	// 先删除关联的流水线运行记录
	var pipelines []models.Pipeline
	h.DB.Where("project_id = ?", id).Find(&pipelines)
	for _, p := range pipelines {
		h.DB.Where("pipeline_id = ?", p.ID).Delete(&models.PipelineRun{})
	}
	
	// 删除关联的流水线
	h.DB.Where("project_id = ?", id).Delete(&models.Pipeline{})
	
	// 删除关联的部署记录
	h.DB.Where("project_id = ?", id).Delete(&models.DeployRecord{})
	
	if err := h.DB.Delete(&models.Project{}, id).Error; err != nil {
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
	if err := h.DB.Where("id = ?", idStr).First(&project).Error; err != nil {
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
