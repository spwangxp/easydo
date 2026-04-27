package task

import (
	"fmt"
	"path/filepath"
	"strings"

	"easydo-agent/internal/system"
)

var defaultDockerPlatforms = []string{"linux/amd64", "linux/arm64"}

const dockerHubRegistry = "docker.io"

func (e *Executor) dockerBuildScript(params TaskParams, workDir string) (string, error) {
	imageName := strings.TrimSpace(stringifyParam(params.Params["image_name"]))
	if imageName == "" {
		return "", fmt.Errorf("image_name is required")
	}
	imageTag := defaultString(stringifyParam(params.Params["image_tag"]), "latest")
	preBuildScript := strings.TrimSpace(stringifyParam(params.Params["pre_build_script"]))
	dockerfile := defaultString(stringifyParam(params.Params["dockerfile"]), "./Dockerfile")
	contextDir := defaultString(stringifyParam(params.Params["context"]), ".")
	registry := normalizeDockerRegistry(stringifyParam(params.Params["registry"]), toBool(params.Params["push"]))
	push := toBool(params.Params["push"])
	platforms := normalizeDockerPlatforms(params.Params["architectures"])
	platformValue := strings.Join(platforms, ",")

	imageRef := imageName + ":" + imageTag
	if registry != "" {
		imageRef = qualifyImageRef(registry, imageName, imageTag)
	}

	switch e.runtime.PreferredBuildBackend {
	case system.BuildBackendHostRuntime:
		runtimeBin := defaultString(e.runtime.PrimaryRuntime, "docker")
		return buildHostRuntimeScript(runtimeBin, imageName, imageTag, preBuildScript, dockerfile, contextDir, registry, imageRef, push, platformValue), nil
	default:
		mirrors := NormalizeDockerHubMirrors(params.Params["dockerhub_mirrors"])
		return buildEmbeddedBuildkitScript(e.EmbeddedBuildkitEnv(), params.TaskID, imageRef, preBuildScript, dockerfile, filepath.Dir(dockerfile), contextDir, registry, push, platformValue, mirrors), nil
	}
}

func buildHostRuntimeScript(runtimeBin, imageName, imageTag, preBuildScript, dockerfile, contextDir, registry, imageRef string, push bool, platforms string) string {
	pushValue := "false"
	if push {
		pushValue = "true"
	}
	preBuildBlock := ""
	if preBuildScript != "" {
		preBuildBlock = fmt.Sprintf("if [ -n %q ]; then\n  %s\nfi\n", preBuildScript, preBuildScript)
	}
	script := fmt.Sprintf(`set -e
RUNTIME_BIN=%q
IMAGE_NAME=%q
IMAGE_TAG=%q
DOCKERFILE=%q
CONTEXT=%q
REGISTRY=%q
IMAGE_REF=%q
PLATFORMS=%q
PUSH_ENABLED=%q
REGISTRY_USER="${EASYDO_CRED_REGISTRY_AUTH_USERNAME:-}"
REGISTRY_PASSWORD="${EASYDO_CRED_REGISTRY_AUTH_PASSWORD:-${EASYDO_CRED_REGISTRY_AUTH_TOKEN:-}}"
if [ -n "$REGISTRY" ] && [ -n "$REGISTRY_USER" ] && [ -n "$REGISTRY_PASSWORD" ]; then
  printf '%%s\n' "$REGISTRY_PASSWORD" | "$RUNTIME_BIN" login "$REGISTRY" --username "$REGISTRY_USER" --password-stdin
fi
%s
if [ -n "$PLATFORMS" ]; then
  case "$PLATFORMS" in
    *,*)
      if [ "$RUNTIME_BIN" != "docker" ]; then
        echo "multi-architecture host runtime builds require docker buildx" >&2
        exit 1
      fi
      if [ "$PUSH_ENABLED" = "true" ] && [ -n "$REGISTRY" ]; then
        "$RUNTIME_BIN" buildx build --platform "$PLATFORMS" -t "$IMAGE_REF" -f "$DOCKERFILE" "$CONTEXT" --push
      else
        mkdir -p .easydo-artifacts/images
        "$RUNTIME_BIN" buildx build --platform "$PLATFORMS" -f "$DOCKERFILE" "$CONTEXT" --output "type=oci,dest=.easydo-artifacts/images/%s.tar"
      fi
      exit 0
      ;;
    *)
      "$RUNTIME_BIN" build --platform "$PLATFORMS" -t "$IMAGE_NAME:$IMAGE_TAG" -f "$DOCKERFILE" "$CONTEXT"
      ;;
  esac
else
  "$RUNTIME_BIN" build -t "$IMAGE_NAME:$IMAGE_TAG" -f "$DOCKERFILE" "$CONTEXT"
fi
`, runtimeBin, imageName, imageTag, dockerfile, contextDir, registry, imageRef, platforms, pushValue, preBuildBlock, shellSafeFilename(imageRef))
	if push {
		script += `if [ -n "$REGISTRY" ]; then
  case "$IMAGE_NAME" in
    "$REGISTRY"/*) : ;;
    *) "$RUNTIME_BIN" tag "$IMAGE_NAME:$IMAGE_TAG" "$IMAGE_REF" ;;
  esac
  "$RUNTIME_BIN" push "$IMAGE_REF"
fi
`
	}
	return script
}

func buildEmbeddedBuildkitScript(buildkitEnv map[string]string, taskID uint64, imageRef, preBuildScript, dockerfile, dockerfileDir, contextDir, registry string, push bool, platforms string, mirrors []string) string {
	socketPath := defaultString(buildkitEnv["EASYDO_BUILDKIT_SOCKET_PATH"], "$(pwd)/.easydo-buildkit/shared/run/buildkitd.sock")
	stateDir := defaultString(buildkitEnv["EASYDO_BUILDKIT_STATE_DIR"], "$(pwd)/.easydo-buildkit/shared/state")
	configPath := defaultString(buildkitEnv["EASYDO_BUILDKIT_CONFIG_PATH"], "$(pwd)/.easydo-buildkit/shared/buildkitd.toml")
	dockerConfigDir := fmt.Sprintf("$(pwd)/.easydo-buildkit/tasks/task_%d/docker", taskID)
	outputLine := fmt.Sprintf("OUTPUT_SPEC=\"type=oci,dest=.easydo-artifacts/images/%s.tar\"\nmkdir -p .easydo-artifacts/images", shellSafeFilename(imageRef))
	if push {
		outputLine = fmt.Sprintf("OUTPUT_SPEC=\"type=image,name=%s,push=true\"\nif [ -n \"$REGISTRY\" ] && [ -n \"$REGISTRY_USER\" ] && [ -n \"$REGISTRY_PASSWORD\" ]; then\n  mkdir -p \"$DOCKER_CONFIG\"\n  AUTH_B64=$(printf '%%s:%%s' \"$REGISTRY_USER\" \"$REGISTRY_PASSWORD\" | base64 | tr -d '\\n')\n  cat > \"$DOCKER_CONFIG/config.json\" <<EOF\n%s\nEOF\nfi", imageRef, buildRegistryAuthConfigJSON(registry))
	}
	preBuildBlock := ""
	if preBuildScript != "" {
		preBuildBlock = fmt.Sprintf("if [ -n %q ]; then\n  %s\nfi\n", preBuildScript, preBuildScript)
	}
	qemuCheckBlock := ""
	if strings.Contains(platforms, ",") || (platforms != "" && !strings.Contains(platforms, "linux/amd64")) {
		qemuCheckBlock = `normalize_platform_arch() {
  case "$1" in
    amd64|x86_64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *) echo "$1" ;;
  esac
}
helper_for_arch() {
  case "$1" in
    amd64) echo "buildkit-qemu-x86_64" ;;
    arm64) echo "buildkit-qemu-aarch64" ;;
    *) return 1 ;;
  esac
}
HOST_ARCH=$(uname -m)
NATIVE_ARCH=$(normalize_platform_arch "$HOST_ARCH")
missing_helpers=""
OLD_IFS=$IFS
IFS=,
for platform in $PLATFORMS; do
  arch=${platform##*/}
  arch=$(normalize_platform_arch "$arch")
  if [ "$arch" = "$NATIVE_ARCH" ]; then
    continue
  fi
  helper=$(helper_for_arch "$arch") || continue
  if ! command -v "$helper" >/dev/null 2>&1; then
    missing_helpers="$missing_helpers $helper"
  fi
done
IFS=$OLD_IFS
if [ -n "$missing_helpers" ]; then
  echo "multi-platform embedded buildkit requires qemu helpers:$missing_helpers" >&2
  exit 1
fi
`
	}
	mirrorConfigBlock := buildDockerHubMirrorConfigBlock(mirrors)
	return fmt.Sprintf(`set -e
SOCKET_PATH=%q
BUILDKIT_STATE_DIR=%q
BUILDKIT_CONFIG_PATH=%q
DOCKER_CONFIG=%q
REGISTRY=%q
PLATFORMS=%q
REGISTRY_USER="${EASYDO_CRED_REGISTRY_AUTH_USERNAME:-}"
REGISTRY_PASSWORD="${EASYDO_CRED_REGISTRY_AUTH_PASSWORD:-${EASYDO_CRED_REGISTRY_AUTH_TOKEN:-}}"
mkdir -p "$DOCKER_CONFIG" .easydo-artifacts/images
%s
%s
%s
%s
export DOCKER_CONFIG
buildctl --addr "unix://$SOCKET_PATH" build \
  --frontend dockerfile.v0 \
  --local context=%q \
  --local dockerfile=%q \
  --opt platform=%q \
  --opt filename=%q \
  --output "$OUTPUT_SPEC"
`, socketPath, stateDir, configPath, dockerConfigDir, registry, platforms, mirrorConfigBlock, qemuCheckBlock, preBuildBlock, outputLine, contextDir, dockerfileDir, platforms, filepath.Base(dockerfile))
}

func NormalizeDockerHubMirrors(value any) []string {
	mirrors := make([]string, 0)
	appendMirror := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		for _, existing := range mirrors {
			if existing == raw {
				return
			}
		}
		mirrors = append(mirrors, raw)
	}
	switch v := value.(type) {
	case []string:
		for _, item := range v {
			appendMirror(item)
		}
	case []any:
		for _, item := range v {
			appendMirror(stringifyParam(item))
		}
	case string:
		for _, item := range strings.Split(v, ",") {
			appendMirror(item)
		}
	}
	return mirrors
}

func buildDockerHubMirrorConfigBlock(mirrors []string) string {
	if len(mirrors) == 0 {
		return ""
	}
	escapedMirrors := make([]string, 0, len(mirrors))
	for _, mirror := range mirrors {
		escapedMirrors = append(escapedMirrors, fmt.Sprintf("%q", mirror))
	}
	return fmt.Sprintf("cat >> \"$BUILDKIT_CONFIG_PATH\" <<'EOF'\n\n[registry.\"docker.io\"]\n  mirrors = [%s]\nEOF", strings.Join(escapedMirrors, ", "))
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func stringifyParam(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func toBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}

func qualifyImageRef(registry, imageName, imageTag string) string {
	registry = strings.TrimSpace(registry)
	imageName = strings.TrimSpace(imageName)
	if isDockerHubRegistryAlias(registry) {
		for _, alias := range []string{dockerHubRegistry, "index.docker.io", "registry-1.docker.io"} {
			if strings.HasPrefix(imageName, alias+"/") {
				return dockerHubRegistry + "/" + strings.TrimPrefix(imageName, alias+"/") + ":" + imageTag
			}
		}
	}
	if strings.HasPrefix(imageName, registry+"/") {
		return imageName + ":" + imageTag
	}
	return registry + "/" + imageName + ":" + imageTag
}

func normalizeDockerRegistry(registry string, push bool) string {
	registry = strings.TrimSpace(registry)
	if registry == "" {
		if push {
			return dockerHubRegistry
		}
		return ""
	}
	if isDockerHubRegistryAlias(registry) {
		return dockerHubRegistry
	}
	return registry
}

func isDockerHubRegistryAlias(registry string) bool {
	normalized := strings.ToLower(strings.TrimSpace(registry))
	normalized = strings.TrimSuffix(normalized, "/")
	return normalized == dockerHubRegistry || normalized == "index.docker.io" || normalized == "registry-1.docker.io" || normalized == "https://index.docker.io/v1"
}

func buildRegistryAuthConfigJSON(registry string) string {
	if isDockerHubRegistryAlias(registry) {
		return `{"auths":{"docker.io":{"auth":"$AUTH_B64"},"index.docker.io":{"auth":"$AUTH_B64"},"registry-1.docker.io":{"auth":"$AUTH_B64"},"https://index.docker.io/v1/":{"auth":"$AUTH_B64"}}}`
	}
	return fmt.Sprintf(`{"auths":{"%s":{"auth":"$AUTH_B64"}}}`, registry)
}

func shellSafeFilename(value string) string {
	replacer := strings.NewReplacer("/", "_", ":", "_", " ", "_")
	return replacer.Replace(value)
}

func normalizeDockerPlatforms(value any) []string {
	items := make([]string, 0, len(defaultDockerPlatforms))
	appendPlatform := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		for _, existing := range items {
			if existing == raw {
				return
			}
		}
		items = append(items, raw)
	}
	switch v := value.(type) {
	case []string:
		for _, item := range v {
			appendPlatform(item)
		}
	case []any:
		for _, item := range v {
			appendPlatform(stringifyParam(item))
		}
	case string:
		for _, item := range strings.Split(v, ",") {
			appendPlatform(item)
		}
	}
	if len(items) == 0 {
		return append([]string{}, defaultDockerPlatforms...)
	}
	return items
}
