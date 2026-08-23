# 渠道逻辑归组改造：P0 现状基线

> 任务：P0（只读基线）。记录当前代码行为、入口与后续实现契约；本任务未修改业务代码。
> 依据：当前工作树（2026-08-23），并遵循 .agents/downstream.md 的下游变更与冲突约束。

## 1. 当前身份与兼容边界

- model.Channel（表 channels）是一条物理渠道/一个 Key 配置，主键 id（model/channel.go）。Key 可按换行或 JSON 数组解析为现有“单渠道多 Key”模式，但该模式仍共享本行余额、倍率、成本和并发配置。
- channel_id 当前同时是实际请求、日志/成本、监控、状态探测、模型检测和智能调度的身份。未归组时，逻辑组解析必须回退到自身 channel_id，确保升级前行为不变。
- Channel.Weight 是普通渠道选择权重；Ability.Weight/Priority 是模型+用户组路由及智能调度路由权重。两者都按物理 channel_id 存储，不能直接作为新增逻辑组成员 weight。
- 共享范围仅限智能调度、状态探测、模型检测；普通监控性能/成功率/延迟/TPS、成本、余额、倍率、日志与并发仍按物理渠道。

## 2. 普通渠道选择与 weight

### 2.1 内存缓存选择（默认路径）

入口链：middleware.Distribute → service.CacheGetRandomSatisfiedChannel → model.GetRandomSatisfiedChannel。

- model.InitChannelCache（model/channel_cache.go）加载 channels、abilities 和智能调度路由状态，构建 group2model2channels、channelsIDM、channelSmartScheduleRouteCache。
- 非托管（普通）池按优先级降序，再用 Channel.GetWeight（nil 视为 0）随机选择；全 0 时平均分配，低平均权重会放大平滑因子。
- 请求可带 ChannelSelectionOptions.ExcludedChannelIds、请求体大小/估算 token；排除项在缓存路径过滤，重试时由 middleware/distributor_retry.go 维护。

### 2.2 数据库选择（禁用内存缓存时）

model/channel_database_selection.go 的 getChannelFromDatabasePoolWithTrafficPolicy 从 abilities 查询候选，过滤启用状态、请求路径（Advanced Custom）、暂停、请求大小限制，然后按 channelRoutingForTrafficPolicy 得到优先级/weight，最终复用 chooseChannelByWeights（model/channel_weight_selection.go）。

chooseChannelByWeights 语义已有测试（model/channel_weight_selection_test.go）：正 weight 优先排除 0；全部为 0 时平均；溢出返回错误。

### 2.3 重试与显式渠道

- 自动/跨组重试、优先级推进和排除列表在 service/channel_select.go、middleware/distributor_retry.go。
- Token 指定 channel_id 时，middleware.Distribute 直接 GetChannelById(id, true)，检查 Status，不跨成员改投。
- 首次 setup 失败且允许替代渠道时，setupContextForInitialChannel 将失败物理 ID 加入排除列表再走原选择器。

## 3. 智能调度现状

- 路由状态表：ChannelSmartScheduleRouteState（model/channel_smart_schedule_route.go），唯一键（channel_id, group_name, model_name）；保存参与/排除、基础及动态 priority/weight、稳定性保护、采样债务、探索、固定主渠道、revision 等。
- Ability 事实表：Ability（model/ability.go），唯一键（group, model, channel_id），字段 Enabled/Priority/Weight。Channel.AddAbilities/UpdateAbilities 在渠道变更时维护。
- 智能调度执行、评分、样本和路由适配主要在 controller/channel_ratio_monitor_schedule*.go、service/channel_monitor_aggregation.go、service/channel_monitor_redis_*.go、model/channel_smart_schedule_*.go。
- 选择入口 getRandomSatisfiedChannelByAbilityWithTrafficPolicy（model/channel_smart_schedule_route_cache.go）按逻辑池内 route priority 分层后调用 chooseChannelByWeights 选物理 channel；未被智能调度策略托管时回退 officialPriority/officialWeight（渠道原有 priority/weight）。
- 当前每个 channel_id 都有独立调度 route；没有逻辑组 ID 或组级共享评分。运行时状态缓存/Redis key 也以物理渠道维度组织。

## 4. 并发限制（按物理渠道/Key，禁止重复实现）

- 配置持久化在 ChannelRatioMonitor（model/channel_ratio_monitor.go）字段 ConcurrencyLimit、ConcurrencyRevision，唯一 channel_id。API：GET /api/channel_monitor/concurrency；PUT /api/channel_monitor/channel/:id/concurrency。
- 运行时 service/channel_concurrency.go 使用本地缓存或 Redis v1 key（channelConcurrency:v1:*）保存每个 channel 的配置、活跃租约、心跳、释放和 revision；AcquireChannelConcurrency(ctx, channelID) 是唯一获取入口。
- Relay 调用链：controller.acquireRelayChannelConcurrency 先尝试已选 channel；满载时（允许替代且非显式渠道）将该物理 ID 排除并回到现有选择器，最终全部满载返回 429。
- 状态探测/手动测试也直接调用 AcquireChannelConcurrency。逻辑归组不得新增组级计数、租约或 Redis key；组内选出成员后必须复用该成员流程。
- 现有并发回归测试：service/channel_concurrency_test.go、controller/channel_concurrency_test.go。

## 5. 状态探测现状

- 表模型（model/channel_status_probe.go）：ChannelStatusProbeConfig（唯一 channel_id，模型列表、周期、启停、manual request、revision、租约）；ChannelStatusProbeState（唯一 channel_id+model_name，健康计数与样本桶）；ChannelStatusProbeExecution（执行历史，含 RunId/ChannelId/ModelName/Result/成本/请求）。
- 管理 API：GET /api/channel_monitor/status；PUT /api/channel_monitor/status/channel/:id/config；POST /api/channel_monitor/status/channel/:id/run；GET /api/channel_monitor/status/channel/:id/executions。控制器为 controller/channel_status_probe.go。
- Worker 在 controller/channel_status_probe_worker.go：runChannelStatusProbeScanOnce 调用 ClaimDueChannelStatusProbes，每个 config claim 一个 run，按模型逐个 executeChannelStatusProbeModelWithEndpoint；执行前 AcquireChannelConcurrency，随后 testChannel，结果写 ChannelStatusProbeExecution.ChannelId=claim.Config.ChannelId，并可写智能调度样本。
- 现状是一渠道一配置/一轮；共享改造需把 config/state/execution 的逻辑身份提升为组粒度，同时在执行快照保留实际成员 channel_id 与成员成本。

## 6. 模型检测现状

- 表模型（model/channel_model_detection.go）：ChannelModelDetectionConfig 唯一 channel_id；ChannelModelDetectionTarget 按 config/channel/model/claimed-model 建目标；ChannelModelDetectionRun、Execution、CostEvent 均保存 channel_id。
- API 路由由 router/channel-monitor-router.go 中 registerChannelModelDetectionRoutes 提供：GET/PUT /api/channel_monitor/model_detection/settings、GET /api/channel_monitor/model_detection、PUT /api/channel_monitor/model_detection/channel/:id/config、POST /api/channel_monitor/model_detection/channel/:id/estimate、POST /api/channel_monitor/model_detection/channel/:id/run、GET /api/channel_monitor/model_detection/channel/:id/runs、GET /api/channel_monitor/model_detection/runs/:run_id、POST /api/channel_monitor/model_detection/runs/:run_id/cancel；控制器：controller/channel_model_detection_channel.go、..._settings.go、..._run.go、..._query.go、..._runtime.go。
- 服务：service/channel_model_detection_settings.go、..._scheduler.go、..._worker.go、..._run.go、..._query.go、..._cost.go。
- 调度器按 channel config 生成 run（service/channel_model_detection_scheduler.go）；model.CreateChannelModelDetectionRun 以 channel_id + running_run_id='' CAS 保证单渠道单活动轮次。Worker 为每个 target 创建 execution，重试在同一 run 中推进，结算通过 ChannelModelDetectionCostEvent.ChannelId 写入成员日成本。
- 共享改造关键：配置/目标/轮次边界提升为逻辑组；execution/cost event 仍记录实际成员 channel_id，并复用成员并发租约。

## 7. 普通监控、成本、余额和倍率（保持单渠道）

- ChannelMonitorEvent（model/channel_monitor_event.go）必填 ChannelId；service.EmitChannelMonitor*、Redis 聚合器和分钟表按物理 ID 投影。事件来源区分 business/status_probe/smart_probe/model_detection，但没有 logical ID。
- 性能/稳定性模型：model/channel_monitor_performance.go、model/channel_monitor_success.go，指标以 ChannelId+ModelName 聚合；GET /api/channel_monitor/performance、/success/* 返回渠道/Key 明细。
- 普通监控路由：GET /api/channel_monitor/、/performance、/success/today、/success/detail、/history 等，控制器主要在 controller/channel_monitor_realtime_response.go、channel_monitor_today_success.go、channel_monitor_cost.go。
- 成本：GET /api/channel_monitor/cost → controller.GetChannelMonitorCostOverview；ChannelDailyCost、ChannelDailyAPIKeyCost 等按 channel_id/API key 保存；service/channel_daily_cost.go 负责结算及探测/模型检测成本归属。
- 余额/倍率：ChannelRatioMonitor 按 channel_id 唯一保存 ratio、upstream balance、同步配置、并发限制；controller/channel_ratio_monitor*.go 负责更新与展示。
- 逻辑组实现不得新增普通监控/成本/余额/倍率汇总 API，不重写上述物理边界。

## 8. 路由/API 与后续文件所有权

| 领域 | 路由注册 | 主要代码 | 当前主键 |
| --- | --- | --- | --- |
| 渠道管理 | router/channel-router.go | controller/channel.go | channel_id |
| 渠道选择 | middleware/distributor.go | service/channel_select.go；model/channel_cache.go、channel_database_selection.go | group/model/channel_id |
| 智能调度 | router/channel-monitor-router.go | controller/channel_ratio_monitor_schedule*.go；service/channel_monitor_aggregation.go；model/channel_smart_schedule_*.go | channel_id+group+model |
| 并发 | router/channel-monitor-router.go | controller/channel_concurrency.go；service/channel_concurrency.go | channel_id |
| 状态探测 | router/channel-monitor-router.go | controller/channel_status_probe*.go；model/channel_status_probe.go | channel_id+model |
| 模型检测 | registerChannelModelDetectionRoutes | controller/service/model channel_model_detection_* | channel_id+target/run |
| 普通监控/成本 | router/channel-monitor-router.go | controller/channel_monitor_*.go；model/channel_monitor_*.go；service/channel_daily_cost.go | channel_id/API key |

建议所有权：P1 新增 model/channel_logical_group*.go；P2 新增 service/channel_logical_address*.go；P3 新增 controller/service/router logical-group 文件；P4 仅新增 web 配置入口；P5 新增解析器/缓存文件、必要时最小 channel_cache 适配；P7 在 P5 快照契约冻结后先新增 member selector；P6、P8、P9 分别修改 smart-schedule、status-probe、model-detection 文件并统一调用 P7；P10 只增回归测试；P11 只增迁移/集成测试；P12/P13 由主会话协调。

契约：逻辑组至少一成员且一物理渠道最多一组；revision CAS；地址规范化后不一致拒绝；成员 weight 默认 1、全 0 平均、正权排除 0；成员选择后调用 AcquireChannelConcurrency(channel_id)；共享功能每逻辑目标每轮只执行一次；未归组 logical_id=channel_id。

## 9. 未归组验收基线

1. 选择、重试、显式 channel_id、普通监控、成本、余额、倍率、并发、日志和旧 API 与改造前一致。
2. 状态探测/模型检测仍一渠道一配置、一轮一执行。
3. 旧 route state、普通监控事件和 Redis v1 key 保持可读；未归组不写逻辑聚合。

## 10. 验证记录

- P0 建立时的只读搜索已覆盖 SmartSchedule/Weight/并发/StatusProbe/ModelDetection 入口及关联测试；该记录描述的是改造前基线，不代表当前工作树的最终验证结果。
- P0 建立时本机未暴露 Go 命令，因此当时未执行基线测试。后续实现曾通过阶段性定向/包级测试，但 P5-P12 此后仍有集成改动；最终结果必须以主会话在代码收敛后重新执行的定向测试、四包测试和 `go test ./...` 为准。
- 当前实现已完成逻辑组模型、地址预检、管理 API、运行时快照、成员选择器和三条共享链路，并通过最终四包、全仓、前端及三数据库验证。

## 11. P0 交付门槛

本文件是 P0 交付物，本次文档审计未修改业务实现、路由、模型或前端。P0 已用于冻结范围和初始契约；当前是否可以发布由 P13 验收清单和主会话最终验证决定，不以本基线中的历史记录替代。
