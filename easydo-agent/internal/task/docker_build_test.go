package task

import (
	"strings"
	"testing"

	"easydo-agent/internal/system"
	"github.com/sirupsen/logrus"
)

func TestDockerBuildScript_HostRuntimeUsesDetectedRuntime(t *testing.T) {
	executor := &Executor{log: logrus.New(), runtime: system.RuntimeCapabilities{PreferredBuildBackend: system.BuildBackendHostRuntime, PrimaryRuntime: "podman"}}
	script, err := executor.dockerBuildScript(TaskParams{Params: map[string]interface{}{"image_name": "demo/app", "image_tag": "v1", "dockerfile": "./Dockerfile", "context": ".", "push": true, "registry": "registry.example.com"}}, "/workspace")
	if err != nil {
		t.Fatalf("dockerBuildScript returned error: %v", err)
	}
	if !strings.Contains(script, `"$RUNTIME_BIN" build`) || !strings.Contains(script, `RUNTIME_BIN="podman"`) {
		t.Fatalf("expected host runtime podman build script, got:\n%s", script)
	}
	if !strings.Contains(script, `"$RUNTIME_BIN" push "$IMAGE_REF"`) {
		t.Fatalf("expected host runtime push logic, got:\n%s", script)
	}
}

func TestDockerBuildScript_HostRuntimeMultiArchUsesBuildx(t *testing.T) {
	executor := &Executor{log: logrus.New(), runtime: system.RuntimeCapabilities{PreferredBuildBackend: system.BuildBackendHostRuntime, PrimaryRuntime: "docker"}}
	script, err := executor.dockerBuildScript(TaskParams{Params: map[string]interface{}{
		"image_name":    "demo/app",
		"image_tag":     "v1",
		"dockerfile":    "./Dockerfile",
		"context":       ".",
		"push":          true,
		"registry":      "registry.example.com",
		"architectures": []interface{}{"linux/amd64", "linux/arm64"},
	}}, "/workspace")
	if err != nil {
		t.Fatalf("dockerBuildScript returned error: %v", err)
	}
	if !strings.Contains(script, `PLATFORMS="linux/amd64,linux/arm64"`) {
		t.Fatalf("expected host multi-arch platforms in script, got:\n%s", script)
	}
	if !strings.Contains(script, `"$RUNTIME_BIN" buildx build --platform "$PLATFORMS"`) {
		t.Fatalf("expected docker buildx for multi-arch host runtime, got:\n%s", script)
	}
	if !strings.Contains(script, `--push`) {
		t.Fatalf("expected multi-arch host runtime push path to use --push, got:\n%s", script)
	}
}

func TestDockerBuildScript_EmbeddedBuildkitUsesBuildctl(t *testing.T) {
	executor := &Executor{log: logrus.New(), runtime: system.RuntimeCapabilities{PreferredBuildBackend: system.BuildBackendEmbeddedBuildkit}}
	script, err := executor.dockerBuildScript(TaskParams{Params: map[string]interface{}{"image_name": "demo/app", "image_tag": "v1", "dockerfile": "./build/Dockerfile", "context": ".", "push": false}}, "/workspace/app")
	if err != nil {
		t.Fatalf("dockerBuildScript returned error: %v", err)
	}
	if !strings.Contains(script, `buildkitd --addr "unix://$SOCKET_PATH" --root "$STATE_DIR"`) {
		t.Fatalf("expected embedded buildkit to launch buildkitd directly, got:\n%s", script)
	}
	if strings.Contains(script, `rootlesskit buildkitd`) {
		t.Fatalf("expected embedded buildkit script to avoid rootlesskit launcher, got:\n%s", script)
	}
	if !strings.Contains(script, `buildctl --addr "unix://$SOCKET_PATH" build`) {
		t.Fatalf("expected buildctl build in script, got:\n%s", script)
	}
	if !strings.Contains(script, `OUTPUT_SPEC="type=oci,dest=.easydo-artifacts/images/demo_app_v1.tar"`) {
		t.Fatalf("expected OCI output for non-push build, got:\n%s", script)
	}
	if !strings.Contains(script, `--local dockerfile="/workspace/app/build"`) {
		t.Fatalf("expected resolved dockerfile dir, got:\n%s", script)
	}
}

func TestDockerBuildScript_EmbeddedBuildkitMultiArchAddsPlatformOpt(t *testing.T) {
	executor := &Executor{log: logrus.New(), runtime: system.RuntimeCapabilities{PreferredBuildBackend: system.BuildBackendEmbeddedBuildkit}}
	script, err := executor.dockerBuildScript(TaskParams{Params: map[string]interface{}{
		"image_name":    "demo/app",
		"image_tag":     "v1",
		"dockerfile":    "./build/Dockerfile",
		"context":       ".",
		"push":          false,
		"architectures": []interface{}{"linux/amd64", "linux/arm64"},
	}}, "/workspace/app")
	if err != nil {
		t.Fatalf("dockerBuildScript returned error: %v", err)
	}
	if !strings.Contains(script, `--opt platform="linux/amd64,linux/arm64"`) {
		t.Fatalf("expected embedded multi-arch platform opt in script, got:\n%s", script)
	}
	if !strings.Contains(script, `OUTPUT_SPEC="type=oci,dest=.easydo-artifacts/images/demo_app_v1.tar"`) {
		t.Fatalf("expected non-push multi-arch build to keep OCI output, got:\n%s", script)
	}
}

func TestDockerBuildScript_EmbeddedBuildkitPushExportsDockerConfig(t *testing.T) {
	executor := &Executor{log: logrus.New(), runtime: system.RuntimeCapabilities{PreferredBuildBackend: system.BuildBackendEmbeddedBuildkit}}
	script, err := executor.dockerBuildScript(TaskParams{Params: map[string]interface{}{
		"image_name":    "demo/app",
		"image_tag":     "v1",
		"dockerfile":    "./build/Dockerfile",
		"context":       ".",
		"push":          true,
		"registry":      "registry.example.com",
		"architectures": []interface{}{"linux/amd64", "linux/arm64"},
	}}, "/workspace/app")
	if err != nil {
		t.Fatalf("dockerBuildScript returned error: %v", err)
	}
	if !strings.Contains(script, `export DOCKER_CONFIG`) {
		t.Fatalf("expected embedded buildkit push to export DOCKER_CONFIG, got:\n%s", script)
	}
}
