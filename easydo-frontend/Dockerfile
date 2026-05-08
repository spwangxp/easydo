# 阶段1: 构建前端
FROM node:20-alpine AS frontend-builder

# 获取 git commit 信息作为构建参数
ARG GIT_COMMIT=unknown
ARG GIT_COMMIT_SHORT=unknown
ARG GIT_DATE=unknown

WORKDIR /app/easydo-frontend

COPY package*.json ./
RUN npm install

COPY . ./
RUN set -eu; \
    resolved_commit="${GIT_COMMIT}"; \
    if [ -z "${resolved_commit}" ] || [ "${resolved_commit}" = "unknown" ]; then \
      resolved_commit="$(git rev-parse HEAD 2>/dev/null || true)"; \
    fi; \
    if [ -z "${resolved_commit}" ] || [ "${resolved_commit}" = "unknown" ]; then \
      resolved_commit="$(find . -type f -not -path './.git/*' -not -path './node_modules/*' -print | LC_ALL=C sort | xargs sha256sum | sha256sum | awk '{print $1}')"; \
    fi; \
    resolved_short="${GIT_COMMIT_SHORT}"; \
    if [ -z "${resolved_short}" ] || [ "${resolved_short}" = "unknown" ]; then \
      resolved_short="$(printf '%.12s' "${resolved_commit}")"; \
    fi; \
    resolved_date="${GIT_DATE}"; \
    if [ -z "${resolved_date}" ] || [ "${resolved_date}" = "unknown" ]; then \
      resolved_date="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"; \
    fi; \
    GIT_COMMIT="${resolved_commit}" GIT_COMMIT_SHORT="${resolved_short}" GIT_DATE="${resolved_date}" npm run build

# 阶段2: 生产环境
FROM nginx:alpine

# 复制 git info 环境变量
ARG GIT_COMMIT=unknown
ARG GIT_COMMIT_SHORT=unknown
ARG GIT_DATE=unknown
ENV GIT_COMMIT=${GIT_COMMIT}
ENV GIT_COMMIT_SHORT=${GIT_COMMIT_SHORT}
ENV GIT_DATE=${GIT_DATE}

COPY --from=frontend-builder /app/easydo-frontend/dist /usr/share/nginx/html
COPY nginx/default.conf /etc/nginx/conf.d/default.conf

# 复制版本信息打印脚本到 nginx 的 docker-entrypoint.d 目录
COPY docker-entrypoint.d/05-version-info.sh /docker-entrypoint.d/05-version-info.sh
RUN chmod +x /docker-entrypoint.d/05-version-info.sh

EXPOSE 80
