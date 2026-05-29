package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"easydo-server/internal/models"
	"easydo-server/internal/workspaceauth"

	"gorm.io/gorm"
)

const (
	defaultResourcePageLimit          = 20
	maxResourcePageLimit              = 100
	resourceValueMaxStringChars       = 1024
	resourceValueMaxListItems         = 50
	resourceValueMaxDepth             = 6
	resourceValueMaxMapEntries        = 40
	resourceValueMaxSerializedBytes   = 3000
	resourceRedactedPlaceholder       = "[REDACTED]"
	resourceTruncatedDepthPlaceholder = "[truncated depth]"
)

type ResourceUseCase struct {
	DB *gorm.DB
}

type ListResourcesRequest struct {
	Actor       ActorContext
	WorkspaceID uint64
	Page        int
	Limit       int
	Query       string
	Type        string
	Status      string
}

type GetResourceRequest struct {
	Actor       ActorContext
	WorkspaceID uint64
	ResourceID  uint64
}

type GetResourceStatusRequest struct {
	Actor       ActorContext
	WorkspaceID uint64
	ResourceID  uint64
}

type ResourceSummary struct {
	ID          uint64                `json:"id"`
	WorkspaceID uint64                `json:"workspace_id"`
	ProjectID   *uint64               `json:"project_id,omitempty"`
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	Type        models.ResourceType   `json:"type"`
	Environment string                `json:"environment,omitempty"`
	Status      models.ResourceStatus `json:"status"`
	Endpoint    string                `json:"endpoint,omitempty"`
	LastCheckAt int64                 `json:"last_check_at,omitempty"`
	CreatedBy   uint64                `json:"created_by"`
	CreatedAt   time.Time             `json:"created_at"`
	UpdatedAt   time.Time             `json:"updated_at"`
}

type ResourceDetail struct {
	ResourceSummary
	Labels   any `json:"labels,omitempty"`
	Metadata any `json:"metadata,omitempty"`
}

type ResourceStatusSummary struct {
	ID                  uint64                `json:"id"`
	WorkspaceID         uint64                `json:"workspace_id"`
	Name                string                `json:"name"`
	Type                models.ResourceType   `json:"type"`
	Status              models.ResourceStatus `json:"status"`
	BaseInfoStatus      string                `json:"base_info_status,omitempty"`
	BaseInfoSource      string                `json:"base_info_source,omitempty"`
	BaseInfoLastError   string                `json:"base_info_last_error,omitempty"`
	BaseInfoCollectedAt int64                 `json:"base_info_collected_at,omitempty"`
	LastCheckAt         int64                 `json:"last_check_at,omitempty"`
	LastCheckResult     string                `json:"last_check_result,omitempty"`
	BaseInfo            any                   `json:"base_info,omitempty"`
}

type ResourceListResult struct {
	List  []ResourceSummary `json:"list"`
	Total int64             `json:"total"`
	Page  int               `json:"page"`
	Limit int               `json:"limit"`
}

func (u *ResourceUseCase) ListResources(ctx context.Context, req ListResourcesRequest) (ResourceListResult, error) {
	if err := validateResourceQueryActor(req.Actor); err != nil {
		return ResourceListResult{}, err
	}
	if u == nil || u.DB == nil {
		return ResourceListResult{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "db is required"}
	}
	if _, err := u.resolveWorkspace(ctx, req.Actor, req.WorkspaceID); err != nil {
		return ResourceListResult{}, err
	}
	page, limit := normalizeResourcePagination(req.Page, req.Limit)
	queryText := strings.TrimSpace(req.Query)
	resourceType := strings.TrimSpace(req.Type)
	status := strings.TrimSpace(req.Status)

	dbQuery := u.DB.WithContext(ctx).Model(&models.Resource{}).Where("workspace_id = ?", req.WorkspaceID)
	if queryText != "" {
		like := "%" + queryText + "%"
		dbQuery = dbQuery.Where("name LIKE ? OR description LIKE ? OR endpoint LIKE ?", like, like, like)
	}
	if resourceType != "" {
		dbQuery = dbQuery.Where("type = ?", resourceType)
	}
	if status != "" {
		dbQuery = dbQuery.Where("status = ?", status)
	}

	var total int64
	if err := dbQuery.Count(&total).Error; err != nil {
		return ResourceListResult{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to count resources"}
	}
	var resources []models.Resource
	if err := dbQuery.Order("created_at DESC, id DESC").Offset((page - 1) * limit).Limit(limit).Find(&resources).Error; err != nil {
		return ResourceListResult{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to list resources"}
	}
	result := ResourceListResult{List: make([]ResourceSummary, 0, len(resources)), Total: total, Page: page, Limit: limit}
	for _, resource := range resources {
		result.List = append(result.List, buildResourceSummary(resource))
	}
	return result, nil
}

func (u *ResourceUseCase) GetResource(ctx context.Context, req GetResourceRequest) (ResourceDetail, error) {
	if err := validateResourceQueryActor(req.Actor); err != nil {
		return ResourceDetail{}, err
	}
	if u == nil || u.DB == nil {
		return ResourceDetail{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "db is required"}
	}
	if req.ResourceID == 0 {
		return ResourceDetail{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "resource_id is required"}
	}
	if _, err := u.resolveWorkspace(ctx, req.Actor, req.WorkspaceID); err != nil {
		return ResourceDetail{}, err
	}
	resource, err := u.loadResource(ctx, req.WorkspaceID, req.ResourceID)
	if err != nil {
		return ResourceDetail{}, err
	}
	return buildResourceDetail(*resource), nil
}

func (u *ResourceUseCase) GetResourceStatus(ctx context.Context, req GetResourceStatusRequest) (ResourceStatusSummary, error) {
	if err := validateResourceQueryActor(req.Actor); err != nil {
		return ResourceStatusSummary{}, err
	}
	if u == nil || u.DB == nil {
		return ResourceStatusSummary{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "db is required"}
	}
	if req.ResourceID == 0 {
		return ResourceStatusSummary{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "resource_id is required"}
	}
	if _, err := u.resolveWorkspace(ctx, req.Actor, req.WorkspaceID); err != nil {
		return ResourceStatusSummary{}, err
	}
	resource, err := u.loadResource(ctx, req.WorkspaceID, req.ResourceID)
	if err != nil {
		return ResourceStatusSummary{}, err
	}
	return buildResourceStatusSummary(*resource), nil
}

func validateResourceQueryActor(actor ActorContext) error {
	if actor.UserID == 0 {
		return ServiceError{Code: ErrorCodeInvalidArgument, Message: "actor user id is required"}
	}
	return nil
}

func (u *ResourceUseCase) resolveWorkspace(ctx context.Context, actor ActorContext, workspaceID uint64) (WorkspaceContext, error) {
	if workspaceID == 0 {
		return WorkspaceContext{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "workspace_id is required"}
	}
	resolved, err := ResolveWorkspaceForActor(ctx, u.DB, actor, workspaceID)
	if err != nil {
		return WorkspaceContext{}, err
	}
	if resolved.Workspace == nil {
		return WorkspaceContext{}, ServiceError{Code: ErrorCodeForbidden, Message: "workspace access denied"}
	}
	if !workspaceauth.WorkspaceRoleAtLeast(resolved.Role, models.WorkspaceRoleViewer) {
		return WorkspaceContext{}, ServiceError{Code: ErrorCodeForbidden, Message: "workspace read access denied"}
	}
	return resolved, nil
}

func (u *ResourceUseCase) loadResource(ctx context.Context, workspaceID uint64, resourceID uint64) (*models.Resource, error) {
	var resource models.Resource
	if err := u.DB.WithContext(ctx).Where("id = ? AND workspace_id = ?", resourceID, workspaceID).First(&resource).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ServiceError{Code: ErrorCodeNotFound, Message: "resource not found"}
		}
		return nil, ServiceError{Code: ErrorCodeInternalError, Message: "failed to load resource"}
	}
	return &resource, nil
}

func buildResourceSummary(resource models.Resource) ResourceSummary {
	return ResourceSummary{
		ID:          resource.ID,
		WorkspaceID: resource.WorkspaceID,
		ProjectID:   resource.ProjectID,
		Name:        sanitizeResourceText(resource.Name),
		Description: sanitizeResourceText(resource.Description),
		Type:        resource.Type,
		Environment: sanitizeResourceText(resource.Environment),
		Status:      resource.Status,
		Endpoint:    sanitizeResourceText(resource.Endpoint),
		LastCheckAt: resource.LastCheckAt,
		CreatedBy:   resource.CreatedBy,
		CreatedAt:   resource.CreatedAt,
		UpdatedAt:   resource.UpdatedAt,
	}
}

func buildResourceDetail(resource models.Resource) ResourceDetail {
	return ResourceDetail{
		ResourceSummary: buildResourceSummary(resource),
		Labels:          sanitizeResourceJSONText(resource.Labels),
		Metadata:        sanitizeResourceJSONText(resource.Metadata),
	}
}

func buildResourceStatusSummary(resource models.Resource) ResourceStatusSummary {
	return ResourceStatusSummary{
		ID:                  resource.ID,
		WorkspaceID:         resource.WorkspaceID,
		Name:                sanitizeResourceText(resource.Name),
		Type:                resource.Type,
		Status:              resource.Status,
		BaseInfoStatus:      sanitizeResourceText(resource.BaseInfoStatus),
		BaseInfoSource:      sanitizeResourceText(resource.BaseInfoSource),
		BaseInfoLastError:   sanitizeResourceText(resource.BaseInfoLastError),
		BaseInfoCollectedAt: resource.BaseInfoCollectedAt,
		LastCheckAt:         resource.LastCheckAt,
		LastCheckResult:     sanitizeResourceText(resource.LastCheckResult),
		BaseInfo:            sanitizeResourceJSONText(resource.BaseInfo),
	}
}

func sanitizeResourceJSONText(raw string) any {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return sanitizeResourceValue(trimmed)
	}
	return sanitizeResourceValue(decoded)
}

func sanitizeResourceValue(value any) any {
	sanitized := sanitizeResourceValueAtDepth(value, 1)
	serialized, err := json.Marshal(sanitized)
	if err == nil && len(serialized) > resourceValueMaxSerializedBytes {
		return map[string]any{"summary": "[truncated]", "serialized_bytes": len(serialized)}
	}
	return sanitized
}

func sanitizeResourceValueAtDepth(value any, depth int) any {
	if depth > resourceValueMaxDepth {
		return resourceTruncatedDepthPlaceholder
	}
	switch typed := value.(type) {
	case map[string]any:
		return sanitizeResourceMap(typed, depth)
	case []any:
		return sanitizeResourceList(typed, depth)
	case string:
		return sanitizeResourceText(typed)
	case json.Number:
		return typed
	case nil, bool, float64, int, int64, uint64:
		return typed
	case fmt.Stringer:
		return sanitizeResourceText(typed.String())
	default:
		return sanitizeResourceText(fmt.Sprintf("%v", typed))
	}
}

func sanitizeResourceMap(input map[string]any, depth int) map[string]any {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make(map[string]any)
	truncated := 0
	for _, key := range keys {
		if isResourceSensitiveKey(key) {
			continue
		}
		if len(result) >= resourceValueMaxMapEntries {
			truncated++
			continue
		}
		result[key] = sanitizeResourceValueAtDepth(input[key], depth+1)
	}
	if truncated > 0 {
		result["__truncated"] = fmt.Sprintf("[truncated] %d fields", truncated)
	}
	return result
}

func sanitizeResourceList(input []any, depth int) []any {
	limit := len(input)
	truncated := false
	if limit > resourceValueMaxListItems {
		limit = resourceValueMaxListItems
		truncated = true
	}
	result := make([]any, 0, limit)
	for i := 0; i < limit; i++ {
		result = append(result, sanitizeResourceValueAtDepth(input[i], depth+1))
	}
	if truncated && len(result) > 0 {
		result[len(result)-1] = fmt.Sprintf("[truncated %d items]", len(input)-resourceValueMaxListItems+1)
	}
	return result
}

func sanitizeResourceText(value string) string {
	if isResourceSensitiveString(value) {
		return resourceRedactedPlaceholder
	}
	return truncateResourceString(value, resourceValueMaxStringChars)
}

func isResourceSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	compact := strings.NewReplacer("_", "", "-", "", " ", "").Replace(normalized)
	for _, part := range []string{"token", "secret", "password", "credential", "authorization", "apikey", "kubeconfig", "clientkeydata", "clientcertificatedata", "privatekey"} {
		if strings.Contains(compact, part) {
			return true
		}
	}
	return false
}

func isResourceSensitiveString(value string) bool {
	lower := strings.ToLower(value)
	if containsCredentialBearingURI(lower) || looksLikeKubeconfig(lower) || looksLikePrivateKey(lower) {
		return true
	}
	for _, pattern := range []string{
		"bearer ", "basic ", "authorization:", "authorization=",
		"token=", "token:", "secret=", "secret:", "password=", "password:",
		"api_key=", "api-key=", "apikey=", "api_key:", "api-key:", "apikey:",
		"credential=", "credential:", "client-key-data:", "client-certificate-data:",
	} {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

func containsCredentialBearingURI(value string) bool {
	searchFrom := 0
	for {
		idx := strings.Index(value[searchFrom:], "://")
		if idx < 0 {
			return false
		}
		idx += searchFrom
		rest := value[idx+3:]
		end := strings.IndexAny(rest, " \t\n\r\"'<>`)")
		if end >= 0 {
			rest = rest[:end]
		}
		if at := strings.Index(rest, "@"); at > 0 && strings.Contains(rest[:at], ":") {
			return true
		}
		searchFrom = idx + 3
		if searchFrom >= len(value) {
			return false
		}
	}
}

func looksLikeKubeconfig(value string) bool {
	return (strings.Contains(value, "apiversion:") && strings.Contains(value, "clusters:")) || strings.Contains(value, "client-key-data:") || strings.Contains(value, "client-certificate-data:")
}

func looksLikePrivateKey(value string) bool {
	return strings.Contains(value, "-----begin ") && (strings.Contains(value, "private key") || strings.Contains(value, "certificate-----"))
}

func truncateResourceString(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	marker := "...[truncated]"
	markerRunes := []rune(marker)
	if max <= len(markerRunes) {
		return string(markerRunes[:max])
	}
	return string(runes[:max-len(markerRunes)]) + marker
}

func normalizeResourcePagination(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = defaultResourcePageLimit
	}
	if limit > maxResourcePageLimit {
		limit = maxResourcePageLimit
	}
	return page, limit
}
