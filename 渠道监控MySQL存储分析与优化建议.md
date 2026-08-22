# 渠道监控 MySQL 存储分析与优化建议

> 分析日期：2026-08-15
> 数据来源：生产 MySQL `information_schema`、渠道监控配置、执行明细统计和 binlog 状态
> 分析范围：渠道监控持久化表、系统任务、MySQL binlog 及其磁盘占用

## 1. 结论

生产环境约 44GB 的 MySQL 磁盘占用已经可以完整解释：

| 组成 | 大小 | 说明 |
| --- | ---: | --- |
| `new-api` 数据库全部业务表 | 10.33GiB | `information_schema.tables` 汇总值 |
| MySQL binlog | 30.33GiB | 39 个 binlog 文件，共 32,569,380,939 字节 |
| 已确认合计 | 40.66GiB | 业务表与 binlog 合计 |
| 其他 MySQL 文件 | 约 3.3GiB | InnoDB 系统表空间、redo/undo、临时文件及统计口径差异 |

渠道监控分钟指标并不是主要问题。真正的业务表主因是：

```text
channel_smart_schedule_execution_details
```

该表当前占用 9.35GiB，约占全部业务表空间的 90.5%。binlog 又将高频插入和到期删除放大并保留 30 天，最终形成约 30.33GiB 的额外占用。

核心问题不是保留任务失效，而是以下组合：

1. 生产每天约有 2,100 次完整智能调度，换算后相邻任务平均约 40 秒启动一次。这里的 40 秒是任务启动频率，不是单次任务耗时，也不是健康探测周期。
2. 健康度变化触发完整调度是产品需求，调度范围、执行频率和结果语义不能为了节省存储而改变。
3. 每轮为约 110～140 条路由分别保存完整评分 JSON，即使结果为 `unchanged` 也保存全部评分、采样、稳定性和决策快照。
4. 执行明细保留 14 天，在当前速率下自然形成约 300 万行和 9GB 以上数据。
5. binlog 保留 30 天且使用 `FULL` 行镜像，插入和到期删除都会记录完整的大 JSON 行。

因此，代码优化目标不是减少完整调度次数，而是在完整保留每轮结果和所有历史字段的前提下，降低重复 JSON、行结构和 binlog 带来的存储放大。

本方案采用简单直接的切换策略：

1. 不兼容旧执行明细结构，不做旧数据迁移、双读、回填或灰度格式切换。
2. 采用停机更新，旧版本和新版本不并行运行，不设计滚动发布兼容路径。
3. 能在应用内调整的保留期和清理参数统一保存到 `options`，不继续分散在环境变量和不同设置区域。
4. 渠道监控设置弹窗新增独立的“数据保留”Tab，集中管理全部应用侧保留期限。
5. Redis Streams 是渠道监控实时健康链路的必需依赖，不保留进程内事件队列降级路径。
6. 上线时清空全部渠道监控配置、状态和历史，管理员在新版本中重新配置。
7. 这里“不做兼容”指不兼容旧监控表结构和历史数据；新结构本身仍按项目要求支持 SQLite、MySQL 和 PostgreSQL。

## 2. 生产数据

### 2.1 主要表空间

| 表 | 估算行数 | 数据 | 索引 | 合计 |
| --- | ---: | ---: | ---: | ---: |
| `channel_smart_schedule_execution_details` | 2,394,569 | 8.60GiB | 0.75GiB | 9.35GiB |
| `logs` | 412,779 | 0.36GiB | 0.20GiB | 0.56GiB |
| `system_tasks` | 84,584 | 0.28GiB | 0.03GiB | 0.31GiB |
| `channel_monitor_minute_metrics` | 48,734 | 0.03GiB | 0.02GiB | 0.05GiB |
| `channel_monitor_minute_duration_buckets` | 80,813 | 0.02GiB | 0.02GiB | 0.04GiB |
| `channel_status_probe_executions` | 9,442 | 小于 0.01GiB | 0.01GiB | 0.01GiB |

`information_schema.tables.TABLE_ROWS` 对 InnoDB 是估算值。对执行明细表执行精确 `COUNT(*)` 后得到 2,992,342 行，因此容量和增长评估应以精确统计为准。

分钟指标与延迟分桶合计只有约 90MiB。此前担心的“API Key 分钟维度导致 44GB”在本生产实例中并未发生。

### 2.2 执行明细保留范围

```text
最老记录：2026-08-01 05:43:06
最新记录：2026-08-15 11:32:04
精确行数：2,992,342
```

配置的执行明细保留期为 14 天。最老记录与当前时间基本相差 14 天，说明保留清理任务正在生效，没有明显的长期历史积压。

存在几个小时的边界偏差是正常现象，原因包括：

- 保留任务按周期运行，不是逐秒删除；
- 清理使用分批删除和单轮时间预算；
- 预算耗尽后通过续排任务继续清理。

因此，当前 9.35GiB 主要是“仍在有效保留期内的活数据”，不是可通过一次普通清理直接消除的历史垃圾。

### 2.3 每日产生速度

近期完整日数据如下：

| 日期 | 明细行数 | 调度任务数 | 平均每任务明细数 |
| --- | ---: | ---: | ---: |
| 2026-08-14 | 250,331 | 2,133 | 117.4 |
| 2026-08-13 | 235,347 | 2,124 | 110.8 |
| 2026-08-12 | 246,096 | 2,227 | 110.5 |
| 2026-08-11 | 271,999 | 2,482 | 109.6 |
| 2026-08-10 | 308,848 | 2,569 | 120.2 |
| 2026-08-09 | 319,650 | 2,237 | 142.9 |
| 2026-08-08 | 362,875 | 2,552 | 142.2 |
| 2026-08-07 | 261,357 | 1,863 | 140.3 |

以 2026-08-14 为例：

```text
2,133 次任务/天 = 平均每 40.5 秒一次完整调度
250,331 行/天 = 平均每轮保存 117.4 条执行明细
```

最近一小时统计为：

```text
执行明细：10,560 行
调度任务：88 个
平均每任务：120 行
平均 payload：2,733 字节
最大 payload：3,428 字节
```

最近一小时同样对应相邻完整调度任务平均约 40.9 秒启动一次，说明该任务频率不是偶发尖峰，而是稳定状态。这个数字由“统计窗口秒数 ÷ 不同调度任务数”得到，不代表一轮调度执行了 40 秒，也不代表健康探针每 40 秒执行一次。

每轮任务都会读取当前分组、模型和路由，结合性能、稳定性、健康度和经济性输入重新评分，并计算、应用优先级、权重、主路由和保护状态，最后保存约 110～140 条路由执行详情。健康度变化仍必须走这套完整流程，后续优化不得将其降级为局部刷新。

按近期速率估算，执行明细表每天增加约 0.7～0.9GiB 活数据。14 天保留期下，稳定体积约为 9～12GiB，与当前 9.35GiB 完全吻合。

### 2.4 binlog

生产配置：

```text
binlog_expire_logs_seconds = 2592000  # 30 天
binlog_row_image = FULL
```

当前共有 39 个 binlog 文件，总大小：

```text
32,569,380,939 字节 = 30.33GiB
```

执行明细使用大 JSON 文本。在 ROW + FULL 行镜像下：

- 插入明细会把完整行写入 binlog；
- 更新大字段会记录完整或接近完整的行镜像；
- 14 天后删除明细时，又会把被删除行的完整 before image 写入 binlog；
- 索引维护、系统任务更新和路由状态更新还会产生额外 binlog。

当前 binlog 平均增长约 1.01GiB/天。30 天保留后形成 30.33GiB，与实际结果一致。

## 3. 代码路径分析

### 3.1 每条路由保存一行完整 JSON

`controller/channel_ratio_monitor_schedule.go` 的 `channelSmartScheduleTaskHandler.Run` 会遍历本轮全部 `summary.Adjustments`，为每个 adjustment 构造一个 `ChannelSmartScheduleExecutionDetailInput`：

```go
detailInputs := make([]model.ChannelSmartScheduleExecutionDetailInput, 0, len(summary.Adjustments))
for index, adjustment := range summary.Adjustments {
    detailInputs = append(detailInputs, model.ChannelSmartScheduleExecutionDetailInput{
        AdjustmentIndex: index,
        Payload:         adjustment,
    })
}
```

随后 `model/channel_smart_schedule_execution_detail.go` 将每个 adjustment 独立 JSON 编码并插入：

```go
encoded, err := common.Marshal(input.Payload)
```

表结构：

```go
type ChannelSmartScheduleExecutionDetail struct {
    Id              int64
    TaskId          string
    AdjustmentIndex int
    Payload         string
    CreatedAt       int64
}
```

### 3.2 未变化路由也保存完整评分详情

`controller/channel_ratio_monitor_schedule_route.go` 会为所有非 observation-only 的路由生成 adjustment。未发生路由变化时仍设置：

```go
adjustment.Action = channelSmartScheduleAdjustmentUnchanged
```

随后仍调用 `result.recordAdjustment(adjustment)`，最终进入执行明细表。

`channelSmartScheduleTaskAdjustment` 又包含完整的 `ScoreDetails`：

```go
ScoreDetails *model.ChannelSmartScheduleScoreDetails `json:"score_details,omitempty"`
```

`ChannelSmartScheduleScoreDetails` 包含以下大块数据：

- 窗口、水位和策略信息；
- 成本、首字、TPS、稳定性输入；
- 同组比较范围；
- 各评分组件；
- 健康压力和请求占比；
- 当前、候选和最终主渠道决策；
- 调整、选择和决策原因文本。

这些 JSON 字段名和公共上下文会在同一任务的上百条路由记录中重复存储。

### 3.3 当前拆行没有带来数据库分页收益

`controller/channel_ratio_monitor_schedule_execution.go` 查询详情时，会先按 `task_id` 读取该任务的全部执行明细：

```go
detailsByTask, err := model.GetChannelSmartScheduleExecutionDetails([]string{taskID})
```

之后在 Go 内存中完成：

- JSON 解码；
- 分组、模型、动作和关键字过滤；
- 排序；
- 分页切片。

因此，当前“一条路由一行”的设计没有获得数据库级过滤或分页优势，却付出了数百万行、行头、主键和二级索引的存储成本。

### 3.4 完整调度执行频率与需求约束

完整调度不是固定周期 handler，但许多输入变化都会调用 `requestChannelSmartScheduleRun`，包括：

- 渠道配置、状态和模型变化；
- 上游倍率、余额和成本输入变化；
- 分组成员与分组倍率变化；
- 智能调度设置与手动主渠道变化；
- 路由应用期间发生配置冲突后的重新调度。

`EnqueueRequiredSystemTask` 能保证同类型任务不会无限并发，但当输入在一个任务运行期间继续变化时，会保留下一轮 pending 任务。在持续变化的生产环境中，这可能形成“上一轮结束后继续执行下一轮”的连续调度。

生产数据中的“约 40 秒一次”表示不同完整调度任务的平均启动间隔。它不能用于判断单轮耗时，也不能说明某一种健康探测或输入更新固定每 40 秒触发一次。当前任务记录没有保存 `trigger_source`，因此仅凭现有数据无法区分健康度变化、倍率变化、余额变化、配置变化或冲突重试分别贡献了多少任务。

需求已经明确：健康度变化必须触发完整调度，完整读取、评分、决策和应用流程不能改变。因此，不建议增加 1～5 分钟节流、把健康变化改成池级刷新，或跳过连续完整调度。约 40 秒的平均任务间隔应作为存储容量设计的既定负载，而不是本次优化目标。

## 4. 短期生产治理

### 4.1 调整 binlog 保留期

这是最快释放磁盘的手段。将 30 天调整为 3～7 天，可预期释放约 23～27GiB。

操作前必须确认：

1. 所有 MySQL 副本已追上，且不依赖即将删除的 binlog。
2. 时间点恢复策略是否要求保留 30 天本地 binlog。
3. 备份系统是否已经将 binlog 归档到对象存储或其他独立介质。
4. 当前是否存在基于旧 binlog position 的恢复、迁移或 CDC 任务。

建议优先采用“短本地保留 + 外部归档”，而不是为 PITR 长期占用生产数据库本地磁盘。

不要在未确认副本和恢复链路前直接执行 `PURGE BINARY LOGS`。

明确结论：生产主库不建议直接禁用 binlog。只有在确认实例不承担复制、时间点恢复（PITR）或 CDC，且已有独立备份并接受故障恢复能力下降时，才可以由数据库负责人单独评估关闭；本方案不把“禁用 binlog”作为渠道监控优化手段。`binlog_row_image` 从 `FULL` 调整为 `MINIMAL` 也必须先验证所有复制、恢复和 CDC 消费者，不能替代保留期治理和大 JSON 存储优化。

### 4.2 缩短执行明细保留期

将 `ChannelMonitorExecutionDetailRetentionDays` 从 14 天调整为 3 天：

| 保留期 | 预计行数 | 预计活表体积 |
| ---: | ---: | ---: |
| 14 天 | 约 300 万 | 约 9.35GiB |
| 7 天 | 约 150 万 | 约 4.5～5.5GiB |
| 3 天 | 约 65～80 万 | 约 1.8～2.5GiB |
| 1 天 | 约 25～32 万 | 约 0.7～0.9GiB |

系统任务摘要默认按 7 天保留，且渠道比例监控、智能调度、探测、清理、模型检测、渠道测试和模型更新分别使用独立保留配置。缩短执行明细只改变可查询完整路由快照的时间窗口，不改变调度行为，也不删减保留期内任何路由或字段。

建议生产默认值调整为 3 天。3 天窗口内应完整保留每轮的全部路由详情，包括 `unchanged` 的评分、健康度、采样、稳定性和决策字段；超过窗口后再按统一保留策略清理。

### 4.3 新版本上线前的过渡清理（可选）

如果新表结构能在短期内上线，不需要先花时间删除约 200 万行旧数据，也不需要对旧表执行 `OPTIMIZE TABLE`。先把保留期改为 3 天以阻止继续增长，停机更新时直接重建表即可。

只有新版本暂时不能上线时，才需要加速现有清理任务。当前默认参数较保守：

以下环境变量只适用于仍在运行的旧版本过渡期；`SET-04` 完成后由数据库配置替代，不再保留环境变量读取。

```text
CHANNEL_MONITOR_COST_RETENTION_BATCH_SIZE=1000
CHANNEL_MONITOR_CLEANUP_BUDGET_SECONDS=10
CHANNEL_MONITOR_CLEANUP_CONTINUATION_SECONDS=60
```

缩短保留期后会一次形成约 200 万行以上的删除积压。建议在低峰临时调整为：

```text
CHANNEL_MONITOR_COST_RETENTION_BATCH_SIZE=5000
CHANNEL_MONITOR_CLEANUP_BUDGET_SECONDS=60
CHANNEL_MONITOR_CLEANUP_CONTINUATION_SECONDS=15
```

调整后需要监控：

- MySQL CPU、IOPS 和磁盘队列；
- InnoDB row lock、history list length 和 purge lag；
- 副本延迟；
- binlog 增长速度；
- 网关请求 p95/p99；
- 保留任务是否持续成功、删除行数是否推进。

如果副本延迟或业务延迟明显上升，应降低批次或预算，不应使用单条超大 DELETE。

### 4.4 直接重建表空间

由于已经接受丢弃旧监控历史，执行明细不再采用“批量删除旧行，再 `OPTIMIZE TABLE`”的路径。停机后直接删除旧执行明细表，再由新版本创建任务快照结构，可以一次释放旧 `.ibd` 空间，也不会产生数百万行 DELETE binlog。

分钟指标后续拆表时采用相同策略：清空旧指标、时延桶和聚合游标，从当前时间重新聚合。核心 `channels` 配置和共享 `system_tasks` 表不能整体删除。

## 5. 代码优化建议

### 5.1 P0：锁定完整调度行为边界

健康度变化、倍率变化、余额变化、配置变化和冲突重试仍按现有逻辑请求完整调度，不增加最小间隔，不改成池级刷新，也不改变任务合并、评分、路由应用和保护状态计算语义。

存储优化只允许发生在“完整 adjustment 数组已经生成”之后的持久化编码层，以及查询时对应的解码层。应先用回归测试固定任务触发、adjustment 数量与顺序、各字段值和接口响应，再替换底层存储格式，避免压缩改造意外影响业务计算。

### 5.2 P0：直接改为每任务一个压缩快照

不再保留“一条路由一行”的执行明细结构，也不先做逐行压缩。每轮完整调度结束后，将完整 adjustment 数组一次 JSON 编码并使用固定算法压缩，数据库只保存一行：

```text
id
task_id
payload_blob
item_count
created_at
```

为保持实现简单，压缩算法固定使用 Go 标准库 gzip，不增加第三方压缩依赖、codec 列、格式协商和旧格式判断。数组中仍保存全部 `updated`、`failed`、`skipped` 和 `unchanged` adjustment，以及完整评分、健康度、采样、稳定性和决策字段。

当前详情接口本来就会读取一个任务的全部明细，再在 Go 内存中完成解码、过滤、排序和分页。改成任务快照后仍执行相同的接口处理，对外查询结果不变，只把底层的多行读取改成单行解压。

实现要求只保留必要项：

- 写入前限制 adjustment 数量和未压缩 JSON 最大长度；
- 解压时设置最大输出长度，防止损坏数据或异常内存占用；
- 校验 `item_count` 与解压后的数组长度一致；
- 表上只保留任务唯一索引和创建时间清理索引；
- 使用 GORM 二进制字段映射，让新表结构可在 SQLite、MySQL 和 PostgreSQL 创建和读取；
- 使用真实生产样本验证压缩率、解压耗时和详情接口内存峰值。

该方案预计将每日约 25 万行降到约 2,000 行，并利用同一任务内重复字段获得更高压缩率。具体压缩比例以生产样本测试为准。

### 5.3 配置优先：新增“数据保留”Tab

现有渠道监控设置弹窗只有“倍率、通知与错误”和“探针响应”两个 Tab，并且保留期字段混在第一个 Tab 中。建议新增第三个 Tab：

```text
倍率、通知与错误 | 探针响应 | 数据保留
```

将现有 `ChannelMonitorRetentionFields` 整体移动到“数据保留”，所有保留期使用数字输入框并以“天”为单位。应用侧配置统一写入 `options`，保存后由所有节点从数据库读取，不依赖单节点内存缓存。

确定采用以下字段和默认值：

| 界面字段 | 配置项 | 默认值 | 说明 |
| --- | --- | ---: | --- |
| 成本与聚合数据 | `ChannelMonitorCostRetentionDays` | 30 天 | 当前同时覆盖成本和分钟聚合；拆表后再细分 |
| 调度执行详情 | `ChannelMonitorExecutionDetailRetentionDays` | 3 天 | 保存完整任务快照 |
| 未分类监控系统任务 | `ChannelMonitorTaskRetentionDays` | 7 天 | 必须不小于执行详情保留期 |
| 渠道比例监控任务 | `ChannelMonitorRatioMonitorTaskRetentionDays` | 7 天 | 独立清理 |
| 智能调度任务 | `ChannelMonitorSmartScheduleTaskRetentionDays` | 7 天 | 独立清理 |
| 智能调度探测任务 | `ChannelMonitorSmartScheduleProbeTaskRetentionDays` | 3 天 | 高频任务，独立清理 |
| 清理、模型检测、渠道测试、模型更新任务 | 对应 `ChannelMonitor*TaskRetentionDays` | 7 天 | 各类型独立清理 |
| 倍率变化历史 | `ChannelMonitorRatioHistoryRetentionDays` | 365 天 | 当前数据量很小，可由管理员调整 |
| 状态探测历史 | `ChannelMonitorStatusProbeHistoryRetentionDays` | 7 天 | 保留探测执行记录 |
| 模型检测历史 | `ChannelMonitorModelDetectionRetentionDays` | 30 天 | 从环境变量迁移为数据库配置 |

分钟指标拆分后再增加两个配置，不与成本数据共用保留期：

| 界面字段 | 配置项 | 默认值 |
| --- | --- | ---: |
| 路由分钟指标 | `ChannelMonitorRouteMetricRetentionDays` | 30 天 |
| API Key 明细指标 | `ChannelMonitorApiKeyMetricRetentionDays` | 7 天 |

清理任务的运行参数也放在该 Tab 的“高级清理设置”中，优先使用数据库配置：

```text
启用清理任务
清理周期（分钟）
单批删除行数
单轮清理预算（秒）
积压续跑间隔（秒）
```

这些参数应有安全的最小值、最大值和默认值。MySQL binlog 保留期属于数据库实例级配置，不放入应用 Tab，只在运维文档中维护。

### 5.4 非兼容式停机上线

本次不实现旧表读取和历史数据迁移，采用停机更新，不考虑滚动发布和新旧节点混跑：

1. 停止全部应用实例，确保没有渠道监控任务和调度任务继续写库。
2. 执行 5.5 节清理脚本，删除旧渠道监控表、相关系统任务和配置项。
3. 部署并启动新版本，由 GORM 创建新表结构。
4. 确认 Redis 可用后重新配置渠道监控和“数据保留”设置。
5. 执行一次完整调度，检查 gzip 快照、详情查询、分钟指标和清理任务。

该方式会丢失切换前的渠道监控历史和部分可重建状态，但可以删除双读、旧字段、后台迁移、回填状态和长期兼容分支，显著降低代码和测试复杂度。

### 5.5 停机清理清单

全部渠道监控数据都允许丢弃，因此最简单的方式是直接 `DROP TABLE`，让新版本按新模型重新建表。这也能直接释放旧执行明细表约 9.35GiB 的 `.ibd` 空间。

需要删除的渠道监控表共 21 张：

```text
channel_ratio_monitors
channel_ratio_histories
channel_daily_costs
channel_daily_api_key_costs
channel_monitor_minute_metrics
channel_monitor_minute_duration_buckets
channel_monitor_aggregation_states
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

`system_tasks`、`system_task_locks` 和 `options` 是共享表，不能整体删除，只能删除渠道监控相关记录。停机并确认当前数据库后执行：

以下是基于当前表名的清理脚本。最终执行版本由 `CUT-01` 和 `OPS-01` 在所有新表名冻结后复核。MySQL `DROP TABLE` 会隐式提交，整段脚本不能事务回滚；所有语句必须保持幂等，允许从中断位置重新执行。

```sql
USE `new-api`;

-- 子表、历史表和状态表先删除，避免可能的依赖关系阻止后续删除。
DROP TABLE IF EXISTS `channel_model_detection_cost_events`;
DROP TABLE IF EXISTS `channel_model_detection_executions`;
DROP TABLE IF EXISTS `channel_model_detection_runs`;
DROP TABLE IF EXISTS `channel_model_detection_batches`;
DROP TABLE IF EXISTS `channel_model_detection_targets`;
DROP TABLE IF EXISTS `channel_model_detection_configs`;
DROP TABLE IF EXISTS `channel_model_detection_global_configs`;

DROP TABLE IF EXISTS `channel_status_probe_executions`;
DROP TABLE IF EXISTS `channel_status_probe_states`;
DROP TABLE IF EXISTS `channel_status_probe_configs`;

DROP TABLE IF EXISTS `channel_smart_schedule_execution_details`;
DROP TABLE IF EXISTS `channel_smart_schedule_model_sample_states`;
DROP TABLE IF EXISTS `channel_smart_schedule_group_pauses`;
DROP TABLE IF EXISTS `channel_smart_schedule_route_states`;

DROP TABLE IF EXISTS `channel_monitor_minute_duration_buckets`;
DROP TABLE IF EXISTS `channel_monitor_minute_metrics`;
DROP TABLE IF EXISTS `channel_monitor_aggregation_states`;
DROP TABLE IF EXISTS `channel_daily_api_key_costs`;
DROP TABLE IF EXISTS `channel_daily_costs`;
DROP TABLE IF EXISTS `channel_ratio_histories`;
DROP TABLE IF EXISTS `channel_ratio_monitors`;

-- 共享任务表只删除渠道监控使用的任务和任务锁。
DELETE FROM `system_task_locks`
WHERE `type` IN (
    'channel_ratio_monitor',
    'channel_smart_schedule',
    'channel_smart_schedule_probe',
    'channel_monitor_cost_retention',
    'channel_model_detection'
);

DELETE FROM `system_tasks`
WHERE `type` IN (
    'channel_ratio_monitor',
    'channel_smart_schedule',
    'channel_smart_schedule_probe',
    'channel_monitor_cost_retention',
    'channel_model_detection'
);

-- 删除现有及后续新增的渠道监控配置，启动后在界面重新保存。
DELETE FROM `options`
WHERE `key` LIKE 'ChannelMonitor%'
   OR `key` LIKE 'ChannelModelDetection%';
```

以下共享或核心表不能清空：

```text
channels
abilities
logs
system_tasks（只能按上述 type 删除）
system_task_locks（只能按上述 type 删除）
options（只能按上述 key 前缀删除）
```

`channel_test` 系统任务也不删除，因为它属于通用渠道测试能力，不是渠道监控专用任务。旧 `logs` 中即使包含探测或调度日志也不需要删除；新分钟聚合从当前时间开始，不回填旧日志。

### 5.6 P2：补充触发来源与存储指标

当前 `system_tasks` 无法直接判断每轮完整调度由哪个入口触发。建议任务 payload 或审计字段增加：

```text
trigger_source
trigger_count
first_requested_at
last_requested_at
dirty_reasons
```

可能的来源包括：

- manual；
- settings_update；
- ratio_update；
- balance_update；
- channel_update；
- group_update；
- route_conflict_retry；
- upstream_model_update。

同时增加指标：

- `channel_smart_schedule_runs_total{trigger}`；
- `channel_smart_schedule_adjustments_total{action}`；
- `channel_smart_schedule_detail_bytes_total`；
- `channel_smart_schedule_details_per_run`；
- 各触发来源请求次数以及现有任务合并次数；
- 执行明细表每日新增行数和字节数；
- 保留任务删除行数、剩余最老时间和续跑次数；
- binlog 当前总量和每日增长量。

触发来源和指标仅用于定位增长原因与容量规划，不得作为跳过健康度变化完整调度的条件。

## 6. 全表扩展性与高并发优化审查

当前生产环境只有执行明细表已经形成明显的磁盘问题，但“现在体积小”不代表表结构适合未来规模。渠道、分组、模型、API Key 和网关实例数量继续增加后，主要风险会从磁盘占用扩展到事件丢失、节点状态不一致、组合维度膨胀、数据库写放大和锁竞争。

后续设计应按以下增长关系进行容量评估：

```text
路由数 ≈ 渠道数 × 分组数 × 模型数的有效组合
执行明细写入量 ≈ 完整调度次数 × 路由数
当前分钟指标行数 ≈ 分钟数 × 渠道 × 分组 × 模型 × 活跃 API Key
探测与采样写入量 ≈ 渠道 × 模型 × 探测或采样频率
```

其中，执行明细和分钟指标都存在维度乘法效应。完整调度需求不变时，必须通过改变存储粒度和聚合架构解决，不能依赖降低调度频率。

### 6.1 优化优先级

| 优先级 | 问题 | 主要影响 | 优化方向 |
| --- | --- | --- | --- |
| P0 | 实时健康事件使用进程内非阻塞队列 | 高并发时丢事件，多实例健康状态不一致 | Redis Streams 必需依赖和统一聚合器 |
| P0 | 每轮完整调度按路由逐行保存大 JSON | 数据量随路由数线性增长，放大表、索引和 binlog | 清空旧明细，直接切换为每任务一个压缩快照 |
| P1 | 分钟指标包含 API Key 维度 | 渠道、分组、模型和 API Key 形成组合膨胀 | 拆分路由分钟指标与 API Key 分析指标 |
| P1 | 聚合任务反复删除和重建最近时间范围 | 扫描、删除、插入和 binlog 写放大 | 改为关闭分钟增量聚合和脏分钟定点修复 |
| P1 | 路由状态索引顺序不匹配热查询 | 路由数增加后查询和加锁成本上升 | 增加 `(group_name, model_name, channel_id)` 索引 |
| P1 | 路由热状态与大评分 JSON 同表 | 缓存刷新、状态更新和 binlog 携带冷数据 | 拆分窄热状态与压缩解释快照 |
| P2 | 模型采样状态每次重写完整 JSON | 全局锁竞争和 O(样本数) 写入成本 | 追加式样本或 Redis 环形缓冲，滚动聚合 |
| P2 | 探测模型配置及统计桶使用 JSON | 无法单模型调度，状态更新反复重写序列 | 模型配置子表化，共用分钟聚合或紧凑计数器 |
| P2 | `system_tasks` 索引与查询组合不完全匹配 | 调度任务增长后列表、抢占和清理变慢 | 按 `EXPLAIN` 增加 `(type, status, id)` 等索引 |
| P3 | 模型检测报告可能达到大 JSON 规模 | 大范围启用检测后数据表和 binlog 增长 | 压缩或外置报告，MySQL 只保留结构化摘要 |

### 6.2 实时健康事件链路

`service/channel_monitor_event_queue.go` 当前使用进程内非阻塞队列和进程内实时投影：

- 队列容量为 4,096，批量处理上限为 256；
- 队列满时新事件会被丢弃，调用方通常没有处理入队失败；
- 去重和实时投影只存在当前网关进程；
- 实时投影最多保留 512 个 `(channel, model)` 路由，每条路由最多保留 1,000 个事件；
- 超过 512 个活跃渠道模型组合后会发生 LRU 淘汰；
- 多节点部署时，每个节点只能看到本机请求产生的事件。

自适应健康度和智能调度会使用这些实时数据。高并发或多实例情况下，静默丢失和节点视图不一致会直接影响健康判断，这是正确性风险，不只是性能问题。

确定采用 Redis Streams：

1. Redis 是启用渠道监控的必需依赖，启动时校验连接和 Stream 能力，不满足条件则渠道监控不能启动。
2. 不保留进程内事件队列降级路径，避免 Redis 故障后各节点产生互不一致的本地健康状态。
3. 由单个逻辑聚合器完成去重和聚合；可以有多个待命消费者用于故障接管，但同一时刻只有持有租约的一个消费者执行副作用，所有网关读取同一份共享健康状态。
4. 优先保存计数、错误类别和时延桶等聚合值，避免每条路由长期保留 1,000 个完整事件。
5. 定义队列积压、处理延迟、消费失败和 Redis 不可用告警。
6. 健康度变化仍触发现有完整调度；Redis Streams 只保证输入完整一致，不改变调度范围和语义。

### 6.3 分钟指标维度拆分

`channel_monitor_minute_metrics` 当前唯一粒度实际为：

```text
minute_start + channel_id + model_key + group_key + api_key_key
```

每行还重复保存最长 255 字符的完整模型名、分组名和 API Key 名称。当前生产只有 48,734 行和 0.05GiB，说明目前活跃组合有限，但未来行数会同时受到路由数量和活跃 API Key 数量影响。

建议拆分为：

```text
路由分钟指标：minute + channel + model + group
API Key 分析指标：minute + channel + model + group + api_key
```

调度、健康度和路由性能查询只使用路由分钟表；API Key 成本分析和问题排查使用独立表。API Key 表必须保留模型和分组维度，避免现有筛选功能回归，但可以采用更短保留期，防止其高基数拖累核心调度数据。

`channel_monitor_minute_duration_buckets` 当前已经是 `minute + channel + model + group + bucket` 的路由粒度，不包含 API Key。它不需要再拆表，只需跟随路由分钟指标的保留期，并补充与查询匹配的索引。

现有分钟指标和时延桶表主要使用单列索引，而查询经常组合时间、渠道、模型和分组。应先使用生产查询执行 `EXPLAIN`，再按实际选择性增加：

```text
(channel_id, minute_start)
(channel_id, model_key, minute_start)
(group_key, minute_start)
```

查询条件应尽量使用已有哈希键，避免直接在宽字符串名称上建立过多复合索引。拆表时不迁移旧分钟数据，但新表和查询仍需要在 SQLite、MySQL 和 PostgreSQL 正常工作。

### 6.4 分钟聚合写放大

当前聚合器为了处理延迟日志，会重复处理已经聚合过的数据：

- 常规运行重建最近约 2 分钟；
- 启动时重建最近约 5 分钟；
- 每小时修复前约 65 分钟；
- 每次先删除范围内的时延桶和分钟指标，再重新插入全部结果。

该方式在低流量下实现简单，但请求量增加后会反复扫描日志、维护索引，并为 DELETE 和 INSERT 产生大量 binlog。

建议改成：

1. 正常路径只增量聚合已经结束的分钟。
2. 延迟日志到达时记录对应的脏分钟。
3. 后台只重建实际标脏的分钟，不再每小时无差别重建 65 分钟。
4. 能保证幂等时使用 upsert 或差量合并；无法保证时仍可按单个脏分钟删除重建。
5. 记录扫描行数、重建分钟数、延迟日志数和聚合耗时指标。

### 6.5 路由状态索引与冷热字段

`channel_smart_schedule_route_states` 当前唯一索引顺序是：

```text
(channel_id, group_name, model_name)
```

智能调度的路由池查询和加锁经常按 `group_name + model_name` 查找路由。MySQL 不能利用现有索引的左前缀有效处理该条件。随着路由数增加，应新增：

```text
(group_name, model_name, channel_id)
```

上线前需要用热查询 `EXPLAIN` 验证，并确认新增索引的写入成本可接受。

该表还同时保存运行时路由状态和 `last_schedule_score_details` 大 JSON。未来路由达到几十万时，应拆分为：

- 窄热状态表：权重、优先级、健康状态、暂停状态、更新时间等运行时字段；
- 解释快照表：按路由保存最新评分依据，使用固定算法压缩。

这样可以减少缓存全量刷新、热状态更新和 binlog 携带的冷数据。

### 6.6 模型滚动采样状态

`channel_smart_schedule_model_sample_states` 每个 `(channel, model)` 保存最多约 1,500 个样本的 JSON。每次保存探测、手动或状态样本时，会在锁内读取全部样本、解析、排序、重新计算并重写完整 JSON；当前实现还使用进程内全局锁串行化采样保存。

当前只有 75 行，不需要立即迁移，但渠道模型组合和探测频率增加后会形成写入与锁竞争。建议演进为：

- 按路由分片锁，避免无关路由互相阻塞；
- Redis 环形缓冲区或追加式有保留期的样本行；
- 独立维护滚动计数、成功率和延迟桶；
- 定期批量压缩或清理，而不是每次重写 1,500 个样本。

### 6.7 状态探测表

状态探测包括配置、当前状态和执行记录：

- `channel_status_probe_configs`：每渠道一行，探测模型保存在 `models_json`；
- `channel_status_probe_states`：每个渠道模型一行，每次更新会重写分钟、小时、天三个 JSON 桶序列；
- `channel_status_probe_executions`：追加式执行历史，当前 9,442 行、约 0.01GiB，默认保留 7 天。

执行历史当前没有容量问题，但存在较多重叠索引，未来探测频率提高后会增加写入成本。不能仅凭结构直接删索引，应结合慢查询、`EXPLAIN` 和索引使用情况审计。

当每个模型需要独立启停、抢占或执行计划时，应将 `models_json` 规范化为子表：

```text
config_id + channel_id + model_name + enabled + next_run_at
```

状态 JSON 桶可逐步改为共享分钟聚合或固定大小的紧凑计数器，避免每次状态变化重写多个时间序列。

### 6.8 系统任务表

`system_tasks` 当前 84,584 行、0.31GiB。完整调度约每天产生 2,100 个任务，按当前高频任务默认 7 天保留，调度任务规模约控制在 1.5 万行量级，此外还包含其他系统任务。

现有索引主要是单列 `type` 和 `status`，实际查询常见组合为：

```text
(type, status, id)
(type, id DESC)
```

应通过生产 `EXPLAIN` 验证后增加 `(type, status, id)`，并评估 `(type, id)` 是否仍有额外收益。任务的 payload、state、result 和 error 都是文本字段，执行详情迁移到快照表后，任务结果只应保存紧凑摘要和快照引用，避免重复保存完整路由详情。

### 6.9 成本、倍率历史和模型检测

以下表当前结构总体合理，不应作为首轮优化对象：

- `channel_daily_costs` 按渠道和日期聚合，规模有界；
- `channel_daily_api_key_costs` 按渠道、日期和 API Key 指纹聚合，存储粒度合理；
- `channel_ratio_monitors`、`channel_smart_schedule_group_pauses` 和 `channel_monitor_aggregation_states` 都是有界配置或状态；
- 模型检测配置、目标和全局配置表都是低基数配置数据。

仍需注意：

1. 成本事件使用进程内批处理时，应监控 pending Key 上限、数据库失败重试和事件丢弃率；高 API Key 基数下不能静默丢成本事件。
2. `channel_ratio_histories` 保留 365 天，增长后可按查询增加 `(channel_id, created_time, id)` 复合索引。
3. 模型检测执行记录的 `report_json` 和 `official_config_json` 可能较大。大范围启用后，应压缩或外置完整报告，只在 MySQL 保存结构化摘要、状态和对象引用。
4. 模型检测成本事件索引较多，只有检测频率显著提高后才需要结合实际查询审计。

### 6.10 全表结论

| 表 | 当前规模 | 未来增长维度 | 风险 | 结论 |
| --- | ---: | --- | --- | --- |
| `channel_smart_schedule_execution_details` | 299 万行，9.35GiB | 调度次数 × 路由数 | 极高 | 必须压缩并最终改为任务快照 |
| `channel_monitor_minute_metrics` | 48,734 行，0.05GiB | 分钟 × 路由 × API Key | 高 | 拆分路由与 API Key 粒度 |
| `channel_monitor_minute_duration_buckets` | 80,813 行，0.04GiB | 分钟 × 路由 × 时延桶 | 中 | 保持路由粒度，优化索引、聚合和保留期 |
| `channel_smart_schedule_route_states` | 177 行 | 渠道 × 分组 × 模型 | 高 | 补热查询索引，拆分冷热字段 |
| `channel_smart_schedule_model_sample_states` | 75 行 | 渠道 × 模型 × 采样频率 | 中 | 后续改追加或环形缓冲和分片锁 |
| `channel_status_probe_executions` | 9,442 行，0.01GiB | 渠道 × 模型 × 探测频率 | 中 | 保持 7 天，审计重叠索引 |
| `channel_status_probe_states` | 9 行 | 渠道 × 模型 | 中 | 避免反复重写多组 JSON 桶 |
| `channel_status_probe_configs` | 9 行 | 渠道与探测模型 | 中 | 需要独立模型调度时子表化 |
| `channel_ratio_histories` | 579 行 | 渠道 × 调整次数 × 保留期 | 低 | 增长后增加渠道时间复合索引 |
| `channel_daily_api_key_costs` | 1,086 行 | 日期 × 渠道 × API Key | 中 | 保持粒度，监控批处理丢弃和基数 |
| `channel_daily_costs` | 401 行 | 日期 × 渠道 | 低 | 保持现状 |
| `channel_ratio_monitors` | 39 行 | 渠道 | 低 | 保持现状 |
| `channel_smart_schedule_group_pauses` | 2 行 | 分组 | 低 | 保持现状 |
| `channel_monitor_aggregation_states` | 1 行 | 固定状态 | 低 | 保持现状 |
| `channel_model_detection_global_configs` | 1 行 | 固定配置 | 低 | 保持现状 |
| `channel_model_detection_configs` | 3 行 | 检测配置 | 低 | 保持现状 |
| `channel_model_detection_targets` | 3 行 | 检测目标 | 低 | 保持现状 |
| `channel_model_detection_batches` | 0 行 | 检测批次 | 低 | 按检测规模观察 |
| `channel_model_detection_runs` | 4 行 | 检测频率 | 中 | 保留期控制，关注报告大小 |
| `channel_model_detection_executions` | 4 行 | 运行 × 渠道 × 目标 | 中 | 压缩或外置大报告 |
| `channel_model_detection_cost_events` | 196 行 | 检测调用次数 | 低 | 当前保持，增长后审计索引 |
| `system_tasks` | 84,584 行，0.31GiB | 任务频率 × 保留期 | 中 | 增加组合索引，保持结果摘要紧凑 |

综合来看，当前只有执行明细表已经发生容量问题；但面向大并发和更多渠道、分组、模型时，应优先重构实时事件链路、执行详情存储、分钟指标粒度和聚合方式。其余表按上述触发条件演进，避免在低基数配置表上提前做复杂改造。

## 7. 可独立执行的实施计划

### 7.1 拆分和执行规则

以下每个编号都应作为一个独立开发任务，可以单独理解、实现、测试和验收。拆分规则如下：

1. 本地 MySQL、Redis、前后端依赖已经就绪，子计划不再包含环境搭建和依赖安装。
2. 每个计划同时完成代码、行为测试和必要文档，不把测试集中到最后一个大任务。
3. 每个计划结束时仓库必须可编译，相关功能不能停留在只有写入没有读取、只有表没有清理的状态。
4. 不添加旧表双读、双写、历史回填和格式兼容；需要改表时使用 5.5 节停机清理策略。
5. 同一轨道按编号串行，避免多个代理同时修改同一批文件；不同轨道可以并行。
6. 单个计划原则上只覆盖一个稳定业务行为和 2～6 个生产文件。分钟指标原子切换是唯一较大的核心计划，内部可使用子代理分别核对模型、查询和测试，但必须一次完整交付。
7. 后端计划运行目标包 `go test`；前端计划运行对应 `bun test`、`bun run typecheck` 和涉及文件 lint。新增表还要验证 SQLite、MySQL、PostgreSQL。
8. 派生会话只认领一个编号，不修改第 7 节主文档、其他编号的代码或测试；需要补充说明时在交付报告中提出，由主会话统一合并。
9. `model/main.go` 中的 `migrateDB`、`migrateDBFast` 模型注册由主会话在 `CUT-01` 统一整合；`controller/channel_monitor_cost_retention.go` 的清理编排只由设置轨道（`SET-04`、`SET-05`）先完成，`SNAP-02` 必须等待设置轨道后再接入快照清理。并行计划不要在这些共享注册区互相抢改；本地测试可在 fixture 中直接 `AutoMigrate` 目标模型。
10. 不得在任何派生会话执行生产清理 SQL、`PURGE BINARY LOGS`、`DROP TABLE`、Redis `FLUSHDB` 或未限定前缀的删除命令。只允许在本地临时数据库/Redis 验证，生产清理由用户按 `OPS-01` 单独执行。
11. `METRIC-01` 是原子垂直切换，不能拆成多个并行代码会话；其中的子代理只能做只读代码核对、测试设计或基准准备，不能分别提交模型、聚合和查询的半成品。

推荐并行方式：

| 轨道 | 计划 | 轨道内顺序 | 可并行关系 |
| --- | --- | --- | --- |
| 设置与界面 | `SET-01`～`SET-05` | 按编号串行 | 可与快照、Redis、索引并行 |
| 调度快照 | `SNAP-01`～`SNAP-03` | 按编号串行 | 可与设置、Redis、指标并行 |
| Redis 实时链路 | `REDIS-01`～`REDIS-08` | `01→02→03` 后 `04/05/07` 可并行；`04` 完成后做 `06`，全部完成后做 `08` | 可与快照、指标、索引并行 |
| 分钟指标 | `METRIC-01`～`METRIC-06` | 按编号串行 | `METRIC-02` 等设置接线计划等待 `SET-05` |
| 索引与后续结构 | `INDEX-*`、`ROUTE-*`、`SAMPLE-*`、`PROBE-*`、`DETECT-*` | 按各计划依赖执行；`INDEX-01`、`INDEX-02` 可并行 | 后续结构达到阈值再做，不与相同模型改动并行 |
| 上线与验收 | `CUT-*`、`OPS-*`、`VERIFY-*` | 核心代码完成后执行 | 不与生产写入同时执行 |

共享文件的互斥边界：

| 共享区域 | 负责轨道 | 规则 |
| --- | --- | --- |
| `web/src/features/channel-monitor/**` 设置弹窗、保留字段、设置 schema/payload | 设置与界面 | `SET-01`～`SET-05` 串行；`METRIC-02` 必须等待 `SET-05` |
| `controller/channel_monitor_cost_retention.go` 清理任务编排 | 设置与界面 | `SNAP-02` 必须等待 `SET-05`，不得并行改同一文件 |
| `model/main.go` 的迁移注册 | `CUT-01` | 其他计划只新增模型和 fixture，不直接编辑迁移注册区 |
| `model/channel_monitor_minute*.go`、聚合服务和指标查询 | 分钟指标 | `METRIC-01`～`METRIC-06` 在轨道内串行 |
| 实时事件队列、Redis 投影和事件状态页 | Redis 实时链路 | 仅 `REDIS-*` 轨道修改；`REDIS-08` 最后删除旧链路 |

为尽快获得收益，开发提交建议分为四批，但生产只在前三批核心代码全部完成后停机切换一次：

1. 第一批“存储止血”：`SET-01`～`SET-05`、`SNAP-01`～`SNAP-03`、`INDEX-01`、`INDEX-02`。
2. 第二批“实时正确性”：`REDIS-01`～`REDIS-08`。
3. 第三批“高基数聚合”：`METRIC-01`～`METRIC-06`。
4. 第四批“达到阈值再做”：`ROUTE-01`、`SAMPLE-*`、`PROBE-*`、`DETECT-01`、`INDEX-03`。

前三批开发期间都只在本地和测试环境验证，不针对生产设计中间态兼容。最终由 `CUT-01`、`OPS-01`、`OPS-02`、`VERIFY-01` 和 `VERIFY-02` 完成一次停机切换。

### 7.2 设置与“数据保留”Tab

#### SET-01：收敛现有默认保留期

- 范围：成本与路由分钟指标默认保留 30 天，API Key 分钟指标默认 7 天，执行详情默认 3 天；高频系统任务按类型默认保留 7 天，智能调度探测任务默认 3 天，倍率历史 365 天、状态探测 7 天保持不变。
- 主要文件：`controller/channel_ratio_monitor_settings.go`、`web/src/features/channel-monitor/lib/schema.ts` 及现有保留配置测试。
- 验收：`options` 缺失时 API 和 UI 返回对应独立默认值，保存后正确落库；不修改设置页布局。
- 依赖：无。

#### SET-02：新增独立“数据保留”Tab

- 范围：`ChannelMonitorSettingsSection` 增加 `retention`，设置弹窗改为三个 Tab，将现有 `ChannelMonitorRetentionFields` 原样从“倍率、通知与错误”移动到“数据保留”。
- 主要文件：`web/src/features/channel-monitor/components/channel-monitor-settings-dialog.tsx` 及组件测试。
- 验收：三个 Tab 可键盘切换，五个现有字段只出现在新 Tab，保存 payload 与移动前完全一致，窄屏文字不溢出。
- 依赖：无后端依赖，可与 `SET-01` 分开完成，但为避免同一前端测试冲突建议串行。

#### SET-03：模型检测保留期改为数据库配置

- 范围：新增 `ChannelMonitorModelDetectionRetentionDays`，默认 30 天、范围 7～180 天；删除 `CHANNEL_MODEL_DETECTION_RETENTION_DAYS` 读取，贯通设置 GET、更新、校验、`options` 和清理任务。
- 主要文件：`controller/channel_ratio_monitor_settings.go`、`controller/channel_monitor_cost_retention.go`、前端 `types.ts`、schema、payload 和保留字段组件。
- 验收：数据库值覆盖节点旧缓存，缺失使用 30，越界请求被拒绝，清理任务使用数据库最新值。
- 依赖：`SET-02`。该计划会修改保留字段组件、schema 和 payload，必须等待数据保留 Tab 的容器结构完成；后端逻辑也在此计划内一起验收。

#### SET-04：清理批次、预算和续跑间隔配置化

- 范围：新增 `ChannelMonitorCleanupEnabled=true`、`ChannelMonitorCleanupBatchSize=1000`（1～10000）、`ChannelMonitorCleanupBudgetSeconds=10`（1～300）、`ChannelMonitorCleanupContinuationSeconds=60`（15～3600），删除 `CHANNEL_MONITOR_COST_RETENTION_ENABLED` 及对应批次、预算、续跑间隔环境变量读取。启用开关使用“数据保留”Tab 的布尔开关，默认开启。
- 主要文件：设置 API、`controller/channel_monitor_cost_retention.go`、“数据保留”Tab 的“高级清理设置”和相关测试。
- 验收：启用开关、边界校验、持久化和回显正确；每轮清理从数据库直读开关、批次和预算；关闭时不创建清理任务，下一次续排使用新间隔。
- 依赖：`SET-02`、`SET-03`，按顺序执行以避免同文件冲突。

#### SET-05：清理周期配置化

- 范围：新增 `ChannelMonitorCleanupIntervalMinutes=1440`，范围 60～10080；删除 `CHANNEL_MONITOR_COST_RETENTION_INTERVAL_MINUTES` 读取。
- 主要文件：设置 API、清理任务 `Interval()`、高级清理设置和对应测试。
- 验收：默认、有效值和越界处理正确，保存后多节点同步配置并从下一次调度周期生效。
- 依赖：`SET-04`。

### 7.3 调度任务 gzip 快照

#### SNAP-01：任务级 gzip 快照垂直改造

- 范围：继续使用 `channel_smart_schedule_execution_details` 表名和现有模型调用入口，将结构一次改为 `id/task_id/payload_blob/item_count/created_at`；完整数组使用 `common.Marshal` 后 gzip，写入、读取、任务完成事务和详情 API 一次切换。
- 约束：`task_id` 唯一、`created_at` 清理索引；最多 20,000 个 adjustment，未压缩 JSON 上限 64MiB，解压使用上限加一字节检查；不跳过无效项。
- 验收：完整字段和顺序、重复任务替换、空数组、越界、损坏 gzip、数量不一致、事务回滚、筛选排序分页全部通过；数据库启动前需执行 `OPS-01` 清理旧表。
- 依赖：无代码依赖，是一个完整垂直计划，不再拆成只写或只读任务。

#### SNAP-02：快照保留清理和任务保护

- 范围：复用快照的 `id` 和 `created_at` 接入现有批量清理，更新历史清理测试和任务联动删除测试，不另写一套清理器。
- 验收：3 天快照按批次删除；pending、running 和每类最新 100 个任务不被误删；任务删除与快照删除保持现有事务语义；预算耗尽可续跑。
- 依赖：`SNAP-01`、`SET-05`。该计划会修改共享清理编排，必须等待设置轨道完成，避免与 `SET-04`、`SET-05` 并行改同一文件。

#### SNAP-03：调度触发归因和快照指标

- 范围：在调度任务 payload 或审计字段记录 `trigger_source/trigger_count/first_requested_at/last_requested_at/dirty_reasons`，增加每轮 adjustment 数、未压缩字节、压缩字节、压缩耗时和解压失败指标。
- 验收：所有现有触发入口都有明确来源或兜底来源；观测字段不参与调度决策；相同触发下调度结果与改造前一致。
- 依赖：`SNAP-01`。

### 7.4 Redis Streams 实时健康链路

#### REDIS-01：Stream 基础契约和启动检查

- 范围：定义带版本的原始 Stream、消费组、消费者、租约和共享投影 key；Redis 最低版本定为 6.2，以使用 `XAUTOCLAIM`。
- 验收：消费组重复创建幂等；Redis 缺失、版本不足或 Stream 能力不可用时渠道监控明确启动失败，不进入本地降级。
- 依赖：无。

#### REDIS-02：事件发布器

- 范围：复用 `model/channel_monitor_event.go` 校验和 JSON 编码，使用有界超时的 `XADD` 发布；发布失败返回明确状态、标记实时链路不可用并记录日志和指标，不再依赖进程内序号，也不把监控发布失败伪装成上游业务响应失败。
- 验收：并发发布 N 个合法事件后 Stream 中恰好有 N 个不同 `event_id`；非法事件不写入；超时和断连可观测。
- 依赖：`REDIS-01`。

#### REDIS-03：可靠消费、幂等和故障接管

- 范围：实现 `XREADGROUP`、批处理、处理成功后 `XACK`、失败保留 pending、`XAUTOCLAIM` 接管和 `event_id` 幂等；使用 Redis 租约保证同一时刻只有一个逻辑聚合器执行副作用。
- 验收：消费者中止后新消费者可接管，重复投递只生效一次，处理失败不 ACK，原始 Stream 不会越过未确认水位清理。
- 依赖：`REDIS-02`。先固定发布字段和失败语义，再实现消费与幂等，避免并行修改 Stream 契约。

#### REDIS-04：共享路由健康窗口

- 范围：将调度使用的 `(channel, model)` 健康窗口写入 Redis，共享保留最近 60 分钟且每路由最多 1,000 个紧凑调度样本，不长期保存完整原始事件；删除全局 512 路由限制。
- 验收：超过 512 条活跃路由仍全部可查询；第 1001 条淘汰最旧样本；乱序、重复和并发更新结果确定；不同节点读取一致。
- 依赖：`REDIS-03`。

#### REDIS-05：共享看板和当日成本投影

- 范围：把全局、渠道、模型、分组、API Key、性能和当日成本的实时投影改成 Redis 紧凑计数、金额和时延统计，不在每个节点保留完整事件。
- 验收：两个节点查询结果一致；未结算成本转已结算不重复累计；跨日数据正确过期。
- 依赖：`REDIS-03`，可与 `REDIS-04` 并行。

#### REDIS-06：完整调度触发闭环

- 范围：只有逻辑聚合器执行健康变化副作用；共享健康投影写入成功且完整调度任务成功入队后才 ACK，失败通过 pending 重放。
- 验收：所有 `SchedulingEligible` 健康变化仍触发现有完整调度；非调度事件不触发；任务入队失败可重试；重复消费不产生重复有效任务。
- 依赖：`REDIS-03`、`REDIS-04`。

#### REDIS-07：积压和故障可观测性

- 范围：接口和页面展示 Redis 状态、pending 数、最老未处理事件、消费延迟、最后发布/处理时间、重试和接管次数，替换本地队列统计。
- 验收：积压、消费者停止或 Redis 故障时返回 `realtime_degraded=true`，恢复后自动解除；前后端状态测试通过。
- 依赖：`REDIS-02`、`REDIS-03`。

#### REDIS-08：一次性切换并删除旧链路

- 范围：接入应用启动和关闭流程，切换全部生产者与查询消费者，删除 `channel_monitor_event_queue.go`、4096 队列、512 路由上限、本地消费者注册和本地投影降级路径。
- 验收：双节点端到端、Redis 重启、消费者接管、超过 512 路由和高并发发布测试通过；代码搜索不存在旧队列和降级入口。
- 依赖：`REDIS-02`～`REDIS-07` 全部完成。

### 7.5 分钟指标和增量聚合

#### METRIC-01：原子拆分路由与 API Key 分钟指标

- 范围：新路由表粒度为 `minute+channel+model+group`；新 API Key 表保留 `minute+channel+model+group+api_key`，以维持现有渠道、模型和分组过滤；聚合器一次扫描同时生成两表和现有时延桶，全部读写查询一次切换。
- 约束：API Key 表只保存成功、失败和缓存等明细统计；TPS、首字时延、最新值和时延桶等路由指标只在路由表；不迁移或读取旧分钟表。
- 目标表名先按下表冻结，`CUT-01` 只负责最终核对和迁移注册，不允许各派生会话自行另起名称：

  | 用途 | 目标表 | 负责计划 |
  | --- | --- | --- |
  | 路由分钟指标 | `channel_monitor_minute_route_metrics` | `METRIC-01` |
  | API Key 分钟明细 | `channel_monitor_minute_api_key_metrics` | `METRIC-01` |
  | 路由分钟时延桶（沿用） | `channel_monitor_minute_duration_buckets` | `METRIC-01` |
  | 脏分钟标记 | `channel_monitor_dirty_minutes` | `METRIC-03` |

- 验收：两个 API Key 命中同一路由时产生 1 条路由记录和 2 条 API Key 记录，两表计数可核对，调度、今日概览和 API Key 明细接口无回归。
- 依赖：无。这是为避免不可用中间状态而保留的原子计划，必须由一个会话一次完成模型、聚合、查询、清理接线和测试；子代理只能做只读核对、测试设计或基准准备。

#### METRIC-02：两类分钟指标独立保留期

- 范围：增加 `ChannelMonitorRouteMetricRetentionDays=30` 和 `ChannelMonitorApiKeyMetricRetentionDays=7`；路由指标与时延桶保留 30 天，API Key 指标保留 7 天，日成本继续使用成本保留期。
- 验收：设置 API、数据保留 Tab、清理结果和预算续跑完整接通；清理不得越过智能调度最长性能/稳定性窗口。
- 依赖：`METRIC-01`、`SET-05`；不提前创建没有消费者的占位配置。设置 API、schema 和 Tab 由设置轨道先完成，避免并行冲突。

#### METRIC-03：脏分钟持久化和标记

- 范围：新增主库脏分钟小表和幂等标记/领取 API；迟到日志以及跨分钟 retry/final summary 标记所有受影响分钟，暂时保留现有修复 worker。
- 验收：重复标记只保留一条，领取失败不丢标记，跨分钟事件能标记原重试分钟；新表三数据库可用。
- 依赖：`METRIC-01`。

#### METRIC-04：关闭分钟和定点修复 worker

- 范围：正常分钟在安全延迟后只聚合一次并推进 `completed_through`；worker 小批领取脏分钟，只替换目标分钟，成功删除标记、失败保留；删除最近 2 分钟重复重建和每小时 65 分钟 blanket repair。
- 验收：正常分钟不重复写；迟到成功、失败和重试可修正；多次修复幂等；启动追赶有效；结果与范围全量重建完全一致。
- 依赖：`METRIC-03`。

#### METRIC-05：指标查询与复合索引对齐

- 范围：基于生产 `EXPLAIN` 选择路由表、API Key 表和现有时延桶表的复合索引；查询先把模型和分组转成 `model_key/group_key`，不在宽名称上堆叠索引。
- 验收：过滤和聚合结果不变；MySQL 热查询没有明显全表扫描或大范围临时排序；三数据库建表和查询通过；不增加只断言私有索引名的单测。
- 依赖：`METRIC-04`。按指标轨道顺序在聚合行为稳定后用最终查询形态做 `EXPLAIN` 和索引选择。

#### METRIC-06：高基数聚合验收

- 范围：使用可重复本地数据覆盖 1,000 条路由、每路由多个 API Key、重试、跨分钟结果、迟到日志、30 天路由保留和 7 天 API Key 保留，记录扫描行数、生成行数、耗时和体积。
- 验收：路由表行数不随 API Key 数增长；API Key 高基数不拖慢调度查询；正常分钟无周期性重复写；定点修复结果与全量重建一致。
- 依赖：`METRIC-01`～`METRIC-05`，只做验收和报告，不混入业务重构。

### 7.6 查询索引和后续独立改造

#### INDEX-01：路由池复合索引

- 范围：只给路由状态增加 `(group_name, model_name, channel_id)`，不同时拆表。
- 验收：生产等量数据的 `EXPLAIN` 命中目标索引，路由池读取和锁定结果不变，三数据库迁移通过。
- 依赖：无，可优先完成。

#### INDEX-02：系统任务复合索引

- 范围：根据实际 SQL 验证并增加 `(type,status,id)`，只有 `EXPLAIN` 证明需要时才增加 `(type,id)`，不改任务状态机。
- 验收：任务抢占、列表和清理回归通过，并记录新增索引的写入成本。
- 依赖：无。

#### ROUTE-01：路由状态冷热拆分

- 范围：新增 `channel_smart_schedule_route_score_snapshots` 最新评分解释表并固定 gzip，热状态表删除 `last_schedule_score_details`；同一事务写入、清空和删除，需要展示时批量加载解释详情。
- 验收：调度写入失败回滚、渠道删除、清空和详情接口字段保持正确；解压上限和三数据库测试通过。
- 依赖：`INDEX-01`，并在快照、Redis 核心链路稳定后执行；不要与 `INDEX-01` 并行修改同一模型文件。

#### SAMPLE-01：采样 JSON 改为追加行

- 范围：新建 `channel_smart_schedule_model_samples` 样本表（路由、样本时间和来源索引），状态表只保留汇总字段；直接切换，不迁移 `SamplesJSON`，暂时保留现有锁语义。
- 验收：去重、1,500 条上限、窗口裁剪、三种样本来源和恢复状态正确，并发结果一致。
- 依赖：核心快照和 Redis 链路稳定后执行。

#### SAMPLE-02：采样锁并发优化

- 范围：在追加行正确后，将全局锁缩小为路由级锁或数据库行锁，不与存储改造放在同一任务。
- 验收：同路由并发串行、不同路由可并行，渠道删除/禁用竞态和 `go test -race` 通过。
- 依赖：`SAMPLE-01`。

#### PROBE-01：探测模型配置子表化

- 范围：新建 `channel_status_probe_config_models`（`config_id,channel_id,model_name`）子表并删除 `models_json`，先保持按渠道抢占、一次执行多个模型的语义不变。
- 验收：保存、去重、排序、配置修订、手动/定时 claim 和渠道删除正确。
- 依赖：达到模型配置扩展阈值后独立执行。

#### PROBE-02：探测统计桶拆表

- 范围：最新状态继续留在 state 表，分钟、小时和日桶改成 `channel_status_probe_metric_buckets` 固定粒度行，避免每次重写三个 JSON。
- 验收：桶边界、迟到结果、并发 upsert、展示和保留清理正确。
- 依赖：业务语义可独立于 `PROBE-01`，但为避免并行修改探测状态模型和清理代码，实际执行顺序放在 `PROBE-01` 之后。

#### DETECT-01：模型检测报告 gzip

- 范围：只将 `report_json` 改为固定 gzip BLOB，结构化摘要保持不变，`official_config_json` 暂不动，不引入对象存储。
- 验收：1MiB 未压缩限制、损坏数据、解压上限、SHA256 和详情接口往返通过。
- 依赖：`SET-03`，且报告体积或检测频率达到阈值后执行；避免与模型检测保留配置和清理逻辑并行改同一执行模型。

#### INDEX-03：低频表索引审计

- 范围：单独审计探测执行和检测成本事件重叠索引，只删除有查询和写入证据支持的冗余索引。
- 验收：删除前后 `EXPLAIN`、写入基准、列表和重试查询结果完整记录；没有证据则不改索引。
- 依赖：需要生产慢查询或索引使用数据。

### 7.7 停机上线和独立验收计划

#### CUT-01：冻结目标表和 Redis key 清单

- 范围：在清理脚本定稿前复核旧表、最终新表、保留共享表和带版本 Redis 前缀；由本计划统一修改 `model/main.go`，把已完成计划的新模型加入标准与快速 `AutoMigrate`，移除不再使用的旧模型注册，不建立 legacy migration。至少核对 `channel_monitor_minute_route_metrics`、`channel_monitor_minute_api_key_metrics`、`channel_monitor_dirty_minutes` 以及本次实际纳入上线范围的后续结构表。
- 验收：表名、字段、索引、清理责任和 Redis key 所有者无遗漏，SQLite 快速迁移和标准迁移包含本次上线的新模型且不再创建被废弃的旧结构；并行计划未在迁移注册区留下冲突改动。
- 依赖：准备上线的核心表设计已确定。

#### OPS-01：幂等 MySQL 停机清理 Runbook

- 范围：在 5.5 节脚本基础上增加执行前库名、应用已停止、目标行数检查，按最终表名删除 21 张旧表及定向任务和配置，增加执行后核验和中断重跑说明。
- 验收：临时 MySQL 灌入目标和共享数据后，重复运行脚本只有目标数据消失，`channels`、`abilities`、`logs` 和 `channel_test` 保持不变。
- 依赖：`CUT-01`。MySQL DDL 会隐式提交，Runbook 只能保证幂等重跑，不能宣称整体事务可回滚。

#### OPS-02：Redis 清理和命名 Runbook

- 范围：只清理带版本的渠道监控 Stream、消费组、租约和共享投影前缀，禁止 `FLUSHDB` 和生产 `KEYS *`。
- 验收：重复清理无错误，其他业务 Redis key 不受影响。
- 依赖：`REDIS-01` 冻结 key 名称。

#### VERIFY-01：快照容量和数据库矩阵

- 范围：使用脱敏或等结构样本覆盖 120、500、1,000、5,000 路由，记录 gzip 比例、耗时、`allocs/op`、内存峰值和 `max_allowed_packet`；验证 SQLite、MySQL、PostgreSQL 创建、写入、读取和清理。
- 验收：JSON 精确往返、损坏 gzip、解压上限、数量不一致和 3 天清理全部通过，报告不提交生产敏感数据。
- 依赖：`SNAP-01`、`SNAP-02`。

#### VERIFY-02：冷启动完整验收

- 范围：停机清理后检查目标表和索引、默认配置、Redis 消费组、一次完整调度快照、详情查询、分钟聚合和清理任务。
- 验收：检查脚本全部只读或只显式触发一次测试调度，失败项给出明确诊断；完整调度结果和接口验收标准满足第 8 节。
- 依赖：本次准备上线的所有核心计划和 `OPS-01`、`OPS-02` 完成。

## 8. 验收标准

完成治理后建议满足：

| 指标 | 目标 |
| --- | --- |
| 完整调度行为 | 健康度等现有输入变化仍 100% 触发原有完整调度流程 |
| 调度结果 | 优先级、权重、主路由和保护状态与优化前保持一致 |
| 实时事件完整性 | 高并发压测中无静默丢弃；积压、重试和失败均可观测 |
| 多实例一致性 | 不同网关实例读取同一份共享健康聚合结果 |
| 路由覆盖 | 活跃渠道模型组合超过 512 后仍完整参与健康统计，不因本地 LRU 淘汰缺失 |
| Redis 依赖 | 启用渠道监控时 Redis 必须可用，不产生进程内健康状态降级分支 |
| 执行明细内容 | 保留期内所有 adjustment 和字段完整可读，包括 `unchanged` |
| API 兼容性 | 详情过滤、排序、分页和响应字段无回归 |
| 历史数据策略 | 不读取、不迁移旧执行明细和旧分钟指标，切换前数据可直接清空 |
| 配置入口 | 所有应用侧保留期集中在“数据保留”Tab，并持久化到 `options` |
| 执行明细保留 | 默认 3 天，窗口内完整保留所有详情 |
| 执行明细表活数据 | 小于 2～3GiB |
| 压缩收益 | 生产样本验证 `compressed_bytes / raw_json_bytes <= 0.40`（即未压缩 JSON 至少减少 60%），最终值以基准测试为准 |
| 每任务快照行数 | 上线后每日新增约等于任务数，而不是 adjustment 数 |
| 分钟指标粒度 | 调度核心指标不包含 API Key 维度，API Key 分析能力保持可用 |
| 聚合正确性 | 延迟日志对应脏分钟可定点修复，结果与全量重建一致 |
| 聚合写放大 | 不再每小时无差别删除重建最近约 65 分钟数据 |
| 热查询计划 | 路由池、分钟指标和任务查询通过目标复合索引执行，无明显全表扫描 |
| 新结构数据库支持 | 新表可在 SQLite、MySQL、PostgreSQL 创建、写入和读取；不要求迁移旧数据 |
| 触发归因 | 新任务可记录明确触发来源或可识别的兜底来源 |
| binlog 本地保留 | 3～7 天，长期日志外部归档 |
| binlog 总量 | 稳态小于 5～10GiB，按实际恢复目标调整 |
| 清理任务 | 无长期积压，最老记录不超过配置保留期加一个任务周期 |
| 副本延迟 | 清理和表重建期间保持在运维阈值内 |

## 9. 复核 SQL

### 9.1 全库表空间

```sql
SELECT
    table_name,
    table_rows,
    ROUND(data_length / POW(1024, 3), 2) AS data_gb,
    ROUND(index_length / POW(1024, 3), 2) AS index_gb,
    ROUND(data_free / POW(1024, 3), 2) AS free_gb,
    ROUND((data_length + index_length) / POW(1024, 3), 2) AS total_gb
FROM information_schema.tables
WHERE table_schema = DATABASE()
ORDER BY data_length + index_length DESC;
```

### 9.2 上线后快照覆盖范围

```sql
SELECT
    FROM_UNIXTIME(MIN(created_at)) AS oldest,
    FROM_UNIXTIME(MAX(created_at)) AS newest,
    COUNT(*) AS snapshot_count,
    SUM(item_count) AS adjustment_count,
    ROUND(SUM(OCTET_LENGTH(payload_blob)) / POW(1024, 3), 3) AS compressed_gb,
    ROUND(AVG(OCTET_LENGTH(payload_blob)) / 1024, 1) AS avg_snapshot_kb
FROM channel_smart_schedule_execution_details;
```

### 9.3 上线后每日快照增长

```sql
SELECT
    DATE(FROM_UNIXTIME(created_at)) AS day,
    COUNT(*) AS task_count,
    SUM(item_count) AS adjustment_count,
    ROUND(AVG(item_count), 1) AS adjustments_per_task,
    ROUND(SUM(OCTET_LENGTH(payload_blob)) / POW(1024, 2), 1) AS compressed_mb,
    ROUND(AVG(OCTET_LENGTH(payload_blob)) / 1024, 1) AS avg_snapshot_kb
FROM channel_smart_schedule_execution_details
WHERE created_at >= UNIX_TIMESTAMP() - 15 * 86400
GROUP BY DATE(FROM_UNIXTIME(created_at))
ORDER BY day DESC;
```

### 9.4 最近任务变更占比

```sql
SELECT
    COUNT(*) AS tasks,
    ROUND(AVG(updated_at - created_at), 1) AS avg_duration_seconds,
    SUM(COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(result, '$.updated')) AS UNSIGNED), 0)) AS updated,
    SUM(COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(result, '$.unchanged')) AS UNSIGNED), 0)) AS unchanged,
    SUM(COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(result, '$.skipped')) AS UNSIGNED), 0)) AS skipped,
    SUM(COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(result, '$.failed')) AS UNSIGNED), 0)) AS failed
FROM system_tasks
WHERE type = 'channel_smart_schedule'
  AND created_at >= UNIX_TIMESTAMP() - 86400;
```

### 9.5 保留任务状态

```sql
SELECT
    id,
    status,
    FROM_UNIXTIME(created_at) AS created_time,
    FROM_UNIXTIME(updated_at) AS updated_time,
    LEFT(error, 300) AS error,
    LEFT(result, 1500) AS result
FROM system_tasks
WHERE type = 'channel_monitor_cost_retention'
ORDER BY id DESC
LIMIT 10;
```

### 9.6 binlog 配置和文件

```sql
SHOW BINARY LOGS;
SHOW VARIABLES LIKE 'binlog_expire_logs_seconds';
SHOW VARIABLES LIKE 'expire_logs_days';
SHOW VARIABLES LIKE 'binlog_row_image';
```

## 10. 最终判断

本次 44GB 不是 MySQL 异常泄漏，也不是分钟汇总表无限增长。它是当前产品行为和 MySQL 配置共同作用的可预测结果：

```text
按需求持续执行完整调度
× 每轮上百条完整 JSON 快照
× 14 天明细保留
× FULL 行镜像
× 30 天 binlog 保留
= 约 44GB 生产磁盘占用
```

短期通过缩短 binlog 和执行明细保留期可以快速把磁盘占用降到安全范围。正式上线采用停机更新，删除全部 21 张渠道监控表及相关任务和配置，不保留旧监控代码或历史数据；`channels`、`abilities`、`logs` 等核心共享数据保持不动。新版本直接使用每任务一个 gzip 快照、Redis Streams 统一健康事件链路，以及独立“数据保留”Tab。分钟指标拆表后从当前时间重新聚合。这样可以在不改变健康度变化触发完整调度需求的前提下，以较少代码控制数据表与 binlog 的持续增长。
