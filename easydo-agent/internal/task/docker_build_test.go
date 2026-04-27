package task

import (
	"strings"
	"testing"

	"easydo-agent/internal/system"
	"github.com/sirupsen/logrus"
)

func TestDockerBuildScript_HostRuntimeUsesDetectedRuntime(t *testing.T) {
	executor := &Executor{log: logrus.New(), runtime: system.RuntimeCapabilities{PreferredBuildBackend: system.BuildBackendHostRuntime, PrimaryRuntime: "podman"}}
	script, err := executor.dockerBuildScript(TaskParams{TaskID: 101, Params: map[string]interface{}{"image_name": "demo/app", "image_tag": "v1", "dockerfile": "./Dockerfile", "context": ".", "push": true, "registry": "registry.example.com"}}, "/workspace")
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

func TestDockerBuildScript_EmbeddedBuildkitUsesSharedRuntimeRoot(t *testing.T) {
	executor := &Executor{log: logrus.New(), runtime: system.RuntimeCapabilities{PreferredBuildBackend: system.BuildBackendEmbeddedBuildkit}}
	script, err := executor.dockerBuildScript(TaskParams{TaskID: 987, Params: map[string]interface{}{
		"image_name": "demo/app",
		"image_tag":  "v1",
		"dockerfile": "./build/Dockerfile",
		"context":    ".",
	}}, "/workspace/app")
	if err != nil {
		t.Fatalf("dockerBuildScript returned error: %v", err)
	}
	if !strings.Contains(script, `RUNTIME_ROOT="$(pwd)/.easydo-buildkit/shared"`) {
		t.Fatalf("expected embedded buildkit runtime root to be shared, got:\n%s", script)
	}
	if strings.Contains(script, `/task_987`) {
		t.Fatalf("expected embedded buildkit runtime root not to be task scoped, got:\n%s", script)
	}
}

func TestDockerBuildScript_EmbeddedBuildkitKeepsSharedRuntimeRoot(t *testing.T) {
	executor := &Executor{log: logrus.New(), runtime: system.RuntimeCapabilities{PreferredBuildBackend: system.BuildBackendEmbeddedBuildkit}}
	script, err := executor.dockerBuildScript(TaskParams{TaskID: 654, Params: map[string]interface{}{
		"image_name": "demo/app",
		"image_tag":  "v1",
		"dockerfile": "./build/Dockerfile",
		"context":    ".",
	}}, "/workspace/app")
	if err != nil {
		t.Fatalf("dockerBuildScript returned error: %v", err)
	}
	if strings.Contains(script, `rm -rf "$RUNTIME_ROOT"`) {
		t.Fatalf("expected embedded buildkit cleanup not to remove shared runtime root, got:\n%s", script)
	}
}

func TestDockerBuildScript_EmbeddedBuildkitRemovesDirectoryLockOnCleanup(t *testing.T) {
	executor := &Executor{log: logrus.New(), runtime: system.RuntimeCapabilities{PreferredBuildBackend: system.BuildBackendEmbeddedBuildkit}}
	script, err := executor.dockerBuildScript(TaskParams{TaskID: 655, Params: map[string]interface{}{
		"image_name": "demo/app",
		"image_tag":  "v1",
		"dockerfile": "./build/Dockerfile",
		"context":    ".",
	}}, "/workspace/app")
	if err != nil {
		t.Fatalf("dockerBuildScript returned error: %v", err)
	}
	if strings.Contains(script, `rm -f "$LOCK_FILE"`) {
		t.Fatalf("expected embedded buildkit cleanup not to remove lock dir as a file, got:\n%s", script)
	}
	if !strings.Contains(script, `rmdir "$LOCK_FILE" >/dev/null 2>&1 || true`) {
		t.Fatalf("expected embedded buildkit cleanup to remove directory lock, got:\n%s", script)
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
	executor.SetEmbeddedBuildkitEnv(map[string]string{
		"EASYDO_BUILDKIT_SOCKET_PATH": "/tmp/buildkitd.sock",
		"EASYDO_BUILDKIT_STATE_DIR":   "/tmp/buildkit-state",
		"EASYDO_BUILDKIT_CONFIG_PATH": "/tmp/buildkitd.toml",
	})
	script, err := executor.dockerBuildScript(TaskParams{Params: map[string]interface{}{"image_name": "demo/app", "image_tag": "v1", "dockerfile": "./build/Dockerfile", "context": ".", "push": false}}, "/workspace/app")
	if err != nil {
		t.Fatalf("dockerBuildScript returned error: %v", err)
	}
	if strings.Contains(script, `unshare -Ur`) || strings.Contains(script, `rootlesskit buildkitd`) || strings.Contains(script, `buildkitd --rootless`) {
		t.Fatalf("expected embedded buildkit script to use direct privileged buildkitd launch, got:\n%s", script)
	}
	for _, unexpected := range []string{
		`buildkitd --config`,
		`buildkitd did not become ready`,
		`shared buildkitd did not become ready`,
		`mkdir "$LOCK_FILE"`,
	} {
		if strings.Contains(script, unexpected) {
			t.Fatalf("expected embedded buildkit task script not to manage daemon lifecycle with %s, got:\n%s", unexpected, script)
		}
	}
	for _, expected := range []string{
		`SOCKET_PATH="/tmp/buildkitd.sock"`,
		`BUILDKIT_STATE_DIR="/tmp/buildkit-state"`,
		`BUILDKIT_CONFIG_PATH="/tmp/buildkitd.toml"`,
		`buildctl --addr "unix://$SOCKET_PATH" build`,
		`OUTPUT_SPEC="type=oci,dest=.easydo-artifacts/images/demo_app_v1.tar"`,
		`--local context="."`,
		`--local dockerfile="build"`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("expected embedded buildkit script to include %s, got:\n%s", expected, script)
		}
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

func TestDockerBuildScript_EmbeddedBuildkitPushUsesTaskScopedDockerConfig(t *testing.T) {
	executor := &Executor{log: logrus.New(), runtime: system.RuntimeCapabilities{PreferredBuildBackend: system.BuildBackendEmbeddedBuildkit}}
	script, err := executor.dockerBuildScript(TaskParams{TaskID: 123, Params: map[string]interface{}{
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
	if strings.Contains(script, `DOCKER_CONFIG="$(pwd)/.easydo-buildkit/shared/docker"`) {
		t.Fatalf("expected embedded buildkit push not to reuse shared docker config dir, got:\n%s", script)
	}
	if !strings.Contains(script, `.easydo-buildkit/tasks/task_123/docker`) {
		t.Fatalf("expected embedded buildkit push to use task scoped docker config dir, got:\n%s", script)
	}
	if strings.Contains(script, `.easydo-buildkit/tasks/registry.example.com_demo_app_v1/docker`) {
		t.Fatalf("expected embedded buildkit push docker config dir to stop using image-ref scope, got:\n%s", script)
	}
	if !strings.Contains(script, `cat > "$DOCKER_CONFIG/config.json" <<EOF`) {
		t.Fatalf("expected embedded buildkit push to write auth config into task scoped docker config, got:\n%s", script)
	}
}

func TestDockerBuildScript_EmbeddedBuildkitPushUsesDistinctDockerConfigPerTaskID(t *testing.T) {
	executor := &Executor{log: logrus.New(), runtime: system.RuntimeCapabilities{PreferredBuildBackend: system.BuildBackendEmbeddedBuildkit}}
	scriptA, err := executor.dockerBuildScript(TaskParams{TaskID: 123, Params: map[string]interface{}{
		"image_name": "demo/app",
		"image_tag":  "v1",
		"dockerfile": "./build/Dockerfile",
		"context":    ".",
		"push":       true,
		"registry":   "registry.example.com",
	}}, "/workspace/app")
	if err != nil {
		t.Fatalf("dockerBuildScript returned error for task 123: %v", err)
	}
	scriptB, err := executor.dockerBuildScript(TaskParams{TaskID: 456, Params: map[string]interface{}{
		"image_name": "demo/app",
		"image_tag":  "v1",
		"dockerfile": "./build/Dockerfile",
		"context":    ".",
		"push":       true,
		"registry":   "registry.example.com",
	}}, "/workspace/app")
	if err != nil {
		t.Fatalf("dockerBuildScript returned error for task 456: %v", err)
	}
	if !strings.Contains(scriptA, `.easydo-buildkit/tasks/task_123/docker`) {
		t.Fatalf("expected task 123 script to use task_123 docker config dir, got:\n%s", scriptA)
	}
	if !strings.Contains(scriptB, `.easydo-buildkit/tasks/task_456/docker`) {
		t.Fatalf("expected task 456 script to use task_456 docker config dir, got:\n%s", scriptB)
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

func TestDockerBuildScript_EmbeddedBuildkitPushExportsDockerConfigForBuildctl(t *testing.T) {
	executor := &Executor{log: logrus.New(), runtime: system.RuntimeCapabilities{PreferredBuildBackend: system.BuildBackendEmbeddedBuildkit}}
	script, err := executor.dockerBuildScript(TaskParams{TaskID: 123, Params: map[string]interface{}{
		"image_name": "demo/app",
		"image_tag":  "v1",
		"dockerfile": "./build/Dockerfile",
		"context":    ".",
		"push":       true,
		"registry":   "registry.example.com",
	}}, "/workspace/app")
	if err != nil {
		t.Fatalf("dockerBuildScript returned error: %v", err)
	}
	if !strings.Contains(script, `export DOCKER_CONFIG`) {
		t.Fatalf("expected embedded buildkit push to export DOCKER_CONFIG for buildctl, got:\n%s", script)
	}
}

func TestExecutorEmbeddedBuildkitEnvIsCopied(t *testing.T) {
	executor := &Executor{}
	executor.SetEmbeddedBuildkitEnv(map[string]string{
		"EASYDO_BUILDKIT_SOCKET_PATH": "/var/run/easydo/buildkitd.sock",
		"EASYDO_BUILDKIT_STATE_DIR":   "/var/lib/easydo/buildkit",
	})
	got := executor.EmbeddedBuildkitEnv()
	if got["EASYDO_BUILDKIT_SOCKET_PATH"] != "/var/run/easydo/buildkitd.sock" {
		t.Fatalf("socket path=%q, want propagated value", got["EASYDO_BUILDKIT_SOCKET_PATH"])
	}
	got["EASYDO_BUILDKIT_SOCKET_PATH"] = "/mutated"
	if executor.EmbeddedBuildkitEnv()["EASYDO_BUILDKIT_SOCKET_PATH"] != "/var/run/easydo/buildkitd.sock" {
		t.Fatal("expected embedded buildkit env getter to return a defensive copy")
	}
}

func TestDockerBuildScript_EmbeddedBuildkitUsesConfiguredSocketPaths(t *testing.T) {
	executor := &Executor{log: logrus.New(), runtime: system.RuntimeCapabilities{PreferredBuildBackend: system.BuildBackendEmbeddedBuildkit}}
	executor.SetEmbeddedBuildkitEnv(map[string]string{
		"EASYDO_BUILDKIT_SOCKET_PATH": "/var/run/easydo/buildkitd.sock",
		"EASYDO_BUILDKIT_STATE_DIR":   "/var/lib/easydo/buildkit",
		"EASYDO_BUILDKIT_CONFIG_PATH": "/etc/easydo/buildkitd.toml",
	})
	script, err := executor.dockerBuildScript(TaskParams{TaskID: 777, Params: map[string]interface{}{
		"image_name": "demo/app",
		"image_tag":  "v1",
		"dockerfile": "./build/Dockerfile",
		"context":    ".",
	}}, "/workspace/app")
	if err != nil {
		t.Fatalf("dockerBuildScript returned error: %v", err)
	}
	for _, expected := range []string{
		`SOCKET_PATH="/var/run/easydo/buildkitd.sock"`,
		`BUILDKIT_STATE_DIR="/var/lib/easydo/buildkit"`,
		`BUILDKIT_CONFIG_PATH="/etc/easydo/buildkitd.toml"`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("expected embedded buildkit script to include %s, got:\n%s", expected, script)
		}
	}
}

func TestDockerBuildScript_EmbeddedBuildkitGeneratesDockerHubMirrorConfigFromTaskParams(t *testing.T) {
	executor := &Executor{log: logrus.New(), runtime: system.RuntimeCapabilities{PreferredBuildBackend: system.BuildBackendEmbeddedBuildkit}}
	script, err := executor.dockerBuildScript(TaskParams{TaskID: 777, Params: map[string]interface{}{
		"image_name":         "demo/app",
		"image_tag":          "v1",
		"dockerfile":         "./build/Dockerfile",
		"context":            ".",
		"dockerhub_mirrors": []interface{}{"https://mirror-a.example", "https://mirror-b.example"},
	}}, "/workspace/app")
	if err != nil {
		t.Fatalf("dockerBuildScript returned error: %v", err)
	}
	for _, expected := range []string{
		`BUILDKIT_CONFIG_PATH="$(pwd)/.easydo-buildkit/shared/buildkitd.toml"`,
		`[registry."docker.io"]`,
		`mirrors = [`,
		`"https://mirror-a.example"`,
		`"https://mirror-b.example"`,
		`cat >> "$BUILDKIT_CONFIG_PATH" <<'EOF'`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("expected embedded buildkit script to include %s, got:\n%s", expected, script)
		}
	}
	if strings.Contains(script, `docker info --format '{{json .RegistryConfig.IndexConfigs}}'`) {
		t.Fatalf("expected embedded buildkit script not to depend on docker info, got:\n%s", script)
	}
}

func TestDockerBuildScript_EmbeddedBuildkitMultiArchChecksNativeDependentQemuHelpers(t *testing.T) {
	executor := &Executor{log: logrus.New(), runtime: system.RuntimeCapabilities{PreferredBuildBackend: system.BuildBackendEmbeddedBuildkit}}
	script, err := executor.dockerBuildScript(TaskParams{TaskID: 321, Params: map[string]interface{}{
		"image_name":    "demo/app",
		"image_tag":     "v1",
		"dockerfile":    "./build/Dockerfile",
		"context":       ".",
		"architectures": []interface{}{"linux/amd64", "linux/arm64"},
	}}, "/workspace/app")
	if err != nil {
		t.Fatalf("dockerBuildScript returned error: %v", err)
	}
	for _, expected := range []string{
		`HOST_ARCH=$(uname -m)`,
		`normalize_platform_arch() {`,
		`amd64|x86_64) echo "amd64" ;;`,
		`arm64|aarch64) echo "arm64" ;;`,
		`helper_for_arch() {`,
		`amd64) echo "buildkit-qemu-x86_64" ;;`,
		`arm64) echo "buildkit-qemu-aarch64" ;;`,
		`if [ "$arch" = "$NATIVE_ARCH" ]; then`,
		`multi-platform embedded buildkit requires qemu helpers`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("expected embedded multi-arch script to validate qemu helpers with %s, got:\n%s", expected, script)
		}
	}
	for _, unexpected := range []string{
		`buildkit-qemu-riscv64`,
		`buildkit-qemu-ppc64le`,
		`buildkit-qemu-s390x`,
		`buildkit-qemu-i386`,
		`buildkit-qemu-arm`,
	} {
		if strings.Contains(script, unexpected) {
			t.Fatalf("expected embedded multi-arch helper validation to stay limited to arm64/amd64, got:\n%s", script)
		}
	}
}
