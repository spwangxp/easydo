package notifications

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"easydo-server/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	PermissionPolicyWorkspaceMember = "workspace_member"
	PermissionPolicyWorkspaceInvite = "workspace_invitation"
	PermissionPolicyPlatformAdmin   = "platform_admin"
	PermissionPolicyOpen            = "open"

	FamilyWorkspaceInvitation = "workspace.invitation"
	FamilyWorkspaceMember     = "workspace.member"
	FamilyAgentLifecycle      = "agent.lifecycle"
	FamilyPipelineRun         = "pipeline.run"
	FamilyDeploymentRequest   = "deployment.request"

	EventTypeWorkspaceInvitationCreated  = "workspace.invitation.created"
	EventTypeWorkspaceInvitationAccepted = "workspace.invitation.accepted"
	EventTypeWorkspaceMemberRoleUpdated  = "workspace.member.role_updated"
	EventTypeWorkspaceMemberRemoved      = "workspace.member.removed"
	EventTypeAgentApproved               = "agent.approved"
	EventTypeAgentRejected               = "agent.rejected"
	EventTypeAgentRemoved                = "agent.removed"
	EventTypeAgentOffline                = "agent.offline"
	EventTypePipelineRunSucceeded        = "pipeline.run.succeeded"
	EventTypePipelineRunFailed           = "pipeline.run.failed"
	EventTypePipelineRunCancelled        = "pipeline.run.cancelled"
	EventTypeDeploymentRequestCreated    = "deployment.request.created"
	EventTypeDeploymentRequestQueued     = "deployment.request.queued"
	EventTypeDeploymentRequestRunning    = "deployment.request.running"
	EventTypeDeploymentRequestSucceeded  = "deployment.request.succeeded"
	EventTypeDeploymentRequestFailed     = "deployment.request.failed"
	EventTypeDeploymentRequestCancelled  = "deployment.request.cancelled"
)

type EventInput struct {
	WorkspaceID      uint64
	Family           string
	EventType        string
	ResourceType     string
	ResourceID       uint64
	ActorUserID      uint64
	ActorType        string
	Title            string
	Content          string
	Priority         int
	Metadata         map[string]interface{}
	IdempotencyKey   string
	UserRecipients   []uint64
	EmailRecipients  []string
	Channels         []string
	PermissionPolicy string
}

type EmitResult struct {
	Event         models.NotificationEvent
	Deliveries    []models.NotificationDelivery
	InboxMessages []models.InboxMessage
}

func Emit(db *gorm.DB, input EventInput) (EmitResult, error) {
	if db == nil {
		return EmitResult{}, errors.New("notification db is nil")
	}
	if strings.TrimSpace(input.Family) == "" {
		return EmitResult{}, errors.New("notification family is required")
	}
	if strings.TrimSpace(input.EventType) == "" {
		return EmitResult{}, errors.New("notification event_type is required")
	}
	if strings.TrimSpace(input.Title) == "" {
		return EmitResult{}, errors.New("notification title is required")
	}
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		return EmitResult{}, errors.New("notification idempotency_key is required")
	}
	channels := normalizeChannels(input.Channels)
	policy := normalizePermissionPolicy(input.PermissionPolicy)
	metadataJSON, _ := json.Marshal(input.Metadata)

	var event models.NotificationEvent
	err := db.Transaction(func(tx *gorm.DB) error {
		actorType := strings.TrimSpace(input.ActorType)
		if actorType == "" {
			actorType = "system"
		}
		event = models.NotificationEvent{
			WorkspaceID:    input.WorkspaceID,
			Family:         input.Family,
			EventType:      input.EventType,
			ResourceType:   input.ResourceType,
			ResourceID:     input.ResourceID,
			ActorType:      actorType,
			Title:          input.Title,
			Content:        input.Content,
			Priority:       input.Priority,
			Metadata:       string(metadataJSON),
			IdempotencyKey: input.IdempotencyKey,
		}
		if input.ActorUserID > 0 {
			event.ActorUserID = &input.ActorUserID
		}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "idempotency_key"}}, DoNothing: true}).Create(&event).Error; err != nil {
			return err
		}
		if event.ID == 0 {
			return tx.Where("idempotency_key = ?", input.IdempotencyKey).First(&event).Error
		}

		users, err := loadRecipientUsers(tx, uniqueUint64s(input.UserRecipients))
		if err != nil {
			return err
		}
		for _, user := range users {
			if !recipientPermitted(tx, user, input.WorkspaceID, policy) {
				continue
			}
			audience := models.NotificationAudience{
				EventID:          event.ID,
				WorkspaceID:      input.WorkspaceID,
				RecipientUserID:  &user.ID,
				ResourceType:     input.ResourceType,
				ResourceID:       input.ResourceID,
				PermissionPolicy: policy,
				AudienceKey:      fmt.Sprintf("%d:user:%d", event.ID, user.ID),
			}
			if err := tx.Create(&audience).Error; err != nil {
				return err
			}
			notification := buildNotification(event, audience, user.Email)
			if err := tx.Create(&notification).Error; err != nil {
				return err
			}
			if err := createDeliveriesForNotification(tx, &user, notification, channels); err != nil {
				return err
			}
		}

		for _, email := range uniqueEmails(input.EmailRecipients) {
			audience := models.NotificationAudience{
				EventID:          event.ID,
				WorkspaceID:      input.WorkspaceID,
				RecipientEmail:   email,
				ResourceType:     input.ResourceType,
				ResourceID:       input.ResourceID,
				PermissionPolicy: policy,
				AudienceKey:      fmt.Sprintf("%d:email:%s", event.ID, email),
			}
			if err := tx.Create(&audience).Error; err != nil {
				return err
			}
			notification := buildNotification(event, audience, email)
			if err := tx.Create(&notification).Error; err != nil {
				return err
			}
			if err := createDeliveriesForNotification(tx, nil, notification, channels); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return EmitResult{}, err
	}

	result := EmitResult{Event: event}
	if event.ID == 0 {
		return result, nil
	}
	if err := db.Where("notification_id IN (?)", db.Model(&models.Notification{}).Select("id").Where("event_id = ?", event.ID)).Order("id ASC").Find(&result.Deliveries).Error; err != nil {
		return EmitResult{}, err
	}
	if err := db.Where("event_id = ?", event.ID).Order("id ASC").Find(&result.InboxMessages).Error; err != nil {
		return EmitResult{}, err
	}
	return result, nil
}

func buildNotification(event models.NotificationEvent, audience models.NotificationAudience, recipientEmail string) models.Notification {
	return models.Notification{
		EventID:         event.ID,
		AudienceID:      audience.ID,
		WorkspaceID:     event.WorkspaceID,
		RecipientUserID: audience.RecipientUserID,
		RecipientEmail:  recipientEmail,
		Family:          event.Family,
		EventType:       event.EventType,
		ResourceType:    event.ResourceType,
		ResourceID:      event.ResourceID,
		ActorUserID:     event.ActorUserID,
		ActorType:       event.ActorType,
		Title:           event.Title,
		Content:         event.Content,
		Priority:        event.Priority,
		Metadata:        event.Metadata,
		CanonicalKey:    fmt.Sprintf("%d:%d", event.ID, audience.ID),
	}
}

func createDeliveriesForNotification(tx *gorm.DB, user *models.User, notification models.Notification, channels []string) error {
	for _, channel := range channels {
		delivery := models.NotificationDelivery{
			NotificationID: notification.ID,
			Channel:        channel,
			Status:         models.NotificationDeliveryStatusSkipped,
		}
		enabled := true
		if user != nil {
			enabled = resolvePreferenceEnabled(tx, user.ID, notification.WorkspaceID, notification.ResourceType, notification.ResourceID, notification.EventType, channel)
		}
		if !enabled {
			delivery.Status = models.NotificationDeliveryStatusSuppressed
			delivery.ErrorMessage = "suppressed by notification preference"
			if err := tx.Create(&delivery).Error; err != nil {
				return err
			}
			continue
		}

		switch channel {
		case models.NotificationChannelInApp:
			if notification.RecipientUserID == nil {
				delivery.Status = models.NotificationDeliveryStatusSkipped
				delivery.ErrorMessage = "in-app delivery requires recipient user"
				if err := tx.Create(&delivery).Error; err != nil {
					return err
				}
				continue
			}
			delivery.Status = models.NotificationDeliveryStatusDelivered
			delivery.Destination = fmt.Sprintf("user:%d", derefUint64(notification.RecipientUserID))
			delivery.SentAt = time.Now().Unix()
			if err := tx.Create(&delivery).Error; err != nil {
				return err
			}
			inbox := models.InboxMessage{
				NotificationID:  notification.ID,
				EventID:         notification.EventID,
				AudienceID:      notification.AudienceID,
				WorkspaceID:     notification.WorkspaceID,
				RecipientUserID: notification.RecipientUserID,
				Family:          notification.Family,
				EventType:       notification.EventType,
				Title:           notification.Title,
				Content:         notification.Content,
				SenderID:        notification.ActorUserID,
				SenderType:      notification.ActorType,
				Priority:        notification.Priority,
				Metadata:        notification.Metadata,
				Channel:         models.NotificationChannelInApp,
				ResourceType:    notification.ResourceType,
				ResourceID:      notification.ResourceID,
			}
			if err := tx.Create(&inbox).Error; err != nil {
				return err
			}
		case models.NotificationChannelEmail:
			delivery.Destination = strings.TrimSpace(notification.RecipientEmail)
			if delivery.Destination == "" && user != nil {
				delivery.Destination = strings.TrimSpace(user.Email)
			}
			if delivery.Destination == "" {
				delivery.Status = models.NotificationDeliveryStatusSkipped
				delivery.ErrorMessage = "recipient email is empty"
			} else if !SMTPConfigured() {
				delivery.Status = models.NotificationDeliveryStatusNotConfigured
				delivery.ErrorMessage = "smtp delivery is not configured"
			} else {
				delivery.Status = models.NotificationDeliveryStatusPending
				delivery.Provider = "smtp"
			}
			if err := tx.Create(&delivery).Error; err != nil {
				return err
			}
		default:
			delivery.Status = models.NotificationDeliveryStatusSkipped
			delivery.ErrorMessage = "unsupported delivery channel"
			if err := tx.Create(&delivery).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func resolvePreferenceEnabled(tx *gorm.DB, userID, workspaceID uint64, resourceType string, resourceID uint64, eventType, channel string) bool {
	for _, key := range preferenceLookupKeys(userID, workspaceID, resourceType, resourceID, eventType, channel) {
		var pref models.NotificationPreference
		if err := tx.Where("rule_key = ?", key).First(&pref).Error; err == nil {
			return pref.Enabled
		}
	}
	return true
}

func preferenceLookupKeys(userID, workspaceID uint64, resourceType string, resourceID uint64, eventType, channel string) []string {
	workspaceKeys := []string{"*"}
	if workspaceID > 0 {
		workspaceKeys = append([]string{fmt.Sprintf("%d", workspaceID)}, workspaceKeys...)
	}
	resourceType = strings.TrimSpace(resourceType)
	resourceTypeKeys := []string{"*"}
	if resourceType != "" {
		resourceTypeKeys = append([]string{resourceType}, resourceTypeKeys...)
	}
	resourceIDKeys := []string{"*"}
	if resourceID > 0 {
		resourceIDKeys = append([]string{fmt.Sprintf("%d", resourceID)}, resourceIDKeys...)
	}
	eventTypeKeys := []string{strings.TrimSpace(eventType), "*"}
	keys := make([]string, 0, len(workspaceKeys)*len(resourceTypeKeys)*len(resourceIDKeys)*len(eventTypeKeys))
	for _, workspaceKey := range workspaceKeys {
		for _, resourceTypeKey := range resourceTypeKeys {
			for _, resourceIDKey := range resourceIDKeys {
				for _, eventTypeKey := range eventTypeKeys {
					keys = append(keys, strings.Join([]string{fmt.Sprintf("%d", userID), workspaceKey, resourceTypeKey, resourceIDKey, eventTypeKey, channel}, ":"))
				}
			}
		}
	}
	return keys
}

func normalizeChannels(channels []string) []string {
	if len(channels) == 0 {
		return []string{models.NotificationChannelInApp}
	}
	allowed := map[string]struct{}{
		models.NotificationChannelInApp: {},
		models.NotificationChannelEmail: {},
	}
	set := make(map[string]struct{})
	for _, channel := range channels {
		trimmed := strings.TrimSpace(channel)
		if _, ok := allowed[trimmed]; !ok {
			continue
		}
		set[trimmed] = struct{}{}
	}
	if len(set) == 0 {
		return []string{models.NotificationChannelInApp}
	}
	result := make([]string, 0, len(set))
	for channel := range set {
		result = append(result, channel)
	}
	sort.Strings(result)
	return result
}

func normalizePermissionPolicy(policy string) string {
	switch strings.TrimSpace(policy) {
	case PermissionPolicyWorkspaceInvite:
		return PermissionPolicyWorkspaceInvite
	case PermissionPolicyPlatformAdmin:
		return PermissionPolicyPlatformAdmin
	case PermissionPolicyOpen:
		return PermissionPolicyOpen
	default:
		return PermissionPolicyWorkspaceMember
	}
}

func recipientPermitted(tx *gorm.DB, user models.User, workspaceID uint64, policy string) bool {
	switch policy {
	case PermissionPolicyWorkspaceInvite, PermissionPolicyOpen:
		return true
	case PermissionPolicyPlatformAdmin:
		return strings.EqualFold(user.Role, "admin")
	default:
		if strings.EqualFold(user.Role, "admin") {
			return true
		}
		if workspaceID == 0 {
			return false
		}
		var count int64
		tx.Model(&models.WorkspaceMember{}).Where("workspace_id = ? AND user_id = ? AND status = ?", workspaceID, user.ID, models.WorkspaceMemberStatusActive).Count(&count)
		return count > 0
	}
}

func loadRecipientUsers(tx *gorm.DB, ids []uint64) ([]models.User, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var users []models.User
	if err := tx.Where("id IN ?", ids).Order("id ASC").Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

func uniqueUint64s(values []uint64) []uint64 {
	set := make(map[uint64]struct{})
	result := make([]uint64, 0, len(values))
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, ok := set[value]; ok {
			continue
		}
		set[value] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func uniqueEmails(values []string) []string {
	set := make(map[string]struct{})
	result := make([]string, 0, len(values))
	for _, value := range values {
		email := strings.ToLower(strings.TrimSpace(value))
		if email == "" {
			continue
		}
		if _, ok := set[email]; ok {
			continue
		}
		set[email] = struct{}{}
		result = append(result, email)
	}
	sort.Strings(result)
	return result
}

func derefUint64(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
}
