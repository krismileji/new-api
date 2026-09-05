# 渠道监控分析 T0 基线

日期：2026-09-05
基线代码：`9d40dec1de3fff382ba53670a9ec473c953cb981`
状态：T0 已完成

## 当前接口和数据源

| 接口 | 当天数据 | 历史数据 | 当前限制 |
| --- | --- | --- | --- |
| `GET /api/channel_monitor/cost` | `ChannelDailyCost`、`ChannelDailyAPIKeyCost` 日账本 | 同一组日账本表 | 页面请求按日期范围查询数据库；API Key 明细最多 1000 行；成本 controller 会在 Go 中组装渠道和 API Key 层级 |
| `GET /api/channel_monitor/success/today` | Redis shared projection：global、channel、API Key 和元数据 | `ChannelMonitorMinute*` 分钟表，并在请求中汇总 | API Key 结果最多 1000 行；用户归属需要批量查询 Token/User；历史日期仍走分钟表 |
| `GET /api/channel_monitor/success/detail` | 当天边界可读 Redis shared projection | `ChannelMonitorMinute*` 或日志观察数据 | 过滤和明细由 Redis/分钟表分别提供，尚无统一的渠道→用户→Key分页契约 |
| `GET /api/channel_monitor/performance` | Redis shared projection | 分钟性能表 | 当前 Redis 查询已经存在，成本接口没有使用成本日 Redis key 作为金额数据源 |

成本接口的 Redis 读模型目前只用于补充 Redis、Stream、队列和 consumer 健康元数据；`controller/channel_monitor_realtime_cost.go` 明确从数据库日账本读取金额和计数。因此刷新成本卡片仍会触发日账本查询。

## 当前事件字段基线

`model.ChannelMonitorEvent` 的当前事件版本为 `2`，仍兼容消费版本 `1` 的历史 pending payload，包含：

- `channel_id`、`user_id`、`user_attribution`、`api_key_id`、`api_key_name`、`model`、`group`、`occurred_at`、`event_sequence`。
- `source`、`outcome`、`cost_status`。
- 重试和最终结果标记：`is_retry_attempt`、`is_final_attempt`、`final_retry_summary`。
- 成功率和缓存字段：`prompt_tokens`、`input_tokens`、`cache_read_tokens`、`cache_write_tokens`。
- 成本字段：`settled_cost_nano_cny`、`unresolved_cost_nano_cny`。

新事件直接记录请求上下文中的 `user_id`，并将 `user_attribution` 标记为 `request`；无法归属时使用 `user_id=0` 和 `user_attribution=unknown`。版本 `1` 的旧事件缺少这些字段，消费时归一化为未知归属，不会因为版本升级永久重试或阻塞。

## Redis key、Stream 和 TTL 基线

当前版本化命名空间为 `channel_monitor:v1`：

| 类型 | Key/命名 | TTL/说明 |
| --- | --- | --- |
| 样本 Stream | `channel_monitor:v1:events` | 由 consumer group `channel_monitor:v1:aggregators` 消费 |
| 死信 Stream | `channel_monitor:v1:dead_letters` | 用于不可消费事件 |
| 分钟 dashboard hash | `channel_monitor:v1:projection:dashboard:minute:{minute_start}` | 48 小时 |
| 当日 success/cache hash | `channel_monitor:v1:projection:success:day:{day_start}` | T4 新增，48 小时；按 global/channel/user/API Key/model/route 聚合 |
| 成本日 hash | `channel_monitor:v1:projection:cost:day:{day_start}` | 已实现写入路径；页面成本金额当前未直接读取 |
| 成本事件状态 | `channel_monitor:v1:projection:cost:event:{event_id}` | 48 小时 |
| shared event 去重 | `channel_monitor:v1:projection:shared:event:{event_id}` | 48 小时 |
| 运行时/调度去重 | `channel_monitor:v1:projection:runtime:event:{event_id}`、`schedule:event:{event_id}` | 48 小时 |
| 健康观测 | `channel_monitor:v1:observability` | 当前实现按字段更新，没有页面级覆盖契约 |

Redis shared projection 当前支持 global、channel、model、group、API Key、route、API Key scope 和 failure 等维度，并设有查询规模上限：默认最多 1441 分钟、单 hash 500,000 fields、总 hash 5,000,000 fields、响应 256 MiB、维度 20,000。当前 key 设计没有独立的“用户”维度。

## 统计口径基线

| 指标 | 当前分子 | 当前分母/范围 | 来源 |
| --- | --- | --- | --- |
| 实际成功率 | `actual_success_count` | `actual_success_count + actual_failure_count`，包含重试尝试 | Redis shared projection 或分钟表 |
| 最终成功率 | `final_success_count` | `final_success_count + final_failure_count` | Redis shared projection 或分钟表 |
| 缓存命中率 | `cache_hit_count` | `cache_sample_count` | Redis shared projection 或分钟表 |
| 缓存利用率 | `cache_read_tokens` | `input_tokens` | Redis shared projection 或分钟表 |
| 成本 | `settled_cost_nano_cny` 转 CNY | 成本日账本中 settled/unresolved 计数 | `ChannelDailyCost`、`ChannelDailyAPIKeyCost` |

未解析成本、无缓存分母、Redis 不可用和超过返回上限时，当前响应通过健康字段或截断字段表达，不能把这些情况解释为零数据。后续 T1/T8/T9 将统一为 `coverage` 和 `monitoring_health` 契约。

## 最小回归 fixture

下面的 fixture 用于后续任务对照同一组维度和指标。时间使用北京时间日 `2026-09-05`，示例 ID 仅用于测试，不代表生产数据。

| event_id | channel_id | user/token | api_key | model | retry/final | outcome | cache | cost |
| --- | ---: | ---: | ---: | --- | --- | --- | --- | ---: |
| `fixture-001` | 101 | user 11 / token 201 | key 201 | `gpt-a` | first/final | success | hit, read 80 | 1000 nano CNY |
| `fixture-002` | 101 | user 11 / token 201 | key 201 | `gpt-a` | retry/non-final | failure | miss, read 0 | unresolved 0 |
| `fixture-003` | 101 | user 12 / token 202 | key 202 | `gpt-b` | first/final | success | miss, read 20 | 2000 nano CNY |
| `fixture-004` | 102 | user 12 / token 202 | key 202 | `gpt-a` | first/final | success | hit, read 50 | 3000 nano CNY |
| `fixture-005` | 102 | user 12 / token 202 | key 202 | `gpt-a` | retry/final | failure | miss, read 0 | unresolved 0 |

该 fixture 要同时验证三条现有读取路径：

1. Redis 当前投影读取当日 success/cache/performance 结果。
2. 分钟表读取历史 success/cache 结果并由请求侧汇总。
3. `ChannelDailyCost` 与 `ChannelDailyAPIKeyCost` 读取成本日账本和 API Key 成本。

后续任务必须保持以下不变量：渠道 101 的成本为 1000 + 2000，渠道 102 的成本为 3000；重试失败计入实际失败但不重复计入最终失败；API Key 202 可以同时出现在渠道 101 和 102；分页不能改变 scope summary。

## T0 验证记录

- 已只读核对路由、controller、service、model、Redis key 和 Stream consumer group。
- 已执行 `git diff --check`，通过。
- 当前工作树只有设计文档、执行方案和本基线文档未跟踪，未修改生产逻辑。
- 尝试执行 `go test ./model ./service ./controller`，但当前执行环境没有可用的 Go 可执行文件（PowerShell 报 `go is not recognized`），因此相关测试尚未运行；这不是代码失败，需在安装 Go 的环境补跑。

## 已确认的改造缺口

1. 成本当天金额仍读数据库日账本，尚未切换到独立 Redis 当日成本读模型。
2. 历史成功率仍扫描分钟表并在请求中汇总，尚未有数据库日汇总表。
3. Redis shared projection 没有稳定的 user 维度，事件也没有发生时 `user_id`。
4. 成功率和成本 API Key 明细都存在最多 1000 行的响应截断，不能支持真正的服务端分页。
5. 队列满时监控 writer 默认可能在请求 goroutine 直接 `XADD`，严格非阻塞仍需 T3 完成。
