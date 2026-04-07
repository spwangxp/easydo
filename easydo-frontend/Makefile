# ════════════════════════════════════════════════════════════════
# EasyDo Frontend Makefile
# 简化版 - 核心操作委托给根目录 Makefile
# ════════════════════════════════════════════════════════════════

# 颜色定义
GREEN = \033[0;32m
YELLOW = \033[1;33m
BLUE = \033[0;34m
NC = \033[0m

# 项目配置
APP_NAME = frontend
COMPOSE_FILE = ../deploy/docker-compose/docker-compose.yml
DC = docker-compose -f $(COMPOSE_FILE)

# Git 版本信息
GIT_COMMIT = $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
GIT_COMMIT_SHORT = $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GIT_DATE = $(shell date -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || echo "unknown")

.PHONY: all help build run stop logs status clean debug

all help:
	@echo ""
	@echo "$(BLUE)Frontend 操作:$(NC)"
	@echo "   make build   - Docker 编译镜像"
	@echo "   make run     - Docker 运行"
	@echo "   make stop    - Docker 停止"
	@echo "   make logs    - 查看日志"
	@echo "   make status  - 查看状态"
	@echo "   make debug   - 调试部署"
	@echo "   make clean   - 清理产物"
	@echo ""
	@echo "快捷命令请使用根目录: make build-front, make logs-front 等"
	@echo ""

# ════════════════════════════════════════════════════════════════
# Docker 编译 (带版本信息)
# ════════════════════════════════════════════════════════════════

build:
	@echo "$(BLUE)[Frontend]$(NC) Docker 编译中..."
	@echo "[Frontend] Version: $(GIT_COMMIT_SHORT) @ $(GIT_DATE)"
	@$(DC) build frontend \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		--build-arg GIT_COMMIT_SHORT=$(GIT_COMMIT_SHORT) \
		--build-arg GIT_DATE=$(GIT_DATE)
	@echo "$(GREEN)✅ Frontend 镜像编译完成$(NC)"

# ════════════════════════════════════════════════════════════════
# Docker 运行
# ════════════════════════════════════════════════════════════════

run:
	@echo "$(BLUE)[Frontend]$(NC) 启动服务..."
	@$(DC) up -d frontend
	@echo "$(GREEN)✅ Frontend 已启动$(NC)"
	@echo "   访问: http://localhost:8088"

# ════════════════════════════════════════════════════════════════
# Docker 停止
# ════════════════════════════════════════════════════════════════

stop:
	@echo "$(BLUE)[Frontend]$(NC) 停止服务..."
	@$(DC) stop frontend
	@echo "$(GREEN)✅ Frontend 已停止$(NC)"

# ════════════════════════════════════════════════════════════════
# 查看日志
# ════════════════════════════════════════════════════════════════

logs:
	@$(DC) logs -f frontend

# ════════════════════════════════════════════════════════════════
# 查看状态
# ════════════════════════════════════════════════════════════════

status:
	@echo "$(BLUE)[Frontend]$(NC) 状态:"
	@$(DC) ps frontend

# ════════════════════════════════════════════════════════════════
# 调试部署 (删除镜像 + 重新编译 + 启动)
# ════════════════════════════════════════════════════════════════

debug: stop
	@echo "$(RED)[Frontend]$(NC) 调试部署..."
	@docker rmi easydo-frontend easydo-frontend:latest 2>/dev/null || true
	@echo "[Frontend] Version: $(GIT_COMMIT_SHORT) @ $(GIT_DATE)"
	@$(DC) build frontend \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		--build-arg GIT_COMMIT_SHORT=$(GIT_COMMIT_SHORT) \
		--build-arg GIT_DATE=$(GIT_DATE)
	@$(DC) up -d frontend
	@echo "$(GREEN)✅ Frontend 调试部署完成$(NC)"
	@echo "   访问: http://localhost:8088"

# ════════════════════════════════════════════════════════════════
# 清理
# ════════════════════════════════════════════════════════════════

clean:
	@echo "$(YELLOW)[Frontend]$(NC) 清理构建产物..."
	@rm -rf dist
	@echo "$(GREEN)✅ Frontend 产物已清理$(NC)"
