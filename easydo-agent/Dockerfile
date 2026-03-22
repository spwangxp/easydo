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
ENV GIT_COMMIT=${GIT_COMMIT}
ENV GIT_COMMIT_SHORT=${GIT_COMMIT_SHORT}
ENV GIT_DATE=${GIT_DATE}

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo \
  -ldflags "-X easydo-agent/internal/version.Commit=${GIT_COMMIT} -X easydo-agent/internal/version.CommitShort=${GIT_COMMIT_SHORT} -X easydo-agent/internal/version.Date=${GIT_DATE}" \
  -o easydo-agent ./cmd/main.go

FROM moby/buildkit:rootless AS buildkit

# Production image
FROM alpine:3.19

# Create non-root user
RUN addgroup -g 1000 appgroup && \
    adduser -u 1000 -G appgroup -s /bin/sh -D appuser

RUN apk add --no-cache git docker-cli openssh-client-default sshpass kubectl runc

COPY --from=buildkit /usr/bin/buildctl /usr/local/bin/buildctl
COPY --from=buildkit /usr/bin/buildkitd /usr/local/bin/buildkitd
COPY --from=buildkit /usr/bin/rootlesskit /usr/local/bin/rootlesskit

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
