# 渠道监控 Redis 增量统计与数据库恢复检查

## 一句话版

请求或结算产生最小事件，后台写入 Redis Stream 并消费，按日期和业务维度增量更新独立 Redis 统计键；页面刷新只读这些键。Redis 键不存在或版本不完整时，后台从数据库事实初始化基线，再接上基线之后的 Stream/outbox 事件并发布新版本。数据库是权威事实和恢复来源，Stream 是增量日志，Redis 键是页面读模型，三者通过事件 ID、处理位置、统计范围和版本对应起来。

请求线程不做全量统计和 Redis 重建。需要绝对不丢失的成本、收入和结算事实走可靠 outbox/Stream；普通观测事件如果允许丢样本，必须明确标记为近似统计，不能拿它冒充可恢复的账务数据。

- 检查日期：2026-10-09，按北京时间解释当日和历史边界。
- 检查基线：`502859299fd270ab31d78405bd402b682ce94df6` 及检查时的工作区内容，包含尚未提交的成本覆盖修复和智能调度修改。
- 用户要求：**“渠道监控都用redis的数据吧，数据库作为兜底和历史数据，全量检查下，然后写到根目录的md文档中”。**
- 后续澄清：**“不是说必须要全部走缓存，我核心的需求是减少数据库查询，因为我会频繁点刷新”。** 历史、权威记账、配置操作和恢复允许使用数据库，正常监控刷新应尽量直接读已更新的 Redis 数据。
- 方案约束：**“缓存的方案要一致吧，防止多套不同的设计”。** 统一事件、增量更新、持久化及恢复规则，复用已有 Stream 与后台消费者。
- 最新原则：**“缓存按理来说是根据请求然后入redis stream然后用独立的key来增量处理数据，如果没有缓存就从数据库进行初始化，要保证redis缓存的数据和数据库中的数据是对应的，尽量别影响用户的请求速度，大的原则是这样”。** 本文据此纠正此前的短期查询结果缓存建议。
- 本次已实施：“不用展示昨日的”。默认总览已删除昨日成本对比及两日成本摘要查询，手动刷新不再请求这份摘要；历史分析仍按用户选定的日期取数。
- 检查范围：渠道监控注册的全部 **37 个 GET 接口**、页面实际取数、相关 Redis 读取实现、数据库调用、后台恢复和历史边界；补充检查公共分组监控及页面使用的分组配置接口。
- 交付状态：全量审计，以及删除默认总览昨日对比和对应两日摘要请求；本次仅纠正后续方案，新增收入投影和恢复改造尚未实施。

## 1. 检查结论

**目标是请求事件驱动的 Redis 增量统计：请求产生事件，后台消费 Stream 更新独立统计键，页面刷新读取 Redis；键缺失时由数据库初始化并继续处理增量。** 数据库保留可靠事实、历史和恢复依据，两边使用相同统计口径。此前“每隔几秒重新查库并缓存整份响应”的建议已撤回，不作为本次实现方向。

| 应改范围 | 当前缺口 | 按最新原则的改法 | 顺序 |
| --- | --- | --- | --- |
| 今日利润和顶部成本，证据 R01 | 成本已有 Redis 增量统计，但顶部利润接口仍反复汇总数据库收入和成本 | 补齐已确认收入、退款和补扣的可靠变更事件及 Redis 投影；与成本按相同事实范围核对后读取，数据库用于初始化和恢复 | 优先 |
| 共享配置和展示资料，证据 R03 | 总览、并发、性能、成功率和调度接口重复读渠道、设置或监控行 | 复用已有配置副本与共享快照，保存及运行变化后发布新版本；初始化或恢复时查库 | 优先 |
| 昨日对比额外查询 | 原主页面每次刷新请求 `/cost?days=2`，用于昨日对比 | 已删除对比文案、摘要查询和刷新目标 | 已完成 |
| 成本完整性检查，证据 R04 | 今日成本明细每次扫描当前范围 outbox | 后台维护按日期、渠道、模型等维度的待处理及覆盖状态，和已投影金额对应；页面读取这些状态，不能只删除检查 | 明细读取 |
| 探测与模型检测总览，证据 R02 | 当前状态及近期结果直接查库，活动任务约每秒查询 | 在配置和任务状态变化后发布增量或同版本快照；页面读 Redis，历史审计仍读数据库 | 对应页面 |
| 缺失恢复和持久化，证据 R05/R06/R07 | 各入口恢复规则不一致；部分历史只存 Redis；普通监控入队可丢样本 | 统一缺失检测、单次重建及增量续接，补齐需要恢复的数据与缺口记录；无法重建时明确不完整 | 跨模块一致性要求 |

默认总览昨日对比查询已取消。先复用已有成功率和成本增量处理、检查点与重建机制，补齐今日收入及共享资料；各项接入时同时验证恢复和事件可靠性，不能到最后才考虑。SQL 次数和请求延迟仍需测量，不提供推测的减少比例。

**继续保留：** 已有 Redis 实时统计、分组及调度共享快照、独立历史查询，以及数据库扣费、退款、可靠记账和后台持久化。已有链路符合原则的部分直接复用，缺口按同一规则补齐。

配置对话框、历史弹窗和上游测试等低频权威读取不要求全量改成 Redis。缺失时能否恢复、Redis 数据是否有对应的数据库事实或检查点，是本次方案的一部分；并发租约等瞬时数据没有等价数据库历史，不能用历史数值恢复为当前状态。

已有符合方向的部分包括：当日成本金额、当日成功率与缓存统计、分钟性能统计、Redis 并发计数、分组监控共享快照，以及满足开关条件时的智能调度共享读模型。数据库中的可靠记账、后台聚合、历史读取和冷启动重建本身符合要求，不能因为“页面读 Redis”而删除。

## 2. 统一方案及边界

基本流程如下：

1. 用户请求产生带标识、版本及原始归属维度的事件，使用已有可靠交接方式提交；不在请求中计算大范围统计或重建 Redis。
2. 后台消费 Redis Stream，按指标、日期、渠道、模型、用户、API Key 等维度增量更新独立键，并保存对应数据库事实或累计检查点。
3. 页面刷新读取这些键及同版本的资料和覆盖状态，不因点击刷新反复查询数据库汇总。
4. Redis 键缺失时，通过已有恢复机制从数据库装入基线，再接上基线之后的未处理事件；完成后恢复正常增量读取。多个页面或节点只触发同一范围的一次重建。

**两边“对应”指同一统计范围、同一事件版本或处理位置下数值能核对，后台追平后最终一致。** 异步期间 Redis 和数据库可能先后更新；必须报告真实处理位置、延迟和缺口，不能承诺任意瞬间数值完全相等，也不能把初始化中的空值当作完整的零。

| 场景 | 目标主要来源 | 数据库允许参与的方式 |
| --- | --- | --- |
| 今日和分钟统计 | Stream 后台更新的 Redis 独立统计键 | 后台持久化、核对、初始化、缺失恢复和必要兜底 |
| 今日收入和利润 | 新增收入变更投影，复用成本投影，保证二者范围和确认状态对应 | 原收入/成本账本是权威来源，用于初始化、重放及对账；新增投影接入前仍为现有数据库读取 |
| 配置、名称和监控状态 | 已有配置副本或同版本共享快照，变化后更新 | 保存、初始加载、后台发布或恢复及操作时的权威校验 |
| 历史汇总及执行审计记录 | 数据库或已有持久化投影 | 显式历史查询正常查库；默认总览不再查询两日成本摘要 |
| 同时包含历史和今天的范围 | 数据库历史段与 Redis 今日段按日期边界合并 | 历史查询及缺失恢复，不把同一事实在两段重复统计 |
| 配置保存、主动执行、资金扣费及退款 | 权威数据库事务及现有任务流程 | 正常读写、冲突校验、持久记账 |
| 并发租约、实时队列等没有同口径数据库记录的指标 | Redis；不可用时明确显示不可用 | 不能用历史数据库数值冒充当前计数 |

配置写入时的数据库修订校验继续保留；展示快照不能替代它。实际扣费、退款和收入确认也保留可靠事务，异步的是额外监控统计和 Redis 更新。

### 一套事件和恢复规则

复用现有 `channel_monitor:v1:events` 监控 Stream、`channel_cost:v1:events` 可靠成本链路及独立投影键。两个现有 Stream 承担的可靠性职责不同，不能仅为形式统一就合并或重复累计金额；统一事件关联、幂等、确认和恢复规则，新增统计使用同一套消费者及恢复基础设施。

| 统一项 | 实现要求 |
| --- | --- |
| 事件及版本 | 保存唯一事件或结算标识、原发生日期和归属、版本及修正关系；重复投递、重试和乱序不能重复记数或回退金额 |
| 独立统计键 | 按指标、日期和稳定业务维度组织统计，不按页面排序和分页复制数据库响应；复用已有键前缀、维度规范化及金额整数单位 |
| 原子增量 | 通过已有 Lua 或 WATCH/事务将去重标记、增量数据和处理位置一起提交；各消费者只更新自己负责的指标，部分完成后重试安全 |
| 数据库对应 | 以同一事件事实或可重放检查点持久化；Redis 和数据库按相同日期、维度、单位和已确认版本核对，包含退款、迟到事件和异步任务修正 |
| 缺失初始化 | 同范围重建只有一个持有者；读取一致的数据库基线及对应处理位置，补齐之后的 Stream/outbox 增量，再原子发布有效版本 |
| 重建并发 | 新事件继续可靠接收；临时重建代次与正在运行的消费者正确衔接，发布校验租约及版本，不让旧重建覆盖新数据；不能仅凭最大事件 ID 认定前面的事件全部处理 |
| 覆盖状态 | 金额、已处理位置、待处理归属和缺口状态对应；后台发布范围索引后 GET 不再每次扫描 SQL。初始化、队列积压、无法重放及投影失败按真实状态展示 |
| 配置及状态 | 复用 `OptionMap`、渠道缓存、分组和调度快照；配置保存、任务进度、完成和余额变化后发布对应版本，缺失时初始化，不让各接口独立查同一份资料 |
| 手动刷新 | 读取最新已消费的 Redis 统计和版本，不清空聚合键，不为刷新重新计算数据库汇总；历史和明确操作保留其合理 SQL |
| 保留与过期 | TTL 用于数据保留、临时租约或健康过期，不能把统计每几秒过期后重新查库；Stream/outbox 保留必须覆盖尚未持久化和恢复所需增量 |
| 失败与兜底 | 有同口径数据库事实时受限回源并启动恢复；无等价记录时显示不可用/不完整，不伪造零值或当前状态。恢复及兜底有超时、批量和并发限制 |
| 请求性能 | 复用现有小型可靠提交和有界异步处理，聚合、扫描、数据库批量落库和重建放在后台；监控不能拖慢上游响应、流式首字或传输 |

纯内存入队不能提供崩溃后的不丢事件保证。需要保证可恢复的资金及统计事实必须有持久交接，优先复用现有结算事务、可靠 outbox 和 Stream；保留必要的最小提交开销，不新增请求内全量统计，也不宣称“零 I/O 且绝不丢失”。现有普通监控丢样本边界见 R07。

此处规定后续改造方向，**本轮只纠正文档，没有上线新投影或重建代码**。现有成功率、成本、分组和调度机制保留并补齐；撤回此前 `HybridCache` 查询结果包装与 1/5/30 秒响应缓存策略。

## 3. 读取热点及边界证据

### R01. 今日利润、收入及顶部成本直读数据库

入口在 [channel_monitor_analytics.go:336](D:/GoProjects/new-api/controller/channel_monitor_analytics.go:336)。`metric=profit` 在判断“今日走 Redis”之前就进入 `queryChannelMonitorProfitAnalytics`，因此今日、历史和跨日利润均使用数据库。

[channel_monitor_profit.go:39](D:/GoProjects/new-api/controller/channel_monitor_profit.go:39) 将收入记录与成本日汇总或明细组合成数据库统计事实；[同文件:94](D:/GoProjects/new-api/controller/channel_monitor_profit.go:94) 的查询路径是：

1. 读取 Redis 成本队列。
2. 读取收入缺口的持久化目录及进程内待写入标记。
3. 开启数据库只读事务，读取收入状态、汇总、明细、缺口和待记账成本。
4. 返回 `source=database_daily`。

这是正常路径，Redis 中存在今日成本也不会将利润切换到 Redis。利润完整性校验还访问数据库缺口和明细核对结果，见 [channel_monitor_profit_coverage.go:29](D:/GoProjects/new-api/controller/channel_monitor_profit_coverage.go:29)。

前端 [use-channel-monitor-profit.ts:4](D:/GoProjects/new-api/web/src/features/channel-monitor/hooks/use-channel-monitor-profit.ts:4) 请求今日利润；[index.tsx](D:/GoProjects/new-api/web/src/features/channel-monitor/index.tsx) 从 `scope_summary` 取今日成本及成本分类；[channel-monitor-profit.tsx:30](D:/GoProjects/new-api/web/src/features/channel-monitor/components/channel-monitor-profit.tsx:30) 展示该成本。

**页面上顶部“今日已结算成本”与收入、利润共用数据库核对结果，而成本入口的今日分析金额可以来自 Redis。** 两者的数据截止点独立，后台投影和账本写入的先后顺序可能导致短暂差异。不能仅凭 `/cost` 或响应中的 `redis_daily` 推断整个页面今日成本都读 Redis。

按最新原则的改法：保留收入确认、退款、补扣的数据库事实，将已提交的变更通过可靠事件交接给后台，增量更新独立收入键，复用现有可靠成本投影。金额和确认状态按相同事实范围核对；Redis 缺失时从收入账本和成本账本初始化并续接增量。总览和渠道行读同一投影。当前 [ChannelMonitorEvent](D:/GoProjects/new-api/model/channel_monitor_event.go:67) 没有收入结算金额和修订字段，不能仅将利润 GET 改成读现有成本键；也不能把不同进度的收入和成本相减后标记“已确认”。

### R02. 主动探测和模型检测总览以数据库为主

主动探测：[channel_status_probe_overview_query.go:34](D:/GoProjects/new-api/controller/channel_status_probe_overview_query.go:34) 明确说明每次读取当前数据库，只合并正在进行的相同请求，不保存已完成结果。

[channel_status_probe.go:512](D:/GoProjects/new-api/controller/channel_status_probe.go:512) 构建总览时读取数据库中的逻辑渠道关系、探测配置、渠道资料，以及：

- `GetChannelStatusProbeStatesForOverview`：当前探测状态。
- `GetChannelStatusProbeExecutionsSinceForOverview`：展示窗口内的执行结果。
- 监控倍率配置。
- 今日探测成本：开启 Redis 时读取 Redis 日成本，关闭时读取数据库。

因此 `/status` 并非仅为历史记录使用数据库。当前状态和页面近期窗口也直接查库。

模型检测：[channel_model_detection_overview_query.go:34](D:/GoProjects/new-api/service/channel_model_detection_overview_query.go:34) 同样只有请求中的 `singleflight` 合并。

[channel_model_detection_query.go:319](D:/GoProjects/new-api/service/channel_model_detection_query.go:319) 读取数据库渠道、全局和渠道配置、目标、逻辑渠道配置、当前与最近轮次、执行结果及关联成本审计记录。今日检测成本汇总在 Redis 开启时来自 Redis，但轮次及执行详情仍来自数据库。

总览还调用 [channel_model_detection_settings.go:402](D:/GoProjects/new-api/service/channel_model_detection_settings.go:402) 的服务状态查询；配置有效时会检查检测服务兼容性和状态，产生外部 HTTP，并检查数据库中的会话归属。

前端运行中的探测和模型检测按 1 秒刷新。已完成请求不会缓存，所以持续轮询会持续触发上述读取，不能将 `singleflight` 当作 Redis 缓存。

按最新原则的改法：利用已有配置和任务变更通知，在任务启动、进度更新、完成和配置保存时发布运行状态增量或同版本快照，页面读取 Redis；缺失时由数据库当前状态和执行事实重建，历史审计继续查库。检测兼容性与外部服务状态由后台检查并保存真实检查时间，显式“测试服务”仍可调用外部服务。请求合并保留用于受限初始化，不代替增量更新。

### R03. 实时接口仍同步查询配置、名称和部分运行状态

主总览 [channel_ratio_monitor.go:514](D:/GoProjects/new-api/controller/channel_ratio_monitor.go:514) 依次读数据库渠道、倍率监控行和设置，再读取 Redis 今日成本、并发和余额估算。

其中 `LastFetchStatus/Error/Time`、失败次数、已同步倍率、上游原始余额和上次余额查询时间来自数据库监控行。这些不仅是固定名称，也包含当前展示的运行结果。

| 正常请求中的数据库依赖 | 受影响路径 | 说明 |
| --- | --- | --- |
| 渠道和倍率监控行 | 总览、并发、今日成功率、分析选项、主动探测 | 展示列表或并发限制配置并非统一从 Redis 快照取得 |
| `loadChannelMonitorSettings` | 总览、性能、智能调度 | 每次读取数据库 option；另一函数 `getChannelMonitorSettings` 才读取进程内 `OptionMap` |
| 用户名称、渠道名称搜索 | 日分析的搜索和名称补充 | Redis 统计成功后仍可能查数据库，搜索也可能先查库 |
| API Key 所有人和用户资料 | 成本、今日成功率、成功率明细、分钟分析 | 不只影响显示；部分分钟分析按当前 Token 所有人推断用户归属 |
| 数据库只读事务 | `/cost` | `days=1` 即使历史范围为空，也进入 `RunChannelMonitorCostRead`；金额可仍全部来自 Redis |
| 上游账户及成员 | `/upstream_accounts` | 原始余额和查询状态来自数据库，实时消费估算来自 Redis |
| 自动化任务 | `/automations` | 配置和当前任务状态来自数据库；GET 还调用迁移，必要时可以写库 |

关键位置：[channel_ratio_monitor_settings.go:346](D:/GoProjects/new-api/controller/channel_ratio_monitor_settings.go:346)、[channel_monitor_analytics_filter.go:44](D:/GoProjects/new-api/controller/channel_monitor_analytics_filter.go:44)、[channel_monitor_cost.go:876](D:/GoProjects/new-api/controller/channel_monitor_cost.go:876)、[channel_monitor_minute_analytics.go:25](D:/GoProjects/new-api/controller/channel_monitor_minute_analytics.go:25)、[channel_monitor_cost_query.go:24](D:/GoProjects/new-api/model/channel_monitor_cost_query.go:24)、[upstream_automation.go:14](D:/GoProjects/new-api/controller/upstream_automation.go:14)。

按最新原则的改法：复用普通刷新共同使用的配置副本和共享快照，缺少的渠道资料、监控状态及 Key 展示归属在初始化和对应变化后更新同一份数据，不按每个 GET 重建。已有请求级归属继续冻结在事件里，不能用最新 Key 所有人改写历史统计。今日单独查询可跳过空历史事务。

自动化 GET 迁移、账户及变量配置属于对应面板的次级优化，仅在这些面板的实际使用和查询量需要时处理；不把全部配置读取视为必须整改。

原主页面固定请求 `/cost?days=2&summary_only=true`，每次刷新重新读取昨日汇总。用户取消昨日展示后，这个查询已从页面及刷新目标删除，无需为默认总览再新增昨日缓存。显式打开历史分析仍可读取数据库；若以后优化相同历史范围，应考虑迟到成本、退款和补账，不按“已经过了零点”永久冻结结果。

### R04. 刚修复的成本覆盖判断增加了正常路径数据库读取

当前 [channel_monitor_current_analytics.go:240](D:/GoProjects/new-api/controller/channel_monitor_current_analytics.go:240) 按以下顺序查询：

1. Redis 成本流、待处理队列和死信。
2. 数据库 `ChannelDailyCostOutbox` 中 `redis_projected_at=0` 的记录。
3. Redis 今日成本日快照。

**显示的成本金额仍来自 Redis，数据库参与的是“是否还有当前筛选范围内的未投影成本”的判断。** 数据库不可读时保留已读 Redis 金额，并标记 `cost_projection_unavailable`，不会自动换成数据库金额。

[channel_monitor_cost_coverage.go:44](D:/GoProjects/new-api/controller/channel_monitor_cost_coverage.go:44) 限制数据库检查为 3 秒、最多 4096 条加一条上限检测记录；日期、渠道、用户、Key 先在 SQL 筛选，模型等条件再匹配。超过上限仍保守提示覆盖不完整。

这解决了其他模型排队导致当前模型误报的问题，也保留 Redis 队列与数据库 outbox 交接时的完整性检查。按减少刷新查库的目标，它是**打开成本分析后需要优化的重复检查**，不是普通总览每次刷新的直接成本路径。不能只删掉检查，否则尚未进入 Redis 队列的成本可能被误认为已经完整统计。

按最新原则的改法：在事件可靠接收、投影成功及失败恢复时，后台维护同日期、渠道、模型、用户和 Key 的待处理索引与覆盖状态，和成本投影的处理位置对应。待该状态覆盖 Redis 队列与数据库 outbox 的完整交接后，GET 改为读取 Redis 状态，取消每刷新扫描 SQL；不能先删掉数据库检查。其他模型 pending 不影响当前模型，未知归属或缺口继续保守标记。已有全局 `Pending` 心跳不能直接代替范围检查。

### R05. Redis 关闭、缺失和故障没有统一兜底规则

本项记录当前可用性缺口。“没有缓存就从数据库初始化”是最新原则中的必要部分，后续接入每种投影时必须同时验证；故障回源使用原口径，合并初始化并限制数据库压力。

[channel_monitor_realtime_cost.go:23](D:/GoProjects/new-api/controller/channel_monitor_realtime_cost.go:23) 只有 Redis 被关闭时才读数据库日成本；Redis 已开启但读取失败时直接返回错误。[channel_monitor_cost_read_source.go:19](D:/GoProjects/new-api/controller/channel_monitor_cost_read_source.go:19) 同样传播 Redis 错误。

成本日 Hash 缺少有效版本也会返回投影不可用，见 [channel_monitor_redis_daily_cost.go:49](D:/GoProjects/new-api/service/channel_monitor_redis_daily_cost.go:49)。它没有在 GET 中同步恢复或查库替代。

| 数据类型或入口 | Redis 正常 | Redis 已开启但缺失或故障 | Redis 关闭 |
| --- | --- | --- | --- |
| 总览和 `/cost` 的今日成本 | Redis 日成本 | 返回错误，不切数据库金额 | 读取数据库日成本 |
| 日分析的当日成本、成功率及缓存指标 | Redis 日投影 | 读取错误直接失败；健康问题可返回不完整状态 | 走数据库日统计分支 |
| `/success/today` | 今日 Redis；历史部分数据库 | 今日 Redis 读取失败则整个请求失败 | 仍调用 Redis 查询，没有对应关闭分支 |
| 分钟分析、`/performance`、`/success/detail` | Redis 分钟投影 | 错误或覆盖不可用，不使用数据库统计替换 | Redis 查询不可用 |
| 分组监控设置、总览和公共展示 | Redis 共享快照 | 快照不可读或不存在时返回 503 | 返回快照未就绪 |
| `/schedule` | 条件满足时使用 Redis 发布的本机镜像及 Redis 实时指标 | 镜像缺失或过期会报错；性能可显示不可用，未统一切换 SQL | 路由资料可走数据库分支；实时指标仍受其 Redis 来源限制 |
| 并发和限流组实时计数 | Redis 租约与计数 | 错误或 `unavailable/publishing` | 使用本机进程计数，非数据库兜底 |
| 余额估算 | Redis 消费投影结合余额基准 | 估算不可用或省略；原始余额仍可来自数据库 | 不构成同口径的共享实时估算 |
| 被动监控当前周期 | Redis 周期统计，本机持有配置快照 | 可保留本机上次完整周期及原时间，并标记不可用 | Redis 统计不可用 |
| 被动监控历史和版本 | Redis 小时桶及索引 | 无数据库兜底；元信息读取失败返回错误，桶读取失败标记不可用 | 不可用 |
| 诊断计数 | Redis 写角色的 Lua 读取 | 返回错误 | 不可用 |
| 今日利润 | 数据库事实及其他缺口来源 | 金额仍依赖数据库，队列问题影响确认状态 | 仍以数据库为主 |

后续可用性方向：对有同口径持久数据的统计复用已有重建方法，统一页面缺失检测与后台恢复的衔接，避免每次 GET 各自查库重建。初始化明确来源、处理位置和完整性。并发租约等瞬时数据没有等价数据库事实，保留不可用状态；只有 Redis 保存的历史必须先补齐持久化或可靠重放依据才能承诺恢复。

### R06. 部分历史仍只在 Redis

本项说明哪些数据暂时不具备数据库重建依据。按最新原则，需要承诺恢复的统计应补齐持久化；不能因为当前读取快，就认为丢失后可以从数据库初始化。

[channel_monitor_passive_read.go:150](D:/GoProjects/new-api/service/channel_monitor_passive_read.go:150) 读取历史元信息及小时桶；版本接口也读取 Redis 元信息和索引，没有查询数据库。

[channel_monitor_passive_projection.go:18](D:/GoProjects/new-api/service/channel_monitor_passive_projection.go:18) 定义 32 天保留范围；详细周期桶到周期结束后 48 小时过期，小时桶到小时结束后 32 天过期。历史接口最多展示 30 天。**展示 30 天不表示数据库中存在这 30 天数据。**

分组监控的近期状态窗口来自 Redis 探测投影，见 [channel_group_monitor.go:526](D:/GoProjects/new-api/controller/channel_group_monitor.go:526)。独立 `/group_monitor/executions` 执行审计记录才读取数据库。分组快照构建中的长窗口缓存率可组合数据库历史和 Redis 今日数据，见 [channel_group_monitor_cache.go:36](D:/GoProjects/new-api/service/channel_group_monitor_cache.go:36)。

后续持久化方向：确认被动监控的恢复范围，将需要恢复的小时统计、覆盖状态和目标元信息按原配置版本落库或保留可靠可重放事件。不能用今天的配置重新解释旧周期，也不能将执行审计记录当成已有同口径的历史聚合。实际范围和保留周期需在实现前明确，本文不声称当前已可恢复全部历史。

### R07. 现有事件、增量键和恢复基础已具备，但可靠性边界不同

现有代码已有用户描述的主体链路：

- [service/channel_monitor_event_emit.go:114](D:/GoProjects/new-api/service/channel_monitor_event_emit.go:114) 将请求成功/失败观察事件交给 [EnqueueChannelMonitorEvent](D:/GoProjects/new-api/service/channel_monitor_event_writer.go:271)，后台 writer 写入 `channel_monitor:v1:events`。
- [channel_monitor_redis_aggregator.go:180](D:/GoProjects/new-api/service/channel_monitor_redis_aggregator.go:180) 消费后更新路由、共享统计、分组与被动监控；独立键在 [channel_monitor_redis_keys.go](D:/GoProjects/new-api/service/channel_monitor_redis_keys.go) 中定义，日成功率和日成本已有各自 Hash。
- [channel_monitor_daily_persistence.go:337](D:/GoProjects/new-api/service/channel_monitor_daily_persistence.go:337) 后台保存日统计及事件检查点；[model/channel_monitor_daily_checkpoint.go:69](D:/GoProjects/new-api/model/channel_monitor_daily_checkpoint.go:69) 在事务中提交累计值和处理位置，旧修订不会覆盖新修订。
- [channel_monitor_daily_persistence.go:38](D:/GoProjects/new-api/service/channel_monitor_daily_persistence.go:38) 检测日键版本并从数据库恢复，通过 Stream 续接检查点之后的事件；成本 [channel_monitor_reliable_cost_projection.go:296](D:/GoProjects/new-api/service/channel_monitor_reliable_cost_projection.go:296) 从一致的账本快照及 outbox 版本恢复，避免重复累计。
- [channel_monitor_redis_runtime.go:39](D:/GoProjects/new-api/service/channel_monitor_redis_runtime.go:39) 启动时尝试重建；成本运行器还在后台检查当天键并恢复。已有恢复不等于每个页面缺失后的触发与兜底都已经接通，见 R05。

普通监控事件入队为非阻塞内存队列；[channel_monitor_event_writer.go:303](D:/GoProjects/new-api/service/channel_monitor_event_writer.go:303) 队列满或 writer 未就绪会丢样本并告警，进程崩溃也可能丢失尚未写入 Stream 的内存事件。只有数据库检查点及 retained Stream 存在的部分可以重放，不能声称恢复后绝无缺口。

可靠成本使用 [channel_daily_cost_outbox.go:145](D:/GoProjects/new-api/service/channel_daily_cost_outbox.go:145) 的 `channel_cost:v1:events`，发布失败写数据库 outbox，保留资金事件的持久交接。收入 [service/channel_monitor_income.go:35](D:/GoProjects/new-api/service/channel_monitor_income.go:35) 也保留结算前后的数据库事实；它还没有对应的 Redis 收入聚合读取。这些权威事实用于新增收入投影及恢复，不能改成可能丢失的普通观察事件。

按最新原则的改法是补齐缺失统计与恢复交接，复用上述基础；同时测量可靠事件提交和队列溢出时的用户请求延迟。额外统计工作异步执行，必要的资金持久提交继续保留。

## 4. 全部读取接口清单

下表逐条覆盖两份渠道监控路由文件中的 37 个 GET。`SQL` 表示主要业务数据库；“混合”描述真实来源，不代表已经完成数据库兜底。此表是取数清单，不是 37 项整改清单；是否应改以第 1、8 节的刷新热点为准。

路由依据：[channel-monitor-router.go](D:/GoProjects/new-api/router/channel-monitor-router.go)、[channel-model-detector-router.go](D:/GoProjects/new-api/router/channel-model-detector-router.go)。

| # | GET 路径 | 正常数据来源 | 数据库的实际角色及检查结果 |
| --- | --- | --- | --- |
| 1 | `/api/channel_monitor/` | SQL 渠道、监控设置、倍率和余额状态；Redis 今日成本、并发、余额估算；本机及 Redis 健康信息 | 正常依赖 SQL，R03；Redis 错误无统一金额兜底，R05 |
| 2 | `/api/channel_monitor/health` | 本机健康快照，健康工作线程结合 Redis 状态更新 | GET 不同步汇总数据库；本机健康不是所有节点的统一快照 |
| 3 | `/api/channel_monitor/diagnostics` | Redis 写角色 Lua；读取同时推进日界和观测时间 | 没有 SQL 统计替代，R05；并非普通只读副本查询 |
| 4 | `/api/channel_monitor/concurrency` | SQL 渠道 ID、并发配置；Redis 实时计数 | 配置正常查库；关闭 Redis 用本机计数，R03/R05 |
| 5 | `/api/channel_monitor/cost` | 今日 Redis，历史 SQL；事务、资料和 Key 归属另查 SQL | 今日金额方向正确，整条请求仍依赖 SQL；已开启 Redis 失败不切金额来源，R03/R05 |
| 6 | `/api/channel_monitor/analytics/summary` | 今日 cost/success/cache 等日指标走 Redis；历史 SQL；跨日合并；profit 始终 SQL；分钟走 Redis | profit 见 R01，今日成本覆盖见 R04；搜索和名称补充见 R03 |
| 7 | `/api/channel_monitor/analytics/trend` | 按日趋势：今日与历史按上述指标分流，profit 始终 SQL | 同上；不支持分钟趋势 |
| 8 | `/api/channel_monitor/analytics/rows` | 与 summary 共用处理，支持分钟或日统计 | 同上；不能仅根据响应 source 判断是否完全无 SQL |
| 9 | `/api/channel_monitor/analytics/options` | SQL 渠道列表 | 可复用共同的渠道缓存；低频打开分析选项不单独列为必改项，R03 |
| 10 | `/api/channel_monitor/performance` | Redis 分钟性能、成功率；SQL 监控设置 | 指标来自 Redis，设置正常查库，R03/R05 |
| 11 | `/api/channel_monitor/status` | SQL 配置、当前探测状态和近期执行窗口；Redis 今日成本 | 当前状态 SQL 主读，R02 |
| 12 | `/api/channel_monitor/status/channel/:id/executions` | SQL 探测执行记录 | 历史审计正常；运行中的最新记录也沿用这条 SQL 查询 |
| 13 | `/api/channel_monitor/passive` | 本机配置目标快照和 Redis 周期统计 | 统计符合 Redis 方向；配置不是请求中 SQL 读取；故障保留旧周期时明确不可用 |
| 14 | `/api/channel_monitor/passive/:target/history` | Redis 元信息、覆盖信息和小时桶 | 历史没有 SQL 来源，R06 |
| 15 | `/api/channel_monitor/passive/:target/versions` | Redis 目标元信息及旧版本索引 | 版本历史没有 SQL 来源，R06 |
| 16 | `/api/channel_monitor/group_monitor/settings` | Redis 发布的完整共享快照中的设置 | GET 无 SQL 回源；后台构建读取 SQL，缺失直接 503，R05 |
| 17 | `/api/channel_monitor/group_monitor/overview` | Redis 共享快照中的统计和近期状态窗口 | 实时读取符合方向；窗口和故障边界见 R05/R06 |
| 18 | `/api/channel_monitor/group_monitor/executions` | SQL 分组探测执行记录 | 历史查询符合方向 |
| 19 | `/api/channel_monitor/success/today` | 今日 Redis；历史日明细及趋势 SQL；渠道与 Key 所有人 SQL | 今日指标方向正确，资料正常查库；关闭 Redis 也没有今日 SQL 分支，R03/R05 |
| 20 | `/api/channel_monitor/success/detail` | Redis 分钟成功率明细；Key 所有人及用户资料 SQL | 指标方向正确，资料正常查库，R03/R05 |
| 21 | `/api/channel_monitor/tasks` | SQL 系统任务列表及已保存状态 | 历史列表正常；包含当前任务时其状态仍为 SQL 来源 |
| 22 | `/api/channel_monitor/tasks/:task_id/details` | SQL 任务类型与智能调度执行快照明细 | 历史明细符合方向 |
| 23 | `/api/channel_monitor/tasks/:task_id/ratio-details` | SQL 倍率任务及保存的结果 | 历史明细符合方向 |
| 24 | `/api/channel_monitor/schedule` | SQL 监控设置；条件满足时 Redis 发布的路由和经济资料本机镜像；Redis 健康窗口 | 共享读模型符合方向，但设置正常 SQL，开关及过期边界见第 6 节 |
| 25 | `/api/channel_monitor/channel/:id/probe-policy` | SQL 探测策略 | 低频配置读取，可保留权威数据库查询，不属于普通刷新必改项 |
| 26 | `/api/channel_monitor/channel/:id/history` | SQL 倍率监控历史 | 历史符合方向 |
| 27 | `/api/channel_monitor/limit-groups` | SQL 限流组及注册修订；Redis 运行计数 | 正常查配置 SQL，故障标记 unavailable/publishing；关闭 Redis 用本机计数 |
| 28 | `/api/channel_monitor/upstream_accounts` | SQL 上游账户、成员、原始余额和状态；Redis 余额估算 | 当前余额展示也依赖 SQL，R03 |
| 29 | `/api/channel_monitor/automations` | SQL 自动化配置、系统任务状态；请求前尝试迁移 | 配置和当前状态正常查库；GET 可触发迁移写入，R03 |
| 30 | `/api/channel_monitor/variable_groups` | SQL 共享请求与变量定义，转换为配置响应 | 配置正常查库；此 GET 不等于执行外部变量抓取，R03 |
| 31 | `/api/channel_monitor/token_protection/settings` | 本机自动保护管理器的设置快照 | 没有每请求 SQL，但不是统一 Redis 配置快照 |
| 32 | `/api/channel_monitor/token_protection/records` | SQL 持久保护记录及本机 pending 列表 | 历史记录可用 SQL；pending 是本节点内存状态 |
| 33 | `/api/channel_monitor/model_detection` | SQL 配置、当前/近期轮次和结果；今日总成本 Redis；检测服务 HTTP | R02；今日成本 Redis 不代表整个检测视图 Redis |
| 34 | `/api/channel_monitor/model_detection/settings` | SQL 全局检测配置，缺失时可创建默认值 | 配置 GET 正常依赖 SQL，R03 |
| 35 | `/api/channel_monitor/model_detection/service` | SQL 服务配置及会话归属；外部检测服务 HTTP | 当前服务状态未发布到 Redis，R02 |
| 36 | `/api/channel_monitor/model_detection/channel/:id/runs` | SQL 渠道检测轮次及执行信息 | 历史入口符合方向；当前轮次也可能被查询 |
| 37 | `/api/channel_monitor/model_detection/runs/:run_id` | SQL 指定轮次、结果和报告 | 历史明细符合方向；运行中详情轮询仍查 SQL |

### 页面关联的其他读取及操作

| 路径或操作 | 实际来源 | 边界 |
| --- | --- | --- |
| `GET /api/pricing/group-monitor` | 与管理员分组总览同一 Redis 快照 | 不查 SQL 回源，缺失 503；属于公共页面关联展示 |
| `GET /api/group/` | 进程内倍率配置副本 | 页面分组选项；不是每次查询数据库，也不是请求中 Redis GET |
| POST 上游 groups/version/test、变量 fetch、模型检测 estimate/service/test | 显式操作读取配置或调用上游/检测服务 | 操作取数与持续监控 GET 分开；不能当作已有 Redis 展示快照 |
| PUT/POST/DELETE 配置、执行任务、释放保护及统计回填 | 权威数据库写入或任务流程，按操作同步 Redis | 本次未要求取消数据库写入；写完仍须正确更新展示快照 |

## 5. 前端实际取数与刷新

| 页面内容 | 实际后端来源 | 刷新行为 |
| --- | --- | --- |
| 顶部今日成本、用户扣费、利润和利润率 | 今日 `metric=profit` 数据库统计 | 挂载、页面手动刷新或利润核对；失败可保留上次结果 |
| 渠道行利润 | 同一数据库利润接口；总查询未覆盖的渠道可单独请求 | 随对应查询刷新，失败状态保留并提示 |
| 渠道行今日成本、成本下钻 | 总览或分析的 Redis 今日成本；所选历史段数据库 | 主体沿用手动刷新 |
| 原主页面两日成本汇总 | 已删除昨日展示所用的 `/cost?days=2&summary_only=true` 查询 | 挂载和普通手动刷新不再为昨日对比执行这份历史汇总 |
| 今日成功率、缓存写入及日趋势 | 今日 Redis，加数据库历史及资料 | 主体沿用手动刷新 |
| 分钟性能和分钟分析 | Redis 指标，设置/资料可有 SQL | 主体沿用手动刷新 |
| 主动探测及模型检测当前状态 | SQL 为主；检测状态可额外 HTTP | 存在活动任务时约每 1 秒轮询 |
| 活动探测历史及模型检测轮次详情 | SQL 记录 | 活动期间也可按约 1 秒轮询 |
| 被动监控面板 | Redis 周期统计及本机目标快照 | 每 15 秒刷新 |
| 自动化列表、API Key 保护记录 | SQL 为主，保护 pending 是本机内存 | 每 5 秒刷新 |

主体查询配置见 [query-options.ts:32](D:/GoProjects/new-api/web/src/features/channel-monitor/lib/query-options.ts:32)：默认无定时、后台、窗口聚焦和重连刷新；`gcTime=0`、`staleTime=0`、挂载刷新。活动任务间隔常量为 1000 毫秒。

例外位置：[channel-status-probe-view.tsx:162](D:/GoProjects/new-api/web/src/features/channel-monitor/components/channel-status-probe-view.tsx:162)、[index.tsx](D:/GoProjects/new-api/web/src/features/channel-monitor/index.tsx)、[channel-status-probe-history-sheet.tsx:281](D:/GoProjects/new-api/web/src/features/channel-monitor/components/channel-status-probe-history-sheet.tsx:281)、[channel-model-detection-run-detail-sheet.tsx:82](D:/GoProjects/new-api/web/src/features/channel-monitor/components/channel-model-detection-run-detail-sheet.tsx:82)、[channel-passive-monitor-panel.tsx:138](D:/GoProjects/new-api/web/src/features/channel-monitor/components/channel-passive-monitor-panel.tsx:138)、[api-automations.ts:76](D:/GoProjects/new-api/web/src/features/channel-monitor/api-automations.ts:76)、[token-protection-records.tsx:43](D:/GoProjects/new-api/web/src/features/channel-monitor/components/token-protection-records.tsx:43)。

因此不能概括为“渠道监控全部手动刷新”，也不能将浏览器里保留的上次结果等同于服务端 Redis 缓存。

一次手动刷新会并行刷新多个当前活动的查询组，见 [query-options.ts:113](D:/GoProjects/new-api/web/src/features/channel-monitor/lib/query-options.ts:113)。包括总览、并发、限流组、性能、今日成功率、调度及分析，已删除两日成本摘要；今日利润属于活动的分析查询，所以普通页面刷新仍会执行它的数据库计算。状态和检测查询按当前页面追加，并非每次在所有页都执行。

前端已合并同一范围内尚未完成的刷新点击，但上一批完成后再次点击会重新请求。只调整 `staleTime` 也不能阻止手动 `refetchQueries`。按最新原则，后端补齐 Stream 增量键、共享资料和覆盖状态后，刷新读取最新已处理的 Redis 统计，允许继续频繁请求；不依靠页面响应缓存或长时间按钮节流减少 SQL。

对最初的“明细覆盖不完整”提示，应分别判断：当前模型确实有待投影成本；队列、心跳或缺口无法确认；或者前端还保留上次查询状态。后台追平后，主体视图需要再次查询才会移除旧提示。前一次修复已按当前日期、渠道、模型、用户、Key 和搜索条件判断，无法识别或读取失败仍会保守提示。

## 6. 已有 Redis 读模型与数据库正常职责

### 分组共享快照

[channel_group_monitor_snapshot.go:41](D:/GoProjects/new-api/controller/channel_group_monitor_snapshot.go:41) 在后台读取数据库配置及路由候选，结合 Redis 投影构建并发布共享快照。正常 settings/overview/public GET 只使用 [ReadChannelGroupMonitorSnapshot](D:/GoProjects/new-api/service/channel_group_monitor_projection.go:459)。

后台查库符合需求；正常 GET 已没有统计 SQL，本次可以保留。快照缺失后的恢复属于 R05 的可用性范围，不影响本次优化判断。不能把后台 SQL 算成每次 GET SQL，也不能把“后台可重建”说成“GET 已有数据库兜底”。

### 智能调度共享读模型

[channel_smart_schedule_monitor_snapshot.go:141](D:/GoProjects/new-api/model/channel_smart_schedule_monitor_snapshot.go:141) 的使用条件是 **`RedisEnabled && MemoryCacheEnabled`**。

条件满足时，后台加载 Redis 指定修订版本的监控读模型，验证版本、水位及生成时间一致；请求读取同版本的本机镜像，并检查是否过期。[channel_smart_schedule_route.go:459](D:/GoProjects/new-api/model/channel_smart_schedule_route.go:459) 的 routes/summaries 均优先使用该模型。

这属于 Redis 发布快照的缓存读取，即使 GET 不再次访问 Redis，也不是每次查数据库。关闭任一开关会使用数据库路由查询分支。启用共享读模型但镜像不存在或过期时，会返回快照不可用；不能把开关分支理解为 Redis 出错后的自动 SQL 兜底。

`/schedule` 本身仍先查数据库监控设置，见 [channel_ratio_monitor_schedule_route_api.go:74](D:/GoProjects/new-api/controller/channel_ratio_monitor_schedule_route_api.go:74)。实时路由健康样本读取 Redis 窗口；路由及经济资料已基本具备同版本共享展示的基础。

### 可靠记账、持久化和恢复

以下数据库使用应该保留：

- 成本可靠 outbox、成本账本、收入记录、退款和补扣的权威持久写入。
- 消费统计后台落库及日统计历史。
- 后台扫描 pending、重试、对账和发布 Redis 投影。
- 冷启动或 Redis 日快照缺失后的数据库恢复与 stream 续接。
- 保存配置及手动操作时的数据库修订校验。

日成功率恢复例子见 [channel_monitor_daily_persistence.go:38](D:/GoProjects/new-api/service/channel_monitor_daily_persistence.go:38)：已有 Redis 有效版本时直接返回；需要重建时才读取数据库 checkpoint 和 ledger 并重新发布。

页面健康字段的 outbox 数量等可来自后台维护的本机统计，不能因为字段名有 `outbox` 就认定每次 GET 查库。对应状态读取见 [channel_monitor_redis_realtime_status_query.go:16](D:/GoProjects/new-api/service/channel_monitor_redis_realtime_status_query.go:16) 和 [channel_monitor_health_worker.go:24](D:/GoProjects/new-api/service/channel_monitor_health_worker.go:24)。这与 R04 每请求执行 outbox SQL 是两条不同路径。

### Redis 角色和 HTTP 缓存

统计通常使用 `RedisMonitorReadClient`；成本待处理队列使用写角色以避免队列状态的副本滞后；stream 消费使用消费角色。限流组等部分运行计数使用既有 `common.RDB`。连接池角色降级到同一 Redis 池不等于数据库兜底。

诊断读取使用写角色 Lua，因为读取会更新时间和日计数状态，见 [channel_monitor_diagnostics.go:75](D:/GoProjects/new-api/service/channel_monitor_diagnostics.go:75)。

路由上的 [DisableCache](D:/GoProjects/new-api/middleware/disable-cache.go:5) 设置 HTTP `Cache-Control: no-store` 等响应头，约束浏览器和中间缓存，**没有关闭后端 Redis 统计或共享读模型**。`singleflight` 也只是合并同一时间正在执行的请求，不能代替保存结果的缓存。

## 7. 现有文档口径

[profit.md:3](D:/GoProjects/new-api/docs/downstream/channel-monitor/profit.md:3) 已明确写明顶部今日成本、扣费、利润及成本分类使用“同一次数据库核对结果”。这与当前实现一致。后续新增 Redis 收入投影时，应保留同口径核对及确认语义，并将文档更新为实际实现；当前不能宣称利润已改为 Redis 主读。

[analytics.md:48](D:/GoProjects/new-api/docs/downstream/channel-monitor/analytics.md:48) 写明成本覆盖按范围判断以及后台追平后需要刷新，和当前修复一致。它没有承诺该判断整条链路不查数据库。

[profit.md:19](D:/GoProjects/new-api/docs/downstream/channel-monitor/profit.md:19) 的钱包及令牌额度“缓存优先”描述属于扣费读取，与监控利润报表的数据库汇总不是同一个数据路径，不能据此推断利润界面已 Redis 主读。

后续实现完成时应同步更新已有下游文档，明确事件及统计范围、处理位置、持久化与重建方式、延迟和缺口状态，以及保留上次结果时的展示行为。

## 8. 最小优化清单

### 优先改普通刷新路径

1. **补齐今日收入和利润投影。** 保留权威结算、退款、补扣与成本事实，用可靠事件后台增量更新独立收入键，复用成本投影；实现同范围确认、数据库初始化和增量续接，再将总览及渠道行正常读取切到 Redis。
2. **共同配置和资料复用。** 复用已有配置副本与共享快照，补齐缺少的展示字段、变更发布及缺失初始化；优先覆盖总览、并发、性能、成功率及调度共同使用的渠道、监控行和设置。
3. **取消昨日额外查询，已完成。** 默认总览不展示昨日对比，也不挂载或刷新对应两日成本摘要。独立历史分析按显式选择继续读取。

### 按使用页面追加

4. **今日成本分析覆盖检查。** 后台维护范围 pending 和覆盖索引，与金额处理位置对应；可靠交接覆盖完整后，页面读 Redis 并取消重复 outbox SQL 扫描。
5. **探测和模型检测状态发布。** 配置、任务进度和完成后发布增量状态或同版本快照；页面读 Redis，缺失时从数据库状态初始化，历史审计继续查库。

### 每项同时完成恢复和可靠性

6. **复用并补齐重建。** 接通页面缺失检测、跨节点单次初始化、基线和增量衔接、原子发布及恢复状态，验证不会漏算、重算或覆盖新事件。
7. **明确可恢复范围。** 检查普通监控内存队列丢样本、Stream 保留、被动历史及配置版本的持久化缺口；需要完整恢复的数据提供可靠事实和处理位置，无法补齐时保持缺口提示。

### 暂不纳入本次

- 将每个接口数据库响应按 1/5/30 秒 TTL 包装为缓存，或引入第二套通用响应缓存。
- 取消数据库扣费、退款及收入和成本可靠事实写入，或用可丢失观察事件替代资金交接。
- 因形式统一而合并现有 Stream、重写已符合原则的分钟统计、分组及调度快照。
- 取消独立历史详情、配置保存、上游测试等合理的低频数据库访问。

后续实施先记录固定刷新流程的 SQL 次数及用户请求延迟，再验证减少量和异步处理开销。本文没有性能测量，不承诺具体百分比。工作量以完善已有事件链路和数据库恢复为准，不能通过删检查或放宽数据对应要求缩减。

## 9. 后续实施验收项

以下是按最新原则的验收标准。默认总览昨日摘要已删除并完成前端刷新回归验证；新增投影和恢复改造尚未实施，以下对应测试尚未执行。

| 场景 | 需要验证的结果 |
| --- | --- |
| 固定流程连续手动刷新 | 与改造前比较 SQL 次数及耗时；正常新接入统计读 Redis，不因点击重新汇总数据库；新增事件能增量可见 |
| 用户请求速度 | 比较普通、流式及提交失败/队列满场景的延迟；无请求内聚合或重建，可靠交接有界，不丢弃必要资金事实 |
| 事件重复、乱序及修正 | 同事件重放不重复计数；退款、补扣、任务成本修正按版本替换，日期和原始归属正确 |
| 数据库与 Redis 对应 | 同一事实范围及已处理位置下金额、计数和维度相等；处理滞后和缺口明确，不比较不同进度然后误报完整 |
| 并发和多节点恢复 | 缺失统计只初始化一次；重建期间接收的新事件能续接；旧 worker 或过期租约不能覆盖新代次 |
| 今日收入和利润投影 | 收入、成本、扣费分类及确认状态同口径；跨日、退款、缺失恢复后与数据库对账一致 |
| 默认总览昨日查询，已取消 | 卡片仅展示今日，挂载和刷新不请求两日成本摘要；回归测试验证摘要不在刷新范围内 |
| 成本覆盖及队列交接 | Redis 待处理索引与金额进度一致；当前模型 pending 正确，其他模型不误报；未入队 outbox、未知归属及错误都被覆盖 |
| 配置变更及运行变化 | 保存和任务进度事件更新同版本快照；缺失可初始化，操作校验仍查询权威状态 |
| 活动任务轮询 | 约 1 秒请求读已更新 Redis 状态；兼容性 HTTP 不随每个 GET 执行 |
| 数据丢失及断点 | 删除投影键、重启、消费未确认、Stream 截断等场景恢复安全；无法恢复的范围明确不完整，不使用空值或旧状态冒充当前数据 |
| 涉及新增数据库行为 | 真实 SQLite、MySQL、PostgreSQL 三库验证；有迁移时覆盖升级和重复启动 |

## 10. 检查方法及限制

- 根据路由逐项追踪 `controller -> service/model -> Redis/数据库/本机状态/外部服务`，并反查前端的实际使用及刷新配置。
- 已核对两份路由文件的 GET 数量为 37；公共分组展示及分组选项单独列出。
- 未将仅剩定义或测试调用的旧数据库性能/成功率辅助函数认定为现行接口的数据来源。
- 本轮为静态源码检查，没有接入线上 Redis/数据库，没有采集线上 SQL、请求耗时或生产故障样本；因此不提供推测的性能数字。
- 最新事件增量和数据库恢复方案仅根据现有源码及用户原则纠正文档，未改生产代码，未为新增投影执行运行验证。此前的短期数据库响应缓存建议已撤回。
- 删除昨日对比仅修改前端查询、展示和刷新目标，数据库接口及历史记录未改。验证已通过：`bun run test src/features/channel-monitor/lib/__tests__/query-options.test.ts src/features/channel-monitor/components/__tests__/profit.test.tsx src/features/channel-monitor/components/__tests__/monitor-stat-card.test.tsx src/features/channel-monitor/components/__tests__/realtime-status.test.tsx`（4 个文件、64 项测试）、`bun run typecheck`、`bun run build`、修改文件的定向 oxlint 和 oxfmt 检查，以及 `git diff --check`。全量 lint 仍因其他文件的现有错误失败；未重跑数据库矩阵。其他缓存优化仍未实施，此前成本覆盖修复的验证不能替代后续缓存优化的验收。
- 工作区中存在其他并行修改，尤其智能调度代码。本文记录检查时的行为，后续代码变更应重新核对相关条目。
