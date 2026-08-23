# 渠道逻辑归组：集成验收、发布与回滚清单

任务编号：P13。本文只定义 P0-P12 的集成验证、最终验收、发布阻断项和恢复操作，不新增业务实现。代码和接口契约以 [渠道逻辑归组方案](../../../CHANNEL_LOGICAL_GROUP_MONITORING_PLAN.md) 及对应测试为准。

## 1. 验收范围

逻辑组只共享智能调度、状态探测和模型检测。余额、倍率、成本、普通监控、日志、并发限制和渠道视图始终按物理 `channel_id` 独立处理。一个请求最终只使用一个物理渠道/API Key；选中成员后必须调用现有物理渠道并发租约，不得新增逻辑组并发计数、租约或 Redis key。

本次发布的最低通过条件：

- 未归组渠道的旧选路、旧 API、普通监控和成本行为没有回归。
- 同一规范化请求地址才允许归组；Key、倍率、模型映射差异不会被系统自动合并。
- 同一逻辑组在调度、探测和检测中只占一个共享目标；执行明细仍可定位实际成员。
- 成员 weight 只用于组内选 Key；物理 `Channel.Weight` 和智能调度 `Ability.Weight` 语义不变。
- 关系变更使用 revision；新任务读新快照，活动任务使用已冻结的旧快照，旧事件不会被回写到新关系。
- SQLite、MySQL 5.7.8+、PostgreSQL 9.6+ 必须覆盖逻辑组、逻辑调度、逻辑状态探测和模型检测冻结字段的完整迁移面；较早基础 schema 的阶段性结果不能替代最终验证。
- P12 使用一个全局 `CHANNEL_LOGICAL_GROUP_ENABLED` 开关和每个逻辑组的 `status`；任一层关闭都会同时让智能调度、状态探测和模型检测恢复物理渠道旧路径，单组停用不影响其他组。

产品决策状态：本次范围、共享边界、地址判断、member weight、并发复用、页面形态和开关语义均已确定，没有待产品确认项。未通过的项目一律记为实现、测试或发布阻断项。

## 2. P0-P12 追踪

主会话在发布前逐项填写“证据”列。没有自动化测试、手工记录或代码审查证据的项目不得标记完成。

| 任务 | 交付边界 | 验收证据 | 当前状态 |
| --- | --- | --- | --- |
| P0 | 现状基线与物理渠道边界 | `docs/downstream/channel-logical-group-baseline.md`；全仓回归 | 已完成并通过最终自动化 |
| P1 | 逻辑组/成员模型、revision、member weight | `model/channel_logical_group_test.go`、完整 AutoMigrate 测试 | 已完成并通过最终自动化 |
| P2 | 请求地址规范化与预检 | `service/channel_logical_group_address_test.go`；不一致地址拒绝 | 已完成并通过最终自动化 |
| P3 | 管理 API、权限和 revision 冲突 | `controller/channel_logical_group.go`、`router/channel_logical_group_router_test.go` | 已完成并通过最终自动化 |
| P4 | 渠道管理页“同渠道配置”入口 | 两个前端测试文件 8/8、typecheck、lint、format、build | 已完成并通过最终自动化 |
| P5 | 运行时身份解析、缓存、revision 快照和渠道生命周期 | runtime/lifecycle/delete/patch 回归 | 已完成并通过最终自动化 |
| P6 | 智能调度逻辑组候选、样本和路由状态 | 逻辑 state、缓存/数据库选择、affinity、探针和物理基线回退测试 | 已完成并通过最终自动化 |
| P7 | 成员 weight 选择器 | 正权、全零、可用性、冷却和 busy 改投测试 | 已完成并通过最终自动化 |
| P8 | 状态探测共享配置、状态和执行 | 逻辑配置/状态、冻结 revision、busy 改投、物理回退测试 | 已完成并通过最终自动化 |
| P9 | 模型检测共享执行与重试 | 独立逻辑配置、冻结快照、模型并集、成本投影和生命周期测试 | 已完成并通过最终自动化 |
| P10 | 单渠道监控/成本/余额/倍率/并发回归 | `model/channel_logical_group_single_channel_regression_test.go` | 已完成并通过最终自动化 |
| P11 | 三数据库兼容与迁移验证 | SQLite、MySQL 5.7、PostgreSQL 9.6 完整旧表升级测试 | 已完成并通过最终自动化 |
| P12 | 全局开关、单组 status 和旧路径恢复 | 三条共享链路的启停与物理基线回退测试 | 已完成并通过最终自动化 |
| P13 | 本文清单、全量验证、发布/回滚签字 | 全仓、前端、真实三库和变更审计 | 自动化验收完成；生产手工发布项待执行 |

## 3. 自动化验证

### 3.1 环境准备

`go.mod` 要求 Go `>= 1.25.1`。优先使用仓库约定的 Go SDK；没有本机 Go 时可使用 Docker。前端使用 Bun。测试数据库只使用临时库或随机表前缀，不要把生产 DSN 写入文档或命令历史。

```powershell
# 本机（按实际 SDK 路径调整）
$goExe = 'D:\Go\sdk\go1.26.5\bin\go.exe'
& $goExe version

# 或 Docker（需要 Docker Desktop）
docker run --rm -v "${PWD}:/src" -w /src golang:1.25 go version

Set-Location web
bun --version
bun install --frozen-lockfile
Set-Location ..
```

### 3.2 格式、静态检查和单元测试

```powershell
git diff --check
git diff --stat

# 对本次修改的 Go 文件格式化；提交前再检查工作树是否出现无关格式化
& $goExe fmt ./model ./service ./controller ./router
& $goExe test ./model ./service ./controller ./router -count=1

# 逻辑组重点回归
& $goExe test ./model -run 'Logical|SmartScheduleRoute' -count=1
& $goExe test ./service -run 'Logical|ModelDetection' -count=1
& $goExe test ./controller -run 'ChannelStatusProbeGroup|ModelDetection' -count=1
& $goExe test ./router -run 'LogicalChannelGroup' -count=1

# 全量后端（发布阻断项）
& $goExe test ./... -count=1
```

Docker 等价命令（本机无 Go 时）：

```powershell
docker run --rm -v "${PWD}:/src" -w /src golang:1.25 go fmt ./model ./service ./controller ./router
docker run --rm -v "${PWD}:/src" -w /src golang:1.25 go test ./model ./service ./controller ./router -count=1
docker run --rm -v "${PWD}:/src" -w /src golang:1.25 go test ./... -count=1
```

当前结果：最终工作树已使用 Docker Go 1.25 通过 `go test ./model ./service ./controller ./router -count=1` 和 `go test ./... -count=1`。

### 3.3 数据库兼容测试

```powershell
& $goExe test ./model -run 'TestChannelLogicalGroup(SchemaMigrationSQLite|RevisionCASSQLite|SchemaDialectDDLIsPortable|SchemaMigrationConfiguredDatabases)$' -count=1 -v

# 可选真实数据库；要求 MySQL >= 5.7.8、PostgreSQL >= 9.6
$env:TEST_MYSQL_DSN = '由 CI secret 注入，不要提交或回显'
$env:TEST_POSTGRES_DSN = '由 CI secret 注入，不要提交或回显'
& $goExe test ./model -run TestChannelLogicalGroupSchemaMigrationConfiguredDatabases -count=1 -v
Remove-Item Env:TEST_MYSQL_DSN, Env:TEST_POSTGRES_DSN -ErrorAction SilentlyContinue
```

必须确认 `AutoMigrate` 可重复执行、`channel_id` 唯一索引阻止重复归组、revision CAS 的旧版本更新影响 0 行、分页在三种数据库一致，失败事务不留下成员行。

当前结果：SQLite 实际升级、三方言 DDL、真实 MySQL 5.7 和 PostgreSQL 9.6 均通过。测试覆盖逻辑关系、逻辑调度两表、逻辑状态探测两表、模型检测逻辑配置/目标两表，以及状态探测和模型检测旧表增列、旧行保留、重复迁移、唯一约束和 revision CAS。

### 3.4 前端验证

```powershell
Set-Location web
bun run typecheck
bun run test -- src/features/channels/components/dialogs/__tests__/logical-groups-dialog.test.tsx src/features/channels/components/__tests__/channels-primary-buttons-layout.test.tsx
bunx oxlint src/features/channels/components/dialogs/logical-groups-dialog.tsx src/features/channels/components/channels-primary-buttons.tsx src/features/channels/logical-groups-api.ts
bun run build
Set-Location ..
```

如果项目测试脚本不接受文件参数，执行该脚本的全量测试并在报告中注明。现有渠道、监控、成本页面不得因新增入口而改变路由、查询参数或列结构。

当前结果：最终工作树已通过 typecheck、两个组件测试文件 8/8、受影响文件 oxlint、oxfmt 检查和生产 build。

## 4. 集成验收矩阵

测试数据至少准备三个物理渠道：A、B 使用同一规范化请求地址，C 使用不同地址；A/B 各自配置独立余额、倍率和并发上限。测试期间记录物理 `channel_id`、逻辑组 ID、revision、请求 ID 和执行 ID，Key 只保留遮罩值。

| 编号 | 场景与步骤 | 预期结果 | 证据/阻断条件 |
| --- | --- | --- | --- |
| A1 未归组兼容 | 不创建任何逻辑组，执行普通请求、显式渠道请求、重试、监控查询、成本查询、余额/倍率读取和并发设置 | 结果、响应结构和物理归属与升级前一致；未归组 logical ID 回退自身 channel ID | 任一旧 API 字段、分页、筛选或成本归属变化即阻断 |
| A2 地址预检 | 预检 A/B 的大小写、默认端口和末尾 `/` 等等价 URL；预检 A/C 不同 URL；提交包含凭据的 URL | 等价地址通过；不同地址拒绝；返回摘要不含 Key/凭据 | 规范化摘要出现密钥或不同地址可保存即阻断 |
| A3 创建与权限 | ChannelRead 读取列表/详情/预检；ChannelWrite 创建和改成员；ChannelSensitiveWrite 删除；无权限用户分别调用读写删接口 | 读、写、删除分别符合既定权限；错误码稳定；删除不能由仅有 ChannelWrite 的管理员执行 | 越权、完整 Key 泄露或旧路由受影响即阻断 |
| A4 revision 冲突 | 两个管理会话读取同一 revision，先后提交成员变更 | 第一笔成功并递增 revision；第二笔返回 409，不覆盖新成员 | 出现丢更新或旧缓存写回即阻断 |
| A5 成员 weight | A/B weight=3/1，均可用；随后将一方 weight=0；再将全部设为 0 | 正权成员优先，长期比例约 3:1；正权存在时 0 权不选；全 0 时平均 | 统计窗口应固定随机种子/最少样本；不接受把物理 Channel.Weight 当组内配额 |
| A6 调度共享 | 在同一 `(分组, 模型)` 产生 A/B 样本并刷新调度 | 调度池只出现一个逻辑组候选、一份评分和主备决策；成员样本/可用性仍可追溯 | 同组重复占候选、生成逻辑组成本或覆盖普通监控即阻断 |
| A7 Key 并发 | A/B 并发上限均为 1，同时发起两个自动请求，再发起第三个；释放一个租约后重试 | 两个请求分别占用物理渠道；第三个沿用现有 429/重选；释放后可进入空闲 Key | Redis/本地不得出现逻辑组计数、租约或新并发 key；任一 Key 超上限即阻断 |
| A8 成员故障隔离 | 让 A 返回 401/余额不足/成员级 429，B 保持健康；再让 A 恢复 | A 被排除或短冷却，B 仍可服务；只有账号级 429 才触发组保护 | A 故障污染 B 或全部失败前组退出候选即阻断 |
| A9 状态探测去重 | A/B 同组配置相同探测模型，运行一轮并查看执行和成本 | 每逻辑组目标每轮仅一份逻辑执行；实际成员、请求 ID、成本写物理 channel_id | 同目标生成两次请求或成本归组汇总即阻断 |
| A10 模型检测去重 | A/B 同组启动同一检测目标；触发可重试传输错误 | 一份逻辑轮次/目标；重试可改投另一个成员；最终报告与执行可定位物理成员 | owner 无目标时不能导致整组跳过；旧 revision 不能写新关系 |
| A11 关系变更快照 | 活动探测/检测期间把 B 移出组或删除组，随后启动新一轮 | 活动任务继续使用冻结的旧 revision；新任务使用新关系；历史不改写 | 活动任务读到半套成员、旧事件被迁移或结算丢失即阻断 |
| A12 普通数据隔离 | 归组前后对比性能、成功率、首字、TPS、失败分类、余额、倍率、成本、日志和并发 API | 均按物理渠道独立；不出现逻辑组汇总表/接口/页面父子行 | 任一普通页面需要理解 logical ID 或成本合并即阻断 |
| A13 页面兼容 | 打开渠道管理、渠道监控、模型检测、状态探测、成本和调度页面；打开“同渠道配置”入口 | 原页面布局、筛选、分页、操作不变；仅新增入口可创建/编辑/删除和设置 weight | 页面出现新逻辑视图、Key 明文或布局回归即阻断 |
| A14 缓存与多实例 | 多实例先后修改组关系，模拟一次缓存构建失败和 revision 过期写入 | 缓存失败保留上一份完整快照；新 revision 最终收敛；过期写入被拒绝 | 出现空快照、成员半更新或跨实例旧写覆盖即阻断 |
| A15 灰度关闭/恢复 | 保持全局 `CHANNEL_LOGICAL_GROUP_ENABLED=true`，将一个测试逻辑组 `status` 设为停用再恢复；另在隔离实例以全局开关为 `false` 验证总回退 | 单组停用会同时让调度、探测、检测回退物理渠道旧路径且不影响其他组；全局关闭使所有组同时回退；恢复后共享行为重新生效 | 关闭后仍写共享状态、三条链路行为不一致或单组停用影响其他组即发布阻断 |

## 5. 手工验收步骤

1. 使用 Root 管理员创建 A/B/C，确保 A/B 地址规范化后相同，C 不同；为 A/B 设置不同倍率、余额和并发上限。
2. 在渠道管理页打开“同渠道配置”，先执行地址预检，再创建组并设置 A=3、B=1。确认弹窗只显示遮罩 Key、地址摘要和 weight。
3. 刷新渠道列表、渠道监控和成本页面，确认仍是原物理渠道行、原分页和原筛选。
4. 开启智能调度，在同一分组/模型产生足够样本；查看路由执行记录，确认组只占一个候选，随后实际请求在 A/B 间按 member weight 选择。
5. 将 A/B 并发上限都设为 1，使用两个并发请求观察两个 Key 各占一个租约；第三个请求确认沿用现有 429/重选。
6. 临时禁用 A 或让 A 余额不足，重复请求、状态探测和模型检测；确认 B 仍可用，A 的失败和成本只写 A。
7. 运行一次状态探测和一次模型检测，确认每逻辑目标每轮只有一个执行；从执行详情核对实际成员、revision、请求 ID 和遮罩 Key。
8. 在活动任务中替换成员，确认活动任务使用旧快照，新任务使用新 revision；检查旧日志、成本和检测结果没有被迁移。
9. 以第二个管理会话提交旧 revision，确认返回 409；无权限账号尝试写入/删除，确认被拒绝。
10. 保持 `CHANNEL_LOGICAL_GROUP_ENABLED=true`，把一个测试组的 `status` 设为停用，确认调度、探测和检测三条链路同时恢复物理渠道路径且其他组不受影响；恢复组 status 后复测。另在隔离实例以全局开关为 `false` 验证所有组总回退。

## 6. 发布前阻断清单

- [x] 使用满足 `go.mod` 的 Go 版本完成最终 `go test ./model ./service ./controller ./router -count=1`。
- [x] `gofmt` 和 `go test ./...` 全仓结果已在最终工作树确认。
- [x] 最终工作树 `git diff --check` 已通过。
- [x] `git diff --stat`、无关生成物和敏感信息已在封版时复核。
- [x] 前端 `typecheck`、两个逻辑组组件测试、受影响文件 lint/format 和生产 build 已通过。
- [x] SQLite 完整 AutoMigrate、唯一约束、revision CAS 和三方言 DDL 已覆盖全部新增 schema。
- [x] 真实 MySQL 5.7、PostgreSQL 9.6 完整迁移已执行并通过。
- [x] P12 自动化已证明全局开关和单组 `status` 对三条共享链路统一回退。
- [ ] P12 隔离环境手工灰度、停用和恢复仍需执行。
- [x] 未归组和普通监控/成本/余额/倍率/并发回归通过；无逻辑组成本或普通监控聚合。
- [x] API 权限、revision 409、地址预检、重复成员、删除组和 Key 脱敏均通过。
- [ ] 生产数据库、Redis、检测器数据卷和配置已完成备份；已确认回滚窗口和负责人。
- [ ] 发布窗口内无活动模型检测会话；必要时先暂停定时检测并等待/取消活动轮次。

任一阻断项未满足时，只允许部署到隔离环境继续验证，不得进入生产灰度。

## 7. 发布顺序与观测

1. 发布前导出逻辑组列表、成员、weight、地址摘要、status 和 revision；保存为脱敏审计附件。
2. 备份主数据库、部署配置和模型检测器独立数据卷；确认可恢复。本功能没有新增逻辑组 Redis key，不执行逻辑组 Redis 备份或清理。
3. 执行数据库迁移并立即运行 SQLite/目标数据库健康检查；迁移失败停止发布，不手工改表。
4. 部署应用代码，先设置 `CHANNEL_LOGICAL_GROUP_ENABLED=false`；确认启动日志无迁移、缓存或模型检测错误。该环境变量需要重启进程后按新的部署值运行。
5. 设置 `CHANNEL_LOGICAL_GROUP_ENABLED=true` 后只把一个低风险测试组的 `status` 设为启用；该 status 同时启用该组的调度、状态探测和模型检测共享，每条链路至少观察一个完整周期。
6. 观察错误率、429、成员选择比例、并发租约、探测/检测执行去重、成本归属、revision 冲突和缓存刷新失败。
7. 灰度窗口无异常后按组扩大范围；保留原物理渠道 API 和普通监控页面的对照截图/响应。
8. 发布完成后归档测试命令输出、数据库版本、开关状态、灰度组 ID 和异常处置记录。

建议告警：逻辑组无可用成员、共享执行重复、过期 revision 写入、缓存快照为空、成员并发超过上限、逻辑组成本/普通监控聚合意外产生、模型检测 `submission_unknown`/`pending` 成本持续增长。

## 8. 回滚与恢复

回滚优先使用配置/API 关闭共享能力，保留物理渠道、日志、成本和历史数据。禁止直接删除表、清空 Redis 全库或回写旧关系覆盖审计历史。

### 8.1 单组快速回滚

1. 记录当前组 ID、revision、成员和活动任务。
2. 将该逻辑组 `status` 设为停用；该操作同时关闭调度、状态探测和模型检测共享，确认新请求回到物理渠道旧选路。
3. 暂停该组定时探测/检测，等待活动任务结束；无法等待时使用现有取消接口，保留执行和成本记录。
4. 检查该组不再产生逻辑路由写入、共享执行或逻辑组成本；物理监控和成本继续增长。
5. 只有在确认不再需要关系时，才用带 revision 的删除组 API 解除成员；删除失败（409）时先重新读取最新 revision，不强制覆盖。

### 8.2 全局回滚

1. 将部署环境变量 `CHANNEL_LOGICAL_GROUP_ENABLED=false` 并重启相关实例，停止新的逻辑调度、探测和检测共享；保留已有物理路由状态和逻辑组关系。
2. 停止或排空相关 Worker，先处理 `running`、`pending`、`prepared`、`submission_unknown` 等活动/不确定任务。
3. 从发布前备份恢复应用版本和配置；数据库迁移通常保持向前兼容，不执行破坏性降级迁移。
4. 如需恢复关系，使用发布前导出的成员/weight/revision 逐组 API 恢复，并让服务重新构建完整缓存；不得直接修改 `channels.logical_channel_id`。
5. 本功能没有新增逻辑组 Redis key，回滚时不要清理渠道监控或并发 Redis 数据。
6. 重启后确认所有组走物理 fallback，普通监控/成本/余额/倍率/并发接口可读，模型检测器和探测 Worker 无悬挂租约。

### 8.3 数据恢复核对

- 逻辑组删除/关闭不得删除 `ChannelMonitorEvent`、分钟聚合、`ChannelDailyCost`、请求日志、模型检测执行和状态探测历史。
- 恢复后按物理 `channel_id` 对账成本、倍率和余额；按逻辑 ID 对账共享调度/探测/检测；两者不混算。
- 对任何 `pending` 或 `unresolved` 成本事件先人工核对上游账单，再决定结算、退款或重跑；不得用回滚覆盖未知扣费。
- 恢复完成后重新执行第 4 节 A1、A7、A9、A10、A12、A15，形成回滚验收记录。

## 9. 证据归档模板

```text
版本/提交：
数据库类型与版本：
Go/Bun 版本：
执行时间（Asia/Shanghai）：
测试数据（仅 channel_id、逻辑组 ID、revision，不含 Key）：
自动化命令及结果：
手工矩阵通过项：
跳过项及原因：
发布开关状态：
备份位置与恢复演练结果：
阻断项：无 / （列出）
验收人：
```

完成 P13 的定义是：本文所有必选复选项有可追溯证据，P12 开关行为已经实测，主会话确认无发布阻断项，并将发布与回滚记录和变更文件清单一并归档。
