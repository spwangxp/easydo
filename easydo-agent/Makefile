# ════════════════════════════════════════════════════════════════
# EasyDo Agent Makefile
# 简化版 - 核心操作委托给根目录 Makefile
# ════════════════════════════════════════════════════════════════

# 颜色定义
GREEN = \033[0;32m
YELLOW = \033[1;33m
BLUE = \033[0;34m
NC = \033[0m

# 项目配置
APP_NAME = agent
BINARY_NAME = easydo-agent
COMPOSE_FILE = ../docker-compose.yml
DC = docker-compose -f $(COMPOSE_FILE)

# Agent 配置
AGENT_ID ?= 1
SERVER_URL ?= http://localhost:8080
TOKEN_FILE ?= /data/agent/token
AGENT_NAME ?= easydo-agent
MODE ?= docker

# Git 版本信息
GIT_COMMIT = $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
GIT_COMMIT_SHORT = $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GIT_DATE = $(shell date -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || echo "unknown")

.PHONY: all help build run stop logs status clean debug test package

all help:
	@echo ""
	@echo "$(BLUE)Agent 操作:$(NC)"
	@echo "   make build        - Docker 编译镜像"
	@echo "   make run MODE=    - 运行 (docker/host)"
	@echo "   make stop MODE=   - 停止"
	@echo "   make logs         - 查看日志"
	@echo "   make status       - 查看状态"
	@echo "   make debug        - 调试部署"
	@echo "   make clean        - 清理产物"
	@echo "   make prune        - 清理镜像"
	@echo "   make package      - 打包发布"
	@echo "   make test         - 运行测试"
	@echo ""
	@echo "快捷命令请使用根目录: make build-agent, make logs-agent 等"
	@echo ""
	@echo "运行参数:"
	@echo "   MODE=docker make run   # Docker 模式 (默认)"
	@echo "   MODE=host make run     # Host 模式"

# ════════════════════════════════════════════════════════════════
# Docker 编译 (带版本信息)
# ════════════════════════════════════════════════════════════════

build:
	@echo "$(BLUE)[Agent]$(NC) Docker 编译中..."
	@echo "[Agent] Version: $(GIT_COMMIT_SHORT) @ $(GIT_DATE)"
	@$(DC) build agent \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		--build-arg GIT_COMMIT_SHORT=$(GIT_COMMIT_SHORT) \
		--build-arg GIT_DATE=$(GIT_DATE)
	@echo "$(GREEN)✅ Agent 镜像编译完成$(NC)"

# ════════════════════════════════════════════════════════════════
# Docker 运行
# ════════════════════════════════════════════════════════════════

run:
	@echo "$(BLUE)[Agent]$(NC) 启动服务..."
	@if [ "$(MODE)" = "host" ]; then \
		echo "$(YELLOW)[Agent]$(NC) Host 模式运行..."; \
		if [ ! -f "$(BINARY_NAME)" ]; then \
			echo "$(RED)[Agent]$(NC) 二进制文件不存在，请先运行 make build"; \
			exit 1; \
		fi; \
		EASYDO_SERVER_URL=$(SERVER_URL) \
		AGENT_NAME=$(AGENT_NAME) \
		AGENT_TOKEN_FILE=$(TOKEN_FILE) \
		./$(BINARY_NAME); \
	else \
		$(DC) up -d agent; \
	fi
	@echo "$(GREEN)✅ Agent 已启动$(NC)"

# ════════════════════════════════════════════════════════════════
# Docker 停止
# ════════════════════════════════════════════════════════════════

stop:
	@echo "$(BLUE)[Agent]$(NC) 停止服务..."
	@if [ "$(MODE)" = "host" ]; then \
		echo "$(YELLOW)[Agent]$(NC) Host 模式 (Ctrl+C 中断)"; \
	else \
		$(DC) stop agent; \
		echo "$(GREEN)✅ Agent 已停止$(NC)"; \
	fi

# ════════════════════════════════════════════════════════════════
# 查看日志
# ════════════════════════════════════════════════════════════════

logs:
	@$(DC) logs -f agent

# ════════════════════════════════════════════════════════════════
# 查看状态
# ════════════════════════════════════════════════════════════════

status:
	@echo "$(BLUE)[Agent]$(NC) 状态:"
	@$(DC) ps agent 2>/dev/null || echo "   服务未运行"

# ════════════════════════════════════════════════════════════════
# 调试部署 (删除镜像 + 重新编译 + 启动)
# ════════════════════════════════════════════════════════════════

debug: stop
	@echo "$(RED)[Agent]$(NC) 调试部署..."
	@docker rmi easydo3-agent easydo-agent:latest 2>/dev/null || true
	@echo "[Agent] Version: $(GIT_COMMIT_SHORT) @ $(GIT_DATE)"
	@$(DC) build agent \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		--build-arg GIT_COMMIT_SHORT=$(GIT_COMMIT_SHORT) \
		--build-arg GIT_DATE=$(GIT_DATE)
	@$(DC) up -d agent
	@echo "$(GREEN)✅ Agent 调试部署完成$(NC)"

# ════════════════════════════════════════════════════════════════
# 清理
# ════════════════════════════════════════════════════════════════

clean:
	@echo "$(YELLOW)[Agent]$(NC) 清理构建产物..."
	@rm -f $(BINARY_NAME)
	@$(DC) down agent 2>/dev/null || true
	@echo "$(GREEN)✅ Agent 产物已清理$(NC)"

# ════════════════════════════════════════════════════════════════
# 清理镜像
# ════════════════════════════════════════════════════════════════

prune: clean
	@echo "$(YELLOW)[Agent]$(NC) 清理镜像..."
	@docker rmi easydo3-agent easydo-agent:latest 2>/dev/null || true
	@echo "$(GREEN)✅ Agent 镜像已清理$(NC)"

# ════════════════════════════════════════════════════════════════
# 打包发布
# ════════════════════════════════════════════════════════════════

package:
	@echo "$(BLUE)[Agent]$(NC) 打包中..."
	@mkdir -p package
	@echo "[Agent] Version: $(GIT_COMMIT_SHORT) @ $(GIT_DATE)"
	@docker build -t easydo-agent:latest \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		--build-arg GIT_COMMIT_SHORT=$(GIT_COMMIT_SHORT) \
		--build-arg GIT_DATE=$(GIT_DATE) \
		.
	@docker save easydo-agent:latest | gzip > package/easydo-agent-$(GIT_COMMIT_SHORT).tar.gz
	@echo "$(GREEN)✅ Agent 已打包: package/easydo-agent-$(GIT_COMMIT_SHORT).tar.gz$(NC)"
	@ls -lh package/

# ════════════════════════════════════════════════════════════════
# 测试
# ════════════════════════════════════════════════════════════════

test:
	@echo "$(BLUE)[Agent]$(NC) 运行测试..."
	@docker run --rm -v $(PWD):/app -w /app golang:1.21-alpine go test -v ./...
	@echo "$(GREEN)✅ Agent 测试完成$(NC)"
