# Agent 行为回归集

> 从 `agent-eval` 和 `rag` 的评测样本抽取的稳定 workflow fixtures，用于验证 Agent 后端的行为一致性。
> 对应 `EXECUTION-LIST.md` 的 A7 / P4 / R4。

---

## Fixtures

| # | 文件 | 来源 | 场景 | 步骤数 | 关键能力 |
|---|------|------|------|--------|---------|
| 1 | `multi-step-code-gen.json` | agent-eval/T1 | 多步骤代码生成 | 3 | llm_call → code_exec → llm_call |
| 2 | `error-recovery.json` | agent-eval/T4 | 错误恢复 | 4 | llm_call → llm_call → code_exec → llm_call |
| 3 | `ambiguous-intent.json` | agent-eval/T5 | 模糊意图处理 | 3 | llm_call → human → llm_call |
| 4 | `rag-knowledge-qa.json` | rag-eval | 知识问答 | 3 | rag_search → llm_call → llm_call |
| 5 | `gateway-resilience.json` | infra-eval | 网关韧性 | 3 | llm_call → code_exec → llm_call |

## 运行方式

```bash
# 1. 启动 AEE 服务
cd agent-exec-engine
make run

# 2. 运行回归集
./scripts/run-regression.sh

# 3. 查看摘要
cat evidence/regression/summary.txt
```

## 验证检查清单

- [ ] 5 个 fixture 全部成功提交到 `/api/v1/workflows`
- [ ] 每个 fixture 返回有效的 `workflow_id`
- [ ] `multi-step-code-gen` 覆盖 code_exec 工具调用
- [ ] `ambiguous-intent` 覆盖 human-in-the-loop 暂停/恢复
- [ ] `rag-knowledge-qa` 覆盖 rag_search 工具调用
- [ ] `gateway-resilience` 覆盖 llm_call + 健康检查组合

## 扩展方式

新增 fixture：
1. 在 `fixtures/` 下创建新的 `.json` 文件
2. 确保 `metadata.source` 和 `metadata.category` 字段存在
3. 运行 `./scripts/run-regression.sh` 验证

---

## 关联文档

- agent-eval 样本: [`../../agent-eval/shared/tasks.json`](../../agent-eval/shared/tasks.json)
- 系统边界: [`../../docs/system-map.md`](../../docs/system-map.md)
- 跨项目 demo: [`../../docs/cross-project-demo.md`](../../docs/cross-project-demo.md)
