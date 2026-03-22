package handlers

import (
	"testing"

	"easydo-server/internal/models"
)

func TestResolveResourceBackedNodeConfig_DockerRunUsesVMResource(t *testing.T) {
	db := openHandlerTestDB(t)

	workspace := models.Workspace{Name: "ws", Slug: "ws", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: 1}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	resource := models.Resource{WorkspaceID: workspace.ID, Name: "prod-vm-01", Type: models.ResourceTypeVM, Environment: "production", Status: models.ResourceStatusOnline, Endpoint: "10.0.0.8:2202", CreatedBy: 1}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}
	binding := models.ResourceCredentialBinding{WorkspaceID: workspace.ID, ResourceID: resource.ID, CredentialID: 99, Purpose: "ssh_auth", BoundBy: 1}
	if err := db.Create(&binding).Error; err != nil {
		t.Fatalf("create binding failed: %v", err)
	}

	config := map[string]interface{}{
		"target_resource_id": resource.ID,
		"host":               "old-host",
		"port":               22,
		"credentials": map[string]interface{}{
			"ssh_auth": map[string]interface{}{"credential_id": 7},
		},
	}

	if err := resolveResourceBackedNodeConfig(db, "docker-run", workspace.ID, config); err != nil {
		t.Fatalf("resolveResourceBackedNodeConfig failed: %v", err)
	}
	if config["host"] != "10.0.0.8" {
		t.Fatalf("expected host from VM resource, got=%#v", config["host"])
	}
	if config["port"] != "2202" {
		t.Fatalf("expected port from VM endpoint, got=%#v", config["port"])
	}
	credentials, _ := config["credentials"].(map[string]interface{})
	sshAuth, _ := credentials["ssh_auth"].(map[string]interface{})
	if sshAuth["credential_id"] != uint64(99) {
		t.Fatalf("expected ssh_auth credential to be overwritten by resource binding, got=%#v", sshAuth)
	}
}

func TestResolveResourceBackedNodeConfig_DockerRunKeepsLegacyHostWithoutTargetResource(t *testing.T) {
	db := openHandlerTestDB(t)
	config := map[string]interface{}{
		"host": "legacy-host",
		"port": 2022,
	}

	if err := resolveResourceBackedNodeConfig(db, "docker-run", 1, config); err != nil {
		t.Fatalf("resolveResourceBackedNodeConfig failed: %v", err)
	}
	if config["host"] != "legacy-host" || config["port"] != 2022 {
		t.Fatalf("expected legacy host/port to remain unchanged, got=%#v", config)
	}
}

func TestResolveResourceBackedNodeConfig_DockerRunRejectsNonVMResource(t *testing.T) {
	db := openHandlerTestDB(t)

	workspace := models.Workspace{Name: "ws2", Slug: "ws2", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: 1}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	resource := models.Resource{WorkspaceID: workspace.ID, Name: "cluster-01", Type: models.ResourceTypeK8sCluster, Environment: "production", Status: models.ResourceStatusOnline, Endpoint: "10.0.0.9", CreatedBy: 1}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	config := map[string]interface{}{"target_resource_id": resource.ID}
	if err := resolveResourceBackedNodeConfig(db, "docker-run", workspace.ID, config); err == nil {
		t.Fatalf("expected non-VM target resource to fail")
	}
}
