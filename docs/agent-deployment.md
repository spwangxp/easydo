# Agent部署说明

本文档详细说明如何部署和配置EasyDo执行器(Agent)。

## 目录

- [快速部署](#快速部署)
- [Makefile命令](#makefile命令)
- [配置文件](#配置文件)
- [手动部署](#手动部署)
- [故障排查](#故障排查)

---

## 快速部署

### 1. 编译Agent

```bash
# Docker编译Agent（推荐）
make build-agent

# 或本地编译
cd easydo-agent
go build -o easydo-agent main.go
```

### 2. 配置Agent

创建配置文件 `agent.yaml`：

```yaml
# easydo-agent/config.yaml
app:
  name: easydo-agent
  mode: agent

# Server配置
server:
  addr: http://localhost:8080

# Agent配置
agent:
  id: 1                    # Agent唯一标识（注册后获得）
  token: <Token>           # 认证Token（管理员批准后获得）
  heartbeat_interval: 30   # 心跳间隔（秒）
  heartbeat_timeout: 180   # 心跳超时（秒）

# 执行器配置
executor:
  work_dir: /tmp/agent-work
  script_timeout: 3600     # 脚本执行超时（秒）
  docker:
    enabled: true          # 是否支持Docker执行
    network: bridge        # Docker网络模式
```

### 3. 启动Agent

#### 开发环境（本地运行）

```bash
make run-agent
```

或手动运行：

```bash
cd easydo-agent
go run main.go -config agent.yaml
```

#### 生产环境（Docker运行）

```bash
make up-agent
```

### 4. 验证Agent状态

访问管理界面 `/agent` 查看Agent状态：

| 状态 | 说明 |
|------|------|
| 待接纳 | 等待管理员审批 |
| 已接纳 | 管理员已批准，等待配置Token |
| 在线 | 正常运行中 |
| 离线 | 连接断开或停止运行 |

---

## Makefile命令

| 命令 | 说明 |
|------|------|
| `make build-agent` | Docker编译Agent镜像 |
| `make run-agent` | 本地运行Agent |
| `make stop-agent` | 停止本地Agent进程 |
| `make restart-agent` | 重启本地Agent |
| `make status-agent` | 查看Agent运行状态 |
| `make up-agent` | Docker启动Agent容器 |
| `make down-agent` | Docker停止Agent容器 |
| `make logs-agent` | 查看Agent容器日志 |
| `make package-agent` | 打包Agent发布包 |
| `make clean-agent` | 清理Agent构建产物 |

---

## 配置文件

### 完整配置示例

```yaml
# easydo-agent/config.yaml

# 应用配置
app:
  name: easydo-agent
  mode: agent
  log_level: info
  log_file: logs/agent.log

# Server配置
server:
  addr: http://localhost:8080     # Server地址（必填）
  timeout: 30                     # 请求超时（秒）

# Agent配置
agent:
  id: 1                           # Agent ID（注册后获得）
  token: your-token-here          # Token（管理员批准后获得）
  heartbeat_interval: 30          # 心跳间隔（秒）
  heartbeat_timeout: 180          # 心跳超时（秒）
  reconnect_delay: 5              # 重连延迟（秒）

# 执行器配置
executor:
  work_dir: /tmp/agent-work       # 工作目录
  script_timeout: 3600            # 脚本执行超时（秒）
  max_concurrent: 5               # 最大并发任务数
  
  # Docker配置
  docker:
    enabled: true                 # 是否支持Docker执行
    network: bridge               # Docker网络模式
    image_prefix: easydo/         # 镜像前缀
    pull_policy: if_not_present   # 镜像拉取策略

# 资源限制
resources:
  max_cpu: 100                    # 最大CPU使用率(%)
  max_memory: 80                  # 最大内存使用率(%)
  disk_threshold: 90              # 磁盘使用率阈值(%)
```

### 环境变量配置

也可以使用环境变量配置：

```bash
# 使用环境变量启动
EASYDO_SERVER=http://localhost:8080 \
EASYDO_AGENT_ID=1 \
EASYDO_AGENT_TOKEN=your-token-here \
EASYDO_AGENT_HEARTBEAT_INTERVAL=30 \
./easydo-agent -config agent.yaml
```

---

## 手动部署

### Step 1: 注册Agent

Agent启动后会自动向Server注册：

```bash
curl -X POST http://localhost:8080/api/agents/register \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-agent-1",
    "host": "192.168.1.100",
    "port": 8080,
    "os": "linux",
    "arch": "amd64",
    "cpu_cores": 4,
    "memory_total": 8589934592
  }'
```

返回结果：
```json
{
  "code": 200,
  "message": "注册申请已提交，等待管理员审批",
  "data": {
    "agent_id": 1,
    "name": "my-agent-1",
    "status": "offline",
    "registration_status": "pending"
  }
}
```

### Step 2: 等待管理员审批

管理员在管理界面（`/agent`）批准后，会自动生成Token并下发。

### Step 3: 获取Token

管理员批准后，在执行器详情页面可以查看Token：

1. 进入执行器管理页面 `/agent`
2. 点击已批准的执行器查看详情
3. 在"Token信息"区域复制Token

### Step 4: 配置Token

将Token配置到Agent配置文件中：

```yaml
# agent.yaml
agent:
  id: 1
  token: <管理员提供的Token>
```

### Step 5: 重启Agent

配置完成后重启Agent使Token生效：

```bash
# 本地运行
make restart-agent

# Docker运行
make down-agent && make up-agent
```

### Step 6: 验证状态

访问管理界面查看Agent状态：
- 待接纳：等待管理员审批
- 已接纳：管理员已批准，等待配置Token
- 在线：正常运行中

---

## Docker部署

### 使用Docker Compose

```bash
# 启动Agent
docker-compose up -d agent

# 查看日志
docker-compose logs -f agent

# 停止Agent
docker-compose stop agent

# 删除Agent容器
docker-compose rm agent
```

### 独立Docker运行

```bash
# 构建镜像
docker build -t easydo-agent:latest ./easydo-agent

# 运行容器
docker run -d \
  --name easydo-agent \
  -p 8080:8080 \
  -v $(pwd)/agent.yaml:/app/agent.yaml \
  -v $(pwd)/logs:/app/logs \
  -e EASYDO_AGENT_ID=1 \
  -e EASYDO_AGENT_TOKEN=your-token-here \
  easydo-agent:latest
```

---

## 故障排查

### Agent无法注册

**症状**: 注册请求超时或返回错误

**排查步骤**:
1. 检查Server地址是否正确配置
2. 检查网络连接是否正常
3. 确认Server服务是否正常运行

```bash
# 测试Server连接
curl http://localhost:8080/api/health

# 检查Agent日志
make logs-agent
```

### 心跳失败

**症状**: Agent显示离线状态，心跳请求失败

**排查步骤**:
1. 检查Token是否正确配置
2. 检查Agent ID是否正确
3. 确认Agent是否已通过管理员审批

```bash
# 验证Token配置
cat agent.yaml | grep -A2 agent

# 查看Agent状态
curl http://localhost:8080/api/agents/1
```

### 任务执行失败

**症状**: 任务启动后立即失败或超时

**排查步骤**:
1. 检查执行器权限
2. 检查工作目录配置
3. 检查Docker是否正常运行

```bash
# 检查Docker状态
docker info

# 检查工作目录权限
ls -la /tmp/agent-work

# 查看Agent执行日志
make logs-agent
```

### Docker执行失败

**症状**: 使用Docker执行任务时失败

**排查步骤**:
1. 确认Docker已安装并运行
2. 检查Docker权限配置
3. 确认网络模式配置正确

```bash
# 检查Docker daemon状态
docker version

# 测试Docker执行
docker run --rm hello-world

# 检查Agent Docker配置
cat agent.yaml | grep -A5 docker
```

### 常见错误代码

| 错误代码 | 说明 | 解决方案 |
|---------|------|---------|
| 400 | 参数错误 | 检查请求参数是否完整 |
| 401 | 认证失败 | 检查Token是否正确 |
| 403 | 无权限 | 确认管理员已批准 |
| 404 | Agent不存在 | 检查Agent ID |
| 500 | 服务器错误 | 查看Server日志 |

### 日志位置

| 环境 | 日志路径 |
|------|---------|
| 本地 | `./logs/agent.log` |
| Docker | `/app/logs/agent.log` |
| Systemd | `/var/log/easydo/agent.log` |

---

## 监控和维护

### 查看Agent状态

```bash
# 本地运行状态
make status-agent

# Docker容器状态
docker ps | grep agent

# API查询
curl http://localhost:8080/api/agents/1
```

### 心跳记录

可以通过API查询Agent的心跳历史：

```bash
curl "http://localhost:8080/api/agents/1/heartbeats?page=1&page_size=100"
```

### 刷新Token

如需更换Agent的Token：

1. 进入执行器管理页面 `/agent`
2. 点击目标执行器查看详情
3. 在"Token信息"区域点击"刷新Token"
4. 确认后生成新Token
5. 更新Agent配置文件中的Token
6. 重启Agent

---

## 安全建议

1. **Token安全**: Token是Agent的身份凭证，不要泄露给他人
2. **配置文件**: 将agent.yaml加入.gitignore，避免提交到代码仓库
3. **最小权限**: 为Agent工作目录设置最小必要权限
4. **网络安全**: 生产环境建议使用HTTPS
5. **定期更换**: 定期更换Token以提高安全性
