package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"easydo-agent/internal/agent"
	"easydo-agent/internal/config"
	"easydo-agent/internal/server"
	"easydo-agent/internal/system"
	"easydo-agent/internal/version"
	"github.com/sirupsen/logrus"
)

func main() {
	// Initialize logger
	log := logrus.New()
	log.SetLevel(logrus.InfoLevel)
	log.SetOutput(os.Stdout)
	log.SetFormatter(&logrus.TextFormatter{
		DisableColors: false,
		FullTimestamp: true,
	})

	fmt.Println("[INFO] " + version.Info())
	fmt.Println()

	fmt.Println("[DEBUG] Starting EasyDo Agent...")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Debug: log server URL from config and environment
	fmt.Printf("[DEBUG] Before config load...\n")
	cfg, err = config.Load()
	fmt.Printf("[DEBUG] Config loaded: %s, env=%s\n", cfg.ServerURL, os.Getenv("EASYDO_SERVER_URL"))

	// Set log level from config
	logLevel, err := logrus.ParseLevel(cfg.Logging.Level)
	if err != nil {
		log.Warnf("Invalid log level: %s, using default", cfg.Logging.Level)
	} else {
		log.SetLevel(logLevel)
	}

	// Collect system information
	sysInfo, err := system.Collect()
	if err != nil {
		log.Warnf("Failed to collect system info: %v", err)
	}

	log.Infof("System info: hostname=%s, ip=%s, os=%s, arch=%s, cpu=%d, memory=%d",
		sysInfo.Hostname, sysInfo.IPAddress, sysInfo.OS, sysInfo.Arch,
		sysInfo.CPUCores, sysInfo.MemoryTotal)

	// Determine agent name
	agentName := cfg.Agent.Name
	if agentName == "" {
		agentName = sysInfo.Hostname
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start health check server first (required for Docker health check)
	healthServer := server.NewHealthServer(cfg, log)
	if err := healthServer.Start(ctx); err != nil {
		log.Fatalf("Failed to start health check server: %v", err)
	}

	// Create agent client
	agentClient := agent.NewClient(cfg, sysInfo, agentName, log)

	// Start agent
	if err := agentClient.Start(ctx); err != nil {
		log.Fatalf("Failed to start agent: %v", err)
	}

	log.Infof("Agent %s started successfully", agentName)

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Info("Shutting down agent...")

	// Cancel context to stop all background processes
	cancel()

	// Stop health check server first
	if err := healthServer.Stop(ctx); err != nil {
		log.Warnf("Error during health server shutdown: %v", err)
	}

	// Give graceful shutdown time for agent
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := agentClient.Shutdown(shutdownCtx); err != nil {
		log.Warnf("Error during agent shutdown: %v", err)
	}

	log.Info("Agent stopped")
}
