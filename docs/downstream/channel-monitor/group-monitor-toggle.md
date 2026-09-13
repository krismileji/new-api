# 分组监控独立开关

在「配置分组监控 → 分类与分组」中，每个分组都有「启用监控」开关，点击「保存配置」后生效。

- 关闭单个分组后，保留探测模型、展示字、分类、排序和历史记录；定时探测和「立即探测」都会跳过它。
- 重新启用并保存后，该分组恢复参与探测。
- 全部分组关闭时，不再安排定时探测，清除排队中的手动任务，并禁用「立即探测」。
- 全局「启用周期探测」仍只控制定时执行；全局关闭时，可以手动探测已启用的分组。
- 已发送的请求可能完成并留下结果；开关不会删除已有记录。

分组配置新增可选 `enabled` 字段，保存在原有 `groups_json` TEXT 字段内。旧配置省略该字段时默认启用；显式 `false` 会保留。没有新增数据库列或迁移，也没有修改日志库的读写行为。

管理端和用户端接口使用已有 `paused` 状态标记被关闭的分组，即使该分组的探测模型暂不可用，也保持暂停状态。其他分组的监控状态不受影响。

## 验证记录

2026-09-13，Windows，Go 1.26.5。

| 数据库 | 实测版本 | 结果 |
| --- | --- | --- |
| SQLite | 3.50.4 | 通过 |
| MySQL | 5.7.44 | 通过 |
| PostgreSQL | 9.6.24 | 通过 |

`TestChannelGroupMonitorEnabledDatabaseCompatibility` 在三个真实数据库上验证：初始全暂停配置、JSON 保存和重新读取、重复执行配置表 AutoMigrate 两次、定时与手动任务过滤、暂停分组不产生补记超时、全部暂停后清除排队任务、重新启用后的调度，以及配置和历史记录保留。旧数组和对象格式的默认启用行为另有回归测试。

使用独立临时容器和空测试数据库执行，测试结束后移除本次创建的容器及数据卷：

```powershell
docker run --detach --name new-api-group-enabled-mysql-0913 --publish 127.0.0.1:13379:3306 --env MYSQL_ROOT_PASSWORD=group-monitor-test --env MYSQL_DATABASE=new_api_group_enabled_test mysql:5.7.44 --character-set-server=utf8mb4 --collation-server=utf8mb4_unicode_ci
docker run --detach --name new-api-group-enabled-postgres-0913 --publish 127.0.0.1:15479:5432 --env POSTGRES_PASSWORD=group-monitor-test --env POSTGRES_DB=new_api_group_enabled_test postgres:9.6

docker exec new-api-group-enabled-mysql-0913 mysqladmin ping -uroot -pgroup-monitor-test
docker exec new-api-group-enabled-postgres-0913 pg_isready -U postgres -d new_api_group_enabled_test

$env:GROUP_MONITOR_ENABLED_MYSQL_DSN='root:group-monitor-test@tcp(127.0.0.1:13379)/new_api_group_enabled_test?charset=utf8mb4&parseTime=True&loc=Local'
$env:GROUP_MONITOR_ENABLED_POSTGRES_DSN='postgres://postgres:group-monitor-test@127.0.0.1:15479/new_api_group_enabled_test?sslmode=disable'
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./model ./controller -run GroupMonitor -count=1 -timeout=5m -v
```

后端相关测试通过；前端保存、重新加载、键盘切换、失败重试和全部暂停场景通过。前端执行命令（从 `web/` 运行）：

```powershell
bun run test src/features/channel-monitor/components/__tests__/channel-group-monitor-settings-sheet.test.tsx src/features/group-monitor
bun run typecheck
bunx --no-install oxlint -c .oxlintrc.json src/features/group-monitor/types.ts src/features/group-monitor/lib/config-schema.ts src/features/channel-monitor/components/channel-group-monitor-category-editor.tsx src/features/channel-monitor/components/channel-group-monitor-settings-sheet.tsx src/features/channel-monitor/components/__tests__/channel-group-monitor-settings-sheet.test.tsx
bun run build
```

本次修改的既有文件均为下游所有；按本地 `upstream/main` 基线检查，没有修改上游所有的文件。工作区中同时存在其他功能的未提交改动，本记录仅涵盖分组独立启停。
