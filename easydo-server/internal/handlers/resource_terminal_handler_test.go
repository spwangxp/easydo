package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"easydo-server/internal/config"
	"easydo-server/internal/middleware"
	"easydo-server/internal/models"
	"easydo-server/pkg/utils"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func TestTerminalSessionHandler_CreateRejectsNonVMResource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupTerminalSessionTestRedis(t)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "terminal-k8s-maintainer", models.WorkspaceRoleMaintainer)
	seedApprovedResourceAgent(t, db, workspace.ID)
	credential := seedResourceVerificationCredential(t, db, workspace.ID, maintainer.ID, models.TypeToken, map[string]interface{}{
		"token": "k8s-token",
	})
	resource := models.Resource{
		WorkspaceID: workspace.ID,
		Name:        "cluster-for-terminal",
		Type:        models.ResourceTypeK8sCluster,
		Environment: "production",
		Status:      models.ResourceStatusOnline,
		Endpoint:    "https://cluster.example.internal",
		CreatedBy:   maintainer.ID,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}
	if err := db.Create(&models.ResourceCredentialBinding{
		WorkspaceID:  workspace.ID,
		ResourceID:   resource.ID,
		CredentialID: credential.ID,
		Purpose:      "cluster_auth",
		BoundBy:      maintainer.ID,
	}).Error; err != nil {
		t.Fatalf("create binding failed: %v", err)
	}

	router := newTerminalSessionTestRouter(t, db, NewWebSocketHandler())
	token := issueTerminalSessionTestToken(t, &maintainer)
	resp := performTerminalSessionAPIRequest(router, http.MethodPost, fmt.Sprintf("/api/resources/%d/terminal-sessions", resource.ID), workspace.ID, token, nil)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected non-VM terminal create to fail, got=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestTerminalSessionHandler_CreateRejectsMissingSSHBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupTerminalSessionTestRedis(t)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "terminal-no-ssh-maintainer", models.WorkspaceRoleMaintainer)
	seedApprovedResourceAgent(t, db, workspace.ID)
	resource := models.Resource{
		WorkspaceID: workspace.ID,
		Name:        "vm-without-ssh-binding",
		Type:        models.ResourceTypeVM,
		Environment: "production",
		Status:      models.ResourceStatusOnline,
		Endpoint:    "10.0.0.91:22",
		CreatedBy:   maintainer.ID,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	router := newTerminalSessionTestRouter(t, db, NewWebSocketHandler())
	token := issueTerminalSessionTestToken(t, &maintainer)
	resp := performTerminalSessionAPIRequest(router, http.MethodPost, fmt.Sprintf("/api/resources/%d/terminal-sessions", resource.ID), workspace.ID, token, nil)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected terminal create without ssh binding to fail, got=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestTerminalSessionHandler_CreateListGetCloseAndWorkspaceIsolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupTerminalSessionTestRedis(t)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainerA, workspaceA := seedResourceStoreUserAndWorkspace(t, db, "terminal-owner-a", models.WorkspaceRoleMaintainer)
	maintainerB, workspaceB := seedResourceStoreUserAndWorkspace(t, db, "terminal-owner-b", models.WorkspaceRoleMaintainer)
	seedApprovedResourceAgent(t, db, workspaceA.ID)
	credential := seedResourceVerificationCredential(t, db, workspaceA.ID, maintainerA.ID, models.TypePassword, map[string]interface{}{
		"username": "root",
		"password": "secret123",
	})
	resource := models.Resource{
		WorkspaceID: workspaceA.ID,
		Name:        "terminal-vm-a",
		Type:        models.ResourceTypeVM,
		Environment: "production",
		Status:      models.ResourceStatusOnline,
		Endpoint:    "10.0.0.41:22",
		CreatedBy:   maintainerA.ID,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}
	if err := db.Create(&models.ResourceCredentialBinding{
		WorkspaceID:  workspaceA.ID,
		ResourceID:   resource.ID,
		CredentialID: credential.ID,
		Purpose:      "ssh_auth",
		BoundBy:      maintainerA.ID,
	}).Error; err != nil {
		t.Fatalf("create binding failed: %v", err)
	}

	router := newTerminalSessionTestRouter(t, db, NewWebSocketHandler())
	ownerToken := issueTerminalSessionTestToken(t, &maintainerA)
	otherToken := issueTerminalSessionTestToken(t, &maintainerB)

	createResp := performTerminalSessionAPIRequest(router, http.MethodPost, fmt.Sprintf("/api/resources/%d/terminal-sessions", resource.ID), workspaceA.ID, ownerToken, nil)
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected terminal create success, got=%d body=%s", createResp.Code, createResp.Body.String())
	}
	created := decodeResponseData[map[string]interface{}](t, createResp.Body.Bytes())
	sessionID, _ := created["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("expected session_id in create response, body=%s", createResp.Body.String())
	}

	listResp := performTerminalSessionAPIRequest(router, http.MethodGet, fmt.Sprintf("/api/resources/%d/terminal-sessions", resource.ID), workspaceA.ID, ownerToken, nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected terminal list success, got=%d body=%s", listResp.Code, listResp.Body.String())
	}
	if !bytes.Contains(listResp.Body.Bytes(), []byte(sessionID)) {
		t.Fatalf("expected session_id in list response, got=%s", listResp.Body.String())
	}

	getResp := performTerminalSessionAPIRequest(router, http.MethodGet, fmt.Sprintf("/api/resources/%d/terminal-sessions/%s", resource.ID, sessionID), workspaceA.ID, ownerToken, nil)
	if getResp.Code != http.StatusOK {
		t.Fatalf("expected terminal get success, got=%d body=%s", getResp.Code, getResp.Body.String())
	}
	if !bytes.Contains(getResp.Body.Bytes(), []byte(`"status":"active"`)) {
		t.Fatalf("expected active terminal status, got=%s", getResp.Body.String())
	}

	crossWorkspaceResp := performTerminalSessionAPIRequest(router, http.MethodGet, fmt.Sprintf("/api/resources/%d/terminal-sessions/%s", resource.ID, sessionID), workspaceB.ID, otherToken, nil)
	if crossWorkspaceResp.Code != http.StatusNotFound {
		t.Fatalf("expected cross-workspace access to be isolated, got=%d body=%s", crossWorkspaceResp.Code, crossWorkspaceResp.Body.String())
	}

	closeResp := performTerminalSessionAPIRequest(router, http.MethodPost, fmt.Sprintf("/api/resources/%d/terminal-sessions/%s/close", resource.ID, sessionID), workspaceA.ID, ownerToken, mustJSON(t, map[string]interface{}{"reason": "user_closed"}))
	if closeResp.Code != http.StatusOK {
		t.Fatalf("expected terminal close success, got=%d body=%s", closeResp.Code, closeResp.Body.String())
	}

	var stored models.ResourceTerminalSession
	if err := db.Where("session_id = ?", sessionID).First(&stored).Error; err != nil {
		t.Fatalf("load terminal session failed: %v", err)
	}
	if stored.Status != models.ResourceTerminalSessionStatusClosed {
		t.Fatalf("expected stored terminal session closed, got=%s", stored.Status)
	}
	if stored.ClosedAt == 0 {
		t.Fatalf("expected closed_at to be recorded")
	}
	if stored.ClosedBy == nil || *stored.ClosedBy == 0 {
		t.Fatalf("expected closed_by to be recorded")
	}
}

func TestTerminalSessionHandler_DeveloperCanOperateLockedCredentialTerminalSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupTerminalSessionTestRedis(t)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "terminal-locked-maintainer", models.WorkspaceRoleMaintainer)
	developer := seedResourceStoreMember(t, db, workspace.ID, "terminal-locked-developer", models.WorkspaceRoleDeveloper)
	seedApprovedResourceAgent(t, db, workspace.ID)
	credential := seedResourceVerificationCredential(t, db, workspace.ID, maintainer.ID, models.TypePassword, map[string]interface{}{
		"username": "root",
		"password": "secret123",
	})
	if err := db.Model(&models.Credential{}).Where("id = ?", credential.ID).Update("lock_state", models.CredentialLockStateLocked).Error; err != nil {
		t.Fatalf("lock credential failed: %v", err)
	}
	resource := models.Resource{
		WorkspaceID: workspace.ID,
		Name:        "terminal-locked-vm",
		Type:        models.ResourceTypeVM,
		Environment: "production",
		Status:      models.ResourceStatusOnline,
		Endpoint:    "10.0.0.71:22",
		CreatedBy:   maintainer.ID,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}
	if err := db.Create(&models.ResourceCredentialBinding{
		WorkspaceID:  workspace.ID,
		ResourceID:   resource.ID,
		CredentialID: credential.ID,
		Purpose:      "ssh_auth",
		BoundBy:      maintainer.ID,
	}).Error; err != nil {
		t.Fatalf("create binding failed: %v", err)
	}

	router := newTerminalSessionTestRouter(t, db, NewWebSocketHandler())
	developerToken := issueTerminalSessionTestToken(t, &developer)

	createResp := performTerminalSessionAPIRequest(router, http.MethodPost, fmt.Sprintf("/api/resources/%d/terminal-sessions", resource.ID), workspace.ID, developerToken, nil)
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected developer terminal create success with locked credential, got=%d body=%s", createResp.Code, createResp.Body.String())
	}
	created := decodeResponseData[map[string]interface{}](t, createResp.Body.Bytes())
	sessionID, _ := created["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("expected session_id in create response, body=%s", createResp.Body.String())
	}

	listResp := performTerminalSessionAPIRequest(router, http.MethodGet, fmt.Sprintf("/api/resources/%d/terminal-sessions", resource.ID), workspace.ID, developerToken, nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected developer terminal list success, got=%d body=%s", listResp.Code, listResp.Body.String())
	}
	getResp := performTerminalSessionAPIRequest(router, http.MethodGet, fmt.Sprintf("/api/resources/%d/terminal-sessions/%s", resource.ID, sessionID), workspace.ID, developerToken, nil)
	if getResp.Code != http.StatusOK {
		t.Fatalf("expected developer terminal get success, got=%d body=%s", getResp.Code, getResp.Body.String())
	}
	closeResp := performTerminalSessionAPIRequest(router, http.MethodPost, fmt.Sprintf("/api/resources/%d/terminal-sessions/%s/close", resource.ID, sessionID), workspace.ID, developerToken, mustJSON(t, map[string]interface{}{"reason": "user_closed"}))
	if closeResp.Code != http.StatusOK {
		t.Fatalf("expected developer terminal close success, got=%d body=%s", closeResp.Code, closeResp.Body.String())
	}
}

func TestTerminalFrontendConnection_RequiresAuthAndRelaysSessionMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupTerminalSessionTestRedis(t)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "terminal-ws-maintainer", models.WorkspaceRoleMaintainer)
	agent := seedApprovedResourceAgent(t, db, workspace.ID)
	credential := seedResourceVerificationCredential(t, db, workspace.ID, maintainer.ID, models.TypePassword, map[string]interface{}{
		"username": "root",
		"password": "secret123",
	})
	resource := models.Resource{
		WorkspaceID: workspace.ID,
		Name:        "terminal-ws-vm",
		Type:        models.ResourceTypeVM,
		Environment: "production",
		Status:      models.ResourceStatusOnline,
		Endpoint:    "10.0.0.51:22",
		CreatedBy:   maintainer.ID,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}
	if err := db.Create(&models.ResourceCredentialBinding{
		WorkspaceID:  workspace.ID,
		ResourceID:   resource.ID,
		CredentialID: credential.ID,
		Purpose:      "ssh_auth",
		BoundBy:      maintainer.ID,
	}).Error; err != nil {
		t.Fatalf("create binding failed: %v", err)
	}

	wsHandler := NewWebSocketHandler()
	server := newTerminalSessionTestServer(t, db, wsHandler)
	defer server.Close()

	agentConn, _, err := websocket.DefaultDialer.Dial(wsTerminalAgentURL(server.URL, fmt.Sprintf("?agent_id=%d&token=%s", agent.ID, agent.Token)), nil)
	if err != nil {
		t.Fatalf("dial agent websocket failed: %v", err)
	}
	defer agentConn.Close()

	ownerToken := issueTerminalSessionTestToken(t, &maintainer)
	createResp := performTerminalSessionAPIRequest(server.Config.Handler, http.MethodPost, fmt.Sprintf("/api/resources/%d/terminal-sessions", resource.ID), workspace.ID, ownerToken, nil)
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected terminal create success, got=%d body=%s", createResp.Code, createResp.Body.String())
	}
	created := decodeResponseData[map[string]interface{}](t, createResp.Body.Bytes())
	sessionID, _ := created["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("expected session_id in create response, body=%s", createResp.Body.String())
	}

	missingTokenConn, missingTokenResp, missingTokenErr := websocket.DefaultDialer.Dial(wsTerminalFrontendURL(server.URL, fmt.Sprintf("?session_id=%s", sessionID)), nil)
	if missingTokenConn != nil {
		_ = missingTokenConn.Close()
	}
	if missingTokenErr == nil {
		t.Fatal("expected terminal frontend websocket without token to fail")
	}
	if missingTokenResp == nil || missingTokenResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected missing token status 401, got=%v", missingTokenResp)
	}

	frontendConn, _, err := websocket.DefaultDialer.Dial(wsTerminalFrontendURL(server.URL, fmt.Sprintf("?session_id=%s&token=%s", sessionID, ownerToken)), nil)
	if err != nil {
		t.Fatalf("dial terminal frontend websocket failed: %v", err)
	}
	defer frontendConn.Close()

	if err := agentConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set agent read deadline failed: %v", err)
	}
	_, agentOpenRaw, err := agentConn.ReadMessage()
	if err != nil {
		t.Fatalf("read terminal open message failed: %v", err)
	}
	var agentOpen WebSocketMessage
	if err := json.Unmarshal(agentOpenRaw, &agentOpen); err != nil {
		t.Fatalf("unmarshal terminal open message failed: %v", err)
	}
	if agentOpen.Type != "terminal_session_open" {
		t.Fatalf("expected terminal_session_open, got=%s payload=%s", agentOpen.Type, string(agentOpenRaw))
	}
	if got, _ := agentOpen.Payload["session_id"].(string); got != sessionID {
		t.Fatalf("expected terminal open session_id=%s, got=%v", sessionID, agentOpen.Payload["session_id"])
	}

	frontendInput := WebSocketMessage{Type: "terminal_input", Payload: map[string]interface{}{"session_id": sessionID, "data": "ls -la\n"}}
	frontendInputRaw, _ := json.Marshal(frontendInput)
	if err := frontendConn.WriteMessage(websocket.TextMessage, frontendInputRaw); err != nil {
		t.Fatalf("write terminal input failed: %v", err)
	}

	if err := agentConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set agent read deadline failed: %v", err)
	}
	_, agentInputRaw, err := agentConn.ReadMessage()
	if err != nil {
		t.Fatalf("read terminal input relay failed: %v", err)
	}
	var agentInput WebSocketMessage
	if err := json.Unmarshal(agentInputRaw, &agentInput); err != nil {
		t.Fatalf("unmarshal terminal input relay failed: %v", err)
	}
	if agentInput.Type != "terminal_session_input" {
		t.Fatalf("expected terminal_session_input, got=%s payload=%s", agentInput.Type, string(agentInputRaw))
	}
	if got, _ := agentInput.Payload["session_id"].(string); got != sessionID {
		t.Fatalf("expected terminal input session_id=%s, got=%v", sessionID, agentInput.Payload["session_id"])
	}

	frontendRootSwitch := WebSocketMessage{Type: "terminal_root_switch", Payload: map[string]interface{}{"session_id": sessionID}}
	frontendRootSwitchRaw, _ := json.Marshal(frontendRootSwitch)
	if err := frontendConn.WriteMessage(websocket.TextMessage, frontendRootSwitchRaw); err != nil {
		t.Fatalf("write terminal root switch failed: %v", err)
	}

	if err := agentConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set agent read deadline failed: %v", err)
	}
	_, agentRootSwitchRaw, err := agentConn.ReadMessage()
	if err != nil {
		t.Fatalf("read terminal root switch relay failed: %v", err)
	}
	var agentRootSwitch WebSocketMessage
	if err := json.Unmarshal(agentRootSwitchRaw, &agentRootSwitch); err != nil {
		t.Fatalf("unmarshal terminal root switch relay failed: %v", err)
	}
	if agentRootSwitch.Type != "terminal_session_root_switch" {
		t.Fatalf("expected terminal_session_root_switch, got=%s payload=%s", agentRootSwitch.Type, string(agentRootSwitchRaw))
	}
	if got, _ := agentRootSwitch.Payload["session_id"].(string); got != sessionID {
		t.Fatalf("expected terminal root switch session_id=%s, got=%v", sessionID, agentRootSwitch.Payload["session_id"])
	}

	agentOutput := WebSocketMessage{Type: "terminal_session_output", Payload: map[string]interface{}{"session_id": sessionID, "data": "total 0\n", "stream": "stdout"}}
	agentOutputRaw, _ := json.Marshal(agentOutput)
	if err := agentConn.WriteMessage(websocket.TextMessage, agentOutputRaw); err != nil {
		t.Fatalf("write terminal output failed: %v", err)
	}

	if err := frontendConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set frontend read deadline failed: %v", err)
	}
	_, frontendOutputRaw, err := frontendConn.ReadMessage()
	if err != nil {
		t.Fatalf("read terminal output relay failed: %v", err)
	}
	var frontendOutput WebSocketMessage
	if err := json.Unmarshal(frontendOutputRaw, &frontendOutput); err != nil {
		t.Fatalf("unmarshal terminal output relay failed: %v", err)
	}
	if frontendOutput.Type != "terminal_output" {
		t.Fatalf("expected terminal_output, got=%s payload=%s", frontendOutput.Type, string(frontendOutputRaw))
	}
	if got, _ := frontendOutput.Payload["session_id"].(string); got != sessionID {
		t.Fatalf("expected terminal output session_id=%s, got=%v", sessionID, frontendOutput.Payload["session_id"])
	}
}

func TestTerminalFrontendConnection_DeveloperCanAttachToLockedCredentialSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupTerminalSessionTestRedis(t)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "terminal-dev-ws-maintainer", models.WorkspaceRoleMaintainer)
	developer := seedResourceStoreMember(t, db, workspace.ID, "terminal-dev-ws-developer", models.WorkspaceRoleDeveloper)
	agent := seedApprovedResourceAgent(t, db, workspace.ID)
	credential := seedResourceVerificationCredential(t, db, workspace.ID, maintainer.ID, models.TypePassword, map[string]interface{}{
		"username": "root",
		"password": "secret123",
	})
	if err := db.Model(&models.Credential{}).Where("id = ?", credential.ID).Update("lock_state", models.CredentialLockStateLocked).Error; err != nil {
		t.Fatalf("lock credential failed: %v", err)
	}
	resource := models.Resource{
		WorkspaceID: workspace.ID,
		Name:        "terminal-dev-ws-vm",
		Type:        models.ResourceTypeVM,
		Environment: "production",
		Status:      models.ResourceStatusOnline,
		Endpoint:    "10.0.0.81:22",
		CreatedBy:   maintainer.ID,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}
	if err := db.Create(&models.ResourceCredentialBinding{
		WorkspaceID:  workspace.ID,
		ResourceID:   resource.ID,
		CredentialID: credential.ID,
		Purpose:      "ssh_auth",
		BoundBy:      maintainer.ID,
	}).Error; err != nil {
		t.Fatalf("create binding failed: %v", err)
	}

	wsHandler := NewWebSocketHandler()
	server := newTerminalSessionTestServer(t, db, wsHandler)
	defer server.Close()

	agentConn, _, err := websocket.DefaultDialer.Dial(wsTerminalAgentURL(server.URL, fmt.Sprintf("?agent_id=%d&token=%s", agent.ID, agent.Token)), nil)
	if err != nil {
		t.Fatalf("dial agent websocket failed: %v", err)
	}
	defer agentConn.Close()

	developerToken := issueTerminalSessionTestToken(t, &developer)
	createResp := performTerminalSessionAPIRequest(server.Config.Handler, http.MethodPost, fmt.Sprintf("/api/resources/%d/terminal-sessions", resource.ID), workspace.ID, developerToken, nil)
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected developer terminal create success, got=%d body=%s", createResp.Code, createResp.Body.String())
	}
	created := decodeResponseData[map[string]interface{}](t, createResp.Body.Bytes())
	sessionID, _ := created["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("expected session_id in create response, body=%s", createResp.Body.String())
	}

	frontendConn, _, err := websocket.DefaultDialer.Dial(wsTerminalFrontendURL(server.URL, fmt.Sprintf("?session_id=%s&token=%s", sessionID, developerToken)), nil)
	if err != nil {
		t.Fatalf("dial developer terminal frontend websocket failed: %v", err)
	}
	defer frontendConn.Close()

	if err := agentConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set agent read deadline failed: %v", err)
	}
	_, agentOpenRaw, err := agentConn.ReadMessage()
	if err != nil {
		t.Fatalf("read terminal open message failed: %v", err)
	}
	var agentOpen WebSocketMessage
	if err := json.Unmarshal(agentOpenRaw, &agentOpen); err != nil {
		t.Fatalf("unmarshal terminal open message failed: %v", err)
	}
	if agentOpen.Type != "terminal_session_open" {
		t.Fatalf("expected terminal_session_open, got=%s payload=%s", agentOpen.Type, string(agentOpenRaw))
	}
	if got, _ := agentOpen.Payload["session_id"].(string); got != sessionID {
		t.Fatalf("expected terminal open session_id=%s, got=%v", sessionID, agentOpen.Payload["session_id"])
	}
}

func TestTerminalFrontendConnection_OwnerServerRoutingAcrossReplicas(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupTerminalSessionTestRedis(t)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "terminal-replica-maintainer", models.WorkspaceRoleMaintainer)
	agent := seedApprovedResourceAgent(t, db, workspace.ID)
	credential := seedResourceVerificationCredential(t, db, workspace.ID, maintainer.ID, models.TypePassword, map[string]interface{}{
		"username": "root",
		"password": "secret123",
	})
	resource := models.Resource{
		WorkspaceID: workspace.ID,
		Name:        "terminal-replica-vm",
		Type:        models.ResourceTypeVM,
		Environment: "production",
		Status:      models.ResourceStatusOnline,
		Endpoint:    "10.0.0.61:22",
		CreatedBy:   maintainer.ID,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}
	if err := db.Create(&models.ResourceCredentialBinding{
		WorkspaceID:  workspace.ID,
		ResourceID:   resource.ID,
		CredentialID: credential.ID,
		Purpose:      "ssh_auth",
		BoundBy:      maintainer.ID,
	}).Error; err != nil {
		t.Fatalf("create binding failed: %v", err)
	}

	handlerA := NewWebSocketHandler()
	handlerA.serverID = "terminal-server-a"
	handlerB := NewWebSocketHandler()
	handlerB.serverID = "terminal-server-b"

	serverA := newTerminalSessionTestServer(t, db, handlerA)
	defer serverA.Close()
	serverB := newTerminalSessionTestServer(t, db, handlerB)
	defer serverB.Close()

	agentConn, _, err := websocket.DefaultDialer.Dial(wsTerminalAgentURL(serverA.URL, fmt.Sprintf("?agent_id=%d&token=%s", agent.ID, agent.Token)), nil)
	if err != nil {
		t.Fatalf("dial agent websocket failed: %v", err)
	}
	defer agentConn.Close()

	ownerToken := issueTerminalSessionTestToken(t, &maintainer)
	createResp := performTerminalSessionAPIRequest(serverA.Config.Handler, http.MethodPost, fmt.Sprintf("/api/resources/%d/terminal-sessions", resource.ID), workspace.ID, ownerToken, nil)
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected terminal create success, got=%d body=%s", createResp.Code, createResp.Body.String())
	}
	created := decodeResponseData[map[string]interface{}](t, createResp.Body.Bytes())
	sessionID, _ := created["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("expected session_id in create response, body=%s", createResp.Body.String())
	}

	frontendConn, _, err := websocket.DefaultDialer.Dial(wsTerminalFrontendURL(serverB.URL, fmt.Sprintf("?session_id=%s&token=%s", sessionID, ownerToken)), nil)
	if err != nil {
		t.Fatalf("dial terminal frontend websocket on replica B failed: %v", err)
	}
	defer frontendConn.Close()

	if err := agentConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set agent read deadline failed: %v", err)
	}
	if _, _, err := agentConn.ReadMessage(); err != nil {
		t.Fatalf("expected terminal open relay to agent, got err=%v", err)
	}

	agentOutput := WebSocketMessage{Type: "terminal_session_output", Payload: map[string]interface{}{"session_id": sessionID, "data": "hostname\n", "stream": "stdout"}}
	agentOutputRaw, _ := json.Marshal(agentOutput)
	if err := agentConn.WriteMessage(websocket.TextMessage, agentOutputRaw); err != nil {
		t.Fatalf("write agent output failed: %v", err)
	}

	if err := frontendConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set frontend read deadline failed: %v", err)
	}
	_, frontendOutputRaw, err := frontendConn.ReadMessage()
	if err != nil {
		t.Fatalf("expected owner-routed terminal output, got err=%v", err)
	}
	if !bytes.Contains(frontendOutputRaw, []byte(sessionID)) {
		t.Fatalf("expected owner-routed payload to include session_id, got=%s", string(frontendOutputRaw))
	}

	frontendInput := WebSocketMessage{Type: "terminal_input", Payload: map[string]interface{}{"session_id": sessionID, "data": "pwd\n"}}
	frontendInputRaw, _ := json.Marshal(frontendInput)
	if err := frontendConn.WriteMessage(websocket.TextMessage, frontendInputRaw); err != nil {
		t.Fatalf("write frontend input failed: %v", err)
	}

	if err := agentConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set agent read deadline failed: %v", err)
	}
	_, agentInputRaw, err := agentConn.ReadMessage()
	if err != nil {
		t.Fatalf("expected owner-routed terminal input to reach agent, got err=%v", err)
	}
	if !bytes.Contains(agentInputRaw, []byte(sessionID)) {
		t.Fatalf("expected agent payload to include session_id, got=%s", string(agentInputRaw))
	}

	var stored models.ResourceTerminalSession
	if err := db.Where("session_id = ?", sessionID).First(&stored).Error; err != nil {
		t.Fatalf("load terminal session failed: %v", err)
	}
	if stored.OwnerServerID != "terminal-server-b" {
		t.Fatalf("expected terminal session owner_server_id to follow frontend replica, got=%s", stored.OwnerServerID)
	}
}

func newTerminalSessionTestRouter(t *testing.T, db *gorm.DB, wsHandler *WebSocketHandler) *gin.Engine {
	t.Helper()
	config.Init()
	config.Config.Set("server.id", "terminal-test-router")
	config.Config.Set("server.internal_url", "http://127.0.0.1:8080")
	config.Config.Set("server.internal_token", "terminal-internal-token")

	router := gin.New()
	terminalHandler := NewTerminalSessionHandler()
	terminalHandler.WS = wsHandler
	router.GET("/ws/agent/heartbeat", wsHandler.HandleAgentConnection)
	router.GET("/ws/frontend/terminal", wsHandler.HandleTerminalFrontendConnection)

	resources := router.Group("/api/resources")
	resources.Use(middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceMemberRequired())
	{
		resources.POST("/:id/terminal-sessions", terminalHandler.CreateResourceTerminalSession)
		resources.GET("/:id/terminal-sessions", terminalHandler.ListResourceTerminalSessions)
		resources.GET("/:id/terminal-sessions/:session_id", terminalHandler.GetResourceTerminalSession)
		resources.POST("/:id/terminal-sessions/:session_id/close", terminalHandler.CloseResourceTerminalSession)
	}
	_ = db
	return router
}

func newTerminalSessionTestServer(t *testing.T, db *gorm.DB, wsHandler *WebSocketHandler) *httptest.Server {
	t.Helper()
	return httptest.NewServer(newTerminalSessionTestRouter(t, db, wsHandler))
}

func setupTerminalSessionTestRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	t.Setenv("JWT_SECRET", "terminal-test-secret")
	t.Setenv("AUTH_TOKEN_TTL", (4 * time.Hour).String())
	t.Setenv("AUTH_REFRESH_INTERVAL", (10 * time.Minute).String())
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
	})
	return mini
}

func issueTerminalSessionTestToken(t *testing.T, user *models.User) string {
	t.Helper()
	token, _, err := middleware.IssueTokenSession(context.Background(), user)
	if err != nil {
		t.Fatalf("issue token failed: %v", err)
	}
	return token
}

func performTerminalSessionAPIRequest(router http.Handler, method, url string, workspaceID uint64, token string, body []byte) *httptest.ResponseRecorder {
	reqBody := bytes.NewReader(body)
	if body == nil {
		reqBody = bytes.NewReader([]byte{})
	}
	req := httptest.NewRequest(method, url, reqBody)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(middleware.WorkspaceHeaderKey, fmt.Sprintf("%d", workspaceID))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func wsTerminalAgentURL(httpURL string, query string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http") + "/ws/agent/heartbeat" + query
}

func wsTerminalFrontendURL(httpURL string, query string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http") + "/ws/frontend/terminal" + query
}
