package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestShouldShowHelp(t *testing.T) {
	if !shouldShowHelp([]string{"--help"}) {
		t.Fatalf("expected --help to trigger help")
	}
	if !shouldShowHelp([]string{"-h"}) {
		t.Fatalf("expected -h to trigger help")
	}
	if shouldShowHelp([]string{"--config", "foo.yaml"}) {
		t.Fatalf("unexpected help for non-help args")
	}
}

func TestWriteHelpIncludesWorkspaceIDGuidance(t *testing.T) {
	var buf bytes.Buffer
	writeHelp(&buf)
	output := buf.String()
	checks := []string{
		"workspace_id",
		"AGENT_WORKSPACE_ID",
		"--help",
		"平台型",
		"工作空间私有",
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Fatalf("help output missing %q: %s", check, output)
		}
	}
}
