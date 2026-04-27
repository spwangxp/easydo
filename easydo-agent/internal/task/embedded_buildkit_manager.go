package task

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"easydo-agent/internal/system"
	"github.com/sirupsen/logrus"
)

type processHandle struct {
	pid int
}

type EmbeddedBuildkitManager struct {
	log     *logrus.Logger
	baseDir string
	runtime system.RuntimeCapabilities

	mu              sync.Mutex
	env             map[string]string
	mirrorsKey      string
	process         processHandle
	startProcess    func(configPath, socketPath, stateDir, logPath string) (processHandle, error)
	waitUntilReady  func(socketPath string) error
	stopProcess     func(processHandle) error
}

func NewEmbeddedBuildkitManager(log *logrus.Logger, workspacePath string, runtime system.RuntimeCapabilities) *EmbeddedBuildkitManager {
	manager := &EmbeddedBuildkitManager{
		log:     log,
		baseDir: filepath.Join(workspacePath, ".easydo-buildkit", "shared"),
		runtime: runtime,
	}
	manager.startProcess = manager.startProcessImpl
	manager.waitUntilReady = manager.waitUntilReadyImpl
	manager.stopProcess = manager.stopProcessImpl
	return manager
}

func (m *EmbeddedBuildkitManager) EnsureRunning(mirrors []string) error {
	if m == nil || m.runtime.PreferredBuildBackend != system.BuildBackendEmbeddedBuildkit {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	mirrors = NormalizeDockerHubMirrors(mirrors)
	mirrorsKey := strings.Join(mirrors, "\n")
	if len(m.env) > 0 && m.mirrorsKey == mirrorsKey {
		return nil
	}
	if m.process.pid > 0 {
		if err := m.stopProcess(m.process); err != nil && m.log != nil {
			m.log.Warnf("failed to stop embedded buildkit process: %v", err)
		}
		m.process = processHandle{}
	}

	runtimeDir := m.baseDir
	socketPath := filepath.Join(runtimeDir, "run", "buildkitd.sock")
	stateDir := filepath.Join(runtimeDir, "state")
	configPath := filepath.Join(runtimeDir, "buildkitd.toml")
	dockerConfigDir := filepath.Join(runtimeDir, "docker")
	logPath := filepath.Join(runtimeDir, "buildkitd.log")
	for _, dir := range []string{runtimeDir, filepath.Dir(socketPath), stateDir, dockerConfigDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create buildkit runtime dir %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(configPath, []byte(buildEmbeddedBuildkitConfig(mirrors)), 0o644); err != nil {
		return fmt.Errorf("write buildkit config: %w", err)
	}
	proc, err := m.startProcess(configPath, socketPath, stateDir, logPath)
	if err != nil {
		return err
	}
	if err := m.waitUntilReady(socketPath); err != nil {
		_ = m.stopProcess(proc)
		return err
	}
	m.process = proc
	m.mirrorsKey = mirrorsKey
	m.env = map[string]string{
		"EASYDO_BUILDKIT_SOCKET_PATH":   socketPath,
		"EASYDO_BUILDKIT_STATE_DIR":     stateDir,
		"EASYDO_BUILDKIT_CONFIG_PATH":   configPath,
		"EASYDO_BUILDKIT_DOCKER_CONFIG": dockerConfigDir,
	}
	return nil
}

func (m *EmbeddedBuildkitManager) Env() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.env) == 0 {
		return nil
	}
	copied := make(map[string]string, len(m.env))
	for k, v := range m.env {
		copied[k] = v
	}
	return copied
}

func (m *EmbeddedBuildkitManager) Stop() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.process.pid == 0 {
		return nil
	}
	err := m.stopProcess(m.process)
	m.process = processHandle{}
	m.env = nil
	m.mirrorsKey = ""
	return err
}

func (m *EmbeddedBuildkitManager) startProcessImpl(configPath, socketPath, stateDir, logPath string) (processHandle, error) {
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return processHandle{}, fmt.Errorf("open buildkit log: %w", err)
	}
	cmd := exec.Command("buildkitd", "--config", configPath, "--addr", "unix://"+socketPath, "--root", stateDir)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return processHandle{}, fmt.Errorf("start buildkitd: %w", err)
	}
	_ = logFile.Close()
	return processHandle{pid: cmd.Process.Pid}, nil
}

func (m *EmbeddedBuildkitManager) waitUntilReadyImpl(socketPath string) error {
	for i := 0; i < 50; i++ {
		cmd := exec.Command("buildctl", "--addr", "unix://"+socketPath, "debug", "workers")
		if err := cmd.Run(); err == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("buildkitd did not become ready")
}

func (m *EmbeddedBuildkitManager) stopProcessImpl(proc processHandle) error {
	if proc.pid == 0 {
		return nil
	}
	if err := syscall.Kill(-proc.pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
		return err
	}
	return nil
}

func buildEmbeddedBuildkitConfig(mirrors []string) string {
	config := "[worker.oci]\n  networkMode = \"host\"\n"
	if len(mirrors) == 0 {
		return config
	}
	escapedMirrors := make([]string, 0, len(mirrors))
	for _, mirror := range mirrors {
		escapedMirrors = append(escapedMirrors, fmt.Sprintf("%q", mirror))
	}
	return config + fmt.Sprintf("\n[registry.\"docker.io\"]\n  mirrors = [%s]\n", strings.Join(escapedMirrors, ", "))
}
