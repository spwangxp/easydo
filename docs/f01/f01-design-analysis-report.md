# F01 功能设计完整性与测试覆盖分析

## 文档信息
- **文档版本**: F01 v1.0
- **分析日期**: 2026-02-02
- **设计文档**: docs/f01.md, docs/f01_websocket.md
- **当前代码**: easydo-server/internal/handlers/pipeline.go

---

## 第一部分：设计完整性分析

### 1.1 模块清单与状态评估

#### A. Pipeline Configuration（流水线配置）
| 组件 | 状态 | 评估 |
|------|------|------|
| Pipeline结构体 | ✅ 完整 | 已定义ID、Name、Nodes、Edges |
| PipelineNode结构体 | ⚠️ 部分 | 缺少When、AllowFailure、Retry字段 |
| Edge结构体 | ✅ 完整 | 已定义From、To |
| 验证逻辑 | ✅ 完整 | DAG验证已实现 |

**缺失详情**:
- PipelineNode.When: 执行条件（需要ConditionEvaluator支持）
- PipelineNode.AllowFailure: 是否允许失败（需要错误处理逻辑）
- PipelineNode.Retry: 重试次数（需要重试机制）

#### B. DAG Execution Engine（DAG执行引擎）
| 组件 | 状态 | 评估 |
|------|------|------|
| 拓扑排序 | ✅ 完整 | 已实现Kahn算法 |
| 层级计算 | ⚠️ 部分 | 已有串行BFS，缺少并行执行 |
| 任务调度 | ❌ 缺失 | 无并行调度器 |
| 依赖解析 | ⚠️ 部分 | 已有基础依赖解析，缺少复杂场景 |

**缺失详情**:
- executePipelineTasksEnhanced: 并行执行版本未实现
- Level-based并行执行: 需要实现任务分组和并行调度
- 任务状态同步: 并行执行时的状态一致性

#### C. ConditionEvaluator（条件表达式求值器）
| 组件 | 状态 | 评估 |
|------|------|------|
| 变量解析 | ⚠️ 部分 | 有伪代码，缺少实现 |
| 表达式解析 | ⚠️ 部分 | 有伪代码，缺少实现 |
| 安全求值 | ⚠️ 部分 | 有伪代码，缺少实现 |
| 操作符支持 | ✅ 完整 | ==, !=, >, <, &&, \|\| |

**缺失详情**:
- 完整的ConditionEvaluator实现（0个方法已实现）
- 嵌套括号处理
- 变量路径解析（outputs.*, inputs.*, env.*）

#### D. Task Executor（任务执行器）
| 组件 | 状态 | 评估 |
|------|------|------|
| Shell任务 | ✅ 完整 | 已实现 |
| Docker任务 | ✅ 完整 | 已实现 |
| Git Clone任务 | ✅ 完整 | 已实现 |
| Email任务 | ⚠️ 部分 | executeEmailTask存在，缺少完整实现 |
| Webhook任务 | ❌ 缺失 | executeWebhookTask未实现 |
| Agent选择 | ⚠️ 部分 | 有基础选择逻辑，缺少负载均衡优化 |

**缺失详情**:
- Webhook通知的完整实现
- Agent选择算法优化（资源感知、优先级）
- 任务超时处理

#### E. Variable Resolver（变量解析器）
| 组件 | 状态 | 评估 |
|------|------|------|
| 全局变量 | ✅ 完整 | BuildGlobalEnvVars已实现 |
| 任务输出解析 | ✅ 完整 | SetTaskOutput, ResolveVariables已实现 |
| Inputs解析 | ⚠️ 部分 | 有基础支持，缺少完整Inputs管理 |
| 循环引用检测 | ❌ 缺失 | 缺少变量引用循环检测 |

#### F. WebSocket Communication（WebSocket通信）
| 组件 | 状态 | 评估 |
|------|------|------|
| Server端WebSocket | ✅ 完整 | WebSocketHandler已实现 |
| Agent端WebSocket | ✅ 完整 | WebSocketClient已实现 |
| 消息格式定义 | ⚠️ 部分 | 有基础消息类型，缺少完整定义 |
| 前端WebSocket客户端 | ❌ 缺失 | 无前端实现 |
| 心跳机制 | ⚠️ 部分 | 有心跳支持，缺少详细设计 |

**缺失详情**:
- 前端WebSocket客户端（easydo-frontend）
- 完整的消息类型枚举
- 断线重连机制设计
- 消息队列设计

#### G. Agent Management（Agent管理）
| 组件 | 状态 | 评估 |
|------|------|------|
| Agent注册 | ✅ 完整 | RegisterAgent已实现 |
| Agent状态管理 | ✅ 完整 | Status字段已定义 |
| Agent审批 | ✅ 完整 | RegistrationStatus已实现 |
| Agent选择 | ⚠️ 部分 | 有基础选择，缺少智能调度 |

#### H. Error Handling（错误处理）
| 组件 | 状态 | 评估 |
|------|------|------|
| 任务失败处理 | ⚠️ 部分 | 有基础处理，缺少重试机制 |
| 错误传播 | ⚠️ 部分 | 有updateRunStatus，缺少详细错误分类 |
| 失败恢复 | ❌ 缺失 | 无检查点/恢复机制 |

#### I. Logging & Monitoring（日志监控）
| 组件 | 状态 | 评估 |
|------|------|------|
| 执行日志 | ⚠️ 部分 | 有AgentLog，缺少完整日志级别 |
| 实时日志传输 | ⚠️ 部分 | 有WebSocket日志，缺少详细格式定义 |
| 性能监控 | ❌ 缺失 | 无性能指标采集 |

---

### 1.2 设计矛盾与模糊点

#### 矛盾点 1：Task Type判断逻辑
**文档描述**: 支持email、webhook等Server端任务类型

**当前代码** (pipeline.go:796):
```go
if taskType != "shell" && taskType != "docker" && taskType != "git_clone" && taskType != "agent" {
    return true, nil  // 错误：跳过了webhook！
}
```

**矛盾**: 代码逻辑与文档描述不一致

#### 模糊点 1：When条件的执行时机
**问题**: When条件应该在任务执行前判断，还是在任务调度时判断？

**建议**:
- 在任务进入执行队列前判断
- 条件不满足时，标记任务为SKIPPED状态
- 支持后续手动触发跳过任务的执行

#### 模糊点 2：并行度的配置
**问题**: 并行度是全局配置还是节点级别配置？

**建议**:
- 全局配置：max_parallel_tasks
- 节点级别覆盖：node.max_parallel
- 默认值：CPU核心数

#### 模糊点 3：变量解析的时机
**问题**: 变量解析应该在任务执行前还是任务调度前？

**建议**:
- 调度前：解析inputs.*
- 执行前：解析outputs.*（上游任务完成后）
- 即时解析：执行时遇到变量引用时

---

### 1.3 缺失的关键设计

#### 1.3.1 重试机制设计
**缺失内容**:
- 重试策略配置（指数退避、固定间隔）
- 重试次数限制
- 重试条件（什么错误需要重试）
- 重试日志记录

#### 1.3.2 任务超时处理
**缺失内容**:
- 超时时间配置（全局/节点级别）
- 超时后的处理策略（取消、重试、继续）
- 超时时间统计

#### 1.3.3 任务依赖失败处理
**缺失内容**:
- 上游任务失败后，下游任务的行为
- When条件与失败状态的交互
- 失败传播规则

#### 1.3.4 资源配额管理
**缺失内容**:
- Agent资源配额（CPU、内存、并发数）
- 资源抢占策略
- 资源监控告警

---

## 第二部分：测试用例矩阵

### 2.1 Pipeline Configuration 测试

#### TC-PIPELINE-001: 有效DAG配置（必过）
**输入**:
```yaml
nodes:
  - id: build
    type: shell
  - id: test
    type: shell
  - id: deploy
    type: shell
edges:
  - from: build
    to: test
  - from: test
    to: deploy
```

**操作**: 调用ValidateDAG()

**预期输出**: 返回nil（验证通过）

**优先级**: P0

---

#### TC-PIPELINE-002: 无效循环DAG（必过）
**输入**:
```yaml
nodes:
  - id: a
    type: shell
  - id: b
    type: shell
edges:
  - from: a
    to: b
  - from: b
    to: a
```

**操作**: 调用ValidateDAG()

**预期输出**: 返回error（检测到循环）

**优先级**: P0

---

#### TC-PIPELINE-003: 多根节点DAG（必过）
**输入**:
```yaml
nodes:
  - id: init
    type: shell
  - id: build
    type: shell
  - id: deploy
    type: shell
edges:
  - from: init
    to: build
  - from: init
    to: deploy
```

**操作**: 调用BuildPlan()

**预期输出**: MaxLevel=2, LevelMap正确

**优先级**: P0

---

#### TC-PIPELINE-004: 菱形依赖DAG（必过）
**输入**:
```yaml
nodes:
  - id: start
    type: shell
  - id: task1
    type: shell
  - id: task2
    type: shell
  - id: end
    type: shell
edges:
  - from: start
    to: task1
  - from: start
    to: task2
  - from: task1
    to: end
  - from: task2
    to: end
```

**操作**: 调用BuildPlan()

**预期输出**: 
- start.Level = 0
- task1.Level = 1
- task2.Level = 1
- end.Level = 2

**优先级**: P0

---

#### TC-PIPELINE-005: PipelineNode完整字段（推荐）
**输入**: 包含When、AllowFailure、Retry的节点配置

**操作**: 解析节点配置

**预期输出**: 所有字段正确解析

**优先级**: P1

---

### 2.2 DAG Execution Engine 测试

#### TC-DAG-001: 串行执行（必过）
**配置**: 3个线性依赖的任务

**操作**: 执行executePipelineTasks

**预期输出**: 
- 按顺序执行：task1 → task2 → task3
- 所有任务成功
- 最终状态success

**优先级**: P0

---

#### TC-DAG-002: 并行执行（必过）
**配置**: 2个无依赖的任务

**操作**: 执行executePipelineTasksEnhanced

**预期输出**:
- task1和task2并行执行
- 执行时间 < 2 * 单任务时间

**优先级**: P0

---

#### TC-DAG-003: 并行+串行混合（必过）
**配置**:
```
A(Level 0) ──┬──▶ C(Level 1) ──▶ D(Level 2)
             │
B(Level 0) ──┘
```

**操作**: 执行executePipelineTasksEnhanced

**预期输出**:
- A和B并行执行
- C等待A和B完成后执行
- D等待C完成后执行

**优先级**: P0

---

#### TC-DAG-004: 任务失败处理（必过）
**配置**: 3个线性任务，task2会失败

**操作**: 执行executePipelineTasks

**预期输出**:
- task1成功
- task2失败
- task3不执行
- Pipeline状态failed

**优先级**: P0

---

#### TC-DAG-005: AllowFailure任务失败（必过）
**配置**: task2设置AllowFailure=true，task3依赖task2

**操作**: 执行executePipelineTasks

**预期输出**:
- task1成功
- task2失败但标记为warning
- task3执行（因为task2虽然失败但允许）
- Pipeline状态warning

**优先级**: P0

---

#### TC-DAG-006: 任务超时处理（推荐）
**配置**: 任务设置Timeout=5秒

**操作**: 执行超时任务

**预期输出**:
- 5秒后任务被取消
- 任务状态为timeout

**优先级**: P1

---

#### TC-DAG-007: 任务重试（推荐）
**配置**: 任务设置Retry=3

**操作**: 执行会失败一次的任务

**预期输出**:
- 任务失败
- 自动重试
- 最终成功
- 重试次数=1

**优先级**: P1

---

#### TC-DAG-008: 资源限制（推荐）
**配置**: 全局max_parallel=2

**操作**: 执行4个并行任务

**预期输出**:
- 最多2个任务同时执行
- 总执行时间合理

**优先级**: P1

---

### 2.3 ConditionEvaluator 测试

#### TC-COND-001: 空条件（必过）
**输入**: condition = ""

**操作**: Evaluate("")

**预期输出**: true, nil

**优先级**: P0

---

#### TC-COND-002: 简单相等比较（必过）
**输入**: condition = `${outputs.build.status} == "success"`

**上下文**:
```go
Outputs: {"build": {"status": "success"}}
```

**操作**: Evaluate(condition)

**预期输出**: true, nil

**优先级**: P0

---

#### TC-COND-003: 数值比较>（必过）
**输入**: condition = `${outputs.test.exit_code} > 0`

**上下文**:
```go
Outputs: {"test": {"exit_code": 1}}
```

**操作**: Evaluate(condition)

**预期输出**: true, nil

**优先级**: P0

---

#### TC-COND-004: 逻辑AND（必过）
**输入**: condition = `${outputs.a} == "x" && ${outputs.b} == "y"`

**上下文**:
```go
Outputs: {"a": "x", "b": "y"}
```

**操作**: Evaluate(condition)

**预期输出**: true, nil

**优先级**: P0

---

#### TC-COND-005: 逻辑OR（必过）
**输入**: condition = `${outputs.a} == "x" || ${outputs.b} == "y"`

**上下文**:
```go
Outputs: {"a": "failed", "b": "success"}
```

**操作**: Evaluate(condition)

**预期输出**: true, nil

**优先级**: P0

---

#### TC-COND-006: 嵌套括号（必过）
**输入**: condition = `(${outputs.a} == "x" && ${outputs.b} == "y") || ${outputs.c} == "z"`

**上下文**:
```go
Outputs: {"a": "x", "b": "failed", "c": "z"}
```

**操作**: Evaluate(condition)

**预期输出**: true, nil

**优先级**: P0

---

#### TC-COND-007: 变量不存在（必过）
**输入**: condition = `${outputs.nonexistent.status} == "success"`

**上下文**:
```go
Outputs: {}
```

**操作**: Evaluate(condition)

**预期输出**: false, nil

**优先级**: P0

---

#### TC-COND-008: 混合比较（必过）
**输入**: condition = `${outputs.build.status} == "success" && ${inputs.env} == "production"`

**上下文**:
```go
Outputs: {"build": {"status": "success"}}
Inputs: {"env": "production"}
```

**操作**: Evaluate(condition)

**预期输出**: true, nil

**优先级**: P0

---

#### TC-COND-009: 环境变量（推荐）
**输入**: condition = `env.BRANCH == "main"`

**上下文**:
```go
Env: {"BRANCH": "main"}
```

**操作**: Evaluate(condition)

**预期输出**: true, nil

**优先级**: P1

---

#### TC-COND-010: 字符串!=比较（推荐）
**输入**: condition = `${outputs.branch} != "develop"`

**上下文**:
```go
Outputs: {"branch": "main"}
```

**操作**: Evaluate(condition)

**预期输出**: true, nil

**优先级**: P1

---

#### TC-COND-011: 比较操作符>= <=（推荐）
**输入**: 
- condition1 = `${outputs.code} >= 200`
- condition2 = `${outputs.code} <= 500`

**上下文**:
```go
Outputs: {"code": 200}
```

**操作**: Evaluate(condition)

**预期输出**: true, true

**优先级**: P1

---

#### TC-COND-012: 复杂表达式（推荐）
**输入**: condition = `${outputs.a} == "x" && (${outputs.b} == "y" || ${outputs.c} == "z") && ${outputs.d} != "failed"`

**上下文**:
```go
Outputs: {
  "a": "x",
  "b": "y",
  "c": "failed",
  "d": "success"
}
```

**操作**: Evaluate(condition)

**预期输出**: true, nil

**优先级**: P1

---

### 2.4 Variable Resolver 测试

#### TC-VAR-001: 全局变量解析（必过）
**输入**: 包含${global.*}的脚本

**操作**: ResolveVariables(script)

**预期输出**: 全局变量被正确替换

**优先级**: P0

---

#### TC-VAR-002: 任务输出解析（必过）
**输入**: 包含${outputs.task1.field}的脚本

**操作**: ResolveVariables(script)

**预期输出**: 任务输出被正确替换

**优先级**: P0

---

#### TC-VAR-003: 变量引用不存在（必过）
**输入**: 包含${outputs.nonexistent.field}的脚本

**操作**: ResolveVariables(script)

**预期输出**: 保留原引用或返回错误

**优先级**: P0

---

#### TC-VAR-004: 循环引用检测（推荐）
**输入**: 变量A依赖变量B，变量B依赖变量A

**操作**: ResolveVariables(script)

**预期输出**: 检测到循环并返回错误

**优先级**: P1

---

#### TC-VAR-005: 嵌套变量解析（推荐）
**输入**: 变量值中包含另一个变量引用

**操作**: ResolveVariables(script)

**预期输出**: 递归解析所有变量

**优先级**: P1

---

### 2.5 Task Executor 测试

#### TC-TASK-001: Shell任务执行（必过）
**配置**: shell类型任务，脚本="echo hello"

**操作**: 执行任务

**预期输出**:
- 退出码0
- 输出"hello"
- 任务状态success

**优先级**: P0

---

#### TC-TASK-002: Docker任务执行（必过）
**配置**: docker类型任务，镜像="alpine"

**操作**: 执行任务

**预期输出**:
- 容器启动成功
- 任务状态success

**优先级**: P0

---

#### TC-TASK-003: Git Clone任务执行（必过）
**配置**: git_clone类型任务，仓库URL

**操作**: 执行任务

**预期输出**:
- 仓库克隆成功
- 任务状态success

**优先级**: P0

---

#### TC-TASK-004: Email任务执行（推荐）
**配置**: email类型任务

**操作**: 执行任务

**预期输出**:
- 邮件发送成功
- 任务状态success

**优先级**: P1

---

#### TC-TASK-005: Webhook任务执行（推荐）
**配置**: webhook类型任务，URL

**操作**: 执行任务

**预期输出**:
- HTTP请求发送成功
- 任务状态success

**优先级**: P1

---

#### TC-TASK-006: 任务执行失败（必过）
**配置**: 脚本会失败的shell任务

**操作**: 执行任务

**预期输出**:
- 退出码非0
- 任务状态failed

**优先级**: P0

---

#### TC-TASK-007: 任务超时（推荐）
**配置**: 睡眠100秒的任务，Timeout=5

**操作**: 执行任务

**预期输出**:
- 5秒后任务被取消
- 任务状态timeout

**优先级**: P1

---

#### TC-TASK-008: 任务重试（推荐）
**配置**: 首次会失败的任务，Retry=2

**操作**: 执行任务

**预期输出**:
- 第1次失败
- 第2次成功
- 任务状态success
- 重试次数=1

**优先级**: P1

---

### 2.6 WebSocket Communication 测试

#### TC-WS-001: Agent注册（必过）
**配置**: 新Agent连接

**操作**: 发送注册消息

**预期输出**:
- Agent注册成功
- 状态变为online

**优先级**: P0

---

#### TC-WS-002: 心跳保持（必过）
**配置**: 已注册的Agent

**操作**: 定期发送心跳

**预期输出**:
- 心跳超时时间刷新
- Agent保持online状态

**优先级**: P0

---

#### TC-WS-003: 心跳超时（必过）
**配置**: 已注册的Agent

**操作**: 不发送心跳，等待超时

**预期输出**:
- Agent状态变为offline

**优先级**: P0

---

#### TC-WS-004: 任务下发（必过）
**配置**: 在线Agent

**操作**: 下发任务

**预期输出**:
- Agent收到任务
- 任务状态变为running

**优先级**: P0

---

#### TC-WS-005: 日志传输（必过）
**配置**: 执行中的任务

**操作**: Agent发送日志

**预期输出**:
- Server收到日志
- 实时传输到前端

**优先级**: P0

---

#### TC-WS-006: 任务结果上报（必过）
**配置**: 完成的任务

**操作**: Agent上报结果

**预期输出**:
- 任务状态变为success/failed
- 输出结果被保存

**优先级**: P0

---

#### TC-WS-007: 断线重连（推荐）
**配置**: 执行任务的Agent

**操作**: Agent断线后重连

**预期输出**:
- 任务状态恢复
- 继续执行

**优先级**: P1

---

#### TC-WS-008: 消息丢失处理（推荐）
**配置**: 网络不稳定

**操作**: 发送消息，部分丢失

**预期输出**:
- 检测到消息丢失
- 重新同步状态

**优先级**: P1

---

### 2.7 Integration Tests（集成测试）

#### TC-INT-001: 完整流水线执行（必过）
**配置**: 完整的Pipeline配置（5个任务）

**操作**: 执行完整流水线

**预期输出**:
- 所有任务按依赖顺序执行
- 变量正确传递
- 最终状态success

**优先级**: P0

---

#### TC-INT-002: 复杂依赖流水线（必过）
**配置**: 10个任务，复杂DAG依赖

**操作**: 执行流水线

**预期输出**:
- 任务按层级并行执行
- 无死锁
- 正确完成

**优先级**: P0

---

#### TC-INT-003: 条件执行流水线（推荐）
**配置**: 包含When条件的任务

**操作**: 执行流水线

**预期输出**:
- 条件满足的任务执行
- 条件不满足的任务跳过

**优先级**: P1

---

#### TC-INT-004: 失败恢复流水线（推荐）
**配置**: 包含重试机制的任务

**操作**: 执行会失败一次的任务

**预期输出**:
- 首次失败
- 自动重试
- 最终成功

**优先级**: P1

---

#### TC-INT-005: 并行压力测试（推荐）
**配置**: 100个无依赖任务

**操作**: 执行流水线

**预期输出**:
- 并发执行正常
- 无内存泄漏
- 性能达标

**优先级**: P1

---

## 第三部分：实现优先级排序

### P0 - 必须在第一版实现

| 序号 | 模块 | 任务 | 预估工作量 |
|------|------|------|------------|
| 1 | Pipeline配置 | 修复Task Type判断Bug | 1小时 |
| 2 | Pipeline配置 | 添加When/AllowFailure/Retry字段 | 2小时 |
| 3 | ConditionEvaluator | 实现完整的条件求值器 | 8小时 |
| 4 | DAG Engine | 实现并行执行引擎 | 12小时 |
| 5 | 任务执行器 | 实现executeWebhookTask | 4小时 |
| 6 | 测试 | 编写P0测试用例 | 8小时 |

**总计P0**: 约35小时（5人天）

---

### P1 - 应该在第二版实现

| 序号 | 模块 | 任务 | 预估工作量 |
|------|------|------|------------|
| 7 | 任务执行器 | 实现重试机制 | 8小时 |
| 8 | 任务执行器 | 实现超时处理 | 4小时 |
| 9 | 变量解析器 | 实现循环引用检测 | 4小时 |
| 10 | WebSocket | 实现前端WebSocket客户端 | 8小时 |
| 11 | 错误处理 | 实现详细错误分类 | 4小时 |
| 12 | 测试 | 编写P1测试用例 | 8小时 |

**总计P1**: 约36小时（5人天）

---

### P2 - 可以在后续版本实现

| 序号 | 模块 | 任务 | 预估工作量 |
|------|------|------|------------|
| 13 | 资源管理 | 实现资源配额管理 | 16小时 |
| 14 | 监控 | 实现性能监控 | 12小时 |
| 15 | 监控 | 实现详细日志系统 | 8小时 |
| 16 | 恢复 | 实现检查点/恢复机制 | 16小时 |
| 17 | 调度 | 实现智能Agent调度 | 12小时 |

**总计P2**: 约64小时（8人天）

---

## 第四部分：潜在风险与解决方案

### 风险1：ConditionEvaluator安全漏洞
**描述**: 如果使用eval()，可能导致代码注入

**解决方案**:
- 实现自定义表达式解析器（不使用eval）
- 白名单验证变量名
- 限制可用的操作符

**验证方法**: 编写恶意输入测试用例，确保被拒绝

---

### 风险2：并行执行的状态一致性
**描述**: 并行任务可能同时修改共享状态

**解决方案**:
- 任务输出使用Map存储，天然隔离
- 状态更新使用锁保护
- 设计只读共享状态

**验证方法**: 高并发测试，观察状态一致性

---

### 风险3：内存泄漏
**描述**: 并行执行器可能泄漏goroutine

**解决方案**:
- 所有goroutine必须使用WaitGroup
- 实现超时控制
- 定期内存prof测试

**验证方法**: 长时间运行测试，观察内存使用

---

### 风险4：WebSocket断线丢失任务
**描述**: Agent断线可能导致任务状态不一致

**解决方案**:
- 实现心跳检测
- 实现断线重连
- 任务状态持久化

**验证方法**: 模拟断线场景，验证状态恢复

---

### 风险5：变量解析性能问题
**描述**: 复杂变量引用可能导致解析缓慢

**解决方案**:
- 缓存解析结果
- 增量解析
- 编译期优化

**验证方法**: 压力测试，测量解析时间

---

## 第五部分：测试覆盖统计

### 按模块统计

| 模块 | P0用例数 | P1用例数 | 总计 |
|------|----------|----------|------|
| Pipeline配置 | 4 | 1 | 5 |
| DAG Engine | 5 | 3 | 8 |
| ConditionEvaluator | 8 | 4 | 12 |
| Variable Resolver | 3 | 2 | 5 |
| Task Executor | 4 | 4 | 8 |
| WebSocket | 4 | 4 | 8 |
| Integration | 2 | 3 | 5 |
| **总计** | **30** | **21** | **51** |

### 按优先级统计

| 优先级 | 用例数 | 占比 |
|--------|--------|------|
| P0（必须通过） | 30 | 59% |
| P1（最好通过） | 21 | 41% |
| **总计** | **51** | **100%** |

### 预期测试覆盖率

- **代码行覆盖率**: > 80%
- **分支覆盖率**: > 70%
- **边界测试覆盖率**: > 90%

---

## 第六部分：验收标准

### 功能验收标准
- [ ] 所有P0测试用例通过
- [ ] Pipeline配置正确解析
- [ ] DAG验证正确工作
- [ ] 条件表达式正确求值
- [ ] 任务按依赖关系执行
- [ ] 并行执行正常工作
- [ ] WebSocket通信正常

### 性能验收标准
- [ ] 单任务执行时间 < 1分钟
- [ ] 100任务并行执行无内存泄漏
- [ ] WebSocket心跳间隔 < 30秒
- [ ] 任务启动延迟 < 5秒

### 安全验收标准
- [ ] ConditionEvaluator无代码注入漏洞
- [ ] 变量名白名单验证
- [ ] WebSocket认证通过
- [ ] Agent权限验证通过

---

## 附录A：测试数据模板

### A.1 有效DAG配置模板
```yaml
name: "Test Pipeline"
nodes:
  - id: "build"
    type: "shell"
    name: "构建"
    config:
      script: "echo build"
  - id: "test"
    type: "shell"
    name: "测试"
    config:
      script: "echo test"
  - id: "deploy"
    type: "shell"
    name: "部署"
    config:
      script: "echo deploy"
edges:
  - from: "build"
    to: "test"
  - from: "test"
    to: "deploy"
```

### A.2 复杂DAG配置模板
```yaml
name: "Complex Pipeline"
nodes:
  - id: "init"
    type: "shell"
    config:
      script: "echo init"
  - id: "build-a"
    type: "shell"
    config:
      script: "echo build-a"
  - id: "build-b"
    type: "shell"
    config:
      script: "echo build-b"
  - id: "test-a"
    type: "shell"
    config:
      script: "echo test-a"
  - id: "test-b"
    type: "shell"
    config:
      script: "echo test-b"
  - id: "deploy"
    type: "shell"
    config:
      script: "echo deploy"
edges:
  - from: "init"
    to: "build-a"
  - from: "init"
    to: "build-b"
  - from: "build-a"
    to: "test-a"
  - from: "build-b"
    to: "test-b"
  - from: "test-a"
    to: "deploy"
  - from: "test-b"
    to: "deploy"
```

### A.3 条件执行配置模板
```yaml
name: "Conditional Pipeline"
nodes:
  - id: "build"
    type: "shell"
    config:
      script: "echo build"
  - id: "test"
    type: "shell"
    config:
      script: "echo test"
    when: "${outputs.build.status} == \"success\""
  - id: "deploy"
    type: "shell"
    config:
      script: "echo deploy"
    when: "${outputs.test.status} == \"success\" && ${inputs.environment} == \"production\""
    allow_failure: false
    retry: 3
edges:
  - from: "build"
    to: "test"
  - from: "test"
    to: "deploy"
```

---

## 附录B：测试工具选择

### B.1 单元测试
- **框架**: Go testing
- **Mock**: testify/mock
- **覆盖率**: go test -cover

### B.2 集成测试
- **框架**: Go testing + Docker
- **数据库**: Docker MySQL
- **消息队列**: Docker Redis

### B.3 端到端测试
- **工具**: Playwright
- **验证点**: 
  - Pipeline创建页面正常
  - Pipeline执行页面正常
  - 日志显示正常
  - 状态更新正常

### B.4 性能测试
- **工具**: k6或Go benchmark
- **指标**: 
  - 任务执行时间
  - 并发任务数
  - 内存使用

---

## 附录C：测试执行计划

### 第一阶段：单元测试（第1-2天）
```bash
# 执行所有单元测试
cd easydo-server
go test -v ./internal/executor/...
go test -v ./internal/handlers/...
go test -cover ./...
```

### 第二阶段：集成测试（第3-4天）
```bash
# 启动测试环境
docker-compose up -d db redis

# 执行集成测试
go test -v ./internal/... -tags=integration
```

### 第三阶段：E2E测试（第5天）
```bash
# 启动完整环境
docker-compose up -d

# 使用Playwright测试前端
playwright test tests/pipeline/
```

---

**文档编制**: AI Planning Assistant
**版本**: 1.0
**日期**: 2026-02-02
