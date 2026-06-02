package services

import (
	"context"
	"encoding/json"
	"testing"

	"easydo-server/internal/models"

	"gorm.io/gorm"
)

func openAuditLogServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := openActorTestDB(t)
	if err := db.AutoMigrate(&models.AuditLog{}); err != nil {
		t.Fatalf("auto migrate audit log failed: %v", err)
	}
	return db
}

func TestAppendAuditLogPersistsGovernanceEvent(t *testing.T) {
	db := openAuditLogServiceTestDB(t)

	actor := models.User{Username: "audit-actor", Role: "admin", Status: "active"}
	if err := db.Create(&actor).Error; err != nil {
		t.Fatalf("create actor failed: %v", err)
	}
	workspace := models.Workspace{Name: "audit-workspace", Slug: "audit-workspace", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindAdmin, CreatedBy: actor.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}

	err := AppendAuditLog(context.Background(), db, AuditInput{
		WorkspaceID:      &workspace.ID,
		ActorUserID:      actor.ID,
		ActorRole:        actor.Role,
		ActorWorkspaceID: &workspace.ID,
		Action:           "user.disable",
		TargetType:       "user",
		TargetID:         42,
		Before: map[string]any{
			"status": "active",
		},
		After: map[string]any{
			"status": "disabled",
		},
		Metadata: map[string]any{
			"reason": "left company",
		},
		IP:        "127.0.0.1",
		UserAgent: "audit-test",
	})
	if err != nil {
		t.Fatalf("AppendAuditLog returned error: %v", err)
	}

	var stored models.AuditLog
	if err := db.First(&stored).Error; err != nil {
		t.Fatalf("load audit log failed: %v", err)
	}
	if stored.WorkspaceID == nil || *stored.WorkspaceID != workspace.ID {
		t.Fatalf("workspace_id=%v, want %d", stored.WorkspaceID, workspace.ID)
	}
	if stored.ActorUserID != actor.ID || stored.ActorRole != "admin" || stored.ActorWorkspaceID == nil || *stored.ActorWorkspaceID != workspace.ID {
		t.Fatalf("unexpected actor fields: %+v", stored)
	}
	if stored.Action != "user.disable" || stored.TargetType != "user" || stored.TargetID != 42 {
		t.Fatalf("unexpected target fields: %+v", stored)
	}
	if stored.IP != "127.0.0.1" || stored.UserAgent != "audit-test" {
		t.Fatalf("unexpected request fields: %+v", stored)
	}

	var before map[string]string
	if err := json.Unmarshal([]byte(stored.BeforeJSON), &before); err != nil {
		t.Fatalf("unmarshal before_json failed: %v", err)
	}
	if before["status"] != "active" {
		t.Fatalf("before_json=%s, want active status", stored.BeforeJSON)
	}
	var after map[string]string
	if err := json.Unmarshal([]byte(stored.AfterJSON), &after); err != nil {
		t.Fatalf("unmarshal after_json failed: %v", err)
	}
	if after["status"] != "disabled" {
		t.Fatalf("after_json=%s, want disabled status", stored.AfterJSON)
	}
	var metadata map[string]string
	if err := json.Unmarshal([]byte(stored.MetadataJSON), &metadata); err != nil {
		t.Fatalf("unmarshal metadata_json failed: %v", err)
	}
	if metadata["reason"] != "left company" {
		t.Fatalf("metadata_json=%s, want reason", stored.MetadataJSON)
	}
}

func TestAppendAuditLogLeavesNilSnapshotsEmpty(t *testing.T) {
	db := openAuditLogServiceTestDB(t)

	err := AppendAuditLog(context.Background(), db, AuditInput{
		ActorUserID: 1,
		Action:      "workspace.member.remove",
		TargetType:  "workspace_member",
		TargetID:    7,
	})
	if err != nil {
		t.Fatalf("AppendAuditLog returned error: %v", err)
	}

	var stored models.AuditLog
	if err := db.First(&stored).Error; err != nil {
		t.Fatalf("load audit log failed: %v", err)
	}
	if stored.BeforeJSON != "" || stored.AfterJSON != "" || stored.MetadataJSON != "" {
		t.Fatalf("nil snapshots should stay empty, got before=%q after=%q metadata=%q", stored.BeforeJSON, stored.AfterJSON, stored.MetadataJSON)
	}
}
