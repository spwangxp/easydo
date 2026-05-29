package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"easydo-server/internal/models"

	"gorm.io/gorm"
)

func openResourceQueryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := openActorTestDB(t)
	if err := db.AutoMigrate(&models.Resource{}, &models.ResourceCredentialBinding{}, &models.ResourceHealthSnapshot{}); err != nil {
		t.Fatalf("auto migrate resource query models failed: %v", err)
	}
	return db
}

func seedResourceQueryWorkspaceMember(t *testing.T, db *gorm.DB, username string, role string) (models.User, models.Workspace) {
	t.Helper()
	user := models.User{Username: username, Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: username + "-workspace", Slug: username + "-workspace", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: user.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	member := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: user.ID, Role: role, Status: models.WorkspaceMemberStatusActive}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create membership failed: %v", err)
	}
	return user, workspace
}

func TestResourceQueryListIsScopedToWorkspace(t *testing.T) {
	db := openResourceQueryTestDB(t)
	usecase := &ResourceUseCase{DB: db}
	user, workspace := seedResourceQueryWorkspaceMember(t, db, "resource-list-user", models.WorkspaceRoleViewer)
	_, otherWorkspace := seedResourceQueryWorkspaceMember(t, db, "resource-list-other", models.WorkspaceRoleViewer)

	visible := models.Resource{WorkspaceID: workspace.ID, Name: "visible-resource", Type: models.ResourceTypeVM, Status: models.ResourceStatusOnline, Endpoint: "10.0.0.1", CreatedBy: user.ID, Metadata: `{"secret":"hidden"}`}
	hidden := models.Resource{WorkspaceID: otherWorkspace.ID, Name: "hidden-resource", Type: models.ResourceTypeVM, Status: models.ResourceStatusOnline, Endpoint: "10.0.0.2", CreatedBy: user.ID}
	for _, resource := range []*models.Resource{&visible, &hidden} {
		if err := db.Create(resource).Error; err != nil {
			t.Fatalf("create resource failed: %v", err)
		}
	}

	result, err := usecase.ListResources(context.Background(), ListResourcesRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, WorkspaceID: workspace.ID})
	if err != nil {
		t.Fatalf("ListResources returned error: %v", err)
	}
	if result.Total != 1 {
		t.Fatalf("total=%d, want 1", result.Total)
	}
	if len(result.List) != 1 || result.List[0].ID != visible.ID {
		t.Fatalf("list=%+v, want only resource %d", result.List, visible.ID)
	}
	serialized, err := json.Marshal(result.List[0])
	if err != nil {
		t.Fatalf("marshal summary failed: %v", err)
	}
	for _, forbidden := range []string{"metadata", "credential", "secret", "token"} {
		if strings.Contains(strings.ToLower(string(serialized)), forbidden) {
			t.Fatalf("summary leaked forbidden field %q: %s", forbidden, string(serialized))
		}
	}
}

func TestResourceQueryGetRejectsOtherWorkspace(t *testing.T) {
	db := openResourceQueryTestDB(t)
	usecase := &ResourceUseCase{DB: db}
	user, workspace := seedResourceQueryWorkspaceMember(t, db, "resource-get-user", models.WorkspaceRoleViewer)
	_, otherWorkspace := seedResourceQueryWorkspaceMember(t, db, "resource-get-other", models.WorkspaceRoleViewer)
	resource := models.Resource{WorkspaceID: otherWorkspace.ID, Name: "other-resource", Type: models.ResourceTypeVM, Status: models.ResourceStatusOnline, CreatedBy: user.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	_, err := usecase.GetResource(context.Background(), GetResourceRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, WorkspaceID: workspace.ID, ResourceID: resource.ID})
	if err == nil {
		t.Fatal("expected not found error")
	}
	var svcErr ServiceError
	if !errors.As(err, &svcErr) || svcErr.Code != ErrorCodeNotFound {
		t.Fatalf("error=%v, want not found ServiceError", err)
	}
}

func TestResourceStatusReturnsSanitizedBaseInfo(t *testing.T) {
	db := openResourceQueryTestDB(t)
	usecase := &ResourceUseCase{DB: db}
	user, workspace := seedResourceQueryWorkspaceMember(t, db, "resource-status-user", models.WorkspaceRoleViewer)
	baseInfo := `{"nodes":[{"name":"node-1","token":"node-token","cpu":"8"}],"summary":{"ready":true,"authorization":"Bearer abc"},"safe":"ok"}`
	resource := models.Resource{WorkspaceID: workspace.ID, Name: "status-resource", Type: models.ResourceTypeK8sCluster, Status: models.ResourceStatusOnline, BaseInfo: baseInfo, BaseInfoStatus: "success", BaseInfoSource: "agent", BaseInfoLastError: "password=hidden", BaseInfoCollectedAt: 123, LastCheckResult: "token=health-token", CreatedBy: user.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	result, err := usecase.GetResourceStatus(context.Background(), GetResourceStatusRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, WorkspaceID: workspace.ID, ResourceID: resource.ID})
	if err != nil {
		t.Fatalf("GetResourceStatus returned error: %v", err)
	}
	if result.BaseInfoStatus != "success" || result.BaseInfoSource != "agent" || result.BaseInfoCollectedAt != 123 {
		t.Fatalf("status summary=%+v, want base info status/source/collected_at", result)
	}
	serialized, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal status failed: %v", err)
	}
	body := strings.ToLower(string(serialized))
	for _, forbidden := range []string{"node-token", "bearer abc", "authorization", "password=hidden", "health-token"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("status leaked forbidden text %q: %s", forbidden, string(serialized))
		}
	}
	if !strings.Contains(body, "safe") {
		t.Fatalf("status dropped safe base info: %s", string(serialized))
	}
}

func TestResourceStatusTruncatesLargeMeasures(t *testing.T) {
	db := openResourceQueryTestDB(t)
	usecase := &ResourceUseCase{DB: db}
	user, workspace := seedResourceQueryWorkspaceMember(t, db, "resource-large-user", models.WorkspaceRoleViewer)
	large := strings.Repeat("x", 5000)
	baseInfo := `{"measures":{"logs":"` + large + `"},"items":["` + large + `"]}`
	resource := models.Resource{WorkspaceID: workspace.ID, Name: "large-resource", Type: models.ResourceTypeVM, Status: models.ResourceStatusOnline, BaseInfo: baseInfo, CreatedBy: user.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	result, err := usecase.GetResourceStatus(context.Background(), GetResourceStatusRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, WorkspaceID: workspace.ID, ResourceID: resource.ID})
	if err != nil {
		t.Fatalf("GetResourceStatus returned error: %v", err)
	}
	serialized, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal status failed: %v", err)
	}
	if len(serialized) > 4500 || !strings.Contains(string(serialized), "[truncated]") {
		t.Fatalf("status was not capped/truncated, len=%d body=%s", len(serialized), string(serialized))
	}
}

func TestResourceQueryRejectsNonMemberWorkspace(t *testing.T) {
	db := openResourceQueryTestDB(t)
	usecase := &ResourceUseCase{DB: db}
	user := models.User{Username: "resource-nonmember-user", Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: "resource-private-workspace", Slug: "resource-private-workspace", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: user.ID + 100}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}

	_, err := usecase.ListResources(context.Background(), ListResourcesRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, WorkspaceID: workspace.ID})
	if err == nil {
		t.Fatal("expected forbidden error")
	}
	var svcErr ServiceError
	if !errors.As(err, &svcErr) || svcErr.Code != ErrorCodeForbidden {
		t.Fatalf("error=%v, want forbidden ServiceError", err)
	}
}

func TestResourceQueryRejectsInsufficientReadRoleIfBusinessPolicyRequires(t *testing.T) {
	db := openResourceQueryTestDB(t)
	usecase := &ResourceUseCase{DB: db}
	user, workspace := seedResourceQueryWorkspaceMember(t, db, "resource-viewer-user", models.WorkspaceRoleViewer)
	resource := models.Resource{WorkspaceID: workspace.ID, Name: "viewer-readable-resource", Type: models.ResourceTypeVM, Status: models.ResourceStatusOnline, CreatedBy: user.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	result, err := usecase.GetResource(context.Background(), GetResourceRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, WorkspaceID: workspace.ID, ResourceID: resource.ID})
	if err != nil {
		t.Fatalf("viewer should satisfy current resource read policy: %v", err)
	}
	if result.ID != resource.ID {
		t.Fatalf("resource id=%d, want %d", result.ID, resource.ID)
	}
}

func TestResourceQueryRedactsCredentialBearingStrings(t *testing.T) {
	db := openResourceQueryTestDB(t)
	usecase := &ResourceUseCase{DB: db}
	user, workspace := seedResourceQueryWorkspaceMember(t, db, "resource-secret-user", models.WorkspaceRoleViewer)
	kubeconfig := "apiVersion: v1\nclusters:\n- cluster:\n    server: https://cluster.example\nusers:\n- user:\n    client-key-data: kube-client-key-secret\n"
	resource := models.Resource{
		WorkspaceID:       workspace.ID,
		Name:              "secret-resource",
		Type:              models.ResourceTypeK8sCluster,
		Status:            models.ResourceStatusOnline,
		Endpoint:          "postgres://user:pass@host/db",
		Metadata:          `{"apiKey":"camel-secret","api-key":"dash-secret","apikey":"flat-secret","connection":"postgres://meta:secret@db/app","safe":"ok"}`,
		BaseInfo:          `{"endpoint":"postgres://base:secret@db/app","kubeconfig":"` + strings.ReplaceAll(kubeconfig, "\n", "\\n") + `","safe":"ok"}`,
		LastCheckResult:   "connection postgres://health:secret@db/app",
		BaseInfoLastError: kubeconfig,
		CreatedBy:         user.ID,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	listResult, err := usecase.ListResources(context.Background(), ListResourcesRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, WorkspaceID: workspace.ID})
	if err != nil {
		t.Fatalf("ListResources returned error: %v", err)
	}
	getResult, err := usecase.GetResource(context.Background(), GetResourceRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, WorkspaceID: workspace.ID, ResourceID: resource.ID})
	if err != nil {
		t.Fatalf("GetResource returned error: %v", err)
	}
	statusResult, err := usecase.GetResourceStatus(context.Background(), GetResourceStatusRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, WorkspaceID: workspace.ID, ResourceID: resource.ID})
	if err != nil {
		t.Fatalf("GetResourceStatus returned error: %v", err)
	}
	serialized, err := json.Marshal(map[string]any{"list": listResult, "get": getResult, "status": statusResult})
	if err != nil {
		t.Fatalf("marshal resource outputs failed: %v", err)
	}
	body := strings.ToLower(string(serialized))
	for _, forbidden := range []string{
		"postgres://user:pass@host",
		"postgres://meta:secret@db",
		"postgres://base:secret@db",
		"postgres://health:secret@db",
		"camel-secret",
		"dash-secret",
		"flat-secret",
		"kube-client-key-secret",
		"client-key-data",
		"apikey",
		"api-key",
		"api_key",
		"apiversion: v1",
		"clusters:",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("resource outputs leaked forbidden text %q: %s", forbidden, string(serialized))
		}
	}
	if !strings.Contains(body, "safe") {
		t.Fatalf("resource outputs dropped safe values: %s", string(serialized))
	}
}

func TestResourceStatusCapsWideBaseInfo(t *testing.T) {
	db := openResourceQueryTestDB(t)
	usecase := &ResourceUseCase{DB: db}
	user, workspace := seedResourceQueryWorkspaceMember(t, db, "resource-wide-user", models.WorkspaceRoleViewer)
	wide := make(map[string]string, 600)
	for i := 0; i < 600; i++ {
		wide["measure_"+strings.Repeat("x", 4)+string(rune('a'+(i%26)))+string(rune('a'+((i/26)%26)))+string(rune('a'+((i/26/26)%26)))] = strings.Repeat("v", 32)
	}
	raw, err := json.Marshal(wide)
	if err != nil {
		t.Fatalf("marshal wide base info failed: %v", err)
	}
	resource := models.Resource{WorkspaceID: workspace.ID, Name: "wide-resource", Type: models.ResourceTypeVM, Status: models.ResourceStatusOnline, BaseInfo: string(raw), CreatedBy: user.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	result, err := usecase.GetResourceStatus(context.Background(), GetResourceStatusRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, WorkspaceID: workspace.ID, ResourceID: resource.ID})
	if err != nil {
		t.Fatalf("GetResourceStatus returned error: %v", err)
	}
	serialized, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal status failed: %v", err)
	}
	const maxExpectedStatusBytes = 4096
	if len(serialized) > maxExpectedStatusBytes || !strings.Contains(string(serialized), "[truncated]") {
		t.Fatalf("wide status was not finally capped/truncated, len=%d cap=%d body=%s", len(serialized), maxExpectedStatusBytes, string(serialized))
	}
}
