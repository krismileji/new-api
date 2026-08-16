# 渠道监控分钟指标索引验证

本文记录 `METRIC-05` 的本地验证结果。样本库只包含合成数据，不包含生产数据。

## 索引选择

- 三张表继续使用 `minute_start` 开头的唯一维度索引，承担全局时间窗口和聚合替换。
- 路由指标增加 `(channel_id, model_key, minute_start, group_key)`，API Key 指标增加 `(channel_id, model_key, minute_start, group_key, api_key_key)`，用于智能调度和渠道+模型窗口。
- 路由、API Key 指标分别增加 `channel_id, minute_start, ...`，用于未指定模型的渠道明细。
- 路由、API Key 指标分别增加 `group_key, minute_start, ...`，用于分组明细。
- 时延桶增加 `(channel_id, model_key, minute_start, group_key, bucket_index)`，用于路由时延分布。
- `model_name`、`group_name` 只保留展示，不建立宽字符串索引。过滤使用 `model_key/group_key`，分组也使用 key，名称通过 `MIN` 取回。

## MySQL EXPLAIN

验证环境为 MySQL 8.4。合成样本包含 532,800 条路由指标、1,080,000 条 API Key 指标和 1,080,000 条时延桶；路由时间基数覆盖 30 天。查询使用生产相同的 `SUM/MIN/GROUP BY` 和观测边界连接。

| 查询形态 | 命中索引 | type | 估算扫描行 | 说明 |
| --- | --- | --- | ---: | --- |
| 路由全局 30 分钟 | `idx_channel_monitor_minute_route_dimensions` | `range` | 120 | 30 天时间基数，无全表扫描 |
| 路由 channel+60 分钟 | `idx_cm_route_channel_window` | `range` | 2,186 | 未指定模型的渠道明细 |
| 路由 channel+model+60 分钟 | `idx_cm_route_lookup` | `range` | 120 | 精确路由查询 |
| 路由 channel/model IN+60 分钟 | `idx_cm_route_lookup` | `range` | 480 | 批量调度窗口 |
| 路由 group+30 分钟 | `idx_cm_route_group_window` | `range` | 33,712 | 完整生产聚合查询 |
| API Key channel+60 分钟 | `idx_cm_api_channel_window` | `range` | 7,174 | 未指定模型的渠道明细 |
| API Key channel+model+60 分钟 | `idx_cm_api_route_lookup` | `range` | 360 | 渠道模型明细 |
| API Key group+30 分钟 | `idx_cm_api_group_window` | `range` | 108,570 | 完整生产聚合查询 |
| 时延桶 channel+model+60 分钟 | `idx_cm_duration_route_lookup` | `range` | 360 | 路由时延分布 |

聚合查询仍会出现 `Using temporary`，这是 `SUM/GROUP BY` 的有界聚合工作区；关键变化是输入通过复合索引缩小，没有对目标分钟表做明显全表扫描。短时间基数样本若查询占整表较大比例，MySQL 可能合理选择表扫描，因此全局窗口另使用 30 天 minute cardinality 验证。

## 数据库矩阵

同一模型在 SQLite、MySQL 和 PostgreSQL 上分别执行 `AutoMigrate`、写入路由/API Key/时延桶，并运行 channel+model 与 group key 过滤查询，三库均通过。正式迁移注册仍由 `CUT-01` 统一处理。
