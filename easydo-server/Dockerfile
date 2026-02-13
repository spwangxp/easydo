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
ENV GIT_COMMIT=${GIT_COMMIT}
ENV GIT_COMMIT_SHORT=${GIT_COMMIT_SHORT}
ENV GIT_DATE=${GIT_DATE}

RUN go mod tidy && CGO_ENABLED=0 go build -a -installsuffix cgo \
  -ldflags "-X easydo-server/pkg/version.Commit=${GIT_COMMIT} -X easydo-server/pkg/version.CommitShort=${GIT_COMMIT_SHORT} -X easydo-server/pkg/version.Date=${GIT_DATE}" \
  -o main .

FROM alpine:3.18

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories && apk --no-cache add ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/main .
COPY --from=builder /app/config.yaml .

EXPOSE 8080

CMD ["./main"]
