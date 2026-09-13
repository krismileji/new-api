# 智能调度重复版本冲突修复

## 问题与证据

2026-09-13 的线上诊断对应源码提交 `e1dc23f515473dd29d1e313a7914fe8286cab541`。最近 20 个任务中有 19 个失败，均由同一 runner 执行；多次连续报同一个渠道的同一组版本差异，例如渠道 62 的快照 138、当前 140。执行间隔约 1 秒。错误中的 53 条是未能应用本轮结果的路由数，失败池继续使用上一轮路由结果，不等于 53 个上游渠道宕机。

当前容器采样显示正常运行、重启计数为 0，CPU 和内存占用较低。这份采样不支持把持续失败解释为反复重启或资源耗尽。渠道 22 的 `gpt-5.6-terra` 探测 503 是另一个上游错误，不能解释其他模型池的版本冲突。

Redis 样本也有正常推进：指针从 302691 更新到 302694。数据库与 Redis 样本不是同一时刻采集，不能据此声称 Redis 永久不更新或量化二者实时差距。

## 根因

1. 全量调度使用 `GetChannelSmartScheduleRoutes()` 和 `GetChannelSmartScheduleEconomicSnapshot()`。启用 Redis 和内存缓存时，这两个入口读取已发布的展示快照。
2. 池级运行时刷新、配置更新和调度状态写入可以先推进数据库版本。展示快照发布是异步的，因此旧快照中的版本不能作为下一轮计算的当前输入。
3. 数据库的乐观校验正确地拒绝旧版本，整个冲突池回滚，保留上一轮生效结果。
4. 旧代码遇到冲突立即安排下一次全量任务。下一次又读相同的旧快照，形成连续失败。只有状态版本改变、优先级和权重未变时，部分路径还不会刷新缓存，加重快照滞后。

历史中 `9f49b8e54` 把通用读取入口接入共享快照；`38b3a38b8` 修复了池级运行时刷新的读取来源，但全量调度仍使用旧入口。这说明部署可能带入或继续携带该代码缺陷，不能仅凭当前容器启动时间认定是哪一次部署首次触发。

## 修复行为

- 全量调度直接读取数据库当前路由和经济参数，展示接口仍使用发布快照。
- 每次接受状态写入后刷新缓存，包含优先级和权重未改变的情况。
- 真正发生并发配置冲突时仍整池回滚，随后重新读取输入计算。自动重试间隔依次约为 5、10、20、40、60 秒，后续封顶 60 秒；任务 payload 持久化次数和期限，继续使用现有任务合并及租约机制。
- 新配置与尚未领取的重试任务合并时清除退避期限。已经领取并等待的任务会在当前期限到达后读取最新输入；其运行耗时包含等待，最多额外等待本轮 60 秒。其他系统任务类型有独立执行 goroutine，不受这个等待阻塞。
- Redis 发布失败区分租约读取失败、租约变化、源水位变化和临时快照丢失，保留原有版本与事务保护。

没有修改模型结构、迁移、数据库驱动、依赖版本或独立日志库路径。所有改动文件均属于 downstream；未修改 upstream-owned 文件。

## 验证记录

基于同一个线上源码提交建立独立 worktree，先复现旧代码失败：数据库已经推进到版本 11，发布快照仍为 8，全量调度报“路由状态版本已变化（快照 8，当前 11）”。修复后同场景连续两轮成功，经济参数版本变化也能读取到当前值。

| 项目 | 实际环境 | 结果 |
| --- | --- | --- |
| 当前输入读取、经济版本、整池冲突与状态写入 | SQLite 3.50.4 | 通过 |
| 相同数据库回归 | MySQL 5.7.44，utf8mb4 | 通过 |
| 相同数据库回归 | PostgreSQL 9.6.24 | 通过 |
| 快照发布与版本保护 | Redis 8.8.0，独立空实例 | 通过 |
| 自动重试持久化、整池保留、恢复、新输入合并、取消 | controller 专项测试 | 通过 |
| 调度 model 测试及系统任务 service 测试 | Go 1.26.5 | 通过 |
| 后端全部包构建 | Windows，Go 1.26.5 | 通过 |
| 生产目标编译 | Linux amd64、CGO_ENABLED=0、GOWORK=off、GOEXPERIMENT=greenteagc | 通过 |

后端编译使用已有 `web/dist` 满足 embed，不交付该测试二进制；线上 Docker 构建会从服务器源码重新构建前端。本次没有修改前端。

扩大 controller 回归时，以下 5 项测试失败；在未修改的 `e1dc23f515473dd29d1e313a7914fe8286cab541` 上使用相同命令，失败项完全相同。本补丁没有新增失败，但不宣称完整 controller 测试全部通过，也不把这些旧失败的修复混入本次变更：

- `TestProtectChannelSmartScheduleRuntimeFailureIgnoresMinimumSamples`
- `TestProtectChannelSmartScheduleRuntimeFailureDoesNotRecountPersistedErrors`
- `TestRunChannelSmartSchedulePersistsExecutionTimeScoreDetails`
- `TestPlanChannelSmartScheduleUsesHysteresisAndForceReset`
- `TestRunChannelSmartScheduleManualPrimaryAllowsStabilityDegrade`

### 实际验证命令

测试只连接本地临时容器，测试凭据无生产用途。MySQL 初次创建数据库默认字符集不支持中文，先将这个临时数据库改为 utf8mb4，再通过全部回归。

```powershell
docker run --rm -d --name new-api-schedule-fix-mysql -p 127.0.0.1:23371:3306 -e MYSQL_ROOT_PASSWORD=schedule_fix_test_only -e MYSQL_DATABASE=new_api_monitor_conflict_test mysql:5.7.44
docker run --rm -d --name new-api-schedule-fix-postgres -p 127.0.0.1:25471:5432 -e POSTGRES_PASSWORD=schedule_fix_test_only -e POSTGRES_DB=new_api_monitor_conflict_test postgres:9.6
docker run --rm -d --name new-api-schedule-fix-redis -p 127.0.0.1:26371:6379 redis:latest
docker exec -e MYSQL_PWD=schedule_fix_test_only new-api-schedule-fix-mysql mysql -uroot -e 'ALTER DATABASE new_api_monitor_conflict_test CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci'

$env:MONITOR_CONFLICT_MYSQL_DSN='root:schedule_fix_test_only@tcp(127.0.0.1:23371)/new_api_monitor_conflict_test?parseTime=true&charset=utf8mb4'
$env:MONITOR_CONFLICT_POSTGRES_DSN='postgres://postgres:schedule_fix_test_only@127.0.0.1:25471/new_api_monitor_conflict_test?sslmode=disable'
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./controller -run '^TestRedisAdaptiveRefreshConfigurationConflictDatabaseMatrix$' -count=1 -v
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./controller -run '^TestChannelSmartScheduleConflictRetry' -count=1

$env:SCHEDULE_RETRY_REDIS_ADDR='127.0.0.1:26371'
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./model -run 'SmartSchedule' -count=1
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./service -run 'SystemTask' -count=1
& 'D:/Go/sdk/go1.26.5/bin/go.exe' build ./...
```

扩大回归及原始提交对照分别执行：

```powershell
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./controller -run 'SmartSchedule|AdaptiveRefresh' -count=1
```

Linux 目标编译在单独进程执行：

```powershell
$env:GOOS='linux'
$env:GOARCH='amd64'
$env:CGO_ENABLED='0'
$env:GOWORK='off'
$env:GOEXPERIMENT='greenteagc'
& 'D:/Go/sdk/go1.26.5/bin/go.exe' build -o schedule-fix-linux-amd64 .
```

## 上线与回退

本地未连接或更新生产服务器。交付 `smart-schedule-conflict-fix.patch` 和 `deploy-smart-schedule-conflict-fix.sh`，适用于已确认的源码基线、镜像 `new-api-dev:latest`、Compose 项目及服务 `new-api`。

将两个文件放到服务器 `/root/`，执行：

```bash
bash /root/deploy-smart-schedule-conflict-fix.sh /root/smart-schedule-conflict-fix.patch
```

脚本核对源码及 Compose 镜像，检查并应用补丁，备份当前运行镜像，构建成功后重建应用容器。应用容器切换会短暂中断服务。补丁检查或构建失败时停止，原应用继续运行；不操作 MySQL、Redis 数据或容器。

上线后在执行记录查看新批次。旧失败记录会保留；确认新任务不再连续重复同一对快照/当前版本，新的调度结果可以应用。真正发生配置并发修改时允许出现冲突记录，应在重新计算后恢复，不能持续每秒空转。

可在 MySQL 对主库执行下面的只读查询，按 UTC 查看最近 10 分钟的任务与重试期限：

```sql
SET SESSION time_zone = '+00:00';
SELECT id, task_id, FROM_UNIXTIME(created_at) AS created_utc, status,
       JSON_UNQUOTE(JSON_EXTRACT(result, '$.failed')) AS failed_routes,
       JSON_UNQUOTE(JSON_EXTRACT(payload, '$.conflict_retry_attempt')) AS retry_attempt,
       JSON_UNQUOTE(JSON_EXTRACT(payload, '$.retry_not_before')) AS retry_not_before,
       error
FROM `new-api`.system_tasks
WHERE type = 'channel_smart_schedule'
  AND created_at >= UNIX_TIMESTAMP() - 600
ORDER BY id DESC
LIMIT 30;
```

如需回退，将构建时打印的完整镜像名填入变量后执行。没有数据库迁移需要回退；源码补丁仍保留，回退只切换运行镜像。

```bash
BACKUP_IMAGE='填入脚本打印的完整回退镜像名'
docker image inspect "$BACKUP_IMAGE" >/dev/null &&
docker image tag "$BACKUP_IMAGE" new-api-dev:latest &&
docker compose -p new-api -f /www/server/panel/data/compose/new-api/docker-compose.yaml up -d --no-deps --no-build --pull never new-api
```
