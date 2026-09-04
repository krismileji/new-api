# 渠道监控整体架构

渠道监控是下游新增的 Root 管理功能。它把请求、探测、测试和模型检测的观测事件送入 Redis Stream，再投影成实时读模型和调度副作用；历史分钟聚合、日成本账本和调度状态仍落在数据库。本文只说明当前代码路径，不把实时投影描述成唯一账本。

## 功能概述

渠道监控由三层组成：

- **发布层**：业务请求、状态探测、分组探测、智能探测、连通性测试和模型检测在完成后构造 `ChannelMonitorEvent`，校验后写入 Redis Stream。
- **消费层**：主节点启动 Redis runtime，创建消费组并消费 Stream；聚合器租约保证同一时刻只有一个有效聚合器推进共享投影和路由健康投影。
- **读模型层**：页面实时接口读 Redis 投影；趋势、日成本和调度历史读数据库。两条路径并存，时间水位独立推进。

关闭 Redis 时进程不能启动渠道监控 runtime。渠道监控要求 Redis `>= 6.2`，并检查 `XAUTOCLAIM`。

## 使用场景

- 管理页面需要秒级成功率、首字、TPS、今日成本和失败分类。
- 智能调度需要按 `(渠道, 规范化模型)` 维护实时样本窗口。
- 历史趋势、清理和调度评分仍需要数据库分钟表与日成本表。

## 事件流

```text
业务请求 / 失败 / 探针 / 测试 / 模型检测
        │
        ├─ 日成本事实：ChannelDailyCost / ChannelDailyAPIKeyCost
        │     普通请求经成本 Stream + outbox；探针同步；任务事务登记
        └─ ChannelMonitorEvent
              Validate → 有界 writer 队列 → XADD channel_monitor:v1:events
                         │
              消费组 channel_monitor:v1:aggregators
                         ▼
              Redis consumer + aggregator lease + event_id 去重
                   ├─ shared dashboard/cost projection
                   ├─ route-health projection
                   └─ runtime protection / 调度副作用（独立 marker）

消费日志 ──> 每分钟数据库聚合表（历史查询、趋势、清理）
```

当前消费者明确不替换旧的本地历史队列。不能把实现理解成“只有一条统一持久化链路”。

## Redis Stream 契约

| 项 | 当前实现 |
| --- | --- |
| Stream | `channel_monitor:v1:events` |
| 消费组 | `channel_monitor:v1:aggregators` |
| 最低 Redis | `6.2`（需要 `XAUTOCLAIM`） |
| 单次发布超时 | `2s` |
| 默认消费批大小 | `100` |
| 读取阻塞 | `1s` |
| 聚合器租约 | TTL `15s`，心跳 `5s` |
| Pending 接管 | 空闲至少 `30s` 后 `XAUTOCLAIM` |
| 处理失败 | 保持 pending 并重试；超过投递上限进入有界 dead-letter Stream（最长 `10000` 条） |
| schema version | `1` |

消费者名称优先使用环境变量 `CHANNEL_MONITOR_CONSUMER_ID`，否则依次回退 `INSTANCE_ID`、`POD_NAME`、`HOSTNAME`，最后用本机主机名或 `node`。

## 发布可靠性

`PublishChannelMonitorEvent` 先校验事件，再写入 Stream。`ChannelMonitorEvent.Validate` 拒绝无效 schema、负 token/成本、NaN/Inf 指标、过长标识和无效扩展 JSON。可选标量使用指针，显式 `0` 与缺失可区分。

有界 writer：

- 默认队列容量 `8192`，默认 `1` 个 worker，最多重试 `3` 次。
- 队列满时默认允许 `250ms` 内直接发布；仍失败则把事件写入数据库 `ChannelMonitorEventOutbox`，后台认领后补发。
- Redis 不可用或超时会更新发布失败计数，并把实时可用性置为 `false`。调用方不能把失败发布当成实时已处理。

## 事件来源与调度资格

来源枚举：`business`、`status_probe`、`group_probe`、`smart_probe`、`manual_test`、`model_detection`。页面和投影不会把这些来源混成同一成功率：

- 共享投影单独维护业务请求、渠道/模型/分组/API Key 及失败分类。
- 探针和模型检测成本按来源分类。
- 智能调度只使用 `scheduling_eligible=true` 的事件。
- `runtime_protection_eligible` 只控制运行时保护副作用，不能事后用 source 字符串猜测。

## 实时投影

### 共享仪表盘/成本投影

`ChannelMonitorRedisSharedProjection` 把事件写入带 TTL 的 dashboard minute、cost day、route/group/API Key scope hashes。共享 minute/cost/event TTL 为 `48h`。查询返回 `window_start`、`window_end`、`data_cutoff_at`、`processed_at`、`event_watermark`。

同一成本事件 ID 保存当前成本状态；新版本先减去旧状态再加新状态。重复 `event_id` 由 marker 忽略。零金额的 settled 与 unresolved 通过 `cost_status` 区分。

查询有保护上限，超出时返回截断/降级，而不是伪造完整窗口。默认和硬上限见[配置参考](../configuration-reference.md)。

### 路由健康投影

`ChannelMonitorRedisRouteHealthProjection` 按渠道 + 规范化模型维护采样 Sorted Set，受 retention minutes 和 sample limit 双重限制。超出 sample limit 会淘汰最旧样本，并设置 `sample_limit_truncated` 与 `sample_limit_cutoff_at`。窗口截断或投影启动晚于请求窗口时，页面和调度必须显示覆盖不足，不能把剩余样本伪装成完整窗口。

## 成本批处理

普通请求成本默认进入独立成本 Stream，由成本 consumer 写入数据库 outbox。Redis 未接受时，在有界超时内直接写数据库 outbox：Stream 最多等 `250ms`，数据库 fallback 最多等 `750ms`，二者都失败才报告可靠写入失败。

`CHANNEL_DAILY_COST_RELIABLE_OUTBOX=false` 只作为整批紧急回滚，回到旧内存 batcher；该路径不是默认语义，且必须保留可靠链路已经写入的 Stream、outbox 和账务记录。

金额事实源始终是 `ChannelDailyCost` / `ChannelDailyAPIKeyCost`。Redis 投影不是账本。

## 启动与降级

主程序启动阶段调用 `StartChannelMonitorRedisRuntime`。runtime 负责重建 Stream/消费者，并以 `1s` 起步、最长 `30s` 退避自动恢复。

Redis 不可用时：

- 实时接口返回 `realtime_degraded=true` 和降级原因，metadata 置零。
- 这不等价于“没有请求”或“成本为 0”。
- 历史分钟表和日成本表仍可读。
- 智能调度不能把残缺实时窗口当成完整观测。

## 权限边界

`/api/channel_monitor/*` 使用 RootAuth，并禁止 HTTP 缓存。普通用户不能读取事件、投影、消费组或 outbox。

## 数据边界

- 逻辑归组不改变事件、成本和并发的物理渠道归属。
- 实时投影、分钟聚合、日成本、调度状态是四套独立水位。
- 详见[数据一致性](data-consistency.md)和[实时监控](realtime-monitoring.md)。

## 与上游的关系

本功能是下游新增模块。发布点插入现有中继、探测和测试完成路径，尽量使用独立文件和 Option 键，避免改写上游账本语义。关闭 Redis 时渠道监控不能提供实时能力；历史成本和日志仍按原数据库路径工作。
