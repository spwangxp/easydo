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

type pipelineStatisticsTestResponseEnvelope[T any] struct {
	Code int `json:"code"`
	Data T   `json:"data"`
}

type pipelineStatisticsTestResponse struct {
	TotalRuns      int64                            `json:"total_runs"`
	SuccessfulRuns int64                            `json:"successful_runs"`
	FailedRuns     int64                            `json:"failed_runs"`
	SuccessRate    float64                          `json:"success_rate"`
	AvgDuration    float64                          `json:"avg_duration"`
	DailyRuns      []DailyRun                       `json:"daily_runs"`
	Distribution   []pipelineStatisticsDistribution `json:"distribution"`
	RecentFailures []pipelineStatisticsFailure      `json:"recent_failures"`
}

type pipelineStatisticsDistribution struct {
	Status string  `json:"status"`
	Count  int64   `json:"count"`
	Rate   float64 `json:"rate"`
}

type pipelineStatisticsFailure struct {
	RunID       uint64 `json:"run_id"`
	BuildNumber int    `json:"build_number"`
	Status      string `json:"status"`
	ErrorMsg    string `json:"error_msg"`
	CreatedAt   string `json:"created_at"`
}

func performPipelineStatisticsRequest(t *testing.T, handler func(*gin.Context), workspaceID uint64, pipelineID uint64, target string) *httptest.ResponseRecorder {
	t.Helper()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(pipelineID, 10)}}
	c.Set("workspace_id", workspaceID)

	handler(c)

	return w
}

func mustDecodePipelineStatisticsResponse[T any](t *testing.T, recorder *httptest.ResponseRecorder) pipelineStatisticsTestResponseEnvelope[T] {
	t.Helper()

	var response pipelineStatisticsTestResponseEnvelope[T]
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response failed: %v body=%s", err, recorder.Body.String())
	}

	return response
}

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

func TestDefaultResolvedNodeStatus(t *testing.T) {
	tests := []struct {
		name      string
		runStatus string
		want      string
	}{
		{name: "queued run stays queued", runStatus: models.PipelineRunStatusQueued, want: models.PipelineRunStatusQueued},
		{name: "running run defaults pending", runStatus: models.PipelineRunStatusRunning, want: models.PipelineRunStatusPending},
		{name: "cancel requested run defaults pending", runStatus: models.PipelineRunStatusCancelRequested, want: models.PipelineRunStatusPending},
		{name: "unknown run defaults pending", runStatus: "unknown", want: models.PipelineRunStatusPending},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := defaultResolvedNodeStatus(tt.runStatus); got != tt.want {
				t.Fatalf("defaultResolvedNodeStatus(%q)=%q, want %q", tt.runStatus, got, tt.want)
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

func TestParsePipelineConfigJSON_PreservesTopLevelIgnoreFailure(t *testing.T) {
	configJSON := `{
		"version": "2.0",
		"nodes": [
			{
				"node_id": "node_1",
				"task_key": "shell",
				"node_name": "Build",
				"ignore_failure": true,
				"params": {"script": "exit 1"}
			}
		],
		"edges": []
	}`

	config, err := parsePipelineConfigJSON(configJSON)
	if err != nil {
		t.Fatalf("parsePipelineConfigJSON failed: %v", err)
	}
	if len(config.Nodes) != 1 {
		t.Fatalf("nodes=%d, want 1", len(config.Nodes))
	}
	if !config.Nodes[0].IgnoreFailure {
		t.Fatalf("node ignore_failure=%v, want true", config.Nodes[0].IgnoreFailure)
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

func TestBuildHistoricalRunParameterView_SeparatesRuntimeAndDefaultsAndMarksOverrides(t *testing.T) {
	run := models.PipelineRun{
		BaseModel:     models.BaseModel{ID: 42},
		PipelineID:    7,
		BuildNumber:   3,
		TriggerType:   "manual",
		TriggerUser:   "alice",
		TriggerSource: "pipeline_detail",
		RunConfig: `{
			"trigger": {"type": "manual", "source": "pipeline_detail", "operator": "alice"},
			"inputs": {
				"node_1": {
					"script": "echo override",
					"git_ref": "release/2026.05"
				},
				"node_2": {
					"image": "nginx:1.27"
				}
			}
		}`,
		PipelineSnapshot: `{
			"nodes": [
				{
					"node_id": "node_1",
					"node_name": "Build",
					"params": [
						{"key": "script", "label": "脚本", "value": "echo default", "is_flexible": true},
						{"key": "git_ref", "label": "分支", "value": "main", "is_flexible": true},
						{"key": "git_repo_url", "label": "仓库", "value": "https://example.com/repo.git", "is_flexible": false}
					]
				},
				{
					"node_id": "node_2",
					"node_name": "Deploy",
					"params": [
						{"key": "image", "label": "镜像", "value": "nginx:latest", "is_flexible": true}
					]
				}
			]
		}`,
	}

	view := buildHistoricalRunParameterView(run)

	if view.RunID != run.ID {
		t.Fatalf("run_id=%d, want %d", view.RunID, run.ID)
	}
	if view.PipelineID != run.PipelineID {
		t.Fatalf("pipeline_id=%d, want %d", view.PipelineID, run.PipelineID)
	}
	if view.BuildNumber != run.BuildNumber {
		t.Fatalf("build_number=%d, want %d", view.BuildNumber, run.BuildNumber)
	}

	if view.Trigger.Type != "manual" || view.Trigger.Source != "pipeline_detail" || view.Trigger.Operator != "alice" {
		t.Fatalf("unexpected trigger summary: %#v", view.Trigger)
	}

	if len(view.Nodes) != 2 {
		t.Fatalf("nodes len=%d, want 2", len(view.Nodes))
	}

	if view.Nodes[0].NodeID != "node_1" || view.Nodes[0].NodeName != "Build" {
		t.Fatalf("unexpected first node summary: %#v", view.Nodes[0])
	}
	firstRuntime := view.Nodes[0].RuntimeParams
	if len(firstRuntime) != 2 {
		t.Fatalf("runtime_params=%#v, want 2 entries", firstRuntime)
	}
	if firstRuntime[0].Key != "git_ref" || firstRuntime[0].Source != "pipeline_detail" {
		t.Fatalf("unexpected first runtime param: %#v", firstRuntime[0])
	}
	if firstRuntime[1].Key != "script" || firstRuntime[1].Source != "pipeline_detail" {
		t.Fatalf("unexpected second runtime param: %#v", firstRuntime[1])
	}

	firstDefaults := view.Nodes[0].DefaultParams
	if len(firstDefaults) != 3 {
		t.Fatalf("default_params=%#v, want 3 entries", firstDefaults)
	}
	if firstDefaults[0].Key != "git_ref" || firstDefaults[0].Overridden != true {
		t.Fatalf("expected git_ref default overridden, got %#v", firstDefaults[0])
	}
	if firstDefaults[1].Key != "git_repo_url" || firstDefaults[1].Overridden != false {
		t.Fatalf("expected git_repo_url default not overridden, got %#v", firstDefaults[1])
	}
	if firstDefaults[2].Key != "script" || firstDefaults[2].Overridden != true {
		t.Fatalf("expected script default overridden, got %#v", firstDefaults[2])
	}

	secondRuntime := view.Nodes[1].RuntimeParams
	if len(secondRuntime) != 1 {
		t.Fatalf("second runtime_params=%#v, want 1 entry", secondRuntime)
	}
	if secondRuntime[0].Key != "image" || secondRuntime[0].Source != "pipeline_detail" {
		t.Fatalf("unexpected second node runtime param: %#v", secondRuntime[0])
	}
	secondDefaults := view.Nodes[1].DefaultParams
	if len(secondDefaults) != 1 || secondDefaults[0].Overridden != true {
		t.Fatalf("unexpected second node default params: %#v", secondDefaults)
	}
}

func TestBuildHistoricalRunParameterView_FallsBackToTriggerTypeForRuntimeSource(t *testing.T) {
	run := models.PipelineRun{
		BaseModel:   models.BaseModel{ID: 9},
		PipelineID:  3,
		BuildNumber: 8,
		TriggerType: "schedule",
		TriggerUser: "scheduler",
		RunConfig: `{
			"trigger": {"type": "schedule", "operator": "scheduler"},
			"inputs": {"node_1": {"script": "echo scheduled"}}
		}`,
		PipelineSnapshot: `{"nodes":[{"node_id":"node_1","node_name":"Build","params":[{"key":"script","label":"脚本","value":"echo default","is_flexible":true}]}]}`,
	}

	view := buildHistoricalRunParameterView(run)
	runtimeParams := view.Nodes[0].RuntimeParams
	if len(runtimeParams) != 1 || runtimeParams[0].Source != "schedule" {
		t.Fatalf("expected runtime source fallback to trigger type, got %#v", runtimeParams)
	}
	if view.Trigger.Source != "" {
		t.Fatalf("expected empty trigger.source when absent in run data, got %#v", view.Trigger)
	}
}

func TestBuildHistoricalRunParameterView_BestEffortOnMalformedSnapshots(t *testing.T) {
	run := models.PipelineRun{
		BaseModel:        models.BaseModel{ID: 15},
		PipelineID:       11,
		BuildNumber:      5,
		TriggerType:      "webhook",
		TriggerUser:      "gitlab-user",
		TriggerSource:    "gitlab:push",
		RunConfig:        `{"trigger":{"type":"webhook","source":"gitlab:push","operator":"gitlab-user"},"inputs":`,
		PipelineSnapshot: `{"nodes":`,
	}

	view := buildHistoricalRunParameterView(run)
	if view.RunID != run.ID {
		t.Fatalf("run_id=%d, want %d", view.RunID, run.ID)
	}
	if view.Trigger.Type != "webhook" || view.Trigger.Source != "gitlab:push" || view.Trigger.Operator != "gitlab-user" {
		t.Fatalf("unexpected trigger summary: %#v", view.Trigger)
	}
	if len(view.Nodes) != 0 {
		t.Fatalf("expected no nodes for malformed snapshots, got %#v", view.Nodes)
	}
}

func TestBuildHistoricalRunParameterView_ExtractsDefaultsFromLegacySnapshotNodeConfig(t *testing.T) {
	run := models.PipelineRun{
		BaseModel:   models.BaseModel{ID: 19},
		PipelineID:  12,
		BuildNumber: 6,
		TriggerType: "manual",
		TriggerUser: "legacy-user",
		RunConfig:   `{"trigger":{"type":"manual","operator":"legacy-user"},"inputs":{"node_1":{"script":"echo override"}}}`,
		PipelineSnapshot: `{
			"version":"2.0",
			"nodes":[
				{
					"id":"node_1",
					"type":"shell",
					"name":"Legacy Build",
					"config":{"script":"echo default","workdir":"/workspace/app"}
				},
				{
					"id":"node_2",
					"type":"shell",
					"name":"Legacy Test",
					"params":{"command":"go test ./..."}
				}
			]
		}`,
	}

	view := buildHistoricalRunParameterView(run)
	if len(view.Nodes) != 2 {
		t.Fatalf("nodes len=%d, want 2", len(view.Nodes))
	}

	firstDefaults := view.Nodes[0].DefaultParams
	if len(firstDefaults) != 2 {
		t.Fatalf("expected legacy config defaults, got %#v", firstDefaults)
	}
	if firstDefaults[0].Key != "script" || firstDefaults[0].Value != "echo default" || firstDefaults[0].Overridden != true {
		t.Fatalf("unexpected first legacy default param: %#v", firstDefaults[0])
	}
	if firstDefaults[1].Key != "workdir" || firstDefaults[1].Value != "/workspace/app" || firstDefaults[1].Overridden != false {
		t.Fatalf("unexpected second legacy default param: %#v", firstDefaults[1])
	}

	secondDefaults := view.Nodes[1].DefaultParams
	if len(secondDefaults) != 1 {
		t.Fatalf("expected legacy params defaults, got %#v", secondDefaults)
	}
	if secondDefaults[0].Key != "command" || secondDefaults[0].Value != "go test ./..." || secondDefaults[0].Overridden != false {
		t.Fatalf("unexpected legacy params default param: %#v", secondDefaults[0])
	}
}

func TestBuildHistoricalRunParameterView_LegacyDefaultsExcludeCredentialAndInternalKeys(t *testing.T) {
	run := models.PipelineRun{
		BaseModel:   models.BaseModel{ID: 21},
		PipelineID:  13,
		BuildNumber: 7,
		TriggerType: "manual",
		TriggerUser: "legacy-user",
		PipelineSnapshot: `{
			"version":"2.0",
			"nodes":[
				{
					"id":"node_1",
					"type":"git_clone",
					"name":"Legacy Clone",
					"config":{
						"git_repo_url":"https://example.com/repo.git",
						"git_ref":"main",
						"credentials":{"repo_auth":{"credential_id":12}},
						"credentials.repo_auth.credential_id":12,
						"target_resource_id":88,
						"deploy_resource_id":99
					}
				}
			]
		}`,
	}

	view := buildHistoricalRunParameterView(run)
	nodes := view.Nodes
	if len(nodes) != 1 {
		t.Fatalf("nodes len=%d, want 1", len(nodes))
	}
	defaults := nodes[0].DefaultParams
	if len(defaults) != 2 {
		t.Fatalf("expected only visible legacy defaults, got %#v", defaults)
	}
	if defaults[0].Key != "git_ref" || defaults[1].Key != "git_repo_url" {
		t.Fatalf("unexpected filtered legacy defaults: %#v", defaults)
	}
	for _, param := range defaults {
		if strings.HasPrefix(param.Key, "credentials") || strings.HasSuffix(param.Key, "_resource_id") || param.Key == "target_resource_id" {
			t.Fatalf("expected internal key filtered out, got %#v", param)
		}
	}
}

func TestBuildHistoricalRunParameterView_AppendsRuntimeOnlyNodesInStableOrder(t *testing.T) {
	run := models.PipelineRun{
		BaseModel:   models.BaseModel{ID: 23},
		PipelineID:  14,
		BuildNumber: 8,
		TriggerType: "api",
		TriggerUser: "api-user",
		RunConfig: `{
			"trigger": {"type": "api", "operator": "api-user"},
			"inputs": {
				"node_b": {"image": "nginx:1.27"},
				"node_a": {"script": "echo hi"}
			}
		}`,
		PipelineSnapshot: `{
			"nodes": [
				{
					"node_id": "node_0",
					"node_name": "Snapshot Node",
					"params": [
						{"key": "branch", "label": "分支", "value": "main", "is_flexible": true}
					]
				}
			]
		}`,
	}

	view := buildHistoricalRunParameterView(run)
	if len(view.Nodes) != 3 {
		t.Fatalf("nodes len=%d, want 3", len(view.Nodes))
	}
	if view.Nodes[0].NodeID != "node_0" || view.Nodes[1].NodeID != "node_a" || view.Nodes[2].NodeID != "node_b" {
		t.Fatalf("unexpected node order: %#v", view.Nodes)
	}
	if len(view.Nodes[1].DefaultParams) != 0 || len(view.Nodes[2].DefaultParams) != 0 {
		t.Fatalf("expected runtime-only nodes to keep empty default params, got %#v %#v", view.Nodes[1].DefaultParams, view.Nodes[2].DefaultParams)
	}
	if len(view.Nodes[1].RuntimeParams) != 1 || view.Nodes[1].RuntimeParams[0].Key != "script" {
		t.Fatalf("unexpected runtime params for node_a: %#v", view.Nodes[1].RuntimeParams)
	}
	if len(view.Nodes[2].RuntimeParams) != 1 || view.Nodes[2].RuntimeParams[0].Key != "image" {
		t.Fatalf("unexpected runtime params for node_b: %#v", view.Nodes[2].RuntimeParams)
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

func TestGetRunParameterView_ReturnsHistoricalSnapshotView(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "parameter-view-user", models.WorkspaceRoleDeveloper)

	pipeline := models.Pipeline{
		Name:        "parameter-view-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Config:      `{"version":"2.0","nodes":[{"id":"node_1","type":"shell","name":"Build","config":{"script":"echo default"}}],"edges":[]}`,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	run := models.PipelineRun{
		WorkspaceID:      workspace.ID,
		PipelineID:       pipeline.ID,
		BuildNumber:      9,
		Status:           models.PipelineRunStatusSuccess,
		TriggerType:      "manual",
		TriggerSource:    "pipeline_detail",
		TriggerUser:      "alice",
		RunConfig:        `{"trigger":{"type":"manual","source":"pipeline_detail","operator":"alice"},"inputs":{"node_1":{"script":"echo override"}}}`,
		PipelineSnapshot: `{"nodes":[{"node_id":"node_1","node_name":"Build","params":[{"key":"script","label":"脚本","value":"echo default","is_flexible":true}]}]}`,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/pipelines/%d/runs/%d/parameter-view", pipeline.ID, run.ID), nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(pipeline.ID, 10)}, {Key: "run_id", Value: strconv.FormatUint(run.ID, 10)}}
	c.Set("workspace_id", workspace.ID)

	h.GetRunParameterView(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Code int                        `json:"code"`
		Data historicalRunParameterView `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v body=%s", err, w.Body.String())
	}
	if resp.Code != 200 {
		t.Fatalf("code=%d, want 200", resp.Code)
	}
	if resp.Data.RunID != run.ID || resp.Data.PipelineID != pipeline.ID {
		t.Fatalf("unexpected run view ids: %#v", resp.Data)
	}
	if len(resp.Data.Nodes) != 1 || len(resp.Data.Nodes[0].RuntimeParams) != 1 {
		t.Fatalf("unexpected historical parameter view: %#v", resp.Data)
	}
	if resp.Data.Nodes[0].RuntimeParams[0].Value != "echo override" {
		t.Fatalf("runtime param value=%#v, want echo override", resp.Data.Nodes[0].RuntimeParams[0].Value)
	}
}

func TestGetRunRerunPreview(t *testing.T) {
	gin.SetMode(gin.TestMode)

	type previewResponseEnvelope struct {
		Code int                  `json:"code"`
		Data rerunPreviewResponse `json:"data"`
	}

	makeRequest := func(t *testing.T, h *PipelineHandler, workspaceID, pipelineID, runID uint64) previewResponseEnvelope {
		t.Helper()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/pipelines/%d/runs/%d/rerun-preview", pipelineID, runID), nil)
		c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(pipelineID, 10)}, {Key: "run_id", Value: strconv.FormatUint(runID, 10)}}
		c.Set("workspace_id", workspaceID)
		h.GetRunRerunPreview(c)
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
		}
		var resp previewResponseEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response failed: %v body=%s", err, w.Body.String())
		}
		return resp
	}

	makeDefinition := func(params string) string {
		return fmt.Sprintf(`{"version":"2.0","nodes":[{"node_id":"node_1","node_name":"Build","type":"shell","task_key":"shell","params":%s},{"node_id":"node_2","node_name":"Deploy","type":"shell","task_key":"shell","params":[{"key":"image","label":"镜像","value":"nginx:latest","is_flexible":true}]}],"edges":[{"from":"node_1","to":"node_2"}]}`,
			params,
		)
	}

	t.Run("all match response returns can_enter_run_dialog true", func(t *testing.T) {
		db := openHandlerTestDB(t)
		h := &PipelineHandler{DB: db}
		user, workspace := seedCredentialTestUserAndWorkspace(t, db, "rerun-preview-all-match", models.WorkspaceRoleDeveloper)
		pipeline := models.Pipeline{
			Name:        "rerun-preview-all-match",
			WorkspaceID: workspace.ID,
			OwnerID:     user.ID,
			Definition:  makeDefinition(`[{"key":"script","label":"脚本","value":"echo current","is_flexible":true},{"key":"git_ref","label":"分支","value":"main","is_flexible":true}]`),
		}
		if err := db.Create(&pipeline).Error; err != nil {
			t.Fatalf("create pipeline failed: %v", err)
		}
		run := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 1, ResolvedNodes: `[{"node_id":"node_1","resolved_inputs":{"script":"echo historical","git_ref":"release/2026.05"}},{"node_id":"node_2","resolved_inputs":{"image":"nginx:1.27"}}]`}
		if err := db.Create(&run).Error; err != nil {
			t.Fatalf("create run failed: %v", err)
		}

		resp := makeRequest(t, h, workspace.ID, pipeline.ID, run.ID)
		if resp.Data.CanEnterRunDialog != true {
			t.Fatalf("can_enter_run_dialog=%v, want true; resp=%#v", resp.Data.CanEnterRunDialog, resp.Data)
		}
		if resp.Data.MatchKey != "node_id+param_key" {
			t.Fatalf("match_key=%q", resp.Data.MatchKey)
		}
		if len(resp.Data.Matched) != 3 || len(resp.Data.Mismatched) != 0 {
			t.Fatalf("unexpected matches/mismatches: %#v", resp.Data)
		}
		if got := resp.Data.PrefillInputs["node_1"]["script"]; got != "echo historical" {
			t.Fatalf("prefill node_1.script=%#v", got)
		}
		if got := resp.Data.PrefillInputs["node_2"]["image"]; got != "nginx:1.27" {
			t.Fatalf("prefill node_2.image=%#v", got)
		}
		if resp.Data.Failure != nil {
			t.Fatalf("unexpected failure payload: %#v", resp.Data.Failure)
		}
	})

	t.Run("missing node mismatch reason node_not_found", func(t *testing.T) {
		db := openHandlerTestDB(t)
		h := &PipelineHandler{DB: db}
		user, workspace := seedCredentialTestUserAndWorkspace(t, db, "rerun-preview-missing-node", models.WorkspaceRoleDeveloper)
		pipeline := models.Pipeline{Name: "rerun-preview-missing-node", WorkspaceID: workspace.ID, OwnerID: user.ID, Definition: makeDefinition(`[{"key":"script","label":"脚本","value":"echo current","is_flexible":true}]`)}
		if err := db.Create(&pipeline).Error; err != nil {
			t.Fatalf("create pipeline failed: %v", err)
		}
		run := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 1, RunConfig: `{"inputs":{"missing_node":{"script":"echo historical"}}}`, ResolvedNodes: `[{"node_id":"missing_node","resolved_inputs":{"script":"echo historical"}}]`}
		if err := db.Create(&run).Error; err != nil {
			t.Fatalf("create run failed: %v", err)
		}
		resp := makeRequest(t, h, workspace.ID, pipeline.ID, run.ID)
		if resp.Data.CanEnterRunDialog {
			t.Fatalf("expected blocked dialog, got %#v", resp.Data)
		}
		if len(resp.Data.Mismatched) != 1 || resp.Data.Mismatched[0].Reason != "node_not_found" {
			t.Fatalf("unexpected mismatches: %#v", resp.Data.Mismatched)
		}
	})

	t.Run("missing param mismatch reason param_not_found", func(t *testing.T) {
		db := openHandlerTestDB(t)
		h := &PipelineHandler{DB: db}
		user, workspace := seedCredentialTestUserAndWorkspace(t, db, "rerun-preview-missing-param", models.WorkspaceRoleDeveloper)
		pipeline := models.Pipeline{Name: "rerun-preview-missing-param", WorkspaceID: workspace.ID, OwnerID: user.ID, Definition: makeDefinition(`[{"key":"script","label":"脚本","value":"echo current","is_flexible":true}]`)}
		if err := db.Create(&pipeline).Error; err != nil {
			t.Fatalf("create pipeline failed: %v", err)
		}
		run := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 1, RunConfig: `{"inputs":{"node_1":{"git_ref":"release/2026.05"}}}`, ResolvedNodes: `[{"node_id":"node_1","resolved_inputs":{"git_ref":"release/2026.05"}}]`}
		if err := db.Create(&run).Error; err != nil {
			t.Fatalf("create run failed: %v", err)
		}
		resp := makeRequest(t, h, workspace.ID, pipeline.ID, run.ID)
		if len(resp.Data.Mismatched) != 1 || resp.Data.Mismatched[0].Reason != "param_not_found" {
			t.Fatalf("unexpected mismatches: %#v", resp.Data.Mismatched)
		}
	})

	t.Run("params present historically but not eligible in current manual run set returns not_manual_run_param", func(t *testing.T) {
		db := openHandlerTestDB(t)
		h := &PipelineHandler{DB: db}
		user, workspace := seedCredentialTestUserAndWorkspace(t, db, "rerun-preview-not-manual", models.WorkspaceRoleDeveloper)
		pipeline := models.Pipeline{Name: "rerun-preview-not-manual", WorkspaceID: workspace.ID, OwnerID: user.ID, Definition: makeDefinition(`[{"key":"script","label":"脚本","value":"echo current","is_flexible":false}]`)}
		if err := db.Create(&pipeline).Error; err != nil {
			t.Fatalf("create pipeline failed: %v", err)
		}
		run := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 1, RunConfig: `{"inputs":{"node_1":{"script":"echo historical"}}}`, ResolvedNodes: `[{"node_id":"node_1","resolved_inputs":{"script":"echo historical"}}]`}
		if err := db.Create(&run).Error; err != nil {
			t.Fatalf("create run failed: %v", err)
		}
		resp := makeRequest(t, h, workspace.ID, pipeline.ID, run.ID)
		if len(resp.Data.Mismatched) != 1 || resp.Data.Mismatched[0].Reason != "not_manual_run_param" {
			t.Fatalf("unexpected mismatches: %#v", resp.Data.Mismatched)
		}
	})

	t.Run("run_config manual inputs take precedence and resolved non flexible defaults do not block rerun", func(t *testing.T) {
		db := openHandlerTestDB(t)
		h := &PipelineHandler{DB: db}
		user, workspace := seedCredentialTestUserAndWorkspace(t, db, "rerun-preview-run-config-priority", models.WorkspaceRoleDeveloper)
		pipeline := models.Pipeline{
			Name:        "rerun-preview-run-config-priority",
			WorkspaceID: workspace.ID,
			OwnerID:     user.ID,
			Definition:  `{"version":"2.0","nodes":[{"node_id":"node_1","node_name":"Build","type":"shell","task_key":"shell","params":[{"key":"script","label":"脚本","value":"echo current","is_flexible":false},{"key":"image_tag","label":"镜像标签","value":"latest","is_flexible":true},{"key":"architectures","label":"目标架构","value":["linux/amd64","linux/arm64"],"is_flexible":true}]},{"node_id":"node_2","node_name":"Deploy","type":"shell","task_key":"shell","params":[{"key":"script","label":"部署脚本","value":"echo deploy","is_flexible":false}]}],"edges":[{"from":"node_1","to":"node_2"}]}`,
		}
		if err := db.Create(&pipeline).Error; err != nil {
			t.Fatalf("create pipeline failed: %v", err)
		}
		run := models.PipelineRun{
			WorkspaceID: workspace.ID,
			PipelineID:  pipeline.ID,
			BuildNumber: 1,
			RunConfig:   `{"inputs":{"node_1":{"image_tag":"release-2026.05","architectures":["linux/amd64"]}}}`,
			ResolvedNodes: `[
					{"node_id":"node_1","resolved_inputs":{"script":"echo historical default","image_tag":"release-2026.05","architectures":["linux/amd64"],"context":"./app","push":true}},
					{"node_id":"node_2","resolved_inputs":{"script":"echo deploy","region":"cn"}}
				]`,
		}
		if err := db.Create(&run).Error; err != nil {
			t.Fatalf("create run failed: %v", err)
		}

		resp := makeRequest(t, h, workspace.ID, pipeline.ID, run.ID)
		if !resp.Data.CanEnterRunDialog {
			t.Fatalf("expected rerun dialog available, got %#v", resp.Data)
		}
		if resp.Data.Failure != nil {
			t.Fatalf("unexpected failure: %#v", resp.Data.Failure)
		}
		if len(resp.Data.Mismatched) != 0 {
			t.Fatalf("expected no mismatches, got %#v", resp.Data.Mismatched)
		}
		if got := resp.Data.PrefillInputs["node_1"]["image_tag"]; got != "release-2026.05" {
			t.Fatalf("prefill node_1.image_tag=%#v", got)
		}
		architectures, ok := resp.Data.PrefillInputs["node_1"]["architectures"].([]interface{})
		if !ok || len(architectures) != 1 || architectures[0] != "linux/amd64" {
			t.Fatalf("prefill node_1.architectures=%#v", resp.Data.PrefillInputs["node_1"]["architectures"])
		}
		if _, exists := resp.Data.PrefillInputs["node_1"]["script"]; exists {
			t.Fatalf("unexpected non-flexible script prefill: %#v", resp.Data.PrefillInputs["node_1"])
		}
		if _, exists := resp.Data.PrefillInputs["node_2"]; exists {
			t.Fatalf("unexpected node_2 prefill: %#v", resp.Data.PrefillInputs["node_2"])
		}
	})

	t.Run("malformed or missing resolved nodes returns explicit preview failure and no runnable prefill payload", func(t *testing.T) {
		for _, raw := range []string{"", `{"bad":true}`, `[{"node_id":"node_1"}]`, `[{"node_id":"node_1","resolved_inputs":{"script":"echo one"}},{"node_id":"node_1","resolved_inputs":{"script":"echo two"}}]`} {
			db := openHandlerTestDB(t)
			h := &PipelineHandler{DB: db}
			user, workspace := seedCredentialTestUserAndWorkspace(t, db, "rerun-preview-bad-resolved-"+strconv.Itoa(len(raw)), models.WorkspaceRoleDeveloper)
			pipeline := models.Pipeline{Name: "rerun-preview-bad-resolved", WorkspaceID: workspace.ID, OwnerID: user.ID, Definition: makeDefinition(`[{"key":"script","label":"脚本","value":"echo current","is_flexible":true}]`)}
			if err := db.Create(&pipeline).Error; err != nil {
				t.Fatalf("create pipeline failed: %v", err)
			}
			run := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 1, ResolvedNodes: raw}
			if err := db.Create(&run).Error; err != nil {
				t.Fatalf("create run failed: %v", err)
			}
			resp := makeRequest(t, h, workspace.ID, pipeline.ID, run.ID)
			if resp.Data.CanEnterRunDialog {
				t.Fatalf("expected blocked dialog for raw=%q, got %#v", raw, resp.Data)
			}
			if resp.Data.Failure == nil || resp.Data.Failure.Code != "historical_resolved_inputs_unavailable" {
				t.Fatalf("unexpected failure for raw=%q: %#v", raw, resp.Data.Failure)
			}
			if len(resp.Data.PrefillInputs) != 0 || len(resp.Data.Matched) != 0 {
				t.Fatalf("expected no runnable payload for raw=%q, got %#v", raw, resp.Data)
			}
		}
	})

	t.Run("empty resolved inputs returns explicit preview failure and blocked run dialog", func(t *testing.T) {
		db := openHandlerTestDB(t)
		h := &PipelineHandler{DB: db}
		user, workspace := seedCredentialTestUserAndWorkspace(t, db, "rerun-preview-empty-resolved-inputs", models.WorkspaceRoleDeveloper)
		pipeline := models.Pipeline{Name: "rerun-preview-empty-resolved-inputs", WorkspaceID: workspace.ID, OwnerID: user.ID, Definition: makeDefinition(`[{"key":"script","label":"脚本","value":"echo current","is_flexible":true}]`)}
		if err := db.Create(&pipeline).Error; err != nil {
			t.Fatalf("create pipeline failed: %v", err)
		}
		run := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 1, ResolvedNodes: `[{"node_id":"node_1","resolved_inputs":{}}]`}
		if err := db.Create(&run).Error; err != nil {
			t.Fatalf("create run failed: %v", err)
		}
		resp := makeRequest(t, h, workspace.ID, pipeline.ID, run.ID)
		if resp.Data.CanEnterRunDialog {
			t.Fatalf("expected blocked dialog, got %#v", resp.Data)
		}
		if resp.Data.Failure == nil || resp.Data.Failure.Code != "historical_resolved_inputs_empty" {
			t.Fatalf("unexpected failure: %#v", resp.Data.Failure)
		}
		if len(resp.Data.PrefillInputs) != 0 || len(resp.Data.Matched) != 0 || len(resp.Data.Mismatched) != 0 {
			t.Fatalf("expected empty preview payload, got %#v", resp.Data)
		}
	})

	t.Run("missing current pipeline definition returns explicit preview failure and blocked run dialog", func(t *testing.T) {
		db := openHandlerTestDB(t)
		h := &PipelineHandler{DB: db}
		user, workspace := seedCredentialTestUserAndWorkspace(t, db, "rerun-preview-no-definition", models.WorkspaceRoleDeveloper)
		pipeline := models.Pipeline{Name: "rerun-preview-no-definition", WorkspaceID: workspace.ID, OwnerID: user.ID}
		if err := db.Create(&pipeline).Error; err != nil {
			t.Fatalf("create pipeline failed: %v", err)
		}
		run := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 1, ResolvedNodes: `[{"node_id":"node_1","resolved_inputs":{"script":"echo historical"}}]`}
		if err := db.Create(&run).Error; err != nil {
			t.Fatalf("create run failed: %v", err)
		}
		resp := makeRequest(t, h, workspace.ID, pipeline.ID, run.ID)
		if resp.Data.CanEnterRunDialog {
			t.Fatalf("expected blocked dialog, got %#v", resp.Data)
		}
		if resp.Data.Failure == nil || resp.Data.Failure.Code != "current_pipeline_definition_unavailable" {
			t.Fatalf("unexpected failure: %#v", resp.Data.Failure)
		}
	})

	t.Run("missing current manual run definition returns explicit preview failure and blocked run dialog", func(t *testing.T) {
		db := openHandlerTestDB(t)
		h := &PipelineHandler{DB: db}
		user, workspace := seedCredentialTestUserAndWorkspace(t, db, "rerun-preview-no-manual-definition", models.WorkspaceRoleDeveloper)
		pipeline := models.Pipeline{Name: "rerun-preview-no-manual-definition", WorkspaceID: workspace.ID, OwnerID: user.ID, Definition: `{"version":"2.0","nodes":[{"node_id":"node_1","node_name":"Build","type":"shell","task_key":"shell","params":[{"key":"script","label":"脚本","value":"echo current","is_flexible":false}]}],"edges":[]}`}
		if err := db.Create(&pipeline).Error; err != nil {
			t.Fatalf("create pipeline failed: %v", err)
		}
		run := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 1, ResolvedNodes: `[{"node_id":"node_1","resolved_inputs":{"script":"echo historical"}}]`}
		if err := db.Create(&run).Error; err != nil {
			t.Fatalf("create run failed: %v", err)
		}
		resp := makeRequest(t, h, workspace.ID, pipeline.ID, run.ID)
		if resp.Data.CanEnterRunDialog {
			t.Fatalf("expected blocked dialog, got %#v", resp.Data)
		}
		if resp.Data.Failure == nil || resp.Data.Failure.Code != "current_manual_run_definition_unavailable" {
			t.Fatalf("unexpected failure: %#v", resp.Data.Failure)
		}
	})
}

func TestGetRunTasks_UsesUpstreamNodeIgnoreFailureForBlockedStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name                  string
		upstreamIgnoreFailure bool
		expectedDisplayStatus string
	}{
		{
			name:                  "failed upstream without ignore failure blocks downstream",
			upstreamIgnoreFailure: false,
			expectedDisplayStatus: "blocked",
		},
		{
			name:                  "failed upstream with ignore failure leaves downstream pending",
			upstreamIgnoreFailure: true,
			expectedDisplayStatus: "not_executed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openHandlerTestDB(t)
			h := &PipelineHandler{DB: db}
			user, workspace := seedCredentialTestUserAndWorkspace(t, db, "run-tasks-user-"+strings.ReplaceAll(tt.name, " ", "-"), models.WorkspaceRoleDeveloper)
			pipelineConfig := fmt.Sprintf(`{"version":"2.0","nodes":[{"id":"build","type":"shell","name":"Build","ignore_failure":%t,"config":{"script":"exit 1"}},{"id":"deploy","type":"shell","name":"Deploy","config":{"script":"echo deploy"}}],"edges":[{"from":"build","to":"deploy"}]}`,
				tt.upstreamIgnoreFailure,
			)
			pipeline := models.Pipeline{
				Name:        "run-tasks-pipeline",
				WorkspaceID: workspace.ID,
				OwnerID:     user.ID,
				Config:      pipelineConfig,
			}
			if err := db.Create(&pipeline).Error; err != nil {
				t.Fatalf("create pipeline failed: %v", err)
			}

			run := models.PipelineRun{
				WorkspaceID:      workspace.ID,
				PipelineID:       pipeline.ID,
				BuildNumber:      1,
				Status:           models.PipelineRunStatusFailed,
				TriggerType:      "manual",
				Config:           pipeline.Config,
				PipelineSnapshot: pipeline.Config,
			}
			if err := db.Create(&run).Error; err != nil {
				t.Fatalf("create run failed: %v", err)
			}

			failedTask := models.AgentTask{
				WorkspaceID:   workspace.ID,
				AgentID:       1,
				PipelineRunID: run.ID,
				NodeID:        "build",
				TaskType:      "shell",
				Name:          "Build",
				Status:        models.TaskStatusExecuteFailed,
				ExitCode:      7,
				Duration:      12,
				ErrorMsg:      "build failed",
			}
			if err := db.Create(&failedTask).Error; err != nil {
				t.Fatalf("create failed task failed: %v", err)
			}

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/pipelines/1/runs/1/tasks", nil)
			c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(pipeline.ID, 10)}, {Key: "run_id", Value: strconv.FormatUint(run.ID, 10)}}
			c.Set("workspace_id", workspace.ID)

			h.GetRunTasks(c)

			if w.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
			}

			var resp struct {
				Code int `json:"code"`
				Data struct {
					List []struct {
						NodeID        string `json:"node_id"`
						Status        string `json:"status"`
						DisplayStatus string `json:"display_status"`
						ExitCode      int    `json:"exit_code"`
						Duration      int    `json:"duration"`
					} `json:"list"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal response failed: %v body=%s", err, w.Body.String())
			}

			var buildTask *struct {
				NodeID        string `json:"node_id"`
				Status        string `json:"status"`
				DisplayStatus string `json:"display_status"`
				ExitCode      int    `json:"exit_code"`
				Duration      int    `json:"duration"`
			}
			var deployTask *struct {
				NodeID        string `json:"node_id"`
				Status        string `json:"status"`
				DisplayStatus string `json:"display_status"`
				ExitCode      int    `json:"exit_code"`
				Duration      int    `json:"duration"`
			}
			for i := range resp.Data.List {
				task := &resp.Data.List[i]
				switch task.NodeID {
				case "build":
					buildTask = task
				case "deploy":
					deployTask = task
				}
			}

			if buildTask == nil || deployTask == nil {
				t.Fatalf("expected build and deploy tasks in response, got %#v", resp.Data.List)
			}
			if buildTask.DisplayStatus != models.TaskStatusExecuteFailed {
				t.Fatalf("build display_status=%s, want %s", buildTask.DisplayStatus, models.TaskStatusExecuteFailed)
			}
			if buildTask.ExitCode != 7 || buildTask.Duration != 12 {
				t.Fatalf("build exit_code/duration=(%d,%d), want (7,12)", buildTask.ExitCode, buildTask.Duration)
			}
			if deployTask.DisplayStatus != tt.expectedDisplayStatus {
				t.Fatalf("deploy display_status=%s, want %s", deployTask.DisplayStatus, tt.expectedDisplayStatus)
			}
		})
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
		"definition_json":"{\"version\":\"2.0\",\"nodes\":[{\"node_id\":\"node_1\",\"node_name\":\"Build\",\"type\":\"shell\",\"task_key\":\"shell\",\"task_version\":1,\"timeout\":300,\"params\":[{\"key\":\"script\",\"label\":\"脚本\",\"value\":\"echo definition\",\"is_flexible\":true}],\"credential_bindings\":{},\"resource_bindings\":{},\"metadata\":{\"x\":120,\"y\":220}}],\"edges\":[],\"triggers\":[],\"metadata\":{\"version\":\"2.0\"}}"
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

func TestGetPipelineStatistics_RejectsInvalidDateRangeContracts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "pipeline-stats-range-contract-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{Name: "stats-contract-pipeline", WorkspaceID: workspace.ID, OwnerID: user.ID, Config: `{"version":"2.0","nodes":[{"id":"1","type":"shell","name":"Build","config":{"script":"echo hi"}}],"edges":[]}`}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	testCases := []struct {
		name   string
		target string
	}{
		{name: "missing end date", target: "/api/pipelines/1/statistics?start_date=2026-03-01"},
		{name: "invalid start date", target: "/api/pipelines/1/statistics?start_date=bad&end_date=2026-03-01"},
		{name: "reversed range", target: "/api/pipelines/1/statistics?start_date=2026-03-02&end_date=2026-03-01"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			w := performPipelineStatisticsRequest(t, h.GetPipelineStatistics, workspace.ID, pipeline.ID, tc.target)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestGetPipelineStatistics_UsesInclusiveRequestedDateRangeWithTrendDistributionAndRecentFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "pipeline-stats-range-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{Name: "stats-range-pipeline", WorkspaceID: workspace.ID, OwnerID: user.ID, Config: `{"version":"2.0","nodes":[{"id":"1","type":"shell","name":"Build","config":{"script":"echo hi"}}],"edges":[]}`}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	runs := []struct {
		buildNumber int
		status      string
		duration    int
		errorMsg    string
		createdAt   time.Time
	}{
		{buildNumber: 1, status: models.PipelineRunStatusSuccess, duration: 90, createdAt: time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)},
		{buildNumber: 2, status: models.PipelineRunStatusFailed, duration: 45, errorMsg: "npm install failed", createdAt: time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)},
		{buildNumber: 3, status: models.PipelineRunStatusCancelled, duration: 30, createdAt: time.Date(2026, 3, 3, 23, 59, 59, 0, time.UTC)},
		{buildNumber: 4, status: models.PipelineRunStatusFailed, duration: 60, errorMsg: "tests failed", createdAt: time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)},
	}
	for _, run := range runs {
		pipelineRun := models.PipelineRun{
			WorkspaceID: workspace.ID,
			PipelineID:  pipeline.ID,
			BuildNumber: run.buildNumber,
			Status:      run.status,
			TriggerType: "manual",
			Duration:    run.duration,
			ErrorMsg:    run.errorMsg,
		}
		if err := db.Create(&pipelineRun).Error; err != nil {
			t.Fatalf("create run failed: %v", err)
		}
		if err := db.Model(&pipelineRun).Update("created_at", run.createdAt).Error; err != nil {
			t.Fatalf("update run created_at failed: %v", err)
		}
	}

	w := performPipelineStatisticsRequest(t, h.GetPipelineStatistics, workspace.ID, pipeline.ID, "/api/pipelines/1/statistics?start_date=2026-03-02&end_date=2026-03-03")
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	response := mustDecodePipelineStatisticsResponse[pipelineStatisticsTestResponse](t, w)
	if response.Data.TotalRuns != 2 {
		t.Fatalf("expected 2 runs in inclusive range, got %d body=%s", response.Data.TotalRuns, w.Body.String())
	}
	if response.Data.SuccessfulRuns != 0 {
		t.Fatalf("expected 0 successful runs in range, got %d body=%s", response.Data.SuccessfulRuns, w.Body.String())
	}
	if response.Data.FailedRuns != 1 {
		t.Fatalf("expected 1 failed run in range, got %d body=%s", response.Data.FailedRuns, w.Body.String())
	}
	if response.Data.SuccessRate != 0 {
		t.Fatalf("expected 0 success rate, got %v body=%s", response.Data.SuccessRate, w.Body.String())
	}
	if len(response.Data.DailyRuns) != 2 {
		t.Fatalf("expected 2 daily trend points, got %d body=%s", len(response.Data.DailyRuns), w.Body.String())
	}
	if response.Data.DailyRuns[0].Date != "2026-03-02" || response.Data.DailyRuns[0].Failed != 1 || response.Data.DailyRuns[0].Total != 1 {
		t.Fatalf("unexpected first trend point: %+v body=%s", response.Data.DailyRuns[0], w.Body.String())
	}
	if response.Data.DailyRuns[1].Date != "2026-03-03" || response.Data.DailyRuns[1].Total != 1 {
		t.Fatalf("unexpected second trend point: %+v body=%s", response.Data.DailyRuns[1], w.Body.String())
	}
	if len(response.Data.Distribution) == 0 {
		t.Fatalf("expected distribution buckets, got none body=%s", w.Body.String())
	}
	distributionByStatus := make(map[string]int64, len(response.Data.Distribution))
	for _, bucket := range response.Data.Distribution {
		distributionByStatus[bucket.Status] = bucket.Count
	}
	if distributionByStatus[models.PipelineRunStatusFailed] != 1 {
		t.Fatalf("expected failed distribution count=1, got %d body=%s", distributionByStatus[models.PipelineRunStatusFailed], w.Body.String())
	}
	if distributionByStatus[models.PipelineRunStatusCancelled] != 1 {
		t.Fatalf("expected cancelled distribution count=1, got %d body=%s", distributionByStatus[models.PipelineRunStatusCancelled], w.Body.String())
	}
	if len(response.Data.RecentFailures) != 1 {
		t.Fatalf("expected 1 recent failure in range, got %d body=%s", len(response.Data.RecentFailures), w.Body.String())
	}
	if response.Data.RecentFailures[0].BuildNumber != 2 || response.Data.RecentFailures[0].ErrorMsg != "npm install failed" {
		t.Fatalf("unexpected recent failure payload: %+v body=%s", response.Data.RecentFailures[0], w.Body.String())
	}
}

func TestGetPipelineStatistics_RecentFailuresRemainNewestFirstAndExcludeDeploymentRequestRuns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "pipeline-stats-failure-order-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{Name: "stats-failure-order-pipeline", WorkspaceID: workspace.ID, OwnerID: user.ID, Config: `{"version":"2.0","nodes":[{"id":"1","type":"shell","name":"Build","config":{"script":"echo hi"}}],"edges":[]}`}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	runs := []struct {
		buildNumber int
		status      string
		triggerType string
		errorMsg    string
		createdAt   time.Time
	}{
		{buildNumber: 10, status: models.PipelineRunStatusFailed, triggerType: "manual", errorMsg: "older failure", createdAt: time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)},
		{buildNumber: 11, status: models.PipelineRunStatusFailed, triggerType: "deployment_request", errorMsg: "deployment failure", createdAt: time.Date(2026, 3, 3, 10, 0, 0, 0, time.UTC)},
		{buildNumber: 12, status: models.PipelineRunStatusFailed, triggerType: "manual", errorMsg: "newest failure", createdAt: time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)},
	}
	for _, run := range runs {
		pipelineRun := models.PipelineRun{
			WorkspaceID: workspace.ID,
			PipelineID:  pipeline.ID,
			BuildNumber: run.buildNumber,
			Status:      run.status,
			TriggerType: run.triggerType,
			Duration:    60,
			ErrorMsg:    run.errorMsg,
		}
		if err := db.Create(&pipelineRun).Error; err != nil {
			t.Fatalf("create run failed: %v", err)
		}
		if err := db.Model(&pipelineRun).Update("created_at", run.createdAt).Error; err != nil {
			t.Fatalf("update run created_at failed: %v", err)
		}
	}

	w := performPipelineStatisticsRequest(t, h.GetPipelineStatistics, workspace.ID, pipeline.ID, "/api/pipelines/1/statistics?start_date=2026-03-01&end_date=2026-03-05")
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	response := mustDecodePipelineStatisticsResponse[pipelineStatisticsTestResponse](t, w)
	if len(response.Data.RecentFailures) != 2 {
		t.Fatalf("expected 2 recent manual failures, got %d body=%s", len(response.Data.RecentFailures), w.Body.String())
	}
	if response.Data.RecentFailures[0].BuildNumber != 12 || response.Data.RecentFailures[1].BuildNumber != 10 {
		t.Fatalf("expected failures newest-first, got %+v body=%s", response.Data.RecentFailures, w.Body.String())
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

func TestUpdatePipelineTriggers_PersistsWebhookRuntimeMappings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "trigger-webhook-mapping-user", models.WorkspaceRoleDeveloper)
	definitionJSON := string(mustJSON(t, PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{{
			ID:      "node-1",
			TaskKey: "shell",
			Type:    "shell",
			Name:    "Build",
			DefinitionParams: []models.PipelineDefinitionParam{{
				Key:        "script",
				Label:      "脚本",
				Value:      "echo default",
				IsFlexible: true,
			}},
		}},
		Edges: []PipelineEdge{},
	}))
	pipeline := models.Pipeline{
		Name:        "trigger-webhook-mapping-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Environment: "development",
		Definition:  definitionJSON,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	mappingsJSON := `[{"id":"rule-1","source_type":"jsonpath","source_expr":"$.ref","target":{"node_id":"node-1","param_key":"script"},"missing_policy":"ignore"}]`
	body := bytes.NewBuffer(mustJSON(t, map[string]interface{}{
		"provider":                       "gitlab",
		"webhook_enabled":                true,
		"push_enabled":                   true,
		"timezone":                       "UTC",
		"webhook_runtime_input_mappings": mappingsJSON,
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

	var row struct {
		WebhookRuntimeInputMappings string
		WebhookConfigStatus         string
		WebhookConfigInvalidReason  string
	}
	if err := db.Raw(
		"SELECT webhook_runtime_input_mappings, webhook_config_status, webhook_config_invalid_reason FROM pipeline_triggers WHERE pipeline_id = ?",
		pipeline.ID,
	).Scan(&row).Error; err != nil {
		t.Fatalf("load trigger persistence fields failed: %v", err)
	}
	if row.WebhookRuntimeInputMappings != mappingsJSON {
		t.Fatalf("expected webhook runtime mappings to persist, got %q", row.WebhookRuntimeInputMappings)
	}
	if row.WebhookConfigStatus != "valid" {
		t.Fatalf("expected webhook config status to be backend-valid, got %q", row.WebhookConfigStatus)
	}
	if row.WebhookConfigInvalidReason != "" {
		t.Fatalf("expected empty webhook config invalid reason, got %q", row.WebhookConfigInvalidReason)
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

type pipelineMappingStructuredError struct {
	MappingID string `json:"mapping_id"`
	Field     string `json:"field"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}

type pipelineWebhookPreviewResponseEnvelope struct {
	Code    int                              `json:"code"`
	Message string                           `json:"message"`
	Data    webhookRuntimePreviewResult      `json:"data"`
	Errors  []pipelineMappingStructuredError `json:"errors"`
}

type pipelineMappingErrorResponseEnvelope struct {
	Code    int                              `json:"code"`
	Message string                           `json:"message"`
	Errors  []pipelineMappingStructuredError `json:"errors"`
}

func webhookRuntimeTestDefinitionJSON(t *testing.T, nodes ...PipelineNode) string {
	t.Helper()
	return string(mustJSON(t, PipelineConfig{
		Version: "2.0",
		Nodes:   nodes,
		Edges:   []PipelineEdge{},
	}))
}

func webhookRuntimeTestShellNode(nodeID, script string, flexible bool) PipelineNode {
	return PipelineNode{
		ID:          nodeID,
		TaskKey:     "shell",
		Type:        "shell",
		Name:        nodeID,
		TaskVersion: 1,
		Timeout:     300,
		DefinitionParams: []models.PipelineDefinitionParam{
			{Key: "working_dir", Label: "工作目录", Value: ".", IsFlexible: flexible},
			{Key: "shell", Label: "执行 Shell", Value: taskShellSH, IsFlexible: false},
			{Key: "script", Label: "脚本", Value: script, IsFlexible: false},
		},
	}
}

func webhookRuntimeTestDockerRunNode(nodeID string, port interface{}, flexible bool) PipelineNode {
	return PipelineNode{
		ID:          nodeID,
		TaskKey:     "docker-run",
		Type:        "docker-run",
		Name:        nodeID,
		TaskVersion: 1,
		Timeout:     300,
		DefinitionParams: []models.PipelineDefinitionParam{
			{Key: "host", Label: "主机", Value: "example.com", IsFlexible: true},
			{Key: "port", Label: "端口", Value: port, IsFlexible: flexible},
			{Key: "runtime", Label: "运行方式", Value: "docker", IsFlexible: true},
		},
	}
}

func decodePipelineWebhookPreviewResponse(t *testing.T, body []byte) pipelineWebhookPreviewResponseEnvelope {
	t.Helper()
	var resp pipelineWebhookPreviewResponseEnvelope
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode preview response failed: %v body=%s", err, string(body))
	}
	return resp
}

func decodePipelineMappingErrorResponse(t *testing.T, body []byte) pipelineMappingErrorResponseEnvelope {
	t.Helper()
	var resp pipelineMappingErrorResponseEnvelope
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode error response failed: %v body=%s", err, string(body))
	}
	return resp
}

func TestPreviewWebhookRuntimeMappings_ReturnsMatchedInputs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "preview-runtime-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{
		Name:        "preview-runtime-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Environment: "development",
		Definition:  webhookRuntimeTestDefinitionJSON(t, webhookRuntimeTestShellNode("node-1", "echo default", true)),
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	body := bytes.NewBuffer(mustJSON(t, map[string]interface{}{
		"payload":                        map[string]interface{}{"ref": "refs/heads/main"},
		"webhook_runtime_input_mappings": `[{"id":"rule-1","source_type":"jsonpath","source_expr":"$.ref","target":{"node_id":"node-1","param_key":"working_dir"},"missing_policy":"ignore"}]`,
	}))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/pipelines/1/triggers/webhook/preview", body)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(pipeline.ID, 10)}}
	c.Set("user_id", user.ID)
	c.Set("role", "user")
	c.Set("workspace_id", workspace.ID)

	h.PreviewWebhookRuntimeMappings(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	resp := decodePipelineWebhookPreviewResponse(t, w.Body.Bytes())
	if resp.Code != http.StatusOK {
		t.Fatalf("expected business code 200, got %#v", resp)
	}
	if resp.Data.RuleResults["rule-1"].Code != webhookRuntimeMappingStatusMatched {
		t.Fatalf("expected matched rule status, got %#v", resp.Data.RuleResults)
	}
	if got := resp.Data.Values["node-1"]["working_dir"]; got != "refs/heads/main" {
		t.Fatalf("expected mapped preview value, got %#v", resp.Data.Values)
	}
}

func TestPreviewWebhookRuntimeMappings_ReturnsMultipleValuesAndTypeErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "preview-runtime-errors-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{
		Name:        "preview-runtime-errors-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Environment: "development",
		Definition: webhookRuntimeTestDefinitionJSON(t,
			webhookRuntimeTestShellNode("node-1", "echo default", true),
			webhookRuntimeTestDockerRunNode("node-2", 22, true),
		),
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	body := bytes.NewBuffer(mustJSON(t, map[string]interface{}{
		"payload": map[string]interface{}{
			"refs": []interface{}{"main", "release"},
			"port": "not-a-number",
		},
		"webhook_runtime_input_mappings": `[
			{"id":"multiple","source_type":"jsonpath","source_expr":"$.refs[*]","target":{"node_id":"node-1","param_key":"working_dir"},"missing_policy":"ignore"},
			{"id":"type_mismatch","source_type":"jsonpath","source_expr":"$.port","target":{"node_id":"node-2","param_key":"port"},"missing_policy":"ignore"}
		]`,
	}))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/pipelines/1/triggers/webhook/preview", body)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(pipeline.ID, 10)}}
	c.Set("user_id", user.ID)
	c.Set("role", "user")
	c.Set("workspace_id", workspace.ID)

	h.PreviewWebhookRuntimeMappings(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	resp := decodePipelineWebhookPreviewResponse(t, w.Body.Bytes())
	if resp.Data.RuleResults["multiple"].Code != webhookRuntimeMappingStatusMultipleValues {
		t.Fatalf("expected multiple_values status, got %#v", resp.Data.RuleResults)
	}
	if resp.Data.RuleResults["type_mismatch"].Code != webhookRuntimeMappingStatusTypeMismatch {
		t.Fatalf("expected type_mismatch status, got %#v", resp.Data.RuleResults)
	}
}

func TestPreviewWebhookRuntimeMappings_ReturnsStructuredRowStatusesAndSharedErrorFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "preview-runtime-structured-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{
		Name:        "preview-runtime-structured-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Environment: "development",
		Definition:  webhookRuntimeTestDefinitionJSON(t, webhookRuntimeTestShellNode("node-1", "echo default", true)),
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	body := bytes.NewBuffer(mustJSON(t, map[string]interface{}{
		"payload": map[string]interface{}{"ref": "main"},
		"webhook_runtime_input_mappings": `[
			{"id":"matched","source_type":"jsonpath","source_expr":"$.ref","target":{"node_id":"node-1","param_key":"working_dir"},"missing_policy":"ignore"},
			{"id":"missing_fail","source_type":"jsonpath","source_expr":"$.missing","target":{"node_id":"node-1","param_key":"working_dir"},"missing_policy":"fail"}
		]`,
	}))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/pipelines/1/triggers/webhook/preview", body)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(pipeline.ID, 10)}}
	c.Set("user_id", user.ID)
	c.Set("role", "user")
	c.Set("workspace_id", workspace.ID)

	h.PreviewWebhookRuntimeMappings(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	resp := decodePipelineWebhookPreviewResponse(t, w.Body.Bytes())
	if resp.Data.RuleResults["matched"].Code != webhookRuntimeMappingStatusMatched {
		t.Fatalf("expected matched status, got %#v", resp.Data.RuleResults)
	}
	if resp.Data.RuleResults["missing_fail"].Code != webhookRuntimeMappingStatusMissing {
		t.Fatalf("expected missing status, got %#v", resp.Data.RuleResults)
	}
	if len(resp.Errors) != 1 {
		t.Fatalf("expected one structured error, got %#v", resp.Errors)
	}
	if resp.Errors[0].MappingID != "missing_fail" || resp.Errors[0].Field == "" || resp.Errors[0].Code == "" || resp.Errors[0].Message == "" {
		t.Fatalf("expected shared structured error fields, got %#v", resp.Errors[0])
	}
}

func TestUpdatePipelineTriggers_RejectsInvalidWebhookMappingsWithStructuredErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "invalid-trigger-save-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{
		Name:        "invalid-trigger-save-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Environment: "development",
		Definition:  webhookRuntimeTestDefinitionJSON(t, webhookRuntimeTestShellNode("node-1", "echo default", true)),
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	body := bytes.NewBuffer(mustJSON(t, map[string]interface{}{
		"provider":                       "gitlab",
		"webhook_enabled":                true,
		"push_enabled":                   true,
		"webhook_runtime_input_mappings": `[{"id":"bad-target","source_type":"jsonpath","source_expr":"$.ref","target":{"node_id":"node-1","param_key":"unknown"},"missing_policy":"ignore"}]`,
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

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	resp := decodePipelineMappingErrorResponse(t, w.Body.Bytes())
	if len(resp.Errors) == 0 {
		t.Fatalf("expected structured validation errors, got %#v", resp)
	}
	if resp.Errors[0].MappingID != "bad-target" || resp.Errors[0].Field == "" || resp.Errors[0].Code == "" || resp.Errors[0].Message == "" {
		t.Fatalf("expected structured error fields, got %#v", resp.Errors[0])
	}
}

func TestHandleGitLabWebhook_ParsedGitRefOverridesGitCloneInputs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "webhook-mapping-run-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{
		Name:        "webhook-mapping-run-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Environment: "development",
		Definition: webhookRuntimeTestDefinitionJSON(t,
			PipelineNode{
				ID:      "clone-node",
				TaskKey: "git_clone",
				Type:    "git_clone",
				Name:    "Clone",
				DefinitionParams: []models.PipelineDefinitionParam{
					{Key: "git_repo_url", Label: "仓库地址", Value: "https://example.com/repo.git", IsFlexible: false},
					{Key: "git_ref", Label: "分支", Value: "main", IsFlexible: true},
					{Key: "git_commit", Label: "提交", Value: "", IsFlexible: true},
				},
			},
			PipelineNode{
				ID:          "shell-node",
				TaskKey:     "shell",
				Type:        "shell",
				Name:        "shell-node",
				TaskVersion: 1,
				Timeout:     300,
				DefinitionParams: []models.PipelineDefinitionParam{
					{Key: "working_dir", Label: "工作目录", Value: ".", IsFlexible: false},
					{Key: "shell", Label: "执行 Shell", Value: taskShellSH, IsFlexible: false},
					{Key: "script", Label: "脚本", Value: "echo default", IsFlexible: true},
				},
			},
		),
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	trigger := models.PipelineTrigger{
		WorkspaceID:                 workspace.ID,
		PipelineID:                  pipeline.ID,
		Provider:                    "gitlab",
		WebhookEnabled:              true,
		PushEnabled:                 true,
		SecretToken:                 "gitlab-secret",
		WebhookToken:                "public-trigger-token",
		Timezone:                    "UTC",
		PushBranchFilters:           "main\nrelease/*",
		WebhookRuntimeInputMappings: `[{"id":"map-script","source_type":"jsonpath","source_expr":"$.ref","target":{"node_id":"shell-node","param_key":"script"},"missing_policy":"ignore"}]`,
		WebhookConfigStatus:         "valid",
	}
	if err := db.Create(&trigger).Error; err != nil {
		t.Fatalf("create trigger failed: %v", err)
	}

	payload := mustJSON(t, map[string]interface{}{
		"object_kind":   "push",
		"ref":           "refs/heads/main",
		"project":       map[string]interface{}{"path_with_namespace": "group/project"},
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
	var runConfig models.PipelineRunConfigSnapshot
	if err := json.Unmarshal([]byte(run.RunConfig), &runConfig); err != nil {
		t.Fatalf("unmarshal run config failed: %v", err)
	}
	if got := runConfig.Inputs["clone-node"]["git_ref"]; got != "main" {
		t.Fatalf("expected parsed git_ref for clone node, got %#v", runConfig.Inputs)
	}
	if got := runConfig.Inputs["clone-node"]["git_commit"]; got != "abc123def456" {
		t.Fatalf("expected parsed git_commit for clone node, got %#v", runConfig.Inputs)
	}
	if got := runConfig.Inputs["shell-node"]["script"]; got != "refs/heads/main" {
		t.Fatalf("expected mapped shell runtime input, got %#v", runConfig.Inputs)
	}
}

func TestHandleGitLabWebhook_ParsedGitRefAppliesToAllGitCloneNodes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "webhook-multi-clone-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{
		Name:        "webhook-multi-clone-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Environment: "development",
		Definition: webhookRuntimeTestDefinitionJSON(t,
			PipelineNode{
				ID:      "clone-a",
				TaskKey: "git_clone",
				Type:    "git_clone",
				Name:    "Clone A",
				DefinitionParams: []models.PipelineDefinitionParam{
					{Key: "git_repo_url", Label: "仓库地址", Value: "https://example.com/repo-a.git", IsFlexible: false},
					{Key: "git_ref", Label: "分支", Value: "main", IsFlexible: true},
					{Key: "git_commit", Label: "提交", Value: "", IsFlexible: true},
				},
			},
			PipelineNode{
				ID:      "clone-b",
				TaskKey: "git_clone",
				Type:    "git_clone",
				Name:    "Clone B",
				DefinitionParams: []models.PipelineDefinitionParam{
					{Key: "git_repo_url", Label: "仓库地址", Value: "https://example.com/repo-b.git", IsFlexible: false},
					{Key: "git_ref", Label: "分支", Value: "main", IsFlexible: true},
					{Key: "git_commit", Label: "提交", Value: "", IsFlexible: true},
				},
			},
		),
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
		PushBranchFilters: "main",
	}
	if err := db.Create(&trigger).Error; err != nil {
		t.Fatalf("create trigger failed: %v", err)
	}

	payload := mustJSON(t, map[string]interface{}{
		"object_kind":   "push",
		"ref":           "refs/heads/main",
		"project":       map[string]interface{}{"path_with_namespace": "group/project"},
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
	var runConfig models.PipelineRunConfigSnapshot
	if err := json.Unmarshal([]byte(run.RunConfig), &runConfig); err != nil {
		t.Fatalf("unmarshal run config failed: %v", err)
	}
	if got := runConfig.Inputs["clone-a"]["git_ref"]; got != "main" {
		t.Fatalf("expected parsed git_ref for clone-a, got %#v", runConfig.Inputs)
	}
	if got := runConfig.Inputs["clone-a"]["git_commit"]; got != "abc123def456" {
		t.Fatalf("expected parsed git_commit for clone-a, got %#v", runConfig.Inputs)
	}
	if got := runConfig.Inputs["clone-b"]["git_ref"]; got != "main" {
		t.Fatalf("expected parsed git_ref for clone-b, got %#v", runConfig.Inputs)
	}
	if got := runConfig.Inputs["clone-b"]["git_commit"]; got != "abc123def456" {
		t.Fatalf("expected parsed git_commit for clone-b, got %#v", runConfig.Inputs)
	}
}

func TestHandleGitLabWebhook_MergeRequestUsesParsedSourceBranchForGitClone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "webhook-merge-request-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{
		Name:        "webhook-merge-request-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Environment: "development",
		Definition: webhookRuntimeTestDefinitionJSON(t,
			PipelineNode{
				ID:      "clone-node",
				TaskKey: "git_clone",
				Type:    "git_clone",
				Name:    "Clone",
				DefinitionParams: []models.PipelineDefinitionParam{
					{Key: "git_repo_url", Label: "仓库地址", Value: "https://example.com/repo.git", IsFlexible: false},
					{Key: "git_ref", Label: "分支", Value: "main", IsFlexible: true},
					{Key: "git_commit", Label: "提交", Value: "", IsFlexible: true},
				},
			},
		),
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	trigger := models.PipelineTrigger{
		WorkspaceID:                     workspace.ID,
		PipelineID:                      pipeline.ID,
		Provider:                        "gitlab",
		WebhookEnabled:                  true,
		MergeRequestEnabled:             true,
		SecretToken:                     "gitlab-secret",
		WebhookToken:                    "public-trigger-token",
		Timezone:                        "UTC",
		MergeRequestSourceBranchFilters: "feature/*",
		MergeRequestTargetBranchFilters: "main",
	}
	if err := db.Create(&trigger).Error; err != nil {
		t.Fatalf("create trigger failed: %v", err)
	}

	payload := mustJSON(t, map[string]interface{}{
		"object_kind":   "merge_request",
		"project":       map[string]interface{}{"path_with_namespace": "group/project"},
		"user_username": "gitlab-user",
		"object_attributes": map[string]interface{}{
			"action":        "open",
			"source_branch": "feature/demo",
			"target_branch": "main",
			"last_commit":   map[string]interface{}{"id": "mrsha123"},
		},
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
	var runConfig models.PipelineRunConfigSnapshot
	if err := json.Unmarshal([]byte(run.RunConfig), &runConfig); err != nil {
		t.Fatalf("unmarshal run config failed: %v", err)
	}
	if got := runConfig.Inputs["clone-node"]["git_ref"]; got != "feature/demo" {
		t.Fatalf("expected parsed merge request source branch, got %#v", runConfig.Inputs)
	}
	if got := runConfig.Inputs["clone-node"]["git_commit"]; got != "mrsha123" {
		t.Fatalf("expected parsed merge request commit, got %#v", runConfig.Inputs)
	}
}

func TestHandleGitLabWebhook_MappingFailReturnsBusiness422WithoutPipelineRun(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "webhook-mapping-fail-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{
		Name:        "webhook-mapping-fail-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Environment: "development",
		Definition:  webhookRuntimeTestDefinitionJSON(t, webhookRuntimeTestShellNode("node-1", "echo default", true)),
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	trigger := models.PipelineTrigger{
		WorkspaceID:                 workspace.ID,
		PipelineID:                  pipeline.ID,
		Provider:                    "gitlab",
		WebhookEnabled:              true,
		PushEnabled:                 true,
		SecretToken:                 "gitlab-secret",
		WebhookToken:                "public-trigger-token",
		Timezone:                    "UTC",
		WebhookRuntimeInputMappings: `[{"id":"required","source_type":"jsonpath","source_expr":"$.missing","target":{"node_id":"node-1","param_key":"working_dir"},"missing_policy":"fail"}]`,
		WebhookConfigStatus:         "valid",
	}
	if err := db.Create(&trigger).Error; err != nil {
		t.Fatalf("create trigger failed: %v", err)
	}

	payload := mustJSON(t, map[string]interface{}{
		"object_kind":   "push",
		"ref":           "refs/heads/main",
		"project":       map[string]interface{}{"path_with_namespace": "group/project"},
		"user_username": "gitlab-user",
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
	resp := decodePipelineMappingErrorResponse(t, w.Body.Bytes())
	if resp.Code != 422 {
		t.Fatalf("expected business code 422, got %#v", resp)
	}
	var count int64
	if err := db.Model(&models.PipelineRun{}).Where("pipeline_id = ?", pipeline.ID).Count(&count).Error; err != nil {
		t.Fatalf("count pipeline runs failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no runs created on mapping failure, got %d", count)
	}
}

func TestHandleGitLabWebhook_MappingFailReturnsStructuredErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "webhook-mapping-structured-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{
		Name:        "webhook-mapping-structured-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Environment: "development",
		Definition:  webhookRuntimeTestDefinitionJSON(t, webhookRuntimeTestShellNode("node-1", "echo default", true)),
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	trigger := models.PipelineTrigger{
		WorkspaceID:                 workspace.ID,
		PipelineID:                  pipeline.ID,
		Provider:                    "gitlab",
		WebhookEnabled:              true,
		PushEnabled:                 true,
		SecretToken:                 "gitlab-secret",
		WebhookToken:                "public-trigger-token",
		Timezone:                    "UTC",
		WebhookRuntimeInputMappings: `[{"id":"multi","source_type":"jsonpath","source_expr":"$.refs[*]","target":{"node_id":"node-1","param_key":"working_dir"},"missing_policy":"fail"}]`,
		WebhookConfigStatus:         "valid",
	}
	if err := db.Create(&trigger).Error; err != nil {
		t.Fatalf("create trigger failed: %v", err)
	}

	payload := mustJSON(t, map[string]interface{}{
		"object_kind": "push",
		"ref":         "refs/heads/main",
		"refs":        []interface{}{"main", "release"},
		"project":     map[string]interface{}{"path_with_namespace": "group/project"},
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
	resp := decodePipelineMappingErrorResponse(t, w.Body.Bytes())
	if len(resp.Errors) != 1 {
		t.Fatalf("expected structured errors, got %#v", resp)
	}
	if resp.Errors[0].MappingID != "multi" || resp.Errors[0].Field == "" || resp.Errors[0].Code == "" || resp.Errors[0].Message == "" {
		t.Fatalf("expected shared error fields, got %#v", resp.Errors[0])
	}
}

func TestHandleGitLabWebhook_InvalidTriggerConfigRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "webhook-invalid-trigger-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{
		Name:        "webhook-invalid-trigger-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Environment: "development",
		Definition:  webhookRuntimeTestDefinitionJSON(t, webhookRuntimeTestShellNode("node-1", "echo default", true)),
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	trigger := models.PipelineTrigger{
		WorkspaceID:                 workspace.ID,
		PipelineID:                  pipeline.ID,
		Provider:                    "gitlab",
		WebhookEnabled:              true,
		PushEnabled:                 true,
		SecretToken:                 "gitlab-secret",
		WebhookToken:                "public-trigger-token",
		Timezone:                    "UTC",
		WebhookRuntimeInputMappings: `[{"id":"bad-target","source_type":"jsonpath","source_expr":"$.ref","target":{"node_id":"node-1","param_key":"unknown"},"missing_policy":"ignore"}]`,
		WebhookConfigStatus:         "invalid",
		WebhookConfigInvalidReason:  "stale target",
	}
	if err := db.Create(&trigger).Error; err != nil {
		t.Fatalf("create trigger failed: %v", err)
	}

	payload := mustJSON(t, map[string]interface{}{
		"object_kind": "push",
		"ref":         "refs/heads/main",
		"project":     map[string]interface{}{"path_with_namespace": "group/project"},
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
	resp := decodePipelineMappingErrorResponse(t, w.Body.Bytes())
	if resp.Code != 422 {
		t.Fatalf("expected business code 422, got %#v", resp)
	}
	if len(resp.Errors) == 0 {
		t.Fatalf("expected invalid trigger structured errors, got %#v", resp)
	}
}

func TestUpdatePipelineDefinition_InvalidatesWebhookTriggerWhenTargetRemoved(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "webhook-invalidate-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{
		Name:        "webhook-invalidate-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Environment: "development",
		Definition:  webhookRuntimeTestDefinitionJSON(t, webhookRuntimeTestShellNode("node-1", "echo default", true)),
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	trigger := models.PipelineTrigger{
		WorkspaceID:                 workspace.ID,
		PipelineID:                  pipeline.ID,
		Provider:                    "gitlab",
		WebhookEnabled:              true,
		PushEnabled:                 true,
		SecretToken:                 "gitlab-secret",
		WebhookToken:                "public-trigger-token",
		Timezone:                    "UTC",
		WebhookRuntimeInputMappings: `[{"id":"rule-1","source_type":"jsonpath","source_expr":"$.ref","target":{"node_id":"node-1","param_key":"working_dir"},"missing_policy":"ignore"}]`,
		WebhookConfigStatus:         "valid",
	}
	if err := db.Create(&trigger).Error; err != nil {
		t.Fatalf("create trigger failed: %v", err)
	}

	body := bytes.NewBuffer(mustJSON(t, map[string]interface{}{
		"definition_json": webhookRuntimeTestDefinitionJSON(t, webhookRuntimeTestShellNode("node-1", "echo fixed", false)),
	}))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/pipelines/1", body)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(pipeline.ID, 10)}}
	c.Set("user_id", user.ID)
	c.Set("role", "user")
	c.Set("workspace_id", workspace.ID)

	h.UpdatePipeline(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var refreshed models.PipelineTrigger
	if err := db.Where("pipeline_id = ?", pipeline.ID).First(&refreshed).Error; err != nil {
		t.Fatalf("reload trigger failed: %v", err)
	}
	if refreshed.WebhookConfigStatus != "invalid" {
		t.Fatalf("expected invalidated webhook config status, got %#v", refreshed)
	}
	if strings.TrimSpace(refreshed.WebhookConfigInvalidReason) == "" {
		t.Fatalf("expected invalid reason to be persisted, got %#v", refreshed)
	}
}

func TestUpdatePipelineTriggers_RepairMappingsRestoresValidStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "webhook-repair-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{
		Name:        "webhook-repair-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Environment: "development",
		Definition:  webhookRuntimeTestDefinitionJSON(t, webhookRuntimeTestShellNode("node-1", "echo default", true)),
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	trigger := models.PipelineTrigger{
		WorkspaceID:                 workspace.ID,
		PipelineID:                  pipeline.ID,
		Provider:                    "gitlab",
		WebhookEnabled:              true,
		PushEnabled:                 true,
		SecretToken:                 "gitlab-secret",
		WebhookToken:                "public-trigger-token",
		Timezone:                    "UTC",
		WebhookRuntimeInputMappings: `[{"id":"stale","source_type":"jsonpath","source_expr":"$.ref","target":{"node_id":"node-1","param_key":"unknown"},"missing_policy":"ignore"}]`,
		WebhookConfigStatus:         "invalid",
		WebhookConfigInvalidReason:  "stale target",
	}
	if err := db.Create(&trigger).Error; err != nil {
		t.Fatalf("create trigger failed: %v", err)
	}

	body := bytes.NewBuffer(mustJSON(t, map[string]interface{}{
		"provider":                       "gitlab",
		"webhook_enabled":                true,
		"push_enabled":                   true,
		"webhook_runtime_input_mappings": `[{"id":"rule-1","source_type":"jsonpath","source_expr":"$.ref","target":{"node_id":"node-1","param_key":"working_dir"},"missing_policy":"ignore"}]`,
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
	var refreshed models.PipelineTrigger
	if err := db.Where("pipeline_id = ?", pipeline.ID).First(&refreshed).Error; err != nil {
		t.Fatalf("reload trigger failed: %v", err)
	}
	if refreshed.WebhookConfigStatus != "valid" || refreshed.WebhookConfigInvalidReason != "" {
		t.Fatalf("expected repaired webhook config to return valid, got %#v", refreshed)
	}
}

func TestHandleGitLabWebhook_RuntimeRevalidationRejectsStaleTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "webhook-revalidate-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{
		Name:        "webhook-revalidate-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Environment: "development",
		Definition:  webhookRuntimeTestDefinitionJSON(t, webhookRuntimeTestShellNode("node-1", "echo default", true)),
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	trigger := models.PipelineTrigger{
		WorkspaceID:                 workspace.ID,
		PipelineID:                  pipeline.ID,
		Provider:                    "gitlab",
		WebhookEnabled:              true,
		PushEnabled:                 true,
		SecretToken:                 "gitlab-secret",
		WebhookToken:                "public-trigger-token",
		Timezone:                    "UTC",
		WebhookRuntimeInputMappings: `[{"id":"rule-1","source_type":"jsonpath","source_expr":"$.ref","target":{"node_id":"node-1","param_key":"working_dir"},"missing_policy":"ignore"}]`,
		WebhookConfigStatus:         "valid",
	}
	if err := db.Create(&trigger).Error; err != nil {
		t.Fatalf("create trigger failed: %v", err)
	}
	if err := db.Model(&pipeline).Update("definition_json", webhookRuntimeTestDefinitionJSON(t, webhookRuntimeTestShellNode("node-1", "echo fixed", false))).Error; err != nil {
		t.Fatalf("update pipeline definition failed: %v", err)
	}

	payload := mustJSON(t, map[string]interface{}{
		"object_kind": "push",
		"ref":         "refs/heads/main",
		"project":     map[string]interface{}{"path_with_namespace": "group/project"},
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
	resp := decodePipelineMappingErrorResponse(t, w.Body.Bytes())
	if resp.Code != 422 {
		t.Fatalf("expected business code 422, got %#v", resp)
	}
}

func TestHandleGitLabWebhook_RuntimeRevalidationRepairsStaleInvalidStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "webhook-revalidate-repair-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{
		Name:        "webhook-revalidate-repair-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Environment: "development",
		Definition:  webhookRuntimeTestDefinitionJSON(t, webhookRuntimeTestShellNode("node-1", "echo default", true)),
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	trigger := models.PipelineTrigger{
		WorkspaceID:                 workspace.ID,
		PipelineID:                  pipeline.ID,
		Provider:                    "gitlab",
		WebhookEnabled:              true,
		PushEnabled:                 true,
		SecretToken:                 "gitlab-secret",
		WebhookToken:                "public-trigger-token",
		Timezone:                    "UTC",
		WebhookRuntimeInputMappings: `[{"id":"rule-1","source_type":"jsonpath","source_expr":"$.ref","target":{"node_id":"node-1","param_key":"working_dir"},"missing_policy":"ignore"}]`,
		WebhookConfigStatus:         "invalid",
		WebhookConfigInvalidReason:  "stale target",
	}
	if err := db.Create(&trigger).Error; err != nil {
		t.Fatalf("create trigger failed: %v", err)
	}

	payload := mustJSON(t, map[string]interface{}{
		"object_kind":  "push",
		"ref":          "refs/heads/main",
		"checkout_sha": "abc123def456",
		"project":      map[string]interface{}{"path_with_namespace": "group/project"},
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
	if !bytes.Contains(w.Body.Bytes(), []byte(`"code":200`)) {
		t.Fatalf("expected webhook trigger success, got %s", w.Body.String())
	}
	var refreshed models.PipelineTrigger
	if err := db.Where("pipeline_id = ?", pipeline.ID).First(&refreshed).Error; err != nil {
		t.Fatalf("reload trigger failed: %v", err)
	}
	if refreshed.WebhookConfigStatus != "valid" || refreshed.WebhookConfigInvalidReason != "" {
		t.Fatalf("expected runtime revalidation to repair stale invalid status, got %#v", refreshed)
	}
	var runCount int64
	if err := db.Model(&models.PipelineRun{}).Where("pipeline_id = ?", pipeline.ID).Count(&runCount).Error; err != nil {
		t.Fatalf("count pipeline runs failed: %v", err)
	}
	if runCount != 1 {
		t.Fatalf("expected webhook to create one pipeline run, got %d", runCount)
	}
}

func TestCopyPipeline_NewPipelineDoesNotInheritWebhookConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "copy-webhook-user", models.WorkspaceRoleDeveloper)
	sourceDefinition := string(mustJSON(t, map[string]interface{}{
		"version": "2.0",
		"nodes": []map[string]interface{}{{
			"node_id":      "node-1",
			"node_name":    "Build",
			"task_key":     "shell",
			"task_version": 1,
			"timeout":      300,
			"type":         "shell",
			"params": []map[string]interface{}{
				{"key": "working_dir", "label": "工作目录", "value": ".", "is_flexible": true},
				{"key": "shell", "label": "执行 Shell", "value": taskShellSH, "is_flexible": false},
				{"key": "script", "label": "脚本", "value": "echo source", "is_flexible": false},
			},
		}},
		"edges": []map[string]interface{}{},
		"triggers": []map[string]interface{}{
			{"type": "manual", "enabled": true},
			{"type": "webhook", "enabled": true, "provider": "gitlab", "push_enabled": true, "webhook_enabled": true, "webhook_runtime_input_mappings": `[{"id":"rule-1"}]`, "webhook_config_status": "invalid", "webhook_config_invalid_reason": "source stale"},
		},
	}))
	body := bytes.NewBuffer(mustJSON(t, map[string]interface{}{
		"name":            "copied-pipeline",
		"environment":     "development",
		"definition_json": sourceDefinition,
	}))
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
	var copied models.Pipeline
	if err := db.Where("name = ?", "copied-pipeline").First(&copied).Error; err != nil {
		t.Fatalf("load copied pipeline failed: %v", err)
	}
	var copiedDefinition PipelineConfig
	if err := json.Unmarshal([]byte(copied.Definition), &copiedDefinition); err != nil {
		t.Fatalf("unmarshal copied definition failed: %v", err)
	}
	copiedDefinitionJSON := string(mustJSON(t, copiedDefinition))
	if strings.Contains(copiedDefinitionJSON, "source stale") || strings.Contains(copiedDefinitionJSON, `[{\"id\":\"rule-1\"}]`) {
		t.Fatalf("expected copied definition to reset inherited webhook config, got %s", copiedDefinitionJSON)
	}

	getW := httptest.NewRecorder()
	getC, _ := gin.CreateTestContext(getW)
	getC.Request = httptest.NewRequest(http.MethodGet, "/api/pipelines/1/triggers", nil)
	getC.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(copied.ID, 10)}}
	getC.Set("user_id", user.ID)
	getC.Set("role", "user")
	getC.Set("workspace_id", workspace.ID)

	h.GetPipelineTriggers(getC)

	if getW.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", getW.Code, getW.Body.String())
	}
	if bytes.Contains(getW.Body.Bytes(), []byte(`"webhook_enabled":true`)) || bytes.Contains(getW.Body.Bytes(), []byte(`"webhook_config_status":"invalid"`)) {
		t.Fatalf("expected copied pipeline trigger state to start fresh, got %s", getW.Body.String())
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
		Definition:  webhookRuntimeTestDefinitionJSON(t, webhookRuntimeTestShellNode("node-1", "echo default", true)),
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	trigger := models.PipelineTrigger{
		WorkspaceID:                 workspace.ID,
		PipelineID:                  pipeline.ID,
		Provider:                    "gitlab",
		WebhookEnabled:              true,
		PushEnabled:                 true,
		SecretToken:                 "gitlab-secret",
		WebhookToken:                "public-trigger-token",
		Timezone:                    "UTC",
		PushBranchFilters:           "main\nrelease/*",
		WebhookRuntimeInputMappings: `[{"id":"rule-1","source_type":"jsonpath","source_expr":"$.ref","target":{"node_id":"node-1","param_key":"working_dir"},"missing_policy":"ignore"}]`,
		WebhookConfigStatus:         "valid",
	}
	if err := db.Create(&trigger).Error; err != nil {
		t.Fatalf("create trigger failed: %v", err)
	}

	payload := mustJSON(t, map[string]interface{}{
		"object_kind":   "push",
		"ref":           "refs/heads/main",
		"project":       map[string]interface{}{"path_with_namespace": "group/project"},
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
	var runConfig models.PipelineRunConfigSnapshot
	if err := json.Unmarshal([]byte(run.RunConfig), &runConfig); err != nil {
		t.Fatalf("unmarshal run config failed: %v", err)
	}
	if runConfig.Inputs["node-1"]["working_dir"] != "refs/heads/main" {
		t.Fatalf("expected webhook mapped runtime input, got %#v", runConfig.Inputs)
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
		Definition:  webhookRuntimeTestDefinitionJSON(t, webhookRuntimeTestShellNode("node-1", "echo default", true)),
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
		"object_kind":   "push",
		"ref":           "refs/heads/main",
		"project":       map[string]interface{}{"path_with_namespace": "group/project"},
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
	if updatedRun.Status != models.PipelineRunStatusCancelRequested {
		t.Fatalf("expected run status cancel_requested, got %s", updatedRun.Status)
	}
	if updatedRun.EndTime != 0 {
		t.Fatalf("expected run end_time to stay 0 before terminal cancel, got %d", updatedRun.EndTime)
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
	if updatedTasks[1].Status != models.TaskStatusCancelRequested {
		t.Fatalf("expected task 2 to be cancel_requested, got %s", updatedTasks[1].Status)
	}
	if updatedTasks[1].EndTime != 0 {
		t.Fatalf("expected task 2 end_time to stay 0 before terminal cancel, got %d", updatedTasks[1].EndTime)
	}
}

func TestCancelPipelineRun_CancelsQueuedTasksForQueuedRun(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "cancel-queued-run-user", models.WorkspaceRoleDeveloper)

	pipeline := models.Pipeline{
		Name:        "cancel-queued-run-pipeline",
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
		Status:      models.PipelineRunStatusQueued,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}

	task := models.AgentTask{
		WorkspaceID:   workspace.ID,
		PipelineRunID: run.ID,
		NodeID:        "1",
		TaskType:      "shell",
		Name:          "Build",
		Status:        models.TaskStatusQueued,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/pipelines/%d/runs/%d/cancel", pipeline.ID, run.ID), nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(pipeline.ID, 10)}, {Key: "run_id", Value: strconv.FormatUint(run.ID, 10)}}
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
		t.Fatalf("expected queued run to become cancelled, got %s", updatedRun.Status)
	}

	var updatedTask models.AgentTask
	if err := db.First(&updatedTask, task.ID).Error; err != nil {
		t.Fatalf("load task failed: %v", err)
	}
	if updatedTask.Status != models.TaskStatusCancelled {
		t.Fatalf("expected queued task to become cancelled, got %s", updatedTask.Status)
	}

	if updatedTask.EndTime == 0 {
		t.Fatal("expected cancelled queued task end_time to be set")
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
