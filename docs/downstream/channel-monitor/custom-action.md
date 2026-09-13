# 自定义上游的条件触发接口

入口：**渠道监控 → 上游配置与策略 → 自定义 → 条件触发接口**。

可按上游余额或上游倍率设置“小于、小于等于、大于、大于等于”阈值，满足条件后调用指定接口。最多 8 条规则，各自设置请求、执行时区、每日时段、每日次数和冷却时间。新规则默认关闭，完成配置后开启并保存。

## 余额不足时重置示例

| 设置 | 示例 |
| --- | --- |
| 指标与条件 | 上游余额小于 5 |
| 执行时区 | `Asia/Shanghai`（北京时间） |
| 开始时间 | `00:05`，给零点自动重置留出时间 |
| 截止时间 | `23:00`，23:00 起不再发起调用 |
| 每日最多调用次数 | 1 |
| 两次调用最短间隔 | 60 分钟 |
| 请求 | 按上游实际要求填写，例如 `POST /api/reset` |
| 成功判定 | 若响应是 `{"success":true}`，路径填 `success`，期望值填 `true` |

开启余额定时刷新后，可自动检查。手动刷新已保存渠道的余额、倍率，也会检查对应指标的规则。**保存配置、测试获取、请求并回填变量不会执行触发接口。** 此功能不单独轮询，检查频率沿用对应指标的刷新设置。

余额按接口提取并应用结果乘数后的上游余额判定，不使用本地估算扣减后的余额；倍率按上游倍率判定，不包含人民币成本换算。固定值也只在对应指标刷新时检查。

## 触发与限制

- 第一次取得的有效指标已经满足条件时，也可以触发。
- 一次触发后，持续满足条件不会重复执行。必须成功读取到条件外的值，再重新满足条件，才允许下一次触发。跨日不会直接解除这个限制。
- 不在允许时段、冷却未结束或当日次数已满时不执行，也不排队补发。下一次成功刷新时重新判断当前值。
- 每日次数按规则时区的自然日计算。时段包含开始时刻，不包含截止时刻，暂不支持跨日时段。
- 发送前先保存执行登记与次数。并发刷新和服务重启共用持久状态；编辑、关闭后再开启同一规则也保留其执行限制。
- HTTP 错误、超时、响应不符合成功判定及结果未知均计入已登记次数，不自动重试。接口不跟随重定向，也不在认证失败后刷新凭据并重放操作。
- 若服务在登记后退出，执行记录会保持“结果待确认”，自动执行暂停。需先核对上游是否已经重置；确认后如需重新配置，可删除旧规则并添加新规则。新规则有独立的次数限制。

准备凭据后会再次核对配置修订、指标值和截止时间。执行超时不超过渠道监控设置的请求超时和本次截止时间的剩余时长。已经发出的请求无法保证在上游撤销，应根据上游处理耗时预留余量。

成功执行不会立即修改余额或倍率，下一次正常刷新获取新值。若同时使用余额自动禁用策略，建议将重置阈值设在自动禁用阈值之上。

## 请求与执行结果

支持现有自定义请求的 GET、POST、查询参数、请求头、JSON 和表单。基础地址留空时使用该渠道上游地址，也可以指定单独的接口基础地址。沿用渠道代理和现有 SSRF 防护。

查询参数和请求头可使用已配置独立请求的变量。执行前按“每次更新前获取”刷新，缺少初始值时也会先获取；“更新失败时获取”在触发接口中只使用已有值，不会因重置失败再次尝试。请求参数和敏感请求体沿用现有隐藏与保留机制；更换触发接口基础地址不会自动沿用旧地址下隐藏的凭据。

成功判定路径留空时，以 HTTP 2xx 判断成功。填写后还要求 JSON 路径存在、结果为字符串/数字/布尔值，并与期望值相符。期望字符串不加引号，数字或布尔值可填 `0`、`true`、`false`。执行结果不保存响应正文或凭据。

重新打开配置可查看每条规则最近一次登记时间、指标值、当天已登记次数和执行结果。接口失败不会把已成功的指标刷新标记为失败，也不会进入倍率/余额获取的自动重试流程。

## 实现与验证

配置扩展现有 `custom_upstream_config` JSON。运行状态复用已有 `system_tasks.state`，每个渠道保留一条专用记录，按规则 ID 保存次数和最近结果；该类型不加入普通历史任务清理列表。没有新增表、字段、索引、约束、数据库驱动或迁移，也不读写独立日志库。

与本地 `upstream/main` 对比，本次修改的已有文件均为下游文件，未修改上游已有文件或 `relaykit` 模块。

真实数据库验证：SQLite **3.50.4**、MySQL **5.7.44**（utf8mb4）、PostgreSQL **9.6.24**。覆盖旧 JSON 配置升级、并发触发去重、重新打开数据库及再次迁移后状态保留、每日次数、冷却、失败不重试、截止边界、凭据准备跨过截止时间、过期配置和指标、倍率与变量、业务成功判定、GET 结果未知不重放。独立测试表使用临时前缀，完成后清理。

数据库测试命令（PowerShell，临时测试实例）：

```powershell
$env:TEST_CUSTOM_ACTION_MYSQL_DSN='root:action-test@tcp(127.0.0.1:13368)/action_test?charset=utf8mb4&parseTime=True&loc=Local'
$env:TEST_CUSTOM_ACTION_POSTGRES_DSN='host=127.0.0.1 port=15468 user=postgres password=action-test dbname=action_test sslmode=disable'
& D:/go-toolchain-1.25.1/go/bin/go.exe test ./service ./controller -run '^TestChannelMonitorCustomAction' -count=1 -v
```

前端验证（在 `web/`）：

```powershell
bun run test src/features/channel-monitor/lib/__tests__/custom-action.test.ts src/features/channel-monitor/components/__tests__/custom-action.test.tsx src/features/channel-monitor/lib/__tests__/schema.test.ts src/features/channel-monitor/lib/__tests__/upstream-request.test.ts src/features/channel-monitor/lib/__tests__/custom-variable.test.ts src/features/channel-monitor/components/__tests__/custom-variable.test.tsx src/features/channel-monitor/components/__tests__/upstream-config-credentials.test.tsx
bun run typecheck
bun run build
```

另对改动文件执行格式与 lint 检查、`git diff --check`，并运行相关后端回归与根模块构建。
