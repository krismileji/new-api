# 模型广场分组监控

模型广场分组监控向用户展示可用分组的探测状态，入口为 `/group-monitor` 和 `/api/pricing/group-monitor`。管理员在 `/channel-monitor` 的分组监控设置中配置分组、探测模型、间隔和展示窗口。

## 管理视图

Root 管理接口提供配置、候选模型、总览、手动执行和执行历史。配置使用 revision；探测间隔为 30 到 86400 秒，展示窗口至少覆盖两个探测周期，每组配置一个具体文本 `probe_model`。重复手动执行返回冲突。

配置采用“分类 → 分组”两级管理：先创建并命名分类，再在分类下添加监控分组。分类名称去除首尾空白后须为 1 到 64 个字符且不能重复，最多 100 个分类、合计 100 个监控分组。空分类也能独立保存；分类支持重命名和上移、下移，分类下的分组支持单独排序、移除及移动到其他分类。分类内还有分组时，须先移走或移除分组才能删除分类。修改分类不删除历史探测记录。

管理接口通过 `categories` 保存分类名称及顺序，`groups` 中的 `category` 指向已创建分类。保存时按分类顺序排列分组，并保留分类内的分组顺序。用户页面按相同顺序展示有权限查看的分组，不返回空分类。旧配置按原有分类归入对应分类，没有分类的分组归入“未分类”；首次读取不写数据库，保存时转换为新格式。旧客户端不传 `categories` 时保留已有分类，包括空分类。

Worker 每个逻辑分组每轮只执行一次探测；上游失败可以在物理成员之间重试，但管理员执行记录保留最终逻辑结果和实际渠道。下一监测周期开始时仍未完成的分组会记录 timeout，已完成的分组结果保持不变；timeout 显示为黄色告警，且计入失败统计。结果包括 success、upstream_failure、rate_limited、local_failure、unavailable、timeout 和 skipped；skipped 不计入成功率。探测成本归属实际物理渠道。

探测请求根据渠道类型使用对应的 API 端点：Anthropic 渠道使用 Messages API (`/v1/messages`)，其他渠道使用 Responses API (`/v1/responses`)。系统自动选择适配的端点类型，无需手动配置。

## 用户视图

用户接口按 pricing 模块权限和当前用户可用分组过滤，返回 group、可选的 category、initial、status、probe_model、latest_first_token_ms、success_rate、group_ratio、last_finished_at 和 recent_window。分类只随可见分组返回。不会返回渠道 ID、Key、成本、错误详情、租约或管理员配置。

状态包括 unconfigured、paused、pending、healthy、unavailable、unhealthy、rate_limited 和 stale。已配置且有权限查看的分组始终保留状态卡片及历史结果；渠道停用、模型能力关闭或探测模型移除时，用户侧显示 unavailable（暂不可用），管理员视图显示 unconfigured 以提示检查配置。监控关闭时用户侧优先显示 paused（已停用）。

如果保存后仍没有分组，请检查该分组是否属于当前账号的可用分组。普通用户和访客遵循“用户可用分组”设置；管理员还可查看已配置分组倍率的分组。仅在渠道或监控配置中添加分组，不会自动授予用户查看权限。

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

后续补充 `TestGetPricingGroupMonitorKeepsVisibleGroupsWhenRoutesUnavailable`，先复现路由失效时用户接口返回空列表，再修复展示过滤。6 个场景覆盖正常路由、手动停用、自动停用、能力关闭、探测模型移除及全局监控停用；均验证历史结果保留且受限分组不会泄露，修复后全部通过。

本次功能变更涉及的既有文件均为下游扩展文件，已与 `upstream/main` 比较，无需修改上游所有的文件。
