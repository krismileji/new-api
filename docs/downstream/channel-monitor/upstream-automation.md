# 上游自动任务

入口：**渠道监控 → 上游自动任务**。每个上游账户可以配置独立的凭据、指标查询、触发接口和检查间隔，无需先建立渠道；一个任务也可以关联多个渠道。

任务在主节点的系统任务调度器中独立运行，每 30 秒分发到期检查。渠道禁用、旧渠道监控的连续失败上限，以及迁移后的渠道监控总开关，都不会停止已经启用的独立任务。查询失败会退避后继续检查，不会因累计失败而永久停止。

## 使用与恢复

1. 新建任务，填写上游地址、查询余额或倍率的请求，以及触发条件和接口。可以使用任务自己的凭据，也可以引用“共享请求与变量”。
2. 使用“测试获取指标”检查查询和凭据配置。测试与保存不会执行触发接口；草稿提取的凭据仅回填表单，保存后生效。
3. 选择需要更新的关联渠道，启用任务并保存。“立即检查”跳过等待间隔，但仍遵守条件、执行时段、冷却时间和每日次数。
4. 每次成功检查后，任务会通过关联渠道各自保存的上游配置重新查询指标。查询不受渠道启停和原先连续失败停止状态影响。开启原有“余额恢复后自动启用”等策略，并满足余额、成本倍率等条件后，自动禁用的渠道才会恢复。手动禁用保持不变。

关联渠道不会把任务查询到的余额直接写入所有渠道，避免不同账户或余额口径混用。关联前需配置好各渠道的上游查询；删除渠道后可在任务编辑器取消对应关联。

余额触发规则使用任务查询到的上游真实余额（按配置提取并应用结果乘数），不使用关联渠道扣减本地预估消耗后的余额。关联渠道余额预估不完整时，仍保留禁止自动恢复渠道的保护。

关联渠道刷新问题作为附加提示显示在本轮结果和检查记录中，不覆盖“检查完成”或“接口执行成功”，也不累计独立任务的失败次数或延长下次检查间隔。任务自己的指标查询、触发接口或执行后指标复查失败，仍按原有失败规则处理。

| 触发模式 | 行为 |
| --- | --- |
| 持续满足 | 条件持续成立时，每次冷却结束后都可再次调用，仍受执行时段和每日次数限制。新规则默认使用此模式。 |
| 首次满足 | 成功触发后，必须先查询到指标退出条件范围，才能再次触发。跨日或清空今日次数不会重新激活。旧规则保留此模式。 |

一轮检查最多执行一个触发接口；成功后立即复查指标，其余规则等待下一轮。准备凭据等发送前失败可在冷却后重试，不占用调用次数。请求已发出而结果未知、HTTP 错误或成功判定失败时，规则进入待确认状态，后续仍查询指标，但不会自动重放该规则。核对上游结果后，可用“核对结果并解除触发限制”恢复检查；调用次数和冷却时间仍保留。

列表提供上次/下次检查时间、最新指标、每条规则的跳过原因，以及最近 30 次检查记录。共享凭据仍被任务引用时不能删除，也不能移除任务使用的变量；编辑共享配置会使任务的旧编辑版本失效。

## 旧规则迁移

调度器运行或打开任务列表时，会把渠道内已有规则迁移到独立任务。一个来源渠道迁移成一个任务，复制查询、凭据、检查频率和执行规则，保留次数、冷却时间、触发状态。原先暂停自动更新或暂停规则所需指标同步的配置，迁移后保持任务暂停。

迁移在同一数据库事务内创建任务、移除渠道规则、递增渠道配置修订号并记录迁移标记。重复运行不会重复创建；删除独立任务后，旧页面也不能把规则重新写回渠道。旧失败或执行中的记录进入待确认状态。

不同渠道是否属于同一上游账户无法可靠推断，因此不会自动合并。若多个迁移任务对应同一账户，应保留一份任务并集中关联渠道，停用重复任务。单个渠道迁移失败会在列表显示原因，已有独立任务仍正常调度。

数据复用 `system_tasks` 表中的 `upstream_automation_config` 行，不增加表、字段或索引。此类型不排队执行、不参与历史清理，并从通用任务列表中排除；通用详情响应也隐藏其配置和状态。调度记录 `upstream_automation` 使用渠道监控任务的保留天数。旧规则迁移标记保存在原有执行状态行中。

## 关联渠道刷新提示修复验证（2026-09-18）

Go：`go1.26.5 windows/amd64`。真实数据库：SQLite **3.50.4**、MySQL **5.7.44**、PostgreSQL **9.6.24**。修复前新增回归在 SQLite 上复现原错误；修复后全部上游自动任务测试通过，三种数据库均未跳过，根模块构建通过。

```powershell
$env:TEST_CUSTOM_ACTION_MYSQL_DSN='root:automation-refresh-test@tcp(127.0.0.1:58386)/new_api_custom_action_test?charset=utf8mb4&parseTime=True&loc=Local'
$env:TEST_CUSTOM_ACTION_POSTGRES_DSN='postgres://postgres:automation-refresh-test@127.0.0.1:59975/new_api_custom_action_test?sslmode=disable'
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./controller -run '^TestUpstreamAutomation' -count=1 -timeout=240s -v
& 'D:/Go/sdk/go1.26.5/bin/go.exe' build ./...
```

MySQL/PostgreSQL 使用本次创建的隔离测试容器，端口由 Docker 分配，验证后移除容器及其测试卷。上述地址和密码仅用于这次临时测试。

新增回归覆盖重置成功但预估不完整、真实余额充足不触发重置、关联渠道查询失败；检查任务从查询故障恢复后清零失败次数、按原间隔继续执行、结果与历史保留、不会重复重置，以及余额预估不完整时禁止恢复渠道。原有查询故障退避、冷却/次数限制、未知执行结果确认、多渠道恢复及迁移回归也通过。

本次仅修改下游自动任务逻辑、提示和文档，并新增回归测试，未修改 `upstream/main` 已有文件、表结构、数据库依赖、日志库访问或 `relaykit`。本地验证输出保存在 `.local-tests/automation-refresh-20260918/`。

## 验证记录（2026-09-17）

Go：`go1.26.5 windows/amd64`。真实数据库：SQLite **3.50.4**、MySQL **5.7.44**、PostgreSQL **9.6.24**。

使用专用、可丢弃的 MySQL/PostgreSQL 数据库 `new_api_custom_action_test`，分别监听本机端口 13393 和 15493，验证后删除测试容器。创建命令：

```powershell
docker run --detach --name new-api-automation-mysql-test -e MYSQL_ROOT_PASSWORD=automation-test -e MYSQL_DATABASE=new_api_custom_action_test -p 127.0.0.1:13393:3306 mysql:5.7.44 --character-set-server=utf8mb4 --collation-server=utf8mb4_unicode_ci
docker run --detach --name new-api-automation-postgres-test -e POSTGRES_PASSWORD=automation-test -e POSTGRES_DB=new_api_custom_action_test -p 127.0.0.1:15493:5432 postgres:9.6.24
```

以下环境变量与命令从仓库根目录运行：

```powershell
$env:TEST_CUSTOM_ACTION_MYSQL_DSN='root:automation-test@tcp(127.0.0.1:13393)/new_api_custom_action_test?charset=utf8mb4&parseTime=True&loc=Local'
$env:TEST_CUSTOM_ACTION_POSTGRES_DSN='postgres://postgres:automation-test@127.0.0.1:15493/new_api_custom_action_test?sslmode=disable'
$env:TEST_CUSTOM_VARIABLE_MYSQL_DSN=$env:TEST_CUSTOM_ACTION_MYSQL_DSN
$env:TEST_CUSTOM_VARIABLE_POSTGRES_DSN=$env:TEST_CUSTOM_ACTION_POSTGRES_DSN
go test -p 1 ./service ./controller ./model -run 'Test(UpstreamAutomation|ChannelMonitorCustomAction|ChannelMonitorCustomVariable|ChannelMonitorVariableGroup|ChannelMonitor.*Retention|.*ChannelMonitorCleanup|.*SystemTask)' -count=1 -timeout=240s
go test ./controller -run '^TestUpstreamAutomation' -count=1 -timeout=180s
go build ./...
```

数据库矩阵覆盖：无渠道独立运行、重复触发的冷却/每日限制、首次满足跨日不重放、查询故障恢复、多渠道只触发一次、禁用渠道余额刷新与策略恢复、手动禁用和缺失成本样本不恢复、并发检查去重、未知结果确认、发送前凭据失败重试、本地及共享凭据持久化、共享引用保护。旧配置迁移和现有表 `AutoMigrate` 各重复两次，检查凭据、调用状态、唯一任务和旧规则移除。此次没有表结构或日志数据库变更。

API 用例覆盖敏感值隐藏、草稿测试不执行动作/不修改已保存状态、旧修订号拒绝保存、暂停任务拒绝手动检查，以及部分迁移失败不阻塞独立调度。

从 `web/` 运行：

```powershell
bun run test src/features/channel-monitor --maxWorkers=2
bun run typecheck
bunx --no-install oxlint -c .oxlintrc.json src/features/channel-monitor
bun run build
```

上述后端测试、构建、类型检查和 lint 均通过；三数据库自动任务矩阵共 18 个场景通过，前端 106 个文件、618 个测试通过。首次默认 4 个 worker 与构建并行运行时有一个既有交互用例超过 5 秒超时；降低至 2 个 worker 后完整重跑通过，未修改用例超时设置。

上游文件修改仅涉及 `model/system_task.go`：通用列表排除配置行，并使通用详情响应隐藏敏感配置。这两处是防止其他系统任务入口泄漏凭据的必要改动，其余实现使用下游文件和现有任务注册入口。
