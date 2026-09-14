# 共享请求与变量

渠道监控工具栏新增「共享请求与变量」，可独立创建、编辑和删除共享配置。每份配置包含名称、基础地址、代理、超时、独立请求、变量映射及各请求的刷新策略。

1. 在「共享请求与变量」中新建配置，填写登录等请求及需要提取的变量。
2. 在渠道的「上游配置与策略」中选择「自定义上游」，再选择这份共享配置并保存。
3. 其他渠道选择同一份配置，即可在倍率、余额和触发规则的请求中插入这些变量。

渠道尚未选择共享配置、也没有旧的独立请求时，可直接点击接口参数的「插入变量」。菜单按「共享配置名 · 请求名」分组；选择变量会在当前草稿中绑定对应配置并插入 `{{变量名}}`，保留已有的 `Bearer ` 等前缀，保存渠道后生效。已经绑定配置的渠道只列出该配置的变量；列表加载、加载失败和无可用变量时均显示说明，失败后可以直接重试。

变量名的唯一性范围是一份共享配置：同一配置内的所有请求不能重复定义同名变量，前后端都会拒绝保存；不同共享配置可以各自定义 `token` 等同名变量。每个渠道只引用一份共享配置，不会跨配置按名字合并或覆盖。旧的渠道独立请求仍只使用本渠道的变量。

已有渠道的独立配置仍可使用。在原配置中点击「另存为共享配置」，可带入已保存的隐藏凭据。保存共享配置后，再保存渠道以切换到共享引用；其他渠道直接选择该共享配置即可。

## 刷新与编辑

- 请求与变量值只在共享配置中保存一次；各渠道继续保存自己的接口、API Key 参数和策略。
- 「更新失败时获取」会复用已有变量；缺少值或相关接口失败时，刷新对应请求并最多重试一次。同一实例内并发更新的渠道共用刷新后的值。
- 「每次更新前获取」保持原语义：每次监控操作在使用对应变量前重新请求。未引用的独立请求不会执行。
- 「请求并回填变量」只更新编辑草稿，点击「保存共享配置」后生效。
- 配置列表隐藏变量值和标记为敏感的请求参数。保留留空的已配置值时，后端会使用最新保存的值。
- 编辑共享配置会更新所有引用渠道的配置修订号，丢弃旧配置下尚未完成的监控结果。删除被引用的配置、移除仍被渠道模板使用的变量、覆盖并发修改都会被拒绝。
- 改变共享基础地址或代理后，隐藏凭据需要重新填写。共享请求使用自己的网络配置，不依赖某个渠道的地址或代理。

## 存储与兼容

新增主库表 `channel_monitor_variable_groups`，请求配置使用普通 TEXT 字段。渠道原有 `custom_upstream_config` JSON 增加可选 `variable_group_id`，共享引用不嵌入请求或变量值。旧版 `variable_request` 和 `variable_requests` 保持兼容，不自动合并不同渠道的凭据。

独立日志库不使用新表，也不参与共享变量的读写。没有修改 ORM、驱动依赖或 `relaykit` 接口。

本次唯一修改的既有上游所有文件是 `model/main.go`，仅在已有 AutoMigrate 列表中增加新模型；集中迁移没有独立注册钩子。其余既有文件均为下游所有，按本地 `upstream/main` 核对。

## 验证记录

2026-09-14，Windows，Go 1.26.5。

| 数据库 | 实测版本 | 共享配置及旧渠道兼容 | 空库启动、重复迁移 |
| --- | --- | --- | --- |
| SQLite | 3.50.4 | 通过 | 通过 |
| MySQL | 5.7.44 | 通过 | 通过 |
| PostgreSQL | 9.6.24 | 通过 | 通过 |

三个引擎均额外通过了最新发布版 `v1.0.0-rc.37`（`385d2dfd1`）创建的数据库升级验证，每个升级库连续运行两次完整启动迁移。较早的 `v1.0.0-rc.30` 升级路径也已验证。发布版目录使用独立 detached worktree，没有切换当前工作区分支。

`TestChannelMonitorVariableGroupDatabaseMatrix` 验证两个不同渠道的 API Key 参数保持独立、共用一次变量刷新、刷新值持久化、隐藏凭据保留、过期编辑拒绝、引用保护、触发规则引用、旧 JSON 保留以及重复 AutoMigrate。原有自定义请求、变量、触发规则及上游保存测试同步运行。

实际使用的独立测试容器与配置：

```powershell
docker run -d --name new-api-shared-variables-mysql57 -p 127.0.0.1:13317:3306 -e MYSQL_ROOT_PASSWORD=variable-test -e MYSQL_DATABASE=variable_test mysql:5.7.44
docker run -d --name new-api-shared-variables-pg96 -p 127.0.0.1:15496:5432 -e POSTGRES_PASSWORD=variable-test -e POSTGRES_DB=variable_test postgres:9.6.24
docker exec new-api-shared-variables-mysql57 mysql -uroot -pvariable-test -e 'ALTER DATABASE variable_test CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci'
$env:TEST_CUSTOM_VARIABLE_MYSQL_DSN='root:variable-test@tcp(127.0.0.1:13317)/variable_test?charset=utf8mb4&parseTime=True&loc=Local'
$env:TEST_CUSTOM_VARIABLE_POSTGRES_DSN='host=127.0.0.1 port=15496 user=postgres password=variable-test dbname=variable_test sslmode=disable'
go test ./service ./controller ./model -run 'Test.*(ChannelMonitorCustom|ChannelMonitorVariableGroup|ChannelRatioUpstream|SaveChannelMonitorUpstream)' -count=1 -timeout=5m
go build ./...
```

完整启动迁移使用 `scripts/channel-monitor-variable-groups/upgrade_test.go`：先从发布版执行 `seed`，创建用户、渠道和设置，再在当前工作区执行两次 `verify`。同时以 `fresh` 在空库启动，随后再次 `verify`。检查原有数据、用户名唯一性、主键约束以及新共享配置的保留。测试仅在显式设置 `VARIABLE_UPGRADE_MODE` 时运行，必须使用独立测试库。

```powershell
# 从发布版工作树执行；SQL_DSN 指向独立的 variable_upgrade37_test 数据库。
$env:SQL_DSN='root:variable-test@tcp(127.0.0.1:13317)/variable_upgrade37_test?charset=utf8mb4&parseTime=True&loc=Local'
# PostgreSQL 对应：postgres://postgres:variable-test@127.0.0.1:15496/variable_upgrade37_test?sslmode=disable
# SQLite 对应：SQL_DSN=''，VARIABLE_UPGRADE_SQLITE_PATH 指向 new-api-variable-upgrade37-20260914.db。
$env:VARIABLE_UPGRADE_MODE='seed'
go test D:/GoProjects/new-api/scripts/channel-monitor-variable-groups/upgrade_test.go -run TestSharedVariableUpgradeStartup -count=1 -v
# 从当前工作区执行。
$env:VARIABLE_UPGRADE_MODE='verify'
go test ./scripts/channel-monitor-variable-groups -run TestSharedVariableUpgradeStartup -count=2 -v
# SQLite 设置 SQL_DSN=''，并用 VARIABLE_UPGRADE_SQLITE_PATH 指定独立文件。
# 空库验证使用另一独立数据库，依次以 fresh、verify 各运行一次。
```

前端验证从 `web/` 运行：

```powershell
bun run test src/features/channel-monitor/components/__tests__/variable-insertion.test.tsx src/features/channel-monitor/components/__tests__/variable-groups.test.tsx src/features/channel-monitor/components/__tests__/custom-variable.test.tsx src/features/channel-monitor/components/__tests__/custom-action.test.tsx src/features/channel-monitor/components/__tests__/upstream-sync.test.tsx src/features/channel-monitor/components/__tests__/upstream-config-credentials.test.tsx src/features/channel-monitor/lib/__tests__/variable-group.test.ts src/features/channel-monitor/lib/__tests__/custom-variable.test.ts src/features/channel-monitor/lib/__tests__/custom-action.test.ts --maxWorkers=1
bun run typecheck
bun run build
```

涉及文件还执行了 `oxlint -c .oxlintrc.json`、保留版权头的格式检查和 `git diff --check`。下游界面文案使用简体中文，没有修改官方语言文件。

最终结果：前端 9 个测试文件、60 项测试通过，覆盖未绑定时插入变量并保存引用、同名变量按来源隔离、键盘插入并保留前缀、加载失败重试和空状态提示；相关后端 service/controller/model 测试通过；类型检查、lint、格式检查及前后端构建通过。测试容器及发布版临时工作树已清理。
