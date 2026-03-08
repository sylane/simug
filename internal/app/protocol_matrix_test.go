package app

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"simug/internal/agent"
	"simug/internal/git"
	"simug/internal/state"
)

func TestRunAgentWithValidationProtocolMatrix(t *testing.T) {
	tmp := t.TempDir()
	expectedBranch := "agent/20260307-120000-next-task"
	beforeHead := "abcdef"

	restoreGit := git.SetCommandRunnerForTest(mockCommandRunner{responses: map[string]string{
		commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"): expectedBranch + "\n",
		commandKey("git", "status", "--porcelain"):             "\n",
		commandKey("git", "rev-parse", "HEAD"):                 beforeHead + "\n",
	}})
	defer restoreGit()

	tests := []struct {
		name         string
		agentCmd     string
		wantErr      string
		wantErrCause string
		wantTerminal agent.ActionType
		wantActions  int
	}{
		{
			name:         "valid mixed stdout",
			agentCmd:     `printf 'thinking...\nSIMUG: {"action":"comment","body":"hi"}\nnoise\nSIMUG: {"action":"done","summary":"ok","changes":false}\n'`,
			wantErr:      "",
			wantTerminal: agent.ActionDone,
			wantActions:  2,
		},
		{
			name:         "malformed json protocol line",
			agentCmd:     `printf 'note\nSIMUG: {bad-json}\n'`,
			wantErr:      "execution/protocol errors",
			wantErrCause: "invalid json",
		},
		{
			name:         "missing terminal action",
			agentCmd:     `printf 'SIMUG: {"action":"comment","body":"only-comment"}\n'`,
			wantErr:      "execution/protocol errors",
			wantErrCause: "exactly one terminal action",
		},
		{
			name:         "multiple terminal actions",
			agentCmd:     `printf 'SIMUG: {"action":"done","summary":"ok","changes":false}\nSIMUG: {"action":"idle","reason":"x"}\n'`,
			wantErr:      "execution/protocol errors",
			wantErrCause: "exactly one terminal action",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := orchestrator{
				repoRoot: tmp,
				state:    &state.State{Mode: state.ModeManagedPR},
				cfg: config{
					MainBranch:        "main",
					BranchPattern:     regexp.MustCompile("^" + regexp.QuoteMeta(expectedBranch) + "$"),
					MaxRepairAttempts: 0,
				},
				runner:  agent.Runner{Command: tc.agentCmd},
				runID:   "run-test",
				tickSeq: 1,
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			result, afterHead, err := o.runAgentWithValidation(ctx, "prompt", expectedBranch, beforeHead, false, false, nil)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error=%q, want contains %q", err.Error(), tc.wantErr)
				}
				if tc.wantErrCause != "" && !strings.Contains(err.Error(), tc.wantErrCause) {
					t.Fatalf("error=%q, want contains %q", err.Error(), tc.wantErrCause)
				}
				return
			}

			if err != nil {
				t.Fatalf("runAgentWithValidation returned error: %v", err)
			}
			if result.Terminal.Type != tc.wantTerminal {
				t.Fatalf("terminal=%q, want %q", result.Terminal.Type, tc.wantTerminal)
			}
			if len(result.Actions) != tc.wantActions {
				t.Fatalf("actions=%d, want %d", len(result.Actions), tc.wantActions)
			}
			if afterHead != beforeHead {
				t.Fatalf("afterHead=%q, want %q", afterHead, beforeHead)
			}
		})
	}
}
