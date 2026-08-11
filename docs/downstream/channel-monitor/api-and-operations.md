# API 与运维

## 鉴权与响应

所有接口位于 `/api/channel_monitor`，并统一使用 `RootAuth`。接口沿用项目的 `{ success, message, data }` 响应结构；分页接口返回项目通用分页结构。

## 接口清单

| 方法 | 路径 | 用途与主要参数 |
| --- | --- | --- |
| `GET` | `/` | 返回渠道、人工顺序、分组倍率、分组系数和全局设置 |
| `GET` | `/cost` | 成本统计；`days`、`channel_id`、`summary_only`、`page`、`date=YYYY-MM-DD` |
| `GET` | `/performance` | 主表性能与成功率；智能调度启用时使用性能窗口，关闭时接受 `minutes=1..1440` |
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
| `PUT` | `/channel/:id/schedule/route/pause` | 暂停或恢复一条分组模型路由的业务流量；`group`、`model`、`duration_minutes`，`0` 表示恢复；不提供分组级暂停接口 |
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

`GET /performance` 的响应必须返回后端实际采用的 `range_minutes` 和 `range_source`。智能调度实际启用时，忽略客户端手动分钟值，使用 `smart_schedule_performance_window_minutes` 并返回 `range_source=smart_schedule`，允许完整的 `1..43200` 分钟；关闭时校验 `minutes=1..1440` 并返回 `range_source=manual`。两种模式都按每个 `(渠道, 模型)` 的 `observation_since` 裁剪当前主表数据。边界后没有样本时返回样本数 `0` 和空指标，不返回伪造的 `0%`。

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
| `cost_retention_days` | `ChannelMonitorCostRetentionDays` | `120` | `1..3650`；日成本、分钟指标和延迟分桶共用此保留期 |
| `execution_detail_retention_days` | `ChannelMonitorExecutionDetailRetentionDays` | `14` | `1..3650` |
| `task_retention_days` | `ChannelMonitorTaskRetentionDays` | `90` | `1..3650`，且不能短于调度执行明细保留期 |
| `ratio_history_retention_days` | `ChannelMonitorRatioHistoryRetentionDays` | `365` | `1..3650` |
| `email_notification_enabled` | `ChannelMonitorEmailNotificationEnabled` | `false` | 布尔值 |
| `notification_email` | `ChannelMonitorNotificationEmail` | 空 | 有效邮箱，最长 254 字符 |
| `email_notification_types` | `ChannelMonitorEmailNotificationTypes` | 六类全选 | `ratio_change`、`balance_warning`、`channel_disabled`、`group_membership_removed`、`upstream_sync_failed`、`task_failed`；开启邮件通知时至少选择一类 |
| `error_message_mapping` | `ChannelMonitorErrorMessageMapping` | 空 | JSON 对象，最多 100 条；键为上游错误码或 HTTP 状态码，值为用户可见错误信息 |
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
| `smart_schedule_performance_window_minutes` | `ChannelMonitorSmartSchedulePerformanceWindowMinutes` | `60` | `1..43200` |
| `smart_schedule_stability_window_minutes` | `ChannelMonitorSmartScheduleStabilityWindowMinutes` | `5` | `1..43200` 分钟；仅用于稳定性软评分，不直接触发硬保护 |

`ChannelMonitorChannelOrder` 保存页面人工顺序，`ChannelMonitorGroupCoefficients` 保存分组同步系数。`smart_schedule_force_reset` 是一次性命令，不作为长期设置保存。

`upstream_request_timeout_seconds` 同时用于自动更新、手动刷新和上游配置测试。一次倍率刷新中包含的登录、倍率和余额子请求共享同一个总超时预算；自动更新若超时，会按 `auto_update_retry_count` 为下一次尝试重新分配完整预算。该设置与中继请求的 `relay_response_header_timeout_seconds` 相互独立。

`email_notification_types` 控制通知邮件中的分类和主题统计；未勾选的事件仍会写入任务执行记录，但不会触发或进入邮件。`POST /settings/email-preview` 使用同一套邮件构建逻辑生成示例，响应的 `data.subject` 和 `data.html` 就是当前选择对应的最终主题与 HTML 内容。

`error_message_mapping` 对全部渠道统一生效。系统优先匹配上游错误码，再匹配最终 HTTP 状态码；匹配后的文案用于用户使用日志，并只在请求响应尚未开始时替换返回给用户的错误信息。未配置或未匹配时保持原有行为，用户使用日志只展示状态码。

`smart_schedule_group_policies` 以分组名为唯一键，没有默认策略或未配置分组的回退规则。启用智能调度时至少要提交一项策略；每项策略必须完整提交当前版本字段，缺少首字告警请求占比、恢复健康占比、独立秒级窗口或切换确认占比会校验失败，旧的轮数字段和探索租约字段不再输出或参与运行；`models: []` 表示该分组的全部模型。`strategy` 支持 `smart`、`ratio`、`first_token`、`tps`，`apply_mode` 支持 `weight`、`priority_weight`，`sample_mode` 支持 `off`、`traffic`、`probe`，`sampling_order` 支持 `priority_weight`、`ratio`。探索流量只允许与 `priority_weight` 应用方式一起使用，定时探测只会向支持文本 Responses 协议的渠道发送流式 `/v1/responses` 请求。

全局智能调度开启后，命中分组策略及其模型范围的调度池只允许明确参与调度的路由接收首请求、亲和或重试流量；参与候选为空时返回无可用渠道，不回退渠道默认 P/W。未配置策略的分组以及未命中策略模型范围的模型池不参与智能调度，继续使用官方 Ability 候选集合。旧的未参与路由人工 P/W 接口已删除，不提供兼容入口。

探索流量和低优先级轮转合并为统一样本补充。当前版本删除 `priority_sampling_enabled`、`priority_sampling_interval_minutes`、`priority_sampling_base_percent`、`priority_sampling_decay_percent`、`priority_sampling_min_percent`，不读取或迁移旧值。管理端先展示样本补充方式；选择 `sample_mode=traffic` 后，在同一个探索流量配置组内依次展示 `exploration_traffic_percent`、`exploration_max_prompt_tokens`、`sampling_order`，不把统一采样顺序显示为组外的独立常驻字段。切换到其他补充方式后保留该顺序值，自适应备援继续复用它；选择 `sample_mode=probe` 时展示 `probe_interval_minutes`。管理端的探索请求上限和稳定性释放请求上限都以 K Token 输入和回显，默认分别为 `50K` 与 `0K`，其中 `1K = 1000 Token`；API 仍提交实际 Token 数。

`exploration_max_prompt_tokens` 和 `stability_release_max_prompt_tokens` 的 API 值必须为 `0` 或 `1000` 的整数倍，范围为 `0..1000000 Token`；`0` 表示不做该类路由的上限过滤。当前版本不读取、不迁移旧的非整 K 值，例如旧默认值 `16384` 会被拒绝。

`smart_schedule_interval_minutes` 只控制常规情况下的完整评分、正常选主、基础排名和基础 P/W。样本补充对象切换、稳定性保护、自适应备援、降级定时探测、探测恢复、冷却结束试放以及成功延迟抖动都使用分组策略自己的窗口、请求事件或定时器，不等待完整调度。普通采样、软健康和抖动事件按 `(分组, 模型)` 合并 `1` 秒；429 冷却和稳定性硬保护立即执行。完整调度运行期间到达的软事件在新基础快照提交后重放，不能被完整调度覆盖。

`GET /schedule` 的 `sample_scope` 固定为 `channel_model`。每条路由的 `state` 是 `(渠道, 分组, 模型)` 独立决策状态，包含 `rolling_stability_*`、`sampling_debt`、`sampling_candidate`、`sampling_order`、`last_sampling_at` 和持久化的 `adaptive_health_first_token_warning_request_percent` 等秒级运行时字段；没有窗口内样本时 `rolling_stability_score` 返回 `null`。`shared_samples` 是 `(渠道, 模型)` 唯一的一份手动测试和定时探测滚动样本，并返回持久化的 `recovery_success_count` 与 `recovery_success_at`；相同渠道模型的多条分组路由返回同一份 `shared_samples`。`performance_items` 按渠道模型返回，不含分组字段，`group_count` 表示窗口内业务样本实际覆盖的分组数；`stability_items` 会按分组模型调度池投影最终判定结果，但底层请求观测仍是共享口径。健康明细返回 `error_request_percent`、`first_token_warning_request_percent`、`risk_request_percent` 和 `healthy_request_percent`：前两项分别解释错误与首字进入信号，风险占比用于看板与执行明细解释，健康占比用于压力状态恢复和备用切换确认，风险占比不再作为统一进入门槛。429 冷却是独立的 `(渠道, 模型)` 避让状态：普通选路优先跳过，但当前分组没有其他可用候选时允许兜底；亲和、手动测试、常规/降级探测和全部采样仍必须跳过，冷却到期由独立秒级检查触发池刷新。

评分对象的两组业务指标占比各自必须合计为 `100%`。`primary_traffic_percent` 表示“只调整权重”模式下主渠道的目标流量，范围为 `51%..99%`；`primary_switch_threshold_percent` 表示挑战渠道替换当前主渠道所需的最小得分差，范围为 `0%..100%`。`fast_failure_same_channel_retry_count` 范围为 `0..10`，默认 `0`；错误符合原有重试规则且本次耗时不超过 `fast_failure_seconds` 时，系统先在当前渠道额外重试，且不消耗普通 `RetryTimes`，额度用尽后才进入普通重试并排除当前渠道。`fast_failure_same_channel_retry_delay_ms` 控制每次同渠道快速重试前的固定等待，范围为 `0..60000` 毫秒、默认 `1000` 毫秒；普通跨渠道重试不等待，请求取消会中止等待。每次普通重试选中渠道后重新计算这份快速失败额度。保护窗口由 `burst_failure_window_minutes`（`1..60`）和 `burst_failure_window_requests`（`1..1000`）共同限定：在最近分钟范围内只取最近的请求，正常参与路由的连续失败达到 `consecutive_failure_threshold`，或失败请求占比达到 `burst_failure_threshold_percent`（`>0..100%`），就立即进入硬保护，不受 `min_samples` 或滚动稳定性评分限制。成功会清零连续失败并进入失败率分母，429 不进入窗口。`jitter_slow_threshold_seconds` 是固定的首字慢成功阈值，范围为 `0..60` 秒，不叠加历史基线，只参与抖动处罚；每个超出容忍数量的慢成功从稳定性得分中扣除 `1 / 稳定性总样本数`，事件值按池合并 `1` 秒更新。请求失败上限由 `relay_response_header_timeout_seconds` 控制。

自适应进入使用两个独立信号：窗口内非 429 错误率超过 `adaptive_sampling_error_warning_percent`，或首字达到告警秒数的成功请求占比达到必填的 `adaptive_sampling_first_token_warning_request_percent`。请求比例窗口由 `adaptive_sampling_window_minutes`（`1..60`）和 `adaptive_sampling_window_requests`（`1..1000`）共同限定，默认统计最近 10 分钟内最多最近 100 次业务请求、手动测试和定时探测。首字告警请求占比默认 `10%`，有效范围为 `>0..100%`。两个信号按 OR 判定，不先合并风险请求再经过统一进入门槛；压力状态只有在两个进入信号都解除且健康请求占比达到 `adaptive_sampling_recover_request_percent` 后恢复，仍命中的进入信号优先于恢复判断。保存时要求 `adaptive_sampling_first_token_warning_request_percent + adaptive_sampling_recover_request_percent > 100%`。正常单主渠道最低保留比例由 `100% - adaptive_sampling_max_percent` 自动推导；`adaptive_sampling_primary_min_percent` 已从当前契约移除，旧配置中的该字段兼容忽略且不再输出。旧字段 `adaptive_sampling_enter_request_percent` 会被明确拒绝，不读取、不输出、不迁移，不提供旧配置兼容。完整策略示例：

```json
[
  {
    "group": "vip",
    "strategy": "smart",
    "apply_mode": "priority_weight",
    "models": ["gpt-4.1"],
    "stability_enabled": true,
    "min_samples": 20,
    "recovery_stability_score": 95,
    "fast_failure_penalty_percent": 40,
    "fast_failure_seconds": 1,
    "fast_failure_same_channel_retry_count": 2,
    "fast_failure_same_channel_retry_delay_ms": 1000,
    "slow_failure_seconds": 10,
    "burst_failure_window_minutes": 1,
    "burst_failure_window_requests": 100,
    "consecutive_failure_threshold": 2,
    "burst_failure_threshold_percent": 3,
    "recovery_success_threshold": 2,
    "jitter_enabled": true,
    "jitter_tolerance_percent": 5,
    "jitter_slow_threshold_seconds": 10,
    "cooldown_minutes": 30,
    "degraded_probe_enabled": false,
    "sample_mode": "probe",
    "exploration_traffic_percent": 3,
    "exploration_max_prompt_tokens": 50000,
    "sampling_order": "priority_weight",
    "probe_interval_minutes": 10,
    "stability_release_max_prompt_tokens": 0,
    "adaptive_sampling_enabled": true,
    "adaptive_sampling_base_percent": 3,
    "adaptive_sampling_max_percent": 30,
    "adaptive_sampling_error_warning_percent": 5,
    "adaptive_sampling_error_critical_percent": 15,
    "adaptive_sampling_first_token_warning_seconds": 5,
    "adaptive_sampling_first_token_critical_seconds": 10,
    "adaptive_sampling_first_token_warning_request_percent": 10,
    "adaptive_sampling_recover_request_percent": 95,
    "adaptive_sampling_window_minutes": 10,
    "adaptive_sampling_window_requests": 100,
    "adaptive_sampling_switch_confirm_request_percent": 95,
    "adaptive_sampling_min_comparable_channels": 2,
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

旧持久化策略中的 `burst_failure_window_seconds`、`burst_failure_threshold` 和 `adaptive_sampling_window_seconds` 仅用于只读兼容；管理员打开并保存策略后，前端会提交当前的分钟数、请求数和百分比字段。新策略不再输出旧字段。

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
| `channel_smart_schedule` | 设置的调度间隔、手动 API 或强制重置 | 执行常规完整评分并应用基础优先级和权重；不承担事件驱动采样、保护或恢复 |
| `channel_smart_schedule_probe` | 启用定时探测的分组所配置的最短探测间隔 | 为到期文本路由补充流式 Responses 样本；渠道并发已满时跳过 |
| `channel_monitor_cost_retention` | 默认每天一次 | 分批删除超出各自配置保留期的分钟指标、日成本、执行明细、已结束监控任务和倍率历史 |

同类型业务任务已经排队或运行时，手动触发会返回现有任务并标记 `created=false`。任务结果限制失败明细数量，避免单次大规模故障无限放大任务记录。

`channel_smart_schedule` 任务结果的 `adjustments` 逐条返回渠道、分组、模型、得分、新旧优先级与权重、动作和原因。每条 `score_details` 是执行时固化的评分解释，包含原始输入及样本数、同池归一化范围、配置/有效权重、业务与稳定性贡献、最终得分、选主结果及选择/调整原因。完整明细按任务和顺序保存在 `channel_smart_schedule_execution_details` 的独立 `TEXT` 行中，`system_tasks.result` 只保存汇总；查询任务列表时再组装返回，避免单个任务汇总超过数据库 `TEXT` 限制。固定路由还包含 `manual_primary: true`、`manual_primary_until` 和 `manual_primary_allow_stability_degrade`，便于在独立的执行记录页面核对本轮选主是来自评分还是管理员固定，以及固定是否允许稳定性保护。固定、续期和解除的管理审计明细使用 `allow_stability_degrade` 保存操作时的选项值；未传时按默认 `true` 记录。

倍率和余额分别记录连续失败次数。任一项连续失败 `2` 次后，定时任务会暂停该项的后续上游请求，另一项不受影响；管理员手动刷新成功或修改相关上游配置后会恢复自动请求。

## 持久化数据

自动迁移包含以下模型：

- `ChannelRatioMonitor`：每渠道的倍率、上游配置、余额、策略和并发限制。
- `ChannelSmartScheduleRouteState`：每个渠道、分组、模型路由的参与、调度和稳定性状态。
- `ChannelSmartScheduleModelSampleState`：每个渠道、模型唯一的一份手动测试和定时探测滚动样本，以及稳定性恢复后的共享 `observation_since`。
- `ChannelSmartScheduleExecutionDetail`：按任务和路由保存智能调度执行时的评分与调整解释。
- `ChannelMonitorAggregationState`：保存所有节点共享的最新完整分钟水位，聚合数据与水位在同一事务提交。
- `ChannelMonitorMinuteMetric`：按分钟和渠道、模型、分组、API Key 维度保存性能与成功率指标。
- `ChannelMonitorMinuteDurationBucket`：按分钟保存首字延迟分布，供异常抖动判断和稳健延迟评分使用。
- `ChannelRatioHistory`：倍率实际变化的前后值、备注、时间和操作人。
- `ChannelDailyCost`：按北京时间日期和渠道聚合的成本。
- `ChannelDailyAPIKeyCost`：按日期、渠道和 Key 指纹聚合的成本归因。

性能、成功率、缓存率和缓存写请求由后台在每个自然分钟结束后 1 秒从日志聚合到 `ChannelMonitorMinuteMetric`。常规任务只回扫最近 2 分钟，启动回扫 5 分钟，整点在时间预算内修复最近 65 分钟；若任务跨过多个分钟，则从上次连续水位补齐缺口后再推进。`/performance`、`/success/today`、`/success/detail`、`/schedule` 和完整智能调度评分读取前都会确认同一最新完整分钟水位，分钟首秒内的请求最多等待到第 1 秒。普通模型中继请求不执行聚合或水位检查；运行时硬保护和自适应备援直接使用请求级观测，不依赖该水位。

日志和分钟行保留真实分组；智能调度读取首字、TPS、稳定性和首字分布时再按渠道模型跨分组汇总，并以 `max(窗口起点, observation_since)` 作为实际起点。恢复只推进边界，不删除样本、日志、分钟行或延迟分桶；历史与长期统计不应用该边界。分组关联继续写回渠道原有的分组字段，分组倍率和全局设置继续使用系统 Option。保留任务默认按 1000 行一批清理；数据库删除会释放页供后续复用，但 SQLite、MySQL 和 PostgreSQL 都不保证物理文件立即缩小。

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
