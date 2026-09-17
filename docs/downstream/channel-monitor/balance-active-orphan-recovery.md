# 余额恢复被残留 active 请求阻止

日期：2026-09-17。状态：本地修复与验证完成，未部署。

## 现场证据

渠道 #2 因余额 0 低于阈值 1 自动禁用。后续上游余额同步已恢复到
49.740274，Redis `coverage=1`、`sync_failed=0`、`completed=0`，
实际并发租约数为 0，但预估仍有 `active=6`、`unknown_active=2`、
`inflight=1456`（金额单位为百万分之一额度）。

六条记录均为 `status=active`，时间为当日 17:05 至 17:18，到 23:18
仍未释放；恢复标记均明确为 `CompletionUncertain=false`。
其中四条使用均值，每条占用 364；另两条费用未知。
`unknown_active` 使预估始终不完整，因而不能执行余额恢复启用。

此前 4996a2c31 仅回收查询开始前结束的 `unresolved` 普通请求。
完成记录丢失后仍为 `active` 的记录不满足该清理条件，后续成功查询
无法吸收。此次证据确认了该回收缺口，尚不能确认最初完成记录丢失的原因。

## 修复规则

- 仅在一次成功且修订号仍有效的上游余额查询中回收残留占用。
- 查询前后都必须通过写 Redis 确认业务请求与渠道测试均空闲，且
  查询期间没有新的费用跟踪缺口。已有 `coverage=1` 本身不等于空闲。
- 仅检查查询开始前的记录，要求同一 epoch，以及明确的同步请求标记。
  异步任务、WebSocket、无法判断类型的旧记录和查询期间新增请求保留。
- 沿用每轮最多 128 条与游标的有界清理，正确扣减均值/预算/未知计数。
  回收后保留去重记录，迟到的完成或 outbox 回调不重复扣费。
- 手动测试与自动健康检查在发送前建立独立的 Redis 活动租约，结束时
  释放，复用已有并发租约续期机制；不重复计入业务并发限制。
  无法建立租约时，本次测试返回错误，不发送无法被回收逻辑识别的请求。
- 原有恢复开关、成本倍率限制、配置修订和人工禁用保护继续生效。

部署时应更新所有处理请求和监控任务的实例，使直接测试的租约跟踪一致。
完成部署后，符合上述条件的旧记录会在成功余额同步时自动回收；
不需要清空 Redis、删除费用样本或修改用户账务。

## 变更归属

生产修改位于 `service/channel_balance_estimate.go`、
`service/channel_balance_estimate_redis.go`，新增
`service/channel_balance_probe_lease.go`。
与本地 `upstream/main` 对比，仅 `controller/channel-test.go`
是本次修改的上游已有文件：增加五行租约接入，覆盖不经过业务并发
入口的直接渠道测试，防止将正在执行的测试误判为空闲。
其余生产文件与新增测试均为下游路径。

没有修改数据库结构、迁移、数据库依赖或用户扣费算法，也没有修改
`relaykit`。工作区其他任务的改动未作为本次修复内容。

## 验证

Go 1.26.5。独立临时容器：Redis 8.10.1、MySQL 5.7.44、
PostgreSQL 9.6.24；SQLite 使用真实 SQLite 3.50.4。

新增测试先复现旧实现无法回收 active 占用，再验证回收、迟到回调
去重、查询前/后存在真实请求、查询期间新增请求与缺口、失败/旧查询、
异步记录和未知类型保护。三数据库完整链路复现六条现场记录，验证
余额刷新、渠道及 Ability 恢复，以及人工禁用不被覆盖。
渠道直接测试在成功与上游失败时都验证了活动租约的建立和释放。

以下凭据仅用于本次新建的隔离测试容器；MySQL 测试库使用 utf8mb4：

```powershell
docker exec codex-balance-orphan-mysql mysql -uroot -porphan_test_only -e 'ALTER DATABASE new_api_monitor_balance_test CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;'

$env:TEST_CHANNEL_BALANCE_REDIS_ADDR = '127.0.0.1:65363'
$env:MONITOR_BALANCE_MYSQL_DSN = 'root:orphan_test_only@tcp(127.0.0.1:65364)/new_api_monitor_balance_test?parseTime=true&charset=utf8mb4'
$env:MONITOR_BALANCE_POSTGRES_DSN = 'postgres://postgres:orphan_test_only@127.0.0.1:56418/new_api_monitor_balance_test?sslmode=disable'

& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./controller -run '^TestChannelMonitorBalanceOrphanRecoveryDatabaseMatrix$' -count=1 -v -timeout=180s
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./service ./controller -run 'TestChannelBalance|TestChannelDailyCost|TestTextChannelDailyCost|TestAudioChannelDailyCost|TestChannelTest|TestChannelMonitorBalance|TestChannelMonitorAllowsHealthCheckAutoEnable|TestRunChannelRatioMonitorTaskBalanceRecovery|TestRelayTaskWithChannelConcurrency' -count=1 -timeout=180s
& 'D:/Go/sdk/go1.26.5/bin/go.exe' build ./...
```

以上均通过。相关回归包含已有的三数据库余额安全矩阵。
未运行全仓库完整测试集。`git diff --check` 通过。

提交前从暂存区独立导出本次变更，重新验证编译与上述回归，避免依赖
工作区的共享账户功能。首次独立验证的 MySQL 容器使用了默认 latin1，
在中文测试数据写入时失败；设置上述测试库字符集后，以以下命令补跑
两个 MySQL 矩阵：

```powershell
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./controller -run '^TestChannelMonitorBalance(OrphanRecovery|Safety)DatabaseMatrix$/mysql' -count=1 -timeout=180s -json
```

补跑全部通过；独立提交内容的编译、Redis 回收测试、三数据库恢复链路
及相关回归验证完成。
