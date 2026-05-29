package models

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMCPCallAuditModelMigrates(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:mcp_audit_model?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&MCPCallAudit{}); err != nil {
		t.Fatalf("migrate audit: %v", err)
	}
	audit := MCPCallAudit{
		RequestID:     "req-1",
		UserID:        10,
		WorkspaceID:   20,
		ToolName:      "easydo_workspace_list",
		OperationType: "read",
		TargetType:    "workspace",
		TargetID:      "20",
		Protocol:      "streamable_http",
		Status:        "success",
		DurationMS:    12,
	}
	if err := db.Create(&audit).Error; err != nil {
		t.Fatalf("create audit: %v", err)
	}
	if audit.ID == 0 {
		t.Fatalf("expected audit id")
	}
}
