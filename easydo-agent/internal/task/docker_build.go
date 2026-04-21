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
		return buildEmbeddedBuildkitScript(params.TaskID, imageRef, preBuildScript, dockerfile, filepath.Dir(dockerfile), contextDir, registry, push, platformValue), nil
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

func buildEmbeddedBuildkitScript(taskID uint64, imageRef, preBuildScript, dockerfile, dockerfileDir, contextDir, registry string, push bool, platforms string) string {
	runtimeRoot := "$(pwd)/.easydo-buildkit"
	if taskID > 0 {
		runtimeRoot = fmt.Sprintf("$(pwd)/.easydo-buildkit/task_%d", taskID)
	}
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
		qemuCheckBlock = `missing_helpers=""
for helper in buildkit-qemu-aarch64 buildkit-qemu-arm; do
  if ! command -v "$helper" >/dev/null 2>&1; then
    missing_helpers="$missing_helpers $helper"
  fi
done
if [ -n "$missing_helpers" ]; then
  echo "multi-platform embedded buildkit requires qemu helpers:$missing_helpers" >&2
  exit 1
fi
`
	}
	return fmt.Sprintf(`set -e
RUNTIME_ROOT=%q
SOCKET_DIR="$RUNTIME_ROOT/run"
SOCKET_PATH="$SOCKET_DIR/buildkitd.sock"
STATE_DIR="$RUNTIME_ROOT/state"
DOCKER_CONFIG="$RUNTIME_ROOT/docker"
export DOCKER_CONFIG RUNTIME_ROOT
REGISTRY=%q
PLATFORMS=%q
REGISTRY_USER="${EASYDO_CRED_REGISTRY_AUTH_USERNAME:-}"
REGISTRY_PASSWORD="${EASYDO_CRED_REGISTRY_AUTH_PASSWORD:-${EASYDO_CRED_REGISTRY_AUTH_TOKEN:-}}"
mkdir -p "$RUNTIME_ROOT" "$SOCKET_DIR" "$STATE_DIR" "$DOCKER_CONFIG" .easydo-artifacts/images
cleanup() {
  if [ -n "${BUILDKIT_PID:-}" ]; then
    kill "$BUILDKIT_PID" >/dev/null 2>&1 || true
    wait "$BUILDKIT_PID" >/dev/null 2>&1 || true
  fi
  rm -rf "$RUNTIME_ROOT"
}
trap cleanup EXIT
%sbuildkitd --addr "unix://$SOCKET_PATH" --root "$STATE_DIR" >"$STATE_DIR/buildkitd.log" 2>&1 &
BUILDKIT_PID=$!
for _ in $(seq 1 50); do
  if buildctl --addr "unix://$SOCKET_PATH" debug workers >/dev/null 2>&1; then
    break
  fi
  sleep 0.2
done
if ! buildctl --addr "unix://$SOCKET_PATH" debug workers >/dev/null 2>&1; then
  echo "buildkitd did not become ready" >&2
  sed -n '1,200p' "$STATE_DIR/buildkitd.log" >&2 || true
  exit 1
fi
%s
%s
buildctl --addr "unix://$SOCKET_PATH" build \
  --frontend dockerfile.v0 \
  --local context=%q \
  --local dockerfile=%q \
  --opt platform=%q \
  --opt filename=%q \
  --output "$OUTPUT_SPEC"
`, runtimeRoot, registry, platforms, qemuCheckBlock, preBuildBlock, outputLine, contextDir, dockerfileDir, platforms, filepath.Base(dockerfile))
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func stringifyParam(value interface{}) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func toBool(value interface{}) bool {
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

func normalizeDockerPlatforms(value interface{}) []string {
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
	case []interface{}:
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
