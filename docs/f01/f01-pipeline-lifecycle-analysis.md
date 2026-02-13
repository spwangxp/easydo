# 流水线全生命周期设计与测试覆盖分析

## 分析范围
- **创建流程**: 从空白配置到保存有效的Pipeline
- **编辑流程**: 修改现有Pipeline配置
- **执行流程**: 触发PipelineRun并完成执行
- **详情流程**: 查看PipelineRun的历史记录和日志
- **删除流程**: 删除Pipeline及其关联数据

---

## 第一部分：流程设计完整性评估

### 1.1 创建流程（Create Pipeline）

#### 1.1.1 当前设计覆盖情况

| 步骤 | 设计状态 | 文档位置 | 备注 |
|------|----------|----------|------|
| 1.1.1.1 进入创建页面 | ✅ 完整 | v0.md:77-98 | 已有功能列表入口 |
| 1.1.1.2 填写基本信息 | ✅ 完整 | f01.md:4119-4125 | Name, Description, ProjectID |
| 1.1.1.3 配置节点 | ⚠️ 部分 | f01.md:33-98 | 有Config结构，缺少前端交互细节 |
| 1.1.1.4 配置依赖关系 | ⚠️ 部分 | f01.md:91-96 | 有Edges结构，缺少拖拽交互设计 |
| 1.1.1.5 设置环境变量 | ❌ 缺失 | - | 缺少全局env配置的设计 |
| 1.1.1.6 保存配置 | ✅ 完整 | f01.md:399-401 | Pipeline.Config存储策略已定义 |
| 1.1.1.7 验证配置有效性 | ⚠️ 部分 | f01.md:2850-2875 | 缺少具体的验证规则 |

**完整性评分**: 6/10 - 核心结构有定义，但前端交互和验证规则缺失

#### 1.1.2 缺失的设计细节

**1.1.2.1 节点配置交互流程**
- 缺少节点类型选择器设计
- 缺少节点配置表单模板
- 缺少节点间连接的可视化操作

**1.1.2.2 验证规则定义**
- 节点ID唯一性验证
- 边引用的节点必须存在
- DAG无环验证（前端可先做一次）
- 至少存在一个入口节点

**1.1.2.3 保存确认机制**
- 配置变更检测
- 未保存退出提示
- 版本号管理（乐观锁）

---

### 1.2 编辑流程（Edit Pipeline）

#### 1.2.1 当前设计覆盖情况

| 步骤 | 设计状态 | 文档位置 | 备注 |
|------|----------|----------|------|
| 1.2.1.1 进入编辑页面 | ✅ 完整 | v0.md:77-98 | 从详情页入口进入 |
| 1.2.1.2 加载现有配置 | ✅ 完整 | f01.md:399-401 | Config快照机制 |
| 1.2.1.3 修改配置 | ⚠️ 部分 | - | 缺少增量修改设计 |
| 1.2.1.4 配置版本管理 | ❌ 缺失 | - | 缺少版本历史设计 |
| 1.2.1.5 保存策略 | ⚠️ 部分 | f01.md:149-158 | 有快照策略，缺少保存时机 |

**完整性评分**: 5/10 - 缺少版本管理和增量修改设计

#### 1.2.2 关键问题：配置变更对执行历史的影响

**设计原则**（已有）:
```
PipelineRun.Config 是执行时的快照，不受后续 Pipeline 编辑影响
```

**但缺少的设计**:
- 是否保留配置版本历史？
- 如何回滚到历史配置？
- 变更diff展示

---

### 1.3 执行流程（Execute Pipeline）

#### 1.3.1 当前设计覆盖情况

| 步骤 | 设计状态 | 文档位置 | 备注 |
|------|----------|----------|------|
| 1.3.1.1 触发执行 | ✅ 完整 | f01.md:2254-2350 | executePipelineTasksEnhanced |
| 1.3.1.2 选择Agent | ✅ 完整 | f01.md:2114-2150 | Agent选择策略 |
| 1.3.1.3 创建执行计划 | ✅ 完整 | f01.md:1456-1574 | DAG Engine设计 |
| 1.3.1.4 保存配置快照 | ✅ 完整 | f01.md:345-401 | PipelineRun.Config |
| 1.3.1.5 按层级执行 | ✅ 完整 | f01.md:2300-2320 | Level-based并行执行 |
| 1.3.1.6 任务调度 | ✅ 完整 | f01.md:4054-4062 | Agent/Server任务分发 |
| 1.3.1.7 实时日志传输 | ✅ 完整 | f01_websocket.md | 三方实时传输设计 |
| 1.3.1.8 执行状态更新 | ✅ 完整 | f01.md:2530-2550 | updateRunStatus |
| 1.3.1.9 执行完成处理 | ✅ 完整 | f01.md:2380-2400 | 结果汇总 |

**完整性评分**: 9/10 - 执行流程设计最完整

#### 1.3.2 执行流程详细时序图（已有，补充细节）

```
执行触发 → Agent选择 → 计划生成 → 快照保存 → 任务执行 → 状态同步 → 完成

┌─────────────────────────────────────────────────────────────────────────┐
│ 详细步骤：                                                              │
│  1. 用户点击"执行"按钮                                                  │
│  2. 系统创建 PipelineRun 记录（Status=pending）                         │
│  3. 保存 Pipeline.Config 快照到 PipelineRun.Config                      │
│  4. 选择最优 Agent（负载最低 + 资源充足）                               │
│  5. 构建 DAG 执行计划                                                   │
│  6. 验证 DAG 无环                                                      │
│  7. 更新 PipelineRun.AgentID                                            │
│  8. 更新 PipelineRun.Status = running                                   │
│  9. 广播执行开始消息到前端                                               │
│ 10. 进入任务执行循环（按Level）                                          │
│ 11. 并行执行当前Level的所有任务                                         │
│ 12. 等待当前Level所有任务完成                                           │
│ 13. 收集任务输出，更新变量上下文                                         │
│ 14. 判断条件（When）决定是否执行下游任务                                │
│ 15. 循环直到所有Level完成                                               │
│ 16. 更新 PipelineRun.Status = success/failed                           │
│ 17. 广播执行完成消息到前端                                               │
└─────────────────────────────────────────────────────────────────────────┘
```

---

### 1.4 详情流程（View Pipeline Run Details）

#### 1.4.1 当前设计覆盖情况

| 步骤 | 设计状态 | 文档位置 | 备注 |
|------|----------|----------|------|
| 1.4.1.1 查看基本信息 | ✅ 完整 | v0.md:99-118 | 已有功能定义 |
| 1.4.1.2 查看配置快照 | ✅ 完整 | f01.md:399-401 | Config存储策略 |
| 1.4.1.3 查看执行历史 | ✅ 完整 | v0.md:110 | BuildHistory |
| 1.4.1.4 查看任务列表 | ✅ 完整 | f01.md:4133-4150 | AgentTask列表 |
| 1.4.1.5 查看任务日志 | ✅ 完整 | f01.md:188-255 | AgentLog设计 |
| 1.4.1.6 实时日志流 | ✅ 完整 | f01_websocket.md | WebSocket广播 |
| 1.4.1.7 查看执行统计 | ⚠️ 部分 | f01.md:518 | Duration缓存 |
| 1.4.1.8 重试执行 | ✅ 完整 | f01.md:2610-2615 | Retry机制 |

**完整性评分**: 9/10 - 几乎完整

#### 1.4.2 详情页数据展示结构（需要补充）

```
PipelineRun详情页结构：

├── 基本信息
│   ├── 运行ID: #123
│   ├── 状态: ✅ Success
│   ├── 触发方式: Manual (admin)
│   ├── 开始时间: 2026-02-02 10:00:00
│   ├── 结束时间: 2026-02-02 10:05:30
│   └── 耗时: 5分30秒
│
├── 配置快照（只读）
│   ├── PipelineConfig JSON（可展开）
│   └── 版本号: v123
│
├── 执行历史
│   ├── 本次运行的完整DAG图
│   ├── 每个节点的执行状态
│   └── 每个节点的执行时长
│
├── 任务详情（可折叠）
│   ├── Node 1: git_clone (✅ 30s)
│   │   ├── 仓库: git@github.com:xxx/repo.git
│   │   ├── 分支: main
│   │   └── 提交: abc123
│   ├── Node 2: shell (✅ 120s)
│   │   ├── 脚本: npm install && npm run build
│   │   ├── 退出码: 0
│   │   └── 输出: [查看日志]
│   └── Node 3: deploy (✅ 45s)
│       ├── 目标: production
│       └── 输出: [查看日志]
│
├── 实时日志
│   └── WebSocket实时流
│
└── 操作
    ├── 重试此运行
    ├── 查看配置快照
    └── 下载日志
```

---

### 1.5 删除流程（Delete Pipeline）

#### 1.5.1 当前设计覆盖情况

| 步骤 | 设计状态 | 文档位置 | 备注 |
|------|----------|----------|------|
| 1.5.1.1 触发删除 | ✅ 完整 | v0.md:88 | 需要确认 |
| 1.5.1.2 权限检查 | ✅ 完整 | v0.md:95 | 创建者或管理员 |
| 1.5.1.3 依赖检查 | ⚠️ 部分 | v0.md:140 | 有项目关联检查 |
| 1.5.1.4 删除确认 | ⚠️ 部分 | - | 缺少二次确认设计 |
| 1.5.1.5 级联删除 | ✅ 完整 | v0.md:140 | 关联删除PipelineRun |
| 1.5.1.6 清理Agent任务 | ✅ 完整 | f01.md:4146 | GORM外键关联 |
| 1.5.1.7 清理日志 | ✅ 完整 | f01.md:4192 | 级联删除 |

**完整性评分**: 8/10 - 缺少确认流程细节

#### 1.5.2 删除影响范围（需要补充）

```
级联删除范围：
├── Pipeline
│   ├── PipelineRun[]
│   │   ├── AgentTask[]
│   │   │   ├── TaskExecution[]
│   │   │   └── AgentLog[]
│   │   └── PipelineExecutionPlan
│   └── PipelineFavorite
│
└── 不删除
    ├── Agent（可被其他Pipeline使用）
    ├── Project（可被其他Pipeline使用）
    └── 用户（保留审计）
```

---

## 第二部分：端到端流程设计与测试用例

### 2.1 创建流程测试用例

#### TC-CREATE-001: 创建有效流水线（必过）
**前置条件**: 用户已登录，有项目权限

**操作步骤**:
1. 进入流水线列表页
2. 点击"创建流水线"按钮
3. 填写名称: "Test Pipeline"
4. 选择项目: "Test Project"
5. 添加节点1: ID="build", Type="shell", Script="echo build"
6. 添加节点2: ID="test", Type="shell", Script="echo test"
7. 添加边: From="build", To="test"
8. 点击"保存"按钮

**预期结果**:
- 创建成功，跳转到详情页
- Pipeline记录存在于数据库
- Pipeline.Config包含正确的JSON
- PipelineRun.BuildNumber = 1（历史计数从1开始）

**验证点**:
```go
// API验证
pipeline := db.First(&Pipeline{}, id)
assert.Equal("Test Pipeline", pipeline.Name)
assert.NotEmpty(pipeline.Config)
assert.Equal(uint64(1), pipeline.OwnerID)
```

**优先级**: P0

---

#### TC-CREATE-002: 创建时验证节点ID唯一（必过）
**操作步骤**:
1. 添加节点1: ID="build"
2. 添加节点2: ID="build"（重复ID）

**预期结果**:
- 显示错误提示: "节点ID 'build' 已存在"
- 无法保存

**验证点**:
```go
// 验证配置验证逻辑
config := ParsePipelineConfig(json)
errors := config.Validate()
assert.Contains(errors, "duplicate node ID: build")
```

**优先级**: P0

---

#### TC-CREATE-003: 创建时验证边引用的节点存在（必过）
**操作步骤**:
1. 添加节点1: ID="build"
2. 添加边: From="build", To="test"（test不存在）

**预期结果**:
- 显示错误提示: "边引用的节点 'test' 不存在"
- 无法保存

**优先级**: P0

---

#### TC-CREATE-004: 创建时验证DAG无环（必过）
**操作步骤**:
1. 添加节点1: ID="a"
2. 添加节点2: ID="b"
3. 添加节点3: ID="c"
4. 添加边: From="a", To="b"
5. 添加边: From="b", To="c"
6. 添加边: From="c", To="a"（形成环）

**预期结果**:
- 显示错误提示: "配置存在循环依赖，无法保存"
- 无法保存

**优先级**: P0

---

#### TC-CREATE-005: 创建时至少需要一个入口节点（必过）
**操作步骤**:
1. 添加节点1: ID="a"
2. 添加节点2: ID="b"
3. 添加边: From="a", To="b"

**预期结果**:
- 验证通过（a是入口节点）

**操作步骤**:
1. 添加节点1: ID="a"
2. 添加节点2: ID="b"
3. 添加边: From="a", To="b"
4. 添加边: From="b", To="a"（形成环，没有入口节点）

**预期结果**:
- 显示错误提示: "DAG必须有至少一个入口节点"
- 无法保存

**优先级**: P0

---

#### TC-CREATE-006: 创建时验证必填字段（必过）
**操作步骤**:
1. 不填写名称，点击保存
2. 不选择项目，点击保存

**预期结果**:
- 显示错误提示: "名称不能为空"
- 显示错误提示: "请选择项目"

**优先级**: P0

---

#### TC-CREATE-007: 创建时验证节点必填字段（必过）
**操作步骤**:
1. 添加节点，不填写ID
2. 添加节点，不选择Type

**预期结果**:
- 显示错误提示: "节点ID不能为空"
- 显示错误提示: "请选择节点类型"

**优先级**: P0

---

#### TC-CREATE-008: 创建时验证任务类型配置（必过）
**操作步骤**:
1. 添加节点: Type="shell"，不填写Script
2. 添加节点: Type="git_clone"，不填写Repository URL

**预期结果**:
- shell类型无Script: 显示警告或错误
- git_clone类型无URL: 显示错误提示

**优先级**: P1

---

#### TC-CREATE-009: 创建后配置快照验证（推荐）
**操作步骤**:
1. 创建流水线，包含节点A→B→C
2. 直接查询数据库Pipeline.Config

**预期结果**:
- Config字段包含完整的PipelineConfig JSON
- JSON可以解析为有效的PipelineConfig对象
- JSON中的nodes和edges与输入一致

**验证点**:
```go
config := PipelineConfig{}
json.Unmarshal([]byte(pipeline.Config), &config)
assert.Len(config.Nodes, 3)
assert.Len(config.Edges, 2)
```

**优先级**: P1

---

#### TC-CREATE-010: 并发创建测试（推荐）
**操作步骤**:
1. 2个用户同时创建同名流水线

**预期结果**:
- 2个流水线都创建成功（不同的ID）
- 名称允许重复（但有唯一索引则不允许）

**优先级**: P1

---

### 2.2 编辑流程测试用例

#### TC-EDIT-001: 编辑现有流水线（必过）
**前置条件**: 存在ID=1的流水线，配置为A→B→C

**操作步骤**:
1. 进入流水线详情页
2. 点击"编辑"按钮
3. 修改节点C的Script
4. 添加节点D
5. 添加边: From="C", To="D"
6. 保存

**预期结果**:
- 更新成功
- Pipeline.Config更新为新配置
- 旧的PipelineRun不受影响

**验证点**:
```go
// 旧PipelineRun不受影响
oldRun := db.First(&PipelineRun{}, runID)
assert.Contains(oldRun.Config, "A→B→C")

// 新PipelineRun使用新配置
newRun := triggerNewRun()
assert.Contains(newRun.Config, "A→B→C→D")
```

**优先级**: P0

---

#### TC-EDIT-002: 编辑时验证配置有效性（必过）
**操作步骤**:
1. 编辑流水线
2. 删除节点A（但节点B依赖A）
3. 保存

**预期结果**:
- 显示错误提示: "无法删除节点A，存在依赖关系"
- 或自动更新依赖边

**优先级**: P0

---

#### TC-EDIT-003: 编辑时新增循环依赖检测（必过）
**操作步骤**:
1. 现有配置: A→B→C
2. 编辑时添加边: From="C", To="A"

**预期结果**:
- 显示错误提示: "配置存在循环依赖，无法保存"

**优先级**: P0

---

#### TC-EDIT-004: 编辑后执行使用新配置（必过）
**前置条件**: 流水线有历史执行记录

**操作步骤**:
1. 编辑流水线（修改配置）
2. 执行流水线

**预期结果**:
- 新执行使用新配置
- 历史执行仍使用旧配置

**验证点**:
```go
// 历史执行
oldRun := db.First(&PipelineRun{}, oldRunID)
assert.Contains(oldRun.Config, "旧配置")

// 新执行
newRun := triggerNewRun()
assert.Contains(newRun.Config, "新配置")
assert.NotEqual(oldRun.Config, newRun.Config)
```

**优先级**: P0

---

#### TC-EDIT-005: 并发编辑测试（推荐）
**前置条件**: 用户A和用户B同时打开同一个流水线的编辑页

**操作步骤**:
1. 用户A修改配置并保存
2. 用户B也修改配置（不同内容）并保存

**预期结果**:
- 用户A保存成功
- 用户B保存时检测到配置已变更
- 显示冲突提示，提示用户B刷新后重试

**优先级**: P1

---

#### TC-EDIT-006: 编辑权限验证（必过）
**操作步骤**:
1. 用户A创建流水线
2. 用户B（非管理员）尝试编辑

**预期结果**:
- 显示错误: "无权编辑此流水线"
- 或跳转到403页面

**优先级**: P0

---

### 2.3 执行流程测试用例

#### TC-EXEC-001: 触发流水线执行（必过）
**前置条件**: 存在有效的流水线配置A→B→C

**操作步骤**:
1. 进入流水线详情页
2. 点击"执行"按钮
3. 确认执行

**预期结果**:
- 创建新的PipelineRun记录
- PipelineRun.Status = running
- BuildNumber递增
- 显示执行进度

**验证点**:
```go
run := triggerPipelineExecution(pipelineID)
assert.Equal("running", run.Status)
assert.Equal(previousBuildNumber+1, run.BuildNumber)
assert.NotEmpty(run.Config)
assert.NotEmpty(run.AgentID)
```

**优先级**: P0

---

#### TC-EXEC-002: 执行时选择可用Agent（必过）
**前置条件**: 存在多个在线的Agent

**操作步骤**:
1. 触发流水线执行

**预期结果**:
- 选择负载最低的Agent
- Agent.Status变为busy
- PipelineRun.AgentID正确设置

**验证点**:
```go
run := triggerPipelineExecution(pipelineID)
agent := db.First(&Agent{}, run.AgentID)
assert.Equal(models.AgentStatusOnline, agent.Status)
```

**优先级**: P0

---

#### TC-EXEC-003: 无可用Agent时执行失败（必过）
**前置条件**: 所有Agent都离线

**操作步骤**:
1. 触发流水线执行

**预期结果**:
- PipelineRun.Status = failed
- ErrorMsg = "无可用执行器"
- 不创建任何AgentTask

**验证点**:
```go
run := triggerPipelineExecution(pipelineID)
assert.Equal("failed", run.Status)
assert.Contains(run.ErrorMsg, "无可用执行器")
assert.Empty(run.AgentID)
```

**优先级**: P0

---

#### TC-EXEC-004: 任务按DAG依赖顺序执行（必过）
**配置**: A→B→C（A无依赖，B依赖A，C依赖B）

**操作步骤**:
1. 触发流水线执行

**预期结果**:
- A首先执行
- A完成后B开始执行
- B完成后C开始执行
- 不会出现B在A完成前执行

**验证点**:
```go
run := waitForCompletion(pipelineID)
tasks := getTaskExecutionOrder(run.ID)
// 验证执行顺序
assert.Equal("A", tasks[0].NodeID)
assert.Equal("B", tasks[1].NodeID)
assert.Equal("C", tasks[2].NodeID)
```

**优先级**: P0

---

#### TC-EXEC-005: 同层级任务并行执行（必过）
**配置**: A→C，B→C（A和B无依赖，C依赖A和B）

**操作步骤**:
1. 触发流水线执行
2. 记录开始时间和结束时间

**预期结果**:
- A和B并行执行
- A和B的执行时间有重叠
- C在A和B都完成后才开始

**验证点**:
```go
run := waitForCompletion(pipelineID)
a := findTask(run.ID, "A")
b := findTask(run.ID, "B")
c := findTask(run.ID, "C")

// A和B并行
assert.True(a.StartTime < b.EndTime)
assert.True(b.StartTime < a.EndTime)

// C在A和B完成后开始
assert.True(c.StartTime > a.EndTime)
assert.True(c.StartTime > b.EndTime)
```

**优先级**: P0

---

#### TC-EXEC-006: 任务失败导致下游不执行（必过）
**配置**: A→B→C，A执行成功，B执行失败

**操作步骤**:
1. 触发流水线执行

**预期结果**:
- A执行成功
- B执行失败
- C不执行
- PipelineRun.Status = failed

**验证点**:
```go
run := waitForCompletion(pipelineID)
assert.Equal("failed", run.Status)

a := findTask(run.ID, "A")
b := findTask(run.ID, "B")
c := findTask(run.ID, "C")

assert.Equal("success", a.Status)
assert.Equal("failed", b.Status)
assert.Equal("pending", c.Status) // 或skipped
```

**优先级**: P0

---

#### TC-EXEC-007: AllowFailure任务失败后下游继续执行（必过）
**配置**: A(AllowFailure=true)→B，A执行失败

**操作步骤**:
1. 触发流水线执行

**预期结果**:
- A执行失败（但标记为warning）
- B继续执行
- PipelineRun.Status = warning（如果允许）

**验证点**:
```go
run := waitForCompletion(pipelineID)
a := findTask(run.ID, "A")
b := findTask(run.ID, "B")

assert.Equal("failed", a.Status)
assert.Equal("success", b.Status)
```

**优先级**: P0

---

#### TC-EXEC-008: 条件执行When满足时执行（必过）
**配置**: A→B，B的When="${outputs.A.status} == 'success'"

**操作步骤**:
1. A执行成功
2. 触发B执行

**预期结果**:
- B执行（条件满足）

**优先级**: P0

---

#### TC-EXEC-009: 条件执行When不满足时跳过（必过）
**配置**: A→B，B的When="${outputs.A.status} == 'failed'"

**操作步骤**:
1. A执行成功
2. 触发B执行

**预期结果**:
- B跳过执行
- PipelineRun.Status = success（因为B是可选的）

**验证点**:
```go
run := waitForCompletion(pipelineID)
b := findTask(run.ID, "B")
assert.Equal("skipped", b.Status)
```

**优先级**: P0

---

#### TC-EXEC-010: 任务超时处理（推荐）
**配置**: A执行时间60秒，Timeout=30秒

**操作步骤**:
1. 触发流水线执行

**预期结果**:
- A在30秒后被强制终止
- A.Status = timeout
- PipelineRun.Status = failed

**验证点**:
```go
run := waitForCompletion(pipelineID)
a := findTask(run.ID, "A")
assert.Equal("timeout", a.Status)
assert.Less(a.Duration, 35) // 实际执行时间小于超时时间+缓冲
```

**优先级**: P1

---

#### TC-EXEC-011: 任务重试（推荐）
**配置**: B执行会首次失败，Retry=2

**操作步骤**:
1. 触发流水线执行

**预期结果**:
- B第1次执行失败
- 自动重试第2次
- 第2次执行成功
- TaskExecution记录包含2条

**验证点**:
```go
run := waitForCompletion(pipelineID)
b := findTask(run.ID, "B")
assert.Equal("success", b.Status)

executions := db.Where("task_id = ?", b.ID).Order("attempt").Find(&[]TaskExecution{})
assert.Len(executions, 2)
assert.Equal(1, executions[0].Attempt)
assert.Equal(2, executions[1].Attempt)
```

**优先级**: P1

---

#### TC-EXEC-012: 执行中断与恢复（推荐）
**配置**: 长时间运行的流水线

**操作步骤**:
1. 执行过程中停止Agent
2. Agent恢复后重连

**预期结果**:
- 任务状态更新为running
- 任务继续执行
- 不丢失执行进度

**优先级**: P1

---

### 2.4 详情流程测试用例

#### TC-VIEW-001: 查看流水线详情（必过）
**前置条件**: 存在历史执行记录的流水线

**操作步骤**:
1. 进入流水线详情页

**预期结果**:
- 显示基本信息（名称、描述、项目）
- 显示配置快照（可展开）
- 显示执行历史列表
- 显示当前执行状态（如果有）

**验证点**:
```go
page := navigateToPipelineDetail(pipelineID)
assertTitle("Test Pipeline")
assertConfigSnapshotVisible()
assertExecutionHistoryVisible()
```

**优先级**: P0

---

#### TC-VIEW-002: 查看执行历史（必过）
**前置条件**: 流水线有3次执行记录

**操作步骤**:
1. 进入流水线详情页
2. 点击"执行历史"Tab

**预期结果**:
- 显示3条执行记录
- 每条记录包含: BuildNumber、状态、时间、耗时
- 点击可查看详情

**验证点**:
```go
history := getExecutionHistory(pipelineID)
assert.Len(history, 3)
assert.Equal(3, history[0].BuildNumber)
assert.Equal(2, history[1].BuildNumber)
assert.Equal(1, history[2].BuildNumber)
```

**优先级**: P0

---

#### TC-VIEW-003: 查看执行详情（必过）
**前置条件**: 存在执行中的PipelineRun

**操作步骤**:
1. 点击某次执行记录的"详情"按钮

**预期结果**:
- 显示DAG执行图
- 每个节点显示状态图标
- 显示当前层级
- 实时更新执行进度

**验证点**:
```go
detail := getRunDetail(runID)
assert.NotNil(detail.DAG)
assert.Len(detail.Nodes, 3)
for _, node := range detail.Nodes {
    assert.NotEmpty(node.Status)
}
```

**优先级**: P0

---

#### TC-VIEW-004: 实时日志查看（必过）
**前置条件**: 流水线正在执行中

**操作步骤**:
1. 点击正在执行的任务节点
2. 查看实时日志

**预期结果**:
- 日志实时更新
- 显示时间戳
- 显示日志来源（stdout/stderr）

**验证点**:
```go
logs := getTaskLogs(taskID)
assert.NotEmpty(logs)
for _, log := range logs {
    assert.NotEmpty(log.Timestamp)
    assert.NotEmpty(log.Message)
}
```

**优先级**: P0

---

#### TC-VIEW-005: 查看历史日志（必过）
**前置条件**: 流水线已执行完成

**操作步骤**:
1. 点击已完成的任务节点
2. 查看日志

**预期结果**:
- 显示完整日志
- 日志已持久化到数据库
- 支持搜索和过滤

**验证点**:
```go
logs := getTaskLogs(taskID)
assert.Len(logs, 100) // 假设有100条日志
assert.Equal("info", logs[0].Level)
```

**优先级**: P0

---

#### TC-VIEW-006: 查看配置快照（必过）
**前置条件**: 存在历史执行记录

**操作步骤**:
1. 点击某次执行记录
2. 展开"配置快照"

**预期结果**:
- 显示执行时的PipelineConfig JSON
- JSON格式正确，可解析
- 与执行时的配置一致

**验证点**:
```go
snapshot := getConfigSnapshot(runID)
config := PipelineConfig{}
json.Unmarshal([]byte(snapshot), &config)
assert.Len(config.Nodes, 3)
```

**优先级**: P0

---

#### TC-VIEW-007: 查看执行统计（推荐）
**前置条件**: 流水线有足够的历史执行记录

**操作步骤**:
1. 进入流水线详情页
2. 查看统计信息区域

**预期结果**:
- 显示总执行次数
- 显示成功率
- 显示平均耗时
- 显示最近执行结果

**验证点**:
```go
stats := getPipelineStats(pipelineID)
assert.Equal(10, stats.TotalRuns)
assert.Equal(80.0, stats.SuccessRate)
assert.Equal(120.5, stats.AvgDuration)
```

**优先级**: P1

---

#### TC-VIEW-008: 重试历史执行（必过）
**前置条件**: 存在失败的执行记录

**操作步骤**:
1. 点击失败执行的"重试"按钮
2. 确认重试

**预期结果**:
- 创建新的执行记录
- 新执行使用相同的配置快照
- 新执行独立于原执行

**验证点**:
```go
oldRun := getRunDetail(oldRunID)
newRun := retryExecution(oldRunID)

assert.NotEqual(oldRun.ID, newRun.ID)
assert.Equal(oldRun.Config, newRun.Config)
assert.Equal(oldRun.BuildNumber+1, newRun.BuildNumber)
```

**优先级**: P0

---

### 2.5 删除流程测试用例

#### TC-DELETE-001: 删除流水线（必过）
**前置条件**: 存在ID=1的流水线，无执行记录

**操作步骤**:
1. 进入流水线详情页
2. 点击"删除"按钮
3. 确认删除

**预期结果**:
- 流水线从列表中消失
- 数据库中Pipeline记录被删除
- 返回流水线列表页

**验证点**:
```go
deletePipeline(1)
var pipeline Pipeline
result := db.First(&pipeline, 1)
assert.Error(result.Error) // 找不到
```

**优先级**: P0

---

#### TC-DELETE-002: 删除有执行历史的流水线（必过）
**前置条件**: 存在ID=1的流水线，有执行记录

**操作步骤**:
1. 进入流水线详情页
2. 点击"删除"按钮
3. 确认删除

**预期结果**:
- 显示警告: "该流水线有X条执行记录，删除将一并删除"
- 确认后删除
- Pipeline、PipelineRun、AgentTask都被删除

**验证点**:
```go
deletePipeline(1)
assert.Empty(db.Where("pipeline_id = ?", 1).Find(&PipelineRun{}))
assert.Empty(db.Where("pipeline_run_id IN (?)", db.Model(&PipelineRun{}).Select("id").Where("pipeline_id = ?", 1)).Find(&AgentTask{}))
```

**优先级**: P0

---

#### TC-DELETE-003: 删除权限验证（必过）
**前置条件**: 用户A创建流水线，用户B尝试删除

**操作步骤**:
1. 用户B尝试删除用户A的流水线

**预期结果**:
- 显示错误: "无权删除此流水线"

**优先级**: P0

---

#### TC-DELETE-004: 删除时检查项目关联（推荐）
**前置条件**: 流水线属于某项目

**操作步骤**:
1. 删除流水线

**预期结果**:
- 流水线被删除
- 项目不受影响
- 其他流水线不受影响

**验证点**:
```go
projectID := pipeline.ProjectID
deletePipeline(pipeline.ID)

project := db.First(&Project{}, projectID)
assert.NotNil(project) // 项目仍在
```

**优先级**: P1

---

#### TC-DELETE-005: 删除时Agent不受影响（必过）
**前置条件**: Agent执行过该流水线的任务

**操作步骤**:
1. 删除流水线

**预期结果**:
- Agent记录不受影响
- Agent状态不受影响
- Agent可被其他流水线使用

**验证点**:
```go
agentID := run.AgentID
deletePipeline(pipeline.ID)

agent := db.First(&Agent{}, agentID)
assert.NotNil(agent)
assert.Equal(models.AgentStatusOnline, agent.Status)
```

**优先级**: P0

---

#### TC-DELETE-006: 级联删除验证（必过）
**前置条件**: 流水线有完整的执行记录和日志

**操作步骤**:
1. 删除流水线
2. 检查所有关联数据

**预期结果**:
- Pipeline删除
- PipelineRun删除
- AgentTask删除
- TaskExecution删除
- AgentLog删除

**验证点**:
```go
pipelineID := 1
deletePipeline(pipelineID)

assert.Empty(db.Where("pipeline_id = ?", pipelineID).Find(&PipelineRun{}))
assert.Empty(db.Where("pipeline_run_id IN (?)", 
    db.Model(&PipelineRun{}).Select("id").Where("pipeline_id = ?", pipelineID)).
    Find(&AgentTask{}))
assert.Empty(db.Where("task_id IN (?)",
    db.Model(&AgentTask{}).
        Joins("JOIN pipeline_runs ON pipeline_runs.id = agent_tasks.pipeline_run_id").
        Where("pipeline_runs.pipeline_id = ?", pipelineID)).
    Find(&TaskExecution{}))
```

**优先级**: P0

---

## 第三部分：流程设计缺失清单

### 3.1 必须补充的设计

| 序号 | 缺失项 | 影响流程 | 优先级 |
|------|--------|----------|--------|
| 1 | 节点配置表单模板 | 创建、编辑 | P0 |
| 2 | DAG可视化编辑器交互设计 | 创建、编辑 | P0 |
| 3 | 配置版本历史管理 | 编辑 | P0 |
| 4 | 配置变更diff展示 | 编辑 | P1 |
| 5 | 二次确认对话框设计 | 删除 | P0 |
| 6 | 并发编辑冲突处理 | 编辑 | P1 |
| 7 | 全局环境变量配置 | 创建、编辑 | P0 |
| 8 | 流水线模板市场 | 创建 | P1 |

### 3.2 建议补充的设计

| 序号 | 缺失项 | 影响流程 | 优先级 |
|------|--------|----------|--------|
| 9 | 流水线导入导出 | 创建 | P1 |
| 10 | 批量操作（批量执行/删除） | 执行、删除 | P1 |
| 11 | 流水线收藏功能 | 详情 | P1 |
| 12 | 流水线分享功能 | 详情 | P2 |
| 13 | 执行计划预览 | 执行 | P1 |
| 14 | 执行结果对比 | 详情 | P2 |

---

## 第四部分：测试覆盖统计

### 4.1 按流程统计

| 流程 | P0用例数 | P1用例数 | 总计 |
|------|----------|----------|------|
| 创建流程 | 6 | 4 | 10 |
| 编辑流程 | 4 | 2 | 6 |
| 执行流程 | 9 | 4 | 13 |
| 详情流程 | 6 | 2 | 8 |
| 删除流程 | 4 | 2 | 6 |
| **总计** | **29** | **14** | **43** |

### 4.2 按功能统计

| 功能模块 | P0用例数 | P1用例数 | 总计 |
|----------|----------|----------|------|
| 权限验证 | 3 | 0 | 3 |
| 配置验证 | 5 | 2 | 7 |
| DAG执行 | 6 | 3 | 9 |
| 任务执行 | 4 | 2 | 6 |
| 日志查看 | 2 | 0 | 2 |
| 状态管理 | 3 | 1 | 4 |
| 级联删除 | 2 | 1 | 3 |
| 实时更新 | 1 | 1 | 2 |
| 并发处理 | 1 | 2 | 3 |
| 边界条件 | 2 | 2 | 4 |
| **总计** | **29** | **14** | **43** |

---

## 第五部分：验收标准

### 5.1 功能验收

- [ ] 所有P0测试用例通过（29个）
- [ ] 创建流程支持完整的节点配置
- [ ] 编辑流程保留版本历史
- [ ] 执行流程按DAG依赖正确执行
- [ ] 详情流程支持实时日志查看
- [ ] 删除流程正确级联清理

### 5.2 性能验收

- [ ] 流水线列表加载 < 1秒
- [ ] 执行触发响应 < 500ms
- [ ] 实时日志延迟 < 1秒
- [ ] 100次执行历史查询 < 2秒

### 5.3 安全验收

- [ ] 权限验证覆盖所有操作
- [ ] 配置文件隔离（快照机制）
- [ ] 敏感信息不泄露
- [ ] 并发操作安全

---

## 附录：完整测试用例矩阵

### A.1 创建流程测试矩阵

| 用例ID | 用例名称 | 前置条件 | 操作 | 预期结果 | 优先级 |
|--------|----------|----------|------|----------|--------|
| TC-CREATE-001 | 创建有效流水线 | 用户已登录 | 填写有效配置并保存 | 创建成功 | P0 |
| TC-CREATE-002 | 节点ID唯一性验证 | 已在编辑页 | 添加重复ID节点 | 提示错误 | P0 |
| TC-CREATE-003 | 边引用节点存在验证 | 已在编辑页 | 添加无效边 | 提示错误 | P0 |
| TC-CREATE-004 | DAG无环验证 | 已在编辑页 | 添加循环依赖 | 提示错误 | P0 |
| TC-CREATE-005 | 入口节点验证 | 已在编辑页 | 创建无入口DAG | 提示错误 | P0 |
| TC-CREATE-006 | 必填字段验证 | 已在创建页 | 留空必填字段 | 提示错误 | P0 |
| TC-CREATE-007 | 节点必填验证 | 已在编辑页 | 节点缺少必填项 | 提示错误 | P0 |
| TC-CREATE-008 | 任务类型配置验证 | 已在编辑页 | 缺少类型特定配置 | 提示警告 | P1 |
| TC-CREATE-009 | 配置快照验证 | 创建成功 | 查询数据库Config | 快照正确 | P1 |
| TC-CREATE-010 | 并发创建 | 2个用户同时创建 | 2个用户同时保存 | 2个都成功 | P1 |

### A.2 执行流程测试矩阵（补充）

| 用例ID | 用例名称 | 前置条件 | 操作 | 预期结果 | 优先级 |
|--------|----------|----------|------|----------|--------|
| TC-EXEC-013 | 执行时变量解析 | 配置含${变量} | 执行流水线 | 变量被解析 | P0 |
| TC-EXEC-014 | 任务输出传递 | A→B，B使用A输出 | 执行流水线 | B获取A输出 | P0 |
| TC-EXEC-015 | 执行取消 | 任务执行中 | 点击取消按钮 | 任务停止 | P0 |
| TC-EXEC-016 | 全部取消 | 任务执行中 | 点击全部取消 | Pipeline停止 | P0 |
| TC-EXEC-017 | Agent离线处理 | Agent正在执行 | Agent断开连接 | 任务标记失败 | P1 |
| TC-EXEC-018 | 网络恢复重连 | Agent离线后恢复 | 网络恢复 | 任务继续或重试 | P1 |

### A.3 详情流程测试矩阵（补充）

| 用例ID | 用例名称 | 前置条件 | 操作 | 预期结果 | 优先级 |
|--------|----------|----------|------|----------|--------|
| TC-VIEW-009 | 日志搜索 | 有历史日志 | 搜索关键词 | 显示匹配行 | P1 |
| TC-VIEW-010 | 日志过滤 | 有历史日志 | 按级别过滤 | 显示匹配日志 | P1 |
| TC-VIEW-011 | 下载日志 | 有历史日志 | 点击下载按钮 | 下载完整日志 | P1 |
| TC-VIEW-012 | 执行图表展示 | 有执行数据 | 查看统计图表 | 图表正确显示 | P1 |

---

**文档版本**: 1.0
**分析日期**: 2026-02-02
**状态**: 待补充设计细节
