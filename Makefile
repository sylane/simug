.DEFAULT_GOAL := help

GO ?= go
GOCACHE ?= /tmp/go-build
GOEXPERIMENT ?=

BINARY ?= bin/simug
CMD_PKG ?= ./cmd/simug

ITERATIONS ?= 4
SLEEP_SECONDS ?= 2

CODEX_CMD ?= codex exec
CANARY_OUT ?= .simug/canary/real-codex
REPO ?= .
ISSUE_PR ?=
PLANNING_PR ?=
ISSUE ?=

export GOCACHE

.PHONY: help build install test cover run run-once explain-last-failure \
	selfhost-loop selfhost-canary canary-protocol canary-recovery canary-gate \
	sandbox-dry-run chaos

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*## "}; /^[a-zA-Z0-9_.-]+:.*## / {printf "%-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build simug binary to bin/simug
	$(GO) build -o $(BINARY) $(CMD_PKG)

install: ## Install simug into Go bin
	$(GO) install $(CMD_PKG)

test: ## Run all tests
	$(GO) test ./...

cover: ## Run coverage and print function summary
	GOEXPERIMENT=nocoverageredesign $(GO) test ./... -coverprofile=coverage.out
	$(GO) tool cover -func=coverage.out

run: build ## Run simug continuously
	./$(BINARY) run

run-once: build ## Run one orchestration tick
	./$(BINARY) run --once

explain-last-failure: build ## Explain latest failed tick
	./$(BINARY) explain-last-failure

selfhost-loop: ## Run self-host loop (ITERATIONS=<n>)
	scripts/self-host-loop.sh --repo $(REPO) --iterations $(ITERATIONS)

selfhost-canary: ## Run self-host canary (ITERATIONS=<n>)
	scripts/self-host-canary.sh --repo $(REPO) --iterations $(ITERATIONS)

canary-protocol: ## Run real-Codex protocol canary
	scripts/canary-real-codex-protocol.sh --cmd "$(CODEX_CMD)" --out "$(CANARY_OUT)"

canary-recovery: ## Run real-Codex repair/restart canary
	scripts/canary-real-codex-recovery.sh --cmd "$(CODEX_CMD)" --out "$(CANARY_OUT)"

canary-gate: ## Run combined real-Codex validation gate
	scripts/canary-real-codex-gate.sh --cmd "$(CODEX_CMD)" --out "$(CANARY_OUT)"

sandbox-dry-run: ## Validate sandbox dry-run evidence (requires ISSUE_PR and PLANNING_PR)
	scripts/sandbox-dry-run.sh --repo "$(REPO)" $(if $(ISSUE_PR),--issue-pr "$(ISSUE_PR)",) $(if $(PLANNING_PR),--planning-pr "$(PLANNING_PR)",) $(if $(ISSUE),--issue "$(ISSUE)",)

chaos: ## Run stop/restart chaos validation
	scripts/chaos-stop-restart.sh --repo $(REPO) --sleep-seconds $(SLEEP_SECONDS)
