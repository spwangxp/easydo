FROM golang:1.21-alpine AS builder

# Install git for version info
RUN apk add --no-cache git

ENV GOPROXY=https://goproxy.cn,direct

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

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
  go mod tidy; \
  CGO_ENABLED=0 go build -a -installsuffix cgo \
    -ldflags "-X easydo-server/pkg/version.Commit=${resolved_commit} -X easydo-server/pkg/version.CommitShort=${resolved_short} -X easydo-server/pkg/version.Date=${resolved_date}" \
    -o main .

FROM alpine:3.18

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories && apk --no-cache add ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/main .
COPY --from=builder /app/config.yaml .

EXPOSE 8080

CMD ["./main"]
