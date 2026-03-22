package system

import (
	"errors"
	"io/fs"
	"testing"
	"time"
)

type fakeFileInfo struct{}

func (fakeFileInfo) Name() string       { return "fake" }
func (fakeFileInfo) Size() int64        { return 0 }
func (fakeFileInfo) Mode() fs.FileMode  { return 0 }
func (fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (fakeFileInfo) IsDir() bool        { return false }
func (fakeFileInfo) Sys() interface{}   { return nil }

func TestProbeRuntimeCapabilities_HostPrefersLocalRuntime(t *testing.T) {
	deps := probeDependencies{
		lookPath: func(name string) (string, error) {
			switch name {
			case "docker":
				return "/usr/bin/docker", nil
			case "podman":
				return "/usr/bin/podman", nil
			default:
				return "", errors.New("not found")
			}
		},
		stat: func(path string) (fs.FileInfo, error) {
			return nil, errors.New("missing")
		},
		getenv: func(key string) string { return "" },
	}

	capabilities := probeRuntimeCapabilities(deps)
	if capabilities.ExecutionMode != ExecutionModeHost {
		t.Fatalf("execution mode=%s, want %s", capabilities.ExecutionMode, ExecutionModeHost)
	}
	if capabilities.PreferredBuildBackend != BuildBackendHostRuntime {
		t.Fatalf("preferred backend=%s, want %s", capabilities.PreferredBuildBackend, BuildBackendHostRuntime)
	}
	if capabilities.PrimaryRuntime != "docker" {
		t.Fatalf("primary runtime=%s, want docker", capabilities.PrimaryRuntime)
	}
	if !containsString(capabilities.AvailableRuntimes, "podman") {
		t.Fatalf("available runtimes=%v, expected podman", capabilities.AvailableRuntimes)
	}
}

func TestProbeRuntimeCapabilities_ContainerPrefersEmbeddedBuildkit(t *testing.T) {
	deps := probeDependencies{
		lookPath: func(name string) (string, error) {
			switch name {
			case "docker", "buildctl", "buildkitd":
				return "/usr/bin/" + name, nil
			default:
				return "", errors.New("not found")
			}
		},
		stat: func(path string) (fs.FileInfo, error) {
			switch path {
			case "/.dockerenv", "/var/run/docker.sock":
				return fakeFileInfo{}, nil
			default:
				return nil, errors.New("missing")
			}
		},
		getenv: func(key string) string { return "" },
	}

	capabilities := probeRuntimeCapabilities(deps)
	if capabilities.ExecutionMode != ExecutionModeContainer {
		t.Fatalf("execution mode=%s, want %s", capabilities.ExecutionMode, ExecutionModeContainer)
	}
	if capabilities.PreferredBuildBackend != BuildBackendEmbeddedBuildkit {
		t.Fatalf("preferred backend=%s, want %s", capabilities.PreferredBuildBackend, BuildBackendEmbeddedBuildkit)
	}
	if !capabilities.DockerSocketAvailable {
		t.Fatalf("expected docker socket to be detected")
	}
	if !containsString(capabilities.AvailableBuilders, "buildkitd") {
		t.Fatalf("available builders=%v, expected buildkitd", capabilities.AvailableBuilders)
	}
}
