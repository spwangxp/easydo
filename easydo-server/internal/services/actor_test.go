package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"easydo-server/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openActorTestDB(t *testing.T) *gorm.DB {
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

func TestWorkspaceContextRequiresExplicitWorkspaceID(t *testing.T) {
	db := openActorTestDB(t)
	actor := ActorContext{UserID: 1001, Username: "admin-user", SystemRole: "admin"}

	resolved, err := ResolveWorkspaceForActor(context.Background(), db, actor, 0)
	if err != nil {
		t.Fatalf("ResolveWorkspaceForActor returned error: %v", err)
	}
	if resolved.Workspace != nil || resolved.Member != nil {
		t.Fatalf("expected empty workspace context, got %+v", resolved)
	}
	if resolved.WorkspaceID != 0 {
		t.Fatalf("workspace_id=%d, want 0", resolved.WorkspaceID)
	}
}

func TestWorkspaceContextMatchesMiddlewareAdminSemantics(t *testing.T) {
	db := openActorTestDB(t)
	admin := models.User{Username: "workspace-admin", Role: "admin", Status: "active"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}
	workspace := models.Workspace{Name: "team-a", Slug: "team-a", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindAdmin, CreatedBy: admin.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}

	resolved, err := ResolveWorkspaceForActor(context.Background(), db, ActorContext{UserID: admin.ID, Username: admin.Username, SystemRole: admin.Role}, workspace.ID)
	if err != nil {
		t.Fatalf("ResolveWorkspaceForActor returned error: %v", err)
	}
	if resolved.Workspace == nil {
		t.Fatal("expected workspace in context")
	}
	if resolved.WorkspaceID != workspace.ID {
		t.Fatalf("workspace_id=%d, want %d", resolved.WorkspaceID, workspace.ID)
	}
	if resolved.Role != models.WorkspaceRoleOwner {
		t.Fatalf("role=%s, want %s", resolved.Role, models.WorkspaceRoleOwner)
	}
	if resolved.WorkspaceKind != models.WorkspaceKindAdmin {
		t.Fatalf("workspace_kind=%s, want %s", resolved.WorkspaceKind, models.WorkspaceKindAdmin)
	}
	if resolved.Member == nil {
		t.Fatal("expected synthetic owner membership")
	}
	if resolved.Member.UserID != admin.ID || resolved.Member.Role != models.WorkspaceRoleOwner {
		t.Fatalf("member=%+v, want owner membership for admin", resolved.Member)
	}
	capSet := make(map[string]bool, len(resolved.Capabilities))
	for _, capability := range resolved.Capabilities {
		capSet[capability] = true
	}
	if !capSet["workspace.delete"] || !capSet["agent.approve"] {
		t.Fatalf("expected owner capabilities, got %v", resolved.Capabilities)
	}
}

func TestActorSourceDoesNotImportMiddleware(t *testing.T) {
	contents, err := os.ReadFile("actor.go")
	if err != nil {
		t.Fatalf("read actor.go failed: %v", err)
	}
	if strings.Contains(string(contents), "easydo-server/internal/middleware") {
		t.Fatal("actor.go must not import middleware; use lower-level workspace auth helpers")
	}
}

func TestActorWorkspaceDBFailureMapsToInternalError(t *testing.T) {
	db := openActorTestDB(t)
	admin := models.User{Username: "db-failure-admin", Role: "admin", Status: "active"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db failed: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db failed: %v", err)
	}

	_, err = ResolveWorkspaceForActor(context.Background(), db, ActorContext{UserID: admin.ID, Username: admin.Username, SystemRole: admin.Role}, 999)
	if err == nil {
		t.Fatal("expected internal error")
	}
	var svcErr ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("error type=%T, want ServiceError", err)
	}
	if svcErr.Code != ErrorCodeInternalError {
		t.Fatalf("error code=%q, want %q", svcErr.Code, ErrorCodeInternalError)
	}
}

func TestWorkspaceContextRejectsInaccessibleNonAdminWorkspace(t *testing.T) {
	db := openActorTestDB(t)
	user := models.User{Username: "member-user", Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: "admin-space", Slug: "admin-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindAdmin, CreatedBy: user.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	member := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: user.ID, Role: models.WorkspaceRoleViewer, Status: models.WorkspaceMemberStatusActive}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create workspace member failed: %v", err)
	}

	_, err := ResolveWorkspaceForActor(context.Background(), db, ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, workspace.ID)
	if err == nil {
		t.Fatal("expected forbidden error")
	}
	var svcErr ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("error type=%T, want ServiceError", err)
	}
	if svcErr.Code != ErrorCodeForbidden {
		t.Fatalf("error code=%q, want %q", svcErr.Code, ErrorCodeForbidden)
	}
}
