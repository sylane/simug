package app

import (
	"simug/internal/github"
	"simug/internal/state"
)

func (o *orchestrator) clearActivePRState() {
	o.state.ActivePR = 0
	o.state.ActiveBranch = ""
	o.state.ActiveTaskRef = ""
}

func (o *orchestrator) clearBootstrapContext() {
	o.state.PendingTaskID = ""
	o.state.IssueTaskIntent = nil
	o.state.BootstrapIntent = nil
	o.state.BootstrapSessionID = ""
}

func (o *orchestrator) enterManagedPRMode(pr github.PullRequest) {
	if o.state.ActivePR != 0 && o.state.ActivePR != pr.Number {
		o.state.ActiveTaskRef = ""
	}
	o.state.Mode = state.ModeManagedPR
	o.state.ActivePR = pr.Number
	o.state.ActiveBranch = pr.HeadRefName
	o.state.ActiveIssue = 0
	o.clearBootstrapContext()
}

func (o *orchestrator) transitionToIssueTriageMode() {
	o.clearActivePRState()
	o.state.Mode = state.ModeIssueTriage
	o.state.ActiveIssue = 0
	o.clearBootstrapContext()
}
