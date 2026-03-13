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
			agentCmd:     `printf 'thinking...\nnoise\n'; ` + envelopedAgentCommand(`{"action":"comment","body":"hi"}`, `{"action":"done","summary":"ok","changes":false}`),
			wantErr:      "",
			wantTerminal: agent.ActionDone,
			wantActions:  2,
		},
		{
			name: "ignores echoed protocol snippets outside active envelope",
			agentCmd: strings.Join([]string{
				`printf 'SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"stale-turn"}\n'`,
				`printf 'SIMUG: {"envelope":"coordinator","event":"action","turn_id":"stale-turn","payload":{"action":"done","summary":"stale","changes":false}}\n'`,
				`printf 'SIMUG: {"envelope":"coordinator","event":"end","turn_id":"stale-turn"}\n'`,
				envelopedAgentCommand(`{"action":"done","summary":"ok","changes":false}`),
				`printf 'SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"echo-turn"}\n'`,
				`printf 'SIMUG: {"envelope":"coordinator","event":"action","turn_id":"echo-turn","payload":{"action":"idle","reason":"echo"}}\n'`,
				`printf 'SIMUG: {"envelope":"coordinator","event":"end","turn_id":"echo-turn"}\n'`,
			}, "; "),
			wantErr:      "",
			wantTerminal: agent.ActionDone,
			wantActions:  1,
		},
		{
			name:         "malformed json protocol line",
			agentCmd:     `printf 'note\n'; turn="$SIMUG_PROTOCOL_TURN_ID"; printf 'SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"%s"}\n' "$turn"; printf 'SIMUG: {bad-json}\n'; printf 'SIMUG: {"envelope":"coordinator","event":"end","turn_id":"%s"}\n' "$turn"`,
			wantErr:      "execution/protocol errors",
			wantErrCause: "parse active coordinator line",
		},
		{
			name:         "missing terminal action",
			agentCmd:     envelopedAgentCommand(`{"action":"comment","body":"only-comment"}`),
			wantErr:      "execution/protocol errors",
			wantErrCause: "exactly one terminal action",
		},
		{
			name:         "multiple terminal actions",
			agentCmd:     envelopedAgentCommand(`{"action":"done","summary":"ok","changes":false}`, `{"action":"idle","reason":"x"}`),
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

			result, afterHead, err := o.runAgentWithValidation(ctx, "prompt", expectedBranch, beforeHead, false, false, nil, "", false, nil)
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
