# 模型广场分组监控

模型广场分组监控向用户展示已配置分组的探测状态，入口为 `/group-monitor` 和 `/api/pricing/group-monitor`。管理员在 `/channel-monitor` 的分组监控设置中配置分类、分组、探测模型、间隔和展示窗口。

## 管理视图

Root 管理接口提供配置、候选模型、总览、手动执行和执行历史。配置使用 revision；探测间隔为 30 到 86400 秒，展示窗口至少覆盖两个探测周期，每组配置一个具体文本 `probe_model`。重复手动执行返回冲突。

配置采用“分类 → 分组”两级管理：先创建并命名分类，再在分类下添加监控分组。分类名称去除首尾空白后须为 1 到 64 个字符且不能重复，最多 100 个分类、合计 100 个监控分组。空分类也能独立保存；分类支持重命名和上移、下移，分类下的分组支持单独排序、移除及移动到其他分类。分类内还有分组时，须先移走或移除分组才能删除分类。修改分类不删除历史探测记录。

管理接口通过 `categories` 保存分类名称及顺序，`groups` 中的 `category` 指向已创建分类。保存时按分类顺序排列分组，并保留分类内的分组顺序。用户页面按相同顺序展示所有已配置分类和监控分组，空分类显示“此分类暂无监控分组”。旧配置按原有分类归入对应分类，没有分类的分组归入“未分类”；首次读取不写数据库，保存时转换为新格式。旧客户端不传 `categories` 时保留已有分类，包括空分类。

Worker 每个逻辑分组每轮只执行一次探测；上游失败可以在物理成员之间重试，但管理员执行记录保留最终逻辑结果和实际渠道。下一监测周期开始时仍未完成的分组会记录 timeout，已完成的分组结果保持不变；timeout 显示为黄色告警，且计入失败统计。结果包括 success、upstream_failure、rate_limited、local_failure、unavailable、timeout 和 skipped；skipped 不计入成功率。探测成本归属实际物理渠道。

探测请求根据渠道类型使用对应的 API 端点：Anthropic 渠道使用 Messages API (`/v1/messages`)，其他渠道使用 Responses API (`/v1/responses`)。系统自动选择适配的端点类型，无需手动配置。

分组探测执行前检查模型名称是否合法、是否仍在所选渠道的支持范围内，并过滤非文本模型。共享的探测校验已移除渠道类型白名单，解决 Claude 模型可选但执行时返回 `model_not_supported`（“不支持自动文本探测”）的问题。修复后需重新构建并部署后端，历史失败记录会保留。

## 用户视图

用户接口遵循 pricing 模块的启用和登录要求，以监控配置作为展示清单；访客、普通用户、管理员和超级管理员均看到相同的分类和监控分组，没有管理员预览分支。不会再按“用户可用分组”或分组倍率是否已配置过滤监控卡片。接口返回有序 `categories`，以及各分组的 group、可选的 category、initial、status、probe_model、latest_first_token_ms、success_rate、group_ratio、last_finished_at 和 recent_window。分组倍率沿用当前账号的实际倍率计算。不会返回渠道 ID、Key、成本、错误详情、租约或管理员配置；未加入监控配置的分组和历史记录也不返回。

状态包括 unconfigured、paused、pending、healthy、unavailable、unhealthy、rate_limited 和 stale。已配置的分组始终保留状态卡片及历史结果；渠道停用、模型能力关闭或探测模型移除时，用户侧显示 unavailable（暂不可用），管理接口显示 unconfigured 以提示检查配置。监控关闭时用户侧优先显示 paused（已停用）。

保存监控配置只决定状态展示，不授予模型调用权限，也不改变令牌分组、用户可用分组或计费配置。用户重新进入展示页、从其他标签切回展示页时会重新读取配置，避免 30 秒缓存期内仍显示保存前的分类。

管理 API 前缀为 `/api/channel_monitor/group_monitor`：

- `GET /settings`
- `PUT /settings`
- `GET /overview`
- `POST /run`
- `GET /executions`

用户 API 为 `GET /api/pricing/group-monitor`。Worker 扫描间隔由 `CHANNEL_GROUP_MONITOR_SCAN_INTERVAL_MS` 控制，默认 `1000` 毫秒，范围 `200..30000`。

## 分类功能验证（2026-09-13）

分类复用 `groups_json` 的 TEXT 配置，新格式包含 `categories` 和 `groups`，兼容原有分组数组；不新增表、列或数据库迁移，也不涉及单独的日志数据库。实际数据库验证覆盖分类顺序与空分类保存、新配置重读、重复两次 AutoMigrate 后数据保留、没有 `category` 的旧 JSON 配置、添加和移除分类、中文及补充平面 Unicode 字符。

| 数据库 | 实际版本 | 结果 |
| --- | --- | --- |
| SQLite | 3.50.4 | 通过 |
| MySQL | 5.7.44 | 通过 |
| PostgreSQL | 9.6.24 | 通过 |

执行环境为 Go 1.25.1、Windows amd64，MySQL 和 PostgreSQL 使用独立临时 Docker 实例。矩阵要求本地空数据库 `new_api_group_category_test`，连接通过 `GROUP_MONITOR_CATEGORY_MYSQL_DSN`、`GROUP_MONITOR_CATEGORY_POSTGRES_DSN` 注入；本次两个外部数据库用例均实际执行，无跳过。

```powershell
go test ./model -run 'TestChannelGroupMonitorCategoryDatabaseCompatibility' -count=1 -v
go test ./model -run 'Test.*(ChannelGroupMonitor|GroupMonitor)' -skip TestChannelGroupMonitorCategoryDatabaseCompatibility -count=1
go test ./controller -run 'Test.*(ChannelGroupMonitor|PricingGroupMonitor|GroupMonitor)' -count=1
```

以上后端测试均通过。前端在 `web/` 执行 `bun run test src/features/group-monitor src/features/channel-monitor/components/__tests__/channel-group-monitor-settings-sheet.test.tsx`，4 个文件、31 个用例通过，覆盖先创建分类再添加分组、分类重命名和排序、分类间移动分组、空分类保存、旧配置恢复、输入校验及保存失败后保留修改；`bun run typecheck`、受影响文件的 oxlint 和 `bun run build` 均通过。

`go test ./model -run 'TestChannelGroupMonitorCategory' -count=1 -v` 同时验证三数据库持久化、空分类不能启动手动探测，以及新格式按原有顺序向周期探测提供分组。

后续补充 `TestGetPricingGroupMonitorKeepsVisibleGroupsWhenRoutesUnavailable`，先复现路由失效时用户接口返回空列表，再修复展示过滤。6 个场景覆盖正常路由、手动停用、自动停用、能力关闭、探测模型移除及全局监控停用；均验证历史结果保留，且未加入监控配置的分组不会展示，修复后全部通过。

同日排查“分类已保存但用户页不显示”：实际配置包含 3 个分类、4 个分组，旧接口因可用分组和倍率过滤仅返回 `default`。`TestGetPricingGroupMonitorDisplaysConfiguredCategoriesForEveryRole` 以该配置复现问题，修复后验证访客、普通用户、管理员和超级管理员均返回相同的 3 个分类、4 个分组；另覆盖仅保存空分类的接口返回。此次修复不更改数据库结构、持久化格式或模型调用授权。

此次回归执行 `go test ./controller -run 'Test.*(ChannelGroupMonitor|PricingGroupMonitor|GroupMonitor)' -count=1` 通过；前端执行上述相关测试集，5 个文件、35 个用例通过，新增空分类展示、分类顺序、跨标签刷新及重新进入页面刷新用例。`bun run typecheck`、受影响文件 oxlint、格式检查、`bun run build` 以及包含前端资源的根模块 `go build` 均通过。

补充实际配置验证：使用 MySQL 8.4.10 的只读事务读取本地 revision 5 配置并调用修正后的用户接口处理器，四种角色均完整返回 `88`、`77`、`未分类`，分组数均为 4。验证未写入配置或启动探测任务。运行中的 Go 程序内嵌前端资源，更新后须重新编译运行才能加载修正后的接口及页面。

本次功能变更涉及的既有文件均为下游扩展文件，已与 `upstream/main` 比较，无需修改上游所有的文件。
