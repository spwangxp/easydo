package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	"easydo-server/internal/models"
)

const (
	taskExecModeAgent  = "agent"
	taskExecModeServer = "server"
)

type pipelineTaskDefinition struct {
	CanonicalType   string
	Category        string
	ExecMode        string
	ShellTemplate   string
	CredentialSlots []taskCredentialSlot
}

type taskCredentialSlot struct {
	Slot              string
	Label             string
	Required          bool
	AllowedTypes      []models.CredentialType
	AllowedCategories []models.CredentialCategory
}

func (d pipelineTaskDefinition) findCredentialSlot(slot string) (taskCredentialSlot, bool) {
	for _, item := range d.CredentialSlots {
		if item.Slot == slot {
			return item, true
		}
	}
	return taskCredentialSlot{}, false
}

func (s taskCredentialSlot) allowsType(t models.CredentialType) bool {
	if len(s.AllowedTypes) == 0 {
		return true
	}
	for _, item := range s.AllowedTypes {
		if item == t {
			return true
		}
	}
	return false
}

func (s taskCredentialSlot) allowsCategory(c models.CredentialCategory) bool {
	if len(s.AllowedCategories) == 0 {
		return true
	}
	for _, item := range s.AllowedCategories {
		if item == c {
			return true
		}
	}
	return false
}

var pipelineTaskDefinitions = map[string]pipelineTaskDefinition{
	"git_clone": {
		CanonicalType: "git_clone",
		Category:      "source",
		ExecMode:      taskExecModeAgent,
		ShellTemplate: `set -e
REPO_URL={{ shq (dig "repository.url") }}
if [ -z "$REPO_URL" ]; then
  echo "repository.url is required" >&2
  exit 1
fi
BRANCH={{ shq (def "main" (dig "repository.branch")) }}
TARGET_DIR={{ shq (def "./app" (dig "repository.target_dir")) }}
DEPTH={{ toInt (def 0 (dig "repository.depth")) }}
COMMIT={{ shq (def "" (dig "repository.commit_id")) }}
AUTH_TYPE="${EASYDO_CRED_REPO_AUTH_TYPE:-}"
SSH_KEY_FILE=""
cleanup() {
  if [ -n "$SSH_KEY_FILE" ] && [ -f "$SSH_KEY_FILE" ]; then
    rm -f "$SSH_KEY_FILE"
  fi
}
trap cleanup EXIT

easydo_step "准备 Git 检出任务"
easydo_info "working_dir=$TARGET_DIR branch=$BRANCH depth=$DEPTH"

HTTP_HEADER=""
if [ "$AUTH_TYPE" = "SSH_KEY" ] && [ -n "${EASYDO_CRED_REPO_AUTH_PRIVATE_KEY:-}" ]; then
  SSH_KEY_FILE="$(mktemp)"
  printf '%s\n' "${EASYDO_CRED_REPO_AUTH_PRIVATE_KEY}" > "$SSH_KEY_FILE"
  chmod 600 "$SSH_KEY_FILE"
  export GIT_SSH_COMMAND="ssh -i $SSH_KEY_FILE -o StrictHostKeyChecking=no"
elif [ "$AUTH_TYPE" = "TOKEN" ] && [ -n "${EASYDO_CRED_REPO_AUTH_TOKEN:-${EASYDO_CRED_REPO_AUTH_ACCESS_TOKEN:-}}" ]; then
  TOKEN_VALUE="${EASYDO_CRED_REPO_AUTH_TOKEN:-${EASYDO_CRED_REPO_AUTH_ACCESS_TOKEN:-}}"
  AUTH_USER="${EASYDO_CRED_REPO_AUTH_USERNAME:-oauth2}"
  AUTH_HEADER="$(printf '%s:%s' "$AUTH_USER" "$TOKEN_VALUE" | base64 | tr -d '\n')"
  HTTP_HEADER="Authorization: Basic $AUTH_HEADER"
elif [ "$AUTH_TYPE" = "PASSWORD" ] && [ -n "${EASYDO_CRED_REPO_AUTH_USERNAME:-}" ] && [ -n "${EASYDO_CRED_REPO_AUTH_PASSWORD:-}" ]; then
  AUTH_HEADER="$(printf '%s:%s' "${EASYDO_CRED_REPO_AUTH_USERNAME}" "${EASYDO_CRED_REPO_AUTH_PASSWORD}" | base64 | tr -d '\n')"
  HTTP_HEADER="Authorization: Basic $AUTH_HEADER"
fi

rm -rf "$TARGET_DIR"
mkdir -p "$TARGET_DIR"
REPO_URL_LOG="$(easydo_mask_url "$REPO_URL")"
if [ "$DEPTH" -gt 0 ]; then
  if [ -n "$HTTP_HEADER" ]; then
    easydo_cmd "git -c http.extraHeader=Authorization: Basic *** clone --progress --depth $DEPTH -b $BRANCH $REPO_URL_LOG $TARGET_DIR"
    git -c "http.extraHeader=$HTTP_HEADER" clone --progress --depth "$DEPTH" -b "$BRANCH" "$REPO_URL" "$TARGET_DIR"
  else
    easydo_cmd "git clone --progress --depth $DEPTH -b $BRANCH $REPO_URL_LOG $TARGET_DIR"
    git clone --progress --depth "$DEPTH" -b "$BRANCH" "$REPO_URL" "$TARGET_DIR"
  fi
else
  if [ -n "$HTTP_HEADER" ]; then
    easydo_cmd "git -c http.extraHeader=Authorization: Basic *** clone --progress -b $BRANCH $REPO_URL_LOG $TARGET_DIR"
    git -c "http.extraHeader=$HTTP_HEADER" clone --progress -b "$BRANCH" "$REPO_URL" "$TARGET_DIR"
  else
    easydo_cmd "git clone --progress -b $BRANCH $REPO_URL_LOG $TARGET_DIR"
    git clone --progress -b "$BRANCH" "$REPO_URL" "$TARGET_DIR"
  fi
fi
if [ -n "$COMMIT" ]; then
  cd "$TARGET_DIR"
  easydo_cmd "git checkout $COMMIT"
  git checkout "$COMMIT"
fi`,
		CredentialSlots: []taskCredentialSlot{
			{
				Slot:     "repo_auth",
				Label:    "仓库认证",
				Required: false,
				AllowedTypes: []models.CredentialType{
					models.TypeSSHKey,
					models.TypeToken,
					models.TypePassword,
				},
				AllowedCategories: []models.CredentialCategory{
					models.CategoryGitHub,
					models.CategoryGitLab,
					models.CategoryGitee,
					models.CategoryCustom,
				},
			},
		},
	},
	"shell": {
		CanonicalType: "shell",
		Category:      "utils",
		ExecMode:      taskExecModeAgent,
		ShellTemplate: `set -e
WORKDIR={{ shq (def "." (dig "working_dir")) }}
SCRIPT_CONTENT={{ shq (def "" (dig "script")) }}
SCRIPT_LOG={{ logq (def "" (dig "script")) }}
easydo_step "执行自定义脚本任务"
easydo_info "working_dir=$WORKDIR"
easydo_cmd "$SCRIPT_LOG"
{{ def "" (dig "script") }}`,
	},
	"sleep": {
		CanonicalType: "sleep",
		Category:      "utils",
		ExecMode:      taskExecModeAgent,
		ShellTemplate: `set -e
SECONDS_TO_SLEEP={{ toInt (def 60 (dig "seconds")) }}
easydo_step "执行等待任务"
easydo_cmd "sleep $SECONDS_TO_SLEEP"
sleep "$SECONDS_TO_SLEEP"
easydo_info "sleep completed after ${SECONDS_TO_SLEEP}s"`,
	},
	"npm": {
		CanonicalType: "npm",
		Category:      "build",
		ExecMode:      taskExecModeAgent,
		ShellTemplate: `set -e
WORKDIR={{ shq (def "." (dig "working_dir")) }}
CMD={{ shq (def "npm ci && npm run build" (dig "command")) }}
LOG_CMD={{ logq (def "npm ci && npm run build" (dig "command")) }}
easydo_step "执行 npm 任务"
easydo_info "working_dir=$WORKDIR"
easydo_cmd "$LOG_CMD"
mkdir -p "$WORKDIR"
cd "$WORKDIR"
eval "$CMD"`,
	},
	"maven": {
		CanonicalType: "maven",
		Category:      "build",
		ExecMode:      taskExecModeAgent,
		ShellTemplate: `set -e
WORKDIR={{ shq (def "." (dig "working_dir")) }}
CMD={{ shq (def "mvn -B clean package" (dig "command")) }}
LOG_CMD={{ logq (def "mvn -B clean package" (dig "command")) }}
easydo_step "执行 Maven 任务"
easydo_info "working_dir=$WORKDIR"
easydo_cmd "$LOG_CMD"
mkdir -p "$WORKDIR"
cd "$WORKDIR"
eval "$CMD"`,
	},
	"gradle": {
		CanonicalType: "gradle",
		Category:      "build",
		ExecMode:      taskExecModeAgent,
		ShellTemplate: `set -e
WORKDIR={{ shq (def "." (dig "working_dir")) }}
CMD={{ shq (def "./gradlew build" (dig "command")) }}
LOG_CMD={{ logq (def "./gradlew build" (dig "command")) }}
easydo_step "执行 Gradle 任务"
easydo_info "working_dir=$WORKDIR"
easydo_cmd "$LOG_CMD"
mkdir -p "$WORKDIR"
cd "$WORKDIR"
eval "$CMD"`,
	},
	"docker": {
		CanonicalType: "docker",
		Category:      "build",
		ExecMode:      taskExecModeAgent,
		ShellTemplate: `set -e
IMAGE_NAME={{ shq (def "" (dig "image_name")) }}
if [ -z "$IMAGE_NAME" ]; then
  echo "image_name is required" >&2
  exit 1
fi
IMAGE_TAG={{ shq (def "latest" (dig "image_tag")) }}
DOCKERFILE={{ shq (def "./Dockerfile" (dig "dockerfile")) }}
CONTEXT={{ shq (def "." (dig "context")) }}
REGISTRY={{ shq (def "" (dig "registry")) }}

easydo_step "执行 Docker 构建任务"
easydo_info "image=$IMAGE_NAME:$IMAGE_TAG context=$CONTEXT dockerfile=$DOCKERFILE"

REGISTRY_USER="${EASYDO_CRED_REGISTRY_AUTH_USERNAME:-}"
REGISTRY_PASSWORD="${EASYDO_CRED_REGISTRY_AUTH_PASSWORD:-${EASYDO_CRED_REGISTRY_AUTH_TOKEN:-}}"
if [ -n "$REGISTRY" ] && [ -n "$REGISTRY_USER" ] && [ -n "$REGISTRY_PASSWORD" ]; then
  easydo_cmd "docker login $REGISTRY --username $REGISTRY_USER --password-stdin"
  echo "$REGISTRY_PASSWORD" | docker login "$REGISTRY" --username "$REGISTRY_USER" --password-stdin
fi

easydo_cmd "docker build -t $IMAGE_NAME:$IMAGE_TAG -f $DOCKERFILE $CONTEXT"
docker build -t "$IMAGE_NAME:$IMAGE_TAG" -f "$DOCKERFILE" "$CONTEXT"
if [ "{{ boolStr (dig "push") }}" = "true" ]; then
  if [ -n "$REGISTRY" ]; then
    easydo_cmd "docker tag $IMAGE_NAME:$IMAGE_TAG $REGISTRY/$IMAGE_NAME:$IMAGE_TAG"
    docker tag "$IMAGE_NAME:$IMAGE_TAG" "$REGISTRY/$IMAGE_NAME:$IMAGE_TAG"
    easydo_cmd "docker push $REGISTRY/$IMAGE_NAME:$IMAGE_TAG"
    docker push "$REGISTRY/$IMAGE_NAME:$IMAGE_TAG"
  fi
fi`,
		CredentialSlots: []taskCredentialSlot{
			{
				Slot:     "registry_auth",
				Label:    "镜像仓库认证",
				Required: false,
				AllowedTypes: []models.CredentialType{
					models.TypeToken,
					models.TypePassword,
				},
				AllowedCategories: []models.CredentialCategory{
					models.CategoryDocker,
					models.CategoryCustom,
				},
			},
		},
	},
	"artifact_publish": {
		CanonicalType: "artifact_publish",
		Category:      "build",
		ExecMode:      taskExecModeAgent,
		ShellTemplate: `set -e
ARTIFACT_PATH={{ shq (def "" (dig "artifact_path")) }}
TARGET_DIR={{ shq (def "./artifacts" (dig "target_dir")) }}
if [ -z "$ARTIFACT_PATH" ]; then
  echo "artifact_path is required" >&2
  exit 1
fi
if [ ! -e "$ARTIFACT_PATH" ]; then
  echo "artifact path not found: $ARTIFACT_PATH" >&2
  exit 1
fi
easydo_step "发布制品文件"
easydo_cmd "cp -Rv $ARTIFACT_PATH $TARGET_DIR/"
mkdir -p "$TARGET_DIR"
cp -Rv "$ARTIFACT_PATH" "$TARGET_DIR"/`,
	},
	"unit": {
		CanonicalType: "unit",
		Category:      "test",
		ExecMode:      taskExecModeAgent,
		ShellTemplate: `set -e
WORKDIR={{ shq (def "." (dig "working_dir")) }}
CMD={{ shq (def "npm run test:unit" (dig "command")) }}
LOG_CMD={{ logq (def "npm run test:unit" (dig "command")) }}
easydo_step "执行单元测试任务"
easydo_info "working_dir=$WORKDIR"
easydo_cmd "$LOG_CMD"
mkdir -p "$WORKDIR"
cd "$WORKDIR"
eval "$CMD"`,
	},
	"integration": {
		CanonicalType: "integration",
		Category:      "test",
		ExecMode:      taskExecModeAgent,
		ShellTemplate: `set -e
WORKDIR={{ shq (def "." (dig "working_dir")) }}
CMD={{ shq (def "npm run test:integration" (dig "command")) }}
LOG_CMD={{ logq (def "npm run test:integration" (dig "command")) }}
easydo_step "执行集成测试任务"
easydo_info "working_dir=$WORKDIR"
easydo_cmd "$LOG_CMD"
mkdir -p "$WORKDIR"
cd "$WORKDIR"
eval "$CMD"`,
	},
	"e2e": {
		CanonicalType: "e2e",
		Category:      "test",
		ExecMode:      taskExecModeAgent,
		ShellTemplate: `set -e
WORKDIR={{ shq (def "." (dig "working_dir")) }}
CMD={{ shq (def "npm run test:e2e" (dig "command")) }}
LOG_CMD={{ logq (def "npm run test:e2e" (dig "command")) }}
easydo_step "执行端到端测试任务"
easydo_info "working_dir=$WORKDIR"
easydo_cmd "$LOG_CMD"
mkdir -p "$WORKDIR"
cd "$WORKDIR"
eval "$CMD"`,
	},
	"coverage": {
		CanonicalType: "coverage",
		Category:      "test",
		ExecMode:      taskExecModeAgent,
		ShellTemplate: `set -e
WORKDIR={{ shq (def "." (dig "working_dir")) }}
CMD={{ shq (def "npm run test:coverage" (dig "command")) }}
LOG_CMD={{ logq (def "npm run test:coverage" (dig "command")) }}
easydo_step "执行覆盖率任务"
easydo_info "working_dir=$WORKDIR"
easydo_cmd "$LOG_CMD"
mkdir -p "$WORKDIR"
cd "$WORKDIR"
eval "$CMD"`,
	},
	"lint": {
		CanonicalType: "lint",
		Category:      "test",
		ExecMode:      taskExecModeAgent,
		ShellTemplate: `set -e
WORKDIR={{ shq (def "." (dig "working_dir")) }}
CMD={{ shq (def "npm run lint" (dig "command")) }}
LOG_CMD={{ logq (def "npm run lint" (dig "command")) }}
easydo_step "执行代码检查任务"
easydo_info "working_dir=$WORKDIR"
easydo_cmd "$LOG_CMD"
mkdir -p "$WORKDIR"
cd "$WORKDIR"
eval "$CMD"`,
	},
	"ssh": {
		CanonicalType: "ssh",
		Category:      "deploy",
		ExecMode:      taskExecModeAgent,
		ShellTemplate: `set -e
HOST={{ shq (def "" (dig "host")) }}
USER_NAME={{ shq (def "root" (dig "user")) }}
PORT={{ toInt (def 22 (dig "port")) }}
REMOTE_SCRIPT={{ shq (def "" (dig "script")) }}
if [ -z "$HOST" ]; then
  echo "host is required" >&2
  exit 1
fi
if [ -z "$REMOTE_SCRIPT" ]; then
  echo "script is required" >&2
  exit 1
fi
SSH_KEY_FILE=""
REMOTE_SCRIPT_LOG={{ logq (def "" (dig "script")) }}
cleanup() {
  if [ -n "$SSH_KEY_FILE" ] && [ -f "$SSH_KEY_FILE" ]; then
    rm -f "$SSH_KEY_FILE"
  fi
}
trap cleanup EXIT

easydo_step "执行 SSH 远程任务"
easydo_info "target=$USER_NAME@$HOST port=$PORT"
easydo_cmd "ssh -p $PORT $USER_NAME@$HOST <remote-script>"
easydo_cmd "$REMOTE_SCRIPT_LOG"

if [ -n "${EASYDO_CRED_SSH_AUTH_PRIVATE_KEY:-}" ]; then
  SSH_KEY_FILE="$(mktemp)"
  printf '%s\n' "${EASYDO_CRED_SSH_AUTH_PRIVATE_KEY}" > "$SSH_KEY_FILE"
  chmod 600 "$SSH_KEY_FILE"
  ssh -i "$SSH_KEY_FILE" -o StrictHostKeyChecking=no -p "$PORT" "$USER_NAME@$HOST" "$REMOTE_SCRIPT"
else
  ssh -o StrictHostKeyChecking=no -p "$PORT" "$USER_NAME@$HOST" "$REMOTE_SCRIPT"
fi`,
		CredentialSlots: []taskCredentialSlot{
			{
				Slot:     "ssh_auth",
				Label:    "SSH 认证",
				Required: false,
				AllowedTypes: []models.CredentialType{
					models.TypeSSHKey,
				},
				AllowedCategories: []models.CredentialCategory{
					models.CategoryCustom,
					models.CategoryAWS,
					models.CategoryGCP,
					models.CategoryAzure,
				},
			},
		},
	},
	"kubernetes": {
		CanonicalType: "kubernetes",
		Category:      "deploy",
		ExecMode:      taskExecModeAgent,
		ShellTemplate: `set -e
KUBE_CONFIG_FILE=""
CLUSTER_CA_FILE=""
CLIENT_CERT_FILE=""
CLIENT_KEY_FILE=""
cleanup() {
  if [ -n "$KUBE_CONFIG_FILE" ] && [ -f "$KUBE_CONFIG_FILE" ]; then
    rm -f "$KUBE_CONFIG_FILE"
  fi
  if [ -n "$CLUSTER_CA_FILE" ] && [ -f "$CLUSTER_CA_FILE" ]; then
    rm -f "$CLUSTER_CA_FILE"
  fi
  if [ -n "$CLIENT_CERT_FILE" ] && [ -f "$CLIENT_CERT_FILE" ]; then
    rm -f "$CLIENT_CERT_FILE"
  fi
  if [ -n "$CLIENT_KEY_FILE" ] && [ -f "$CLIENT_KEY_FILE" ]; then
    rm -f "$CLIENT_KEY_FILE"
  fi
}
trap cleanup EXIT

easydo_step "执行 Kubernetes 任务"

if [ -n "${EASYDO_CRED_CLUSTER_AUTH_KUBECONFIG:-}" ]; then
  KUBE_CONFIG_FILE="$(mktemp)"
  printf '%s\n' "${EASYDO_CRED_CLUSTER_AUTH_KUBECONFIG}" > "$KUBE_CONFIG_FILE"
  export KUBECONFIG="$KUBE_CONFIG_FILE"
  easydo_info "auth_mode=kubeconfig"
else
  API_SERVER="${EASYDO_CRED_CLUSTER_AUTH_SERVER:-${EASYDO_CRED_CLUSTER_AUTH_API_SERVER:-}}"
  TOKEN_VALUE="${EASYDO_CRED_CLUSTER_AUTH_TOKEN:-${EASYDO_CRED_CLUSTER_AUTH_ACCESS_TOKEN:-}}"
  NAMESPACE_VALUE="${EASYDO_CRED_CLUSTER_AUTH_NAMESPACE:-default}"

  if [ -n "$API_SERVER" ] && [ -n "$TOKEN_VALUE" ]; then
    easydo_info "auth_mode=api_server server=$(easydo_mask_url "$API_SERVER") namespace=$NAMESPACE_VALUE"
    KUBE_CONFIG_FILE="$(mktemp)"
    kubectl --kubeconfig="$KUBE_CONFIG_FILE" config set-cluster easydo --server="$API_SERVER" >/dev/null

    if [ -n "${EASYDO_CRED_CLUSTER_AUTH_CA_CERT:-}" ]; then
      CLUSTER_CA_FILE="$(mktemp)"
      printf '%s\n' "${EASYDO_CRED_CLUSTER_AUTH_CA_CERT}" > "$CLUSTER_CA_FILE"
      kubectl --kubeconfig="$KUBE_CONFIG_FILE" config set-cluster easydo --certificate-authority="$CLUSTER_CA_FILE" >/dev/null
    fi

    kubectl --kubeconfig="$KUBE_CONFIG_FILE" config set-credentials easydo-user --token="$TOKEN_VALUE" >/dev/null

    if [ -n "${EASYDO_CRED_CLUSTER_AUTH_CERT_PEM:-}" ] && [ -n "${EASYDO_CRED_CLUSTER_AUTH_KEY_PEM:-}" ]; then
      CLIENT_CERT_FILE="$(mktemp)"
      CLIENT_KEY_FILE="$(mktemp)"
      printf '%s\n' "${EASYDO_CRED_CLUSTER_AUTH_CERT_PEM}" > "$CLIENT_CERT_FILE"
      printf '%s\n' "${EASYDO_CRED_CLUSTER_AUTH_KEY_PEM}" > "$CLIENT_KEY_FILE"
      kubectl --kubeconfig="$KUBE_CONFIG_FILE" config set-credentials easydo-user --client-certificate="$CLIENT_CERT_FILE" --client-key="$CLIENT_KEY_FILE" >/dev/null
    fi

    kubectl --kubeconfig="$KUBE_CONFIG_FILE" config set-context easydo --cluster=easydo --user=easydo-user --namespace="$NAMESPACE_VALUE" >/dev/null
    kubectl --kubeconfig="$KUBE_CONFIG_FILE" config use-context easydo >/dev/null
    export KUBECONFIG="$KUBE_CONFIG_FILE"
  fi
fi

if [ -n "{{ def "" (dig "command") }}" ]; then
  CMD={{ shq (def "" (dig "command")) }}
  LOG_CMD={{ logq (def "" (dig "command")) }}
  easydo_cmd "$LOG_CMD"
  eval "$CMD"
else
  MANIFEST={{ shq (def "" (dig "manifest")) }}
  if [ -z "$MANIFEST" ]; then
    echo "manifest or command is required" >&2
    exit 1
  fi
  easydo_cmd "kubectl apply -f $MANIFEST"
  kubectl apply -f "$MANIFEST"
fi`,
		CredentialSlots: []taskCredentialSlot{
			{
				Slot:     "cluster_auth",
				Label:    "集群认证",
				Required: false,
				AllowedTypes: []models.CredentialType{
					models.TypeToken,
					models.TypeCert,
				},
				AllowedCategories: []models.CredentialCategory{
					models.CategoryKubernetes,
					models.CategoryCustom,
				},
			},
		},
	},
	"docker-run": {
		CanonicalType: "docker-run",
		Category:      "deploy",
		ExecMode:      taskExecModeAgent,
		ShellTemplate: `set -e
IMAGE_NAME={{ shq (def "" (dig "image_name")) }}
IMAGE_TAG={{ shq (def "latest" (dig "image_tag")) }}
CONTAINER_NAME={{ shq (def "" (dig "container_name")) }}
RUN_ARGS={{ def "" (dig "run_args") }}
REGISTRY={{ shq (def "" (dig "registry")) }}
if [ -z "$IMAGE_NAME" ]; then
  echo "image_name is required" >&2
  exit 1
fi

RUN_ARGS_LOG={{ logq (def "" (dig "run_args")) }}
easydo_step "执行 Docker 运行任务"

if [ -z "$REGISTRY" ]; then
  FIRST_PART="${IMAGE_NAME%%/*}"
  if [ "$FIRST_PART" != "$IMAGE_NAME" ]; then
    case "$FIRST_PART" in
      *.*|*:*|localhost) REGISTRY="$FIRST_PART" ;;
    esac
  fi
fi

IMAGE_REF="$IMAGE_NAME:$IMAGE_TAG"
if [ -n "$REGISTRY" ]; then
  case "$IMAGE_NAME" in
    "$REGISTRY"/*) IMAGE_REF="$IMAGE_NAME:$IMAGE_TAG" ;;
    *) IMAGE_REF="$REGISTRY/$IMAGE_NAME:$IMAGE_TAG" ;;
  esac
fi

REGISTRY_USER="${EASYDO_CRED_REGISTRY_AUTH_USERNAME:-}"
REGISTRY_PASSWORD="${EASYDO_CRED_REGISTRY_AUTH_PASSWORD:-${EASYDO_CRED_REGISTRY_AUTH_TOKEN:-}}"
if [ -n "$REGISTRY" ] && [ -n "$REGISTRY_USER" ] && [ -n "$REGISTRY_PASSWORD" ]; then
  easydo_cmd "docker login $REGISTRY --username $REGISTRY_USER --password-stdin"
  echo "$REGISTRY_PASSWORD" | docker login "$REGISTRY" --username "$REGISTRY_USER" --password-stdin
fi

if [ -n "$CONTAINER_NAME" ]; then
  easydo_cmd "docker rm -f $CONTAINER_NAME"
  docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
  easydo_cmd "docker run -d --name $CONTAINER_NAME $RUN_ARGS_LOG $IMAGE_REF"
  docker run -d --name "$CONTAINER_NAME" $RUN_ARGS "$IMAGE_REF"
else
  easydo_cmd "docker run --rm $RUN_ARGS_LOG $IMAGE_REF"
  docker run --rm $RUN_ARGS "$IMAGE_REF"
fi`,
		CredentialSlots: []taskCredentialSlot{
			{
				Slot:     "registry_auth",
				Label:    "镜像仓库认证",
				Required: false,
				AllowedTypes: []models.CredentialType{
					models.TypeToken,
					models.TypePassword,
				},
				AllowedCategories: []models.CredentialCategory{
					models.CategoryDocker,
					models.CategoryCustom,
				},
			},
		},
	},
	"email": {
		CanonicalType: "email",
		Category:      "notify",
		ExecMode:      taskExecModeServer,
		CredentialSlots: []taskCredentialSlot{
			{
				Slot:     "smtp_auth",
				Label:    "SMTP 认证",
				Required: false,
				AllowedTypes: []models.CredentialType{
					models.TypePassword,
					models.TypeToken,
				},
				AllowedCategories: []models.CredentialCategory{
					models.CategoryEmail,
					models.CategoryCustom,
				},
			},
		},
	},
	"webhook": {
		CanonicalType: "webhook",
		Category:      "notify",
		ExecMode:      taskExecModeServer,
		CredentialSlots: []taskCredentialSlot{
			{
				Slot:     "webhook_auth",
				Label:    "Webhook 认证",
				Required: false,
				AllowedTypes: []models.CredentialType{
					models.TypeToken,
					models.TypePassword,
					models.TypeOAuth2,
				},
				AllowedCategories: []models.CredentialCategory{
					models.CategoryCustom,
				},
			},
			{
				Slot:     "webhook_mtls",
				Label:    "Webhook 证书",
				Required: false,
				AllowedTypes: []models.CredentialType{
					models.TypeCert,
				},
				AllowedCategories: []models.CredentialCategory{
					models.CategoryCustom,
				},
			},
		},
	},
	"in_app": {
		CanonicalType: "in_app",
		Category:      "notify",
		ExecMode:      taskExecModeServer,
	},
}

var pipelineTaskAliases = map[string]string{
	"github":   "git_clone",
	"gitee":    "git_clone",
	"agent":    "shell",
	"custom":   "shell",
	"script":   "shell",
	"dingtalk": "webhook",
	"wechat":   "webhook",
}

func (c *PipelineConfig) ValidateTaskTypes() (bool, string) {
	for _, node := range c.Nodes {
		if _, _, ok := getPipelineTaskDefinition(node.Type); !ok {
			return false, fmt.Sprintf("流水线配置无效：不支持的任务类型 '%s'", node.Type)
		}
	}
	return true, ""
}

func getPipelineTaskDefinition(taskType string) (string, pipelineTaskDefinition, bool) {
	normalized := strings.TrimSpace(strings.ToLower(taskType))
	if alias, ok := pipelineTaskAliases[normalized]; ok {
		normalized = alias
	}
	def, ok := pipelineTaskDefinitions[normalized]
	return normalized, def, ok
}

func isAgentPipelineTaskType(taskType string) bool {
	_, def, ok := getPipelineTaskDefinition(taskType)
	return ok && def.ExecMode == taskExecModeAgent
}

func isServerPipelineTaskType(taskType string) bool {
	_, def, ok := getPipelineTaskDefinition(taskType)
	return ok && def.ExecMode == taskExecModeServer
}

func normalizePipelineNodeConfig(rawType, canonical string, nodeConfig map[string]interface{}) map[string]interface{} {
	cfg := cloneMap(nodeConfig)
	if cfg == nil {
		cfg = make(map[string]interface{})
	}

	if wd, ok := cfg["workingDir"].(string); ok && strings.TrimSpace(wd) != "" {
		cfg["working_dir"] = wd
	}

	if envJSON, ok := cfg["envVars"].(string); ok && strings.TrimSpace(envJSON) != "" {
		envMap := map[string]interface{}{}
		if err := json.Unmarshal([]byte(envJSON), &envMap); err == nil {
			cfg["env"] = envMap
		}
	}
	if envJSON, ok := cfg["env"].(string); ok && strings.TrimSpace(envJSON) != "" {
		envMap := map[string]interface{}{}
		if err := json.Unmarshal([]byte(envJSON), &envMap); err == nil {
			cfg["env"] = envMap
		}
	}

	switch strings.ToLower(rawType) {
	case "github", "gitee":
		if _, ok := cfg["repository"].(map[string]interface{}); !ok {
			repo := map[string]interface{}{}
			if url, ok := cfg["url"].(string); ok && strings.TrimSpace(url) != "" {
				repo["url"] = url
			}
			if branch, ok := cfg["branch"].(string); ok && strings.TrimSpace(branch) != "" {
				repo["branch"] = branch
			}
			if dir, ok := cfg["target_dir"].(string); ok && strings.TrimSpace(dir) != "" {
				repo["target_dir"] = dir
			}
			cfg["repository"] = repo
		}
	case "docker-run":
		if image, ok := cfg["image"].(string); ok && strings.TrimSpace(image) != "" {
			if _, exists := cfg["image_name"]; !exists {
				cfg["image_name"] = image
			}
		}
	}

	if canonical == "shell" {
		if script, ok := cfg["command"].(string); ok && strings.TrimSpace(script) != "" {
			if _, exists := cfg["script"]; !exists {
				cfg["script"] = script
			}
		}
	}

	return cfg
}

func renderPipelineAgentScript(taskType string, nodeConfig map[string]interface{}) (string, string, error) {
	canonical, def, ok := getPipelineTaskDefinition(taskType)
	if !ok {
		return "", "", fmt.Errorf("unsupported task type: %s", taskType)
	}
	if def.ExecMode != taskExecModeAgent {
		return canonical, "", nil
	}

	cfg := normalizePipelineNodeConfig(taskType, canonical, nodeConfig)
	tpl := strings.TrimSpace(def.ShellTemplate)
	if tpl == "" {
		return canonical, "", fmt.Errorf("shell template is empty for task type: %s", canonical)
	}

	script, err := renderTaskTemplate(tpl, cfg)
	if err != nil {
		return canonical, "", err
	}
	script = taskShellLogPrelude() + "\n\n" + strings.TrimSpace(script)
	if strings.TrimSpace(script) == "" {
		return canonical, "", fmt.Errorf("rendered script is empty for task type: %s", canonical)
	}
	return canonical, script, nil
}

func taskShellLogPrelude() string {
	return strings.TrimSpace(`easydo_mask_text() {
  printf '%s' "$1" | sed -E \
    -e 's#(https?://)[^/@[:space:]:]+:[^/@[:space:]]+@#\1***:***@#g' \
    -e 's#([Aa]uthorization[[:space:]]*[:=][[:space:]]*(Basic|Bearer)[[:space:]]+)[^[:space:]"'\'';]+#\1***#g' \
    -e 's#((access_token|token|password|secret|client_secret|api_key|private_key|cert_pem|key_pem|tls_client_key|tls_client_cert|tls_ca_cert|authorization)[[:space:]]*[:=][[:space:]]*)[^[:space:],"'\'';]+#\1***#g' \
    -e 's#(--(password|token|access-token|secret|client-secret|api-key|authorization)(=|[[:space:]]+))[^[:space:]"'\'';]+#\1***#g'
}

easydo_step() {
  printf '[easydo][step] %s\n' "$*"
}

easydo_info() {
  printf '[easydo][info] %s\n' "$(easydo_mask_text "$*")"
}

easydo_warn() {
  printf '[easydo][warn] %s\n' "$(easydo_mask_text "$*")"
}

easydo_error() {
  printf '[easydo][error] %s\n' "$(easydo_mask_text "$*")" >&2
}

easydo_cmd() {
  printf '%s\n' "$1" | while IFS= read -r easydo_line; do
    printf '[easydo][cmd] %s\n' "$(easydo_mask_text "$easydo_line")"
  done
}

easydo_mask_url() {
  easydo_mask_text "$1"
}`)
}

func renderTaskTemplate(shellTemplate string, params map[string]interface{}) (string, error) {
	dig := func(path string) interface{} {
		return getNestedValue(params, path)
	}
	funcMap := template.FuncMap{
		"dig":     dig,
		"def":     templateDefault,
		"toInt":   toInt,
		"boolStr": boolString,
		"shq":     shellQuote,
		"logq": func(v interface{}) string {
			return shellQuote(sanitizeTaskLogPreview(toString(v), 2400))
		},
	}

	tpl, err := template.New("task-shell").Funcs(funcMap).Parse(shellTemplate)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, params); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

func templateDefault(def, val interface{}) interface{} {
	if isTemplateEmpty(val) {
		return def
	}
	return val
}

func isTemplateEmpty(v interface{}) bool {
	if v == nil {
		return true
	}
	switch val := v.(type) {
	case string:
		return strings.TrimSpace(val) == ""
	case []interface{}:
		return len(val) == 0
	case map[string]interface{}:
		return len(val) == 0
	}
	return false
}

func getNestedValue(data map[string]interface{}, path string) interface{} {
	if data == nil {
		return nil
	}
	parts := strings.Split(path, ".")
	var current interface{} = data
	for _, part := range parts {
		obj, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		v, ok := obj[part]
		if !ok {
			return nil
		}
		current = v
	}
	return current
}

func toInt(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case int32:
		return int(val)
	case int64:
		return int(val)
	case float64:
		return int(val)
	case float32:
		return int(val)
	case json.Number:
		i, _ := val.Int64()
		return int(i)
	case string:
		val = strings.TrimSpace(val)
		if val == "" {
			return 0
		}
		var i int
		_, _ = fmt.Sscanf(val, "%d", &i)
		return i
	default:
		return 0
	}
}

func boolString(v interface{}) string {
	switch val := v.(type) {
	case bool:
		if val {
			return "true"
		}
	case string:
		if strings.EqualFold(strings.TrimSpace(val), "true") {
			return "true"
		}
	case int:
		if val != 0 {
			return "true"
		}
	case float64:
		if val != 0 {
			return "true"
		}
	}
	return "false"
}

func shellQuote(v interface{}) string {
	s := fmt.Sprintf("%v", v)
	s = strings.ReplaceAll(s, `'`, `'"'"'`)
	return "'" + s + "'"
}

func cloneMap(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		out := make(map[string]interface{}, len(input))
		for k, v := range input {
			out[k] = v
		}
		return out
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		out = make(map[string]interface{}, len(input))
		for k, v := range input {
			out[k] = v
		}
	}
	return out
}
