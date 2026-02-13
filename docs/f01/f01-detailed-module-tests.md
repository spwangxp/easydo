# F01 核心模块详细测试用例

## 文档信息
- **模块数量**: 5个核心模块
- **测试用例总数**: 待统计
- **创建日期**: 2026-02-02
- **目标读者**: 开发人员、测试人员

---

## 目录

1. [DAG执行模块测试用例](#1-dag执行模块测试用例)
2. [变量系统模块测试用例](#2-变量系统模块测试用例)
3. [日志系统模块测试用例](#3-日志系统模块测试用例)
4. [Agent调度模块测试用例](#4-agent调度模块测试用例)
5. [通信模块测试用例](#5-通信模块测试用例)

---

## 1. DAG执行模块测试用例

### 1.1 DAG验证测试

#### DAG-TEST-001: 有效DAG验证（必过）
**用例ID**: DAG-TEST-001  
**优先级**: P0  
**模块**: DAG执行

**前置条件**: 无

**输入**:
```go
config := PipelineConfig{
    Nodes: []PipelineNode{
        {ID: "build", Type: "shell"},
        {ID: "test", Type: "shell"},
        {ID: "deploy", Type: "shell"},
    },
    Edges: []Edge{
        {From: "build", To: "test"},
        {From: "test", To: "deploy"},
    },
}
```

**操作**:
```go
err := dagEngine.ValidateDAG(config.Nodes, config.Edges)
```

**预期结果**:
- 返回 nil（验证通过）
- 无错误

**验证方法**:
```go
assert.NoError(t, err)
```

**测试数据**:
- 线性依赖: A → B → C
- 并行依赖: A → (B, C), B → D, C → D
- 菱形依赖: start → (task1, task2) → end

---

#### DAG-TEST-002: 循环依赖检测（必过）
**用例ID**: DAG-TEST-002  
**优先级**: P0  
**模块**: DAG执行

**输入**:
```go
config := PipelineConfig{
    Nodes: []PipelineNode{
        {ID: "a", Type: "shell"},
        {ID: "b", Type: "shell"},
        {ID: "c", Type: "shell"},
    },
    Edges: []Edge{
        {From: "a", To: "b"},
        {From: "b", To: "c"},
        {From: "c", To: "a"},  // 形成环
    },
}
```

**操作**:
```go
err := dagEngine.ValidateDAG(config.Nodes, config.Edges)
```

**预期结果**:
- 返回错误: "cyclic dependency detected"
- 错误包含循环路径信息

**验证方法**:
```go
assert.Error(t, err)
assert.Contains(t, err.Error(), "cyclic")
assert.Contains(t, err.Error(), "a -> b -> c -> a")
```

**测试场景**:
| 场景 | 边配置 |
|------|--------|
| 简单环 | A→B→C→A |
| 多环 | A→B→C→A, A→D→A |
| 自环 | A→A |

---

#### DAG-TEST-003: 多入口节点DAG（必过）
**用例ID**: DAG-TEST-003  
**优先级**: P0  
**模块**: DAG执行

**输入**:
```go
config := PipelineConfig{
    Nodes: []PipelineNode{
        {ID: "init", Type: "shell"},
        {ID: "build", Type: "shell"},
        {ID: "deploy", Type: "shell"},
    },
    Edges: []Edge{
        {From: "init", To: "build"},
        {From: "init", To: "deploy"},
    },
}
```

**操作**:
```go
err := dagEngine.ValidateDAG(config.Nodes, config.Edges)
```

**预期结果**:
- 验证通过
- init 节点被识别为入口节点

**验证方法**:
```go
assert.NoError(t, err)
assert.Len(t, dagEngine.GetEntranceNodes(), 1)
assert.Equal(t, "init", dagEngine.GetEntranceNodes()[0])
```

---

#### DAG-TEST-004: 无效边引用（必过）
**用例ID**: DAG-TEST-004  
**优先级**: P0  
**模块**: DAG执行

**输入**:
```go
config := PipelineConfig{
    Nodes: []PipelineNode{
        {ID: "build", Type: "shell"},
    },
    Edges: []Edge{
        {From: "build", To: "nonexistent"},  // 引用不存在的节点
    },
}
```

**操作**:
```go
err := dagEngine.ValidateDAG(config.Nodes, config.Edges)
```

**预期结果**:
- 返回错误: "edge references non-existent node: nonexistent"

**验证方法**:
```go
assert.Error(t, err)
assert.Contains(t, err.Error(), "nonexistent")
```

---

#### DAG-TEST-005: 孤岛节点检测（推荐）
**用例ID**: DAG-TEST-005  
**优先级**: P1  
**模块**: DAG执行

**输入**:
```go
config := PipelineConfig{
    Nodes: []PipelineNode{
        {ID: "a", Type: "shell"},
        {ID: "b", Type: "shell"},
        {ID: "c", Type: "shell"},
    },
    Edges: []Edge{
        {From: "a", To: "b"},  // c是孤岛节点
    },
}
```

**操作**:
```go
err := dagEngine.ValidateDAG(config.Nodes, config.Edges)
```

**预期结果**:
- 验证通过（孤岛节点是允许的）
- 或返回警告

**验证方法**:
```go
// 孤岛节点是允许的
assert.NoError(t, err)
warnings := dagEngine.GetWarnings()
assert.Contains(t, warnings, "isolated node: c")
```

---

### 1.2 层级计算测试

#### DAG-TEST-010: 线性DAG层级计算（必过）
**用例ID**: DAG-TEST-010  
**优先级**: P0  
**模块**: DAG执行

**输入**:
```go
config := PipelineConfig{
    Nodes: []PipelineNode{
        {ID: "a", Type: "shell"},
        {ID: "b", Type: "shell"},
        {ID: "c", Type: "shell"},
    },
    Edges: []Edge{
        {From: "a", To: "b"},
        {From: "b", To: "c"},
    },
}
```

**操作**:
```go
plan := dagEngine.BuildExecutionPlan(config.Nodes, config.Edges)
```

**预期结果**:
| 节点 | 层级 |
|------|------|
| a | 0 |
| b | 1 |
| c | 2 |

**验证方法**:
```go
assert.Equal(t, 0, plan.GetLevel("a"))
assert.Equal(t, 1, plan.GetLevel("b"))
assert.Equal(t, 2, plan.GetLevel("c"))
assert.Equal(t, 3, plan.MaxLevel)  // 最大层级+1
```

---

#### DAG-TEST-011: 并行DAG层级计算（必过）
**用例ID**: DAG-TEST-011  
**优先级**: P0  
**模块**: DAG执行

**输入**:
```go
// A → C
// B → C
config := PipelineConfig{
    Nodes: []PipelineNode{
        {ID: "a", Type: "shell"},
        {ID: "b", Type: "shell"},
        {ID: "c", Type: "shell"},
    },
    Edges: []Edge{
        {From: "a", To: "c"},
        {From: "b", To: "c"},
    },
}
```

**操作**:
```go
plan := dagEngine.BuildExecutionPlan(config.Nodes, config.Edges)
```

**预期结果**:
| 节点 | 层级 |
|------|------|
| a | 0 |
| b | 0 |
| c | 1 |

**验证方法**:
```go
assert.Equal(t, 0, plan.GetLevel("a"))
assert.Equal(t, 0, plan.GetLevel("b"))
assert.Equal(t, 1, plan.GetLevel("c"))
assert.Len(t, plan.GetNodesAtLevel(0), 2)  // Level 0有2个节点
assert.Len(t, plan.GetNodesAtLevel(1), 1)  // Level 1有1个节点
```

---

#### DAG-TEST-012: 复杂DAG层级计算（必过）
**用例ID**: DAG-TEST-012  
**优先级**: P0  
**模块**: DAG执行

**输入**:
```go
//     A → B ─┐
//     ↓       ↓
//     C → D ─┘
config := PipelineConfig{
    Nodes: []PipelineNode{
        {ID: "a", Type: "shell"},
        {ID: "b", Type: "shell"},
        {ID: "c", Type: "shell"},
        {ID: "d", Type: "shell"},
    },
    Edges: []Edge{
        {From: "a", To: "b"},
        {From: "a", To: "c"},
        {From: "b", To: "d"},
        {From: "c", To: "d"},
    },
}
```

**操作**:
```go
plan := dagEngine.BuildExecutionPlan(config.Nodes, config.Edges)
```

**预期结果**:
| 节点 | 层级 |
|------|------|
| a | 0 |
| b | 1 |
| c | 1 |
| d | 2 |

**验证方法**:
```go
assert.Equal(t, 0, plan.GetLevel("a"))
assert.Equal(t, 1, plan.GetLevel("b"))
assert.Equal(t, 1, plan.GetLevel("c"))
assert.Equal(t, 2, plan.GetLevel("d"))
```

---

#### DAG-TEST-013: 深层DAG层级计算（推荐）
**用例ID**: DAG-TEST-013  
**优先级**: P1  
**模块**: DAG执行

**输入**:
```go
// 10层深的线性DAG
nodes := make([]PipelineNode, 10)
edges := make([]Edge, 9)
for i := 0; i < 10; i++ {
    nodes[i] = PipelineNode{ID: fmt.Sprintf("node_%d", i)}
    if i < 9 {
        edges[i] = Edge{From: fmt.Sprintf("node_%d", i), To: fmt.Sprintf("node_%d", i+1)}
    }
}
```

**操作**:
```go
plan := dagEngine.BuildExecutionPlan(nodes, edges)
```

**预期结果**:
- node_0.level = 0
- node_9.level = 9
- MaxLevel = 10

**验证方法**:
```go
for i := 0; i < 10; i++ {
    assert.Equal(t, i, plan.GetLevel(fmt.Sprintf("node_%d", i)))
}
assert.Equal(t, 10, plan.MaxLevel)
```

---

### 1.3 执行顺序测试

#### DAG-TEST-020: 串行执行顺序（必过）
**用例ID**: DAG-TEST-020  
**优先级**: P0  
**模块**: DAG执行

**输入**:
```go
// A → B → C
config := PipelineConfig{
    Nodes: []PipelineNode{
        {ID: "a", Type: "shell", Config: map[string]interface{}{"script": "echo A"}},
        {ID: "b", Type: "shell", Config: map[string]interface{}{"script": "echo B"}},
        {ID: "c", Type: "shell", Config: map[string]interface{}{"script": "echo C"}},
    },
    Edges: []Edge{
        {From: "a", To: "b"},
        {From: "b", To: "c"},
    },
}
```

**操作**:
```go
run := dagEngine.Execute(config)
```

**预期结果**:
- 执行顺序: A → B → C
- 总时长 ≈ A时长 + B时长 + C时长
- 无并行执行

**验证方法**:
```go
order := run.GetExecutionOrder()
assert.Equal(t, "a", order[0].NodeID)
assert.Equal(t, "b", order[1].NodeID)
assert.Equal(t, "c", order[2].NodeID)

assert.True(t, order[1].StartTime >= order[0].EndTime)
assert.True(t, order[2].StartTime >= order[1].EndTime)
```

---

#### DAG-TEST-021: 并行执行顺序（必过）
**用例ID**: DAG-TEST-021  
**优先级**: P0  
**模块**: DAG执行

**输入**:
```go
// A → C
// B → C
config := PipelineConfig{
    Nodes: []PipelineNode{
        {ID: "a", Type: "shell", Config: map[string]interface{}{"script": "sleep 2 && echo A"}},
        {ID: "b", Type: "shell", Config: map[string]interface{}{"script": "sleep 2 && echo B"}},
        {ID: "c", Type: "shell", Config: map[string]interface{}{"script": "echo C"}},
    },
    Edges: []Edge{
        {From: "a", To: "c"},
        {From: "b", To: "c"},
    },
}
```

**操作**:
```go
run := dagEngine.Execute(config)
```

**预期结果**:
- A和B并行执行（时间重叠）
- C在A和B完成后执行
- 总时长 ≈ 2秒（A/B执行时间）+ C时长

**验证方法**:
```go
a := run.GetTask("a")
b := run.GetTask("b")
c := run.GetTask("c")

// A和B并行
assert.True(t, a.StartTime < b.EndTime)
assert.True(t, b.StartTime < a.EndTime)

// C在A和B完成后
assert.True(t, c.StartTime >= a.EndTime)
assert.True(t, c.StartTime >= b.EndTime)

// 总时长
assert.True(t, run.Duration < 6)  // 小于顺序执行的6秒
```

---

#### DAG-TEST-022: 混合执行顺序（必过）
**用例ID**: DAG-TEST-022  
**优先级**: P0  
**模块**: DAG执行

**输入**:
```go
//     A → C → E
//     ↓   ↓
//     B → D
config := PipelineConfig{
    Nodes: []PipelineNode{
        {ID: "a", Type: "shell"},
        {ID: "b", Type: "shell"},
        {ID: "c", Type: "shell"},
        {ID: "d", Type: "shell"},
        {ID: "e", Type: "shell"},
    },
    Edges: []Edge{
        {From: "a", To: "c"},
        {From: "a", To: "b"},
        {From: "b", To: "d"},
        {From: "c", To: "e"},
        {From: "d", To: "e"},
    },
}
```

**操作**:
```go
run := dagEngine.Execute(config)
```

**预期结果**:
- Level 0: A, B 并行
- Level 1: C, D 并行（A完成后C，B完成后D）
- Level 2: E（C和D完成后）

**验证方法**:
```go
level0 := run.GetTasksAtLevel(0)
level1 := run.GetTasksAtLevel(1)
level2 := run.GetTasksAtLevel(2)

assert.Len(t, level0, 2)  // A, B
assert.Len(t, level1, 2)  // C, D
assert.Len(t, level2, 1)  // E
```

---

### 1.4 失败处理测试

#### DAG-TEST-030: 单任务失败（必过）
**用例ID**: DAG-TEST-030  
**优先级**: P0  
**模块**: DAG执行

**输入**:
```go
// A → B → C，B失败
config := PipelineConfig{
    Nodes: []PipelineNode{
        {ID: "a", Type: "shell", Config: map[string]interface{}{"script": "exit 0"}},
        {ID: "b", Type: "shell", Config: map[string]interface{}{"script": "exit 1"}},
        {ID: "c", Type: "shell", Config: map[string]interface{}{"script": "exit 0"}},
    },
    Edges: []Edge{
        {From: "a", To: "b"},
        {From: "b", To: "c"},
    },
}
```

**操作**:
```go
run := dagEngine.Execute(config)
```

**预期结果**:
- A 执行成功
- B 执行失败
- C 不执行
- PipelineRun 失败

**验证方法**:
```go
assert.Equal(t, "success", run.GetTask("a").Status)
assert.Equal(t, "failed", run.GetTask("b").Status)
assert.Equal(t, "pending", run.GetTask("c").Status)  // 或 skipped
assert.Equal(t, "failed", run.Status)
```

---

#### DAG-TEST-031: AllowFailure任务失败（必过）
**用例ID**: DAG-TEST-031  
**优先级**: P0  
**模块**: DAG执行

**输入**:
```go
// A(AllowFailure=true) → B
config := PipelineConfig{
    Nodes: []PipelineNode{
        {ID: "a", Type: "shell", AllowFailure: true, Config: map[string]interface{}{"script": "exit 1"}},
        {ID: "b", Type: "shell", Config: map[string]interface{}{"script": "exit 0"}},
    },
    Edges: []Edge{
        {From: "a", To: "b"},
    },
}
```

**操作**:
```go
run := dagEngine.Execute(config)
```

**预期结果**:
- A 失败但标记为 warning
- B 正常执行
- PipelineRun 成功或 warning

**验证方法**:
```go
assert.Equal(t, "failed", run.GetTask("a").Status)
assert.Equal(t, "warning", run.GetTask("a").ErrorCategory)
assert.Equal(t, "success", run.GetTask("b").Status)
assert.Equal(t, "warning", run.Status)
```

---

#### DAG-TEST-032: 条件跳过（必过）
**用例ID**: DAG-TEST-032  
**优先级**: P0  
**模块**: DAG执行

**输入**:
```go
// A → B(When: A.status == "success") → C
config := PipelineConfig{
    Nodes: []PipelineNode{
        {ID: "a", Type: "shell", Config: map[string]interface{}{"script": "exit 0"}},
        {ID: "b", Type: "shell", When: "${outputs.a.status} == \"success\"", Config: map[string]interface{}{"script": "exit 0"}},
        {ID: "c", Type: "shell", Config: map[string]interface{}{"script": "exit 0"}},
    },
    Edges: []Edge{
        {From: "a", To: "b"},
        {From: "b", To: "c"},
    },
}
```

**操作**:
```go
run := dagEngine.Execute(config)
```

**预期结果**:
- A 执行成功
- B 条件满足，执行
- C 执行

**验证方法**:
```go
assert.Equal(t, "success", run.GetTask("a").Status)
assert.Equal(t, "success", run.GetTask("b").Status)
assert.Equal(t, "success", run.GetTask("c").Status)
```

---

#### DAG-TEST-033: 条件不满足跳过（必过）
**用例ID**: DAG-TEST-033  
**优先级**: P0  
**模块**: DAG执行

**输入**:
```go
// A → B(When: A.status == "failed") → C
config := PipelineConfig{
    Nodes: []PipelineNode{
        {ID: "a", Type: "shell", Config: map[string]interface{}{"script": "exit 0"}},  // 成功
        {ID: "b", Type: "shell", When: "${outputs.a.status} == \"failed\"", Config: map[string]interface{}{"script": "exit 0"}},
        {ID: "c", Type: "shell", Config: map[string]interface{}{"script": "exit 0"}},
    },
    Edges: []Edge{
        {From: "a", To: "b"},
        {From: "b", To: "c"},
    },
}
```

**操作**:
```go
run := dagEngine.Execute(config)
```

**预期结果**:
- A 执行成功
- B 条件不满足，跳过
- C 执行（因为B跳过，依赖链断裂）

**验证方法**:
```go
assert.Equal(t, "success", run.GetTask("a").Status)
assert.Equal(t, "skipped", run.GetTask("b").Status)
assert.Equal(t, "pending", run.GetTask("c").Status)  // C未执行
```

---

### 1.5 DAG模块测试用例汇总

| 用例ID | 用例名称 | 优先级 | 类别 |
|--------|----------|--------|------|
| DAG-TEST-001 | 有效DAG验证 | P0 | 验证 |
| DAG-TEST-002 | 循环依赖检测 | P0 | 验证 |
| DAG-TEST-003 | 多入口节点 | P0 | 验证 |
| DAG-TEST-004 | 无效边引用 | P0 | 验证 |
| DAG-TEST-005 | 孤岛节点检测 | P1 | 验证 |
| DAG-TEST-010 | 线性DAG层级 | P0 | 层级 |
| DAG-TEST-011 | 并行DAG层级 | P0 | 层级 |
| DAG-TEST-012 | 复杂DAG层级 | P0 | 层级 |
| DAG-TEST-013 | 深层DAG层级 | P1 | 层级 |
| DAG-TEST-020 | 串行执行顺序 | P0 | 执行 |
| DAG-TEST-021 | 并行执行顺序 | P0 | 执行 |
| DAG-TEST-022 | 混合执行顺序 | P0 | 执行 |
| DAG-TEST-030 | 单任务失败 | P0 | 失败 |
| DAG-TEST-031 | AllowFailure失败 | P0 | 失败 |
| DAG-TEST-032 | 条件满足执行 | P0 | 失败 |
| DAG-TEST-033 | 条件不满足跳过 | P0 | 失败 |

**DAG模块测试用例总计**: 16个（P0: 14, P1: 2）

---

## 2. 变量系统模块测试用例

### 2.1 变量解析测试

#### VAR-TEST-001: 环境变量解析（必过）
**用例ID**: VAR-TEST-001  
**优先级**: P0  
**模块**: 变量系统

**输入**:
```go
script := `
echo "Build Number: $BUILD_NUMBER"
echo "Run ID: $RUN_ID"
echo "Workspace: $WORKSPACE"
`
config := map[string]interface{}{
    "script": script,
}
```

**操作**:
```go
resolver := NewVariableResolver()
resolver.SetEnvVars(map[string]string{
    "BUILD_NUMBER": "123",
    "RUN_ID":       "456",
    "WORKSPACE":    "/workspace",
})

resolvedScript, err := resolver.ResolveVariables(script)
```

**预期结果**:
- 变量被正确替换
- 输出包含实际值

**验证方法**:
```go
assert.NoError(t, err)
assert.Contains(t, resolvedScript, "123")
assert.Contains(t, resolvedScript, "456")
assert.Contains(t, resolvedScript, "/workspace")
```

---

#### VAR-TEST-002: 任务输出解析（必过）
**用例ID**: VAR-TEST-002  
**优先级**: P0  
**模块**: 变量系统

**输入**:
```go
// 上游任务输出
upstreamOutputs := map[string]interface{}{
    "build": map[string]interface{}{
        "status":    "success",
        "exit_code": 0,
        "commit_id": "abc123",
    },
}

// 下游任务脚本
script := `
echo "Building commit: ${outputs.build.commit_id}"
echo "Exit code: ${outputs.build.exit_code}"
`
```

**操作**:
```go
resolver := NewVariableResolver()
resolver.SetTaskOutput("build", upstreamOutputs["build"])

resolvedScript, err := resolver.ResolveVariables(script)
```

**预期结果**:
- ${outputs.build.commit_id} → abc123
- ${outputs.build.exit_code} → 0

**验证方法**:
```go
assert.NoError(t, err)
assert.Contains(t, resolvedScript, "abc123")
assert.Contains(t, resolvedScript, "0")
```

---

#### VAR-TEST-003: 输入变量解析（必过）
**用例ID**: VAR-TEST-003  
**优先级**: P0  
**模块**: 变量系统

**输入**:
```go
inputs := map[string]interface{}{
    "environment": "production",
    "version":     "1.0.0",
    "debug":       true,
}

script := `
export ENV=${inputs.environment}
export VERSION=${inputs.version}
export DEBUG=${inputs.debug}
`
```

**操作**:
```go
resolver := NewVariableResolver()
resolver.SetInputs(inputs)

resolvedScript, err := resolver.ResolveVariables(script)
```

**预期结果**:
- 变量被正确替换

**验证方法**:
```go
assert.NoError(t, err)
assert.Contains(t, resolvedScript, "production")
assert.Contains(t, resolvedScript, "1.0.0")
assert.Contains(t, resolvedScript, "true")
```

---

#### VAR-TEST-004: 嵌套变量解析（推荐）
**用例ID**: VAR-TEST-004  
**优先级**: P1  
**模块**: 变量系统

**输入**:
```go
// 变量值中包含另一个变量引用
taskOutput := map[string]interface{}{
    "version": "1.0",
    "full_version": "${outputs.build.version}-release",
}

script := "echo ${outputs.build.full_version}"
```

**操作**:
```go
resolver := NewVariableResolver()
resolver.SetTaskOutput("build", taskOutput)

resolvedScript, err := resolver.ResolveVariables(script)
```

**预期结果**:
- 递归解析所有变量
- 输出: 1.0-release

**验证方法**:
```go
// 可能需要多次解析
for strings.Contains(resolvedScript, "${") {
    resolvedScript, _ = resolver.ResolveVariables(resolvedScript)
}
assert.Contains(t, resolvedScript, "1.0-release")
```

---

#### VAR-TEST-005: 变量不存在处理（必过）
**用例ID**: VAR-TEST-005  
**优先级**: P0  
**模块**: 变量系统

**输入**:
```go
script := "echo ${outputs.nonexistent.field}"
```

**操作**:
```go
resolver := NewVariableResolver()
resolvedScript, err := resolver.ResolveVariables(script)
```

**预期结果**:
- 返回错误或保留原引用
- 或替换为空字符串

**验证方法**:
```go
// 三种处理方式均可接受
// 1. 返回错误
assert.Error(t, err)

// 2. 保留原引用
// assert.Contains(t, resolvedScript, "${outputs.nonexistent.field}")

// 3. 替换为空
// assert.NotContains(t, resolvedScript, "${")
```

---

### 2.2 变量传递测试

#### VAR-TEST-010: 基本变量传递（必过）
**用例ID**: VAR-TEST-010  
**优先级**: P0  
**模块**: 变量系统

**输入**:
```go
// Pipeline配置: A → B
pipeline := PipelineConfig{
    Nodes: []PipelineNode{
        {
            ID:   "build",
            Type: "shell",
            Config: map[string]interface{}{
                "script":          "echo 'version=1.0.0'",
                "output_extraction": []map[string]interface{}{
                    {"pattern": "version=(.+)", "field": "version"},
                },
            },
        },
        {
            ID:   "deploy",
            Type: "shell",
            Config: map[string]interface{}{
                "script": "echo 'Deploying version: ${outputs.build.version}'",
            },
        },
    },
    Edges: []Edge{
        {From: "build", To: "deploy"},
    },
}
```

**操作**:
```go
run := dagEngine.Execute(pipeline)
```

**预期结果**:
- build 任务输出 version=1.0.0
- deploy 任务可访问 ${outputs.build.version}

**验证方法**:
```go
build := run.GetTask("build")
assert.Equal(t, "1.0.0", build.Outputs["version"])

deploy := run.GetTask("deploy")
assert.Equal(t, "success", deploy.Status)
```

---

#### VAR-TEST-011: 多变量传递（必过）
**用例ID**: VAR-TEST-011  
**优先级**: P0  
**模块**: 变量系统

**输入**:
```go
// A输出多个变量，B引用所有变量
outputs := map[string]interface{}{
    "status":    "success",
    "exit_code": 0,
    "commit_id": "abc123",
    "branch":    "main",
    "duration":  45.5,
}

script := `
echo "Status: ${outputs.build.status}"
echo "Commit: ${outputs.build.commit_id}"
echo "Branch: ${outputs.build.branch}"
`
```

**操作**:
```go
resolver := NewVariableResolver()
resolver.SetTaskOutput("build", outputs)

resolvedScript, err := resolver.ResolveVariables(script)
```

**预期结果**:
- 所有变量都被正确替换

**验证方法**:
```go
assert.NoError(t, err)
assert.Contains(t, resolvedScript, "success")
assert.Contains(t, resolvedScript, "abc123")
assert.Contains(t, resolvedScript, "main")
```

---

#### VAR-TEST-012: 跨多任务变量传递（推荐）
**用例ID**: VAR-TEST-012  
**优先级**: P1  
**模块**: 变量系统

**输入**:
```go
// A → B → C
// A输出变量被C引用
pipeline := PipelineConfig{
    Nodes: []PipelineNode{
        {ID: "build", Type: "shell", Config: map[string]interface{}{"script": "echo 'version=2.0'"}},
        {ID: "test", Type: "shell", Config: map[string]interface{}{"script": "echo 'Testing ${outputs.build.version}'"}},
        {ID: "deploy", Type: "shell", Config: map[string]interface{}{"script": "echo 'Deploying ${outputs.build.version}'"}},
    },
    Edges: []Edge{
        {From: "build", To: "test"},
        {From: "test", To: "deploy"},
    },
}
```

**操作**:
```go
run := dagEngine.Execute(pipeline)
```

**预期结果**:
- build 输出 version=2.0
- test 正确引用
- deploy 正确引用（跨过test直接引用build）

**验证方法**:
```go
build := run.GetTask("build")
test := run.GetTask("test")
deploy := run.GetTask("deploy")

assert.Equal(t, "2.0", build.Outputs["version"])
assert.Equal(t, "success", test.Status)
assert.Equal(t, "success", deploy.Status)
```

---

### 2.3 作用域测试

#### VAR-TEST-020: 变量作用域隔离（必过）
**用例ID**: VAR-TEST-020  
**优先级**: P0  
**模块**: 变量系统

**输入**:
```go
// A和B都设置同名变量version
pipeline := PipelineConfig{
    Nodes: []PipelineNode{
        {ID: "a", Type: "shell", Config: map[string]interface{}{"script": "echo 'version=1.0'"}},
        {ID: "b", Type: "shell", Config: map[string]interface{}{"script": "echo 'version=2.0'"}},
        {ID: "c", Type: "shell", Config: map[string]interface{}{"script": "echo '${outputs.a.version} vs ${outputs.b.version}'"}},
    },
    Edges: []Edge{
        {From: "a", To: "c"},
        {From: "b", To: "c"},
    },
}
```

**操作**:
```go
run := dagEngine.Execute(pipeline)
```

**预期结果**:
- A的version=1.0
- B的version=2.0
- C看到两个独立的变量

**验证方法**:
```go
a := run.GetTask("a")
b := run.GetTask("b")
c := run.GetTask("c")

assert.Equal(t, "1.0", a.Outputs["version"])
assert.Equal(t, "2.0", b.Outputs["version"])
// C可以看到a和b的输出
assert.Contains(t, c.Stdout, "1.0")
assert.Contains(t, c.Stdout, "2.0")
```

---

#### VAR-TEST-021: 变量覆盖测试（推荐）
**用例ID**: VAR-TEST-021  
**优先级**: P1  
**模块**: 变量系统

**输入**:
```go
// A设置version=1.0，B覆盖version=2.0
pipeline := PipelineConfig{
    Nodes: []PipelineNode{
        {ID: "a", Type: "shell", Config: map[string]interface{}{"script": "echo 'version=1.0'"}},
        {ID: "b", Type: "shell", Config: map[string]interface{}{"script": "echo 'version=2.0'"}},
        {ID: "c", Type: "shell", Config: map[string]interface{}{"script": "echo 'Final: ${outputs.b.version}'"}},
    },
    Edges: []Edge{
        {From: "a", To: "b"},
        {From: "b", To: "c"},
    },
}
```

**操作**:
```go
run := dagEngine.Execute(pipeline)
```

**预期结果**:
- A的version=1.0
- B的version=2.0（不覆盖A的输出）
- C引用B的version=2.0

**验证方法**:
```go
a := run.GetTask("a")
b := run.GetTask("b")
c := run.GetTask("c")

assert.Equal(t, "1.0", a.Outputs["version"])
assert.Equal(t, "2.0", b.Outputs["version"])
assert.Contains(t, c.Stdout, "2.0")
```

---

### 2.4 变量系统测试用例汇总

| 用例ID | 用例名称 | 优先级 | 类别 |
|--------|----------|--------|------|
| VAR-TEST-001 | 环境变量解析 | P0 | 解析 |
| VAR-TEST-002 | 任务输出解析 | P0 | 解析 |
| VAR-TEST-003 | 输入变量解析 | P0 | 解析 |
| VAR-TEST-004 | 嵌套变量解析 | P1 | 解析 |
| VAR-TEST-005 | 变量不存在处理 | P0 | 解析 |
| VAR-TEST-010 | 基本变量传递 | P0 | 传递 |
| VAR-TEST-011 | 多变量传递 | P0 | 传递 |
| VAR-TEST-012 | 跨多任务传递 | P1 | 传递 |
| VAR-TEST-020 | 变量作用域隔离 | P0 | 作用域 |
| VAR-TEST-021 | 变量覆盖测试 | P1 | 作用域 |

**变量系统模块测试用例总计**: 10个（P0: 8, P1: 2）

---

## 3. 日志系统模块测试用例

### 3.1 日志传输测试

#### LOG-TEST-001: 实时日志传输（必过）
**用例ID**: LOG-TEST-001  
**优先级**: P0  
**模块**: 日志系统

**输入**:
```go
script := `
for i in {1..100}; do
    echo "Log line $i"
done
`
```

**操作**:
```go
// 订阅日志
logChan := wsClient.SubscribeTaskLogs(taskID)

// 执行任务
run := dagEngine.Execute(pipeline)

// 收集日志
receivedLogs := []AgentLog{}
for i := 0; i < 100; i++ {
    log := <-logChan
    receivedLogs = append(receivedLogs, log)
}
```

**预期结果**:
- 收到100条日志
- 日志按顺序到达
- 延迟 < 1秒

**验证方法**:
```go
assert.Len(t, receivedLogs, 100)
for i := 0; i < 100; i++ {
    assert.Contains(t, receivedLogs[i].Message, fmt.Sprintf("Log line %d", i+1))
}
```

---

#### LOG-TEST-002: 日志缓冲到Redis（必过）
**用例ID**: LOG-TEST-002  
**优先级**: P0  
**模块**: 日志系统

**输入**:
```go
// 大量日志输出
script := `
for i in {1..1000}; do
    echo "Log line $i"
done
`
```

**操作**:
```go
run := dagEngine.Execute(pipeline)

// 检查Redis队列
redisKey := fmt.Sprintf("logs:%d:%d", run.ID, task.ID)
queueLen := redis.LLen(redisKey)
```

**预期结果**:
- Redis队列中有1000条日志
- 无日志丢失

**验证方法**:
```go
assert.Equal(t, 1000, int(queueLen))

// 验证日志内容
logs := redis.LRange(redisKey, 0, -1)
assert.Len(t, logs, 1000)
```

---

#### LOG-TEST-003: 定时刷新到MySQL（必过）
**用例ID**: LOG-TEST-003  
**优先级**: P0  
**模块**: 日志系统

**前置条件**: 定时器间隔=5秒

**输入**:
```go
script := `
for i in {1..100}; do
    echo "Log line $i"
    sleep 0.1
done
`
```

**操作**:
```go
run := dagEngine.Execute(pipeline)

// 等待定时刷新
time.Sleep(6 * time.Second)

// 检查MySQL
dbLogs := db.Model(&AgentLog{}).Where("task_id = ?", taskID).Count()
```

**预期结果**:
- 100条日志都已刷新到MySQL
- 或部分刷新

**验证方法**:
```go
// 5秒后应该有一定数量的日志在MySQL
count := db.Model(&AgentLog{}).Where("task_id = ?", taskID).Count()
assert.Greater(t, int(count), 0)
```

---

#### LOG-TEST-004: 任务完成立即刷新（必过）
**用例ID**: LOG-TEST-004  
**优先级**: P0  
**模块**: 日志系统

**输入**:
```go
script := `
echo "Start"
sleep 1
echo "Middle"
sleep 1
echo "End"
`
```

**操作**:
```go
run := dagEngine.Execute(pipeline)
waitForCompletion(run.ID)

// 立即检查MySQL
dbLogs := db.Model(&AgentLog{}).Where("task_id = ?", taskID).Find(&[]AgentLog{})
```

**预期结果**:
- 所有日志都在MySQL中
- 无日志丢失

**验证方法**:
```go
// 任务完成后，Redis队列应该为空
assert.Equal(t, 0, int(redis.LLen(redisKey)))

// MySQL中有完整日志
assert.Len(t, dbLogs, 3)
```

---

### 3.2 日志格式测试

#### LOG-TEST-010: 日志格式验证（必过）
**用例ID**: LOG-TEST-010  
**优先级**: P0  
**模块**: 日志系统

**输入**:
```go
script := `
echo "Normal log"
echo "Error log" >&2
`
```

**操作**:
```go
run := dagEngine.Execute(pipeline)
logs := getTaskLogs(taskID)
```

**预期结果**:
- 每条日志包含: Timestamp, Level, Message, Source

**验证方法**:
```go
for _, log := range logs {
    assert.NotZero(t, log.Timestamp)
    assert.NotEmpty(t, log.Level)  // debug/info/warn/error
    assert.NotEmpty(t, log.Message)
    assert.NotEmpty(t, log.Source) // stdout/stderr
}
```

---

#### LOG-TEST-011: 日志级别分类（推荐）
**用例ID**: LOG-TEST-011  
**优先级**: P1  
**模块**: 日志系统

**输入**:
```go
script := `
echo "Debug message" >&1
echo "Info message" >&1
echo "Warning message" >&2
echo "Error message" >&2
`
```

**操作**:
```go
run := dagEngine.Execute(pipeline)
logs := getTaskLogs(taskID)
```

**预期结果**:
- stdout日志标记为info
- stderr日志标记为error

**验证方法**:
```go
for _, log := range logs {
    if log.Source == "stdout" {
        assert.Equal(t, "info", log.Level)
    } else if log.Source == "stderr" {
        assert.Equal(t, "error", log.Level)
    }
}
```

---

### 3.3 日志缓冲测试

#### LOG-TEST-020: Redis队列溢出处理（推荐）
**用例ID**: LOG-TEST-020  
**优先级**: P1  
**模块**: 日志系统

**输入**:
```go
// 超过队列最大长度
maxQueueSize := 10000
script := generateLargeOutput(15000)  // 15000行日志
```

**操作**:
```go
run := dagEngine.Execute(pipeline)
queueLen := redis.LLen(fmt.Sprintf("logs:%d:%d", run.ID, task.ID))
```

**预期结果**:
- 队列长度限制生效
- 超过部分被丢弃或处理

**验证方法**:
```go
assert.LessOrEqual(t, int(queueLen), maxQueueSize)
```

---

#### LOG-TEST-021: 批量写入MySQL优化（推荐）
**用例ID**: LOG-TEST-021  
**优先级**: P1  
**模块**: 日志系统

**输入**:
```go
// 10000条日志
script := generateLargeOutput(10000)
```

**操作**:
```go
run := dagEngine.Execute(pipeline)
waitForCompletion(run.ID)

// 记录MySQL写入次数
beforeCount := getMySQLInsertCount()
```

**预期结果**:
- 批量写入而非逐条插入
- 插入次数远小于日志条数

**验证方法**:
```go
// 应该是批量插入，不是10000次单条插入
assert.Less(t, getMySQLInsertCount()-beforeCount, 100)
```

---

### 3.4 日志系统测试用例汇总

| 用例ID | 用例名称 | 优先级 | 类别 |
|--------|----------|--------|------|
| LOG-TEST-001 | 实时日志传输 | P0 | 传输 |
| LOG-TEST-002 | Redis缓冲 | P0 | 缓冲 |
| LOG-TEST-003 | 定时刷新MySQL | P0 | 缓冲 |
| LOG-TEST-004 | 任务完成刷新 | P0 | 缓冲 |
| LOG-TEST-010 | 日志格式验证 | P0 | 格式 |
| LOG-TEST-011 | 日志级别分类 | P1 | 格式 |
| LOG-TEST-020 | Redis队列溢出 | P1 | 缓冲 |
| LOG-TEST-021 | 批量写入优化 | P1 | 性能 |

**日志系统模块测试用例总计**: 8个（P0: 5, P1: 3）

---

## 4. Agent调度模块测试用例

### 4.1 Agent选择测试

#### AGENT-TEST-001: 选择负载最低的Agent（必过）
**用例ID**: AGENT-TEST-001  
**优先级**: P0  
**模块**: Agent调度

**前置条件**:
- Agent A: 0个运行中任务
- Agent B: 5个运行中任务

**操作**:
```go
agentID := scheduler.SelectAgent(pipelineID)
```

**预期结果**:
- 选择 Agent A（负载最低）

**验证方法**:
```go
selectedAgent := db.First(&Agent{}, agentID)
assert.Equal(t, "A", selectedAgent.Name)

runningTasks := db.Model(&AgentTask{}).Where("agent_id = ? AND status = ?", agentID, "running").Count()
assert.Equal(t, int64(0), runningTasks)
```

---

#### AGENT-TEST-002: 选择在线Agent（必过）
**用例ID**: AGENT-TEST-002  
**优先级**: P0  
**模块**: Agent调度

**前置条件**:
- Agent A: 在线
- Agent B: 离线

**操作**:
```go
agentID := scheduler.SelectAgent(pipelineID)
```

**预期结果**:
- 选择 Agent A（在线）
- 不会选择离线Agent

**验证方法**:
```go
selectedAgent := db.First(&Agent{}, agentID)
assert.Equal(t, "online", selectedAgent.Status)
```

---

#### AGENT-TEST-003: 无可用Agent（必过）
**用例ID**: AGENT-TEST-003  
**优先级**: P0  
**模块**: Agent调度

**前置条件**: 所有Agent都离线

**操作**:
```go
agentID, err := scheduler.SelectAgent(pipelineID)
```

**预期结果**:
- 返回错误或nil
- 流水线执行失败

**验证方法**:
```go
assert.Error(t, err)
assert.Contains(t, err.Error(), "no available agent")
```

---

#### AGENT-TEST-004: Agent资源匹配（推荐）
**用例ID**: AGENT-TEST-004  
**优先级**: P1  
**模块**: Agent调度

**前置条件**:
- Agent A: 2核CPU, 4GB内存
- Agent B: 8核CPU, 16GB内存
- Pipeline需要: 4核CPU, 8GB内存

**操作**:
```go
agentID := scheduler.SelectAgent(pipelineID)
```

**预期结果**:
- 选择 Agent B（满足资源要求）
- 或选择 Agent A（不满足，被拒绝后选择B）

**验证方法**:
```go
selectedAgent := db.First(&Agent{}, agentID)
assert.Equal(t, "B", selectedAgent.Name)
```

---

### 4.2 任务分配测试

#### AGENT-TEST-010: 单Agent分配（必过）
**用例ID**: AGENT-TEST-010  
**优先级**: P0  
**模块**: Agent调度

**输入**:
```go
// 单个PipelineRun
pipeline := PipelineConfig{
    Nodes: []PipelineNode{
        {ID: "build", Type: "shell"},
        {ID: "test", Type: "shell"},
        {ID: "deploy", Type: "shell"},
    },
    Edges: []Edge{
        {From: "build", To: "test"},
        {From: "test", To: "deploy"},
    },
}
```

**操作**:
```go
run := dagEngine.Execute(pipeline)
```

**预期结果**:
- 3个任务都分配给同一个Agent
- AgentID一致

**验证方法**:
```go
build := run.GetTask("build")
test := run.GetTask("test")
deploy := run.GetTask("deploy")

assert.Equal(t, build.AgentID, test.AgentID)
assert.Equal(t, test.AgentID, deploy.AgentID)
```

---

#### AGENT-TEST-011: 多Pipeline任务分配（必过）
**用例ID**: AGENT-TEST-011  
**优先级**: P0  
**模块**: Agent调度

**前置条件**:
- 2个Pipeline同时执行
- 3个Agent可用

**操作**:
```go
run1 := TriggerExecution(pipeline1.ID, ExecutionRequest{})
run2 := TriggerExecution(pipeline2.ID, ExecutionRequest{})
```

**预期结果**:
- 每个Pipeline的所有任务分配给同一个Agent
- 不同Pipeline可能分配给不同Agent

**验证方法**:
```go
tasks1 := getRunTasks(run1.ID)
tasks2 := getRunTasks(run2.ID)

for _, task := range tasks1 {
    assert.Equal(t, tasks1[0].AgentID, task.AgentID)
}
for _, task := range tasks2 {
    assert.Equal(t, tasks2[0].AgentID, task.AgentID)
}
```

---

### 4.3 负载均衡测试

#### AGENT-TEST-020: 负载均衡验证（必过）
**用例ID**: AGENT-TEST-020  
**优先级**: P0  
**模块**: Agent调度

**前置条件**:
- 10个Pipeline同时执行
- 3个Agent

**操作**:
```go
var runs []PipelineRun
for i := 0; i < 10; i++ {
    run := TriggerExecution(pipelines[i].ID, ExecutionRequest{})
    runs = append(runs, run)
}

// 统计每个Agent的任务数
agentTaskCounts := make(map[uint64]int)
for _, run := range runs {
    task := run.GetFirstTask()
    agentTaskCounts[task.AgentID]++
}
```

**预期结果**:
- 任务均匀分配到3个Agent
- 每个Agent的任务数接近平均值（10/3 ≈ 3-4个）

**验证方法**:
```go
maxCount := 0
minCount := 100
for _, count := range agentTaskCounts {
    if count > maxCount {
        maxCount = count
    }
    if count < minCount {
        minCount = count
    }
}
assert.LessOrEqual(t, maxCount-minCount, 2)  // 差异不超过2
```

---

#### AGENT-TEST-021: Agent下线处理（必过）
**用例ID**: AGENT-TEST-021  
**优先级**: P0  
**模块**: Agent调度

**前置条件**:
- Pipeline正在执行
- 任务分配给Agent A

**操作**:
```go
run := TriggerExecution(pipeline.ID, ExecutionRequest{})

// Agent A下线
setAgentOffline(agentID)

// 等待超时
time.Sleep(agentTimeout + 5*time.Second)
```

**预期结果**:
- 运行中的任务失败
- 错误信息: "agent offline"
- Pipeline失败

**验证方法**:
```go
task := run.GetFirstTask()
assert.Equal(t, "failed", task.Status)
assert.Contains(t, task.ErrorMsg, "agent offline")
assert.Equal(t, "failed", run.Status)
```

---

### 4.4 Agent调度测试用例汇总

| 用例ID | 用例名称 | 优先级 | 类别 |
|--------|----------|--------|------|
| AGENT-TEST-001 | 选择负载最低Agent | P0 | 选择 |
| AGENT-TEST-002 | 选择在线Agent | P0 | 选择 |
| AGENT-TEST-003 | 无可用Agent | P0 | 选择 |
| AGENT-TEST-004 | Agent资源匹配 | P1 | 选择 |
| AGENT-TEST-010 | 单Agent分配 | P0 | 分配 |
| AGENT-TEST-011 | 多Pipeline分配 | P0 | 分配 |
| AGENT-TEST-020 | 负载均衡验证 | P0 | 均衡 |
| AGENT-TEST-021 | Agent下线处理 | P0 | 均衡 |

**Agent调度模块测试用例总计**: 8个（P0: 7, P1: 1）

---

## 5. 通信模块测试用例

### 5.1 WebSocket连接测试

#### WS-TEST-001: 建立连接（必过）
**用例ID**: WS-TEST-001  
**优先级**: P0  
**模块**: 通信

**操作**:
```go
client := NewWSClient(serverURL)
err := client.Connect()
```

**预期结果**:
- 连接成功
- 收到连接确认消息

**验证方法**:
```go
assert.NoError(t, err)
assert.True(t, client.IsConnected())

// 收到连接确认
msg := client.ReadMessage()
assert.Equal(t, "connected", msg.Type)
```

---

#### WS-TEST-002: 心跳保持（必过）
**用例ID**: WS-TEST-002  
**优先级**: P0  
**模块**: 通信

**前置条件**: 连接已建立

**操作**:
```go
// 等待心跳
time.Sleep(heartbeatInterval + 1*time.Second)

// 检查Agent状态
agent := db.First(&Agent{}, agentID)
```

**预期结果**:
- 心跳定期发送
- Agent状态保持online

**验证方法**:
```go
assert.Equal(t, "online", agent.Status)
```

---

#### WS-TEST-003: 心跳超时（必过）
**用例ID**: WS-TEST-003  
**优先级**: P0  
**模块**: 通信

**前置条件**: Agent已连接

**操作**:
```go
// 停止发送心跳
agent.StopHeartbeat()

// 等待超时
time.Sleep(heartbeatTimeout + 5*time.Second)

// 检查Agent状态
agent := db.First(&Agent{}, agentID)
```

**预期结果**:
- Agent状态变为offline
- 触发重连

**验证方法**:
```go
assert.Equal(t, "offline", agent.Status)
```

---

#### WS-TEST-004: 断线重连（必过）
**用例ID**: WS-TEST-004  
**优先级**: P0  
**模块**: 通信

**操作**:
```go
// 断开连接
client.Disconnect()

// 等待重连
time.Sleep(retryInterval + 1*time.Second)

// 检查重连结果
```

**预期结果**:
- 自动重连
- 重连成功后状态恢复

**验证方法**:
```go
assert.True(t, client.IsConnected())
assert.NotZero(t, client.GetReconnectCount())
```

---

### 5.2 消息广播测试

#### WS-TEST-010: 订阅执行进度（必过）
**用例ID**: WS-TEST-010  
**优先级**: P0  
**模块**: 通信

**操作**:
```go
// 前端订阅
wsClient.SubscribeExecution(run.ID)

// 触发执行
run := TriggerExecution(pipeline.ID, ExecutionRequest{})

// 等待进度更新
progressMsg := wsClient.ReadMessage()
```

**预期结果**:
- 收到执行进度更新

**验证方法**:
```go
assert.Equal(t, "run_progress", progressMsg.Type)
assert.Equal(t, run.ID, progressMsg.RunID)
```

---

#### WS-TEST-011: 任务状态广播（必过）
**用例ID**: WS-TEST-011  
**优先级**: P0  
**模块**: 通信

**操作**:
```go
wsClient.SubscribeTaskLogs(taskID)

run := TriggerExecution(pipeline.ID, ExecutionRequest{})
waitForTaskStart(taskID)
```

**预期结果**:
- 收到任务状态变更消息

**验证方法**:
```go
msg := wsClient.ReadMessage()
assert.Equal(t, "task_status", msg.Type)
assert.Equal(t, "running", msg.Status)
```

---

#### WS-TEST-012: 日志广播（必过）
**用例ID**: WS-TEST-012  
**优先级**: P0  
**模块**: 通信

**操作**:
```go
wsClient.SubscribeTaskLogs(taskID)

run := TriggerExecution(pipeline.ID, ExecutionRequest{})

// 等待日志
logMsg := wsClient.ReadMessage()
```

**预期结果**:
- 收到实时日志消息

**验证方法**:
```go
assert.Equal(t, "task_log", logMsg.Type)
assert.NotEmpty(t, logMsg.Message)
assert.NotZero(t, logMsg.Timestamp)
```

---

### 5.3 消息顺序测试

#### WS-TEST-020: 消息顺序保证（必过）
**用例ID**: WS-TEST-020  
**优先级**: P0  
**模块**: 通信

**输入**:
```go
// 快速产生大量日志
script := `
for i in {1..100}; do
    echo "Log $i"
done
`
```

**操作**:
```go
wsClient.SubscribeTaskLogs(taskID)
run := TriggerExecution(pipeline.ID, ExecutionRequest{})
waitForCompletion(run.ID)

// 收集所有消息
messages := wsClient.GetAllMessages()
```

**预期结果**:
- 消息按时间戳排序
- 无消息丢失

**验证方法**:
```go
var lastTimestamp int64
for _, msg := range messages {
    assert.GreaterOrEqual(t, msg.Timestamp, lastTimestamp)
    lastTimestamp = msg.Timestamp
}
```

---

#### WS-TEST-021: 多消息类型顺序（推荐）
**用例ID**: WS-TEST-021  
**优先级**: P1  
**模块**: 通信

**操作**:
```go
wsClient.SubscribeExecution(run.ID)
run := TriggerExecution(pipeline.ID, ExecutionRequest{})
waitForCompletion(run.ID)
```

**预期结果**:
- 消息类型顺序: 任务状态 → 日志 → 完成状态

**验证方法**:
```go
messages := wsClient.GetAllMessages()
expectedOrder := []string{"task_status", "task_log", "run_progress", "run_completed"}

orderIndex := 0
for _, msg := range messages {
    if msg.Type == expectedOrder[orderIndex] {
        orderIndex++
    }
}
assert.Equal(t, len(expectedOrder), orderIndex)
```

---

### 5.4 通信模块测试用例汇总

| 用例ID | 用例名称 | 优先级 | 类别 |
|--------|----------|--------|------|
| WS-TEST-001 | 建立连接 | P0 | 连接 |
| WS-TEST-002 | 心跳保持 | P0 | 连接 |
| WS-TEST-003 | 心跳超时 | P0 | 连接 |
| WS-TEST-004 | 断线重连 | P0 | 连接 |
| WS-TEST-010 | 订阅执行进度 | P0 | 广播 |
| WS-TEST-011 | 任务状态广播 | P0 | 广播 |
| WS-TEST-012 | 日志广播 | P0 | 广播 |
| WS-TEST-020 | 消息顺序保证 | P0 | 顺序 |
| WS-TEST-021 | 多消息类型顺序 | P1 | 顺序 |

**通信模块测试用例总计**: 9个（P0: 8, P1: 1）

---

## 6. 五大模块测试用例汇总

| 模块 | P0用例 | P1用例 | 总计 |
|------|--------|--------|------|
| DAG执行模块 | 14 | 2 | 16 |
| 变量系统模块 | 8 | 2 | 10 |
| 日志系统模块 | 5 | 3 | 8 |
| Agent调度模块 | 7 | 1 | 8 |
| 通信模块 | 8 | 1 | 9 |
| **总计** | **42** | **9** | **51** |

### 累计测试用例统计

| 文档 | 测试用例数 |
|------|-----------|
| 流程级测试用例 | 43 |
| 边界条件测试 | 35 |
| **模块详细测试** | **51** |
| **总计** | **129** |

---

## 7. 测试验证命令

### 7.1 运行所有测试

```bash
# 运行DAG执行模块测试
go test -v ./internal/executor/... -run "DAG"

# 运行变量系统模块测试
go test -v ./internal/executor/... -run "VAR"

# 运行日志系统模块测试
go test -v ./internal/handlers/... -run "LOG"

# 运行Agent调度模块测试
go test -v ./internal/scheduler/... -run "AGENT"

# 运行通信模块测试
go test -v ./internal/handlers/... -run "WS"
```

### 7.2 运行边界测试

```bash
go test -v ./... -run "BOUNDARY"
```

### 7.3 运行完整测试套件

```bash
go test -v ./... -run "TEST-" 
```

---

**文档版本**: 1.0  
**创建日期**: 2026-02-02  
**包含模块**: DAG执行、变量系统、日志系统、Agent调度、通信
