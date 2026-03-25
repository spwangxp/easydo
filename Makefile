# ════════════════════════════════════════════════════════════════
# EasyDo 项目统一 Makefile v3.0
# 提供所有服务以及整体的 构建、运行、清理、调试、日志 等操作
# ════════════════════════════════════════════════════════════════

# 颜色定义
RED      = \033[0;31m
GREEN    = \033[0;32m
YELLOW   = \033[1;33m
BLUE     = \033[0;34m
CYAN     = \033[0;36m
NC       = \033[0m

# 项目配置
PROJECT_NAME   = easydo
FRONTEND_DIR   = easydo-frontend
SERVER_DIR     = easydo-server
AGENT_DIR      = easydo-agent
COMPOSE_FILE   = docker-compose.yml
MULTI_COMPOSE_FILE = docker-compose.multireplica.yml

# Docker Compose 配置
DC             = docker-compose -f $(COMPOSE_FILE)
DC_MULTI       = docker-compose -f $(COMPOSE_FILE) -f $(MULTI_COMPOSE_FILE)

# ════════════════════════════════════════════════════════════════
# 一键操作 (所有模块)
# ════════════════════════════════════════════════════════════════

.PHONY: all help build run stop down restart logs status ports
.PHONY: build-all run-all down-all restart-all logs-all status-all
.PHONY: clean-all prune-all rebuild-all debug-all
.PHONY: multi-up multi-down multi-status multi-logs multi-rebuild multi-debug
.PHONY: scale-server scale-server-up scale-server-down scale-agent scale-agent-up scale-agent-down
.PHONY: clean-multi-front clean-multi-srv clean-multi-agent
.PHONY: build-multi-front build-multi-srv build-multi-agent
.PHONY: deploy-multi-front deploy-multi-srv deploy-multi-agent
.PHONY: debug-multi-front debug-multi-srv debug-multi-agent

all help:
	@echo ""
	@echo "$(CYAN)╔════════════════════════════════════════════════════════════╗$(NC)"
	@echo "$(CYAN)║                    EasyDo 项目管理 v3.0                    ║$(NC)"
	@echo "$(CYAN)╚════════════════════════════════════════════════════════════╝$(NC)"
	@echo ""
	@echo "$(YELLOW)📦 一键操作:$(NC)"
	@echo "   make build        - 编译所有模块"
	@echo "   make up          - 启动所有服务"
	@echo "   make down        - 停止所有服务"
	@echo "   make restart     - 重启所有服务"
	@echo "   make logs        - 查看所有日志"
	@echo "   make status      - 查看服务状态"
	@echo ""
	@echo "$(YELLOW)🚀 多副本快捷命令:$(NC)"
	@echo "   make multi-up       - 启动多副本栈 (server/server2 + agent/agent2 + LB)"
	@echo "   make multi-down     - 停止多副本栈"
	@echo "   make multi-status   - 查看多副本栈状态"
	@echo "   make multi-logs     - 查看多副本栈日志"
	@echo "   make multi-debug    - 清理现场 + 删除旧镜像 + 重新编译并部署多副本栈"
	@echo "   make scale-server-up   - 扩容后端到 2 副本"
	@echo "   make scale-server-down - 缩容后端到 1 副本 (保留 LB)"
	@echo "   make scale-agent-up    - 扩容 Agent 到 2 副本"
	@echo "   make scale-agent-down  - 缩容 Agent 到 1 副本"
	@echo "   make scale-server N=1|2 - 设置后端副本数"
	@echo "   make scale-agent N=1|2  - 设置 Agent 副本数"
	@echo ""
	@echo "$(YELLOW)🚀 多副本单模块操作:$(NC)"
	@echo "   make clean-multi-front - 清理多副本前端产物"
	@echo "   make clean-multi-srv   - 清理多副本后端产物"
	@echo "   make clean-multi-agent - 清理多副本 Agent 产物"
	@echo "   make build-multi-front - 编译多副本前端镜像"
	@echo "   make build-multi-srv   - 编译多副本后端镜像"
	@echo "   make build-multi-agent - 编译多副本 Agent 镜像"
	@echo "   make deploy-multi-front - 部署多副本前端 (停+编+启)"
	@echo "   make deploy-multi-srv   - 部署多副本后端 (停 server1/2 + 编 + 启)"
	@echo "   make deploy-multi-agent - 部署多副本 Agent (停 agent1/2 + 编 + 启)"
	@echo "   make debug-multi-front  - 调试多副本前端 (删镜像+编+启)"
	@echo "   make debug-multi-srv    - 调试多副本后端 (删镜像+编+启)"
	@echo "   make debug-multi-agent  - 调试多副本 Agent (删镜像+编+启)"
	@echo ""
	@echo "$(YELLOW)🔧 运维操作:$(NC)"
	@echo "   make clean        - 清理构建产物"
	@echo "   make prune       - 完整清理（停止+删除容器+镜像）"
	@echo "   make rebuild      - 重新编译并启动"
	@echo "   make debug       - 清理镜像 + 重新编译 + 启动"
	@echo ""
	@echo "$(YELLOW)🐳 单模块编译:$(NC)"
	@echo "   make build-front - 编译前端"
	@echo "   make build-srv   - 编译后端"
	@echo "   make build-agent - 编译 Agent"
	@echo ""
	@echo "$(YELLOW)🐳 单模块清理产物:$(NC)"
	@echo "   make clean-front - 清理前端产物"
	@echo "   make clean-srv   - 清理后端产物"
	@echo "   make clean-agent - 清理 Agent 产物"
	@echo ""
	@echo "$(YELLOW)🐳 单模块删除镜像:$(NC)"
	@echo "   make rmi-front  - 删除前端镜像"
	@echo "   make rmi-srv    - 删除后端镜像"
	@echo "   make rmi-agent  - 删除 Agent 镜像"
	@echo ""
	@echo "$(YELLOW)🐳 单模块启停:$(NC)"
	@echo "   make start-front  - 启动前端"
	@echo "   make start-srv    - 启动后端"
	@echo "   make start-agent - 启动 Agent"
	@echo "   make stop-front   - 停止前端"
	@echo "   make stop-srv     - 停止后端"
	@echo "   make stop-agent  - 停止 Agent"
	@echo ""
	@echo "$(YELLOW)🐳 单模块重启:$(NC)"
	@echo "   make reload-front - 重启前端"
	@echo "   make reload-srv   - 重启后端"
	@echo "   make reload-agent - 重启 Agent"
	@echo ""
	@echo "$(YELLOW)🐳 单模块停止+删除容器:$(NC)"
	@echo "   make down-front  - 停止删除前端容器"
	@echo "   make down-srv    - 停止删除后端容器"
	@echo "   make down-agent - 停止删除 Agent 容器"
	@echo ""
	@echo "$(YELLOW)🐳 单模块日志:$(NC)"
	@echo "   make logs-front  - 前端日志"
	@echo "   make logs-srv    - 后端日志"
	@echo "   make logs-agent  - Agent 日志"
	@echo ""
	@echo "$(YELLOW)🐳 单模块重建:$(NC)"
	@echo "   make rebuild-front - 重建前端"
	@echo "   make rebuild-srv   - 重建后端"
	@echo "   make rebuild-agent - 重建 Agent"
	@echo "   make debug-front  - 调试前端"
	@echo "   make debug-srv    - 调试后端"
	@echo "   make debug-agent  - 调试 Agent"
	@echo ""
	@echo "$(YELLOW)⚡ 快捷命令:$(NC)"
	@echo "   make b           - build (编译)"
	@echo "   make u           - up (启动)"
	@echo "   make d           - down (停止)"
	@echo "   make r           - restart (重启)"
	@echo "   make c           - clean (清理产物)"
	@echo "   make s           - status (状态)"
	@echo "   make l           - logs (日志)"
	@echo ""
	@echo "$(YELLOW)🔗 访问地址:$(NC)"
	@echo "   前端: http://localhost"
	@echo "   后端: http://localhost:8080"
	@echo "   MySQL: localhost:3306"
	@echo "   Redis: localhost:6379"
	@echo ""

# ════════════════════════════════════════════════════════════════
# 核心操作
# ════════════════════════════════════════════════════════════════

build:
	@echo ""
	@echo "$(BLUE)📦 编译所有模块...$(NC)"
	@make _build-front _build-srv _build-agent
	@echo ""
	@echo "$(GREEN)✅ 所有模块编译完成!$(NC)"

up:
	@echo ""
	@echo "$(GREEN)🚀 启动所有服务...$(NC)"
	@$(DC) up -d
	@sleep 2
	@make ports
	@echo ""
	@echo "$(GREEN)✅ 服务启动成功!$(NC)"

down:
	@echo ""
	@echo "$(YELLOW)🛑 停止所有服务...$(NC)"
	@$(DC) down
	@echo "$(GREEN)✅ 服务已停止!$(NC)"

restart: down up
	@echo "$(GREEN)✅ 服务已重启!$(NC)"

logs:
	@$(DC) logs -f

status:
	@echo ""
	@echo "$(BLUE)📊 服务状态:$(NC)"
	@$(DC) ps
	@make ports

ports:
	@echo ""
	@echo "$(YELLOW)📡 端口占用:$(NC)"
	@echo "   前端 (HTTP):    :80"
	@echo "   前端多副本:     :8088"
	@echo "   后端 (API):     :8080"
	@echo "   MySQL:          :3306"
	@echo "   Redis:          :6379"

# ════════════════════════════════════════════════════════════════
# 多副本操作
# ════════════════════════════════════════════════════════════════

multi-up:
	@echo ""
	@echo "$(GREEN)🚀 启动多副本服务栈...$(NC)"
	@$(DC_MULTI) up -d
	@sleep 2
	@make multi-status
	@echo ""
	@echo "$(GREEN)✅ 多副本服务已启动!$(NC)"

multi-down:
	@echo ""
	@echo "$(YELLOW)🛑 停止多副本服务栈...$(NC)"
	@$(DC_MULTI) down --remove-orphans
	@echo "$(GREEN)✅ 多副本服务已停止!$(NC)"

multi-status:
	@echo ""
	@echo "$(BLUE)📊 多副本服务状态:$(NC)"
	@$(DC_MULTI) ps
	@echo ""
	@echo "$(YELLOW)📡 多副本访问地址:$(NC)"
	@echo "   前端(LB):       http://localhost:8088"
	@echo "   后端(API):      http://localhost:8080"

multi-logs:
	@$(DC_MULTI) logs -f

multi-rebuild:
	@echo ""
	@echo "$(CYAN)🔄 重建多副本服务栈...$(NC)"
	@$(DC_MULTI) down --remove-orphans
	@$(DC_MULTI) up --build -d
	@echo "$(GREEN)✅ 多副本服务已重建!$(NC)"

multi-debug:
	@echo ""
	@echo "$(RED)🔥 清理现场 + 删除旧镜像 + 重新编译并部署多副本栈...$(NC)"
	@$(DC_MULTI) down -v --remove-orphans 2>/dev/null || true
	@docker rmi easydo3-frontend:latest easydo3-server:latest easydo3-server2:latest easydo3-agent:latest easydo3-agent2:latest 2>/dev/null || true
	@$(DC_MULTI) up -d --build
	@echo ""
	@echo "$(GREEN)✅ 多副本调试部署完成!$(NC)"
	@$(DC_MULTI) ps

scale-server:
	@if [ "$(N)" = "2" ]; then \
		$(MAKE) scale-server-up; \
	elif [ "$(N)" = "1" ]; then \
		$(MAKE) scale-server-down; \
	else \
		echo "$(RED)❌ 用法: make scale-server N=1 或 make scale-server N=2$(NC)"; \
		exit 1; \
	fi

scale-server-up:
	@echo ""
	@echo "$(GREEN)📈 扩容后端到 2 副本...$(NC)"
	@$(DC_MULTI) up -d server server2 server-lb frontend
	@$(DC_MULTI) ps server server2 server-lb frontend
	@echo "$(GREEN)✅ 后端已扩容到 2 副本!$(NC)"

scale-server-down:
	@echo ""
	@echo "$(YELLOW)📉 缩容后端到 1 副本...$(NC)"
	@$(DC_MULTI) stop server2
	@$(DC_MULTI) rm -f server2 >/dev/null 2>&1 || true
	@$(DC_MULTI) ps server server2 server-lb frontend
	@echo "$(GREEN)✅ 后端已缩容到 1 副本!$(NC)"

scale-agent:
	@if [ "$(N)" = "2" ]; then \
		$(MAKE) scale-agent-up; \
	elif [ "$(N)" = "1" ]; then \
		$(MAKE) scale-agent-down; \
	else \
		echo "$(RED)❌ 用法: make scale-agent N=1 或 make scale-agent N=2$(NC)"; \
		exit 1; \
	fi

scale-agent-up:
	@echo ""
	@echo "$(GREEN)📈 扩容 Agent 到 2 副本...$(NC)"
	@$(DC_MULTI) up -d agent agent2
	@$(DC_MULTI) ps agent agent2
	@echo "$(GREEN)✅ Agent 已扩容到 2 副本!$(NC)"

scale-agent-down:
	@echo ""
	@echo "$(YELLOW)📉 缩容 Agent 到 1 副本...$(NC)"
	@$(DC_MULTI) stop agent2
	@$(DC_MULTI) rm -f agent2 >/dev/null 2>&1 || true
	@$(DC_MULTI) ps agent agent2
	@echo "$(GREEN)✅ Agent 已缩容到 1 副本!$(NC)"

# ════════════════════════════════════════════════════════════════
# 多副本单模块操作 (clean / build / deploy / debug)
# ════════════════════════════════════════════════════════════════

.PHONY: clean-multi-front clean-multi-srv clean-multi-agent
.PHONY: build-multi-front build-multi-srv build-multi-agent
.PHONY: deploy-multi-front deploy-multi-srv deploy-multi-agent
.PHONY: debug-multi-front debug-multi-srv debug-multi-agent

# --- clean (only local build artifacts; containers/images handled by deploy/debug) ---

clean-multi-front:
	@echo ""
	@echo "$(YELLOW)🧹 [多副本] 清理 Frontend 构建产物...$(NC)"
	@make -C $(FRONTEND_DIR) clean
	@echo "$(GREEN)✅ [多副本] Frontend 产物已清理!$(NC)"

clean-multi-srv:
	@echo ""
	@echo "$(YELLOW)🧹 [多副本] 清理 Server 构建产物...$(NC)"
	@make -C $(SERVER_DIR) clean
	@echo "$(GREEN)✅ [多副本] Server 产物已清理!$(NC)"

clean-multi-agent:
	@echo ""
	@echo "$(YELLOW)🧹 [多副本] 清理 Agent 构建产物...$(NC)"
	@make -C $(AGENT_DIR) clean
	@echo "$(GREEN)✅ [多副本] Agent 产物已清理!$(NC)"

# --- build (compile images only, no restart) ---

build-multi-front:
	@make -C $(FRONTEND_DIR) build

build-multi-srv:
	@make -C $(SERVER_DIR) build

build-multi-agent:
	@make -C $(AGENT_DIR) build

# --- deploy (stop containers -> rebuild images -> start containers) ---

deploy-multi-front:
	@echo ""
	@echo "$(CYAN)🔄 [多副本] 部署 Frontend...$(NC)"
	@$(DC_MULTI) down frontend 2>/dev/null || true
	@$(DC_MULTI) up --build -d frontend
	@echo "$(GREEN)✅ [多副本] Frontend 部署完成!$(NC)"

deploy-multi-srv:
	@echo ""
	@echo "$(CYAN)🔄 [多副本] 部署 Server...$(NC)"
	@$(DC_MULTI) down server server2 2>/dev/null || true
	@$(DC_MULTI) up --build -d server server2 server-lb
	@echo "$(GREEN)✅ [多副本] Server 部署完成!$(NC)"

deploy-multi-agent:
	@echo ""
	@echo "$(CYAN)🔄 [多副本] 部署 Agent...$(NC)"
	@$(DC_MULTI) down agent agent2 2>/dev/null || true
	@$(DC_MULTI) up --build -d agent agent2
	@echo "$(GREEN)✅ [多副本] Agent 部署完成!$(NC)"

# --- debug (stop -> delete images -> rebuild -> start) ---

debug-multi-front:
	@echo ""
	@echo "$(RED)🔥 [多副本] 调试部署 Frontend...$(NC)"
	@$(DC_MULTI) down frontend 2>/dev/null || true
	@docker rmi easydo-frontend easydo-frontend:latest 2>/dev/null || true
	@$(DC_MULTI) up --build -d frontend
	@echo "$(GREEN)✅ [多副本] Frontend 调试部署完成!$(NC)"

debug-multi-srv:
	@echo ""
	@echo "$(RED)🔥 [多副本] 调试部署 Server...$(NC)"
	@$(DC_MULTI) down server server2 2>/dev/null || true
	@docker rmi easydo-server easydo-server:latest 2>/dev/null || true
	@$(DC_MULTI) up --build -d server server2 server-lb
	@echo "$(GREEN)✅ [多副本] Server 调试部署完成!$(NC)"

debug-multi-agent:
	@echo ""
	@echo "$(RED)🔥 [多副本] 调试部署 Agent...$(NC)"
	@$(DC_MULTI) down agent agent2 2>/dev/null || true
	@docker rmi easydo-agent easydo-agent:latest 2>/dev/null || true
	@$(DC_MULTI) up --build -d agent agent2
	@echo "$(GREEN)✅ [多副本] Agent 调试部署完成!$(NC)"

# ════════════════════════════════════════════════════════════════
# 编译操作
# ════════════════════════════════════════════════════════════════

build-all: build

_build-front:
	@make -C $(FRONTEND_DIR) build

_build-srv:
	@make -C $(SERVER_DIR) build

_build-agent:
	@make -C $(AGENT_DIR) build

build-front:
	@make -C $(FRONTEND_DIR) build

build-srv:
	@make -C $(SERVER_DIR) build

build-agent:
	@make -C $(AGENT_DIR) build

# ════════════════════════════════════════════════════════════════
# 运行操作
# ════════════════════════════════════════════════════════════════

run-all: up

up-all: up

down-all: down

restart-all: restart

# ════════════════════════════════════════════════════════════════
# 日志操作
# ════════════════════════════════════════════════════════════════

logs-all: logs

logs-front:
	@$(DC) logs -f frontend

logs-srv:
	@$(DC) logs -f server

logs-agent:
	@$(DC) logs -f agent

# ════════════════════════════════════════════════════════════════
# 单个模块清理操作
# ════════════════════════════════════════════════════════════════

clean-front:
	@echo ""
	@echo "$(YELLOW)🧹 清理 Frontend 构建产物...$(NC)"
	@make -C $(FRONTEND_DIR) clean
	@echo "$(GREEN)✅ Frontend 产物已清理!$(NC)"

clean-srv:
	@echo ""
	@echo "$(YELLOW)🧹 清理 Server 构建产物...$(NC)"
	@make -C $(SERVER_DIR) clean
	@echo "$(GREEN)✅ Server 产物已清理!$(NC)"

clean-agent:
	@echo ""
	@echo "$(YELLOW)🧹 清理 Agent 构建产物...$(NC)"
	@make -C $(AGENT_DIR) clean
	@echo "$(GREEN)✅ Agent 产物已清理!$(NC)"

# ════════════════════════════════════════════════════════════════
# 单个模块删除镜像操作
# ════════════════════════════════════════════════════════════════

rmi-front:
	@echo ""
	@echo "$(RED)🗑️ 删除 Frontend 镜像...$(NC)"
	@docker rmi easydo-frontend easydo-frontend:latest 2>/dev/null || true
	@echo "$(GREEN)✅ Frontend 镜像已删除!$(NC)"

rmi-srv:
	@echo ""
	@echo "$(RED)🗑️ 删除 Server 镜像...$(NC)"
	@docker rmi easydo-server easydo-server:latest 2>/dev/null || true
	@echo "$(GREEN)✅ Server 镜像已删除!$(NC)"

rmi-agent:
	@echo ""
	@echo "$(RED)🗑️ 删除 Agent 镜像...$(NC)"
	@docker rmi easydo-agent easydo-agent:latest 2>/dev/null || true
	@echo "$(GREEN)✅ Agent 镜像已删除!$(NC)"

# ════════════════════════════════════════════════════════════════
# 单个模块停止+删除容器
# ════════════════════════════════════════════════════════════════

down-front:
	@echo ""
	@echo "$(YELLOW)🛑 停止并删除 Frontend 容器...$(NC)"
	@$(DC) down frontend
	@echo "$(GREEN)✅ Frontend 已停止并删除!$(NC)"

down-srv:
	@echo ""
	@echo "$(YELLOW)🛑 停止并删除 Server 容器...$(NC)"
	@$(DC) down server
	@echo "$(GREEN)✅ Server 已停止并删除!$(NC)"

down-agent:
	@echo ""
	@echo "$(YELLOW)🛑 停止并删除 Agent 容器...$(NC)"
	@$(DC) down agent
	@echo "$(GREEN)✅ Agent 已停止并删除!$(NC)"

# ════════════════════════════════════════════════════════════════
# 单个模块重新加载 (重新启动)
# ════════════════════════════════════════════════════════════════

reload-front:
	@echo ""
	@echo "$(CYAN)🔄 重新加载 Frontend...$(NC)"
	@$(DC) restart frontend
	@echo "$(GREEN)✅ Frontend 已重新加载!$(NC)"

reload-srv:
	@echo ""
	@echo "$(CYAN)🔄 重新加载 Server...$(NC)"
	@$(DC) restart server
	@echo "$(GREEN)✅ Server 已重新加载!$(NC)"

reload-agent:
	@echo ""
	@echo "$(CYAN)🔄 重新加载 Agent...$(NC)"
	@$(DC) restart agent
	@echo "$(GREEN)✅ Agent 已重新加载!$(NC)"

# ════════════════════════════════════════════════════════════════
# 状态操作
# ════════════════════════════════════════════════════════════════

status-all: status

status-front:
	@$(DC) ps frontend

status-srv:
	@$(DC) ps server

status-agent:
	@$(DC) ps agent

# ════════════════════════════════════════════════════════════════
# 清理操作
# ════════════════════════════════════════════════════════════════

clean:
	@echo ""
	@echo "$(YELLOW)🧹 清理构建产物...$(NC)"
	@make -C $(FRONTEND_DIR) clean
	@make -C $(SERVER_DIR) clean
	@make -C $(AGENT_DIR) clean
	@echo "$(GREEN)✅ 产物已清理!$(NC)"

prune:
	@echo ""
	@echo "$(RED)💥 完整清理 (停止+删除容器+镜像)...$(NC)"
	@$(DC) down -v --remove-orphans 2>/dev/null || true
	@docker rmi easydo-frontend easydo-frontend:latest 2>/dev/null || true
	@docker rmi easydo-server easydo-server:latest 2>/dev/null || true
	@docker rmi easydo-agent easydo-agent:latest 2>/dev/null || true
	@make -C $(FRONTEND_DIR) clean
	@make -C $(SERVER_DIR) clean
	@make -C $(AGENT_DIR) clean
	@echo "$(GREEN)✅ 完整清理完成!$(NC)"

# ════════════════════════════════════════════════════════════════
# 重建操作 (不删除镜像，只重启容器)
# ════════════════════════════════════════════════════════════════

rebuild:
	@echo ""
	@echo "$(CYAN)🔄 重新编译并启动 (保留镜像)...$(NC)"
	@$(DC) down
	@make build
	@$(DC) up -d
	@echo "$(GREEN)✅ 重建完成!$(NC)"

rebuild-front:
	@$(DC) down frontend 2>/dev/null || true
	@make -C $(FRONTEND_DIR) build
	@$(DC) up -d frontend
	@echo "$(GREEN)✅ Frontend 重建完成!$(NC)"

rebuild-srv:
	@$(DC) down server 2>/dev/null || true
	@make -C $(SERVER_DIR) build
	@$(DC) up -d server
	@echo "$(GREEN)✅ Server 重建完成!$(NC)"

rebuild-agent:
	@$(DC) down agent 2>/dev/null || true
	@make -C $(AGENT_DIR) build
	@$(DC) up -d agent
	@echo "$(GREEN)✅ Agent 重建完成!$(NC)"

# ════════════════════════════════════════════════════════════════
# 调试部署 (删除镜像 + 重新编译 + 启动)
# ════════════════════════════════════════════════════════════════

debug:
	@echo ""
	@echo "$(RED)🔥 调试部署 (删除旧镜像)...$(NC)"
	@$(DC) down -v --remove-orphans 2>/dev/null || true
	@docker rmi easydo-frontend easydo-frontend:latest 2>/dev/null || true
	@docker rmi easydo-server easydo-server:latest 2>/dev/null || true
	@docker rmi easydo-agent easydo-agent:latest 2>/dev/null || true
	@make build
	@$(DC) up -d
	@echo "$(GREEN)✅ 调试部署完成!$(NC)"

debug-all: debug

debug-front:
	@make -C $(FRONTEND_DIR) debug

debug-srv:
	@make -C $(SERVER_DIR) debug

debug-agent:
	@make -C $(AGENT_DIR) debug

# ════════════════════════════════════════════════════════════════
# 单个模块快捷操作
# ════════════════════════════════════════════════════════════════

.PHONY: start stop rm

start: up
stop: down

start-front:
	@$(DC) up -d frontend

stop-front:
	@$(DC) down frontend

start-srv:
	@$(DC) up -d server

stop-srv:
	@$(DC) down server

start-agent:
	@$(DC) up -d agent

stop-agent:
	@$(DC) down agent

rm:
	@$(DC) rm -f

rm-front:
	@$(DC) rm -f frontend

rm-srv:
	@$(DC) rm -f server

rm-agent:
	@$(DC) rm -f agent

# ════════════════════════════════════════════════════════════════
# 健康检查
# ════════════════════════════════════════════════════════════════

.PHONY: health check

health check:
	@echo ""
	@echo "$(BLUE)🏥 服务健康检查:$(NC)"
	@echo ""
	@$(DC) ps --format "table {{.Name}}\t{{.Status}}\t{{.Ports}}"
	@echo ""
	@echo "$(YELLOW)测试 API 连通性:$(NC)"
	@-curl -s -o /dev/null -w "   API : %{http_code}\n" http://localhost:8080/api/health 2>/dev/null || echo "   API : 无法连接"
	@-curl -s -o /dev/null -w "   Frontend : %{http_code}\n" http://localhost 2>/dev/null || echo "   Frontend : 无法连接"
	@echo ""

# ════════════════════════════════════════════════════════════════
# 快捷别名
# ════════════════════════════════════════════════════════════════

b  = build
u  = up
d  = down
r  = restart
c  = clean
p  = prune
s  = status
l  = logs
h  = help

bu = build
rb = rebuild
db = debug

# ════════════════════════════════════════════════════════════════
# 开发常用命令
# ════════════════════════════════════════════════════════════════

.PHONY: dev dev-front dev-srv dev-agent

dev:
	@echo ""
	@echo "$(CYAN)🔧 开发模式: 编译 + 启动 + 实时日志$(NC)"
	@make build
	@$(DC) up -d
	@sleep 3
	@echo ""
	@echo "$(GREEN)✅ 开发环境已启动!$(NC)"
	@echo "$(YELLOW)查看日志: make logs$(NC)"

dev-front:
	@make build-front && $(DC) up -d frontend && $(DC) logs -f frontend

dev-srv:
	@make build-srv && $(DC) up -d server && $(DC) logs -f server

dev-agent:
	@make build-agent && $(DC) up -d agent && $(DC) logs -f agent

# ════════════════════════════════════════════════════════════════
# Agent 专用命令
# ════════════════════════════════════════════════════════════════

.PHONY: agent-logs agent-status agent-restart agent-debug

agent-logs:
	@$(DC) logs -f agent

agent-status:
	@$(DC) ps agent

agent-restart:
	@$(DC) restart agent

agent-debug:
	@make -C $(AGENT_DIR) debug

# ════════════════════════════════════════════════════════════════
# 基础设施 (db, redis)
# ════════════════════════════════════════════════════════════════

.PHONY: infra up-infra down-infra restart-infra

infra db-redis:
	@$(DC) up -d db redis

up-infra:
	@$(DC) up -d db redis

down-infra:
	@$(DC) down db redis

restart-infra:
	@$(DC) restart db redis

# ════════════════════════════════════════════════════════════════
# 版本信息
# ════════════════════════════════════════════════════════════════

.PHONY: version info

version info:
	@echo ""
	@echo "$(BLUE)📋 Git 版本信息:$(NC)"
	@echo "   Commit: $$(git rev-parse HEAD 2>/dev/null || echo 'unknown')"
	@echo "   Short:  $$(git rev-parse --short HEAD 2>/dev/null || echo 'unknown')"
	@echo "   Date:   $$(date -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || echo 'unknown')"
	@echo ""
	@echo "$(BLUE)🐳 镜像版本:$(NC)"
	@docker images --format "   {{.Repository}}:{{.Tag}} - {{.CreatedSince}}" | grep -E "easydo" || echo "   无镜像"

# ════════════════════════════════════════════════════════════════
# Docker 资源清理
# ════════════════════════════════════════════════════════════════

.PHONY: docker-clean docker-prune system-prune

docker-clean:
	@echo "$(YELLOW)🧹 清理 Docker 资源...$(NC)"
	@$(DC) down -v --remove-orphans 2>/dev/null || true
	@docker system prune -af --filter "until=1h" 2>/dev/null || true
	@echo "$(GREEN)✅ Docker 资源已清理!$(NC)"

system-prune:
	@echo "$(YELLOW)💥 完整系统清理...$(NC)"
	@make prune
	@docker system prune -af --volumes 2>/dev/null || true
	@echo "$(GREEN)✅ 系统已完整清理!$(NC)"

# ════════════════════════════════════════════════════════════════
# 帮助信息 - 简洁版
# ════════════════════════════════════════════════════════════════

help-short:
	@echo ""
	@echo "$(CYAN)EasyDo 快捷命令:$(NC)"
	@echo "  make b   - 编译所有"
	@echo "  make u   - 启动服务"
	@echo "  make d   - 停止服务"
	@echo "  make r   - 重启服务"
	@echo "  make l   - 查看日志"
	@echo "  make s   - 服务状态"
	@echo "  make c   - 清理产物"
	@echo "  make p   - 完整清理"
	@echo "  make db  - 调试部署"
	@echo "  make h   - 帮助信息"
