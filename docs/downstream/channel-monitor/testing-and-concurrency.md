# 测试与并发限制

## 单渠道与批量测试

渠道监控支持对单个渠道发起连接测试，也可以打开统一的渠道连通性测试面板。测试会真实请求上游，并可能产生上游费用和渠道成本记录。

批量模式先选择已定价模型，再只显示支持全部所选模型的渠道。普通批量任务按“渠道 × 模型”展开，前端最多同时发起 `5` 个请求。渠道监控入口限制为单模型，渠道管理入口可以使用多模型批量组合。

并发循环模式固定选择一个渠道和一个模型，用于观察稳定性和延迟分布：

- 并发数范围 `1` 到 `20`，默认 `3`。
- 每个并发的循环次数范围 `1` 到 `50`，默认 `5`。
- 总请求数最多 `200`。
- 结果显示成功、失败、响应时间、平均值、最快、最慢和 P95。

停止操作只阻止尚未发出的任务，已经发送的请求会等待结束。测试请求不做请求去重，每个组合或循环都独立命中上游。

## 端点和流式测试

可以自动检测端点，或显式选择：

- OpenAI Chat Completions。
- OpenAI Responses。
- OpenAI Responses Compact。
- Anthropic Messages。
- Gemini Generate Content。
- Jina Rerank。
- Image Generations。
- Embeddings。

Chat、Responses、Anthropic 和 Gemini 等兼容端点可以执行流式测试。Embeddings、Image Generations、Jina Rerank 和 Responses Compact 不支持测试面板的流式开关。

流式测试验证事件流是否包含有效数据和正常结束信号，并在可用时读取用量；非流式测试验证响应体和用量。端点、流式开关和模型会传给同一个后端渠道测试接口。

## 单渠道并发限制

每个渠道可以设置 `0` 到 `100000` 的并发上限，`0` 表示不限。页面实时显示“当前/上限”。限制覆盖常规中继、Realtime、Claude、Gemini 和任务提交，租约在上游调用结束后释放。

自动选择的渠道满载时，本次请求会临时排除该渠道并尝试其他符合模型和分组条件的渠道。显式指定或锁定的渠道满载时直接返回 HTTP `429`；所有候选都满载时同样返回 `429`。

## 单机与多实例

启用 Redis 时，并发租约通过 Redis 原子脚本在所有实例间共享，租约 TTL 为 `2` 分钟并每 `30` 秒续期，进程异常后过期租约会自动回收。配置带修订号同步，避免旧实例覆盖新上限。

未启用 Redis 时使用进程内计数，只能约束单个实例；多实例部署若需要全局上限必须启用 Redis。

## 关键实现

- `controller/channel-test.go`：端点、流式和响应验证。
- `web/src/features/channels/components/dialogs/channel-batch-test-dialog.tsx`：批量与循环调度。
- `controller/channel_concurrency.go`：中继接入、满载重选和 429 错误。
- `service/channel_concurrency.go`：本地/Redis 租约和配置同步。
- `model/channel_ratio_monitor.go`：并发限制与修订号持久化。
