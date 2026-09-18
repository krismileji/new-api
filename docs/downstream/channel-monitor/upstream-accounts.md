# 渠道监控共享上游账户

## 已确认的业务边界

- 一个账户代表同一余额池，账户拥有监控认证、余额接口与计算、充值/订阅换算、余额保护和自动任务。
- 渠道保留转发 Key、上游分组、倍率与倍率获取规则、调度及历史成本。
- 账户余额包含所有关联渠道的消耗和在途预估；总览按账户去重。
- 低余额统一告警并保护全部关联渠道；恢复只作用于因余额自动停用且满足恢复条件的渠道。
- 余额任务可复用账户配置；倍率规则必须选择渠道。同一账户可配置多个自动任务，各任务独立保留规则、检查间隔、调用次数、冷却和历史。

## 使用方式

1. 打开「渠道监控 → 上游账户 → 从渠道创建账户」。选择已配置渠道作为余额与认证来源，填写账户名称和余额刷新间隔。
2. 勾选共用余额的渠道，先预览配置差异，再确认关联。只能关联相同上游类型或尚未配置上游的渠道；来源渠道必须保留关联。渠道自身的倍率、倍率获取规则、分组和调度策略保留。
3. 点击「刷新余额」建立新余额基线。余额池归集所有成员的已完成费用及在途费用；渠道列表中的同一账户余额是同一份数据，应在账户列表查看，不能按渠道相加。
4. 在「上游自动任务」中按需新建任务，选择账户即可复用其认证与余额配置；倍率规则还须选择账户内的来源渠道。多个任务分别调度和执行，相同规则在不同任务中的调用次数和冷却独立计算。
5. 后续认证、余额接口、余额公式、充值／订阅换算及保护阈值在「编辑共享配置」维护。代理、余额查询 API Key、刷新间隔和成员在「管理关联」维护。渠道自己的配置窗口保留倍率及渠道策略编辑，共享字段只读。

账户刷新间隔为 0 时关闭定时余额刷新，手动查询和自动任务仍可按需读取。余额恢复是否自动启用遵循现有「余额恢复后自动启用」设置及倍率／健康检查条件；手动停用的渠道保持停用。Redis 未启用时保存上游原始余额，实时费用预估不可用；Redis 覆盖不完整时不依据不完整估算恢复渠道。

只有到期且有关联渠道的账户会产生定时查询任务；运行记录沿用渠道倍率监控任务的历史保留期，清理运行记录不会删除账户自动任务规则。

关联操作不会按域名自动推断账户。关联后的旧渠道任务继续使用自身配置执行，创建账户任务不影响已有任务。解除关联后渠道保留当前配置独立运行，在途请求继续结算到原余额池。删除账户须先解除成员并删除或处理引用它的自动任务。

同一任务保留防重复执行保护。账户凭据与余额写入仍受账户锁保护；账户被其他任务或余额查询占用时，本任务保持待检查状态，在后续调度中重试，不累计失败或消耗检查间隔。历史版本已停用的任务保持停用，其调用记录和待确认状态保留，可继续编辑。

每个账户最多关联 100 个渠道。Sub2API API Key 账户创建时需使用单 Key 来源渠道，账户单独保留余额查询 Key；各渠道的转发 Key 和倍率查询仍保持各自归属。空账户可重新关联渠道或删除；共享配置编辑使用其关联渠道作为操作入口。

## 自定义配置只复用账户余额

在「上游配置与策略」选择「自定义」，然后在「上游余额来源」选择「关联上游账户」并选中已有账户。也可继续使用「固定输入」或「接口查询」。账户可以是 New API、Sub2API 或自定义类型，不要求与当前自定义倍率请求使用相同地址。

此模式只复用账户的余额查询、余额计算和充值／订阅换算。渠道的倍率来源、地址、认证、自定义变量、分组、同步开关及保护阈值独立保存；账户凭据不会用于渠道自己的倍率请求。账户更新换算后，引用渠道同步使用新的换算。余额消费和在途预估仍归集到同一账户；暂停某个渠道的余额同步也不会漏算该渠道的实际消费。各渠道使用自己的阈值触发保护。

在「上游自动任务」选择「自定义（可单独关联账户余额）」，同样可在「上游余额来源」选择账户。任务的地址、代理、倍率、自定义变量和触发请求保持独立，动作前后读取账户余额；账户凭据不会注入任务动作。此类任务保留自身规则和调用限制。「测试获取指标」只读取指标，不执行动作、不改写账户快照或渠道状态。

切回固定输入或接口查询并保存后，渠道解除余额关联，在途消费继续结算到原余额池。仅关联余额的渠道会显示在账户成员列表中，切换来源仍从渠道的上游配置完成。删除账户前必须处理所有渠道和任务引用。

此次余额来源扩展使用现有账户表和自定义配置 JSON，不新增数据库字段。

## 下游边界

主要修改下游渠道监控文件。与本地 `upstream/main` 比较，本次仅 `model/main.go` 属于上游已有路径；其唯一改动是在现有 AutoMigrate 清单注册账户模型，以便正常启动创建及升级账户表。未改变依赖、计费扣款或 relaykit API。

## 验证记录

2026-09-17 至 2026-09-18，在隔离测试库中完成以下验证：

| 组件 | 实际版本 | 结果 |
| --- | --- | --- |
| SQLite | 3.50.4 | 新建、从 v1.0.0-rc.37 升级、再次启动及账户业务回归通过 |
| MySQL | 5.7.44 | 同上；独立日志库数据保留；测试库使用 utf8mb4 |
| PostgreSQL | 16.14 | 同上；独立日志库数据保留 |
| Redis | 8.10.1 | 跨渠道余额归集、重复结算去重、账户定位版本保护通过 |

最新上游发布版由 GitHub Releases API 确认为 `v1.0.0-rc.37`，使用 `git archive v1.0.0-rc.37` 在临时目录创建独立源码副本。升级测试先在发布版上启动并写入代表性用户、渠道、选项和消费日志，再在当前源码启动两次，检查数据、账户余额／凭据、渠道倍率／关联索引、主键及渠道唯一约束。新建数据库也连续启动两次。账户只属于主库；同时验证 MySQL/PostgreSQL 单独日志库的旧日志未受影响。

Go 工具：`D:/go-toolchain-1.25.1/go/bin/go.exe`，1.25.1。测试命令（下列 DSN 指向本次单独创建的 Docker 测试容器）：

```powershell
$env:TEST_CUSTOM_ACTION_MYSQL_DSN='root:account-test-only@tcp(127.0.0.1:23306)/new_api_custom_action_test?charset=utf8mb4&parseTime=True&loc=Local'
$env:TEST_CUSTOM_ACTION_POSTGRES_DSN='postgres://postgres:account-test-only@127.0.0.1:25432/new_api_custom_action_test?sslmode=disable'
$env:TEST_CHANNEL_BALANCE_REDIS_ADDR='127.0.0.1:26379'
& 'D:/go-toolchain-1.25.1/go/bin/go.exe' test ./model ./service ./controller -run 'TestUpstreamAccount|TestUpstreamAutomation|TestChannelBalance|TestChannelMonitorVariableGroup|TestChannelRatioMonitorBalance' -count=1
& 'D:/go-toolchain-1.25.1/go/bin/go.exe' build ./...
```

升级测试使用 `scripts/upstream-account-upgrade/upgrade_test.go`。同一文件复制到发布版临时目录，按以下模式分别执行（每步独立进程）：

```powershell
# 发布版：ACCOUNT_UPGRADE_MODE=seed
# 当前版升级库：ACCOUNT_UPGRADE_MODE=verify，连续两次
# 当前版新库：ACCOUNT_UPGRADE_MODE=fresh，再 ACCOUNT_UPGRADE_MODE=verify
& 'D:/go-toolchain-1.25.1/go/bin/go.exe' test ./scripts/upstream-account-upgrade -run '^TestUpstreamAccountUpgrade$' -count=1 -v
```

SQLite 通过 `ACCOUNT_UPGRADE_SQLITE_PATH` 指向临时文件；MySQL 使用 `SQL_DSN=root:account-test-only@tcp(127.0.0.1:23306)/account_upgrade?charset=utf8mb4&parseTime=True&loc=Local`，`LOG_SQL_DSN` 使用 `account_upgrade_log`；PostgreSQL 使用 `postgres://postgres:account-test-only@127.0.0.1:25432/account_upgrade?sslmode=disable`，日志库同样为 `account_upgrade_log`。新建路径改用 `account_fresh` 和 `account_fresh_log`。

前端验证（在 `web/` 执行）：

```powershell
bun run typecheck
bun x oxlint -c .oxlintrc.json <本次变更的渠道监控 TS/TSX 文件>
bun run test src/features/channel-monitor/components/__tests__/upstream-accounts.test.tsx src/features/channel-monitor/components/__tests__/upstream-automations.test.tsx src/features/channel-monitor/components/__tests__/upstream-config-credentials.test.tsx src/features/channel-monitor/components/__tests__/balance-estimate.test.tsx src/features/channel-monitor/components/__tests__/custom-variable.test.tsx src/features/channel-monitor/components/__tests__/upstream-sync.test.tsx src/features/channel-monitor/components/__tests__/variable-groups.test.tsx
bun run build
```

7 个前端测试文件、44 个用例通过；类型检查、涉及文件 lint、生产构建通过。新增界面采用延迟加载，保留全部现有版权头，未新增翻译键或修改语言包。后端新增回归覆盖账户轮询去重、不同渠道倍率保留、旧结果拒绝、成员保护、手动停用保留、凭据刷新、内置账户认证继承及任务只执行一次。

### 余额来源扩展验证（2026-09-18）

使用相同版本的真实 SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 16.14 和 Redis 8.10.1 再次验证。数据库连接环境变量与上文一致，本次容器为 `codex-balance-source-mysql`、`codex-balance-source-postgres`、`codex-balance-source-redis`；只使用独立测试库。

```powershell
& 'D:/go-toolchain-1.25.1/go/bin/go.exe' test ./model ./service ./controller -run 'TestChannelMonitorBalanceSource|TestNormalizeChannelMonitorBalanceAccountSource|TestUpstreamAccount|TestUpstreamAutomation|TestChannelBalance|TestChannelMonitorVariableGroup|TestChannelRatioMonitorBalance|TestSaveChannelMonitorUpstreamConfig' -count=1
& 'D:/go-toolchain-1.25.1/go/bin/go.exe' build ./...
```

全部通过。新增验证覆盖跨上游类型关联、独立倍率认证、账户换算更新、切回自定义后解除引用、任务动作认证隔离、草稿测试不改写快照、失效账户拒绝、任务引用阻止删除、同一余额池按各渠道阈值分别保护，以及暂停同步的成员仍计入共享消费。

前端执行以下用例及 `bun run typecheck`、涉及 11 个文件的 `oxlint` / `oxfmt --check`、`bun run build`，全部通过；11 个测试文件共 79 个用例。

```powershell
bun x vitest run src/features/channel-monitor/components/__tests__/balance-source.test.tsx src/features/channel-monitor/components/__tests__/upstream-accounts.test.tsx src/features/channel-monitor/components/__tests__/upstream-automations.test.tsx src/features/channel-monitor/components/__tests__/upstream-config-credentials.test.tsx src/features/channel-monitor/components/__tests__/custom-variable.test.tsx src/features/channel-monitor/components/__tests__/upstream-sync.test.tsx src/features/channel-monitor/components/__tests__/variable-groups.test.tsx src/features/channel-monitor/lib/__tests__/schema.test.ts src/features/channel-monitor/lib/__tests__/upstream-request.test.ts src/features/channel-monitor/lib/__tests__/custom-variable.test.ts src/features/channel-monitor/lib/__tests__/custom-action.test.ts
```

此扩展只修改下游文件；未增加上游文件改动、依赖、语言包或 relaykit 改动。前述账户表迁移验证仍适用。

### 自动任务独立执行验证（2026-09-18）

已移除任务合并入口、API、服务及事务实现、配置标记和单账户单任务限制。关联账户不会再阻止旧渠道任务执行。原有任务和执行历史保留，历史停用任务不会自动启用；再次保存时丢弃旧配置中的合并标记。

Go `go1.26.5 windows/amd64`；真实 SQLite **3.50.4**、MySQL **5.7.44**、PostgreSQL **9.6.24**。后两者使用独立 Docker 容器 `codex-task-independence-mysql` 和 `codex-task-independence-postgres`，测试库与日常数据库隔离。

```powershell
$env:TEST_CUSTOM_ACTION_MYSQL_DSN='root:task-test-only@tcp(127.0.0.1:23306)/new_api_custom_action_test?charset=utf8mb4&parseTime=True&loc=Local'
$env:TEST_CUSTOM_ACTION_POSTGRES_DSN='postgres://postgres:task-test-only@127.0.0.1:25432/new_api_custom_action_test?sslmode=disable'
go test ./controller -run 'TestUpstreamAccount|TestUpstreamAutomation' -count=1
go build ./...
```

全部通过，三个数据库均实际运行。回归覆盖同账户多个任务分别执行、检查间隔与计数及冷却独立、关联账户前后旧任务继续执行、账户占用不消耗任务调度、历史停用任务可编辑且保留状态，以及同一任务并发检查不重复执行。本次不改变表结构、迁移或数据库依赖。

前端在 `web/` 执行：

```powershell
bun run test src/features/channel-monitor/components/__tests__/upstream-accounts.test.tsx src/features/channel-monitor/components/__tests__/upstream-automations.test.tsx src/features/channel-monitor/components/__tests__/upstream-automation-selection.test.tsx src/features/channel-monitor/components/__tests__/upstream-list-layout.test.tsx
bun run typecheck
bun x oxlint -c .oxlintrc.json src/features/channel-monitor/api-automations.ts src/features/channel-monitor/lib/upstream-account.ts src/features/channel-monitor/components/upstream-accounts-dialog.tsx src/features/channel-monitor/components/upstream-account-editor.tsx src/features/channel-monitor/components/upstream-automations-dialog.tsx src/features/channel-monitor/components/upstream-automation-metadata.tsx src/features/channel-monitor/components/__tests__/upstream-accounts.test.tsx src/features/channel-monitor/components/__tests__/upstream-automations.test.tsx
bun x oxfmt --check src/features/channel-monitor/api-automations.ts src/features/channel-monitor/lib/upstream-account.ts src/features/channel-monitor/components/upstream-accounts-dialog.tsx src/features/channel-monitor/components/upstream-account-editor.tsx src/features/channel-monitor/components/upstream-automations-dialog.tsx src/features/channel-monitor/components/upstream-automation-metadata.tsx src/features/channel-monitor/components/__tests__/upstream-accounts.test.tsx src/features/channel-monitor/components/__tests__/upstream-automations.test.tsx
bun run build
```

4 个测试文件共 20 个用例通过，类型检查、所改文件 lint、格式检查和生产构建通过。与 `upstream/main` 比较，本次所改路径均属下游，没有修改上游已有文件。
