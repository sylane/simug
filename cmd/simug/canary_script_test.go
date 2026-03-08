package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRealCodexCanaryScriptContract(t *testing.T) {
	path := filepath.Join("..", "..", "scripts", "canary-real-codex-protocol.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read canary script: %v", err)
	}
	content := string(data)

	required := []string{
		"SIMUG_REAL_CODEX=1",
		"SIMUG_REAL_CODEX_CMD",
		"SIMUG_CANARY_OUT_DIR",
		"go test ./internal/agent -run TestRealCodexProtocolConformanceCanary",
	}
	for _, needle := range required {
		if !strings.Contains(content, needle) {
			t.Fatalf("missing %q in canary script", needle)
		}
	}
}
