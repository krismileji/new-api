# 模型广场分组监控

模型广场分组监控向用户展示可用分组的探测状态，入口为 `/group-monitor` 和 `/api/pricing/group-monitor`。管理员在 `/channel-monitor` 的分组监控设置中配置分组、探测模型、间隔和展示窗口。

## 管理视图

Root 管理接口提供配置、候选模型、总览、手动执行和执行历史。配置使用 revision；探测间隔为 30 到 86400 秒，展示窗口至少覆盖两个探测周期，每组配置一个具体文本 `probe_model`。重复手动执行返回冲突。

Worker 每个逻辑分组每轮只执行一次探测；上游失败可以在物理成员之间重试，但管理员执行记录保留最终逻辑结果和实际渠道。结果包括 success、upstream_failure、rate_limited、local_failure、unavailable 和 skipped；skipped 不计入成功率。探测成本归属实际物理渠道。

## 用户视图

用户接口按 pricing 模块权限和当前用户可用分组过滤，只返回 group、initial、status、probe_model、latest_first_token_ms、success_rate、last_finished_at 和 recent_window。不会返回渠道 ID、Key、成本、错误详情、租约或管理员配置。

状态包括 unconfigured、paused、pending、healthy、unavailable、unhealthy、rate_limited 和 stale。监控关闭时用户仍可看到已配置分组的 paused 状态；无效探测模型只保留在管理员视图。
