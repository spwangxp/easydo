# ════════════════════════════════════════════════════════════════
# EasyDo Server Makefile
# 简化版 - 核心操作委托给根目录 Makefile
# ════════════════════════════════════════════════════════════════

# 颜色定义
GREEN = \033[0;32m
YELLOW = \033[1;33m
BLUE = \033[0;34m
NC = \033[0m

# 项目配置
APP_NAME = server
BINARY_NAME = server
COMPOSE_FILE = ../deploy/docker-compose/docker-compose.yml
DC = docker-compose -f $(COMPOSE_FILE)

# Git 版本信息
GIT_COMMIT = $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
GIT_COMMIT_SHORT = $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GIT_DATE = $(shell date -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || echo "unknown")

.PHONY: all help build run stop logs status clean debug test

all help:
	@echo ""
	@echo "$(BLUE)Server 操作:$(NC)"
	@echo "   make build   - Docker 编译镜像"
	@echo "   make run     - Docker 运行"
	@echo "   make stop    - Docker 停止"
	@echo "   make logs    - 查看日志"
	@echo "   make status  - 查看状态"
	@echo "   make debug   - 调试部署"
	@echo "   make clean   - 清理产物"
	@echo "   make test    - 运行测试"
	@echo ""
	@echo "快捷命令请使用根目录: make build-srv, make logs-srv 等"
	@echo ""

# ════════════════════════════════════════════════════════════════
# Docker 编译 (带版本信息)
# ════════════════════════════════════════════════════════════════

build:
	@echo "$(BLUE)[Server]$(NC) Docker 编译中..."
	@echo "[Server] Version: $(GIT_COMMIT_SHORT) @ $(GIT_DATE)"
	@$(DC) build server \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		--build-arg GIT_COMMIT_SHORT=$(GIT_COMMIT_SHORT) \
		--build-arg GIT_DATE=$(GIT_DATE)
	@echo "$(GREEN)✅ Server 镜像编译完成$(NC)"

# ════════════════════════════════════════════════════════════════
# Docker 运行
# ════════════════════════════════════════════════════════════════

run:
	@echo "$(BLUE)[Server]$(NC) 启动服务..."
	@$(DC) up -d server
	@echo "$(GREEN)✅ Server 已启动$(NC)"
	@echo "   API: http://localhost:8080"

# ════════════════════════════════════════════════════════════════
# Docker 停止
# ════════════════════════════════════════════════════════════════

stop:
	@echo "$(BLUE)[Server]$(NC) 停止服务..."
	@$(DC) stop server
	@echo "$(GREEN)✅ Server 已停止$(NC)"

# ════════════════════════════════════════════════════════════════
# 查看日志
# ════════════════════════════════════════════════════════════════

logs:
	@$(DC) logs -f server

# ════════════════════════════════════════════════════════════════
# 查看状态
# ════════════════════════════════════════════════════════════════

status:
	@echo "$(BLUE)[Server]$(NC) 状态:"
	@$(DC) ps server

# ════════════════════════════════════════════════════════════════
# 调试部署 (删除镜像 + 重新编译 + 启动)
# ════════════════════════════════════════════════════════════════

debug: stop
	@echo "$(RED)[Server]$(NC) 调试部署..."
	@docker rmi easydo-server easydo-server:latest 2>/dev/null || true
	@echo "[Server] Version: $(GIT_COMMIT_SHORT) @ $(GIT_DATE)"
	@$(DC) build server \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		--build-arg GIT_COMMIT_SHORT=$(GIT_COMMIT_SHORT) \
		--build-arg GIT_DATE=$(GIT_DATE)
	@$(DC) up -d server
	@echo "$(GREEN)✅ Server 调试部署完成$(NC)"
	@echo "   API: http://localhost:8080"

# ════════════════════════════════════════════════════════════════
# 清理
# ════════════════════════════════════════════════════════════════

clean:
	@echo "$(YELLOW)[Server]$(NC) 清理构建产物..."
	@rm -f $(BINARY_NAME)
	@echo "$(GREEN)✅ Server 产物已清理$(NC)"

# ════════════════════════════════════════════════════════════════
# 测试
# ════════════════════════════════════════════════════════════════

test:
	@echo "$(BLUE)[Server]$(NC) 运行测试..."
	@go test -v ./...
	@echo "$(GREEN)✅ Server 测试完成$(NC)"
