# F01 边界条件测试用例补充

## 概述

本文档补充了流水线全生命周期的边界条件测试用例，涵盖：
- 输入边界条件
- 状态转换边界
- 时间相关边界
- 资源限制边界
- 错误场景边界
- 并发边界
- 数据大小限制
- 字符数量限制

---

## 一、输入边界条件测试

### 1.1 字符串长度边界

#### TC-BOUNDARY-001: Pipeline名称最大长度（必过）
**场景**: Pipeline名称达到最大长度限制

**输入**:
```go
name := strings.Repeat("a", 129) // 超过128限制
```

**操作**:
```go
CreatePipeline(CreatePipelineRequest{
    Name:        name,
    Description: "Test",
    ProjectID:   1,
    Config:      validConfig,
})
```

**预期结果**:
- 返回错误: "name exceeds maximum length of 128 characters"
- Pipeline未创建

**验证方法**:
```go
// 边界值测试
testCases := []struct {
    name       string
    nameLen    int
    shouldFail bool
}{
    {"127 chars", 127, false},
    {"128 chars", 128, false},
    {"129 chars", 129, true},
    {"256 chars", 256, true},
}
```

**优先级**: P0

---

#### TC-BOUNDARY-002: 节点ID长度限制（必过）
**场景**: 节点ID长度超出限制

**输入**:
```go
nodeID := strings.Repeat("n", 65) // 超过64限制
config := fmt.Sprintf(`{
    "nodes": [{"id": "%s", "type": "shell"}],
    "edges": []
}`, nodeID)
```

**操作**:
```go
CreatePipeline(CreatePipelineRequest{
    Name:     "Test",
    ProjectID: 1,
    Config:   config,
})
```

**预期结果**:
- 返回错误: "node ID exceeds maximum length of 64 characters"
- Pipeline未创建

**优先级**: P0

---

#### TC-BOUNDARY-003: 脚本内容长度（必过）
**场景**: Shell脚本内容超出最大长度

**输入**:
```go
script := strings.Repeat("echo hello\n", 10000) // 约80KB
config := fmt.Sprintf(`{
    "nodes": [{
        "id": "build",
        "type": "shell",
        "config": {"script": "%s"}
    }],
    "edges": []
}`, script)
```

**操作**:
```go
CreatePipeline(CreatePipelineRequest{
    Name:     "Test",
    ProjectID: 1,
    Config:   config,
})
```

**预期结果**:
- 返回警告或拒绝: "script exceeds maximum length of 65535 characters"
- 或接受但截断存储

**验证方法**:
```go
// 验证最大长度
maxScriptLen := 64 * 1024 // 64KB
assert.LessOrEqual(t, len(script), maxScriptLen)
```

**优先级**: P1

---

#### TC-BOUNDARY-004: 描述字段最大长度（推荐）
**场景**: Pipeline描述超出最大长度

**输入**:
```go
description := strings.Repeat("x", 65537) // 超过64KB限制
```

**操作**:
```go
CreatePipeline(CreatePipelineRequest{
    Name:        "Test",
    Description: description,
    ProjectID:   1,
    Config:      validConfig,
})
```

**预期结果**:
- 返回错误: "description exceeds maximum length"
- 或自动截断

**优先级**: P1

---

### 1.2 数字范围边界

#### TC-BOUNDARY-005: BuildNumber最大值（必过）
**场景**: Pipeline执行次数达到最大值

**操作**:
```go
// 模拟第10000次执行
pipeline := db.First(&Pipeline{})
pipeline.BuildNumber = 9999

run := TriggerExecution(pipeline.ID, ExecutionRequest{})
```

**预期结果**:
- 创建成功，BuildNumber = 10000
- 无整数溢出

**验证方法**:
```go
// 验证最大值
maxBuildNumber := int64(9999999999) // 10^10 - 1
assert.LessOrEqual(t, newRun.BuildNumber, maxBuildNumber)
```

**优先级**: P1

---

#### TC-BOUNDARY-006: Timeout边界值（必过）
**场景**: 超时时间设置边界值

**测试用例**:
```go
testCases := []struct {
    name         string
    timeout      int
    shouldAccept bool
}{
    {"timeout=0", 0, true},          // 使用默认
    {"timeout=1", 1, true},          // 最小值
    {"timeout=60", 60, true},        // 合理最小
    {"timeout=3600", 3600, true},    // 1小时
    {"timeout=86400", 86400, true},  // 24小时
    {"timeout=-1", -1, false},       // 无效
}
```

**操作**:
```go
for _, tc := range testCases {
    config := fmt.Sprintf(`{
        "nodes": [{
            "id": "build",
            "type": "shell",
            "config": {"script": "sleep 1"},
            "timeout": %d
        }],
        "edges": []
    }`, tc.timeout)

    run := CreatePipelineWithConfig(config)
    // 验证
}
```

**预期结果**:
- timeout=0: 使用默认3600秒
- timeout>0: 使用指定值
- timeout<0: 返回错误

**优先级**: P0

---

#### TC-BOUNDARY-007: 重试次数边界（必过）
**场景**: RetryCount边界值测试

**测试用例**:
```go
testCases := []struct {
    name       string
    retryCount int
    shouldPass bool
}{
    {"retry=0", 0, true},      // 不重试
    {"retry=1", 1, true},      // 重试1次
    {"retry=3", 3, true},      // 默认值
    {"retry=10", 10, true},    // 较高
    {"retry=100", 100, false}, // 过高
}
```

**操作**:
```go
config := fmt.Sprintf(`{
    "nodes": [{
        "id": "build",
        "type": "shell",
        "config": {"script": "exit 1"},
        "retry_count": %d
    }],
    "edges": []
}`, tc.retryCount)
```

**预期结果**:
- retry<=10: 接受
- retry>10: 返回警告或拒绝

**优先级**: P1

---

### 1.3 数组/集合边界

#### TC-BOUNDARY-008: 节点数量上限（必过）
**场景**: Pipeline中节点数量达到上限

**输入**:
```go
// 创建101个节点
nodes := make([]Node, 101)
for i := 0; i < 101; i++ {
    nodes[i] = Node{
        ID:   fmt.Sprintf("node_%d", i),
        Type: "shell",
        Config: map[string]interface{}{
            "script": fmt.Sprintf("echo %d", i),
        },
    }
}
```

**操作**:
```go
CreatePipeline(CreatePipelineRequest{
    Name:     "Test",
    ProjectID: 1,
    Config:   toJSON(PipelineConfig{Nodes: nodes}),
})
```

**预期结果**:
- 返回错误: "maximum number of nodes is 100"
- Pipeline未创建

**验证方法**:
```go
maxNodes := 100
assert.LessOrEqual(t, len(config.Nodes), maxNodes)
```

**优先级**: P0

---

#### TC-BOUNDARY-009: 依赖数量上限（必过）
**场景**: 单个节点的入度或出度达到上限

**输入**:
```go
// 创建1个节点，被100个节点依赖
config := fmt.Sprintf(`{
    "nodes": [
        {"id": "root", "type": "shell"},
        %s
    ],
    "edges": [
        {"from": "root", "to": "leaf_%d"}
    ]
}`, generateManyLeafNodes(100), 100)
```

**操作**:
```go
run := TriggerExecution(pipeline.ID, ExecutionRequest{})
```

**预期结果**:
- DAG验证通过
- 执行正常
- 大量并行子任务可正确调度

**优先级**: P1

---

#### TC-BOUNDARY-010: 输入参数数量上限（推荐）
**场景**: Pipeline输入参数数量过多

**输入**:
```go
inputs := make(map[string]interface{})
for i := 0; i < 101; i++ {
    inputs[fmt.Sprintf("param_%d", i)] = fmt.Sprintf("value_%d", i)
}
```

**操作**:
```go
TriggerExecution(pipeline.ID, ExecutionRequest{
    Inputs: inputs,
})
```

**预期结果**:
- 返回警告: "too many input parameters"
- 或接受并处理

**优先级**: P2

---

## 二、状态转换边界测试

### 2.1 Pipeline状态转换

#### TC-STATE-001: Pipeline状态合法转换（必过）
**场景**: 验证PipelineRun状态的合法转换

**状态机**:
```
pending → running → success/failed/cancelled
pending → cancelled
running → cancelled
```

**测试用例**:
```go
testCases := []struct {
    from   string
    to     string
    valid  bool
}{
    {"pending", "running", true},
    {"pending", "cancelled", true},
    {"running", "success", true},
    {"running", "failed", true},
    {"running", "cancelled", true},
    {"success", "running", false},    // 不能从成功回到运行
    {"failed", "running", false},     // 不能从失败回到运行
    {"cancelled", "running", false},  // 不能从取消回到运行
}
```

**操作**:
```go
for _, tc := range testCases {
    run := createRunWithStatus(tc.from)
    err := updateRunStatus(run.ID, tc.to)

    if tc.valid {
        assert.NoError(t, err)
    } else {
        assert.Error(t, err)
        assert.Contains(t, err.Error(), "invalid state transition")
    }
}
```

**优先级**: P0

---

#### TC-STATE-002: Agent状态转换（必过）
**场景**: Agent状态机转换

**状态机**:
```
offline → online → busy → online → offline
```

**测试用例**:
```go
testCases := []struct {
    from   string
    to     string
    valid  bool
}{
    {"offline", "online", true},
    {"online", "busy", true},
    {"busy", "online", true},
    {"online", "offline", true},
    {"offline", "busy", false},    // 必须先online
    {"busy", "offline", false},    // 必须先回online
}
```

**操作**:
```go
for _, tc := range testCases {
    agent := createAgentWithStatus(tc.from)
    err := updateAgentStatus(agent.ID, tc.to)

    if tc.valid {
        assert.NoError(t, err)
    } else {
        assert.Error(t, err)
    }
}
```

**优先级**: P0

---

#### TC-STATE-003: Task状态转换（必过）
**场景**: AgentTask状态机转换

**状态机**:
```
pending → running → success/failed
pending → running → cancelled
pending → skipped
```

**测试用例**:
```go
testCases := []struct {
    from    string
    to      string
    valid   bool
    reason  string
}{
    {"pending", "running", true, "开始执行"},
    {"running", "success", true, "执行成功"},
    {"running", "failed", true, "执行失败"},
    {"running", "cancelled", true, "被取消"},
    {"pending", "skipped", true, "条件不满足"},
    {"success", "running", false, "成功状态不能逆转"},
    {"failed", "running", false, "失败状态不能逆转"},
    {"cancelled", "running", false, "取消状态不能逆转"},
}
```

**操作**:
```go
for _, tc := range testCases {
    task := createTaskWithStatus(tc.from)
    err := updateTaskStatus(task.ID, tc.to)

    if tc.valid {
        assert.NoError(t, err)
    } else {
        assert.Error(t, err)
        assert.Contains(t, err.Error(), "invalid state transition")
    }
}
```

**优先级**: P0

---

### 2.2 状态转换副作用

#### TC-STATE-004: 状态转换触发操作（必过）
**场景**: 特定状态转换触发关联操作

**测试用例**:
```go
func TestStateTransitionTriggers(t *testing.T) {
    // pending → running: 创建执行时间戳
    run := createRunWithStatus("pending")
    updateRunStatus(run.ID, "running")
    assert.NotZero(t, run.StartTime)

    // running → success: 计算执行时长
    run.StartTime = time.Now().Unix() - 300
    updateRunStatus(run.ID, "success")
    assert.Equal(t, 300, run.Duration)

    // running → failed: 记录错误信息
    run.StartTime = time.Now().Unix() - 100
    updateRunStatus(run.ID, "failed")
    assert.NotEmpty(t, run.ErrorMsg)
}
```

**优先级**: P0

---

## 三、时间相关边界测试

### 3.1 执行时长边界

#### TC-TIME-001: 极短任务执行（必过）
**场景**: 任务执行时间极短（毫秒级）

**输入**:
```go
script := `
#!/bin/bash
echo "Hello World"
sleep 0.001  # 1毫秒
`
```

**操作**:
```go
run := TriggerExecution(pipeline.ID, ExecutionRequest{
    Config: toJSON(PipelineConfig{
        Nodes: []Node{{
            ID:   "build",
            Type: "shell",
            Config: map[string]interface{}{"script": script},
        }},
    }),
})
waitForCompletion(run.ID)
```

**预期结果**:
- 任务执行成功
- Duration > 0（即使很短）
- 无时间精度问题

**验证方法**:
```go
task := findTask(run.ID, "build")
assert.Equal(t, "success", task.Status)
assert.GreaterOrEqual(t, task.Duration, 0)
assert.Less(t, task.Duration, 1)  // 小于1秒
```

**优先级**: P0

---

#### TC-TIME-002: 极长任务执行（推荐）
**场景**: 任务执行时间接近24小时

**输入**:
```go
script := `
#!/bin/bash
sleep 86390  # 接近24小时(86400秒)
echo "Done"
`
config := map[string]interface{}{
    "script":  script,
    "timeout": 86400,  // 24小时
}
```

**操作**:
```go
run := TriggerExecution(pipeline.ID, ExecutionRequest{
    Config: toJSON(PipelineConfig{
        Nodes: []Node{{
            ID:       "build",
            Type:     "shell",
            Config:   config,
            Timeout:  86400,
        }},
    }),
})
```

**预期结果**:
- 任务在超时前正常执行
- Duration正确记录（可能截断）
- 无整数溢出

**验证方法**:
```go
task := findTask(run.ID, "build")
assert.True(t, task.Duration <= 86400)  // 不超过超时时间
```

**优先级**: P1

---

#### TC-TIME-003: 并行任务总时长（必过）
**场景**: 大量并行任务的总执行时间

**输入**:
```go
// 10个并行任务，每个执行10秒
nodes := make([]Node, 10)
for i := 0; i < 10; i++ {
    nodes[i] = Node{
        ID:   fmt.Sprintf("task_%d", i),
        Type: "shell",
        Config: map[string]interface{}{
            "script": fmt.Sprintf("sleep 10 && echo task_%d", i),
        },
    }
}
```

**操作**:
```go
run := TriggerExecution(pipeline.ID, ExecutionRequest{
    Config: toJSON(PipelineConfig{Nodes: nodes}),
})
waitForCompletion(run.ID)
```

**预期结果**:
- 总执行时间 ≈ 10秒（并行）
- 非顺序执行的100秒

**验证方法**:
```go
assert.True(t, run.Duration < 15)  // 允许少量 overhead
```

**优先级**: P0

---

### 3.2 时间戳边界

#### TC-TIME-004: Unix时间戳边界（推荐）
**场景**: 2038年问题（32位时间戳溢出）

**输入**:
```go
// 设置一个未来的时间戳
futureTime := time.Date(2038, 1, 1, 0, 0, 0, 0, time.UTC)
```

**操作**:
```go
run := createRun()
run.StartTime = futureTime.Unix()
db.Save(run)
```

**预期结果**:
- 时间戳正确存储
- 无溢出错误
- 时间计算正确

**验证方法**:
```go
retrieved := db.First(&PipelineRun{}, run.ID)
assert.Equal(t, futureTime.Unix(), retrieved.StartTime)
```

**优先级**: P2

---

#### TC-TIME-005: 时区处理（推荐）
**场景**: 不同时区的用户执行流水线

**输入**:
```go
timezones := []string{
    "UTC",
    "Asia/Shanghai",
    "America/New_York",
    "Europe/London",
}
```

**操作**:
```go
for _, tz := range timezones {
    original := time.Local
    time.Local, _ = time.LoadLocation(tz)

    run := TriggerExecution(pipeline.ID, ExecutionRequest{})

    // 验证时间显示正确
    assert.NotZero(t, run.StartTime)

    time.Local = original
}
```

**预期结果**:
- 时间戳使用UTC存储
- 前端根据时区显示本地时间
- 无时区混淆

**验证方法**:
```go
// 数据库存储UTC
assert.Equal(t, time.UTC, time.Unix(run.StartTime, 0).Location())

// 前端转换正确
frontendTime := convertToTimezone(run.StartTime, userTimezone)
assert.Equal(t, expectedHour, frontendTime.Hour())
```

**优先级**: P1

---

## 四、资源限制边界测试

### 4.1 并发执行边界

#### TC-CONCURRENCY-001: 最大并行度限制（必过）
**场景**: 超过系统配置的最大并行任务数

**输入**:
```go
// 系统配置 max_parallel_tasks = 5
// 提交10个并行任务
nodes := generateParallelNodes(10)
```

**操作**:
```go
// 多个用户同时触发执行
var wg sync.WaitGroup
results := make(chan Run, 10)

for i := 0; i < 10; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        run := TriggerExecution(pipeline.ID, ExecutionRequest{})
        results <- run
    }()
}
wg.Wait()
close(results)
```

**预期结果**:
- 最多5个任务同时执行
- 其他任务排队等待
- 总执行时间增加

**验证方法**:
```go
concurrentCount := 0
maxConcurrent := 0
for _, log := range executionLogs {
    if log.Status == "running" {
        concurrentCount++
        if concurrentCount > maxConcurrent {
            maxConcurrent = concurrentCount
        }
    } else {
        concurrentCount--
    }
}
assert.LessOrEqual(t, maxConcurrent, 5)
```

**优先级**: P0

---

#### TC-CONCURRENCY-002: Agent并发任务限制（必过）
**场景**: 单个Agent的任务并发限制

**输入**:
```go
// Agent配置 max_concurrent_tasks = 3
// 分配5个任务给同一个Agent
```

**操作**:
```go
run := TriggerExecution(pipeline.ID, ExecutionRequest{
    Config: toJSON(PipelineConfig{
        Nodes: generateParallelNodes(5),
    }),
})
```

**预期结果**:
- 单个Agent最多执行3个任务
- 其他任务排队
- 无资源耗尽

**验证方法**:
```go
agentTasks := getAgentTasks(agentID)
concurrentCount := countRunningTasks(agentTasks)
assert.LessOrEqual(t, concurrentCount, 3)
```

**优先级**: P0

---

#### TC-CONCURRENCY-003: 数据库连接池限制（推荐）
**场景**: 高并发时的数据库连接池

**输入**:
```go
// 100个并发流水线执行
// 数据库连接池大小 = 10
```

**操作**:
```go
var wg sync.WaitGroup
for i := 0; i < 100; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        run := TriggerExecution( pipelines[i%10].ID, ExecutionRequest{})
    }()
}
wg.Wait()
```

**预期结果**:
- 无数据库连接超时
- 无连接池耗尽错误
- 执行正常完成

**验证方法**:
```go
// 检查数据库连接使用
dbStats := db.DB.Stats()
assert.Less(t, dbStats.OpenConnections, dbStats.MaxOpenConnections)
```

**优先级**: P1

---

### 4.2 内存使用边界

#### TC-MEMORY-001: 大日志缓冲（必过）
**场景**: 任务产生大量日志（100MB+）

**输入**:
```go
// 生成100MB日志输出
script := fmt.Sprintf(`
for i in {1..1000000}; do
    echo "Log line $i: %s"
done
`, strings.Repeat("x", 100))
```

**操作**:
```go
run := TriggerExecution(pipeline.ID, ExecutionRequest{
    Config: toJSON(PipelineConfig{
        Nodes: []Node{{
            ID:   "build",
            Type: "shell",
            Config: map[string]interface{}{"script": script},
        }},
    }),
})
waitForCompletion(run.ID)
```

**预期结果**:
- 日志正确缓冲到Redis
- 无内存溢出
- 日志正确持久化到MySQL

**验证方法**:
```go
// 检查Redis队列
redisLen := redis.LLen(fmt.Sprintf("logs:%d:%d", run.ID, task.ID))
assert.Equal(t, 1000000, int(redisLen))

// 检查MySQL存储
logCount := db.Model(&AgentLog{}).Where("task_id = ?", task.ID).Count()
assert.Equal(t, 1000000, logCount)
```

**优先级**: P1

---

#### TC-MEMORY-002: Pipeline Config大小限制（必过）
**场景**: Pipeline Config JSON超出大小限制

**输入**:
```go
// 10MB的Config
configJSON := generateLargeConfig(10 * 1024 * 1024)
```

**操作**:
```go
CreatePipeline(CreatePipelineRequest{
    Name:     "Test",
    ProjectID: 1,
    Config:   configJSON,
})
```

**预期结果**:
- 返回错误: "config exceeds maximum size of 1MB"
- 或自动压缩存储

**验证方法**:
```go
maxConfigSize := 1 * 1024 * 1024 // 1MB
assert.LessOrEqual(t, len(configJSON), maxConfigSize)
```

**优先级**: P1

---

## 五、错误场景边界测试

### 5.1 网络错误

#### TC-ERROR-001: Agent连接中断（必过）
**场景**: 任务执行中Agent连接断开

**输入**:
```go
// 长时间运行的任务
script := `
echo "Starting long task"
sleep 60
echo "Task done"
`
```

**操作**:
```go
run := TriggerExecution(pipeline.ID, ExecutionRequest{
    Config: toJSON(PipelineConfig{
        Nodes: []Node{{
            ID:   "build",
            Type: "shell",
            Config: map[string]interface{}{"script": script},
        }},
    }),
})

// 模拟Agent在执行中断开连接
disconnectAgent(agentID)

// 等待重连
time.Sleep(10 * time.Second)

// 检查任务状态
task := findTask(run.ID, "build")
```

**预期结果**:
- 任务状态变为failed或timeout
- 错误信息: "agent connection lost"
- 不产生僵尸任务

**验证方法**:
```go
task := findTask(run.ID, "build")
assert.True(t, task.Status == "failed" || task.Status == "timeout")
assert.Contains(t, task.ErrorMsg, "connection")
```

**优先级**: P0

---

#### TC-ERROR-002: WebSocket消息丢失（推荐）
**场景**: 网络不稳定导致消息丢失

**输入**:
```go
// 模拟网络不稳定
simulateNetworkJitter(50 * time.Millisecond)
```

**操作**:
```go
run := TriggerExecution(pipeline.ID, ExecutionRequest{})
task := findTask(run.ID, "build")

// 发送大量日志
for i := 0; i < 1000; i++ {
    sendTaskLog(task.ID, fmt.Sprintf("Log %d", i))
}
```

**预期结果**:
- 丢失消息有补偿机制
- 日志最终一致
- 无数据丢失或重复

**验证方法**:
```go
// 检查日志完整性
logs := getTaskLogs(task.ID)
expectedLogs := 1000
assert.GreaterOrEqual(t, len(logs), expectedLogs*0.99) // 允许1%丢失
```

**优先级**: P1

---

#### TC-ERROR-003: 数据库连接失败（必过）
**场景**: 任务执行中数据库连接失败

**输入**:
```go
// 模拟数据库连接失败
simulateDBFailure()
```

**操作**:
```go
run := TriggerExecution(pipeline.ID, ExecutionRequest{})
time.Sleep(5 * time.Second)

// 恢复数据库
restoreDB()

// 检查任务状态
task := findTask(run.ID, "build")
```

**预期结果**:
- 任务状态为failed
- 错误信息: "database connection failed"
- 任务可重试

**验证方法**:
```go
task := findTask(run.ID, "build")
assert.Equal(t, "failed", task.Status)
assert.Contains(t, task.ErrorMsg, "database")
```

**优先级**: P0

---

### 5.2 资源不足

#### TC-ERROR-004: 磁盘空间不足（推荐）
**场景**: Agent磁盘空间不足

**输入**:
```go
// 模拟磁盘满
fillDisk(agentID)
```

**操作**:
```go
run := TriggerExecution(pipeline.ID, ExecutionRequest{
    Config: toJSON(PipelineConfig{
        Nodes: []Node{{
            ID:   "build",
            Type: "shell",
            Config: map[string]interface{}{
                "script": "echo hello > /tmp/large_file",
            },
        }},
    }),
})
```

**预期结果**:
- 任务失败
- 错误信息: "disk space exhausted"
- Agent状态正常

**验证方法**:
```go
task := findTask(run.ID, "build")
assert.Equal(t, "failed", task.Status)
assert.Contains(t, task.ErrorMsg, "disk")
```

**优先级**: P1

---

#### TC-ERROR-005: 内存不足（推荐）
**场景**: 任务消耗过多内存

**输入**:
```go
script := `
#!/bin/bash
# 消耗500MB内存
dd if=/dev/zero of=/dev/shm/test bs=1M count=500
`
```

**操作**:
```go
run := TriggerExecution(pipeline.ID, ExecutionRequest{
    Config: toJSON(PipelineConfig{
        Nodes: []Node{{
            ID:   "build",
            Type: "shell",
            Config: map[string]interface{}{"script": script},
            Timeout: 30,
        }},
    }),
})
```

**预期结果**:
- 任务失败或被杀
- 错误信息: "memory exhausted"或"killed"
- Agent正常运行

**验证方法**:
```go
task := findTask(run.ID, "build")
assert.True(t, task.Status == "failed" || task.Status == "timeout")
assert.Contains(t, task.ErrorMsg, "memory") || assert.Contains(t, task.ErrorMsg, "killed")
```

**优先级**: P1

---

## 六、特殊场景测试

### 6.1 空值/空指针

#### TC-NULL-001: 空Config处理（必过）
**场景**: Pipeline Config为null或空

**输入**:
```go
configs := []string{
    "null",
    "",
    "{}",
}
```

**操作**:
```go
for _, config := range configs {
    err := CreatePipeline(CreatePipelineRequest{
        Name:     "Test",
        ProjectID: 1,
        Config:   config,
    })
    // 验证
}
```

**预期结果**:
- 返回错误: "config is required"或"invalid config format"
- Pipeline未创建

**优先级**: P0

---

#### TC-NULL-002: 空Nodes数组（必过）
**场景**: Pipeline Config中nodes为空数组

**输入**:
```go
config := `{"nodes": [], "edges": []}`
```

**操作**:
```go
err := CreatePipeline(CreatePipelineRequest{
    Name:     "Test",
    ProjectID: 1,
    Config:   config,
})
```

**预期结果**:
- 返回错误: "nodes cannot be empty"
- Pipeline未创建

**优先级**: P0

---

#### TC-NULL-003: 任务输出为null（必过）
**场景**: 任务outputs字段为null

**输入**:
```go
// 任务正常执行但outputs为null
script := `echo "hello"`
```

**操作**:
```go
run := TriggerExecution(pipeline.ID, ExecutionRequest{})
task := findTask(run.ID, "build")
```

**预期结果**:
- 任务执行成功
- outputs默认为空对象{}
- 下游任务可正常执行（无引用outputs）

**验证方法**:
```go
assert.Equal(t, "success", task.Status)
assert.NotNil(t, task.Outputs)  // 不为nil
assert.Empty(t, task.Outputs)   // 但为空
```

**优先级**: P0

---

### 6.2 特殊字符

#### TC-SPECIAL-004: 脚本中的特殊字符（必过）
**场景**: Shell脚本包含特殊字符

**输入**:
```go
script := `
#!/bin/bash
echo "Hello World"
echo 'Single quotes'
echo $HOME
echo "Path: $PATH"
echo "Backtick: \`date\`"
echo "Newline: 
Second line"
echo "Special: !@#$%^&*()"
`
```

**操作**:
```go
run := TriggerExecution(pipeline.ID, ExecutionRequest{
    Config: toJSON(PipelineConfig{
        Nodes: []Node{{
            ID:   "build",
            Type: "shell",
            Config: map[string]interface{}{"script": script},
        }},
    }),
})
```

**预期结果**:
- 脚本正确执行
- 无注入风险
- 特殊字符被正确转义

**验证方法**:
```go
task := findTask(run.ID, "build")
assert.Equal(t, "success", task.Status)
assert.Contains(t, task.Stdout, "Hello World")
assert.Contains(t, task.Stdout, "Single quotes")
```

**优先级**: P0

---

#### TC-SPECIAL-005: Pipeline名称包含特殊字符（必过）
**场景**: Pipeline名称包含emoji或特殊Unicode

**输入**:
```go
names := []string{
    "Pipeline with spaces",
    "流水线-中文",
    "パイプライン",
    "Pipeline 🚀",
    "Pipeline\twith\ttabs",
}
```

**操作**:
```go
for _, name := range names {
    resp := CreatePipeline(CreatePipelineRequest{
        Name:     name,
        ProjectID: 1,
        Config:   validConfig,
    })
    // 验证
}
```

**预期结果**:
- 创建成功（如果允许）
- 或返回错误（如果不支持）
- 名称正确存储和显示

**验证方法**:
```go
pipeline := db.First(&Pipeline{}, resp.Data.ID)
assert.Equal(t, name, pipeline.Name)

// 前端显示正确
page := navigateToPipelineDetail(pipeline.ID)
assertText(t, page, ".pipeline-name", name)
```

**优先级**: P1

---

## 七、测试覆盖汇总

### 边界条件测试统计

| 类别 | P0用例 | P1用例 | P2用例 | 总计 |
|------|--------|--------|--------|------|
| 输入边界 | 5 | 4 | 0 | 9 |
| 状态转换 | 4 | 1 | 0 | 5 |
| 时间边界 | 2 | 2 | 1 | 5 |
| 资源限制 | 2 | 3 | 0 | 5 |
| 错误场景 | 3 | 4 | 0 | 7 |
| 特殊场景 | 2 | 2 | 0 | 4 |
| **总计** | **18** | **16** | **1** | **35** |

### 原始测试用例 + 边界测试

| 类型 | 数量 |
|------|------|
| 原始测试用例 | 43 |
| 边界条件测试 | 35 |
| **总计** | **78** |

---

## 八、边界值速查表

### 8.1 数字边界值

| 字段 | 最小值 | 默认值 | 最大值 | 超出处理 |
|------|--------|--------|--------|----------|
| Pipeline Name长度 | 1 | - | 128 | 拒绝 |
| Node ID长度 | 1 | - | 64 | 拒绝 |
| Script长度 | 0 | - | 65535 | 警告/截断 |
| Description长度 | 0 | - | 65536 | 拒绝/截断 |
| Timeout | 0 | 3600 | 86400 | 使用默认 |
| RetryCount | 0 | 0 | 10 | 拒绝/警告 |
| Node Count | 1 | - | 100 | 拒绝 |
| Max Parallel | 1 | CPU核心数 | 100 | 使用最大值 |

### 8.2 时间边界值

| 场景 | 边界值 | 处理 |
|------|--------|------|
| 最短任务 | 1ms | 正确记录 |
| 最长任务 | 24h | 超时控制 |
| 状态保留 | 90天 | 归档策略 |
| 日志保留 | 30天 | 自动清理 |

### 8.3 大小边界值

| 资源 | 限制 | 处理 |
|------|------|------|
| Config JSON | 1MB | 拒绝/压缩 |
| 单任务日志 | 100MB | 截断/存储 |
| 总日志 | 无限制 | 清理策略 |
| Artifact | 1GB | 拒绝/存储 |

---

**文档版本**: 1.0
**创建日期**: 2026-02-02
**类型**: 边界条件测试用例补充
