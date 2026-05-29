package mcp

import (
	"strings"
	"sync"
	"time"

	"easydo-server/internal/handlers"
	"easydo-server/internal/services"
	"easydo-server/pkg/utils"

	"gorm.io/gorm"
)

type ServerOptions struct {
	DB             *gorm.DB
	Registry       *Registry
	AuditRecorder  AuditRecorder
	AllowedOrigins []string
	Now            func() time.Time
	ServerID       string

	MaxBodySizeBytes             int64
	RequestTimeout               time.Duration
	MaxInFlightPerUser           int
	MaxInFlightPerIP             int
	SSEIdleTimeout               time.Duration
	SSEMaxSessionLifetime        time.Duration
	SSECleanupInterval           time.Duration
	MaxSSEOpenConnectionsPerUser int
	MaxSSEOpenConnectionsPerIP   int
}

type Server struct {
	db             *gorm.DB
	registry       *Registry
	auditRecorder  AuditRecorder
	allowedOrigins map[string]struct{}
	maxBodySize    int64
	requestTimeout time.Duration
	userLimiter    *inFlightLimiter
	ipLimiter      *inFlightLimiter
	now            func() time.Time
	serverID       string
	sseSessions    *sseSessionManager
	sseRelayOnce   sync.Once
}

func NewServer(opts ServerOptions) *Server {
	registry := opts.Registry
	if registry == nil {
		registry = NewRegistry()
		registerDefaultTools(registry, opts.DB)
	}
	auditRecorder := opts.AuditRecorder
	if auditRecorder == nil {
		auditRecorder = NewGormAuditRecorder(opts.DB)
	}
	maxBodySize := opts.MaxBodySizeBytes
	if maxBodySize <= 0 {
		maxBodySize = MaxBodySizeBytes
	}
	requestTimeout := StreamableHTTPRequestTimeout
	if opts.RequestTimeout > 0 {
		requestTimeout = opts.RequestTimeout
	}
	maxPerUser := opts.MaxInFlightPerUser
	if maxPerUser <= 0 {
		maxPerUser = StreamableHTTPMaxInFlightPerUser
	}
	maxPerIP := opts.MaxInFlightPerIP
	if maxPerIP <= 0 {
		maxPerIP = StreamableHTTPMaxInFlightPerIP
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	sseIdleTimeout := opts.SSEIdleTimeout
	if sseIdleTimeout <= 0 {
		sseIdleTimeout = SSEIdleTimeout
	}
	sseMaxSessionLifetime := opts.SSEMaxSessionLifetime
	if sseMaxSessionLifetime <= 0 {
		sseMaxSessionLifetime = SSEMaxSessionLifetime
	}
	sseCleanupInterval := opts.SSECleanupInterval
	if sseCleanupInterval <= 0 {
		sseCleanupInterval = SSESessionCleanupInterval
	}
	maxSSEPerUser := opts.MaxSSEOpenConnectionsPerUser
	if maxSSEPerUser <= 0 {
		maxSSEPerUser = SSEMaxOpenConnectionsPerUser
	}
	maxSSEPerIP := opts.MaxSSEOpenConnectionsPerIP
	if maxSSEPerIP <= 0 {
		maxSSEPerIP = SSEMaxOpenConnectionsPerIP
	}
	serverID := strings.TrimSpace(opts.ServerID)
	if serverID == "" {
		serverID = utils.ServerID()
	}
	allowedOrigins := make(map[string]struct{}, len(opts.AllowedOrigins))
	for _, origin := range opts.AllowedOrigins {
		if origin != "" {
			allowedOrigins[origin] = struct{}{}
		}
	}
	server := &Server{
		db:             opts.DB,
		registry:       registry,
		auditRecorder:  auditRecorder,
		allowedOrigins: allowedOrigins,
		maxBodySize:    maxBodySize,
		requestTimeout: requestTimeout,
		userLimiter:    newInFlightLimiter(maxPerUser),
		ipLimiter:      newInFlightLimiter(maxPerIP),
		now:            now,
		serverID:       serverID,
		sseSessions: newSSESessionManager(sseSessionManagerOptions{
			now:             now,
			idleTimeout:     sseIdleTimeout,
			maxLifetime:     sseMaxSessionLifetime,
			cleanupInterval: sseCleanupInterval,
			userLimiter:     newInFlightLimiter(maxSSEPerUser),
			ipLimiter:       newInFlightLimiter(maxSSEPerIP),
		}),
	}
	server.startSSERelayConsumer()
	return server
}

func registerDefaultTools(registry *Registry, db *gorm.DB) {
	_ = RegisterWorkspaceTools(registry, &services.WorkspaceUseCase{DB: db})
	_ = RegisterPipelineTools(registry, &services.PipelineQueryUseCase{DB: db})
	_ = RegisterPipelineOperationTools(registry, handlers.NewPipelineOperationService(db))
	_ = RegisterResourceTools(registry, &services.ResourceUseCase{DB: db})
}
