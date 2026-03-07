# Changelog

All notable changes to this project are documented in this file.

The format is based on Keep a Changelog:
https://keepachangelog.com/en/1.1.0/

## [Unreleased]

### Added

- Initial changelog scaffold for `simug` task-based development workflow.
- High-fidelity orchestrator tracing in `.simug/events.log` with per-run/per-tick command traces (`git`/`gh`), exit codes, stdout/stderr tails, and explicit invariant decision events.
- Per-attempt Codex archive artifacts under `.simug/archive/agent/...` storing prompt input, raw output, and metadata linked to run/tick/attempt context.
- New `simug explain-last-failure` command to summarize the latest failed tick with violated invariant context and suggested next action using event and archive artifacts.
- Prompt contract regression tests for managed/bootstrap/repair prompt builders to catch drift in required protocol rules (`SIMUG:` lines, terminal-action rule, no-push constraints).
- Prompt-driven protocol matrix tests covering mixed stdout with valid, malformed JSON, missing-terminal, and multi-terminal Codex response classes.
- Prompt-tuning harness tests for repeatable failure corpus scenarios, plus bounded retry handling for Codex execution/protocol failures via repair prompts.
- Dual-entity routing contract coverage for `SIMUG:` (coordinator) and `SIMUG_MANAGER:` (manager), with deterministic quarantine of ambiguous unprefixed output lines.
