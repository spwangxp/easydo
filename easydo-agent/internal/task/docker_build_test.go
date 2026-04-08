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

func TestDockerBuildScript_HostRuntimeRunsPreBuildScriptInSameShell(t *testing.T) {
	executor := &Executor{log: logrus.New(), runtime: system.RuntimeCapabilities{PreferredBuildBackend: system.BuildBackendHostRuntime, PrimaryRuntime: "docker"}}
	script, err := executor.dockerBuildScript(TaskParams{Params: map[string]interface{}{
		"image_name":       "demo/app",
		"image_tag":        "v1",
		"dockerfile":       "./Dockerfile",
		"context":          ".",
		"pre_build_script": "cd ./app",
	}}, "/workspace")
	if err != nil {
		t.Fatalf("dockerBuildScript returned error: %v", err)
	}
	if !strings.Contains(script, "cd ./app") {
		t.Fatalf("expected host runtime script to include pre-build shell content, got:\n%s", script)
	}
	if strings.Index(script, "cd ./app") > strings.Index(script, `"$RUNTIME_BIN" build`) {
		t.Fatalf("expected pre-build script to run before docker build, got:\n%s", script)
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

func TestDockerBuildScript_HostRuntimePushDefaultsRegistryToDockerHub(t *testing.T) {
	executor := &Executor{log: logrus.New(), runtime: system.RuntimeCapabilities{PreferredBuildBackend: system.BuildBackendHostRuntime, PrimaryRuntime: "docker"}}
	script, err := executor.dockerBuildScript(TaskParams{Params: map[string]interface{}{
		"image_name": "demo/app",
		"image_tag":  "v1",
		"dockerfile": "./Dockerfile",
		"context":    ".",
		"push":       true,
	}}, "/workspace")
	if err != nil {
		t.Fatalf("dockerBuildScript returned error: %v", err)
	}
	if !strings.Contains(script, `REGISTRY="docker.io"`) {
		t.Fatalf("expected empty registry push to default to docker.io, got:\n%s", script)
	}
	if !strings.Contains(script, `IMAGE_REF="docker.io/demo/app:v1"`) {
		t.Fatalf("expected docker hub image ref for empty registry push, got:\n%s", script)
	}
}

func TestDockerBuildScript_HostRuntimeMultiArchPushDefaultsRegistryToDockerHub(t *testing.T) {
	executor := &Executor{log: logrus.New(), runtime: system.RuntimeCapabilities{PreferredBuildBackend: system.BuildBackendHostRuntime, PrimaryRuntime: "docker"}}
	script, err := executor.dockerBuildScript(TaskParams{Params: map[string]interface{}{
		"image_name":    "demo/app",
		"image_tag":     "v1",
		"dockerfile":    "./Dockerfile",
		"context":       ".",
		"push":          true,
		"architectures": []interface{}{"linux/amd64", "linux/arm64"},
	}}, "/workspace")
	if err != nil {
		t.Fatalf("dockerBuildScript returned error: %v", err)
	}
	if !strings.Contains(script, `REGISTRY="docker.io"`) {
		t.Fatalf("expected empty registry multi-arch push to default to docker.io, got:\n%s", script)
	}
	if !strings.Contains(script, `IMAGE_REF="docker.io/demo/app:v1"`) {
		t.Fatalf("expected docker hub image ref for empty registry multi-arch push, got:\n%s", script)
	}
	if !strings.Contains(script, `--push`) {
		t.Fatalf("expected empty registry multi-arch push to keep buildx --push path, got:\n%s", script)
	}
}

func TestDockerBuildScript_HostRuntimePushNormalizesDockerHubAliases(t *testing.T) {
	executor := &Executor{log: logrus.New(), runtime: system.RuntimeCapabilities{PreferredBuildBackend: system.BuildBackendHostRuntime, PrimaryRuntime: "docker"}}
	script, err := executor.dockerBuildScript(TaskParams{Params: map[string]interface{}{
		"image_name": "demo/app",
		"image_tag":  "v1",
		"dockerfile": "./Dockerfile",
		"context":    ".",
		"push":       true,
		"registry":   "index.docker.io",
	}}, "/workspace")
	if err != nil {
		t.Fatalf("dockerBuildScript returned error: %v", err)
	}
	if !strings.Contains(script, `REGISTRY="docker.io"`) {
		t.Fatalf("expected docker hub alias to normalize to docker.io, got:\n%s", script)
	}
	if !strings.Contains(script, `IMAGE_REF="docker.io/demo/app:v1"`) {
		t.Fatalf("expected docker hub alias image ref to normalize to docker.io, got:\n%s", script)
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
	if !strings.Contains(script, `--local context="."`) {
		t.Fatalf("expected relative context dir, got:\n%s", script)
	}
	if !strings.Contains(script, `--local dockerfile="build"`) {
		t.Fatalf("expected relative dockerfile dir, got:\n%s", script)
	}
}

func TestDockerBuildScript_EmbeddedBuildkitRunsPreBuildScriptBeforeBuildctl(t *testing.T) {
	executor := &Executor{log: logrus.New(), runtime: system.RuntimeCapabilities{PreferredBuildBackend: system.BuildBackendEmbeddedBuildkit}}
	script, err := executor.dockerBuildScript(TaskParams{Params: map[string]interface{}{
		"image_name":       "demo/app",
		"image_tag":        "v1",
		"dockerfile":       "./build/Dockerfile",
		"context":          ".",
		"pre_build_script": "cd ./app",
	}}, "/workspace/app")
	if err != nil {
		t.Fatalf("dockerBuildScript returned error: %v", err)
	}
	if !strings.Contains(script, "cd ./app") {
		t.Fatalf("expected embedded buildkit script to include pre-build shell content, got:\n%s", script)
	}
	if strings.Index(script, "cd ./app") > strings.Index(script, `buildctl --addr "unix://$SOCKET_PATH" build`) {
		t.Fatalf("expected pre-build script before buildctl invocation, got:\n%s", script)
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

func TestDockerBuildScript_EmbeddedBuildkitPushDefaultsRegistryToDockerHub(t *testing.T) {
	executor := &Executor{log: logrus.New(), runtime: system.RuntimeCapabilities{PreferredBuildBackend: system.BuildBackendEmbeddedBuildkit}}
	script, err := executor.dockerBuildScript(TaskParams{Params: map[string]interface{}{
		"image_name": "demo/app",
		"image_tag":  "v1",
		"dockerfile": "./build/Dockerfile",
		"context":    ".",
		"push":       true,
	}}, "/workspace/app")
	if err != nil {
		t.Fatalf("dockerBuildScript returned error: %v", err)
	}
	if !strings.Contains(script, `REGISTRY="docker.io"`) {
		t.Fatalf("expected embedded buildkit empty registry push to default to docker.io, got:\n%s", script)
	}
	if !strings.Contains(script, `OUTPUT_SPEC="type=image,name=docker.io/demo/app:v1,push=true"`) {
		t.Fatalf("expected embedded buildkit empty registry push to target docker hub image ref, got:\n%s", script)
	}
}

func TestDockerBuildScript_EmbeddedBuildkitPushAddsDockerHubAliasAuthEntries(t *testing.T) {
	executor := &Executor{log: logrus.New(), runtime: system.RuntimeCapabilities{PreferredBuildBackend: system.BuildBackendEmbeddedBuildkit}}
	script, err := executor.dockerBuildScript(TaskParams{Params: map[string]interface{}{
		"image_name": "demo/app",
		"image_tag":  "v1",
		"dockerfile": "./build/Dockerfile",
		"context":    ".",
		"push":       true,
		"registry":   "registry-1.docker.io",
	}}, "/workspace/app")
	if err != nil {
		t.Fatalf("dockerBuildScript returned error: %v", err)
	}
	if !strings.Contains(script, `REGISTRY="docker.io"`) {
		t.Fatalf("expected registry-1.docker.io to normalize to docker.io, got:\n%s", script)
	}
	if !strings.Contains(script, `OUTPUT_SPEC="type=image,name=docker.io/demo/app:v1,push=true"`) {
		t.Fatalf("expected alias push output to target docker.io image ref, got:\n%s", script)
	}
	for _, expected := range []string{
		`"docker.io":{"auth":"$AUTH_B64"}`,
		`"index.docker.io":{"auth":"$AUTH_B64"}`,
		`"registry-1.docker.io":{"auth":"$AUTH_B64"}`,
		`"https://index.docker.io/v1/":{"auth":"$AUTH_B64"}`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("expected embedded buildkit auth config to include %s, got:\n%s", expected, script)
		}
	}
}
