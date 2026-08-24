# 渠道监控 CM-10 并发与故障验收

`cmd/channel-monitor-acceptance` 用于在隔离的测试或预发布环境复现以下组合负载，并生成机器可读 JSON 报告：

本验收是生产一次性全量上线前的硬闸门，不是生产灰度或上线后补测。所有场景报告、告警投递、Redis 版本/持久化证据、跨实例接管、账务对账和回滚演练未齐全时，必须取消生产上线窗口。

- 用户请求并发：100、500、1,000；
- 管理员刷新并发：10、50；
- 用户请求和管理员刷新同时运行；
- 正常、Redis 重启、Redis 高延迟、Redis 不可用、writer 队列满、数据库暂时不可用等独立场景。

工具统计用户请求和管理员请求各自的 P50/P95/P99、最大延迟、错误率、吞吐、HTTP 状态码和端点请求数。每组场景前后还会读取渠道监控实时元数据，输出 writer 队列、丢弃事件、消费 lag、水位及 Redis 各角色连接池指标的变化。可选读取 Prometheus 文本端点并对指定指标计算增量。

## 安全边界

工具默认只执行 dry-run，不发送网络请求。实际执行必须同时满足：

1. 提供 `--execute`；
2. 提供 `--confirm=CM10_LOAD_TEST`；
3. `--environment` 为 `test` 或 `staging`；
4. 显式提供 `--base-url`；
5. 用户请求正文来自显式文件；
6. 用户和管理员令牌只从环境变量读取；
7. 公网域名额外要求 `--allow-public-test-host`。

工具不会修改 Redis 配置、停止进程、写数据库管理接口或自动注入故障。非 `normal` 场景只记录场景标签和故障证据文件的 SHA-256，故障必须由测试环境的基础设施工具在外部注入和恢复。用户 relay 请求仍可能调用上游并产生测试账单，因此必须使用专用测试渠道、测试模型和有额度上限的测试账户。

`--scenario` 只接受 `normal`、`redis-high-latency`、`redis-restart`、`redis-unavailable`、`writer-queue-full`、`database-unavailable` 和 `recovered`。执行任何非 `normal` 场景时还必须提供 `--fault-evidence-file`；工具只把原始证据的 SHA-256 写入报告，不复制可能含基础设施信息的内容。仅有标签或哈希仍不能证明故障真实生效，发布审核必须同时查看原始证据。

报告不包含令牌、Cookie 或请求正文。若指定 `--report-file`，文件权限设置为仅当前用户可读写。

## Dry-run

从仓库根目录运行：

```powershell
go run ./cmd/channel-monitor-acceptance `
  --admin-view=status-probe `
  --duration=30s
```

默认输出 100/500/1,000 用户并发和 10/50 管理员并发的 6 组计划。状态监测和模型检测视图的预期扇出均为每个管理员每次刷新 1 个请求。

报告中的 `config.required_matrix_shape` 只有在并发集合恰好覆盖上述 3 x 2 矩阵时才为 true。小规模本机报告只能验证工具行为，不能作为 CM-10 负载闸门证据。

可用 `--user-concurrency=2 --admin-users=1 --duration=2s` 生成本机小规模计划。

## 执行示例

准备只面向测试渠道的请求 JSON，并在当前终端设置密钥：

```powershell
$env:CM10_USER_TOKEN = '<测试用户 API Key>'
$env:CM10_ADMIN_TOKEN = '<测试环境 root PAT 或 Dashboard Access Token>'
```

本地测试环境的小规模验证：

```powershell
go run ./cmd/channel-monitor-acceptance `
  --execute `
  --confirm=CM10_LOAD_TEST `
  --environment=test `
  --base-url=http://127.0.0.1:3000 `
  --user-body-file=tmp/cm10-chat-request.json `
  --user-concurrency=2 `
  --admin-users=1 `
  --admin-view=channels `
  --duration=5s `
  --report-file=tmp/cm10-local.json
```

完整一次性上线验收矩阵：

```powershell
go run ./cmd/channel-monitor-acceptance `
  --execute `
  --confirm=CM10_LOAD_TEST `
  --environment=staging `
  --base-url=https://staging.example.test `
  --user-body-file=tmp/cm10-chat-request.json `
  --user-concurrency=100,500,1000 `
  --admin-users=10,50 `
  --admin-view=channels `
  --duration=60s `
  --max-user-p95=2s `
  --max-user-p99=5s `
  --max-user-error-rate=1 `
  --max-admin-p95=1s `
  --max-admin-p99=2s `
  --max-admin-error-rate=1 `
  --max-fanout-mismatch=0 `
  --max-writer-dropped-delta=0 `
  --report-file=tmp/cm10-normal.json
```

公网预发布域名需要额外添加 `--allow-public-test-host`。阈值检查失败时进程退出码为 1；未配置的延迟或错误率阈值会在报告中标记为 `skipped`。

## 页面请求扇出

`--admin-view` 必须选择一个当前视图。工具按 CM-06 的可见视图请求集合发起并统计请求，不会触发隐藏或未打开的查询。

| 视图 | 每次刷新请求数 | 只读请求 |
| --- | ---: | --- |
| `channels` | 6 | 总览、并发、性能、成本、今日成功率、智能调度摘要 |
| `groups` | 4 | 总览、性能、成本、今日成功率 |
| `models` | 4 | 总览、性能、成本、今日成功率 |
| `status-probe` | 1 | 状态快照 |
| `model-detection` | 1 | 模型检测快照 |
| `smart-schedule` | 5 | 总览、智能调度摘要与详情、成本、今日成功率 |
| `task-history` | 1 | 倍率任务历史 |
| `smart-schedule-history` | 1 | 智能调度执行历史 |

报告中的 `admin_refreshes * expected_requests_per_refresh` 必须等于 `admin_requests.requests`。差值写入 `fanout_mismatch`，默认要求为 0。端点实际计数位于 `admin_requests.requests_by_endpoint`。

工具的管理员 worker 会按 `--admin-refresh-interval` 循环发起所选视图，形成可重复的合成并发负载。验证状态监测和模型检测每秒轮询时，分别使用 `--admin-view=status-probe` 或 `--admin-view=model-detection --admin-refresh-interval=1s`；其他视图的循环只是压测中的合成刷新，不代表前端会自动轮询。需要模拟一次手动刷新时，将 `--duration` 与刷新间隔设为相等（例如均为 `1s`）。

## 故障场景

每种故障单独执行，避免多种故障叠加后无法归因：

1. 在测试环境确认基线健康并执行 `--scenario=normal`；
2. 使用外部基础设施工具注入一个故障；
3. 确认故障已经生效，再用相同负载参数执行，例如 `--scenario=redis-unavailable --fault-evidence-file=tmp/redis-unavailable-injection.txt`；
4. 保存报告后恢复故障；
5. 再运行一次 `--scenario=recovered`，确认队列、错误率和连接池等待恢复。

建议至少保留这些场景报告：

- `normal`；
- `redis-high-latency`；
- `redis-restart`；
- `redis-unavailable`；
- `writer-queue-full`；
- `database-unavailable`；
- `recovered`。

工具不会把“故障标签”当作故障已生效的证据。应对照 `config.fault_evidence_sha256`、`monitor_before`、`monitor_after`、`monitor_numeric_deltas`、服务端日志和基础设施监控确认实际故障状态。监控快照采集失败会让 `metric_capture` 检查失败；配置了 writer drop 上限却取不到 `writer_dropped_events` 时也会失败，不再静默标记为 skipped。

每个必需场景都要使用默认 100/500/1,000 x 10/50 矩阵，形成 6 个同时包含用户请求和管理员刷新的组合。至少保存 7 份 `channels` 视图报告（normal、5 个独立故障、recovered），并额外保存 `status-probe` 和 `model-detection` 各一份 normal 全矩阵报告，用来证明 1 秒轮询不会把 DB 查询组按管理员数放大。任一组合缺失、任一非 skipped 检查失败或必需阈值仍为 skipped，负载闸门均为 missing。

## Redis 兼容性和持久化证据

在负载开始前保存以下原始输出，不能只记录 `PING` 成功：

```powershell
redis-cli INFO server
redis-cli COMMAND INFO XAUTOCLAIM
redis-cli CONFIG GET appendonly appendfsync save
redis-cli INFO persistence
```

`redis_version` 必须不低于 6.2，`XAUTOCLAIM` 必须存在。自管 Redis 必须证明 AOF 已启用且最近 AOF 写入状态正常；托管 Redis 若禁止 `CONFIG`，必须提交控制台持久化配置、服务等级和重启恢复的等价证据。随后执行 Redis 重启场景，证明成本 Stream/outbox 恢复、路由/任务快照重建且 revision 和水位不倒退。缺任一项时 `target_redis_compatibility_and_persistence_evidence` 保持 missing。

## 跨实例接管

至少使用两个加载相同制品和配置、但实例标识不同的应用实例：

1. 记录 consumer、pending、unread、takeover count、路由/页面/任务 snapshot revision 和 event watermark；
2. 在存在 pending 消息时强制终止当前 consumer 实例，等待超过 claim idle 门槛后确认另一实例通过 `XAUTOCLAIM` 接管；
3. 在一个实例持有 snapshot build lease 时终止它，由另一实例接管重建；恢复旧实例后确认旧 fencing token 无法发布；
4. 证明事件最终只产生一次副作用、pending 回落、revision/event watermark 不倒退，用户选路没有同步 DB 回源风暴；
5. 保存两个实例日志、Redis 原始输出、请求报告和时间线。

确定性单元测试只证明实现分支，不能替代上述真实多实例证据。

## 账务对账

每次完整矩阵使用独立测试账户、渠道和时间窗口，并保留请求 ID/event ID。按日、渠道、API Key 和成本类型核对可靠接受事件、成本 Stream、DB outbox、日账本、任务结算、退款/差额调整和页面展示快照。重复投递必须只记账一次，最终 pending/unread/outbox 必须排空，逐事件集合差异和各维度金额差异都必须为零。只比较总金额不能发现事件互相抵消，不能通过闸门。

## 一次性回滚演练

回滚演练必须在相同制品上对全部实例同时应用同一配置，不允许逐实例、逐用户或逐流量切换。至少演练 Redis pool 隔离回滚、成本可靠链路全量切回旧 batcher、前端轮询制品整批回滚，以及没有独立开关的 writer/route/page snapshot 使用整批制品回滚。保留回滚前后配置、实例清单、时间线、队列/账务状态和恢复验证；不得清空 Stream、pending、outbox 或快照伪造恢复。

## 报告证明边界

工具默认只读取 `--metrics-path`（默认 `/api/channel_monitor/`）。选择 `--admin-view` 只决定合成管理员请求，不会自动改变指标采集端点。证明状态、模型检测和路由 snapshot 时，必须分别将 `--metrics-path` 显式设为 `/api/channel_monitor/status`、`/api/channel_monitor/model_detection` 和 `/api/channel_monitor/schedule?metrics=true`，再保存对应视图报告；不能把总览快照字段或仅有端点请求次数当作这些信号的证明。

工具输出的 `report_scope` 固定为 `single_environment_load_scenario`，`does_not_prove` 明列其不能单独证明的外部闸门。最终 CM-10 证据包必须同时包含：所有负载 JSON、故障注入原文及哈希、Redis 兼容性/持久化输出、告警规则和真实通知、跨实例时间线、零差异对账、回滚演练、服务端日志检索语句和制品 SHA。机器契约中相应 hard gate 仍为 `missing`，直到发布负责人审核这些原始证据并在发布记录中逐项关闭；不能因为工具退出码为 0 就改成 complete。

## Prometheus 对照

若测试环境开放只读 Prometheus 文本端点：

```powershell
go run ./cmd/channel-monitor-acceptance `
  <其他执行参数> `
  --prometheus-path=/metrics `
  --prometheus-metrics=redis_pool_wait_total,db_query_total,http_requests_total
```

只采集 `--prometheus-metrics` 指定的指标名，保留 label 维度。前后值和增量分别写入 `prometheus_before`、`prometheus_after` 和 `prometheus_numeric_deltas`。

## 本机验证与真实环境待办

本机可验证命令解析、安全保护、dry-run 矩阵、延迟百分位、监控 JSON/Prometheus 解析，以及单次页面刷新精确扇出：

```powershell
go test ./cmd/channel-monitor-acceptance
```

100/500/1,000 用户并发、10/50 管理员并发、真实 Redis/数据库故障、账务对账、跨实例接管和一次性回滚必须在隔离测试环境执行。本机 fixture 不具备真实上游、Redis 集群、数据库连接池或多实例拓扑，不能替代最终上线验收报告。
