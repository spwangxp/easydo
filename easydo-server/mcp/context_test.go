package mcp

import (
	"context"
	"errors"
	"os"
	"testing"

	"easydo-server/internal/config"
	"easydo-server/internal/middleware"
	"easydo-server/internal/models"
	"easydo-server/internal/services"
	"easydo-server/pkg/utils"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setupMCPAuthTestRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	t.Setenv("JWT_SECRET", "test-secret")
	config.Init()

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}
	previousRedis := utils.RedisClient
	utils.RedisClient = redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() {
		if utils.RedisClient != nil {
			_ = utils.RedisClient.Close()
		}
		utils.RedisClient = previousRedis
		mini.Close()
		_ = os.Unsetenv("JWT_SECRET")
	})
	return mini
}

func TestAuthenticateRejectsMissingBearerToken(t *testing.T) {
	_, err := AuthenticateRequest(context.Background(), "")
	if err == nil {
		t.Fatal("expected unauthorized error")
	}
	var svcErr services.ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("error type=%T, want ServiceError", err)
	}
	if svcErr.Code != services.ErrorCodeUnauthorized {
		t.Fatalf("error code=%q, want %q", svcErr.Code, services.ErrorCodeUnauthorized)
	}
}

func TestAuthenticateReturnsActorFromValidJWTSession(t *testing.T) {
	setupMCPAuthTestRedis(t)
	user := &models.User{BaseModel: models.BaseModel{ID: 2002}, Username: "valid-alice", Role: "admin"}
	tokenValue, _, err := middleware.IssueTokenSession(context.Background(), user)
	if err != nil {
		t.Fatalf("issue token failed: %v", err)
	}

	actor, err := AuthenticateRequest(context.Background(), "Bearer "+tokenValue)
	if err != nil {
		t.Fatalf("AuthenticateRequest returned error: %v", err)
	}
	if actor.UserID != user.ID {
		t.Fatalf("actor.UserID=%d, want %d", actor.UserID, user.ID)
	}
	if actor.Username != user.Username {
		t.Fatalf("actor.Username=%q, want %q", actor.Username, user.Username)
	}
	if actor.SystemRole != user.Role {
		t.Fatalf("actor.SystemRole=%q, want %q", actor.SystemRole, user.Role)
	}
	if actor.SessionID == "" {
		t.Fatal("expected non-empty session id")
	}
}

func TestAuthenticateUsesJWTSessionValidation(t *testing.T) {
	mini := setupMCPAuthTestRedis(t)
	user := &models.User{BaseModel: models.BaseModel{ID: 2001}, Username: "alice", Role: "admin"}
	tokenValue, _, err := middleware.IssueTokenSession(context.Background(), user)
	if err != nil {
		t.Fatalf("issue token failed: %v", err)
	}
	mini.FlushAll()

	_, err = AuthenticateRequest(context.Background(), "Bearer "+tokenValue)
	if err == nil {
		t.Fatal("expected unauthorized error")
	}
	var svcErr services.ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("error type=%T, want ServiceError", err)
	}
	if svcErr.Code != services.ErrorCodeUnauthorized {
		t.Fatalf("error code=%q, want %q", svcErr.Code, services.ErrorCodeUnauthorized)
	}
}

func TestResolveWorkspaceForToolRequiresExplicitWorkspaceID(t *testing.T) {
	db := openAuditTestDB(t)
	resolved, err := ResolveWorkspaceForTool(context.Background(), db, services.ActorContext{UserID: 3001, Username: "admin-user", SystemRole: "admin"}, 0)
	if err != nil {
		t.Fatalf("ResolveWorkspaceForTool returned error: %v", err)
	}
	if resolved.WorkspaceID != 0 || resolved.Workspace != nil {
		t.Fatalf("expected zero workspace context, got %+v", resolved)
	}
}
