package middleware

import (
	"context"
	"testing"
	"time"

	"easydo-server/internal/config"
	"easydo-server/internal/models"
	"easydo-server/pkg/utils"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setupWorkspaceTestRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	config.Init()
	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}
	previousRedis := utils.RedisClient
	utils.RedisClient = redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() {
		if utils.RedisClient != nil {
			_ = utils.RedisClient.Close()
		}
		utils.RedisClient = previousRedis
		mini.Close()
	})
	return mini
}

func TestResolveUserWorkspace_SkipsInactiveWorkspaceMembership(t *testing.T) {
	setupWorkspaceTestRedis(t)
	db := openMiddlewareTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() {
		models.DB = originalDB
	})

	user := models.User{Username: "member-user", Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	archived := models.Workspace{Name: "archived-space", Slug: "archived-space", Status: models.WorkspaceStatusArchived, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: user.ID}
	active := models.Workspace{Name: "active-space", Slug: "active-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: user.ID}
	if err := db.Create(&archived).Error; err != nil {
		t.Fatalf("create archived workspace failed: %v", err)
	}
	if err := db.Create(&active).Error; err != nil {
		t.Fatalf("create active workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: archived.ID, UserID: user.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive}).Error; err != nil {
		t.Fatalf("create archived membership failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: active.ID, UserID: user.ID, Role: models.WorkspaceRoleDeveloper, Status: models.WorkspaceMemberStatusActive}).Error; err != nil {
		t.Fatalf("create active membership failed: %v", err)
	}

	workspace, member, err := ResolveUserWorkspace(user.ID, 0)
	if err != nil {
		t.Fatalf("ResolveUserWorkspace returned error: %v", err)
	}
	if workspace == nil || member == nil {
		t.Fatalf("expected active workspace membership, got workspace=%v member=%v", workspace, member)
	}
	if workspace.ID != active.ID {
		t.Fatalf("workspace_id=%d, want=%d", workspace.ID, active.ID)
	}
	if member.WorkspaceID != active.ID {
		t.Fatalf("member.workspace_id=%d, want=%d", member.WorkspaceID, active.ID)
	}

	workspace, member, err = ResolveUserWorkspace(user.ID, archived.ID)
	if err != nil {
		t.Fatalf("ResolveUserWorkspace archived lookup returned error: %v", err)
	}
	if workspace != nil || member != nil {
		t.Fatalf("expected inactive requested workspace to be ignored, got workspace=%v member=%v", workspace, member)
	}
}

func TestResolveUserWorkspace_RefreshesWhenWorkspaceAuthVersionChanges(t *testing.T) {
	setupWorkspaceTestRedis(t)
	db := openMiddlewareTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() {
		models.DB = originalDB
	})

	user := models.User{Username: "cached-member-user", Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: "cache-space", Slug: "cache-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: user.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	member := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: user.ID, Role: models.WorkspaceRoleDeveloper, Status: models.WorkspaceMemberStatusActive}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create member failed: %v", err)
	}

	resolvedWorkspace, resolvedMember, err := ResolveUserWorkspace(user.ID, workspace.ID)
	if err != nil {
		t.Fatalf("initial resolve failed: %v", err)
	}
	if resolvedWorkspace == nil || resolvedMember == nil {
		t.Fatal("expected initial membership resolution")
	}
	if resolvedMember.Role != models.WorkspaceRoleDeveloper {
		t.Fatalf("initial role=%s, want developer", resolvedMember.Role)
	}

	if err := db.Model(&member).Update("role", models.WorkspaceRoleMaintainer).Error; err != nil {
		t.Fatalf("update member role failed: %v", err)
	}
	if err := BumpWorkspaceAuthVersion(context.Background(), workspace.ID); err != nil {
		t.Fatalf("bump workspace auth version failed: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	resolvedWorkspace, resolvedMember, err = ResolveUserWorkspace(user.ID, workspace.ID)
	if err != nil {
		t.Fatalf("resolve after version bump failed: %v", err)
	}
	if resolvedWorkspace == nil || resolvedMember == nil {
		t.Fatal("expected refreshed membership resolution")
	}
	if resolvedMember.Role != models.WorkspaceRoleMaintainer {
		t.Fatalf("refreshed role=%s, want maintainer", resolvedMember.Role)
	}
}
