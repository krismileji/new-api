# 渠道监控分钟指标高基数验收

> 子计划：`METRIC-06`
> 验收日期：2026-08-16
> 状态：通过，SQLite、MySQL、PostgreSQL 已在同一代码版本完成完整矩阵

## 1. 验收范围

本报告只验证分钟指标容量、查询、增量聚合、定点修复和保留边界，不修改生产行为。固定 fixture 位于：

```text
service/channel_monitor_metric_high_cardinality_integration_test.go
```

数据分布固定且不使用随机数或 sleep 判断：

| 项目 | 数量 |
| --- | ---: |
| 路由 | 1,000 |
| 每路由 API Key | 4 |
| 时间范围 | 连续 2 分钟 |
| 正常 consume 日志 | 4,000 |
| retry 日志 | 1,000 |
| 下一分钟 final summary 日志 | 1,000 |
| 初始日志总数 | 6,000 |
| 迟到日志 | 1 |
| 路由保留边界样本 | 第 0～31 天，共 32 条 |
| API Key 保留边界样本 | 第 0～8 天，共 9 条 |

SQLite 使用临时文件；MySQL 和 PostgreSQL 只允许连接独立的 `new_api_metric06` 本地测试库。测试结束会删除该测试创建的全部表，未执行任何生产清理命令。

本轮环境为 Go 1.26.5、MySQL 8.4.10、PostgreSQL 16.14；项目最低兼容版本仍以仓库约束的 MySQL 5.7.8 和 PostgreSQL 9.6 为准，本报告不能替代最低版本矩阵。

## 2. 最终同版本结果

最终矩阵开始和结束时，四个相关文件的 SHA256 完全一致，确认运行期间代码没有漂移：

```text
model/channel_monitor_minute.go
D83D94927693249DBDF4729D5A9823AC67109509474FA3CB476230F92C65DD9B

model/channel_monitor_dirty_minute.go
42A9D027E469EE7A54462105D85A149EC9B1A7607E072DB53F044EC04E463775

service/channel_monitor_aggregation.go
9570BF99A2F1AF536770D84B1C8423B6DA764CA5B3D2D56291529AA287CDFE82

service/channel_monitor_metric_high_cardinality_integration_test.go
E0A4F0AC56E45EA515D7A95802D42BAB54790B3DA7B7DED3C05F85E1ECA0C7A6
```

### 2.1 高基数行数和查询

| 数据库 | 扫描日志 | 路由行 | API Key 行 | 时延桶行 | 调度窗口 | 调度读取 API Key 表 | 调度查询 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| SQLite | 6,000 | 2,000 | 5,000 | 1,000 | 1,000 | 0 次 | 15ms |
| MySQL 8 | 6,000 | 2,000 | 5,000 | 1,000 | 1,000 | 0 次 | 24ms |
| PostgreSQL 16 | 6,000 | 2,000 | 5,000 | 1,000 | 1,000 | 0 次 | 17ms |

第一分钟中 4,000 条不同 API Key 明细只生成 1,000 条路由记录和 4,000 条 API Key 记录。路由表行数不随每路由 API Key 数增长，调度窗口查询只读取路由表和时延桶表，不读取 API Key 分钟表。

初始 6,000 条日志生成并持久化 8,000 行分钟数据：2,000 行路由、5,000 行 API Key、1,000 行时延桶。加入 1 条迟到日志后，全量结果为 8,002 行：2,000 行路由、5,001 行 API Key、1,001 行时延桶。

聚合结果和运行日志中的 `generated_rows` 均按三张表精确相加，API Key 行不再被遗漏；日志同时分别输出 `route_metric_rows`、`api_key_metric_rows` 和 `duration_bucket_rows`，便于容量核对。

### 2.2 聚合耗时

| 数据库 | 插入 6,000 日志 | 初始聚合 | 正常分钟重复 worker | 定点修复 | 两分钟全量重建 | 保留清理 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| SQLite | 427ms | 933ms | 1ms | 1,224ms | 1,235ms | 30ms |
| MySQL 8 | 788ms | 2,088ms | 16ms | 1,034ms | 648ms | 78ms |
| PostgreSQL 16 | 146ms | 333ms | 16ms | 356ms | 280ms | 55ms |

这些数字用于本地回归对比，不是生产性能承诺。机器、容器缓存和数据库参数均未按生产基准环境固定。

### 2.3 正常分钟零重复写

初始聚合推进水位后，再次运行正常 worker：

| 数据库 | 水位 revision 变化 | 路由记录 ID 变化 | 三张分钟表 create/delete |
| --- | ---: | ---: | ---: |
| SQLite | 0 | 0 | 0 |
| MySQL 8 | 0 | 0 | 0 |
| PostgreSQL 16 | 0 | 0 | 0 |

该项满足 `METRIC-04` 删除周期性重复重建后的验收要求。

## 3. 修复与回归证据

### 3.1 修复前失败

修复前基线中，retry 位于第一分钟，final summary 位于第二分钟。dirty worker 分别重建每一分钟，两个事件无法在同一次聚合范围内配对；两分钟全量重建可以配对。

| 数据库 | dirty repair 第一分 retry | 两分钟全量重建 retry | 路由 hash | API Key hash | 时延桶 hash |
| --- | ---: | ---: | --- | --- | --- |
| SQLite | 1,000 | 0 | 不同 | 不同 | 相同 |
| MySQL 8 | 1,000 | 0 | 不同 | 不同 | 相同 |

SQLite 和 MySQL 得到完全相同的失败 hash：

```text
dirty route: db5cc41073179843896c829a31dc1c85b39896e9bdbb1540d9ca6e74a9e01de4
full  route: 72f410c02dcb20ca9c36a7349e98fb2b4ba7a312e8c24b4b2647914184224922
dirty api:   a37d9717055e55428975172783c2ec197f12566b8d16ea12787277e3328d1c6c
full  api:   fea0cd015195eaaac33470fda20f38117702382c8f060366eb60baa74a25e54d
bucket:      64ce9669f4158b2f990e1e8d447458603b84045e23c063903cbd4b47647b6266
```

该基线证明原逐分钟查询无法看到范围外 final summary，修复前的 `METRIC-04` 不满足“定点修复结果与范围全量重建完全一致”。

PostgreSQL 基线还在 `MarkChannelMonitorDirtyMinutes` 幂等 upsert 阶段返回：

```text
ERROR: column reference "mark_count" is ambiguous (SQLSTATE 42702)
```

### 3.2 修复后等价性

`METRIC-04` 修复后，目标分钟包含 retry 时会按 `request_id` 分批读取范围外 companion error 行，用完整路由/API Key/duration 维度进行多重集匹配；普通无 retry 分钟不增加 companion 查询。PostgreSQL upsert 使用当前表限定 `mark_count` 后不再出现 `42702`。

最终矩阵结果：

| 数据库 | 第一分目标日志 | companion 日志 | dirty repair retry | 全量重建 retry | 三类 hash |
| --- | ---: | ---: | ---: | ---: | --- |
| SQLite | 5,001 | 1,000 | 0 | 0 | 全部相同 |
| MySQL 8 | 5,001 | 1,000 | 0 | 0 | 全部相同 |
| PostgreSQL 16 | 5,001 | 1,000 | 0 | 0 | 全部相同 |

第一分钟定点修复的 `scanned_logs=6001`，准确包含 5,001 条目标范围日志和 1,000 条 companion 日志；第二分钟没有目标范围 retry，保持 `scanned_logs=1000`。三库最终 hash 完全一致：

```text
route:  72f410c02dcb20ca9c36a7349e98fb2b4ba7a312e8c24b4b2647914184224922
api:    fea0cd015195eaaac33470fda20f38117702382c8f060366eb60baa74a25e54d
bucket: 64ce9669f4158b2f990e1e8d447458603b84045e23c063903cbd4b47647b6266
```

PostgreSQL 最终运行直接通过正常脏分钟标记路径，fixture fallback 未触发。

### 3.3 关闭分钟写入竞态与故障可见性

关闭分钟日志现在无条件写入 dirty marker，不再先读取 `completed_through` 决定是否标记。确定性回归固定执行“旧水位存在 -> 写入关闭分钟日志 -> 推进水位”的顺序，验证 marker 在水位提交前已经存在，因此不会出现聚合器扫描完成后、提交水位前写入的日志永久漏聚合。

另一个故障注入用例强制 dirty marker 持久化失败，验证源日志已持久化，但 `createLog` 返回包含原始错误的明确失败，不再记录日志后静默报告成功。该行为未重新引入周期性范围回扫。

## 4. 当前体积

下表为加入迟到日志并完成范围重建后的实际分配。数据和索引分别采集；行数较少，MySQL/PostgreSQL 固定页和 B-Tree 初始分配占比较高。

| 数据库 | 表 | 行数 | 数据 | 索引 | 合计 |
| --- | --- | ---: | ---: | ---: | ---: |
| SQLite | route metrics | 2,000 | 328.0KiB | 736.0KiB | 1.04MiB |
| SQLite | API Key metrics | 5,001 | 964.0KiB | 2.55MiB | 3.49MiB |
| SQLite | duration buckets | 1,001 | 132.0KiB | 192.0KiB | 324.0KiB |
| MySQL 8 | route metrics | 2,000 | 1.52MiB | 1.06MiB | 2.58MiB |
| MySQL 8 | API Key metrics | 5,001 | 2.52MiB | 6.06MiB | 8.58MiB |
| MySQL 8 | duration buckets | 1,001 | 208.0KiB | 288.0KiB | 496.0KiB |
| PostgreSQL 16 | route metrics | 2,000 | 2.23MiB | 1.51MiB | 3.74MiB |
| PostgreSQL 16 | API Key metrics | 5,001 | 5.59MiB | 4.89MiB | 10.48MiB |
| PostgreSQL 16 | duration buckets | 1,001 | 560.0KiB | 520.0KiB | 1.05MiB |

### 4.1 稠密保留上限外推

若 1,000 条路由每分钟均活跃，30 天路由表上限为 `43.2M` 行；每路由 4 个 API Key 每分钟均活跃，7 天 API Key 表上限为 `40.32M` 行。按本次小样本的字节/行直接外推：

| 数据库 | 路由表 43.2M | API Key 表 40.32M | 单桶/路由/分钟 43.2M |
| --- | ---: | ---: | ---: |
| SQLite | 22.08GiB | 27.13GiB | 13.34GiB |
| MySQL 8 | 54.38GiB | 67.54GiB | 20.41GiB |
| PostgreSQL 16 | 79.93GiB | 82.61GiB | 44.12GiB |

这是刻意保守的稠密上限，不是容量预测：真实活跃率、每分钟出现的路由/API Key 比例、页填充率、压缩和数据库维护状态都会显著改变结果。它说明保留期配置只能限制时间窗口，无法抵消“所有路由与所有 API Key 每分钟持续活跃”产生的数量级；生产容量评估应使用实际活跃分钟比例重新计算。

## 5. 保留边界

删除条件使用严格小于 cutoff：

| 数据 | cutoff | 准备样本 | 删除 | 保留 |
| --- | ---: | ---: | ---: | ---: |
| 路由分钟指标 | 30 天 | 第 0～31 天，共 32 条 | 第 31 天 1 条 | 第 0～30 天 31 条 |
| 时延桶 | 30 天 | 第 0～31 天，共 32 条 | 第 31 天 1 条 | 第 0～30 天 31 条 |
| API Key 分钟指标 | 7 天 | 第 0～8 天，共 9 条 | 第 8 天 1 条 | 第 0～7 天 8 条 |

SQLite、MySQL 和 PostgreSQL 的边界结果一致。

## 6. 运行方式

```powershell
$env:CHANNEL_MONITOR_METRIC06_DIALECT = 'sqlite'
go test ./service -run '^TestChannelMonitorMetricHighCardinalityValidation$' -count=1 -v

$env:CHANNEL_MONITOR_METRIC06_DIALECT = 'mysql'
$env:CHANNEL_MONITOR_METRIC06_DSN = '<local new_api_metric06 DSN>'
go test ./service -run '^TestChannelMonitorMetricHighCardinalityValidation$' -count=1 -v

$env:CHANNEL_MONITOR_METRIC06_DIALECT = 'postgres'
$env:CHANNEL_MONITOR_METRIC06_DSN = '<local new_api_metric06 DSN>'
go test ./service -run '^TestChannelMonitorMetricHighCardinalityValidation$' -count=1 -v
```

未设置 `CHANNEL_MONITOR_METRIC06_DIALECT` 时该高基数用例跳过，不拖慢普通 `go test ./service`。

## 7. 验收结论

`METRIC-06` 已通过：

- 路由行数不随 API Key 数增长。
- 调度查询不读取 API Key 分钟表。
- 正常分钟无周期性重复写。
- 关闭分钟日志无条件写 dirty marker，消除了扫描完成与水位提交之间的永久漏标竞态。
- dirty marker 持久化失败会向调用方返回错误，不再静默成功。
- 跨分钟 retry/final summary 的定点修复与两分钟全量重建完全一致。
- `generated_rows` 精确包含路由、API Key 和时延桶三类行。
- companion 行已计入 `ScannedLogRows`。
- PostgreSQL 脏分钟幂等标记不再出现 `42702`。
- 30 天路由/时延桶和 7 天 API Key 保留边界正确。
- SQLite、MySQL、PostgreSQL 使用同一代码版本全部通过。
- 独立 MySQL/PostgreSQL 测试表在运行退出后完整清理，两库最终均为 0 张测试表。

剩余说明：

- 耗时仅作为本机后续回归基线，不设硬阈值。
- 稠密体积外推是基于小样本页分配的保守上限，不能直接当作生产容量预测。
- 本轮真实数据库为 MySQL 8.4.10、PostgreSQL 16.14；最低支持版本仍需由项目完整数据库矩阵持续保障。

本子计划没有修改 `model/main.go`、根实施计划或生产清理脚本，也没有执行旧数据兼容、回填、双读双写或生产数据清理。
