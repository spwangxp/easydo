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

const statisticsPipelineRunTriggerTypeDeploymentRequest = "deployment_request"

type StatisticsHandler struct {
	DB *gorm.DB
}

type statisticsDateRange struct {
	Start time.Time
	End   time.Time
}

func NewStatisticsHandler() *StatisticsHandler {
	return &StatisticsHandler{DB: models.DB}
}

func regularStatisticsPipelineRunsQuery(db *gorm.DB) *gorm.DB {
	return db.Where("(trigger_type IS NULL OR trigger_type = '' OR trigger_type <> ?)", statisticsPipelineRunTriggerTypeDeploymentRequest)
}

func parseStatisticsDateRange(c *gin.Context) (statisticsDateRange, bool) {
	startDate := c.DefaultQuery("start_date", "")
	endDate := c.DefaultQuery("end_date", "")

	if startDate == "" && endDate == "" {
		return statisticsDateRange{}, true
	}
	if startDate == "" || endDate == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "start_date and end_date are required together"})
		return statisticsDateRange{}, false
	}

	startTime, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid start_date"})
		return statisticsDateRange{}, false
	}

	endTime, err := time.Parse("2006-01-02", endDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid end_date"})
		return statisticsDateRange{}, false
	}

	if endTime.Before(startTime) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "end_date must be on or after start_date"})
		return statisticsDateRange{}, false
	}

	return statisticsDateRange{
		Start: startTime,
		End:   endTime.Add(24 * time.Hour),
	}, true
}

func applyStatisticsDateRange(query *gorm.DB, dateRange statisticsDateRange) *gorm.DB {
	if !dateRange.Start.IsZero() {
		query = query.Where("created_at >= ?", dateRange.Start)
	}
	if !dateRange.End.IsZero() {
		query = query.Where("created_at < ?", dateRange.End)
	}
	return query
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
	dateRange, ok := parseStatisticsDateRange(c)
	if !ok {
		return
	}
	workspaceID := c.GetUint64("workspace_id")

	query := applyStatisticsDateRange(
		regularStatisticsPipelineRunsQuery(h.DB.Model(&models.PipelineRun{})).Where("workspace_id = ?", workspaceID),
		dateRange,
	)

	type overviewAggregate struct {
		TotalRuns     int64
		SuccessRuns   int64
		FailedRuns    int64
		TotalDuration int64
	}

	var aggregate overviewAggregate
	query.Select(`
		COUNT(*) AS total_runs,
		SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END) AS success_runs,
		SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) AS failed_runs,
		COALESCE(SUM(CASE WHEN duration > 0 THEN duration ELSE 0 END), 0) AS total_duration
	`).Scan(&aggregate)

	successRate := float64(0)
	if aggregate.TotalRuns > 0 {
		successRate = float64(aggregate.SuccessRuns) * 100 / float64(aggregate.TotalRuns)
	}

	var avgDuration float64
	if aggregate.TotalRuns > 0 {
		avgDuration = float64(aggregate.TotalDuration) / float64(aggregate.TotalRuns)
	}

	avgDurationStr := formatDuration(int(avgDuration))

	var pipelineCount int64
	h.DB.Model(&models.Pipeline{}).Where("workspace_id = ?", workspaceID).Count(&pipelineCount)

	var projectCount int64
	h.DB.Model(&models.Project{}).Where("workspace_id = ?", workspaceID).Count(&projectCount)

	today := time.Now()
	todayStart := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
	var todayRuns int64
	regularStatisticsPipelineRunsQuery(h.DB.Model(&models.PipelineRun{})).Where("workspace_id = ? AND created_at >= ?", workspaceID, todayStart).Count(&todayRuns)

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": OverviewResponse{
			TotalRuns:     aggregate.TotalRuns,
			SuccessRate:   math.Round(successRate*100) / 100,
			AvgDuration:   avgDurationStr,
			FailedCount:   aggregate.FailedRuns,
			PipelineCount: pipelineCount,
			ProjectCount:  projectCount,
			TodayRuns:     todayRuns,
		},
	})
}

// GetTrend returns daily run statistics for trend chart
func (h *StatisticsHandler) GetTrend(c *gin.Context) {
	dateRange, ok := parseStatisticsDateRange(c)
	if !ok {
		return
	}
	if dateRange.Start.IsZero() || dateRange.End.IsZero() {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "start_date and end_date are required"})
		return
	}

	workspaceID := c.GetUint64("workspace_id")
	days := int(dateRange.End.Sub(dateRange.Start).Hours()/24 + 0.5)
	dailyRuns := make([]DailyRun, 0, days)

	type trendRow struct {
		Date    string
		Total   int64
		Success int64
		Failed  int64
	}

	var rows []trendRow
	regularStatisticsPipelineRunsQuery(h.DB.Model(&models.PipelineRun{})).
		Select(`DATE(created_at) AS date,
			COUNT(*) AS total,
			SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END) AS success,
			SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) AS failed`).
		Where("workspace_id = ? AND created_at >= ? AND created_at < ?", workspaceID, dateRange.Start, dateRange.End).
		Group("DATE(created_at)").
		Order("date ASC").
		Scan(&rows)

	rowByDate := make(map[string]trendRow, len(rows))
	for _, row := range rows {
		rowByDate[row.Date] = row
	}

	for i := 0; i < days; i++ {
		date := dateRange.Start.AddDate(0, 0, i)
		dateKey := date.Format("2006-01-02")
		row := rowByDate[dateKey]
		successRate := float64(0)
		if row.Total > 0 {
			successRate = float64(row.Success) * 100 / float64(row.Total)
		}
		dailyRuns = append(dailyRuns, DailyRun{
			Date:        dateKey,
			DateLabel:   getWeekdayLabel(date.Weekday()),
			Total:       row.Total,
			Success:     row.Success,
			Failed:      row.Failed,
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
	dateRange, ok := parseStatisticsDateRange(c)
	if !ok {
		return
	}

	workspaceID := c.GetUint64("workspace_id")
	limit := 10
	if limitStr := c.DefaultQuery("limit", "10"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	type topPipelineRow struct {
		PipelineID    uint64
		Name          string
		RunCount      int64
		SuccessCount  int64
		TotalDuration int64
	}

	query := h.DB.Model(&models.Pipeline{}).
		Select(`pipelines.id AS pipeline_id,
			pipelines.name AS name,
			COUNT(pipeline_runs.id) AS run_count,
			SUM(CASE WHEN pipeline_runs.status = 'success' THEN 1 ELSE 0 END) AS success_count,
			COALESCE(SUM(CASE WHEN pipeline_runs.duration > 0 THEN pipeline_runs.duration ELSE 0 END), 0) AS total_duration`).
		Joins(`LEFT JOIN pipeline_runs ON pipeline_runs.pipeline_id = pipelines.id AND pipeline_runs.workspace_id = pipelines.workspace_id AND (pipeline_runs.trigger_type IS NULL OR pipeline_runs.trigger_type = '' OR pipeline_runs.trigger_type <> ?)`, statisticsPipelineRunTriggerTypeDeploymentRequest).
		Where("pipelines.workspace_id = ?", workspaceID)

	if !dateRange.Start.IsZero() {
		query = query.Where("pipeline_runs.created_at >= ?", dateRange.Start)
	}
	if !dateRange.End.IsZero() {
		query = query.Where("pipeline_runs.created_at < ?", dateRange.End)
	}

	var rows []topPipelineRow
	query.Group("pipelines.id, pipelines.name").
		Having("COUNT(pipeline_runs.id) > 0").
		Order("run_count DESC, pipelines.id ASC").
		Limit(limit).
		Scan(&rows)

	pipelineStats := make([]PipelineStats, 0, len(rows))
	for _, row := range rows {
		successRate := float64(0)
		if row.RunCount > 0 {
			successRate = float64(row.SuccessCount) * 100 / float64(row.RunCount)
		}
		avgDuration := float64(0)
		if row.RunCount > 0 {
			avgDuration = float64(row.TotalDuration) / float64(row.RunCount)
		}
		pipelineStats = append(pipelineStats, PipelineStats{
			PipelineID:  row.PipelineID,
			Name:        row.Name,
			RunCount:    row.RunCount,
			SuccessRate: math.Round(successRate*100) / 100,
			AvgDuration: formatDuration(int(avgDuration)),
		})
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
