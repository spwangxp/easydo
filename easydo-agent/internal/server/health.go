package server

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"easydo-agent/internal/config"
	"github.com/sirupsen/logrus"
)

// HealthServer provides a lightweight HTTP server for Docker health checks
type HealthServer struct {
	cfg       *config.Config
	log       *logrus.Logger
	httpServer *http.Server
	httpClient *http.Client
	listener  chan struct{}
	mu        sync.RWMutex
	running   bool
	stopChan  chan struct{}
}

// NewHealthServer creates a new health check server
func NewHealthServer(cfg *config.Config, log *logrus.Logger) *HealthServer {
	return &HealthServer{
		cfg:      cfg,
		log:      log,
		httpClient: &http.Client{Timeout: 2 * time.Second},
		stopChan: make(chan struct{}),
		listener: make(chan struct{}, 1),
	}
}

// Start starts the health check server
func (s *HealthServer) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.mu.Unlock()

	// Create HTTP server with minimal configuration
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.healthHandler)
	mux.HandleFunc("/ready", s.readyHandler)

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.cfg.GetServerPort()),
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	// Start server in goroutine
	port := s.cfg.GetServerPort()
	go func() {
		s.log.Infof("Health check server starting on port %d", port)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.log.Warnf("Health server error: %v", err)
		}
		select {
		case s.listener <- struct{}{}:
		default:
		}
	}()

	startTimeout := time.After(5 * time.Second)
	listenerReady := make(chan struct{})
	
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-startTimeout:
				return
			default:
				req, _ := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/health", port), nil)
				resp, err := s.httpClient.Do(req)
				if err == nil {
					resp.Body.Close()
					if resp.StatusCode == http.StatusOK {
						close(listenerReady)
						return
					}
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-listenerReady:
		s.log.Infof("Health check server started successfully on port %d", port)
		return nil
	case <-startTimeout:
		s.log.Warnf("Health check server startup timeout, proceeding anyway")
		return nil
	}
}

// Stop stops the health check server
func (s *HealthServer) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	s.mu.Unlock()

	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		s.log.Warnf("Health server shutdown error: %v", err)
		return err
	}

	s.log.Info("Health check server stopped")
	return nil
}

// healthHandler handles /health endpoint requests
func (s *HealthServer) healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Return simple success response for Docker health check
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

// readyHandler handles /ready endpoint requests
func (s *HealthServer) readyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Return simple success response
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Ready"))
}
