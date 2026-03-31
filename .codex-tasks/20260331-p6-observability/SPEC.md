# P6 Observability

## Context

- Project classification: existing Go backend service, feature implementation
- Risk level: medium-high
- Repo probe:
  - target project: `/Users/xioshark/Desktop/career/agent-exec-engine`
  - git worktree exists and target project already has prior `.codex-tasks` history
  - `HOOK.md`, `STANDARDS.md`, and `RUNBOOK.md` are not present under the target project
  - GitNexus MCP resources/tools are unavailable in-session, so impact analysis uses direct caller scans
- Skill routing used for this pass:
  - `pua` core skill for execution style
  - `taskmaster` for multi-step task tracking

## Goal

Implement the P6 observability slice from `PLAN.md` with real runtime wiring:

1. OTLP trace exporter initialization and shutdown
2. trace spans in scheduler, MCP tool calls, and sandbox execution
3. Prometheus metrics wiring for workflow, tool, and sandbox runtime paths
4. trace-aware structured logging
5. Grafana and Prometheus local config assets that make the compose stack usable

## Non-Goals

1. claiming the stub workflow HTTP endpoint is production ready
2. introducing hidden fallbacks that fake successful tracing or metrics
3. rewriting unrelated runtime modules or changing existing workflow semantics

## Constraints

1. keep Debug-First behavior: failures in OTLP setup should be explicit
2. prefer minimal API surface changes through optional dependency injection
3. preserve existing tests unless behavior is intentionally expanded

## Impact Analysis

- `observability.NewTracer`
  - direct callers: `cmd/server/main.go`
  - affected process: server bootstrap only
  - risk: low
- `dag.Scheduler.Run` / `dag.Scheduler.executeStep`
  - direct callers: scheduler tests, integration tests, runtime workflow execution
  - affected process: workflow orchestration and retries
  - risk: medium
- `mcp.Registry.Call`
  - direct callers: `internal/mcp/server.go`, `internal/llm/executor.go`
  - affected process: external MCP calls and internal LLM tool loop
  - risk: medium
- `sandbox.Executor.Execute`
  - direct callers: `internal/sandbox/pool.go`, `internal/mcp/tools/code_exec.go`, docker tests
  - affected process: user code execution path
  - risk: medium
- `observability.Logger.WithContext`
  - current callers: none
  - affected process: future log correlation only
  - risk: low

## Test Plan

1. `go test ./internal/observability ./internal/mcp ./internal/dag -count=1`
2. `go test ./... -count=1`
3. `docker compose -f deployments/docker-compose.yaml config`

## Risks

1. Prometheus registration can panic in tests if metrics use only the default registry
2. tracing injection in `Registry.Call` must not double count tool metrics already recorded elsewhere
3. compose currently references Prometheus assets that are missing from the repository, so dashboard work must close that gap explicitly
