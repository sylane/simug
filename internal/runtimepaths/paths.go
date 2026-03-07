package runtimepaths

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	CurrentDirName = ".simug"
)

// ResolveDataDir returns the runtime data directory path.
func ResolveDataDir(repoRoot string) (string, error) {
	current := filepath.Join(repoRoot, CurrentDirName)

	if st, err := os.Stat(current); err == nil {
		if !st.IsDir() {
			return "", fmt.Errorf("runtime path %s exists but is not a directory", current)
		}
		return current, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat runtime path %s: %w", current, err)
	}

	return current, nil
}

func EnsureDataDir(repoRoot string) (string, error) {
	dir, err := ResolveDataDir(repoRoot)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create runtime dir %s: %w", dir, err)
	}
	return dir, nil
}
