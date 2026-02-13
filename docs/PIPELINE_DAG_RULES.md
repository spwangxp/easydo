# 流水线任务 DAG 规则规范

**版本**: v1.0  
**制定日期**: 2026-02-08  
**适用范围**: EasyDo 流水线任务编排系统

---

## 一、理论基础：DAG（有向无环图）定义

### 1.1 数学定义

**有向无环图（Directed Acyclic Graph, DAG）** 是一个满足以下条件的有向图：

```
设 G = (V, E)，其中：
- V 是节点集合（Vertices）
- E 是边集合（Edges），E ⊆ V × V

DAG 必须满足：
1. 有向性：∀(u,v) ∈ E，方向从 u 指向 v
2. 无环性：不存在路径 v₁ → v₂ → ... → vₙ，其中 v₁ = vₙ
```

### 1.2 流水线场景的映射

| DAG 概念 | 流水线概念 | 说明 |
|---------|-----------|------|
| 节点 (V) | 任务节点 | 具体的构建、测试、部署任务 |
| 边 (E) | 依赖关系 | 任务间的执行顺序依赖 |
| 入度 (In-degree) | 前置依赖数 | 当前任务依赖的其他任务数量 |
| 出度 (Out-degree) | 后置依赖数 | 依赖当前任务的其他任务数量 |
| 拓扑排序 | 执行顺序 | 任务的实际执行序列 |

---

## 二、强制规则（必须满足）

### 2.1 节点规则

#### R1: 节点标识唯一性
- **规则**: 所有节点必须拥有唯一的 ID
- **验证**: 不允许重复的节点 ID
- **错误信息**: "节点ID '{id}' 重复"

```go
// 伪代码
nodeIDs := make(map[string]bool)
for _, node := range nodes {
    if nodeIDs[node.ID] {
        return false, fmt.Sprintf("节点ID '%s' 重复", node.ID)
    }
    nodeIDs[node.ID] = true
}
```

#### R2: 节点ID非空性
- **规则**: 节点 ID 不能为空字符串
- **验证**: 每个节点必须有非空 ID
- **错误信息**: "节点ID不能为空"

#### R3: 节点数量限制
- **规则**: 流水线必须至少包含 1 个节点
- **验证**: `len(nodes) >= 1`
- **错误信息**: "节点列表为空"

### 2.2 边规则

#### R4: 边的方向性
- **规则**: 每条边必须有明确的源节点（from）和目标节点（to）
- **验证**: from 和 to 必须指向存在的节点
- **错误信息**: 
  - "边引用的源节点 '{id}' 不存在"
  - "边引用的目标节点 '{id}' 不存在"

#### R5: 禁止自环
- **规则**: 节点不能依赖自身
- **验证**: `edge.From != edge.To`
- **错误信息**: "节点 '{id}' 不能自引用"
- **原因**: 自环会导致任务永远无法执行完成

#### R6: 禁止重复边
- **规则**: 相同的 from→to 边只能出现一次
- **验证**: 边集合中不能存在重复
- **错误信息**: "边 '{from}->{to}' 重复"

### 2.3 连通性规则

#### R7: 单节点无边许可
- **规则**: 单节点流水线可以没有边（简单任务场景）
- **验证**: `len(nodes) == 1 && len(edges) == 0` → 合法
- **示例**: 只有一个 Shell 脚本的定时任务

#### R8: 多节点必须有边
- **规则**: 多节点流水线必须至少包含一条依赖边
- **验证**: `len(nodes) > 1 && len(edges) == 0` → 非法
- **错误信息**: "多节点流水线必须包含依赖边"
- **原因**: 无边则无法确定执行顺序，违背流水线设计初衷

#### R9: 节点可达性
- **规则**: 所有节点必须能从至少一个入口节点到达
- **验证**: 通过 BFS/DFS 检查所有节点的可达性
- **错误信息**: "存在不可达节点: [id1, id2, ...]"
- **原因**: 不可达节点永远不会被执行，是无效配置

**可达性算法**:
```go
// 1. 找出所有入口节点（入度为0）
entryPoints := []string{}
for nodeID, degree := range inDegree {
    if degree == 0 {
        entryPoints = append(entryPoints, nodeID)
    }
}

// 2. BFS 遍历
reachable := make(map[string]bool)
queue := entryPoints
for len(queue) > 0 {
    current := queue[0]
    queue = queue[1:]
    reachable[current] = true
    
    for _, neighbor := range adjacency[current] {
        if !reachable[neighbor] {
            queue = append(queue, neighbor)
        }
    }
}

// 3. 检查所有节点是否可达
for _, node := range nodes {
    if !reachable[node.ID] {
        return false, fmt.Sprintf("存在不可达节点: %s", node.ID)
    }
}
```

### 2.4 无环性规则

#### R10: 禁止循环依赖
- **规则**: 流水线中不能存在循环依赖
- **验证**: 使用 Kahn 算法或 DFS 检测环
- **错误信息**: "检测到循环依赖"
- **原因**: 循环依赖会导致死锁，任务永远无法执行

**Kahn 算法检测**:
```go
// 拓扑排序检测
queue := []string{} // 入度为0的节点
for nodeID, degree := range inDegree {
    if degree == 0 {
        queue = append(queue, nodeID)
    }
}

visitedCount := 0
for len(queue) > 0 {
    current := queue[0]
    queue = queue[1:]
    visitedCount++
    
    for _, neighbor := range adjacency[current] {
        inDegree[neighbor]--
        if inDegree[neighbor] == 0 {
            queue = append(queue, neighbor)
        }
    }
}

// 如果访问的节点数不等于总节点数，说明存在环
if visitedCount != len(nodes) {
    return false, "检测到循环依赖"
}
```

### 2.5 执行完整性规则

#### R11: 至少一个入口节点
- **规则**: 流水线必须至少有一个入度为 0 的节点（起始任务）
- **验证**: 检查入度为 0 的节点数量 >= 1
- **错误信息**: "流水线必须至少有一个起始任务"
- **原因**: 没有入口节点，整个流水线无法开始执行

#### R12: 至少一个出口节点
- **规则**: 流水线必须至少有一个出度为 0 的节点（结束任务）
- **验证**: 检查出度为 0 的节点数量 >= 1
- **错误信息**: "流水线必须至少有一个结束任务"
- **原因**: 没有出口节点，流水线永远不会结束

---

## 三、建议规则（最佳实践）

### 3.1 规模限制（建议）

#### S1: 节点数量上限
- **建议**: 单个流水线的节点数量不超过 50 个
- **原因**: 
  - 过多节点会导致维护困难
  - 执行时间难以预测
  - 故障排查复杂度增加

#### S2: 依赖深度限制
- **建议**: 依赖链深度不超过 10 层
- **原因**: 
  - 过深的依赖链增加执行风险
  - 任一节点失败影响范围过大
  - 不利于并行化执行

#### S3: 并行度限制
- **建议**: 单个节点的直接后继节点数不超过 10 个
- **原因**: 
  - 过多的并行任务可能超出系统资源限制
  - 可能导致下游服务压力过载

### 3.2 结构建议（建议）

#### S4: 避免钻石依赖反模式
```
反模式示例：
    A
   / \
  B   C
   \ /
    D

其中 B 和 C 都依赖 A，D 同时依赖 B 和 C
这种情况 D 会等待 B 和 C 都完成后才执行，
但 B 和 C 是独立的，可能导致资源浪费

建议：明确依赖关系，避免过度复杂的并行
```

#### S5: 任务分组建议
- **建议**: 相关的任务尽量放在相邻的层级
- **示例**: 
  - 所有代码检出任务放在第一层
  - 所有构建任务放在第二层
  - 所有测试任务放在第三层
  - 所有部署任务放在第四层

### 3.3 命名规范（建议）

#### S6: 节点命名规范
- **建议**: 使用有意义的节点名称
- **格式**: `[动作]-[对象]-[环境]`
- **示例**:
  - `clone-frontend-code`
  - `build-backend-service`
  - `deploy-to-production`

#### S7: 节点 ID 规范
- **建议**: 使用简洁、有意义的 ID
- **格式**: 小写字母、数字、连字符
- **示例**:
  - `step-1-clone`
  - `step-2-build-frontend`
  - `step-3-deploy`

---

## 四、验证算法流程

### 4.1 完整验证流程

```go
func ValidatePipeline(nodes []Node, edges []Edge) (bool, []string) {
    errors := []string{}
    
    // Step 1: 基础检查
    if len(nodes) == 0 {
        errors = append(errors, "节点列表为空")
        return false, errors
    }
    
    // Step 2: 节点唯一性检查
    nodeMap := make(map[string]Node)
    for _, node := range nodes {
        if node.ID == "" {
            errors = append(errors, "节点ID不能为空")
            continue
        }
        if _, exists := nodeMap[node.ID]; exists {
            errors = append(errors, fmt.Sprintf("节点ID '%s' 重复", node.ID))
        }
        nodeMap[node.ID] = node
    }
    
    if len(errors) > 0 {
        return false, errors
    }
    
    // Step 3: 连通性规则检查
    if len(nodes) == 1 && len(edges) == 0 {
        // 单节点无边是合法的
        return true, nil
    }
    
    if len(nodes) > 1 && len(edges) == 0 {
        errors = append(errors, "多节点流水线必须包含依赖边")
        return false, errors
    }
    
    // Step 4: 边有效性检查
    edgeSet := make(map[string]bool)
    for _, edge := range edges {
        // 检查自环
        if edge.From == edge.To {
            errors = append(errors, fmt.Sprintf("节点 '%s' 不能自引用", edge.From))
            continue
        }
        
        // 检查节点存在性
        if _, exists := nodeMap[edge.From]; !exists {
            errors = append(errors, fmt.Sprintf("边引用的源节点 '%s' 不存在", edge.From))
            continue
        }
        if _, exists := nodeMap[edge.To]; !exists {
            errors = append(errors, fmt.Sprintf("边引用的目标节点 '%s' 不存在", edge.To))
            continue
        }
        
        // 检查重复边
        edgeKey := fmt.Sprintf("%s->%s", edge.From, edge.To)
        if edgeSet[edgeKey] {
            errors = append(errors, fmt.Sprintf("边 '%s' 重复", edgeKey))
        }
        edgeSet[edgeKey] = true
    }
    
    if len(errors) > 0 {
        return false, errors
    }
    
    // Step 5: 构建图结构
    adjacency := make(map[string][]string)
    inDegree := make(map[string]int)
    outDegree := make(map[string]int)
    
    for _, node := range nodes {
        adjacency[node.ID] = []string{}
        inDegree[node.ID] = 0
        outDegree[node.ID] = 0
    }
    
    for _, edge := range edges {
        adjacency[edge.From] = append(adjacency[edge.From], edge.To)
        inDegree[edge.To]++
        outDegree[edge.From]++
    }
    
    // Step 6: 检查入口节点
    entryPoints := []string{}
    for nodeID, degree := range inDegree {
        if degree == 0 {
            entryPoints = append(entryPoints, nodeID)
        }
    }
    
    if len(entryPoints) == 0 {
        errors = append(errors, "流水线必须至少有一个起始任务")
    }
    
    // Step 7: 检查出口节点
    exitPoints := []string{}
    for nodeID, degree := range outDegree {
        if degree == 0 {
            exitPoints = append(exitPoints, nodeID)
        }
    }
    
    if len(exitPoints) == 0 {
        errors = append(errors, "流水线必须至少有一个结束任务")
    }
    
    // Step 8: 可达性检查
    reachable := make(map[string]bool)
    queue := append([]string{}, entryPoints...)
    
    for len(queue) > 0 {
        current := queue[0]
        queue = queue[1:]
        
        if reachable[current] {
            continue
        }
        reachable[current] = true
        
        for _, neighbor := range adjacency[current] {
            if !reachable[neighbor] {
                queue = append(queue, neighbor)
            }
        }
    }
    
    unreachableNodes := []string{}
    for _, node := range nodes {
        if !reachable[node.ID] {
            unreachableNodes = append(unreachableNodes, node.ID)
        }
    }
    
    if len(unreachableNodes) > 0 {
        errors = append(errors, fmt.Sprintf("存在不可达节点: %v", unreachableNodes))
    }
    
    // Step 9: 无环性检查（Kahn算法）
    tempInDegree := make(map[string]int)
    for k, v := range inDegree {
        tempInDegree[k] = v
    }
    
    queue = []string{}
    for nodeID, degree := range tempInDegree {
        if degree == 0 {
            queue = append(queue, nodeID)
        }
    }
    
    visitedCount := 0
    for len(queue) > 0 {
        current := queue[0]
        queue = queue[1:]
        visitedCount++
        
        for _, neighbor := range adjacency[current] {
            tempInDegree[neighbor]--
            if tempInDegree[neighbor] == 0 {
                queue = append(queue, neighbor)
            }
        }
    }
    
    if visitedCount != len(nodes) {
        errors = append(errors, "检测到循环依赖")
    }
    
    if len(errors) > 0 {
        return false, errors
    }
    
    return true, nil
}
```

---

## 五、合法与非法示例

### 5.1 合法示例

#### E1: 单节点流水线
```json
{
  "nodes": [{"id": "1", "type": "shell", "name": "单任务"}],
  "edges": []
}
```

#### E2: 串行流水线
```json
{
  "nodes": [
    {"id": "1", "type": "shell", "name": "检出"},
    {"id": "2", "type": "shell", "name": "构建"},
    {"id": "3", "type": "shell", "name": "测试"}
  ],
  "edges": [
    {"from": "1", "to": "2"},
    {"from": "2", "to": "3"}
  ]
}
```

#### E3: 并行流水线
```json
{
  "nodes": [
    {"id": "1", "type": "shell", "name": "检出"},
    {"id": "2", "type": "shell", "name": "构建前端"},
    {"id": "3", "type": "shell", "name": "构建后端"},
    {"id": "4", "type": "shell", "name": "部署"}
  ],
  "edges": [
    {"from": "1", "to": "2"},
    {"from": "1", "to": "3"},
    {"from": "2", "to": "4"},
    {"from": "3", "to": "4"}
  ]
}
```

### 5.2 非法示例

#### E4: 空流水线
```json
{
  "nodes": [],
  "edges": []
}
```
**错误**: "节点列表为空"

#### E5: 多节点无边
```json
{
  "nodes": [
    {"id": "1", "type": "shell"},
    {"id": "2", "type": "shell"}
  ],
  "edges": []
}
```
**错误**: "多节点流水线必须包含依赖边"

#### E6: 自环依赖
```json
{
  "nodes": [{"id": "1", "type": "shell"}],
  "edges": [{"from": "1", "to": "1"}]
}
```
**错误**: "节点 '1' 不能自引用"

#### E7: 循环依赖
```json
{
  "nodes": [
    {"id": "1", "type": "shell"},
    {"id": "2", "type": "shell"},
    {"id": "3", "type": "shell"}
  ],
  "edges": [
    {"from": "1", "to": "2"},
    {"from": "2", "to": "3"},
    {"from": "3", "to": "1"}
  ]
}
```
**错误**: "检测到循环依赖"

#### E8: 不可达节点
```json
{
  "nodes": [
    {"id": "1", "type": "shell"},
    {"id": "2", "type": "shell"},
    {"id": "3", "type": "shell"}
  ],
  "edges": [
    {"from": "1", "to": "2"}
    // 节点 3 没有连接到图中
  ]
}
```
**错误**: "存在不可达节点: [3]"

#### E9: 无入口节点
```json
{
  "nodes": [
    {"id": "1", "type": "shell"},
    {"id": "2", "type": "shell"}
  ],
  "edges": [
    {"from": "1", "to": "2"},
    {"from": "2", "to": "1"}
  ]
}
```
**错误**: 
- "检测到循环依赖"
- "流水线必须至少有一个起始任务"

---

## 六、错误代码与处理建议

| 错误代码 | 错误描述 | 处理建议 |
|---------|---------|---------|
| E001 | 节点列表为空 | 至少添加一个任务节点 |
| E002 | 节点ID为空 | 为所有节点分配唯一ID |
| E003 | 节点ID重复 | 修改重复的节点ID |
| E004 | 多节点无边 | 添加节点间的依赖关系 |
| E005 | 边引用不存在的源节点 | 检查边的from字段 |
| E006 | 边引用不存在的目标节点 | 检查边的to字段 |
| E007 | 节点自引用 | 移除自环边或重新设计依赖 |
| E008 | 边重复 | 删除重复的依赖边 |
| E009 | 存在不可达节点 | 将孤立节点连接到依赖图中 |
| E010 | 检测到循环依赖 | 检查并打破循环依赖链 |
| E011 | 无起始任务 | 确保至少有一个入度为0的节点 |
| E012 | 无结束任务 | 确保至少有一个出度为0的节点 |

---

## 七、总结

本规则规范基于DAG的数学定义，结合流水线任务的实际场景，制定了**12条强制规则**和**7条建议规则**。

**核心原则**:
1. **唯一性**: 每个节点必须有唯一标识
2. **连通性**: 所有节点必须在依赖图中可达
3. **无环性**: 不能存在循环依赖
4. **完整性**: 必须有明确的起始和结束任务
5. **合理性**: 多节点必须有依赖边，单节点可无依赖

通过严格执行这些规则，可以确保流水线的正确性、可执行性和可维护性。
