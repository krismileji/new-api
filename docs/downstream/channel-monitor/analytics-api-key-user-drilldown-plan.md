# 渠道监控分析卡片：用户下钻与数据核查方案

- 记录日期：2026-09-08。
- 核查基线：当前代码 `9f49b8e54`；本地 `upstream/main` 为 `67a0585d0`。
- 状态：已实施本轮修复；最新处理状态与验证结果见第 9 节。
- 范围：渠道监控中的「渠道成本分析」和「成功率与缓存分析」，以及打开它们的首页统计卡片。

## 1. 需求

两个分析弹窗的「API Key 明细」页签统一采用下面的展开顺序：

```text
用户（用户名、显示名、用户 ID）
└─ 该用户的 API Key（名称、Key ID）
   └─ 该 Key 调用的模型
      └─ 该模型实际使用的物理渠道
```

初始只展示当前统计范围内有数据的用户；点击用户才加载其 Key，点击 Key 才加载模型，点击模型才加载渠道。用户、Key、模型和渠道每层均显示对应的汇总指标。API Key 指客户端使用的入站令牌，不是渠道配置中的上游密钥，列表不展示完整密钥。

保留「渠道汇总」现有的 `渠道 → 模型 → 用户 → API Key` 顺序。保留北京时间、默认当日、最多 90 天、排序和搜索等现有能力。每次下钻都必须继承日期和全部父级筛选；从单个渠道进入时，还要始终保留该渠道的限制。

本文保留修改前的调查依据，并持续记录实施结果。第 3 节描述基线问题，第 9 节记录当前已完成的修复和验证边界。

## 2. 当前入口与数据来源

### 2.1 应修改的实际入口

首页在 `web/src/features/channel-monitor/index.tsx` 中通过 `openCostHistory`、`openSuccessAnalytics` 打开同一个 `ChannelMonitorAnalyticsDialog`，由 `metric='cost' | 'success'` 区分指标。

| 职责 | 当前文件或接口 |
| --- | --- |
| 根分组、日期、搜索、总览、根分页 | `web/src/features/channel-monitor/components/channel-monitor-analytics-dialog.tsx` |
| 展开行、子级查询、指标单元格 | `web/src/features/channel-monitor/components/channel-monitor-analytics-table.tsx` |
| 两个页签的下钻顺序 | `web/src/features/channel-monitor/lib/analytics-expansion.ts` |
| 查询参数与响应类型 | `web/src/features/channel-monitor/api.ts`、`types-analytics.ts` |
| 请求缓存和轮询 | `web/src/features/channel-monitor/hooks/use-channel-monitor-analytics.ts` |
| 两个弹窗实际请求 | `GET /api/channel_monitor/analytics/rows` |
| 参数解析、历史查询、用户名称补全 | `controller/channel_monitor_analytics.go` |
| 今日筛选分组、跨日合并 | `controller/channel_monitor_current_analytics.go` |

旧的 `channel-monitor-api-key-cost-table.tsx`、`channel-monitor-success-api-key-table.tsx` 已有用户分组代码，但不是这两个新分析弹窗的渲染入口。只修改旧表格无法解决当前需求。

### 2.2 统计数据链路

| 指标与日期 | 当前读取方式 | 核查重点 |
| --- | --- | --- |
| 今日成本 | Redis 日成本投影；其金额来自可靠成本账本投影 | 不能把实时读缓存当作独立账务事实源；检查投影积压及归属缺口 |
| 历史成本，未限定用户/Key/模型的渠道、日期汇总 | `ChannelDailyCost` | 包含不能完整归属到用户或模型的历史成本 |
| 历史成本，用户/Key/模型及受筛选明细 | 有明细表时读 `ChannelMonitorDailyCostDetail` | 检查是否已回填、与渠道日账是否对平 |
| 今日成功率和缓存 | Redis 日成功指标中的 `Facts`，兼容旧 `Rows` 路径 | 必须先按全部筛选过滤事实，再分组，不能把多种预聚合层重复相加 |
| 历史成功率和缓存 | `ChannelMonitorDailySuccessLedger`，表名 `channel_monitor_daily_success_metrics` | 核对日持久化检查点和历史覆盖范围 |
| 包含今日的跨日查询 | 昨日及以前取数据库，今日取 Redis，再合并 | 今日不能同时从数据库和 Redis 计算两次；比例合并后重算 |

接口已支持 `group_by=user`、`user_id`、`api_key_id`、`model`、`channel_id`，且批量查询用户名和显示名。现有表中也已有用户、Key 和模型维度，因此主流程不需要为“先显示用户”新建统计表。

## 3. 修改前的问题

下列“已确认”指修改前基线代码可以确认的问题，不代表已在生产环境复现。修复结果见第 9 节；线上历史缺口需结合实际覆盖提示和账本核验。

### P1-01：API Key 页签缺少用户层（已确认）

`channel-monitor-analytics-dialog.tsx` 将 API Key 页签根分组设为 `api_key`；`analytics-expansion.ts` 只定义 `api_key → model → channel`，没有该页签的 `user → api_key`。

影响：不同用户的 Key 直接混排，无法先看到用户总成本、成功率与缓存指标；同名 Key 也不易区分归属。

修改：API Key 页签根分组改为 `user`，补齐 `user → api_key → model → channel`。两个指标共用这一流程，避免成本与成功率各维护一套实现。

### P1-02：子级明细只展示前 20 条（已确认）

`queryFromExpansionContext` 固定使用 `page: 1, pageSize: 20`。展开行只渲染 `childResponse.items`，没有消费子级 `total/page/page_size` 来提供翻页或继续加载。

影响：用户超过 20 个 Key、Key 超过 20 个模型，或模型超过 20 个渠道时，后面的记录无法查看。父级是完整汇总，但用户看到的子级合计偏小，容易误判统计错误。

修改：每个展开节点独立分页或“加载更多”，明确显示已加载条数和总条数。父级指标始终使用后端全量汇总，不能改成当前可见子行相加。按需加载，不一次拉取所有用户的所有后代。

### P1-03：未知用户、未知 Key 无法精确下钻（已确认）

现有行可以返回 `user_id=0` 或 `api_key_id=0`，例如未归属历史成本。前端下钻会把该 ID 传入请求，但 `channelMonitorAnalyticsPositiveInt` 拒绝显式的 `0`，返回 400。后端查询结构同时用零值表示“未指定筛选”，不能直接通过放宽校验解决。

此外，后端 Key 分组身份为 `api_key_id + api_key_key + user_id`，前端当前只向下传 `apiKeyId`。当同一 ID 下存在多个历史指纹分组时，仅按 ID 查询不能精确对应被点击的那一行。

修改：区分“未传筛选”和“精确筛选未知 ID”。建议用可选整数或显式的参数存在标记：未传表示全部，显式 `0` 表示未知，正数表示指定对象，负数仍拒绝。Key 下钻同时保留用户和指纹等完整分组身份；指纹只使用现有不可逆标识，不传输完整密钥。

“未归属用户 / 未识别 API Key / 未知模型”必须保留，既不能静默丢弃，也不能因缺少 ID 而放宽为全部数据。系统探测等没有真实用户的成本也需要说明来源，不能伪装成普通用户。

### P1-04：后台刷新会丢失已展开内容（已确认代码路径）

`useChannelMonitorAnalytics` 对包含今日的范围每 5 秒刷新。根表和子表都在 `isFetching` 时拒绝使用已有响应：根表改成骨架屏，子级改成加载行。展开状态保存在行组件本地，因此根表刷新会卸载行组件，子表刷新会卸载更深层的行。

影响：用户可能刚展开到模型或渠道，就因轮询、重新聚焦窗口或重连被收起；增加用户这一层后会更难操作。

修改：同一查询的后台刷新保留已成功返回的数据，只展示轻量刷新状态；首次加载或切换到没有缓存的新范围才显示骨架屏。展开状态按稳定的维度身份和查询范围管理。日期、页签、搜索、指定渠道变化时重置对应状态，不能只凭 `group_by` 相同接受另一范围的旧响应。

### P1-05：历史成本汇总与明细缺口未被完整表达（已确认条件性问题）

`queryChannelMonitorHistoricalCostAnalytics` 的渠道汇总读取 `ChannelDailyCost`；只要明细表存在，用户、Key、模型查询就读取 `ChannelMonitorDailyCostDetail`。`queryChannelMonitorHistoricalCostDetailAnalytics` 没有对照日账或回填检查点，直接用空原因列表返回覆盖完整。

可由当前代码推导的场景：某历史日渠道账本有 10 元，明细表已创建但该日尚未回填。渠道页签显示 10 元，用户页签为 0 或空列表，却仍报告 `complete`。

对比：今日读取 `service/channel_monitor_redis_daily_cost.go` 时已经计算“渠道账本减已归属明细”，把差额放入未知项，并标记 `cost_attribution_incomplete`；历史路径没有等效处理。

修改：核对历史回填记录及日账与明细的金额、已结算数、未解析数。缺口应展示未知归属差额或明确“明细未完整回填”，并返回 `partial`。差额不得当作用户已归属消费。复用现有幂等回填能力，不得重放权威账本或直接修改账务金额。

### P2-01：名称搜索与今日、历史搜索语义不一致（已确认）

前端提示可搜索渠道、用户或 Key，但今日匹配主要检查 Key 名、指纹、模型名和各类 ID；历史 SQL 也没有匹配用户名、显示名或渠道名称。用户名称在分页后才补全，不能支撑筛选。

今日还对 ID 做字符串包含匹配，历史对数字输入采用精确 ID 匹配。例如没有其他名称命中的情况下，搜索 `12` 可能在今日命中 ID `312`，历史则只匹配 ID `12`。

修改：按统一契约支持用户名、显示名、用户 ID，以及已有 Key/模型/渠道搜索。名称匹配和 ID 规则在 Redis 与数据库路径保持一致。名称应在分页和聚合前解析为受限的 ID 集合；不能只在当前 20 行上过滤。搜索沿下钻保留，并明确总览对应搜索后的完整范围。

### P2-02：指标口径与首页筛选上下文不够清楚（已确认）

- 弹窗的“调用数”和“成功率”使用 `actual_*`，代表已实际派发的上游尝试，包含重试；不是按客户端请求去重后的 `final_*`。两个字段都有返回，但目前只展示前者。
- `channelMonitorRedisSharedEventDeltaFromEvent` 只把流式请求的输入、缓存读取 token 纳入“缓存利用率”；不能理解为全部调用的缓存命中率。
- “缓存写入”实际是 `cache_write_request_count`，表示有缓存写入 token 的请求尝试次数，不是写入 token 数、金额或比例；其流式范围与缓存利用率也不同。
- 首页成功率卡片可单独选择 Key 作为缓存利用率口径，但 `openSuccessAnalytics` 没有把该选择传入弹窗。弹窗打开后是全部范围，数值可能与刚才选择的 Key 不同。
- 成本总额包含业务、探测、模型检测等来源；分组探测已属于探测成本，不能再次扣减。API Key 页签需要解释系统成本和历史未归属金额。

修改：增加简洁的指标说明、分子分母和单位；例如“上游尝试数”“上游成功率”“流式缓存利用率”“缓存写入次数”。打开弹窗时应明确“全部范围”，或显式携带所选 Key 的筛选状态，不能让局部卡片数值与全量弹窗无说明地对照。API Key 页签默认仍先展示用户。

### P2-03：子级覆盖状态和历史覆盖边界提示不足（已确认代码路径）

根弹窗会展示根响应的 `coverage`，展开行没有展示子响应的覆盖状态。如果根渠道金额完整、子级归属不完整，根上的“完整”不能替代子级状态。

历史成功查询会检查已存在检查点的 `coverage_partial`，但没有验证请求范围中缺失的检查点或统计启用前的覆盖边界。缺少记录不总是“当天没有请求”，应结合持久化进度区分。

修改：每层使用自己的覆盖信息；展示数据更新时间及统计不足的中文原因，区分无样本、未采集、回填中、投影不可用。对历史覆盖边界的判断需要结合实际检查点和保留策略，不能只凭空表判错或判完整。

## 4. 数据合理性结论与应保留的规则

核查时确认公式和主要读源设计有合理基础。本轮针对分组、筛选、明细覆盖与口径展示进行修复，验证结果见第 9 节；覆盖不足的数据仍不能被当作完整统计。

| 指标 | 当前计算方式 | 应保留或补充的约束 |
| --- | --- | --- |
| 成本 | `cost_nano_cny / 1_000_000_000`，换算为人民币元 | 从整数纳元汇总后再格式化；这是渠道成本，不是用户扣费金额 |
| 已结算、未解析 | 分别累计 `settled_count`、`unresolved_count` | 未解析不能按免费处理；零元已结算仍是已结算 |
| 解析率 | `Σ已结算 / (Σ已结算 + Σ未解析)` | 分母为零显示 `-`，不能简单平均子级百分比 |
| 上游尝试数 | `actual_success_count + actual_failure_count` | 重试单独计入，探测不混入业务成功率 |
| 上游成功率 | `Σactual_success_count / Σactual_sample_count` | 不能误标为最终请求成功率；无样本显示 `-` |
| 最终请求成功率 | `Σfinal_success_count / Σfinal_sample_count` | 现有字段可用于对照，不能直接替换上游尝试口径 |
| 流式缓存利用率 | `Σcache_read_tokens / Σinput_tokens` | 仅按当前约定的流式样本计算；按 token 加权 |
| 请求缓存命中率 | `Σcache_hit_count / Σcache_sample_count` | 与 token 利用率是不同指标，不能互换 |
| 缓存写入次数 | 有正数缓存写 token 的请求尝试数 | 单位“次”，不是缓存写 token 总量 |
| 顶部总览 | 后端 `scope_summary`，当前与 `summary` 同范围 | 对完整筛选范围汇总，不随当前分页变成页内小计 |

日期参数采用北京时间的左闭右开区间 `[from, to)`，界面选择的结束日会转换为次日零点。当前跨日实现将历史范围截到今日零点，今日只读取一次；后续修改必须保留这个边界。

缓存 token 是否规范化为“缓存读属于总输入的一部分”，以及不同提供方是否完整上报，需要用实际事件和日志抽样核对。不能在尚未核对输入口径时，简单通过把异常百分比截到 100% 来掩盖问题。

## 5. 修改方案

### 5.1 普通用户的查询链

所有请求使用同一个 `/api/channel_monitor/analytics/rows` 接口，并继承 `metric/from/to/search/sort/direction` 和入口的 `channel_id`：

| 操作 | `group_by` | 累积筛选 |
| --- | --- | --- |
| 打开 API Key 页签 | `user` | 当前日期及入口渠道范围 |
| 展开用户 U | `api_key` | 上述条件 + `user_id=U` |
| 展开 Key K | `model` | 上述条件 + `api_key_id=K`，并保留完整 Key 分组身份 |
| 展开模型 M | `channel` | 上述条件 + `model=M` |
| 点击渠道叶子 | 无新增下钻 | 保持当前用户、Key、模型和物理渠道归属 |

用户行使用用户 ID 作为身份，用户名和显示名只用于展示；删除或缺少用户记录时回退为“用户 #ID”。Key 使用用户、Key ID、指纹组合身份，同名 Key 不合并。模型使用稳定模型标识，渠道使用物理渠道 ID。

今日 `Facts` 和历史表已有完整维度，可直接复用其先筛选后分组逻辑。但旧 Redis `Rows` 路径必须额外验证：当前部分模型/渠道分支要求 `row.UserID == 0`，与新增的 `query.User > 0` 会冲突。应优先让可恢复数据走事实投影；如保留旧路径，则必须支持完整的新筛选链，不能去掉用户条件来凑出结果。

### 5.2 前端实施范围

1. 修改共享弹窗与展开顺序，补齐用户层、每层的名称/ID 和维度说明。
2. 在共享展开表中增加子级分页、子级覆盖提示，并保留同范围后台刷新时的数据和展开状态。
3. 扩展类型、查询参数及缓存键，保留未知 ID 和 Key 的完整身份；缓存键包含全部筛选和分页。
4. 补充指标单位与口径说明，处理首页缓存 Key 选择与弹窗范围的说明或传递。
5. 使用现有 Base UI 表格、按钮和加载/错误组件，继续按需请求。无需新增 UI 库。

### 5.3 后端与对账实施范围

1. 统一今日、历史、跨日的筛选语义，补齐未知身份、Key 指纹和名称搜索。
2. 保持“过滤事实 → 分组 → 汇总原始计数 → 重算比例 → 排序分页”，禁止对已分页结果二次聚合冒充用户总额。
3. 为历史成本明细引入日账对账和回填状态判断，沿用已有 `ChannelMonitorCostBackfillCheckpoint`、`ChannelMonitorCostReconciliation` 等能力。主账金额不因展示修复而变动。
4. 明确未归属差额、业务成本、探测成本和模型检测成本之间的关系。不能把原本未知的成本推给当前某个用户。
5. 批量补充展示名称，保留历史冻结的用户 ID；用户或 Key 删除、改名不能导致历史统计被删除或转移。
6. 根据实际日检查点、持久化进度和保留边界返回覆盖状态；根汇总与子明细可以有不同的完整性。

现有统计表字段能够支撑主要需求，初步不需要新建表或升级依赖。若实施时发现必须更改表结构，应单独说明原因并执行迁移验证，不能把展示调整扩大成无关重构。

## 6. 实施顺序与回归范围

1. 先用最小测试数据复现：缺少用户层、未知 ID 被拒绝、子级第 21 条不可见、后台刷新丢失展开、历史账本与明细缺口。
2. 明确未知筛选与完整 Key 身份的接口契约，统一今日/历史/跨日查询。
3. 修改共享展开表和 API Key 根层，完成分页与刷新状态保留。
4. 补齐对账、覆盖提示、指标说明和首页入口上下文。
5. 完成以下验收后再宣称功能与数据修复完成。

| 验收场景 | 预期 |
| --- | --- |
| 两个用户、多个 Key、同名 Key、同一模型走多个渠道 | 两个卡片都严格按用户 → Key → 模型 → 渠道展开，没有串用户或串 Key |
| 用户改名、Key 改名/删除、未知用户/Key/模型 | 历史归属稳定；未知项可精确查看；不返回 400，也不扩大范围 |
| 任一层超过 20 条、根翻页、子翻页 | 所有结果均可到达；完整汇总不随分页改变 |
| 自动刷新、窗口重新聚焦、网络重连 | 已展开层级保留；失败提示可重试；首次加载与后台更新有区别 |
| 日期、搜索或入口渠道变化 | 不显示其他范围的数据；原筛选完整继承 |
| 1 次失败后重试成功 | 上游尝试为 2、上游成功率为 50%；最终成功为 1/1，文案能解释差异 |
| 两组流式输入 100/900，缓存读 80/90 | 总缓存利用率为 170/1000=17%，不是 (80%+10%)/2 |
| 只有非流式调用、无输入 token、零样本 | 缓存利用率按既定样本规则显示；无分母显示 `-`，不伪造 0% |
| 未解析转已结算、任务最终修正、重复事件 | 不重复累计成本或请求数，不改变权威账本归属日期 |
| 探测、分组探测、模型检测与业务混合 | 成本分项对平；分组探测不重复扣除；成功率只包含业务口径 |
| 今日、纯历史、跨日的同一组数据 | 不漏今日、不重复今日；各层原始计数对平，比例重新计算 |
| 历史日账有金额、明细未回填或部分回填 | 显式显示未知差额或明细不足；不得返回完整的零明细 |
| Redis 不可用、回放未完成、持久化缺口 | 显示错误或不完整，不能伪装成“完整且没有调用” |

涉及数据库查询、分组、回填或迁移的实施，必须在真实 SQLite、MySQL、PostgreSQL 上验证。现有内存 SQLite、miniredis 测试不能替代三数据库矩阵。可扩展 `controller/channel_monitor_consistency_matrix_test.go` 的专用环境用例，但用例因环境变量缺失而跳过不算验证完成。

如果修改迁移，另需在全新库和最新发布版代表性旧库上执行升级、至少重复两次启动/迁移，并覆盖受影响的独立日志库。最终记录精确数据库版本、命令和结果；无法执行时明确记录阻塞。前端修改后执行相关交互测试、`bun run typecheck`、涉及文件的 lint 和必要的构建检查。

## 7. 调查阶段记录及线上对账边界

调查阶段只读取代码、既有文档和测试，并新建此方案。实施阶段也没有读取生产请求日志、调用线上监控接口、执行线上回填或调整生产数据库。

调查阶段已执行：

- 在 `web/` 运行 `bun run test src/features/channel-monitor/components/__tests__/analytics-dialog-expansion.test.tsx src/features/channel-monitor/components/__tests__/analytics-table.test.tsx`：2 个测试文件、10 个用例通过。它们验证的是现有展开、日期、排序和表格行为，尚未覆盖本方案的新层级及发现的缺陷。
- 在项目根目录运行 `go test ./controller -run '^TestChannelMonitor(Analytics(Current|Cost|Historical)|CurrentAndMixedCostAnalytics|CurrentSuccessFiltersFacts)' -count=1`：通过。现有用例覆盖分页总览稳定、用户名称、Key 明细、Redis 失败、成本下钻、跨日不重复今日，以及事实先筛选再分组等行为；测试使用内存 SQLite 和 miniredis，不代表真实三数据库或生产 Redis 验证。

尚需实施阶段核验：选取同一北京时间日期及固定数据截止点，对照事件/日账、接口完整分页结果和页面展示。优先包含一个普通用户、一个同名 Key 场景、一个发生重试的模型、一个有流式缓存的模型，以及存在历史未知成本的日期。记录金额纳元、各类原始计数、缓存 token、`snapshot_revision`、`processed_at`、`coverage`，避免把不同刷新时刻的变化误当成统计错误。

调查阶段未执行三数据库矩阵；实施阶段已补做，详见第 9 节。尚未在生产环境进行页面与历史账本抽样对账，不能据此宣称线上旧数据已经补齐。

## 8. 下游与上游边界

已阅读 `.agents/downstream.md`，并通过 `git ls-tree upstream/main` 核对本次涉及的渠道监控目录和关键控制器/统计文件：这些目标路径在当前本地上游基线中不存在，属于下游功能。

本轮修改位于既有下游渠道监控分析模块及其测试中，没有修改既有上游文件。下游界面使用简体中文文案，没有增加或修改官方 locale 文件。保留 new-api 与 QuantumNous 的项目名称、标识和归属信息。

## 9. 实施结果（2026-09-08）

### 9.1 已完成的修复

| 问题 | 处理结果 |
| --- | --- |
| P1-01 用户层缺失 | 两个弹窗的 API Key 页签改为 `用户 → API Key → 模型 → 渠道`；保留全部父级筛选和入口渠道限制 |
| P1-02 子级截断 | 每个展开节点独立分页，显示页码、总条数；请求失败后可重试或返回上一页，父级总计不随分页改变 |
| P1-03 未知归属与 Key 身份 | `user_id=0`、`api_key_id=0` 表示精确筛选未知项；未传参数才表示全部。新增可选 `api_key_key`、`model_key`，保留空标识和完整分组身份 |
| P1-04 刷新丢失展开 | 同一查询的后台刷新保留数据和展开层级；更新失败保留旧结果并提示重试；日期、搜索、页签、分页等范围变化时重置相应状态 |
| P1-05 历史成本缺口 | 按物理渠道和日期核对日账与明细的金额、已结算数、未解析数和探测分项；存在缺口时返回 `cost_attribution_incomplete`，不再报告完整的零明细 |
| P2-01 搜索不一致 | 在分组、分页前解析用户名称/显示名和渠道名称；今日、历史共用精确数字 ID 与名称搜索范围，SQL 通配符按字面值处理。名称匹配超过 2000 个用户或渠道时明确提示缩小范围 |
| P2-02 指标解释不足 | 展示“上游尝试数 / 上游成功率 / 流式缓存利用率 / 缓存写入次数”，增加成功数、尝试数及缓存 token 分子分母；说明成本来源、未知归属及未解析金额，弹窗明确展示全部用户及 Key 的范围 |
| P2-03 覆盖提示不足 | 每层分别展示覆盖提示并转为中文说明；历史成功率检查每个请求日期的持久化检查点，区分有检查点的空闲日、缺失日和回放不完整；数据源不可用时不展示伪零总计或“暂无统计数据” |

录入开发演示数据后，流式缓存利用率的 Token 分子和分母进一步改为复用排行榜的 `K / M / B / T` 格式（按 1000 进位，T 表示万亿）。共享分析表格的所有层级和首页缓存摘要同步使用，例如 `152302827 / 288676800` 显示为 `152.3M / 288.7M`。表格悬停保留完整整数，比例仍使用原始数据。该显示调整的 3 个相关测试文件、24 个用例，以及类型、lint、格式和前端构建检查均通过；没有修改排行榜、数据库或统计计算逻辑。

旧 Redis 预聚合兼容路径也已调整：按用户筛选后重新分组，统一模型标识为成功率日账的既有格式；组合明细保留去重，避免把全局 Key 路由汇总和用户路由汇总相加。旧数据无法确认的归属返回不完整状态，不通过放宽筛选补出结果。

本轮采用“检测历史缺口并清楚提示”的处理方式，没有自动执行回填，也没有修改权威账本、用户扣费、表结构、数据库驱动依赖或 `relaykit/`。已有真实历史缺口仍需在目标环境使用现有幂等回填流程处理。

### 9.2 回归与构建验证

- 前端相关 6 个测试文件、34 个用例通过，包含真实查询 Hook 与 Axios 请求边界的交互测试：键盘展开、逐层筛选、未知身份、子级翻页、页加载失败返回、后台更新/失败重试、日期切换和子级覆盖提示。
- `bun run typecheck`、涉及文件的 oxlint 检查及 `bun run build` 通过。
- 后端相关分析测试通过，包含今日和历史的同一组筛选案例、原有跨日不重复今日用例、历史对账/检查点，以及旧缓存模型身份和重复汇总回归。
- 已检查 `git diff --check`；没有修改上游所有的既有文件。

前端测试命令（工作目录 `web/`）：

```powershell
bun run test src/features/channel-monitor/components/__tests__/analytics-dialog-expansion.test.tsx src/features/channel-monitor/components/__tests__/analytics-table.test.tsx src/features/channel-monitor/components/__tests__/analytics-user-drilldown.test.tsx src/features/channel-monitor/components/__tests__/analytics-refresh.test.tsx src/features/channel-monitor/components/__tests__/analytics-coverage.test.tsx src/features/channel-monitor/__tests__/api.test.ts --reporter=dot
bun run typecheck
bun run build
```

后端测试命令（项目根目录，不设置数据库矩阵环境变量）：

```powershell
go test ./controller -run '^TestChannelMonitor(Analytics|CurrentAndMixedCostAnalytics|CurrentSuccessFiltersFacts)' -count=1
```

### 9.3 真实数据库验证

数据库矩阵用例：`controller/channel_monitor_analytics_read_matrix_test.go`。测试只连接本地专用库，校验库名后初始化测试数据；不允许指向生产库。

| 数据库 | 实际版本 | 本次连接 | 结果 |
| --- | --- | --- | --- |
| SQLite | 3.50.4 | `.local-tests/cm-consistency-verification/analytics.sqlite`，真实文件数据库 | 通过 |
| MySQL | 8.4.10 | `127.0.0.1:56573/new_api_cm_schema_analytics`，独立 Docker 实例 | 通过 |
| PostgreSQL | 18.6 | `127.0.0.1:54050/new_api_cm_schema_analytics`，独立 Docker 实例 | 通过 |

三库执行同一组接口/查询案例：全部与未知归属、空 Key 标识、同 ID 不同指纹/用户、未知模型、用户名/显示名/渠道名称、数字 ID、通配符，以及逐渠道成本对账、缺失检查点和已持久化空闲日。

本次测试实例创建命令（Docker Desktop CLI，临时端口由 Docker 分配）：

```powershell
& 'D:\Docker\Docker\resources\bin\docker.exe' run -d --name new-api-analytics-01a07ed9-mysql -e MYSQL_ALLOW_EMPTY_PASSWORD=yes -e MYSQL_DATABASE=new_api_cm_schema_analytics -p 127.0.0.1::3306 mysql:8 --character-set-server=utf8mb4 --collation-server=utf8mb4_unicode_ci
& 'D:\Docker\Docker\resources\bin\docker.exe' run -d --name new-api-analytics-01a07ed9-postgres -e POSTGRES_HOST_AUTH_METHOD=trust -e POSTGRES_DB=new_api_cm_schema_analytics -p 127.0.0.1::5432 postgres:18-alpine
```

矩阵命令（项目根目录；以下端口为本次实际分配值，重新创建实例时以 `docker port` 为准）：

```powershell
$env:CHANNEL_MONITOR_CONSISTENCY_DIALECT='sqlite'
$env:SQL_DSN=''
$env:CM_UPGRADE_SQLITE_PATH=Join-Path (Get-Location) '.local-tests/cm-consistency-verification/analytics.sqlite'
go test ./controller -run '^TestChannelMonitorAnalyticsReadDatabaseMatrix$' -count=1 -v

$env:CHANNEL_MONITOR_CONSISTENCY_DIALECT='mysql'
$env:SQL_DSN='root@tcp(127.0.0.1:56573)/new_api_cm_schema_analytics?charset=utf8mb4&parseTime=true'
go test ./controller -run '^TestChannelMonitorAnalyticsReadDatabaseMatrix$' -count=1 -v

$env:CHANNEL_MONITOR_CONSISTENCY_DIALECT='postgres'
$env:SQL_DSN='postgres://postgres@127.0.0.1:54050/new_api_cm_schema_analytics?sslmode=disable'
go test ./controller -run '^TestChannelMonitorAnalyticsReadDatabaseMatrix$' -count=1 -v
```

本地原始结果保存在 `.local-tests/cm-consistency-verification/` 的 `sqlite-read-matrix.log`、`mysql-read-matrix.log`、`postgres-read-matrix.log`、`controller-tests.log`、`frontend-tests.log` 和 `frontend-build.log`。该目录被 Git 忽略，不作为产品代码提交。

验证完成后已停止并删除本次创建的 MySQL、PostgreSQL 测试容器及其匿名数据卷；SQLite 测试文件和验证日志保留在上述本地目录。

没有生产模型或迁移变更，因此本轮验证对象为现有表上的分析读取逻辑，不声称完成发布版数据库升级测试。分析接口读取主数据库；本轮未修改或扩展独立日志数据库路径。
