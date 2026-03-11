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
		"secret.read",
		"secret.write",
		"secret.value.read",
		"credential.read",
		"credential.write",
		"credential.value.read",
	}
	for _, capability := range required {
		if !capSet[capability] {
			t.Fatalf("expected developer capability %s", capability)
		}
	}
	if capSet["workspace.member.manage"] {
		t.Fatalf("developer should not manage workspace members")
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
