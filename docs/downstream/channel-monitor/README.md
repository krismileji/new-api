# 渠道监控

渠道监控是 Root 管理员使用的渠道运行管理功能，入口为 `/channel-monitor`，管理 API 前缀为 `/api/channel_monitor`。它提供渠道状态、上游同步、智能调度、成本统计、探测、模型检测、连通性测试和并发限制。

## 功能说明

- [监控总览](dashboard.md)：渠道、分组、模型和智能调度视图。
- [上游同步](upstream-sync.md)：上游倍率、余额、认证和自动处置。
- [智能调度](smart-scheduling.md)：调度准入、评分、保本兜底、稳定性保护和重试选路。
- [成本统计](cost-statistics.md)：渠道/API Key 成本、成功率、性能和未解析数据。
- [实时监控](realtime-monitoring.md)：事件流、实时投影和分钟聚合读取。
- [状态探测](status-probe.md)：定时/手动探测、健康状态和探测样本。
- [模型检测](model-detection.md)：独立检测器接入、渠道模型检测和结果结算。
- [模型广场分组监控](model-market-monitoring.md)：用户侧分组状态与管理员探测配置。
- [连通性测试](connectivity-test.md)：单次、批量和并发循环测试。
- [渠道并发限制](channel-concurrency.md)：渠道并发上限、租约和满载重选。
- [本地探针响应](local-probe-response.md)：公开请求的本地探针响应。

## 权限边界

`/api/channel_monitor/*` 使用 RootAuth；配置、调度、探测和并发写操作由对应处理器记录业务审计。普通用户不能访问管理接口。模型广场分组监控另有用户接口 `/api/pricing/group-monitor`，只返回当前用户可见分组的公开状态字段。

## 数据边界

逻辑归组只改变智能调度、状态探测和模型检测的共享身份；成本、余额、倍率、日志、性能明细和并发租约仍按物理渠道记录。Redis 实时投影与数据库分钟聚合并存，成本金额以数据库日账为准。
