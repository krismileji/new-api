# 渠道监控利润功能系统复查记录

> 更新日期：2026-10-07。**最新全量复查见第 30 节：当前工作区未发现新的已确认下游利润缺陷；后端四包、前端 773 项、类型检查及构建重新通过。** F-18 修复证据见第 29 节。官方 F-14-A 按用户要求保留；旧记录缺证据仍需人工核对，纯内存批次保留既有退出边界。没有提交、推送或部署。
> 本轮对象：提交 `30c7db2e1`，相对父提交 `f4baf15c475dee5a60848e8f446d46ebab6b4739` 的 52 个文件改动。此前“未提交”描述属于当时的历史记录。
> 第 1 至 16 节保留此前故障、修复和验证证据；涉及 F-13 数据库直读的结论仅适用于当时版本，已由第 17 节替代。没有修改生产账目、提交或部署。
> **前轮验证：** F-06 已按用户选择的“保留预扣待人工核对”实现；三库和中断回归通过，最后 service 整包及构建通过，前端 773 项通过。上述证据作为本轮输入，不代表第 16 节的任务已经核查完成。

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

### 修复前待处理清单（2026-10-07，第 21 节已更新状态）

以下保留修复前的五类问题及其触发条件；当前状态以第 21 节为准。详细原始归属、复现、命令和验收要求见第 20 节。

| 编号 | 优先级与状态 | 触发条件及影响 | 证据与后续验收要求 |
|---|---|---|---|
| F-14 | **P1，未修复；分官方语义与下游扩展** | A：旧周期结算差额释放新周期额度；B：跨周期补充预扣后全额退款漏退新周期补扣 | 三库、54 个组合用例；A 遵循用户要求等待官方，不能宣称风险消失；B 是下游责任，应修复自己的分周期记录与退款 |
| F-15 | **P1，下游缺陷，未修复** | 明确退款指令长期待恢复时，官方七天清理删除其依赖回执，退款事务持续失败 | 三库调用真实清理入口复现；在下游保存可恢复依据，不能简单改官方全局保留期 |
| F-16 | P2，下游缺陷，未修复 | MySQL 默认 changed-rows 语义：免费 Midjourney 初始结算失败；普通任务同秒无变化成本结算误报并发冲突 | 三库对照；MySQL `clientFoundRows=false` 复现，其他两库通过；修复局部无变化更新判断，不改全局驱动参数 |
| V-05 | P2，下游缺陷，未修复 | 自己完成的分钟仍在续期快照中，被误判失去租约，取消同批后续重建；过期清理另有残留 | 未过期、无竞争者的服务层屏障复现，加三库模型状态矩阵；需协调完成与续期，保留真实接管保护 |
| F-13 | P2，下游缓存衔接缺口，未消除 | Redis 删除失败或旧快照回填，余额偏高或偏低，影响展示及额度判断 | 三库资金 + 隔离 Redis 故障测试；继续官方缓存优先，不能用每次查库规避，不承诺固定时间自愈 |

本次只登记问题和验收要求，不表示已开始修复或已完成验收。现有整包回归通过与上述故障复现失败同时成立；未评估实际线上受影响用户或金额。

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

## 16. 提交后改动核查任务（2026-10-02）

### 目标、基线与当前状态

用户先要求：“现在需要核查一下改动，先将核查任务写在文档中”；后续执行目标为“开始根据文档逐个核查，不要做的太复杂了”。以下任务按实际证据更新，任务条目代表需要检查的风险，不代表已发现缺陷。

核查对象固定为 `git diff 30c7db2e1^ 30c7db2e1` 的全部 52 个文件，以及它们直接影响的调用点、启动流程和恢复任务。重点检查这笔修复是否引入计费回归、默认行为变化、额外耦合或不必要的复杂度，而不只重跑已有 F-01 至 F-09 的测试。若核查过程中代码有变化，记录实际验证的提交和工作区差异，不能混用不同版本的结论。

继续采用已确定业务规则：请求异常中断且缺少最终用量时保留预扣待人工核对，不自动退款、不按预扣自动确认收入。本轮不因“利润监控启用”的表述自行增加开关或改变这项规则。

### 逐项任务与验收条件

按表内顺序推进，先检查可能改变资金的路径。每项完成后将“待核查”改为“通过 / 发现问题 / 受阻”，并补充代码位置、验证命令、结果和限制。

| 编号 | 核查任务 | 重点场景与验收条件 | 状态 |
|---|---|---|---|
| A-01 | 提交范围与必要性 | 逐文件登记生产实现、测试、文档、工具；对照父提交和 `upstream/main`，核对每处上游文件修改原因。确认没有夹带无关改动、重复实现或只为测试通过而改变业务规则。 | 通过，必要性说明见后文 |
| A-02 | 自动初始化与实际影响范围 | 追踪 `InitializeChannelMonitorIncome`、`ChannelMonitorIncomeReady`、主从节点启动和迁移顺序；明确“就绪状态”不是用户开关。检查关闭可靠成本队列、关闭消费日志、不打开利润页面时，哪些资金及后台行为仍然运行；文档必须与代码一致。 | 通过 |
| A-03 | 首次预扣兼容性 | 覆盖钱包、订阅、两种优先回退、零额度、信任额度、无限额度令牌、Playground 和探测请求；使用真实入口调用顺序核对渠道尚未初始化的情况。资金来源、令牌、预扣记录必须同时成功或回滚，明确失败不能错误回退成第二次扣款。 | 通过 |
| A-04 | 补充预扣与重试 | 核对模型/分组/渠道变化后的补充额度与归属；额度不足、取消、重复请求标识、过期快照及提交回执丢失。确定回滚后可退原预扣；提交不确定时不能按旧金额退款或继续上游；保留原错误码契约。 | F-11 已修正；预扣三库及会话回归通过 |
| A-05 | 最终结算与恢复所有权 | 绘制允许的收入状态转换并列出各入口；检查等额、返还、补扣、免费请求、绕过 `SettleBilling` 的调用、重复结算以及多个恢复执行者。只有持久化的最终指令允许自动执行，最终资金和确认只提交一次。 | 发现问题：F-10，已修正 |
| A-06 | 失败退款与本地响应 | 检查普通失败、零预扣、本地响应、持久退款执行失败、入队回执丢失、退款与结算交错；订阅重置/到期/删除、令牌删除等异常必须保留可解释状态。退款完成后不能留存可再次补扣的预扣，不能因成本未知而错误确认整段利润。 | 已覆盖退款回归通过；多进程强制中断仍属 R-04 |
| A-07 | 异步任务与 Midjourney | 核对任务提交、初始资金事务、立即成功/失败、轮询早到、重复回调、旧快照回写、退款重试以及报表已清理的任务。真实任务入口必须使用最新已扣额度；资金、任务状态、用量统计和收入不能互相重复累计。 | 发现问题：F-10/F-11，三库复核通过 |
| A-08 | 缓存、批量写入与并发 | 检查钱包/令牌缓存失效、缓存重建与提交交错、Redis 不可用、遗留批次、各事务锁顺序和多请求竞争。数据库扣款不能靠缓存猜测；恢复重试不能重复写缓存差额；检查新增全局互斥锁是否把无关用户串行阻塞。 | F-10/F-12 已修正；10 月 6 日恢复缓存优先，F-13 保留为未消除的异常一致性限制，见第 17 节 |
| A-09 | 中断证据与人工核对 | 覆盖预扣事务前后、最终指令保存前后、最终提交前后退出及确认回读失败；后台不得处理 `reserved`。检查核对工具默认只读、必填证据、原进程终止条件、快照校验、审计事务和重复提交；人工核对不改变资金、不抢占仍运行的请求或任务。 | 通过（外部终止证明依赖人工） |
| A-10 | 利润口径、跨日及查询覆盖 | 核对 1:1 换算快照、订阅名义收入、违规费用和实时分次扣费；收入、最终成本及缺口跨午夜归属一致。检查仅成本行、未确认收入、亏损过滤、汇总/趋势/下钻和渠道行的一致性，以及 SQL SUM、JSON/JS 数值边界。 | 三库统计通过；R-05 数值上限已实测，真实 WebSocket 跨日未测 |
| A-11 | 历史清理与故障降级 | 检查坏目录/文件、超过读取上限、清理中断、恢复与清理并发；保留所有尚未结束的资金事实及成本依赖。过期报表不能阻断合法任务退款；缺口故障不能误报利润已确认，也不能无故阻止网关启动。 | 已测清理/降级通过；共享盘故障及多进程并发仍待部署验证 |
| A-12 | 数据库兼容与升级 | 在真实 SQLite、MySQL、PostgreSQL 上验证新增事务、嵌套事务、唯一约束、锁和 `clientFoundRows`；核对本提交全部结构变化，覆盖新库、代表性发布版升级和至少两次迁移。若修改触及独立日志库则同步验证，记录版本、命令及数据/索引保持结果。 | 通过 |
| A-13 | 前端和日常使用回归 | 检查刷新失败保留旧值的提示、首次失败、重试、分页、分组切换、日期与币种显示、待确认和真实零利润区分；确认未新增无依据的“利润开关”。核对测试夹具修正是否保留原用户行为断言，必要场景补浏览器操作验证。 | 通过（源码及组件回归） |
| A-14 | 性能与后台运行边界 | 比较父提交和本提交在固定负载下的写入次数、事务时间、锁等待、错误率和请求延迟；检查每分钟恢复任务的批次上限、公平性、超时及多节点重复领取。记录可重复结果；未实测不得直接宣称性能无影响。 | 已完成固定计费样本、事务/锁日志、恢复并发及额度读取成本验证；完整网关负载未测 |
| A-15 | 回归证据与最终交付 | 核对已有测试是否覆盖真实入口和失败后资金事实，避免仅断言内部实现；复跑受影响专项及相关整包、前端类型检查/构建/渠道监控测试。检查临时日志是否仍可读取；最终汇总当前问题、未验证边界、实际用户影响和最小必要修正，不以历史绿灯代替本轮结论。 | 已完成本轮回归与交付记录；部署限制保留 |

### 执行方式与记录要求

1. 先完成 A-01、A-02 的范围及默认行为核对，再按 A-03 至 A-09 检查资金链路；后续完成统计、清理、数据库、前端、性能和最终回归。共用同一份调用链及状态表，避免各层重复造测试。
2. 优先使用现有隔离测试和确定输入复现。发现问题时先记录触发条件、实际结果、预期结果及资金/可用性影响，再实施最小修正并增加对应回归；不要仅依据猜测或测试夹具缺失判断生产缺陷。
3. 已有问题复发沿用 F 编号并重新打开；新确认的业务问题从 F-10 起编号，验收夹具问题从 V-05 起编号。疑点及环境缺失单独记录，不能与确定缺陷混计。
4. SQLite、MySQL、PostgreSQL 使用隔离实例，不连接业务库。缓存、多节点、真实 WebSocket 跨日和故障实验使用明确隔离环境；无法执行时保留“受阻”和具体原因，不写成通过。
5. 复用第 5 节 R-01 至 R-05 跟踪真实共享目录、Redis/数据库灾难恢复、强制终止和规模问题。每项区分源码审查、故障注入与真实环境实测，不能用一种证据替代另一种。
6. 执行到表达式、插件或前端代码审查时，先读适用的项目规则和技能；如发现涉及认证的路径，按项目认证规范补相应指导和验证。任何修正都保留上游文件最小改动原则。

每项核查记录至少包含：**任务编号、版本、代码位置、触发条件、实际/预期结果、证据命令或日志、影响、关联 F/V/R 编号、修正及复核状态**。已有测试失败日志不得被新的成功结果覆盖。

本轮完成条件：A-01 至 A-15 都有明确结论；确认的资金和可用性缺陷修正并复核；适用的真实三库检查通过；无法验证的环境和性能限制明确保留。存在受阻项时只能报告已完成范围，不能把整份清单标为全部核查通过。

### 本轮执行记录（持续更新）

版本为 `30c7db2e1` 加当前未提交工作区差异。以下仅记录已经执行的检查；尚未覆盖的任务不提前关闭。

#### A-02 / A-03：启动行为与首次预扣

源码核对：`model/main.go` 中主节点先迁移再初始化，从节点只检查已存在状态；`model/channel_monitor_income.go` 初始化成功后设置就绪状态，旧换算未迁移时从节点拒绝就绪，缺口日志故障保留利润待确认而不阻断启动。`service/system_task.go` 的任务执行器只在主节点运行，收入恢复每分钟运行且受就绪状态约束。`model/user.go` 在就绪后关闭钱包额度批量延迟写入；这属于所有计费请求的实际行为变化，不能描述为只影响利润页面。

无独立利润启停开关。不打开页面不会停止收入记账；关闭消费日志也不会停止收入记账；关闭可靠成本队列会使利润无法确认，不会停掉资金和收入处理。

扩展现有 `service/channel_monitor_income_test.go` 的 `TestChannelMonitorIncomePreConsumeCompatibility`，关闭消费日志及可靠成本队列后，通过 `PreConsumeBilling → InitChannelMeta → SettleBilling` 实际入口顺序覆盖钱包、订阅、两种资金回退、钱包零额度、订阅零额度（仍按既有规则先预扣 1）、无限令牌、Playground、信任额度和探测请求。10 个场景通过，核对最终钱包、订阅、令牌金额和重复结算；探测请求不登记收入。

```powershell
& D:/Go/sdk/go1.26.5/bin/go.exe test ./service -run '^TestChannelMonitorIncomePreConsumeCompatibility$' -count=1 -v
```

证据：[兼容性测试](D:/temp/profit-audit-20261002-compatibility-v2.log)。首次编译时新测试错误使用了订阅套餐的指针字段，已改为显式设置用户订阅的回退字段；不属于生产缺陷。原日志保留在 `D:/temp/profit-audit-20261002-compatibility.log`。

本轮还实际运行了 `TestChannelMonitorIncomeReservationDatabase`、`TestChannelMonitorIncomeFundingConfirmationIsAtomic`、`TestChannelMonitorIncomeParityUpgrade` 的真实三库用例，均通过；坏目录与清理用例在 SQLite 通过。证据在 [首次矩阵日志](D:/temp/profit-audit-20261002-funding-matrix.log)，该命令整体失败原因是下述新缓存测试的 PostgreSQL 夹具，不能将整条命令记为通过。

#### F-10（P1）：结算后缓存可能重复加减差额，提交结果不明时保留旧余额

关联 A-05、A-07、A-08。在 `SettleChannelMonitorIncomeFunding` 数据库提交之后、缓存差额更新之前，由另一个读取入口从数据库重建缓存，原实现会在新余额上再次叠加差额。例如已预扣 100、最终消费 50，数据库正确余额为 9950，缓存会变为 10000；补扣方向则会低估余额。提交成功但 COMMIT 回执及确认回读同时失败时，原函数提前返回，保留旧缓存。

影响：余额显示、令牌额度校验和后续请求准入可能错误；本复现没有证明数据库重复扣款。订阅用户的钱包不应随订阅结算变动，但令牌缓存仍受影响。同类写法还存在于任务初始结算、任务后续结算/退款及 Midjourney 扣款/退款；任务后续结算原代码还会把订阅差额错误应用到钱包缓存。

确定复现使用真实数据库提交回调，在提交完成后通过 `GetUserCache`、`GetTokenByKey` 重建缓存，不使用等待时长或随机并发碰运气。普通收入原实现 SQLite 共 16 场景，4 个暖缓存正常场景通过，12 个重建/故障组合失败：[修复前证据](D:/temp/profit-audit-20261002-cache-before.log)。

最小修正：共用原预扣路径的缓存失效函数，将其命名及参数扩展为资金缓存失效；结算不再盲目叠加 Redis 差额，提交结果不明时也使涉及的缓存失效，由现有读取入口从数据库加载。订阅仅失效令牌缓存，保留钱包独立性。没有增加表、字段、恢复状态、后台任务或新的资金账本。改动均位于下游已有文件。

普通收入修正后 SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 9.6.24 的 48 个场景通过，覆盖钱包/订阅、返还/补扣、暖缓存/提交后重建/回执丢失/回执和回读同时丢失，以及重复调用。证据：[三库缓存回归](D:/temp/profit-audit-20261002-cache-matrix-v2.log)。使用端口 13318、15438 的专用隔离实例及进程内 miniredis，未使用业务 MySQL 或 Redis。真实 Redis 中断和旧快照延迟写回等部署边界仍归 A-08，不能据此宣布缓存相关风险全部关闭。

#### V-05：新缓存回归夹具未初始化 PostgreSQL 保留字引用

首次三库缓存验证调用了此前夹具不使用的 `GetTokenByKey`，夹具只切换数据库类型，没有像启动流程一样调用 `initCol`，导致 PostgreSQL 查询使用 MySQL 的反引号。已在新测试中显式初始化并清理引用状态，同时确保断言失败也恢复 `DB`。未修改生产查询，也未跳过 PostgreSQL。修正后三库通过，失败原日志保留在上述首次矩阵日志中。

F-10 同类路径验证已完成：`TestChannelMonitorIncomeTaskCacheMatchesCommittedBalance` 在三库的 48 个场景通过，覆盖任务初始差额、后续结算、退款以及 Midjourney 扣款/退款，并组合提交后缓存重建、回执丢失。任务初始事务及 Midjourney 原故障矩阵也通过：[任务及 Midjourney 三库日志](D:/temp/profit-audit-20261002-task-cache-matrix.log)。整个 F-10 共新增 96 个三库缓存场景；它们是确定输入的金额契约检查，不是压力测试。

#### F-11（P2）：任务收入归属更新失败被忽略，仍然完成补扣和任务入库

关联 A-04、A-07。`service/task_billing.go` 的 `PersistTaskWithBilling` 调用 `prepareChannelMonitorIncome` 忽略其错误。模拟先在渠道 A 预扣 100、重试渠道 B 最终额度 150，最终归属更新暂时失败、之后数据库恢复：原实现返回成功、补扣 50 并将旧渠道的收入标为 settled。缺口标记虽保守阻止部分利润确认，但无法自动修复收入所归属的错误渠道。

原复现：[任务归属失败日志](D:/temp/profit-audit-20261002-attribution-before.log)。最小修正仅将该调用换成返回错误的现有入口，失败立即退出任务持久化；不增加新状态。回归确认不保存任务、不补扣、保留原 reserved 金额并可通过既有失败处理退还原预扣。最终归属及早到任务结算的正常用例仍通过。

该文件属于上游已有文件，修改前已实际比较 `git diff upstream/main -- service/task_billing.go`；修正位于此前下游新增的持久化接入点，只增加错误检查，无法通过配置或另一个注册点替代。没有改插件元信息、用量协议或价格。记录仍保守保留收入缺口，不擅自解除原故障证据。

SQLite 回归：[修正后及早到结算](D:/temp/profit-audit-20261002-attribution-after.log)；同一服务层故障场景分别设置 `TEST_PROFIT_TASK_MYSQL_DSN`、`TEST_PROFIT_TASK_POSTGRES_DSN` 指向隔离库，MySQL 5.7.44 和 PostgreSQL 9.6.24 均通过：[MySQL](D:/temp/profit-audit-20261002-attribution-mysql.log)、[PostgreSQL](D:/temp/profit-audit-20261002-attribution-postgres.log)。命令：`go test ./service -run '^TestTaskIncomeAttributionFailureDoesNotCommitTaskFunding$' -count=1 -v`。

#### A-09 / A-11：中断、人工核对、跨日与保留边界

实际命令：`go test ./model -run '^TestChannelMonitorIncome(ManualReconciliationPreservesFunds|FundingSurvivesProcessExit|GapFollowsCrossDayCost|RetentionAllowsLateTaskBilling)$' -count=1 -v`，三库 DSN 与上文一致。

人工核对的状态/金额/时间戳检查、审计失败共同回滚、原资金保持不变和重复确认拒绝在三库通过；进程退出四个阶段使用 SQLite 子进程，符合预扣已提交而无最终指令时保留资金、不确认收入的业务口径。缺口跟随最终成本日期、历史清理不阻断晚到任务退款/结算在三库通过。[本轮恢复矩阵](D:/temp/profit-audit-20261002-recovery-matrix.log)。另有 PostgreSQL 并发投影及三库准备幂等/回读故障：[归属并发矩阵](D:/temp/profit-audit-20261002-preparation-matrix.log)。

源码确认工具默认只读，写入需同时指定 input/apply；reserved 必须提交已确认原请求终止的声明。该声明需要操作者的外部核对证据，工具无法自动证明多节点上不存在活跃请求。真实共享目录、停电和灾难恢复仍属于 R 项，不以进程退出测试替代。

#### A-12：重新生成新库与发布版升级证据

本轮发现第 11 节引用的临时升级脚本以及部分历史日志已经不在磁盘上，因此没有继续把历史文字描述当成本轮可复查证据。通过 `git archive v1.0.0-rc.41` 导出仓库现有发布版（`2035a82aeb5414253a728bd937d4b8f97aa99b9b`），将现有 `scripts/channel-monitor-profit-upgrade/upgrade_test.go` 原样复制到临时发布版目录，执行种子初始化；再用当前工作区执行升级验证。

三库均完成：fresh 初始化加两次 verify；release seed 加当前版本两次 verify，共 18 个阶段全部通过。测试检查用户额度、渠道、独立日志库数据、唯一约束、索引、收入恢复字段及缺口表。SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 9.6.24，MySQL 开启 `clientFoundRows=true`。数据库只在端口 13318 / 15438 的既有隔离容器中新建，库名前缀 `new_api_profit_audit_20261002_`；SQLite 使用独立文件。

```powershell
& D:/temp/profit-audit-20261002-schema/validate.ps1 -Engine sqlite
& D:/temp/profit-audit-20261002-schema/validate.ps1 -Engine mysql
& D:/temp/profit-audit-20261002-schema/validate.ps1 -Engine postgres
```

[完整执行脚本](D:/temp/profit-audit-20261002-schema/validate.ps1)，同目录的 `{engine}-{fresh|upgrade}-{0|1|2}.log` 保留各阶段日志。本轮 F-10/F-11 未新增表结构；新的事务/失败路径已有独立三库验证，未以结构迁移测试替代资金行为测试。

#### A-13 / A-15：前端与整包回归

本轮未改前端代码。按项目前端规范及 shadcn-ui、React 技能读取共享错误状态组件和既有利润组件：利润失败提示保留旧数据、首次失败不显示伪造的零成本、重试按钮有可访问名称、请求期间禁用；共享 ErrorState 用于整块空错误状态，不能同时保留单元格的利润明细，当前组合未重复实现弹窗或通用表格。各渠道查询继续通过统一 Query key 去重；成本、收入和利润使用同一份汇总快照；未添加利润开关或翻译键。

| 本轮命令 | 结果及证据 |
|---|---|
| `go test ./model ./service -count=1` | model 88.618s、service 90.633s 通过；[日志](D:/temp/profit-audit-20261002-model-service-full.log)。该次包含 F-10，早于 F-11 |
| `go test ./controller ./relay -count=1` | controller 237.806s、relay 0.354s 通过；[日志](D:/temp/profit-audit-20261002-controller-relay-full.log)。该次包含 F-10，早于 F-11 |
| `go build ./...` | 通过；[日志](D:/temp/profit-audit-20261002-build.log)，早于 F-11 |
| `bun run test src/features/channel-monitor` | 130 文件、773 项通过；[日志](D:/temp/profit-audit-20261002-frontend.log) |
| `bun run typecheck` | 通过；[日志](D:/temp/profit-audit-20261002-typecheck.log) |
| `bun run build` | 通过；[日志](D:/temp/profit-audit-20261002-frontend-build.log) |
| `bunx oxlint -c .oxlintrc.json` 后接本提交 5 个前端文件 | 退出 0，无输出 |

尚未在真实浏览器连接已部署服务验证；不能将组件测试称为真实端到端验收。A-14 的隔离计费入口固定负载比较已开始，尚不代表完整网关吞吐。预扣的全局批次锁问题见 F-12；启用批量写入时仍会持锁直到数据库事务及 Redis 失效完成，不能声称已消除所有模式下的串行等待。

F-11 修正后 service 整包再次通过，69.301 秒：[最新 service 日志](D:/temp/profit-audit-20261002-service-final.log)。只对新增服务层改动补做相关范围验证，没有反复运行未再变更的前端测试。

最终另跑 `go test ./controller ./relay -run '^(TestKlingNativeRouteSubmitRetryPollSettleAndQuery|Test.*TaskSubmission.*|Test.*Midjourney.*)$' -count=1`：controller 通过，relay 没有匹配测试（不能记成 relay 专项通过）；[任务入口日志](D:/temp/profit-audit-20261003-task-entry-final.log)。F-11 后 `go build ./...` 退出 0：[最终构建](D:/temp/profit-audit-20261003-build-final.log)。最后执行 `git diff --stat`、`git diff --check`，无空白错误；本轮未提交、推送或部署。

`TestChannelMonitorAnalyticsReadDatabaseMatrix` 在 SQLite、MySQL 5.7.44、PostgreSQL 9.6.24 都通过，覆盖利润和成本各来源、分组过滤、缺失覆盖及系统探测：[SQLite](D:/temp/profit-audit-20261002-analytics-sqlite.log)、[MySQL](D:/temp/profit-audit-20261002-analytics-mysql.log)、[PostgreSQL](D:/temp/profit-audit-20261002-analytics-postgres.log)。使用新建专用 `new_api_cm_schema_analytics`，设置 `CHANNEL_MONITOR_CONSISTENCY_DIALECT` / `SQL_DSN` / `CM_UPGRADE_SQLITE_PATH` 后运行 `go test ./controller -run '^TestChannelMonitorAnalyticsReadDatabaseMatrix$' -count=1 -v`。

人工核对 CLI 已在本轮新建 SQLite 升级验证库实际只读运行：`go run ./cmd/channel-profit-reconcile -limit 1`，返回 `read_only: true`，未写入任何核对；[输出](D:/temp/profit-audit-20261002-reconcile-readonly.log)。

**10 月 3 日阶段结论（F-13 已被第 17 节替代）：当时 F-10/F-11/F-12/F-13 均有修正及三库专项证据；10 月 6 日撤回 F-13 数据库直读，保留缓存异常限制，不再将其计入已修复项。**

#### A-06：退款跨订阅周期及资金记录缺失（2026-10-03）

扩展现有 `model/channel_monitor_income_test.go` 的 `TestChannelMonitorIncomeRefundPreservesSubscriptionPeriodAndMissingRecords`，三库共 12 个场景通过：订阅已重置时不从新周期使用量扣除旧预扣；订阅到期仍能退款；订阅或令牌被删除时整笔退款回滚，收入保持 `refund_pending`、退款记录保持未执行；恢复缺失记录后可以准确退款且重复调用不再变更资金。证据：[三库退款边界](D:/temp/profit-audit-20261003-refund-boundary-v2.log)。首次夹具用户名超过 32 字符，改为短键后通过，未修改生产字段或校验；[失败夹具日志](D:/temp/profit-audit-20261003-refund-boundary.log) 保留。

#### F-12（P2）：关闭批量写入仍持有全局锁，阻塞无关用户预扣

关联 A-08/A-14。`ReserveChannelMonitorIncome` 在未开启批量写入时也持有用户和令牌批次全局锁。确定复现将账户 A 暂停在收入记录 INSERT 前（尚未写库），账户 B 的独立预扣仍无法进入事务，直至请求期限耗尽。该等待不是同一用户余额竞争或数据库行锁导致。[修正前证据](D:/temp/profit-audit-20261003-lock-before.log)。

最小修正只在 `common.BatchUpdateEnabled` 为真时取得原有批次锁并检查遗留批次；关闭时使用已有数据库事务及行锁。该开关在启动期设置，未发现服务运行期切换入口。未新增互斥锁、状态表或后台机制；文件为下游已有文件。

`TestChannelMonitorIncomeIndependentReservationsDoNotWaitForBatchLocks` 在 SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 9.6.24 通过，并验证开启批量写入后待落库的钱包/令牌批次仍拒绝预扣，批次落库后资金精确扣减。2 秒期限仅是防挂起保护，不是性能比较。[最终三库日志](D:/temp/profit-audit-20261003-lock-matrix-v2.log)；同时复跑预扣和退款矩阵：[日志](D:/temp/profit-audit-20261003-lock-matrix.log)。批量模式中的全局串行等待仍是保留限制。

F-12 后 `go test ./model ./service -count=1` 整包通过（model 86.254s、service 92.328s）：[日志](D:/temp/profit-audit-20261003-lock-full.log)。本轮没有新增测试文件，全部回归补在已有模型及服务测试文件内。

#### A-01：52 个文件清单

清单由本轮实际执行 `git diff HEAD^ HEAD --name-only` 和 `git ls-tree -r --name-only upstream/main` 生成；存在于上游表示归属，不表示差异已逐行通过。上游接入的必要性继续对照第 10、14、15 节，禁止把测试通过等同于必要性审查通过。

| 文件 | 类型 | 归属 |
|---|---|---|
| CHANNEL_MONITOR_PROFIT_REVIEW.md | 文档 | 下游 |
| cmd/channel-profit-reconcile/main.go | 工具 | 下游 |
| controller/channel_monitor_analytics_coverage.go | 实现 | 下游 |
| controller/channel_monitor_analytics_read_matrix_test.go | 测试 | 下游 |
| controller/channel_monitor_analytics_system_probe_test.go | 测试 | 下游 |
| controller/channel_monitor_consistency_matrix_test.go | 测试 | 下游 |
| controller/channel_monitor_profit.go | 实现 | 下游 |
| controller/channel_monitor_profit_coverage.go | 实现 | 下游 |
| controller/channel_monitor_profit_test.go | 测试 | 下游 |
| controller/midjourney.go | 实现 | 上游已有 |
| controller/plugin_native_e2e_test.go | 测试 | 上游已有 |
| controller/relay.go | 实现 | 上游已有 |
| docs/downstream/channel-monitor/README.md | 文档 | 下游 |
| docs/downstream/channel-monitor/profit.md | 文档 | 下游 |
| model/channel_daily_cost_outbox.go | 实现 | 下游 |
| model/channel_local_response_refund.go | 实现 | 下游 |
| model/channel_monitor_income.go | 实现 | 下游 |
| model/channel_monitor_income_concurrency_test.go | 测试 | 下游 |
| model/channel_monitor_income_gap.go | 实现 | 下游 |
| model/channel_monitor_income_journal.go | 实现 | 下游 |
| model/channel_monitor_income_parity_test.go | 测试 | 下游 |
| model/channel_monitor_income_reconcile.go | 实现 | 下游 |
| model/channel_monitor_income_test.go | 测试 | 下游 |
| model/channel_monitor_reservation.go | 实现 | 下游 |
| model/main.go | 实现 | 上游已有 |
| model/midjourney.go | 实现 | 上游已有 |
| model/midjourney_billing.go | 实现 | 下游 |
| model/subscription.go | 实现 | 上游已有 |
| model/task_billing.go | 实现 | 下游 |
| model/user.go | 实现 | 上游已有 |
| relay/mjproxy_handler.go | 实现 | 上游已有 |
| scripts/channel-monitor-profit-upgrade/upgrade_test.go | 测试 | 下游 |
| service/billing.go | 实现 | 上游已有 |
| service/billing_session.go | 实现 | 上游已有 |
| service/channel_daily_cost.go | 实现 | 下游 |
| service/channel_daily_cost_recovery_test.go | 测试 | 下游 |
| service/channel_local_response_billing.go | 实现 | 下游 |
| service/channel_monitor_income.go | 实现 | 下游 |
| service/channel_monitor_income_test.go | 测试 | 下游 |
| service/channel_monitor_profit_queue.go | 实现 | 下游 |
| service/channel_monitor_reservation.go | 实现 | 下游 |
| service/midjourney.go | 实现 | 上游已有 |
| service/quota.go | 实现 | 上游已有 |
| service/task_billing.go | 实现 | 上游已有 |
| service/task_billing_test.go | 测试 | 上游已有 |
| service/task_channel_cost_test.go | 测试 | 下游 |
| service/violation_fee.go | 实现 | 上游已有 |
| web/src/features/channel-monitor/components/__tests__/execution-records-dialog.fixture.tsx | 测试 | 下游 |
| web/src/features/channel-monitor/components/__tests__/profit.test.tsx | 测试 | 下游 |
| web/src/features/channel-monitor/components/channel-monitor-profit.tsx | 实现 | 下游 |
| web/src/features/channel-monitor/index.tsx | 实现 | 下游 |
| web/src/features/channel-monitor/types-analytics.ts | 实现 | 下游 |

#### A-01：上游文件接入必要性结论

本提交的上游生产文件变更均位于现有计费或任务接入点：`controller/relay.go` 与 `service/task_billing.go` 使任务插入和初始差额共同提交；`service/billing.go`、`service/billing_session.go` 接入持久预扣和最终结算所有权；`model/subscription.go` 允许既有订阅预扣使用调用方事务并拒绝复用旧回执；`model/user.go` 避免监控就绪后钱包扣款留在内存批次。`model/main.go` 注册新增缺口表。`model/midjourney.go`、`controller/midjourney.go`、`service/midjourney.go` 防止轮询覆盖资金字段并继续处理终态未退款任务；`relay/mjproxy_handler.go` 删除已转入资金事务的重复用量累计。`service/quota.go`、`service/violation_fee.go` 覆盖绕过普通最终结算入口的实时及违规费用。这些位置没有可替代现有调用的注册钩子，分离实现留在下游文件，未复制整段上游处理器。

上游测试文件 `controller/plugin_native_e2e_test.go` 补齐真实入口需要的成本事件表；`service/task_billing_test.go` 将部分扣款预期调整为共同回滚，同时保留余额、令牌、用量和日志断言，符合资金原子性要求。其余测试夹具、下游报表、缺口覆盖、核对工具和说明分别对应 F-01 至 F-09 及 V 项，没有发现为通过测试而放宽资金校验。52 文件清单是原提交规模；本轮未提交修改为 12 个文件。上游生产文件包括 `service/task_billing.go` 的 F-11 错误检查，以及 `model/user.go`、`model/user_cache.go`、`model/token.go` 的 F-13 额度读取条件；另外更新已有功能说明，未增加表或后台任务。

#### A-04 / A-05：预扣、最终指令与恢复状态表

| 起点 → 终点 | 入口及所有者 | 资金约束 |
|---|---|---|
| 无记录 → `reserved` | `ReserveChannelMonitorIncome`，当前请求 | 预扣、令牌、记录共同提交；重复请求标识拒绝，不再次调用上游 |
| `reserved` → `reserved` | `AdjustChannelMonitorReservation` / 归属准备，当前请求 | 旧额度必须匹配；补扣失败共同回滚；提交不明保留实际预扣，停止继续请求 |
| `reserved` → `funding_pending` | 普通 `SettleBilling` 保存最终指令 | 保存目标及差额，核对资金来源/令牌/订阅；交接后当前会话不得另行退款 |
| `funding_pending` → `settled` | 当前请求或收入恢复任务 | 收入行锁后提交资金、令牌和确认；重复执行不再变更资金 |
| `reserved` → `settled` | `InsertTaskWithBilling` | 任务入库、初始差额和收入共同提交，之后由任务行控制终态修正 |
| `reserved` → `refund_pending` → `settled`（0） | 本地响应/失败退款队列 | 队列先接管；实际退款和 `Applied`、关闭预扣共同提交 |
| `pending` → `funding_pending` → `settled` | 兼容入口保存明确最终差额 | 无指令的旧 `pending` 不由后台猜测扣费 |
| `pending` / `settled` → `refund_unfunded` / `refund_pending` | 旧兼容及 Midjourney 退款标记 | 退款先阻止利润确认；失败恢复原标记，终态任务继续重试 |
| `pending` / `reserved` → `settled` | 人工核对 CLI | 必填证据、旧快照及终止声明；仅修正监控，不改资金 |

依据：模型预扣矩阵覆盖过期快照、重复标识、提交回执丢失；`TestChannelMonitorIncomeReservedSessionLifecycle` 覆盖补扣不足的 403、渠道重试、零预扣、已入队退款、最终指令无法确认后的停止行为；`TestChannelMonitorIncomeRecoveryOwnsFailedFinalSettlement` 覆盖等额、返还和补扣及实际系统任务入口。它们包含在本轮已通过的模型/服务整包与专项中。数据量为零不跳过关闭预扣。普通直接调用 `BillingSession.Settle` 会拒绝持久预扣会话，必须通过已核对入口完成。

后台每分钟运行，资金和成本各最多 100 条，共用 45 秒上下文；失败行更新时间后排到后面。资金恢复只选 `funding_pending`，每笔重新锁收入行；系统任务领取和账本幂等分别保护调度和资金。没有搭建两个完整网关节点做持续抢占实验，不能把模型行锁验证称为多节点部署验证。批量模式及本地退款沿用进程级批次锁，互斥等待本身不受 context 取消，这个运行边界仍需保留。

2026-10-03 补充实际验证：`TestChannelMonitorIncomeRecoveryFairnessAndConcurrentWorkers` 使用查询完成屏障，让两个恢复执行者都读到同一条 `funding_pending` 指令后再同时进入结算；SQLite 3.50.4 / MySQL 5.7.44 / PostgreSQL 9.6.24 钱包、令牌和收入都仅提交一次。同一测试还确认缺失令牌的最旧失败记录会回滚钱包并轮转，让下一批正常记录完成；已取消上下文不改变资金。命令：设置 `TEST_COST_BACKLOG_MYSQL_DSN` / `TEST_COST_BACKLOG_POSTGRES_DSN` 后运行 `go test ./model -run '^TestChannelMonitorIncomeRecoveryFairnessAndConcurrentWorkers$' -count=1 -v`。[三库证据](D:/temp/profit-audit-20261003-recovery-workers.log)。这验证数据库并发恢复契约，不替代完整节点调度部署测试；没有新增生产逻辑或测试文件。

#### F-13（P2，原直读方案已撤回，当前限制见第 17 节）：Redis 删除失败后仍读取已过期的资金缓存

关联 A-08。F-10 的失效策略解决了正常删除及提交不确定场景下的重复加减，但 `invalidateChannelMonitorFundingCache` 在 `DEL` 失败时只记录日志。结算重入发现已经 `settled` 后直接返回，不会再次尝试失效。Redis 恢复可读而保留旧缓存时，显示和准入判断可能继续使用旧余额，直到缓存过期或被清除。缓存 miss 后延迟写回旧快照也是尚未验证的相邻边界，不能宣称 F-10 已完全解决所有缓存交错。

本轮使用独立 Redis **8.10.1**（`127.0.0.1:16389`，DB 13）及 PostgreSQL **9.6.24** 新库 `new_api_profit_cache_failure_20261003`，只对实验 Redis 创建临时 ACL 用户禁止 `DEL`，其他读命令正常。实际入口 `SettleChannelMonitorIncomeFunding` 返还 50 后：数据库由 9900 变为 9950，缓存仍 9900；重复结算后数据库仍 9950；恢复管理连接并删除缓存后，读取返回 9950。没有证明数据库重复扣款，确认的是删除失败后的缓存恢复缺口。

复现命令：`go run D:/temp/profit-audit-20261003-performance/redis-failure.go`。[脚本](D:/temp/profit-audit-20261003-performance/redis-failure.go)、[实测日志](D:/temp/profit-audit-20261003-redis-failure.log)。ACL 用户由脚本清理；未连接业务 Redis 6379。此实验是确定的命令失败注入，不是网络分区/Redis 重启的完整灾难恢复验证。

**修正：** 采用直接读取已提交额度的最小方式，不新增表、缓存恢复队列或账本。`ChannelMonitorIncomeReady` 为真时，`GetUserQuota` 直接读数据库；`GetUserCache` 保留原有缓存身份/版本检查，再用数据库额度填充返回值；`GetTokenByKey` 命中缓存后，只从同一 ID、用户和 Key 的数据库记录读取剩余/已用额度。数据库错误不能回退到旧额度；记录已删除时返回原有不存在错误。未就绪时保留原来的缓存路径。代价是缓存命中时增加数据库额度查询，缓存仅保留非额度信息的作用。

修改前实际执行 `git diff upstream/main -- model/user.go model/token.go model/user_cache.go`，三者均属于上游文件；没有能保证所有用户余额和令牌额度读取都经过下游计费函数的钩子，因此在三个现有读取入口分别加窄条件，未改身份验证状态、到期时间或版本屏障。功能说明更新于既有 `docs/downstream/channel-monitor/profit.md`。

`TestChannelMonitorIncomeBalancesIgnoreStaleFundingCache` 在 SQLite / MySQL 5.7.44 / PostgreSQL 9.6.24 通过，覆盖 Redis 清理失败、清理后旧快照重新入缓存、正向缓存与已耗尽真实令牌冲突、令牌已删除、数据库读取失败；同时复跑 F-10 的普通和任务缓存矩阵：[三库证据](D:/temp/profit-audit-20261003-cache-authority.log)。真实 Redis 禁止 `DEL` 的实验使用新库重新执行，读取从原来的 9900 修正为 9950，重复结算仍不重复返还：[修正后日志](D:/temp/profit-audit-20261003-redis-failure-fixed.log)。该日志里的 `cache` 标签指公开额度读取返回值，不是声称 Redis 中的旧 hash 已删除。

因令牌额度读取位于认证链路，修改前阅读 OWASP [Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)、[Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)，并从官方 ASVS 页面确认最新稳定版为 **5.0.0**。适用约束：服务端校验、数据库失败时拒绝继续使用旧凭据数据、保留失效/过期验证和用户版本屏障、日志不记录可用凭据；本修改没有增加凭据或会话生命周期。新增测试检查耗尽、删除及读取失败不能被旧缓存绕过，已有认证/中间件回归随整包检查。未声称整站已通过 ASVS 全量认证。

另外运行既有 `TestChannelSmallInputResponseRefundDatabase`，设置 `TEST_PROBE_POLICY_REDIS_ADDR=127.0.0.1:16389`，以及指向专用 `new_api_profit_refund_20261003` 的两种数据库 DSN；SQLite / MySQL 5.7.44 / PostgreSQL 9.6.24 全通过，验证本地退款缓存 Lua 及重复执行：[真实 Redis 三库日志](D:/temp/profit-audit-20261003-real-redis.log)。本地退款有独立的缓存幂等标记，不能用其通过来替代 F-13 路径。

#### A-10 / R-05：金额边界的实际结果

单笔收入先校验 `0..common.MaxQuota`，使用 decimal 按保存的每单位额度、1:1 比例换算，并拒绝超过 `int64` 的纳元金额。三库统计 SQL 的 `SUM` 被扫描到 `int64`。临时只读 SQL 实验 `go run D:/temp/profit-audit-20261003-performance/numeric.go` 结果：三库都精确返回 9007199254740993；总和达到 9223372036854775808 时，SQLite 报整数溢出，MySQL / PostgreSQL 报扫描超出 int64，未静默变成负数。[三库日志](D:/temp/profit-audit-20261003-numeric.log)。控制器传播该错误，不会把失败汇总作为已确认零利润。

API 仍返回 JSON 数字，前端仍用 JS number；`bun` 实测 `JSON.parse("9007199254740993")` 得到 9007199254740992：[日志](D:/temp/profit-audit-20261003-js-number.log)。约 **900.72 万元** 后不保证纳元整数精确；该示例仅损失 1 纳元，不能说分位金额已经错误。约 **92.23 亿元** 是单个正向 int64 汇总的上限，超出会查询失败。未改成字符串或 BigInt，也没有声称支持无限金额/无限区间；R-05 保留金额范围与精度限制。利润并不是资金结算依据。

#### A-11：清理与降级结论的范围

已复核：缺口 reader 超过 10000 项或任一坏文件时保守返回错误；清理按 128 项分页独立处理，不调用受限 reader，坏证据保留；数据库保留边界先推进，已过期报表不再确认。收入清理在查询和 DELETE 条件两处排除 `reserved` / `funding_pending`；退款需求保存在独立退款记录，任务资金事实保存在任务行，因此删除过期报表不等于丢失退款需求。outbox 删除两次检查未投影收入依赖。已有坏文件/10001 项/重启重跑、晚到任务、跨日归属回归通过。

仍未实测共享挂载分区、真正断电和两个独立网关进程同时执行清理/恢复，保留 R-01 至 R-04，不写成完整灾难恢复通过。没有为补齐核查增加新的日志协议或清理架构。

#### A-14：固定负载比较（隔离计费入口）

使用 PostgreSQL 9.6.24、Go 1.26.5、每组 120 次 `PreConsumeBilling(100) → SettleBilling(50)`，各工作者使用独立钱包和令牌，关闭 Redis、批量写入、消费日志。每个版本使用新建 `new_api_profit_bench_{parent|committed|current}_v3`，相同临时 [脚本](D:/temp/profit-audit-20261003-performance/main.go)，三个版本依次运行：父提交 `f4baf15c4`、已提交 `30c7db2e1`、当前 F-10/F-11/F-12 工作区。所有组失败 0，最终钱包总额精确断言通过。

| 版本 | 并发 | 总耗时 ms | p50 / p95 / p99 ms | SQL 次数 / 写语句次数 | 事务次数 / 累计事务 ms |
|---|---:|---:|---|---|---|
| 父提交 | 1 | 3196.271 | 26.085 / 28.275 / 29.744 | 1320 / 720 | 840 / 2983.138 |
| 已提交 | 1 | 3007.985 | 24.202 / 28.074 / 28.178 | 2520 / 960 | 600 / 2740.232 |
| 当前工作区 | 1 | 3311.393 | 27.648 / 32.507 / 44.828 | 2520 / 960 | 600 / 3016.756 |
| 父提交 | 8 | 762.231 | 48.452 / 62.032 / 71.712 | 1320 / 720 | 840 / 4143.126 |
| 已提交 | 8 | 1152.217 | 74.307 / 82.244 / 86.082 | 2520 / 960 | 600 / 3064.260 |
| 当前工作区 | 8 | 629.625 | 37.403 / 61.837 / 65.825 | 2520 / 960 | 600 / 4374.315 |

每笔 SQL 尝试从 **11 → 21**，写语句尝试 **6 → 8**，顶层事务 **7 → 5**。包含为拒绝重复创建而执行的失败 INSERT，不能把写语句次数称为已提交写入次数；数据库日志可见预期的唯一键冲突。事务时间从 BeginTx 调用至 Commit/Rollback 返回累计，并发时会重叠，不能直接与墙钟总耗时相减。日志：[父提交](D:/temp/profit-audit-20261003-performance/parent-v3.log)、[已提交](D:/temp/profit-audit-20261003-performance/committed-v3.log)、[工作区](D:/temp/profit-audit-20261003-performance/current-v3.log)。

连接配置 `log_lock_waits=on`、`deadlock_timeout=1ms`；[数据库日志](D:/temp/profit-audit-20261003-performance/postgres-lock-v3.log) 没有匹配到等待超过阈值的锁日志，不等于锁等待绝对为零，也看不到 Go 互斥锁等待。进程锁阻塞由 F-12 的确定交错用例独立证明。

这是一次固定小样本；先前未加事务计时的探索日志仍保留。F-12 后本样本并发等待下降，但单请求延迟有抖动，不能宣称普遍提速或零性能影响。未做 HTTP、上游、同账户热点、批量模式、真实 Redis 延迟或线上规模负载，A-14 保留这些未验证项。

F-13 后单独量化缓存命中时的新增读取：MySQL 5.7.44 + Redis 8.10.1，每组依次调用 `GetUserCache`、`GetUserQuota`、`GetTokenByKey`，120 组，全部读取准确。未就绪的原缓存路径 SQL 0 次，总耗时 292.318 ms，p50/p95 2.595/2.814 ms；就绪后 SQL 360 次，总耗时 557.344 ms，p50/p95 4.690/4.943 ms。即这三个入口各多一次额度查询；不是每个 HTTP 请求固定增加三个查询，实际取决于调用链。没有宣称 Redis 故障时整个网关具备同样延迟。[实验脚本](D:/temp/profit-audit-20261003-performance/cache-reads.go)、[日志](D:/temp/profit-audit-20261003-cache-read-cost-v2.log)。首次临时脚本未初始化 SQL 保留字引用，后改为使用从节点 `InitDB` 正常初始化；[失败日志](D:/temp/profit-audit-20261003-cache-read-cost.log) 保留，未改变生产 SQL 适配。

#### A-15：本轮核查交付与限制

F-13 后 `go test ./model ./service ./middleware -count=1` 通过（model 90.118s、service 91.545s、middleware 2.762s）：[最终整包日志](D:/temp/profit-audit-20261003-authority-full.log)；`go build ./...` 退出 0：[最终构建](D:/temp/profit-audit-20261003-authority-build.log)。三库专项见前文。前端没有再次修改，沿用本轮已经实际完成的 773 项测试、类型检查和构建；没有重复运行同一组验证。`git diff --check` 无空白错误。未提交、推送、部署；隔离实验只在专用端口和新建测试库执行。

10 月 3 日已逐项核查并记录 A-01 至 A-15，当时 F-10/F-11/F-12/F-13 修正均有复核；F-13 方案于 10 月 6 日撤回，当前结论见第 17 节。资金事务、退款及恢复在明确测试环境通过；不能把它解读为“不存在未知问题”或“线上部署已经验收”。仍保留 R-01 至 R-05：真实共享目录挂载/权限漂移、数据库和文件系统同时丢失、Redis 数据丢失及恢复、完整节点强制中断/跨日 WebSocket、线上规模负载。代码没有擅自替用户选择新的崩溃退款政策，也没有添加利润开关。

## 17. 按用户要求恢复官方缓存读取（2026-10-06）

用户强调“尽量不要影响用户请求速度”，并要求使用官方方式。此前将 F-13 的异常处理扩大成正常读取时查询数据库，违背了这个性能约束。现已撤回 `GetUserQuota`、`GetUserCache`、`GetTokenByKey` 三个入口的额外 SQL 查询，恢复仓库官方 `upstream/main`（`c2b7a9a9e`，2026-09-25）的缓存优先实现；该日期是本地上游基线，不冒充远端最新版本。

缓存命中直接返回；未命中或 Redis 读取失败时使用原有数据库回源及缓存填充；显式要求数据库读取的调用仍按原有参数执行。`model/token.go`、`model/user_cache.go`、`model/user.go` 与本轮开始时 HEAD 已无工作区差异，仅删除本轮自己的改动，未撤销仓库已有钱包大额度或持久写入行为。身份状态、认证版本屏障、令牌到期等官方检查保持原样。

F-10 保留结算后缓存失效，不直接恢复已证明会重复叠加的结算后缓存差额写入；普通请求、任务和 Midjourney 的资金事务仍由数据库共同提交。这意味着**恢复的是官方额度读取方式，不是把整个下游计费系统改回官方**。正常失效后的下一次读取仍需数据库回源，预扣和结算本身仍有数据库写入，不能宣称利润记账完全没有请求成本。

### F-13 的当前状态

恢复缓存优先后，Redis 可读但删除失败，或者持有旧快照的读者在删除后重新填充缓存时，额度仍可能短暂滞后；这项限制没有被修复，不能以“官方也是缓存”证明其不存在。沿用现有错误日志及缓存到期/后续失效机制，不新增后台队列、数据库字段或每次查询。TTL 由既有配置决定，部分缓存写入会刷新 TTL，因此不能承诺故障后一律在固定 60 秒内恢复。

撤回仅影响读取。已提交资金不会因缓存失败或重复结算再次扣退，正常缓存失效路径也不再给重建后的余额重复叠加差额。前述 F-13 数据库直读测试和性能结果保留为历史证据，不能用于证明当前版本立即读取强一致余额。

### 本次验证

在已有测试文件中将原 `TestChannelMonitorIncomeBalancesIgnoreStaleFundingCache` 改为 `TestChannelMonitorIncomePreservesCacheFirstReads`：监控就绪、缓存已命中时对所有 GORM 查询注入错误，三个公开读取入口及令牌验证仍成功，证明没有偷偷查询数据库；保留删除失败和旧快照写回的原复现；推进模拟 Redis 时间验证到期后回源返回真实额度；Redis 读失败也能回源，重复结算不重复退款。没有靠删除失败分支或伪造正确缓存来掩盖 F-13。

设置隔离 `TEST_COST_BACKLOG_MYSQL_DSN` / `TEST_COST_BACKLOG_POSTGRES_DSN` 后执行 `go test ./model -run '^TestChannelMonitorIncome(PreservesCacheFirstReads|FundingCacheMatchesCommittedBalance|TaskCacheMatchesCommittedBalance)$' -count=1 -v`：SQLite 3.50.4 / MySQL 5.7.44 / PostgreSQL 9.6.24 全部通过；同时覆盖 F-10 普通/任务/Midjourney 的全部缓存矩阵。[三库日志](D:/temp/profit-audit-20261006-cache-first-matrix.log)。

真实 Redis 8.10.1（16389，DB 11）+ MySQL 5.7.44（13318，新建 `new_api_profit_cache_read_20261006`）小样本：监控未就绪/就绪各 120 组 `GetUserCache → GetUserQuota → GetTokenByKey`，暖缓存 SQL 均为 **0**；p50 为 2.657 / 2.666 ms，p95 为 2.830 / 3.116 ms。此结果只证明没有额外额度 SQL，不是完整网关吞吐结论。[脚本](D:/temp/profit-audit-20261006-cache-reads.go)、[日志](D:/temp/profit-audit-20261006-cache-read-cost.log)。未访问业务 Redis 6379 或 MySQL 3306。

最终 `go test ./model ./service ./middleware -count=1` 全部通过（91.043s / 94.197s / 3.890s）：[整包日志](D:/temp/profit-audit-20261006-cache-first-full.log)；`go build ./...` 退出 0：[构建日志](D:/temp/profit-audit-20261006-cache-first-build.log)。本次未新增生产逻辑、依赖或表结构。检查 `git diff --stat`、`git diff --check`，并确认三个读取文件无未提交差异。剩余未提交文件为 9 个，其中上游生产文件仅 `service/task_billing.go` 的 F-11 错误检查；未提交、推送或部署。

## 18. 再次复查结果（2026-10-07）

**结论：不能确认“所有问题都改完”。** F-10/F-11/F-12 的现有修正及官方缓存优先读取通过本次专项验证；F-13 按第 17 节仍是未消除的一致性限制。本次首次整包回归出现后台分钟聚合租约测试失败，随后单例及 service 整包重跑通过，但根因尚未确认，记为 V-05 未关闭项。本次没有继续修改生产代码或测试断言，没有提交、推送或部署。

### 改动边界复核

重新检查现有生产差异：普通、任务、Midjourney 资金提交后失效缓存；任务归属准备失败向调用方返回错误；仅启用批量模式时获取预扣全局批次锁。`git diff --exit-code HEAD -- model/user.go model/user_cache.go model/token.go` 返回 0，三个读取文件无本轮未提交改动，缓存命中路径没有重新加入额度 SQL 查询。现有 9 个未提交文件中，上游生产文件仍只有 `service/task_billing.go`，原因是现有任务持久化入口必须传播归属准备失败，不能继续补扣和插入任务。

本结论不代表整个下游计费与官方一致，也不代表请求零性能成本：预扣/结算仍写数据库；缓存删除仍是同步 Redis 操作，使用 3 秒上下文超时；失效后回源以及批量模式的锁等待仍可能影响延迟。

### 本次实际验证

测试只使用隔离数据库 MySQL 5.7.44（13318）、PostgreSQL 9.6.24（15438）及 SQLite 3.50.4；两种服务端数据库均使用 `new_api_cost_backlog_test`。未访问业务 MySQL 3306 或 Redis 6379。

| 验证 | 命令与结果 | 证据 |
|---|---|---|
| 资金、缓存、批次锁、恢复并发、订阅退款三库专项 | 配置 `TEST_COST_BACKLOG_MYSQL_DSN` / `TEST_COST_BACKLOG_POSTGRES_DSN` 后运行 `go test ./model -run '^TestChannelMonitorIncome(PreservesCacheFirstReads\|FundingCacheMatchesCommittedBalance\|TaskCacheMatchesCommittedBalance\|IndependentReservationsDoNotWaitForBatchLocks\|RecoveryFairnessAndConcurrentWorkers\|RefundPreservesSubscriptionPeriodAndMissingRecords)$' -count=1 -v`；三库通过，16.891s | [日志](D:/temp/profit-audit-20261007-matrix.log) |
| F-11 任务归属失败原子性 | 分别配置 `TEST_PROFIT_TASK_MYSQL_DSN`、`TEST_PROFIT_TASK_POSTGRES_DSN`，顺序执行 `go test ./service -run '^TestTaskIncomeAttributionFailureDoesNotCommitTaskFunding$' -count=1 -v`；MySQL 3.298s、PostgreSQL 2.831s，均通过 | [MySQL](D:/temp/profit-audit-20261007-attribution-mysql.log)、[PostgreSQL](D:/temp/profit-audit-20261007-attribution-postgres.log) |
| 首次整包回归 | `go test ./model ./service ./middleware -count=1`；model 92.565s、middleware 3.788s 通过；service 94.371s 失败，见 V-05 | [完整原始日志](D:/temp/profit-audit-20261007-full.log) |
| V-05 单例重跑 | `go test ./service -run '^TestRepairChannelMonitorDirtyMinutesRenewsLeaseDuringSlowRebuild$' -count=1 -v`；通过，5.248s | [日志](D:/temp/profit-audit-20261007-lease-focused.log) |
| service 独立整包重跑 | `go test ./service -count=1`；通过，68.216s | [日志](D:/temp/profit-audit-20261007-service-recheck.log) |
| 构建 | `go build ./...`；退出 0 | [日志](D:/temp/profit-audit-20261007-build.log) |

以上使用 Go 1.26.5。`git diff --check` 及已修改 Go 文件的 gofmt 检查通过。此次未修改模型结构、迁移、驱动或前端，未重复运行历史迁移矩阵和前端验证；第 16 节部署及规模验证边界仍保留。

### V-05：后台分钟聚合租约回归偶发失败（未关闭）

首次整包回归中，`TestRepairChannelMonitorDirtyMinutesRenewsLeaseDuringSlowRebuild` 在 `service/channel_monitor_aggregation_test.go:244` 失败：重建返回后，待修复分钟记录预期为 0，实际为 1。附近日志显示 SQLite `SQLITE_BUSY`，随后重建完成，而一条租约续期 UPDATE 被 `interrupted`。该测试使用独立临时 SQLite 主库和日志库，主动阻塞日志查询 3 秒，租约为 2 秒、续期间隔 100 毫秒。

源码中后台续期和完成清理并发执行；`CompleteChannelMonitorDirtyMinutes` 对租约已过期或标记再次变化的记录会保留并释放，供后续重试。因此需进一步定位续期、SQLite 写锁与完成清理的具体交错，不能只把失败归咎于测试机器负载或其他测试进程。单例和独立整包均通过，只能说明本次未再次复现，不能证明问题已经修复。

这项失败直接证明的是后台聚合一次执行后仍有待修复记录，可能导致后续重复重建或统计更新延后；本次没有发现由它造成资金重复扣退的证据，也没有证实线上必然发生。涉及的聚合生产代码及该测试均无本次工作区改动。保留失败日志，尚未修改租约实现、放宽断言或延长测试租约来掩盖失败。

## 19. 提交后的完整链路复查（2026-10-07）

基线为 `1871733bc`。本轮开始工作区干净；复核从普通请求预扣、最终指令、后台恢复、异步任务、Midjourney 退款，到成本归属、历史清理、报表和前端展示。**新增确认 F-14（P1）一项；已有 V-05 仍未关闭并补充受控复现；F-13 继续保留。** 本轮只更新本记录，没有修改生产代码、认证逻辑、前端或正式测试断言，也没有提交、推送或部署。

### F-14（P1，未修复）：旧周期订阅结算返还会冲减新周期用量

位置：`model/task_billing.go:412` 的 `applyTaskFundingDelta`，尤其 425–445 行的订阅差额更新；普通结算从 `model/channel_monitor_income.go:432` 附近调用该函数。它只读取当前订阅 `AmountUsed`，对负差额直接减去并截断到零，没有确认预扣所属周期。与之相比，`model/channel_local_response_refund.go:214` 的本地退款明确检查 `LastResetTime <= record.CreatedAt`，避免旧周期退款影响新周期。

本轮通过真实公开模型入口复现以下顺序，不直接调用内部算术函数：

1. `ReserveChannelMonitorIncome` 对订阅预扣 100，令牌从 1000 减为 900。
2. `PrepareChannelMonitorIncome` 保存最终费用 50、返还差额 -50 的 `funding_pending` 指令。
3. 将订阅夹具推进到新周期（`LastResetTime` 晚于原预扣回执），新周期已有其他请求用量 25。
4. 调用 `RecoverChannelMonitorIncomeFunding`，并再调用一次检查幂等性。
5. **实际：新周期用量 25 → 0；期望：仍为 25。** 令牌剩余 950、收入额度 50、收入状态 `settled`；第二次恢复未重复执行。这不是重复退款，而是一次退款使用了错误的周期。

SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 9.6.24 均得到同一失败断言。可能给用户额外释放新周期订阅额度，使订阅消费与收入记录不一致。普通长请求或最终结算延迟至重置后即可触发这条链路，不要求用户主动并发攻击。异步任务及其他调用共用该差额函数，需要修复时一起核对，但本轮的运行证据只证明上述普通最终结算恢复场景；没有证明所有任务场景或实际线上损失。测试未补齐成本，不声称已通过 HTTP 观察到整行已确认利润错误。

本地缓存读取方式与此问题无关，不能靠恢复每次查数据库修复。修复应明确预扣/调整所属周期，再处理旧周期负差额；还需验证跨周期补扣、任务回调、补充预扣和重复恢复，避免破坏用户既定的中断预扣政策。本轮未实施修复。`model/task_billing.go` 是下游文件；上游 `PostConsumeUserSubscriptionDelta` 也采用当前订阅差额更新，不能仅以“官方方式”证明周期安全，也不应将该问题误归因于最近缓存失效提交。

复现使用 Go overlay 将临时用例附加到已有模型测试文件，工作区测试文件未改动：

- [复现源文件](D:/temp/profit-audit-20261007-overlay-income-test.go)（新增 `TestAuditProfitSettlementAfterSubscriptionReset`）。
- [overlay 配置](D:/temp/profit-audit-20261007-overlay.json)。
- 命令：设置下述模型三库 DSN 后，`go test -overlay D:/temp/profit-audit-20261007-overlay.json ./model -run '^TestAuditProfitSettlementAfterSubscriptionReset$' -count=1 -v`。
- [三库失败日志](D:/temp/profit-audit-20261007-reset-repro-three-db.log)。首次实验只有 SQLite 完成，原隔离容器在主回归后已不在 Docker 列表，MySQL/PostgreSQL 连接拒绝；该日志保留为 [首次结果](D:/temp/profit-audit-20261007-reset-repro.log)，未把连接失败算作业务缺陷。随后在新隔离容器完成三库复现。

### V-05 补充：续期未完成时，过期租约使已重建分钟再次等待处理

原测试本轮随 service 整包通过，前轮失败仍保留。进一步阅读后确认：`service/channel_monitor_aggregation.go:350` 附近在完成重建后直接调用清理；续期协程独立运行；`model/channel_monitor_dirty_minute.go:522` 清理只删除 `claimed_until > now` 的记录，否则保留并释放领取；主流程最后取消续期并返回。

使用与原用例相同的独立 SQLite 主库/日志库及 2 秒租约、100 毫秒续期间隔，在续期 UPDATE 前设置屏障，让日志查询跨过租约期限，直到完成 DELETE 尝试才放行续期。结果重建成功、函数返回成功，但脏分钟数仍为 1，续期被取消并报告 `interrupted`，与原失败的结果一致。这给出了可产生原失败结果的具体交错，尚不能证明原日志的每个内部时序完全相同。

- [overlay 源文件](D:/temp/profit-audit-20261007-overlay-lease-expired-test.go)、[配置](D:/temp/profit-audit-20261007-overlay-lease-expired.json)。
- 命令：`go test -overlay D:/temp/profit-audit-20261007-overlay-lease-expired.json ./service -run '^TestAuditDirtyMinuteCompletionBeforeBlockedRenewal$' -count=1 -v`。
- [复现失败日志](D:/temp/profit-audit-20261007-lease-expired-barrier.log)：3.43 秒用例内重建成功、预期 0 实际 1。
- 对照试验只阻塞续期、不跨过租约时通过：[日志](D:/temp/profit-audit-20261007-lease-barrier.log)。另一次尝试在重建事务中通过第二连接直接改过期时间，因测试自身 SQLite 锁冲突而终止，不作为产品缺陷证据：[探索日志](D:/temp/profit-audit-20261007-lease-explicit-expiry.log)。最终保留的复现源恢复为原有慢查询方式，没有向仓库加入依赖 sleep 的新测试。

影响是后台统计重复重建或延迟；未证明资金重复扣退。生产租约为 2 分钟，实验缩短为 2 秒，因此不推断线上频率或通常延迟。该项属于渠道监控共享聚合路径，不能据此称利润收入/成本账本已经算错。仍未修复，继续沿用 V-05，不重复计为新的资金缺陷。

### 范围与结果矩阵

| 链路 | 本轮核查 | 结论 |
|---|---|---|
| 金额与收入口径 | 保存的额度单位、1:1 换算、单笔边界、累计余额、安全加减与 SQL 汇总 | 既有金额、三库 parity 回归通过；前述纳元精度与总和边界仍在 |
| 普通预扣/补扣/结算 | 事务边界、状态转换、提交不确定、重复请求、缓存失效、非批量锁 | 现有三库专项通过；新增跨周期 F-14 失败 |
| 普通失败退款 | 持久退款接管、重复退款、订阅回执和周期、缺失令牌/订阅、缓存幂等 | 既有回归通过；不能用本地退款周期测试代表最终结算周期安全 |
| 异步任务 | 初始差额与入库原子性、归属失败、终态校正、早到回调、历史过期后扣退 | 源码与既有回归通过；共用订阅差额函数列入 F-14 修复范围 |
| Midjourney | 初始扣费、终态退款重试、旧快照与重复回调 | 现有三库及服务回归通过 |
| 实时/违规收费接入 | 独立结算标识、最终指令持久化、共同成本归属、错误传播 | 源码及整包回归，未连接真实供应商或运行跨午夜 WebSocket |
| 恢复及人工核对 | 只恢复明确指令、两执行者幂等、失败轮转、取消、人工证据与快照检查 | 三库专项通过；不改变保留孤立预扣待人工核对的政策 |
| Redis 与批次 | 缓存命中、失效后重建、删除失败、旧快照回填、批次锁 | F-10/F-12 回归通过；F-13 未消除；本轮未新增请求 SQL |
| 成本及覆盖 | 收入/成本并发投影、缺口跨日归属、队列/死信不可读、保留期边界 | 模型、服务、控制器回归；不可确认时保持待确认 |
| 文件故障与清理 | 坏文件、目录不可用、超过上限、分批清理、保留资金指令 | 故障回归通过；真实共享盘与双重存储损坏未新增部署验证 |
| 报表查询 | 一致性快照、成本单边/收入单边、汇总分页、仅亏损、逐日覆盖与下钻 | 真实 SQLite/MySQL/PostgreSQL 控制器矩阵通过 |
| 前端 | 查询键、手动刷新、独立补查失败、待确认占位、趋势缺口、格式化 | 渠道监控 130 文件/773 测试、类型检查和构建通过；未新增浏览器人工验收 |
| 后台分钟聚合 | 租约续期、完成清理、过期保留 | V-05 受控交错复现，未关闭 |
| 启动/升级 | 初始化、从节点检测、缺口恢复、1:1 parity 迁移 | 源码及三库 parity 回归；本轮没有重跑旧发布版到新版本的完整主库/日志库迁移矩阵，历史证据仍见前文 |
| 性能 | 缓存优先、同步失效、资金事务、批次锁 | 未新增生产逻辑；不重复宣称零开销；本轮没有新增 HTTP/线上规模压测 |

### 实际执行与环境

所有 Go 命令使用 Go 1.26.5。主模型专项先在 13318/15438 的原隔离实例完成，版本为 SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 9.6.24。新增复现及分析矩阵使用新容器 `codex-profit-review-mysql-20261007`（13319）和 `codex-profit-review-postgres-20261007`（15439），镜像分别为 `mysql:5.7.44`、`postgres:9.6.24`；没有连接业务 MySQL 3306 或 Redis 6379。

模型 DSN：`TEST_COST_BACKLOG_MYSQL_DSN` 指向 `127.0.0.1:13319/new_api_cost_backlog_test`（`charset=utf8mb4&parseTime=true&loc=Local&clientFoundRows=true`），`TEST_COST_BACKLOG_POSTGRES_DSN` 指向 `127.0.0.1:15439/new_api_cost_backlog_test?sslmode=disable`。分析矩阵通过 `CHANNEL_MONITOR_CONSISTENCY_DIALECT` 选择引擎，`SQL_DSN` 指向同端口专用 `new_api_cm_schema_analytics`；SQLite 的 `CM_UPGRADE_SQLITE_PATH=D:/temp/cm-consistency-verification-profit-20261007.db`。MySQL 专用分析库先误用默认字符集，中文夹具插入失败；改测试库及其新建表为 utf8mb4 后通过，未改生产代码。[初次环境失败日志](D:/temp/profit-audit-20261007-analytics-mysql.log)保留。

| 命令 | 结果与日志 |
|---|---|
| `go test ./model -run 'TestChannelMonitorIncome\|TestChannelMonitorDirtyMinute\|TestTaskBilling\|TestMidjourneyBilling' -count=1 -v`（配置模型三库 DSN） | 通过，86.986s；[日志](D:/temp/profit-audit-20261007-comprehensive-model.log)；没有同名匹配的测试不算已执行，实际用例以日志为准 |
| `go test ./model -count=1` | 通过，69.298s；[日志](D:/temp/profit-audit-20261007-comprehensive-model-full.log)；此整包命令未设置外部数据库 DSN，不能代替前一项三库专项 |
| `go test ./controller ./service ./middleware -count=1` | 通过，301.459s / 113.069s / 3.089s；[日志](D:/temp/profit-audit-20261007-comprehensive-backend.log) |
| `go test ./controller -run '^TestChannelMonitorAnalyticsReadDatabaseMatrix$' -count=1 -v`（分别设置分析矩阵环境） | SQLite 2.940s、MySQL 4.583s、PostgreSQL 3.593s 通过；[SQLite](D:/temp/profit-audit-20261007-analytics-sqlite.log)、[MySQL](D:/temp/profit-audit-20261007-analytics-mysql-utf8.log)、[PostgreSQL](D:/temp/profit-audit-20261007-analytics-postgres.log) |
| `go build ./...` | 退出 0；[日志](D:/temp/profit-audit-20261007-comprehensive-build.log) |
| `bun run test src/features/channel-monitor`（web） | 130 测试文件、773 项通过；[日志](D:/temp/profit-audit-20261007-comprehensive-web.log) |
| `bun run typecheck`（web） | 退出 0；[日志](D:/temp/profit-audit-20261007-comprehensive-typecheck.log) |
| `bun run build`（web） | 退出 0；[日志](D:/temp/profit-audit-20261007-comprehensive-web-build.log) |

**当前交付结论：不能宣称利润功能没有问题。** 需要优先修复 F-14；V-05 继续待修复；F-13 沿用用户要求缓存优先后的已知限制。其余本轮列明路径未发现新的已证实缺陷，不能扩大为全供应商、线上灾难恢复或所有并发场景均已验证。

## 20. 剩余问题统一复查与官方归属（2026-10-07）

### 结论、基线与修复原则

本次完成第 2、19 节所列利润功能链路的统一复核，并补齐周期切换、长期恢复依赖、MySQL 默认连接语义、缓存正负差额和聚合完成/续期交错的组合验证。最终归并为 **5 类未关闭问题**：F-13、F-14、F-15、F-16、V-05。同一根因的多个入口归入同一项，不将每个失败测试拆成新问题。F-14、F-15 涉及额度正确性，优先级为 P1；其余为 P2。它们尚未修复。

此前复查遗漏组合场景，尤其把 `clientFoundRows=true` 的 MySQL 结果推广到默认连接配置、把单独的退款周期保护推广到“跨周期补扣再退款”、把持久退款指令误当成完整持久恢复依据。因此之前“只剩三项”的表述不充分，本节更正该结论；不能用已有测试数量为完成背书。

- 下游代码基线：`1871733bc`；前轮修复提交 `30c7db2e1`。本轮只编辑本复查文件，复现通过仓库外 Go overlay 执行，没有修改生产代码或仓库测试文件，没有提交、推送或部署。
- 官方对照：本地 `upstream/main = c2b7a9a9e`，提交日期 2026-09-25。本轮未拉取远程最新代码，故“官方已有”仅指该基线，不等同于上游今天尚未修复。
- 遵循用户要求：官方既有问题记录并等待官方；下游新增问题在自己的实现内修复。文件属于官方并不表示其中新增的下游逻辑可以免责；文件属于下游也不表示沿用官方的业务语义应擅自改写。
- 保留官方缓存优先读取，不恢复每次查余额数据库；不改变“孤立预扣保留、等待人工核对”的既定政策。

### F-14：订阅跨周期调整（P1，两个责任边界）

**A．官方已有的当前周期差额语义。** 原周期预扣 100，重置后当前已用 25，最终费用为 50。负差额 -50 作用于当前 `amount_used`，结果为 0，额外释放新周期额度。当前 `PostConsumeUserSubscriptionDelta` 与官方同名函数比较，在统一换行后内容一致。本次实际执行的是当前仓库内这个相同函数，不是独立启动官方版本。

下游 `applyTaskFundingDelta` 沿用相同语义，普通直接结算、持久恢复、异步任务结算和退款也实测出现该现象。持久恢复增加了跨周期迟到的机会，但该差额规则不是最近缓存提交创造的。按用户决定，A 记录为已知风险并等待官方，不擅自改 `model/subscription.go` 的通用结算规则，也不能把“暂不改”记成“已修复”。

**B．下游的补充预扣与整单退款不匹配。** 原周期预扣 100；重置后当前已用 25；`AdjustChannelMonitorReservation` 将预留从 100 补到 150，在新周期实际扣了 50，使已用变为 75。随后整单失败退款：令牌 850 回到 1000，但下游退款仅检查最初回执的周期，因其属于旧周期而跳过全部订阅退款，当前已用仍为 75；正确结果应为 25，即至少退回本次在新周期扣的 50。重复退款也不会补回这部分。

代码链：`model/channel_monitor_reservation.go` 的补充预扣 → `service/channel_monitor_reservation.go` 汇总额记录 → `model/channel_local_response_refund.go` 以原始回执时间决定整个订阅退款。该分周期证据缺失属于下游，不应与 A 一起推给官方。

本次矩阵为三库 × 同周期/重置后 × 9 条路径，共 54 组：官方差额、直接结算、恢复、任务结算、任务退款、单独补充预扣、本地退款、补扣后本地退款、清理回执后退款。单独本地退款跨周期保留新周期 25 的保护有效；它不能覆盖 B。正差额在新周期如何计费属于业务规则，不能以修复 B 为名顺带改官方语义。

后续关闭 B 必须证明：可识别各次补扣的所属周期；同周期与跨周期部分/全额退款正确；重复、恢复、任务移交不重退；A 仍维持明确记录的官方行为。保留现有中断预扣政策。

### F-15：明确退款指令的依赖先被清理（P1，下游）

`CleanupSubscriptionPreConsumeRecords` 按 `updated_at` 清理历史回执，官方后台使用七天保留期。下游 `ApplyChannelLocalResponseRefund` 则必须先查到 `SubscriptionPreConsumeRecord` 才能完成退款事务。

三库复现顺序：正常预扣并持久化明确退款指令 → 将该预扣回执的创建、更新时间设为八天前 → 调用真实 `CleanupSubscriptionPreConsumeRecords(7 * 86400)` → 两次应用退款。两次都返回 `gorm.ErrRecordNotFound`，退款仍 `Applied=false`，令牌仍为 900（预扣前 1000）；同周期订阅已用仍为 100，重置场景保持 25。整个退款事务回滚，系统单靠继续重试无法恢复。

触发前提是退款未完成持续到回执清理，例如长期维护、失败依赖或恢复积压；没有证据证明通常请求会等七天，也没有查询线上发生频率。这里是**已经有明确退款指令**的恢复失败，与用户选择的“没有最终指令的孤立预扣不自动退款”不同。

官方清理函数是既有实现；新的长期重试依赖是下游建立的。因此应由下游补足可恢复证据或依赖保留机制，不能直接把官方全局七天保留期延长当成完整修复。关闭标准：真实清理先执行和退款先执行两种顺序、同/跨周期、重复恢复均正确；已经退款的回执不能被再次用于退钱；不能为缺失证据的旧数据臆造资金事实。

### F-16：MySQL 无变化 UPDATE 被当成失败（P2，下游）

MySQL 默认 `clientFoundRows=false` 返回实际变化行数；SQLite/PostgreSQL 对这里的 UPDATE 返回匹配行数。此前专项 DSN 使用 `clientFoundRows=true`，掩盖了下游把“0 行变化”统一当成记录不存在或并发冲突的问题。本次验证两处实际入口，合并为同类问题。

**A．免费 Midjourney 初始结算。** 零金额是合法输入，`PrepareMidjourneyTaskBilling`、`SettleMidjourneyBilling` 仅拒绝负值或越界。初始结算仍调用 `updateTaskUsageInTx(..., 0)`；用户 `used_quota` 没变化时，MySQL 返回 0，被转成 `record not found`，事务回滚。真实测试中 SQLite/PostgreSQL 的收入为 `settled`，MySQL 为 `pending`，结算标记和请求次数也未在该事务提交。HTTP 调用点只记录初始结算错误并继续响应，未发现这条初始结算的自动持久重试链。此例费用为零，不把它描述成用户现金损失。

**B．普通任务无变化成本结算/重复回调。** 任务已有成本事件与相同 `private_data`，目标额度等于原额度时，`applyTaskBillingOnce` 仍因 `costContextChanged` 写同样的私有数据。若 GORM 自动更新的 `updated_at` 也在同一秒，MySQL 返回 0，错误报告 `task quota changed concurrently`。三库实验依次结算 100、150、150：固定数据库测试时钟使同秒可确定复现，MySQL 第一、三次失败，真正发生额度变化的第二次成功，SQLite/PostgreSQL 均成功。

固定时钟只用于构造“时间戳没有变化”的前提，没有改生产时钟；自由时钟对照三库全部通过。生产重试跨到下一秒后可能成功，因此 B 的证据是误报冲突、额外重试和结算延迟，不是证明一定永久卡死或重复扣费。代码存在有界退避，不能将测试固定时钟下最终报错直接推广成线上必然最终失败。

两处均位于下游文件 `model/midjourney_billing.go`、`model/task_billing.go`。修复应跳过不需要的额度更新或区分“记录存在且无需修改”与真正冲突，保留任务标记、请求次数、收入及成本一致性；不要改全局 MySQL 连接参数。关闭标准需覆盖零金额、同秒相同目标、真实正负差额、重复回调、真实记录缺失及 CAS 失败，在 MySQL 两种 `clientFoundRows` 设置和另两库分别验证。

### V-05：后台完成与租约续期的交错（P2，下游）

第 19 节的过期租约清理残留仍有效。本次新增更直接的交错，不需要租约过期或其他节点竞争：续期协程复制两条 active claims 后暂停；主流程完成并删除第一分钟；续期恢复时发现第一条不存在，将整批判断为失去租约，取消第二分钟的重建。服务层屏障测试使用两分钟未过期租约，首分钟成功、第二分钟收到 `context canceled` 和 `lease lost`，0.44 秒即复现。

三库模型矩阵区分有效租约、自己完成、到期但无人接管、他人接管、重新标脏；确认当前批次续期遇到前一条不匹配会回滚后续续期。另有 MySQL 更新同一到期时间返回零行的问题，归在本类加固边界：生产租约 120 秒、续期间隔 40 秒，通常不会重复写相同截止时间，不能把这个合成场景当成常见线上故障。

影响为重复工作或后台统计延后；没有证明资金重复扣退。修复范围是下游 `service/channel_monitor_aggregation.go` 与 `model/channel_monitor_dirty_minute.go` 的协调，不涉及官方请求扣费。必须区分自己完成与真实接管，覆盖完成、续期、重新标脏、到期和不同持有者的交错，不能单纯延长租约或放宽所有权保护。

### F-13：缓存失效与旧快照回填（P2，下游衔接）

补齐了正向扣款的对照：数据库余额 9900 扣到 9850；Redis 删除失败或先前读取的旧快照随后回填时，缓存仍为 9900。三库资金配合隔离 Redis 故障均复现；绕开失效缓存后读取 9850，重复最终结算不再次扣款。原先退款后的缓存偏低证据仍保留。

影响不仅是展示：偏低可能在入口提前拒绝本可执行的请求；偏高会影响缓存额度判断。普通持久正向预扣仍有数据库钱包/令牌检查，不能说所有请求都会因此透支；信任额度、实时链路也不能只凭普通预扣测试宣称完全无影响。缓存 TTL 可被其他写入刷新，因此不承诺最多 60 秒必定恢复。

官方缓存优先读法继续保留，问题在下游事务提交后的失效衔接。删除失败存在同步等待开销，当前失效超时上限为 3 秒；本轮没有压测其对实际延迟分位数的影响。后续方案必须同时覆盖失效失败、旧快照迟到回填、扣款和退款两方向，并验证正常命中时没有新增数据库读取；不能只做失效重试就宣称所有竞态已解决。

### 完整范围收口

本节针对同一代码基线扩展第 19 节验证；无生产改动，因此沿用其整包后端、构建、前端 773 项、类型检查和报表三库证据，不反复执行相同检查。各类仍以失败复现为准，不能被整包通过覆盖。

| 范围 | 已执行或复用的证据 | 本轮结论 |
|---|---|---|
| 普通钱包/订阅预扣、补扣、终结与恢复 | 第 19 节原子事务/重复恢复/提交不确定；本次九路径周期矩阵 | 钱包已有修复未见新确定回归；订阅归入 F-14/F-15 |
| 普通本地失败退款与人工核对 | 既有幂等、失败轮转与人工证据测试；本次真实七天清理、跨周期补扣再退款 | 明确指令依赖缺陷记录 F-15；不改变孤立预扣政策 |
| 异步任务和 Midjourney | 初始差额、终态退款、成本早到/迟到、过期历史；本次默认 MySQL、同秒/自由时钟对照 | F-14 共用语义及 F-16；未发现额外独立的重复资金扣退证据 |
| 实时、违规扣费 | 第 19 节接入、结算标识、归属与整包回归 | 共享资金问题按 F-13/F-14 处理；无真实供应商跨午夜验收 |
| 成本、缺口、队列与历史清理 | 原故障回归和报表三库矩阵；本次清理资金依赖 | 利润不可确认保护保留；新依赖缺陷 F-15 单列 |
| 后台聚合 | 原慢查询实验、本次无竞争者服务屏障与三库状态矩阵 | V-05 未关闭 |
| 查询与前端 | 同基线控制器三库、773 项前端测试、类型检查及构建 | 未确认新的独立问题；测试不替代线上数据验收 |
| 缓存与请求开销 | 命中时拒绝 SQL 的既有测试；正负差额、删除失败和旧快照回填 | F-13；不新增余额直读，未证明零延迟影响 |
| 启动、迁移与部署 | 第 19 节初始化/parity 与此前迁移证据 | 本轮无 schema 修改，没有重跑完整发布版升级矩阵 |

本次范围内的源码核对、组合实验和归属分析已完成，结果集中在上述五类。保留原 R-01 至 R-05 边界：真实多节点共享目录、双重存储损坏与完整灾难恢复、线上规模、真实供应商长连接等不在这些隔离测试的证明范围；没有查生产用户账目，也没有新增 HTTP 吞吐/延迟压测。这些是明确的验证边界，不等于又发现了五个确定缺陷，也不应删除后宣称不存在未知问题。

### 可复查的执行证据

运行环境：Go 1.26.5；SQLite 3.50.4；MySQL 5.7.44；PostgreSQL 9.6.24。仍使用第 19 节专用隔离容器和 13319/15439 端口，模型库 `new_api_cost_backlog_test`。没有操作业务 MySQL 3306 或 Redis 6379。缓存故障使用测试隔离 Redis。

模型 overlay：[源文件](D:/temp/profit-closure-20261007-model.go)、[映射](D:/temp/profit-closure-20261007-model.json)。它在仓库外附加到已有 `model/channel_monitor_income_test.go`，因此日志行号属于 overlay 内容，不是仓库源码的新增行。服务屏障同理：[源文件](D:/temp/profit-closure-20261007-lease.go)、[映射](D:/temp/profit-closure-20261007-lease.json)。

模型环境变量沿用第 19 节账号和端口，DSN 形状如下（不重复记录测试密码）：

```text
TEST_COST_BACKLOG_MYSQL_DSN=root:<测试密码>@tcp(127.0.0.1:13319)/new_api_cost_backlog_test?charset=utf8mb4&parseTime=true&loc=Local&clientFoundRows=false
TEST_COST_BACKLOG_POSTGRES_DSN=postgres://postgres:<测试密码>@127.0.0.1:15439/new_api_cost_backlog_test?sslmode=disable
```

前面的订阅扩展矩阵使用 `clientFoundRows=true`；末尾清理专项、默认 MySQL 行数专项及现有收入专项使用 `false`。MySQL 模型专用库起初默认 latin1，现有中文夹具插入失败；改该测试库默认字符集为 utf8mb4 后，现有专项最终通过，未改生产连接或数据库。[首次环境失败日志](D:/temp/profit-closure-20261007-default-mysql.log)保留，不将字符集夹具故障计为产品缺陷。

| 实际命令（`go` 为上述 SDK） | 结果与证据 |
|---|---|
| `go test -overlay D:/temp/profit-closure-20261007-model.json ./model -run '^TestAuditProfitRemainingSubscriptionMatrix$' -count=1 -v` | 54 组现状刻画通过，7.379s；[日志](D:/temp/profit-closure-20261007-subscription-expanded.log) |
| `go test -overlay D:/temp/profit-closure-20261007-model.json ./model -run '^TestAuditProfitRemainingSubscriptionMatrix$/./receipt_cleanup_refund' -count=1 -v` | 六组确认清理后持续退款失败，4.148s；[日志](D:/temp/profit-closure-20261007-cleanup.log) |
| `go test -overlay D:/temp/profit-closure-20261007-model.json ./model -run '^TestAuditProfitLeaseSnapshotMatrix$' -count=1 -v` | 三库状态对照；MySQL 同截止时间断言失败，其余预设现状成立；[日志](D:/temp/profit-closure-20261007-lease-default-mysql.log) |
| `go test -overlay D:/temp/profit-closure-20261007-lease.json ./service -run '^TestAuditProfitLeaseOwnCompletedSnapshot$' -count=1 -v` | 失败复现，2.432s；[日志](D:/temp/profit-closure-20261007-lease.log) |
| `go test -overlay D:/temp/profit-closure-20261007-model.json ./model -run '^TestAuditProfit(ZeroMidjourneyCharge\|CacheStalePositiveCharge)$' -count=1 -v` | MySQL 免费 MJ 失败，另外两库成功；六组缓存陈旧现状成立；整条命令 FAIL，4.694s；[日志](D:/temp/profit-closure-20261007-cache-and-zero.log) |
| `go test -overlay D:/temp/profit-closure-20261007-model.json ./model -run '^TestAuditProfitTaskCostReplay$' -count=1 -v` | 自由时钟三库通过4.935s，[对照](D:/temp/profit-closure-20261007-task-replay.log)；固定时钟 MySQL 无变化更新失败、另两库通过4.947s，[确定性复现](D:/temp/profit-closure-20261007-task-replay-fixed-clock.log)。overlay 最终保留固定时钟版本 |
| `go test ./model -run '^TestChannelMonitorIncome' -count=1 -v` | 三库、MySQL 默认 changed-rows、utf8mb4 测试库，最终通过54.226s；[日志](D:/temp/profit-closure-20261007-existing-final.log) |

**结果解读：** 订阅、清理和缓存的现状刻画用例断言的是“当前错误结果确实出现”，PASS 不是修复验收通过。MJ、任务重放和服务屏障用例断言期望行为，FAIL 是本次确认缺陷的证据。不能把 PowerShell 最后读取日志的退出码当成前面 `go test` 成功。临时证据位于本机 `D:/temp`，没有随仓库提交，后续验收应将必要的行为断言整合到现有回归文件。

**本次交付状态：分析与记录完成；五类剩余问题没有在本轮修复。** 官方 A 类行为按用户要求保留待上游；其余下游问题需按各项关闭标准修复和验收，不能宣称“都改完了”或“已经没有严重问题”。

## 21. 逐项修复与修复后统一复查（2026-10-07）

### 当前交付结论

用户授权逐项修复，并明确选择“接受短暂滞后，后台可靠恢复”。本轮按该口径完成下游修复和复查。没有修改官方订阅差额规则，没有修改官方七天回执清理策略，没有提交、推送或部署，也没有操作生产资金。

| 问题 | 当前状态 | 改动及复查结果 |
|---|---|---|
| F-14-A | 官方已知行为，按用户要求保留 | 旧周期最终结算差额仍作用于当前周期；官方 `PostConsumeUserSubscriptionDelta` 未修改，下游共用差额语义也未另行重写。风险没有消失，不算修复完成 |
| F-14-B | 下游修复完成 | 订阅预扣记录当前周期和该周期实际预扣额度；每次跨周期补扣重新累计当前周期份额。全额退款仅返还仍属于当前周期的份额。三库验证同/跨周期、再次重置及重复退款 |
| F-15 | 新记录恢复链修复完成，旧缺失证据有明确边界 | 在资金事务保存周期证据，退款排队时复制到退款记录。官方回执清理前后都能完成有证据的退款；排队后再重置不冲减新周期。已升级的旧行不伪造周期证据；回执已丢失、跨周期补扣无法区分的旧行保留人工核对 |
| F-16 | 两处下游判断已修复 | 零金额只验证资金归属存在，不写无变化用量；任务行已锁定时，同额度且相同私有数据的无变化更新视为成功，真实额度 CAS 仍要求更新一行。MySQL 两种行数设置和另外两库验证通过 |
| V-05 | 下游修复完成 | 完成清理和从活动列表移除，与续期快照/更新串行协调；到期但无人接管允许完成，仍校验领取者、领取时刻和标脏次数；MySQL 相同期限续期验证所有权后成功。慢重建、两分钟并发屏障、三库到期/接管/重新标脏回归通过 |
| F-13 | 按用户接受的最终一致性口径完成 | 资金事务内写入可重试缓存清理任务；Redis 删除失败、进程重启后后台可继续。清理同时更新缓存代次，回填在读取数据库前取代次、Lua 写入时比较，拒绝失效前旧快照。正常命中不增加 SQL，不恢复每次查库 |

### 修复后复查范围与关键边界

资金：保留官方最终差额行为，修正下游整单失败退款。新周期补扣 50 后退款，当前已用由 75 回到 25；补扣后再次重置再补扣，只退最后周期实际扣的部分。三库组合扩展为 12 路径 × 同周期/重置后 × 3 引擎，72 组；额外覆盖清理先于排队、排队后再重置、重复恢复。原有资金原子性、提交确认丢失、人工核对、任务早到/迟到及历史清理专项仍通过。

旧数据：新增周期字段使用可空值区分“旧版没有证据”与合法周期时间 0。旧版同周期预扣可继续补扣并获得证据；旧版跨周期预扣没有足够证据时拒绝继续自动调整。旧退款若回执仍在，沿用既有退款保护；回执已经丢失或旧跨周期补扣无法归属时不推测退款。这属于存量数据核对限制，不声称升级能够自动修复历史错误余额。旧记录升级两次后金额、状态、唯一索引不变，周期字段保持 NULL。

缓存：每个用户/令牌缓存键保存一条 SQL 修复任务，令牌键仅含 HMAC。资金和任务同事务提交或回滚；修复 ACK 按随机修订号删除，后台处理期间出现的新资金变动不会被误删。保留最早待处理时间，避免活跃用户一直排到积压末尾。主节点现有恢复任务每分钟处理至多 500 个键，Redis 故障时保留任务，恢复成功后再确认；使用独立监控 Redis 写连接池，测试回退到共享测试客户端。

缓存代次不设 TTL，每个涉及的账户/令牌增加一个小 Redis 键，避免长暂停的旧读取跨过代次有效期后回填。当前所有生产额度回填入口已传入数据库读取前的代次；不读取资金状态的用户资料更新沿用原逻辑。Redis 全量回滚、代次键被外部清理/驱逐等灾难恢复不在此保证内，仍属于原 R-03 部署边界。故障期间允许旧缓存继续服务是用户明确选择，不能描述成强一致、零滞后或固定一分钟必定恢复；恢复速度取决于调度、Redis/数据库可用性及积压。

请求开销：正常缓存命中路径没有新增 Redis 命令或 SQL；未命中后的数据库回填增加一次 Redis GET（200 毫秒上下文预算，失败不阻断数据库回源）。开启 Redis 时每次下游钱包/令牌资金变动增加对应的修复任务 UPSERT，订阅初始预扣/补扣也增加周期快照读取；这有实际开销，不能宣称完全不影响请求速度。同步失效预算从 3 秒降至 200 毫秒，失败留给后台；现有 go-redis v8.11.5 的连接读写 deadline 使用上下文期限。没有做线上 HTTP 吞吐/延迟压测，不将暖缓存零 SQL 测试等同于整体零开销。

聚合：删去“名义到期就不能完成”的限制，未删所有权和标脏保护。实际接管通过数据库原子领取修改持有者/领取时刻；旧持有者无法删除新持有者记录。重新标脏仍保留标记等待下一轮。后台续期互斥不进入用户请求扣费路径。旧慢查询回归和新屏障回归同时通过。

认证边界：本次需改缓存回填 Lua，因此读取了 OWASP Authentication、Session Management Cheat Sheets，并核对 ASVS 5.0.0 V7（7.2.1、7.4.1、7.4.2 相关服务端验证/失效原则）。资金代次不替代认证版本：认证 fence 与 committed floor 检查先执行；取不到资金代次时仍执行认证检查，只禁止未知代次回填。认证版本回滚/过期、迟到回填、单调 floor、令牌 fence 和新增资金代次交错测试通过。修复任务和新增日志不保存可用 API key、密码或会话令牌。没有更改登录、会话生命周期或恢复机制，也不宣称完成全站 ASVS 合规审计。

参考：[Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)、[Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)、[ASVS 5.0.0 V7](https://github.com/OWASP/ASVS/blob/v5.0.0/5.0/en/0x16-V7-Session-Management.md)。

### 官方文件改动边界

对照本地 `upstream/main c2b7a9a9e`，本轮生产变更中 5 个文件属于官方已有文件：

| 文件 | 最小接入原因 |
|---|---|
| `model/main.go` | 在现有迁移表清单注册下游缓存修复表，增加一行 |
| `model/user_cache.go` | 仅在缓存未命中后、读 DB 前取资金代次，传给回填；命中返回不变 |
| `model/token.go` | 同样在令牌缓存未命中后取代次；正常验证入口与命中行为不变 |
| `model/user_auth_cache.go` | 在现有原子 Lua 内增加资金代次比较；认证版本检查保留且先执行 |
| `model/token_cache.go` | 在现有原子 Lua 内增加代次比较；原令牌变更 fence、完整哈希和活跃额度保护保留 |

其余生产逻辑均在下游已有文件或新增下游 `model/channel_monitor_funding_cache.go`。单纯在下游结算后重复 DEL 无法拦住官方回填入口中已经读出的旧快照，故上述窄接入不可由“只加后台重试”替代；没有复制官方缓存实现或重写官方扣费机制。没有修改 `model/subscription.go`、官方回执清理、通用配额读取、定价表达式或供应商协议。新增测试主要整合在已有收入回归文件，聚合屏障整合在已有聚合测试，未散建测试文件。

### 验证命令、版本及结果

Go 1.26.5；SQLite 3.50.4；MySQL 5.7.44；PostgreSQL 9.6.24。数据库仍是隔离容器 `codex-profit-review-mysql-20261007`（13319）和 `codex-profit-review-postgres-20261007`（15439）。模型矩阵使用 `new_api_cost_backlog_test`；MySQL DSN 与第 20 节相同，分别使用 `clientFoundRows=false/true`。没有使用业务 3306/6379。缓存故障测试采用 miniredis，不冒充真实 Redis 服务验证。

| 命令 | 观测结果 |
|---|---|
| `go test ./model -run 'TestChannelMonitorIncome\|TestChannelMonitorDirtyMinute\|TestTaskBilling\|TestMidjourneyBilling' -count=1 -v`（三库、false） | 通过66.257s；[日志](D:/temp/profit-fix-20261007-model-matrix.log) |
| `go test ./model -count=1`（不带外部 DSN） | 完整模型包通过107.190s；[日志](D:/temp/profit-fix-20261007-model-full.log)。后续只增加边界测试、认证代次读取失败保护及队列公平性，另跑最终专项 |
| `go test ./service ./controller ./middleware -count=1` | 109.082s / 300.462s / 1.101s 通过；[日志](D:/temp/profit-fix-20261007-backend.log) |
| `go test ./model -run 'TestChannelMonitorIncome(PreservesCacheFirstReads\|FundingCacheRepairKeepsNewerIntent\|DirtyLeaseCompletionMatrix)' -count=1 -v`（三库、false） | 通过9.531s；[日志](D:/temp/profit-fix-20261007-cache-lease.log) |
| `go test ./model -run 'TestChannelMonitorIncomePeriodUpgrade\|TestChannelMonitorIncomePreservesCacheFirstReads\|TestChannelMonitorIncomeFundingCacheRepair' -count=1 -v` | 通过6.560s；[旧下游结构升级和缓存](D:/temp/profit-fix-20261007-upgrade-cache.log) |
| `go test ./model -run 'TestChannelMonitorIncome(SubscriptionRefundPeriods\|ZeroMidjourneyCharge\|TaskCostReplay\|PreservesCacheFirstReads\|FundingCacheRepair\|DirtyLeaseCompletionMatrix)' -count=1 -v`（三库、true） | 通过16.959s；[日志](D:/temp/profit-fix-20261007-final-true.log) |
| `go test ./model -run 'TestChannelMonitorIncome\|TestChannelMonitorDirtyMinute\|TestTaskBilling\|TestMidjourneyBilling\|TestUserAuth\|TestPendingUserAuth\|TestCommittedUserAuth\|TestTokenCacheInit' -count=1 -v`（最终三库、false） | 通过72.543s；[最终专项](D:/temp/profit-fix-20261007-final-false.log) |
| `go test ./service -run 'TestRepairChannelMonitorDirtyMinutes\|TestRunChannelMonitorAggregation' -count=1 -v` | 通过8.324s；[聚合和新屏障](D:/temp/profit-fix-20261007-aggregation.log) |
| `go test ./service -run 'TestRepairChannelMonitorDirtyMinutes\|TestChannelMonitorIncome' -count=1` | 最终通过6.278s；[日志](D:/temp/profit-fix-20261007-final-service.log) |
| `go build ./...` | 最终退出0；[日志](D:/temp/profit-fix-20261007-final-build.log) |
| `bun run test src/features/channel-monitor`（web） | 130 文件、773 项通过，92.50s；[日志](D:/temp/profit-fix-20261007-web.log)。没有前端源码变更，沿用第19节类型检查/前端构建证据 |

表结构：从远程只读标签查询确认当前最新 `v1.0.0-rc.41`，SHA `2035a82aeb5414253a728bd937d4b8f97aa99b9b`，导出源码在隔离目录生成发布版数据库。三库分别执行 fresh 初始化 + 两次 verify，以及发布版 seed + 当前代码两次 verify，**18 个阶段全部通过**；覆盖真实主库和独立日志库，保留用户额度、渠道及日志数据、唯一约束和索引，确认新增表/字段存在。另由 `TestChannelMonitorIncomePeriodUpgradePreservesLegacyEvidence` 验证本次修改前的下游收入/退款表升级两次，旧金额与状态不变、周期 NULL、唯一约束有效。

[完整升级执行脚本](D:/temp/profit-fix-schema-20261007/validate.ps1)，同目录 `{engine}-{fresh|upgrade}-{0|1|2}.log` 保存结果。独立库名前缀为 `new_api_profit_fix_20261007_`；SQLite 文件为 `profit-validation-{fresh|upgrade}.db` 与独立 log 文件。没有使用 mock 或只测 SQLite 代替三库。

本轮修复后统一复查已完成：上述下游故障有实现及验收证据；官方已知风险、旧证据缺失和原 R-01 至 R-05 部署验证边界保留。未发现新的已证实阻断项，不承诺所有部署条件或未来未知缺陷均已排除。新增缓存恢复依赖主节点任务正常运行，升级应先停旧节点、主节点完成迁移后再启新版节点，避免混合版本写入缺少新证据。

最后将冷回填代次读取也限制为 200 毫秒，随后执行 go test ./model -run 'TestChannelMonitorIncomePreservesCacheFirstReads|TestChannelMonitorIncomeFundingGeneration|TestUserAuth|TestPendingUserAuth|TestCommittedUserAuth|TestTokenCacheInit' -count=1，退出 0，2.261s；[末次缓存与认证回归](D:/temp/profit-fix-20261007-final-cache-auth.log)。

## 22. 当前工作区再次完整复查（2026-10-07）

### 当前结论

复查对象为 `HEAD 1871733bc` 加第 21 节尚未提交的修复，包括新增的缓存修复表及后台接入；官方归属基线仍为本地 `upstream/main c2b7a9a9e`。本次检查资金事务、预扣与最终结算、订阅周期、普通及任务退款、人工核对边界、成本归属、利润查询、缓存恢复、聚合租约和升级证据。**确认一处尚未关闭的 P2 下游问题，归入 F-13；不能继续将 F-13 标记为完整修复。** 没有发现新的已证实 P0/P1 缺陷；这不撤销原有 F-14-A 官方 P1 风险。

本轮仅审查并更新本记录，没有修改生产代码，没有提交、推送或部署。第 21 节列明的其他修复回归仍通过；旧记录证据不足和 R-01 至 R-05 验证边界继续保留。

### F-13-C：前序恢复耗尽共享预算，阻止独立缓存修复（P2，下游，未修复）

位置：`service/channel_monitor_income.go` 的 `channelMonitorIncomeRecoveryHandler.Run`，第 200 至 204 行。资金恢复、成本归属恢复、缓存恢复依次使用同一个 45 秒 context，缓存排在最后。前两步若因慢查询、锁等待或积压处理耗尽该预算，后面的缓存修复查询会立即收到 `context deadline exceeded`，即使 Redis 已恢复、缓存修复表可正常访问，也没有执行机会。若该前序故障持续在每轮触发，缓存恢复就持续被连带阻塞。

影响：资金已经提交的用户或令牌可能继续使用陈旧额度，影响展示和缓存额度判断。SQL 修复记录没有丢失，数据库资金没有因本实验重复扣退；前序负载恢复正常后仍可补偿。因此这是恢复隔离不足，不是证明余额永久损坏，也不能声称每次后台任务都会触发。用户接受的是 Redis 故障期间短暂滞后，不能据此把无关收入恢复持续阻止缓存清理当成已经满足“后台可靠恢复”。

归属：`service/channel_monitor_income.go` 是下游文件；本轮修复把新增缓存恢复追加到已有共享预算的末尾，引入这处衔接遗漏。它不是官方缓存优先策略的问题，不需要修改官方余额读取、订阅规则或回执保留策略。

确定性复现：在仓库外 overlay 的服务测试中，通过 GORM 查询回调让第一条收入恢复查询等待生产代码真实的 45 秒 deadline；没有缩短生产 timeout，没有取消父 context，没有给 Redis 或缓存修复表注入故障。调用真实 handler 后，验证修复记录仍为 1 条、旧缓存仍存在，父 context 与 Redis PING 正常。随后以存活的 context 直接调用同一缓存恢复函数，成功处理 1 条并清除旧缓存。该实验模拟收入查询超时，不是线上负载压测，也未实测真实数据库锁等待的发生频率。

本用例以“确认现有缺陷”的现状断言通过，**PASS 不代表该问题已修复**。日志同时记录成本恢复与缓存修复查询收到已过期 context。既有模型缓存恢复用例直接调用模型恢复函数，缺少 handler 层“前序超时 + 独立修复可用”的组合，因而先前全通过没有覆盖这个遗漏。

关闭要求：让缓存修复获得独立调度机会和合理执行预算，同时继续服从系统任务自身的取消/租约丢失。可使用独立下游系统任务，或对各恢复阶段分配独立且有界的预算；不能只增大共享 timeout，也不能脱离父任务取消无限运行。验收至少覆盖资金恢复超时、成本恢复超时、Redis 恢复、父任务取消以及新修订号不会被旧 ACK 删除；无需把用户请求改为每次读数据库。

### 全范围复核结果

| 范围 | 本次复核及证据 | 结论 |
|---|---|---|
| 钱包/令牌预扣、补扣、最终结算 | 事务路径及提交不确定分支；重新执行三库收入、任务、MJ 回归 | 未发现新的确定资金原子性或重复扣退回归 |
| 订阅周期、回执清理、普通退款 | 重读持久周期证据及排队/执行分支；三库同/跨周期、清理及重复退款用例 | F-14-B/F-15 修复保持；F-14-A 和旧证据缺失仍按既定边界保留 |
| 异步任务、MJ、实时与本地失败 | 三库任务/MJ及服务生命周期、实时收入、任务成本更正回归 | F-16 保持通过，未确认新的独立问题 |
| 缓存失败、迟到回填、恢复 ACK | 重读生成代次与队列修订号逻辑，重跑三库缓存专项；新增 handler 超时实验 | 模型层修复通过，但服务调度遗漏 F-13-C 未关闭 |
| 成本归属、缺口与保留期 | 重读恢复顺序、查询确认条件；收入及保留期、归属失败回归 | 未发现新的确定归属回归；恢复预算问题统一归入 F-13-C |
| 后台聚合、完成与续期 | 重读锁范围、所有权保护；三库租约矩阵及服务慢重建/交错回归 | V-05 保持通过 |
| 利润报表与界面 | 重读利润汇总、缺口、亏损筛选及快照查询；重跑控制器利润用例；界面未变，沿用第 21 节 773 项前端回归和此前类型检查/构建证据 | 未确认新的查询/展示缺陷；本轮没有重新运行前端检查 |
| 初始化、升级与官方边界 | 核对模型注册、可空周期字段、当前 diff；重跑三库旧下游结构升级用例；发布版 fresh/upgrade 18 阶段沿用第 21 节同一代码证据 | 本轮没有 schema 改动，没有重复执行发布版全矩阵；不扩大既有兼容性证明范围 |

上述检查覆盖当前利润功能和本次修复相关路径；没有把“重新检查”解释为重新审计全部供应商定价表达式、所有认证流程或整个上游项目。已保存的同代码验证可复用，但明确区分本轮新执行与沿用。没有进行生产账目核对、多节点实机验收或线上 HTTP 吞吐/延迟压测，不能据此承诺不存在任何未知缺陷。

### 本轮实际执行证据

Go 1.26.5。模型使用真实 SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 9.6.24；沿用隔离端口 13319/15439 和 `new_api_cost_backlog_test`，MySQL `clientFoundRows=false`。本轮服务/控制器测试使用其 SQLite 测试夹具，缓存使用 miniredis。没有操作业务 3306/6379。

| 实际命令 | 结果 |
|---|---|
| `go test -overlay D:/temp/profit-rereview-20261007-service.json ./service -run '^TestAuditProfitRecoveryBudgetIsolation$' -count=1 -v` | 现状刻画 PASS，47.063s，确认 F-13-C；[日志](D:/temp/profit-rereview-20261007-budget.log) |
| `go test ./model -run 'TestChannelMonitorIncome\|TestChannelMonitorDirtyMinute\|TestTaskBilling\|TestMidjourneyBilling\|TestChannelLocalResponseRefund' -count=1 -v`（上述三库 DSN） | PASS，75.438s；[日志](D:/temp/profit-rereview-20261007-model.log) |
| `go test ./service ./controller -run 'TestChannelMonitorIncome\|TestSettleBillingPersistsFinalWalletChargeForProfit\|TestTaskIncome\|TestRealtimeProfit\|TestTaskBillingCorrections\|TestRepairChannelMonitorDirtyMinutes\|TestRunChannelMonitorAggregation\|TestChannelMonitorProfit' -count=1 -v` | service PASS 10.665s；controller PASS 2.574s；[日志](D:/temp/profit-rereview-20261007-service-controller.log) |

[复现执行脚本](D:/temp/profit-rereview-20261007-run.ps1)、[overlay 源码](D:/temp/profit-rereview-20261007-service.go)、[映射](D:/temp/profit-rereview-20261007-service.json)均在仓库外。后续修复时应将必要的期望行为断言整合到已有服务回归文件。当前复查记录是本轮唯一仓库改动，其他未提交代码来自第 21 节，不能混称为本轮新修复。

## 23. 恢复调度修复及最终交付复核（2026-10-07）

### 交付结论

用户要求“你自己检查好了修复完成再给我”。按此前确定的约束继续修复：官方缓存优先、尽量不增加请求开销、官方已知行为等待上游、允许 Redis 故障期间短暂滞后并由后台可靠恢复、无证据的中断预扣不得自动退钱。本轮已经关闭 F-13-C；当前没有已确认但尚未处理的下游利润功能缺陷。F-14-B、F-15、F-16、V-05 的验收结果保持有效。该结论以明确测试和审查范围为限，不代表已修复官方 F-14-A，或证明所有线上环境不存在未知故障。

### F-13-C 的最终实现

资金结算、成本归属、缓存清理分别注册为独立系统任务：`channel_monitor_income_recovery`、`channel_monitor_income_cost_recovery`、`channel_monitor_funding_cache_recovery`。每类任务每分钟调度，有自己的任务记录、租约、45 秒执行预算和完成状态。资金和成本恢复每次至多处理 100 条，缓存恢复每次至多处理 500 个键。保留现有主节点执行器，不修改官方任务框架。

资金或成本查询占用自己的预算时，其他任务仍可领取和执行。缓存任务只依赖 Redis 启用，利润记录未就绪时也可修复已有缓存指令。每个执行 context 仍继承系统任务的取消信号，不以独立恢复为由绕过租约失效保护。成本归属也独立，避免资金恢复耗尽预算后成本持续没有执行机会。

回归通过真实调度入口创建任务、真实领取建立不同类型的租约，然后在查询回调设置可控屏障：分别阻塞资金及成本查询，证明其他两类任务在屏障未释放时成功，缓存记录被处理、旧缓存被删除；随后取消被阻塞任务，验证其停止并进入失败状态。另验证 Redis 不可用时保留记录、父 context 取消时不清理、不丢记录，以及 Redis 恢复后的下一次调度成功。这些测试无依赖睡眠的竞态判定，未缩短生产预算。

原最终结算服务用例同步更新为分别调用资金和成本任务，保留精确用户余额、令牌额度、收入状态和成本日期断言，并覆盖成本先于资金完成的顺序。不是删除原断言来获得通过。模型层对新修订号 ACK 保护、事务回滚、重复恢复、迟到快照和缓存命中零 SQL 的回归仍保留。

### 请求速度、官方边界和使用文档

本轮仅变更下游 `service/channel_monitor_income.go` 的后台注册/执行、已有 `service/channel_monitor_income_test.go` 及文档。没有新增请求路径的数据库读取或 Redis 命令，没有继续扩大第 21 节的 5 个官方文件接入点。完整未提交改动中，官方已有文件仍只有 `model/main.go`、`model/user_cache.go`、`model/token.go`、`model/user_auth_cache.go`、`model/token_cache.go`，原因见第 21 节；本轮重新以本地 `upstream/main` 核对了归属。

代价是每分钟多两类后台任务记录/领取和独立恢复的数据库工作；请求路径成本仍以第 21 节说明为准，不能声称整个利润功能零开销。独立任务解决共享预算问题，不隔离数据库物理故障或 Redis 服务故障。没有通过修改官方订阅规则、清理期限或每次余额查库规避问题。

已同步更新 `docs/downstream/channel-monitor/profit.md` 的任务类型与恢复边界，以及目录索引。运行记录仅在本文件，不写入功能文档。

### 最终验证证据

环境：Go 1.26.5；真实 SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 9.6.24。新增调度三库验证使用专用库 `new_api_profit_recovery_test`，仍在隔离端口 13319/15439；MySQL 使用默认 `clientFoundRows=false`。此外启动专用 Redis 8.8.0 容器 `codex-profit-review-redis-20261007`，只绑定 127.0.0.1:16389，使用隔离 DB 15。没有使用业务 3306/6379。

| 实际执行 | 结果与证据 |
|---|---|
| `go test ./service -run 'TestChannelMonitorIncomeRecoveryQueues\|TestChannelMonitorIncomeCacheRecovery\|TestSystemTask' -count=1 -v` | SQLite 调度隔离、取消、故障恢复和原调度器用例通过 2.021s；[日志](D:/temp/profit-final-20261007-recovery.log) |
| `go test -overlay D:/temp/profit-final-20261007-recovery-matrix.json ./service -run 'TestChannelMonitorIncomeRecoveryQueues\|TestChannelMonitorIncomeCacheRecovery' -count=1 -v`，分别设置 `AUDIT_RECOVERY_ENGINE=mysql/postgres` | MySQL 3.734s、PostgreSQL 3.159s 通过；overlay 仅切换新增用例的数据库夹具，生产代码未替换。[MySQL](D:/temp/profit-final-20261007-recovery-mysql.log)、[PostgreSQL](D:/temp/profit-final-20261007-recovery-postgres.log)、[脚本](D:/temp/profit-final-20261007-recovery-matrix.ps1) |
| `go test -overlay D:/temp/profit-final-20261007-real-redis.json ./model -run '^TestAuditProfitRealRedisRecovery$' -count=1 -v`，三库 DSN 沿用第 22 节 | 3.456s 通过；三库各测扣款/退款两方向，真实 Redis 执行 Lua。人为错误凭据使 Redis 操作失败，SQL 修复记录保留；恢复有效客户端后清理成功，旧快照回填被拒绝、余额正确、重复结算不重扣退。[日志](D:/temp/profit-final-20261007-real-redis.log)、[脚本](D:/temp/profit-final-20261007-real-redis.ps1) |
| `go test ./model ./service ./controller ./middleware -count=1` | model 110.971s、controller 282.190s、middleware 4.069s 通过；首次 service 失败，原因及处置见下文。整条命令当时退出 1，不能记为全通过。[原始日志](D:/temp/profit-final-20261007-backend.log) |
| `go test ./service -run 'TestChannelMonitorIncome\|TestFetchChannelMonitorCustomUpstreamRatioReusesRequest\|TestSystemTask' -count=1` | 更新原调用顺序后通过 2.147s；[专项](D:/temp/profit-final-20261007-service-regression.log) |
| `go test -p 1 ./service -count=1` | 最终完整 service 通过 69.741s；[日志](D:/temp/profit-final-20261007-service-full.log) |
| `go build ./...` | 最终构建退出 0；[日志](D:/temp/profit-final-20261007-build.log) |
| `git diff --check`、`git diff --stat` | 最终检查通过；核对含此前未提交修复及新增未跟踪缓存文件，未执行提交 |

首次整包 service 的两处失败均保留原始证据：一是旧回归只运行原资金任务，任务拆分后未执行独立成本恢复，导致成本日期断言失败，已按新调度契约补齐调用，未放宽断言；二是无关自定义上游 HTTP 测试遇到 Windows `bind: ... lacked sufficient buffer space or ... queue was full`，该测试源码未改，单项和后续完整 service 均通过。未把这次失败隐藏成首次整包成功。

沿用且重新检查了文件和日志的证据：第 22 节三库模型资金/退款/保留期/租约回归 75.438s；第 21 节 MySQL 两种 affected-rows 设置和三库修复矩阵；三库发布版 `v1.0.0-rc.41` fresh/upgrade、两次重启的 18 阶段全部通过；旧下游周期字段迁移两次后仍为 NULL，金额和唯一性保持。此次没有进一步修改表结构或资金模型，因此没有重复执行发布版全迁移。前端没有变更，沿用 130 文件/773 项通过及此前类型检查/构建证据。

### 逐项完成审计

| 用户要求/验收项 | 最终证据及判定 |
|---|---|
| 修复自己的资金问题，保留官方语义 | F-14-B/F-15/F-16 对应三库行为和升级证据通过；F-14-A 明确按原要求留给上游，未改官方订阅代码 |
| 后台可靠恢复，避免旧缓存重新污染 | 同事务 SQL 指令、修订号 ACK、代次 Lua；三库 + 真实 Redis 正负差额恢复通过；F-13-C 独立调度和取消测试通过，已关闭 |
| 尽量不影响用户请求速度 | 缓存命中零新增 SQL 的回归保留；本轮只改后台；第 21 节新增事务 UPSERT/冷回填读取的开销如实保留 |
| 中断预扣不猜测退款 | reserved 不由恢复任务消费；三库及进程中断原用例有效；无最终依据仍需人工核对 |
| 统计不错误确认利润 | 原子确认、成本归属/缺口/历史范围与查询回归通过；前端待确认和失败提示的同代码验证有效 |
| 聚合并发与租约 | 三库所有权矩阵和服务层慢重建、完成/续期屏障通过；未取消接管和重新标脏保护 |
| 三库兼容与升级 | 真实最低支持系列 MySQL 5.7.44/PG 9.6.24 + SQLite；资金与迁移证据完整；新增任务领取/完成三库通过 |
| 根目录记录完整结果 | 本节统一当前状态，保留历史失败与修复过程；开头已指向本节，不再把 F-13-C 列为当前未修复 |
| 修复后再次复查 | 检查事务、退款、缓存交错、恢复调度、清理、聚合、查询及所有本轮 diff；四个相关后端包最终通过、构建通过，没有新的已证实下游未关闭项 |

交付范围为当前工作区代码与验证记录，尚未提交或部署。旧证据缺失、共享目录/基础设施故障、金额精度和真实供应商/线上规模等 R-01 至 R-05 既定边界没有被测试结果抹去。本轮新增真实 Redis 证据只证明所测客户端故障、恢复与 Lua 行为，不冒充全量 Redis 数据回滚或灾难恢复验收。

## 24. 用户再次确认范围后的最终复查与修复（2026-10-07）

### 范围与结论

用户明确回复“仍保留官方行为，只修复下游问题”。本节遵循该范围，F-14-A 官方订阅周期差额不改；无证据中断预扣仍不自动退款，Redis 故障期间短暂滞后仍允许。重新读取当前代码和未提交 diff，未直接沿用上一轮完成判断。

补查发现并修复恢复链同一队列内部的单条慢记录阻塞，记为 **F-13-D（P2，下游恢复隔离，已修复）**。此前 F-13-C 已将不同队列分离，但不能据此推导同一队列中的每条记录也隔离。最终三库验证表明单条资金/成本恢复及失败轮转超时后，同批正常记录仍可处理；恢复依赖解除后重试可完成原指令，不重复扣款。没有新增确认而留待用户再次要求修复的下游项。

### F-13-D 的复现、修复和验收

修复前 `RecoverChannelMonitorIncomeFunding` 和 `RecoverChannelMonitorIncomeCosts` 每条操作直接复用整批 context。若最前一条记录的依赖长期等待，它会耗尽整批预算，之后更新失败时间的轮转也使用已过期 context，下一条正常账户/成本事件没有恢复机会。重复发生时，后续记录持续延后。没有证明资金丢失；持久指令仍在，问题是后台恢复进度。

先执行期望行为用例：两个独立用户，首条资金查询等待 context 截止，第二条正常。修复前 15 秒批次结束后，恢复数为 0、第二个用户余额仍 900、收入仍 `funding_pending`，断言失败。该测试通过 GORM 回调模拟依赖等待，不声称真实数据库锁等待频率或线上受影响规模。[修复前失败日志](D:/temp/profit-closure2-20261007-slow-before.log)。

最终代码给每条资金及成本恢复分别加 5 秒子 context，失败记录轮转也用独立 5 秒子 context；均继承批次取消和截止时间。资金提交结果核对继续沿用原有最多 5 秒的只读核对，未删除提交不确定保护。因此“5 秒”是单步预算，不能把整个资金记录处理声称为严格 5 秒。请求中的预扣、结算、读取和退款路径未添加任何新操作。

最终回归同时让首条恢复和其失败轮转等待至各自截止，使用 20 秒父预算：父 context 仍有效，第二条恢复数为 1；首条超时事务不改余额。解除故障后，只处理原失败记录，已成功记录不重复处理。资金与成本 × SQLite/MySQL/PostgreSQL 共 6 个组合通过，使用生产的 5 秒预算，没有缩短超时或以 sleep 猜测竞态。

额外新增三库并行恢复同一收入的屏障用例：资金恢复与成本归属均先加载快照，再同时放行；最终钱包 900→850、令牌 900→850、令牌已用 100→150、收入 settled 且 Quota=150、成本归属为保存的前一天。SQLite 若发生可重试写竞争，事务失败后下轮恢复，最终精确断言一致。MySQL/PostgreSQL 不允许吞掉其他错误。本次三个引擎均通过，没有发现字段覆盖或重复资金变化。

### 本轮验证

环境仍为 Go 1.26.5、SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 9.6.24。使用专用测试库 `new_api_cost_backlog_test` 与隔离端口 13319/15439，MySQL `clientFoundRows=false`，未操作业务 3306/6379。

| 实际命令 | 结果 |
|---|---|
| `go test ./model -run '^TestChannelMonitorIncomeRecoveryContinuesAfterSlowRecord/sqlite/funding$' -count=1 -v` | 修复前 FAIL，16.786s，保留原始证据 |
| `go test ./model -run '^TestChannelMonitorIncomeRecoveryContinuesAfterSlowRecord$' -count=1 -v`（三库） | 第一步修复通过34.369s；后续追加失败轮转等待验证，以最终矩阵为准。[阶段日志](D:/temp/profit-closure2-20261007-slow-after.log) |
| `go test ./model -run 'TestChannelMonitorIncome\|TestChannelMonitorDirtyMinute\|TestTaskBilling\|TestMidjourneyBilling\|TestChannelLocalResponseRefund' -count=1 -v`（三库） | 最终通过135.702s，包含六组慢记录及失败轮转测试64.32s，无数据库 skip；[日志](D:/temp/profit-closure2-20261007-matrix.log) |
| `go test ./model -run '^TestChannelMonitorIncomeFundingAndCostRecoverConcurrently$' -count=1 -v`（三库） | 通过2.921s；[日志](D:/temp/profit-closure2-20261007-concurrent.log) |
| `go test -p 1 ./service -count=1` | 最终整包通过70.339s；[日志](D:/temp/profit-closure2-20261007-service.log) |
| `go build ./...` | 最终退出0；[日志](D:/temp/profit-closure2-20261007-build.log) |
| `git diff --check`、`git diff --stat`、修改文件 `gofmt -l` | 完成最终格式与变更范围检查 |

### 当前交付审计

- 下游资金、退款与数据库兼容：本轮三库资金、订阅周期证据、回执清理、任务/MJ、缓存、租约及旧结构升级回归通过；本轮仅增加后台 context 边界，没有再改 schema。第 21 节发布版 fresh/upgrade 18 阶段仍适用。
- 恢复调度：第 23 节独立队列/独立租约/父取消回归保持有效；本轮单条超时与失败轮转的实际故障测试补齐队列内隔离，三库资金/成本并行验证完成。
- 请求速度与官方改动：本轮生产改动只有下游 `model/channel_monitor_income.go` 的两个后台循环；没有修改官方文件、请求查询或定价规则。完整工作区的 5 个官方窄接入文件仍以第 21 节说明为准。本轮再次以 `upstream/main` 核对所改模型及测试均属下游。
- 全链路复查：普通预扣/最终结算/中断、任务/MJ、订阅退款证据、成本归属/清理、利润确认和缓存恢复的前轮证明仍保留，本轮重跑覆盖受影响链路；第 23 节真实 Redis、控制器/中间件整包和前端 773 项证据没有被本次修改涉及。没有以单个新增测试替代此前全面验证。
- 文档：根目录记录保留修复前失败和最终成功，顶部指向本节；利润使用文档补充单条恢复等待上限。未提交、推送或部署，未读取生产账目。

**交付结论：在用户再次确认的“保留官方行为、只修复下游”范围内，本轮确认的问题均已修复并重新验证，当前没有已证实且未关闭的下游利润缺陷。** F-14-A、旧数据缺失证据和 R-01 至 R-05 的既定部署/容量边界继续如实保留，不将它们改写成已修复，也不承诺不存在未知缺陷。

## 25. 再次全量复查：缓存恢复隔离遗漏（2026-10-07）

### 当前结论与审查对象

用户要求“再重新全量复查一遍还有没有问题”。审查对象为 HEAD `1871733bc` 加当前全部未提交利润改动，包括未跟踪文件 `model/channel_monitor_funding_cache.go`；官方归属基准为本地 `upstream/main` `c2b7a9a9e`。本轮是复查，仓库内只更新本记录，没有修改生产代码或仓库测试，没有提交、推送或部署。

**新增确认 1 项下游 P2 问题 F-13-E，尚未修复；本轮未确认其他新的 P0/P1 问题。** 官方已知 F-14-A 仍按用户决定保留，不能写成不存在严重风险。第 24 节给资金/成本队列增加超时，但遗漏了缓存队列中的同类故障隔离；此前复查没有将三个独立队列逐一套用单条失败矩阵，这是本轮再次发现问题的具体原因。既有正常回归通过不能替代故障隔离验证。

### F-13-E：缓存恢复确认被阻塞后，后续正常键无法恢复（P2，下游，未修复）

位置：`model/channel_monitor_funding_cache.go:72-81`，`RecoverChannelMonitorFundingCaches`。恢复按最早时间顺序读取记录；每条先清理 Redis，再按缓存键与修订号删除 SQL 指令作为确认。两个步骤共用整批 context，任一步返回错误就退出整个循环。生产服务每批最多 500 个键、预算 45 秒。

触发条件：第一条缓存指令的 SQL 行被另一个事务持锁，或者其确认删除发生记录相关错误。即使 Redis 正常且该键已清理，SQL 确认仍可能等待至批次结束或报错；后续无关键没有执行机会。该指令仍按原最早时间排列，持续或反复故障会继续延后其他账户的余额缓存恢复。事务内更新同一缓存指令也可能竞争该行锁；本轮没有测量线上竞争频率。

影响是余额缓存滞后时间延长，进而影响展示及缓存额度判断；不等于资金被重复扣退或修复指令丢失。本轮实验未证明这两种后果。问题属于新增的下游恢复代码，不是官方订阅或官方缓存优先策略本身的问题。

两组独立复现：

1. SQLite + miniredis，通过数据库回调仅让第一条指令的 DELETE 确认失败。连续两次恢复均中止，首个 Redis 键已删除，第二个仍在；撤掉故障后两条指令均恢复完成。这是确定性错误隔离实验。
2. 真实 MySQL 5.7.44 + miniredis，在独立事务中对第一条缓存指令执行实际行锁，调用真实恢复函数。使用 **1 秒调用预算** 加速复现，SQL DELETE 等待约 996ms 后超时；第二个 Redis 键仍在，Redis 本身可正常响应。释放行锁后恢复处理两条记录，第二个键被清理。**没有实际等待生产的 45 秒，也没有把 miniredis 写成真实 Redis。** 该实验验证的是实际 SQL 行锁与恢复控制流。

修复验收要求：每个键的 Redis 清理和 SQL 确认应有受父 context 约束的等待边界；记录局部失败应保留持久指令并允许其他正常键继续。还需验证超过单批上限时失败记录不会长期占满队首，以及修订号变化时不误确认新指令。仅把 `return` 改为 `continue` 不能解决慢等待耗尽整批预算和跨批公平性。保持官方缓存命中路径与当前资金事务原子性，不以请求每次读数据库规避该问题。

### 全量复核范围及结果

| 范围 | 本轮复核与结果 |
|---|---|
| 普通预扣、最终结算、补扣/返还与中断 | 重读 reservation、收入状态与服务接入；三库模型及服务整包覆盖原子资金、重复处理、提交不确定与无证据中断政策；未确认新的重复扣退款问题 |
| 订阅周期证据与回执清理 | 核对持久周期字段、退款依据及清理后的恢复；F-14-B/F-15 的修复仍在，F-14-A 官方语义与旧数据缺证据边界保留 |
| 异步任务、MJ、实时与本地失败 | 核对初始结算、任务校正、退款事务和成本关联；F-16 修复及原有资金/成本断言保留，未确认新的独立缺陷 |
| 成本归属、缺口、保留期 | 核对收入清理、成本日期、缺口数据库/文件记录、查询确认条件；未确认新的利润错误确认缺陷 |
| 恢复调度与缓存 | 独立任务、资金/成本单条预算、修订号确认、代次检查和冷缓存回填均纳入核对；新增 F-13-E，不能继续声称恢复隔离完整 |
| 聚合、脏分钟与租约 | 检查本轮相关变更并重跑模型/服务回归；原有完成与续期协调、所有权保护保持通过 |
| 报表查询与前端 | 核对利润汇总、覆盖范围、亏损筛选、日期及前端查询、趋势和金额展示；渠道监控 130 文件/773 项通过，类型检查通过 |
| 数据库与官方边界 | 本轮模型测试连接真实 SQLite/MySQL/PostgreSQL；没有再改 schema 或官方代码。第 21 节发布版 fresh/upgrade 18 阶段、第 23 节真实 Redis 专项沿用原证据，不冒充本轮重新执行 |

全量是当前利润功能相关链路和工作区变更的完整复核范围，不是重新审计所有供应商协议、整个认证体系或全站计费表达式。没有访问生产流水、进行真实供应商跨午夜请求、多节点实机验收或线上负载压测；R-01 至 R-05 的既定边界继续保留。

### 本轮实际验证证据

环境：Go 1.26.5；SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 9.6.24。整包模型矩阵使用 `TEST_COST_BACKLOG_MYSQL_DSN` / `TEST_COST_BACKLOG_POSTGRES_DSN`，隔离端口 13319/15439、库 `new_api_cost_backlog_test`，MySQL `clientFoundRows=false`。行锁复现使用独立 `new_api_profit_recovery_test`，避免与整包测试共享数据。没有访问业务 3306/6379。

| 实际命令 | 结果 |
|---|---|
| `go test -overlay D:/temp/profit-review3-20261007-cache-ack.json ./model -run '^TestAuditProfitCacheAckFailureIsolation$' -count=1 -v` | 现状刻画 PASS 1.988s，证明缺陷可复现，不是修复验收通过；[日志](D:/temp/profit-review3-20261007-cache-ack.log)、[脚本](D:/temp/profit-review3-20261007-cache-ack.ps1) |
| `go test -overlay D:/temp/profit-review3-20261007-real-lock.json ./model -run '^TestAuditProfitCacheAckActualRowLock$' -count=1 -v` | 真实 MySQL 行锁现状刻画 PASS 2.532s；[日志](D:/temp/profit-review3-20261007-real-lock.log)、[脚本](D:/temp/profit-review3-20261007-real-lock.ps1) |
| `go test -p 1 ./model ./service ./controller ./middleware -count=1`（上述三库环境） | 全部 PASS，退出 0：model 189.311s、service 68.424s、controller 233.289s、middleware 2.311s；[日志](D:/temp/profit-review3-20261007-backend.log) |
| `bun run test src/features/channel-monitor`（`web/`） | 130 文件、773 项 PASS，88.11s；[日志](D:/temp/profit-review3-20261007-web.log) |
| `bun run typecheck`（`web/`） | 退出 0；[日志](D:/temp/profit-review3-20261007-typecheck.log) |
| `go build ./...` | 退出 0；[日志](D:/temp/profit-review3-20261007-build.log) |
| `git diff --check`、`git diff --stat` | 通过；仅提示已有 CRLF 后续转 LF，未报告空白错误。统计含此前未提交改动，另用 `git status --short` 核对未跟踪缓存文件 |

临时复现源码和 overlay 保存在仓库外，只加入测试函数，没有替换生产实现。本轮发现项已经登记；F-13-E 当前保持未关闭，不能用既有测试通过将它改写成“全部修好”。

## 26. 循环修复与同类路径复查（2026-10-07）

### 范围及本轮修复

用户目标为“修复、修复完后进行复查，循环执行，直到全部没问题再交付给我”。仍遵循此前确认的官方行为边界：F-14-A 不修改；缓存优先、允许故障期间暂时滞后；无资金依据的 reserved 不猜测退款。本轮继续处理已确认下游问题，并将同一故障矩阵应用到资金、成本、缓存三个恢复队列。

**F-13-E（P2）：缓存确认失败阻断后续键。** 增加每条 5 秒操作预算，失败保留 SQL 指令并累计错误，继续处理其他键。扫描位置保存在独立 Redis 键中，通过 Lua 比较旧值并写入带随机修订号的新位置；随机修订号避免游标绕回原位置后误接纳旧任务更新。每轮固定最大键，分批向前，到边界后重新扫描失败及新指令。进度在尝试前写入，整批预算耗尽或进程退出后下一任务继续；新一轮仍会重试未确认记录。Redis 游标缺失或 JSON 损坏会重新扫描，资金修复依据始终在 SQL，不会因游标丢失被删除。ACK 仍限定原修订号，不能删除并发资金提交产生的新指令。

**F-13-F（P2）：资金/成本失败轮转更新也失败时，后续批次仍反复选中队首。** 这是进一步核查 F-13-D 后发现的同类遗漏；单步超时不足以证明跨批公平性。新增回归在首条依赖和失败轮转均注入错误、批次大小为 1 时，第二次调用仍无法触及正常记录；修复前 SQLite 的资金、成本两种场景均失败，证据为 [日志](D:/temp/profit-fix4-rotation-before.log)。

最终资金和成本恢复分别使用独立 SQL 扫描进度，按收入 ID 扫描并保存每轮固定上界，不再依赖修改故障收入的更新时间。新表 `channel_monitor_income_recovery_cursors` 只保存 funding/cost 两行；每条尝试前按修订号更新进度，CAS 失败说明另一任务已推进，本任务退出而不覆盖。进度提交响应丢失时读回精确修订号及边界，确认确实提交后才继续；不能仅凭目标 ID 推定成功。失败资金事务及成本记录仍保留，下轮重扫，不因进度推进而当作已结算；原资金事务幂等和提交不确定核对保持不变。Redis 关闭时资金/成本恢复仍可工作。

### 修复后审查与验证范围

| 要求 | 当前证据 |
|---|---|
| 单条失败不挡同批其他账户 | 缓存三库确定性 ACK 错误；MySQL/PG 真实行锁等待生产 5 秒子预算后，父 20 秒预算仍有效，两个正常键被清理 |
| 跨批公平性及持久进度 | 缓存 limit=1 跨批测试，更换 Redis 客户端继续；固定扫描上界后新增键不能阻止回到旧失败记录；资金/成本 × 三库首条与轮转失败后下一批完成正常记录 |
| 失败不丢依据、重试不重复扣退 | 缓存 SQL 指令失败后仍在，释放依赖后清理；资金余额精确断言，原跨进程退出和重复恢复、并发退款回归保持 |
| 并发和提交不确定 | 原三库并发恢复及丢失提交响应回归不放宽断言；首次新增游标提前退出导致测试失败，已修正精确读回确认后重跑通过 |
| 数据库升级 | SQLite/MySQL/PG fresh、发布版 seed 后 upgrade、两次 verify 共 18 阶段通过；独立日志库数据和索引保留，新增游标值及主键唯一性跨重启保留 |
| Redis 实际脚本 | Redis 8.8.0 + 三库，正/负资金差额，错误凭据故障、恢复、迟到回填及幂等通过；隔离 16389/DB15，测试后停止临时容器 |
| 请求速度和官方边界 | 本轮变化都在后台恢复；请求缓存命中、预扣和结算路径未增加操作。官方文件本轮仅 `model/main.go` 增加一项必要模型注册；未改订阅语义、官方清理期限或驱动参数 |
| 全链路利润功能 | 延续第 25 节普通/订阅/任务/MJ/实时、成本归属/缺口/保留期、聚合租约、报表/前端检查；再次执行四个相关后端包。前端本轮无变更，沿用第 25 节 773 项和类型检查结果 |

完整工作区此前的官方接入仍为第 21 节的五个文件；本轮没有新增第六个官方修改文件。新增扫描实现位于下游模型文件；唯一 schema 注册使用既有主库迁移列表，不复制官方迁移机制。代价是后台每条扫描进度写入和少量批次查询；不声称整个利润功能零开销，也没有做线上吞吐压测。

### 实际命令与结果

Go 1.26.5；真实 SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 9.6.24。行为测试采用隔离端口 13319/15439、`new_api_cost_backlog_test`，MySQL `clientFoundRows=false`；升级另建 `new_api_profit_fix4_20261007_*` 主库和日志库，SQLite 使用独立临时文件。没有访问业务 3306/6379。

| 命令 | 结果/记录 |
|---|---|
| `go test ./model -run '^TestChannelMonitorIncomeFundingCache' -count=1 -v`（三库） | 18.275s PASS，真实行锁及跨批故障回归；[日志](D:/temp/profit-fix4-cache-matrix.log) |
| `go test ./model -run '^TestChannelMonitorIncomeRecoveryAdvancesWhenRotationFails/sqlite' -count=1 -v` | 修复前 FAIL 2.204s，资金/成本均证明 F-13-F；未隐藏失败 |
| `go test ./model -run 'TestChannelMonitorIncomeRecovery\|TestChannelMonitorIncomeFundingCache\|TestChannelMonitorIncomeFundingAndCost' -count=1 -v`（三库） | 阶段测试 60.229s；新隔离矩阵通过，原并发用例因游标竞争报错而失败；最终实现将竞争视作另一任务接手后退出，见后续复测。[阶段日志](D:/temp/profit-fix4-recovery-matrix.log) |
| `go test ./model -run 'TestChannelMonitorIncomeFundingConfirmationIsAtomic\|TestChannelMonitorIncomeRecoveryFairnessAndConcurrentWorkers' -count=1 -v`（三库） | 最终 PASS 9.188s，保留原精确资金断言；[日志](D:/temp/profit-fix4-ack-concurrent.log) |
| `D:/temp/profit-fix4-schema/validate.ps1`；各阶段执行 `go test ./scripts/channel-monitor-profit-upgrade -run '^TestChannelMonitorProfitUpgrade$' -count=1 -v` | 18 阶段通过；[脚本](D:/temp/profit-fix4-schema/validate.ps1)、[汇总](D:/temp/profit-fix4-schema/run.log)，同目录各引擎 fresh/upgrade 0/1/2 日志。发布版种子沿用第 21 节导出的 rc.41 源码，当前升级连续两次验证 |
| `go test -overlay D:/temp/profit-fix4-real-redis.json ./model -run '^TestAuditProfitRealRedisRecovery$' -count=1 -v` | 4.268s PASS；[日志](D:/temp/profit-fix4-real-redis.log)、[脚本](D:/temp/profit-fix4-real-redis.ps1) |
| `go test -p 1 ./model ./service ./controller ./middleware -count=1`（三库） | 首次 model 176.584s FAIL，因新增进度提交响应丢失未读回；service 72.205s、controller 317.225s、middleware 2.266s PASS。整条命令退出 1，后续 model/service 按最终修复单独完整重跑；[原始日志](D:/temp/profit-fix4-backend.log) |
| `go test -p 1 ./model -count=1`（三库） | 最终 PASS 239.091s，退出 0；[日志](D:/temp/profit-fix4-model-final.log) |
| `go test -p 1 ./service -count=1` | 最终 PASS 112.190s，退出 0；[日志](D:/temp/profit-fix4-service-final.log) |
| `go build ./...` | 最终退出 0；[日志](D:/temp/profit-fix4-final-build.log) |
| `git diff --check`、`git diff --stat`、修改 Go 文件 `gofmt -l`、`git status --short` | 最终通过；两个未跟踪模型文件已纳入审查。官方归属仍仅五个文件，本轮在 `model/main.go` 增加注册，未修改其他四个官方接入点 |

### 最终完成审计

F-13-E/F-13-F 的故障复现、实现、同类复查和修复后验证均完成；验证中发现的游标并发退出与提交响应丢失问题也已闭环，没有放宽原资金结果断言。最终模型整包包含前述专项、资金/成本并发、订阅周期、历史清理、任务/MJ、慢记录、缓存和进程退出等回归；服务整包按最终实现重跑。控制器/中间件此前整包在本轮通过，之后只增加模型内部的游标提交读回确认，报表及中间件未进一步修改。前端 773 项与类型检查沿用第 25 节同一前端代码的实际结果。

重新检查事务边界、缓存失效与确认顺序、失败后的持久状态、固定扫描上界、进程退出后的重试、并发进度更新及升级注册，没有新的已确认下游未关闭项。资金恢复仍有原来的提交不确定只读核对预算；每条扫描进度写入和恢复分别受限，不能将整个资金处理总耗时声称为严格 5 秒。SQLite 持有数据库级写锁、数据库整体故障、Redis 不可用等共享基础服务问题仍需要依赖恢复，不承诺固定恢复时限。

**在用户确认的“保留官方行为、只修复下游”范围内，本次循环修复与复查已完成，所有已确认下游问题均已修复并验证。** F-14-A 按既定授权留给官方，旧记录缺证据及 R-01 至 R-05 的部署、容量和生产验证边界不因本轮通过而消失。没有声称全部线上环境不存在未知问题；没有提交、推送或部署。

## 27. 全量复查及独立退款队列遗漏（2026-10-07）

### 当前结论

用户要求“再全量复查一遍还有问题吗”。本轮重新读取 HEAD `1871733bc` 加全部未提交改动，含两个未跟踪模型文件；归属基准仍为 `upstream/main c2b7a9a9e`。**确认 1 项新增下游 P2 问题 F-17，尚未修复；未确认其他新的 P0/P1 问题。** F-14-A 为用户决定保留的官方已知问题，这不等于严重风险已经消失。

本轮只复查并更新此记录，未修改生产代码或仓库测试。以下实验源码/overlay 全部在仓库外，调用实际生产函数；没有替换生产实现。上一轮三个恢复队列的修复并未回退，问题出在另一条独立退款队列：此前检查了其退款事务与重复调用，却遗漏了它自己的后台批量调度隔离。这是前轮全量复查覆盖不足，不能归因于“每次复查自然都会有新问题”。

### F-17：独立退款队列缺少单条等待边界（P2，下游，未修复）

位置：`service/channel_local_response_billing.go:60-72`，`channelLocalResponseRefundHandler.Run`；等待实际发生于 `model/channel_local_response_refund.go:199-227` 的退款事务。此队列处理普通监控请求失败和本地响应的持久退款，不属于第 26 节的资金最终结算、成本归属或额度缓存三个队列。

处理流程按 `updated_at, id` 读取最多 100 条 `cache_applied=false` 记录，每条 `ApplyChannelLocalResponseRefund` 直接使用任务 context，失败后的更新时间更新也使用同一 context，且忽略该更新结果。首条用户/退款/订阅依赖等待会推迟后续无关退款；context 过期时更新时间轮转也失败，下一次仍可能从相同首条开始。该 handler 本身没有独立时间上限；系统任务的 `runWithLeaseHeartbeat` 只提供取消及续租，租约 TTL 不是任务执行超时。因此不能把前轮其他 handler 的 45 秒预算套用到这里。

影响：已经持久化且可以退款的后续账户延迟拿回预扣，利润继续待确认；单条退款执行期间还持有进程内用户/令牌批次互斥锁，其他依赖这些锁的操作也可能等待。本轮没有做并发用户请求延迟压测，不给出实际延迟、流量或受影响金额。**未观察到重复退款、已提交资金丢失或退款指令被删除。** 归属为原有下游实现（路径不在 upstream/main，最近相关提交 `30c7db2e1`），不是官方订阅行为，也不是第 26 节新增游标逻辑引入的回归。

复现证据：

- SQLite 文件数据库：创建两名各有 900 余额、各待退 100 的用户，通过查询回调让第一名用户依赖等待到 context 截止。真实创建并领取退款任务，连续两次运行 handler，第二名余额始终为 900，两条指令均未执行；解除故障后再运行，两人均为 1000。使用 1 秒调用 context 加速场景，不表示生产 handler 有 1 秒超时。
- 真实 PostgreSQL 9.6.24：独立事务对第一名用户执行实际 `SELECT ... FOR UPDATE`，不使用等待回调。相同两次任务每次约 995ms 后 context 截止，第二名仍未退款；释放行锁后两笔均正常完成。运行在隔离端口 15439、新建库 `new_api_profit_review5_refund`，没有接触业务数据库。

修复验收应同时包括：单条执行和整批预算、失败/超时后的跨批进度、轮转本身失败、父取消与租约、资金幂等、原订阅周期证据及缺失回执边界；检查进程级锁的持有范围对独立账户的影响。仅增加 `continue` 或给整批套超时不能证明关闭该问题。必须保留官方行为及请求缓存优先，不能以跳过退款或猜测历史扣款来消除积压。

### 本轮完整复核范围

| 路径 | 结果及边界 |
|---|---|
| 普通预扣、追加预扣、最终结算、提交不确定 | 重读记录与会话状态转换、身份和额度校验、事务提交/读回；三库模型及服务整包回归通过，未确认新的资金原子性缺陷 |
| 失败退款、本地响应、订阅周期及回执清理 | 单笔退款和周期证据保持有效；追踪独立后台 handler 后确认 F-17。官方 F-14-A 与旧数据无证据的人工核对规则保持 |
| 任务/MJ/实时、重复回调与成本校正 | 检查任务锁、金额/成本共同更新、保留期判断、收入校正及服务接入；现有整包断言保持通过，未确认新的独立问题 |
| 三个恢复队列与新增游标 | 重新检查固定上界、修订号 CAS、扫描回绕、失败记录保留与提交响应丢失处理；增加“保存进度后取消”实验，三条路径均能继续正常记录、重试原记录且不重复处理 |
| 缓存读写及请求路径 | 检查冷回填的资金代次、热命中及持久修复指令；没有恢复余额每次查库，没有修改官方行为。本轮未运行请求吞吐或延迟压测 |
| 成本缺口、文件日志、清理与跨日 | 核对缺口持久化、解析失败、保留期、成本归属迁移及查询阻断；保持保守待确认，无新增确定缺陷 |
| 聚合、脏分钟与租约 | 复核完成/续租互斥及所有权条件，重跑模型/服务整包；未确认新的回归 |
| 利润报表及前端 | 重读收入/成本 UNION、快照、覆盖、亏损筛选、分页、逐日趋势、查询刷新和金额格式；130 文件/773 项通过，类型检查通过 |
| 初始化/升级与官方归属 | 本轮无 schema/代码变更，沿用第 26 节真实三库 18 阶段迁移及 Redis 8.8.0 专项的同代码证据，不冒充本轮重新执行 |

### 实际验证与记录

Go 1.26.5；模型整包连接真实 SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 9.6.24，环境变量仍为 `TEST_COST_BACKLOG_MYSQL_DSN` / `TEST_COST_BACKLOG_POSTGRES_DSN`，库 `new_api_cost_backlog_test`，端口 13319/15439，MySQL `clientFoundRows=false`。前端未修改。没有访问生产账目、业务 3306/6379，没有提交、推送或部署。

| 实际命令 | 结果 |
|---|---|
| `go test -p 1 ./model ./service ./controller ./middleware -count=1`（三库环境） | 全部 PASS，退出 0：model 182.984s、service 70.598s、controller 238.344s、middleware 2.342s；[日志](D:/temp/profit-review5-backend.log) |
| `bun run test src/features/channel-monitor`（`web/`） | 130 文件/773 项 PASS，93.38s；[日志](D:/temp/profit-review5-web.log) |
| `bun run typecheck`（`web/`） | 退出 0；[日志](D:/temp/profit-review5-typecheck.log) |
| `go test -overlay D:/temp/profit-review5-interrupted.json ./model -run '^TestAuditProfitRecoveryInterruptedAfterProgress$' -count=1 -v` | PASS 3.555s；资金/成本/缓存保存进度后取消，下一批先处理正常记录，再重试原记录；[日志](D:/temp/profit-review5-interrupted.log)、[脚本](D:/temp/profit-review5-interrupted.ps1) |
| `go test -overlay D:/temp/profit-review5-refund.json ./service -run '^TestAuditProfitRefundQueueBlockedByOneRecord$' -count=1 -v` | 现状刻画 PASS 4.745s，确认 F-17，不是修复验收通过；[日志](D:/temp/profit-review5-refund.log)、[脚本](D:/temp/profit-review5-refund.ps1) |
| `go test -overlay D:/temp/profit-review5-refund-pg.json ./service -run '^TestAuditProfitRefundQueueBlockedByOneRecord$' -count=1 -v` | 真实 PostgreSQL 行锁现状刻画 PASS 4.794s；[日志](D:/temp/profit-review5-refund-pg.log)、[脚本](D:/temp/profit-review5-refund-pg.ps1) |
| `git diff --check`、`git diff --stat`、`git status --short` | 完成范围与格式检查，包含此前未提交修改及两个未跟踪文件；本轮仓库变更仅此记录 |

临时夹具调整说明：中断实验最初回调匹配了构造成本子查询而提前取消，改为已读取到指定成本事件后取消，生产代码未变；退款实验最初使用内存 SQLite，context 取消后连接关闭导致库表消失，改为独立文件库后复现稳定。保留 [中断夹具初版日志](D:/temp/profit-review5-interrupted-fixture-before.log) 与 [退款夹具初版日志](D:/temp/profit-review5-refund-fixture-before.log)，不把夹具失败计为产品缺陷。

本轮收口为 **1 项新增、未修复下游 P2：F-17**。没有以正常测试全部通过覆盖真实故障复现，也没有继续保留“全部没有问题”的当前结论。R-01 至 R-05、真实供应商跨午夜、多节点实机、基础服务灾难恢复及线上容量等既定未验证边界继续保留。

## 28. 退款队列修复验收与再次全量复核（2026-10-07）

### 当前结论

审查对象仍为 HEAD `1871733bc` 加工作区全部利润相关改动，含两个未跟踪模型文件；官方归属基准为 `upstream/main c2b7a9a9e`。承接上一轮尚在执行的 F-17 修复，本轮完成其三库验收，再复核相邻的退款事务、批次和提交不确定路径。**F-17 的队列阻塞问题已修复；新确认 F-18（P1，下游），尚未修复。不能称“所有下游问题已经解决”，当前不建议提交。** 普通整包回归全通过与专项故障复现失败同时成立。

### F-17 修复及验收

- 独立退款任务整批预算为 45 秒，每条 Apply 操作为父 context 下的 5 秒子预算。
- 复用既有恢复游标表，增加 `refund` 类型的独立进度；未添加新字段或新表。资金、成本仍各用自己的类型。采用固定上界、尝试前持久推进、修订号 CAS；失败记录不会被当作已退款，下轮重新扫描。查询辅助函数泛型化，资金/成本原有提交确认丢失与并发回归也重新执行。
- 非批量模式不再获取全局用户/令牌批次锁；批量退款用 TryLock，竞争时保留持久指令供重试；资金事务提交及内存批次移除后先释放锁，再执行 Redis 与缓存确认。入队尚未完成所有权移交，仍保留批量模式原有的等待锁行为。这不是保证所有请求无等待，也没有修改官方批次写入器。
- 三库专项覆盖同批第一条慢依赖、limit=1 跨批失败重试、MySQL/PG 实际行锁、用户/令牌批次竞争，以及直接模式下其他批次锁已占用时仍可退款。解除故障后重试，最终余额逐一精确核对，无重复退款。
- 真实 PostgreSQL 9.6 handler 验收：实际事务锁住首名用户，调用 20 秒父预算；首条在约 5 秒后失败，第二名当批由 900 退回 1000；连续两次仍保留首条，解除锁后首条也到 1000。调用真实任务创建、领取和 handler，不替换生产实现。

### F-18：退款同步内存批次遇到 COMMIT 响应丢失，增量可能重复应用（P1，下游，未修复）

位置：`model/channel_local_response_refund.go` 的 `QueueChannelLocalResponseRefund` 与 `ApplyChannelLocalResponseRefund`。两者都会将当前用户/令牌的待写批次增量计入 SQL 余额，但只有 `Transaction` 返回 nil 才从内存批次移除。若数据库实际提交成功、客户端收到提交错误，函数直接返回，已提交的增量仍留在内存；后台退款重试或普通 `batchUpdate` 随后再次应用同一增量。

确定性复现使用真实数据库事务，在实际 COMMIT 成功后注入响应丢失错误，并继续调用真实退款与普通批次写入器。SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 9.6.24 × 入队/退款两个阶段，6 个场景均失败：

1. 钱包和令牌余额各为 900，已用额度为 100，有一笔待退 100 的持久请求。
2. 同一账户/令牌另有正常待写增量 -25。
3. 在退款入队或实际退款阶段，COMMIT 已成功但调用者收到错误。
4. 重试退款并执行正常批次刷新后，正确余额应为 975、令牌已用应为 25；实际钱包和令牌余额都为 **950**、已用为 **50**，重复扣了 25。

这不是重复执行退款金额本身，而是退款事务吸收的批次增量被再次应用。未测量线上发生频率或实际受影响金额。触发需要启用批量更新、同账户/令牌有非零待写增量、数据库提交响应不确定；普通单笔、无批次或正常提交测试不能排除此问题。利润记录启用后钱包常规扣费直写减少了钱包批次来源，但令牌既有路径仍可进入批次，不能据此判定不可达。

归属：退款文件不存在于 `upstream/main`，历史最近修改为 `30c7db2e1`；“事务出错直接返回、仅成功后删除批次”的行为在本轮隔离修复前已经存在。本轮只缩小互斥锁范围和新增恢复进度，没有创造这一提交确认处理方式。因此是**既有下游问题，不是官方订阅 F-14-A，也不是单纯数据库驱动问题**。

修复验收必须同时覆盖入队和退款、钱包和令牌、正负待写增量、确实回滚、已提交但响应丢失，以及提交后读回也失败的情形。不能在错误时一律删除批次（可能丢失未提交增量），也不能只在读回成功时处理而放任读回失败后普通写入器重放。应为批次所有权移交提供可核对的幂等依据或避免退款重复接管这些增量；保持官方缓存优先和用户确认的订阅边界。**本轮只确认并登记此项，尚未实施 F-18 修复。**

### 全量复核覆盖与限制

| 范围 | 结果 |
|---|---|
| 预扣、追加预扣、普通最终结算、实时 | 重核预扣/最终指令与恢复事务边界；模型及服务整包通过，未新增确认其他资金错误 |
| 订阅、失败请求及本地响应退款 | F-17 验收通过；扩大提交不确定矩阵后确认 F-18；官方 F-14-A 保留，历史缺证据不猜退 |
| 异步任务、MJ、收入校正 | 检查失败任务退款重试、任务资金及收入共同更新、缓存失效接入；整包原有断言通过 |
| 资金/成本/缓存/退款四种恢复任务 | 逐一检查预算、失败保留、持久进度、独立租约与固定扫描上界；本轮退款实际行锁和跨批矩阵通过 |
| 聚合、租约、成本缺口、清理与跨日 | 延续第 27 节审查并重跑最终模型/服务/控制器包；未确认新的独立缺陷，已有保守待确认规则保留 |
| 查询与前端 | 重读利润查询缓存键、按日期/渠道查询、金额换算、失败提示、逐日确认与趋势；没有前端改动，沿用第 27 节同代码的 130 文件/773 项及 typecheck 结果，不冒充本轮重跑 |
| 数据库升级与官方边界 | 本轮仅复用游标表，无新增 schema；沿用第 26 节 18 阶段 fresh/发布版 upgrade/两次启动证据。新增行为有真实三库验证；未修改官方文件的新接入 |

工作区官方文件仍只有 `model/main.go`（必要模型注册）、`model/token.go` / `model/user_cache.go`（冷回填前读取资金代次）、`model/token_cache.go` / `model/user_auth_cache.go`（发布旧资金快照时的代次检查），原因和此前验证见第 21 至 27 节。本轮 F-17 实现全部位于下游文件；没有新增官方改动，也没有审计或修改官方订阅语义和全站认证体系。热缓存读取未增加数据库查询；后台增加退款扫描进度读写，未做线上延迟或吞吐压测。

“全量”指当前利润相关链路、已有问题和本次变更范围；不是全站供应商协议或未知线上环境的无缺陷保证。R-01 至 R-05、真实供应商跨午夜、多节点实机、生产流水及基础服务灾难恢复等既定限制继续保留。

### 实际验证证据

Go 1.26.5；SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 9.6.24、Redis 8.8.0。三库专项/整包使用隔离 13319/15439、`new_api_cost_backlog_test`，MySQL `clientFoundRows=false`。真实 handler 使用独立 `new_api_profit_fix6_refund`；普通退款与真实 Redis 使用独立 `new_api_profit_fix6_local_refund`、16389/DB14。未访问业务 3306/6379。

| 命令 | 结果 |
|---|---|
| `go test ./model -run 'TestChannelMonitorIncomeRefundRecoveryIsolation\|TestChannelMonitorIncomeRecoveryFairnessAndConcurrentWorkers\|TestChannelMonitorIncomeFundingAndCostRecoverConcurrently\|TestChannelMonitorIncomeFundingConfirmationIsAtomic' -count=1 -v`（三库） | PASS 42.928s；[日志](D:/temp/profit-fix6-matrix-final.log) |
| `go test -overlay D:/temp/profit-fix6-refund-pg.json ./service -run '^TestAuditProfitRefundQueueBlockedByOneRecord$' -count=1 -v` | 修复验收 PASS 12.924s；[日志](D:/temp/profit-fix6-refund-pg.log)、[测试源码](D:/temp/profit-fix6-refund-pg.go) |
| `go test -overlay D:/temp/profit-fix6-real-refund.json ./model -run '^TestChannelSmallInputResponseRefundDatabase$' -count=1 -v`（真实三库 + Redis） | PASS 4.240s；直接/批量、重复退款、订阅、缺失令牌回滚、Redis Lua；[日志](D:/temp/profit-fix6-real-refund.log)。overlay 仅给现有测试补迁移缓存修复表，没有替换生产函数 |
| `go test -overlay D:/temp/profit-fix6-commit-matrix.json ./model -run '^TestAuditProfitRefundBatchCommitLoss$' -count=1 -v`（三库） | **FAIL 6.254s，6 场景均复现 F-18**；[日志](D:/temp/profit-fix6-commit-matrix.log)、[测试源码](D:/temp/profit-fix6-commit-matrix.go)。不能写成通过 |
| `go test -p 1 ./model ./service ./controller ./middleware -count=1`（三库） | PASS：model 209.533s、service 70.950s、controller 235.984s、middleware 2.166s；[日志](D:/temp/profit-fix6-backend.log) |
| `go build ./...` | 退出 0；[日志](D:/temp/profit-fix6-build.log) |
| `git diff --check`、`git diff --stat`、上游文件归属核对 | 已检查；统计包含此前未提交改动及两个未跟踪文件，未提交/推送/部署 |

测试夹具修正：最初同批慢查询回调在检查结果时仍生效，测试读取没有 deadline 而等待，已停止该测试并把故障解除移到结果读取之前，最终矩阵重新通过。没有放宽金额或处理顺序断言，也未把这个夹具等待当成产品缺陷。真实 Redis 退款夹具补迁移当前缓存修复表后才运行。所有外部实验源码均保留在 `D:/temp/`，不向仓库加入临时 overlay。

本轮结论：**F-17 已闭环，F-18 未关闭；官方 F-14-A 继续保留。全量正常回归通过不能抵消已复现的资金错误。**
## 29. F-18 修复、同类路径复查与完成审计（2026-10-07）

### 修复结果

按用户持续目标“修复、复查、继续修复下游问题”处理第 28 节确认的 F-18。**F-18 已修复并验收；当前利润功能范围没有已确认未关闭的下游问题。** 官方 F-14-A 保持原有行为，未把用户接受的历史缺证据、基础服务故障和部署边界改写为已解决。

最终实现分开两种资金责任：

1. **实际退款只退该笔请求。** 入队已将其预扣与退款指令共同持久化，Apply 不再吸收其他请求的用户/令牌内存批次，也不再获取全局批次锁。退款金额与 Applied 仍在同一 SQL 事务；提交响应丢失时，后续按 Applied 重试。普通批次写入仍按原流程处理自己的增量。
2. **入队同步批次带唯一标识。** 新增可空的 `batch_transfer_id varchar(36)`，与退款指令及本机当时待写增量共同提交。正常入队后从普通批次移除一次。若事务已尝试提交但结果不确定，捕获的批次从普通写入器隔离，保留原请求与标识供重试，不因提交错误直接再次放回。若确实在 COMMIT 前失败，原批次仍留在普通写入器。
3. **重试不依赖成功读回一次。** 同标识行存在，说明该批次已共同提交，结束接管；不存在则重新执行同一事务。数据库不可读时继续保留原批次，后续新请求的增量独立写入。另一节点先成功保存同一退款时，只把本机未提交批次还给普通写入器，不重复退款。归还同时检查溢出与两类锁竞争，失败保留记录，不截断额度。
4. **重复入队不会吞并新批次。** 已存在的同请求直接使用原记录，验证归属和金额；不会把后续请求的新批次计入旧退款。冲突金额仍拒绝。模型不接受调用方伪造 Applied、CacheApplied 或批次标识。
5. **会话与后台接入。** 入队提交不确定会关闭该会话的普通退款权，避免调用方再执行非幂等退款。本机确认循环复用已有下游成本后台运行模块，在主从节点、Redis 关闭时均运行，并随模块 context 停止；每分钟一轮、父预算 45 秒、单条 5 秒，独立扫描固定上界，旧失败记录不会被新记录无限推后。数据库中已提交的退款仍由原独立退款任务执行。

没有改官方批次写入器、官方订阅结算语义或驱动参数。最终没有修改 `service/system_task.go`：中间方案的启动接入已替换为下游 `service/channel_daily_cost_outbox.go`，归属核对确认该文件不在 `upstream/main`。工作区官方改动仍是第 28 节列出的五处既有缓存/迁移接入，本轮没有新增官方修改。

### 资金及性能影响、明确边界

原 F-18 在内存增量 -25 时会重复扣减；最终三库断言逐项检查用户余额、令牌余额、令牌已用，确保每笔独立增量仅应用一次。测试还加入后续 -10 与 -5，最终必须为 `1000 + 原增量 - 15`，不能只检查队列状态或错误返回。

实际退款减少进程级互斥锁及无关批次处理；入队新增一个请求标识查重读取和持久标识，正常请求缓存命中没有增加数据库查询。本轮没有做线上延迟、吞吐或容量压测，不宣称零开销。后台只在存在未确认转移时访问其 SQL/Redis；空队列无需 SQL 查询。

**纯内存边界不扩大为持久承诺：** 未实际提交的普通批量额度原本就在进程内，强制退出仍可能丢失这些尚未持久化的批次；本次没有重写官方批量系统。已实际提交的退款指令和批次标识在进程退出后可恢复，已用独立子进程验证。监控预扣的 reserved 原本已有资金依据，未形成持久退款指令而退出时仍按既定规则保留待人工核对。不能把“提交已成功后的恢复”当成“所有纯内存状态均能跨进程保留”。

### 修复后的同类复查

| 要求/路径 | 证据和结论 |
|---|---|
| 入队/实际退款，COMMIT 成功但响应丢失 | 三库正负增量矩阵通过，原多扣复现不再出现 |
| COMMIT 实际回滚、COMMIT 前确定失败 | 同矩阵分别注入；未提交增量不丢失，重试不重复，断言不放宽 |
| 提交后读回也失败，新请求继续产生批次 | 保留原转移并运行真实普通批次写入；恢复后最终余额准确 |
| 另一节点先完成同一退款 | 本机转移标识与对方不同，归还本机未提交增量，金额三库验证通过 |
| 旧退款重复入队/重复执行 | 后续新 -5 批次只写一次，原退款无第二次加款 |
| 首条失败、跨批、取消、进程退出 | 本机转移 limit=1 公平性/取消；已提交但未确认的退款经子进程退出后恢复，余额 975 且再次恢复 0 条 |
| 会话结束与普通退款互斥 | service overlay 调用真实 BillingSession；提交不确定后 NeedsRefund=false，调用 Refund 不重复返款；恢复后钱包/令牌均 1000 |
| 原 F-17 四队列故障隔离 | 模型整包包含退款慢依赖、实际 MySQL/PG 行锁、跨批及资金/成本/缓存旧故障矩阵；批量锁已占用时实际退款现在仍可完成 |
| 预扣/最终结算/订阅/任务/MJ/成本归属 | 重核利润预扣不吸收待写批次、最终资金共同提交、历史清理、周期证据、缺口与幂等入口；既有模型/服务/控制器回归通过，无新确认缺陷 |
| 聚合租约、报表、前端 | 原修复保持，后端四包通过。利润前端本轮没有修改，沿用第 27 节同代码 773 项及类型检查；不将同时进行的 group-monitor 改动计入本任务验证 |
| 数据库升级 | 新字段实际三库 fresh、发布版 seed 后 upgrade、两次 verify 共 18 阶段通过；标识跨重启保留，主库/独立日志库数据与索引保留 |
| 官方边界与用户速度要求 | 无新增官方生产修改；热缓存路径不变，实际退款移除全局锁，未进行线上性能压测 |

### 实际命令与结果

Go 1.26.5；SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 9.6.24；真实 Redis 8.8.0，隔离 16389/DB14。行为矩阵使用 13319/15439、`new_api_cost_backlog_test`，MySQL `clientFoundRows=false`。升级另建 `new_api_profit_fix7_20261007_*` 主库和日志库；SQLite 使用独立文件。未接触业务 3306/6379，Redis 测试容器已停止。

| 实际命令 | 观察结果 |
|---|---|
| `go test ./model -run '^TestChannelMonitorIncomeRefund' -count=1 -v`（三库，阶段验证） | PASS 47.876s，原隔离回归和第一版 18 场景资金矩阵；[日志](D:/temp/profit-fix7-refund-matrix.log) |
| `go test ./model -run '^TestChannelMonitorIncomeRefundBatchCommitLoss$' -count=1 -v`（三库） | PASS 17.822s，30 场景；[日志](D:/temp/profit-fix7-batch-final.log) |
| `go test ./model -run '^TestChannelMonitorIncomeRefundTransfer' -count=1 -v` | PASS 2.300s，取消/公平性和实际子进程退出恢复；[日志](D:/temp/profit-fix7-transfer-lifecycle.log) |
| `go test -overlay D:/temp/profit-fix7-session.json ./service -run '^TestAuditProfitRefundSessionCommitLoss$' -count=1 -v` | PASS 2.149s，真实会话幂等与循环停止；[日志](D:/temp/profit-fix7-session.log)、[实验源码](D:/temp/profit-fix7-session.go) |
| `go test -overlay D:/temp/profit-fix7-real-refund.json ./model -run '^TestChannelSmallInputResponseRefundDatabase$' -count=1 -v`（三库 + 真实 Redis） | PASS 4.890s，直接/批量、订阅、回滚、缓存 Lua；[日志](D:/temp/profit-fix7-real-refund.log) |
| `D:/temp/profit-fix7-schema/validate.ps1`；各阶段 `go test ./scripts/channel-monitor-profit-upgrade -run '^TestChannelMonitorProfitUpgrade$' -count=1 -v` | 18 阶段 PASS；[脚本](D:/temp/profit-fix7-schema/validate.ps1)、[汇总](D:/temp/profit-fix7-schema/run.log)。release seed 沿用此前 rc.41 源码，当前 verify 重跑两次 |
| `go test -p 1 ./model ./service ./controller ./middleware -count=1`（三库） | PASS，退出 0：model 224.185s、service 68.810s、controller 266.182s、middleware 2.206s；[日志](D:/temp/profit-fix7-backend.log) |
| `go test -p 1 ./service -count=1`（最终后台接入） | PASS 100.406s，退出 0；[日志](D:/temp/profit-fix7-service-final.log)。包含现有 runtime 启停、Redis 关闭及停用生产者后的继续恢复回归 |
| `go build ./...`（最终生产代码） | 退出 0；[日志](D:/temp/profit-fix7-build.log) |
| `git diff --check`、`git diff --stat`、修改 Go 文件 `gofmt -l`、`git status --short` | 完成；两个未跟踪模型文件纳入范围；本轮新增后端改动全部是下游文件 |

初次编译曾因尚未接入扫描代码的 `slices` 未使用而失败，完成恢复实现后所有相关验证重跑通过；未隐藏为通过。模型整包运行后只追加两项上述独立运行通过的生命周期测试，未再改模型生产实现。服务最终接入从官方启动文件移至下游 runtime 后，服务整包重新执行，构建和会话专项均按最终实现运行。

### 完成审计

F-18 的复现、修复、已提交/未提交/读回失败/并发所有权/重复调用/进程退出证据均已具备；第 28 节的失败日志保留为历史证据。重新检查了四类持久恢复与本机转移的职责、原退款与普通批次各自资金归属、会话退款权、锁竞争、缓存、迁移及上下游文件边界。所有已确认下游利润缺陷现已关闭，官方 F-14-A 和原 R-01 至 R-05 继续保留。

工作区同时出现的 `docs/downstream/channel-monitor/model-market-monitoring.md`、`web/src/features/group-monitor/` 修改由其他工作产生，本任务未编辑或回退，也不把其验证归入利润修复。共享索引保留同时存在的修改。本轮未提交、推送或部署，不作“所有未知线上环境绝对无缺陷”的保证。

## 30. 当前工作区再次全量复查（2026-10-07）

### 结论和审查基线

本轮审查 HEAD `819360aea0f14db1ce79f173487b1ae76f276310` 加全部未提交利润改动，包含未跟踪的 `model/channel_monitor_funding_cache.go`、`model/channel_monitor_income_recovery.go`；官方归属基准仍为 `upstream/main c2b7a9a9e0b548c2051a949fceabb59029adcb49`。HEAD 的 group-monitor 提交不属于本次利润修改。

**本轮未发现新的已确认下游利润缺陷；没有重新打开 F-18 或其他已关闭问题。** 这是当前代码检查及回归的结果，不能解释为消除了官方 F-14-A，或验证了所有线上故障。此次只补充本记录，没有修改生产代码、测试代码、数据库结构或官方行为，没有提交、推送或部署。

### 覆盖清单

| 范围 | 本轮复核内容与结果 |
|---|---|
| 钱包、令牌、订阅预扣 | 核对 reserved 与资金共同提交、追加预扣、周期证据及失败后的退款责任；原资金、订阅与缺失记录回归重新通过 |
| 普通结算、实时和异步任务 | 核对最终收入指令、资金确认、提交错误后的重试、任务及 Midjourney 退款、免费/同秒无变化更新；模型、服务整包通过 |
| F-18 退款与批次接管 | 重读入队查重、唯一转移标识、提交不确定时隔离批次、另节点先入队后的归还、后续批次独立写入；真实三库提交丢失/回滚矩阵及进程退出用例随模型整包重新通过；Apply 仍只处理本笔退款 |
| 四类持久恢复与本机确认 | 核对各自预算、游标、固定上界、CAS、取消、单条失败保留及本机循环启动/停止；真实行锁、故障隔离、公平性及并发回归随整包通过 |
| 缓存与用户请求速度 | 核对资金事务内修复指令、修订号确认、旧资金快照回填代次；仍优先读取热缓存，没有改为每次请求查库。冷缓存额外代次读取和后台扫描有成本，本轮未进行容量或线上延迟压测 |
| 成本、缺口与清理 | 核对资金/成本分别恢复时的归属、收入缺口内存及日志、过期清理、订阅回执清理后的独立退款依据；不将缺证据的 reserved 猜测为可退款 |
| 聚合租约 | 核对续期、完成与移除的同步范围及真实接管保护；服务回归重新通过 |
| 利润报表 | 重读收入/成本 UNION、只收入/只成本维度、同一 SQL 快照、缺口和队列覆盖、按维度汇总、分页排序、确认后亏损筛选；控制器整包通过 |
| 前端 | 核对查询键、手动刷新、单渠道补查、失败提示、人民币换算、利润率、逐日确认与趋势；渠道监控 130 文件/773 项重新通过，类型检查通过 |
| 初始化、升级及官方归属 | 核对现有模型注册与新增字段；本轮没有 schema 变化，没有重跑发布版升级矩阵。第 29 节的升级结果是历史记录，不能标成此次执行 |

退款批次锁的疑点已对照实际调用链检查：普通额度批次写入、快照与本机归还都受对应 mutation lock 保护；退款 Apply 不再持有这两类锁。首次入队仍等待批次锁，本机待确认集合扫描仍随积压数量增长，因此不承诺所有故障下零等待或固定恢复时长。没有通过随机压力循环或仅观察日志将这些容量边界误判为已验证。

### 本轮实际验证

使用 Go 1.26.5。模型三库回归配置真实 SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 9.6.24；MySQL/PG 使用隔离端口 13319/15439、专用库 `new_api_cost_backlog_test`，通过 `TEST_COST_BACKLOG_MYSQL_DSN`、`TEST_COST_BACKLOG_POSTGRES_DSN` 配置，MySQL 保持 `clientFoundRows=false`。

| 实际命令 | 观察结果 |
|---|---|
| `go test -p 1 ./model ./service ./controller ./middleware -count=1`（上述三库环境） | 退出 0；model 225.243s、service 67.985s、controller 235.721s、middleware 2.190s；[日志](D:/temp/profit-review8-backend.log) |
| `bun run test src/features/channel-monitor`（web） | 130 文件、773 项全部通过，79.01s；[日志](D:/temp/profit-review8-web.log) |
| `bun run typecheck`（web） | 退出 0；[日志](D:/temp/profit-review8-typecheck.log) |
| `go build ./...` | 退出 0；[日志](D:/temp/profit-review8-build.log) |
| `git diff --check`、`git diff --stat`、`git status --short`、对照 `upstream/main` 文件归属 | 完成；包含既有改动及两个未跟踪模型文件。仅文档存在 Git 的 CRLF 转 LF 提示，没有 diff 格式错误 |

整包通过不代表所有使用其他环境变量的可选外部服务测试都已运行。本轮真实 Redis 专项及 fresh/发布版 upgrade/两次启动的 18 阶段矩阵没有重新执行；第 29 节记录的外部 `D:/temp/profit-fix7-*` 临时文件在本轮检查时已不可读取，不能将历史链接作为本轮仍可独立重放的附件。此次验证日志使用独立 `profit-review8-*` 文件保存。隔离 Redis 容器保持停止，未操作业务 3306/6379。

工作区相对 HEAD 的官方文件修改仍为五个：`model/main.go` 注册下游模型；`model/token.go`、`model/user_cache.go` 在冷回填前读取资金代次；`model/token_cache.go`、`model/user_auth_cache.go` 发布资金快照时检查代次。未增加新的官方文件修改，未改变认证版本检查顺序，也未进行全站认证合规审计。

### 保留事项

- **官方 F-14-A**：订阅跨周期结算原语义仍按用户明确选择保留，等待上游处理；不是“全部问题消失”。
- **已接受边界**：Redis 故障期间余额允许短暂滞后，依靠后台持续重试；缺证据历史预扣人工核对；未提交的纯内存批次沿用既有进程退出边界。
- **未验证部署范围**：R-01 至 R-05、真实供应商跨午夜、多节点实机、生产账目及容量、基础服务灾难恢复仍不在此次验收证据内。

本轮收口：利润相关代码链路与现有故障回归已复查完成，新增确认缺陷为 0；没有用测试通过掩盖既有官方问题或未验证边界。
