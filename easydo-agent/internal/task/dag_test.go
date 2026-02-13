package task

import (
	"testing"
)

func TestPipelineConfig_GetEdges(t *testing.T) {
	tests := []struct {
		name          string
		config        PipelineConfig
		expectedCount int
	}{
		{
			name: "new format with edges",
			config: PipelineConfig{
				Version: "2.0",
				Nodes: []PipelineNode{
					{ID: "1", Type: "shell"},
					{ID: "2", Type: "shell"},
				},
				Edges: []PipelineEdge{
					{From: "1", To: "2"},
				},
			},
			expectedCount: 1,
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
			expectedCount: 1,
		},
		{
			name:          "empty config",
			config:        PipelineConfig{},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edges := tt.config.GetEdges()
			if len(edges) != tt.expectedCount {
				t.Errorf("Expected %d edges, got %d", tt.expectedCount, len(edges))
			}
		})
	}
}

func TestPipelineNode_GetNodeConfig(t *testing.T) {
	tests := []struct {
		name        string
		node        PipelineNode
		expectedKey string
		expectedVal string
		expectEmpty bool
	}{
		{
			name: "config takes precedence",
			node: PipelineNode{
				ID:     "1",
				Type:   "shell",
				Config: map[string]interface{}{"script": "echo config"},
				Params: map[string]interface{}{"script": "echo params"},
			},
			expectedKey: "script",
			expectedVal: "echo config",
		},
		{
			name: "fallback to params",
			node: PipelineNode{
				ID:     "1",
				Type:   "shell",
				Params: map[string]interface{}{"script": "echo params"},
			},
			expectedKey: "script",
			expectedVal: "echo params",
		},
		{
			name:        "empty config",
			node:        PipelineNode{ID: "1", Type: "shell"},
			expectEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := tt.node.GetNodeConfig()

			if tt.expectEmpty {
				if len(config) != 0 {
					t.Errorf("Expected empty config, got %v", config)
				}
				return
			}

			val, ok := config[tt.expectedKey].(string)
			if !ok {
				t.Errorf("Expected key %s not found", tt.expectedKey)
				return
			}

			if val != tt.expectedVal {
				t.Errorf("Expected %s=%s, got %s", tt.expectedKey, tt.expectedVal, val)
			}
		})
	}
}

func TestDAGEngine_BuildGraph(t *testing.T) {
	tests := []struct {
		name        string
		config      PipelineConfig
		expectError bool
		errContains string
	}{
		{
			name: "valid simple DAG",
			config: PipelineConfig{
				Nodes: []PipelineNode{
					{ID: "1", Type: "shell"},
					{ID: "2", Type: "shell"},
				},
				Edges: []PipelineEdge{
					{From: "1", To: "2"},
				},
			},
			expectError: false,
		},
		{
			name: "valid complex DAG",
			config: PipelineConfig{
				Nodes: []PipelineNode{
					{ID: "1", Type: "shell"},
					{ID: "2", Type: "shell"},
					{ID: "3", Type: "shell"},
					{ID: "4", Type: "shell"},
				},
				Edges: []PipelineEdge{
					{From: "1", To: "3"},
					{From: "2", To: "3"},
					{From: "3", To: "4"},
				},
			},
			expectError: false,
		},
		{
			name: "missing source node",
			config: PipelineConfig{
				Nodes: []PipelineNode{
					{ID: "1", Type: "shell"},
				},
				Edges: []PipelineEdge{
					{From: "nonexistent", To: "1"},
				},
			},
			expectError: true,
			errContains: "source node not found",
		},
		{
			name: "missing target node",
			config: PipelineConfig{
				Nodes: []PipelineNode{
					{ID: "1", Type: "shell"},
				},
				Edges: []PipelineEdge{
					{From: "1", To: "nonexistent"},
				},
			},
			expectError: true,
			errContains: "target node not found",
		},
		{
			name: "empty node ID",
			config: PipelineConfig{
				Nodes: []PipelineNode{
					{ID: "", Type: "shell"},
				},
			},
			expectError: true,
			errContains: "node ID cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewDAGEngine(tt.config, nil)
			err := engine.BuildGraph()

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got nil")
				} else if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestDAGEngine_GetExecutableNodes(t *testing.T) {
	config := PipelineConfig{
		Nodes: []PipelineNode{
			{ID: "1", Type: "shell"},
			{ID: "2", Type: "shell"},
			{ID: "3", Type: "shell"},
		},
		Edges: []PipelineEdge{
			{From: "1", To: "3"},
			{From: "2", To: "3"},
		},
	}

	engine := NewDAGEngine(config, nil)
	if err := engine.BuildGraph(); err != nil {
		t.Fatalf("Failed to build graph: %v", err)
	}

	executable := engine.GetExecutableNodes()
	if len(executable) != 2 {
		t.Errorf("Expected 2 executable nodes, got %d", len(executable))
	}

	engine.MarkCompleted("1", true, nil)

	executable = engine.GetExecutableNodes()
	if len(executable) != 1 {
		t.Errorf("Expected 1 executable node, got %d", len(executable))
	}

	engine.MarkCompleted("2", true, nil)

	executable = engine.GetExecutableNodes()
	if len(executable) != 1 {
		t.Errorf("Expected 1 executable node, got %d", len(executable))
	}
	if executable[0] != "3" {
		t.Errorf("Expected node 3 to be executable, got %s", executable[0])
	}
}

func TestDAGEngine_IsCompleted(t *testing.T) {
	config := PipelineConfig{
		Nodes: []PipelineNode{
			{ID: "1", Type: "shell"},
			{ID: "2", Type: "shell"},
		},
		Edges: []PipelineEdge{
			{From: "1", To: "2"},
		},
	}

	engine := NewDAGEngine(config, nil)
	if err := engine.BuildGraph(); err != nil {
		t.Fatalf("Failed to build graph: %v", err)
	}

	if engine.IsCompleted() {
		t.Error("Should not be completed initially")
	}

	engine.MarkCompleted("1", true, nil)
	if engine.IsCompleted() {
		t.Error("Should not be completed after completing only node 1")
	}

	engine.MarkCompleted("2", true, nil)
	if !engine.IsCompleted() {
		t.Error("Should be completed after completing all nodes")
	}
}

func TestDAGEngine_GetNodeOutput(t *testing.T) {
	config := PipelineConfig{
		Nodes: []PipelineNode{
			{ID: "1", Type: "git_clone"},
			{ID: "2", Type: "shell"},
		},
	}

	engine := NewDAGEngine(config, nil)
	if err := engine.BuildGraph(); err != nil {
		t.Fatalf("Failed to build graph: %v", err)
	}

	outputs := map[string]interface{}{
		"commit_id": "abc123",
		"branch":    "main",
	}

	engine.MarkCompleted("1", true, outputs)

	retrieved := engine.GetNodeOutput("1")
	if retrieved == nil {
		t.Fatal("Expected output but got nil")
	}

	if retrieved["commit_id"] != "abc123" {
		t.Errorf("Expected commit_id=abc123, got %v", retrieved["commit_id"])
	}

	if engine.GetNodeOutput("2") != nil {
		t.Error("Expected nil output for incomplete node")
	}
}

func TestParsePipelineAssign(t *testing.T) {
	jsonData := `{
		"run_id": 123,
		"config": {
			"version": "2.0",
			"nodes": [
				{"id": "1", "type": "shell", "name": "Build"}
			],
			"edges": []
		},
		"agent_config": {
			"workspace": "/workspace/run-123",
			"timeout": 3600,
			"env_vars": {"CI": "true"}
		}
	}`

	msg, err := ParsePipelineAssign([]byte(jsonData))
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	if msg.RunID != 123 {
		t.Errorf("Expected run_id=123, got %d", msg.RunID)
	}

	if msg.Config.Version != "2.0" {
		t.Errorf("Expected version=2.0, got %s", msg.Config.Version)
	}

	if len(msg.Config.Nodes) != 1 {
		t.Errorf("Expected 1 node, got %d", len(msg.Config.Nodes))
	}

	if msg.AgentConfig.Workspace != "/workspace/run-123" {
		t.Errorf("Expected workspace=/workspace/run-123, got %s", msg.AgentConfig.Workspace)
	}

	if msg.AgentConfig.EnvVars["CI"] != "true" {
		t.Error("Expected CI=true in env_vars")
	}
}

func TestParsePipelineAssign_InvalidJSON(t *testing.T) {
	invalidJSON := `{invalid json}`
	_, err := ParsePipelineAssign([]byte(invalidJSON))
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestDAGEngine_IgnoreFailure(t *testing.T) {
	tests := []struct {
		name                    string
		edgeIgnoreFailure       bool
		node1Success            bool
		expectNode2Executable   bool
		expectBlockingExecution bool
	}{
		{
			name:                    "Node1 success - Node2 can execute",
			edgeIgnoreFailure:       false,
			node1Success:            true,
			expectNode2Executable:   true,
			expectBlockingExecution: false,
		},
		{
			name:                    "Node1 failed without IgnoreFailure - Node2 blocked",
			edgeIgnoreFailure:       false,
			node1Success:            false,
			expectNode2Executable:   false,
			expectBlockingExecution: true,
		},
		{
			name:                    "Node1 failed with IgnoreFailure - Node2 can execute",
			edgeIgnoreFailure:       true,
			node1Success:            false,
			expectNode2Executable:   true,
			expectBlockingExecution: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := PipelineConfig{
				Nodes: []PipelineNode{
					{ID: "1", Type: "shell"},
					{ID: "2", Type: "shell"},
				},
				Edges: []PipelineEdge{
					{From: "1", To: "2", IgnoreFailure: tt.edgeIgnoreFailure},
				},
			}

			engine := NewDAGEngine(config, nil)
			if err := engine.BuildGraph(); err != nil {
				t.Fatalf("Failed to build graph: %v", err)
			}

			executable := engine.GetExecutableNodes()
			if len(executable) != 1 || executable[0] != "1" {
				t.Errorf("Expected node 1 to be executable initially, got %v", executable)
			}

			engine.MarkCompleted("1", tt.node1Success, nil)

			executable = engine.GetExecutableNodes()
			node2Executable := len(executable) == 1 && executable[0] == "2"
			if node2Executable != tt.expectNode2Executable {
				t.Errorf("Expected node2 executable=%v, got %v (executable nodes: %v)",
					tt.expectNode2Executable, node2Executable, executable)
			}

			blocking := engine.HasFailedNodesBlockingExecution()
			if blocking != tt.expectBlockingExecution {
				t.Errorf("Expected blocking execution=%v, got %v", tt.expectBlockingExecution, blocking)
			}
		})
	}
}

func TestDAGEngine_ComplexDependenciesWithIgnoreFailure(t *testing.T) {
	config := PipelineConfig{
		Nodes: []PipelineNode{
			{ID: "A", Type: "shell"},
			{ID: "B", Type: "shell"},
			{ID: "C", Type: "shell"},
		},
		Edges: []PipelineEdge{
			{From: "A", To: "C", IgnoreFailure: false},
			{From: "B", To: "C", IgnoreFailure: false},
		},
	}

	engine := NewDAGEngine(config, nil)
	if err := engine.BuildGraph(); err != nil {
		t.Fatalf("Failed to build graph: %v", err)
	}

	executable := engine.GetExecutableNodes()
	if len(executable) != 2 {
		t.Errorf("Expected 2 executable nodes (A, B), got %d: %v", len(executable), executable)
	}

	engine.MarkCompleted("A", false, nil)

	engine.MarkCompleted("B", true, nil)

	executable = engine.GetExecutableNodes()
	if len(executable) != 0 {
		t.Errorf("Expected 0 executable nodes (C blocked by A), got %d: %v", len(executable), executable)
	}

	if !engine.HasFailedNodesBlockingExecution() {
		t.Error("Expected execution to be blocked by failed node A")
	}

	config2 := PipelineConfig{
		Nodes: []PipelineNode{
			{ID: "A", Type: "shell"},
			{ID: "B", Type: "shell"},
			{ID: "C", Type: "shell"},
		},
		Edges: []PipelineEdge{
			{From: "A", To: "C", IgnoreFailure: true},
			{From: "B", To: "C", IgnoreFailure: false},
		},
	}

	engine2 := NewDAGEngine(config2, nil)
	if err := engine2.BuildGraph(); err != nil {
		t.Fatalf("Failed to build graph: %v", err)
	}

	engine2.MarkCompleted("A", false, nil)
	engine2.MarkCompleted("B", true, nil)

	executable = engine2.GetExecutableNodes()
	if len(executable) != 1 || executable[0] != "C" {
		t.Errorf("Expected node C to be executable (A->C has IgnoreFailure), got %v", executable)
	}
}

func TestDAGEngine_FullPipelineExecution(t *testing.T) {
	config := PipelineConfig{
		Nodes: []PipelineNode{
			{ID: "A", Type: "shell", IgnoreFailure: false},
			{ID: "B", Type: "shell", IgnoreFailure: false},
			{ID: "C", Type: "shell", IgnoreFailure: false},
		},
		Edges: []PipelineEdge{
			{From: "A", To: "B"},
			{From: "B", To: "C"},
		},
	}

	engine := NewDAGEngine(config, nil)
	if err := engine.BuildGraph(); err != nil {
		t.Fatalf("Failed to build graph: %v", err)
	}

	executable := engine.GetExecutableNodes()
	if len(executable) != 1 || executable[0] != "A" {
		t.Errorf("Step 1: Expected only node A executable, got %v", executable)
	}

	engine.MarkCompleted("A", true, nil)

	executable = engine.GetExecutableNodes()
	if len(executable) != 1 || executable[0] != "B" {
		t.Errorf("Step 3: Expected only node B executable after A completed, got %v", executable)
	}

	engine.MarkCompleted("B", true, nil)

	executable = engine.GetExecutableNodes()
	if len(executable) != 1 || executable[0] != "C" {
		t.Errorf("Step 5: Expected only node C executable after B completed, got %v", executable)
	}

	engine.MarkCompleted("C", true, nil)

	if !engine.IsCompleted() {
		t.Error("Step 7: Expected all nodes to be completed")
	}

	executable = engine.GetExecutableNodes()
	if len(executable) != 0 {
		t.Errorf("Step 8: Expected no executable nodes, got %v", executable)
	}
}

func TestDAGEngine_ParallelExecution(t *testing.T) {
	config := PipelineConfig{
		Nodes: []PipelineNode{
			{ID: "A", Type: "shell", IgnoreFailure: false},
			{ID: "B", Type: "shell", IgnoreFailure: false},
			{ID: "C", Type: "shell", IgnoreFailure: false},
		},
		Edges: []PipelineEdge{
			{From: "A", To: "C"},
			{From: "B", To: "C"},
		},
	}

	engine := NewDAGEngine(config, nil)
	if err := engine.BuildGraph(); err != nil {
		t.Fatalf("Failed to build graph: %v", err)
	}

	executable := engine.GetExecutableNodes()
	if len(executable) != 2 {
		t.Errorf("Step 1: Expected 2 executable nodes (A, B), got %d: %v", len(executable), executable)
	}

	engine.MarkCompleted("A", true, nil)

	executable = engine.GetExecutableNodes()
	if len(executable) != 1 || executable[0] != "B" {
		t.Errorf("Step 3: Expected only B executable (C waiting for B), got %v", executable)
	}

	engine.MarkCompleted("B", true, nil)

	executable = engine.GetExecutableNodes()
	if len(executable) != 1 || executable[0] != "C" {
		t.Errorf("Step 5: Expected only C executable after both A and B completed, got %v", executable)
	}

	engine.MarkCompleted("C", true, nil)

	if !engine.IsCompleted() {
		t.Error("Step 7: Expected all nodes to be completed")
	}
}
