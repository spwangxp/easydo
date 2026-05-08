FROM golang:1.21-alpine AS builder

# Install git for go modules
RUN apk add --no-cache git

WORKDIR /app

ENV GOPROXY=https://goproxy.cn,direct

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Tidy up dependencies (ensure go.sum is up to date)
RUN go mod tidy

# Build the agent
# Get git commit info for versioning
ARG GIT_COMMIT=unknown
ARG GIT_COMMIT_SHORT=unknown
ARG GIT_DATE=unknown
RUN set -eu; \
  resolved_commit="${GIT_COMMIT}"; \
  if [ -z "${resolved_commit}" ] || [ "${resolved_commit}" = "unknown" ]; then \
    resolved_commit="$(git rev-parse HEAD 2>/dev/null || true)"; \
  fi; \
  if [ -z "${resolved_commit}" ] || [ "${resolved_commit}" = "unknown" ]; then \
    resolved_commit="$(find . -type f -not -path './.git/*' -print | LC_ALL=C sort | xargs sha256sum | sha256sum | awk '{print $1}')"; \
  fi; \
  resolved_short="${GIT_COMMIT_SHORT}"; \
  if [ -z "${resolved_short}" ] || [ "${resolved_short}" = "unknown" ]; then \
    resolved_short="$(printf '%.12s' "${resolved_commit}")"; \
  fi; \
  resolved_date="${GIT_DATE}"; \
  if [ -z "${resolved_date}" ] || [ "${resolved_date}" = "unknown" ]; then \
    resolved_date="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"; \
  fi; \
  CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo \
    -ldflags "-X easydo-agent/internal/version.Commit=${resolved_commit} -X easydo-agent/internal/version.CommitShort=${resolved_short} -X easydo-agent/internal/version.Date=${resolved_date}" \
    -o easydo-agent ./cmd/main.go

FROM moby/buildkit:rootless AS buildkit
FROM alpine/helm:4 AS helm

# Production image
FROM alpine:3.19

# Create non-root user
RUN addgroup -g 1000 appgroup && \
    adduser -u 1000 -G appgroup -s /bin/sh -D appuser

RUN apk add --no-cache git docker-cli openssh-client-default sshpass kubectl runc python3 shadow-subids util-linux && \
    echo 'appuser:100000:65536' > /etc/subuid && \
    echo 'appuser:100000:65536' > /etc/subgid

COPY --from=buildkit /usr/bin/buildctl /usr/local/bin/buildctl
COPY --from=buildkit /usr/bin/buildkitd /usr/local/bin/buildkitd
COPY --from=buildkit /usr/bin/buildkit-qemu-* /usr/local/bin/
COPY --from=buildkit /usr/bin/rootlesskit /usr/local/bin/rootlesskit
COPY --from=helm /usr/bin/helm /usr/local/bin/helm

# Create directories for agent data
RUN mkdir -p /data/agent && \
    mkdir -p /home/appuser/.local/share/buildkit /home/appuser/.config/buildkit && \
    chown -R appuser:appgroup /data/agent

# Copy binary from builder
COPY --from=builder /app/easydo-agent /usr/local/bin/easydo-agent

# Copy configuration
COPY --from=builder /app/config.yaml /etc/easydo-agent/config.yaml

WORKDIR /data/agent

USER root

# Expose health check port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Run the agent
ENTRYPOINT ["easydo-agent"]
CMD ["--config=/etc/easydo-agent/config.yaml"]
