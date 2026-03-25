package task

import (
	"encoding/json"
	"fmt"
)

type PipelineNode struct {
	ID      string                 `json:"id"`
	Type    string                 `json:"type"`
	Name    string                 `json:"name"`
	Config  map[string]interface{} `json:"config,omitempty"`
	Params  map[string]interface{} `json:"params,omitempty"`
	Timeout int                    `json:"timeout"`
	// 节点配置选项
	IgnoreFailure bool `json:"ignore_failure"` // 失败时是否继续执行下游节点
	RetryCount    int  `json:"retry_count"`    // 失败重试次数
	RetryInterval int  `json:"retry_interval"` // 重试间隔（秒）
}

type PipelineEdge struct {
	From          string `json:"from"`
	To            string `json:"to"`
	IgnoreFailure bool   `json:"ignore_failure"` // 失败时是否继续执行下游节点
}

type PipelineConnection struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
}

type PipelineConfig struct {
	Version     string               `json:"version"`
	Nodes       []PipelineNode       `json:"nodes"`
	Edges       []PipelineEdge       `json:"edges"`
	Connections []PipelineConnection `json:"connections"`
}

func (c *PipelineConfig) GetEdges() []PipelineEdge {
	if len(c.Edges) > 0 {
		return c.Edges
	}

	if len(c.Connections) > 0 {
		edges := make([]PipelineEdge, len(c.Connections))
		for i, conn := range c.Connections {
			edges[i] = PipelineEdge{
				From: conn.From,
				To:   conn.To,
			}
		}
		return edges
	}

	return nil
}

func (n *PipelineNode) GetNodeConfig() map[string]interface{} {
	if n.Config != nil && len(n.Config) > 0 {
		return n.Config
	}
	if n.Params != nil && len(n.Params) > 0 {
		return n.Params
	}
	return make(map[string]interface{})
}

// NodeStatus represents the execution status of a node
type NodeStatus int

const (
	NodeStatusPending NodeStatus = iota
	NodeStatusRunning
	NodeStatusSuccess
	NodeStatusFailed
)

func (s NodeStatus) String() string {
	switch s {
	case NodeStatusPending:
		return "pending"
	case NodeStatusRunning:
		return "running"
	case NodeStatusSuccess:
		return "success"
	case NodeStatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

type DAGEngine struct {
	config          PipelineConfig
	nodeMap         map[string]*PipelineNode
	inDegree        map[string]int
	graph           map[string][]string
	completed       map[string]bool
	outputs         map[string]map[string]interface{}
	executor        *Executor
	logCallback     func(taskID uint64, level, message, source string, lineNumber int)
	nodeStatus      map[string]NodeStatus
	initialInDegree map[string]int
}

func NewDAGEngine(config PipelineConfig, executor *Executor) *DAGEngine {
	return &DAGEngine{
		config:          config,
		nodeMap:         make(map[string]*PipelineNode),
		inDegree:        make(map[string]int),
		graph:           make(map[string][]string),
		completed:       make(map[string]bool),
		outputs:         make(map[string]map[string]interface{}),
		executor:        executor,
		nodeStatus:      make(map[string]NodeStatus),
		initialInDegree: make(map[string]int),
	}
}

func (e *DAGEngine) SetLogCallback(callback func(taskID uint64, level, message, source string, lineNumber int)) {
	e.logCallback = callback
}

func (e *DAGEngine) BuildGraph() error {
	for i := range e.config.Nodes {
		node := &e.config.Nodes[i]
		if node.ID == "" {
			return fmt.Errorf("node ID cannot be empty")
		}
		e.nodeMap[node.ID] = node
		e.inDegree[node.ID] = 0
		e.graph[node.ID] = []string{}
		e.nodeStatus[node.ID] = NodeStatusPending
	}

	edges := e.config.GetEdges()
	for _, edge := range edges {
		if _, exists := e.nodeMap[edge.From]; !exists {
			return fmt.Errorf("source node not found: %s", edge.From)
		}
		if _, exists := e.nodeMap[edge.To]; !exists {
			return fmt.Errorf("target node not found: %s", edge.To)
		}
		e.graph[edge.From] = append(e.graph[edge.From], edge.To)
		e.inDegree[edge.To]++
	}

	for nodeID, degree := range e.inDegree {
		e.initialInDegree[nodeID] = degree
	}

	return nil
}

// GetExecutableNodes returns all nodes that are ready to execute
// A node is executable if:
// 1. Its status is pending
// 2. All its dependencies are met:
//   - Dependency succeeded, OR
//   - Dependency failed AND this edge has ignore_failure=true
func (e *DAGEngine) GetExecutableNodes() []string {
	var result []string

	for _, node := range e.config.Nodes {
		nodeID := node.ID

		// Only pending nodes can be executed
		if e.nodeStatus[nodeID] != NodeStatusPending {
			continue
		}

		allDependenciesMet := true
		for _, edge := range e.config.GetEdges() {
			if edge.To == nodeID {
				depStatus := e.nodeStatus[edge.From]

				// If dependency is still running or pending, we cannot execute yet
				if depStatus == NodeStatusPending || depStatus == NodeStatusRunning {
					allDependenciesMet = false
					break
				}

				// If dependency succeeded, it's met
				if depStatus == NodeStatusSuccess {
					continue
				}

				// If dependency failed, check edge's IgnoreFailure
				// If edge has ignore_failure=true, dependency failure is ignored
				if depStatus == NodeStatusFailed && edge.IgnoreFailure {
					continue
				}

				// Dependency failed and edge does not ignore failure
				allDependenciesMet = false
				break
			}
		}

		if allDependenciesMet {
			result = append(result, nodeID)
		}
	}

	return result
}

func (e *DAGEngine) MarkCompleted(nodeID string, success bool, outputs map[string]interface{}) {
	e.completed[nodeID] = true

	if success {
		e.nodeStatus[nodeID] = NodeStatusSuccess
	} else {
		e.nodeStatus[nodeID] = NodeStatusFailed
	}

	if outputs != nil {
		e.outputs[nodeID] = outputs
	}

	// 无论成功还是失败，都需要递减下游节点的inDegree
	// 这样即使节点失败，下游依赖该节点的节点仍然可以继续执行
	for _, neighbor := range e.graph[nodeID] {
		e.inDegree[neighbor]--
	}
}

func (e *DAGEngine) GetNodeStatus(nodeID string) NodeStatus {
	return e.nodeStatus[nodeID]
}

// HasFailedNodesBlockingExecution checks if there are any failed nodes
// that are blocking the execution of pending nodes.
// A failed node blocks execution if:
// 1. There is a pending node that depends on it
// 2. The edge does NOT have ignore_failure=true
func (e *DAGEngine) HasFailedNodesBlockingExecution() bool {
	for _, node := range e.config.Nodes {
		if e.nodeStatus[node.ID] == NodeStatusPending {
			for _, edge := range e.config.GetEdges() {
				if edge.To == node.ID {
					depStatus := e.nodeStatus[edge.From]

					// If dependency is still pending or running, it's not blocking yet
					// We're waiting for it to complete
					if depStatus == NodeStatusPending || depStatus == NodeStatusRunning {
						break
					}
					// If dependency failed and edge does not ignore failure, it's blocking
					if depStatus == NodeStatusFailed && !edge.IgnoreFailure {
						return true
					}
				}
			}
		}
	}
	return false
}

func (e *DAGEngine) IsCompleted() bool {
	return len(e.completed) == len(e.config.Nodes)
}

func (e *DAGEngine) GetNode(nodeID string) *PipelineNode {
	return e.nodeMap[nodeID]
}

func (e *DAGEngine) GetNodeOutput(nodeID string) map[string]interface{} {
	return e.outputs[nodeID]
}

type PipelineAssignMessage struct {
	RunID       uint64         `json:"run_id"`
	Config      PipelineConfig `json:"config"`
	AgentConfig AgentConfig    `json:"agent_config"`
}

type AgentConfig struct {
	Workspace string            `json:"workspace"`
	Timeout   int               `json:"timeout"`
	EnvVars   map[string]string `json:"env_vars"`
}

func ParsePipelineAssign(data []byte) (*PipelineAssignMessage, error) {
	var msg PipelineAssignMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("failed to parse pipeline assign message: %w", err)
	}
	return &msg, nil
}
