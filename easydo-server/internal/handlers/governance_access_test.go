package handlers

import (
	"testing"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
)

func newGovernanceTestContext(systemRole, workspaceRole, workspaceKind string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c := &gin.Context{}
	c.Set("user_id", uint64(1))
	c.Set("role", systemRole)
	c.Set("workspace_id", uint64(2))
	c.Set("workspace_role", workspaceRole)
	c.Set("workspace_kind", workspaceKind)
	return c
}

func TestRequirePlatformGovernance(t *testing.T) {
	tests := []struct {
		name          string
		systemRole    string
		workspaceKind string
		want          bool
	}{
		{name: "admin in admin workspace", systemRole: "admin", workspaceKind: models.WorkspaceKindAdmin, want: true},
		{name: "admin in normal workspace", systemRole: "admin", workspaceKind: models.WorkspaceKindNormal, want: false},
		{name: "non-admin in admin workspace", systemRole: "user", workspaceKind: models.WorkspaceKindAdmin, want: false},
		{name: "non-admin in normal workspace", systemRole: "user", workspaceKind: models.WorkspaceKindNormal, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RequirePlatformGovernance(BuildGovernanceContext(newGovernanceTestContext(tt.systemRole, models.WorkspaceRoleOwner, tt.workspaceKind)))
			if got != tt.want {
				t.Fatalf("RequirePlatformGovernance()=%v, want=%v", got, tt.want)
			}
		})
	}
}

func TestRequireWorkspaceGovernance(t *testing.T) {
	tests := []struct {
		name          string
		systemRole    string
		workspaceRole string
		workspaceKind string
		want          bool
	}{
		{name: "owner in normal workspace", systemRole: "user", workspaceRole: models.WorkspaceRoleOwner, workspaceKind: models.WorkspaceKindNormal, want: true},
		{name: "admin in normal workspace", systemRole: "admin", workspaceRole: models.WorkspaceRoleViewer, workspaceKind: models.WorkspaceKindNormal, want: true},
		{name: "maintainer in normal workspace", systemRole: "user", workspaceRole: models.WorkspaceRoleMaintainer, workspaceKind: models.WorkspaceKindNormal, want: false},
		{name: "owner in admin workspace", systemRole: "user", workspaceRole: models.WorkspaceRoleOwner, workspaceKind: models.WorkspaceKindAdmin, want: false},
		{name: "admin in admin workspace", systemRole: "admin", workspaceRole: models.WorkspaceRoleOwner, workspaceKind: models.WorkspaceKindAdmin, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RequireWorkspaceGovernance(BuildGovernanceContext(newGovernanceTestContext(tt.systemRole, tt.workspaceRole, tt.workspaceKind)))
			if got != tt.want {
				t.Fatalf("RequireWorkspaceGovernance()=%v, want=%v", got, tt.want)
			}
		})
	}
}

func TestRequireNormalWorkspaceKind(t *testing.T) {
	tests := []struct {
		name          string
		workspaceKind string
		want          bool
	}{
		{name: "normal workspace", workspaceKind: models.WorkspaceKindNormal, want: true},
		{name: "admin workspace", workspaceKind: models.WorkspaceKindAdmin, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RequireNormalWorkspaceKind(BuildGovernanceContext(newGovernanceTestContext("admin", models.WorkspaceRoleOwner, tt.workspaceKind)))
			if got != tt.want {
				t.Fatalf("RequireNormalWorkspaceKind()=%v, want=%v", got, tt.want)
			}
		})
	}
}

func TestBuildGovernanceContextReadsWorkspaceKindFromContext(t *testing.T) {
	ctx := BuildGovernanceContext(newGovernanceTestContext("admin", models.WorkspaceRoleOwner, models.WorkspaceKindAdmin))

	if ctx.WorkspaceKind != models.WorkspaceKindAdmin {
		t.Fatalf("WorkspaceKind=%s, want=%s", ctx.WorkspaceKind, models.WorkspaceKindAdmin)
	}
	if ctx.WorkspaceID != 2 {
		t.Fatalf("WorkspaceID=%d, want=2", ctx.WorkspaceID)
	}
}

func TestGovernanceWorkspaceKindByIDFallsBackToNormalForBlankKind(t *testing.T) {
	db := openHandlerTestDB(t)
	workspace := models.Workspace{
		Name:       "blank-kind-workspace",
		Slug:       "blank-kind-workspace",
		Status:     models.WorkspaceStatusActive,
		Visibility: models.WorkspaceVisibilityPrivate,
		Kind:       models.WorkspaceKindAdmin,
		CreatedBy:  1,
	}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Model(&models.Workspace{}).Where("id = ?", workspace.ID).Update("kind", "").Error; err != nil {
		t.Fatalf("blank workspace kind failed: %v", err)
	}

	if got := workspaceKindByID(db, workspace.ID); got != models.WorkspaceKindNormal {
		t.Fatalf("workspaceKindByID()=%s, want=%s", got, models.WorkspaceKindNormal)
	}
}

func TestGovernanceContextForWorkspaceTreatsAdminAsWorkspaceOwner(t *testing.T) {
	db := openHandlerTestDB(t)

	admin := models.User{Username: "governance-admin-owner", Role: "admin", Status: "active"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}
	workspace := models.Workspace{
		Name:       "governance-admin-normal-workspace",
		Slug:       "governance-admin-normal-workspace",
		Status:     models.WorkspaceStatusActive,
		Visibility: models.WorkspaceVisibilityPrivate,
		Kind:       models.WorkspaceKindNormal,
		CreatedBy:  admin.ID,
	}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}

	ctx := governanceContextForWorkspace(db, workspace.ID, admin.ID, admin.Role)

	if ctx.WorkspaceKind != models.WorkspaceKindNormal {
		t.Fatalf("WorkspaceKind=%s, want=%s", ctx.WorkspaceKind, models.WorkspaceKindNormal)
	}
	if ctx.WorkspaceRole != models.WorkspaceRoleOwner {
		t.Fatalf("WorkspaceRole=%s, want=%s", ctx.WorkspaceRole, models.WorkspaceRoleOwner)
	}
	if !RequireWorkspaceGovernance(ctx) {
		t.Fatal("admin should have workspace governance in normal workspace")
	}
}

func TestGenericAccessControlHelpersDoNotDependOnWorkspaceKind(t *testing.T) {
	db := openHandlerTestDB(t)

	owner := models.User{Username: "workspace-kind-owner", Role: "user", Status: "active"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner failed: %v", err)
	}
	developer := models.User{Username: "workspace-kind-developer", Role: "user", Status: "active"}
	if err := db.Create(&developer).Error; err != nil {
		t.Fatalf("create developer failed: %v", err)
	}
	admin := models.User{Username: "workspace-kind-admin", Role: "admin", Status: "active"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}

	workspace := models.Workspace{
		Name:       "admin-kind-access",
		Slug:       "admin-kind-access",
		Status:     models.WorkspaceStatusActive,
		Visibility: models.WorkspaceVisibilityPrivate,
		Kind:       models.WorkspaceKindAdmin,
		CreatedBy:  owner.ID,
	}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	members := []models.WorkspaceMember{
		{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive},
		{WorkspaceID: workspace.ID, UserID: developer.ID, Role: models.WorkspaceRoleDeveloper, Status: models.WorkspaceMemberStatusActive},
	}
	if err := db.Create(&members).Error; err != nil {
		t.Fatalf("create workspace members failed: %v", err)
	}

	if !userCanWriteWorkspaceResource(db, workspace.ID, developer.ID, developer.Role) {
		t.Fatal("developer should retain generic write permission regardless of workspace kind")
	}
	if userCanManageWorkspace(db, workspace.ID, developer.ID, developer.Role) {
		t.Fatal("developer should not gain maintainer-level management permission")
	}
	if !isWorkspaceOwner(db, workspace.ID, owner.ID, owner.Role) {
		t.Fatal("owner should retain ownership semantics regardless of workspace kind")
	}
	if !isWorkspaceOwner(db, workspace.ID, admin.ID, admin.Role) {
		t.Fatal("admin should retain ownership shortcut regardless of workspace kind")
	}
}
