package services

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"

	"easydo-server/internal/models"
	"easydo-server/internal/smtpclient"

	"gorm.io/gorm"
)

func openNotificationSenderServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := openActorTestDB(t)
	if err := db.AutoMigrate(&models.MasterKey{}, &models.NotificationSenderConfig{}, &models.AuditLog{}); err != nil {
		t.Fatalf("auto migrate notification sender models failed: %v", err)
	}
	if _, err := models.LoadOrCreateMasterKey(db); err != nil {
		t.Fatalf("initialize master key failed: %v", err)
	}
	return db
}

func TestNotificationSenderServiceSavesEncryptedPlatformConfig(t *testing.T) {
	db := openNotificationSenderServiceTestDB(t)
	usecase := &NotificationSenderService{DB: db}
	admin := seedUserManagementUser(t, db, "sender-admin", "admin")
	adminWorkspace := seedUserManagementWorkspace(t, db, "sender-admin-space", models.WorkspaceKindAdmin, admin.ID)
	seedUserManagementMember(t, db, adminWorkspace.ID, admin.ID, models.WorkspaceRoleOwner)

	saved, err := usecase.SaveConfig(context.Background(), SaveNotificationSenderConfigRequest{
		Actor:        ActorContext{UserID: admin.ID, Username: admin.Username, SystemRole: admin.Role},
		WorkspaceID:  adminWorkspace.ID,
		Scope:        models.NotificationSenderScopePlatform,
		Enabled:      true,
		FromName:     "EasyDo",
		FromAddress:  "noreply@example.com",
		SMTPHost:     "smtp.example.com",
		SMTPPort:     587,
		SMTPUsername: "noreply@example.com",
		SMTPPassword: "smtp-secret",
		SMTPTLSMode:  "starttls",
	})
	if err != nil {
		t.Fatalf("SaveConfig returned error: %v", err)
	}
	if !saved.SMTPPasswordConfigured || saved.SMTPPassword != "" {
		t.Fatalf("saved config should report configured password without plaintext: %+v", saved)
	}

	var stored models.NotificationSenderConfig
	if err := db.Where("scope = ? AND workspace_id = 0", models.NotificationSenderScopePlatform).First(&stored).Error; err != nil {
		t.Fatalf("load stored sender config failed: %v", err)
	}
	if stored.SMTPPasswordEncrypted == "" || strings.Contains(stored.SMTPPasswordEncrypted, "smtp-secret") {
		t.Fatalf("stored encrypted password is invalid: %q", stored.SMTPPasswordEncrypted)
	}
	var auditCount int64
	if err := db.Model(&models.AuditLog{}).Where("action = ?", "notification_sender.save").Count(&auditCount).Error; err != nil {
		t.Fatalf("count audit logs failed: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("audit count=%d, want 1", auditCount)
	}
}

func TestNotificationSenderServiceEffectiveConfigPrefersWorkspaceThenPlatform(t *testing.T) {
	db := openNotificationSenderServiceTestDB(t)
	usecase := &NotificationSenderService{DB: db}
	admin := seedUserManagementUser(t, db, "sender-effective-admin", "admin")
	owner := seedUserManagementUser(t, db, "sender-effective-owner", "user")
	adminWorkspace := seedUserManagementWorkspace(t, db, "sender-effective-admin-space", models.WorkspaceKindAdmin, admin.ID)
	workspace := seedUserManagementWorkspace(t, db, "sender-effective-workspace", models.WorkspaceKindNormal, owner.ID)
	seedUserManagementMember(t, db, adminWorkspace.ID, admin.ID, models.WorkspaceRoleOwner)
	seedUserManagementMember(t, db, workspace.ID, owner.ID, models.WorkspaceRoleOwner)

	if _, err := usecase.SaveConfig(context.Background(), SaveNotificationSenderConfigRequest{
		Actor:        ActorContext{UserID: admin.ID, Username: admin.Username, SystemRole: admin.Role},
		WorkspaceID:  adminWorkspace.ID,
		Scope:        models.NotificationSenderScopePlatform,
		Enabled:      true,
		FromAddress:  "platform@example.com",
		SMTPHost:     "smtp.platform.example.com",
		SMTPPort:     25,
		SMTPPassword: "platform-secret",
	}); err != nil {
		t.Fatalf("save platform config failed: %v", err)
	}
	if _, err := usecase.SaveConfig(context.Background(), SaveNotificationSenderConfigRequest{
		Actor:             ActorContext{UserID: owner.ID, Username: owner.Username, SystemRole: owner.Role},
		WorkspaceID:       workspace.ID,
		Scope:             models.NotificationSenderScopeWorkspace,
		TargetWorkspaceID: workspace.ID,
		Enabled:           true,
		FromAddress:       "workspace@example.com",
		SMTPHost:          "smtp.workspace.example.com",
		SMTPPort:          587,
		SMTPPassword:      "workspace-secret",
	}); err != nil {
		t.Fatalf("save workspace config failed: %v", err)
	}

	effective, err := usecase.GetEffectiveConfig(context.Background(), workspace.ID)
	if err != nil {
		t.Fatalf("GetEffectiveConfig returned error: %v", err)
	}
	if effective.Scope != models.NotificationSenderScopeWorkspace || effective.FromAddress != "workspace@example.com" || effective.SMTPPassword != "workspace-secret" {
		t.Fatalf("effective workspace config=%+v", effective)
	}

	if err := db.Model(&models.NotificationSenderConfig{}).Where("scope = ? AND workspace_id = ?", models.NotificationSenderScopeWorkspace, workspace.ID).Update("enabled", false).Error; err != nil {
		t.Fatalf("disable workspace config failed: %v", err)
	}
	effective, err = usecase.GetEffectiveConfig(context.Background(), workspace.ID)
	if err != nil {
		t.Fatalf("GetEffectiveConfig fallback returned error: %v", err)
	}
	if effective.Scope != models.NotificationSenderScopePlatform || effective.FromAddress != "platform@example.com" || effective.SMTPPassword != "platform-secret" {
		t.Fatalf("effective fallback config=%+v", effective)
	}
}

func TestNotificationSenderServiceFallsBackToSMTPLoginAuth(t *testing.T) {
	db := openNotificationSenderServiceTestDB(t)
	usecase := &NotificationSenderService{DB: db}
	admin := seedUserManagementUser(t, db, "sender-login-auth-admin", "admin")
	adminWorkspace := seedUserManagementWorkspace(t, db, "sender-login-auth-admin-space", models.WorkspaceKindAdmin, admin.ID)
	seedUserManagementMember(t, db, adminWorkspace.ID, admin.ID, models.WorkspaceRoleOwner)
	server := startLoginOnlySMTPServer(t, "platform@example.com", "platform-secret")
	host, rawPort, err := net.SplitHostPort(server.addr)
	if err != nil {
		t.Fatalf("split smtp server addr failed: %v", err)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		t.Fatalf("parse smtp server port failed: %v", err)
	}

	if _, err := usecase.SaveConfig(context.Background(), SaveNotificationSenderConfigRequest{
		Actor:        ActorContext{UserID: admin.ID, Username: admin.Username, SystemRole: admin.Role},
		WorkspaceID:  adminWorkspace.ID,
		Scope:        models.NotificationSenderScopePlatform,
		Enabled:      true,
		FromName:     "EasyDo Platform",
		FromAddress:  "platform@example.com",
		SMTPHost:     host,
		SMTPPort:     port,
		SMTPUsername: "platform@example.com",
		SMTPPassword: "platform-secret",
		SMTPTLSMode:  "plain",
	}); err != nil {
		t.Fatalf("save platform config failed: %v", err)
	}

	result, err := usecase.TestEffectiveConfig(context.Background(), TestNotificationSenderConfigRequest{
		Actor:       ActorContext{UserID: admin.ID, Username: admin.Username, SystemRole: admin.Role},
		WorkspaceID: adminWorkspace.ID,
		ToAddress:   "target@example.com",
	})
	if err != nil {
		t.Fatalf("TestEffectiveConfig returned error: %v", err)
	}
	if result.Status != "success" {
		t.Fatalf("expected LOGIN auth fallback to succeed, got %+v", result)
	}
	if !server.sawLoginAuth {
		t.Fatalf("expected LOGIN auth to be used, sawPlain=%v sawLogin=%v", server.sawPlainAuth, server.sawLoginAuth)
	}
}

func TestNotificationSenderServiceTestEffectiveConfigSendsEmailAndRecordsResult(t *testing.T) {
	db := openNotificationSenderServiceTestDB(t)
	usecase := &NotificationSenderService{DB: db}
	admin := seedUserManagementUser(t, db, "sender-test-admin", "admin")
	adminWorkspace := seedUserManagementWorkspace(t, db, "sender-test-admin-space", models.WorkspaceKindAdmin, admin.ID)
	seedUserManagementMember(t, db, adminWorkspace.ID, admin.ID, models.WorkspaceRoleOwner)

	saved, err := usecase.SaveConfig(context.Background(), SaveNotificationSenderConfigRequest{
		Actor:        ActorContext{UserID: admin.ID, Username: admin.Username, SystemRole: admin.Role},
		WorkspaceID:  adminWorkspace.ID,
		Scope:        models.NotificationSenderScopePlatform,
		Enabled:      true,
		FromName:     "EasyDo Platform",
		FromAddress:  "platform@example.com",
		SMTPHost:     "smtp.platform.example.com",
		SMTPPort:     587,
		SMTPUsername: "platform@example.com",
		SMTPPassword: "platform-secret",
	})
	if err != nil {
		t.Fatalf("save platform config failed: %v", err)
	}

	original := notificationSenderSendMail
	t.Cleanup(func() { notificationSenderSendMail = original })
	notificationSenderSendMail = func(req smtpclient.SendRequest) error {
		if req.Host != "smtp.platform.example.com" || req.Port != 587 {
			return fmt.Errorf("unexpected smtp target: %s:%d", req.Host, req.Port)
		}
		if req.From != "platform@example.com" {
			return fmt.Errorf("unexpected from address: %s", req.From)
		}
		if len(req.To) != 1 || req.To[0] != "target@example.com" {
			return fmt.Errorf("unexpected recipients: %v", req.To)
		}
		if !strings.Contains(string(req.Message), "Subject: EasyDo 邮件发送配置测试") {
			return fmt.Errorf("missing test subject: %s", string(req.Message))
		}
		return nil
	}

	result, err := usecase.TestEffectiveConfig(context.Background(), TestNotificationSenderConfigRequest{
		Actor:       ActorContext{UserID: admin.ID, Username: admin.Username, SystemRole: admin.Role},
		WorkspaceID: adminWorkspace.ID,
		ToAddress:   "target@example.com",
	})
	if err != nil {
		t.Fatalf("TestEffectiveConfig returned error: %v", err)
	}
	if result.Status != "success" || result.TestedAt == 0 || result.Error != "" {
		t.Fatalf("unexpected test result: %+v", result)
	}

	var stored models.NotificationSenderConfig
	if err := db.First(&stored, saved.ID).Error; err != nil {
		t.Fatalf("reload sender config failed: %v", err)
	}
	if stored.LastTestStatus != "success" || stored.LastTestedAt == 0 || stored.LastTestError != "" {
		t.Fatalf("unexpected stored test metadata: %+v", stored)
	}
}

type loginOnlySMTPServer struct {
	addr         string
	sawPlainAuth bool
	sawLoginAuth bool
}

func startLoginOnlySMTPServer(t *testing.T, username, password string) *loginOnlySMTPServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen smtp test server failed: %v", err)
	}
	server := &loginOnlySMTPServer{addr: listener.Addr().String()}
	done := make(chan struct{})
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
	})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		writer := bufio.NewWriter(conn)
		writeSMTPLine(t, writer, "220 localhost ESMTP")
		authenticated := false
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			command := strings.TrimRight(line, "\r\n")
			upper := strings.ToUpper(command)
			switch {
			case strings.HasPrefix(upper, "EHLO"):
				writeSMTPLine(t, writer, "250-localhost")
				writeSMTPLine(t, writer, "250 AUTH LOGIN")
			case strings.HasPrefix(upper, "AUTH PLAIN"):
				server.sawPlainAuth = true
				writeSMTPLine(t, writer, "504 5.7.4 Unrecognized authentication type")
			case strings.HasPrefix(upper, "AUTH LOGIN"):
				server.sawLoginAuth = true
				writeSMTPLine(t, writer, "334 VXNlcm5hbWU6")
				rawUser, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				writeSMTPLine(t, writer, "334 UGFzc3dvcmQ6")
				rawPassword, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				gotUser := decodeSMTPBase64(t, rawUser)
				gotPassword := decodeSMTPBase64(t, rawPassword)
				if gotUser != username || gotPassword != password {
					writeSMTPLine(t, writer, "535 5.7.8 Authentication credentials invalid")
					return
				}
				authenticated = true
				writeSMTPLine(t, writer, "235 2.7.0 Authentication successful")
			case strings.HasPrefix(upper, "MAIL FROM"):
				if !authenticated {
					writeSMTPLine(t, writer, "530 5.7.0 Authentication required")
					continue
				}
				writeSMTPLine(t, writer, "250 2.1.0 OK")
			case strings.HasPrefix(upper, "RCPT TO"):
				writeSMTPLine(t, writer, "250 2.1.5 OK")
			case upper == "DATA":
				writeSMTPLine(t, writer, "354 End data with <CR><LF>.<CR><LF>")
				for {
					dataLine, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					if strings.TrimRight(dataLine, "\r\n") == "." {
						break
					}
				}
				writeSMTPLine(t, writer, "250 2.0.0 OK")
			case upper == "QUIT":
				writeSMTPLine(t, writer, "221 2.0.0 Bye")
				return
			default:
				writeSMTPLine(t, writer, "250 OK")
			}
		}
	}()
	return server
}

func writeSMTPLine(t *testing.T, writer *bufio.Writer, line string) {
	t.Helper()
	if _, err := writer.WriteString(line + "\r\n"); err != nil {
		t.Fatalf("write smtp line failed: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush smtp line failed: %v", err)
	}
}

func decodeSMTPBase64(t *testing.T, raw string) string {
	t.Helper()
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		t.Fatalf("decode smtp auth value failed: %v", err)
	}
	return string(decoded)
}
