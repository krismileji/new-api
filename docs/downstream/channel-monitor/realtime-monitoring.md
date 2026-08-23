# 统一事件流、实时读模型与时间一致性

本文说明渠道监控事件发布、Redis Stream 消费、实时投影、分钟历史聚合以及页面实时数据。Redis 实时轨道与数据库分钟聚合、成本批处理并存。

## 当前数据路径

~~~text
业务请求/失败/探针/测试/模型检测
        │
        ├─ 日成本事实：ChannelDailyCost（普通请求经内存 batcher；探针同步；任务事务登记）
        └─ ChannelMonitorEvent（校验后 XADD Redis Stream）
                         │
        channel_monitor:v1:events
                         │ XGROUP channel_monitor:v1:aggregators
                         ▼
        Redis consumer + aggregator lease + dedup/ack
             ├─ route-health projection（智能调度实时窗口）
             ├─ shared dashboard/cost projection（性能/成功率/今日成本）
             └─ runtime protection / optional schedule effect

消费日志 ──> 每分钟数据库聚合表（历史查询、趋势、清理）
~~~

Redis 投影是低延迟读模型，不是数据库成本账本；分钟表也不是 Redis Stream 的同步副本。两条历史/实时路径都保留，页面根据接口选择读取。

## 事件契约与来源

ChannelMonitorEvent 的 schema version 当前为 1。必填时间为 occurred_at（业务/探针完成或失败时间）和 created_at（事件创建时间）；消费成功后由投影写入处理元数据。事件使用 UUID event_id、单调分配的 event_sequence，并携带渠道、分组、规范化模型、请求 ID、节点、入站 API Key、重试、最终尝试、是否 dispatch、状态码、错误分类、首字、TPS、token、缓存和成本字段。

来源枚举：business、status_probe、group_probe、smart_probe、manual_test、model_detection。来源不会被页面混入：Redis 共享投影单独维护业务请求、渠道/模型/分组/API Key 及失败分类；探针和模型检测成本按来源分类，智能调度只使用带 SchedulingEligible 的事件。

可计入调度的资格由事件字段 scheduling_eligible 冻结：业务和智能探测默认可参与，手动/状态探测是否参与由上下文设置，不应由来源字符串事后猜测。runtime_protection_eligible 仅控制运行时保护副作用。

ChannelMonitorEvent.Validate 拒绝无效 schema、负 token/成本、NaN/Inf 指标、过长标识和无效扩展 JSON；可选标量使用指针，显式 0 与缺失可区分。序列化/反序列化通过 common.Marshal/common.Unmarshal。

## 发布、消费和幂等

PublishChannelMonitorEvent 先校验事件，再以 XADD channel_monitor:v1:events 写入 event_id 与 JSON payload，单次发布超时 2s。Redis 不可用或超时会更新发布失败计数、最后失败时间并把实时可用性置为 false；调用方不能把失败发布当作实时已处理。

启动时必须连接 Redis >=6.2，检查 XAUTOCLAIM，幂等创建消费组 channel_monitor:v1:aggregators。主程序在启动阶段调用 StartChannelMonitorRedisRuntime；runtime 负责重建 Stream/消费者并以 1s 起步、最长 30s 退避自动恢复。

消费者：

- 每批默认最多 100 条，读取新消息阻塞 1s。
- 使用聚合器租约，TTL 15s，心跳 5s；消费者心跳 key 同样用于健康状态。
- 处理前领取 pending，使用 XAUTOCLAIM 接管空闲至少 30s 的消息；处理失败保持 pending 并重试，超过投递上限的消息进入有界 dead-letter Stream。
- 共享投影使用 event ID marker/dedup；Redis 事务脚本把 marker、XACK 和必要删除绑定，副作用（运行时保护/调度）另有 marker、续租和丢失检测。

当前消费者注释明确：它“不安装投影或替换 legacy local queue”。因此不能把当前实现描述为删除旧队列或只有一条统一持久化链路。

## 实时投影与版本替换

### Shared dashboard/cost projection

ChannelMonitorRedisSharedProjection 把事件写入带 TTL 的 dashboard minute、cost day、route/group/API Key scope hashes，并返回：window_start、window_end、data_cutoff_at、processed_at、event_watermark。指标包括实际/最终成功数、首字样本、TPS、缓存读写 token、已结算/未解析成本和失败分类。

同一成本事件 ID（普通事件从 OtherJson.cost_event_id 获取，任务修正也沿用）保存当前成本状态；新版本先减去旧状态，再加上新状态，旧事件不会覆盖更新版本，重复 event ID 由 marker 忽略。零金额的 settled 与 unresolved 通过 cost_status 区分，不能把 0 误判为没有成本事件。

### Route-health projection

ChannelMonitorRedisRouteHealthProjection 按渠道+规范化模型维护采样 Sorted Set，受 retention minutes 和 sample limit 双重限制；超出 sample limit 会淘汰最旧样本并设置 sample_limit_truncated、sample_limit_cutoff_at。快照带 coverage_start、projection_started_at、data_cutoff_at、processed_at、event_watermark，智能调度据此判断窗口是否完整。

投影窗口截断或投影启动晚于请求窗口时，页面/调度必须显示覆盖不足或降级，不能把剩余样本伪装为完整窗口。

## 页面 API 与时间字段

实时性能 GET /api/channel_monitor/performance 和成功率明细 GET /api/channel_monitor/success/detail 直接调用 Redis shared projection；今日成功卡片同样通过 QueryChannelMonitorRealtimePageFromRedis 读取。响应同时返回：

| 字段 | 来源与含义 |
| --- | --- |
| generated_at | 当前 API 响应时间 |
| data_cutoff_at | 投影纳入的最大事件发生时间/分钟元数据 |
| processed_at | 投影完成处理时间 |
| event_watermark | 已应用的事件消费水位 |
| pending_count / queue_depth | Redis 消费组 pending 数；queue_depth 是兼容别名 |
| oldest_pending_at、consumer_lag_seconds | 最老未处理消息时间及相对当前时间的积压秒数 |
| last_published_at、last_processed_at | 发布器最后成功发布与消费者最后处理时间 |
| redis_status、redis_available、redis_consumer_running | Redis/消费者健康状态 |
| degraded_reasons、realtime_degraded | Redis 不可用、消费者停止/组缺失、积压、发布失败、marker 释放失败或 Stream trim 失败等原因 |

实时窗口缺少 Redis 或 projection query 失败时，metadata 置零并返回降级状态；这不等价于“没有请求”或“成本为 0”。性能接口在智能调度启用且存在策略时采用设置中的智能窗口；否则读取手动 1..1440 分钟（默认 15）。

## 分钟历史聚合和一致性边界

主节点的 StartChannelMonitorAggregationWorker 在自然分钟结束 1s 后处理消费日志。它用 ChannelMonitorAggregationState.completed_through 推进连续水位，启动/中断时从持久化水位追赶；迟到日志标记 ChannelMonitorDirtyMinute，worker 以 2min lease 小批领取并替换对应分钟，成功完成后删除标记，失败释放 lease 保留重试。

分钟表（路由、API Key、首字分桶）服务历史趋势和长期查询，不会因为 Redis realtime projection 暂时落后而自动补齐 Redis；反过来也不会因为 Redis 事件已处理就移动数据库分钟水位。清理任务按路由/延迟/API Key 独立保留期执行，并保护智能调度所需的窗口。

因此存在可观测而非强一致的时间差：事件已经 XADD 但尚未消费时，pending_count/consumer_lag_seconds 增加；事件已消费但分钟 worker 未到边界时，实时页面先更新、历史分钟查询稍后更新；Redis 故障时历史表仍可读，但实时接口降级。

## 数据可见性边界

`realtime_degraded=true` 表示实时投影不可用或延迟，不表示没有请求或成本为零。历史金额读取 `ChannelDailyCost`/`ChannelDailyAPIKeyCost`；实时投影不作为账本。Redis 实时轨道与数据库分钟聚合、成本批处理并存，不能互相替代。
