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
| `POST` | `/settings/email-preview` | 按提交的 `notification_types` 生成邮件主题和 HTML 预览，不发送邮件 |
| `POST` | `/ratio/run` | 手动创建或复用倍率更新任务 |
| `POST` | `/schedule/run` | 手动创建或复用智能调度任务 |
| `GET` | `/schedule` | 返回分组模型路由、实际优先级/权重、独立调度状态和渠道模型共享观测指标 |
| `PUT` | `/order` | 保存监控页渠道顺序；`channel_ids` |
| `PUT` | `/channel/:id` | 人工记录渠道倍率和备注；`ratio`、`remark` |
| `PUT` | `/channel/:id/schedule/routes` | 批量更新该渠道全部分组模型路由的参与状态；`excluded`，不修改当前路由值或稳定性状态 |
| `PUT` | `/channel/:id/schedule/route` | 更新一条分组模型路由的参与状态；`group`、`model`、`excluded` |
| `PUT` | `/channel/:id/schedule/route/primary` | 固定、续期或解除一条分组模型路由的主渠道状态；`group`、`model`、`duration_minutes` |
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
| `upstream_request_timeout_seconds` | `ChannelMonitorUpstreamRequestTimeoutSeconds` | `30` | `1..600` 秒；倍率与余额刷新单次尝试的总超时 |
| `auto_update_consecutive_failure_limit` | `ChannelMonitorAutoUpdateConsecutiveFailureLimit` | `2` | `1..100`；倍率与余额分别计数，达到后停止对应自动更新 |
| `auto_disable_on_update_failure` | `ChannelMonitorAutoDisableOnUpdateFailure` | `false` | 布尔值 |
| `auto_enable_on_cost_ratio_recovery` | `ChannelMonitorAutoEnableOnCostRatioRecovery` | `false` | 布尔值 |
| `auto_enable_on_balance_recovery` | `ChannelMonitorAutoEnableOnBalanceRecovery` | `false` | 布尔值 |
| `cost_retention_days` | `ChannelMonitorCostRetentionDays` | `120` | `1..3650` |
| `email_notification_enabled` | `ChannelMonitorEmailNotificationEnabled` | `false` | 布尔值 |
| `notification_email` | `ChannelMonitorNotificationEmail` | 空 | 有效邮箱，最长 254 字符 |
| `email_notification_types` | `ChannelMonitorEmailNotificationTypes` | 六类全选 | `ratio_change`、`balance_warning`、`channel_disabled`、`group_membership_removed`、`upstream_sync_failed`、`task_failed`；开启邮件通知时至少选择一类 |
| `probe_response_enabled` | `ChannelMonitorProbeResponseEnabled` | `false` | 布尔值；规则见[本地探针响应](probe-response.md) |
| `probe_response_match_input` | `ChannelMonitorProbeResponseMatchInput` | `hi` | 去首尾空白后不能为空，最长 4096 个字符 |
| `probe_response_text` | `ChannelMonitorProbeResponseText` | `Hi. What are you working on?` | 去首尾空白后不能为空，最长 16384 个字符 |
| `probe_response_min_delay_ms` | `ChannelMonitorProbeResponseMinDelayMilliseconds` | `500` | `0..600000` 毫秒，不能大于最大延迟 |
| `probe_response_max_delay_ms` | `ChannelMonitorProbeResponseMaxDelayMilliseconds` | `2000` | `0..600000` 毫秒，不能小于最小延迟 |
| `probe_response_input_tokens` | `ChannelMonitorProbeResponseInputTokens` | `4387` | `0..1000000` |
| `probe_response_cache_write_tokens` | `ChannelMonitorProbeResponseCacheWriteTokens` | `172` | `0..1000000` |
| `probe_response_cached_tokens` | `ChannelMonitorProbeResponseCachedTokens` | `4001` | `0..1000000` |
| `probe_response_output_tokens` | `ChannelMonitorProbeResponseOutputTokens` | `12` | `0..1000000` |
| `relay_response_header_timeout_seconds` | `RelayResponseHeaderTimeoutSeconds` | `0` | `0..600` 秒，`0` 不限制；流式请求限制首个有效模型事件，非流式请求限制响应头；位于智能调度设置 |
| `smart_schedule_enabled` | `ChannelMonitorSmartScheduleEnabled` | `false` | 布尔值 |
| `smart_schedule_group_policies` | `ChannelMonitorSmartScheduleGroupPolicies` | `[]` | 最多 100 个完整分组策略；未配置分组不参与调度 |
| `smart_schedule_interval_minutes` | `ChannelMonitorSmartScheduleIntervalMinutes` | `10` | `1..525600` |
| `smart_schedule_performance_minutes` | `ChannelMonitorSmartSchedulePerformanceMinutes` | `60` | `15`、`60`、`360`、`1440` |

`ChannelMonitorChannelOrder` 保存页面人工顺序，`ChannelMonitorGroupCoefficients` 保存分组同步系数。`smart_schedule_force_reset` 是一次性命令，不作为长期设置保存。

`upstream_request_timeout_seconds` 同时用于自动更新、手动刷新和上游配置测试。一次倍率刷新中包含的登录、倍率和余额子请求共享同一个总超时预算；自动更新若超时，会按 `auto_update_retry_count` 为下一次尝试重新分配完整预算。该设置与中继请求的 `relay_response_header_timeout_seconds` 相互独立。

`email_notification_types` 控制通知邮件中的分类和主题统计；未勾选的事件仍会写入任务执行记录，但不会触发或进入邮件。`POST /settings/email-preview` 使用同一套邮件构建逻辑生成示例，响应的 `data.subject` 和 `data.html` 就是当前选择对应的最终主题与 HTML 内容。

`smart_schedule_group_policies` 以分组名为唯一键，没有默认策略或未配置分组的回退规则。启用智能调度时至少要提交一项策略，每项都必须包含完整字段；`models: []` 表示该分组的全部模型。`strategy` 支持 `smart`、`ratio`、`first_token`、`tps`，`apply_mode` 支持 `weight`、`priority_weight`，`sample_mode` 支持 `off`、`traffic`、`probe`。探索流量只允许与 `priority_weight` 一起使用，定时探测只会向支持文本 Responses 协议的渠道发送流式 `/v1/responses` 请求。

`GET /schedule` 的 `sample_scope` 固定为 `channel_model`。每条路由的 `state` 是 `(渠道, 分组, 模型)` 独立决策状态，`shared_samples` 是 `(渠道, 模型)` 唯一的一份手动测试和定时探测滚动样本；相同渠道模型的多条分组路由返回相同的 `shared_samples`。`performance_items` 按渠道模型返回，不含分组字段，`group_count` 表示窗口内业务样本实际覆盖的分组数；`stability_items` 会按分组模型调度池投影最终判定结果，但底层请求观测仍是共享口径。

评分对象的两组业务指标占比各自必须合计为 `100%`。`primary_traffic_percent` 表示“只调整权重”模式下主渠道的目标流量，范围为 `51%..99%`；`primary_switch_threshold_percent` 表示挑战渠道替换当前主渠道所需的最小得分差，范围为 `0%..100%`。抖动绝对容差使用秒且范围为 `0..60`，慢成功阈值为当前基线加上绝对容差；基准学习周期使用分钟且范围为 `1..43200`。完整策略示例：

```json
[
  {
    "group": "vip",
    "strategy": "smart",
    "apply_mode": "priority_weight",
    "models": ["gpt-4.1"],
    "stability_enabled": true,
    "min_samples": 20,
    "degrade_stability_score": 90,
    "recovery_stability_score": 95,
    "fast_failure_penalty_percent": 40,
    "fast_failure_seconds": 1,
    "slow_failure_seconds": 10,
    "jitter_enabled": true,
    "jitter_tolerance_percent": 5,
    "jitter_absolute_tolerance_seconds": 10,
    "jitter_baseline_minutes": 60,
    "cooldown_minutes": 30,
    "sample_mode": "probe",
    "exploration_traffic_percent": 3,
    "probe_interval_minutes": 10,
    "scoring": {
      "stability_percent": 50,
      "primary_traffic_percent": 90,
      "primary_switch_threshold_percent": 3,
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
  }
]
```

## 固定主渠道

`PUT /api/channel_monitor/channel/:id/schedule/route/primary` 按渠道 ID、分组和模型唯一定位路由。请求体：

```json
{
  "group": "vip",
  "model": "gpt-4.1",
  "duration_minutes": 60,
  "allow_stability_degrade": true
}
```

`group` 和 `model` 必须为非空的实际路由值。`duration_minutes` 必须显式提交：`1..525600` 表示固定或续期，对已固定的同一路由调用时会从当前时间重新计算到期时间；`0` 表示立即解除固定。负数、超过 `525600` 或缺少该字段都会返回 `400`。

`allow_stability_degrade` 为可选布尔值，省略时默认为 `true`。`true` 表示固定期间允许稳定性保护临时降级该路由，但保留固定到期时间和选主意图，到期前恢复后继续作为固定主渠道。`false` 表示严格固定：仍记录稳定性样本和分数，但固定期内不自动降级。对已固定路由重新提交时，到期时间和该布尔选项会一起更新；解除固定时该选项不影响解除结果。

新设固定前，渠道和对应 `Ability` 必须已启用，路由必须已参与智能调度且不在稳定性保护状态。同一 `(group, model)` 池只保留一个固定主渠道，固定新渠道时会先解除旧渠道并恢复其保存的路由值。接口会立即更新实际优先级和权重；若智能调度已启用且存在分组策略，还会创建或复用一个调度任务。

成功响应的 `data` 包含 `channel_id`、`group`、`model`、`duration_minutes`、`allow_stability_degrade`、`manual_primary_until`、`routing_changed` 和可选的 `task`。`manual_primary_until` 是 Unix 秒级时间戳，解除后为 `0`。调度看板路由状态使用 `manual_primary_allow_stability_degrade` 返回当前固定的选项值。后台每分钟清理一次已到期的固定并刷新路由缓存，调度看板读取和调度任务执行前也会再次清理，无需再调用解除接口。

严格固定 `30` 分钟的请求示例：

```json
{
  "group": "vip",
  "model": "gpt-4.1",
  "duration_minutes": 30,
  "allow_stability_degrade": false
}
```

解除固定的请求示例：

```json
{
  "group": "vip",
  "model": "gpt-4.1",
  "duration_minutes": 0
}
```

## 系统任务

| 任务类型 | 触发方式 | 说明 |
| --- | --- | --- |
| `channel_ratio_monitor` | 设置的更新间隔或手动 API | 更新倍率和余额、执行策略、通知和恢复 |
| `channel_smart_schedule` | 设置的调度间隔、手动 API 或强制重置 | 计算并应用优先级和权重 |
| `channel_smart_schedule_probe` | 启用定时探测的分组所配置的最短探测间隔 | 为到期文本路由补充流式 Responses 样本；渠道并发已满时跳过 |
| `channel_monitor_cost_retention` | 默认每天一次 | 删除超出保留期的日成本数据 |

同类型业务任务已经排队或运行时，手动触发会返回现有任务并标记 `created=false`。任务结果限制失败明细数量，避免单次大规模故障无限放大任务记录。

`channel_smart_schedule` 任务结果的 `adjustments` 逐条返回渠道、分组、模型、得分、新旧优先级与权重、动作和原因。每条 `score_details` 是执行时固化的评分解释，包含原始输入及样本数、同池归一化范围、配置/有效权重、业务与稳定性贡献、最终得分、选主结果及选择/调整原因。完整明细按任务和顺序保存在 `channel_smart_schedule_execution_details` 的独立 `TEXT` 行中，`system_tasks.result` 只保存汇总；查询任务列表时再组装返回，避免单个任务汇总超过数据库 `TEXT` 限制。固定路由还包含 `manual_primary: true`、`manual_primary_until` 和 `manual_primary_allow_stability_degrade`，便于在独立的执行记录页面核对本轮选主是来自评分还是管理员固定，以及固定是否允许稳定性保护。固定、续期和解除的管理审计明细使用 `allow_stability_degrade` 保存操作时的选项值；未传时按默认 `true` 记录。

倍率和余额分别记录连续失败次数。任一项连续失败 `2` 次后，定时任务会暂停该项的后续上游请求，另一项不受影响；管理员手动刷新成功或修改相关上游配置后会恢复自动请求。

## 持久化数据

自动迁移包含以下模型：

- `ChannelRatioMonitor`：每渠道的倍率、上游配置、余额、策略和并发限制。
- `ChannelSmartScheduleRouteState`：每个渠道、分组、模型路由的参与、调度和稳定性状态。
- `ChannelSmartScheduleModelSampleState`：每个渠道、模型唯一的一份手动测试和定时探测滚动样本。
- `ChannelSmartScheduleExecutionDetail`：按任务和路由保存智能调度执行时的评分与调整解释。
- `ChannelMonitorMinuteDurationBucket`：按分钟保存首字延迟分布，供异常抖动判断和稳健延迟评分使用。
- `ChannelRatioHistory`：倍率实际变化的前后值、备注、时间和操作人。
- `ChannelDailyCost`：按北京时间日期和渠道聚合的成本。
- `ChannelDailyAPIKeyCost`：按日期、渠道和 Key 指纹聚合的成本归因。

性能、成功率、缓存率和缓存写请求由后台每分钟从日志聚合到 `ChannelMonitorMinuteMetric`，日志和分钟行保留真实分组；智能调度读取首字、TPS、稳定性和首字分布时再按渠道模型跨分组汇总。分组关联继续写回渠道原有的分组字段，分组倍率和全局设置继续使用系统 Option。

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
