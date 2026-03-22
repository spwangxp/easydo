package middleware

import (
	"testing"

	"easydo-server/internal/models"
)

func TestWorkspaceRoleAtLeast(t *testing.T) {
	if !WorkspaceRoleAtLeast(models.WorkspaceRoleOwner, models.WorkspaceRoleMaintainer) {
		t.Fatalf("owner should satisfy maintainer role")
	}
	if !WorkspaceRoleAtLeast(models.WorkspaceRoleDeveloper, models.WorkspaceRoleViewer) {
		t.Fatalf("developer should satisfy viewer role")
	}
	if WorkspaceRoleAtLeast(models.WorkspaceRoleViewer, models.WorkspaceRoleDeveloper) {
		t.Fatalf("viewer should not satisfy developer role")
	}
}

func TestExpandWorkspaceCapabilitiesForDeveloper(t *testing.T) {
	capabilities := ExpandWorkspaceCapabilities(models.WorkspaceRoleDeveloper)
	capSet := make(map[string]bool, len(capabilities))
	for _, capability := range capabilities {
		capSet[capability] = true
	}

	required := []string{
		"workspace.read",
		"project.write",
		"pipeline.run",
		"credential.read",
		"credential.write",
		"credential.value.read",
		"resource.read",
		"resource.use",
		"resource.monitor.read",
		"store.template.read",
		"store.template.use",
	}
	for _, capability := range required {
		if !capSet[capability] {
			t.Fatalf("expected developer capability %s", capability)
		}
	}
	if capSet["workspace.member.manage"] {
		t.Fatalf("developer should not manage workspace members")
	}
	if capSet["resource.write"] || capSet["resource.operate"] || capSet["store.template.manage"] {
		t.Fatalf("developer should not manage resources or templates")
	}
}

func TestExpandWorkspaceCapabilitiesForMaintainer(t *testing.T) {
	capabilities := ExpandWorkspaceCapabilities(models.WorkspaceRoleMaintainer)
	capSet := make(map[string]bool, len(capabilities))
	for _, capability := range capabilities {
		capSet[capability] = true
	}

	required := []string{
		"workspace.member.manage",
		"workspace.invitation.manage",
		"agent.write",
		"agent.approve",
		"agent.token.rotate",
		"resource.read",
		"resource.use",
		"resource.write",
		"resource.operate",
		"resource.monitor.read",
		"resource.monitor.write",
		"store.template.read",
		"store.template.use",
		"store.template.manage",
	}
	for _, capability := range required {
		if !capSet[capability] {
			t.Fatalf("expected maintainer capability %s", capability)
		}
	}
	if capSet["workspace.delete"] {
		t.Fatalf("maintainer should not delete workspace")
	}
}

func TestExpandWorkspaceCapabilitiesForViewer(t *testing.T) {
	capabilities := ExpandWorkspaceCapabilities(models.WorkspaceRoleViewer)
	capSet := make(map[string]bool, len(capabilities))
	for _, capability := range capabilities {
		capSet[capability] = true
	}

	required := []string{
		"resource.read",
		"resource.monitor.read",
		"store.template.read",
	}
	for _, capability := range required {
		if !capSet[capability] {
			t.Fatalf("expected viewer capability %s", capability)
		}
	}

	forbidden := []string{
		"resource.use",
		"resource.write",
		"resource.operate",
		"resource.monitor.write",
		"store.template.use",
		"store.template.manage",
	}
	for _, capability := range forbidden {
		if capSet[capability] {
			t.Fatalf("viewer should not have capability %s", capability)
		}
	}
}
