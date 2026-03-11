package middleware

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openMiddlewareTestDB(t *testing.T) *gorm.DB {
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
	if err := db.AutoMigrate(&models.User{}, &models.Workspace{}, &models.WorkspaceMember{}); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}
	return db
}

func TestWorkspaceContext_AllowsAdminIntoArbitraryWorkspaceAsOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openMiddlewareTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() {
		models.DB = originalDB
	})

	admin := models.User{Username: "workspace-admin", Role: "admin", Status: "active"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}
	workspace := models.Workspace{Name: "team-a", Slug: "team-a", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: admin.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/?workspace_id="+fmt.Sprintf("%d", workspace.ID), nil)
	c.Request.Header.Set(WorkspaceHeaderKey, fmt.Sprintf("%d", workspace.ID))
	c.Set("user_id", admin.ID)
	c.Set("role", "admin")

	WorkspaceContext()(c)

	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if got := c.GetUint64("workspace_id"); got != workspace.ID {
		t.Fatalf("workspace_id=%d, want=%d", got, workspace.ID)
	}
	if got := c.GetString("workspace_role"); got != models.WorkspaceRoleOwner {
		t.Fatalf("workspace_role=%s, want=%s", got, models.WorkspaceRoleOwner)
	}
	capabilities, _ := c.Get("capabilities")
	capList, _ := capabilities.([]string)
	capSet := make(map[string]bool, len(capList))
	for _, capability := range capList {
		capSet[capability] = true
	}
	if !capSet["workspace.delete"] || !capSet["agent.approve"] {
		t.Fatalf("expected owner capabilities for admin workspace context, got=%v", capList)
	}
}
