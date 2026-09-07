# 渠道监控智能调度路由一致性修复方案

日期：2026-09-07
状态：已实施，待发布灰度

第 1 至 8 节记录排查结论和原方案，最终实现范围、差异和验证记录见第 9 节。没有连接生产环境或发布。

## 1. 问题结论

当前页面中的“预计流量”不是历史真实流量，而是根据当前优先级、权重和候选层计算出的理论分布。实际请求进入分发流程后，还可能经过以下路径：

1. 渠道亲和缓存优先于普通智能调度选路。
2. 渠道并发达到上限时，会将当前渠道加入排除集合并切换候选。
3. 上游失败、快速失败和任务重试会继续选择备用渠道。
4. 429 冷却、请求大小限制、稳定性试放和探索流量会改变本次请求的可选集合。

因此，页面显示某渠道 100% 时，不能直接推导所有用户请求最终都会使用该渠道。页面当前的“预计流量”标签虽然表达了理论含义，但缺少真实请求分布和路由来源，容易被误认为实际占比。

代码中还存在会造成确定性不一致的实现问题：

| 场景 | 页面采用的路由 | 实际请求采用的路由 | 结果 |
| --- | --- | --- | --- |
| 逻辑渠道组 | 使用逻辑候选折叠后的有效优先级 | 亲和资格判断仍使用物理渠道优先级 | 亲和请求可绕过页面显示的最高候选层 |
| 缓存刷新期间 | 渠道和部分状态来自数据库，运行态优先级来自缓存 | 选路完整使用缓存快照 | 页面可能把实际仍会使用的渠道显示为降级或备用 |
| 关闭逻辑渠道组 | 页面在 `GetChannelSmartScheduleRouteRuntimeViewsWithContext` 中直接返回数据库路由 | 请求仍可能使用智能调度缓存 | 页面优先级和实际优先级不同 |

已用临时测试复现：页面计算出的唯一最高候选为渠道 9453，普通智能调度也选择 9453，但亲和路径仍判定渠道 9451 可用并返回 9451；另外，数据库将渠道标记为降级而缓存仍可选时，页面会排除该渠道，实际请求却仍选择该渠道。

## 2. 修复目标

1. 普通请求、亲和请求、并发重选和页面展示使用同一份有效智能调度路由快照。
2. 逻辑渠道组只在候选层参与优先级和权重竞争，物理成员只负责最终 Key 选择。
3. 页面明确区分“预计流量”和“实际请求分布”。
4. 页面能够说明当前数据的快照时间、刷新状态和路由来源，避免把缓存过期或刷新中的数据当成准确实时状态。
5. 保持现有重试、429 冷却、请求大小限制、稳定性保护、探索流量和显式指定渠道行为。

## 3. 统一路由快照设计

在 `model` 包内复用已有路由快照，增加只读运行态投影。渠道、智能调度状态和逻辑组关系在现有同步锁下读取；429 冷却仍由 service 层提供，并在逻辑组折叠前按物理渠道应用。

每个路由快照至少包含：

- 物理路由键：`channel_id`、分组、模型。
- 有效优先级和有效权重。
- 候选渠道 ID；逻辑渠道组成员 ID 和成员权重。
- 是否参与智能调度、是否可用、暂停截止时间和 429 冷却状态。
- 稳定性、探索和请求大小限制状态。
- 快照生成时间、版本或控制修订号。

实现上优先扩展现有 `channelSmartScheduleCachedRoute` 和运行态缓存，避免另建一套独立的调度规则。页面只读快照，不能在前端再次推导一套与后端不同的候选层。

快照应提供以下两个层次的能力：

1. 候选阶段：复用 `prepareChannelSmartScheduleCachedRoutes`、数据库适配和逻辑组折叠，完成物理过滤和有效候选计算。
2. 成员阶段：复用 `selectLogicalSmartScheduleMemberID`，根据当前物理成员可用性和成员权重选择最终渠道。

普通缓存选路和数据库选路必须调用同一套候选规则。数据库路径只能负责加载数据，不能重新实现一套不同的优先级、逻辑组和状态判断。

## 4. 后端修改方案

### 4.1 修复亲和路由绕过智能调度

修改 `model/channel_smart_schedule_affinity.go` 中的资格判断和成员选择逻辑：

- 亲和缓存只能固定智能调度候选，不能固定候选层以下的物理渠道。
- 对逻辑渠道组，先通过统一快照确认逻辑候选仍位于当前可路由的最高优先级层，再在成员中按成员权重选择物理渠道。
- 亲和资格判断不能直接读取物理路由的 `priority` 与 `weight`，必须读取统一快照中的有效候选值。
- 亲和命中但候选层已变化时，清除或暂时忽略该亲和映射，回到普通智能调度选路。
- 429、暂停、稳定性降级和请求大小限制继续在物理成员层判断，避免一个成员不可用时错误排除整个逻辑组。

现有 `middleware/distributor.go` 中亲和优先于普通选路的顺序可以保留，但必须保证亲和资格判断和普通选路使用同一候选快照。

### 4.2 统一缓存和数据库选路

修改以下路径，使它们都使用统一的候选快照：

- `model/channel_cache.go`
- `model/channel_smart_schedule_route_cache.go`
- `model/channel_database_selection.go`
- `model/channel_smart_schedule_logical_group.go`
- `model/channel_smart_schedule_affinity.go`

重点处理：

- 逻辑渠道组启用和关闭两种模式都必须走同一套有效路由投影。
- 快照为空、正在刷新或标记为脏时，沿用当前完整快照，不能把数据库半新状态和缓存半旧状态拼成一条页面路由。
- 如果没有可用完整快照，应返回明确的运行态不可用状态；不要继续生成看似完整的 100% 预计流量。
- `effective_priority`、`effective_weight`、`routing_candidate_channel_id` 和有效状态必须来自同一个快照版本。

### 4.3 记录请求实际使用的路由来源

在请求上下文和管理员日志中补充路由来源字段，至少区分：

- `smart_schedule`：普通智能调度候选。
- `affinity`：渠道亲和命中。
- `logical_member`：逻辑候选确定后的物理成员选择。
- `concurrency_fallback`：并发不足后的切换。
- `retry`：上游失败或任务失败后的重试。
- `rate_limit_fallback`：429 冷却后的备用选择。
- `specific_channel`：显式指定渠道。

同时记录：请求尝试序号、候选渠道 ID、最终物理渠道 ID、快照版本和快照生成时间。敏感的亲和原始值继续只记录指纹或脱敏提示，不记录原文。

路由来源应复用现有日志的管理员信息扩展位置，避免修改核心账务字段和公开用户响应。

### 4.4 增加真实请求分布接口数据

页面继续保留“预计流量”，但增加按时间窗口统计的“实际请求分布”。接口响应建议同时提供：

- 统计窗口起止时间。
- 物理渠道最终成功请求数及占比。
- 物理渠道请求尝试数及占比。
- 发生重试的请求数。
- 因并发、429、亲和或其他原因切换的次数。
- 数据覆盖范围、事件水位和降级原因。

“实际请求分布”必须明确统计口径：

- 最终成功分布：每个请求只计入最终成功的物理渠道。
- 尝试分布：每次真实上游尝试计一次，允许总数大于请求数。

缺少有效窗口或 Redis 投影不可用时，返回数据不完整状态，不把缺失数据显示为 0%。已有实时投影和数据库分钟聚合应优先复用，不能为了页面统计重复写账务链路。

### 4.5 调整监控接口响应

修改 `controller/channel_ratio_monitor_schedule_route_api.go` 和 `controller/channel_ratio_monitor_schedule_route_response.go`：

- 从统一运行态快照生成 `effective_*` 和逻辑候选字段。
- 不再把数据库中的旧 `State` 与缓存中的新优先级拼成一条有效路由。
- 返回快照版本、生成时间、是否刷新中和数据是否可用。
- 保留现有字段以兼容前端，必要时增加明确的 `runtime_snapshot` 对象。
- 当快照脏或过期时，返回中文状态说明，前端显示“数据刷新中”或“数据可能延迟”。

## 5. 前端修改方案

修改 `web/src/features/channel-monitor`：

1. 保留“预计流量分布”名称，并在说明中明确它来自当前有效优先级和权重。
2. 新增“实际请求分布”区域，展示统计窗口、最终成功分布和尝试分布。
3. 对亲和、重试、并发切换和逻辑成员选择显示来源提示，避免用户把最终物理渠道误解为智能调度候选层。
4. 使用后端返回的候选 ID、有效优先级、有效权重和成员权重，不在 `smart-schedule-summary.ts` 中重新判断缓存状态。
5. 快照刷新中、过期或实时投影降级时，显示状态提示并保留数据覆盖范围；不能用空数组或 0% 代替缺失数据。
6. 智能调度页面在打开期间按合理间隔刷新运行态和真实请求分布；手动刷新继续使用现有 React Query 查询失效机制。
7. 下游新增文案使用简体中文字面量，不新增前端 i18n key，不修改 locale 文件。

## 6. 实施顺序

### 阶段一：建立一致性测试基线

先增加能够稳定复现当前问题的测试，至少覆盖：

- 逻辑候选有效优先级高于物理成员原始优先级时，普通选路和亲和选路得到相同候选层。
- 数据库状态比缓存新时，页面不会用数据库状态覆盖缓存有效状态。
- 逻辑渠道组关闭时，页面和请求仍使用相同的智能调度路由。
- 亲和、429 冷却、并发切换、失败重试和显式指定渠道的来源可区分。
- 请求大小限制、稳定性试放和探索流量不因统一快照改造而改变。

### 阶段二：实现统一运行态快照

先完成 model 层快照和候选、成员两阶段选路，再让缓存选路、数据库选路和亲和选路接入。此阶段不改变前端展示文案和统计口径，只确保后端决策一致。

### 阶段三：接入监控接口和真实分布

将页面路由响应切换到统一快照，增加快照状态和真实请求分布字段，并保留旧字段兼容已有客户端。

### 阶段四：更新页面展示

将预计分布和实际分布分开展示，补充刷新状态和路由来源提示，完成前端行为测试。

### 阶段五：灰度和回归

先在开启渠道监控、开启亲和、开启逻辑渠道组和存在重试流量的环境灰度，重点观察：

- 页面候选层与普通请求首选渠道是否一致。
- 亲和命中后是否仍停留在同一候选层。
- 逻辑成员权重是否只影响最终物理成员选择。
- 实际分布统计是否区分最终请求和尝试请求。
- 快照刷新失败时是否明确降级而不是显示 0% 或错误 100%。

## 7. 验证要求

### Go 测试

至少运行：

```powershell
go test ./model -run 'SmartSchedule|ChannelSmartSchedule' -count=1
go test ./service -run 'ChannelAffinity|ChannelConcurrency' -count=1
go test ./controller -run 'ChannelMonitorSmartSchedule|ChannelConcurrency|RelayRetry' -count=1
```

新增或大幅改写的测试使用 `require` 做前置和致命断言，使用 `assert` 做非致命值断言。测试必须断言路由候选、最终物理渠道和来源等真实行为，不能只验证函数能够运行。

### 数据库矩阵

如果实现涉及新的查询、模型字段、迁移、快照持久化或数据库行为，必须使用真实实例验证 SQLite、MySQL 和 PostgreSQL。验证记录需要包含：

- 精确数据库版本。
- 启动命令和测试命令。
- 全新数据库迁移结果。
- 从最近发布版本数据库升级的结果。
- 启动和迁移至少执行两次，确认幂等。
- 现有数据、索引、约束和唯一性保持不变。
- 主数据库和单独日志数据库均已覆盖时的结果。

如果实现完全复用现有数据库结构，也要说明这一点，并验证新增查询在三种支持的数据库驱动上均通过。

### 前端检查

```powershell
cd web
bun run test -- src/features/channel-monitor/lib/__tests__/smart-schedule-summary.test.ts
bun run typecheck
bun run lint -- src/features/channel-monitor
bun run build
```

还应增加或更新组件测试，覆盖预计分布、实际分布、快照刷新中、数据不完整、亲和来源和重试来源等用户可见行为。

### 完成前检查

```powershell
git diff --stat
git diff --check
```

实施完成后需要列出所有修改文件，并说明其中哪些是上游已有文件、为什么必须修改。不得修改项目身份、组织身份、模块路径或既有版权归属信息。

## 8. 验收标准

1. 同一份有效运行态快照下，普通选路、亲和选路和页面候选层使用相同的有效优先级和逻辑候选。
2. 亲和缓存不能把请求带到智能调度当前最高候选层之外；显式指定渠道仍按显式语义处理并单独标记来源。
3. 页面显示的预计分布与当前候选层和权重一致，刷新中或过期时有明确状态。
4. 页面可以查看真实请求分布，并明确统计窗口、最终成功口径和尝试口径。
5. 失败重试、并发重选、429 冷却、探索流量、稳定性保护和请求大小限制均有回归覆盖。
6. SQLite、MySQL 和 PostgreSQL 验证结果完整记录，前端 typecheck、lint、定向测试和构建全部通过。

## 9. 实施结果与验证记录

### 已实施

- [x] 增加回归用例，复现逻辑候选优先级、零权重亲和、缓存与数据库状态混用、关闭逻辑组、冷却成员分流问题。
- [x] 亲和资格采用普通选路的有效候选层和零权重规则；缓存亲和成员选择与候选复核在同一读锁内完成。暂不可用时保留亲和映射，当前请求回到普通选路。
- [x] 监控运行态的优先级、权重、参与状态、暂停、稳定性、临时流量状态、请求大小限制和渠道启用状态统一取自缓存。配置与历史得分仍保留数据库原值。
- [x] 路由清单、有效字段、快照版本同锁读取；补入数据库已删除但缓存尚未发布删除的路由。关闭内存缓存时复用数据库路径，并明确返回有效的数据库运行态。
- [x] 429 冷却按实际选路的物理排除顺序参与逻辑候选折叠。前端有效参与状态不再被数据库新配置覆盖，冷却成员不再平分可用成员的预计流量。
- [x] 管理员日志及监控事件补充每次尝试的来源、候选 ID、物理 ID、逻辑组、快照版本和尝试序号。正常缓存选路及缓存亲和在选择时记录版本；数据库与显式指定渠道不伪造缓存版本。
- [x] 新增 `actual_traffic`，直接读取 Redis 原始业务尝试窗口，按分组和实际路由模型分别计算尝试数、最终成功数、重试请求数、来源及逻辑成员数。
- [x] Redis 窗口保留未参与评分的业务事件，但评分与健康汇总继续过滤它们。请求 ID 仅以 SHA-256 指纹进入健康窗口，不写入原文；新增字段为可选 JSON，不改表结构。
- [x] 页面展示预计分布与实际分布、窗口、快照时间与版本；缺失或过期快照隐藏预计占比；不完整实际窗口隐藏占比，保留已知计数。Redis 读取失败仍返回路由与中文降级状态。
- [x] 智能调度详情打开期间每 30 秒刷新，后台标签页不轮询，摘要保持手动刷新。页面刷新时间与请求事件截止时间分开判断。

### 实际范围与影响

1. 预计占比仍表示当前候选层的理论分流。请求路径约束、请求大小、亲和、重试、并发和 429 会改变实际结果；修复不承诺理论占比等于历史业务占比。
2. 实际分布分母为当前页面路由清单内、同分组和模型的物理渠道。包括备用、排除或禁用渠道在窗口内发生的历史调用；已经完全移出清单的渠道不属于该分母，接口用 `actual_traffic_scope=listed_routes` 明示范围。
3. 最终成功按请求指纹去重；尝试按事件 ID 去重，允许一个请求贡献多次尝试。每行重试请求数只在该物理渠道内去重，不可相加作为全池独立请求数。探测、未发出请求和最终重试汇总不计入。
4. 逻辑成员和 429 标记是独立维度，不与来源数相加。429 标记表示本次选路应用了冷却筛选或冷却兜底，并不证明某个具体请求原本一定会选中哪条渠道。
5. 首次启用新统计口径、窗口被截断、旧事件缺少模型归属或请求指纹时显示覆盖不完整。历史事件不会凭空补出来源或精确请求数；滚动升级期间也可能出现未记录来源。
6. 调度候选修正会把原先越过最高层的亲和请求重新分配到有效候选层，可能改变这些请求的渠道和供应商成本。计费公式、配额、数据库 schema 和 Redis 路由快照格式不变。
7. Redis 每个样本增加有限的归属字段，并保留以前被评分资格过滤的业务事件；仍受原有保留时长和样本上限约束。详情刷新复用现有批量窗口读取，不增加逐渠道 SQL 统计。
8. 清除了 HEAD `91b5c4912` 中 `controller/channel_ratio_monitor_schedule_probe.go` 文件尾部 6 行落在函数外的残留代码。这是继续编译验证所必需的修复，未改动探测端点选择。

### 数据库与 Redis

没有 schema、迁移、驱动、账务字段或日志数据库查询变更，因此不涉及发布版数据库升级迁移测试。新增/调整的数据库选路及运行态读取在以下真实实例运行通过：

| 引擎 | 精确版本 | 结果 |
| --- | --- | --- |
| SQLite | 3.50.4 | 普通选路、亲和、逻辑覆盖、排除、关闭逻辑组均通过 |
| MySQL | 8.4.10 | 同上 |
| PostgreSQL | 18.6，Alpine x86_64，GCC 15.2.0 | 同上 |
| Redis | 8.8.0，64-bit，jemalloc 5.3.0 | 实际 Lua 写入、重复事件去重、指纹与路由信息读取通过 |

使用独立临时容器，未操作本机已有 MySQL/Redis 数据。可复现命令（PowerShell；密码仅用于临时测试实例）：

```powershell
$docker = 'D:/Docker/Docker/resources/bin/docker.exe'
& $docker run -d --rm --name schedule-fix-mysql -e MYSQL_ROOT_PASSWORD=schedule-test -e MYSQL_DATABASE=schedule_fix -p 127.0.0.1:23306:3306 mysql:8
& $docker run -d --rm --name schedule-fix-postgres -e POSTGRES_PASSWORD=schedule-test -e POSTGRES_DB=schedule_fix -p 127.0.0.1:25432:5432 postgres:18-alpine
& $docker run -d --rm --name schedule-fix-redis -p 127.0.0.1:26379:6379 redis:latest
$env:SCHEDULE_FIX_MYSQL_DSN='root:schedule-test@tcp(127.0.0.1:23306)/schedule_fix?charset=utf8mb4&parseTime=True&loc=Local'
$env:SCHEDULE_FIX_POSTGRES_DSN='host=127.0.0.1 port=25432 user=postgres password=schedule-test dbname=schedule_fix sslmode=disable'
go test ./model -run '^TestSmartScheduleRoutingDatabaseMatrix$' -v -count=1
$env:SCHEDULE_FIX_REDIS_ADDR='127.0.0.1:26379'
go test ./service -run '^TestChannelMonitorTrafficRedisCompatibility$' -v -count=1
& $docker stop schedule-fix-mysql schedule-fix-postgres schedule-fix-redis
```

### 回归检查

- `go test ./model ./service -run 'SmartSchedule|ChannelAffinity|LogicalAffinity|ChannelRouting|ChannelMonitorTraffic|RedisRouteHealth|RedisLogicalAggregator|ChannelConcurrency|ChannelRateLimit|ChannelSelection|PerformanceTiming' -count=1`：通过。
- 最终将 `./middleware` 加入上述范围，并追加 `Distribut|SetupContext`：model、service、middleware 全部通过，覆盖分发和请求上下文接入。
- `go test ./service -run 'ChannelMonitorTrafficRedisCompatibility|ChannelMonitor.*Event|ChannelMonitor.*Redis|PerformanceTiming' -count=1`：通过，包括真实 Redis 用例（设置上面的环境变量）。
- `go test ./controller -run 'GetChannelMonitorSmartSchedule|SmartScheduleActualTraffic|ChannelConcurrency|RelayRetry' -count=1`：通过；覆盖纯倍率池也加载业务统计、Redis 错误返回降级路由。
- controller 扩大到所有 SmartSchedule 测试时，以下 5 项在原始 HEAD 隔离工作树中同样失败（基线仅移除了上述文件尾部语法残留以便编译）：`TestProtectChannelSmartScheduleRuntimeFailureIgnoresMinimumSamples`、`TestProtectChannelSmartScheduleRuntimeFailureDoesNotRecountPersistedErrors`、`TestRunChannelSmartSchedulePersistsExecutionTimeScoreDetails`、`TestPlanChannelSmartScheduleUsesHysteresisAndForceReset`、`TestRunChannelSmartScheduleManualPrimaryAllowsStabilityDegrade`。未改动这些策略实现或放宽其断言；用 `-skip` 显式排除这 5 项后，其余 SmartSchedule、ChannelConcurrency、RelayRetry 全部通过。
- `cd web; bun run test -- src/features/channel-monitor`：81 个文件、430 项通过；随后新增页面刷新时间回归，4 个受影响测试文件 63 项通过。
- `bun run typecheck`、`bunx oxlint -c .oxlintrc.json src/features/channel-monitor`、`bun run build`：通过。
- `go build ./...`：通过。额外尝试 `go test -race` 时当前 Windows Go 环境因 `CGO_ENABLED=0` 拒绝运行，未取得竞态检测结果。
- Playwright + Edge 模拟 API 验证真实页面：1440x1100、390x844，预计和实际分布均渲染，无页面水平溢出、无 pageerror。使用模拟业务数据，未访问生产接口。
- `git diff --check`：通过。没有提交或发布。
- 验证结束后已停止并自动删除三个测试容器，移除基线隔离工作树。前端开发服务保留在 `http://localhost:5173/channel-monitor`，使用时需连接实际后端（默认 `http://localhost:3000`）；浏览器验证的模拟数据不会写入该服务。

### 上游文件与发布

与 `upstream/main` 对比，本次只有两个上游已有文件需要接入：`service/channel_select.go` 增加一行请求选路观察器；`service/channel_affinity.go` 增加一行管理员路由记录，使成功和失败日志共用现有扩展入口。其余改动全部位于下游渠道监控/智能调度模块或新增测试文件；未触碰 `relaykit` 模块和项目归属信息。

发布时需要同时更新后端与前端，并在新事件填满所配置窗口后比较完整实际占比。灰度重点观察：亲和是否回到有效最高层、备用流量的重试与并发来源、Redis 样本截断率以及后台刷新负载。
