package main

import (
	"context"
	"fmt"
	"io"
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

func shouldShowHelp(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}

func writeHelp(w io.Writer) {
	_, _ = fmt.Fprintln(w, "EasyDo Agent")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintln(w, "  easydo-agent [--help|-h]")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Configuration file search order:")
	_, _ = fmt.Fprintln(w, "  1. ./config.yaml")
	_, _ = fmt.Fprintln(w, "  2. /data/agent/config.yaml")
	_, _ = fmt.Fprintln(w, "  3. /etc/easydo-agent/config.yaml")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Agent config fields:")
	_, _ = fmt.Fprintln(w, "  agent.name           Optional agent name")
	_, _ = fmt.Fprintln(w, "  agent.token_file     Token persistence file path")
	_, _ = fmt.Fprintln(w, "  agent.workspace_id   Optional workspace ID")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Registration behavior:")
	_, _ = fmt.Fprintln(w, "  - Omit workspace_id or set it to 0 to register as 平台型")
	_, _ = fmt.Fprintln(w, "  - Set workspace_id to register as 工作空间私有 for that workspace")
	_, _ = fmt.Fprintln(w, "  - Re-registration with an approved token keeps the server-side scope unchanged")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Environment overrides:")
	_, _ = fmt.Fprintln(w, "  EASYDO_SERVER_URL")
	_, _ = fmt.Fprintln(w, "  AGENT_SERVER_PORT")
	_, _ = fmt.Fprintln(w, "  AGENT_NAME")
	_, _ = fmt.Fprintln(w, "  AGENT_TOKEN_FILE")
	_, _ = fmt.Fprintln(w, "  AGENT_WORKSPACE_ID")
	_, _ = fmt.Fprintln(w, "  AGENT_HEARTBEAT_INTERVAL")
	_, _ = fmt.Fprintln(w, "  AGENT_POLL_INTERVAL")
	_, _ = fmt.Fprintln(w, "  AGENT_LOG_LEVEL")
}

func loadConfigOrExit(log *logrus.Logger) *config.Config {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}
	return cfg
}

func main() {
	if shouldShowHelp(os.Args[1:]) {
		writeHelp(os.Stdout)
		return
	}

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

	cfg := loadConfigOrExit(log)

	// Debug: log server URL from config and environment
	fmt.Printf("[DEBUG] Before config load...\n")
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
