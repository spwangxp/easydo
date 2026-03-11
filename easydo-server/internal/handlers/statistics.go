package handlers

import (
	"easydo-server/internal/models"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type StatisticsHandler struct {
	DB *gorm.DB
}

func NewStatisticsHandler() *StatisticsHandler {
	return &StatisticsHandler{DB: models.DB}
}

// OverviewResponse represents the overview statistics response
type OverviewResponse struct {
	TotalRuns     int64   `json:"total_runs"`
	SuccessRate   float64 `json:"success_rate"`
	AvgDuration   string  `json:"avg_duration"`
	FailedCount   int64   `json:"failed_count"`
	PipelineCount int64   `json:"pipeline_count"`
	ProjectCount  int64   `json:"project_count"`
	TodayRuns     int64   `json:"today_runs"`
}

// TrendResponse represents the run trend data
type TrendResponse struct {
	DailyRuns []DailyRun `json:"daily_runs"`
}

type DailyRun struct {
	Date        string  `json:"date"`
	DateLabel   string  `json:"date_label"`
	Total       int64   `json:"total"`
	Success     int64   `json:"success"`
	Failed      int64   `json:"failed"`
	SuccessRate float64 `json:"success_rate"`
}

// PipelineStats represents statistics for a single pipeline
type PipelineStats struct {
	PipelineID  uint64  `json:"pipeline_id"`
	Name        string  `json:"name"`
	RunCount    int64   `json:"run_count"`
	SuccessRate float64 `json:"success_rate"`
	AvgDuration string  `json:"avg_duration"`
}

// TopPipelinesResponse represents the top pipelines response
type TopPipelinesResponse struct {
	Pipelines []PipelineStats `json:"pipelines"`
}

// GetOverview returns overall statistics
func (h *StatisticsHandler) GetOverview(c *gin.Context) {
	startDate := c.DefaultQuery("start_date", "")
	endDate := c.DefaultQuery("end_date", "")
	workspaceID := c.GetUint64("workspace_id")

	totalQuery := h.DB.Model(&models.PipelineRun{}).Where("workspace_id = ?", workspaceID)
	successQuery := h.DB.Model(&models.PipelineRun{}).Where("workspace_id = ?", workspaceID)
	failedQuery := h.DB.Model(&models.PipelineRun{}).Where("workspace_id = ?", workspaceID)
	durationQuery := h.DB.Model(&models.PipelineRun{}).Where("workspace_id = ?", workspaceID)

	if startDate != "" {
		startTime, err := time.Parse("2006-01-02", startDate)
		if err == nil {
			totalQuery = totalQuery.Where("created_at >= ?", startTime)
			successQuery = successQuery.Where("created_at >= ?", startTime)
			failedQuery = failedQuery.Where("created_at >= ?", startTime)
			durationQuery = durationQuery.Where("created_at >= ?", startTime)
		}
	}
	if endDate != "" {
		endTime, err := time.Parse("2006-01-02", endDate)
		if err == nil {
			endTime = endTime.Add(24 * time.Hour)
			totalQuery = totalQuery.Where("created_at < ?", endTime)
			successQuery = successQuery.Where("created_at < ?", endTime)
			failedQuery = failedQuery.Where("created_at < ?", endTime)
			durationQuery = durationQuery.Where("created_at < ?", endTime)
		}
	}

	var totalRuns int64
	totalQuery.Count(&totalRuns)

	var successRuns int64
	successQuery.Where("status = ?", "success").Count(&successRuns)

	var failedRuns int64
	failedQuery.Where("status = ?", "failed").Count(&failedRuns)

	successRate := float64(0)
	if totalRuns > 0 {
		successRate = float64(successRuns) * 100 / float64(totalRuns)
	}

	var avgDuration float64
	var totalDuration int64
	durationQuery.Where("duration > 0").Pluck("COALESCE(SUM(duration), 0)", &totalDuration)
	if totalRuns > 0 {
		avgDuration = float64(totalDuration) / float64(totalRuns)
	}

	avgDurationStr := formatDuration(int(avgDuration))

	var pipelineCount int64
	h.DB.Model(&models.Pipeline{}).Where("workspace_id = ?", workspaceID).Count(&pipelineCount)

	var projectCount int64
	h.DB.Model(&models.Project{}).Where("workspace_id = ?", workspaceID).Count(&projectCount)

	today := time.Now()
	todayStart := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
	var todayRuns int64
	h.DB.Model(&models.PipelineRun{}).Where("workspace_id = ? AND created_at >= ?", workspaceID, todayStart).Count(&todayRuns)

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": OverviewResponse{
			TotalRuns:     totalRuns,
			SuccessRate:   math.Round(successRate*100) / 100,
			AvgDuration:   avgDurationStr,
			FailedCount:   failedRuns,
			PipelineCount: pipelineCount,
			ProjectCount:  projectCount,
			TodayRuns:     todayRuns,
		},
	})
}

// GetTrend returns daily run statistics for trend chart
func (h *StatisticsHandler) GetTrend(c *gin.Context) {
	workspaceID := c.GetUint64("workspace_id")
	// Get date range (default: last 7 days)
	days := 7
	if daysStr := c.DefaultQuery("days", "7"); daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
			days = d
		}
	}

	today := time.Now()
	dailyRuns := make([]DailyRun, 0, days)

	for i := days - 1; i >= 0; i-- {
		date := today.AddDate(0, 0, -i)
		dateStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
		dateEnd := dateStart.Add(24 * time.Hour)

		// Get runs for this day
		var total int64
		h.DB.Model(&models.PipelineRun{}).
			Where("workspace_id = ? AND created_at >= ? AND created_at < ?", workspaceID, dateStart, dateEnd).
			Count(&total)

		var success int64
		h.DB.Model(&models.PipelineRun{}).
			Where("workspace_id = ? AND created_at >= ? AND created_at < ? AND status = ?", workspaceID, dateStart, dateEnd, "success").
			Count(&success)

		var failed int64
		h.DB.Model(&models.PipelineRun{}).
			Where("workspace_id = ? AND created_at >= ? AND created_at < ? AND status = ?", workspaceID, dateStart, dateEnd, "failed").
			Count(&failed)

		successRate := float64(0)
		if total > 0 {
			successRate = float64(success) * 100 / float64(total)
		}

		// Format date label (e.g., "周一", "周二")
		weekday := date.Weekday()
		dateLabel := getWeekdayLabel(weekday)

		dailyRuns = append(dailyRuns, DailyRun{
			Date:        date.Format("2006-01-02"),
			DateLabel:   dateLabel,
			Total:       total,
			Success:     success,
			Failed:      failed,
			SuccessRate: math.Round(successRate*100) / 100,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": TrendResponse{
			DailyRuns: dailyRuns,
		},
	})
}

// GetTopPipelines returns top pipelines by run count
func (h *StatisticsHandler) GetTopPipelines(c *gin.Context) {
	workspaceID := c.GetUint64("workspace_id")
	limit := 10
	if limitStr := c.DefaultQuery("limit", "10"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	// Get date range from query params
	startDate := c.DefaultQuery("start_date", "")
	endDate := c.DefaultQuery("end_date", "")

	// Get all pipelines with their run statistics
	var pipelines []models.Pipeline
	h.DB.Where("workspace_id = ?", workspaceID).Find(&pipelines)

	pipelineStats := make([]PipelineStats, 0, len(pipelines))

	for _, pipeline := range pipelines {
		// Build query for this pipeline's runs
		runQuery := h.DB.Model(&models.PipelineRun{}).Where("workspace_id = ? AND pipeline_id = ?", workspaceID, pipeline.ID)

		// Apply date filter if provided
		if startDate != "" {
			startTime, err := time.Parse("2006-01-02", startDate)
			if err == nil {
				runQuery = runQuery.Where("created_at >= ?", startTime)
			}
		}
		if endDate != "" {
			endTime, err := time.Parse("2006-01-02", endDate)
			if err == nil {
				endTime = endTime.Add(24 * time.Hour)
				runQuery = runQuery.Where("created_at < ?", endTime)
			}
		}

		// Get run count
		var runCount int64
		runQuery.Count(&runCount)

		if runCount == 0 {
			continue
		}

		// Get success count
		var successCount int64
		runQuery.Where("status = ?", "success").Count(&successCount)

		// Calculate success rate
		successRate := float64(successCount) * 100 / float64(runCount)

		// Calculate average duration
		var totalDuration int64
		h.DB.Model(&models.PipelineRun{}).
			Where("workspace_id = ? AND pipeline_id = ? AND duration > 0", workspaceID, pipeline.ID).
			Pluck("COALESCE(SUM(duration), 0)", &totalDuration)

		avgDuration := float64(totalDuration) / float64(runCount)
		avgDurationStr := formatDuration(int(avgDuration))

		pipelineStats = append(pipelineStats, PipelineStats{
			PipelineID:  pipeline.ID,
			Name:        pipeline.Name,
			RunCount:    runCount,
			SuccessRate: math.Round(successRate*100) / 100,
			AvgDuration: avgDurationStr,
		})
	}

	// Sort by run count descending
	for i := 0; i < len(pipelineStats)-1; i++ {
		for j := i + 1; j < len(pipelineStats); j++ {
			if pipelineStats[j].RunCount > pipelineStats[i].RunCount {
				pipelineStats[i], pipelineStats[j] = pipelineStats[j], pipelineStats[i]
			}
		}
	}

	// Limit results
	if len(pipelineStats) > limit {
		pipelineStats = pipelineStats[:limit]
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": TopPipelinesResponse{
			Pipelines: pipelineStats,
		},
	})
}

// formatDuration formats seconds into human readable string
func formatDuration(seconds int) string {
	if seconds <= 0 {
		return "0s"
	}

	// If value is too large (over 1 year), it might be in milliseconds
	if seconds > 31536000 {
		seconds = seconds / 1000
	}

	minutes := seconds / 60
	remainingSeconds := seconds % 60

	if minutes > 0 {
		return strconv.Itoa(minutes) + "m " + strconv.Itoa(remainingSeconds) + "s"
	}
	return strconv.Itoa(seconds) + "s"
}

// getWeekdayLabel returns Chinese weekday label
func getWeekdayLabel(weekday time.Weekday) string {
	labels := map[time.Weekday]string{
		time.Sunday:    "周日",
		time.Monday:    "周一",
		time.Tuesday:   "周二",
		time.Wednesday: "周三",
		time.Thursday:  "周四",
		time.Friday:    "周五",
		time.Saturday:  "周六",
	}
	return labels[weekday]
}
