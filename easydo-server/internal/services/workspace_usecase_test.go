package services

import (
	"context"
	"errors"
	"testing"

	"easydo-server/internal/models"
)

func TestWorkspaceUseCaseListHidesAdminWorkspaceForNonAdmin(t *testing.T) {
	db := openActorTestDB(t)
	usecase := &WorkspaceUseCase{DB: db}

	user := models.User{Username: "workspace-list-user", Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	adminWorkspace := models.Workspace{Name: "admin-space", Slug: "admin-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindAdmin, CreatedBy: user.ID}
	normalWorkspace := models.Workspace{Name: "normal-space", Slug: "normal-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: user.ID}
	for _, workspace := range []*models.Workspace{&adminWorkspace, &normalWorkspace} {
		if err := db.Create(workspace).Error; err != nil {
			t.Fatalf("create workspace failed: %v", err)
		}
	}
	for _, member := range []models.WorkspaceMember{{WorkspaceID: adminWorkspace.ID, UserID: user.ID, Role: models.WorkspaceRoleViewer, Status: models.WorkspaceMemberStatusActive}, {WorkspaceID: normalWorkspace.ID, UserID: user.ID, Role: models.WorkspaceRoleDeveloper, Status: models.WorkspaceMemberStatusActive}} {
		if err := db.Create(&member).Error; err != nil {
			t.Fatalf("create membership failed: %v", err)
		}
	}

	result, err := usecase.ListWorkspaces(context.Background(), ListWorkspacesRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}})
	if err != nil {
		t.Fatalf("ListWorkspaces returned error: %v", err)
	}
	if len(result.List) != 1 {
		t.Fatalf("workspace count=%d, want 1", len(result.List))
	}
	if result.List[0].ID != normalWorkspace.ID {
		t.Fatalf("workspace id=%d, want %d", result.List[0].ID, normalWorkspace.ID)
	}
	if result.List[0].Kind != models.WorkspaceKindNormal {
		t.Fatalf("workspace kind=%q, want %q", result.List[0].Kind, models.WorkspaceKindNormal)
	}
	if result.CurrentWorkspaceID != normalWorkspace.ID {
		t.Fatalf("current_workspace_id=%d, want %d", result.CurrentWorkspaceID, normalWorkspace.ID)
	}
}

func TestWorkspaceUseCaseListPreservesCurrentWorkspaceIDOutsideReturnedList(t *testing.T) {
	db := openActorTestDB(t)
	usecase := &WorkspaceUseCase{DB: db}

	user := models.User{Username: "workspace-current-user", Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	returnedWorkspace := models.Workspace{Name: "returned-space", Slug: "returned-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: user.ID}
	otherWorkspace := models.Workspace{Name: "other-space", Slug: "other-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: user.ID}
	for _, workspace := range []*models.Workspace{&returnedWorkspace, &otherWorkspace} {
		if err := db.Create(workspace).Error; err != nil {
			t.Fatalf("create workspace failed: %v", err)
		}
		if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: user.ID, Role: models.WorkspaceRoleDeveloper, Status: models.WorkspaceMemberStatusActive}).Error; err != nil {
			t.Fatalf("create membership failed: %v", err)
		}
	}

	result, err := usecase.ListWorkspaces(context.Background(), ListWorkspacesRequest{
		Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role, CurrentWorkspaceID: otherWorkspace.ID},
		Query: "returned",
	})
	if err != nil {
		t.Fatalf("ListWorkspaces returned error: %v", err)
	}
	if len(result.List) != 1 || result.List[0].ID != returnedWorkspace.ID {
		t.Fatalf("returned list=%+v, want only workspace %d", result.List, returnedWorkspace.ID)
	}
	if result.CurrentWorkspaceID != otherWorkspace.ID {
		t.Fatalf("current_workspace_id=%d, want preserved %d", result.CurrentWorkspaceID, otherWorkspace.ID)
	}
}

func TestWorkspaceUseCaseGetRejectsInvisibleWorkspace(t *testing.T) {
	db := openActorTestDB(t)
	usecase := &WorkspaceUseCase{DB: db}

	user := models.User{Username: "workspace-get-user", Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: "admin-space", Slug: "admin-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindAdmin, CreatedBy: user.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	member := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: user.ID, Role: models.WorkspaceRoleDeveloper, Status: models.WorkspaceMemberStatusActive}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create membership failed: %v", err)
	}

	_, err := usecase.GetWorkspace(context.Background(), GetWorkspaceRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, WorkspaceID: workspace.ID})
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

func TestWorkspaceUseCaseListDeduplicatesDuplicateActiveMemberships(t *testing.T) {
	db := openActorTestDB(t)
	usecase := &WorkspaceUseCase{DB: db}

	if err := db.Migrator().DropIndex(&models.WorkspaceMember{}, "idx_workspace_user"); err != nil {
		t.Fatalf("drop unique membership index failed: %v", err)
	}
	user := models.User{Username: "workspace-duplicate-member-user", Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: "duplicate-space", Slug: "duplicate-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: user.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	for range []int{1, 2} {
		member := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: user.ID, Role: models.WorkspaceRoleDeveloper, Status: models.WorkspaceMemberStatusActive}
		if err := db.Create(&member).Error; err != nil {
			t.Fatalf("create duplicate membership failed: %v", err)
		}
	}

	result, err := usecase.ListWorkspaces(context.Background(), ListWorkspacesRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}})
	if err != nil {
		t.Fatalf("ListWorkspaces returned error: %v", err)
	}
	if len(result.List) != 1 {
		t.Fatalf("workspace count=%d, want 1; list=%+v", len(result.List), result.List)
	}
	if result.List[0].ID != workspace.ID {
		t.Fatalf("workspace id=%d, want %d", result.List[0].ID, workspace.ID)
	}
}

func TestWorkspaceUseCaseListPaginatesOnlyWhenRequested(t *testing.T) {
	db := openActorTestDB(t)
	usecase := &WorkspaceUseCase{DB: db}

	user := models.User{Username: "workspace-paginate-user", Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	for i := 0; i < 25; i++ {
		workspace := models.Workspace{Name: "paged-space-" + string(rune('a'+i)), Slug: "paged-space-" + string(rune('a'+i)), Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: user.ID}
		if err := db.Create(&workspace).Error; err != nil {
			t.Fatalf("create workspace failed: %v", err)
		}
		if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: user.ID, Role: models.WorkspaceRoleDeveloper, Status: models.WorkspaceMemberStatusActive}).Error; err != nil {
			t.Fatalf("create membership failed: %v", err)
		}
	}

	fullResult, err := usecase.ListWorkspaces(context.Background(), ListWorkspacesRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}})
	if err != nil {
		t.Fatalf("ListWorkspaces full returned error: %v", err)
	}
	if len(fullResult.List) != 25 {
		t.Fatalf("full workspace count=%d, want 25", len(fullResult.List))
	}

	pagedResult, err := usecase.ListWorkspaces(context.Background(), ListWorkspacesRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, Page: 1, Limit: 20, Paginate: true})
	if err != nil {
		t.Fatalf("ListWorkspaces paged returned error: %v", err)
	}
	if len(pagedResult.List) != 20 {
		t.Fatalf("paged workspace count=%d, want 20", len(pagedResult.List))
	}
}

func TestWorkspaceUseCaseListIncludesRoleAndCapabilities(t *testing.T) {
	db := openActorTestDB(t)
	usecase := &WorkspaceUseCase{DB: db}

	user := models.User{Username: "workspace-role-user", Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: "blank-kind-space", Slug: "blank-kind-space", Description: "searchable description", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: "", CreatedBy: user.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	member := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: user.ID, Role: models.WorkspaceRoleMaintainer, Status: models.WorkspaceMemberStatusActive}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create membership failed: %v", err)
	}

	result, err := usecase.ListWorkspaces(context.Background(), ListWorkspacesRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, Query: "searchable", Page: 1, Limit: 10})
	if err != nil {
		t.Fatalf("ListWorkspaces returned error: %v", err)
	}
	if len(result.List) != 1 {
		t.Fatalf("workspace count=%d, want 1", len(result.List))
	}
	item := result.List[0]
	if item.Role != models.WorkspaceRoleMaintainer {
		t.Fatalf("role=%q, want %q", item.Role, models.WorkspaceRoleMaintainer)
	}
	if item.Kind != models.WorkspaceKindNormal {
		t.Fatalf("kind=%q, want %q", item.Kind, models.WorkspaceKindNormal)
	}
	if len(item.Capabilities) == 0 {
		t.Fatal("expected capabilities")
	}
	capSet := make(map[string]bool, len(item.Capabilities))
	for _, capability := range item.Capabilities {
		capSet[capability] = true
	}
	if !capSet["workspace.member.manage"] || !capSet["agent.approve"] {
		t.Fatalf("capabilities=%v, want maintainer capabilities", item.Capabilities)
	}
}

func TestWorkspaceUseCaseRejectsNonMemberGet(t *testing.T) {
	db := openActorTestDB(t)
	usecase := &WorkspaceUseCase{DB: db}

	user := models.User{Username: "workspace-nonmember-user", Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: "private-space", Slug: "private-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: user.ID + 999}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}

	_, err := usecase.GetWorkspace(context.Background(), GetWorkspaceRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, WorkspaceID: workspace.ID})
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
