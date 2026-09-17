# 未确认请求余额占用恢复

日期：2026-09-17。状态：代码与定向验证完成，未部署。

## 问题与行为

普通请求结束后，如果没有可信的上游用量，原实现将其保留为
`unresolved`，继续计入进行中请求数和预估占用。成功同步上游余额时也不
释放这些记录，导致历史未确认请求跨同步累积。请求覆盖恢复又要求这个
预估计数先归零，实际并发已经空闲时仍可能无法恢复。

修复保留现有计算方式和按渠道、模型、计价配置隔离的近期均值：

- 成功同步余额时，吸收查询开始之前已经结束的普通未确认请求占用。
  真实成本账务仍保留原来的未确认状态，新余额成为监控的计算基准。
- 正在执行的请求、查询期间刚结束的请求继续保留占用；后者由下一轮
  成功同步处理。失败或已被更新请求取代的余额响应不释放占用。
- 异步任务提交和 WebSocket 中间用量继续标记为未确认结束。请求记录
  保存该标记，读取旧格式记录时使用已有恢复标记判别。
- 每轮同步最多检查 128 条占用记录，通过游标继续处理后面的记录，
  避免长请求阻挡清理。已有 141 笔积压在没有其他占用干扰的情况下需要
  两轮成功同步；30 分钟费用样本继续保留。
- 已吸收请求保留有期限的去重标记，迟到的用量或 outbox 回调不会再次
  扣减该笔消费。
- 请求覆盖恢复检查写 Redis 上的真实并发租约，要求查询开始和结束两侧
  都确认空闲；查询期间出现新的数据缺口时继续保持不完整。原先已经
  完整、且同步期间没有新缺口的覆盖状态继续有效。

本次仅调整下游余额监控的 Redis 状态和接入逻辑。没有修改数据库结构、
迁移、用户钱包计算或近期均值算法。与 `upstream/main` 比较，所有本次
修改的既有文件均为下游路径，没有修改上游已有文件。

## 验证

Go：1.26.5。使用本次独立创建的本机测试容器，未使用开发数据库。

| 引擎 | 实际版本 | 结果 |
| --- | --- | --- |
| Redis | 8.8.0 | 原子并发结算、未确认占用吸收、保留真实在途、重复回调通过 |
| SQLite | 3.50.4 | 余额安全矩阵及本次空闲恢复回归通过 |
| MySQL | 5.7.44 | 同上，字符集 utf8mb4 |
| PostgreSQL | 9.6.24 | 同上 |

余额安全矩阵包括已有的新建与代表已发布版本升级、重复迁移、余额保存、
状态转换、失败恢复和通知回归，并增加真实并发空闲时解除旧预估计数阻塞
的验证。

以下密码只属于本次临时测试容器：

```powershell
$env:TEST_CHANNEL_BALANCE_REDIS_ADDR = '127.0.0.1:16394'
$env:MONITOR_BALANCE_MYSQL_DSN = 'root:balance_sync_test_only@tcp(127.0.0.1:13394)/new_api_monitor_balance_test?parseTime=true&charset=utf8mb4'
$env:MONITOR_BALANCE_POSTGRES_DSN = 'postgres://postgres:balance_sync_test_only@127.0.0.1:15494/new_api_monitor_balance_test?sslmode=disable'

go test ./service ./controller -run 'TestChannelBalance|TestChannelDailyCost|TestTextChannelDailyCost|TestAudioChannelDailyCost|TestChannelTestDailyCost|TestChannelMonitorBalance|TestRelayTaskWithChannelConcurrency' -count=1 -timeout=180s

go test ./service ./controller -run 'TestChannelBalanceSyncKeeps|TestChannelMonitorBalanceSafetyDatabaseMatrix/.*/idle_balance_recovery' -count=1 -v -timeout=120s

go build ./...
```

以上检查均通过。新增回归覆盖旧格式记录、不同模型费用样本保留、缺价格
的异步任务、同步失败、查询边界、分批清理进度、迟到回调和查询期间数据
缺口。没有运行整个仓库的完整测试集。

验证后已停止并自动删除本次三个专用测试容器。`git diff --check` 和新增
Go 文件的格式检查通过；工作区其他并行任务的修改未纳入本次修复。
