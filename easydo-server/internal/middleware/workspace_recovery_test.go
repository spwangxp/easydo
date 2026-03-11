package middleware

import (
	"testing"

	"easydo-server/internal/models"
)

func TestResolveUserWorkspace_SkipsInactiveWorkspaceMembership(t *testing.T) {
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
