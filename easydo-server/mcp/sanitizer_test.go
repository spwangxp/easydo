package mcp

import (
	"strings"
	"testing"
)

func TestSanitizeSummaryRedactsSecretsAndTruncates(t *testing.T) {
	long := strings.Repeat("x", MaxOutputStringChars+25)
	input := map[string]any{
		"token":        "top-secret-token",
		"nested":       map[string]any{"Authorization": "Bearer abc", "note": long},
		"items":        []any{1, 2, 3},
		"many":         makeSequence(MaxOutputListItems + 5),
		"safe_message": "visible",
	}

	sanitized := SanitizeSummary(input)
	root, ok := sanitized.(map[string]any)
	if !ok {
		t.Fatalf("sanitized type=%T, want map[string]any", sanitized)
	}
	if root["token"] != "[REDACTED]" {
		t.Fatalf("token=%v, want [REDACTED]", root["token"])
	}

	nested, ok := root["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested type=%T, want map[string]any", root["nested"])
	}
	if nested["Authorization"] != "[REDACTED]" {
		t.Fatalf("nested authorization=%v, want [REDACTED]", nested["Authorization"])
	}

	note, ok := nested["note"].(string)
	if !ok {
		t.Fatalf("nested.note type=%T, want string", nested["note"])
	}
	if len([]rune(note)) > MaxOutputStringChars {
		t.Fatalf("len(note)=%d, want <= %d", len([]rune(note)), MaxOutputStringChars)
	}
	if !strings.Contains(note, "truncated") {
		t.Fatalf("note=%q, want truncation marker", note)
	}

	many, ok := root["many"].([]any)
	if !ok {
		t.Fatalf("many type=%T, want []any", root["many"])
	}
	if len(many) != MaxOutputListItems {
		t.Fatalf("len(many)=%d, want %d", len(many), MaxOutputListItems)
	}
	last, ok := many[len(many)-1].(string)
	if !ok {
		t.Fatalf("last type=%T, want string marker", many[len(many)-1])
	}
	if !strings.Contains(last, "truncated") {
		t.Fatalf("last=%q, want truncation marker", last)
	}
}

func makeSequence(n int) []any {
	items := make([]any, n)
	for i := 0; i < n; i++ {
		items[i] = i
	}
	return items
}
