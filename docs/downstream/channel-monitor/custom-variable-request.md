# 自定义上游的独立请求与变量

在「渠道监控 → 上游配置与策略」中选择「自定义」，点击「添加独立请求」。支持多个独立请求，每个请求设置自己的刷新策略，并从一次响应中映射多个变量。倍率与余额可以共用这些变量。

请求使用可折叠卡片展示。收起时显示请求名称、请求方式与路径、刷新策略、变量名称及已有值的数量；展开后编辑请求和多行变量映射。查询参数、请求头中的「插入变量」菜单按所属请求分组，插入 `{{变量名}}`，也可自行组合前缀或多个变量。

例如「登录获取凭据」请求产生 `token` 和 `user_id`，策略为「更新失败时获取」；「余额服务认证」请求产生 `balance_token`，策略为「每次更新前获取」。两者互相独立，余额查询不会触发仅由倍率使用的请求。

## Token 自动更新示例

1. 添加请求并命名，例如「登录获取凭据」。在变量映射中将变量名设为 `token`，「当前值」填写已有 Token，也可以留空。
2. 配置独立请求，例如 `POST /api/login`。基础地址留空时使用上方的自定义接口基础地址，也可以配置其他认证服务的地址。
3. 按认证接口要求填写查询参数、请求头、JSON 或表单请求体。含账号密码的 JSON 请求体应开启「敏感请求体」，参数也可标记为敏感。
4. 如果响应为 `{"data":{"access_token":"...","user_id":42}}`，将 `token` 的 JSON 取值路径设为 `data.access_token`；点击「添加变量」，将 `user_id` 映射到 `data.user_id`。响应为纯文本时选择「文本」，每个映射使用完整文本响应。
5. 在倍率、余额的请求头中添加 `Authorization`，开启该行「使用变量模板」，填写 `Bearer {{token}}`。查询参数同样可开启变量模板并填写 `{{token}}`。
6. 选择「更新失败时获取」并保存。

保存配置无需先调用接口。每张卡片的「请求并回填变量」只调用该请求，一并回填它的全部映射，不改变其他请求。任一映射失败时不回填部分结果。之后点击保存生效；其他独立请求、倍率或余额接口尚未完成时，也可先回填当前请求。

## 刷新规则

| 策略 | 行为 |
| --- | --- |
| 每次更新前获取 | 使用本请求变量的倍率或余额更新开始前，先调用该独立请求，再应用新值查询。 |
| 更新失败时获取 | 先使用当前值查询；接口请求、HTTP 状态或结果提取失败后，只刷新失败接口所引用的、采用此策略的独立请求，再重试一次。 |

没有初始值时，首次使用变量会先调用其所属独立请求。只有固定输入或当前请求未引用变量时，不调用独立请求。同一次倍率与余额更新中，共用的独立请求最多执行一次，不会因引用多个变量而重复执行。

已保存渠道自动获取的新值会保存到原有自定义配置中，后续更新继续使用。独立请求失败、映射无效或替换后的参数超限时保留原值；获取成功后若业务接口仍失败，会正常报告失败，不再循环获取。一次操作沿用渠道监控的上游请求超时限制。

同一进程中，同时发起的倍率和余额更新按渠道串行协调；「更新失败时获取」会读取最新保存的值，避免重复更新过期 Token。保存前核对配置修订号和完整旧值；并发修改配置或其他节点已替换变量时，丢弃旧结果。

变量值按敏感信息处理，配置查询只返回是否已有值。重新编辑时，隐藏值留空会保留已保存值，变量模板仍可见。认证请求仅返回映射值，应用变量的业务接口响应预览会隐藏。更换独立请求基础地址后，不沿用旧地址下隐藏的认证参数。

最多配置 8 个请求、合计 32 个变量。变量名在本渠道所有独立请求之间不能重复，支持字母、数字、下划线，以字母或下划线开头，最多 64 个字符。删除请求或重命名变量后，仍引用旧变量的参数会提示校验错误。模板仅支持倍率、余额的查询参数和请求头；独立请求本身不支持引用变量。JSON 映射支持字符串、数字和布尔值，保留 `0`、`false`；空值、对象、数组和包含非法控制字符的值会被拒绝。变量及替换后参数最多 8192 字节，完整配置仍受现有 60 KB 限制。

旧版单请求、单变量配置可直接读取，会在编辑器中呈现为一张请求卡片；原值、取值路径和刷新策略保持不变，保存后写入新的多请求格式。

## 验证记录

本次仅扩展现有 `custom_upstream_config` JSON 内容，无新增表、字段、索引、约束或迁移。自动变量保存仅写主库的渠道监控配置，不涉及独立日志库。修改的已有文件均为下游独有文件，与本地 `upstream/main` 比较后确认未修改上游已有文件。

真实数据库验证：SQLite **3.50.4**、MySQL **5.7.44**（utf8mb4）、PostgreSQL **9.6.24** 均通过。覆盖旧版无变量配置读写、新值保存、重新打开数据库后数据保留、并发倍率/余额刷新、多变量整体保存且不覆盖其他请求、任一映射失败保留原值、配置变更后拒绝覆盖，以及凭据大小写不同不能匹配。测试使用独立表前缀并清理本次测试表。

本次数据库命令（PowerShell；账号与端口均为本次临时测试实例）：

```powershell
docker exec new-api-variable-test-mysql sh -c 'mysql -uroot -p"$MYSQL_ROOT_PASSWORD" -e "ALTER DATABASE monitor_variables CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;"'
$env:TEST_CUSTOM_VARIABLE_MYSQL_DSN='root:variable-test-password@tcp(127.0.0.1:56657)/monitor_variables?charset=utf8mb4&parseTime=True&loc=Local'
$env:TEST_CUSTOM_VARIABLE_POSTGRES_DSN='host=127.0.0.1 port=56667 user=postgres password=variable-test-password dbname=monitor_variables sslmode=disable'
& D:\go-toolchain-1.25.1\go\bin\go.exe test ./service -run '^TestChannelMonitorCustomVariableDatabaseMatrix$' -count=1 -v
```

相关后端验证：

```powershell
& D:\go-toolchain-1.25.1\go\bin\go.exe test ./service ./controller -run 'Test(NormalizeChannelMonitorCustom|ChannelMonitorCustom|FetchChannelMonitorCustom|FetchChannelMonitorUpstream|TestChannelMonitorUpstream|SaveChannelMonitorUpstream|ResolveChannelMonitorUpstream|RunChannelRatioMonitorTaskUpdatesCustomFixedSources)' -count=1
& D:\go-toolchain-1.25.1\go\bin\go.exe build ./...
```

前端验证（从 `web/` 运行）：

```powershell
bun run test src/features/channel-monitor/lib/__tests__/schema.test.ts src/features/channel-monitor/lib/__tests__/upstream-request.test.ts src/features/channel-monitor/lib/__tests__/custom-variable.test.ts src/features/channel-monitor/components/__tests__/custom-variable.test.tsx src/features/channel-monitor/components/__tests__/upstream-config-credentials.test.tsx
bun run typecheck
bun run build
```

另对本次全部前端文件执行 `oxlint` 和保留版权头的 `oxfmt` 检查，对改动执行 `git diff --check`。
