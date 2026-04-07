# 阶段1: 构建前端
FROM node:20-alpine AS frontend-builder

# 获取 git commit 信息作为构建参数
ARG GIT_COMMIT=unknown
ARG GIT_COMMIT_SHORT=unknown
ARG GIT_DATE=unknown
ENV GIT_COMMIT=${GIT_COMMIT}
ENV GIT_COMMIT_SHORT=${GIT_COMMIT_SHORT}
ENV GIT_DATE=${GIT_DATE}

WORKDIR /app/easydo-frontend

COPY package*.json ./
RUN npm install

COPY . ./
RUN npm run build

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
