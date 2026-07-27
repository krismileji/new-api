# API 与运维

## 鉴权与响应

所有接口位于 `/api/channel_monitor`，并统一使用 `RootAuth`。接口沿用项目的 `{ success, message, data }` 响应结构；分页接口返回项目通用分页结构。

## 接口清单

| 方法 | 路径 | 用途与主要参数 |
| --- | --- | --- |
| `GET` | `/` | 返回渠道、人工顺序、分组倍率、分组系数和全局设置 |
| `GET` | `/cost` | 成本统计；`days`、`channel_id`、`summary_only`、`page`、`date=YYYY-MM-DD` |
| `GET` | `/performance` | 性能与成功率；`minutes=1..1440` |
| `GET` | `/success/today` | 按日请求、成功率、缓存率和缓存写统计；可选 `days=1..90`、`date=YYYY-MM-DD` |
| `GET` | `/success/detail` | 成功率明细；指定 `channel_id`（可加 `model_name`）或 `group`，二选一 |
| `GET` | `/tasks` | 任务记录；`kind=ratio|schedule` 和通用分页参数 |
| `PUT` | `/settings` | 部分更新全局监控和智能调度设置 |
| `POST` | `/ratio/run` | 手动创建或复用倍率更新任务 |
| `POST` | `/schedule/run` | 手动创建或复用智能调度任务 |
| `GET` | `/schedule` | 隔离模式下返回分组模型路由、实际优先级/权重、调度状态和性能/成功率指标；其他范围返回 `409` |
| `PUT` | `/order` | 保存监控页渠道顺序；`channel_ids` |
| `PUT` | `/channel/:id` | 人工记录渠道倍率和备注；`ratio`、`remark` |
| `PUT` | `/channel/:id/schedule` | 更新参与状态；`excluded`，不修改当前路由或稳定性状态 |
| `POST` | `/channel/:id/schedule/stability/clear` | 手动解除低成功率降级或稳定性试放，恢复保护前保存的路由 |
| `PUT` | `/channel/:id/schedule/route` | 更新一条分组模型路由的参与状态；`group`、`model`、`excluded` |
| `POST` | `/channel/:id/schedule/route/stability/clear` | 手动解除一条分组模型路由的低成功率降级或稳定性试放；`group`、`model` |
| `PUT` | `/channel/:id/concurrency` | 设置并发上限；`concurrency_limit` |
| `GET` | `/channel/:id/history` | 查询倍率变更历史，支持通用分页 |
| `PUT` | `/channel/:id/upstream` | 保存上游认证、同步、换算、余额和策略配置 |
| `POST` | `/channel/:id/upstream/groups` | 用提交的上游配置读取分组列表 |
| `POST` | `/channel/:id/upstream/version` | 用 `base_url` 读取 Sub2API 公开版本 |
| `POST` | `/channel/:id/upstream/test` | 测试尚未保存或已保存的上游配置 |
| `POST` | `/channel/:id/upstream/fetch` | 使用已保存配置刷新倍率，必要时同时记录余额 |
| `POST` | `/channel/:id/upstream/balance/fetch` | 使用已保存配置刷新余额，复用请求时可同时刷新倍率 |
| `POST` | `/channel/:id/upstream/group/apply` | 把渠道的上游令牌切换到已保存分组并刷新倍率 |
| `PUT` | `/group` | 直接更新本地分组倍率；`group`、`ratio` |
| `PUT` | `/group/channels` | 替换分组关联渠道；`group`、`channel_ids` |
| `PUT` | `/group/sync` | 按最高成本倍率同步；`group`、`coefficient` |

## 全局设置

设置保存在系统 `Option` 表中。未配置或值无效时使用以下默认值：

| API 字段 | Option 键 | 默认值 | 有效范围或枚举 |
| --- | --- | ---: | --- |
| `auto_update_interval_minutes` | `ChannelMonitorAutoUpdateIntervalMinutes` | `0` | `0..525600`，`0` 关闭 |
| `auto_update_retry_count` | `ChannelMonitorAutoUpdateRetryCount` | `2` | `0..10` |
| `auto_update_consecutive_failure_limit` | `ChannelMonitorAutoUpdateConsecutiveFailureLimit` | `2` | `1..100`；倍率与余额分别计数，达到后停止对应自动更新 |
| `auto_disable_on_update_failure` | `ChannelMonitorAutoDisableOnUpdateFailure` | `false` | 布尔值 |
| `auto_enable_on_cost_ratio_recovery` | `ChannelMonitorAutoEnableOnCostRatioRecovery` | `false` | 布尔值 |
| `auto_enable_on_balance_recovery` | `ChannelMonitorAutoEnableOnBalanceRecovery` | `false` | 布尔值 |
| `cost_retention_days` | `ChannelMonitorCostRetentionDays` | `120` | `1..3650` |
| `email_notification_enabled` | `ChannelMonitorEmailNotificationEnabled` | `false` | 布尔值 |
| `notification_email` | `ChannelMonitorNotificationEmail` | 空 | 有效邮箱，最长 254 字符 |
| `probe_response_enabled` | `ChannelMonitorProbeResponseEnabled` | `false` | 布尔值；规则见[本地探针响应](probe-response.md) |
| `relay_response_header_timeout_seconds` | `RelayResponseHeaderTimeoutSeconds` | `0` | `0..600` 秒，`0` 不限制；位于智能调度设置 |
| `smart_schedule_enabled` | `ChannelMonitorSmartScheduleEnabled` | `false` | 布尔值 |
| `smart_schedule_scope` | `ChannelMonitorSmartScheduleScope` | `channel` | `channel`、`group_model` |
| `smart_schedule_groups` | `ChannelMonitorSmartScheduleGroups` | `[]` | 最多 100 个，每项最长 64 字符；空数组表示全部分组 |
| `smart_schedule_interval_minutes` | `ChannelMonitorSmartScheduleIntervalMinutes` | `10` | `1..525600` |
| `smart_schedule_strategy` | `ChannelMonitorSmartScheduleStrategy` | `smart` | `smart`、`ratio`、`first_token`、`tps` |
| `smart_schedule_stability_enabled` | `ChannelMonitorSmartScheduleStabilityEnabled` | `false` | 布尔值 |
| `smart_schedule_scoring` | `ChannelMonitorSmartScheduleScoring` | 见下方 | 稳定性、策略指标百分比、得分曲线指数和相对权重拉伸 |
| `smart_schedule_apply_mode` | `ChannelMonitorSmartScheduleApplyMode` | `weight` | `weight`、`priority_weight` |
| `smart_schedule_performance_minutes` | `ChannelMonitorSmartSchedulePerformanceMinutes` | `60` | `15`、`60`、`360`、`1440` |
| `smart_schedule_models` | `ChannelMonitorSmartScheduleModels` | `[]` | 最多 100 个，每项最长 255 字符 |
| `smart_schedule_min_samples` | `ChannelMonitorSmartScheduleMinSamples` | `5` | `1..100000` |
| `smart_schedule_min_success_rate` | `ChannelMonitorSmartScheduleMinSuccessRate` | `80` | `0..100` |
| `smart_schedule_cooldown_minutes` | `ChannelMonitorSmartScheduleCooldownMinutes` | `30` | `1..525600` |

`ChannelMonitorChannelOrder` 保存页面人工顺序，`ChannelMonitorGroupCoefficients` 保存分组同步系数。`smart_schedule_force_reset` 是一次性命令，不作为长期设置保存。

`smart_schedule_scoring` 必须提交完整对象；两个策略的指标占比各自合计为 `100%`，曲线指数范围为 `0.1..5`。相对权重拉伸的两个分差范围均为 `0..100`，且完整拉伸分差必须大于开始拉伸分差：

```json
{
  "stability_percent": 50,
  "curve_exponent": 1,
  "relative_weight_enabled": true,
  "relative_weight_start_percent": 3,
  "relative_weight_full_percent": 10,
  "smart": {
    "cost_ratio_percent": 40,
    "first_token_percent": 40,
    "tps_percent": 20
  },
  "ratio": {
    "cost_ratio_percent": 70,
    "first_token_percent": 20,
    "tps_percent": 10
  }
}
```

旧客户端提交不含三个相对权重字段的完整评分对象时，后端自动补为 `true / 3 / 10` 后保存。关闭相对权重拉伸时仍需保留有效的开始和完整分差，便于再次开启。

## 系统任务

| 任务类型 | 触发方式 | 说明 |
| --- | --- | --- |
| `channel_ratio_monitor` | 设置的更新间隔或手动 API | 更新倍率和余额、执行策略、通知和恢复 |
| `channel_smart_schedule` | 设置的调度间隔、手动 API 或强制重置 | 计算并应用优先级和权重 |
| `channel_monitor_cost_retention` | 默认每天一次 | 删除超出保留期的日成本数据 |

同类型业务任务已经排队或运行时，手动触发会返回现有任务并标记 `created=false`。任务结果限制失败明细数量，避免单次大规模故障无限放大任务记录。

倍率和余额分别记录连续失败次数。任一项连续失败 `2` 次后，定时任务会暂停该项的后续上游请求，另一项不受影响；管理员手动刷新成功或修改相关上游配置后会恢复自动请求。

## 持久化数据

自动迁移包含以下模型：

- `ChannelRatioMonitor`：每渠道的倍率、上游配置、余额、策略、调度状态和并发限制。
- `ChannelSmartScheduleRouteState`：每个渠道、分组、模型路由的参与、调度、稳定性和范围切换暂存状态。
- `ChannelRatioHistory`：倍率实际变化的前后值、备注、时间和操作人。
- `ChannelDailyCost`：按北京时间日期和渠道聚合的成本。
- `ChannelDailyAPIKeyCost`：按日期、渠道和 Key 指纹聚合的成本归因。

性能、成功率、缓存率和缓存写请求由后台每分钟从日志聚合到 `ChannelMonitorMinuteMetric`，页面只读取分钟表。分组关联继续写回渠道原有的分组字段，分组倍率和全局设置继续使用系统 Option。

SQLite、MySQL 和 PostgreSQL 都通过 GORM 迁移和方言兼容查询支持；部署升级前仍应按项目惯例备份主数据库和独立日志数据库。

成本详情未传 `date` 时沿用完整窗口汇总，保证旧调用方兼容；页面会默认传入北京时间当天。传入日期必须位于 `days` 指定的窗口内；区间累计和趋势仍覆盖完整窗口，渠道汇总与 API Key 明细仅聚合所选日期。

`/success/today` 未传参数时保持原有的今日统计响应；传入 `days` 后返回连续的北京时间日趋势，`date` 指定渠道、API Key 和缓存写渠道明细对应的日期。指定日期必须位于所选窗口内。

## 安全与审计

上游访问令牌、登录密码和自定义敏感值不会在概览响应中明文返回，只返回是否已配置。自定义 HTTP 请求使用 SSRF 防护，并在需要时复用渠道代理。

管理端的倍率、分组、上游、设置、任务、状态、调度和并发变更都会写入管理操作审计。渠道监控审计内容固定使用简体中文；凭据和完整 API Key 不进入审计详情。

本地探针响应仅在公开中继请求通过正常鉴权和模型请求限流后生效。命中请求在渠道分配前返回，不选择渠道、不请求上游、不扣费，也不写消费日志或渠道成本；详细边界见[本地探针响应](probe-response.md)。

## 运维检查

启用自动更新前确认邮件发送配置、上游凭据和渠道代理可用。启用稳定性保护前确认消费日志与 `ERROR_LOG_ENABLED` 同时开启。多实例部署使用渠道并发上限时确认 Redis 可用。

升级或同步上游后至少执行：

```powershell
go test ./controller ./service ./model
Set-Location web
bun test
bun run build
```
