package runtimepaths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDataDirDefaultsToCurrent(t *testing.T) {
	tmp := t.TempDir()
	got, err := ResolveDataDir(tmp)
	if err != nil {
		t.Fatalf("ResolveDataDir returned error: %v", err)
	}
	want := filepath.Join(tmp, CurrentDirName)
	if got != want {
		t.Fatalf("ResolveDataDir = %q, want %q", got, want)
	}
}

func TestResolveDataDirUsesCurrentWhenItExists(t *testing.T) {
	tmp := t.TempDir()
	current := filepath.Join(tmp, CurrentDirName)
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatalf("mkdir current dir: %v", err)
	}

	got, err := ResolveDataDir(tmp)
	if err != nil {
		t.Fatalf("ResolveDataDir returned error: %v", err)
	}
	if got != current {
		t.Fatalf("ResolveDataDir = %q, want %q", got, current)
	}
}
