package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelfHostLoopScriptContract(t *testing.T) {
	path := filepath.Join("..", "..", "scripts", "self-host-loop.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read self-host script: %v", err)
	}
	content := string(data)

	required := []string{
		"go build -o bin/simug ./cmd/simug",
		"./bin/simug run --once",
		".simug/selfhost",
	}
	for _, needle := range required {
		if !strings.Contains(content, needle) {
			t.Fatalf("missing %q in self-host script", needle)
		}
	}
}
