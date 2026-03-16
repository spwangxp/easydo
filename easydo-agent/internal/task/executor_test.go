package task

import "testing"

func TestParseParams_PreservesEnvVarsWhenJSONContainsNonStringValues(t *testing.T) {
	params, err := ParseParams(map[string]interface{}{
		"id":       float64(1),
		"env_vars": `{"EASYDO_CRED_REPO_AUTH_PASSWORD":"secret","CI":true,"DEPTH":2}`,
	})
	if err != nil {
		t.Fatalf("parse params failed: %v", err)
	}
	if params.EnvVars["EASYDO_CRED_REPO_AUTH_PASSWORD"] != "secret" {
		t.Fatalf("expected credential env to be preserved, got %#v", params.EnvVars)
	}
	if params.EnvVars["CI"] != "true" {
		t.Fatalf("expected bool env to stringify, got %#v", params.EnvVars["CI"])
	}
	if params.EnvVars["DEPTH"] != "2" {
		t.Fatalf("expected numeric env to stringify, got %#v", params.EnvVars["DEPTH"])
	}
	if len(params.EnvVars) != 3 {
		t.Fatalf("expected all env vars to survive parsing, got %#v", params.EnvVars)
	}
}
