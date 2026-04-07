package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
)

func TestPipelineConfig_GetEdges(t *testing.T) {
	tests := []struct {
		name          string
		config        PipelineConfig
		expectedEdges int
		expectedFrom  string
		expectedTo    string
	}{
		{
			name: "new format with edges",
			config: PipelineConfig{
				Version: "2.0",
				Nodes: []PipelineNode{
					{ID: "1", Type: "shell"},
					{ID: "2", Type: "shell"},
					{ID: "3", Type: "shell"},
				},
				Edges: []PipelineEdge{
					{From: "1", To: "3"},
					{From: "2", To: "3"},
				},
			},
			expectedEdges: 2,
			expectedFrom:  "1",
			expectedTo:    "3",
		},
		{
			name: "old format with connections",
			config: PipelineConfig{
				Version: "1.0",
				Nodes: []PipelineNode{
					{ID: "1", Type: "shell"},
					{ID: "2", Type: "shell"},
				},
				Connections: []PipelineConnection{
					{From: "1", To: "2"},
				},
			},
			expectedEdges: 1,
			expectedFrom:  "1",
			expectedTo:    "2",
		},
		{
			name:          "empty edges",
			config:        PipelineConfig{},
			expectedEdges: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edges := tt.config.getEdges()

			if len(edges) != tt.expectedEdges {
				t.Errorf("Expected %d edges, got %d", tt.expectedEdges, len(edges))
			}

			if tt.expectedEdges > 0 {
				// Verify edge structure
				found := false
				for _, edge := range edges {
					if edge.From == tt.expectedFrom && edge.To == tt.expectedTo {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected edge from %s to %s not found", tt.expectedFrom, tt.expectedTo)
				}
			}
		})
	}
}

func TestPipelineNode_GetNodeConfig(t *testing.T) {
	tests := []struct {
		name         string
		node         PipelineNode
		expectedType string
		expectEmpty  bool
	}{
		{
			name: "new format with config",
			node: PipelineNode{
				ID:     "1",
				Type:   "shell",
				Config: map[string]interface{}{"script": "echo hello"},
			},
			expectedType: "echo hello",
		},
		{
			name: "old format with params",
			node: PipelineNode{
				ID:     "1",
				Type:   "shell",
				Params: map[string]interface{}{"script": "echo world"},
			},
			expectedType: "echo world",
		},
		{
			name: "config takes precedence",
			node: PipelineNode{
				ID:     "1",
				Type:   "shell",
				Config: map[string]interface{}{"script": "config script"},
				Params: map[string]interface{}{"script": "params script"},
			},
			expectedType: "config script",
		},
		{
			name:        "empty config and params",
			node:        PipelineNode{ID: "1", Type: "shell"},
			expectEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := tt.node.getNodeConfig()

			if tt.expectEmpty {
				if len(config) != 0 {
					t.Errorf("Expected empty config, got %v", config)
				}
				return
			}

			script, ok := config["script"].(string)
			if !ok {
				t.Error("Expected script in config")
				return
			}

			if script != tt.expectedType {
				t.Errorf("Expected script '%s', got '%s'", tt.expectedType, script)
			}
		})
	}
}

func TestPipelineConfig_ParseAndValidate(t *testing.T) {
	// Test parsing a valid pipeline config
	configJSON := `{
		"version": "2.0",
		"nodes": [
			{"id": "1", "type": "git_clone", "name": "Clone", "config": {"repository": {"url": "test.git", "branch": "main"}}},
			{"id": "2", "type": "shell", "name": "Build", "config": {"script": "npm run build"}},
			{"id": "3", "type": "shell", "name": "Test", "config": {"script": "npm test"}}
		],
		"edges": [
			{"from": "1", "to": "2"},
			{"from": "2", "to": "3"}
		]
	}`

	var config PipelineConfig
	err := json.Unmarshal([]byte(configJSON), &config)
	if err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}

	// Verify nodes
	if len(config.Nodes) != 3 {
		t.Errorf("Expected 3 nodes, got %d", len(config.Nodes))
	}

	// Verify edges
	edges := config.getEdges()
	if len(edges) != 2 {
		t.Errorf("Expected 2 edges, got %d", len(edges))
	}

	// Verify node structure
	nodeMap := make(map[string]*PipelineNode)
	for i := range config.Nodes {
		nodeMap[config.Nodes[i].ID] = &config.Nodes[i]
	}

	if nodeMap["1"] == nil {
		t.Error("Node 1 not found")
	}
	if nodeMap["2"] == nil {
		t.Error("Node 2 not found")
	}
	if nodeMap["3"] == nil {
		t.Error("Node 3 not found")
	}

	// Verify edge direction
	hasEdge12 := false
	hasEdge23 := false
	for _, edge := range edges {
		if edge.From == "1" && edge.To == "2" {
			hasEdge12 = true
		}
		if edge.From == "2" && edge.To == "3" {
			hasEdge23 = true
		}
	}

	if !hasEdge12 {
		t.Error("Expected edge from 1 to 2")
	}
	if !hasEdge23 {
		t.Error("Expected edge from 2 to 3")
	}
}

func TestParseAndValidatePipelineConfig_PreservesNodeCoordinates(t *testing.T) {
	handler := &PipelineHandler{}
	raw := `{
		"version":"2.0",
		"nodes":[
			{"id":"1","type":"shell","name":"Build","x":0,"y":0,"config":{"script":"echo build"}},
			{"id":"2","type":"shell","name":"Test","x":520,"y":340,"config":{"script":"echo test"}}
		],
		"edges":[
			{"from":"1","to":"2"}
		]
	}`

	config, refs, errMsg, err := handler.parseAndValidatePipelineConfig(raw, 0, "", 0, 0)
	if err != nil {
		t.Fatalf("expected parse success, got err=%v, msg=%s", err, errMsg)
	}
	if len(refs) != 0 {
		t.Fatalf("expected no credential refs, got %d", len(refs))
	}

	normalized, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal normalized config failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(normalized, &payload); err != nil {
		t.Fatalf("unmarshal normalized payload failed: %v", err)
	}

	nodes, ok := payload["nodes"].([]interface{})
	if !ok || len(nodes) != 2 {
		t.Fatalf("expected 2 nodes in normalized payload, got %#v", payload["nodes"])
	}

	firstNode, ok := nodes[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected first node object, got %#v", nodes[0])
	}
	secondNode, ok := nodes[1].(map[string]interface{})
	if !ok {
		t.Fatalf("expected second node object, got %#v", nodes[1])
	}

	if firstNode["x"] != float64(0) || firstNode["y"] != float64(0) {
		t.Fatalf("expected first node coordinates to persist, got x=%v y=%v", firstNode["x"], firstNode["y"])
	}
	if secondNode["x"] != float64(520) || secondNode["y"] != float64(340) {
		t.Fatalf("expected second node coordinates to persist, got x=%v y=%v", secondNode["x"], secondNode["y"])
	}
}

func TestDAGExecutionOrder(t *testing.T) {
	// Test that the execution order follows DAG dependencies
	config := PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{
			{ID: "1", Type: "git_clone", Name: "Clone"},
			{ID: "2", Type: "shell", Name: "Build"},
			{ID: "3", Type: "shell", Name: "Test"},
			{ID: "4", Type: "shell", Name: "Deploy"},
		},
		Edges: []PipelineEdge{
			{From: "1", To: "2"},
			{From: "1", To: "3"},
			{From: "2", To: "4"},
			{From: "3", To: "4"},
		},
	}

	// Build in-degree map
	inDegree := make(map[string]int)
	for _, node := range config.Nodes {
		inDegree[node.ID] = 0
	}

	for _, edge := range config.Edges {
		inDegree[edge.To]++
	}

	// Find initial nodes (in-degree 0)
	var queue []string
	for nodeID, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, nodeID)
		}
	}

	// Simulate execution order
	executed := make(map[string]bool)
	executionOrder := []string{}

	for len(queue) > 0 {
		nodeID := queue[0]
		queue = queue[1:]

		executed[nodeID] = true
		executionOrder = append(executionOrder, nodeID)

		// Find edges from this node
		for _, edge := range config.Edges {
			if edge.From == nodeID {
				inDegree[edge.To]--
				if inDegree[edge.To] == 0 {
					queue = append(queue, edge.To)
				}
			}
		}
	}

	// Verify all nodes executed
	if len(executionOrder) != 4 {
		t.Errorf("Expected 4 nodes executed, got %d", len(executionOrder))
	}

	// Verify node 1 executed first (no dependencies)
	if len(executionOrder) > 0 && executionOrder[0] != "1" {
		t.Errorf("Expected node 1 to execute first, got %s", executionOrder[0])
	}

	// Verify node 4 executed last (has most dependencies)
	if len(executionOrder) > 0 && executionOrder[len(executionOrder)-1] != "4" {
		t.Errorf("Expected node 4 to execute last, got %s", executionOrder[len(executionOrder)-1])
	}
}

func TestJSONEncode(t *testing.T) {
	handler := &PipelineHandler{}

	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{
			name:     "string",
			input:    "hello",
			expected: `"hello"`,
		},
		{
			name:     "map",
			input:    map[string]interface{}{"key": "value"},
			expected: `{"key":"value"}`,
		},
		{
			name:     "nil",
			input:    nil,
			expected: "null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.jsonEncode(tt.input)
			if result != tt.expected {
				t.Errorf("jsonEncode(%v) = %s, expected %s", tt.input, result, tt.expected)
			}
		})
	}
}

func TestValidateDAG(t *testing.T) {
	tests := []struct {
		name        string
		config      PipelineConfig
		expectValid bool
		expectErr   string
	}{
		{
			name: "valid simple DAG",
			config: PipelineConfig{
				Version: "2.0",
				Nodes: []PipelineNode{
					{ID: "1", Type: "git_clone", Name: "Clone"},
					{ID: "2", Type: "shell", Name: "Build"},
					{ID: "3", Type: "shell", Name: "Test"},
				},
				Edges: []PipelineEdge{
					{From: "1", To: "2"},
					{From: "2", To: "3"},
				},
			},
			expectValid: true,
		},
		{
			name: "valid complex DAG with multiple dependencies",
			config: PipelineConfig{
				Version: "2.0",
				Nodes: []PipelineNode{
					{ID: "1", Type: "git_clone", Name: "Clone"},
					{ID: "2", Type: "shell", Name: "Build"},
					{ID: "3", Type: "shell", Name: "Test"},
					{ID: "4", Type: "shell", Name: "Deploy"},
				},
				Edges: []PipelineEdge{
					{From: "1", To: "2"},
					{From: "1", To: "3"},
					{From: "2", To: "4"},
					{From: "3", To: "4"},
				},
			},
			expectValid: true,
		},
		{
			name: "valid DAG with multiple entry points",
			config: PipelineConfig{
				Version: "2.0",
				Nodes: []PipelineNode{
					{ID: "1", Type: "git_clone", Name: "Clone Frontend"},
					{ID: "2", Type: "git_clone", Name: "Clone Backend"},
					{ID: "3", Type: "shell", Name: "Build Frontend"},
					{ID: "4", Type: "shell", Name: "Build Backend"},
					{ID: "5", Type: "shell", Name: "Deploy All"},
				},
				Edges: []PipelineEdge{
					{From: "1", To: "3"},
					{From: "2", To: "4"},
					{From: "3", To: "5"},
					{From: "4", To: "5"},
				},
			},
			expectValid: true,
		},
		{
			name: "invalid - empty nodes",
			config: PipelineConfig{
				Version: "2.0",
				Nodes:   []PipelineNode{},
			},
			expectValid: false,
			expectErr:   "流水线配置无效：节点列表为空",
		},
		{
			name: "invalid - multiple nodes without edges",
			config: PipelineConfig{
				Version: "2.0",
				Nodes: []PipelineNode{
					{ID: "1", Type: "git_clone", Name: "Clone"},
					{ID: "2", Type: "shell", Name: "Build"},
				},
				Edges: []PipelineEdge{},
			},
			expectValid: false,
			expectErr:   "流水线配置无效：多节点流水线必须包含依赖边",
		},
		{
			name: "invalid - duplicate node IDs",
			config: PipelineConfig{
				Version: "2.0",
				Nodes: []PipelineNode{
					{ID: "1", Type: "git_clone", Name: "Clone"},
					{ID: "1", Type: "shell", Name: "Build"},
				},
				Edges: []PipelineEdge{
					{From: "1", To: "1"},
				},
			},
			expectValid: false,
			expectErr:   "流水线配置无效：节点ID '1' 重复",
		},
		{
			name: "invalid - self-referencing edge",
			config: PipelineConfig{
				Version: "2.0",
				Nodes: []PipelineNode{
					{ID: "1", Type: "shell", Name: "A"},
				},
				Edges: []PipelineEdge{
					{From: "1", To: "1"},
				},
			},
			expectValid: false,
			expectErr:   "流水线配置无效：节点 '1' 不能自引用",
		},
		{
			name: "invalid - duplicate edge",
			config: PipelineConfig{
				Version: "2.0",
				Nodes: []PipelineNode{
					{ID: "1", Type: "shell", Name: "A"},
					{ID: "2", Type: "shell", Name: "B"},
				},
				Edges: []PipelineEdge{
					{From: "1", To: "2"},
					{From: "1", To: "2"},
				},
			},
			expectValid: false,
			expectErr:   "流水线配置无效：边 '1->2' 重复",
		},
		{
			name: "valid - single node without edges",
			config: PipelineConfig{
				Version: "2.0",
				Nodes: []PipelineNode{
					{ID: "1", Type: "shell", Name: "Single Task"},
				},
				Edges: []PipelineEdge{},
			},
			expectValid: true,
		},
		{
			name: "invalid - unreachable node",
			config: PipelineConfig{
				Version: "2.0",
				Nodes: []PipelineNode{
					{ID: "1", Type: "shell", Name: "A"},
					{ID: "2", Type: "shell", Name: "B"},
					{ID: "3", Type: "shell", Name: "C"},
				},
				Edges: []PipelineEdge{
					{From: "1", To: "2"},
					// Node 3 is unreachable
				},
			},
			expectValid: false,
			expectErr:   "流水线配置无效：存在孤立节点（未连接到依赖图）: [3]",
		},
		{
			name: "invalid - disconnected components",
			config: PipelineConfig{
				Version: "2.0",
				Nodes: []PipelineNode{
					{ID: "1", Type: "shell", Name: "A"},
					{ID: "2", Type: "shell", Name: "B"},
				},
				Edges: []PipelineEdge{},
			},
			expectValid: false,
			expectErr:   "流水线配置无效：多节点流水线必须包含依赖边",
		},
		{
			name: "invalid - self-referencing node",
			config: PipelineConfig{
				Version: "2.0",
				Nodes: []PipelineNode{
					{ID: "1", Type: "shell", Name: "Single Task"},
				},
				Edges: []PipelineEdge{
					{From: "1", To: "1"},
				},
			},
			expectValid: false,
			expectErr:   "流水线配置无效：节点 '1' 不能自引用",
		},
		{
			name: "invalid - edge to non-existent node",
			config: PipelineConfig{
				Version: "2.0",
				Nodes: []PipelineNode{
					{ID: "1", Type: "git_clone", Name: "Clone"},
				},
				Edges: []PipelineEdge{
					{From: "1", To: "2"},
				},
			},
			expectValid: false,
			expectErr:   "流水线配置无效：边引用的目标节点 '2' 不存在",
		},
		{
			name: "invalid - edge from non-existent node",
			config: PipelineConfig{
				Version: "2.0",
				Nodes: []PipelineNode{
					{ID: "1", Type: "git_clone", Name: "Clone"},
				},
				Edges: []PipelineEdge{
					{From: "2", To: "1"},
				},
			},
			expectValid: false,
			expectErr:   "流水线配置无效：边引用的源节点 '2' 不存在",
		},
		{
			name: "invalid - simple cycle",
			config: PipelineConfig{
				Version: "2.0",
				Nodes: []PipelineNode{
					{ID: "1", Type: "shell", Name: "Node1"},
					{ID: "2", Type: "shell", Name: "Node2"},
				},
				Edges: []PipelineEdge{
					{From: "1", To: "2"},
					{From: "2", To: "1"},
				},
			},
			expectValid: false,
			expectErr:   "流水线配置无效：检测到循环依赖",
		},
		{
			name: "invalid - complex cycle",
			config: PipelineConfig{
				Version: "2.0",
				Nodes: []PipelineNode{
					{ID: "1", Type: "shell", Name: "Node1"},
					{ID: "2", Type: "shell", Name: "Node2"},
					{ID: "3", Type: "shell", Name: "Node3"},
				},
				Edges: []PipelineEdge{
					{From: "1", To: "2"},
					{From: "2", To: "3"},
					{From: "3", To: "1"},
				},
			},
			expectValid: false,
			expectErr:   "流水线配置无效：检测到循环依赖",
		},
		{
			name: "invalid - empty node ID",
			config: PipelineConfig{
				Version: "2.0",
				Nodes: []PipelineNode{
					{ID: "", Type: "shell", Name: "Node1"},
				},
				Edges: []PipelineEdge{},
			},
			expectValid: false,
			expectErr:   "流水线配置无效：节点ID不能为空",
		},
		{
			name: "valid - old format with connections",
			config: PipelineConfig{
				Version: "1.0",
				Nodes: []PipelineNode{
					{ID: "1", Type: "git_clone", Name: "Clone"},
					{ID: "2", Type: "shell", Name: "Build"},
					{ID: "3", Type: "shell", Name: "Test"},
				},
				Connections: []PipelineConnection{
					{From: "1", To: "2"},
					{From: "2", To: "3"},
				},
			},
			expectValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, errMsg := tt.config.ValidateDAG()

			if tt.expectValid {
				if !valid {
					t.Errorf("Expected valid DAG, but got error: %s", errMsg)
				}
			} else {
				if valid {
					t.Error("Expected invalid DAG, but got valid")
				}
				if errMsg != tt.expectErr {
					t.Errorf("Expected error '%s', but got '%s'", tt.expectErr, errMsg)
				}
			}
		})
	}
}

func TestValidatePipelineCredentialBindings_UnknownSlot(t *testing.T) {
	handler := &PipelineHandler{}
	config := PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{
			{
				ID:   "1",
				Type: "git_clone",
				Name: "Clone",
				Config: map[string]interface{}{
					"git_repo_url": "https://example.com/repo.git",
					"credentials": map[string]interface{}{
						"unknown_slot": map[string]interface{}{
							"credential_id": 1,
						},
					},
				},
			},
		},
	}

	_, err := handler.validatePipelineCredentialBindings(&config, 0, "", 0, 0)
	if err == nil {
		t.Fatalf("expected unknown slot validation error")
	}
	if !strings.Contains(err.Error(), "不支持凭据槽位") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidatePipelineCredentialBindings_CategoryMismatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "binding-user", models.WorkspaceRoleDeveloper)
	encrypted, err := NewCredentialHandler().encryptionService.EncryptCredentialData(map[string]interface{}{"token": "docker-only-token"})
	if err != nil {
		t.Fatalf("encrypt payload failed: %v", err)
	}
	credential := models.Credential{
		Name:             "docker-token",
		Type:             models.TypeToken,
		Category:         models.CategoryDocker,
		Scope:            models.ScopeWorkspace,
		WorkspaceID:      workspace.ID,
		OwnerID:          user.ID,
		EncryptedPayload: encrypted,
		Status:           models.CredentialStatusActive,
	}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatalf("create credential failed: %v", err)
	}

	handler := &PipelineHandler{DB: db}
	config := PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{{
			ID:   "1",
			Type: "git_clone",
			Name: "Clone",
			Config: map[string]interface{}{
				"git_repo_url": "https://example.com/repo.git",
				"credentials": map[string]interface{}{
					"repo_auth": map[string]interface{}{"credential_id": credential.ID},
				},
			},
		}},
	}
	_, err = handler.validatePipelineCredentialBindings(&config, user.ID, "user", 0, workspace.ID)
	if err == nil {
		t.Fatalf("expected category mismatch validation error")
	}
	if !strings.Contains(err.Error(), "不支持凭据分类") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidatePipelineCredentialBindings_RejectsMissingPayloadForType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "binding-payload-user", models.WorkspaceRoleDeveloper)
	encrypted, err := NewCredentialHandler().encryptionService.EncryptCredentialData(map[string]interface{}{"username": "oauth2"})
	if err != nil {
		t.Fatalf("encrypt payload failed: %v", err)
	}
	credential := models.Credential{
		Name:             "broken-token",
		Type:             models.TypeToken,
		Category:         models.CategoryGitHub,
		Scope:            models.ScopeWorkspace,
		WorkspaceID:      workspace.ID,
		OwnerID:          user.ID,
		EncryptedPayload: encrypted,
	}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatalf("create credential failed: %v", err)
	}

	handler := &PipelineHandler{DB: db}
	config := PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{{
			ID:   "1",
			Type: "git_clone",
			Name: "Clone",
			Config: map[string]interface{}{
				"git_repo_url": "https://example.com/repo.git",
				"credentials": map[string]interface{}{
					"repo_auth": map[string]interface{}{"credential_id": credential.ID},
				},
			},
		}},
	}
	_, err = handler.validatePipelineCredentialBindings(&config, user.ID, "user", 0, workspace.ID)
	if err == nil {
		t.Fatalf("expected missing payload validation error")
	}
	if !strings.Contains(err.Error(), "missing required payload") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Regression test: validatePipelineCredentialBindings must process flat-key bindings
// (e.g. "credentials.repo_auth.credential_id") correctly. Before the fix, the flat
// keys were never expanded, causing the validation to find no bindings at all and
// skip required slot checks.
func TestValidatePipelineCredentialBindings_FlatKeyBindings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "flat-key-validate-user", models.WorkspaceRoleDeveloper)
	encrypted, err := NewCredentialHandler().encryptionService.EncryptCredentialData(map[string]interface{}{
		"token":      "ghp_flat_key_validation",
		"token_type": "bearer",
		"username":   "oauth2",
	})
	if err != nil {
		t.Fatalf("encrypt payload failed: %v", err)
	}
	credential := models.Credential{
		Name:             "flat-key-validation-repo-auth",
		Type:             models.TypeToken,
		Category:         models.CategoryGitHub,
		Scope:            models.ScopeWorkspace,
		WorkspaceID:      workspace.ID,
		OwnerID:          user.ID,
		EncryptedPayload: encrypted,
		Status:           models.CredentialStatusActive,
	}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatalf("create credential failed: %v", err)
	}

	handler := &PipelineHandler{DB: db}
	// Flat-key format: this is the EXACT DB shape that previously caused the
	// expandFlatCredentialBindings bug to bypass all validation.
	config := PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{{
			ID:   "1",
			Type: "git_clone",
			Name: "Clone",
			Config: map[string]interface{}{
				"git_repo_url": "https://example.com/repo.git",
				// Flat key format — NOT nested
				"credentials.repo_auth.credential_id": credential.ID,
			},
		}},
	}

	refs, err := handler.validatePipelineCredentialBindings(&config, user.ID, "user", 0, workspace.ID)
	if err != nil {
		t.Fatalf("validatePipelineCredentialBindings failed with flat keys: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 credential ref, got %d", len(refs))
	}
	if refs[0].CredentialSlot != "repo_auth" {
		t.Fatalf("expected slot repo_auth, got %s", refs[0].CredentialSlot)
	}
	if refs[0].CredentialID != credential.ID {
		t.Fatalf("expected credential_id=%d, got %d", credential.ID, refs[0].CredentialID)
	}
}

func TestParseAndValidatePipelineConfig_NormalizesTaskType(t *testing.T) {
	handler := &PipelineHandler{}
	raw := `{
		"version":"2.0",
		"nodes":[
			{"id":"1","type":"github","name":"Clone","task_version":1,"params":[{"key":"git_repo_url","label":"Repo","value":"https://example.com/repo.git","is_flexible":false}]}
		],
		"edges":[]
	}`

	config, refs, errMsg, err := handler.parseAndValidatePipelineConfig(raw, 0, "", 0, 0)
	if err != nil {
		t.Fatalf("expected parse success, got err=%v, msg=%s", err, errMsg)
	}
	if len(refs) != 0 {
		t.Fatalf("expected no credential refs, got %d", len(refs))
	}
	if len(config.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(config.Nodes))
	}
	if config.Nodes[0].Type != "git_clone" {
		t.Fatalf("expected normalized type git_clone, got %s", config.Nodes[0].Type)
	}
	if config.Nodes[0].TaskKey != "git_clone" {
		t.Fatalf("expected normalized task_key git_clone, got %s", config.Nodes[0].TaskKey)
	}
	if config.Nodes[0].TaskVersion != 1 {
		t.Fatalf("expected task_version=1, got %d", config.Nodes[0].TaskVersion)
	}
	if len(config.Nodes[0].DefinitionParams) != 1 {
		t.Fatalf("expected definition params preserved, got %#v", config.Nodes[0].DefinitionParams)
	}
}

func TestParseAndValidatePipelineConfig_RejectsUnknownParamKey(t *testing.T) {
	handler := &PipelineHandler{}
	raw := `{
		"version":"2.0",
		"nodes":[
			{"id":"1","type":"git_clone","name":"Clone","task_version":1,"params":[{"key":"unknown_param","label":"Unknown","value":"x","is_flexible":false}]}
		],
		"edges":[]
	}`

	_, _, errMsg, err := handler.parseAndValidatePipelineConfig(raw, 0, "", 0, 0)
	if err == nil {
		t.Fatalf("expected validation failure for unknown param key")
	}
	if !strings.Contains(errMsg, "参数 key 'unknown_param'") {
		t.Fatalf("unexpected error message: %s", errMsg)
	}
}

func TestCreatePipelineRunRecordWithSnapshot_StoresNewRunContractSnapshots(t *testing.T) {
	db := openHandlerTestDB(t)
	handler := &PipelineHandler{DB: db}
	pipeline := models.Pipeline{
		Name:        "typed-pipeline",
		WorkspaceID: 99,
		OwnerID:     7,
		Definition:  `{"nodes":[{"node_id":"node_1"}]}`,
		Version:     3,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	config := PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{{
			ID:          "node_1",
			Type:        "shell",
			TaskKey:     "shell",
			TaskVersion: 1,
			Name:        "Shell",
			DefinitionParams: []models.PipelineDefinitionParam{{
				Key:        "script",
				Label:      "脚本",
				Value:      "echo hi",
				IsFlexible: true,
			}},
		}},
	}

	run, _, err := handler.createPipelineRunRecordWithSnapshot(db, pipeline, config, pipelineRunTriggerContext{
		TriggerType:     "manual",
		TriggerSource:   "pipeline_detail",
		TriggerUser:     "admin",
		TriggerUserID:   7,
		TriggerUserRole: "admin",
		RunConfig: models.PipelineRunConfigSnapshot{
			Trigger: models.PipelineRunTriggerSnapshot{Type: "manual", Source: "pipeline_detail", Operator: "admin"},
			Inputs: map[string]map[string]interface{}{
				"node_1": {"script": "echo override"},
			},
			Options: map[string]interface{}{"dry_run": false},
		},
	})
	if err != nil {
		t.Fatalf("create run failed: %v", err)
	}
	if run.RunConfig == "" || run.PipelineSnapshot == "" || run.ResolvedNodes == "" || run.Outputs == "" || run.Events == "" {
		t.Fatalf("expected new run snapshot columns to be populated, got run=%+v", run)
	}
	if run.Config == "" || run.Config == run.PipelineSnapshot {
		t.Fatalf("expected legacy execution config snapshot to be populated separately from authored pipeline snapshot")
	}

	var runConfigSnapshot models.PipelineRunConfigSnapshot
	if err := json.Unmarshal([]byte(run.RunConfig), &runConfigSnapshot); err != nil {
		t.Fatalf("unmarshal run config failed: %v", err)
	}
	if runConfigSnapshot.Trigger.Type != "manual" {
		t.Fatalf("expected trigger type manual, got %+v", runConfigSnapshot.Trigger)
	}
	if runConfigSnapshot.Inputs["node_1"]["script"] != "echo override" {
		t.Fatalf("expected node-scoped inputs to be stored, got %#v", runConfigSnapshot.Inputs)
	}

	var pipelineSnapshot PipelineConfig
	if err := json.Unmarshal([]byte(run.PipelineSnapshot), &pipelineSnapshot); err != nil {
		t.Fatalf("unmarshal pipeline snapshot failed: %v", err)
	}
	if len(pipelineSnapshot.Nodes) != 1 || pipelineSnapshot.Nodes[0].TaskKey != "shell" {
		t.Fatalf("expected pipeline snapshot to preserve task key, got %#v", pipelineSnapshot.Nodes)
	}
}

func TestCreatePipelineRunRecordWithSnapshot_DeploymentExecutionConfigDoesNotMutateAuthoredSnapshot(t *testing.T) {
	db := openHandlerTestDB(t)
	handler := &PipelineHandler{DB: db}
	pipeline := models.Pipeline{
		Name:        "deployment-authored-snapshot",
		WorkspaceID: 77,
		OwnerID:     9,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	authored := PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{{
			ID:      "deploy",
			Type:    "shell",
			TaskKey: "shell",
			Name:    "Deploy",
			Config: map[string]interface{}{
				"script": "echo ${inputs.app_name}",
				"host":   "${inputs.resource_host}",
			},
		}},
	}
	execution := clonePipelineConfig(authored)
	execution.Nodes[0].Config["script"] = "echo nginx-web"
	execution.Nodes[0].Config["host"] = "10.0.0.8"

	run, _, err := handler.createPipelineRunRecordWithSnapshot(db, pipeline, authored, pipelineRunTriggerContext{
		TriggerType:     pipelineRunTriggerTypeDeploymentRequest,
		TriggerUser:     "release-bot",
		TriggerUserID:   9,
		TriggerUserRole: "developer",
		RunConfig: models.PipelineRunConfigSnapshot{
			Inputs: map[string]map[string]interface{}{
				"deploy": {
					"app_name":      "nginx-web",
					"resource_host": "10.0.0.8",
				},
			},
		},
		ExecutionConfig: &execution,
	})
	if err != nil {
		t.Fatalf("create run failed: %v", err)
	}

	var snapshot PipelineConfig
	if err := json.Unmarshal([]byte(run.PipelineSnapshot), &snapshot); err != nil {
		t.Fatalf("unmarshal pipeline snapshot failed: %v", err)
	}
	if got := snapshot.Nodes[0].Config["script"]; got != "echo ${inputs.app_name}" {
		t.Fatalf("expected authored snapshot script, got %#v", got)
	}
	if got := snapshot.Nodes[0].Config["host"]; got != "${inputs.resource_host}" {
		t.Fatalf("expected authored snapshot host, got %#v", got)
	}

	var legacyExecution PipelineConfig
	if err := json.Unmarshal([]byte(run.Config), &legacyExecution); err != nil {
		t.Fatalf("unmarshal execution config failed: %v", err)
	}
	if got := legacyExecution.Nodes[0].Config["script"]; got != "echo nginx-web" {
		t.Fatalf("expected resolved execution script, got %#v", got)
	}
	if got := legacyExecution.Nodes[0].Config["host"]; got != "10.0.0.8" {
		t.Fatalf("expected resolved execution host, got %#v", got)
	}
}

func TestCreatePipelineRunRecordWithSnapshot_PopulatesBindingsResolvedNodesAndLifecycleEvents(t *testing.T) {
	db := openHandlerTestDB(t)
	handler := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "run-contract-user", models.WorkspaceRoleDeveloper)

	encrypted, err := NewCredentialHandler().encryptionService.EncryptCredentialData(map[string]interface{}{
		"token":      "ghp_pipeline_token",
		"token_type": "bearer",
		"username":   "oauth2",
	})
	if err != nil {
		t.Fatalf("encrypt payload failed: %v", err)
	}
	credential := models.Credential{
		Name:             "repo-auth",
		Type:             models.TypeToken,
		Category:         models.CategoryGitHub,
		Scope:            models.ScopeWorkspace,
		WorkspaceID:      workspace.ID,
		OwnerID:          user.ID,
		EncryptedPayload: encrypted,
		Status:           models.CredentialStatusActive,
	}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatalf("create credential failed: %v", err)
	}

	resource := models.Resource{
		WorkspaceID: workspace.ID,
		Name:        "prod-vm-01",
		Type:        models.ResourceTypeVM,
		Environment: "production",
		Status:      models.ResourceStatusOnline,
		Endpoint:    "10.0.0.8:22",
		CreatedBy:   user.ID,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	pipeline := models.Pipeline{
		Name:        "run-contract-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Version:     5,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	config := PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{
			{
				ID:          "node_1",
				Name:        "Clone",
				Type:        "git_clone",
				TaskKey:     "git_clone",
				TaskVersion: 1,
				DefinitionParams: []models.PipelineDefinitionParam{
					{Key: "git_repo_url", Value: "https://example.com/repo.git", IsFlexible: false},
					{Key: "git_ref", Value: "main", IsFlexible: true},
				},
				CredentialBindings: map[string]uint64{"repo_auth": credential.ID},
				Metadata:           map[string]interface{}{"x": 100.0, "y": 120.0},
			},
			{
				ID:          "node_2",
				Name:        "Deploy",
				Type:        "docker-run",
				TaskKey:     "docker-run",
				TaskVersion: 1,
				DefinitionParams: []models.PipelineDefinitionParam{
					{Key: "image", Value: "nginx:latest", IsFlexible: false},
					{Key: "target_resource_id", Value: resource.ID, IsFlexible: false},
				},
				ResourceBindings: map[string]uint64{"target_resource_id": resource.ID},
				Metadata:         map[string]interface{}{"x": 420.0, "y": 120.0},
			},
		},
		Edges: []PipelineEdge{{From: "node_1", To: "node_2"}},
		Triggers: []map[string]interface{}{
			{"type": "manual", "enabled": true},
		},
		Metadata: map[string]interface{}{"version": "2.0"},
	}

	run, _, err := handler.createPipelineRunRecordWithSnapshot(db, pipeline, config, pipelineRunTriggerContext{
		TriggerType:     "manual",
		TriggerSource:   "pipeline_detail",
		TriggerUser:     "admin",
		TriggerUserID:   user.ID,
		TriggerUserRole: "developer",
		RunConfig: models.PipelineRunConfigSnapshot{
			Trigger: models.PipelineRunTriggerSnapshot{Type: "manual", Source: "pipeline_detail", Operator: "admin"},
			Inputs: map[string]map[string]interface{}{
				"node_1": {"git_ref": "release/2026.04"},
			},
		},
	})
	if err != nil {
		t.Fatalf("create run failed: %v", err)
	}

	var resolvedNodes []map[string]interface{}
	if err := json.Unmarshal([]byte(run.ResolvedNodes), &resolvedNodes); err != nil {
		t.Fatalf("unmarshal resolved nodes failed: %v", err)
	}
	if len(resolvedNodes) != 2 {
		t.Fatalf("expected resolved node skeletons for all nodes, got %#v", resolvedNodes)
	}
	if resolvedNodes[0]["task_key"] == nil || resolvedNodes[0]["status"] == nil {
		t.Fatalf("expected resolved node skeleton to include task_key and status, got %#v", resolvedNodes[0])
	}

	var bindings map[string]interface{}
	if err := json.Unmarshal([]byte(run.BindingsSnapshot), &bindings); err != nil {
		t.Fatalf("unmarshal bindings snapshot failed: %v", err)
	}
	credentials, _ := bindings["credentials"].(map[string]interface{})
	nodeCredentials, _ := credentials["node_1"].(map[string]interface{})
	repoAuth, _ := nodeCredentials["repo_auth"].(map[string]interface{})
	if repoAuth["credential_id"] != float64(credential.ID) || repoAuth["credential_name"] != credential.Name {
		t.Fatalf("expected credential binding snapshot for node_1, got %#v", repoAuth)
	}
	resources, _ := bindings["resources"].(map[string]interface{})
	nodeResources, _ := resources["node_2"].(map[string]interface{})
	targetResource, _ := nodeResources["target_resource_id"].(map[string]interface{})
	if targetResource["resource_id"] != float64(resource.ID) || targetResource["resource_name"] != resource.Name {
		t.Fatalf("expected resource binding snapshot for node_2, got %#v", targetResource)
	}

	var events []map[string]interface{}
	if err := json.Unmarshal([]byte(run.Events), &events); err != nil {
		t.Fatalf("unmarshal events failed: %v", err)
	}
	if len(events) < 2 {
		t.Fatalf("expected lifecycle events to include run_created and run_started/run_queued, got %#v", events)
	}
	if events[0]["event_type"] != "run_created" {
		t.Fatalf("expected first event run_created, got %#v", events)
	}
}

func TestGetRunDetail_PrefersRunRecordResolvedNodesAndOutputs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "run-detail-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{
		Name:        "run-detail-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Config:      `{"version":"2.0","nodes":[{"id":"node_1","type":"shell","name":"Build","config":{"script":"echo hi"}}],"edges":[]}`,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	resolvedNodesJSON := `[{"node_id":"node_1","task_type":"shell","name":"Build","config":{"script":"echo resolved"}}]`
	outputsJSON := `{"node_1":{"status":"execute_success","exit_code":0,"commit_sha":"run-record-commit"}}`
	run := models.PipelineRun{
		WorkspaceID:      workspace.ID,
		PipelineID:       pipeline.ID,
		BuildNumber:      1,
		Status:           models.PipelineRunStatusSuccess,
		TriggerType:      "manual",
		Config:           pipeline.Config,
		PipelineSnapshot: pipeline.Config,
		ResolvedNodes:    resolvedNodesJSON,
		Outputs:          outputsJSON,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}
	task := models.AgentTask{
		WorkspaceID:   workspace.ID,
		AgentID:       1,
		PipelineRunID: run.ID,
		NodeID:        "node_1",
		TaskType:      "shell",
		Name:          "Build",
		Status:        models.TaskStatusExecuteSuccess,
		ResultData:    `{"commit_sha":"task-row-commit"}`,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/pipelines/1/runs/1", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(pipeline.ID, 10)}, {Key: "run_id", Value: strconv.FormatUint(run.ID, 10)}}
	c.Set("workspace_id", workspace.ID)

	h.GetRunDetail(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"resolved_nodes_json":[{"config":{"script":"echo resolved"},"name":"Build","node_id":"node_1","task_type":"shell"}]`)) {
		t.Fatalf("expected resolved_nodes_json response to use run record payload, got %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"outputs_json":{"node_1":{"commit_sha":"run-record-commit","exit_code":0,"status":"execute_success"}}`)) {
		t.Fatalf("expected outputs_json response to use run record payload, got %s", w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("task-row-commit")) {
		t.Fatalf("expected run detail to avoid task result_data as truth, got %s", w.Body.String())
	}
}

func TestUpdatePipeline_NullProjectIDRemainsNull(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}

	if err := db.Exec("INSERT INTO pipelines (created_at, updated_at, name, description, config, workspace_id, project_id, owner_id, environment, is_public, is_favorite) VALUES (CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, ?, ?, ?, ?, NULL, ?, ?, ?, ?)",
		"null-project-pipeline",
		"pipeline without project",
		`{"version":"2.0","nodes":[{"id":"1","type":"shell","name":"Build","config":{"script":"echo build"}}],"edges":[]}`,
		uint64(1),
		uint64(1),
		"test",
		false,
		false,
	).Error; err != nil {
		t.Fatalf("insert pipeline failed: %v", err)
	}

	var pipeline models.Pipeline
	if err := db.Where("name = ?", "null-project-pipeline").First(&pipeline).Error; err != nil {
		t.Fatalf("load pipeline failed: %v", err)
	}

	body := bytes.NewBufferString(`{"description":"updated description"}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/pipelines/1", body)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: "1"}}
	c.Set("user_id", uint64(1))
	c.Set("role", "admin")
	c.Set("workspace_id", uint64(1))

	h.UpdatePipeline(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var projectID sql.NullInt64
	if err := db.Raw("SELECT project_id FROM pipelines WHERE id = ?", pipeline.ID).Scan(&projectID).Error; err != nil {
		t.Fatalf("query project_id failed: %v", err)
	}
	if projectID.Valid {
		t.Fatalf("expected project_id to remain NULL, got %d", projectID.Int64)
	}
}

func TestCreatePipeline_WithoutProjectIDStoresNullProject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "create-null-project", models.WorkspaceRoleDeveloper)

	body := bytes.NewBufferString(`{"name":"pipeline-without-project","environment":"development","config":"{\"version\":\"2.0\",\"nodes\":[{\"id\":\"1\",\"type\":\"in_app\",\"name\":\"Notify\",\"config\":{\"title\":\"done\"}}],\"edges\":[]}"}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/pipelines", body)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("user_id", user.ID)
	c.Set("role", "user")
	c.Set("workspace_id", workspace.ID)

	h.CreatePipeline(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var pipeline models.Pipeline
	if err := db.Where("name = ?", "pipeline-without-project").First(&pipeline).Error; err != nil {
		t.Fatalf("load pipeline failed: %v", err)
	}

	var projectID sql.NullInt64
	if err := db.Raw("SELECT project_id FROM pipelines WHERE id = ?", pipeline.ID).Scan(&projectID).Error; err != nil {
		t.Fatalf("query project_id failed: %v", err)
	}
	if projectID.Valid {
		t.Fatalf("expected project_id to be NULL, got %d", projectID.Int64)
	}
}

func TestCreatePipeline_PersistsDefinitionJSONAsSourceOfTruth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "create-definition-pipeline", models.WorkspaceRoleDeveloper)

	body := bytes.NewBufferString(`{
		"name":"definition-pipeline",
		"environment":"development",
		"config":"{\"version\":\"2.0\",\"nodes\":[{\"id\":\"legacy\",\"type\":\"shell\",\"name\":\"Legacy\",\"config\":{\"script\":\"echo legacy\"}}],\"edges\":[]}",
		"definition_json":"{\"version\":\"2.0\",\"nodes\":[{\"node_id\":\"node_1\",\"node_name\":\"Build\",\"task_key\":\"shell\",\"task_version\":1,\"params\":[{\"key\":\"script\",\"label\":\"脚本\",\"value\":\"echo definition\",\"is_flexible\":true}],\"credential_bindings\":{},\"resource_bindings\":{},\"metadata\":{\"x\":120,\"y\":220}}],\"edges\":[],\"triggers\":[],\"metadata\":{\"version\":\"2.0\"}}"
	}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/pipelines", body)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("user_id", user.ID)
	c.Set("role", "user")
	c.Set("workspace_id", workspace.ID)

	h.CreatePipeline(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var pipeline models.Pipeline
	if err := db.Where("name = ?", "definition-pipeline").First(&pipeline).Error; err != nil {
		t.Fatalf("load pipeline failed: %v", err)
	}
	if strings.TrimSpace(pipeline.Definition) == "" {
		t.Fatalf("expected definition_json to be persisted")
	}

	var definition PipelineConfig
	if err := json.Unmarshal([]byte(pipeline.Definition), &definition); err != nil {
		t.Fatalf("unmarshal definition failed: %v", err)
	}
	if len(definition.Nodes) != 1 {
		t.Fatalf("expected one definition node, got %#v", definition.Nodes)
	}
	if definition.Nodes[0].ID != "node_1" {
		t.Fatalf("expected node_id to normalize to node_1, got %#v", definition.Nodes[0].ID)
	}
	if definition.Nodes[0].TaskKey != "shell" {
		t.Fatalf("expected task_key=shell, got %#v", definition.Nodes[0].TaskKey)
	}
	if len(definition.Nodes[0].DefinitionParams) != 1 || definition.Nodes[0].DefinitionParams[0].Value != "echo definition" {
		t.Fatalf("expected authored params to be preserved, got %#v", definition.Nodes[0].DefinitionParams)
	}
}

func TestGetPipelineTaskTypes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &PipelineHandler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	handler.GetPipelineTaskTypes(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if int(resp["code"].(float64)) != 200 {
		t.Fatalf("expected code=200, got %+v", resp["code"])
	}

	data, ok := resp["data"].([]interface{})
	if !ok || len(data) == 0 {
		t.Fatalf("expected non-empty task type list")
	}
}

func TestApplyServerCredentialConfig_WebhookMTLS(t *testing.T) {
	nodeConfig := map[string]interface{}{}
	slot := taskCredentialSlot{Slot: "webhook_mtls"}
	credential := models.Credential{Type: models.TypeCert}
	decrypted := map[string]interface{}{
		"cert_pem":    "CERT",
		"key_pem":     "KEY",
		"ca_cert":     "CA",
		"server_name": "api.example.com",
	}

	applyServerCredentialConfig("webhook", slot, credential, decrypted, nodeConfig)

	if nodeConfig["tls_client_cert"] != "CERT" {
		t.Fatalf("expected tls_client_cert to be populated")
	}
	if nodeConfig["tls_client_key"] != "KEY" {
		t.Fatalf("expected tls_client_key to be populated")
	}
	if nodeConfig["tls_ca_cert"] != "CA" {
		t.Fatalf("expected tls_ca_cert to be populated")
	}
	if nodeConfig["tls_server_name"] != "api.example.com" {
		t.Fatalf("expected tls_server_name to be populated")
	}
}

func TestBuildWebhookTLSConfig_Empty(t *testing.T) {
	tlsConfig, err := buildWebhookTLSConfig(map[string]interface{}{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if tlsConfig != nil {
		t.Fatalf("expected nil tls config for empty input")
	}
}

func TestBuildWebhookTLSConfig_InsecureSkipVerify(t *testing.T) {
	tlsConfig, err := buildWebhookTLSConfig(map[string]interface{}{
		"tls_insecure_skip_verify": true,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if tlsConfig == nil || !tlsConfig.InsecureSkipVerify {
		t.Fatalf("expected tls config with insecure skip verify")
	}
}

func TestBuildWebhookTLSConfig_InvalidCombination(t *testing.T) {
	_, err := buildWebhookTLSConfig(map[string]interface{}{
		"tls_client_cert": "only-cert",
	})
	if err == nil || !strings.Contains(err.Error(), "without tls_client_key") {
		t.Fatalf("expected tls_client_cert without key error, got %v", err)
	}
}

func TestBuildWebhookTLSConfig_InvalidCA(t *testing.T) {
	_, err := buildWebhookTLSConfig(map[string]interface{}{
		"tls_ca_cert": "not-a-pem",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid tls_ca_cert PEM") {
		t.Fatalf("expected invalid tls_ca_cert PEM error, got %v", err)
	}
}

func TestGetPipelineRuns_ExcludesDeploymentRequestRuns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "pipeline-history-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{Name: "history-pipeline", WorkspaceID: workspace.ID, OwnerID: user.ID, Config: `{"version":"2.0","nodes":[{"id":"1","type":"shell","name":"Build","config":{"script":"echo hi"}}],"edges":[]}`}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	runs := []models.PipelineRun{
		{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 1, Status: models.PipelineRunStatusSuccess, TriggerType: "manual", TriggerUser: "builder"},
		{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 2, Status: models.PipelineRunStatusSuccess, TriggerType: "deployment_request", TriggerUser: "release-bot"},
	}
	for i := range runs {
		if err := db.Create(&runs[i]).Error; err != nil {
			t.Fatalf("create run failed: %v", err)
		}
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/pipelines/1/history", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(pipeline.ID, 10)}}
	c.Set("workspace_id", workspace.ID)

	h.GetPipelineRuns(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("release-bot")) {
		t.Fatalf("expected deployment-triggered run excluded from pipeline history, got %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"total":1`)) {
		t.Fatalf("expected total=1 after excluding deployment-triggered runs, got %s", w.Body.String())
	}
}

func TestGetPipelineStatistics_ExcludesDeploymentRequestRuns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "pipeline-stats-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{Name: "stats-pipeline", WorkspaceID: workspace.ID, OwnerID: user.ID, Config: `{"version":"2.0","nodes":[{"id":"1","type":"shell","name":"Build","config":{"script":"echo hi"}}],"edges":[]}`}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	runs := []models.PipelineRun{
		{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 1, Status: models.PipelineRunStatusSuccess, TriggerType: "manual", Duration: 60},
		{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 2, Status: models.PipelineRunStatusFailed, TriggerType: "deployment_request", Duration: 180},
	}
	for i := range runs {
		if err := db.Create(&runs[i]).Error; err != nil {
			t.Fatalf("create run failed: %v", err)
		}
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/pipelines/1/statistics", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(pipeline.ID, 10)}}
	c.Set("workspace_id", workspace.ID)

	h.GetPipelineStatistics(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"total_runs":1`)) {
		t.Fatalf("expected deployment-triggered runs excluded from statistics total, got %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"failed_runs":0`)) {
		t.Fatalf("expected deployment-triggered failures excluded from statistics, got %s", w.Body.String())
	}
}

func TestGetPipelineList_ExcludesManagementHiddenPipelinesByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "pipeline-list-user", models.WorkspaceRoleDeveloper)

	visible := models.Pipeline{
		Name:        "visible-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Config:      `{"version":"2.0","nodes":[{"id":"1","type":"shell","name":"Build","config":{"script":"echo visible"}}],"edges":[]}`,
	}
	hidden := models.Pipeline{
		Name:             "publish-owned-pipeline",
		WorkspaceID:      workspace.ID,
		OwnerID:          user.ID,
		Config:           `{"version":"2.0","nodes":[{"id":"1","type":"shell","name":"Build","config":{"script":"echo hidden"}}],"edges":[]}`,
		ManagementHidden: true,
	}
	if err := db.Create(&visible).Error; err != nil {
		t.Fatalf("create visible pipeline failed: %v", err)
	}
	if err := db.Create(&hidden).Error; err != nil {
		t.Fatalf("create hidden pipeline failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/pipelines", nil)
	c.Set("user_id", user.ID)
	c.Set("role", "user")
	c.Set("workspace_id", workspace.ID)

	h.GetPipelineList(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("visible-pipeline")) {
		t.Fatalf("expected visible pipeline in response, got %s", w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("publish-owned-pipeline")) {
		t.Fatalf("expected management-hidden pipeline excluded from response, got %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"total":1`)) {
		t.Fatalf("expected hidden pipeline excluded from total count, got %s", w.Body.String())
	}
}

func TestGetPipelineList_IncludesManagementHiddenPipelinesWhenRequested(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "pipeline-hidden-user", models.WorkspaceRoleDeveloper)

	visible := models.Pipeline{Name: "visible-pipeline", WorkspaceID: workspace.ID, OwnerID: user.ID, Config: `{"version":"2.0","nodes":[{"id":"1","type":"shell","name":"Build","config":{"script":"echo visible"}}],"edges":[]}`}
	hidden := models.Pipeline{Name: "publish-owned-pipeline", WorkspaceID: workspace.ID, OwnerID: user.ID, Config: `{"version":"2.0","nodes":[{"id":"1","type":"shell","name":"Build","config":{"script":"echo hidden"}}],"edges":[]}`, ManagementHidden: true}
	if err := db.Create(&visible).Error; err != nil {
		t.Fatalf("create visible pipeline failed: %v", err)
	}
	if err := db.Create(&hidden).Error; err != nil {
		t.Fatalf("create hidden pipeline failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/pipelines?include_publish_owned=true", nil)
	c.Set("user_id", user.ID)
	c.Set("role", "user")
	c.Set("workspace_id", workspace.ID)

	h.GetPipelineList(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("publish-owned-pipeline")) {
		t.Fatalf("expected management-hidden pipeline included when explicitly requested, got %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"total":2`)) {
		t.Fatalf("expected hidden pipeline included in total count, got %s", w.Body.String())
	}
}

func TestUpdatePipelineTriggers_PersistsDisabledFlagsAndBlankCron(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "trigger-settings-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{
		Name:        "trigger-settings-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Environment: "development",
		Config:      `{"version":"2.0","nodes":[{"id":"1","type":"in_app","name":"Notify","config":{"title":"done"}}],"edges":[]}`,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	body := bytes.NewBuffer(mustJSON(t, map[string]interface{}{
		"provider":                            "gitlab",
		"webhook_enabled":                     false,
		"push_enabled":                        false,
		"tag_enabled":                         false,
		"schedule_enabled":                    false,
		"cron_expression":                     "",
		"timezone":                            "UTC",
		"push_branch_filters":                 "main\nrelease/*",
		"tag_filters":                         "v*",
		"merge_request_source_branch_filters": "feature/*",
		"merge_request_target_branch_filters": "main",
	}))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/pipelines/1/triggers", body)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(pipeline.ID, 10)}}
	c.Set("user_id", user.ID)
	c.Set("role", "user")
	c.Set("workspace_id", workspace.ID)

	h.UpdatePipelineTriggers(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var trigger models.PipelineTrigger
	if err := db.Where("pipeline_id = ?", pipeline.ID).First(&trigger).Error; err != nil {
		t.Fatalf("load trigger failed: %v", err)
	}
	if trigger.WebhookEnabled {
		t.Fatalf("expected webhook_enabled=false to persist")
	}
	if trigger.PushEnabled {
		t.Fatalf("expected push_enabled=false to persist")
	}
	if trigger.TagEnabled {
		t.Fatalf("expected tag_enabled=false to persist")
	}
	if trigger.ScheduleEnabled {
		t.Fatalf("expected schedule_enabled=false to persist")
	}
	if trigger.CronExpression != "" {
		t.Fatalf("expected blank cron expression to persist, got %q", trigger.CronExpression)
	}
	if trigger.PushBranchFilters != "main\nrelease/*" {
		t.Fatalf("expected push branch filters to persist, got %q", trigger.PushBranchFilters)
	}
	if trigger.TagFilters != "v*" {
		t.Fatalf("expected tag filters to persist, got %q", trigger.TagFilters)
	}
	if trigger.MergeRequestSourceBranchFilters != "feature/*" {
		t.Fatalf("expected mr source branch filters to persist, got %q", trigger.MergeRequestSourceBranchFilters)
	}
	if trigger.MergeRequestTargetBranchFilters != "main" {
		t.Fatalf("expected mr target branch filters to persist, got %q", trigger.MergeRequestTargetBranchFilters)
	}
}

func TestGetPipelineTriggers_ReturnsWorkspaceScopedConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "get-trigger-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{
		Name:        "get-trigger-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Environment: "development",
		Config:      `{"version":"2.0","nodes":[{"id":"1","type":"in_app","name":"Notify","config":{"title":"done"}}],"edges":[]}`,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	trigger := models.PipelineTrigger{
		WorkspaceID:                     workspace.ID,
		PipelineID:                      pipeline.ID,
		Provider:                        "gitlab",
		WebhookEnabled:                  true,
		PushEnabled:                     true,
		TagEnabled:                      false,
		ScheduleEnabled:                 true,
		CronExpression:                  "0 0 * * *",
		Timezone:                        "UTC",
		SecretToken:                     "secret-token",
		WebhookToken:                    "public-token",
		PushBranchFilters:               "main\nrelease/*",
		MergeRequestTargetBranchFilters: "main",
	}
	if err := db.Create(&trigger).Error; err != nil {
		t.Fatalf("create trigger failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/pipelines/1/triggers", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(pipeline.ID, 10)}}
	c.Set("user_id", user.ID)
	c.Set("role", "user")
	c.Set("workspace_id", workspace.ID)

	h.GetPipelineTriggers(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"provider":"gitlab"`)) {
		t.Fatalf("expected provider in response, got %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"push_enabled":true`)) {
		t.Fatalf("expected push_enabled=true in response, got %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"cron_expression":"0 0 * * *"`)) {
		t.Fatalf("expected cron expression in response, got %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"push_branch_filters":"main\nrelease/*"`)) {
		t.Fatalf("expected push branch filters in response, got %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`/api/pipeline/run/webhook/public-token`)) {
		t.Fatalf("expected vendor-neutral webhook url in response, got %s", w.Body.String())
	}
}

func TestHandleGitLabWebhook_PushCreatesQueuedWebhookRun(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "webhook-trigger-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{
		Name:        "webhook-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Environment: "development",
		Config:      `{"version":"2.0","nodes":[{"id":"1","type":"git_clone","name":"Clone","config":{"repository":{"url":"https://example.com/repo.git","branch":"main"}}},{"id":"2","type":"shell","name":"Build","config":{"script":"echo build"}}],"edges":[{"from":"1","to":"2"}]}`,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	trigger := models.PipelineTrigger{
		WorkspaceID:       workspace.ID,
		PipelineID:        pipeline.ID,
		Provider:          "gitlab",
		WebhookEnabled:    true,
		PushEnabled:       true,
		SecretToken:       "gitlab-secret",
		WebhookToken:      "public-trigger-token",
		Timezone:          "UTC",
		PushBranchFilters: "main\nrelease/*",
	}
	if err := db.Create(&trigger).Error; err != nil {
		t.Fatalf("create trigger failed: %v", err)
	}

	payload := mustJSON(t, map[string]interface{}{
		"object_kind": "push",
		"ref":         "refs/heads/main",
		"project": map[string]interface{}{
			"path_with_namespace": "group/project",
		},
		"user_username": "gitlab-user",
		"checkout_sha":  "abc123def456",
	})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/pipeline/run/webhook/public-trigger-token", bytes.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("X-Gitlab-Token", "gitlab-secret")
	c.Params = gin.Params{{Key: "token", Value: "public-trigger-token"}}

	h.HandleGitLabWebhook(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var run models.PipelineRun
	if err := db.Where("pipeline_id = ?", pipeline.ID).First(&run).Error; err != nil {
		t.Fatalf("load pipeline run failed: %v", err)
	}
	if run.TriggerType != "webhook" {
		t.Fatalf("trigger_type=%s, want webhook", run.TriggerType)
	}
	if run.Status != models.PipelineRunStatusQueued {
		t.Fatalf("status=%s, want queued", run.Status)
	}
	if run.TriggerUser != "gitlab-user" {
		t.Fatalf("trigger_user=%s, want gitlab-user", run.TriggerUser)
	}
	if !strings.Contains(run.TriggerSource, "gitlab:push") {
		t.Fatalf("trigger_source=%s, want gitlab:push metadata", run.TriggerSource)
	}

	var runConfig models.PipelineRunConfigSnapshot
	if err := json.Unmarshal([]byte(run.RunConfig), &runConfig); err != nil {
		t.Fatalf("unmarshal run config failed: %v", err)
	}
	if runConfig.Inputs["1"]["git_ref"] != "main" {
		t.Fatalf("expected webhook git_ref runtime input, got %#v", runConfig.Inputs)
	}
	if runConfig.Inputs["1"]["git_commit"] != "abc123def456" {
		t.Fatalf("expected webhook git_commit runtime input, got %#v", runConfig.Inputs)
	}

	var pipelineSnapshot PipelineConfig
	if err := json.Unmarshal([]byte(run.PipelineSnapshot), &pipelineSnapshot); err != nil {
		t.Fatalf("unmarshal pipeline snapshot failed: %v", err)
	}
	if len(pipelineSnapshot.Nodes[0].DefinitionParams) == 0 {
		t.Fatalf("expected pipeline snapshot to preserve authored params, got %#v", pipelineSnapshot.Nodes[0])
	}
	authoredParams := map[string]interface{}{}
	for _, param := range pipelineSnapshot.Nodes[0].DefinitionParams {
		authoredParams[param.Key] = param.Value
	}
	if authoredParams["git_ref"] != "main" {
		t.Fatalf("pipeline snapshot git_ref=%v, want authored main", authoredParams["git_ref"])
	}
	if _, exists := authoredParams["git_commit"]; exists && strings.TrimSpace(toString(authoredParams["git_commit"])) != "" {
		t.Fatalf("expected pipeline snapshot to avoid patched git_commit, got %#v", authoredParams)
	}

	var executionConfig PipelineConfig
	if err := json.Unmarshal([]byte(run.Config), &executionConfig); err != nil {
		t.Fatalf("unmarshal execution config failed: %v", err)
	}
	if executionConfig.Nodes[0].Config["git_ref"] != "main" {
		t.Fatalf("execution git_ref=%v, want main", executionConfig.Nodes[0].Config["git_ref"])
	}
	if executionConfig.Nodes[0].Config["git_commit"] != "abc123def456" {
		t.Fatalf("execution commit=%v, want abc123def456", executionConfig.Nodes[0].Config["git_commit"])
	}
}

func TestHandleGitLabWebhook_PushBranchFilterMissReturnsIgnored(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "webhook-filter-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{
		Name:        "webhook-filter-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Environment: "development",
		Config:      `{"version":"2.0","nodes":[{"id":"1","type":"git_clone","name":"Clone","config":{"repository":{"url":"https://example.com/repo.git","branch":"main"}}}],"edges":[]}`,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	trigger := models.PipelineTrigger{
		WorkspaceID:       workspace.ID,
		PipelineID:        pipeline.ID,
		Provider:          "gitlab",
		WebhookEnabled:    true,
		PushEnabled:       true,
		SecretToken:       "gitlab-secret",
		WebhookToken:      "public-trigger-token",
		Timezone:          "UTC",
		PushBranchFilters: "release/*",
	}
	if err := db.Create(&trigger).Error; err != nil {
		t.Fatalf("create trigger failed: %v", err)
	}

	payload := mustJSON(t, map[string]interface{}{
		"object_kind": "push",
		"ref":         "refs/heads/main",
		"project": map[string]interface{}{
			"path_with_namespace": "group/project",
		},
		"user_username": "gitlab-user",
		"checkout_sha":  "abc123def456",
	})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/pipeline/run/webhook/public-trigger-token", bytes.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("X-Gitlab-Token", "gitlab-secret")
	c.Params = gin.Params{{Key: "token", Value: "public-trigger-token"}}

	h.HandleGitLabWebhook(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"ignored":true`)) {
		t.Fatalf("expected ignored response, got %s", w.Body.String())
	}
	var count int64
	if err := db.Model(&models.PipelineRun{}).Where("pipeline_id = ?", pipeline.ID).Count(&count).Error; err != nil {
		t.Fatalf("count pipeline runs failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no runs created when filter misses, got %d", count)
	}
}

func TestCancelPipelineRun_CancelsRunningTasks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "cancel-run-user", models.WorkspaceRoleDeveloper)

	pipeline := models.Pipeline{
		Name:        "cancel-test-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Environment: "development",
		Config:      `{"version":"2.0","nodes":[{"id":"1","type":"shell","name":"Build","config":{"script":"echo build"}},{"id":"2","type":"shell","name":"Test","config":{"script":"echo test"}}],"edges":[{"from":"1","to":"2"}]}`,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	run := models.PipelineRun{
		WorkspaceID: workspace.ID,
		PipelineID:  pipeline.ID,
		BuildNumber: 1,
		Status:      models.PipelineRunStatusRunning,
		StartTime:   time.Now().Unix() - 60,
		Config:      pipeline.Config,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}

	tasks := []models.AgentTask{
		{
			WorkspaceID:   workspace.ID,
			PipelineRunID: run.ID,
			NodeID:        "1",
			TaskType:      "shell",
			Name:          "Build",
			Status:        models.TaskStatusExecuteSuccess,
			StartTime:     time.Now().Unix() - 50,
			EndTime:       time.Now().Unix() - 30,
			Duration:      20,
		},
		{
			WorkspaceID:   workspace.ID,
			PipelineRunID: run.ID,
			NodeID:        "2",
			TaskType:      "shell",
			Name:          "Test",
			Status:        models.TaskStatusRunning,
			StartTime:     time.Now().Unix() - 25,
		},
	}
	for i := range tasks {
		if err := db.Create(&tasks[i]).Error; err != nil {
			t.Fatalf("create task failed: %v", err)
		}
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/pipelines/%d/runs/%d/cancel", pipeline.ID, run.ID), nil)
	c.Params = gin.Params{
		{Key: "id", Value: strconv.FormatUint(pipeline.ID, 10)},
		{Key: "run_id", Value: strconv.FormatUint(run.ID, 10)},
	}
	c.Set("user_id", user.ID)
	c.Set("role", "user")
	c.Set("workspace_id", workspace.ID)

	h.CancelPipelineRun(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var updatedRun models.PipelineRun
	if err := db.First(&updatedRun, run.ID).Error; err != nil {
		t.Fatalf("load run failed: %v", err)
	}
	if updatedRun.Status != models.PipelineRunStatusCancelled {
		t.Fatalf("expected run status cancelled, got %s", updatedRun.Status)
	}

	var updatedTasks []models.AgentTask
	if err := db.Where("pipeline_run_id = ?", run.ID).Order("node_id ASC").Find(&updatedTasks).Error; err != nil {
		t.Fatalf("load tasks failed: %v", err)
	}
	if len(updatedTasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(updatedTasks))
	}

	if updatedTasks[0].Status != models.TaskStatusExecuteSuccess {
		t.Fatalf("expected task 1 to remain success, got %s", updatedTasks[0].Status)
	}
	if updatedTasks[1].Status != models.TaskStatusCancelled {
		t.Fatalf("expected task 2 to be cancelled, got %s", updatedTasks[1].Status)
	}
}

func TestCancelPipelineRun_RejectsTerminalRun(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "cancel-terminal-user", models.WorkspaceRoleDeveloper)

	pipeline := models.Pipeline{
		Name:        "cancel-terminal-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Config:      `{"version":"2.0","nodes":[{"id":"1","type":"shell","name":"Build","config":{"script":"echo build"}}],"edges":[]}`,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	run := models.PipelineRun{
		WorkspaceID: workspace.ID,
		PipelineID:  pipeline.ID,
		BuildNumber: 1,
		Status:      models.PipelineRunStatusSuccess,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/pipelines/%d/runs/%d/cancel", pipeline.ID, run.ID), nil)
	c.Params = gin.Params{
		{Key: "id", Value: strconv.FormatUint(pipeline.ID, 10)},
		{Key: "run_id", Value: strconv.FormatUint(run.ID, 10)},
	}
	c.Set("user_id", user.ID)
	c.Set("role", "user")
	c.Set("workspace_id", workspace.ID)

	h.CancelPipelineRun(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("不支持取消操作")) {
		t.Fatalf("expected cancellation rejected message, got %s", w.Body.String())
	}
}
