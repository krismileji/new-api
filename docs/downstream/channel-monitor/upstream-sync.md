# 上游同步与策略
## 上游类型

| 类型 | 认证方式 | 倍率/余额 | 分组列表与应用 |
| --- | --- | --- | --- |
| New API | 公开接口 | 支持公开分组数据 | 可读取分组；公开模式不执行用户令牌分组切换 |
| New API | 用户 ID + 管理面板访问令牌 | 支持 | 支持读取分组并把当前渠道的上游令牌切换到所选分组 |
| Sub2API | 当前渠道 API Key | 支持新版倍率和余额 | 不支持自动读取或应用分组，需手工填写分组名或 ID |
| Sub2API | 登录邮箱 + 密码 | 自动登录并缓存访问 Token | 支持读取和应用分组 |
| Sub2API | 手动 Token（可选 Refresh Token） | 使用手动 Token；可自动换取并缓存短期访问 Token | 支持读取和应用分组 |
| 自定义上游 | 固定值或自定义 HTTP 请求 | 支持分别配置倍率和余额 | 不自动管理远端分组 |

Sub2API 还可以在不提供凭据的情况下读取公开版本信息，便于确认上游部署版本和接口兼容性。

Sub2API 配置要求手动 Token 必填，Refresh Token 可选。首次配置时可从已登录的 Sub2API 面板读取 `localStorage` 中的 `auth_token` 和 `refresh_token`；监控请求默认使用手动 Token，填写 Refresh Token 后会在访问 Token 被拒绝时通过 `/api/v1/auth/refresh` 换取新 Token 并重试。上游返回新的 Refresh Token 时，服务端会按渠道配置修订号和旧凭据做条件更新，避免并发中的旧请求覆盖新配置。历史上单独保存的 Refresh Token 认证配置仍可继续读取。

编辑 Sub2API 配置时可同时填写手动 Token 和 Refresh Token，并分别执行真实上游测试。手动 Token 测试只验证手动 Token；Refresh Token 测试只验证 Refresh Token。保存时 Token 是必填主凭据，Refresh Token 作为可选续期凭据一并持久化。

## 自定义上游

倍率和余额可以分别选择固定值或 HTTP 数据源。HTTP 数据源支持：

- `GET`、`POST` 和相对于上游基础地址的接口路径。
- 查询参数、自定义请求头、JSON 请求体或表单请求体。
- JSON 路径取值、多个 JSON 路径的四则运算或纯文本响应。
- 对提取结果应用正数乘数。
- 将查询参数、请求头或请求体标记为敏感值；读取配置时只返回“已配置”状态。
- 余额复用倍率接口响应，避免重复请求同一个上游端点。

自定义请求仍经过受保护的 HTTP 客户端。配置了渠道代理时复用渠道代理传输，同时对目标 URL 和重定向执行 SSRF 校验。

### JSON 取值与计算

倍率和余额的「JSON 取值路径 / 表达式」都支持以下写法：

| 用途 | 填写内容 |
| --- | --- |
| 直接取值（兼容原配置） | `data.balance` |
| 总额减去已用额度 | `=json("data.total") - json("data.used")` |
| 两项余额相加 | `=json("data.balance") + json("data.bonus")` |
| 两个字段相乘 | `=json("data.price") * json("data.count")` |
| 两个字段相除 | `=json("data.used") / json("data.total")` |
| 多字段与常量组合 | `=(json("data.total") - json("data.used")) / 100` |

表达式必须以 `=` 开头，`json("路径")` 从本次接口的同一份 JSON 响应取值，路径沿用现有 GJSON 语法，例如数组项 `data.items.0.balance`。支持数字、数字字符串、正负号、`+`、`-`、`*`、`/` 和括号，先乘除后加减。完整路径或表达式最多 512 个字符。没有 `=` 前缀时仍按普通路径处理，包括路径中的连字符和查询条件。

如果 JSON 字段名本身以 `=` 开头，可转义为 `\=credit`，或填写 `=json("=credit")`。

最终值 = 表达式计算结果 × 结果乘数。例如响应为 `{"data":{"total":120,"used":35}}`，填写减法表达式并设置结果乘数为 `0.01`，得到余额 `0.85`。余额复用倍率接口时，两项各自计算，并分别应用自己的结果乘数。

字段缺失、`null`、非数字、除数为零或计算溢出都会作为本次同步失败处理，不会用零替代。计算后的倍率仍须在 `0` 到 `1000000` 之间，余额绝对值不得超过 `1000000000000000`。纯文本响应继续按数字取值并乘以结果乘数。

## 同步开关

每个渠道可独立开启或关闭倍率同步和余额同步。关闭后，定时任务和对应手动刷新入口都会跳过该指标；已保存的历史数据不会被删除。

保存配置和“测试配置”是两个动作。测试可以使用尚未保存的表单内容，并在未重新填写时复用同一上游主机下已保存的敏感凭据；敏感凭据不会跨主机复用。

## 成本倍率换算

成本倍率按以下公式产生：

```text
成本倍率 = 上游倍率 × 换算系数
```

换算模式包括：

- 不换算：系数为 `1`。
- 充值：`实付人民币 / 到账美元额度`。
- 订阅：`订阅价格 / (每日美元额度 × 周期天数)`，天、周、月分别按 `1`、`7`、`30` 天。

策略比较、分组同步和成本统计都使用成本倍率；原始上游倍率仍单独保存并保留变更历史。

## 分组倍率策略

当某渠道的目标成本倍率乘以分组系数后高于当前本地分组倍率，可按渠道在该分组中的位置执行不同动作。

仅剩一个渠道时可选择：仅记录、更新分组倍率或禁用渠道。

存在多个渠道时可选择：仅记录、参与更新分组倍率、禁用渠道或移除当前分组关联。更新分组倍率时采用参与渠道中的最高目标倍率；移除关联时，如果该分组是渠道唯一分组，则保留关联，避免产生无分组渠道。

手动“按最高成本倍率更新”使用同一口径，并持久化本次使用的分组系数。

## 余额策略

每个渠道可配置余额预警值和余额自动禁用阈值：

- 余额预警值仅用于页面预警和邮件通知：余额低于预警值时发送一次通知；恢复到阈值以上后清除通知去重状态，下一次再次跌破时可重新通知。
- 有效余额低于自动禁用阈值时，把启用渠道改为系统自动禁用，并记录明确的禁用原因；该判断不依赖是否配置或触发余额预警。
- 保存固定余额的自定义上游时也立即执行自动禁用判断。

禁用比较使用严格小于，有效余额等于阈值时保持启用。开启余额同步并配置自动禁用阈值后，只要 Redis 估算可用且请求覆盖有效，就按“最近同步的上游余额 − 本轮已完成消费 − 进行中预估”判断，即使没有设置预警值或上游余额尚未低于预警值也生效。余额查询期间完成、可能已被上游扣除的待确认消费会从本次禁用扣算中排除，避免重复扣减；缺少有效估算时只能依据上游余额禁用，不能据此自动恢复。请求开始和结算都会触发异步检查，禁用不主动中断进行中的请求。这些估算不修改用户钱包或实际计费。

Sub2API 账号和 Token 认证的余额响应必须包含有效的 `balance` 数字。字段缺失、`balance: null` 或 `data: null` 都记录为获取失败并保留原余额；明确返回 `0` 时才按零余额执行策略。

自动恢复只处理由渠道监控自身原因禁用的渠道，不会覆盖人工禁用或其他系统禁用原因。

渠道健康检查启用“成功后重新启用”时，也会先校验当前渠道监控条件，校验使用最近一次有效监控记录，不在健康检查中重复请求上游：已开启余额同步并设置自动禁用阈值时，余额不得低于阈值；已开启倍率同步并配置禁用渠道策略时，成本倍率必须满足所属分组倍率要求。相关监控数据缺失、仍处于更新失败状态或条件不满足时，健康检查不会自动启用渠道；人工禁用始终不会被覆盖。

## 自动更新与通知

倍率更新任务可设置 `0` 到 `525600` 分钟的间隔，`0` 表示关闭。每个失败渠道默认最多重试 `3` 次，可配置 `0` 到 `10` 次；每次重试前默认等待 `0` 秒（可配置 `0` 到 `600` 秒），`0` 表示立即重试。Sub2API 认证失败不会做无意义的重复认证请求。倍率或余额分别连续失败默认达到 `10` 次后停止自动更新，停止次数可设为 `0` 到 `100`。`0` 表示不因连续失败停止自动更新，之前因达到次数而停止的同步会在下一轮自动更新中继续尝试；每轮仍遵循重试次数和等待时间。需要停止的渠道可在渠道设置中手动关闭倍率或余额同步。

可选的失败自动禁用会在倍率或余额更新最终失败后系统禁用渠道。后续成功更新且余额未低于自动禁用阈值时，可以恢复由该失败原因禁用的渠道。

New API / Sub2API 在倍率成功、余额失败时保留成功倍率，并只重试余额。余额重试耗尽后仍执行失败自动禁用，即使连续失败停止次数设为 `0`；认证失败不做无效重试。启用余额同步且设置禁用阈值时，缺少有效余额的本轮结果不能用于成本倍率恢复启用。余额触发的禁用也会纳入“渠道自动禁用”邮件，并包含渠道、备注和阈值原因。

“成本倍率恢复后自动启用”只恢复先前因成本倍率高于分组倍率而自动禁用、且当前所有关联分组都满足严格小于条件的渠道。余额自动禁用渠道的恢复需要单独开启“余额恢复后自动启用”，并要求余额恢复且成本倍率按分组系数换算后小于或等于全部所属分组倍率；两种恢复都不会覆盖其他禁用原因。

邮件通知汇总倍率变化、余额预警、自动禁用、分组关联移除、分组更新失败和渠道更新失败，并包含渠道名称、ID 和备注。任务即使部分渠道失败也会继续处理其余渠道，结果中保留失败明细和是否发生重试恢复。

启用邮件通知并选择同步失败类型后，倍率或余额分别连续失败达到“同步失败告警次数”时发送一次。告警次数可独立设置为 `1` 到 `100` 次，默认 `10` 次；停止次数为 `0` 时，达到告警次数后自动更新仍会继续。若更早达到大于 `0` 的停止次数，则在停止时提前通知，避免停止后无法达到告警次数。持续失败不会重复通知，成功恢复或上游配置变化后清除去重状态，再次达到告警次数时可重新通知。邮件发送失败不会确认通知状态，后续任务会继续尝试发送。其他通知类型仍按各自配置执行。

定时间隔、重试、连续失败阈值、邮件类型和自动启用开关的完整 Option 列表见[配置参考](../configuration-reference.md)。

## 告警配置验证（2026-09-14）

使用 Go 1.26.5，在 SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 9.6.24 上执行配置保存与读取测试，三种数据库均通过。测试覆盖新配置缺失时的默认值、非法值拒绝、边界值保存、重新读取，以及更新其他配置后保留告警次数。本次复用现有 Option 存储，无生产表结构或迁移变更。

以下为本次隔离容器的测试连接与实际执行命令，密码仅用于临时测试数据库；复现时使用空的 `new_api_monitor_alert_test` 数据库并替换端口：

```powershell
$env:MONITOR_ALERT_MYSQL_DSN = 'root:alert-test-only@tcp(127.0.0.1:57656)/new_api_monitor_alert_test?parseTime=true&charset=utf8mb4'
$env:MONITOR_ALERT_POSTGRES_DSN = 'postgres://postgres:alert-test-only@127.0.0.1:54582/new_api_monitor_alert_test?sslmode=disable'
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./controller -run '^TestChannelMonitorSyncFailureAlertSettingsDatabaseMatrix$' -count=1 -v -timeout=120s
```

倍率和余额通知回归覆盖配置为 `3` 次、停止次数为 `0` 或 `20` 时的通知发送、持续失败去重、邮件失败重试及恢复后的再次通知。前端四个相关测试文件共 `44` 个用例通过，类型检查、涉及文件 lint、前端构建与根模块构建通过。格式检查仅剩设置弹窗原有的两处 `TabsTrigger` 换行差异。

所有改动均位于下游文件，未修改 `upstream/main` 已有文件。

## 余额保护修复验证（2026-09-15）

使用 Go 1.26.5，`TestChannelMonitorBalanceSafetyDatabaseMatrix` 在真实 SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 9.6.24 上全部通过。覆盖上游与本地两种记账顺序、差额重新读取和失败后保留、实际后续消费触发渠道及 Ability 禁用、缺失/空/零余额、余额失败阻止倍率恢复及恢复后的启用、部分失败重试和开关、人工禁用保护、认证失败停止重试、重试期间配置变更、禁用邮件及去重。

本次沿用现有有符号浮点列 `balance_pending_consumption` 保存对账差额，没有表结构、迁移、数据库依赖或独立日志库路径变更。仅修改下游文件，未修改 `upstream/main` 已有文件。

隔离测试容器的创建命令如下。测试密码仅用于临时数据库，端口由 Docker 分配，通过 `docker port` 读取：

```powershell
docker run -d --name codex-balance-mysql-20260915 -e MYSQL_ROOT_PASSWORD=balance-test-only -e MYSQL_DATABASE=new_api_monitor_balance_test -p 127.0.0.1::3306 mysql:5.7.44
docker run -d --name codex-balance-postgres-20260915 -e POSTGRES_PASSWORD=balance-test-only -e POSTGRES_DB=new_api_monitor_balance_test -p 127.0.0.1::5432 postgres:9.6
docker exec -e MYSQL_PWD=balance-test-only codex-balance-mysql-20260915 mysql -uroot -e 'ALTER DATABASE new_api_monitor_balance_test CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci'

$env:MONITOR_BALANCE_MYSQL_DSN = 'root:balance-test-only@tcp(127.0.0.1:63564)/new_api_monitor_balance_test?parseTime=true&charset=utf8mb4'
$env:MONITOR_BALANCE_POSTGRES_DSN = 'postgres://postgres:balance-test-only@127.0.0.1:63569/new_api_monitor_balance_test?sslmode=disable'
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./controller -run '^TestChannelMonitorBalanceSafetyDatabaseMatrix$' -count=1 -v -timeout=180s
```

其他验证命令：

```powershell
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./controller ./service -run 'Test(AutoDisableChannelMonitorForLowBalance|RecordChannelMonitorBalanceUpdate|FetchChannelMonitorUpstream|ManualSharedUpstreamRequest|SaveChannelMonitorCustomFixedBalance|ChannelMonitorAllowsHealthCheckAutoEnable|RunChannelRatioMonitorTask|CostRatioRecovery|FetchSub2API|ChannelMonitorCustom.*|FetchNewAPI)' -count=1 -timeout=180s
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./controller ./model ./service -count=1 -timeout=300s
& 'D:/Go/sdk/go1.26.5/bin/go.exe' build ./...
```

相关回归、model 全包测试和根模块构建通过。controller / service 全包测试有以下 8 个既有失败；使用 Go overlay 恢复本次修改文件为基线 `9c0574450`、排除新增测试后，全包对照复现了相同失败，本次不修改这些无关路径：

- `TestGetChannelMonitorRecoveryBeforeBackgroundCheck`
- `TestProtectChannelSmartScheduleRuntimeFailureIgnoresMinimumSamples`
- `TestProtectChannelSmartScheduleRuntimeFailureDoesNotRecountPersistedErrors`
- `TestRunChannelSmartSchedulePersistsExecutionTimeScoreDetails`
- `TestPlanChannelSmartScheduleUsesHysteresisAndForceReset`
- `TestRunChannelSmartScheduleManualPrimaryAllowsStabilityDegrade`
- `TestUpdateChannelMonitorSettingsValidatesAndPersists`
- `TestUpdateGroupedChannelAddressValidatesMembersAndAdvancesRevision`

## 禁用阈值独立判断验证（2026-09-18）

修正预警值与余额保护的耦合：普通渠道、共享余额来源的逐渠道策略、健康检查恢复均按自动禁用阈值独立判断。新增回归覆盖未设置预警值、上游余额高于或等于预警值、已完成与进行中消费共同跨过禁用阈值、阈值相等时保持启用、实际消费下降后的恢复，以及估算不可用时阻止恢复。原有恢复测试显式初始化 Redis 和请求覆盖状态，继续验证自动恢复、人工禁用保护和重置接口不重复执行。

使用 Go 1.26.5，余额安全与残留请求恢复矩阵、共享余额来源矩阵、上游自动任务矩阵在真实 SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 9.6.24 上全部通过。Redis 8.8.0 的现有原子操作回归、定时同步、预警邮件去重、恢复回归及根模块构建通过。没有表结构、迁移、数据库依赖、独立日志库路径或 `relaykit/` 改动；所有修改文件均为下游文件。

以下为隔离测试容器的连接和实际验证命令。两个数据库均为空的专用测试库；密码仅用于本次临时容器，复现时替换映射端口：

```powershell
$env:MONITOR_BALANCE_MYSQL_DSN = 'root:balance-threshold-test-only@tcp(127.0.0.1:59931)/new_api_monitor_balance_test?parseTime=true&charset=utf8mb4'
$env:MONITOR_BALANCE_POSTGRES_DSN = 'postgres://postgres:balance-threshold-test-only@127.0.0.1:59934/new_api_monitor_balance_test?sslmode=disable'
$env:TEST_CUSTOM_ACTION_MYSQL_DSN = 'root:balance-threshold-test-only@tcp(127.0.0.1:59931)/new_api_custom_action_test?parseTime=true&charset=utf8mb4'
$env:TEST_CUSTOM_ACTION_POSTGRES_DSN = 'postgres://postgres:balance-threshold-test-only@127.0.0.1:59934/new_api_custom_action_test?sslmode=disable'
$env:TEST_CHANNEL_BALANCE_REDIS_ADDR = '127.0.0.1:59938'

go test ./service ./controller -run 'Test(ChannelBalance|UpstreamAccount.*Balance|ChannelMonitorBalance|ChannelMonitorAllowsHealthCheckAutoEnable)' -count=1 -timeout=180s -v
go test ./controller -run '^TestChannelMonitorBalanceSource' -count=1 -timeout=120s -v
go test ./controller -run 'Test(AutoDisableChannelMonitorForLowBalance|RecordChannelMonitorBalanceUpdate|FetchChannelMonitorUpstream|ManualSharedUpstreamRequest|SaveChannelMonitorCustomFixedBalance|RunChannelRatioMonitorTask|CostRatioRecovery|ChannelMonitor.*Recover|UpstreamAutomation)' -count=1 -timeout=180s -v
go build ./...
```
