package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"easydo-server/internal/config"
	"easydo-server/internal/models"
	"easydo-server/pkg/utils"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestFetchCrossServerLiveTaskLogs_UsesOwnerPresenceAndInternalToken(t *testing.T) {
	config.Init()
	config.Config.Set("server.id", "server-local")
	config.Config.Set("server.internal_token", "shared-secret")

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}
	defer mini.Close()

	previousRedis := utils.RedisClient
	utils.RedisClient = redis.NewClient(&redis.Options{Addr: mini.Addr()})
	defer func() {
		if utils.RedisClient != nil {
			_ = utils.RedisClient.Close()
		}
		utils.RedisClient = previousRedis
	}()

	var gotToken string
	var handlerErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get(utils.InternalTokenHeader)
		if r.URL.Path != "/internal/tasks/88/live-logs" {
			handlerErr = fmt.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("since_seq"); got != "9" {
			handlerErr = fmt.Errorf("since_seq=%s, want=9", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code": 200,
			"data": map[string]interface{}{
				"list": []map[string]interface{}{{
					"task_id":         88,
					"pipeline_run_id": 55,
					"level":           "info",
					"message":         "owner-log",
					"timestamp":       12345,
					"source":          "stdout",
				}},
			},
		})
	}))
	defer server.Close()

	if err := utils.PutAgentPresence(context.Background(), utils.AgentPresence{
		AgentID:           7,
		AgentSessionID:    "session-7",
		ServerID:          "server-owner",
		ServerURL:         server.URL,
		Status:            models.AgentStatusOnline,
		LastHeartbeatAt:   123,
		HeartbeatInterval: 10,
	}); err != nil {
		t.Fatalf("put presence failed: %v", err)
	}

	task := models.AgentTask{
		BaseModel:      models.BaseModel{ID: 88},
		AgentID:        7,
		PipelineRunID:  55,
		OwnerServerID:  "server-owner",
		AgentSessionID: "session-7",
		Status:         models.TaskStatusRunning,
	}

	logs, err := NewTaskHandler().fetchCrossServerLiveTaskLogs(context.Background(), task, 9)
	if err != nil {
		t.Fatalf("fetchCrossServerLiveTaskLogs failed: %v", err)
	}
	if handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if gotToken != "shared-secret" {
		t.Fatalf("internal token=%s, want shared-secret", gotToken)
	}
	if len(logs) != 1 {
		t.Fatalf("logs len=%d, want=1", len(logs))
	}
	if logs[0].Message != "owner-log" {
		t.Fatalf("log message=%s, want owner-log", logs[0].Message)
	}
}

func TestFetchCrossServerLiveTaskLogs_SkipsWhenPresenceDoesNotMatchOwner(t *testing.T) {
	config.Init()
	config.Config.Set("server.id", "server-local")

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}
	defer mini.Close()

	previousRedis := utils.RedisClient
	utils.RedisClient = redis.NewClient(&redis.Options{Addr: mini.Addr()})
	defer func() {
		if utils.RedisClient != nil {
			_ = utils.RedisClient.Close()
		}
		utils.RedisClient = previousRedis
	}()

	if err := utils.PutAgentPresence(context.Background(), utils.AgentPresence{
		AgentID:           8,
		AgentSessionID:    "session-8",
		ServerID:          "server-other",
		ServerURL:         "http://example.invalid",
		Status:            models.AgentStatusOnline,
		LastHeartbeatAt:   123,
		HeartbeatInterval: 10,
	}); err != nil {
		t.Fatalf("put presence failed: %v", err)
	}

	logs, err := NewTaskHandler().fetchCrossServerLiveTaskLogs(context.Background(), models.AgentTask{
		BaseModel:     models.BaseModel{ID: 99},
		AgentID:       8,
		PipelineRunID: 66,
		OwnerServerID: "server-owner",
		Status:        models.TaskStatusRunning,
	}, 0)
	if err != nil {
		t.Fatalf("expected nil error on owner mismatch, got %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("expected no logs, got %d", len(logs))
	}
}
