package handlers

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"easydo-server/internal/models"
)

func TestGetPipelineTaskDefinitionExposeTypedContract(t *testing.T) {
	def, ok := getTaskDefinition("git_clone")
	if !ok {
		t.Fatalf("expected typed task definition for git_clone")
	}
	if def.TaskKey != "git_clone" {
		t.Fatalf("expected task_key git_clone, got %s", def.TaskKey)
	}
	if def.ExecutorType != taskExecModeAgent {
		t.Fatalf("expected agent executor type, got %s", def.ExecutorType)
	}
	if len(def.FieldsSchema) == 0 {
		t.Fatalf("expected fields schema to be populated")
	}
	if len(def.OutputsSchema) == 0 {
		t.Fatalf("expected outputs schema to be populated")
	}
	if def.ExecutionSpec.Mode != "shell_template" {
		t.Fatalf("expected shell_template mode, got %s", def.ExecutionSpec.Mode)
	}
	if def.ExecutionSpec.EnvMapping["git_ref"] != "EASYDO_INPUT_GIT_REF" {
		t.Fatalf("expected env mapping for git_ref")
	}

	repoFieldFound := false
	for _, field := range def.FieldsSchema {
		if field.Key == "git_repo_url" {
			repoFieldFound = true
			if !field.Required {
				t.Fatalf("expected git_repo_url to be required")
			}
		}
	}
	if !repoFieldFound {
		t.Fatalf("expected git_repo_url field in schema")
	}

	commitOutputFound := false
	for _, output := range def.OutputsSchema {
		if output.Key == "git_commit" {
			commitOutputFound = true
		}
	}
	if !commitOutputFound {
		t.Fatalf("expected git_commit output in schema")
	}

	if len(def.CredentialSlots) != 1 || def.CredentialSlots[0].SlotKey != "repo_auth" {
		t.Fatalf("expected typed credential slot repo_auth, got %#v", def.CredentialSlots)
	}
}

func TestGetPipelineTaskTypesResponseIncludesTypedDefinitionFields(t *testing.T) {
	handler := &PipelineHandler{}
	response := handler.listPipelineTaskTypes()
	if len(response) == 0 {
		t.Fatalf("expected non-empty task type response")
	}

	var gitClone *pipelineTaskTypeResponse
	for i := range response {
		if response[i].TaskKey == "git_clone" {
			gitClone = &response[i]
			break
		}
	}
	if gitClone == nil {
		t.Fatalf("expected git_clone in response")
	}
	if gitClone.Type != gitClone.TaskKey {
		t.Fatalf("expected type and task_key to match, got type=%s task_key=%s", gitClone.Type, gitClone.TaskKey)
	}
	if len(gitClone.FieldsSchema) == 0 {
		t.Fatalf("expected response to include fields schema")
	}
	if len(gitClone.OutputsSchema) == 0 {
		t.Fatalf("expected response to include outputs schema")
	}
	if gitClone.ExecutionSpec.Mode == "" {
		t.Fatalf("expected response to include execution spec")
	}

	encoded, err := json.Marshal(gitClone)
	if err != nil {
		t.Fatalf("marshal response failed: %v", err)
	}
	if !strings.Contains(string(encoded), "fields_schema") || !strings.Contains(string(encoded), "outputs_schema") {
		t.Fatalf("expected marshaled response to contain typed schema fields, got=%s", string(encoded))
	}
}

func TestBuildAndDeployTaskDefinitionsExposeFieldSchemas(t *testing.T) {
	for _, taskKey := range []string{"npm", "maven", "gradle", "docker", "artifact_publish", "ssh", "kubernetes", "docker-run", "email", "in_app"} {
		def, ok := getTaskDefinition(taskKey)
		if !ok {
			t.Fatalf("expected task definition for %s", taskKey)
		}
		if len(def.FieldsSchema) == 0 {
			t.Fatalf("expected %s to expose fields schema", taskKey)
		}
	}
}

func TestDockerTaskDefinitionIncludesRequiredImageField(t *testing.T) {
	def, ok := getTaskDefinition("docker")
	if !ok {
		t.Fatalf("expected docker definition")
	}
	imageNameFound := false
	pushFound := false
	for _, field := range def.FieldsSchema {
		if field.Key == "image_name" {
			imageNameFound = true
			if !field.Required {
				t.Fatalf("expected image_name to be required")
			}
		}
		if field.Key == "push" {
			pushFound = true
			if field.Type != "boolean" {
				t.Fatalf("expected push field to be boolean, got %s", field.Type)
			}
		}
	}
	if !imageNameFound || !pushFound {
		t.Fatalf("expected docker fields schema to include image_name and push, got %#v", def.FieldsSchema)
	}
}

func TestNpmTaskDefinitionIncludesCommandField(t *testing.T) {
	def, ok := getTaskDefinition("npm")
	if !ok {
		t.Fatalf("expected npm definition")
	}
	commandFound := false
	for _, field := range def.FieldsSchema {
		if field.Key == "command" {
			commandFound = true
			if !field.Required {
				t.Fatalf("expected npm command to be required")
			}
			if field.Default != "npm ci && npm run build" {
				t.Fatalf("expected npm command default to be populated, got %#v", field.Default)
			}
		}
	}
	if !commandFound {
		t.Fatalf("expected npm fields schema to include command")
	}
}

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

func TestRenderPipelineAgentScript(t *testing.T) {
	_, script, err := renderPipelineAgentScript("git_clone", map[string]interface{}{
		"git_repo_url":      "https://example.com/repo.git",
		"git_ref":           "main",
		"git_checkout_path": "./src",
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
	normalized := normalizePipelineNodeConfig("git_clone", "git_clone", map[string]interface{}{
		"git_repo_url":      "https://example.com/repo.git",
		"git_ref":           "main",
		"git_checkout_path": "./work",
		"env":               `{"A":"B"}`,
	})

	if normalized["git_repo_url"] != "https://example.com/repo.git" {
		t.Fatalf("expected git_repo_url to be preserved")
	}
	if normalized["git_ref"] != "main" {
		t.Fatalf("expected git_ref to be preserved")
	}
	if normalized["git_checkout_path"] != "./work" {
		t.Fatalf("expected git_checkout_path to be preserved")
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

func TestRenderPipelineAgentScript_KubernetesComplexCommandIsShellParsable(t *testing.T) {
	_, script, err := renderPipelineAgentScript("kubernetes", map[string]interface{}{
		"command": strings.Join([]string{
			"set -e",
			"cat <<\"EOF\" > input_params_values.yaml",
			"image:",
			"  tag: '8.0'",
			"EOF",
			"mkdir -p chart",
			"wget -O /tmp/resolved-chart.tgz --header \"X-EasyDo-Internal-Token:$EASYDO_INTERNAL_SERVER_TOKEN\" \"$EASYDO_INTERNAL_SERVER_URL/internal/store/chart-artifact?object_key=$EASYDO_CHART_OBJECT_KEY&file_name=$EASYDO_CHART_FILE_NAME\"",
			"tar -xzf /tmp/resolved-chart.tgz -C chart",
			"helm --kubeconfig $KUBECONFIG upgrade --install mysql ./chart -n middleware --create-namespace -f input_params_values.yaml",
		}, "\n"),
	})
	if err != nil {
		t.Fatalf("expected kubernetes script render success, got err: %v", err)
	}

	tempFile, err := os.CreateTemp(t.TempDir(), "k8s-script-*.sh")
	if err != nil {
		t.Fatalf("create temp script failed: %v", err)
	}
	defer tempFile.Close()
	if _, err := tempFile.WriteString(script); err != nil {
		t.Fatalf("write temp script failed: %v", err)
	}

	cmd := exec.Command("/bin/sh", "-n", tempFile.Name())
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected rendered kubernetes script to be shell-parseable, got err=%v output=%s\nscript=%s", err, string(output), script)
	}
	if !strings.Contains(script, "if [ -n \"$CMD\" ]; then") {
		t.Fatalf("expected kubernetes script to branch on quoted CMD variable, got script=%s", script)
	}
}

func TestRenderPipelineAgentScript_KubernetesComplexCommandExecutesWithoutWrapperSyntaxError(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := tmpDir + "/bin"
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin dir failed: %v", err)
	}
	writeStub := func(name, body string) {
		path := binDir + "/" + name
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatalf("write stub %s failed: %v", name, err)
		}
	}
	writeStub("kubectl", "#!/bin/sh\nexit 0\n")
	writeStub("wget", "#!/bin/sh\nwhile [ $# -gt 0 ]; do\n  if [ \"$1\" = \"-O\" ]; then\n    shift\n    outfile=\"$1\"\n  fi\n  shift\ndone\nprintf test > \"$outfile\"\n")
	writeStub("tar", "#!/bin/sh\nexit 0\n")
	writeStub("helm", "#!/bin/sh\nexit 0\n")

	_, script, err := renderPipelineAgentScript("kubernetes", map[string]interface{}{
		"command": strings.Join([]string{
			"set -e",
			"cat <<\"EOF\" > input_params_values.yaml",
			"image:",
			"  tag: '8.0'",
			"EOF",
			"mkdir -p chart",
			"wget -O /tmp/resolved-chart.tgz --header \"X-EasyDo-Internal-Token:$EASYDO_INTERNAL_SERVER_TOKEN\" \"$EASYDO_INTERNAL_SERVER_URL/internal/store/chart-artifact?object_key=$EASYDO_CHART_OBJECT_KEY&file_name=$EASYDO_CHART_FILE_NAME\"",
			"tar -xzf /tmp/resolved-chart.tgz -C chart",
			"helm --kubeconfig $KUBECONFIG upgrade --install mysql ./chart -n middleware --create-namespace -f input_params_values.yaml",
		}, "\n"),
	})
	if err != nil {
		t.Fatalf("expected kubernetes script render success, got err: %v", err)
	}

	tempFile, err := os.CreateTemp(tmpDir, "k8s-runtime-*.sh")
	if err != nil {
		t.Fatalf("create temp script failed: %v", err)
	}
	defer tempFile.Close()
	if _, err := tempFile.WriteString(script); err != nil {
		t.Fatalf("write temp script failed: %v", err)
	}

	cmd := exec.Command("/bin/sh", tempFile.Name())
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"EASYDO_INTERNAL_SERVER_URL=http://server",
		"EASYDO_INTERNAL_SERVER_TOKEN=test-token",
		"EASYDO_CHART_OBJECT_KEY=store/charts/demo.tgz",
		"EASYDO_CHART_FILE_NAME=mysql-14.0.3.tgz",
		"EASYDO_CRED_CLUSTER_AUTH_KUBECONFIG=apiVersion: v1",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected rendered kubernetes script to execute without wrapper syntax error, got err=%v output=%s\nscript=%s", err, string(output), script)
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
		"git_repo_url": "https://example.com/repo.git",
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
