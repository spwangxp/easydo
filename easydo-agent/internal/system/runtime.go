package system

import (
	"io/fs"
	"os"
	"os/exec"
	"sort"
	"strings"
)

const (
	ExecutionModeHost      = "host"
	ExecutionModeContainer = "container"

	BuildBackendHostRuntime      = "host-runtime"
	BuildBackendEmbeddedBuildkit = "embedded-buildkit"
)

type RuntimeCapabilities struct {
	ExecutionMode         string   `json:"execution_mode"`
	PreferredBuildBackend string   `json:"preferred_build_backend"`
	PrimaryRuntime        string   `json:"primary_runtime"`
	AvailableRuntimes     []string `json:"available_runtimes,omitempty"`
	AvailableBuilders     []string `json:"available_builders,omitempty"`
	DockerSocketAvailable bool     `json:"docker_socket_available"`
}

type probeDependencies struct {
	lookPath func(string) (string, error)
	stat     func(string) (fs.FileInfo, error)
	getenv   func(string) string
}

func defaultProbeDependencies() probeDependencies {
	return probeDependencies{
		lookPath: exec.LookPath,
		stat:     os.Stat,
		getenv:   os.Getenv,
	}
}

func ProbeRuntimeCapabilities() RuntimeCapabilities {
	return probeRuntimeCapabilities(defaultProbeDependencies())
}

func probeRuntimeCapabilities(deps probeDependencies) RuntimeCapabilities {
	capabilities := RuntimeCapabilities{
		ExecutionMode: ExecutionModeHost,
	}

	runtimeCandidates := []string{"docker", "podman", "nerdctl", "ctr"}
	builderCandidates := []string{"docker", "podman", "nerdctl", "buildctl", "buildkitd"}

	for _, name := range runtimeCandidates {
		if hasExecutable(deps, name) {
			capabilities.AvailableRuntimes = append(capabilities.AvailableRuntimes, name)
		}
	}
	for _, name := range builderCandidates {
		if hasExecutable(deps, name) {
			capabilities.AvailableBuilders = append(capabilities.AvailableBuilders, name)
		}
	}

	capabilities.DockerSocketAvailable = fileExists(deps, "/var/run/docker.sock")
	if isContainerized(deps) {
		capabilities.ExecutionMode = ExecutionModeContainer
	}
	capabilities.PrimaryRuntime = pickPrimaryRuntime(capabilities.AvailableRuntimes)
	capabilities.PreferredBuildBackend = chooseBuildBackend(capabilities)

	sort.Strings(capabilities.AvailableRuntimes)
	sort.Strings(capabilities.AvailableBuilders)
	return capabilities
}

func chooseBuildBackend(capabilities RuntimeCapabilities) string {
	if capabilities.ExecutionMode == ExecutionModeContainer {
		return BuildBackendEmbeddedBuildkit
	}
	if capabilities.PrimaryRuntime != "" {
		return BuildBackendHostRuntime
	}
	return BuildBackendEmbeddedBuildkit
}

func pickPrimaryRuntime(runtimes []string) string {
	priority := []string{"docker", "podman", "nerdctl", "ctr"}
	for _, preferred := range priority {
		if containsString(runtimes, preferred) {
			return preferred
		}
	}
	return ""
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func hasExecutable(deps probeDependencies, name string) bool {
	if deps.lookPath == nil {
		return false
	}
	_, err := deps.lookPath(name)
	return err == nil
}

func fileExists(deps probeDependencies, path string) bool {
	if deps.stat == nil {
		return false
	}
	_, err := deps.stat(path)
	return err == nil
}

func isContainerized(deps probeDependencies) bool {
	if fileExists(deps, "/.dockerenv") || fileExists(deps, "/run/.containerenv") {
		return true
	}
	if deps.getenv != nil {
		if strings.TrimSpace(deps.getenv("KUBERNETES_SERVICE_HOST")) != "" {
			return true
		}
		if strings.TrimSpace(deps.getenv("container")) != "" {
			return true
		}
	}
	return false
}

func (c RuntimeCapabilities) Labels() []string {
	labels := []string{
		"execution=" + c.ExecutionMode,
		"build-backend=" + c.PreferredBuildBackend,
	}
	if c.PrimaryRuntime != "" {
		labels = append(labels, "primary-runtime="+c.PrimaryRuntime)
	}
	for _, runtimeName := range c.AvailableRuntimes {
		labels = append(labels, "runtime="+runtimeName)
	}
	for _, builderName := range c.AvailableBuilders {
		labels = append(labels, "builder="+builderName)
	}
	if c.DockerSocketAvailable {
		labels = append(labels, "docker-socket=available")
	}
	return labels
}
