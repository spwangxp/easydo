package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

type testRealtimeShutdowner struct {
	called chan struct{}
}

func (s *testRealtimeShutdowner) Shutdown(context.Context) error {
	close(s.called)
	return nil
}

func TestServeHTTPWithGracefulShutdownStopsRealtimeHandler(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	realtime := &testRealtimeShutdowner{called: make(chan struct{})}
	server := &http.Server{Handler: http.NewServeMux()}

	done := make(chan error, 1)
	go func() {
		done <- serveHTTPWithGracefulShutdown(ctx, server, listener, realtime, time.Second)
	}()

	cancel()

	select {
	case <-realtime.called:
	case <-time.After(time.Second):
		t.Fatal("realtime handler shutdown was not called")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveHTTPWithGracefulShutdown returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop after context cancellation")
	}
}
