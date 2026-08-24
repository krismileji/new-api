# 渠道监控一次性上线 Runbook

本 Runbook 是 CM-09 的上线契约。发布方式固定为一次性全量启用：不按实例、用户、租户、页面或流量比例灰度。环境变量开关仅用于故障隔离和紧急回滚，不用于逐步放量。

机器可读的配置、信号、阈值和闸门位于 one-shot-rollout-contract.json。发布负责人必须在同一发布记录中保存配置快照、闸门证据、告警投递结果和回滚演练结果。

## 当前结论

截至 2026-08-24，**禁止执行一次性上线**。CM-01～CM-08 的代码和确定性回归已经完成，以下外部硬闸门仍未完成：

- 告警规则目前只有阈值契约，尚未在实际告警平台安装并完成投递测试。
- 尚未在隔离环境执行 100/500/1,000 用户并发与 10/50 管理员并发矩阵、真实 Redis/DB 故障、跨实例接管、账务对账和回滚演练。
- 目标 Redis 的版本、`XAUTOCLAIM` 支持和 AOF/等价持久化配置尚未随发布记录提交证据。

这些不是可接受的观察项。任一项未关闭都必须取消上线窗口；不得用先上线再观察替代。

## 实际配置审计

当前生产代码已实现的配置如下。未列出的 feature flag 不存在，部署清单不得凭空设置并声称生效。

| 配置 | 默认值 | 作用 | 紧急处置 |
| --- | ---: | --- | --- |
| REDIS_CLIENT_POOL_ISOLATION | true | 用户、监控写、监控读、consumer 使用独立 client/pool | Redis 连接总数成为故障源时可设 false 并整批重启；这会恢复共享池竞争 |
| REDIS_POOL_SIZE | 10 | 用户请求 Redis 池 | 只依据压测调整 |
| REDIS_MONITOR_WRITE_POOL_SIZE | 4 | writer 和监控指标写池 | 调整后整批重启 |
| REDIS_MONITOR_READ_POOL_SIZE | 8 | 页面、route-health 和状态读取池 | 调整后整批重启 |
| REDIS_MONITOR_CONSUMER_POOL_SIZE | 4 | Stream consumer、租约、projection 写池 | 兼容旧名 REDIS_CONSUMER_POOL_SIZE；新名优先 |
| CHANNEL_MONITOR_EVENT_WRITER_QUEUE_CAPACITY | 8192 | 进程内有界观测事件队列 | 只能缓冲短暂抖动，不是吞吐承诺 |
| CHANNEL_STATUS_PROBE_OVERVIEW_CACHE_TTL_MS | 3000 | 状态 overview 本地新鲜期 | 0 会回到同步 DB 查询，禁止作为高峰回滚方式 |
| CHANNEL_STATUS_PROBE_OVERVIEW_STALE_TTL_MS | 30000 | 状态 overview stale-while-revalidate 期限 | 超期后没有可服务的旧快照 |
| CHANNEL_STATUS_PROBE_OVERVIEW_REDIS_TTL_SECONDS | 60 | 状态 overview Redis 快照 TTL | 0 会关闭 Redis 快照 |
| CHANNEL_MODEL_DETECTION_OVERVIEW_CACHE_TTL_MS | 1000 | 模型检测本地快照新鲜期 | 0 会关闭快照读契约，不可在 1 秒轮询时使用 |
| CHANNEL_MODEL_DETECTION_OVERVIEW_STALE_TTL_MS | 300000 | 模型检测旧快照可服务期限 | 超期后进入快照不可用保护 |
| CHANNEL_MODEL_DETECTION_OVERVIEW_REDIS_TTL_SECONDS | 600 | 模型检测 Redis 快照 TTL | 0 会关闭跨实例快照 |
| CHANNEL_DAILY_COST_RELIABLE_OUTBOX | true | 普通请求成本使用独立成本 Stream 和数据库 outbox | 仅在可靠链路故障时全量设 false 回到旧 batcher；不得删除 outbox 数据 |
| CHANNEL_SMART_SCHEDULE_ROUTE_SNAPSHOT_MAX_AGE_SECONDS | 300 | 智能调度最后完整路由快照的最大可服务年龄 | 超期进入保护失败；不得调大后把过期路由长期当作可用 |

事件 writer、路由快照、页面 snapshot、状态快照和前端 1 秒轮询目前没有独立的生产 feature flag。不得把不存在的开关写进回滚步骤。现有开关用于整批故障隔离和紧急回滚，不用于实例、用户或流量灰度。

## 启动与关闭顺序

启动顺序：

1. 初始化主 DB、日志 DB 和四类 Redis client，并逐池 PING；
2. 初始化内存渠道缓存，启动智能调度 dirty refresh worker；
3. 初始化 Stream/group，启动 consumer runtime；
4. 启动有界事件 writer；
5. 启动成本 Stream/outbox consumer 与恢复任务；
6. 启动系统任务、分钟聚合和 HTTP 服务。

关闭顺序：

1. HTTP server 停止接收新请求；
2. 智能调度 dirty refresh worker 停止；
3. writer drain 并停止；
4. 成本 Stream/outbox runtime 停止；
5. consumer runtime 停止；
6. 成本 batcher 与数据库 outbox flush；
7. 四类 Redis client 关闭；
8. DB 由主程序 defer 关闭。

启动失败时不得留下只运行 consumer 而没有 writer 的半启动实例。关闭时不得先关 Redis 再 drain writer 或 flush 成本。

## 上线前硬闸门

每项必须附可复现命令、时间、实例和原始输出：

1. go test ./...、web/bun run typecheck、前端测试和 git diff --check 全部通过；
2. `redis-cli INFO server` 显示版本不低于 6.2，`redis-cli COMMAND INFO XAUTOCLAIM` 返回命令定义；
3. `redis-cli CONFIG GET appendonly appendfsync save` 与 `redis-cli INFO persistence` 证明 AOF 已启用且最近写入正常；托管 Redis 禁止 CONFIG 时，必须提交控制台持久化配置、服务等级和重启恢复的等价原始证据；
4. CM-02 route snapshot 证明 revision/event watermark 单调、原子替换、Redis 故障使用最后完整副本、无快照快速保护失败；
5. CM-04/05 证明状态和模型页面每秒只读快照，其他页面手动刷新不触发无保护 DB 扇出；
6. CM-07 的确定性回归证明 DB/Redis/进程重启后成本 outbox 可恢复且重复投递不重复记账，并在隔离环境完成同等故障演练和最终对账；
7. CM-08 确定性测试证明 consumer 接管、Stream 裁剪失败、快照重建失败、两个权限范围隔离和版本不倒退；真实多实例接管仍由 CM-10 单独证明；
8. 100/500/1000 用户并发与 10/50 管理员刷新形成 6 个并发组合，每个必需故障场景均保存完整报告，用户请求 P95/P99 不因监控池占满显著上升；
9. 下列告警规则已经安装，并在测试环境逐条触发；通知必须包含 instance、chain、当前值、水位/版本和 runbook 链接；
10. 两个以上实例完成 consumer/快照 writer 强制终止与接管，证明 fencing、revision 和水位不倒退；
11. DB 日账本、任务结算、退款、成本 Stream/outbox 和展示快照完成逐事件对账，差异为零；
12. 同一制品、同一配置在全部实例完成一次性回滚和恢复演练，没有逐实例、逐用户或逐流量放量。

## 告警阈值

阈值是初始保护线，最终值必须由压测校准。监控系统可以把本契约映射为 Prometheus、日志告警或等价规则，但不得改变语义。

| 信号 | 触发条件 | 级别 | 处置 |
| --- | --- | --- | --- |
| writer drop/queue full | writer_dropped_events 60 秒增量 > 0 | 严重 | 检查 write pool/Redis；观测数据已丢，不做同步回退 |
| writer queue | 使用率 >= 80% 持续 2 分钟 | 警告 | 检查 XADD 延迟、write pool timeout 和 Redis |
| writer retry | 5 分钟增量 >= 10 | 警告 | 检查 XADD 超时和网络 |
| consumer lag | >= 30 秒持续 2 分钟 | 严重 | 检查 consumer pool、租约、pending/unread 和 handler |
| 页面 snapshot age | `now - data.generated_at >= 30` 秒持续 2 分钟 | 严重 | 检查 snapshot worker；不得让每个请求回源 DB |
| 状态/模型/路由 snapshot age | 对应 `snapshot_age_seconds >= 30` 持续 2 分钟 | 严重 | 按接口和实例定位快照 worker |
| event watermark、状态/模型/路由 snapshot revision | 任意倒退 | 严重 | 隔离对应读链路，保留旧完整快照 |
| 成本 Stream 积压 | unread 或 pending 持续增长 | 严重 | 检查成本 consumer、Redis 和数据库 outbox 持久化 |
| 成本 outbox 积压 | pending > 0 且最老事件持续变旧，或当前 pending 的 retry gauge >= 1 | 严重 | 检查主 DB、租约、重试和日账本事务 |
| 成本账本写失败 | `cost_ledger_failed_count` 60 秒增量 > 0 | 严重 | 检查日账本事务；该字段是进程级单调计数器 |
| 成本可靠写入失败 | 60 秒增量 > 0 | 严重 | Redis 与 DB 可靠缓冲均未接受事件；立即隔离链路并核对账务 |
| 成本死信 | 60 秒增量 > 0 | 严重 | 检查无法解析事件；死信写入失败时源消息必须保持 pending |
| 任一 Redis pool wait | timeouts 60 秒增量 > 0 | 警告 | 按 user/monitor_write/monitor_read/monitor_consumer 单独定位 |

redis_pool_stats 还提供 pool size、total/idle/stale connections、hits/misses、command count/error、累计和最大命令延迟。等待以 go-redis PoolStats.Timeouts 为准，四类池不得合并；consumer_lag_seconds 也不能与 writer queue 相加。

实时 API 已导出 writer、consumer、四类 Redis pool、成本 Stream/outbox、成本可靠写入失败/死信和账本失败计数。状态、模型和路由接口分别导出实际 snapshot revision/age，页面 age 由 `generated_at` 计算；准确 API 与 JSON path 以机器契约的 `signal_sources` 为准。统一 Prometheus 映射、告警规则安装和真实投递仍未完成，因此两个告警硬闸门都保持关闭；`cost_outbox_retry_count` 是当前 pending 行的可下降 gauge，不能冒充单调计数器。

普通请求成本的可靠接受是账务边界，不是可丢弃监控事件：请求会先等待最多 250ms 的成本 Stream 写入；Redis 未接受时再等待最多 750ms 的数据库 outbox 写入。两处都失败才返回未记录状态并增加 `cost_publish_failed_count`。CM-10 必须证明 Redis/DB 故障下这段最多 1 秒的有界等待不突破用户请求 P95/P99 阈值，并完成最终零差异对账；不能用“异步成本”表述掩盖这段等待。

## 一次性启用

只有所有硬闸门关闭后才执行：

1. 冻结部署变更并导出当前环境变量；
2. 在目标制品中保持 REDIS_CLIENT_POOL_ISOLATION=true，设置压测确定的四个 pool size 和 writer capacity；
3. 用同一部署操作替换全部实例；不得保留新旧读写语义混合运行；
4. 验证所有实例的版本、Stream group、consumer heartbeat、四类 pool 和快照 revision 一致；
5. 开放流量后连续观察一个完整业务高峰。观察期不是灰度期，不改变用户或实例流量分配。

## 逐链路紧急回滚

回滚只隔离异常链路，保持同一版本/配置在全部实例一致：

- Redis pool 隔离：连接总数本身造成 Redis 故障时，全量设置 REDIS_CLIENT_POOL_ISOLATION=false 并整批重启；确认用户池 timeout 未增加。恢复时先验证 Redis 连接上限，再全量设回 true。
- 事件 writer/consumer：当前没有同步旧路径开关。出现 drop 时保用户请求、接受观测缺口并修复 Redis；不得恢复每请求同步 XADD。若必须关闭事件链路，只能停止整套渠道监控写/消费并明确页面降级。
- 路由快照：当前没有独立读取开关；故障时继续使用有界最后完整副本，超过最大陈旧期进入保护错误。不得启用逐请求 DB fallback，需回滚时整批回滚制品。
- 页面/状态/模型快照：不得把 TTL 设为 0 后让每秒轮询同步扫 DB；必要时先全量回滚前端轮询或整批回滚制品。
- 成本 outbox/batcher：账务优先。可靠链路故障时可全量设置 `CHANNEL_DAILY_COST_RELIABLE_OUTBOX=false` 回到旧 batcher；不能删除成本 Stream、outbox 或已写账务记录，恢复后先 flush 再重新开启。
- 前端刷新：如 1 秒轮询造成压力，统一回滚前端制品到手动刷新/旧间隔；不按用户灰度。

任何回滚都保留队列、版本、成本和告警证据；禁止清空 Stream、pending、outbox 或快照来恢复。恢复前必须确认原故障指标回到阈值内，并重新跑对应闸门。

## 上线后记录

发布记录至少包含：制品 SHA、全量切换时间、实例清单、环境变量（凭据脱敏）、Redis 版本、四类 pool stats、writer/pending/unread/lag、快照版本和年龄、用户选路 DB 回源审计（目标为 0）、成本 Stream/outbox/可靠写失败与对账、P95/P99、告警事件、回滚决定和负责人。未出现告警也要记录为零值，不能留空。
