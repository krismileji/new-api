# 条件触发接口的今日次数与手动重置

“上游配置与策略 → 条件触发接口”在每日次数输入框旁显示“今日已调用 / 上限 / 剩余次数”。统计使用已保存规则的执行时区和上限；尚未保存的上限修改会单独提示。保存配置保留原有调用次数，例如已经调用 1 次，将上限从 1 改为 3 并保存后，剩余 2 次。

“重置今日次数”需要确认，仅清零选中规则当天的调用计数。冷却时间、已触发状态、最近一次执行时间、执行结果和其他规则的计数均保留，不保存表单草稿，也不执行上游接口。尚未保存的规则、今日没有调用、状态读取失败或接口仍在执行时不能重置。

后端在现有事务及行锁内校验已保存规则、规则时区对应的日期、已调用次数和最近登记时间。确认期间跨日、新增调用或规则删除会拒绝重置，避免清除用户尚未看到的调用；成功后记录操作者、渠道、规则、日期及重置前次数。每日限额、冷却和条件恢复限制仍由后端执行逻辑统一判断。

接口：`POST /api/channel_monitor/channel/:id/upstream/actions/:action_id/reset-count`，继承渠道监控路由的 Root 权限限制。请求包含 `day`、`attempts`、`last_attempt`，成功返回更新后的规则状态。

复用现有 `SystemTask.State`，没有数据库表结构、索引或迁移变更。新增服务及控制器文件实现重置；现有改动涉及下游渠道监控路由和前端功能目录，这些路径在已检查的 `upstream/main` 中均不存在，未修改上游所有文件。

## 验证记录

日期：2026-09-16，Go 1.25.1，Windows amd64。

| 数据库 | 实际版本 | 结果 |
| --- | --- | --- |
| SQLite | 3.50.4 | 通过 |
| MySQL | 5.7.44，utf8mb4 | 通过 |
| PostgreSQL | 9.6.24 | 通过 |

新增测试覆盖 8 种重置成功或拒绝场景，以及配置保存、HTTP 重置、后续触发限制的集成流程，分别运行在上述三个真实数据库上，共 27 个场景。既有动作执行及成功后刷新指标的三数据库回归也通过。没有跳过 MySQL 或 PostgreSQL 测试。

使用独立本地测试实例，命令如下（凭据仅用于一次性测试数据库）：

```powershell
docker run --detach --name new-api-custom-action-mysql-test -e MYSQL_ROOT_PASSWORD=custom-action-test -e MYSQL_DATABASE=new_api_custom_action_test -p 127.0.0.1:13392:3306 mysql:5.7.44
docker run --detach --name new-api-custom-action-postgres-test -e POSTGRES_PASSWORD=custom-action-test -e POSTGRES_DB=new_api_custom_action_test -p 127.0.0.1:15492:5432 postgres:9.6
docker exec new-api-custom-action-mysql-test mysql -uroot -pcustom-action-test -e 'ALTER DATABASE new_api_custom_action_test CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci'

$env:TEST_CUSTOM_ACTION_MYSQL_DSN = 'root:custom-action-test@tcp(127.0.0.1:13392)/new_api_custom_action_test?charset=utf8mb4&parseTime=True&loc=Local'
$env:TEST_CUSTOM_ACTION_POSTGRES_DSN = 'postgres://postgres:custom-action-test@127.0.0.1:15492/new_api_custom_action_test?sslmode=disable'
& D:/go-toolchain-1.25.1/go/bin/go.exe test ./controller -run 'TestChannelMonitorCustomActionReset' -count=1 -v
& D:/go-toolchain-1.25.1/go/bin/go.exe test ./controller ./service -run '^TestChannelMonitorCustomAction' -count=1 -v
& D:/go-toolchain-1.25.1/go/bin/go.exe build ./...
```

新增矩阵通过（6.489 秒）；完整条件触发接口回归通过（控制器 29.352 秒，服务层 3.417 秒）；Go 构建通过。集成测试初次因配置保存会清空旧余额而缺少测试指标失败，已显式初始化新指标后重新通过。测试关闭 Redis，因此保存配置时余额预估缓存的预期不可用提示不影响本功能验证。

从 `web/` 执行以下前端检查并通过：

```powershell
bun run test src/features/channel-monitor/components/__tests__/custom-action.test.tsx src/features/channel-monitor/components/__tests__/custom-variable.test.tsx src/features/channel-monitor/components/__tests__/upstream-sync.test.tsx src/features/channel-monitor/__tests__/api.test.ts
bun run typecheck
bunx --no-install oxlint -c .oxlintrc.json src/features/channel-monitor/api.ts src/features/channel-monitor/components/upstream-config-dialog.tsx src/features/channel-monitor/components/channel-monitor-custom-action-fields.tsx src/features/channel-monitor/components/channel-monitor-custom-action-quota.tsx src/features/channel-monitor/components/__tests__/custom-action.test.tsx
bunx --no-install oxfmt --check src/features/channel-monitor/api.ts src/features/channel-monitor/components/upstream-config-dialog.tsx src/features/channel-monitor/components/channel-monitor-custom-action-fields.tsx src/features/channel-monitor/components/channel-monitor-custom-action-quota.tsx src/features/channel-monitor/components/__tests__/custom-action.test.tsx
bun run build
```

四个相关前端测试文件共 43 个用例通过，涵盖新增规则、上限修改及保存、确认重置、跨日计数、失败提示、执行中禁止重置；最终调整条件渲染后再次通过本功能 7 个交互用例、类型检查、lint 和格式检查。
