package handlers

import (
	"strings"
	"testing"

	"easydo-server/internal/models"
)

func TestValidateTaskTypes(t *testing.T) {
	validCfg := PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{
			{ID: "1", Type: "git_clone"},
			{ID: "2", Type: "npm"},
			{ID: "3", Type: "webhook"},
		},
		Edges: []PipelineEdge{
			{From: "1", To: "2"},
			{From: "2", To: "3"},
		},
	}

	if ok, msg := validCfg.ValidateTaskTypes(); !ok {
		t.Fatalf("expected valid task types, got error: %s", msg)
	}

	invalidCfg := PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{
			{ID: "1", Type: "parallel"},
		},
	}

	ok, msg := invalidCfg.ValidateTaskTypes()
	if ok {
		t.Fatalf("expected invalid task types")
	}
	if !strings.Contains(msg, "不支持的任务类型") {
		t.Fatalf("expected unsupported task type error, got: %s", msg)
	}
}

func TestGetPipelineTaskDefinitionWithAlias(t *testing.T) {
	canonical, def, ok := getPipelineTaskDefinition("github")
	if !ok {
		t.Fatalf("expected github alias to be supported")
	}
	if canonical != "git_clone" {
		t.Fatalf("expected canonical type git_clone, got: %s", canonical)
	}
	if def.ExecMode != taskExecModeAgent {
		t.Fatalf("expected agent exec mode")
	}

	canonical, def, ok = getPipelineTaskDefinition("dingtalk")
	if !ok {
		t.Fatalf("expected dingtalk alias to be supported")
	}
	if canonical != "webhook" {
		t.Fatalf("expected canonical type webhook, got: %s", canonical)
	}
	if def.ExecMode != taskExecModeServer {
		t.Fatalf("expected server exec mode")
	}
}

func TestRenderPipelineAgentScript(t *testing.T) {
	_, script, err := renderPipelineAgentScript("git_clone", map[string]interface{}{
		"repository": map[string]interface{}{
			"url":        "https://example.com/repo.git",
			"branch":     "main",
			"target_dir": "./src",
		},
	})
	if err != nil {
		t.Fatalf("expected git_clone script render success, got err: %v", err)
	}
	if !strings.Contains(script, "git clone") {
		t.Fatalf("expected script to contain git clone, got: %s", script)
	}
	if !strings.Contains(script, "./src") {
		t.Fatalf("expected script to contain target directory")
	}

	_, _, err = renderPipelineAgentScript("shell", map[string]interface{}{
		"script": "echo hello",
	})
	if err != nil {
		t.Fatalf("expected shell script render success, got err: %v", err)
	}

	_, _, err = renderPipelineAgentScript("unsupported_type", map[string]interface{}{})
	if err == nil {
		t.Fatalf("expected unsupported type render error")
	}
}

func TestNormalizePipelineNodeConfig(t *testing.T) {
	normalized := normalizePipelineNodeConfig("github", "git_clone", map[string]interface{}{
		"url":        "https://example.com/repo.git",
		"branch":     "main",
		"workingDir": "./work",
		"env":        `{"A":"B"}`,
	})

	repo, ok := normalized["repository"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected repository map in normalized config")
	}
	if repo["url"] != "https://example.com/repo.git" {
		t.Fatalf("expected normalized repository.url")
	}
	if normalized["working_dir"] != "./work" {
		t.Fatalf("expected working_dir mapped from workingDir")
	}
	env, ok := normalized["env"].(map[string]interface{})
	if !ok || env["A"] != "B" {
		t.Fatalf("expected env json to be parsed, got: %#v", normalized["env"])
	}
}

func TestResolveTaskMaxRetries(t *testing.T) {
	tests := []struct {
		name     string
		config   map[string]interface{}
		expected int
	}{
		{
			name:     "nil config",
			config:   nil,
			expected: 0,
		},
		{
			name:     "missing retry_count",
			config:   map[string]interface{}{},
			expected: 0,
		},
		{
			name: "retry_count explicit zero",
			config: map[string]interface{}{
				"retry_count": 0,
			},
			expected: 0,
		},
		{
			name: "retry_count positive",
			config: map[string]interface{}{
				"retry_count": 2,
			},
			expected: 2,
		},
		{
			name: "retry_count negative",
			config: map[string]interface{}{
				"retry_count": -1,
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveTaskMaxRetries(tt.config)
			if got != tt.expected {
				t.Fatalf("expected max retries=%d, got=%d", tt.expected, got)
			}
		})
	}
}

func TestTaskCredentialSlots(t *testing.T) {
	_, gitDef, ok := getPipelineTaskDefinition("git_clone")
	if !ok {
		t.Fatalf("expected git_clone definition")
	}
	repoSlot, slotOK := gitDef.findCredentialSlot("repo_auth")
	if !slotOK {
		t.Fatalf("expected git_clone to define repo_auth slot")
	}
	if repoSlot.Required {
		t.Fatalf("expected repo_auth to be optional")
	}
	if !repoSlot.allowsType(models.TypeSSHKey) || !repoSlot.allowsType(models.TypeToken) {
		t.Fatalf("expected repo_auth to allow SSH_KEY and TOKEN")
	}
	if repoSlot.allowsType(models.TypeCert) {
		t.Fatalf("repo_auth should not allow CERTIFICATE")
	}

	_, emailDef, ok := getPipelineTaskDefinition("email")
	if !ok {
		t.Fatalf("expected email definition")
	}
	smtpSlot, slotOK := emailDef.findCredentialSlot("smtp_auth")
	if !slotOK {
		t.Fatalf("expected email to define smtp_auth slot")
	}
	if !smtpSlot.allowsCategory(models.CategoryEmail) {
		t.Fatalf("smtp_auth should allow email category")
	}
	if smtpSlot.allowsCategory(models.CategoryKubernetes) {
		t.Fatalf("smtp_auth should not allow kubernetes category")
	}

	_, webhookDef, ok := getPipelineTaskDefinition("webhook")
	if !ok {
		t.Fatalf("expected webhook definition")
	}
	mtlsSlot, slotOK := webhookDef.findCredentialSlot("webhook_mtls")
	if !slotOK {
		t.Fatalf("expected webhook_mtls slot")
	}
	if !mtlsSlot.allowsType(models.TypeCert) {
		t.Fatalf("webhook_mtls should allow certificate type")
	}

	_, kubeDef, ok := getPipelineTaskDefinition("kubernetes")
	if !ok {
		t.Fatalf("expected kubernetes definition")
	}
	clusterSlot, slotOK := kubeDef.findCredentialSlot("cluster_auth")
	if !slotOK {
		t.Fatalf("expected cluster_auth slot")
	}
	if !clusterSlot.allowsType(models.TypeToken) || !clusterSlot.allowsType(models.TypeCert) {
		t.Fatalf("cluster_auth should allow TOKEN and CERTIFICATE")
	}
	if clusterSlot.allowsType(models.TypeIAM) {
		t.Fatalf("cluster_auth should not allow IAM_ROLE without runtime support")
	}
	if clusterSlot.allowsCategory(models.CategoryAWS) || clusterSlot.allowsCategory(models.CategoryGCP) || clusterSlot.allowsCategory(models.CategoryAzure) {
		t.Fatalf("cluster_auth should not allow cloud-provider categories without runtime support")
	}

	_, sshDef, ok := getPipelineTaskDefinition("ssh")
	if !ok {
		t.Fatalf("expected ssh definition")
	}
	sshSlot, slotOK := sshDef.findCredentialSlot("ssh_auth")
	if !slotOK {
		t.Fatalf("expected ssh_auth slot")
	}
	if !sshSlot.allowsType(models.TypeSSHKey) {
		t.Fatalf("ssh_auth should allow SSH_KEY")
	}
	if !sshSlot.allowsType(models.TypePassword) {
		t.Fatalf("ssh_auth should allow PASSWORD")
	}
	if !sshSlot.allowsCategory(models.CategoryGitHub) || !sshSlot.allowsCategory(models.CategoryCustom) {
		t.Fatalf("ssh_auth should not restrict SSH credentials by category")
	}
}

func TestRenderPipelineAgentScript_SSHPasswordCredentialIntegration(t *testing.T) {
	_, script, err := renderPipelineAgentScript("ssh", map[string]interface{}{
		"host":   "10.0.0.8",
		"script": "echo ok",
	})
	if err != nil {
		t.Fatalf("expected ssh script render success, got err: %v", err)
	}
	if !strings.Contains(script, "EASYDO_CRED_SSH_AUTH_PASSWORD") {
		t.Fatalf("expected ssh script to support password auth env")
	}
	if !strings.Contains(script, "EASYDO_CRED_SSH_AUTH_USERNAME") {
		t.Fatalf("expected ssh script to support username auth env")
	}
	if !strings.Contains(script, "sshpass -e ssh") {
		t.Fatalf("expected ssh script to use sshpass for password auth")
	}
}

func TestRenderPipelineAgentScript_KubernetesCredentialIntegration(t *testing.T) {
	_, script, err := renderPipelineAgentScript("kubernetes", map[string]interface{}{
		"manifest": "./k8s/deploy.yaml",
	})
	if err != nil {
		t.Fatalf("expected kubernetes script render success, got err: %v", err)
	}
	if !strings.Contains(script, "EASYDO_CRED_CLUSTER_AUTH_KUBECONFIG") {
		t.Fatalf("expected script to use cluster auth kubeconfig env")
	}
	if !strings.Contains(script, "EASYDO_CRED_CLUSTER_AUTH_TOKEN") {
		t.Fatalf("expected script to use cluster auth token env")
	}
}

func TestRenderPipelineAgentScript_DockerRunCredentialIntegration(t *testing.T) {
	_, script, err := renderPipelineAgentScript("docker-run", map[string]interface{}{
		"host":       "10.0.0.8",
		"user":       "root",
		"image_name": "app",
		"image_tag":  "latest",
	})
	if err != nil {
		t.Fatalf("expected docker-run script render success, got err: %v", err)
	}
	if !strings.Contains(script, "EASYDO_CRED_REGISTRY_AUTH_USERNAME") {
		t.Fatalf("expected docker-run script to use registry auth username env")
	}
	if !strings.Contains(script, "EASYDO_CRED_SSH_AUTH_PASSWORD") {
		t.Fatalf("expected docker-run script to use ssh auth password env")
	}
	if !strings.Contains(script, "sshpass -e ssh") {
		t.Fatalf("expected docker-run script to execute over ssh")
	}
	if !strings.Contains(script, "for candidate in docker podman nerdctl") {
		t.Fatalf("expected docker-run script to auto-detect remote runtime")
	}
	if !strings.Contains(script, "RUN_ARGS='-d -p 18080:80'") && strings.Contains(script, "-d -p 18080:80") {
		t.Fatalf("expected docker-run run_args assignment to be shell-quoted, got script=%s", script)
	}
}

func TestRenderPipelineAgentScript_DockerRunEncodesRemoteArguments(t *testing.T) {
	_, script, err := renderPipelineAgentScript("docker-run", map[string]interface{}{
		"host":       "10.0.0.8",
		"user":       "root",
		"image_name": "nginx",
		"image_tag":  "alpine",
		"run_args":   "-d -p 18080:80",
	})
	if err != nil {
		t.Fatalf("expected docker-run script render success, got err: %v", err)
	}
	if strings.Contains(script, "sh -s -- \"$RUNTIME_HINT\" \"$IMAGE_REF\" \"$CONTAINER_NAME\" \"$RUN_ARGS\"") {
		t.Fatalf("expected docker-run script to avoid positional ssh args for run_args, got script=%s", script)
	}
	if !strings.Contains(script, "EASYDO_REMOTE_RUN_ARGS_B64=") {
		t.Fatalf("expected docker-run script to encode run_args before ssh transport")
	}
	if !strings.Contains(script, "decode_easydo_b64()") {
		t.Fatalf("expected docker-run script to decode remote arguments from base64 payload")
	}
}

func TestRenderPipelineAgentScript_DockerRunDetachesAndChecksStability(t *testing.T) {
	_, script, err := renderPipelineAgentScript("docker-run", map[string]interface{}{
		"host":       "10.0.0.8",
		"user":       "root",
		"image_name": "nginx",
		"image_tag":  "alpine",
	})
	if err != nil {
		t.Fatalf("expected docker-run script render success, got err: %v", err)
	}
	if strings.Contains(script, `run --rm "$IMAGE_REF"`) || strings.Contains(script, `run --rm $RUN_ARGS`) {
		t.Fatalf("expected docker-run script to avoid foreground --rm execution, got script=%s", script)
	}
	if !strings.Contains(script, `run -d`) {
		t.Fatalf("expected docker-run script to start container in detached mode, got script=%s", script)
	}
	if !strings.Contains(script, `sleep 10`) {
		t.Fatalf("expected docker-run script to wait 10 seconds before success, got script=%s", script)
	}
	if !strings.Contains(script, `RestartCount`) {
		t.Fatalf("expected docker-run script to inspect restart count, got script=%s", script)
	}
	if !strings.Contains(script, `docker-run container did not stay running for 10s`) {
		t.Fatalf("expected docker-run script to fail on early exit/restart, got script=%s", script)
	}
}

func TestRenderPipelineAgentScript_DockerRunFailsOnDuplicateContainerName(t *testing.T) {
	_, script, err := renderPipelineAgentScript("docker-run", map[string]interface{}{
		"host":           "10.0.0.8",
		"user":           "root",
		"image_name":     "nginx",
		"image_tag":      "alpine",
		"container_name": "existing-nginx",
	})
	if err != nil {
		t.Fatalf("expected docker-run script render success, got err: %v", err)
	}
	if strings.Contains(script, `rm -f "$CONTAINER_NAME"`) {
		t.Fatalf("expected docker-run script to preserve existing same-name containers, got script=%s", script)
	}
	if !strings.Contains(script, `inspect "$CONTAINER_NAME" >/dev/null 2>&1`) {
		t.Fatalf("expected docker-run script to check for duplicate container names before start, got script=%s", script)
	}
	if !strings.Contains(script, `container name already exists: $CONTAINER_NAME`) {
		t.Fatalf("expected docker-run script to emit duplicate container-name error, got script=%s", script)
	}
}

func TestRenderPipelineAgentScript_GitCloneCredentialIntegration(t *testing.T) {
	_, script, err := renderPipelineAgentScript("git_clone", map[string]interface{}{
		"repository": map[string]interface{}{
			"url": "https://example.com/repo.git",
		},
	})
	if err != nil {
		t.Fatalf("expected git_clone script render success, got err: %v", err)
	}
	if !strings.Contains(script, "EASYDO_CRED_REPO_AUTH_ACCESS_TOKEN") {
		t.Fatalf("expected git_clone script to support access_token fallback")
	}
	if !strings.Contains(script, "EASYDO_CRED_REPO_AUTH_PASSWORD") {
		t.Fatalf("expected git_clone script to support password auth env")
	}
	if !strings.Contains(script, "EASYDO_CRED_REPO_AUTH_USERNAME") {
		t.Fatalf("expected git_clone script to support username env")
	}
	if !strings.Contains(script, "--progress") {
		t.Fatalf("expected git_clone script to force visible progress output")
	}
	if !strings.Contains(script, "[easydo][cmd]") {
		t.Fatalf("expected git_clone script to emit structured command logs")
	}
}

func TestRenderPipelineAgentScript_CustomShellLogsSanitizedPreview(t *testing.T) {
	_, script, err := renderPipelineAgentScript("shell", map[string]interface{}{
		"script": "curl -H 'Authorization: Bearer super-secret-token' https://example.com && echo password=hunter2",
	})
	if err != nil {
		t.Fatalf("expected shell script render success, got err: %v", err)
	}
	if !strings.Contains(script, "[easydo][cmd]") {
		t.Fatalf("expected shell script preview log to be rendered")
	}
	if !strings.Contains(script, "Authorization: Bearer ***") {
		t.Fatalf("expected authorization header to be masked in script preview")
	}
	if !strings.Contains(script, "password=***") {
		t.Fatalf("expected password assignment to be masked in script preview")
	}
}
