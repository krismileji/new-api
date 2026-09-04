# 完整配置参考

本页列出当前下游功能使用的环境变量和数据库 Option。未列出的上游配置仍以官方文档为准。测试专用变量（如 `CHANNEL_MONITOR_VERIFY01`）不部署使用。

## 环境变量

| 变量 | 默认 | 范围/说明 | 变更影响 |
| --- | --- | --- | --- |
| `CHANNEL_LOGICAL_GROUP_ENABLED` | `true` | 全局逻辑归组开关 | `false` 时新请求走物理渠道路径，关系和历史保留 |
| `CHANNEL_DAILY_COST_RELIABLE_OUTBOX` | `true` | 成本可靠 outbox | `false` 只作为整批紧急回滚到内存 batcher |
| `CHANNEL_MONITOR_CONSUMER_ID` | 空 | 消费者身份；否则回退 `INSTANCE_ID` / `POD_NAME` / `HOSTNAME` | 多实例必须可区分，避免租约互相覆盖 |
| `CHANNEL_MONITOR_EVENT_WRITER_QUEUE_CAPACITY` | `8192` | 事件发布队列容量 | 过小会增加直接发布和 outbox 回退 |
| `CHANNEL_MONITOR_EVENT_WRITER_WORKERS` | `1` | 发布 worker 数 | 加大可提高 XADD 吞吐 |
| `CHANNEL_MONITOR_EVENT_WRITER_DIRECT_OVERFLOW` | `true` | 队列满时是否直接发布 | 关闭后更容易写入 outbox |
| `CHANNEL_MONITOR_EVENT_WRITER_PERSIST_OVERFLOW` | `true` | 队列满时是否持久化 outbox | 关闭后溢出事件可能丢失 |
| `CHANNEL_MONITOR_REDIS_HANDLER_TIMEOUT_MS` | `3000` | 单条消费处理超时 | `<=0` 回退 `3000` |
| `CHANNEL_MONITOR_REDIS_PENDING_RETRY_LIMIT` | `3` | pending 重试上限 | 超过后进入 dead-letter |
| `CHANNEL_MONITOR_REDIS_CONSUMER_WORKERS` | `1` | 消费 worker，最大 `32` | 只影响消费吞吐，不增加聚合器数量 |
| `CHANNEL_MONITOR_REDIS_SHARED_MAX_QUERY_MINUTES` | `1441` | 实时查询最大分钟，硬上限 `1441` | 超出截断窗口 |
| `CHANNEL_MONITOR_REDIS_SHARED_MAX_HASH_FIELDS` | `500000` | 单 hash 字段上限，硬上限 `2000000` | 超出返回降级 |
| `CHANNEL_MONITOR_REDIS_SHARED_MAX_TOTAL_HASH_FIELDS` | `5000000` | 总字段上限，硬上限 `20000000` | 超出返回降级 |
| `CHANNEL_MONITOR_REDIS_SHARED_MAX_RESPONSE_BYTES` | `268435456` | 响应字节上限，硬上限 `512MiB` | 超出返回降级 |
| `CHANNEL_MONITOR_REDIS_SHARED_MAX_DIMENSION_ENTRIES` | `20000` | 单维度条目上限，硬上限 `100000` | 超出截断 |
| `CHANNEL_MONITOR_REDIS_SHARED_MAX_RESPONSE_DIMENSIONS` | `50000` | 响应维度上限，硬上限 `500000` | 超出截断 |
| `CHANNEL_MONITOR_AGGREGATION_BACKFILL_MAX_CHUNKS` | `1` | 分钟追赶块数，最大 `24` | 启动追赶速度 |
| `CHANNEL_MONITOR_AGGREGATION_BACKFILL_BUDGET_SECONDS` | `10` | 追赶预算，最大 `300` | 超时留下水位 |
| `CHANNEL_MONITOR_AGGREGATION_BACKFILL_YIELD_MS` | `50` | 块间让出，最大 `5000` | 降低数据库压力 |
| `CHANNEL_MONITOR_ADAPTIVE_REFRESH_MIN_INTERVAL_SECONDS` | `10` | `0..3600` | 调度运行时刷新最小间隔 |
| `CHANNEL_SMART_SCHEDULE_ROUTE_SNAPSHOT_MAX_AGE_SECONDS` | `300` | Redis 路由快照最大年龄 | 过期后重新生成快照 |
| `CHANNEL_STATUS_PROBE_SCAN_INTERVAL_MS` | `1000` | `200..30000`，超出回退 `1000` | 状态探测 worker 扫描间隔 |
| `CHANNEL_GROUP_MONITOR_SCAN_INTERVAL_MS` | `1000` | `200..30000`，超出回退 `1000` | 分组监控 worker 扫描间隔 |
| `CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_ENABLED` | `true` | 上游模型更新任务 | `false` 停止该系统任务 |
| `CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_INTERVAL_MINUTES` | `30` | `<1` 回退 `30` | 上游模型同步频率 |
| `CHANNEL_UPSTREAM_MODEL_UPDATE_MIN_CHECK_INTERVAL_SECONDS` | `300` | 单渠道最小检查间隔 | 避免频繁打上游 |
| `GPT56_DETECTOR_URL` | 空 | 仅当数据库没有检测器地址时作为初始值 | 不覆盖已保存设置 |
| `GPT56_DETECTOR_PROXY_TOKEN` | 空 | 检测器代理 Token | 不写入管理响应 |
| `CHANNEL_TEST_FREQUENCY` | 上游监控设置 | 自动渠道测试分钟数 | 与上游渠道测试共用 |
| `CHANNEL_TEST_ENABLED` | 上游监控设置 | 自动渠道测试开关 | 与上游渠道测试共用 |
| `CHANNEL_UPDATE_FREQUENCY` | 上游设置 | 渠道更新频率 | 上游已有变量 |
| `RELAY_RESPONSE_HEADER_TIMEOUT` | `1800` | 进程启动时的传输层响应头超时（秒） | 管理端 Option `RelayResponseHeaderTimeoutSeconds` 可再覆盖为 `0..600`，`0` 表示不额外限制 |

渠道监控还要求启用 Redis。未启用 Redis 时 runtime 初始化失败，实时投影和 Stream 消费不可用。

## 渠道监控 Option

通过 Root 接口 `PUT /api/channel_monitor/settings` 更新。并发修改智能调度配置时，请求必须带当前 `smart_schedule_control_revision`，否则返回“渠道监控设置已被其他请求修改，请刷新后重试”。

### 上游同步与自动处置

| Option | JSON 字段 | 默认 | 范围 |
| --- | --- | --- | --- |
| `ChannelMonitorAutoUpdateIntervalMinutes` | `auto_update_interval_minutes` | `0` | `0..525600`，`0` 关闭定时更新 |
| `ChannelMonitorAutoUpdateRetryCount` | `auto_update_retry_count` | `3` | `0..10` |
| `ChannelMonitorAutoUpdateRetryDelaySeconds` | `auto_update_retry_delay_seconds` | `0` | `0..600` |
| `ChannelMonitorAutoUpdateConsecutiveFailureLimit` | `auto_update_consecutive_failure_limit` | `10` | `1..100` |
| `ChannelMonitorAutoDisableOnUpdateFailure` | `auto_disable_on_update_failure` | `false` | 倍率或余额最终失败后系统禁用 |
| `ChannelMonitorAutoEnableOnCostRatioRecovery` | `auto_enable_on_cost_ratio_recovery` | `false` | 只恢复因成本倍率禁用的渠道 |
| `ChannelMonitorAutoEnableOnBalanceRecovery` | `auto_enable_on_balance_recovery` | `false` | 只恢复因余额禁用的渠道 |
| `ChannelMonitorUpstreamRequestTimeoutSeconds` | `upstream_request_timeout_seconds` | `30` | `1..600` |
| `ChannelMonitorChannelConcurrencyWaitSeconds` | `channel_concurrency_wait_seconds` | `1` | `0..600` |
| `ChannelMonitorGroupCoefficients` | 分组视图中的系数 | 缺失按 `1` | `>=0` |
| `ChannelMonitorChannelOrder` | 页面人工顺序 | 空 | 只影响监控页排序 |

邮件通知：

| Option | 默认 | 说明 |
| --- | --- | --- |
| `ChannelMonitorEmailNotificationEnabled` | `false` | 总开关 |
| `ChannelMonitorNotificationEmail` | 空 | 必须是合法邮箱 |
| `ChannelMonitorEmailNotificationTypes` | 全部类型 | `ratio_change`、`balance_warning`、`channel_disabled`、`group_membership_removed`、`upstream_sync_failed`、`task_failed` |

### 保留与清理

| Option | 默认天数/值 | 说明 |
| --- | --- | --- |
| `ChannelMonitorCostRetentionDays` | `30` | 日成本 |
| `ChannelMonitorRouteMetricRetentionDays` | `30` | 路由分钟 |
| `ChannelMonitorDurationBucketRetentionDays` | `30` | 首字分桶 |
| `ChannelMonitorApiKeyMetricRetentionDays` | `7` | API Key 分钟 |
| `ChannelMonitorExecutionDetailRetentionDays` | `3` | 调度执行明细 |
| `ChannelMonitorTaskRetentionDays` | `7` | 通用任务 |
| `ChannelMonitorRatioMonitorTaskRetentionDays` | `7` | 倍率任务 |
| `ChannelMonitorSmartScheduleTaskRetentionDays` | `7` | 调度任务 |
| `ChannelMonitorSmartScheduleProbeTaskRetentionDays` | `3` | 智能探测任务 |
| `ChannelMonitorCleanupTaskRetentionDays` | `7` | 清理任务 |
| `ChannelMonitorModelDetectionTaskRetentionDays` | `7` | 模型检测任务 |
| `ChannelMonitorChannelTestTaskRetentionDays` | `7` | 连通性测试任务 |
| `ChannelMonitorModelUpdateTaskRetentionDays` | `7` | 模型更新任务 |
| `ChannelMonitorRatioHistoryRetentionDays` | `365` | 倍率历史 |
| `ChannelMonitorStatusProbeHistoryRetentionDays` | `7` | 状态探测历史 |
| `ChannelMonitorGroupMonitorRetentionDays` | `7` | 分组监控历史 |
| `ChannelMonitorModelDetectionRetentionDays` | `30` | 模型检测历史，范围 `7..180` |
| `ChannelMonitorCleanupEnabled` | `true` | 清理总开关 |
| `ChannelMonitorCleanupBatchSize` | `1000` | 单批删除行数 |
| `ChannelMonitorCleanupBudgetSeconds` | `10` | 单轮预算 |
| `ChannelMonitorCleanupContinuationSeconds` | `60` | 超预算后继续等待 |
| `ChannelMonitorCleanupIntervalMinutes` | `1440` | 清理间隔 |

### 智能调度

| Option | 默认 | 范围 |
| --- | --- | --- |
| `ChannelMonitorSmartScheduleEnabled` | `false` | 总开关 |
| `ChannelMonitorSmartScheduleGroupPolicies` | `[]` | 每组一条策略，最多 `100` 组 |
| `ChannelMonitorSmartSchedulePerformanceWindowMinutes` | `60` | `1..43200` |
| `ChannelMonitorSmartScheduleRealtimeRetentionMinutes` | `60` | 实时样本保留分钟 |
| `ChannelMonitorSmartScheduleRealtimeSampleLimit` | `20000` | 实时样本上限 |
| `ChannelMonitorSmartScheduleRateLimitCooldownSeconds` | `30` | `0..300` |
| `ChannelSmartScheduleControlRevision` | 自动生成 | 乐观并发 |

分组策略 JSON 的默认值和算法见[调度算法](channel-monitor/scheduling-algorithm.md)。

### 错误可见性

| Option | 默认 | 限制 |
| --- | --- | --- |
| `ChannelMonitorErrorMessageMapping` | 空 | JSON 对象，最多 `100` 条；键是上游错误码或 HTTP 状态码，最长 `128`；值是用户可见消息，最长 `4096` |
| `ChannelMonitorErrorMessageWhitelist` | 空 | 最多 `32` 个错误码/状态码，命中后跳过全部用户侧处理 |
| `ChannelMonitorErrorMessageKeywords` | 空 | 每行一个关键字，最多 `32` 个，每个最长 `128`，大小写不敏感子串屏蔽 |

无效的已保存映射在请求时被忽略，不会把上游失败变成网关新错误。管理员日志保留原始错误。

### 本地探针

| Option | 默认 | 范围 |
| --- | --- | --- |
| `ChannelMonitorProbeResponseEnabled` | `false` | 总开关 |
| `ChannelMonitorProbeResponseAllowedIPs` | 空 | 最多 `64` 个 IP，总长 `4096`；空表示不限制。配置非法时运行时关闭探针 |
| `ChannelMonitorProbeResponseMatchInput` | `hi` | 非空，最长 `4096` |
| `ChannelMonitorProbeResponseText` | `Hi. What are you working on?` | 非空，最长 `16384` |
| `ChannelMonitorProbeResponseMinDelayMilliseconds` | `500` | `0..600000`，不能大于最大延迟 |
| `ChannelMonitorProbeResponseMaxDelayMilliseconds` | `2000` | `0..600000` |
| `ChannelMonitorProbeResponseInputTokens` | `4387` | `0..1000000` |
| `ChannelMonitorProbeResponseCacheWriteTokens` | `0` | `0..1000000` |
| `ChannelMonitorProbeResponseCachedTokens` | `3840` | `0..1000000` |
| `ChannelMonitorProbeResponseOutputTokens` | `14` | `0..1000000` |

### 其他

| Option | 默认 | 说明 |
| --- | --- | --- |
| `RelayResponseHeaderTimeoutSeconds` | `0` | 管理端覆盖，`0..600`；`0` 表示不额外限制等待上游响应头 |
| `GroupRatio` | 上游分组倍率 | 渠道监控分组视图和策略会更新该 Option，并同时推进经济修订号 |

## 逻辑归组

没有独立 Option。全局开关是环境变量 `CHANNEL_LOGICAL_GROUP_ENABLED`。组和成员存在 `ChannelLogicalGroup` / `ChannelLogicalGroupMember`，变更使用 revision，旧 revision 返回 `409`。

## 模型检测

检测器 URL、间隔和渠道目标保存在 `ChannelModelDetectionGlobalConfig` / `ChannelModelDetectionConfig`。管理接口：

- `GET/PUT /api/channel_monitor/model_detection/settings`
- `GET /api/channel_monitor/model_detection`
- `PUT /api/channel_monitor/model_detection/channel/:id/config`
- `POST /api/channel_monitor/model_detection/channel/:id/run`
- 内部中继 `POST /internal/model-detector/v1/responses`

## 配置变更影响

- 修改分组倍率或成本换算会更新经济修订号，旧调度任务不能用过期经济数据写回。
- 修改智能调度策略或总开关会推进 control revision，并可能清理临时流量。
- 修改清理保留期不会立刻删数据，下一轮清理任务按新截止时间删除。
- 修改探针 Allowed IPs 为非法值时，运行时关闭本地探针，避免误命中。
- 关闭逻辑归组或禁用单个逻辑组只影响新请求，不回放已经冻结的探测/检测/调度任务。


