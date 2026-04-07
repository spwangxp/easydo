# ════════════════════════════════════════════════════════════════
# EasyDo Project Makefile
# 多副本部署唯一入口 / Multi-replica deployment only
# ════════════════════════════════════════════════════════════════

# 颜色定义
RED    = \033[0;31m
GREEN  = \033[0;32m
YELLOW = \033[1;33m
BLUE   = \033[0;34m
CYAN   = \033[0;36m
NC     = \033[0m

# 项目配置
FRONTEND_DIR = easydo-frontend
SERVER_DIR   = easydo-server
AGENT_DIR    = easydo-agent

COMPOSE_FILE = deploy/docker-compose/docker-compose.yml
DC           = docker-compose -f $(COMPOSE_FILE)

# 常用服务组
STACK_SERVICES    = db redis minio server server2 server-lb frontend agent agent2
SERVER_SERVICES   = server server2 server-lb frontend
AGENT_SERVICES    = agent agent2
INFRA_SERVICES    = db redis minio

.PHONY: all help help-short \
	build up down restart logs status ports health check \
	multi-up multi-down multi-status multi-logs \
	build-front build-srv build-agent \
	clean-front clean-srv clean-agent \
	rmi-front rmi-srv rmi-agent \
	logs-front logs-srv logs-agent \
	status-front status-srv status-agent \
	reload-front reload-srv reload-agent \
	down-front down-srv down-agent \
	rebuild rebuild-front rebuild-srv rebuild-agent \
	debug debug-front debug-srv debug-agent \
	deploy-front deploy-srv deploy-agent \
	clean prune docker-clean system-prune \
	dev dev-front dev-srv dev-agent \
	scale-server scale-server-up scale-server-down scale-agent scale-agent-up scale-agent-down \
	infra up-infra down-infra restart-infra db-redis \
	version info

all: help

help:
	@echo ""
	@echo "$(CYAN)╔════════════════════════════════════════════════════════════╗$(NC)"
	@echo "$(CYAN)║                        EasyDo Makefile                    ║$(NC)"
	@echo "$(CYAN)╚════════════════════════════════════════════════════════════╝$(NC)"
	@echo ""
	@echo "$(YELLOW)Stack$(NC)"
	@echo "  make build        - 编译全部镜像"
	@echo "  make up           - 启动多副本服务栈"
	@echo "  make down         - 停止多副本服务栈"
	@echo "  make restart      - 重启多副本服务栈"
	@echo "  make logs         - 查看服务栈日志"
	@echo "  make status       - 查看服务栈状态"
	@echo "  make health       - 健康检查"
	@echo ""
	@echo "$(YELLOW)Modules$(NC)"
	@echo "  make build-front|build-srv|build-agent"
	@echo "  make deploy-front|deploy-srv|deploy-agent"
	@echo "  make debug-front|debug-srv|debug-agent"
	@echo "  make logs-front|logs-srv|logs-agent"
	@echo "  make status-front|status-srv|status-agent"
	@echo "  make reload-front|reload-srv|reload-agent"
	@echo "  make down-front|down-srv|down-agent"
	@echo ""
	@echo "$(YELLOW)Maintenance$(NC)"
	@echo "  make rebuild      - 重建整个服务栈"
	@echo "  make debug        - 无缓存重建整个服务栈"
	@echo "  make clean        - 清理本地构建产物"
	@echo "  make prune        - 停服务并清理镜像/卷"
	@echo "  make docker-clean - 清理 Docker 临时资源"
	@echo ""
	@echo "$(YELLOW)Info$(NC)"
	@echo "  make ports        - 查看约定端口"
	@echo "  make version      - 查看版本信息"
	@echo "  make infra        - 启动基础设施(db/redis/minio)"
	@echo "  make scale-server N=1|2"
	@echo "  make scale-agent N=1|2"
	@echo ""
	@echo "$(YELLOW)URLs$(NC)"
	@echo "  Frontend: http://localhost:8088"
	@echo "  Backend : http://localhost:8080"
	@echo ""

help-short:
	@echo "make up | down | status | logs | build | rebuild | debug | health"

# ════════════════════════════════════════════════════════════════
# Stack operations
# ════════════════════════════════════════════════════════════════

build:
	@echo ""
	@echo "$(BLUE)📦 编译全部镜像...$(NC)"
	@$(MAKE) build-front
	@$(MAKE) build-srv
	@$(MAKE) build-agent
	@echo "$(GREEN)✅ 全部镜像编译完成!$(NC)"

up:
	@echo ""
	@echo "$(GREEN)🚀 启动多副本服务栈...$(NC)"
	@$(DC) up -d
	@sleep 2
	@$(MAKE) status

down:
	@echo ""
	@echo "$(YELLOW)🛑 停止多副本服务栈...$(NC)"
	@$(DC) down --remove-orphans
	@echo "$(GREEN)✅ 多副本服务已停止!$(NC)"

restart: down up

logs:
	@$(DC) logs -f

status:
	@echo ""
	@echo "$(BLUE)📊 多副本服务状态:$(NC)"
	@$(DC) ps
	@$(MAKE) ports

ports:
	@echo ""
	@echo "$(YELLOW)📡 端口:$(NC)"
	@echo "  Frontend : 8088"
	@echo "  Backend  : 8080"
	@echo "  MariaDB  : 3306"
	@echo "  Redis    : 6379"
	@echo "  MinIO    : 9000 / 9001"

health check:
	@echo ""
	@echo "$(BLUE)🏥 服务健康检查:$(NC)"
	@$(DC) ps --format "table {{.Name}}\t{{.Status}}\t{{.Ports}}"
	@echo ""
	@echo "$(YELLOW)HTTP 检查:$(NC)"
	@-curl -s -o /dev/null -w "  API      : %{http_code}\n" http://localhost:8080/api/health 2>/dev/null || echo "  API      : 无法连接"
	@-curl -s -o /dev/null -w "  Frontend : %{http_code}\n" http://localhost:8088 2>/dev/null || echo "  Frontend : 无法连接"

# 兼容旧入口
multi-up: up
multi-down: down
multi-status: status
multi-logs: logs

# ════════════════════════════════════════════════════════════════
# Module operations
# ════════════════════════════════════════════════════════════════

build-front:
	@$(MAKE) -C $(FRONTEND_DIR) build

build-srv:
	@$(MAKE) -C $(SERVER_DIR) build

build-agent:
	@$(MAKE) -C $(AGENT_DIR) build

clean-front:
	@echo "$(YELLOW)🧹 清理 Frontend 构建产物...$(NC)"
	@$(MAKE) -C $(FRONTEND_DIR) clean

clean-srv:
	@echo "$(YELLOW)🧹 清理 Server 构建产物...$(NC)"
	@$(MAKE) -C $(SERVER_DIR) clean

clean-agent:
	@echo "$(YELLOW)🧹 清理 Agent 构建产物...$(NC)"
	@$(MAKE) -C $(AGENT_DIR) clean

rmi-front:
	@echo "$(RED)🗑️ 删除 Frontend 镜像...$(NC)"
	@$(DC) down --rmi local --remove-orphans frontend 2>/dev/null || true

rmi-srv:
	@echo "$(RED)🗑️ 删除 Server 镜像...$(NC)"
	@$(DC) stop server server2 server-lb 2>/dev/null || true
	@$(DC) rm -f server server2 server-lb 2>/dev/null || true
	@docker image prune -f >/dev/null 2>&1 || true

rmi-agent:
	@echo "$(RED)🗑️ 删除 Agent 镜像...$(NC)"
	@$(DC) stop agent agent2 2>/dev/null || true
	@$(DC) rm -f agent agent2 2>/dev/null || true
	@docker image prune -f >/dev/null 2>&1 || true

logs-front:
	@$(DC) logs -f frontend

logs-srv:
	@$(DC) logs -f server server2 server-lb

logs-agent:
	@$(DC) logs -f agent agent2

status-front:
	@$(DC) ps frontend

status-srv:
	@$(DC) ps server server2 server-lb

status-agent:
	@$(DC) ps agent agent2

reload-front:
	@$(DC) restart frontend

reload-srv:
	@$(DC) restart server server2 server-lb

reload-agent:
	@$(DC) restart agent agent2

down-front:
	@$(DC) stop frontend 2>/dev/null || true
	@$(DC) rm -f frontend 2>/dev/null || true

down-srv:
	@$(DC) stop server server2 server-lb 2>/dev/null || true
	@$(DC) rm -f server server2 server-lb 2>/dev/null || true

down-agent:
	@$(DC) stop agent agent2 2>/dev/null || true
	@$(DC) rm -f agent agent2 2>/dev/null || true

deploy-front:
	@echo "$(CYAN)🔄 部署 Frontend...$(NC)"
	@$(MAKE) down-front
	@$(DC) up --build -d frontend

deploy-srv:
	@echo "$(CYAN)🔄 部署 Server...$(NC)"
	@$(MAKE) down-srv
	@$(DC) up --build -d $(SERVER_SERVICES)

deploy-agent:
	@echo "$(CYAN)🔄 部署 Agent...$(NC)"
	@$(MAKE) down-agent
	@$(DC) up --build -d $(AGENT_SERVICES)

rebuild-front:
	@$(MAKE) deploy-front

rebuild-srv:
	@$(MAKE) deploy-srv

rebuild-agent:
	@$(MAKE) deploy-agent

debug-front:
	@echo "$(RED)🔥 无缓存部署 Frontend...$(NC)"
	@$(MAKE) down-front
	@$(DC) build --no-cache frontend
	@$(DC) up -d frontend

debug-srv:
	@echo "$(RED)🔥 无缓存部署 Server...$(NC)"
	@$(MAKE) down-srv
	@$(DC) build --no-cache server server2
	@$(DC) up -d $(SERVER_SERVICES)

debug-agent:
	@echo "$(RED)🔥 无缓存部署 Agent...$(NC)"
	@$(MAKE) down-agent
	@$(DC) build --no-cache agent agent2
	@$(DC) up -d $(AGENT_SERVICES)

# ════════════════════════════════════════════════════════════════
# Maintenance
# ════════════════════════════════════════════════════════════════

clean:
	@echo ""
	@echo "$(YELLOW)🧹 清理本地构建产物...$(NC)"
	@$(MAKE) clean-front
	@$(MAKE) clean-srv
	@$(MAKE) clean-agent
	@echo "$(GREEN)✅ 本地产物清理完成!$(NC)"

rebuild:
	@echo ""
	@echo "$(CYAN)🔄 重建整个服务栈...$(NC)"
	@$(DC) down --remove-orphans
	@$(DC) up -d --build
	@echo "$(GREEN)✅ 服务栈重建完成!$(NC)"

debug:
	@echo ""
	@echo "$(RED)🔥 无缓存重建整个服务栈...$(NC)"
	@$(DC) down -v --remove-orphans --rmi local 2>/dev/null || true
	@$(DC) build --no-cache
	@$(DC) up -d
	@echo "$(GREEN)✅ 无缓存部署完成!$(NC)"

prune:
	@echo ""
	@echo "$(RED)💥 停服务并清理镜像/卷...$(NC)"
	@$(DC) down -v --remove-orphans --rmi local 2>/dev/null || true
	@$(MAKE) clean
	@echo "$(GREEN)✅ 清理完成!$(NC)"

docker-clean:
	@echo "$(YELLOW)🧹 清理 Docker 临时资源...$(NC)"
	@$(DC) down -v --remove-orphans 2>/dev/null || true
	@docker system prune -af --filter "until=1h" 2>/dev/null || true
	@echo "$(GREEN)✅ Docker 临时资源已清理!$(NC)"

system-prune:
	@echo "$(YELLOW)💥 完整系统清理...$(NC)"
	@$(MAKE) prune
	@docker system prune -af --volumes 2>/dev/null || true
	@echo "$(GREEN)✅ 系统已完整清理!$(NC)"

# ════════════════════════════════════════════════════════════════
# Development
# ════════════════════════════════════════════════════════════════

dev:
	@echo "$(CYAN)🔧 开发模式: build + up + logs$(NC)"
	@$(MAKE) build
	@$(DC) up -d
	@sleep 3
	@$(DC) logs -f

dev-front:
	@$(MAKE) build-front && $(DC) up -d frontend && $(DC) logs -f frontend

dev-srv:
	@$(MAKE) build-srv && $(DC) up -d $(SERVER_SERVICES) && $(DC) logs -f server server2 server-lb

dev-agent:
	@$(MAKE) build-agent && $(DC) up -d $(AGENT_SERVICES) && $(DC) logs -f agent agent2

# ════════════════════════════════════════════════════════════════
# Scaling / infra
# ════════════════════════════════════════════════════════════════

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
	@echo "$(GREEN)📈 启用双后端副本...$(NC)"
	@$(DC) up -d $(SERVER_SERVICES)
	@$(DC) ps $(SERVER_SERVICES)

scale-server-down:
	@echo "$(YELLOW)📉 停用第二个后端副本...$(NC)"
	@$(DC) stop server2 2>/dev/null || true
	@$(DC) rm -f server2 2>/dev/null || true
	@$(DC) ps server server2 server-lb frontend

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
	@echo "$(GREEN)📈 启用双 Agent 副本...$(NC)"
	@$(DC) up -d $(AGENT_SERVICES)
	@$(DC) ps $(AGENT_SERVICES)

scale-agent-down:
	@echo "$(YELLOW)📉 停用第二个 Agent 副本...$(NC)"
	@$(DC) stop agent2 2>/dev/null || true
	@$(DC) rm -f agent2 2>/dev/null || true
	@$(DC) ps agent agent2

infra db-redis:
	@$(DC) up -d $(INFRA_SERVICES)

up-infra: infra

down-infra:
	@$(DC) stop $(INFRA_SERVICES) 2>/dev/null || true
	@$(DC) rm -f $(INFRA_SERVICES) 2>/dev/null || true

restart-infra:
	@$(DC) restart $(INFRA_SERVICES)

# ════════════════════════════════════════════════════════════════
# Info
# ════════════════════════════════════════════════════════════════

version info:
	@echo ""
	@echo "$(BLUE)📋 Git 版本信息:$(NC)"
	@echo "  Commit: $$(git rev-parse HEAD 2>/dev/null || echo 'unknown')"
	@echo "  Short : $$(git rev-parse --short HEAD 2>/dev/null || echo 'unknown')"
	@echo "  Date  : $$(date -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || echo 'unknown')"
	@echo ""
	@echo "$(BLUE)🐳 镜像版本:$(NC)"
	@docker images --format "  {{.Repository}}:{{.Tag}} - {{.CreatedSince}}" | grep -E "easydo|docker-compose" || echo "  无镜像"

# 简短别名
b = build
u = up
d = down
c = clean
p = prune
s = status
l = logs
h = help
