package routers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"easydo-server/internal/config"
	"easydo-server/internal/handlers"
	"easydo-server/internal/middleware"
	"easydo-server/internal/models"
	"easydo-server/pkg/utils"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupRouterUserCreateTestEnv(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	t.Setenv("JWT_SECRET", "test-secret")
	t.Setenv("AUTH_TOKEN_TTL", (4 * time.Hour).String())
	t.Setenv("AUTH_REFRESH_INTERVAL", (10 * time.Minute).String())
	config.Init()

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}
	utils.RedisClient = redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() {
		_ = utils.RedisClient.Close()
		mini.Close()
	})
	return mini
}

func openRouterTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_", ":", "_").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", name)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		Logger:                                   logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite db failed: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db failed: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	if err := db.AutoMigrate(
		&models.User{},
		&models.Workspace{},
		&models.WorkspaceMember{},
		&models.WorkspaceInvitation{},
		&models.Project{},
		&models.Pipeline{},
		&models.PipelineRun{},
		&models.Agent{},
		&models.AgentTask{},
		&models.TaskExecution{},
		&models.AgentLog{},
		&models.AgentHeartbeat{},
		&models.AgentTaskEvent{},
		&models.AgentLogChunk{},
		&models.Credential{},
		&models.CredentialEvent{},
		&models.PipelineCredentialRef{},
	); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}
	return db
}

func issueRouterTestToken(t *testing.T, user *models.User) string {
	t.Helper()
	token, _, err := middleware.IssueTokenSession(context.Background(), user)
	if err != nil {
		t.Fatalf("issue token failed: %v", err)
	}
	return token
}

func TestCreateUserRouteAllowsPlatformAdminWithStaleWorkspaceHeader(t *testing.T) {
	setupRouterUserCreateTestEnv(t)
	db := openRouterTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() {
		models.DB = originalDB
	})

	admin := models.User{Username: "route-admin-stale-header", Role: "admin", Status: "active"}
	if err := admin.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set admin password failed: %v", err)
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}
	workspace := models.Workspace{Name: "route-shared-stale", Slug: "route-shared-stale", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: admin.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	api := router.Group("/api")
	users := api.Group("/users")
	users.Use(middleware.JWTAuth())
	{
		userHandler := handlers.NewUserHandler()
		users.POST("", userHandler.CreateUser)
	}
	token := issueRouterTestToken(t, &admin)
	body, err := json.Marshal(map[string]interface{}{
		"username":       "route-created-with-stale-header",
		"password":       "1qaz2WSX",
		"workspace_id":   workspace.ID,
		"workspace_role": "developer",
	})
	if err != nil {
		t.Fatalf("marshal payload failed: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(middleware.WorkspaceHeaderKey, "999999")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected admin create to ignore stale workspace header, got %d body=%s", w.Code, w.Body.String())
	}
}
