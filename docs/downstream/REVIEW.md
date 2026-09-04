# 文档审查记录

本次审查按 `docs-downstream-review-plan.md` 对照当前代码完成。文档只覆盖下游新增或改变的行为，并以代码为准。

## 扫描范围

- 下游新增 Go 文件：渠道监控、逻辑归组、智能调度、成本账本、探测、模型检测、并发、错误映射、中继重试等。
- 前端：`/channel-monitor`、`/group-monitor`、`/usage-logs`、逻辑归组对话框、渠道测试。
- 路由：`/api/channel_monitor/*`、`/api/channel/logical-groups`、`/api/pricing/group-monitor`、日志 `all/user-visible/self`。
- 环境变量和 Option：见 [配置参考](configuration-reference.md)。

未纳入正式功能文档的下游页面：钱包「小铺充值」只是把侧边栏配置的 HTTP 店铺 URL 嵌到 iframe，没有独立后端能力。

## 补充文档

- [channel-monitor/architecture.md](channel-monitor/architecture.md)
- [channel-monitor/scheduling-algorithm.md](channel-monitor/scheduling-algorithm.md)
- [channel-monitor/data-consistency.md](channel-monitor/data-consistency.md)
- [integration-guide.md](integration-guide.md)
- [configuration-reference.md](configuration-reference.md)

## 更新文档

- 渠道监控索引、连通性测试、并发限制、本地探针、状态探测、模型检测、分组监控、智能调度、实时监控、上游同步
- 逻辑归组、使用日志、中继可靠性、图像定价保护
- 下游索引

## 原遗漏点处理

| 遗漏点 | 处理 |
| --- | --- |
| 事件发布 / Redis Stream / 成本可靠 outbox | architecture.md、realtime-monitoring.md |
| 评分公式、保本、稳定性、429、自适应采样 | scheduling-algorithm.md |
| 实时 vs 历史 vs 账本 | data-consistency.md |
| Sub2API 认证模式、自定义上游 | 已有 upstream-sync.md，配置参考补充 Option |
| 余额预警和自动禁用 | 已有 upstream-sync.md |
| 错误映射/关键字/白名单 | relay-reliability.md、configuration-reference.md |
| 探针 Allowed IPs | local-probe-response.md |
| 并发等待和 429 | channel-concurrency.md |
| 图像倍率检查时机和不可重试 | image-pricing-guard.md |
| 日志范围接口 | usage-logs.md |
| 环境变量完整列表 | configuration-reference.md |

## 结构建议

保持「一个大功能一个目录、一个具体功能一个文件」。算法、架构和配置单独成页，避免把 dashboard.md 再扩成总手册。后续新增下游功能时，先补对应目录的 README，再视需要把环境变量追加到配置参考。
