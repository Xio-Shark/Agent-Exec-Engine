# P5 AI Infra Platform Integration

## Context

- Project classification: existing Go backend service, feature integration
- Risk level: medium-high
- Repo probe:
  - Git worktree root is `/Users/xioshark/Desktop/career`
  - Target project is `/Users/xioshark/Desktop/career/agent-exec-engine`
  - `.gitnexus/` exists at repo root, but GitNexus MCP resources are unavailable in this session
  - `HOOK.md`, `STANDARDS.md`, and `RUNBOOK.md` were not found under the target project
- Required external dependency:
  - `/Users/xioshark/Desktop/career/滕彦翕/项目/ai-infra-platform-push/`

## Goal

Implement the P5 integration slice that can be landed safely on the current codebase:

1. extend config with AI Infra gateway and scheduler endpoints
2. add an OpenAI-compatible LLM client and GPU scheduler HTTP client
3. add an LLM step executor that requests and releases GPU around inference
4. update deployment manifests to connect with AI Infra services
5. add automated tests for config and HTTP clients

## Non-Goals

1. turning the stub workflow API into a production workflow runtime
2. claiming end-to-end workflow execution works before P4/P7 are implemented
3. introducing hidden fallbacks that mask integration failures

## Constraints

1. follow Debug-First: expose missing preconditions instead of faking success
2. keep functions under the repository quality limits where practical
3. prefer real local source of truth from `ai-infra-platform-push` over outdated plan text

## Test Plan

1. `go test ./internal/config ./internal/infra ./internal/llm -count=1`
2. `go test ./... -count=1` if targeted tests pass
3. validate YAML manifests with `docker compose config` if compose schema remains coherent

## Risks

1. `PLAN.md` expects `/api/v1/tasks`, but the current AI Infra API exposes `/jobs` and `/jobs/{id}/schedule`
2. `LLMStepExecutor` does not exist yet, so part of P4 must be pulled forward
3. current workflow HTTP API is still stubbed, so runtime acceptance remains partial
