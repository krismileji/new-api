# 渠道监控利润功能系统复查记录

> 日期：2026-10-01。状态：9 项已确认问题均已修复并完成本台账范围内复核；F-06 按用户选择的“保留预扣待人工核对”关闭。
> 对象：当前工作区，包含此前尚未提交的利润修复。HEAD：`f4baf15c475dee5a60848e8f446d46ebab6b4739`。
> 下文保留首次系统复查的故障证据；当前修复状态以本节及第 15 节为准，第 7 至 14 节保留分阶段验证记录。没有修改生产账目、提交或部署。
> **最新进展：** F-06 的原子预扣、补充预扣、最终结算、持久退款及人工核对已通过三库和中断回归；最新 service 整包 68.636s 通过，最终构建通过。四包完整测试及后续复跑证据见第 15 节。前端本阶段未变化，沿用全套 773 项通过证据。此前失败和未验证边界保留，不作覆盖。

| 编号 | 当前状态 | 本次验证 |
|---|---|---|
| F-01 | 已实现，三库针对性回归通过 | 任务插入、初始差额与收入确认共同提交；任一失败不留下目标额度任务 |
| F-02 | 已修复，源码及整包回归复核通过 | 坏文件和不可创建目录不再使收入初始化失败；查询仍拒绝确认利润 |
| F-03 | 已修复，源码及整包回归复核通过 | 10001 个过期标记可清理；混入的坏标记和有效标记保留，数据库收入清理继续 |
| F-04 | 已实现，三库针对性回归通过 | 过期任务退款、补扣及重复回调成功；未过期成本意外缺失仍回滚资金 |
| F-05 | 已实现，三库与查询回归通过 | 缺口关联成本事件，随最终成本日期归属；未结束时保守覆盖后续日期 |
| F-06 | 已修复并复核，按方案 1 关闭 | 钱包/订阅/令牌预扣与 reserved 共同提交；三库、进程退出及服务层整包通过；中断不自动退款、不确认收入 |
| F-07 | 已实现，三库针对性回归通过 | 初始扣费及退款分别原子提交；失败终态且有余额的任务继续重试 |
| F-08 | 已实现，组件回归通过 | 首次失败及保留旧结果时均提示并可重试；12 个组件测试通过 |
| F-09 | 复核新增并修复，三库通过 | 累计钱包/令牌额度不再使用单笔 int32 上限 |

## 1. 结论与此前遗漏原因

首次系统复查记录 **8 项问题：5 项 P1、3 项 P2**，后续新增 F-09（P1），共 **9 项：6 项 P1、3 项 P2**。9 项均已有修复实现；F-06 的最终证据与验收状态见第 15 节。首次故障测试中的 F-01 需按下文更正理解，不能将组件夹具直接当成控制器完整入口的证据。另列出 5 项待验证风险或部署边界，不混入问题数。

最需优先处理的后果包括：初始差额返还失败却被当作已结算、退款不足、监控文件异常阻止整个网关启动、历史清理阻断退款、跨日收入缺口归属错误，以及 Midjourney 退款失败后失去重试机会。原“正常提交入口多退款”的表述已按 F-01 的重新验证更正。

前几轮复查不够系统，不能把反复发现问题归结为正常现象。具体遗漏是：

1. 验证了“退款早于收入创建”，却没有组合“初始资金写入失败”和后续结算/退款。任务目标额度被误当成资金已提交事实。
2. 验证了缺口文件能恢复，却遗漏坏文件、数量上限、过期清理与整个服务启动之间的相互影响。F-02、F-03 是前轮新实现引入的回归，应明确承认。
3. 检查正常结算和正常清理，却没验证清理后任务才完成；检查同日缺口，却没验证跨日收入归属迁移。
4. 检查收入状态机时，没有同时检查轮询状态机。任务终态不等于资金处理终态。
5. 把已有测试通过当成过强的完成依据。本轮现有后端相关回归、前端 771 个测试均通过，但新故障复现仍证明当前实现有问题。

因此，此前“修复完成”只能对应当时列出的局部分支，不能代表整个利润功能已经可靠。后续应按本台账逐项补充修复和验收证据，而不是再输出零散问题清单。

## 2. 审查范围与矩阵

“完整复查”指对下列明确边界逐项检查、记录结果和未验证部分，不是证明不存在未知缺陷。本轮不是整个网关、所有供应商协议、支付系统或认证系统的全面审计；没有读取生产流水估算受影响用户和金额。

审查口径：实际扣费额度按请求保存的每单位额度折算为人民币，平台按 1:1 记账，再减渠道已记录成本。订阅为额度消耗的名义金额，不是订阅销售回款。确认利润必须同时满足收入、成本、历史覆盖与队列完整。

| 环节 | 核对内容 | 结果/边界 |
|---|---|---|
| 普通请求 | reserved → 最终资金与确认事务；预扣、补扣、返还；钱包/订阅 | 原子预扣、最终事务、提交确认丢失及人工核对三库通过；第 15 节记录中断政策 |
| 金额口径 | 1:1、请求快照、非法金额、纳币整数、利润率分母 | 未发现新增确定问题；规模边界见 R-05 |
| 异步任务 | 插入、初始扣费、早到结算/退款、重复回调、成本登记、提交日 | F-01、F-04 修复及早到校正回归通过；持续断连后的提交结果仍需核对 |
| Midjourney | 提前退款占位、初始确认与退款交错、退款失败、轮询退出 | 占位修复有效；F-07；其他交错见 R-04 |
| 实时会话 | 分次扣费、终结扣费、共同成本事件、跨日迁移 | F-05 模型三库及查询回归通过；未运行真实 WebSocket 跨午夜 |
| 违规扣费 | 独立结算键、按资金提交确认、请求成本关联 | 静态核对；确认失败同 F-06，未模拟真实供应商响应 |
| 成本链路 | 业务、失败尝试、探测、模型检测、任务成本修正 | 成本仅有行不会被 UNION 丢弃；清理见 F-04 |
| Redis/DB outbox | 发布降级、持久化后 ACK、幂等投影、积压/死信归属 | 既有队列回归；灾难恢复见 R-03 |
| 缺口持久化 | 内存、数据库、文件、重放、多节点、异常文件与上限 | F-02、F-03、F-05；共享目录前提见 R-01 |
| 历史保留 | retained_from 先推进、收入/成本删除、任务依赖 | 利润过期保护存在；F-03、F-04 |
| SQL 汇总 | UNION ALL、范围汇总、分页、排序、仅亏损 | 汇总先于分页；仅亏损要求该行已确认；回归通过 |
| 覆盖状态 | 启用日、旧标记迁移、逐日确认、渠道隔离、未知队列 | 既有修复与 F-05 查询回归通过；旧无关联缺口不能凭空补出请求归属 |
| 筛选与下钻 | 日期、渠道、用户、Key、模型、搜索、下钻上下文 | 参数化筛选、排序白名单、90 天限制；相关回归通过 |
| 页面 | 同快照、占位、错误提示、趋势、隐私、超过 200 渠道补查 | 总览及分析弹窗错误提示有效；F-08 |
| 启动/升级 | 主从初始化、1:1 迁移、独立日志库、重复启动 | 引用前轮三库证据；新故障见 F-02/F-03 |

主要核对文件：

- 收入：`model/channel_monitor_income.go`、其 gap/journal/parity/concurrency 文件与测试、`service/channel_monitor_income.go`。
- 资金与任务：`service/billing.go`、`service/quota.go`、`service/violation_fee.go`、`service/task_billing.go`、`model/task_billing.go`、`model/user.go`、`controller/relay.go`，及 service/controller/model 的 Midjourney 实现。
- 成本与保留：`service/channel_daily_cost.go`、`service/channel_daily_cost_outbox.go`、`service/channel_monitor_profit_queue.go`、`model/channel_task_cost_event.go`、`model/channel_daily_cost_projection.go`、`model/channel_monitor_cost_retention.go`、`controller/channel_monitor_cost_retention.go`。
- 查询：`controller/channel_monitor_profit.go`、`controller/channel_monitor_profit_coverage.go`、`controller/channel_monitor_analytics.go`、其 filter/coverage 文件及测试。
- 前端：`web/src/features/channel-monitor/` 的 index、types-analytics、profit/analytics hooks，profit/profit-status/profit-trend/analytics-dialog/analytics-table/channel-view 组件，日期/格式化/下钻/刷新工具及测试。
- 初始化与既有证据：`model/main.go`、`main.go`、`scripts/channel-monitor-profit-upgrade/upgrade_test.go`、已有利润文档及前两轮验证记录。

## 3. 问题台账与原始复现

P1 表示可能影响真实资金、错误确认利润或服务可用性；P2 表示影响恢复、清理或结果可信展示。此节描述修复前行为，当前状态见文件开头。

| 编号 | 级别 | 问题 | 证据 |
|---|---|---|---|
| F-01 | P1 | 初始差额返还失败，收入少记且后续退款不足 | 下调失败的精确余额/收入断言；原上调夹具范围已更正 |
| F-02 | P1 | 缺口文件异常使整个网关不能启动 | 初始化实测 + 启动调用链；前轮新增回归 |
| F-03 | P2 | 超过 10000 个日志后，过期清理也无法运行 | 10001 个过期文件实际复现；前轮新增回归 |
| F-04 | P1 | 清理删除活跃任务成本依赖，退款事务回滚 | 正常登记 → 真实清理函数 → 退款实测 |
| F-05 | P1 | 收入跨日迁移，缺口留在旧日 | 插入失败/实际扣费 + 相邻日期投影实测 |
| F-06 | P2 | 资金成功但收入确认失败，无可靠恢复闭环 | 确认失败实测、重新初始化仍 pending、调用点检索 |
| F-07 | P1 | Midjourney 先保存终态，退款失败后不再轮询 | CAS → 退款失败 → 后续查询排除任务实测 |
| F-08 | P2 | 渠道行独立补查失败，静默保留旧已确认利润 | 前端分支及调用链；未做新 UI 复现 |
| F-09 | P1 | 累计钱包/令牌额度误用单笔 int32 上限，合法扣费或退款失败 | 超过 int32 的余额在三库执行扣费与退款回归，详见第 9 节 |

### F-01：初始差额返还失败后的错误确认和退款不足

**证据更正（后续复核）：** 原夹具直接写入目标额度并调用结算，绕过了 `controller/relay.go` 插入前的 `Billing.Reserve(result.Quota)`。正常任务提交中，上调预留失败会在插入前返回，因此下面的“预扣 100、存 150、补扣失败”不能作为该入口已发生多退款的证明。实际仍存在相反方向的窗口：预扣 150、目标 100，任务存入 100 后返还 50 失败；后续回调读取 100 作为已付金额，可能少退 50。此前将原组件复现推广到正常入口是不准确的。本条须以真实入口和下调失败用例重新验收。

**位置：** [任务先持久化](D:/GoProjects/new-api/controller/relay.go:1231)、[按任务额度计算差额](D:/GoProjects/new-api/model/task_billing.go:200)、[收入无条件改 settled](D:/GoProjects/new-api/model/channel_monitor_income.go:382)。

**触发：** 初始余额 10000，预扣 150。目标额度下调为 100，控制器无需额外预留，任务先保存 `Quota=100`。随后返还 50 的钱包写入失败。任务已 durable，HTTP 流程不会按未持久化任务撤销。后续仍以 100 为已扣基准。

**重新复现：** 再次结算为 100，差额为零，跳过资金变更，却把收入 100 标为 settled。实际钱包扣了 150。随后退款按 100 退还，余额变为 **9950**，比初始余额少 50。

此复现未补齐成本，因此不声称“通过 HTTP API 已观察到整行 profit_confirmed=true”；已经证明的是收入金额/状态错误和退款不足。它使用控制器可达的下调顺序，但仍是服务与模型的故障注入测试，未冒充完整供应商 HTTP 端到端调用。

**根因：** `Task.Quota` 同时被用作目标额度、已扣金额和幂等基准，但初始扣费与任务持久化没有共同的资金提交事实。

**修复方向：** 持久化实际已提交资金状态/金额，据此计算后续差额和可退金额，收入确认依赖该事实。仅移除一次 `status=settled` 不能解决余额不一致。

**验收：** 预扣 150、目标 100、返还失败后，再次结算只返还尚未返还的 50；直接失败退款应退还实际净扣的 150。上调预留失败不得留下错误任务额度。覆盖钱包、订阅、零差额、重复回调和初始写入各失败阶段。

### F-02：辅助监控日志故障阻止整个服务启动

**位置：** [收入初始化](D:/GoProjects/new-api/model/channel_monitor_income.go:96)、[日志解析](D:/GoProjects/new-api/model/channel_monitor_income_journal.go:59)、[InitDB](D:/GoProjects/new-api/model/main.go:238)、[主程序终止启动](D:/GoProjects/new-api/main.go:375)。

**触发：** 缺口目录有一个 `broken.gap`，或目录无法创建/读取、共享挂载失效、条目超限。

**复现：** 单个坏文件使初始化返回“收入缺口日志归属无效”。错误沿 InitDB 传播到主程序 FatalLog，整个网关无法正常启动。测试实测初始化错误，未启动并终止真实服务进程；致命影响由入口调用链确认。

**根因/影响：** 将辅助监控存储作为网关启动必要条件。已有功能文档只说明异常会让利润待确认，没有说明实际会阻止启动。此为前轮缺口日志实现新增的回归。

**修复方向：** 将业务服务可用性与利润可确认状态解耦，明确降级原因，恢复后重新核对。不能简单吞错后把利润当完整。

**验收：** 坏文件、权限错误、挂载暂失、超限时业务仍能启动；利润保持未确认、原因可定位；恢复后可重放，不要求删除有效证据。

### F-03：达到日志上限后无法靠清理恢复

**位置：** [受限读取](D:/GoProjects/new-api/model/channel_monitor_income_journal.go:65)、[收入清理](D:/GoProjects/new-api/model/channel_monitor_income.go:274)、[其他清理的前置依赖](D:/GoProjects/new-api/model/channel_monitor_cost_retention.go:65)。

**触发：** 按日/渠道去重后的文件总数仍超过 10000。

**复现：** 10001 个全部过期的合法标记，清理返回“超过读取上限”，仍剩 10001 个，初始化也失败。清理使用与查询相同的受限 reader，尚未删除任何条目就退出。

**扩大影响：** 收入清理在成本和分钟指标清理之前，错误会阻止后续清理，导致其他表继续积累。F-02 是故障隔离，F-03 是清理恢复死锁，修复其中一个不能替代另一个。

**修复方向：** 清理独立分页、有预算地处理合法过期条目，坏条目可隔离但保留告警。查询仍可保守拒绝确认。

**验收：** 10001 个过期标记、过期/有效混合、坏文件混入、中断重跑均能推进；不删除有效缺口，保留边界不回退，不阻断其他表。

### F-04：清理历史成本后不能退还真实用户资金

**位置：** [按日删除任务成本事件](D:/GoProjects/new-api/model/channel_monitor_cost_retention.go:149)、[缺失事件使资金事务失败](D:/GoProjects/new-api/model/task_billing.go:293)。

**触发：** 已注册成本且 `ChannelCostResolved=true` 的任务还在进行，提交日早于成本保留边界。成本保留允许 1 天，默认 30 天；回调、退款和补扣可能晚于该边界。

**复现：** 两天前的进行中任务，额度 100、钱包 9900，正常注册成本 80。清理实际删除 1 条成本事件。随后退款返回 `resolved task cost event is missing`，钱包保持 9900，未退还 100。

用例关闭收入开关以隔离这条成本依赖；成本删除和缺失事件拒绝退款均不依赖该开关，开启利润同样受影响。

**根因：** 同一记录同时是可过期的分析历史和资金事务必要状态。报表保留期不应决定用户能否退款。

**修复方向：** 将资金结算必需状态与分析保留策略分开，或保留活跃/待结算任务依赖。不能直接忽略缺失事件后任意重建不一致历史。

**验收：** 超保留期的进行中任务可退款/补扣，重复回调不重复记账；过期报表仍标明过期；并发清理和结算不会破坏依赖。

### F-05：跨日会话的缺失收入不再阻塞结束日

**位置：** [缺口使用当前日](D:/GoProjects/new-api/service/channel_monitor_income.go:59)、[实时分次扣费](D:/GoProjects/new-api/service/quota.go:148)、[收入迁移到成本日](D:/GoProjects/new-api/model/channel_monitor_income.go:343)、[仅匹配相交日期的缺口](D:/GoProjects/new-api/controller/channel_monitor_profit_coverage.go:34)。

**触发：** 实时会话跨越北京时间午夜。D 日某次收入插入临时失败，但扣费成功，缺口只标在 D 日。后续收入正常，D+1 日成本完成后，已保存收入都移到结束日；缺失记录没有可迁移的关联。

**复现：** 按实时函数实际使用的 prepare/funding/confirm 顺序注入一次收入 INSERT 失败，两次各扣 50，实际扣 100，只存收入 50。向成本确认函数输入下一日发生时间后，收入日等于缺口的排他结束时间 `gap.To`，新日期不受缺口覆盖。

这是组件级相邻日期输入复现，未等待真实午夜，也未搭建完整 WebSocket → Redis → HTTP 查询。日期失配已经实测；“结束日可能被错误确认”由覆盖查询和缺失记录不贡献 pending count 推导，前提是其他确认条件均满足。

**影响：** 结束日收入少算，缺口却留在前一天。前轮缩小缺口影响范围未覆盖归属迁移。

**修复方向：** 缺口保存请求/成本事件关联，随最终归属同步迁移或扩展；尚不知结束日的会话保留可靠未完成标记。

**验收：** 真实跨日会话任一分次收入失败，结束日必须待确认；无关渠道仍能确认。覆盖成本先到、收入先到、会话中断及结束前重启。

### F-06：资金已扣、收入确认失败后没有恢复闭环

**位置：** [确认失败仅日志](D:/GoProjects/new-api/service/channel_monitor_income.go:66)、[初始化恢复范围](D:/GoProjects/new-api/model/channel_monitor_income.go:57)、[pending 不计收入](D:/GoProjects/new-api/controller/channel_monitor_profit.go:42)。

**触发：** pending 已写入，资金已提交，随后 confirm 更新失败或进程退出。普通请求没有任务回调为其校正。

**复现：** 只对收入 UPDATE 注入失败，SettleBilling 返回成功、资金已扣。恢复写入并重新初始化后仍 pending。检索调用点未找到普通收入 pending 的持久化重放/对账 worker；缺口文件恢复不等于资金提交事实恢复。

**影响：** 收入少计，受影响范围长期待确认。保持 pending 是正确保护，缺陷是没有安全恢复闭环；不能盲目将 pending 改 settled，否则会重现 F-01。

**修复方向：** 为资金提交和收入确认建立可靠事务事实/outbox，区分未扣、已扣待确认、退款待完成。无法自动判定时要有基于证据的人工核对路径。

**验收：** 资金提交前后、confirm 前后分别中断，恢复后收敛到实际净扣费，不重复扣费、不猜测失败交易已成功。

### F-07：Midjourney 退款失败后任务不再进入轮询

**位置：** [保存终态后退款](D:/GoProjects/new-api/controller/midjourney.go:212)、[退款失败返回](D:/GoProjects/new-api/service/midjourney.go:102)、[轮询只取未到 100% 的任务](D:/GoProjects/new-api/model/midjourney.go:96)。

**触发：** 上游失败，轮询先将任务 CAS 为 FAILURE、progress=100%，然后钱包退款发生临时失败。退款返回值没有形成持久化待退款任务。

**复现：** 初始钱包 10000，正常扣费后 9900。按轮询顺序保存终态并注入退款失败，退款返回 false，收入恢复为 100、settled。随后 GetAllUnFinishTasks 不含该任务，HasUnfinishedMidjourneyTasks 为 false，钱包仍为 9900。

此时收入与当前净扣款一致，不是收入重复记账。问题是失败任务应退的钱没有退，恢复后也不会自动再试，利润继续包含这笔未完成退款。

**修复方向：** 分离任务完成状态与退款状态，持久化可幂等重放的退款需求。状态 CAS 不能替代资金事务；不能简单让所有终态任务无限重复退款。

**验收：** 钱包暂失、保存终态后退出、退款成功后状态写入失败均能恢复；钱包/Key/用量/收入最终一致，多节点最多退一次。

### F-08：渠道行独立补查失败没有旧结果提示

**位置：** [行内利润渲染](D:/GoProjects/new-api/web/src/features/channel-monitor/components/channel-monitor-profit.tsx:107)、[超过总览页时补查](D:/GoProjects/new-api/web/src/features/channel-monitor/components/channel-monitor-channel-view.tsx:442)。

**触发：** 超过总览 200 条的渠道独立查询利润，曾成功，后续该独立请求失败，而总览请求仍成功。

**代码确认：** ChannelMonitorProfitCell 读取 query.data，没有使用 query.isError 或显示旧结果时间。TanStack Query 保留成功数据，行里仍显示旧的已确认利润。总览错误提示跟随总览请求，无法反映独立补查失败。

分析弹窗和下钻已有“更新失败，保留上次结果”提示，不属于此问题。本条没有新增 UI 实测。

**修复方向：** 独立行显示加载、失败、上次核对状态并支持重试，复用现有错误展示模式。

**验收：** 201 个以上渠道，总览成功但单行补查失败时该行明确提示；首次失败不显示零；恢复后提示消失。补充针对性组件测试。

## 4. 此前修复的复核结论

| 先前修复 | 本轮结论 | 尚不能宣称完成的边界 |
|---|---|---|
| 通用任务早到退款/补扣后收入用旧快照 | 最新任务额度校正覆盖初始资金成功路径 | F-01：任务额度不是充分资金提交证据 |
| 全局缺口污染未来/无关渠道 | 按日/渠道缺口和旧区间迁移回归通过 | F-05 跨日缺口遗漏；未知归属仍保守阻塞 |
| 一个不完整日阻塞完整日 | 逐行覆盖、逐日确认和仅亏损回归通过 | 总范围仍可能不完整，属正常语义 |
| 总览成本/利润不同快照 | 都使用 profit scope_summary | F-08 为独立补查问题 |
| Midjourney 提前退款收入复活 | 占位及 refund_unfunded/refund_pending 回归通过 | F-07；未证明所有资金崩溃交错安全 |
| 批量钱包未落库却确认收入 | Ready 后钱包直写 DB，回归通过 | Token/用量仍可批量；吞吐及已有积压未压测 |
| 缺口仅在内存，重启或其他节点不可见 | 文件正常重放/直接读取有效 | 共享持久目录前提；F-02/F-03 新回归；双存储故障受限 |
| 旧汇率使收入膨胀 | 1:1 换算与原始快照修正回归通过 | 订阅仍为名义消耗，不是回款或净利润 |

## 5. 待验证风险与部署条件

以下不计入 9 项已确认问题，不能把合理怀疑写成已发生故障。

| 编号 | 风险/条件 | 当前证据与缺少的验证 |
|---|---|---|
| R-01 | 多节点必须共享持久缺口目录 | 默认本地日志目录不能共享 DB 故障期间缺口；文档已要求共享。本轮未检查实际部署挂载及共享盘分区/权限漂移 |
| R-02 | DB 与文件系统同时不可写 | 仅剩进程内标记，节点再退出无法保证恢复；已知设计限制 |
| R-03 | 可靠 outbox 停用历史、死信裁剪、Redis 数据丢失 | 代码检查当前配置，不能证明历史一直可靠；死信有容量限制。待真实崩溃/Redis 恢复/停用再启用矩阵 |
| R-04 | Midjourney 资金及任务状态的崩溃交错 | 已改为事务共同提交，并通过三库写入故障、旧快照和重复调用回归；未做进程强制终止与持续断连实验，不能用定点失败替代所有崩溃情形 |
| R-05 | 大规模汇总金额与性能 | 单笔 int64 校验不能替代 SQL SUM、JSON 到 JS 安全整数、长区间/多渠道规模验证；本轮不声称实际已溢出 |

首次复查未验证真实浏览器、多节点并发故障、真实 WebSocket 跨午夜、共享盘宕机、真实供应商响应、生产流水对账、Linux 断电恢复、ClickHouse 独立日志库。后续已修改数据库行为并重新运行第 9 节所列三库用例；旧矩阵通过不能代替新故障验收。

## 6. 已核对、不应误报的问题

- 首次启用当天缺少启用前收入，全天待确认；刷新不会补历史。
- 订阅收入是名义消耗；本页账面毛利不等于销售回款或最终净利润。
- 收入为零时利润率为空；完整成本大于零时利润可为负。未确认金额/利润率用占位，不按零认定盈利。
- 仅亏损过滤已确认明细；顶部汇总和趋势仍是整个筛选范围，不是当前分页求和。
- pending、未解析成本、队列/缺口导致未确认是保护机制；F-06 针对恢复闭环。
- 队列超过 4096、归属不明、不可读时保守阻塞；这是安全上限，与 F-03 清理死锁不同。
- 总览虽只取 200 行，scope_summary 是 SQL 全量汇总，顶部总额不会截断；独立补查的问题见 F-08。
- 手动刷新是当前产品行为，没有每秒自动刷新本身不是缺陷。
- 分组均在允许列表；筛选参数化、排序白名单，本轮未发现新的利润筛选注入问题。此结论不是认证审计。

## 7. 验证证据

### 7.1 本轮故障复现

工具链 `D:/Go/sdk/go1.26.5/bin/go.exe`，模块声明 Go 1.25.1；未另测 1.25.1 编译器。

[复现源文件](D:/temp/profit-audit-systematic-20261001_test.go) 和 [原始日志](D:/temp/profit-audit-systematic-20261001.log) 保存在仓库外。执行时源位于 service/channel_monitor_profit_audit_temp_test.go，结束后已移除。各条输入、输出、根因及验收已写入本报告，不依赖临时目录才能理解。

```powershell
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./service -run '^TestProfitAudit' -count=1 -v
```

最终 5 个用例 PASS。**PASS 表示断言当前缺陷确实发生，不表示修复成功。**

| 用例 | 关键观察 | 问题 |
|---|---|---|
| TestProfitAuditFailedInitialFunding | 原夹具 charge=100，income=150 settled、wallet=10050；绕过正常入口上调预留，不能推广到该入口 | F-01 原始证据，已更正 |
| TestProfitAuditRetentionBlocksRefund | 删除活跃任务成本 1 条；退款报 missing；wallet=9900 | F-04 |
| TestProfitAuditJournalFailureAndCleanup | broken.gap 初始化失败；10001 个过期标记零删除 | F-02/F-03 |
| TestProfitAuditCrossDayGapAndConfirmationRecovery | charged=100，recorded=50，income day=gap.To；另一次确认失败重新初始化仍 pending | F-05/F-06 |
| TestProfitAuditMidjourneyRefundNotRetried | wallet=9900，income=100 settled，终态任务被后续轮询排除 | F-07 |

使用 SQLite 隔离测试数据、GORM 定点失败、临时目录，未连接业务 MySQL/Redis。跨日用例最初直接使用 PreWss 遇到测试夹具 Key 列名未初始化；这不是业务发现。改用实时函数实际调用的 prepare/funding/confirm 顺序后取得最终证据，没有冒充完整 WebSocket 端到端测试。

### 7.2 本轮现有回归与构建

```powershell
# 仓库根目录
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./controller ./model ./service -run 'Test(ChannelMonitorProfit|.*Income|.*Profit|ChannelMonitorAnalytics|ChannelMonitor.*Coverage|ChannelMonitorCostRetention|ChannelMonitor.*Retention|.*Midjourney|.*Wallet|.*Batch)' -count=1
& 'D:/Go/sdk/go1.26.5/bin/go.exe' build ./...
git diff --check

# web/ 目录
bun run test src/features/channel-monitor
bun run typecheck
```

结果：后端三个包相关回归通过；前端 **130 个测试文件、771 个测试通过**；根模块构建、前端类型检查、diff 空白检查通过。后端为按正则选择的回归，不是全仓库全部 Go 测试。

本轮没有实现变更，没有重复前端生产构建或 relaykit 独立构建。日志为 D:/temp/ 下的 profit-audit-backend-20261001.log、profit-audit-frontend-20261001.log、profit-audit-build-20261001.log、profit-audit-typecheck-20261001.log。

### 7.3 前轮三数据库证据（非本轮重跑）

| 实际引擎版本 | 已有验证 | 本轮使用方式 |
|---|---|---|
| SQLite 3.50.4 | 收入/缺口、统计矩阵、新库、发布版升级、重复启动 | 引用已有结果，本轮另有 SQLite 故障复现 |
| MySQL 5.7.44 | 同上，独立日志库、数据/索引/唯一约束保留 | 仅引用，未验证新增故障矩阵 |
| PostgreSQL 9.6.24 | 同上，含既有收入/成本并发回归 | 仅引用，未验证新增故障矩阵 |

发布版种子 v1.0.0-rc.41；新库与升级库均重复验证启动，包含独立日志库。前轮隔离容器已删除，没有使用本地业务数据库。

精确环境、命令、输出及记录：

- [第一轮验证](D:/temp/profit-validation-20260930/verification.md)
- [第二轮验证](D:/temp/profit-validation-20261001/verification.md)
- [三库执行脚本](D:/temp/profit-validation-20261001/validate.ps1)

先前执行入口：

```powershell
& 'D:/temp/profit-validation-20261001/validate.ps1' -Engine mysql
& 'D:/temp/profit-validation-20261001/validate.ps1' -Engine postgres
& 'D:/temp/profit-validation-20261001/validate.ps1' -Engine sqlite
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./scripts/channel-monitor-profit-upgrade -run '^TestChannelMonitorProfitUpgrade$' -count=1 -v
```

SQLite 脚本的统计矩阵路径最初不满足测试防误用命名，随后改用 cm-consistency-verification.db 手动通过；MySQL 隔离库最初 latin1 无法存中文，改 utf8mb4 后完整回归通过。上述环境修正不能隐瞒，也不能将先前三库通过当成 F-01 至 F-07 已修复的证明。

## 8. 修复顺序与关闭标准

1. 优先 F-01、F-04、F-07：修复资金事实、保留依赖和退款恢复。区分目标额度、资金已提交、成本已投影、退款已完成。
2. 然后 F-02、F-03：监控异常不阻止服务启动，也不使清理永远不能推进。
3. 再补 F-05、F-06：以请求/成本事件关联和可靠资金事实驱动归属及重放，缺口只在有证据后解除。
4. 完成 F-08：覆盖超过 200 渠道的独立补查时效提示。

关闭每条问题时记录：修改位置、原复现变为正确行为的结果、钱包/订阅/Key/任务/收入/成本核对、故障恢复验证及未覆盖边界。数据库行为修改须重跑真实 SQLite、MySQL、PostgreSQL；迁移须验证新库、发布版升级、独立日志库、重复启动。构建通过或单日正常请求不能代替这些验收。

首次审计仅新增本报告；后续按用户的逐项修复要求修改代码。现有未提交改动均保留，尚未提交或部署。

## 9. 修复进展与新增验证

### F-02 / F-03：故障降级与日志清理

`model/channel_monitor_income.go` 不再将缺口目录创建/恢复失败传播成网关启动失败；记录错误，保留数据库收入写入。查询仍直接读取缺口目录，目录异常时不会确认利润。日志清理改用分批遍历，不依赖查询的 10000 条上限；坏文件保留为故障证据，不阻断数据库清理。后续复核还发现文件遍历可能耗尽整个收入清理预算，已按文件、数据库缺口、收入分别预留时间，并在每个文件处检查取消和预算。

`TestChannelMonitorIncomeJournalFaultDoesNotBlockStartupOrRetention` 覆盖坏文件、10001 个过期标记与有效标记混合、反复初始化/清理、目录父级实际为普通文件。SQLite 通过。未把“普通文件阻止建目录”冒充操作系统 ACL 或共享盘宕机实验。

### F-04：资金调整不再依赖已过期的报表历史

`model/task_billing.go` 根据清理先持久化的 `retained_from` 判断任务提交日。已过期的任务只修正资金、额度及任务状态，不重建已过期收入/成本；未过期事件缺失仍整体回滚。清理并发导致读取旧状态时，沿用现有有限事务重试。

`TestChannelMonitorIncomeRetentionAllowsLateTaskBilling` 通过真实成本登记、清理再分别退款/补扣，核对钱包、订阅、Key、用户及渠道用量、任务额度、重复回调与不重建历史。SQLite **3.50.4**、MySQL **5.7.44**、PostgreSQL **9.6.24** 全部通过。无新增列或迁移；并发清理交错尚未逐个实测。

本次隔离容器 `codex-profit-fix-mysql-20261001`（13318）与 `codex-profit-fix-postgres-20261001`（15438），仅使用专用 `new_api_cost_backlog_test` 数据库，未连接业务库。设置 `TEST_COST_BACKLOG_MYSQL_DSN` / `TEST_COST_BACKLOG_POSTGRES_DSN` 后实际执行：

```powershell
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./model -run '^TestChannelMonitorIncome(Retention|Preparation|JournalFault)' -count=1 -v
```

结果全部通过，包含 PostgreSQL 收入与成本并发准备回归。首次新增测试因两条用户夹具共用空 `aff_code` 违反唯一约束失败；修正夹具为独立值后通过，未更改业务约束。

### F-08：独立渠道行查询错误提示

`ChannelMonitorProfitCell` 在独立查询失败时显示“刷新失败，显示上次核对结果”或“利润加载失败”，提供重试按钮。保留原金额及分析入口，不将首次失败显示为零。复用现有 `Button` 及总览行内状态模式；共享 `ErrorState` 会替换内容，无法直接保留原金额/导航布局，因此未引入新的通用错误组件。

在原组件测试文件增加首次失败和旧结果刷新失败两种真实 QueryClient 场景；先观察缺少状态提示的失败，再实现修复。`bun run test src/features/channel-monitor/components/__tests__/profit.test.tsx`：12 项通过；`bun run typecheck` 及两处修改文件的 `oxlint` 通过。未做浏览器验收。

### 本次汇总回归与证据更正

以下命令实际执行通过：

```powershell
# 根目录；按正则选择的相关后端回归，并非全部 Go 测试
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./controller ./model ./service -run 'Test(ChannelMonitorProfit|.*Income|.*Profit|ChannelMonitorAnalytics|ChannelMonitor.*Coverage|ChannelMonitorCostRetention|ChannelMonitor.*Retention|.*Midjourney|.*Wallet|.*Batch|.*TaskBilling)' -count=1
& 'D:/Go/sdk/go1.26.5/bin/go.exe' build ./...
git diff --check

# web/；130 个文件、773 项测试通过
bun run test src/features/channel-monitor
bun run typecheck
bun run build
bunx oxlint -c .oxlintrc.json src/features/channel-monitor/components/channel-monitor-profit.tsx src/features/channel-monitor/components/__tests__/profit.test.tsx
```

F-01 另使用 `go test ./service -run '^TestProfitAuditFailedInitialReturn$' -count=1 -v` 重新故障注入；日志实测 `funding charge=150; income=100; status=settled`、`wallet=9950`。**该用例通过表示缺陷仍存在，不表示修复。** [源文件](D:/temp/profit-audit-initial-return-20261001_test.go) 与 [日志](D:/temp/profit-audit-initial-return-20261001.log) 保存在仓库外，临时测试源已移除。

本次增加的 F-04 修复位于 downstream-owned 的 `model/task_billing.go`，未新增上游文件修改。整个工作区仍包含此前已修改的 upstream-owned `model/main.go`（迁移注册）、`model/user.go`（钱包及时落库）、`service/task_billing.go`（任务收入接入）；它们不能算作本次新增改动，也尚未提交。

### 当时尚未关闭（后续状态见第 10 节）

本阶段结束时 F-05、F-06 仍需代码修复；后续进展记录在第 10 节。所有已实现项目需在剩余修复后统一复查。本文不宣称所有问题已修复，也不以先前通过的测试替代未做的验收。

### F-01：初始任务差额与插入原子提交

新增 `model.InsertTaskWithBilling`，复用任务钱包/订阅与令牌事务调整方法。`service.PersistTaskWithBilling` 接入真实 BillingSession，控制器只替换任务持久化调用。插入、差额及已有收入确认在同一事务内完成；失败回滚任务，原预扣仍由会话负责返还。成功后会话不再执行第二次差额调整。没有增加数据库列或新的持久化账本。

提交响应丢失时查询已持久化任务，存在则按已提交处理；若数据库仍不可读，明确保留预扣并记录“提交结果未知”，不猜测回滚后再退款。该持续断连情形仍需基于任务与资金证据核对，不能宣称自动恢复已覆盖。

`TestChannelMonitorIncomeInitialTaskFundingIsAtomic` 在 SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 9.6.24 验证钱包/订阅、150→100 与 150→0、任务/钱包/订阅/令牌/收入写入失败整体回滚、COMMIT 确认丢失、重复退款。现有 `TestTaskIncomeReconcilesCompletionBeforeInitialSettlement` 改为调用真实持久化入口，提前退款或补扣后不会重复扣费，收入按最新任务校正。

### F-07 / R-04：Midjourney 扣费、退款及恢复

准备扣费只在请求内保存目标，持久化任务额度初始为 0；资金、令牌、用量、收入和已扣标记共同提交。退款同样原子提交，任务额度归零是重试幂等依据。失败终态且仍有已扣额度的任务继续由现有轮询处理，无需再次请求上游；查询回写排除资金字段，防止旧快照恢复已退款额度。空任务 ID 或渠道错误被标记失败后也可在下次轮询进入退款。

`TestChannelMonitorIncomeMidjourneyRefundRecovery` 在上述三库验证五个写入阶段失败、轮询查询保留失败任务、重复初始扣费/退款、旧快照回写以及钱包/Key/用户用量/渠道用量/收入一致性。另覆盖扣费后删除渠道，退款仍返还用户资金，不再依赖已删除的渠道用量行；轮询收到失败原因但状态未规范时统一保存 FAILURE，保证重试可检索。原服务层“令牌失败但钱包已扣”的测试调整为事务全部回滚，不再把部分成功作为正确行为。初始资金失败不会生成多退款；初始退款早到的任务不再随后补扣。

### F-09：累计余额误用单笔额度上限

复查新复用的 `addTaskBillingQuota` 时确认它对钱包余额、Key 余额及累计用量也使用 int32 上限。即使单笔仅扣 100，余额超过 2^31−1 也会被当作溢出，导致原异步任务退款/结算失败，并影响本次新增调用。

已将累计值检查改为钱包允许的安全整数范围并检查当前平台 int 范围；单笔入口仍限制 `common.MaxQuota`，没有放宽单次费用。上述 Midjourney 三库测试用超过 int32 的余额验证扣费与退款。最终复核仍须检查相关任务回归是否出现范围语义差异。

本阶段实际执行：

```powershell
# 已设置同上三库 DSN
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./model -run '^TestChannelMonitorIncome(Midjourney|Initial|Retention)' -count=1 -v
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./controller ./model ./service ./relay -run 'Test(.*Income|.*Midjourney|.*Task.*Billing|.*Task.*Quota|ExecuteTask|.*Plugin.*Protocol|.*Plugin.*Submission)' -count=1
```

三库用例及四包选择性回归通过。上游文件最小必要改动：`controller/relay.go` 接入原子持久化；`service/task_billing.go` 增加会话交接；`model/midjourney.go` 将请求目标排除出持久化并保留退款待处理任务、防旧快照覆盖；`controller/midjourney.go` 接入终态退款重试；`service/midjourney.go` 接入事务；`relay/mjproxy_handler.go` 移除已纳入事务的重复用量增加。新增模型文件为下游事务实现，不修改插件合同。

## 10. 最新复查：跨日归属、确认事务及未关闭项

本节是对当前工作区的最新核对，不覆盖掉前面的原始失败证据。没有发现足以单列 F-10 的新确定问题；F-06 的剩余部分仍属于同一个恢复闭环缺陷，不能换一个名称后声称原问题已经关闭。

### F-05 当前实现与验收

缺口使用成本事件的哈希作为稳定标识，文件名同时保存事件关联。结束日尚未知时，该渠道从缺口发生日起保持待确认；成本投影和缺口日期在同一事务内更新为最终成本日。成本先完成、缺口后恢复的顺序通过读取并锁定 outbox 补齐日期。数据库缺口保留最终归属，正常删除过期 outbox 不会使日期信息丢失。清理未完成时保留事件关联的数据库缺口，避免文件残留却提前删除其最终日期。

实际验证：

- `TestChannelMonitorIncomeGapFollowsCrossDayCost`：真实成本登记、领取及投影，覆盖成本先到/缺口先到、数据库不可用时文件落地、清空内存后恢复、outbox 删除及到期清理；SQLite、MySQL、PostgreSQL 均通过。
- `TestChannelMonitorProfitAnalyticsCombinesIncomeAndAllCostSources/cross_day_income_gap`：成本日为 D+1 时，该日相关渠道不能确认，无关渠道及 D+2 可确认。直接调用真实统计查询，不是 HTTP 或 WebSocket 端到端验证。
- 查询用例第一次失败是测试没有开启 `ChannelMonitorIncomeReady`，导致成本确认函数按设计直接返回。已在用例中显式设置并恢复该开关，再次通过；没有为通过测试修改生产覆盖判断。

仍有边界：旧版本按日保存、没有事件关联的缺口不能自动推断到具体跨日请求；事件始终未结束或关联证据丢失时，该渠道保持保守待确认。没有真实跨午夜会话、进程强杀或多节点共享盘故障实验，因此不宣称这些端到端验收已完成。

### F-06 当前实现与剩余缺口

新增 `SettleChannelMonitorIncomeFunding` 将最终钱包/订阅差额、令牌差额及收入 `pending → settled` 放进同一数据库事务。普通请求、实时分次扣费与违规扣费已接入；零差额也必须提交收入确认。COMMIT 返回错误后读取已提交状态；若连读取也失败，则保留“提交结果未知”，不把它自动视为扣费失败并退款。

`TestChannelMonitorIncomeFundingConfirmationIsAtomic` 在三库覆盖钱包/订阅的差额 −50、0、+50，确认更新失败时差额全部回滚，COMMIT 确认丢失后可读到已提交事实，重复调用不重复扣费。**这证明最终事务的原子性，不能证明整个请求生命周期可恢复。**

以下仍未解决，F-06 继续保持未关闭：

1. **预扣发生在最终事务之前。** 用例中预扣 100 后余额为 9900；确认故障时最终事务回滚，余额仍为 9900，收入仍 pending。后续显式重试可以成功，但进程退出后没有持久化的普通请求恢复任务保存完整预扣事实并驱动重试。
2. **旧 pending 不能自动确认。** 初始化恢复缺口和汇率，不会核对普通请求的实际净扣。现有 pending 行不能独立区分未扣、只预扣、已最终扣费和已退款；成本、目标额度或消费日志都不能替代资金提交证据。
3. **持续断连不等于已验证恢复。** 当前提交确认丢失测试在提交后可以立即重新查询；没有覆盖提交后数据库一直不可读直至进程退出，以及重启后的自动收敛。
4. **prepare 失败仍保留旧扣费回退。** 有关联的持久缺口会阻止错误确认，但不会自行补出丢失收入。这是保守保护，不能称为收入恢复。

后续应先补足预扣/最终提交/退款之间的可核对事实及恢复入口，沿用现有计费结构，避免引入第二套平行账本。验收必须同时检查资金净额、令牌及收入，覆盖故障后不显式调用原请求结算方法的重启恢复。证据不足的历史记录继续待确认，禁止批量把 pending 改成 settled，禁止为了让利润变为已确认而删除缺口。

### 全链路收尾清单

| 检查项 | 最新结果 | 是否还需补充 |
|---|---|---|
| 初始任务差额、钱包/订阅、早到退款、重复处理 | 三库故障回归通过 | 持续断连与进程强杀仍未实测 |
| Midjourney 初始扣费、退款、失败终态重试、旧快照 | 三库故障回归通过 | 跨进程崩溃矩阵仍未实测 |
| 过期任务退款/补扣与历史不复活 | 三库回归通过 | 并发清理所有交错未穷尽 |
| 跨日缺口、渠道隔离、结束日查询 | 三库模型及 SQLite 查询回归通过 | 真实 WebSocket/多节点端到端未测 |
| 普通收入最终事务 | 三库故障回归通过 | F-06 预扣、历史记录与重启恢复未完成 |
| 坏文件、目录不可创建、10001 条标记、清理预算 | 相关模型回归通过 | 真实共享盘权限漂移与宕机未测 |
| 收入/成本合并、覆盖、筛选、分页、趋势 | controller 选择性回归通过 | 不等于生产规模压测或生产对账 |
| 前端首次失败、旧值失败、重试与统一快照 | 前阶段 773 项测试、类型检查及构建通过 | 最新阶段未改前端，未重复运行；浏览器验收未做 |
| 新库、发布版升级、独立日志库、重复启动 | 只有前阶段完整三库矩阵证据 | 当前最终版本尚未重新跑完整升级矩阵，不能宣称最终数据库验收完成 |
| Git 差异、编译 | 最新后端构建通过，差异检查通过 | 尚未提交或部署 |

### 最新实际执行命令与结果

工具链为 Go 1.26.5。模型矩阵连接第 9 节所列隔离容器及专用测试库，实际版本：SQLite **3.50.4**、MySQL **5.7.44**、PostgreSQL **9.6.24**。共五组矩阵用例、每组三个真实引擎，全部通过，无引擎跳过。下列 DSN 环境变量仅在矩阵命令的进程中设置；随后四包回归不代表再次完成 MySQL/PostgreSQL 矩阵。

```powershell
# 已设置 TEST_COST_BACKLOG_MYSQL_DSN / TEST_COST_BACKLOG_POSTGRES_DSN
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./model -run '^TestChannelMonitorIncome(FundingConfirmation|GapFollows|Midjourney|Initial|Retention)' -count=1 -v

& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./controller -run '^TestChannelMonitorProfitAnalyticsCombines' -count=1 -v
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./controller ./model ./service ./relay -run 'Test(ChannelMonitorProfit|.*Income|.*Profit|ChannelMonitorAnalytics|ChannelMonitor.*Coverage|ChannelMonitorCostRetention|ChannelMonitor.*Retention|.*Midjourney|.*Wallet|.*Batch|.*Task.*Billing|.*Task.*Quota|ExecuteTask)' -count=1
& 'D:/Go/sdk/go1.26.5/bin/go.exe' build ./...
git diff --stat
git diff --check
```

结果：上述选择性测试与根模块构建通过。没有把全仓库 Go 测试、真实浏览器、多节点或数据库升级矩阵写成已执行。

日志：[三库矩阵](D:/temp/profit-review-latest-matrix.log)、[跨日查询](D:/temp/profit-crossday-api.log)、[四包回归](D:/temp/profit-review-latest-backend.log)、[根模块构建](D:/temp/profit-review-latest-build.log)。复查结论与关键断言均已写在本文，临时日志是补充证据。

### 上游文件修改核对

按当前 `upstream/main` 核对，工作区以下已有上游文件被修改；其余本功能新增文件及下游文件不列入此表。这里只解释必要接入，不扩大重构范围。

| 文件 | 必要原因 |
|---|---|
| `model/main.go` | 注册收入缺口迁移 |
| `model/user.go` | 利润启用时钱包及时写入数据库，避免仅排队却确认收入 |
| `controller/relay.go`、`service/task_billing.go` | 异步任务插入与初始差额共同提交、会话交接 |
| `service/task_billing_test.go` | 以原子回滚行为替代旧部分成功预期，验证计费契约 |
| `model/midjourney.go` | 区分请求内目标与已扣额度、保留待退款任务、防止旧快照覆盖资金字段 |
| `controller/midjourney.go`、`service/midjourney.go` | 终态退款重试及事务扣费/退款接入 |
| `relay/mjproxy_handler.go` | 移除已纳入事务的重复用量增加 |
| `service/billing.go` | 普通请求最终差额与收入确认事务接入，提交不确定时避免单独确认 |
| `service/quota.go`、`service/violation_fee.go` | 实时分次扣费与违规费用接入同一事务方法 |

**当前结论：复查记录已经统一，但全部修复的验收尚未完成。F-06 仍需继续处理，未执行的验收按上表保留，不能再以局部测试通过宣布整个利润功能无问题。**

## 11. F-06 持久化结算恢复进展

### 已实现的恢复范围

收入表增加 `funding_delta`、`funding_token_id`、`funding_subscription_id` 三个字段，复用现有 `status` 的 `funding_pending` 状态。普通请求、实时分次扣费与违规费用首次写入收入时，同时保存最终差额、令牌及订阅归属；不再先创建 pending、然后另行写恢复指令。任务和 Midjourney 继续使用已有事务路径，不进入这个普通请求恢复队列。

普通 BillingSession 在锁内保存意图、交出预扣处理权。最终事务暂时失败时，结算被接受为待恢复，调用方仍可完成用量及日志记录；收入保持 `funding_pending`，不计作已确认收入。会话不再另行退还预扣，避免后台补差额时重复退款。准备失败不再继续走另一条不带幂等保护的资金扣费路径。重复指令核对用户、来源、额度及差额，冲突时拒绝执行。

通过已有系统任务调度注册 `channel_monitor_income_recovery`，每分钟最多处理 100 条、每轮 45 秒预算。恢复只从数据库读取指令，不依赖原 HTTP 请求、BillingSession 或调用方重新提供差额；失败条目保留并更新重试排序。最终资金、令牌差额及收入确认共同提交。清理显式保留 `funding_pending`，且删除时再次检查状态，避免选择条目后并发转为恢复状态被删。

未新增独立资金账本、进程内重试队列或 Redis 恢复依赖。新增字段由既有收入表迁移注册覆盖。旧字段缺失的行仍为 pending，不会被恢复 worker 猜测扣费。

### 实际验证

- 三库 `TestChannelMonitorIncomeFundingConfirmationIsAtomic`：钱包/订阅差额 −50、0、+50、重复/冲突指令、确认故障整体回滚、仅使用持久化参数恢复、提交确认丢失、重复恢复无二次扣费、旧 pending 不被处理、保留清理不删除资金指令。
- 同一三库用例模拟已有收入表没有三个新字段，插入旧 pending 及金额，执行两次 AutoMigrate；旧状态、额度与金额保留。
- `TestChannelMonitorIncomeRecoveryOwnsFailedFinalSettlement`：真实预扣 100、最终 50/100/150，注入最终确认失败；前台接受后仍显示资金 pending、退款入口不再退款。移除会话引用后，实际领取并执行系统任务，钱包、令牌及收入收敛；第二次恢复处理数为零。此为持久化故障与调度 handler 验证，不冒充 OS 进程强杀。
- 后端 controller/model/service/relay 相关收入、利润、计费、额度、违规费、系统任务和 Midjourney 回归通过。最初一个旧测试仍期待 `pending`，修正为新语义 `funding_pending`；没有删除故障断言。

本阶段命令：

```powershell
# 同第 9 节隔离数据库 DSN，实际三库版本未变
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./model -run '^TestChannelMonitorIncomeFundingConfirmationIsAtomic$' -count=1 -v
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./service -run '^TestChannelMonitorIncomeRecoveryOwnsFailedFinalSettlement$' -count=1
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./controller ./model ./service ./relay -run 'Test(.*Income|.*Profit|.*Billing|.*Quota|.*Violation|.*SystemTask|.*Midjourney)' -count=1
& 'D:/Go/sdk/go1.26.5/bin/go.exe' build ./...
git diff --check
```

日志：[三库故障及原收入表升级](D:/temp/profit-funding-recovery-matrix.log)、[会话交接及实际恢复 handler](D:/temp/profit-funding-recovery-service.log)、[四包回归](D:/temp/profit-funding-recovery-final-backend.log)、[构建](D:/temp/profit-funding-recovery-build.log)。

### 完整迁移验证

在现有两个隔离容器内新建专用 fresh/upgrade 主库、独立日志库及统计一致性库；SQLite 使用新的独立文件目录。对 SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 9.6.24 执行：新库初始化及两次验证、v1.0.0-rc.41 发布版源码种子升级及两次验证、独立日志库数据/索引/唯一约束保留、统计一致性矩阵，全部通过。升级种子沿用此前解压的 release41 源码目录；它不是 Git checkout，没有把失败的 git describe 写作发布版本验证证据。

```powershell
& 'D:/temp/profit-funding-recovery-validation-20261001/validate.ps1' -Engine mysql
& 'D:/temp/profit-funding-recovery-validation-20261001/validate.ps1' -Engine postgres
& 'D:/temp/profit-funding-recovery-validation-20261001/validate.ps1' -Engine sqlite
```

[执行脚本与各阶段日志目录](D:/temp/profit-funding-recovery-validation-20261001/validate.ps1)。SQLite 首次运行因新临时目录名未满足防误用断言失败，尚未写库；将文件名改为 `profit-validation-fresh.db` / `profit-validation-upgrade.db` 后完整通过。最后会话交接调整没有再次修改表结构；其后另跑了三库原收入表升级与故障用例。

### F-06 尚未关闭的具体内容

1. **意图之前的中断**：预扣已发生，但尚未进入最终结算或最终意图首次写库确实失败，再退出时仍没有可重放的资金指令。当前无法安全推断实际使用量，不能宣称整个预扣生命周期已经可恢复。
2. **历史记录的核对入口**：旧 pending、数据库与文件同时失败、历史缺口仍缺少完整的证据核对处理路径；不得仅按旧目标额度自动扣费或确认收入。
3. **已写意图但后续归属读取失败**：资金 worker 可以恢复，但 prepare 报错时已持久化的保守缺口尚不能凭单条收入成功自动删除（一个成本事件可能关联多次收入）。这一组合仍需复查和回归，不能把“资金可恢复”等同于“所有利润覆盖缺口已解除”。
4. **最终验收**：仍需进程级中断/重启验证、恢复与并发请求/清理的交错检查，以及全台账的最后复查。当前未宣称全部问题修复完成。

修改仍限于第 10 节所列计费入口及下游收入实现/现有测试；系统任务接入使用现有注册接口，没有修改上游调度框架。

## 12. F-06 归属恢复、进程退出与旧记录核对

### 已保存收入不再被误记成永久缺口

收入成功 INSERT 后、回读临时失败时，以成功写入的新行作为持久化证据继续处理；不对“确实没写入”作成功推断。已读取并核实收入后，成本归属读取失败也不再创建永久收入缺口：该行的 `cost_recorded=0` 本身就会阻止利润确认，恢复任务再补齐成本最终日期。后台扫描已完成成本且收入归属未完成的记录，包括资金已经 settled 的记录，不只处理资金 pending。

归属处理复用同一个函数并保持 outbox → income 锁顺序，不修改收入额度或资金。成本 outbox 清理保留已有 `cost_recorded=0` 的收入依赖，并在删除条件再次检查依赖。避免恢复任务尚未完成，成本结束日就因保留期被删除。

这解决了第 11 节第 3 项中的**成功 INSERT 后回读失败**及**已保存收入的成本读取失败**。未解除其他请求的真实缺口，也未盲目删除一个成本事件下的全部缺口。INSERT 本身报错且提交结果始终无法读取，仍属于待核对情况，不能用后续“某条收入成功”证明整个事件无缺口。

三库扩展原 `TestChannelMonitorIncomePreparationIsIdempotentAndRestoresGap`，验证故障后 settled 收入仍可补成本日期、恢复不改变额度、重复恢复不重复处理、清理保留依赖。服务层组合注入收入 INSERT 后回读不可用、成本读取失败及最终确认失败，实际恢复 handler 后资金和日期均正确，未生成永久缺口。

### 进程退出验证

`TestChannelMonitorIncomeFundingSurvivesProcessExit` 使用独立 Go 测试子进程和文件 SQLite。子进程持久化预扣状态夹具及最终指令后，分别在最终资金提交之前、之后执行 `os.Exit(23)`，不运行 defer/连接关闭/内存重试。父进程重新打开数据库，仅调用持久化恢复：提交前退出恢复一次；提交后退出恢复零次；钱包和令牌均为 9950、已用 50、收入 50 对应 500000000 纳元。重复恢复数为零。

这是实际进程退出与数据库重开测试；预扣初始状态由夹具写入，**不证明预扣过程中退出可恢复**，也不等同于操作系统掉电或所有数据库断电恢复实验。

### 旧 pending 的人工核对入口

新增 `cmd/channel-profit-reconcile`，仅供有数据库管理权限的运维人员使用。默认只读列出旧 pending，可按 ID 分页；不会启动网关、后台调度或数据库迁移。需要明确设置 `SQL_DSN`，或在 SQLite 下设置现有数据库的 `PROFIT_SQLITE_PATH`，不会自动连接默认库。

操作过程：

1. 核对请求已结束、不会再有异步结算/退款；收集可独立核实的预扣、补扣、返还和退款记录，算出净扣额度。消费目标日志或当前余额本身不足以证明净扣。
2. 只读命令列出旧 pending，保存结算键、用户、原 quota 和 updated_at。证据不足就保留待确认，不自动选零、不重新扣费。
3. 制作单条核对 JSON，填写 `settlement_key`、`user_id`、`expected_quota`、`expected_updated_at`、显式的 `net_charged_quota`、`operator`、`evidence`。evidence 填证据位置及核对结论，不填 Key、密码、请求/响应敏感内容。操作人是运维输入声明，命令不会冒充已认证的网站账户。
4. 使用 `-input` 预览当前行与核对值；复核后以同一文件加 `-apply` 应用。只修正收入金额/状态，不扣费、不退款、不修改成本、缺口或保留边界。

```powershell
# 先显式配置对应数据库环境变量；以下命令不含生产连接信息
go run ./cmd/channel-profit-reconcile -limit 100 -after-id 0
go run ./cmd/channel-profit-reconcile -input D:/reconcile/verified-income.json
go run ./cmd/channel-profit-reconcile -input D:/reconcile/verified-income.json -apply
```

模型事务只允许旧 `pending`，核对用户、原金额及更新时间；拒绝 funding_pending、settled、refund_pending、过期快照、缺少明确净扣值或证据的输入。修正与 `channel_monitor_income_reconcile` 系统任务历史记录在主库同一事务提交，审计写入失败则修正回滚。沿用既有运维历史表，未增加新审计表或资金账本。成本或真实缺口仍未解决时，核对收入后也不会错误确认整行利润。

`TestChannelMonitorIncomeManualReconciliationPreservesFunds` 在三库通过：成功修正、余额不变、重复拒绝、状态拒绝、快照冲突、净扣字段遗漏拒绝、审计故障回滚及证据保留。命令在三库隔离 fresh 数据库实际完成种子 → 只读预览 → apply → 回读；MySQL/PostgreSQL 再次 apply 被拒绝。只操作合成测试记录，没有处理真实业务历史。

### 最新验证与剩余工作

实际三库仍为 SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 9.6.24；使用前述隔离容器和专用库。本阶段没有新增数据库列，复用第 11 节已验证的结构。实际命令：

```powershell
# 设置三库隔离 DSN 后
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./model -run '^TestChannelMonitorIncome(Preparation|FundingConfirmation)' -count=1 -v
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./model -run '^TestChannelMonitorIncomeManualReconciliationPreservesFunds$' -count=1 -v
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./model -run '^TestChannelMonitorIncomeFundingSurvivesProcessExit$' -count=1 -v
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./service -run '^TestChannelMonitorIncomeRecoveryOwnsFailedFinalSettlement$' -count=1
& 'D:/Go/sdk/go1.26.5/bin/go.exe' build -o D:/temp/channel-profit-reconcile.exe ./cmd/channel-profit-reconcile
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./controller ./model ./service ./relay -run 'Test(.*Income|.*Profit|.*Billing|.*Quota|.*Violation|.*SystemTask|.*Midjourney|.*Outbox)' -count=1
& 'D:/Go/sdk/go1.26.5/bin/go.exe' build ./...
git diff --check
```

日志：[归属及资金三库回归](D:/temp/profit-cost-recovery-matrix.log)、[人工核对三库回归](D:/temp/profit-manual-reconcile-matrix.log)、[进程退出](D:/temp/profit-recovery-process-exit.log)、[服务层组合故障](D:/temp/profit-cost-recovery-service.log)、[最新四包回归](D:/temp/profit-cost-recovery-final-backend.log)、[构建](D:/temp/profit-cost-recovery-build.log)。命令行端到端使用仓库外 [隔离数据种子](D:/temp/profit-manual-reconcile-seed.go)，输入和预览/apply/回读/重复拒绝日志保存在 `D:/temp/profit-manual-{mysql,postgres}-*.log`；SQLite 结果已在工具输出中核对。

第 11 节“旧 pending 无核对入口”已有可执行处理路径；证据的真实性仍须由运维核实，软件不能凭空重建已丢失流水。F-06 仍未关闭：**预扣后、最终意图保存前退出或数据库持续不可写**的自动恢复仍需处理，最终全链路审查也未完成。本阶段没有把上述限制从范围中删掉。

本阶段修改的已有文件均为下游文件；新增人工核对模型及命令复用原有 GORM、JSON 包和系统任务表，无新依赖。此前上游文件改动理由仍见第 10 节。

## 13. 扩大复查后的最新结果与验收阻塞

本节覆盖此前第 12 节之后的工作。已有的 9 项利润问题编号不重排；测试失败在未核实业务根因前单列为 V 项，不直接宣称又发现 3 项生产资金缺陷。

### F-06 补充：MySQL 重复插入不能作为持久化成功证据

原实现使用冲突忽略插入，再在回读失败时根据 `RowsAffected == 1` 判断新收入已经持久化。MySQL 开启 `clientFoundRows=true` 时，无实际变更的重复键处理也可能报告命中一行，因此不能据此跳过已存在记录的身份核对。

已改为普通 INSERT，然后按结算键回读并校验已有记录；重复键或提交回执丢失时，仍以可读取的保存记录为准。只有 INSERT 明确成功且有新行 ID，回读又失败时，才使用此次写入的记录。插入和读取均失败则返回错误，不把请求中的用户/金额当成数据库事实。原有回归增加了“相同结算键、不同用户、回读故障”必须拒绝的组合。

实际执行以下命令，MySQL DSN 额外启用 `clientFoundRows=true`：

```powershell
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./model -run '^TestChannelMonitorIncome(Preparation|Funding|Manual)' -count=1 -v
```

SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 9.6.24 均通过，日志：[三库含 clientFoundRows 回归](D:/temp/profit-final-foundrows-matrix.log)。本次没有再次修改表结构，第 11 节的真实新库/升级/重复迁移验证仍适用于当前结构。

### 扩大范围后，验收仍失败

| 检查 | 最新结果 | 证据及限制 |
|---|---|---|
| 前端类型检查 | 通过 | `bun run typecheck`；[日志](D:/temp/profit-final-typecheck.log) |
| 前端生产构建 | 通过 | `bun run build`；[日志](D:/temp/profit-final-frontend-build.log) |
| 渠道监控前端整套测试 | 772 通过、1 失败，共 773 项 | 130 个测试文件中 1 个失败；见 V-01；[日志](D:/temp/profit-final-frontend.log) |
| 后端 controller/model/service/relay 全包测试 | model、relay 通过，controller、service 失败 | 不是此前按名称筛选的利润子集；见 V-02/V-03；[日志](D:/temp/profit-final-full-backend.log) |
| 利润收入模型三库专项 | 通过 | 上述 preparation/funding/manual、进程退出及并发相关用例；不能代替失败的整包验收 |
| 工作区 diff 检查 | 通过 | 实际运行 `git diff --stat` 与 `git diff --check`；不表示测试通过 |

实际新增执行的命令：

```powershell
# 仓库根目录
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./controller ./model ./service ./relay -count=1
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./controller ./service -run '^(TestKlingNativeRouteSubmitRetryPollSettleAndQuery|TestTaskExpressionChannelCostDatabaseMatrix)$' -count=1
# web 目录
bun run test src/features/channel-monitor
bun run test src/features/channel-monitor/components/__tests__/execution-records-dialog.test.tsx
bun run typecheck
bun run build
```

#### V-01：执行记录弹窗测试稳定失败，原因待核实

位置：`web/src/features/channel-monitor/components/__tests__/execution-records-dialog.fixture.tsx:593` 附近的分组切换及请求完成等待。全套和独立重跑均在等待“低成本备用渠道”时超时，日志同时有 React `act(...)` 警告。**退出的直接原因是预期内容未出现，不只是控制台警告。**

测试模拟切换到 vip 分组、等待接口响应，然后检查对应明细。需要继续区分请求夹具/异步通知问题与页面筛选状态问题；当前尚未通过真实浏览器证实生产页面受影响。未通过删除断言、增加等待时长或隐藏日志将其改成通过。独立失败日志：[弹窗复跑](D:/temp/profit-execution-dialog-rerun.log)。

#### V-02：Kling 原生任务端到端测试缺少成本事件表

`TestKlingNativeRouteSubmitRetryPollSettleAndQuery` 全包和独立重跑均失败。实际日志明确显示 `ApplyTaskBilling` 查询隔离前缀下的 `channel_task_cost_events` 表不存在，导致差额事务回滚；随后断言得到 NOT_START/250000，预期 SUCCESS/1。

该测试的 `openTaskDialectDatabase` 只初始化 User、Channel、Task、Log、ChannelRatioMonitor，未包含 ChannelTaskCostEvent。当前证据首先指向测试数据库准备不完整；不能把测试夹具缺表直接写成正常迁移后生产库也会缺表。仍需补齐必要夹具后验证成功状态、差额、重复扣费及对外查询断言，不能绕开资金事务的错误处理。独立复跑日志：[后端失败项复跑](D:/temp/profit-final-backend-failures-rerun.log)。

#### V-03：任务表达式成本测试有整包/独立结果差异

`TestTaskExpressionChannelCostDatabaseMatrix/sqlite` 在 service 全包测试中失败，包括 `single_actual_usage` 等子项：预期最终额度 25000，实际 0；预期余额 975000，实际 1000000；成本也未按预期投影。使用相同工作区按完整测试名独立运行则通过。

这说明目前不能排除测试间共享状态/缓存未恢复，尚未定位具体污染来源，也未证明所有生产请求都受到影响。此次失败命令未配置该用例专用的 `TEST_TASK_COST_MYSQL_*` / `TEST_TASK_COST_POSTGRES_*` 环境变量，不能将这个独立通过结果写成三库通过。该项目仍是整包验收阻塞，应定位共享状态并显式初始化/清理，不应只保留独立运行结果。

### 未关闭事项及下一步条件

1. F-06 的预扣成功到最终意图写入之间，仍缺少持久化恢复事实。对无法确认实际用量的中断请求，保留预扣待人工核对、全额退还或按预扣确认收入会产生不同的真实账务结果；已经提出业务口径问题，目前没有收到选择，不能擅自实行其中一种资金处置。
2. V-01、V-02、V-03 仍待修复或核实，并重新运行受影响用例及此前失败的完整测试范围。
3. 第 2、4 节列出的真实 WebSocket 跨午夜、多节点、灾难恢复和规模边界，仍按原有证据等级保留；已有模型故障注入不冒充这些环境验证。

**本轮复查记录已补齐最新失败证据，但整个修复目标尚未完成。当前状态不能用于发布“利润功能全部修复”或“全套测试通过”的结论。**

## 14. 三项验收问题的最小修复

### V-01：查询通知在测试中未纳入 React act

进一步调查时，原用例也出现直接运行及独立重跑通过的结果，因此第 13 节“稳定失败”的结论应更正为**可复现但非每次必现**。等待适配器的 Promise 完成，不等于 TanStack Query 后续通知已被 React 渲染；原夹具在 `act` 外等待 DOM，存在未受控的异步更新。

仅在该独立进程夹具中用 `notifyManager.setNotifyFunction` 将查询通知交给 React `act`，末尾恢复默认通知函数。没有改产品组件、接口、筛选逻辑或超时时间，保留加载中不显示旧行、vip 明细出现、翻页、切换任务清空搜索及筛选持久化断言。未新增组件，没有组件复用缺口。

`bun run test src/features/channel-monitor` 最新结果为 **130 个文件、773 项全部通过**，日志：[完整前端复跑](D:/temp/profit-final-frontend-v2.log)。`bun run typecheck` 与涉及夹具的 `bunx oxlint -c .oxlintrc.json src/features/channel-monitor/components/__tests__/execution-records-dialog.fixture.tsx` 均退出 0，日志：[类型检查](D:/temp/profit-execution-fixture-typecheck.log)、[lint](D:/temp/profit-execution-fixture-lint.log)。仅修改测试代码，沿用第 13 节的生产构建结果。

### V-02：补齐原生任务端到端测试所需的表

在 `controller/plugin_native_e2e_test.go` 的现有数据库夹具中补建 `ChannelTaskCostEvent`，仍通过真实轮询和资金事务。没有让业务代码忽略缺表错误，成功状态、最终额度 1、用户余额 999999、重试不重复预扣及查询响应断言保留。

SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 9.6.24 均通过。该文件存在于 upstream/main；修改前已比较上游，修改仅为迁移模型列表增加一项。直接使用已有数据库 helper，无须修改上游公共测试工具或轮询实现。

### V-03：补齐测试快照的表达式缓存键

任务成本夹具创建 `BillingSnapshot` 时只填写 `ExprString`，未填写 `ExprHash`；表达式编译缓存按传入哈希寻址，空键会与先前不完整的测试快照共享。独立运行时空键首次编译的是正确表达式，所以可单独通过；整包运行时则会误用已有程序。

夹具现在同时填写该表达式的 `billingexpr.ExprHashString(...)`，与正式任务提交 `relay/relay_task.go` 创建快照的方式一致。仅一行测试数据修正，没有通过清空全局缓存、跳过整包测试或修改预期额度掩盖错误。生产任务提交与普通价格快照创建点均已核对存在表达式哈希；未将缺字段的测试快照直接认定为生产任务都会算零。

该测试文件为下游文件。SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 9.6.24 的全部现有成本用例通过，包括单次/批量实际用量、零用量、免费用户、即时完成/失败、晚登记、资金成本共同回滚及旧按次计费，使用独立日志数据库。

### 本阶段数据库验证命令与证据

```powershell
# SQLite + 邻接余额估算用例（原来可能先写入表达式缓存）
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./service ./controller -run '^(TestKlingNativeRouteSubmitRetryPollSettleAndQuery|TestTaskExpressionChannelCostDatabaseMatrix|TestChannelBalance.*)$' -count=1
# 配置 TEST_TASK_COST_MYSQL_DSN / _LOG_DSN 和 TEST_TASK_COST_POSTGRES_DSN / _LOG_DSN 后
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./service -run '^TestTaskExpressionChannelCostDatabaseMatrix$' -count=1 -v
# 分别 TEST_TASK_DB_DIALECT=mysql / postgres，设置 TEST_MYSQL_DSN / TEST_POSTGRES_DSN 后
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./controller -run '^TestKlingNativeRouteSubmitRetryPollSettleAndQuery$' -count=1 -v
```

仍使用第 9 至 13 节的隔离容器，主库 `new_api_cost_backlog_test`、独立日志库 `new_api_profit_log`；原生任务测试使用各自唯一表前缀。没有连接业务库。日志：[邻接用例](D:/temp/profit-fixed-test-fixtures.log)、[任务成本三库](D:/temp/profit-test-fixtures-cost-matrix.log)、[Kling MySQL](D:/temp/profit-test-fixtures-kling-mysql.log)、[Kling PostgreSQL](D:/temp/profit-test-fixtures-kling-postgres.log)。没有生产数据库结构改动，不需重做第 11 节的发布版升级矩阵。

后端四包命令 `go test ./controller ./model ./service ./relay -count=1` 已完成，日志为 [四包复跑](D:/temp/profit-final-full-backend-v2.log)：model、service、relay 全包通过；controller 的 Kling 原生任务用例通过，但另一个 `TestSecurityAccountDeletionConcurrentRequestsHaveOneWinner` 失败。账户删除并发用例在前次整包运行中未失败，本次断言单一成功请求数期望 1、实际 0。单独执行 `go test ./controller -run '^TestSecurityAccountDeletionConcurrentRequestsHaveOneWinner$' -count=1 -v` 通过，见 [独立复跑](D:/temp/profit-controller-account-rerun.log)。这不是利润修复的确定业务缺陷；未修改认证代码或删除该断言，也没有用单测通过替代整包验证。

随后 `go test ./controller -count=1` **整包通过，238.833 秒**，日志为 [controller 整包再次复跑](D:/temp/profit-controller-full-v3.log)。因此当前四个包各自的最新完整运行都通过，但不存在“一次四包命令全部通过”的最新证据；账户删除用例的偶发失败原因未在本利润任务中解决，也不宣称其已修复。第 13 节 V-01/V-02/V-03 已有修正和完整范围复跑证据，可关闭这三个验收问题。

F-06 的预扣中断窗口、已提出但未获得答复的资金处置口径，以及第 5 节部署/灾难恢复边界仍保留。以上测试修复不替代这部分工作。

### 剩余阻塞核对

再次核对了当前代码、`TestChannelMonitorIncomeJournalFaultDoesNotBlockStartupOrRetention` 的具体断言、三库收入恢复日志、前端全量结果和 controller 最后一次完整结果。F-02/F-03 的启动降级、异常文件保留、10001 条过期标记清理及数据库清理继续执行都有实际断言，已将首页过时的“待最终复查”更新为已复核。没有因其他项目已通过就关闭 F-06，也没有重复运行已通过且未发生变化的测试。

目前没有仍在运行、需要等待的测试。剩余确定问题为 F-06：预扣已经提交、实际用量及最终结算指令尚未持久化时进程退出，程序缺少足够事实决定净扣金额。需要用户选择异常请求的处置：① 保留预扣并标记待人工核对（建议）；② 确认请求已终止后退还全部预扣；③ 按预扣金额作为最终收费。这是实际账务规则，不是代码修改权限问题；当前未收到选择。三种方案都仍需实现可靠的预扣事实记录，并验证中断、恢复及重复处理，不能仅靠文档声明为已修复。

该口径已连续数轮待确认，期间可独立执行的三项测试修复和完整复跑均已完成。自动修复在此等待业务输入；第 5 节的部署条件和未覆盖环境保留，不据此声称不存在未知问题。下一步应按选定口径补齐 F-06，再做相关三库及中断恢复验收，继续更新同一台账。

## 15. 方案 1：持久预扣与人工核对闭环

本节取代第 13、14 节的“尚未收到资金处置选择”。用户已明确选择 **保留预扣并标记待人工核对**；不自动退款，也不按预扣金额自动确认收入。本次未修改生产资金、提交代码或部署服务。

### F-06 实现与逐路径复核

复用现有收入表的 `reserved` 状态保存实际预扣事实，不增加资金账本、表字段或孤儿退款调度。钱包/订阅、令牌扣减和预扣记录在同一事务提交。金额仍受单笔上限和保存的换算快照约束。重复请求标识、旧订阅凭据、令牌不足和事务内写入失败均不能留下第二笔或半笔扣款。

| 路径 | 最终行为与证据 |
|---|---|
| 首次预扣 | 事务成功才继续上游；选路上下文已就绪而 `RelayInfo.ChannelMeta` 尚未初始化时也能正确记录，不提前重置处理器状态 |
| 补充预扣 | 余额、令牌和已预扣金额共同更新；旧金额重放被拒绝；明确额度不足时仍可退还原预扣并返回 403 |
| 提交回执丢失 | 停止继续扣款，保留实际落库的金额待核对；预扣余额缓存失效后从数据库重读；初始及补充预扣均有故障注入 |
| 正常结束 | `reserved` 转入现有 `funding_pending`，最终资金和收入确认共同提交；50/100/150 的返还、等额与补扣均验证；重试改换渠道后按最终渠道和成本事件归属 |
| 明确失败、本地响应、零预扣 | 复用原有持久退款队列；关闭收入预扣记录与退款共同提交；本地退款执行失败可重放，结算不能抢回所有权，重复退款无资金变化 |
| 最终指令保存前退出 | `reserved` 保留、后台资金恢复忽略它；不增加收入、不自动退款；历史清理保留核对证据 |
| 最终指令已保存后退出 | 只重放保存的差额；资金已经提交的记录不会再次扣款 |
| 异步任务及旧路径 | 任务插入仍和初始差额共同提交；已有任务早到退款/结算、历史清理和 Midjourney 回归继续通过；监控未就绪时沿用既有预扣入口 |
| 人工核对 | 默认只读列出 `pending` / `reserved` 及资金归属；核对 `reserved` 必须提供 `request_terminated: true`，先确认请求及所属进程终止且无任务继续结算；状态、金额和时间戳防止覆盖陈旧快照；修改与审计共同提交，资金余额保持不变 |

进程退出验证启动独立 SQLite 子进程，在预扣事务内更新令牌后、预扣提交后、最终结算提交前及提交后分别直接退出。重新打开数据库：未提交预扣时余额无变化且无记录；已提交预扣时余额少 100、记录为 `reserved`，恢复任务处理数为 0；最终指令则按保存的差额恢复。它验证了进程退出，不冒充断电、主库灾难恢复或真实多节点实验。

本轮实现中发现并修正的接入遗漏（零预扣关闭、本地退款所有权、确定回滚与提交不确定区分、处理器初始化前的渠道归属）都属于 F-06 的实施验收，没有把未通过的中间版本当成修复完成。最终指令尚未保存且写库失败时仍记录核对缺口；人工确认单条收入不自动解除独立缺口或未知成本。

### 新发现的验收用例问题 V-04

在追加 model/service 整包运行时，`TestChannelDailyCostRecoveryReservesCleanupBeforeDeadline/round` 实际返回虚拟时刻 3.025 秒，原断言要求严格等于 3 秒，导致 service 整包失败。原测试将记账截止时刻当成整个函数返回时刻，遗漏了随后释放租约的独立三秒预算；取消后的 SQLite 锁重试可以占用该预算。该次失败完整保留在 [复跑原始日志](D:/temp/profit-reservation-final-model-service.log)，没有认定为生产资金少退或重复扣款。

修正仅在既有测试：在故障回调直接断言记账 context 的精确 deadline，函数返回时检查仍处于记账截止至清理预算截止的区间，并保留释放数量为 1、未记账、无租约、已设置重试时间的断言。没有增加等待时长、删除资金断言或改生产调度。修正后 service 整包通过，见 [V-04 修正后整包日志](D:/temp/profit-reservation-final-service-v2.log)。

### 实际验证环境、命令及结果

数据库仍为专用隔离实例：SQLite **3.50.4**、MySQL **5.7.44**（`127.0.0.1:13318`）、PostgreSQL **9.6.24**（`127.0.0.1:15438`）；测试库 `new_api_cost_backlog_test`。MySQL 额外开启 `clientFoundRows=true`。未连接业务 MySQL/Redis。缓存回执丢失用例使用进程内 miniredis，不能代替第 5 节的真实 Redis 灾难恢复验证。

```powershell
# 配置 TEST_COST_BACKLOG_MYSQL_DSN / TEST_COST_BACKLOG_POSTGRES_DSN 指向上述隔离库
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./model -run '^TestChannelMonitorIncome(Reservation|Manual|Funding|InitialTask|Preparation|Retention)' -count=1 -v
# 最终补充预扣回执丢失、额度不足及旧订阅凭据用例
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./model -run '^TestChannelMonitorIncomeReservationDatabase$' -count=1 -v
# 完整范围与后续最小修改的复跑
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./controller ./model ./service ./relay -count=1
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./model ./service -count=1
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./service -count=1
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./service -run '^TestChannelMonitorIncomeReservedSessionLifecycle$' -count=1
& 'D:/Go/sdk/go1.26.5/bin/go.exe' build ./...
git diff --stat
git diff --check
```

| 检查 | 结果与证据 |
|---|---|
| 收入专项三库、人工核对及进程退出 | 通过；[综合矩阵](D:/temp/profit-reservation-matrix-v1.log) |
| 最终预扣三库 | 通过；[最后矩阵](D:/temp/profit-reservation-final-matrix-v3.log)，含回执丢失、令牌不足、补充预扣、持久退款、旧订阅凭据拒绝 |
| 四包完整运行 | 当次全部通过：controller 288.845s、model 101.706s、service 114.258s、relay 0.440s；[日志](D:/temp/profit-reservation-full-v1.log) |
| 后续 model/service 完整运行 | model 84.317s 通过；service 暴露 V-04，不能将该次记为通过；上文已记录修正 |
| V-04 修正后 service | 70.296s，通过；[日志](D:/temp/profit-reservation-final-service-v2.log) |
| 真实初始化顺序专项 | 通过；[渠道初始化顺序](D:/temp/profit-reservation-initial-channel.log) |
| 最新 service 与构建 | service 整包 68.636s 通过；[最新整包日志](D:/temp/profit-reservation-final-service-v3.log)；`go build ./...` 退出 0，[最终构建日志](D:/temp/profit-reservation-final-build-v2.log)；包含渠道初始化顺序和结构化额度错误修正 |
| 前端 | 本阶段未改前端，沿用第 14 节 130 文件、773 项通过以及类型检查、构建证据 |

本阶段无新表、字段、索引、ORM 或驱动变更，继续使用第 11 节新库/发布版升级/独立日志库/重复迁移的结构验证；上述新增资金路径另行实际跑了三库，未以旧迁移日志替代行为验证。

### 修改边界及最终结论

新增预扣实现位于下游的 `model/channel_monitor_reservation.go` 和 `service/channel_monitor_reservation.go`，复用既有任务资金更新、收入、系统任务、退款队列和 JSON 工具。回归集中扩展已有收入测试文件，不按每个调用层另建测试文件。新状态未增加前端文案或翻译键。

对照 `upstream/main`，本阶段新增两处必须接入的上游文件：`model/subscription.go` 允许既有订阅预扣逻辑参与外层事务，并拒绝把旧预扣凭据用于新令牌扣款；`service/billing_session.go` 接入监控预扣/补充预扣/退款、保护提交不确定状态并关闭零额度请求。没有复制订阅选择和重置逻辑。其余已有上游文件及必要原因仍见第 10 节，`controller/plugin_native_e2e_test.go` 的夹具改动见第 14 节；新修改的本地退款和成本恢复测试文件均为下游文件。

最终结论：**F-01 至 F-09 共 9 项已确认问题均已修复并完成当前范围复核，V-01 至 V-04 的验收问题已修正并有完整范围复跑证据。** 最后实际运行 `git diff --stat` 和 `git diff --check`，未发现空白格式错误；未发现需要继续编号的新确定利润缺陷。第 5 节真实部署、灾难恢复、规模边界以及未验证环境继续保留，不能宣称已经证明不存在未知问题。预扣中断需要人工核对是用户明确选定的业务规则，不再列为未实现的自动恢复缺陷。
