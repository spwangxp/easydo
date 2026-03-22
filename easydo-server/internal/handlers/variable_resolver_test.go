package handlers

import (
	"easydo-server/internal/models"
	"testing"
)

func TestVariableResolver_ResolveVariables(t *testing.T) {
	resolver := NewVariableResolver()

	// Set up test data
	resolver.SetTaskOutput("node1", map[string]interface{}{
		"commit_id":       "abc123",
		"short_commit_id": "abc123d",
		"branch":          "main",
	})

	resolver.SetEnvVars(map[string]string{
		"BUILD_NUMBER": "42",
		"CI":           "true",
	})

	resolver.SetInputs(map[string]interface{}{
		"repository_url": "git@github.com:test/repo.git",
	})

	resolver.SetSecrets(map[string]string{
		"api_key": "secret123",
	})

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "output variable",
			input:    "Commit: ${outputs.node1.commit_id}",
			expected: "Commit: abc123",
		},
		{
			name:     "env variable",
			input:    "Build #${env.BUILD_NUMBER}",
			expected: "Build #42",
		},
		{
			name:     "input variable",
			input:    "Repo: ${inputs.repository_url}",
			expected: "Repo: git@github.com:test/repo.git",
		},
		{
			name:     "secret variable",
			input:    "Key: ${secrets.api_key}",
			expected: "Key: secret123",
		},
		{
			name:     "multiple variables",
			input:    "Build #${env.BUILD_NUMBER} commit ${outputs.node1.short_commit_id}",
			expected: "Build #42 commit abc123d",
		},
		{
			name:     "no variables",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := resolver.ResolveVariables(tt.input)
			if err != nil {
				t.Errorf("ResolveVariables() error = %v", err)
				return
			}
			if result != tt.expected {
				t.Errorf("ResolveVariables() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestVariableResolver_ResolveNodeConfig(t *testing.T) {
	resolver := NewVariableResolver()

	resolver.SetTaskOutput("git_clone", map[string]interface{}{
		"commit_id":       "abc123def",
		"short_commit_id": "abc123d",
	})

	resolver.SetEnvVars(map[string]string{
		"BUILD_NUMBER": "100",
	})

	config := map[string]interface{}{
		"image_name": "myapp",
		"image_tag":  "${outputs.git_clone.short_commit_id}",
		"build_args": map[string]interface{}{
			"COMMIT_ID":    "${outputs.git_clone.commit_id}",
			"BUILD_NUMBER": "${env.BUILD_NUMBER}",
		},
	}

	resolved, err := resolver.ResolveNodeConfig(config)
	if err != nil {
		t.Fatalf("ResolveNodeConfig() error = %v", err)
	}

	// Check image_tag
	if tag, ok := resolved["image_tag"].(string); !ok || tag != "abc123d" {
		t.Errorf("image_tag = %v, expected abc123d", tag)
	}

	// Check build_args
	if buildArgs, ok := resolved["build_args"].(map[string]interface{}); ok {
		if commitID, ok := buildArgs["COMMIT_ID"].(string); !ok || commitID != "abc123def" {
			t.Errorf("COMMIT_ID = %v, expected abc123def", commitID)
		}
		if buildNum, ok := buildArgs["BUILD_NUMBER"].(string); !ok || buildNum != "100" {
			t.Errorf("BUILD_NUMBER = %v, expected 100", buildNum)
		}
	} else {
		t.Error("build_args not resolved properly")
	}
}

func TestVariableResolver_ResolveNodeConfigPreservesIntegerLikeNumericInputs(t *testing.T) {
	resolver := NewVariableResolver()
	resolver.SetInputs(map[string]interface{}{
		"port":         float64(8000),
		"max_num_seqs": float64(2),
		"gpu_memory":   float64(0.9),
	})

	config := map[string]interface{}{
		"port":       "${inputs.port}",
		"max_num":    "${inputs.max_num_seqs}",
		"gpu_memory": "${inputs.gpu_memory}",
	}

	resolved, err := resolver.ResolveNodeConfig(config)
	if err != nil {
		t.Fatalf("ResolveNodeConfig() error = %v", err)
	}
	if got := resolved["port"]; got != "8000" {
		t.Fatalf("port = %v, expected 8000", got)
	}
	if got := resolved["max_num"]; got != "2" {
		t.Fatalf("max_num = %v, expected 2", got)
	}
	if got := resolved["gpu_memory"]; got != "0.9" {
		t.Fatalf("gpu_memory = %v, expected 0.9", got)
	}
}

func TestOutputExtractor_ExtractOutputs(t *testing.T) {
	tests := []struct {
		name        string
		stdout      string
		stderr      string
		extractions []OutputExtractionConfig
		expected    map[string]interface{}
		expectErr   bool
	}{
		{
			name:   "extract version from stdout",
			stdout: "Version: 1.2.3\nBuild completed",
			extractions: []OutputExtractionConfig{
				{
					Field:  "version",
					Regex:  `Version:\s*([\d.]+)`,
					Source: "stdout",
				},
			},
			expected: map[string]interface{}{
				"version": "1.2.3",
			},
			expectErr: false,
		},
		{
			name:   "extract multiple fields",
			stdout: "Version: 1.0.0\nTests: 100 passed, 0 failed",
			extractions: []OutputExtractionConfig{
				{
					Field:  "version",
					Regex:  `Version:\s*([\d.]+)`,
					Source: "stdout",
				},
				{
					Field:  "test_passed",
					Regex:  `Tests:\s*(\d+)\s+passed`,
					Source: "stdout",
				},
			},
			expected: map[string]interface{}{
				"version":     "1.0.0",
				"test_passed": "100",
			},
			expectErr: false,
		},
		{
			name:   "required extraction not found",
			stdout: "No version here",
			extractions: []OutputExtractionConfig{
				{
					Field:    "version",
					Regex:    `Version:\s*([\d.]+)`,
					Source:   "stdout",
					Required: true,
				},
			},
			expected:  nil,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extractor := &OutputExtractor{
				Extractions: tt.extractions,
			}

			outputs, err := extractor.ExtractOutputs(tt.stdout, tt.stderr)

			if tt.expectErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ExtractOutputs() unexpected error = %v", err)
				return
			}

			for key, expectedVal := range tt.expected {
				actualVal, exists := outputs[key]
				if !exists {
					t.Errorf("Missing output key: %s", key)
					continue
				}
				if actualVal != expectedVal {
					t.Errorf("Output %s = %v, expected %v", key, actualVal, expectedVal)
				}
			}
		})
	}
}

func TestConvertToString(t *testing.T) {
	tests := []struct {
		input    interface{}
		expected string
	}{
		{"hello", "hello"},
		{42, "42"},
		{int64(123), "123"},
		{float64(8000), "8000"},
		{3.14, "3.14"},
		{true, "true"},
		{[]int{1, 2, 3}, "[1 2 3]"},
	}

	for _, tt := range tests {
		result := convertToString(tt.input)
		if result != tt.expected {
			t.Errorf("convertToString(%v) = %v, expected %v", tt.input, result, tt.expected)
		}
	}
}

func TestBuildGlobalEnvVars(t *testing.T) {
	pipeline := &models.Pipeline{
		Name:        "Test Pipeline",
		Description: "Test Description",
	}
	run := &models.PipelineRun{
		BaseModel:   models.BaseModel{ID: 123},
		BuildNumber: 42,
	}

	envVars := BuildGlobalEnvVars(pipeline, run)

	if envVars["CI"] != "true" {
		t.Error("CI should be true")
	}
	if envVars["EASYDO"] != "true" {
		t.Error("EASYDO should be true")
	}
	if envVars["PIPELINE_NAME"] != "Test Pipeline" {
		t.Error("PIPELINE_NAME mismatch")
	}
	if envVars["BUILD_NUMBER"] != "42" {
		t.Error("BUILD_NUMBER should be 42")
	}
	if envVars["BUILD_TAG"] != "build-123" {
		t.Error("BUILD_TAG should be build-123")
	}
}
