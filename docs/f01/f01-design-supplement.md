# F01 功能设计补充规范

**文档版本**: v1.0  
**创建日期**: 2026-02-02  
**基于版本**: F01 v1.4, F01_websocket v1.5

---

## 一、设计遗漏补充

### 1.1 任务类型处理函数规范

根据 f01.md 文档，需要明确每种任务类型的处理函数签名：

```go
// 任务处理函数类型
type TaskHandler func(db *gorm.DB, run *PipelineRun, node *PipelineNode, resolver *VariableResolver) *TaskResult

// TaskResult 定义任务执行结果
type TaskResult struct {
    Success      bool
    Outputs      map[string]interface{}
    ErrorMsg     string
    ExitCode     int
    Duration     int
    ShouldContinue bool // 是否继续执行下游任务
}

// 任务处理函数注册表
var TaskHandlers = map[string]TaskHandler{
    "shell":     ShellTaskHandler,
    "docker":    DockerTaskHandler,
    "git_clone": GitCloneTaskHandler,
    "email":     EmailTaskHandler,
}

// Shell 任务处理器
func ShellTaskHandler(db *gorm.DB, run *PipelineRun, node *PipelineNode, resolver *VariableResolver) *TaskResult {
    config := node.getNodeConfig()
    
    // 解析脚本中的变量
    script := config["script"].(string)
    if resolvedScript, err := resolver.ResolveVariables(script); err == nil {
        script = resolvedScript
    }
    
    // 获取工作目录
    workDir := ""
    if wd, ok := config["working_dir"].(string); ok {
        workDir = wd
    }
    
    // 获取超时时间
    timeout := node.Timeout
    if timeout <= 0 {
        timeout = 3600
    }
    
    // 获取环境变量
    envVars := map[string]string{}
    if env, ok := config["env"].(map[string]interface{}); ok {
        for k, v := range env {
            if strVal, ok := v.(string); ok {
                envVars[k] = strVal
            }
        }
    }
    
    // 选择 Agent
    agentID, err := SelectAgent(db, run.ID)
    if err != nil {
        return &TaskResult{
            Success:  false,
            ErrorMsg: err.Error(),
        }
    }
    
    // 创建任务
    task := &AgentTask{
        AgentID:       agentID,
        PipelineRunID: run.ID,
        NodeID:        node.ID,
        TaskType:      "shell",
        Name:          node.Name,
        Script:        script,
        WorkDir:       workDir,
        EnvVars:       encodeEnvVars(envVars),
        Status:        TaskStatusPending,
        Timeout:       timeout,
    }
    db.Create(task)
    
    // 等待任务完成
    result := WaitForTaskCompletion(db, task, timeout+30)
    
    // 构建输出
    outputs := map[string]interface{}{
        "status":    result.Status,
        "exit_code": result.ExitCode,
        "duration":  result.Duration,
    }
    
    // 从 ResultData 解析额外输出
    if result.ResultData != "" {
        var extraOutputs map[string]interface{}
        if err := json.Unmarshal([]byte(result.ResultData), &extraOutputs); err == nil {
            for k, v := range extraOutputs {
                outputs[k] = v
            }
        }
    }
    
    return &TaskResult{
        Success:        result.Status == TaskStatusSuccess,
        Outputs:        outputs,
        ErrorMsg:       result.ErrorMsg,
        ExitCode:       result.ExitCode,
        Duration:       result.Duration,
        ShouldContinue: result.Status == TaskStatusSuccess,
    }
}

// Docker 任务处理器
func DockerTaskHandler(db *gorm.DB, run *PipelineRun, node *PipelineNode, resolver *VariableResolver) *TaskResult {
    config := node.getNodeConfig()
    
    // 构建 Docker 构建脚本
    var script strings.Builder
    
    imageName := getStringFromMap(config, "image_name")
    imageTag := getStringFromMap(config, "image_tag", "latest")
    dockerfile := getStringFromMap(config, "dockerfile", "./Dockerfile")
    context := getStringFromMap(config, "context", ".")
    
    script.WriteString(fmt.Sprintf("docker build -t %s:%s -f %s %s\n", imageName, imageTag, dockerfile, context))
    
    // 推送配置
    if push, ok := config["push"].(bool); ok && push {
        registry := getStringFromMap(config, "registry")
        if registry != "" {
            script.WriteString(fmt.Sprintf("docker tag %s:%s %s/%s:%s\n", imageName, imageTag, registry, imageName, imageTag))
            script.WriteString(fmt.Sprintf("docker push %s/%s:%s\n", registry, imageName, imageTag))
        }
    }
    
    // 获取仓库信息用于镜像标签
    if repo, ok := config["repository"].(map[string]interface{}); ok {
        if commitID, ok := repo["commit_id"].(string); ok && commitID != "" {
            shortCommitID := commitID[:7]
            script.WriteString(fmt.Sprintf("docker tag %s:%s %s/%s:%s\n", imageName, imageTag, registry, imageName, shortCommitID))
        }
    }
    
    // 代理给 Shell 处理器执行
    nodeCopy := *node
    nodeCopy.Type = "shell"
    nodeCopy.Config["script"] = script.String()
    
    return ShellTaskHandler(db, run, &nodeCopy, resolver)
}

// Git Clone 任务处理器
func GitCloneTaskHandler(db *gorm.DB, run *PipelineRun, node *PipelineNode, resolver *VariableResolver) *TaskResult {
    config := node.getNodeConfig()
    
    // 获取仓库配置
    repoConfig := config["repository"].(map[string]interface{})
    repoURL := getStringFromMap(repoConfig, "url")
    branch := getStringFromMap(repoConfig, "branch", "main")
    targetDir := getStringFromMap(repoConfig, "target_dir")
    depth := 0
    if d, ok := repoConfig["depth"].(float64); ok {
        depth = int(d)
    }
    
    // 构建 Git 脚本
    var script strings.Builder
    script.WriteString("set -e\n")
    
    if targetDir != "" {
        script.WriteString(fmt.Sprintf("mkdir -p %s\n", targetDir))
        script.WriteString(fmt.Sprintf("cd %s\n", targetDir))
    }
    
    if depth > 0 {
        script.WriteString(fmt.Sprintf("git clone --depth %d -b %s %s .\n", depth, branch, repoURL))
    } else {
        script.WriteString(fmt.Sprintf("git clone -b %s %s .\n", branch, repoURL))
    }
    
    // 如果指定了 commit
    if commitID, ok := repoConfig["commit_id"].(string); ok && commitID != "" {
        script.WriteString(fmt.Sprintf("git checkout %s\n", commitID))
    }
    
    // 获取提交信息
    script.WriteString("COMMIT_ID=$(git rev-parse HEAD)\n")
    script.WriteString("SHORT_COMMIT_ID=$(git rev-parse --short HEAD)\n")
    script.WriteString("BRANCH=$(git rev-parse --abbrev-ref HEAD)\n")
    script.WriteString("echo \"GIT_COMMIT_ID=$COMMIT_ID\"\n")
    script.WriteString("echo \"GIT_SHORT_COMMIT_ID=$SHORT_COMMIT_ID\"\n")
    script.WriteString("echo \"GIT_BRANCH=$BRANCH\"\n")
    
    // 代理给 Shell 处理器执行
    nodeCopy := *node
    nodeCopy.Type = "shell"
    nodeCopy.Config["script"] = script.String()
    nodeCopy.Config["output_extraction"] = []OutputExtractionConfig{
        {Field: "commit_id", Regex: `GIT_COMMIT_ID=(\S+)`, Source: "stdout"},
        {Field: "short_commit_id", Regex: `GIT_SHORT_COMMIT_ID=(\S+)`, Source: "stdout"},
        {Field: "branch", Regex: `GIT_BRANCH=(\S+)`, Source: "stdout"},
    }
    
    result := ShellTaskHandler(db, run, &nodeCopy, resolver)
    
    // 添加仓库特定输出
    if result.Success && result.Outputs != nil {
        result.Outputs["url"] = repoURL
        result.Outputs["branch"] = branch
        if checkoutDir, ok := result.Outputs["checkout_dir"].(string); !ok || checkoutDir == "" {
            result.Outputs["checkout_path"] = targetDir
        }
    }
    
    return result
}

// Email 任务处理器
func EmailTaskHandler(db *gorm.DB, run *PipelineRun, node *PipelineNode, resolver *VariableResolver) *TaskResult {
    config := node.getNodeConfig()
    
    // 邮件配置
    to := config["to"].([]interface{})
    cc := config["cc"].([]interface{})
    subject := getStringFromMap(config, "subject", "Pipeline Notification")
    body := getStringFromMap(config, "body", "")
    bodyType := getStringFromMap(config, "body_type", "text")
    
    // 解析模板变量
    resolvedSubject, _ := resolver.ResolveVariables(subject)
    resolvedBody, _ := resolver.ResolveVariables(body)
    
    // 发送邮件 (简化实现)
    email := &Email{
        To:      convertToStringSlice(to),
        CC:      convertToStringSlice(cc),
        Subject: resolvedSubject,
        Body:    resolvedBody,
        BodyType: bodyType,
    }
    
    err := SendEmail(email)
    
    outputs := map[string]interface{}{
        "sent":     err == nil,
        "recipients": len(to),
        "cc_count": len(cc),
    }
    
    if err != nil {
        outputs["error"] = err.Error()
    }
    
    return &TaskResult{
        Success:  err == nil,
        Outputs:  outputs,
        ErrorMsg: errorToString(err),
    }
}
```

### 1.2 辅助函数定义

```go
// getStringFromMap 从 map 获取字符串，带默认值
func getStringFromMap(m map[string]interface{}, key string, defaults ...string) string {
    if v, ok := m[key].(string); ok {
        return v
    }
    if len(defaults) > 0 {
        return defaults[0]
    }
    return ""
}

// convertToStringSlice 转换 interface slice 为 string slice
func convertToStringSlice(input []interface{}) []string {
    result := make([]string, len(input))
    for i, v := range input {
        result[i] = fmt.Sprintf("%v", v)
    }
    return result
}

// encodeEnvVars 编码环境变量为 JSON 字符串
func encodeEnvVars(env map[string]string) string {
    data, _ := json.Marshal(env)
    return string(data)
}

// errorToString 转换错误为字符串
func errorToString(err error) string {
    if err == nil {
        return ""
    }
    return err.Error()
}

// SelectAgent 选择合适的 Agent 执行任务
func SelectAgent(db *gorm.DB, runID uint64) (uint64, error) {
    // 查找在线且已批准状态的 Agent
    var agents []Agent
    db.Where("status IN (?, ?) AND registration_status = ?",
        AgentStatusOnline, AgentStatusBusy,
        AgentRegistrationStatusApproved).
        Find(&agents)
    
    if len(agents) == 0 {
        return 0, fmt.Errorf("no available agents")
    }
    
    // 选择负载最小的 Agent
    var selectedAgent Agent
    minTasks := int64(999999)
    
    for _, agent := range agents {
        var runningTasks int64
        db.Model(&AgentTask{}).
            Where("agent_id = ? AND status = ?", agent.ID, TaskStatusRunning).
            Count(&runningTasks)
        
        if runningTasks < minTasks {
            minTasks = runningTasks
            selectedAgent = agent
        }
    }
    
    return selectedAgent.ID, nil
}

// WaitForTaskCompletion 等待任务完成
func WaitForTaskCompletion(db *gorm.DB, task *AgentTask, timeoutSeconds int) *AgentTask {
    timeout := time.After(time.Duration(timeoutSeconds) * time.Second)
    tick := time.Tick(5 * time.Second)
    
    for {
        select {
        case <-timeout:
            // 超时，更新任务状态
            db.Model(task).Updates(map[string]interface{}{
                "status":    TaskStatusFailed,
                "error_msg": "task execution timeout",
                "end_time":  time.Now().Unix(),
            })
            return task
            
        case <-tick:
            // 检查任务状态
            var currentTask AgentTask
            db.First(&currentTask, task.ID)
            
            if currentTask.Status == TaskStatusSuccess ||
               currentTask.Status == TaskStatusFailed ||
               currentTask.Status == TaskStatusCancelled {
                return &currentTask
            }
        }
    }
}
```

---

## 二、输出提取配置规范

### 2.1 OutputExtractionConfig 定义

```go
// OutputExtractionConfig 定义如何从输出中提取变量
type OutputExtractionConfig struct {
    Field    string `json:"field"`              // 输出字段名
    Regex    string `json:"regex"`              // 正则表达式
    Source   string `json:"source"`             // stdout/stderr
    Required bool   `json:"required,omitempty"` // 是否必需
}

// OutputExtractor 从输出中提取变量
type OutputExtractor struct {
    Extractions []OutputExtractionConfig
}

// ExtractOutputs 从 stdout/stderr 中提取变量
func (e *OutputExtractor) ExtractOutputs(stdout, stderr string) (map[string]interface{}, error) {
    outputs := make(map[string]interface{})
    
    for _, extraction := range e.Extractions {
        var source string
        switch extraction.Source {
        case "stdout":
            source = stdout
        case "stderr":
            source = stderr
        default:
            source = stdout
        }
        
        re := regexp.MustCompile(extraction.Regex)
        matches := re.FindStringSubmatch(source)
        
        if len(matches) > 1 {
            outputs[extraction.Field] = matches[1]
        } else if extraction.Required {
            return nil, fmt.Errorf("required output field '%s' not found", extraction.Field)
        }
    }
    
    return outputs, nil
}
```

### 2.2 常用正则表达式模板

```go
// 常用正则表达式模板
var RegexTemplates = map[string]string{
    // 版本号
    "version": `Version:\s*([0-9]+\.[0-9]+\.[0-9]+)`,
    
    // Git 提交 ID
    "commit_id": `([0-9a-f]{40})`,
    "short_commit_id": `([0-9a-f]{7,40})`,
    
    // 分支名
    "branch": `([a-zA-Z_][a-zA-Z0-9_/-]*)`,
    
    // 测试结果
    "test_passed": `(\d+)\s+passed`,
    "test_failed": `(\d+)\s+failed`,
    "test_skipped": `(\d+)\s+skipped`,
    
    // 构建时间
    "build_time": `Build time:\s*(\d+)`,
    "build_duration": `Duration:\s*(\d+)s`,
    
    // 镜像信息
    "image_id": `([sha256:[a-f0-9]{64}]|latest)`,
    "image_tag": `([a-zA-Z0-9_.-]+)`,
    "image_size": `([0-9.]+\s*[KMGT]?B)`,
    
    // Docker 构建
    "docker_layer_count": `(\d+)\s+steps`,
    
    // 部署信息
    "replica": `replicas\s*=\s*(\d+)`,
    "namespace": `namespace/([a-z0-9-]+)`,
    
    // 错误信息
    "error_message": `Error:\s*(.+)`,
    "error_line": `at\s+([a-zA-Z0-9_./-]+:\d+)`,
}
```

---

## 三、环境变量构建规范

### 3.1 BuildGlobalEnvVars 详细实现

```go
// BuildGlobalEnvVars 构建全局环境变量
func BuildGlobalEnvVars(pipeline *Pipeline, run *PipelineRun) map[string]string {
    now := time.Now()
    
    envVars := map[string]string{
        // CI 环境标识
        "CI":          "true",
        "EASYDO":      "true",
        
        // 流水线信息
        "PIPELINE_ID":          fmt.Sprintf("%d", pipeline.ID),
        "PIPELINE_NAME":        pipeline.Name,
        "PIPELINE_DESCRIPTION": pipeline.Description,
        
        // 运行信息
        "RUN_ID":       fmt.Sprintf("%d", run.ID),
        "BUILD_NUMBER": fmt.Sprintf("%d", run.BuildNumber),
        "BUILD_TAG":    fmt.Sprintf("build-%d", run.ID),
        "BUILD_URL":    fmt.Sprintf("http://localhost:8080/pipelines/%d/runs/%d", pipeline.ID, run.ID),
        
        // 时间信息
        "BUILD_DATE":      now.Format("2006-01-02"),
        "BUILD_TIME":      now.Format("15:04:05"),
        "BUILD_TIMESTAMP": fmt.Sprintf("%d", now.Unix()),
    }
    
    // 如果有 Git 信息（从最近的 git_clone 任务获取）
    // 这些值会在任务执行时动态更新
    if run.StartTime > 0 {
        envVars["BUILD_DURATION"] = fmt.Sprintf("%d", now.Unix()-run.StartTime)
    }
    
    return envVars
}
```

### 3.2 任务级环境变量

```go
// BuildTaskEnvVars 构建任务级环境变量
func BuildTaskEnvVars(task *AgentTask, globalVars map[string]string) map[string]string {
    envVars := make(map[string]string)
    
    // 复制全局变量
    for k, v := range globalVars {
        envVars[k] = v
    }
    
    // 任务特定变量
    envVars["TASK_ID"] = fmt.Sprintf("%d", task.ID)
    envVars["NODE_ID"] = task.NodeID
    envVars["TASK_TYPE"] = task.TaskType
    envVars["TASK_NAME"] = task.Name
    
    // 仓库信息
    if task.RepoURL != "" {
        envVars["GIT_URL"] = task.RepoURL
    }
    if task.RepoBranch != "" {
        envVars["GIT_BRANCH"] = task.RepoBranch
    }
    if task.RepoCommit != "" {
        envVars["GIT_COMMIT"] = task.RepoCommit
    }
    if task.RepoPath != "" {
        envVars["GIT_CHECKOUT_PATH"] = task.RepoPath
    }
    
    // 工作目录
    workspace := fmt.Sprintf("/workspace/run-%d", task.PipelineRunID)
    envVars["WORKSPACE"] = workspace
    envVars["TASK_WORK_DIR"] = workspace
    
    // 解析任务的环境变量配置
    if task.EnvVars != "" {
        var taskEnvVars map[string]string
        if err := json.Unmarshal([]byte(task.EnvVars), &taskEnvVars); err == nil {
            for k, v := range taskEnvVars {
                envVars[k] = v
            }
        }
    }
    
    return envVars
}
```

---

## 四、WebSocket 消息格式规范

### 4.1 Server → Agent 消息

```json
{
  "type": "pipeline_assign",
  "data": {
    "run_id": 456,
    "config": {
      "version": "2.0",
      "nodes": [...],
      "edges": [...]
    },
    "agent_config": {
      "workspace": "/workspace/run-456",
      "timeout": 7200,
      "env_vars": {
        "CI": "true",
        "BUILD_NUMBER": "456"
      }
    }
  },
  "timestamp": 1706784000000
}
```

### 4.2 Agent → Server 消息

#### 任务状态消息

```json
{
  "type": "task_status",
  "data": {
    "run_id": 456,
    "node_id": "1",
    "status": "running",
    "start_time": 1706784000000
  },
  "timestamp": 1706784000000
}
```

```json
{
  "type": "task_status",
  "data": {
    "run_id": 456,
    "node_id": "1",
    "status": "success",
    "exit_code": 0,
    "end_time": 1706784600000,
    "duration": 600,
    "result": {
      "commit_id": "abc123def",
      "checkout_dir": "./frontend"
    }
  },
  "timestamp": 1706784600000
}
```

#### 任务日志流消息

```json
{
  "type": "task_log_stream",
  "data": {
    "run_id": 456,
    "node_id": "1",
    "line_number": 1,
    "level": "info",
    "source": "stdout",
    "message": "[INFO] Starting build process...",
    "timestamp": 1706784001000
  },
  "timestamp": 1706784001000
}
```

#### 心跳消息

```json
{
  "type": "heartbeat",
  "data": {
    "agent_id": 10,
    "status": "idle",
    "running_tasks": 2,
    "load": 0.75
  },
  "timestamp": 1706784000000
}
```

### 4.3 Server → Frontend 消息

#### 任务状态广播

```json
{
  "type": "task_status",
  "channel": "run:456",
  "data": {
    "task_id": 123,
    "node_id": "node-1",
    "node_name": "Build Frontend",
    "status": "running",
    "start_time": 1706784000000
  },
  "timestamp": 1706784000000
}
```

#### 任务日志广播

```json
{
  "type": "task_log",
  "channel": "run:456",
  "data": {
    "task_id": 123,
    "node_id": "node-1",
    "level": "info",
    "message": "[INFO] npm install completed successfully",
    "timestamp": 1706784100000,
    "line_number": 156
  },
  "timestamp": 1706784100000
}
```

#### 执行进度广播

```json
{
  "type": "run_progress",
  "channel": "run:456",
  "data": {
    "run_id": 456,
    "status": "running",
    "total_nodes": 10,
    "completed_nodes": 4,
    "running_nodes": 2,
    "failed_nodes": 0,
    "pending_nodes": 4,
    "progress": 40,
    "duration": 120
  },
  "timestamp": 1706784120000
}
```

---

## 五、错误码规范

### 5.1 任务错误码

| 错误码 | 含义 | 处理建议 |
|--------|------|----------|
| 0 | 成功 | 无 |
| 1 | 一般错误 | 检查错误信息 |
| 2 | 语法错误 | 检查脚本语法 |
| 127 | 命令未找到 | 检查命令是否安装 |
| 128 | 无效退出参数 | 检查退出逻辑 |
| 130 | Ctrl+C 中断 | 任务被取消 |
| 137 | 被 SIGKILL 杀死 | 任务被强制终止(可能超时) |
| 139 | 段错误 | 脚本内部错误 |
| 143 | 被 SIGTERM 终止 | 任务被正常终止 |
| 255 | 退出码超范围 | 检查脚本逻辑 |

### 5.2 系统错误码

| 错误码 | 含义 | 处理建议 |
|--------|------|----------|
| 1001 | 无可用 Agent | 检查 Agent 状态 |
| 1002 | Agent 连接失败 | 检查网络连接 |
| 1003 | 任务超时 |  увеличить超时时间 |
| 1004 | 任务被取消 | 检查取消原因 |
| 1005 | 任务不存在 | 检查任务 ID |
| 1006 | 权限不足 | 检查权限配置 |
| 1007 | 仓库访问失败 | 检查凭据 |
| 1008 | Docker 构建失败 | 检查 Dockerfile |
| 1009 | 镜像推送失败 | 检查 registry 权限 |
| 1010 | 邮件发送失败 | 检查邮件配置 |

---

## 六、日志级别规范

### 6.1 日志级别定义

| 级别 | 用途 | 存储策略 |
|------|------|----------|
| debug | 调试信息 | 默认不存储 |
| info | 一般信息 | 完整存储 |
| warn | 警告信息 | 完整存储 |
| error | 错误信息 | 完整存储，关联 ErrorMsg |

### 6.2 日志格式规范

```
[TIMESTAMP] [LEVEL] [SOURCE] [TASK_ID:NODE_ID] Message

示例：
[2026-02-01 16:27:50] [INFO] [stdout] [123:node-1] npm install completed successfully
[2026-02-01 16:27:55] [ERROR] [stderr] [123:node-1] npm ERR! 404 Not Found: package@1.0.0
```

---

**文档版本**: 1.0  
**最后更新**: 2026-02-02  
**状态**: 待实施
