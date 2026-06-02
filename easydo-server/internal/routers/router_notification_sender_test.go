package routers

import (
	"net/http"
	"testing"

	"easydo-server/internal/config"

	"github.com/gin-gonic/gin"
)

func TestNotificationSenderRoutesAreRegistered(t *testing.T) {
	config.Init()
	gin.SetMode(gin.TestMode)
	router := InitRouter()

	expected := map[string]string{
		http.MethodGet + " /api/notification-senders/effective":     "",
		http.MethodPut + " /api/notification-senders/platform":      "",
		http.MethodPut + " /api/workspaces/:id/notification-sender": "",
		http.MethodPost + " /api/notification-senders/test":         "",
		http.MethodGet + " /api/audit-logs":                         "",
		http.MethodGet + " /api/workspaces/:id/audit-logs":          "",
	}
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := expected[key]; ok {
			expected[key] = route.Handler
		}
	}
	for key, handler := range expected {
		if handler == "" {
			t.Fatalf("route %s was not registered; routes=%#v", key, router.Routes())
		}
	}
}
