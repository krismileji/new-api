# VERIFY-02 渠道监控冷启动完整验收

验收日期：2026-08-16

## 1. 结论

VERIFY-02 通过。SQLite、MySQL 和 PostgreSQL 均能从空库完成标准迁移并启动；MySQL、PostgreSQL 均创建 24 张最终渠道监控表且不再创建旧 `channel_monitor_minute_metrics`。Redis 8.8.0 在隔离逻辑库中自动创建 `channel_monitor:v1:events` 和 `channel_monitor:v1:aggregators`，消费组无 pending，消费者心跳正常。

一次独立测试调度已完成：任务成功、数据库只写入一行 gzip 快照、快照包含两条完整 adjustment、详情接口过滤和分页可读。分钟聚合、脏分钟定点修复、默认保留配置和清理任务保护规则均通过验收。

本次没有执行生产 `DROP TABLE`、`PURGE BINARY LOGS`、`FLUSHDB` 或宽前缀 Redis 删除。

## 2. 验收环境与隔离

| 组件 | 版本/目标 | 隔离方式 |
| --- | --- | --- |
| SQLite | `glebarez/sqlite` | 独立临时文件 |
| MySQL | 8.4.10，`127.0.0.1:13306` | 独立临时数据库 `verify02_mysql_20260816` |
| PostgreSQL | 16.14，`127.0.0.1:15432` | 独立临时数据库 `verify02_pg_20260816` |
| Redis | 8.8.0，`127.0.0.1:6379` | 隔离逻辑库 15；只清理本次创建的精确 `channel_monitor:v1:*` key |

应用分别使用三个数据库完成真实冷启动，均到达 HTTP ready 状态并返回 `/api/status` 200。启动阶段分钟聚合 worker 正常建立 `channel_monitor_aggregation_states`，系统任务 runner 自动完成一次空库清理任务。

## 3. 数据库结构

三个数据库均验证以下 24 张最终表存在：

```text
channel_ratio_monitors
channel_ratio_histories
channel_daily_costs
channel_daily_api_key_costs
channel_monitor_minute_route_metrics
channel_monitor_minute_api_key_metrics
channel_monitor_minute_duration_buckets
channel_monitor_dirty_minutes
channel_monitor_aggregation_states
channel_monitor_redis_effect_states
channel_smart_schedule_route_states
channel_smart_schedule_group_pauses
channel_smart_schedule_model_sample_states
channel_smart_schedule_execution_details
channel_status_probe_configs
channel_status_probe_states
channel_status_probe_executions
channel_model_detection_global_configs
channel_model_detection_configs
channel_model_detection_targets
channel_model_detection_batches
channel_model_detection_runs
channel_model_detection_executions
channel_model_detection_cost_events
```

`channel_monitor_minute_metrics` 在三个数据库中均不存在。

MySQL 和 PostgreSQL 实库检查、带 `CHANNEL_MONITOR_VERIFY02=1` 门禁的 SQLite 冷启动迁移测试均确认以下关键索引存在且列顺序正确。SQLite 测试会执行全库标准迁移，默认 `go test ./model` 明确跳过，避免日常模型测试重复承担冷启动成本：

| 表 | 索引 |
| --- | --- |
| `channel_smart_schedule_execution_details` | 唯一 `task_id`，普通 `created_at` |
| `channel_smart_schedule_route_states` | `(group_name, model_name, channel_id)` |
| `system_tasks` | `(type, status, id)` |
| 路由分钟指标 | 唯一维度索引及 route/channel/group 三类窗口索引 |
| API Key 分钟指标 | 唯一维度索引及 route/channel/group 三类窗口索引 |
| `channel_monitor_dirty_minutes` | 唯一 `minute_start`、标记时间和领取时间索引 |

MySQL 和 PostgreSQL 空库中没有持久化 `ChannelMonitor%` 或 `ChannelModelDetection%` 配置，应用按代码默认值返回配置，符合不兼容旧配置、重新配置的上线策略。

## 4. 默认配置

缺少对应 `options` 时，设置接口验收结果为：

| 配置 | 默认值 |
| --- | ---: |
| 成本保留 | 30 天 |
| 路由分钟指标保留 | 30 天 |
| API Key 分钟指标保留 | 7 天 |
| 执行详情保留 | 3 天 |
| 监控任务保留 | 90 天 |
| 倍率历史保留 | 365 天 |
| 状态探测历史保留 | 7 天 |
| 模型检测历史保留 | 30 天 |
| 清理任务启用 | `true` |
| 清理批次 | 1000 |
| 单轮预算 | 10 秒 |
| 续跑间隔 | 60 秒 |
| 清理周期 | 1440 分钟 |

另已验证清理任务的启用状态、周期、批次、预算和续跑间隔每次从数据库读取，不受节点旧缓存覆盖。

## 5. 功能验收

### 5.1 单次完整调度和 gzip 快照

新增 `TestVerify02ColdStartSingleScheduleSnapshotAndDetails`，只显式执行一次完整调度：

- 两条参与路由均生成 adjustment，任务状态为 `succeeded`。
- `system_tasks.result` 只保存摘要，不包含 `adjustments`。
- `channel_smart_schedule_execution_details` 只保存一行，`item_count=2`。
- `payload_blob` 以 gzip `1f 8b` 文件头开头。
- 解压后两条 adjustment 顺序、评分详情、分组和模型完整。
- 详情接口按 `group=vip` 查询，`page_size=1` 时返回总数 2 和一条当前页数据。

该验收没有改变健康度变化触发完整调度、评分、权重、主路由或保护状态语义。

### 5.2 Redis 冷启动

真实 Redis 冷启动结果：

```text
TYPE channel_monitor:v1:events = stream
group = channel_monitor:v1:aggregators
consumers = 1
pending = 0
lag = 0
consumer heartbeat = exists
```

同时通过消费组幂等创建、runtime 重启、调度副作用失败重试且不重复投影的测试。未观察到本地队列或 Redis 缺失时的降级路径。

### 5.3 分钟聚合

分钟聚合验收覆盖：

- 正常分钟只聚合一次。
- 同一分钟生成路由指标和 API Key 指标。
- 迟到错误日志标记脏分钟后只重建目标分钟。
- 修复完成后删除脏分钟标记，重复修复保持幂等。
- 启动时推进 `covered_from` 和 `completed_through`。

### 5.4 清理任务

真实三数据库冷启动均自动生成并完成一次 `channel_monitor_cost_retention` 系统任务。行为测试进一步确认：

- 过期成本、路由分钟指标、API Key 分钟指标按独立 cutoff 删除。
- 执行快照、倍率历史、探测历史和系统任务按保留规则删除。
- pending、running 和每类最新终态任务受保护。
- 禁用清理时任务正常结束且不删除数据。
- 清理周期和续跑间隔使用数据库最新配置。

## 6. 执行命令与结果

主要命令：

```powershell
# 三数据库真实冷启动，分别设置 SQL_DSN、REDIS_CONN_STRING 和独立端口
D:\Go\sdk\go1.26.5\bin\go.exe run .

# VERIFY-02 核心行为测试
D:\Go\sdk\go1.26.5\bin\go.exe test ./controller -count=1 -run '^(TestVerify02ColdStartSingleScheduleSnapshotAndDetails|TestGetChannelMonitorOverviewReturnsRetentionDefaultsWhenOptionsAreMissing|TestGetChannelMonitorSmartScheduleExecutionDetailsFiltersAndPaginatesOneTask|TestChannelMonitorCleanupHandlerEnabledUsesDatabaseValue|TestChannelMonitorCleanupIntervalUsesLatestDatabaseValue)$' -v
D:\Go\sdk\go1.26.5\bin\go.exe test ./service -count=1 -run '^(TestRunChannelMonitorAggregationAggregatesEachNormalMinuteOnceAndRepairsDirtyMinute|TestInitChannelMonitorRedisStreamCreatesIdempotentGroup|TestChannelMonitorRedisLogicalAggregatorRetriesSchedulingWithoutRepeatingProjection|TestChannelMonitorRedisRuntimeRestartsConsumerAndStopsIdempotently)$' -v
D:\Go\sdk\go1.26.5\bin\go.exe test ./model -count=1 -run '^(TestDeleteChannelMonitorCostsBeforeRemovesOnlyExpiredRows|TestDeleteChannelMonitorCostsBeforeUsesIndependentMetricCutoffs|TestDeleteChannelMonitorHistoryBeforeHonorsRetentionAndTaskGuards|TestChannelSmartScheduleExecutionDetailsPreserveOrderedRuntimePayloads|TestChannelSmartScheduleExecutionDetailIndexes)$' -v

# 全库 SQLite 冷启动迁移属于显式 VERIFY-02 验收，日常 model 测试默认跳过
$env:CHANNEL_MONITOR_VERIFY02='1'
D:\Go\sdk\go1.26.5\bin\go.exe test ./model -count=1 -run '^TestVerify02SQLiteColdStartCreatesFinalChannelMonitorSchema$' -v

# 受影响后端包全量回归
D:\Go\sdk\go1.26.5\bin\go.exe test ./controller ./service ./model -count=1
```

结果：全部通过。未设置 `CHANNEL_MONITOR_VERIFY02` 时，目标 SQLite 冷启动测试在 `0.00s` 显示 `SKIP`，默认 `go test ./model -count=1` 用时 `26.412s` 并通过；显式设置为 `1` 后目标测试用时 `2.01s` 并通过。日常模型测试不再重复执行这次全库迁移验收。受影响后端包全量回归三个包均为 `ok`。

## 7. 失败诊断

验收过程中出现两次测试使用问题，均不属于产品缺陷：

1. 只筛选 `TestChannelSmartScheduleTriggerAttributionDoesNotChangeCompleteResult` 的一个子测试时，父测试仍比较两个结果，未执行的第二个结果保持零值并导致父断言失败。为避免依赖该双场景测试，新增了只执行一次调度的独立 VERIFY-02 验收测试。
2. PostgreSQL 路由索引 fixture 首次复用了已经完成应用迁移的 `public` schema。PostgreSQL 索引名在 schema 内共享，正式表已有同名索引，导致 fixture 的 `HasIndex` 误报。改用空的独立临时数据库后，SQLite、MySQL、PostgreSQL 三个子测试全部通过；正式表的 PostgreSQL 索引也已通过 `pg_indexes` 直接确认。

## 8. 改动文件

- `controller/channel_monitor_verify02_test.go`：单次完整调度、gzip 快照和详情 API 验收。
- `model/channel_monitor_verify02_test.go`：使用 `CHANNEL_MONITOR_VERIFY02=1` 显式门禁的 SQLite 空库标准迁移、24 张最终表、旧表缺失和关键索引验收。
- `docs/downstream/channel-monitor/cold-start-verification.md`：本报告。

未修改业务语义、`model/main.go` 或实施计划文档。

## 9. 未覆盖项

- 未在生产数据库或生产 Redis 执行停机清理；生产清理由用户按 OPS Runbook 单独执行。
- 未重复 VERIFY-01 的 120/500/1,000/5,000 路由容量和压缩基准。
- 本地实库版本为 MySQL 8.4.10、PostgreSQL 16.14；最低支持版本的 SQL 兼容性继续由现有方言测试覆盖，没有本地启动 MySQL 5.7 或 PostgreSQL 9.6 实例。
- 未执行真实多网关进程压测；Redis 多消费者接管、幂等、积压和共享投影由 REDIS 轨道测试覆盖。
- binlog 保留、磁盘回收和副本延迟属于生产运维验收，不在本地冷启动范围内。
