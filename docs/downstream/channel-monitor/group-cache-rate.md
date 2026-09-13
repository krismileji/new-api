# 分组监控缓存率

管理员在“配置分组监控 → 展示设置”中使用“显示缓存率”开关，默认关闭。开启并保存后，用户分组监控页在成功率旁增加缓存率，桌面端与移动端均可查看。

缓存率沿用渠道监控 Redis 统计的请求口径：近 24 小时分钟桶内，命中缓存请求数除以有效缓存样本数，以百分比展示。有效样本按现有统计逻辑判断输入 token 数；探测请求不计入实际业务请求统计。零命中显示 `0.0%`，没有有效样本、Redis 不可用或 Redis 查询窗口限制不足 24 小时时显示“暂无数据”。缓存统计读取失败会记录后端错误，分组状态与探测历史继续返回。

管理配置 API 和用户 API 增加 `show_cache_rate`，管理总览和用户接口的分组条目按需返回 `cache_rate`（0–100 的百分比数值）。关闭时不查询缓存统计，也不返回分组缓存率字段；开启但无有效样本时不返回该分组的数值。只有已配置的监控分组会展示。

开关存入现有 `groups_json` TEXT 配置对象，不新增表、列、索引或迁移，也不改变日志数据库。旧数组配置及缺省字段均读取为关闭；旧客户端保存时未传 `show_cache_rate` 会保留已有开关，显式 `false` 可关闭。保存继续使用现有 revision 冲突校验。

## 验证记录

2026-09-13，Windows amd64，Go 1.25.1。三数据库使用临时 SQLite 文件和两个独立 Docker 测试实例；外部实例均实际执行，没有跳过。

| 数据库 | 实际版本 | 结果 |
| --- | --- | --- |
| SQLite | 3.50.4 | 通过 |
| MySQL | 5.7.44 | 通过 |
| PostgreSQL | 9.6.24 | 通过 |

配置验证覆盖开启保存与重读、两次 AutoMigrate 后开关和原有分组/分类数据保留、旧数组配置缺省关闭、后续更新与显式关闭、中文和 Unicode 配置。没有 schema 变更，旧配置在读取时不会触发迁移或回写。

测试实例与执行命令（项目根目录）：

```powershell
docker run --rm -d --pull never --name new-api-group-cache-mysql -p 127.0.0.1::3306 -e MYSQL_ALLOW_EMPTY_PASSWORD=yes -e MYSQL_DATABASE=new_api_group_category_test mysql:5.7.44 --character-set-server=utf8mb4 --collation-server=utf8mb4_unicode_ci
docker run --rm -d --pull never --name new-api-group-cache-postgres -p 127.0.0.1::5432 -e POSTGRES_HOST_AUTH_METHOD=trust -e POSTGRES_DB=new_api_group_category_test postgres:9.6

# 本次端口分别为 54985、59777；重新运行时以 docker port 输出为准。
$env:GROUP_MONITOR_CATEGORY_MYSQL_DSN='root@tcp(127.0.0.1:54985)/new_api_group_category_test?charset=utf8mb4&parseTime=true'
$env:GROUP_MONITOR_CATEGORY_POSTGRES_DSN='postgres://postgres@127.0.0.1:59777/new_api_group_category_test?sslmode=disable'
& 'D:\go-toolchain-1.25.1\go\bin\go.exe' test ./model -run 'TestChannelGroupMonitorCategoryDatabaseCompatibility' -count=1 -v
& 'D:\go-toolchain-1.25.1\go\bin\go.exe' test ./service ./controller -run 'Test(GetChannelGroupMonitorCacheRates|ChannelGroupMonitorCacheRate|GetPricingGroupMonitorCacheRate)' -count=1
& 'D:\go-toolchain-1.25.1\go\bin\go.exe' test ./model ./controller -run 'Test.*(ChannelGroupMonitor|PricingGroupMonitor|GroupMonitor)' -skip TestChannelGroupMonitorCategoryDatabaseCompatibility -count=1
```

上述测试通过。缓存率回归覆盖请求命中口径、24 小时窗口、探测排除、仅返回配置分组、零命中与无样本、Redis 不可用、管理设置恢复与旧客户端兼容，以及公开 API 隐藏开关关闭后的缓存率。

前端在 `web/` 执行以下命令，5 个测试文件、48 个用例通过，类型检查、受影响文件 lint 和生产构建通过。缓存率相关用例覆盖开关恢复、开启和关闭保存、保存失败保留修改、缺省关闭及移动端统计排列。

```powershell
bun run test src/features/group-monitor src/features/channel-monitor/components/__tests__/channel-group-monitor-settings-sheet.test.tsx
bun run typecheck
bunx oxlint -c .oxlintrc.json src/features/group-monitor/types.ts src/features/group-monitor/lib/config-schema.ts src/features/group-monitor/api.ts src/features/group-monitor/index.tsx src/features/group-monitor/__tests__/index.test.tsx src/features/channel-monitor/components/channel-group-monitor-settings-sheet.tsx src/features/channel-monitor/components/__tests__/channel-group-monitor-settings-sheet.test.tsx
bun run build
```

随后在项目根目录执行包含前端资源的 Go 构建，结果通过：

```powershell
& 'D:\go-toolchain-1.25.1\go\bin\go.exe' build -o 'D:\temp\new-api-group-cache-check.exe' .
```

本次涉及的既有文件均已与 `upstream/main` 比较，均为下游扩展文件；没有修改上游所有的文件。工作区中并行进行的其他修改保留。
