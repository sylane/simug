package main

import "testing"

func TestParseRunArgs(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantOnce  bool
		wantHelp  bool
		expectErr bool
	}{
		{name: "default", args: nil, wantOnce: false, wantHelp: false},
		{name: "once", args: []string{"--once"}, wantOnce: true, wantHelp: false},
		{name: "help", args: []string{"--help"}, wantOnce: false, wantHelp: true},
		{name: "unknown", args: []string{"--bad"}, expectErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotOnce, gotHelp, err := parseRunArgs(tc.args)
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRunArgs returned error: %v", err)
			}
			if gotOnce != tc.wantOnce {
				t.Fatalf("once=%v, want %v", gotOnce, tc.wantOnce)
			}
			if gotHelp != tc.wantHelp {
				t.Fatalf("help=%v, want %v", gotHelp, tc.wantHelp)
			}
		})
	}
}

func TestRunMainUsagePaths(t *testing.T) {
	if code := runMain([]string{"help"}); code != exitSuccess {
		t.Fatalf("help exit code=%d, want %d", code, exitSuccess)
	}
	if code := runMain([]string{"unknown"}); code != exitUsage {
		t.Fatalf("unknown exit code=%d, want %d", code, exitUsage)
	}
	if code := runMain([]string{"run", "--help"}); code != exitSuccess {
		t.Fatalf("run --help exit code=%d, want %d", code, exitSuccess)
	}
}
