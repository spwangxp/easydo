# F01 WebSocket 通信架构补充

**功能版本**: v1.5  
**创建日期**: 2026-02-01  
**基于版本**: F01 v1.4

---

## 七、WebSocket 通信架构

### 7.1 架构概述

WebSocket 通信用于实现以下场景的实时交互：

| 通信方向 | 用途 | 消息类型 |
|----------|------|----------|
| Server → Agent | 任务下发 | task_assign, task_cancel, agent_config |
| Agent → Server | 日志上报 | task_log, task_status, heartbeat |
| Server → Frontend | 实时状态 | task_status, task_log, run_progress, agent_status |
| Frontend → Server | 订阅管理 | subscribe, unsubscribe |

```
┌─────────────────────────────────────────────────────────────────────────┐
│                      WebSocket 通信架构                                   │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  ┌──────────────┐     ┌──────────────┐     ┌──────────────┐           │
│  │   Frontend   │     │    Server    │     │    Agent     │           │
│  └──────┬───────┘     └──────┬───────┘     └──────┬───────┘           │
│         │                    │                     │                    │
│         │   WS: Subscribe    │                     │                    │
│         │───────────────────>│                     │                    │
│         │                    │   WS: Task Push     │                    │
│         │                    │────────────────────>│                    │
│         │   WS: Broadcast    │                     │                    │
│         │<───────────────────│                     │                    │
│         │                    │                     │   WS: Log Report   │
│         │                    │<────────────────────│                    │
│         │   WS: Broadcast    │                     │                    │
│         │<───────────────────│                     │                    │
│         │                    │                     │ WS: Status Update  │
│         │                    │<────────────────────│                    │
│         │   WS: Broadcast    │                     │                    │
│         │<───────────────────│                     │                    │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

### 7.2 Server ↔ Agent WebSocket 协议

#### 7.2.1 流水线级别下发消息 (Server → Agent)

**设计原则**：每次 PipelineRun 执行时，将整个 PipelineConfig 发送给 Agent，由 Agent 自主调度 DAG 执行。

```json
{
  "type": "pipeline_assign",
  "data": {
    "run_id": 456,
    "config": {
      "version": "2.0",
      "nodes": [
        {
          "id": "1",
          "type": "git_clone",
          "name": "检出代码",
          "config": {
            "repository": {
              "url": "git@github.com:company/frontend.git",
              "branch": "main",
              "target_dir": "./frontend"
            }
          },
          "timeout": 300
        },
        {
          "id": "2",
          "type": "shell",
          "name": "前端构建",
          "config": {
            "script": "cd ./frontend && npm install && npm run build"
          },
          "timeout": 600
        }
      ],
      "edges": [
        {"from": "1", "to": "2"}
      ]
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

**消息说明**：
| 字段 | 类型 | 说明 |
|------|------|------|
| run_id | uint64 | PipelineRun ID |
| config | object | 完整的 PipelineConfig |
| config.nodes | array | 所有节点配置 |
| config.edges | array | 依赖关系 |
| agent_config | object | Agent 执行配置 |
| agent_config.workspace | string | 工作目录 |
| agent_config.timeout | int | 整体超时(秒) |
| agent_config.env_vars | object | 环境变量 |

**Agent 端处理流程**：
1. 接收 PipelineConfig
2. 构建本地 DAG
3. 按依赖顺序执行任务（同一层级并行）
4. 上报任务状态和日志

#### 7.2.2 任务下发消息 (Server → Agent) - [已废弃]

**注意**：任务级下发消息已废弃，由 `pipeline_assign` 替代。

原有的 `task_assign` 消息不再使用，Agent 自主根据 DAG 调度任务执行。

#### 7.2.3 任务状态上报消息 (Agent → Server)

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
    },
    "logs": [
      {
        "level": "info",
        "source": "stdout",
        "message": "[INFO] Build started",
        "timestamp": 1706784000000
      }
    ]
  },
  "timestamp": 1706784600000
}
```

**注意**：task_status 只负责状态上报，日志通过 task_log_stream 实时传输。

```json
{
  "type": "task_status",
  "data": {
    "run_id": 456,
    "node_id": "1",
    "status": "failed",
    "exit_code": 1,
    "end_time": 1706784600000,
    "duration": 600,
    "error_msg": "npm install failed with exit code 1"
  },
  "timestamp": 1706784600000
}
```

**消息说明**：
| 字段 | 类型 | 说明 |
|------|------|------|
| run_id | uint64 | PipelineRun ID |
| node_id | string | 节点 ID |
| status | string | 状态 (pending/running/success/failed) |
| start_time | int64 | 开始时间 |
| end_time | int64 | 结束时间 |
| duration | int | 执行时长(秒) |
| exit_code | int | 退出码 |
| result | object | 执行结果（包含 outputs 字段） |
| error_msg | string | 错误信息 |

#### 7.2.4 日志上报消息 (Agent → Server) - 实时流模式

**注意**：使用 `task_log_stream` 实时流式传输，日志产生后立即发送。

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

**消息说明**：

| 字段 | 类型 | 说明 |
|------|------|------|
| run_id | uint64 | PipelineRun ID |
| node_id | string | 节点 ID |
| line_number | int | 日志行号（用于排序） |
| level | string | 日志级别 (debug/info/warn/error) |
| source | string | 输出源 (stdout/stderr/system) |
| message | string | 日志内容 |
| timestamp | int64 | Unix 时间戳(毫秒) |

**设计说明**：
- Agent 产生日志后立即发送，无需本地缓冲
- Server 接收后写入 Redis 缓冲队列 (key: `logs:{run_id}:{task_id}`)
- Server 定时刷新到 MySQL（默认 5 秒，可配置）
- 子任务完成时立即刷新剩余日志
- Frontend 订阅后实时接收日志更新

**传输示例**（持续产生的日志）：

```
T1: Agent 产生日志行 #1 → 立即发送 task_log_stream
T2: Server 接收 → 写入 Redis → 广播给 Frontend
T3: Agent 产生日志行 #2 → 立即发送 task_log_stream
T4: Server 接收 → 写入 Redis → 广播给 Frontend
... (持续实时传输)
```

#### 7.2.5 心跳消息 (双向)

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

### 7.3 Server ↔ Frontend WebSocket 协议

#### 7.3.1 订阅消息 (Frontend → Server)

```json
{
  "type": "subscribe",
  "data": {
    "channels": ["run:456", "agent:10"]
  }
}
```

#### 7.3.2 取消订阅消息 (Frontend → Server)

```json
{
  "type": "unsubscribe",
  "data": {
    "channels": ["run:456"]
  }
}
```

#### 7.3.3 任务状态更新广播 (Server → Frontend)

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

#### 7.3.4 任务日志广播 (Server → Frontend)

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

#### 7.3.5 流水线执行进度广播 (Server → Frontend)

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

#### 7.3.6 Agent 状态更新广播 (Server → Frontend)

```json
{
  "type": "agent_status",
  "channel": "agent:10",
  "data": {
    "agent_id": 10,
    "name": "agent-worker-1",
    "status": "online",
    "running_tasks": 2,
    "load": 0.65
  },
  "timestamp": 1706784120000
}
```

### 7.3.7 Server 端日志缓冲与刷新机制

**架构说明**：

```
┌─────────────────────────────────────────────────────────────────────────┐
│                    Server 端日志处理流程                                  │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  ┌──────────┐    ┌──────────────┐    ┌──────────┐    ┌──────────┐      │
│  │  Agent   │───▶│  WebSocket   │───▶│  Redis   │───▶│  MySQL   │      │
│  │ (日志流) │    │  (接收)      │    │  (缓冲)  │    │ (持久化) │      │
│  └──────────┘    └──────────────┘    └──────────┘    └──────────┘      │
│       │                  │                  │               │           │
│       │  task_log_stream │                  │               │           │
│       │─────────────────▶│                  │               │           │
│       │                  │ LPUSH logs:456:123│               │           │
│       │                  │──────────────────▶│               │           │
│       │                  │                   │  定时刷新      │           │
│       │                  │                   │  (默认5秒)     │           │
│       │                  │                   │               │           │
│       │                  │                   ├─ LRANGE + DEL │           │
│       │                  │                   │               │           │
│       │                  │                   ├─ MySQL INSERT │           │
│       │                  │                   │──────────────▶│           │
│       │                  │                   │               │           │
│       │                  │ Frontend 广播      │               │           │
│       │                  │◀──────────────────│               │           │
│       │                  │                   │               │           │
│       ▼                  ▼                   ▼               ▼           │
│                                                                          │
│  刷新触发条件：                                                          │
│  1. 定时器触发（默认 5 秒，可配置）                                       │
│  2. 子任务完成时立即刷新                                                  │
│  3. 异常恢复时补偿刷新                                                   │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

**Redis 键设计**：

| 键名 | 类型 | 说明 |
|------|------|------|
| `logs:{run_id}:{task_id}` | List | 日志缓冲队列，左入右出 |

**刷新配置**（在 config.yaml 中）：

```yaml
# 日志缓冲配置
logging:
  buffer:
    enabled: true           # 启用日志缓冲
    flush_interval: 5s      # 刷新间隔（默认 5 秒，可配置）
    max_batch_size: 1000    # 单次最大刷新条数
    redis_key_prefix: "logs"  # Redis 键前缀
```

**刷新逻辑（伪代码）**：

```
FUNCTION flushLogsToMySQL(runID, taskID):
  redisKey ← "logs:" + runID + ":" + taskID
  
  // 批量读取日志
  logs ← REDIS.LRANGE(redisKey, 0, -1)
  IF logs IS_EMPTY:
    REDIS.DEL(redisKey)
    RETURN
  
  // 转换格式并批量写入
  agentLogs ← []
  FOR log IN logs:
    agentLog ← PARSE(log)
    agentLogs.APPEND(agentLog)
  
  // 事务写入 MySQL
  DB.Transaction:
    FOR agentLog IN agentLogs:
      DB.CREATE(agentLog)
  
  // 清除已刷新的日志
  REDIS.DEL(redisKey)

FUNCTION periodicFlush():
  WHILE server IS_RUNNING:
    SLEEP(config.flush_interval)
    
    FOR each active task:
      flushLogsToMySQL(task.run_id, task.id)

FUNCTION onTaskComplete(runID, taskID):
  // 任务完成时立即刷新剩余日志
  flushLogsToMySQL(runID, taskID)
```

### 7.4 后端 WebSocket 实现

#### 7.4.1 WebSocket Hub (easydo-server/internal/websocket/hub.go)

```go
package websocket

import (
    "sync"
    "time"

    "github.com/gorilla/websocket"
)

// Hub WebSocket 连接管理中心
type Hub struct {
    // 已注册的连接
    clients map[string]*Client

    // 频道订阅者
    subscribers map[string]map[*Client]bool

    // 广播通道
    broadcast chan *Message

    // 注册通道
    register chan *Client

    // 注销通道
    unregister chan *Client

    // 锁
    mu sync.RWMutex

    // 配置
    heartbeatInterval time.Duration
    writeTimeout      time.Duration
    readTimeout       time.Duration
}

// NewHub 创建 Hub 实例
func NewHub() *Hub {
    return &Hub{
        clients:           make(map[string]*Client),
        subscribers:       make(map[string]map[*Client]bool),
        broadcast:         make(chan *Message, 256),
        register:          make(chan *Client),
        unregister:        make(chan *Client),
        heartbeatInterval: 30 * time.Second,
        writeTimeout:      10 * time.Second,
        readTimeout:       60 * time.Second,
    }
}

// Run 启动 Hub
func (h *Hub) Run() {
    for {
        select {
        case client := <-h.register:
            h.mu.Lock()
            h.clients[client.ID] = client
            h.mu.Unlock()

        case client := <-h.unregister:
            h.mu.Lock()
            if _, ok := h.clients[client.ID]; ok {
                delete(h.clients, client.ID)
                // 清理频道订阅
                for channel := range client.subscriptions {
                    if subscribers, ok := h.subscribers[channel]; ok {
                        delete(subscribers, client)
                        if len(subscribers) == 0 {
                            delete(h.subscribers, channel)
                        }
                    }
                }
            }
            h.mu.Unlock()

        case message := <-h.broadcast:
            h.broadcastMessage(message)
        }
    }
}

// broadcastMessage 广播消息
func (h *Hub) broadcastMessage(msg *Message) {
    h.mu.RLock()
    defer h.mu.RUnlock()

    if subscribers, ok := h.subscribers[msg.Channel]; ok {
        for client := range subscribers {
            select {
            case client.send <- msg:
            default:
                // 发送队列满，移除该客户端
                close(client.send)
                delete(h.subscribers[msg.Channel], client)
            }
        }
    }
}

// Subscribe 订阅频道
func (h *Hub) Subscribe(client *Client, channels []string) {
    h.mu.Lock()
    defer h.mu.Unlock()

    for _, channel := range channels {
        if h.subscribers[channel] == nil {
            h.subscribers[channel] = make(map[*Client]bool)
        }
        h.subscribers[channel][client] = true
        client.subscriptions[channel] = true
    }
}

// Unsubscribe 取消订阅
func (h *Hub) Unsubscribe(client *Client, channels []string) {
    h.mu.Lock()
    defer h.mu.Unlock()

    for _, channel := range channels {
        if subscribers, ok := h.subscribers[channel]; ok {
            delete(subscribers, client)
            delete(client.subscriptions, channel)
            if len(subscribers) == 0 {
                delete(h.subscribers, channel)
            }
        }
    }
}

// Publish 发布消息到频道
func (h *Hub) Publish(channel string, msgType string, data interface{}) {
    message := &Message{
        Type:      msgType,
        Channel:   channel,
        Data:      data,
        Timestamp: time.Now().UnixMilli(),
    }

    select {
    case h.broadcast <- message:
    default:
        // 队列满，异步处理
        go func() {
            h.broadcast <- message
        }()
    }
}
```

#### 7.4.2 WebSocket Client (easydo-server/internal/websocket/client.go)

```go
package websocket

import (
    "encoding/json"
    "sync"
    "time"

    "github.com/gorilla/websocket"
)

// Client WebSocket 客户端
type Client struct {
    ID            string
    hub           *Hub
    conn          *websocket.Conn
    send          chan *Message
    subscriptions map[string]bool
    mu            sync.Mutex
    userID        uint64
    agentID       uint64
}

// Message WebSocket 消息
type Message struct {
    Type      string      `json:"type"`
    Channel   string      `json:"channel,omitempty"`
    Data      interface{} `json:"data"`
    Timestamp int64       `json:"timestamp"`
}

// NewClient 创建客户端
func NewClient(id string, hub *Hub, conn *websocket.Conn) *Client {
    return &Client{
        ID:            id,
        hub:           hub,
        conn:          conn,
        send:          make(chan *Message, 256),
        subscriptions: make(map[string]bool),
    }
}

// ReadLoop 读取消息循环
func (c *Client) ReadLoop() {
    defer func() {
        c.hub.unregister <- c
        c.conn.Close()
    }()

    c.conn.SetReadDeadline(time.Now().Add(c.hub.readTimeout))
    c.conn.SetPongHandler(func(string) error {
        c.conn.SetReadDeadline(time.Now().Add(c.hub.readTimeout))
        return nil
    })

    for {
        _, message, err := c.conn.ReadMessage()
        if err != nil {
            if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
                // 记录错误日志
            }
            break
        }

        var msg Message
        if err := json.Unmarshal(message, &msg); err != nil {
            continue
        }

        c.handleMessage(&msg)
    }
}

// WriteLoop 写入消息循环
func (c *Client) WriteLoop() {
    ticker := time.NewTicker(c.hub.heartbeatInterval)
    defer func() {
        ticker.Stop()
        c.conn.Close()
    }()

    for {
        select {
        case message, ok := <-c.send:
            c.conn.SetWriteDeadline(time.Now().Add(c.hub.writeTimeout))
            if !ok {
                c.conn.WriteMessage(websocket.CloseMessage, []byte{})
                return
            }

            data, _ := json.Marshal(message)
            if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
                return
            }

        case <-ticker.C:
            c.conn.SetWriteDeadline(time.Now().Add(c.hub.writeTimeout))
            if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
                return
            }
        }
    }
}

// handleMessage 处理消息
func (c *Client) handleMessage(msg *Message) {
    switch msg.Type {
    case "subscribe":
        var data struct {
            Channels []string `json:"channels"`
        }
        if err := json.Unmarshal([]byte(msg.Data.(string)), &data); err == nil {
            c.hub.Subscribe(c, data.Channels)
        }

    case "unsubscribe":
        var data struct {
            Channels []string `json:"channels"`
        }
        if err := json.Unmarshal([]byte(msg.Data.(string)), &data); err == nil {
            c.hub.Unsubscribe(c, data.Channels)
        }

    case "heartbeat":
        // 更新客户端活跃时间
    }
}
```

### 7.5 Agent WebSocket Client 实现

**文件**: `easydo-agent/internal/client/websocket.go`

```go
package client

import (
    "encoding/json"
    "log"
    "sync"
    "time"

    "easydo-agent/internal/config"
    "easydo-agent/internal/executor"

    "github.com/gorilla/websocket"
)

// WSClient WebSocket 客户端
type WSClient struct {
    conn       *websocket.Conn
    config     *config.Config
    executor   *executor.Executor
    sendChan   chan []byte
    recvChan   chan *WSMessage
    mu         sync.Mutex
    connected  bool
    agentID    uint64
    runID      uint64       // PipelineRun ID
    nodeID     string       // 节点 ID
    taskID     uint64
    lineNumber int          // 日志行号计数器
    heartbeatTicker *time.Ticker
}

// WSMessage WebSocket 消息
type WSMessage struct {
    Type      string          `json:"type"`
    Data      json.RawMessage `json:"data"`
    Timestamp int64           `json:"timestamp"`
}

// NewWSClient 创建客户端
func NewWSClient(cfg *config.Config, exec *executor.Executor) *WSClient {
    return &WSClient{
        config:   cfg,
        executor: exec,
        sendChan: make(chan []byte, 256),
        recvChan: make(chan *WSMessage, 128),
    }
}

// Connect 连接 Server
func (c *WSClient) Connect() error {
    url := c.config.ServerURL + "/ws/agent"
    conn, _, err := websocket.DefaultDialer.Dial(url, nil)
    if err != nil {
        return err
    }

    c.conn = conn
    c.connected = true

    // 启动读写循环
    go c.writeLoop()
    go c.readLoop()
    go c.heartbeatLoop()

    // 注册 Agent
    c.register()

    return nil
}

// register 注册 Agent
func (c *WSClient) register() {
    msg := WSMessage{
        Type: "agent_register",
        Data: json.RawMessage(`{
            "name": "` + c.config.Name + `",
            "labels": ["linux", "docker"],
            "capacity": 4
        }`),
        Timestamp: time.Now().UnixMilli(),
    }
    c.send(msg)
}

// heartbeatLoop 心跳循环
func (c *WSClient) heartbeatLoop() {
    c.heartbeatTicker = time.NewTicker(30 * time.Second)
    for range c.heartbeatTicker.C {
        if c.connected {
            c.sendHeartbeat()
        }
    }
}

// sendHeartbeat 发送心跳
func (c *WSClient) sendHeartbeat() {
    msg := WSMessage{
        Type: "heartbeat",
        Data: json.RawMessage(`{
            "status": "idle",
            "running_tasks": 0,
            "load": 0.0
        }`),
        Timestamp: time.Now().UnixMilli(),
    }
    c.send(msg)
}

// writeLoop 写入循环
func (c *WSClient) writeLoop() {
    for data := range c.sendChan {
        c.mu.Lock()
        if c.connected {
            if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
                c.connected = false
            }
        }
        c.mu.Unlock()
    }
}

// readLoop 读取循环
func (c *WSClient) readLoop() {
    for {
        _, message, err := c.conn.ReadMessage()
        if err != nil {
            c.connected = false
            c.reconnect()
            return
        }

        var msg WSMessage
        if err := json.Unmarshal(message, &msg); err != nil {
            continue
        }

        c.handleMessage(&msg)
    }
}

// handleMessage 处理消息
func (c *WSClient) handleMessage(msg *WSMessage) {
    switch msg.Type {
    case "task_assign":
        c.handleTaskAssign(msg)

    case "task_cancel":
        c.handleTaskCancel(msg)

    case "agent_config":
        c.handleAgentConfig(msg)
    }
}

// handleTaskAssign 处理任务分配
func (c *WSClient) handleTaskAssign(msg *WSMessage) {
    var data struct {
        TaskID   uint64 `json:"task_id"`
        RunID    uint64 `json:"run_id"`
        NodeID   string `json:"node_id"`
        Script   string `json:"script"`
        WorkDir  string `json:"work_dir"`
        Timeout  int    `json:"timeout"`
    }

    if err := json.Unmarshal(msg.Data, &data); err != nil {
        return
    }

    c.taskID = data.TaskID
    c.runID = data.RunID
    c.nodeID = data.NodeID
    c.lineNumber = 0  // 重置行号计数器

    // 创建任务执行器
    task := &executor.Task{
        ID:      data.TaskID,
        RunID:   data.RunID,
        NodeID:  data.NodeID,
        Script:  data.Script,
        WorkDir: data.WorkDir,
        Timeout: time.Duration(data.Timeout) * time.Second,
        Logger:  c,
    }

    // 异步执行
    go func() {
        // 报告状态：运行中
        c.reportStatus(data.TaskID, data.RunID, data.NodeID, "running", nil)

        // 执行任务
        result, err := c.executor.Execute(task)

        // 报告结果
        if err != nil {
            c.reportStatus(data.TaskID, data.RunID, data.NodeID, "failed", map[string]interface{}{
                "error_msg": err.Error(),
            })
        } else {
            c.reportStatus(data.TaskID, data.RunID, data.NodeID, "success", map[string]interface{}{
                "exit_code": result.ExitCode,
                "result":    result.Output,
            })
        }
    }()
}

// handleTaskCancel 处理任务取消
func (c *WSClient) handleTaskCancel(msg *WSMessage) {
    var data struct {
        TaskID uint64 `json:"task_id"`
    }
    json.Unmarshal(msg.Data, &data)

    c.executor.Cancel(data.TaskID)
}

// reportStatus 报告任务状态
func (c *WSClient) reportStatus(taskID, runID uint64, nodeID, status string, extra map[string]interface{}) {
    statusMsg := map[string]interface{}{
        "task_id": taskID,
        "run_id":  runID,
        "node_id": nodeID,
        "status":  status,
    }

    if extra != nil {
        for k, v := range extra {
            statusMsg[k] = v
        }
    }

    data, _ := json.Marshal(statusMsg)

    c.sendChan <- []byte(`{
        "type": "task_status",
        "data": ` + string(data) + `,
        "timestamp": ` + string(json.RawMessage(time.Now().Format("20060102150405")))) + `
    }`)
}

// Log 实现 Logger 接口（实时传输模式）
func (c *WSClient) Log(level, source, message string, lineNumber int) {
    // 增加行号
    c.lineNumber++

    logMsg := map[string]interface{}{
        "run_id":      c.runID,
        "node_id":     c.nodeID,
        "line_number": c.lineNumber,
        "level":       level,
        "source":      source,
        "message":     message,
        "timestamp":   time.Now().UnixMilli(),
    }

    data, _ := json.Marshal(logMsg)

    // 实时发送，无需缓冲
    select {
    case c.sendChan <- []byte(`{
        "type": "task_log_stream",
        "data": ` + string(data) + `,
        "timestamp": ` + string(json.RawMessage(time.Now().Format("20060102150405")))) + `
    }`):
    default:
        // 队列满时丢弃（避免阻塞任务执行）
        // 生产环境应使用有界队列 + 背压机制
    }
}

// send 发送消息
func (c *WSClient) send(msg WSMessage) {
    data, _ := json.Marshal(msg)
    select {
    case c.sendChan <- data:
    default:
        // 队列满
    }
}

// reconnect 重连
func (c *WSClient) reconnect() {
    for i := 0; i < 10; i++ {
        time.Sleep(time.Duration(i+1) * time.Second)
        if err := c.Connect(); err == nil {
            return
        }
    }
}
```

### 7.6 前端 WebSocket 实现

**文件**: `easydo-frontend/src/utils/websocket.js`

```javascript
import { ref, reactive } from 'vue'
import { ElMessage } from 'element-plus'

class WSClient {
    constructor() {
        this.ws = null
        this.reconnectAttempts = 0
        this.maxReconnectAttempts = 5
        this.reconnectDelay = 3000
        this.heartbeatInterval = 30000
        this.heartbeatTimer = null
        this.messageHandlers = new Map()
        this.isConnected = ref(false)
        this.subscribedChannels = reactive(new Set())
    }

    connect(url) {
        return new Promise((resolve, reject) => {
            try {
                this.ws = new WebSocket(url)

                this.ws.onopen = () => {
                    console.log('[WS] Connected')
                    this.isConnected.value = true
                    this.reconnectAttempts = 0
                    this.startHeartbeat()
                    resolve()
                }

                this.ws.onclose = () => {
                    console.log('[WS] Disconnected')
                    this.isConnected.value = false
                    this.stopHeartbeat()
                    this.reconnect()
                }

                this.ws.onerror = (error) => {
                    console.error('[WS] Error:', error)
                    reject(error)
                }

                this.ws.onmessage = (event) => {
                    this.handleMessage(event.data)
                }
            } catch (error) {
                reject(error)
            }
        })
    }

    handleMessage(data) {
        try {
            const message = JSON.parse(data)
            const handler = this.messageHandlers.get(message.type)
            if (handler) {
                handler(message)
            }
        } catch (error) {
            console.error('[WS] Failed to parse message:', error)
        }
    }

    on(type, handler) {
        this.messageHandlers.set(type, handler)
    }

    off(type) {
        this.messageHandlers.delete(type)
    }

    subscribe(channels) {
        if (!this.isConnected.value) return

        const msg = {
            type: 'subscribe',
            data: { channels }
        }
        this.ws.send(JSON.stringify(msg))

        channels.forEach(ch => this.subscribedChannels.add(ch))
    }

    unsubscribe(channels) {
        if (!this.isConnected.value) return

        const msg = {
            type: 'unsubscribe',
            data: { channels }
        }
        this.ws.send(JSON.stringify(msg))

        channels.forEach(ch => this.subscribedChannels.delete(ch))
    }

    startHeartbeat() {
        this.heartbeatTimer = setInterval(() => {
            if (this.isConnected.value) {
                this.ws.send(JSON.stringify({ type: 'heartbeat' }))
            }
        }, this.heartbeatInterval)
    }

    stopHeartbeat() {
        if (this.heartbeatTimer) {
            clearInterval(this.heartbeatTimer)
            this.heartbeatTimer = null
        }
    }

    reconnect() {
        if (this.reconnectAttempts >= this.maxReconnectAttempts) {
            ElMessage.error('WebSocket 连接失败，请刷新页面重试')
            return
        }

        this.reconnectAttempts++
        const delay = this.reconnectDelay * this.reconnectAttempts

        console.log(`[WS] Reconnecting in ${delay}ms (attempt ${this.reconnectAttempts})`)

        setTimeout(() => {
            this.connect(this.ws.url).then(() => {
                // 重连成功后重新订阅
                if (this.subscribedChannels.size > 0) {
                    this.subscribe(Array.from(this.subscribedChannels))
                }
            }).catch(() => {})
        }, delay)
    }

    disconnect() {
        this.stopHeartbeat()
        if (this.ws) {
            this.ws.close()
            this.ws = null
        }
        this.isConnected.value = false
    }
}

export const wsClient = new WSClient()
```

**前端使用示例**:

```javascript
import { wsClient } from '@/utils/websocket'

// 在 composable 中使用
export function usePipelineWS(runId) {
    const logs = ref([])
    const status = ref('pending')
    const progress = ref(0)

    const connect = async () => {
        await wsClient.connect(`ws://${window.location.host}/ws`)

        // 订阅流水线运行频道
        wsClient.subscribe([`run:${runId}`])

        // 监听任务状态更新
        wsClient.on('task_status', (msg) => {
            if (msg.data.node_id) {
                status.value = msg.data.status
            }
        })

        // 监听任务日志
        wsClient.on('task_log', (msg) => {
            logs.value.push({
                timestamp: msg.data.timestamp,
                level: msg.data.level,
                message: msg.data.message
            })
        })

        // 监听执行进度
        wsClient.on('run_progress', (msg) => {
            progress.value = msg.data.progress
        })
    }

    const disconnect = () => {
        wsClient.unsubscribe([`run:${runId}`])
        wsClient.off('task_status')
        wsClient.off('task_log')
        wsClient.off('run_progress')
    }

    return {
        logs,
        status,
        progress,
        connect,
        disconnect
    }
}
```

### 7.7 重连策略

| 场景 | 重试策略 |
|------|----------|
| 网络断开 | 指数退避：3s, 6s, 12s, 24s, 48s |
| 连接超时 | 最大重试次数：5 次 |
| 心跳超时 | 60s 无响应视为断开 |
| 重连成功 | 自动恢复订阅的频道 |

---

*文档版本: v1.5*  
*基于版本: F01 v1.4*  
*最后更新: 2026-02-01*
