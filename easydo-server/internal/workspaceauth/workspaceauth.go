package workspaceauth

import (
	"fmt"
	"sort"
	"strings"

	"easydo-server/internal/models"
)

var workspaceRoleOrder = map[string]int{
	models.WorkspaceRoleViewer:     10,
	models.WorkspaceRoleDeveloper:  20,
	models.WorkspaceRoleMaintainer: 30,
	models.WorkspaceRoleOwner:      40,
}

func WorkspaceRoleRank(role string) int {
	return workspaceRoleOrder[models.NormalizeWorkspaceRole(role)]
}

func WorkspaceRoleAtLeast(role string, expected string) bool {
	return WorkspaceRoleRank(role) >= WorkspaceRoleRank(expected)
}

func WorkspaceVisibleToSystemRole(systemRole string, workspaceKind string) bool {
	if strings.EqualFold(strings.TrimSpace(systemRole), "admin") {
		return true
	}
	return normalizeWorkspaceKindForAccess(workspaceKind) != models.WorkspaceKindAdmin
}

func NonAdminVisibleWorkspaceCondition(alias string) (string, []any) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		alias = "workspaces"
	}
	return fmt.Sprintf("COALESCE(NULLIF(TRIM(%s.kind), ''), ?) <> ?", alias), []any{models.WorkspaceKindNormal, models.WorkspaceKindAdmin}
}

func ExpandWorkspaceCapabilities(role string) []string {
	role = models.NormalizeWorkspaceRole(role)
	capSet := map[string]struct{}{
		"workspace.read":        {},
		"workspace.member.read": {},
		"project.read":          {},
		"pipeline.read":         {},
		"pipeline.run.read":     {},
		"agent.read":            {},
		"credential.read":       {},
		"resource.read":         {},
		"resource.monitor.read": {},
		"store.template.read":   {},
	}

	if WorkspaceRoleAtLeast(role, models.WorkspaceRoleDeveloper) {
		for _, capability := range []string{
			"project.write",
			"pipeline.write",
			"pipeline.run",
			"credential.write",
			"credential.value.read",
			"resource.use",
			"store.template.use",
		} {
			capSet[capability] = struct{}{}
		}
	}

	if WorkspaceRoleAtLeast(role, models.WorkspaceRoleMaintainer) {
		for _, capability := range []string{
			"workspace.member.manage",
			"workspace.invitation.manage",
			"agent.write",
			"agent.approve",
			"agent.token.rotate",
			"resource.write",
			"resource.operate",
			"resource.monitor.write",
			"store.template.manage",
		} {
			capSet[capability] = struct{}{}
		}
	}

	if WorkspaceRoleAtLeast(role, models.WorkspaceRoleOwner) {
		capSet["workspace.write"] = struct{}{}
		capSet["workspace.delete"] = struct{}{}
	}

	capabilities := make([]string, 0, len(capSet))
	for capability := range capSet {
		capabilities = append(capabilities, capability)
	}
	sort.Strings(capabilities)
	return capabilities
}

func normalizeWorkspaceKindForAccess(kind string) string {
	if strings.EqualFold(strings.TrimSpace(kind), models.WorkspaceKindAdmin) {
		return models.WorkspaceKindAdmin
	}
	return models.WorkspaceKindNormal
}
