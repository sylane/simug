package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"simug/internal/runtimepaths"
)

type eventLogger struct {
	path string
	mu   sync.Mutex
}

func newEventLogger(repoRoot string) (*eventLogger, error) {
	dir, err := runtimepaths.EnsureDataDir(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve runtime dir for event log: %w", err)
	}
	return &eventLogger{path: filepath.Join(dir, "events.log")}, nil
}

func (l *eventLogger) log(kind, message string, fields map[string]any) error {
	if l == nil {
		return nil
	}
	entry := map[string]any{
		"ts":      time.Now().UTC().Format(time.RFC3339Nano),
		"kind":    kind,
		"message": message,
	}
	if len(fields) > 0 {
		entry["fields"] = fields
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode event log entry: %w", err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open event log: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("append event log: %w", err)
	}
	return nil
}
