# User Control Design

## 设计目标

这个项目不是单纯追求“让 Agent 更快执行”，而是优先解决长任务里用户最真实的三个顾虑：

1. 任务跑到一半，我还能不能介入。
2. 进程挂掉以后，会不会只能从头再来。
3. 工具调用如果失控，会不会把宿主环境一起拖坏。

因此，这里的控制感设计不是 UI 文案，而是直接落在执行引擎的状态机、恢复机制和隔离边界上。

## 三个核心机制

| 用户顾虑 | 机制 | 代码 / 接口落点 | 解决的问题 |
| --- | --- | --- | --- |
| 我能不能在关键节点介入 | `human` 步骤 + resume API | [docs/api.md](api.md) 中 `POST /api/v1/workflows/:id/resume`；[internal/dag/scheduler_run.go](../internal/dag/scheduler_run.go) | 把“人工审批 / 人工补充输入”变成正式状态，而不是临时打断流程 |
| 中断后会不会丢进度 | Redis Checkpoint + 断点恢复 | [internal/dag/checkpoint.go](../internal/dag/checkpoint.go)、[internal/dag/scheduler_state.go](../internal/dag/scheduler_state.go) | 每步落 checkpoint，服务重启或人工恢复时不必整条工作流重跑 |
| 工具失控会不会污染环境 | Docker Sandbox + cgroup + hardkill | [docs/architecture.md](architecture.md)、[internal/sandbox/executor.go](../internal/sandbox/executor.go) | 把风险收敛到单次工具调用，而不是扩散到宿主进程 |

## 为什么这三件事比“单纯更快”更重要

对用户来说，长任务最痛的往往不是等待，而是不知道现在发生了什么、也不知道失败后要付出多大代价重来。

- `human` 步骤把“等待人工确认”显式建模成 `paused` 状态，说明系统承认有些决策不该默认替用户做完。
- Checkpoint 把“恢复”从口头承诺变成真实能力。当前实现使用 Redis 保存 checkpoint，带版本冲突保护和 24 小时 TTL，避免多实例同时覆盖状态。
- Sandbox 把“安全”从 best effort 变成强边界。资源限制、网络隔离、超时 hardkill 都是为了让用户敢把代码执行权交给 Agent，而不是只能在最保守的只读模式下使用。

## 一个最短控制链路

1. 工作流执行到 `human` 步骤，scheduler 将 workflow 状态切到 `paused`。
2. 当前状态会被持久化到 checkpoint；如果服务此时重启，恢复点仍然存在。
3. 用户或 reviewer 通过 `POST /api/v1/workflows/:id/resume` 注入人工输入，工作流从暂停点继续。
4. 后续如果触发 `tool_call`，调用会进入 Docker 沙箱；即使超时或 OOM，影响也被限制在容器内部。

这条链路对应的现有验证点包括：

- [internal/dag/scheduler_test.go](../internal/dag/scheduler_test.go) 中的人机步骤暂停 / 恢复测试
- [internal/dag/checkpoint_test.go](../internal/dag/checkpoint_test.go) 中的保存、冲突与 TTL 测试
- [cmd/server/main.go](../cmd/server/main.go) 中的优雅关停，保证服务退出时不会直接丢弃可恢复状态

## 设计取舍

- 不做“所有步骤都允许随意回滚”。当前只保证从明确 checkpoint 继续，而不是提供复杂的任意时刻 time travel。
- 不把控制感建立在日志堆砌上。日志和 trace 用于排障，可暂停、可恢复、可隔离才是用户真正能感知到的控制权。
- 不用“更快的默认自动化”替代人为确认。对于高风险步骤，显式暂停比静默自动继续更符合信任模型。

## 结论

在这个引擎里，`checkpoint`、`human-in-the-loop` 和 `sandbox` 分别对应的是可恢复、可介入和可隔离。它们共同回答的是同一个问题：用户为什么敢把长任务交给 Agent，而不是为什么这个 Agent 只能在 demo 里跑得快。