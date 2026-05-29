package safety

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSafetyHelpersDoNotDependOnMCPPackage(t *testing.T) {
	sourcePath := filepath.Join("json.go")
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read json.go: %v", err)
	}
	if strings.Contains(string(content), "easydo-server/mcp") {
		t.Fatal("json.go must not import easydo-server/mcp")
	}
}

func TestSanitizeJSONValueSanitizesStructFields(t *testing.T) {
	type nested struct {
		Authorization string
		Note          string `json:"note"`
	}
	type payload struct {
		Token          string
		Password       string `json:"password,omitempty"`
		DisplayName    string `json:"display_name"`
		Nested         nested `json:"nested"`
		Ignored        string `json:"-"`
		unexportedNote string
	}

	sanitized := SanitizeJSONValue(payload{
		Token:          "abc123",
		Password:       "p@ssw0rd",
		DisplayName:    "demo-user",
		Nested:         nested{Authorization: "Bearer secret", Note: "kept"},
		Ignored:        "omit me",
		unexportedNote: "private",
	}, DefaultOptions())

	got, ok := sanitized.(map[string]any)
	if !ok {
		t.Fatalf("type=%T, want map[string]any", sanitized)
	}

	want := map[string]any{
		"Token":        "[REDACTED]",
		"password":     "[REDACTED]",
		"display_name": "demo-user",
		"nested": map[string]any{
			"Authorization": "[REDACTED]",
			"note":          "kept",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sanitized=%#v, want %#v", got, want)
	}
	if _, exists := got["Ignored"]; exists {
		t.Fatal("expected json:\"-\" field to be omitted")
	}
	if _, exists := got["unexportedNote"]; exists {
		t.Fatal("expected unexported field to be omitted")
	}
}

func TestSanitizeJSONValueSanitizesStructPointers(t *testing.T) {
	type credentials struct {
		APIToken      string `json:"api_token"`
		Authorization string `json:"authorization"`
		NormalField   string `json:"normal_field"`
	}

	value := &credentials{
		APIToken:      "token-value",
		Authorization: "Bearer secret",
		NormalField:   "visible",
	}

	sanitized := SanitizeJSONValue(value, DefaultOptions())
	got, ok := sanitized.(map[string]any)
	if !ok {
		t.Fatalf("type=%T, want map[string]any", sanitized)
	}

	want := map[string]any{
		"api_token":    "[REDACTED]",
		"authorization": "[REDACTED]",
		"normal_field": "visible",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sanitized=%#v, want %#v", got, want)
	}
}

func TestSanitizeJSONValueRedactsSensitiveStructFieldWithBenignJSONTag(t *testing.T) {
	type payload struct {
		Token string `json:"value"`
		Label string `json:"label"`
	}

	sanitized := SanitizeJSONValue(payload{Token: "secret-token", Label: "visible"}, DefaultOptions())
	got, ok := sanitized.(map[string]any)
	if !ok {
		t.Fatalf("type=%T, want map[string]any", sanitized)
	}

	want := map[string]any{
		"value": "[REDACTED]",
		"label": "visible",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sanitized=%#v, want %#v", got, want)
	}
}
