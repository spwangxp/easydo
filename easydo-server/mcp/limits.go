package mcp

import "time"

const (
	DefaultPageLimit                  = 20
	MaxPageLimit                      = 100
	MaxBodySizeBytes                  = 1 << 20
	MaxOutputStringChars              = 4096
	MaxOutputListItems                = 100
	MaxJSONDepth                      = 6
	StreamableHTTPRequestTimeout      = 30 * time.Second
	StreamableHTTPMaxInFlightPerUser  = 8
	StreamableHTTPMaxInFlightPerIP    = 32
	SSEIdleTimeout                    = 2 * time.Minute
	SSEMaxSessionLifetime             = 30 * time.Minute
	SSEMaxOpenConnectionsPerUser      = 4
	SSEMaxOpenConnectionsPerIP        = 16
	SSESessionCleanupInterval         = 30 * time.Second
)

func NormalizePagination(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = DefaultPageLimit
	}
	if limit > MaxPageLimit {
		limit = MaxPageLimit
	}
	return page, limit
}
