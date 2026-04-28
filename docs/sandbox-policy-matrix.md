# Sandbox Policy Matrix

> 这份文档把 Docker sandbox 的安全策略写成可验证的矩阵。
> 对应 `EXECUTION-LIST.md` 的 A5。

---

## 策略总览

| 维度 | 策略 | 实现位置 | 测试 |
|------|------|----------|------|
| **网络** | `network=none`（完全禁用） | `internal/sandbox/executor.go` | `TestExecute_NetworkNone` |
| **文件系统** | 只读挂载 + 路径遍历防护 | `internal/sandbox/executor.go` | `TestExecute_ReadOnlyFS` |
| **CPU/内存** | cgroup 硬限 + OOM 检测 | `internal/sandbox/executor.go` | `TestExecute_OOM` |
| **超时** | 软超时 + hardkill | `internal/sandbox/executor.go` | `TestExecute_Timeout` |
| **产物收集** | stdout/stderr + 文件 artifact | `internal/sandbox/executor.go` | `TestExecute_FileInjectAndCollect` |
| **清理** | 容器自动移除 | `internal/sandbox/executor.go` | `TestCleanup` |

---

## 详细策略

### 1. 网络隔离

| 项 | 值 |
|----|-----|
| 默认策略 | `network=none` |
| 效果 | 容器无法访问任何网络接口 |
| 验证命令 | `cd agent-exec-engine && go test ./internal/sandbox -tags=docker -run TestExecute_NetworkNone -v` |
| 失败样例 | 测试执行 `curl google.com`，预期返回空或连接错误 |

### 2. 文件系统隔离

| 项 | 值 |
|----|-----|
| 默认策略 | 只读挂载 workspace，禁止路径遍历 |
| 效果 | 代码只能读取注入的文件，无法逃逸到宿主机 |
| 验证命令 | `go test ./internal/sandbox -tags=docker -run TestExecute_ReadOnlyFS -v` |
| 失败样例 | 测试尝试写入 `/etc/passwd`，预期被拒绝 |

### 3. 资源限制

| 项 | CPU | 内存 | PID |
|----|-----|------|-----|
| 限制方式 | cgroup shares（可选） | cgroup memory limit（可选） | cgroup pids limit（可选） |
| OOM 检测 | N/A | `ContainerInspect` 检查 `OOMKilled` | N/A |
| 验证命令 | `go test ./internal/sandbox -tags=docker -run TestExecute_OOM -v` |
| 失败样例 | 分配大量内存触发 OOM，预期返回明确错误类型 |

### 4. 超时与 Hardkill

| 项 | 值 |
|----|-----|
| 软超时 | 通过 `context.WithTimeout` 传递 |
| Hardkill | 超时后 `ContainerKill` + `ContainerRemove` |
| 验证命令 | `go test ./internal/sandbox -tags=docker -run TestExecute_Timeout -v` |
| 失败样例 | 执行 `sleep 60`，设置 1s 超时，预期被强制终止 |

### 5. 产物收集

| 项 | 值 |
|----|-----|
| stdout | `ContainerLogs` 收集 |
| stderr | `ContainerLogs` 收集 |
| 文件 artifact | 从容器内指定路径拷贝出来 |
| 验证命令 | `go test ./internal/sandbox -tags=docker -run TestExecute_FileInjectAndCollect -v` |

---

## 错误返回格式

沙箱执行失败时，返回结构化错误：

| 场景 | 错误类型 | 日志字段 |
|------|---------|---------|
| 超时 | `DeadlineExceeded` | `error=timeout` |
| OOM | `OOMKilled` | `error=oom` |
| 网络违规 | 命令返回非零 | `error=exit_code` |
| 路径遍历 | `400 Bad Request`（校验层拒绝） | `error=path_traversal` |

---

## 当前未覆盖（诚实声明）

| 能力 | 状态 | 说明 |
|------|------|------|
| seccomp 策略 | 未配置 | 依赖 Docker 默认 seccomp profile |
| AppArmor/SELinux | 未配置 | 当前环境未启用 |
| GPU 隔离 | 未配置 | 沙箱不涉及 GPU 访问 |
| 多容器编排 | 不需要 | 每个工具调用一个独立短容器 |

---

## Reviewer 验证路径

```bash
# 运行全部沙箱测试（需要 Docker daemon）
cd agent-exec-engine
go test ./internal/sandbox -tags=docker -count=1 -v

# 运行单条策略验证
go test ./internal/sandbox -tags=docker -run 'TestExecute_(Timeout|OOM|NetworkNone|ReadOnlyFS|FileInjectAndCollect)' -v
```

---

## 关联文档

- 架构说明: [`architecture.md`](architecture.md)
- 系统边界: [`system-map.md`](system-map.md)
- 后端简历证据: [`backend-resume-evidence.md`](backend-resume-evidence.md)
