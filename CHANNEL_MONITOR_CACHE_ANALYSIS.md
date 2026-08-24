# 渠道监控缓存与异步并发评估

评估日期：2026-08-23；最终状态更新：2026-08-24

## 结论

当前实现已经完成本报告规划的缓存、异步和可靠账务代码改造，但还没有取得隔离环境中 100/500/1,000 用户并发、10/50 管理员并发、真实 Redis/DB 故障和告警投递的最终验收报告，因此仍不能直接承诺上千并发上线目标。

- **改造前**监控页面的普通查询默认不会自动刷新；性能、成功率等实时统计使用 Redis 投影，但总览、成本、模型检测等接口仍有同步 DB 查询。展示接口没有统一的短 TTL 响应缓存，手动刷新会并行触发多个接口。
- **改造前**智能调度的执行任务已经异步入队，Redis 消费和分钟聚合也在后台运行，这是正确方向。
- **改造前**用户请求关键路径仍同步发布监控 Redis Stream 事件；发布超时可达 2 秒，存在把监控故障传导到用户请求的风险。该问题已由 CM-01 的有界 writer 改造。
- **改造前**智能调度运行时缓存变脏时，选路请求会同步刷新多张数据库表；该问题已由 CM-02 的后台 dirty 刷新和无快照保护模式改造。
- **改造前**页面自动刷新策略与目标还有差距；该问题已由 CM-06 收敛为状态监测/模型检测每秒刷新，其余场景手动刷新。

因此，当前状态可概括为：**CM-01～CM-08 的代码和确定性回归已经完成；CM-09 已完成一次性上线 Runbook、阈值和机器可读契约，但告警规则安装和真实平台投递尚未验证；CM-10 已完成验收工具和安全门禁，但尚未在隔离环境执行最终负载、故障、对账和回滚矩阵。**

## 本轮实施状态（一次性上线）

本次按一次性上线执行，不做按实例、用户、页面或流量比例的灰度。已实施的链路在上线窗口统一启用；保留的开关仅用于紧急回滚或故障隔离，不作为灰度控制面。

- **CM-01～CM-03、CM-06：已实施**。包括有界监控事件 writer、版本化 Redis 权威路由快照、Redis 连接池隔离以及前端轮询/手动刷新收敛。
- **CM-04：已实施**。总览、成本、性能、成功率和智能调度已接入统一页面 snapshot store；query key 包含权限和规范化过滤条件，支持 1 秒 fresh、5 分钟 stale、本地最后完整副本、跨实例 fencing lease 和单调 CAS。
- **CM-05：已实施**。状态探测和模型检测 overview 均使用本地/Redis task snapshot、singleflight、跨实例构建租约、版本/水位 fencing 和最大 stale 截止；稳定 1 秒轮询不再同步组装 DB overview。
- **CM-07～CM-08：已实施并通过确定性故障回归**。普通请求成本使用独立成本 Stream 与数据库 outbox，日账本更新和 outbox 完成状态同事务提交；测试覆盖 Redis/DB/进程恢复、超时双写幂等、consumer 接管、死信失败保护、权限隔离和旧 writer fencing。
- **CM-09～CM-10：外部验收未完成**。一次性上线 Runbook、机器可读契约和验收工具已经提交；告警规则安装/真实投递、Redis >= 6.2、`XAUTOCLAIM`、AOF/等价持久化证据、100/500/1,000 用户并发、10/50 管理员并发、真实故障、跨实例接管、账务对账和回滚演练仍必须在隔离环境完成。
- 已知降级：监控观测 writer 队列满时允许丢弃可重建的观测事件并记录指标；没有完整智能调度快照时快速保护失败；成本、结算、退款仍沿用独立账务链路，未套用可丢弃监控队列。
- 本地验证已通过全仓 `go test ./...`、`go vet ./...`、前端 typecheck、68 个 channel-monitor 测试文件的 366 项测试、前端全量 122 个文件的 607 项测试、生产构建和 `git diff --check`。`-race` 未执行，因为当前 Windows 环境没有可用的 C 编译器；这不替代隔离环境中的 CM-10 验收。

## 目标架构原则

当前实现与剩余验收继续以以下三条原则为硬约束：

1. **请求相关数据**：用户请求只生成轻量监控事件，异步写入 Redis Stream，由后台 consumer 消费、聚合和落库；请求 goroutine 不等待 Redis 写入、聚合或监控数据库操作。
   这里的“监控事件”仅指可丢失的观测数据；成本、结算、退款等账务事件虽然也应异步化，但必须走独立的可靠缓冲/幂等链路，不能套用监控事件满载丢弃策略。
2. **智能调度数据**：选路只读取 Redis 中的路由/状态/策略快照（本地内存可以作为只读副本），dirty、配置变更和状态变化由后台任务重建并原子发布 Redis 快照；正常请求路径不得同步回源多张 DB 表。
3. **渠道监控页面数据**：页面接口优先读取 Redis projection；需要固定窗口或复杂组合时，由后台生成请求快照并写入 Redis，页面只读取快照。展示层可以秒级最终一致，不能把每次手动刷新变成 DB/Redis 全量扫描。

计费结算、并发限制和实际路由决策的正确性不依赖页面展示缓存；页面缓存只负责降低读取压力。

### Redis 与持久化前置条件

- 当前渠道监控 Stream 启动检查要求 Redis **>= 6.2**，因为 consumer 使用 `XAUTOCLAIM`；部署检查不能只验证能否 `PING`。
- Stream、projection、路由快照和任务快照应使用明确的 key 前缀、TTL、版本和水位；不要依赖 Redis 全局淘汰策略来决定业务数据是否还存在。
- 监控 projection 可以接受 Redis 重启后重建和少量观测缺口；成本 outbox、账务日账本和路由快照不能只依赖易失的 Redis 内存。成本可靠缓冲至少要有 AOF/持久化配置、可恢复 outbox 或等价的事务性 DB 记录，并做重启恢复演练。
- 用户请求池 `REDIS_POOL_SIZE` 默认是 10；监控写、监控读和 consumer 已分别使用独立 client，默认池大小为 4、8、4，并可单独配置。池大小不是队列容量，最终值仍必须通过压测分别设置并监控连接等待。

## 数据分类与边界

| 数据类别 | 权威来源 | 页面/请求读取方式 | 一致性要求 |
| --- | --- | --- | --- |
| 请求成功/失败、耗时、TPS、缓存命中等观测事件 | Redis Stream 消费后的 projection | 监控页面读 projection 或 dashboard snapshot | 允许秒级延迟；队列极端满载时允许少量观测缺口 |
| 智能调度路由、权重、暂停、健康和逻辑组关系 | 版本化 Redis 路由快照；DB 是重建输入 | 用户选路只读 Redis/最后一次完整本地副本 | 路由快照必须完整、带 revision 和水位；Redis 故障时使用最后一次完整快照 |
| 渠道名称、状态、倍率、分组、监控设置 | 脱敏后的配置快照；DB 是变更来源 | 页面读 Redis/内存配置快照 | 配置变更后主动失效并后台重建 |
| 成本、结算、退款和对账 | DB 日账本/任务成本表 | 页面读 Redis 成本展示快照；导出、审计和结算读 DB | 账务不能丢、不能依赖展示缓存 |
| 历史任务、执行详情、审计记录 | DB 或独立历史索引 | 手动查询/异步报表 | 不进入用户请求热路径 |

任何 Redis 页面快照都不能包含渠道密钥、访问令牌或未脱敏凭据；快照 key 必须绑定权限范围、租户/实例范围、查询窗口和数据版本。

文档中的“改造前”表示评估时基线，“当前已实施”表示本轮已落地能力，“目标/改造”表示仍待完成方案；三者不能混用作为已经完成的能力。

## 已确认决策与当前容量

### 请求事件队列

- 已完成 CM-01 的代码接入：`EmitChannelMonitorSuccessEvent`/`EmitChannelMonitorFailureEvent`、渠道测试、模型检测和任务成本修正统一进入有界 writer；请求路径只做校验、序列化和非阻塞入队。Redis Stream 本身仍没有固定的正常事件长度，当前按所有 consumer group 的安全水位执行 `XTRIM MINID`；consumer 停止或长期不 ACK 时，Stream 可能持续增长，不能把死信流的 `MAXLEN=10,000` 当作正常事件上限。
- writer 队列容量默认是 8,192，可由 `CHANNEL_MONITOR_EVENT_WRITER_QUEUE_CAPACITY` 配置；队列满时立即丢弃观测事件并记录指标，不再占用请求 goroutine 等待 Redis。旧的同步发布函数只保留为 writer 内部 XADD 原语和测试注入点。
- 现有 consumer `BatchSize=100`（`service/channel_monitor_redis_consumer.go:21`）只是一次消费数量，不是生产队列容量；死信流的 `MAXLEN=10,000` 只限制隔离消息，也不是正常事件队列容量。
- 现有智能调度 adaptive refresh 使用无界去重 `map`，与请求事件队列不同，也不能作为请求监控事件队列的容量依据。

容量仍应按压测校准，而不是写死为业务承诺。队列满的判定是：待发送事件数达到容量且 writer 在 Redis 超时、连接池耗尽或持续重试，生产速度超过发送速度。容量估算应按“峰值事件速率 × Redis 最坏不可用窗口 + 重试余量”校验，并分别压测单实例和多实例，不应只按平均 TPS 估算。

本次采用“直接异步写 Redis Stream”的实现方式：请求只将事件非阻塞交给后台 writer，writer 随即执行 Redis `XADD`，不增加监控数据库中间写入。这里的有界队列只是控制异步 writer 的并发和背压，不改变目标链路。

不建议在每个请求中直接 `go PublishChannelMonitorEvent(...)`：这种方式没有统一容量控制，Redis 变慢时会无限堆积 goroutine，并且进程崩溃会丢失尚未执行的事件。

队列满时优先保证用户请求不阻塞：当前立即记录 `queue_full`、丢弃数和告警，不同步等待 DB/Redis，也不默认增加 outbox；监控展示允许少量采样缺口，计费、结算和路由正确性不依赖这些事件。只有在后续明确要求监控事件零丢失时，才增加可恢复 outbox。

### Redis 不可用时的回源策略

逐请求回源 DB 的性能风险较大。当前智能调度 DB 路径不是一次简单查询，通常至少涉及 abilities、暂停状态、channels，智能调度场景还会读取 route states/样本等数据；模型和重试路径可能再次查询。并发放大后会快速占满 DB 连接池，造成用户请求排队。

因此采用以下最终策略：

- 智能调度优先使用最后一次完整 Redis/本地快照；
- Redis 不可用时继续使用最后一次完整快照，并返回快照年龄/降级指标；
- 快照恢复前不执行每个请求独立的 DB fallback，避免 DB 查询风暴；
- 如果实例没有任何可用快照，进入明确的保护模式并快速失败，待后台恢复快照后再恢复选路；
- 监控页面 Redis 快照不可用时也不做无保护的全量 DB fallback，只允许受控后台重建或管理员显式的低频审计查询。

“最后一次完整快照”必须包含 `revision`、生成时间、源数据水位和校验状态。Redis 故障时可继续使用它，但要暴露快照年龄并设置可配置的最大陈旧时间；超过上限后应停止选择已无法证明有效的渠道，进入保护模式，而不是无限期使用旧的启用/暂停状态。没有快照时允许在启动或后台恢复阶段执行一次受控的 DB 重建，但禁止由每个请求各自回源。保护模式可能降低业务可用性，必须有告警、恢复探测和清晰的错误码/重试提示。

### Redis 不可用时各类请求的当前影响

不能把“回溯 DB”理解为一个统一动作，当前各接口行为不同：

| 场景 | 当前 Redis 故障行为 | 性能/可用性影响 | 验收重点 |
| --- | --- | --- | --- |
| 智能调度实际选路 | 读取最后一次完整本地副本，不再由请求同步刷新 DB；没有可用快照时快速保护失败 | 避免 DB 查询风暴，但快照超过最大陈旧期会降低可用性 | 多实例故障和恢复期间 revision/水位不倒退、用户请求不回源 DB |
| 性能、成功率实时统计 | 优先读取统一页面 snapshot；Redis 不可用时只在最大 stale 期限内服务本地完整副本 | 超过 stale 期限后页面保护失败，不把每次刷新变成 DB 全量扫描 | 10/50 管理员并发下 DB/Redis 扇出有界、快照年龄可见 |
| 成本 | 页面读统一 snapshot；普通请求成本先尝试独立成本 Stream，Redis 未接受时写数据库 outbox | 账务可靠接受仍是同步边界：最坏会等待 250ms Redis 超时，再等待 750ms DB outbox 超时 | Redis/DB 故障时用户 P95/P99、可靠写失败计数和最终对账同时达标 |
| 状态监测 | 读取本地/Redis task snapshot，首次缺失由 singleflight、跨实例租约受控构建 | 1 秒轮询不再由每个请求重复组装多组 DB 查询 | 多实例轮询期间 DB 查询组不随管理员数线性增长 |
| 模型检测 | 读取本地/Redis task snapshot；历史详情仍按需查询 DB | 1 秒轮询与历史审计查询分离 | 跨实例快照接管、revision 单调和最大 stale 保护 |

监控观测事件已经不在用户请求中等待 Redis；成本事件由于不能丢，仍需在返回前获得 Redis Stream 或数据库 outbox 至少一处的可靠接受。这个最多 1 秒的故障边界不是监控同步回退，也不能在文档中隐藏；CM-10 必须分别测量用户请求延迟、页面 DB 查询量和账务正确性，不能用一个“Redis 故障是否报错”指标替代。

### 已确认的页面刷新策略

- 状态监测页面打开后即视为 active，按 1 秒读取状态快照；
- 模型检测页面打开后即视为 active，按 1 秒读取模型检测快照；
- 这两个页面即使当前没有正在执行的任务，也按打开状态刷新；
- 其他渠道监控页面和任务/历史详情全部关闭自动刷新，只响应手动刷新；
- 手动刷新已收敛为只刷新当前视图并防止重复触发；状态探测、模型检测 overview、实时 Redis 状态以及总览、成本、性能、成功率、智能调度五个页面均已接入有界 stale-while-revalidate 快照；
- 成本页面允许短暂延迟，DB 日账本仍是结算、导出和审计的最终来源；
- Redis 使用同一服务下的独立用户请求、监控写入、监控读取 client/连接池，具体池大小通过压测确定。

### 队列指标展示

渠道监控页面会显示队列状态，但必须拆分两段队列：

- `writer_queue_depth` / `writer_queue_capacity`：应用内等待异步 writer 写入 Redis Stream 的事件数及容量；
- `pending_count` / `queue_depth`：已经写入 Redis Stream、**已经投递给 consumer group 但尚未 ACK** 的消息数；当前页面已有该字段，其中 `queue_depth` 是 `pending_count` 的兼容别名。它不是整个 Stream 的长度，尚未投递给 consumer 的新消息不会计入 `XPending`；如需完整积压量，另增 `unread_count`/`stream_backlog_count`，不能与 `pending_count` 混为一谈；
- `consumer_lag_seconds`：Stream consumer 的最老未处理消息延迟；
- `writer_dropped_events`、`writer_retry_events`、`writer_queue_depth / writer_queue_capacity`：异步 writer 的丢弃、重试和容量使用率。

不能把不同阶段简单相加后称为一个“总队列”：监控 writer、监控 consumer pending、成本 Stream unread/pending 和成本数据库 outbox 分别用于判断不同堵点。当前接口已经提供 writer queue/capacity、监控 pending/lag、成本 Stream unread/pending 及成本 outbox 状态；监控事件主 Stream 尚未单独导出 unread 条数，仍需结合最早未处理时间和 consumer lag 判断完整积压。

### 当前页面顶部状态栏

当前顶部“渠道监控”标题旁的状态栏（`web/src/features/channel-monitor/components/channel-monitor-realtime-status.tsx`）展示的是**实时链路健康状态**，不是渠道数量或成本统计：

| 顶部显示 | 含义 | 当前来源 |
| --- | --- | --- |
| `Redis 正常/故障` | 当前实例能否访问监控 Redis | Redis Ping/监控状态 |
| `消费者 运行中/已停止` | Redis Stream consumer 是否仍有心跳 | consumer heartbeat |
| `实时数据已降级` | Redis、consumer、积压、Stream 裁剪或副作用标记存在异常 | `realtime_degraded` |
| `Redis 待处理 N` | 已投递给 consumer group 但尚未 ACK 的消息数，不包括尚未投递的新消息 | `pending_count`；`queue_depth` 是兼容别名 |
| `成本待写队列 N` | 成本 batcher 尚未写入成本表的内存聚合条目数 | `cost_queue_pending_count` |
| `成本 Stream 未读 N / 待确认 M` | 独立成本 Stream 尚未投递和已投递未 ACK 的消息数 | `cost_stream_unread_count` / `cost_stream_pending_count` |
| `成本 Outbox N` | 已可靠写入数据库 outbox、等待日账本事务应用的事件数 | `cost_outbox_pending_count` |
| `成本可靠写入失败` / `成本死信异常` | Redis 与 DB 可靠缓冲均失败，或成本事件无法解析进入死信 | `cost_publish_failed_count` / `cost_dead_letter_count` |
| `副作用标记释放故障` | 监控事件对应的运行时副作用标记释放失败 | `marker_release_failure_active` |
| `Stream 裁剪故障` | 已确认消息的 Stream 清理失败 | `stream_trim_failure_active` |

状态栏文字还会显示：数据截至时间、最早未处理消息时间、consumer 延迟、最后发布/处理时间、重试次数、接管次数、隔离消息数、标记释放失败次数、Stream 裁剪失败次数、成本 outbox 重试和成本可靠写入/死信计数。

顶部已经显示 `监控写入队列 N/{capacity}` 和 writer 队列年龄/丢弃/重试；默认 capacity 为 8,192，并展示运行时实际配置值。它表示“还没写入 Redis Stream”的事件，和 `Redis 待处理` 分开展示。

顶部状态栏的计数不是全局统一队列长度：`Redis 待处理` 是监控 Redis consumer group 的 `XPending`，`监控写入队列` 和 `成本待写队列` 是当前进程内存计数，成本 Stream/outbox 是共享可靠链路计数；前端合并多个响应时对积压计数取最大值、对最早时间取最小正值。监控主 Stream unread 和内存队列 capacity 之外的容量仍不能用 `queue_depth` 兼容别名冒充。

成本可靠链路元数据已并入五类页面的共享实时状态，不再依赖成本金额接口成功后才可见。`cost_queue_pending_count` 仍只表示当前节点的旧内存 batcher；成本 Stream 和数据库 outbox 使用独立字段。

### 当前容量与“队列满”定义

| 阶段 | 当前容量/计数 | 满载含义 | 备注 |
| --- | --- | --- | --- |
| 监控 writer 队列 | 已实现；默认 8,192，可配置 | 达到容量后非阻塞丢弃观测事件并告警 | 仍需通过 Redis 故障压测校准容量 |
| Redis Stream unread | 无固定上限 | consumer 未跟上生产，Stream 只会继续增长 | 当前页面不展示；应增加 `unread_count` 或 `stream_backlog_count` |
| Redis consumer pending | 无固定容量；`XPending` 计数 | 已投递消息未 ACK | 不是 Stream 总长度；consumer 停止时 unread 和 pending 都可能增加 |
| 死信 Stream | 近似 `MAXLEN=10,000` | 隔离消息达到裁剪水位 | 只保存异常消息，不能承接正常事件 |
| 成本 batcher | `pending` map 最多 4,096 个聚合键，另有最多 256 条 `retryBatch` | 新聚合键达到 `MaxPending` | 每个进程独立；已有聚合键仍可合并，状态探测成本不在此队列 |
| 成本 Stream | 独立 `channel_cost:v1:events`，正常处理后 ACK/DEL | unread 或 pending 增长 | 与可丢弃监控 Stream 隔离，Redis 写失败转数据库 outbox |
| 成本数据库 outbox | 每批 claim 256 条；已处理幂等记录保留 30 天后每批清理 1,000 条 | pending/重试持续增长 | 日账本更新和 `processed_at` 同事务提交，不能人工清空 |
| 成本死信 Stream | 近似 `MAXLEN=10,000` | 无法解析的成本事件 | 死信写入、源消息 ACK/DEL 由 Lua 原子执行；死信失败保留源 pending |
| 智能调度 dirty 集合 | group/model 去重 map，由后台 worker 消费 | 基数受已配置路由池数量影响 | 不是用户请求监控队列；CM-10 观察 dirty 持续时间和重建次数 |

### 成本队列为什么保持独立

成本待写队列不建议并入监控 Stream，原因不是技术上做不到，而是两条链路的可靠性要求不同：

| 链路 | 数据性质 | 满载/失败策略 | 消费结果 |
| --- | --- | --- | --- |
| 监控事件 | 观测和统计数据 | 可在极端过载时丢弃并告警，优先保护用户请求 | Redis projection、实时状态、趋势 |
| 成本事件 | 记账/对账数据 | 原则上不能丢，需要重试、幂等和可恢复积压 | 成本日账本、结算和审计数据 |

如果共用一条 Stream，成本数据库写入变慢会阻塞监控事件，监控事件过载丢弃策略也可能误伤成本数据，形成队头阻塞或记账风险。当前 CM-07 已按以下边界落地：

- 普通请求成本默认写独立的 `channel_cost:v1:events` Stream；Redis 未接受时写数据库 outbox，日账本应用以 `EventId` 幂等。
- outbox 与日账本在同一事务中完成，失败可续租重试；已处理记录保留 30 天并按每批 1,000 条受控清理。
- `CHANNEL_DAILY_COST_RELIABLE_OUTBOX=false` 只用于全量紧急回滚到旧 batcher，不用于灰度；旧 batcher 的状态仍单独显示为 `cost_queue_pending_count`。
- 任务成本注册、结算和探测成本等需要即时记账的路径继续保持同步/事务语义，不能因为监控异步化而变成可丢失的展示事件。

因此，**实现模式可以一致（事件 -> 队列 -> Stream -> consumer），但不能共用同一个队列，也不能共用同一个“待处理”计数。**

结论上，当前“成本待写队列”**不包含**在顶部的“Redis 待处理”中：前者是紧急回滚时旧 batcher 的进程内待落库聚合条目，后者是监控 Stream consumer group 的 `XPending`。默认可靠链路已经单独展示 `cost_stream_pending_count`、`cost_stream_unread_count`、`cost_outbox_pending_count`、`cost_publish_failed_count` 和 `cost_dead_letter_count`，不能并入监控 `pending_count`。

### 成本可靠链路当前实现

当前普通请求实现位于 `service/channel_daily_cost_outbox.go`，旧回滚路径位于 `service/channel_daily_cost_batcher.go`：

1. 请求生成唯一 `EventId`，先在 250ms 上限内写成本 Stream；Redis 未接受时在 750ms 上限内写数据库 outbox，二者均失败才增加 `cost_publish_failed_count`。
2. 成本 consumer 每批最多 256 条，先把 Stream 事件持久化到 outbox，再 ACK/DEL；解析失败通过 Lua 原子写死信并 ACK/DEL，死信写入失败时源消息保持 pending。
3. outbox 使用租约恢复，日账本更新和 `processed_at` 同事务提交；重复 Stream 投递、Redis 超时后的 DB 双写和进程重启均依赖 `EventId` 保证只应用一次。
4. outbox 状态通过数据库聚合查询，导出 pending、最老 pending 和当前 pending 行的重试 gauge；可靠写失败、死信和账本写失败另有进程级单调计数器，不能把可下降的 `cost_outbox_retry_count` 当作 counter；已处理记录保留 30 天。
5. 状态探测成本、任务结算和退款继续保留各自的同步/事务边界，不由普通请求成本 Stream 改写语义。
6. 旧 batcher 仅在可靠链路全量关闭时启用；其 4,096 个聚合键容量和同步回退风险仍是回滚路径的已知限制，不能作为正常生产容量。

当前剩余工作不是再设计两阶段迁移，而是在 CM-10 中验证 Redis/DB/进程故障下的请求延迟、可靠接受、恢复耗时和最终零差异对账。

## 具体改造方案

### 1. 请求事件改为异步写 Redis Stream

涉及：`service/channel_monitor_event_emit.go`、`service/channel_monitor_event_publisher.go`、`service/channel_monitor_redis_runtime.go`。

1. 将 `EmitChannelMonitorSuccessEvent`/`EmitChannelMonitorFailureEvent` 中的同步 `PublishChannelMonitorEvent` 改为 `EnqueueChannelMonitorEvent`，请求 goroutine 只完成事件校验、序列化和非阻塞入队。
2. 已新增有界队列和后台 writer worker：worker 在后台执行 `XADD`，失败按有限次数重试，再由现有 Redis consumer 消费；后续可根据压测增加批量 Pipeline，但不能把批量化当作请求路径保证。
3. 队列满时不能等待 2 秒，也不能静默处理：当前立即记录 `queue_full` 和丢弃数并丢弃该观测事件；队列深度、容量、最老事件时间/年龄、丢弃和重试均已导出。
4. writer 已有独立生命周期、退出 drain 和重试上限；进程崩溃时尚未写入 Stream 的可重建观测事件允许丢失并由丢弃/重启证据界定，不把账务事件放入此队列。
5. 保留现有 `PublishChannelMonitorEvent` 作为 worker 内部的同步 XADD 原语，不再从 relay 响应路径直接调用。

目标链路：

`用户请求 -> 构造事件 -> 有界内存队列 -> 异步 XADD Redis Stream -> consumer -> Redis projection/聚合/落库`

### 2. 智能调度改成 Redis 权威快照

涉及：`model/channel_cache.go`、`model/channel_smart_schedule_route_cache.go`、`controller/channel_ratio_monitor_schedule_runtime.go`。

1. 设计版本化 Redis key，例如 `channel_monitor:schedule:v1:snapshot:{group}:{model}`，内容包含路由渠道、优先级、权重、可用状态、暂停时间、逻辑组成员、配置 revision、生成时间和过期时间。
2. 配置变更、探测结果、限流/暂停变化只发布 dirty 事件；后台 scheduler 使用 single writer + Redis lease 合并同一 group/model 的刷新。
3. 后台从 DB 构建完整快照，写入临时版本 key，校验成功后通过版本指针原子切换；不能逐字段修改正在使用的快照。
4. `GetRandomSatisfiedChannel` 只读 Redis 快照或由 Redis 快照异步填充的本地只读副本；正常请求路径禁止 `RefreshLogicalChannelRuntimeCache`、`RefreshChannelSmartScheduleRoutePoolCache` 和 DB fallback。
5. Redis 暂时不可用时优先使用最后一次完整本地快照并记录过期年龄；没有任何快照时应快速失败或走明确的保护策略，不能让所有请求同时回源 DB。

目标链路：

`DB/事件变化 -> 后台构建 -> Redis 版本化路由快照 -> 本地只读副本/选路 -> 用户请求`

### 3. 渠道监控页面改成 Redis projection/snapshot

涉及：`controller/channel_ratio_monitor.go`、`controller/channel_monitor_cost.go`、`controller/channel_ratio_monitor_performance.go`、`controller/channel_monitor_today_success.go`、`controller/channel_model_detection_query.go`、`controller/channel_status_probe.go`。

1. 总览、成本、性能、成功率等接口新增统一 `ChannelMonitorSnapshotStore`，先按规范化 query key 读取 Redis；key 必须包含租户/权限范围、日期/窗口、渠道/分组/模型过滤、数据版本。
2. 固定窗口的页面数据由后台 snapshot worker 生成并写 Redis；参数化查询设置最大窗口、最大过滤数量和 TTL，防止任意参数制造无限 key。
3. 手动刷新只读取最新快照并返回 `generated_at`、`data_cutoff_at`、`event_watermark`、`stale`；快照不存在或过期时异步提交重建任务，不在 HTTP 请求中执行全量 DB 查询。
4. 成本金额和结算仍以 DB 日账本为最终来源；daily-cost batcher 成功落库后更新 Redis 成本快照，页面读取 Redis，导出/审计场景再显式读取 DB。
5. Redis projection 已有 `event_watermark`，快照应沿用该水位，避免页面把不同处理批次的数据拼在一起。

### 4. 运行中的状态监测和模型检测

涉及：`web/src/features/channel-monitor/lib/query-options.ts`、状态探测 worker、模型检测 worker 和对应 controller。

1. 将状态监测和模型检测页面打开时的刷新间隔改为 `1,000 ms`，但只允许这两个页面使用该 helper；是否存在 active run 不再决定页面是否轮询。
2. 智能调度执行对话框、任务历史、普通详情和其他页面移除该 helper，保持 `refetchInterval: false`，只响应手动刷新。
3. 状态探测 worker 每次状态变化写 Redis task/status snapshot；状态 controller 运行中优先读该快照，DB 只用于配置和历史查询。
4. 模型检测 worker 将 run 状态、当前 channel/model、进度、错误和更新时间写 Redis；模型检测 overview 的 1 秒轮询只读该快照，历史详情仍按需读 DB。
5. 前端 1 秒请求表示“请求频率”，快照中的 `updated_at` 才表示“数据新鲜度”；两者必须在 UI 和接口字段中区分。

### 5. Redis 客户端与连接池隔离

涉及：`common/redis.go` 及调用 Redis 的监控服务。

拆分至少三类 client：

- 用户请求相关 Redis：并发租约、限流、鉴权等；
- 监控写 Redis：事件 writer、projection 和 task snapshot；
- 监控读 Redis：页面查询、智能调度快照读取和 route-health。

每类 client 独立连接池、超时和指标。监控读请求不能耗尽用户请求连接池；连接池大小应通过压测而不是固定猜测决定。

### 6. 前端手动刷新收敛请求量

涉及：`web/src/features/channel-monitor/lib/query-options.ts` 和 `web/src/features/channel-monitor/index.tsx`。

1. `refetchChannelMonitorQueries` 按当前 tab/弹窗返回需要刷新的 query key，不再对所有 key 使用 `type: 'all'`。
2. 手动刷新按钮增加 500～1,000 ms 防抖和进行中锁，重复点击合并为一次刷新。
3. 普通查询保持 `staleTime: Infinity`、关闭窗口聚焦/重连刷新；状态监测和模型检测页面打开时即启用 1 秒轮询，不再要求必须存在 active run。
4. 页面显示快照生成时间、数据截止时间、consumer lag 和是否为旧快照，避免把异步延迟误认为请求失败。

### 7. 失败、重启和一致性策略

1. Redis 快照使用版本号/水位和原子发布；旧快照保留到新快照确认可读后再删除。
2. Redis 重启或淘汰后由后台重建 projection、路由快照和 task snapshot；重建期间页面返回旧快照/降级状态，不触发 DB 请求风暴。
3. 多实例只允许一个 writer 重建同一快照，其他实例读共享 Redis；本地副本必须带版本和过期时间。
4. 所有异步链路增加队列深度、丢弃数、重试数、consumer lag、快照年龄、回源次数和 Redis 命令延迟指标，并设置告警。

### 8. 一次性上线与回滚

1. **上线前准备**。完成 CM-00～CM-10 的代码、测试、故障演练、告警和容量检查；writer queue、Redis pending/unread、cost batcher、快照年龄、回源次数以及 DB/Redis 延迟指标必须已经可见。
2. **一次性切换**。在单一维护窗口内同时启用事件 writer、路由快照、页面 snapshot、状态/模型 task snapshot、成本可靠缓冲和前端刷新策略；不采用按实例、按用户或按页面的灰度切换。
3. **上线前置闸门**。成本 outbox 的重启恢复和幂等对账、路由快照的 revision/权限校验、快照缺失时的保护模式以及前端请求扇出检查，任一项未通过都不得上线。
4. **上线后观察**。一次性切换后连续观察完整业务高峰，重点检查用户请求 P95/P99、Redis 用户池等待、writer 丢弃/重试、consumer lag、快照年龄、DB 回源和成本对账；观察期不是灰度期，不改变流量分配。
5. **回滚原则**。发现用户请求延迟、账务缺失、快照版本错乱或 Redis 负载异常时，立即将受影响链路的开关恢复到上线前旧路径组合；保留事件/成本指标和后台重建能力，禁止用全局回源 DB 作为长期回滚方案。

成本链路原有一个不可同时满足的约束：在没有可靠 outbox 时，不能既保证“成本绝不丢失”又保证“队列满时请求绝不等待 DB”。CM-07 已增加可靠 Stream/outbox；当前剩余边界是可靠接受仍会在故障时等待最多 250ms + 750ms，必须在 CM-10 验证而不能宣称完全无等待。

## 可验证任务拆解与并行执行分析

本节把上面的完整目标拆成可以单独提交、单独验收和单独回滚的任务。每个任务只负责一个稳定的技术边界，完成条件必须由测试、指标或故障演练证明；“局部代码已经实现”不等于任务表中的全部验收条件已完成。当前状态以“本轮实施状态”一节为准。

本轮已经落地 CM-01～CM-08 的代码与确定性回归，并提交 CM-09 Runbook/机器契约和 CM-10 验收工具。剩余工作是把契约接入真实告警平台并在隔离环境执行 Redis/DB 故障、跨实例接管、成本对账、回滚和最终并发矩阵。

### 任务清单

| 编号 | 单一交付物 | 主要文件/模块 | 前置任务 | 可并行 | 验收证据 | 回滚点 |
| --- | --- | --- | --- | --- | --- | --- |
| CM-00 | 基线和配置契约 | 监控指标、紧急回滚开关、压测脚本/记录 | 无 | 否，必须先完成 | 记录用户请求 P95/P99、Redis/DB 命令延迟、连接池等待、页面刷新扇出、当前回源次数；配置来源与默认值可审计 | 使用记录中的上线前制品和配置整批回滚 |
| CM-01 | 有界监控事件 writer | service/channel_monitor_event_emit.go、publisher.go、redis_runtime.go | CM-00 | 与 CM-02、CM-03、CM-07 并行 | 队列满时入队立即返回；Redis 超时不会阻塞请求；writer 重试/丢弃/队列年龄有指标；事件 ID 重试不重复投影 | 当前无 writer flag；整批回滚制品或停止整套监控事件链路 |
| CM-02 | Redis 权威路由快照 | model/channel_cache.go、model/channel_smart_schedule_route_cache.go、调度刷新 worker | CM-00 | 与 CM-01、CM-03、CM-07 并行 | dirty 事件合并为一次刷新；快照版本和水位单调；选路命中快照时 DB 回源为 0；Redis 故障使用最后完整本地副本 | 当前无读取 flag；保留完整快照并整批回滚制品 |
| CM-03 | Redis client/连接池隔离 | common/redis.go、监控读写和 consumer 初始化 | CM-00 | 与 CM-01、CM-02、CM-07 并行 | 用户池、监控写池、监控读池分别报告连接使用率/等待/命令延迟；监控长查询无法耗尽用户池 | 关闭独立 client flag，回到现有 client；保留指标 |
| CM-04 | 页面 snapshot store 读契约 | 监控 controller、projection 查询服务、快照 key/版本规范 | CM-00、CM-03 | 与 CM-02、CM-07 并行；CM-05 依赖其字段契约 | 相同 query key 在 TTL 内命中同一快照；响应包含 generated_at、data_cutoff_at、event_watermark、stale；缺失快照只提交后台重建，不同步全量查 DB | 当前无逐接口 flag；整批回滚制品，不把 TTL 设 0 制造 DB 扇出 |
| CM-05 | 状态/模型检测 task snapshot | 状态探测 worker、模型检测 worker、对应 controller | CM-04 | 状态和模型两条 worker 可并行 | 页面每秒请求只读取 Redis task snapshot；无 active run 仍按页面打开状态刷新；overview 每秒请求不增加对应 DB 查询组 | 当前无 task snapshot flag；整批回滚前后端制品 |
| CM-06 | 前端轮询和手动刷新收敛 | web/src/features/channel-monitor/lib/query-options.ts、index.tsx 及相关组件 | CM-05 的接口字段契约 | 与 CM-08 并行 | 只有状态监测和模型检测页面产生 1 秒请求；其他页面自动刷新为 false；一次手动刷新只 refetch 当前 tab/弹窗；重复点击在 500～1,000 ms 内合并 | 整批回滚前端制品到旧轮询策略 |
| CM-07 | 成本可靠缓冲和幂等恢复 | channel_daily_cost batcher、cost outbox/Stream、恢复任务 | CM-00 | 与 CM-01、CM-02、CM-03、CM-04 并行 | DB 故障、Redis 重启、进程重启后成本事件可恢复；同一幂等键只结算一次；可靠写失败、Stream 和 outbox 指标可见；账本与展示快照可对账 | 全量设置 `CHANNEL_DAILY_COST_RELIABLE_OUTBOX=false` 回旧 batcher；不得删除 Stream、outbox 或账务记录 |
| CM-08 | 失败恢复和一致性演练 | Redis 重启/淘汰、writer 停止、consumer 接管、版本发布和权限边界测试 | CM-01、CM-02、CM-04、CM-07 | 与 CM-06 并行；最终切换前完成 | 旧快照在新快照可读前仍可用；重建只允许一个 writer；重复事件不重复副作用；无权限不能命中其他范围快照；故障期间无 DB 回源风暴 | 使用已有紧急开关或整批回滚制品，保留旧快照和恢复任务 |
| CM-09 | 一次性切换和告警 | 配置默认值、阈值、dashboard、runbook | CM-01～CM-08 | 否，必须串行 | 上线前完成全部闸门；上线时一次性启用；writer、lag、快照、成本可靠链路和连接池等待均能触发告警并定位实例 | 按 Runbook 使用已有紧急开关或整批回滚制品，不做全局 DB 回源 |
| CM-10 | 隔离环境并发/故障最终验收 | 压测、端到端页面测试、账务对账报告 | CM-09 | 否，最后执行 | 生产一次性上线前在 100/500/1,000 用户请求及 10～50 管理员刷新场景满足 P95/P99、DB 回源、Redis 池等待、成本正确性和路由正确性指标 | 取消生产上线窗口，恢复隔离环境并保留采样数据 |

### 每个任务的最小验收步骤

以下步骤是任务完成时必须留下的证据，避免只凭人工浏览判断完成。命令应在对应改动稳定后执行；若某任务尚未产生专用测试，先用下列模块级命令建立基线，再补充针对性测试。

#### CM-00：基线和开关

1. 用现有 Go 测试和前端检查建立基线：go test ./...；在 web/ 执行 bun run typecheck、bun run lint、bun run test。
2. 固定一组窗口、过滤条件和权限范围，记录一次手动刷新发出的请求数、每个接口的 DB/Redis 命令数及响应时间。
3. 导出机器契约中实际存在的配置和默认值，验证紧急回滚开关只按整批实例生效；不得为没有开关的链路伪造配置。

#### CM-01：有界监控事件 writer

1. 单元测试覆盖：正常入队、队列满立即丢弃、Redis 超时重试、停止时 flush、重复 EventId 去重，以及请求 context 取消后 writer 仍可独立完成或按策略丢弃。
2. 用可控 Redis 延迟或注入错误运行请求压测；验收请求 P95/P99 不随 XADD 超时线性增加，队列满时没有同步 DB/Redis 等待。
3. 对照 Stream 和 projection 的 EventId/去重指标，确认一次业务事件最多产生一次投影和一次运行时副作用；重试次数单独计数。

#### CM-02：Redis 权威路由快照

1. 对同一 group/model 连续发布多个 dirty 事件，验证后台只生成一个最新版本，快照指针切换原子且 revision/event watermark 不倒退。
2. 在选路请求中打开 DB 查询计数，分别验证命中新快照、命中过期但仍允许使用的本地副本、无快照保护模式三种结果；无快照不得触发每请求 DB 重建。
3. 模拟 Redis 重启或读超时，确认最后完整副本的年龄和降级状态可见；超过最大陈旧时间时返回明确保护错误，不使用无限期旧状态。

#### CM-03：Redis client/连接池隔离

1. 为用户请求池、监控写池、监控读池分别设置可观测名称，记录 pool size、连接等待、超时和命令延迟。
2. 用长窗口性能查询占满监控读池，同时发送用户请求；验收用户请求池无等待增长，或增长不超过基线阈值。
3. 逐个关闭监控读/写池，确认用户请求和核心并发租约仍能按预期工作；不要把池大小直接当作吞吐承诺。

#### CM-04：页面 snapshot store

1. 对总览、成本、性能、成功率、智能调度页面分别构造规范化 query key，验证权限范围、日期/窗口、过滤条件和版本均参与 key，敏感字段不会写入快照。
2. 第一次读取只提交重建任务并返回旧快照或 stale；第二次读取命中相同版本时不执行全量 DB 查询。记录命中率、快照年龄、回源次数和重建耗时。
3. 让 projection 的 event watermark 前进，确认新快照发布后旧版本仍可读，且不会把不同水位的数据拼接到一个响应。

#### CM-05：状态/模型检测 task snapshot

1. 状态 worker 和模型检测 worker 各自写入 run id、状态、进度、错误、更新时间和源水位；运行记录历史仍保留在 DB。
2. 页面打开但没有 active run 时持续观察 5 秒，确认每秒只产生快照读取，不产生重复 DB overview 查询。
3. worker 暂停或 Redis 暂时不可用时，接口返回最后快照和 stale/age；恢复后版本单调更新，不能回退到旧 run。

#### CM-06：前端轮询和手动刷新

1. 在浏览器网络面板或测试 mock 中分别打开状态、模型检测、总览、成本、性能、成功率、智能调度和任务历史页面，统计 5 秒内请求数；只有前两类应接近每秒一次。
2. 在每个 tab/弹窗连续点击刷新，确认 500～1,000 ms 内只发起一批当前视图所需 query，不调用全局 type: all refetch。
3. 验证页面显示 generated_at、data_cutoff_at、consumer lag、stale/降级状态，并且这些字段使用现有 i18n 约定，不把请求频率当成数据新鲜度。

#### CM-07：成本可靠缓冲和幂等恢复

1. 注入 DB 写入超时、Redis 重启和进程重启，确认成本事件进入可恢复 outbox/独立 Stream，恢复任务能够继续处理而不依赖请求重放。
2. 对同一请求/任务/账单幂等键重复投递，验证最终日账本、任务结算和退款只产生一次；对账差异必须为零或有明确可解释的重试状态。
3. 默认可靠链路下若 Redis 和 outbox 均未接受事件，必须增加 `cost_publish_failed_count` 并保留请求关联日志；再全量切换到旧 batcher 的回滚演练也必须完成，不能静默丢账。

#### CM-08：失败恢复和一致性

1. 依次模拟 writer 进程退出、consumer 停止、consumer 接管、Stream 裁剪失败、Redis 淘汰和快照构建失败，确认指标和页面状态能区分 writer queue、unread、pending、lag 和成本队列。
2. 并发触发同一快照重建，验证只有一个租约持有者写临时版本并发布指针；其他实例只读旧版本或等待，不重复扫 DB。
3. 用两个权限范围和两个租户构造相同 query 参数，验证 key 和响应严格隔离；任何快照内容不得出现密钥、令牌或未脱敏凭据。

#### CM-09：一次性切换和告警

1. 记录机器契约中实际存在的配置、默认值、负责人、启停命令和回滚条件；事件 writer、路由快照、页面 snapshot、状态/模型快照和前端刷新没有独立生产开关。
2. 为 writer queue/drop/retry、consumer lag、页面生成时间、状态/模型/路由 snapshot age/revision、成本 Stream unread/pending、成本 outbox pending/age/retry gauge、成本可靠写失败/死信/账本失败和连接池等待配置阈值告警；告警必须包含实例、链路和当前水位。实际 API/JSON path 以机器契约 `signal_sources` 为准。
3. 上线窗口前完成全部前置检查，随后一次性启用所有新链路；观察完整业务高峰，若指标不达标则按回滚预案恢复上线前旧路径，不进行流量分配或实例级灰度。

#### CM-10：并发/故障最终验收

1. 组合 100、500、1,000 个用户请求与 10～50 个管理员手动刷新，分别测 Redis 正常、Redis 高延迟、Redis 不可用、writer 队列满和 DB 暂时不可用。
2. 验收用户请求 P95/P99、错误率、Redis 用户池等待、监控池等待、DB 查询量、页面请求扇出、快照年龄和回源次数均达到目标；不能只看页面是否最终显示。
3. 导出成本并与 DB 日账本/任务结算对账，检查路由选择、并发限制和计费结果未受展示缓存影响；保存压测报告、日志检索语句和回滚演练记录。

### 并行关系和推荐实施顺序

依赖关系可以简化为：

CM-00 -> {CM-01, CM-02, CM-03, CM-07} -> CM-04 -> {CM-05, CM-06, CM-08} -> CM-09 -> CM-10

其中 CM-05 的状态快照 worker 和模型检测 worker 可以在同一任务内分两条子分支并行开发，但必须共用同一 snapshot 字段契约；CM-06 只有在接口返回生成时间、水位和 stale 字段稳定后才能合并。CM-08 可以和 CM-06 并行编写测试，最终仍要在 CM-09 前完成。

推荐的工作波次如下：

1. **波次 A（串行）**：CM-00。冻结基线、开关命名、指标名称和压测场景；没有基线就无法判断优化是否影响用户请求。
2. **波次 B（四路并行）**：CM-01、CM-02、CM-03、CM-07。它们分别负责事件写入、路由读取、连接池隔离和账务可靠性，接口边界不同；合并前只需约定指标命名和 feature flag 规则。
3. **波次 C（先契约后并行）**：先完成 CM-04 的 snapshot store/key/metadata 契约，再并行推进 CM-05 和 CM-08；所有页面迁移和前端切换在一次性上线窗口统一启用。
4. **波次 D（两路并行）**：CM-06 前端刷新策略与 CM-08 故障/权限/版本演练并行；前端只依赖已冻结的 API 字段，不应在此阶段修改后端账务语义。
5. **波次 E（一次性切换）**：CM-09。完成所有前置闸门后，在同一维护窗口启用全部新链路；feature flag 仅保留为紧急回滚和故障隔离手段。
6. **波次 F（隔离环境上线前验收）**：CM-10。在生产一次性上线前完成高并发、故障、重启、跨实例接管、对账和回滚验证；验收报告不完整时取消生产上线窗口。

不可并行或必须设置闸门的事项：

- 成本可靠 outbox 的重启、重复投递和幂等已通过确定性回归；隔离环境最终对账、故障恢复和请求延迟证据未完成前不能生产上线。
- 路由快照的 revision、水位、权限隔离和无 DB fallback 已通过确定性回归；真实 Redis/多实例演练未完成前不能生产上线。
- 状态/模型检测 task snapshot 与 1 秒轮询已经同步落地；隔离环境必须证明 DB 查询组不随管理员数线性增长。
- writer 队列满载和丢弃指标已经实现；告警规则安装、真实投递与 Redis 故障下 P95/P99 未验证前不能生产上线。
- CM-01～CM-08 不再分链路灰度；只有 CM-09 告警投递和 CM-10 隔离环境硬闸门全部关闭后，才允许一次性全量上线。

### 任务提交和回滚约定

每个任务的提交说明至少包含：任务编号、改动文件、开关名称和默认值、验收命令/指标、已知降级行为、回滚步骤以及是否触及 upstream-owned 文件。任务应保持小而可回退：一个提交只完成一个任务或一个任务的测试，禁止把格式化、依赖升级和无关重构混入改造。所有任务完成后再执行一次 git diff --stat 和 git diff --check，并在 CM-10 报告中链接每个任务的测试证据。

## 改造前基线与剩余目标

以下模块表和问题分析保留评估时的改造前基线，便于验收时对照；本轮已落地状态以文档顶部为准，不能把本节中的“改造前实现”当作当前代码行为。

| 模块/接口 | 改造前实现 | 判定 | 主要影响 |
| --- | --- | --- | --- |
| 渠道监控总览 `/channel_monitor/` | 渠道、倍率、成本和设置主要直接查数据库；部分统计读取 Redis shared projection；实时 metadata 会再次执行完整 projection 查询（`service/channel_monitor_redis_page_query.go:98`）。 | **弱缓存** | 一次刷新同时产生 DB 查询和 Redis 窗口扫描。 |
| 成本 `/cost` | 成本账本、API Key 成本、渠道元数据直接查 DB；Redis 只补充健康信息；没有统一响应缓存。 | **未缓存/弱缓存** | 多管理员频繁刷新会重复扫描账本和元数据。 |
| 性能 `/performance` | 使用 Redis shared projection；查询窗口最多创建 1,441 个分钟 Hash 的 `HGETALL`（`service/channel_monitor_redis_shared_projection.go:896,926`）；没有响应级 TTL。 | **有底层缓存，无响应缓存** | Redis CPU、网络和连接池压力随刷新次数线性增长。 |
| 成功率详情 `/success/detail` | 使用 Redis shared projection；没有响应缓存；详情查询后还会查询完整 metadata projection。 | **有底层缓存，无响应缓存** | 重复执行相同窗口和过滤条件。 |
| 今日/历史成功率 `/success/today` | 今日实时数据来自 Redis/分钟聚合；历史日期和多日趋势有 10 秒进程内缓存和 singleflight（`model/channel_monitor_today_success.go:14`），但接口仍会查询渠道元数据。 | **部分缓存** | 统计读取较好，但多实例之间不共享进程内缓存；今日详情不是完全依赖该 DB 缓存。 |
| 状态总览 `/status` | 进程内缓存，默认 TTL 3 秒、上限 30 秒，并使用 singleflight（`controller/channel_status_probe_overview_cache.go:17`）。 | **有缓存** | 缓存未命中时仍会执行多次 DB 查询；多实例各自回源。 |
| 智能调度路由 `/schedule` | 路由列表、经济快照和部分当前窗口评分查 DB；开启 metrics 时按路由读取 Redis route-health（`controller/channel_ratio_monitor_schedule_route_api.go:53`）；没有统一响应缓存。 | **弱缓存** | 手动刷新仍可能按路由循环读取 DB/Redis，缺少共享快照和查询合并。 |
| 智能调度实际选路 | 当前主要读取进程内 `channelSmartScheduleRouteCache`；dirty 时同步从 DB 重建，Redis 目前主要承载 route-health 和监控事件，并不是路由快照来源（`model/channel_cache.go:155`、`model/channel_smart_schedule_route_cache.go:71`）。 | **不符合目标** | 多实例之间缓存不统一，进程重启或 dirty 高峰会回源 DB。 |
| 并发 `/concurrency` | 配置本地缓存约 1 分钟；活动数走 Redis 或本地内存；每次仍查询 DB 枚举渠道 ID（`service/channel_concurrency.go:257`）。 | **部分缓存** | 配置读取可控，但渠道枚举仍是同步 DB 依赖。 |
| 分组监控、任务/历史、模型检测 | 分组总览、任务/历史和模型检测 overview 主要直接查询 DB，未发现统一统计响应缓存；模型检测 overview 一次包含渠道、配置、目标、运行记录和执行记录等多组查询。 | **未缓存** | 手动刷新或 1 秒轮询时 DB 压力较大；模型检测必须先有 Redis 运行快照再启用每秒刷新。 |

## 用户请求与后台链路

### 已满足异步要求的部分

- 智能调度手动执行通过系统任务队列入队（`controller/channel_ratio_monitor_schedule.go:327`），执行本身不占用 HTTP 请求等待时间。
- Redis Stream consumer 在后台 goroutine 运行（`service/channel_monitor_redis_runtime.go:82`）。
- 分钟聚合在后台 worker 运行（`service/channel_monitor_aggregation.go:49`）。
- 普通请求的每日成本默认进入独立成本 Stream，Redis 未接受时同步写数据库 outbox；旧 batcher 仅是 `CHANNEL_DAILY_COST_RELIABLE_OUTBOX=false` 的紧急回滚路径。
- Redis 开启时会强制启用内存渠道缓存（`main.go:78`）。

### 改造前不满足“用户请求不受监控影响”的部分

1. **监控事件同步发布**

   旧实现中的同步发布风险已由 CM-01 消除：relay、渠道测试、模型检测和任务成本修正现在只进入有界 writer；`PublishChannelMonitorEvent` 不再从请求/后台生产调用点直接调用。仍需通过 Redis 延迟、队列满和进程退出演练验证请求 P95/P99、丢弃告警和 writer 停机收尾。

2. **脏运行时缓存同步刷新**

   改造前选路命中 dirty 标记后会同步刷新多张表。本轮已改为后台去重刷新版本化 Redis 权威路由快照，并通过原子指针读取最后完整本地副本；revision/水位单调、跨实例 lease、稳定快照续建和无快照保护均有回归测试。

3. **共享 Redis 连接池**

   改造前用户请求事件、并发租约、监控查询和后台 consumer 共用 `common.RDB`。本轮 CM-03 已拆分用户、监控写、监控读和 consumer 连接池，并增加分角色指标；仍需 CM-08/CM-10 的故障与容量验收。

4. **同步发布调用点不止一处**

   改造前普通 relay、渠道测试、模型检测和任务成本修正等生产调用点会直接发布。本轮 CM-01 已统一接入有界 writer，同步 `XADD` 只保留为 writer 内部原语和测试注入点。

   writer 重试必须沿用 `EventId` 幂等语义：Redis 已写入但客户端超时后再次 `XADD` 可能产生重复 Stream entry，consumer 必须依靠现有 dedup marker 防止 projection、运行时副作用和调度触发重复执行；监控页面还应单独统计重复/重试，而不是把它们计为新的业务请求。

## 自动刷新与手动刷新

目标策略应明确为：

- 状态监测页面打开后（包括状态探测主视图及打开的详情）每 1 秒刷新；
- 模型检测页面打开后（包括打开的运行详情）每 1 秒刷新；
- 其他所有渠道监控场景，包括总览、成本、性能、成功率、智能调度路由、调度任务历史、分组监控和非运行中的详情，均关闭自动刷新，只响应管理员手动刷新。

改造前运行中间隔由共享常量设为 3 秒，且同一 helper 被状态探测、模型检测、智能调度执行对话框和任务历史等多个组件使用。本轮 CM-06 已限定为状态监测和模型检测页面每秒刷新，其他页面关闭自动刷新。

改造前手动刷新会对多个 query key 并行执行 `refetchQueries({ type: 'all' })`。本轮 CM-06 已改为只刷新当前视图并合并重复点击；后端五类页面已经接入统一 snapshot store。多管理员并发下的真实请求扇出和冷启动回源上限仍需由 CM-10 隔离环境报告验收。

## 核对后需要补充的风险

- **刷新频率不等于数据新鲜度**：状态探测前端每 1 秒请求，响应中的 `generated_at`、`snapshot_age_seconds` 和 `stale` 才表示快照新鲜度；CM-10 必须按这些字段验收，而不能只统计请求频率。
- **模型检测已使用共享 Redis task snapshot**：模型检测 overview 使用 1 秒本地快照、Redis 跨实例共享、singleflight、构建租约和 5 分钟本地 stale 兜底；首次 miss 只允许一个构建者组装 DB overview。CM-10 仍需在多实例环境验证轮询期间 DB 查询组不增长。

- **实时状态使用单实例短缓存，页面数据使用共享快照**：`GetChannelMonitorRedisRealtimeStatus` 的 750 ms 进程内缓存合并同一实例的 XINFO/XPending 探测；总览、成本、性能、成功率、智能调度和两类 task overview 使用 Redis lease/fencing 解决跨实例重复重建。最终连接池容量仍需 CM-10 校准。
- **singleflight 与 Redis lease 分层使用**：进程内 singleflight 合并本实例请求，Redis fencing lease 保证跨实例只允许一个快照 builder 发布，单调 CAS 防止旧 writer 覆盖新版本。
- **观测事件可靠性边界已明确**：有界内存队列在进程崩溃时可能丢失尚未写入 Stream 的可重建观测事件；队列深度、容量、丢弃、重试、最老事件年龄和 consumer lag 已导出。账务事件不使用该可丢弃队列。
- **Redis 快照失效策略已实现但仍需实机演练**：TTL、revision/event watermark、配置变更失效、旧完整副本、Redis 重启重建和权限隔离均已有确定性测试；真实 Redis 淘汰策略与多实例时序仍由 CM-10 验收。

## 已落地优化与剩余验收

### P0：用户请求关键路径隔离（已落地）

1. `PublishChannelMonitorEvent` 已收敛到有界异步 writer：请求线程只完成轻量事件构造和非阻塞入队，队列满时记录丢弃/降级指标，不做同步 Redis/DB fallback。
2. 监控写、监控读、后台 consumer 和用户请求已经使用独立 Redis client/连接池。
3. 普通请求成本已经迁移到独立可靠 Stream/outbox；可靠接受仍保留 250ms + 750ms 的有界故障等待，必须由 CM-10 验证用户 P95/P99。

### P1：智能调度 dirty cache（已落地）

1. dirty 标记只触发后台去重刷新，选路请求只读最后一次完整 Redis/本地快照。
2. 跨实例 lease、版本化临时 key、原子指针切换和本地完整副本防止并发读到半成品。
3. 正常请求路径不再同步 DB 回源；Redis 故障使用最后完整副本，超过最大陈旧期或无快照时快速保护失败。

### P1：监控读取端短缓存和查询合并（已落地）

1. 五类页面、状态探测和模型检测已经使用有界 stale-while-revalidate snapshot，按权限、窗口和过滤条件生成 key。
2. metadata 与页面 payload 共享生成时间、数据截止时间、revision 和事件水位，不由同一次刷新重复扫描完整窗口。
3. 实时链路健康状态使用 750ms 本地缓存与 singleflight；页面 snapshot 使用跨实例 Redis lease/fencing。
4. 成本账本仍以 DB 为最终来源，页面读成本展示 snapshot，写入和结算不依赖展示缓存。

### P2：降低前端刷新扇出（已落地）

1. 手动刷新只刷新当前 tab 和当前打开的对话框，不再对所有 query 执行 `type: 'all'` refetch。
2. 自动轮询只用于打开的状态监测和模型检测页面，间隔为 1,000ms；智能调度执行、任务历史及其他查询保持手动刷新。
3. 重复刷新已经合并，并显示快照生成时间、数据截止时间和 stale 状态。

## 建议的验收指标

在 100、500、1,000 并发用户请求和 10～50 个管理员同时手动刷新场景下，至少验证：

- 用户请求 P95/P99 延迟在 Redis 监控查询变慢、Redis 短暂不可用、监控队列满时不显著上升；
- 用户请求路径不再出现 `PublishChannelMonitorEvent` 的 Redis 网络等待；
- 智能调度选路不会因 dirty cache 触发每请求 DB 刷新，刷新次数与 dirty 事件数接近 1:1；
- 打开的状态监测和模型检测页面每秒刷新只读取 Redis task snapshot/投影，不因轮询产生每秒多组 DB 查询；稳定缓存命中时智能调度选路 DB 回源为 0；
- Redis 用户池与监控池分别报告连接使用率、等待数、命令延迟和超时；
- 手动刷新一次只产生当前视图所需的请求，重复窗口在 TTL 内命中缓存；
- 监控数据允许 1～3 秒延迟，但成本结算、路由选择和并发限制的正确性不受展示缓存影响。

## 当前最终判断

本轮已经完成有界监控 writer、版本化 Redis 权威路由快照、四类 Redis 连接池隔离、统一页面 snapshot store、状态/模型 task snapshot、成本可靠 Stream/outbox、故障与权限确定性回归以及前端刷新收敛。一次性上线方案已经确定，不再需要灰度产品决策。当前唯一剩余阶段是 CM-09/CM-10 的外部证据：告警规则安装和真实投递、Redis >= 6.2/`XAUTOCLAIM`/AOF 或等价持久化、100/500/1,000 用户并发与 10/50 管理员并发、真实 Redis/DB 故障、跨实例接管、账务对账和回滚演练；这些硬闸门通过前仍不得执行生产全量上线。
