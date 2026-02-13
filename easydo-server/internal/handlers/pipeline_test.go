package handlers

import (
	"encoding/json"
	"testing"
)

func TestPipelineConfig_GetEdges(t *testing.T) {
	tests := []struct {
		name           string
		config         PipelineConfig
		expectedEdges  int
		expectedFrom   string
		expectedTo     string
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
		name          string
		node          PipelineNode
		expectedType  string
		expectEmpty   bool
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
			name: "invalid - multiple disconnected nodes",
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
			expectErr:   "流水线配置无效：存在不可达节点: [3]",
		},
		{
			name: "valid - disconnected components (multiple entry points)",
			config: PipelineConfig{
				Version: "2.0",
				Nodes: []PipelineNode{
					{ID: "1", Type: "shell", Name: "A"},
					{ID: "2", Type: "shell", Name: "B"},
				},
				Edges: []PipelineEdge{},
			},
			expectValid: true,
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
