package services

import (
	"fmt"
	"strings"
	"time"

	cronlib "github.com/robfig/cron/v3"
)

func ComputeNextScheduleTime(expr string, timezone string, from time.Time) (time.Time, error) {
	spec := strings.TrimSpace(expr)
	if spec == "" {
		return time.Time{}, fmt.Errorf("cron expression is required")
	}
	if timezone != "" && !strings.HasPrefix(spec, "CRON_TZ=") && !strings.HasPrefix(spec, "TZ=") {
		spec = fmt.Sprintf("CRON_TZ=%s %s", timezone, spec)
	}

	schedule, err := cronlib.ParseStandard(spec)
	if err != nil {
		return time.Time{}, err
	}
	next := schedule.Next(from)
	if next.IsZero() {
		return time.Time{}, fmt.Errorf("invalid cron expression")
	}
	return next.UTC(), nil
}
