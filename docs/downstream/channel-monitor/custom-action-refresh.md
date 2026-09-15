# 条件触发接口成功后的指标刷新

条件触发接口（例如上游额度重置）成功后，立即重新获取本次允许同步的余额和倍率。手动刷新、自动更新及共用指标接口均使用补充刷新后的结果返回数据、计算余额阈值和执行分组倍率策略。

补充刷新保留单次手动刷新对同步开关的覆盖。指标共用接口时仅增加一次指标请求。成功动作执行后停止使用旧指标继续调用其他规则；补充刷新及同一轮指标重试可以解除已经恢复的规则状态，但不再调用触发接口。

接口调用失败、超时或结果未知时仍不重试动作。动作成功但指标刷新失败时，保留动作的成功状态和次数，返回“触发接口已成功，但重新获取上游指标失败”，避免将旧指标作为成功刷新结果交给后续策略。配置修订变化时丢弃旧请求结果。

本次复用了现有指标读取、余额记录及策略入口，没有新增表结构或数据库迁移。生产逻辑改动位于 `controller/channel_monitor_custom_action.go`、`controller/channel_ratio_monitor.go`、`controller/channel_ratio_monitor_task.go` 和 `service/channel_monitor_custom_action_runtime.go`。另为当前工作区新增的余额记录上下文参数补齐了调用参数。上述文件均为下游文件，在检查的 `upstream/main` 中不存在；未修改上游所有文件。

## 验证记录

验证日期：2026-09-16。Go：1.25.1，Windows amd64。数据库使用独立的本地测试实例。

| 数据库 | 实际版本 | 结果 |
| --- | --- | --- |
| SQLite | 3.50.4 | 通过 |
| MySQL | 5.7.44，数据库字符集 utf8mb4 | 通过 |
| PostgreSQL | 9.6.24 | 通过 |

新增矩阵覆盖 11 种场景、每种运行于三个数据库：手动余额/倍率刷新、自动更新、共用及独立指标接口、同步暂停、单次手动刷新覆盖、补充刷新失败、指标重试时禁止再次执行动作、配置修订冲突。断言包含 HTTP 返回值、数据库余额/倍率、动作状态和调用次数、分组倍率及渠道启用状态。既有动作并发去重、持久化、每日限额、失败不重试等矩阵也通过。

实际执行的 PowerShell 验证命令（测试数据库使用一次性测试凭据）：

```powershell
$env:TEST_CUSTOM_ACTION_MYSQL_DSN = 'root:custom-action-test@tcp(127.0.0.1:13392)/new_api_custom_action_test?charset=utf8mb4&parseTime=True&loc=Local'
$env:TEST_CUSTOM_ACTION_POSTGRES_DSN = 'postgres://postgres:custom-action-test@127.0.0.1:15492/new_api_custom_action_test?sslmode=disable'

& D:/go-toolchain-1.25.1/go/bin/go.exe test ./controller ./service -run '^Test(ChannelMonitorCustomAction|FetchChannelMonitorUpstream|Manual.*Upstream|RunChannelRatioMonitorTask)' -count=1 -v
& D:/go-toolchain-1.25.1/go/bin/go.exe test ./controller -run '^Test(ChannelMonitorCustomAction|FetchChannelMonitorUpstream|Manual.*Upstream|RunChannelRatioMonitorTask|ChannelBalance|RecordChannelMonitorBalance)' -count=1 -v
& D:/go-toolchain-1.25.1/go/bin/go.exe build ./...
git diff --check
```

第一条测试命令的服务层测试通过（3.043 秒）；该轮控制器测试遇到工作区余额写入接口增加上下文参数导致的编译失败，补齐参数后，第二条控制器测试命令通过（24.486 秒），包含全部 33 个新增数据库场景。最终构建和差异空白检查通过。初次 MySQL 验证遇到测试库默认 latin1 无法存储中文，调整专用测试库为 utf8mb4 后通过；没有为此修改生产代码。
