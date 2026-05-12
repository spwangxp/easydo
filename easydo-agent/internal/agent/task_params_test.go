package agent

import "testing"

func TestTaskParseParams_DecodesStructuredParamsJSON(t *testing.T) {
	task := &Task{
		ID:       21,
		TaskType: "docker",
		Params:   `{"image_name":"demo/app","image_tag":"v1","push":true}`,
		EnvVars:  `{"CI":true}`,
	}

	params, err := task.ParseParams()
	if err != nil {
		t.Fatalf("parse params failed: %v", err)
	}
	if params.TaskType != "docker" {
		t.Fatalf("task type=%s, want docker", params.TaskType)
	}
	if params.Params["image_name"] != "demo/app" {
		t.Fatalf("image_name=%v, want demo/app", params.Params["image_name"])
	}
	if params.Params["push"] != true {
		t.Fatalf("push=%v, want true", params.Params["push"])
	}
	if params.EnvVars["CI"] != "true" {
		t.Fatalf("CI env=%v, want true", params.EnvVars["CI"])
	}
}

func TestTaskParseParams_ExtractsShellFromStructuredParams(t *testing.T) {
	task := &Task{
		ID:       22,
		TaskType: "shell",
		Params:   `{"shell":"bash","script":"for i in {1..3}; do echo $i; done"}`,
	}

	params, err := task.ParseParams()
	if err != nil {
		t.Fatalf("parse params failed: %v", err)
	}
	if params.Shell != "bash" {
		t.Fatalf("shell=%q, want bash", params.Shell)
	}
}
