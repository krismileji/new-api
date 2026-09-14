# 渠道监控成本积压修复执行计划

创建日期：2026-09-14。核对基线：`611c6f394e41f26afb7367988b7012ada1a27570`。

状态：P0～P4 已完成本地实现和验证；P5 尚未部署、尚未进行线上验收。改动基于上述源码基线，下方复选框及第 5 节记录实际结果。

现状依据见 [渠道监控当前设计与流程核对](CHANNEL_MONITOR_CURRENT_DESIGN.md)。本计划采用局部修复，保留现有 Redis 成本接收、outbox 持久化、实时成本投影和后台日账本处理流程。

## 1. 目标和范围

目标是让成本后台在正常业务流量下持续消化旧待办，减少大事务反复超时导致的整批回滚，并保留现有金额一致性和事件去重保证。

线上日志已经确认成本账本多轮出现 `context deadline exceeded`。源码确认每批最多 256 条、记账事务共用 3 秒超时，Key 成本和监控明细仍逐条更新。这是需要修复的放大因素，尚不能认定为本次故障的唯一根因。数据库连接等待、锁等待和实际 SQL 耗时需要通过实施后的诊断数据继续核对。

本次交付包含：批内聚合、账本专用批量和超时、现有 worker 的有界重试、少量诊断日志及相关回归。沿用现有表结构、事件身份、租约、事务、Redis 消费和投影协议。智能调度冲突、前端状态重构及历史隔离重放另行处理。

“后台待处理”是多个阶段的合计，线上曾显示的 2,112 条不能全部归因于成本账本。本次必须单独验收成本 outbox 的消化情况，同时观察实时事件和成本 Stream 的积压。

## 2. 参数与实现约束

| 项目 | 修改前实现 | 本次实现 |
| --- | --- | --- |
| 账本每批领取上限 | 与成本 Stream 共用 256 条 | 独立设置为 64 条 |
| 账本事务超时 | 与其他操作共用 3 秒 | 独立设置为 10 秒 |
| 其他 Redis、领取和清理操作 | 使用各自现有超时 | 保留原有超时边界 |
| outbox 租约 | 30 秒 | 保留；记账、溢出回退及失败清理共同受租约剩余时间约束 |
| 正常后台扫描 | 启动执行，随后每分钟处理 | 保留 |
| 单轮处理预算 | 约 45 秒，批次结束后检查 | 保留预算，并约束提前重试和新增批次的开始时机 |
| 失败重试 | 已写入 `next_attempt_at`，但本轮退出后等待分钟扫描 | 当前轮次预算允许时，按已保存的重试时间提前唤醒 |

64 条和 10 秒是初始验证值，需要结合三库验证及代表性数据确认。它们不代表任何线上负载都能在 10 秒内完成。单个维度持续超时时，继续定位数据库原因，不通过无限延长超时掩盖问题。

必须保持以下约束：

- 渠道总账、Key 成本、监控成本明细和对应 outbox 完成标记在同一事务提交；失败整体回滚。
- 聚合发生在领取事件后的账本写入阶段。outbox 原始事件及 EventId 仍逐条保留和去重。
- pending 和成功计数按数据库实际提交的事件数变化，不能按领取数、聚合维度数或尝试数扣减。
- 金额及计数继续使用现有整数精度和溢出检查；聚合自身也检查溢出。
- 失去租约的 worker 不得更新其他 worker 已接管的事件；停机和失败清理使用有时限的上下文。
- 下游新增日志使用简体中文。遵守 `.agents/downstream.md`，修改前重新核对文件与 `upstream/main` 的归属。

## 3. 执行顺序

| 编号 | 工作项 | 依赖 | 完成产物 |
| --- | --- | --- | --- |
| P0 | 补诊断并建立基线 | 无 | 可解释的批处理日志、固定验证样本与现状记录 |
| P1 | 合并 Key 成本和明细的重复写入 | P0 | 聚合实现、金额与归属回归 |
| P2 | 拆分账本参数并约束处理时间 | P1 | 固定小批次、独立超时、租约与停机回归 |
| P3 | 在现有 worker 内提前重试 | P2 | 有界重试、虚拟时间回归 |
| P4 | 完成真实数据库与链路验证 | P0～P3 | 三库验证记录、构建结果、发布候选版本 |
| P5 | 部署并观察消化结果 | P4 | 线上验收记录或明确的后续阻塞原因 |

每一步先完成对应回归，再进入下一步。P0～P3 可按独立逻辑提交；P4 验收完整候选版本后进入发布。

### P0：补诊断并建立基线

主要位置：`service/channel_daily_cost_outbox.go`，以及成本模型的批量写入入口。

- [x] 在领取、记账及失败释放阶段记录耗时和错误；记账耗时包含事务提交或回滚，不能把最后一条 SQL 的报错位置当成唯一慢点。
- [x] 每轮汇总领取数、实际提交数、失败释放数、总耗时和下一次重试时间。聚合维度数在 P1 完成后接入同一日志，P0 不单独复制聚合逻辑。
- [x] 记录日志时复用已有 pending、最早待处理和运行状态；缺失或过期的观测明确标注，不新增逐条数据库查询。连接池等待可按需读取现有 `database/sql` 统计，锁等待和执行计划由排障时采集。
- [x] 成功日志按有工作的一轮汇总，失败记录阶段和原因；连续同类错误限频。日志不包含 Key 原文、事件载荷或用户明细。
- [x] 准备两类固定数据：大量事件命中相同统计维度，以及每条事件命中不同维度。记录修改前的 SQL 数量、提交事件数，以及新建行和更新已有行两类样本的事务耗时。

完成标准：可以区分领取失败、事务超时、失败清理失败和处理成功但速度不足；性能记录使用固定样本，不能用随机输入、大循环或睡眠构造形式化测试。

### P1：合并同一批的重复数据库更新

主要位置：`model/channel_daily_api_key_cost.go` 的 `addChannelDailyCostBatch`，以及 `model/channel_monitor_daily_cost_detail.go`。

- [x] 保留当前输入校验和渠道日总账聚合，在相同批次内补齐 Key 成本聚合。
- [x] Key 成本按 `day_start + channel_id + key_fingerprint` 合并；空指纹仍沿用现有跳过 Key 成本的语义，不能因此跳过渠道总账或监控明细。
- [x] 监控明细先按现有规则规范化，再按完整唯一维度合并：`day_start + channel_id + user_id + api_key_id + api_key_key + model_key + source_kind`。
- [x] 累加成本、探测成本、分组探测成本、已结算和未解析计数时检查溢出。使用现有溢出错误语义，使恢复 worker 仍能识别需要逐条复核的事件。
- [x] 保留原稳定遍历顺序对应的覆盖语义：新行创建时间来自原本首次写入，更新时间及名称、归属等字段来自原本最后一次覆盖；同一时间戳的事件也要保持稳定。不能用一个最大时间戳替代所有时间字段。
- [x] 聚合后按确定顺序写入，保持三类表的写入顺序，避免新增多实例锁顺序竞争。每个相同目标行只执行一次原有受保护写入流程。
- [x] 将实际渠道、Key 和明细的聚合维度数接入 P0 的批处理日志，用于比较同一批事件的 SQL 写入量。
- [x] 保持调用方的事务和投影行为。共享入口还被直接记账路径使用，不能只验证普通请求经 outbox 的情况。

完成标准：同一维度的多条事件得到与原逐条处理相同的金额、计数、创建时间、更新时间及归属；不同维度不串账。相同 Key 的 64 条事件可以合并为一次 Key 行更新；不存在该行时仍允许现有的查询、插入及冲突处理。

建议覆盖：重复维度、全部不同维度、跨日、跨用户/Key/模型/来源、零成本及未解析成本、探测分类、名称变化、相同时间戳、批内溢出和已有累计值溢出。

### P2：账本专用批次、超时和租约预算

主要位置：`service/channel_daily_cost_outbox.go` 的 `applyChannelDailyCostOutboxBatch`。

- [x] 新增账本专用的 64 条批量上限和 10 秒事务超时，定位并替换账本调用点；不得直接放大全局共用的 `channelDailyCostOutboxDBOperationTimeout`。
- [x] 每次只领取本次准备处理的最多 64 条事件，成功后再领取下一批。不能先领取 256 条，再让后面的子批次持有租约等待。
- [x] 依据领取记录的租约截止时间计算剩余预算，为失败释放预留时间。记账、现有溢出逐条回退共用此次领取的总预算，不能让每次回退各自重新获得完整 10 秒。
- [x] 保留溢出事件不阻塞正常事件的现有行为；预算用尽时保留未尝试和未完成事件，释放仍由本 worker 持有的租约，等待后续处理。
- [x] 一轮处理接近 45 秒预算时，不再开始无法在预算内完成并清理的新批次。已领取批次按有界上下文完成提交或失败释放。
- [x] 检查正常恢复、停机和 `FlushChannelDailyCostOutbox` 的调用链。后台自动恢复必须尊重重试时间；现有显式 Flush 的处理语义单独保留和测试。

完成标准：单批操作有明确上限，部分提交只统计实际完成事件；失败能释放待办，超时、取消及租约接管不会造成重复记账或无限等待。

### P3：在现有 worker 内实现提前重试

主要位置：`service/channel_daily_cost_outbox.go` 的 `runDBRecovery` 和批处理返回结果。

- [x] 沿用现有 `attempt_count`、指数退避和 `next_attempt_at`。把成功保存的下一次重试时间返回现有恢复循环，不通过高频查询重新计算，也不新增持久化调度状态。
- [x] 失败后，若下一次重试仍在当前轮次预算内，使用可取消的计时等待提前唤醒；预算耗尽或重试时间超过预算时，结束本轮，回到正常分钟扫描。
- [x] 提前唤醒属于原轮次，等待时间也计入 45 秒预算，不能每次失败都重置预算。整个恢复循环仍由一个 worker 串行执行。
- [x] 保留本轮的 `createdBefore` 边界，避免持续追赶新事件；提前重试时用当前时间判断 `readyBefore`，否则刚到期的记录仍会被旧边界排除。
- [x] 扫描继续校验 `next_attempt_at`、完成状态和租约。提前唤醒不能强制重试所有待办，也不能使用 Flush 的忽略重试时间行为。
- [x] 领取失败尚无事件级重试时间时，只在本轮预算内进行有退避的有限重试。失败释放未成功时，不假定数据库已经保存新重试时间，保留租约接管恢复路径。
- [x] 正常分钟扫描、统计刷新和历史清理保留原有节奏；无待办时等待正常扫描，停机立即取消计时等待。

完成标准：在一轮前段发生一次短暂故障，依赖恢复后能在已保存的重试时间到期时继续处理；一轮末尾或持续故障仍受预算约束。没有忙循环、并行领取或正常扫描饥饿。

时间相关回归使用可控时钟、可控返回值或同步信号，不依赖真实睡眠和耗时竞速断言。

### P4：回归与真实数据库验证

数据库行为发生变化，必须运行真实 SQLite、MySQL 和 PostgreSQL；仅编译成功、使用 mock 或某个子测试被跳过，不算完成。

- [x] 将 P1～P3 的行为场景纳入实际回归，新增或实质改写的 Go 测试使用 `require` 做初始化和致命断言，使用 `assert` 做非致命值检查。
- [x] 三库使用同一组金额、Key 和监控明细场景，覆盖去重、回滚、重试、部分成功、溢出、重启恢复及多实例租约接管。
- [x] 验证故障发生在总账、Key 成本、监控明细或完成标记写入阶段时，均不留下半笔账；提交结果不确定后重试仍只记账一次。
- [x] 验证 Stream 保存成功后确认、Redis 即时投影失败后补偿，以及直接记账和任务成本修正的既有语义。记账优化不得重复追加投影或重复累加 Redis 成本。
- [x] 在固定重复维度和高维度样本上记录改造前后的 SQL 数、事务耗时及实际处理速度。前者应减少重复更新，后者验证固定小批次的处理边界。
- [x] 使用与生产相近的已有成本行和未完成 outbox 数据验证启动、消化和重启。若最终引入任何 schema 或迁移变更，另加最新发布版代表性数据库的升级测试、全新建库测试和至少两次启动/迁移的幂等验证。
- [x] 核对数据库访问仍位于主数据库；如果改动扩展到共享日志库路径，补齐独立日志库配置验证并记录结果。
- [x] 完成相关包测试、全项目构建、`git diff --stat` 和 `git diff --check`，记录实际修改的上游所属文件及必要性；预期本次业务改动均为下游文件。

现有测试入口：

| 入口 | 用途与补齐要求 |
| --- | --- |
| `model/channel_daily_api_key_cost_test.go` | 总账/Key 成本、归属与溢出；补聚合等价性 |
| `model/channel_monitor_daily_cost_detail_test.go` | 完整维度、归属和分类成本；补聚合及失败回滚 |
| `model/channel_daily_cost_outbox_test.go` | 去重、租约、整批回滚；当前配置数据库用例只有较简单的单事件场景，必须扩展到 Key 和明细 |
| `service/channel_daily_cost_outbox_cm07_test.go` | 成本可靠交接、失败重试、溢出、重启及 Flush；补预算与提前唤醒 |
| `service/channel_daily_cost_projection_database_test.go` | 真实 Redis + 三库链路，作为既有投影行为的回归入口 |

在项目根目录执行以下基础命令，并补上实施时新增的用例选择器：

```powershell
go test ./model -run '^(TestChannelDailyCost|TestChannelDailyAPIKeyCost|TestAddChannelDailyCostBatch|TestAddChannelDailyAPIKeyCost|TestGetChannelDailyAPIKeyCost|TestChannelMonitorDailyCostDetail|TestChannelTaskCostEvent)' -count=1 -v
go test ./service -run '^(TestCM07|TestChannelDailyCost)' -count=1 -v
go build ./...
git diff --stat
git diff --check
```

配置真实数据库后，还要明确执行并检查以下入口的每个子测试没有跳过：

```powershell
go test ./model -run '^TestChannelDailyCostOutboxConfiguredDatabases$' -count=1 -v
go test ./service -run '^TestChannelDailyCostStreamProjectionDatabaseMatrix$' -count=1 -v
```

原有模型矩阵读取 `TEST_MYSQL_DSN`、`TEST_POSTGRES_DSN`；投影矩阵读取 `TEST_COST_PROJECTION_MYSQL_DSN`、`TEST_COST_PROJECTION_POSTGRES_DSN` 和 `TEST_COST_PROJECTION_REDIS_ADDR`。投影夹具要求独立空测试库 `new_api_cost_projection_test`、本机连接以及 Redis 地址 `127.0.0.1:26382`，使用 Redis 数据库 1～3。准备环境时先核对当前夹具，避免误用业务数据库。

建议验证 MySQL 5.7.44 和 PostgreSQL 9.6.24，以覆盖项目支持范围的旧版本；SQLite 使用项目实际驱动创建真实临时数据库。若采用其他受支持版本，记录实际版本；如依赖特定版本行为，必须补测最低支持版本。所有版本均以运行时查询结果为准。

| 环境 | 实际版本 | 命令/用例与结果 | 状态 |
| --- | --- | --- | --- |
| SQLite | 3.50.4 | 批量矩阵、预算回归及真实 Redis 投影矩阵通过 | 已验证 |
| MySQL | 5.7.44 | 批量矩阵、原有 outbox 矩阵及真实 Redis 投影矩阵通过 | 已验证 |
| PostgreSQL | 9.6.24 | 批量矩阵、原有 outbox 矩阵及真实 Redis 投影矩阵通过 | 已验证 |
| Redis 链路 | 8.8.0 | 三库的即时更新、补偿、重复交付及任务修正回归通过 | 已验证 |
| Go 构建及相关回归 | Go 1.26.5 windows/amd64 | 相关 model/service 测试及 `go build ./...` 通过 | 已验证 |

完成标准：三库新增契约用例实际执行通过，金额核对正确，构建通过且验证记录完整。任何必须验证的环境不可用时，记录阻塞原因，保留本阶段未完成状态。

### P5：部署与线上验收

- [ ] 记录当前应用镜像、候选版本、部署时间和回退镜像；部署前采集一个固定观察窗口，记录成本待办数量、最早待办时间、实际处理增量及错误频率。
- [ ] 部署通过 P4 的完整版本，确认运行镜像包含本次改动。多实例发布记录各节点版本，观察期间避免把不同节点的本地累计数直接相减。
- [ ] 发布后至少观察 15 分钟，并覆盖一段有代表性的业务流量；若历史待办尚未消化，继续观察，不能仅因一轮成功就宣告恢复。
- [ ] 按相同时间窗口比较成本新进入量和实际完成量。有历史积压时，完成速度应持续高于进入速度，最早待办的事件时间持续向后推进，旧重试记录逐步完成。
- [ ] 抽样核对一组已知事件的渠道总账、Key 成本和完整维度明细，包含重试过的事件，确认没有遗漏或重复；并确认 Redis 成本投影仍能正常补偿。
- [ ] 分别观察监控事件、成本 Stream 和成本 outbox。若成本恢复而调度冲突仍导致实时事件积压，记录为剩余问题，不把总待办变化作为成本修复的唯一结论。
- [ ] 若固定小批次仍持续超时，结合新增阶段日志采集数据库连接池等待、锁等待、慢查询及执行计划，再提出针对性修复。

需要回退的情况包括：金额或计数不一致、重复记账、租约异常、后台数据库负载明显恶化。回退应用版本，保留 outbox、待办、去重记录及已提交账本，让原版本继续接管；不得通过清空记录或重置计数伪造恢复。

完成标准：成本旧待办持续被消化，事务错误不再持续阻塞处理，金额核对正确。正常分钟批次可以短暂非零；历史隔离累计数不要求归零。

## 4. 交付清单

- [x] P0～P3 代码与针对性回归完成。
- [x] P4 三库和 Redis 链路验证完成，命令、实际版本及结果已记录。
- [x] 源码基线、改动文件及上游归属核对已记录；尚未发布。
- [ ] P5 线上观察和金额抽查完成。当前尚未部署、尚未验收。
- [x] 本文状态更新为实际进度，线上数据库负载与其他队列积压仍待观察。

本次实施不以文档勾选数量作为完成依据。代码、数据库验证和线上验收分别记录，只有对应结果实际达成后才能勾选。

## 5. 本次执行记录（2026-09-14）

### 5.1 实现与文件归属

| 文件 | 实际改动 |
| --- | --- |
| `service/channel_daily_cost_outbox.go` | 账本独立 64 条批次与 10 秒超时；领取、记账、释放及整轮预算；按已保存时间提前重试；限频失败日志和有工作的轮次汇总 |
| `model/channel_daily_api_key_cost.go` | Key 成本及完整维度明细聚合，保持创建时间和最后覆盖字段，累计值继续检查溢出 |
| `model/channel_monitor_daily_cost_detail.go` | 单条与聚合记录复用原有受保护写入流程，明细溢出返回账本溢出分类 |
| `model/channel_daily_cost_outbox.go` | 将实际聚合维度传回诊断；事务失败返回零个已确认成功事件，包括完成标记更新后提交失败的情况 |
| `model/channel_daily_cost_batch_database_test.go` | 新增真实三库夹具、固定样本、字段语义、回滚、取消、提交回执丢失及租约接管回归 |
| `service/channel_daily_cost_recovery_test.go` | 新增固定小批次、部分成功及虚拟时钟预算、提前重试、清理和停机回归 |
| `service/channel_daily_cost_projection_database_test.go` | 现有真实 Redis + 三库矩阵增加账本小批次及部分成功验证 |

这些现有代码文件均不在本地 `upstream/main` 树中，属于下游文件。本次未修改上游所属文件、表结构、GORM 标签、迁移、数据库依赖或 Redis 协议；成本读写使用主数据库，没有扩展到独立日志数据库。`CHANNEL_MONITOR_CURRENT_DESIGN.md` 保留为修改前的现状记录。

每轮开始新批次前预留领取 3 秒、记账及溢出回退合计 10 秒、释放 3 秒。提前重试的等待也计入 45 秒预算。正常分钟扫描继续发现跨实例事件及到期租约；一轮结束不代表停止自动恢复。

诊断日志中的领取数、聚合维度数包含本轮重复尝试，不能作为唯一事件数。`applied` 仅统计确认提交的事件数；`pending_estimate` 和 `oldest_pending_cached` 是缓存观测，并带 `stats_observed_at`，不在日志路径追加数据库查询。

### 5.2 固定样本对照

样本为同一个渠道、同一天、64 条事件；每条成本 100 纳元、探测成本 20、分组探测成本 10、已结算数 1。重复维度样本命中同一 Key/用户/模型/来源；不同维度样本使用 64 个 Key。每种样本先新建账本，再用新 EventId 更新已有账本，并核对全部金额和事件数；重复提交相同 outbox ID 的实际应用数为零。

以下是“重复维度、更新已有账本”一次本地对照记录。SQL 数来自 GORM Trace，耗时包括真实事务处理和提交；它们不代表生产环境的固定吞吐或延迟分布。

| 数据库 | 修改前 SQL 数 | 修改后 SQL 数 | 修改前事务耗时 | 修改后事务耗时 |
| --- | --- | --- | --- | --- |
| SQLite 3.50.4 | 133 | 7 | 9.50 ms | 5.34 ms |
| MySQL 5.7.44 | 135 | 9 | 120.04 ms | 12.07 ms |
| PostgreSQL 9.6.24 | 133 | 7 | 59.66 ms | 6.27 ms |

64 个不同 Key 更新已有行时，SQL 仍为 SQLite/PostgreSQL 133 条、MySQL 135 条；该场景没有可合并的重复写入，依靠独立小批次控制事务工作量。初始化新行的 SQL 数分别为 389/391/389，修改前后相同。真实数据库慢查询、连接等待和锁等待仍需在线上发生故障时定位。

### 5.3 已验证的行为

- 三库均验证相同维度聚合、跨日期/用户/Key/模型/来源隔离、空指纹、零成本与未解析成本、探测分类、名称覆盖、相同时间戳以及先创建后收到旧事件的时间语义。
- 在总账、Key、明细和完成标记四个阶段注入失败，验证真实事务整体回滚；租约到期后的新消费者能接管，旧消费者不能释放或重复应用已被接管的事件。
- 三库均验证完成标记更新后取消事务，返回零个已确认成功事件。另在真实数据库 Commit 成功后模拟回执超时，验证再次应用或释放同一事件不会重复记账。
- 小批次回归使用 65 条事件，验证首批只领取和提交 64 条，第 65 条保持未领取状态；后续 Flush 完成全部金额核对。
- 三库均验证有效事件与溢出事件混合时有效部分正常提交，失败部分释放租约并保存重试时间；未到重试时间的记录不会被普通恢复提前领取。
- 虚拟时钟验证提前重试不等待下一分钟、不追赶本轮之后的新事件、持续失败遵守整轮预算、领取失败退避、释放失败等待租约恢复、停机后不再领取。
- 虚拟时钟验证普通应用与溢出逐条回退共用同一个截止时间，租约或本轮期限临近时缩短应用预算，并为失败清理留出时间。
- 真实 Redis 与三库的既有投影、确认失败重放、数据库补偿、统计重建、直接记账及任务修正回归通过。
- 相关 model/service 测试和全项目构建通过，Git 空白检查通过。数据库与 Redis 均使用本次专用本地容器，未连接或修改生产环境。

### 5.4 环境和复现命令

以下容器和凭据仅用于本地独立测试。容器须完成初始化且目标端口空闲后再运行测试；测试夹具要求空库，会清理其创建的表及 Redis 数据。

```powershell
docker run -d --name new-api-cost-backlog-mysql -p 127.0.0.1:23328:3306 -e MYSQL_ROOT_PASSWORD=cost_backlog_test_only -e MYSQL_DATABASE=new_api_cost_backlog_test mysql:5.7.44 --character-set-server=utf8mb4 --collation-server=utf8mb4_unicode_ci
docker run -d --name new-api-cost-backlog-postgres -p 127.0.0.1:25454:5432 -e POSTGRES_PASSWORD=cost_backlog_test_only -e POSTGRES_DB=new_api_cost_backlog_test postgres:9.6.24
docker run -d --name new-api-cost-backlog-redis -p 127.0.0.1:26382:6379 redis:latest
docker exec -e MYSQL_PWD=cost_backlog_test_only new-api-cost-backlog-mysql mysql -uroot -e 'CREATE DATABASE new_api_cost_projection_test CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;'
docker exec new-api-cost-backlog-postgres createdb -U postgres new_api_cost_projection_test

$env:TEST_COST_BACKLOG_MYSQL_DSN='root:cost_backlog_test_only@tcp(127.0.0.1:23328)/new_api_cost_backlog_test?parseTime=true&charset=utf8mb4'
$env:TEST_COST_BACKLOG_POSTGRES_DSN='postgres://postgres:cost_backlog_test_only@127.0.0.1:25454/new_api_cost_backlog_test?sslmode=disable'
$env:TEST_MYSQL_DSN=$env:TEST_COST_BACKLOG_MYSQL_DSN
$env:TEST_POSTGRES_DSN=$env:TEST_COST_BACKLOG_POSTGRES_DSN
$env:TEST_COST_PROJECTION_MYSQL_DSN='root:cost_backlog_test_only@tcp(127.0.0.1:23328)/new_api_cost_projection_test?parseTime=true&charset=utf8mb4'
$env:TEST_COST_PROJECTION_POSTGRES_DSN='postgres://postgres:cost_backlog_test_only@127.0.0.1:25454/new_api_cost_projection_test?sslmode=disable'
$env:TEST_COST_PROJECTION_REDIS_ADDR='127.0.0.1:26382'

go test ./model -run '^TestChannelDailyCostBatchDatabaseMatrix$' -count=1 -v -timeout 90s
go test ./service -run '^TestChannelDailyCostStreamProjectionDatabaseMatrix$' -count=1 -v -timeout 120s
go test ./model -run '^(TestChannelDailyCost|TestChannelDailyAPIKeyCost|TestAddChannelDailyCost|TestAddChannelDailyAPIKeyCost|TestGetChannelDaily|TestChannelMonitorDailyCost|TestChannelTaskCost|TestChannelMonitorCost|TestSettleUnresolved)' -count=1 -timeout 180s
go test ./service -run '^(TestCM07|TestChannelDailyCost|TestReliableDailyCost|TestChannelTaskCost|TestChannelModelDetectionDailyCost)' -count=1 -timeout 180s
go build ./...
git diff --stat
git diff --check

docker rm -f -v new-api-cost-backlog-mysql new-api-cost-backlog-postgres new-api-cost-backlog-redis
```

三个数据库版本来自测试内的版本查询；Redis 8.8.0 来自 `redis-server --version`。`redis:latest` 是本次使用的本地镜像标签，后续复现时需要重新记录其实际版本。

P5 仍待发布后的真实流量、积压消化和金额抽查结果；本地验证不代替线上验收。
