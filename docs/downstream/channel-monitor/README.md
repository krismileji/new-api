# 渠道监控

渠道监控是 Root 管理员使用的二开运维模块，页面入口为 `/channel-monitor`，API 前缀为 `/api/channel_monitor`。模块把渠道状态、上游倍率与余额、成本、性能、成功率、调度和连通性测试集中到一个工作区。

## 文档导航

- [页面与功能](overview.md)：页面视图、筛选、操作和状态含义。
- [上游同步与策略](upstream-and-policies.md)：New API、Sub2API、自定义上游、倍率换算、余额和自动处置。
- [智能调度](smart-scheduling.md)：评分策略、参与规则、稳定性保护和任务执行。
- [成本、性能与成功率](cost-performance.md)：成本口径、API Key 归因、性能指标和失败明细。
- [测试与并发限制](testing-and-concurrency.md)：单次、批量、并发循环测试及渠道并发租约。
- [本地探针响应](probe-response.md)：单轮 `hi` 探针的本地拦截、协议响应和计费边界。
- [API 与运维](api-and-operations.md)：接口清单、系统选项、环境变量、数据和任务。

## 能力边界

渠道监控不会替代渠道管理和计费配置。渠道名称、类型、模型、密钥、代理、原始分组、优先级和权重仍以渠道配置为准；监控模块读取这些信息，并维护自己的倍率、上游认证、统计和调度状态。

页面上的“上游倍率”是上游平台返回或人工记录的倍率；“成本倍率”是在上游倍率上应用充值或订阅换算后的本地人民币成本系数；“分组倍率”是本系统实际分组配置。三者不能混用。

## 权限与语言

所有 `/api/channel_monitor` 路由都使用 `RootAuth`。渠道监控属于下游专用功能，页面、接口错误、通知邮件和操作日志固定使用简体中文，不加入上游前端 i18n 词条。

## 主要实现位置

- `router/channel-monitor-router.go`：Root API 路由。
- `controller/channel_ratio_monitor*.go`、`controller/channel_monitor*.go`：接口、策略和系统任务。
- `service/channel_ratio_monitor.go`、`service/channel_monitor*.go`：上游读取、认证和成本换算。
- `model/channel_ratio_monitor.go`、`model/channel_daily*.go`、`model/channel_monitor*.go`：持久化与统计。
- `web/src/features/channel-monitor/`：页面和交互。
- `web/src/routes/_authenticated/channel-monitor/`：前端路由。

## 基础验证

```powershell
go test ./controller ./service ./model
Set-Location web
bun test
bun run build
```
