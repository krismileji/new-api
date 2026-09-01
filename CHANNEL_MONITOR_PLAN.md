# 渠道监控页面缓存与查询优化方案

> 方案与执行记录：按独立任务逐项验证；只保留服务于“去页面快照”或有证据查询优化的改动，避免长时间全仓库扫描。

## 1. 目标与边界

本次只处理两件事：

1. 去掉渠道监控页面把旧数据当成最新数据的缓存快照。
2. 只有在能复现重复请求、查询范围过大或未下推筛选/分页时，才做直接的查询优化。

这里的“页面缓存”专指浏览器端渠道监控页面的 React Query 数据，以及刷新/切页期间仍展示的旧结果。以下内容不在本次范围内：

- Redis、内存、业务结算、日志、任务运行时缓存。
- 新增缓存层、刷新队列、定时合并器、全局请求调度器或缓存版本/围栏机制。
- 无证据的索引、迁移、SQL 重写、接口字段改造、全局超时/取消改造。
- 移动端布局、计费、路由策略及其他渠道功能重构。

后端只有在页面接口的实际调用链仍复用“页面专用快照”时才修改；业务缓存即使存在，也不能为了本任务一并删除。

### 已知事实（避免重复设计）

- 渠道概览响应已经包含当前并发字段，主页面不需要再读取独立并发接口。
- 状态探测和模型检测的概览读取只用 `singleflight` 合并**进行中的**相同请求；完成结果不会保留，不能把它当成页面快照删除。
- 渠道监控 Redis projection 是实时聚合数据源，不是“已完成页面响应的缓存”。本轮只审计它的查询范围，不删除或重建它。
- 已存在一个页面内短生命周期的刷新 in-flight 防重入保护。任务 2 只验证它是否仍有必要，不扩展为队列、尾随刷新或跨页面机制。

## 2. 当前未提交改动的取舍

已确认与目标直接相关，纳入后续验证：

| 改动 | 价值 | 归属任务 |
| --- | --- | --- |
| 概览接口返回的并发字段由主页面直接复用，移除页面额外并发 GET | 减少一次重复请求，避免前端二次合并 | 任务 2 |
| 监控查询使用 `gcTime: 0`，关闭常规自动重试/窗口/重连刷新 | 页面离开后不保留快照，刷新失败不自动叠加请求 | 任务 2 |
| 手动刷新先清理活动查询旧结果，再读取新结果 | 刷新期间不把旧结果误显示为最新结果 | 任务 2 |
| 首次进入不额外手动刷新，模型检测页不读取无关性能数据 | 删除首屏重复或无关请求 | 任务 2 |
| 对应单测和渠道监控说明文档 | 固化可观察行为和验收口径 | 任务 5 |

不再纳入本方案的推测性改动：后端 RPM 分批/并发连接重写、Redis 版本围栏、刷新队列或时间窗合并、全局动作后刷新、批量动作限流、仅部分调用点的超时/`AbortSignal`、绕过现有 GET 去重的 `disableDuplicate`。除非后续出现明确复现证据，否则不恢复这些设计。

## 3. 独立任务

每项任务都有独立输入、允许修改范围和验收命令；没有问题时以“确认无需修改”结束。

### 任务 1：建立最小查询基线（只读）

**检查范围**

- 从渠道监控入口开始，沿实际调用关系检查各视图和已打开的弹窗：渠道、分组、模型性能、状态探测、模型检测、智能调度、成本/成功率明细，以及页面显示所依赖的分组顺序查询。
- 记录每项的 query key、接口、启用条件、分页/筛选参数、刷新入口，以及页面切换或刷新时是否仍展示旧数据。
- 只检查渠道监控直接使用的前端文件和后端调用链，不做全仓库搜索。

**产出与验收**

- 产出一张“页面/弹窗 → query key → 接口 → 请求条件 → 是否需要修改”的表。
- 只读命令：`rg`、`git diff`、必要的定向源码阅读；不改业务代码，不启动 Docker，不运行长任务。

#### 当前页面数据查询方案

以下是当前代码实际采用的查询方案，作为任务 1 的基线和任务 2/4 的验收对照。`channelMonitorRequestConfig` 继续使用现有的 GET 去重及 `DisableCache` 请求策略；表中的“活动”指 React Query 的 `enabled` 或 `type: 'active'` 条件。

| 页面/弹窗 | query key | 接口 | 请求条件/分页 | 活动与刷新策略 | 结论 |
| --- | --- | --- | --- | --- | --- |
| 主页面概览（渠道、分组、模型性能、智能调度共用） | `['channel-monitor']` | `GET /api/channel_monitor/` | 后端返回完整渠道、分组、设置、今日成本及并发字段；搜索、上游类型、分组、模型和排序在前端派生 | 常驻；手动刷新和视图切换仅刷新活动查询；`gcTime: 0` | 保留；已移除独立并发 GET |
| 主页面性能/成功率 | `['channel-monitor-performance', source, minutes]` | `GET /api/channel_monitor/performance?minutes=...` | `minutes` 为手动或智能调度窗口；后端使用 Redis 实时窗口 projection；渠道/分组/模型筛选在已返回指标上前端派生 | 仅渠道/分组/模型视图活动；状态探测和模型检测禁用；无普通轮询 | 保留；窗口条件已传递 |
| 主页面智能调度摘要 | `['channel-monitor','smart-schedule','routes','summary']` | `GET /api/channel_monitor/schedule?metrics=false` | 返回智能调度摘要和路由；页面按设置、分组策略过滤显示 | 主页面常驻；手动刷新仅在对应范围刷新 | 保留 |
| 智能调度主视图明细 | `['channel-monitor','smart-schedule','routes','metrics']` | `GET /api/channel_monitor/schedule?metrics=true` | 返回路由指标明细 | 仅 `view === 'smart-schedule'`；无普通轮询 | 保留；隐藏视图不请求 |
| 状态探测概览 | `['channel-monitor','status-probe',{model}]` | `GET /api/channel_monitor/status?model=...` | 模型筛选下推；返回当前渠道探测状态 | 仅状态探测视图；有活动任务时 1 秒轮询 | 保留 |
| 状态探测历史 | `['channel-monitor','status-probe','executions',channelId,latestExecutionId,{page,pageSize,model,result,trigger}]` | `GET /api/channel_monitor/status/channel/:id/executions` | `page_size=20`；模型、结果、触发器筛选下推；仅第 1 页带最新执行 ID | 按需打开；有活动探测时 1 秒轮询 | 保留；已有分页和筛选 |
| 模型检测概览 | `['channel-monitor','model-detection','overview']` | `GET /api/channel_monitor/model_detection` | 返回各渠道当前检测状态 | 仅模型检测视图；有活动任务时 1 秒轮询；不加载性能查询 | 保留 |
| 模型检测历史 | `['channel-monitor','model-detection','history',channelId,query]` | `GET /api/channel_monitor/model_detection/channel/:id/runs` | 页码、页大小、触发器、状态、模型、结果下推 | 选中渠道后按需加载；活动任务时轮询 | 保留；已有分页和筛选 |
| 模型检测运行详情 | `['channel-monitor','model-detection','run',runId]` | `GET /api/channel_monitor/model_detection/runs/:runId` | 单个运行 ID | 详情打开且有运行 ID 时加载；活动运行时轮询 | 保留 |
| 成本摘要 | `['channel-monitor','cost','summary',2]` | `GET /api/channel_monitor/cost?days=2&page=1&summary_only=true` | 仅近 2 天摘要，不加载日期、API key 或渠道明细 | 主页面常驻；手动刷新活动查询；无普通轮询 | 保留；摘要与明细分离 |
| 成本历史/明细弹窗 | `['channel-monitor','cost',channelId|all,days,datePage,detailDate]` | `GET /api/channel_monitor/cost` | `days`、渠道、日期页、日期下推；服务端按日期分页，API key 结果有上限 | 弹窗打开时加载；切换条件生成新 key | 保留；已有分页和范围限制 |
| 今日成功率弹窗/日历洞察 | `['channel-monitor','success','daily',days,date]` | `GET /api/channel_monitor/success/today?days=...&date=...` | 天数和选中北京时间日期下推 | 弹窗打开时加载；日期/天数变化重新查询 | 保留 |
| 成功率明细弹窗 | `['channel-monitor-success-detail',minutes,scope,target,model]` | `GET /api/channel_monitor/success/detail` | 性能窗口及渠道/分组/模型条件下推；响应有 API key/失败分类上限 | 明细打开时加载；手动刷新只刷新活动明细 | 保留 |
| 倍率/余额任务历史 | `['channel-monitor-task-history','ratio',page,pageSize]` | `GET /api/channel_monitor/tasks?p=...&page_size=...&kind=ratio` | 服务端分页，页面大小固定 | 弹窗打开时加载；任务执行期间由现有逻辑刷新 | 保留 |
| 智能调度执行历史 | `['channel-monitor-smart-schedule-executions',page]` | `GET /api/channel_monitor/tasks?p=...&page_size=...&kind=schedule` | 服务端分页；详情另按任务 ID分页并支持搜索、分组、模型、动作筛选 | 弹窗打开时加载 | 保留 |
| 渠道倍率历史 | `['channel-monitor-history',channelId]` | `GET /api/channel_monitor/channel/:id/history?p=1&page_size=100` | 单渠道，固定最多 100 条 | 历史弹窗打开时加载 | 保留；范围已限制 |
| 分组监控设置 | `['channel-monitor','group-monitor','settings']` | `GET /api/channel_monitor/group_monitor/settings` | 单份设置数据 | 设置面板打开时加载；`gcTime: 0` | 保留；按需加载 |
| 模型检测设置 | `CHANNEL_MODEL_DETECTION_SETTINGS_QUERY_KEY` | `GET /api/channel_monitor/model_detection/settings` | 单份全局设置数据 | 设置面板打开时加载；`gcTime: 0` | 保留；按需加载 |
| 用户分组顺序（页面显示依赖） | `['user-groups']` | `GET /api/user/self/groups` | 返回分组描述、倍率和排序 | 常驻，`staleTime=5 分钟`；不是渠道监控业务快照 | 保留；不属于本次页面数据快照 |

统一规则：普通监控查询关闭自动重试、窗口聚焦刷新、重连刷新和常规轮询；活动探测/检测任务的 1 秒轮询是运行状态需求，继续保留。手动刷新只对当前视图及已打开明细调用 `resetQueries`，先移除旧结果再发起新请求；隐藏查询通过 `enabled` 或活动过滤排除。查询组件仍使用 React Query 的同 key in-flight 合并和项目现有 HTTP GET 去重，不新增跨页面缓存或调度层。

### 任务 2：前端去快照与刷新请求收口

**允许修改**

- `web/src/features/channel-monitor/lib/query-options.ts`
- `web/src/features/channel-monitor/index.tsx`
- 对应 `__tests__` 中的渠道监控查询/刷新测试。

**处理规则**

- 对页面查询关闭失活数据保留；刷新时先清除当前活动查询的旧结果，再发起本次读取。
- 保留项目现有 React Query/HTTP GET 去重；刷新按钮在请求中不可重复提交，但不新增队列、时间窗或全局调度器。
- 活动任务的必要轮询可保留，普通页面查询不增加轮询；不要把这两类请求混为一谈。
- 验证现有页面内 in-flight 防重入保护：若按钮禁用和现有请求去重已覆盖连续点击，则删去多余状态；若同步双击会重置/取消进行中的查询，则只保留最小的本地防重入保护，并以回归测试说明原因。
- 只刷新当前视图和已打开的明细；不因切页读取隐藏视图数据。
- 已由概览返回的并发数据直接复用；确认没有调用方后再考虑清理冗余 helper，不能为了清理而扩大改动。

**独立验收**

- 定向 Vitest：刷新尚未完成时，旧结果不可见；新结果完成后正常显示。
- 连续点击刷新只保留有界的活动请求，不绕过 GET 去重。
- `cd web; bunx vitest run src/features/channel-monitor/lib/__tests__/query-options.test.ts`
- `cd web; bun run typecheck`，再对受影响文件运行 targeted lint。

### 任务 3：后端页面快照确认（只读优先）

**检查范围**

- 仅沿任务 1 找到的页面接口进入 controller → service → model。
- 判断接口是否直接读取当前数据，还是复用了持久化/Redis/内存的页面专用快照。

**允许修改**

- 只有确认存在页面专用快照复用时，删除该读取路径；保留业务结算、任务运行和其他非页面用途的缓存。
- 不新增迁移、缓存状态或跨请求同步机制。

**独立验收**

- 相关 controller/service/model 定向测试通过；同一接口连续请求能读到更新后的数据。
- 若没有页面专用快照，提交“无需修改”的证据，不改后端。

### 任务 4：按接口组审计查询效率（可并行，只改有证据的项）

任务 4 分成互不依赖的三个检查；各检查先记录请求/SQL 证据，再决定是否改动。

#### 4A. 概览、渠道与并发

- 检查渠道列表、并发配置/实时并发是否重复读取，是否能复用同一概览结果。
- 检查渠道筛选是否下推，是否一次读取了页面不需要的范围。
- 只做减少重复读取或缩小已有查询范围的最小改动。

#### 4B. 性能、成功率与成本

- 检查时间范围、渠道/分组/模型条件是否传到接口和数据库层。
- 检查汇总页是否误加载日期/API key/渠道明细等隐藏数据，明细是否按页读取。
- 只补缺失的条件、分页或明显的重复请求，不重写统计模型。

#### 4C. 状态探测、模型检测与智能调度

- 检查视图未激活时是否仍请求数据，历史/详情是否按需加载，列表是否缺少分页或过滤。
- 只取消无用请求或补充已有接口支持的分页/过滤；不改变探测、调度和排序业务规则。

**任务 4 的统一验收**

- 用固定筛选、时间范围和分页参数，验证请求参数、SQL 条件和返回结果；记录修改前后的请求数或查询范围。
- 有修改时增加对应定向测试；无修改时保留“现有条件已下推/没有重复请求”的证据。
- 任务 4 不包含数据库 schema/index 变更。若发现必须改数据库结构，另立任务并按项目要求执行真实 SQLite、MySQL、PostgreSQL 矩阵，不能在本轮顺手处理。

### 任务 5：回归与范围清理

**验证**

- 前端受影响测试、`bun run typecheck`、受影响文件 lint。
- 有后端改动时只运行对应 package/controller/service/model 的定向 Go 测试。
- `git diff --stat`、`git diff --check`，并确认没有遗留测试进程、Docker、后台服务或子代理。

**范围审查**

- 对照本文件逐项确认每个改动都服务于“去页面快照”或“有证据的查询优化”。
- 删除无直接收益的抽象、队列、超时、缓存围栏和无关格式化。
- 未提交改动没有直接收益的，回滚；有直接收益但尚未验证的，补到对应任务后再保留。

## 4. 执行顺序与并行边界

1. 先完成任务 1，确认查询清单和基线。
2. 任务 2 由一个前端改动单元完成；任务 3、4A、4B、4C 可在只读审计阶段并行。
3. 后端修改只能在各自接口组的文件范围内进行；共享入口文件如需修改，由主任务串行合并，禁止多个代理同时写同一文件。
4. 所有改动完成后执行任务 5；没有通过独立验收的任务不得标记完成。

## 5. 最终验收标准

- 离开或刷新渠道监控页面时，不再把失活查询的旧快照当作最新数据展示。
- 手动刷新请求有界、可去重，且不读取隐藏视图的无关数据。
- 概览、性能、成本、状态、模型检测和智能调度的查询条件/分页已核对；只有有证据的问题才修改。
- 没有新增缓存层、后台调度器、数据库结构变更或与本需求无关的重构。
- 每项改动都能由对应小任务独立复现和验证。

## 6. 本轮执行结果

| 任务 | 结果 | 证据 |
| --- | --- | --- |
| 任务 1：查询基线 | 完成 | 已核对渠道、分组、性能、成功率、成本、状态探测、模型检测、智能调度及历史/详情查询；分页、筛选和启用条件已记录在审计结论中。 |
| 任务 2：前端去快照与刷新收口 | 完成 | `gcTime: 0`；关闭普通自动重试、窗口/重连刷新；手动刷新先 `resetQueries` 清除活动旧结果；保留进行中任务轮询和请求去重；概览复用并发字段；模型检测不再加载无关性能查询。 |
| 任务 3：后端页面快照 | 无需修改 | 沿页面接口链路未发现持久化、Redis 或内存的页面专用响应快照；保留实时 projection、任务运行态和业务缓存。 |
| 任务 4A：概览/渠道/并发 | 无需修改 | 概览已从同一批渠道和监控配置生成并发数据；前端已移除独立并发 GET；全量渠道是页面计数、分组和模型视图的既有契约。 |
| 任务 4B：性能/成功率/成本 | 无需修改 | 时间窗口、渠道/分组/模型条件及明细分页已核对，未发现明确重复请求、过大范围或未下推条件。 |
| 任务 4C：状态/模型检测/智能调度 | 无需修改 | 未激活视图通过 `enabled`/活动查询过滤排除；历史和详情按需加载，现有分页与活动任务轮询保留。 |
| 任务 5：回归与范围清理 | 完成 | 定向 Vitest 17/17、`bun run typecheck`、受影响文件 `oxlint`、`git diff --check` 均通过；未新增后端或数据库结构改动。 |

### 修改文件

- `web/src/features/channel-monitor/lib/query-options.ts`
- `web/src/features/channel-monitor/index.tsx`
- `web/src/features/channel-monitor/lib/__tests__/query-options.test.ts`
- `docs/downstream/channel-monitor/dashboard.md`
- `CHANNEL_MONITOR_PLAN.md`

### 数据库验证边界

本轮没有 schema、迁移或数据库查询语义改动，因此不启动真实数据库矩阵。只读审计未发现方言风险；MySQL/PostgreSQL 实例当前不可用，不能将未执行的真实三库矩阵写成已通过。
