package mcp

import (
	"context"

	"easydo-server/internal/middleware"
	"easydo-server/internal/services"

	"gorm.io/gorm"
)

func AuthenticateRequest(ctx context.Context, db *gorm.DB, authorization string) (services.ActorContext, error) {
	token, err := middleware.ExtractBearerToken(authorization)
	if err != nil {
		return services.ActorContext{}, services.ServiceError{Code: services.ErrorCodeUnauthorized, Message: "unauthorized"}
	}
	if db != nil {
		if actor, tokenErr := services.NewMCPTokenService(db).Authenticate(ctx, token); tokenErr == nil {
			return actor, nil
		}
	}
	claims, err := middleware.ParseToken(token)
	if err != nil {
		return services.ActorContext{}, services.ServiceError{Code: services.ErrorCodeUnauthorized, Message: "unauthorized"}
	}
	if err := middleware.ValidateTokenSession(ctx, claims); err != nil {
		return services.ActorContext{}, services.ServiceError{Code: services.ErrorCodeUnauthorized, Message: "unauthorized"}
	}
	return services.ActorContext{
		UserID:     claims.UserID,
		Username:   claims.Username,
		SystemRole: claims.Role,
		SessionID:  claims.SessionID,
	}, nil
}

func ResolveWorkspaceForTool(ctx context.Context, db *gorm.DB, actor services.ActorContext, workspaceID uint64) (services.WorkspaceContext, error) {
	if actor.CurrentWorkspaceID != 0 && workspaceID != actor.CurrentWorkspaceID {
		return services.WorkspaceContext{}, services.ServiceError{Code: services.ErrorCodeForbidden, Message: "workspace access denied"}
	}
	return services.ResolveWorkspaceForActor(ctx, db, actor, workspaceID)
}

func enforceMCPTokenWorkspace(actor services.ActorContext, workspaceID uint64) error {
	if actor.CurrentWorkspaceID == 0 || workspaceID == actor.CurrentWorkspaceID {
		return nil
	}
	return services.ServiceError{Code: services.ErrorCodeForbidden, Message: "workspace access denied"}
}
