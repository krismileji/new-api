# 渠道监控恢复与隔离排查

这次问题包含真实的事件处理故障和不准确的展示。隔离日志是真实记录，但“隔离”不等于整条事件的统计全部丢失，更不等于用户请求失败。旧页面把当前运行状态、历史记录和累计次数混在一起，容易让已经恢复的情况看起来仍需人工处理。

少量正在处理的新事件可以与正常状态同时出现。累计计数不会在恢复后自动清零，也不需要人工清零。

## 本次已确认的问题

2026-09-12 的排查中，隔离累计数从 2,571 增长到 2,604。提供的日志汇总包含 232 条隔离事件，均为“智能调度池级 Redis 软刷新发生配置冲突”，处理失败次数均为 20。一次 30 秒观测没有新增重试或隔离，不能据此排除间歇复发。

抽查的当前快照有 248 条路由，均携带非零 `state_revision`。这排除了该份快照缺失修订号的情况。旧日志没有记录具体冲突字段，因此不能证明线上每次冲突都来自同一字段。

本地进一步复现了无需管理员修改配置就会发生的冲突：内存缓存和 Redis 均启用时，后台池级刷新使用已发布的读快照作为写入依据。第一条事件把数据库路由修订号从 8 推进到 9，缓存尚未发布新版本，第二条正常事件仍带修订号 8 写入，数据库正确拒绝旧结果。经济版本变动也会让缓存中的经济快照反复失效。这是程序把异步展示快照用于后台写入导致的版本竞争。

旧处理路径存在两个问题：

1. 数据库写入保护返回的 `ConflictReason` 被简化成“配置冲突”，缺少渠道、冲突字段和版本值。
2. 配置冲突与坏事件共用失败上限，重试达到 20 次、单条处理仍冲突时，原始监控事件进入隔离队列并被确认。

本次完整修复：

1. 后台刷新从主库读取当前路由状态和经济版本，再计算并执行原有乐观校验；连续正常事件不再等待页面快照发布。页面仍读取已发布快照。
2. 真正并发修改配置时，原事件保留待重试，下一轮读取当前状态重算。配置冲突、上下文超时、网络中断、Redis 连接池等待超时和处理所有权竞争不计入坏消息隔离次数。批处理及最终单条复核都遵守该规则。冲突日志保留 `detail=` 具体原因，每个消费者最多每 30 秒输出一次。
3. 处理函数超时后，使用新的、有时限的上下文释放本次持有的副作用标记；不会继续拿已取消的上下文清理，阻塞下一次重试。释放仍校验持有者，不会删除其他消费者的标记。
4. 仅发生短暂事件积压、成本排队或投影延迟时，后台先自动处理，不发送一对异常/恢复邮件。持续异常达到原有 300 秒人工处理阈值才通知；观测失败、后台停止、Redis 不可用和新增数据丢弃等故障不受此抑制。
5. 仅有隔离历史时提示“存在历史隔离记录”，摘要与详情显示当前运行状态，重试、接管、隔离明确标为累计值。正常的 1 秒处理延迟不标黄。过期的正常观测不能压住新的积压、隔离或关键故障。
6. 已通知过历史隔离后，新增隔离仍会触发告警。发送失败自动退避重试，处理随后恢复也不会吞掉未发送的新增隔离通知。旧通知记录与浏览器已知晓状态兼容升级；普通恢复、进程本地计数归零不会反复发送相同历史通知。

监控处理顺序是路由统计、共享统计投影、调度运行时副作用。因此最后一步失败不证明前两步统计丢失，隔离数量不能等同于业务请求失败数或统计丢失数。

仓库中的 `e12f1a115` 还修正了两类告警误判：成本 outbox 正常领取的首次尝试被算成重试；历史隔离累计值让后续短暂异常都显示为需要人工处理。完整补丁包含这部分已有修复。

持续冲突仍会形成待处理积压，按现有处理延迟和健康检查提示异常；修复不保证所有配置冲突都能自行解除。

## 自动处理与通知约定

健康检查每 10 秒在后台运行，与是否打开页面无关。邮件使用已有渠道监控设置中的通知开关、收件邮箱及“监控健康异常”通知类型；本地回归替换邮件发送器验证流程，没有向真实收件人发送测试邮件。

| 情况 | 系统行为 | 是否需要日常手工处理 |
| --- | --- | --- |
| 正常批次、短暂积压、可恢复的配置冲突 | 保留待处理事件，自动重试；当前状态与累计诊断分开展示 | 不需要 |
| 仅有已经记录的历史隔离 | 保留原始记录与累计次数；已知晓提示保持收起 | 不需要清理或每日复核 |
| 持续 300 秒的积压、关键依赖不可用、后台停止 | 按配置告警；仍有自动恢复路径的任务继续重试 | 收到有效告警后再处理原因 |
| 新增无法处理的隔离事件 | 告警并保留原因；不会被已通知的历史记录遮盖 | 需要针对具体错误修复 |
| 已告警的运行故障恢复 | 连续正常 60 秒后发送一次恢复通知 | 不需要手动确认恢复 |
| 邮件发送失败 | 自动退避重试，页面显示通知错误 | 持续失败时处理邮箱配置或连通性 |

历史通知、故障通知和恢复通知都有发送状态；同一运行故障沿用 15 分钟提醒间隔，避免持续刷屏。

## Docker 日志命令

以下命令在服务器的 Bash 中执行。应用容器名按实际部署替换；多副本分别检查。

```bash
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'
APP=new-api
docker inspect --format 'name={{.Name}} image={{.Config.Image}} image_id={{.Image}} started={{.State.StartedAt}} restarts={{.RestartCount}} oom={{.State.OOMKilled}}' "$APP"
docker stats --no-stream "$APP"

# 最近的隔离原因。结果受最近 24 小时和最后 200000 行日志范围限制。
docker logs --since 24h --tail 200000 --timestamps "$APP" 2>&1 | grep -F '渠道监控 Redis 消息已隔离' | tail -n 30

# 按隔离原因汇总；这是上述日志范围内的记录数，不是全量历史计数。
docker logs --since 24h --tail 200000 "$APP" 2>&1 | grep -F '渠道监控 Redis 消息已隔离' | sed 's/^.* attempts=/attempts=/' | sort | uniq -c | sort -nr | head -n 20

# 部署本次修复后，查看配置冲突的具体字段和值。
docker logs --since 10m --tail 50000 --timestamps "$APP" 2>&1 | grep -E '渠道监控 Redis 事件暂缓处理|渠道监控 Redis 消息已隔离|渠道监控 Redis 消费者中断' | tail -n 50
```

没有匹配输出时，先检查 Docker 日志的覆盖时间。需要更早的记录时，检查部署配置指定的日志目录或日志平台。

## Redis 快照与计数变化

下面脚本使用服务器上的 Python 3、Docker 和 Redis 容器内的 `redis-cli`。它读取应用容器的连接配置，只在本机传给 Redis 客户端，不输出连接串或密码。默认容器名为 `new-api` 和 `new-api-redis`；Redis 客户端使用应用配置中的主机、端口、数据库和 ACL 用户。

```bash
python3 - <<'PY'
import json, os, subprocess, time
from urllib.parse import urlsplit, unquote

app, redis_container = 'new-api', 'new-api-redis'
cfg = json.loads(subprocess.check_output(['docker', 'inspect', app]))[0]
settings = dict(x.split('=', 1) for x in cfg['Config']['Env'] if '=' in x)
u = urlsplit(settings.get('REDIS_CONN_STRING', ''))
if u.scheme not in ('redis', 'rediss'):
    raise SystemExit('未找到标准 Redis 连接配置')
env = dict(os.environ, REDISCLI_AUTH=unquote(u.password or ''))
cmd = ['docker', 'exec', '-e', 'REDISCLI_AUTH', redis_container,
       'redis-cli', '--json', '-h', u.hostname, '-p', str(u.port or 6379),
       '-n', u.path.strip('/') or '0']
if u.username:
    cmd += ['--user', unquote(u.username)]
if u.scheme == 'rediss':
    cmd += ['--tls']

def query(*args):
    return json.loads(subprocess.check_output(cmd + list(args), env=env, timeout=20))

def counters():
    value = query('HGETALL', 'channel_monitor:v1:observability')
    if isinstance(value, list):
        value = dict(zip(value[::2], value[1::2]))
    return {k: int(value.get(k, 0)) for k in [
        'retry_count', 'takeover_count', 'quarantine_count',
        'last_quarantined_at', 'marker_release_failure_count',
        'marker_release_failure_active']}

a = counters()
print('计数 A:', json.dumps(a, ensure_ascii=False), flush=True)
revision = query('GET', 'channel_smart_schedule:v1:route_snapshot:current')
raw = query('GET', 'channel_smart_schedule:v1:route_snapshot:version:' + str(revision) + ':monitor') if revision else None
if raw:
    snapshot = json.loads(raw)
    routes = snapshot.get('Routes', [])
    print('快照检查:', json.dumps({
        'revision': revision, 'routes': len(routes),
        'missing_state_revision': sum('state_revision' not in r for r in routes),
        'zero_state_revision': sum(r.get('state_revision') == 0 for r in routes)
    }, ensure_ascii=False), flush=True)
else:
    print('快照检查: 未找到当前监控快照', flush=True)
time.sleep(30)
b = counters()
print('计数 B:', json.dumps(b, ensure_ascii=False))
print('30 秒增量:', json.dumps({k: b[k]-a[k] for k in [
    'retry_count', 'takeover_count', 'quarantine_count',
    'marker_release_failure_count']}, ensure_ascii=False))
PY
```

## 根据结果处理

| 结果 | 处理 |
| --- | --- |
| 隔离数或最近隔离时间继续增长 | 按隔离日志中的 `error=` 排查，当前仍在新增问题 |
| 新版本仅有暂缓处理日志，随后待处理下降 | 后台已经自动处理并恢复，无需人工重试或清理计数 |
| 配置冲突长期重复，待处理量或最早待处理时间持续增长 | 保留最新 `detail=`，检查对应渠道、配置版本及快照刷新错误；需要继续处理冲突来源 |
| 当前快照缺 `state_revision` | 核对镜像是否包含 `e928dd5a1` 的修订号序列化修复，以及是否混用旧节点 |
| 清理当前状态正常，但累计失败数大于零 | 记录过往失败，结合计数增量判断是否复发 |
| `Sub2API 读取上游余额失败` | 排查对应渠道的余额接口连接与响应；这条日志本身不能解释调度事件的配置冲突 |
| 探测请求约 60 秒后出现 `context deadline exceeded` | 检查探测时限和上游流响应；与事件隔离日志分开定位 |

## 历史隔离记录

本次修复防止新的配置冲突事件因重试次数而被隔离，不会自动重放既有隔离队列。监控事件进入隔离前可能已经完成部分投影，因此隔离条数不能直接换算成用户请求失败数或全部统计丢失数。

先保留隔离记录，再评估事件仍在不在保留范围内、哪些副作用已完成，以及重放去重是否有效。当前去重保护有时间边界，不能直接把全部隔离消息批量塞回事件流，也不能通过清空计数把历史统计标成已补齐。

## 本次部署命令

已确认线上 Compose 项目名和服务名均为 `new-api`，配置文件为 `/www/server/panel/data/compose/new-api/docker-compose.yaml`，当前应用镜像为 `new-api-dev:latest`。以下命令交给管理员执行，本地排查没有连接或更新生产服务器。

### 准备源码

把本轮最新导出的 `monitor-autorecovery-fix.patch` 上传到服务器 `/root/monitor-autorecovery-fix.patch`。进入服务器的 new-api **源码目录**执行以下命令，目录中应有 `go.mod` 和 `Dockerfile`。Compose 目录未必是源码目录。该完整补丁包含事件处理、页面展示、通知修复，以及 `e12f1a115` 的告警误判修复；它替代之前导出的版本，已经应用过部分改动时不要重复叠加。

```bash
# 先进入已有源码目录，再应用补丁。若源码已经包含本次改动，跳过应用步骤。
git apply --check /root/monitor-autorecovery-fix.patch && git apply /root/monitor-autorecovery-fix.patch
git diff --check
```

如补丁检查报错，保留报错继续排查，不要强制覆盖服务器上的源码。

### 构建并更新应用

仍在包含修复的源码目录，整段复制执行。脚本核对 Compose 的目标镜像，只备份和更新应用镜像。构建失败会停止，原应用继续运行。最后重建应用容器时会有短暂服务中断。

```bash
bash <<'SH'
set -euo pipefail
test -f go.mod
test -f Dockerfile
grep -q 'ErrChannelMonitorRedisRetryable' service/channel_monitor_redis_retry.go
grep -q 'recordRetryableFailure' service/channel_monitor_redis_consumer.go
grep -q 'service.ErrChannelMonitorRedisRetryable' controller/channel_ratio_monitor_schedule_runtime.go
grep -q 'GetChannelSmartScheduleRoutePoolForRefresh' controller/channel_ratio_monitor_schedule_runtime.go

COMPOSE_FILE=/www/server/panel/data/compose/new-api/docker-compose.yaml
TARGET_IMAGE=$(docker compose -p new-api -f "$COMPOSE_FILE" config --format json | python3 -c 'import json,sys; print(json.load(sys.stdin)["services"]["new-api"].get("image", ""))')
if [ "$TARGET_IMAGE" != 'new-api-dev:latest' ]; then
    printf 'Compose 镜像与预期不符：%s，请保留输出继续排查。\n' "$TARGET_IMAGE" >&2
    exit 1
fi

OLD_IMAGE_ID=$(docker inspect --format '{{.Image}}' new-api)
BACKUP_IMAGE="new-api-dev:before-monitor-conflict-$(date -u +%Y%m%dT%H%M%SZ)"
docker image tag "$OLD_IMAGE_ID" "$BACKUP_IMAGE"
printf '回退镜像：%s\n' "$BACKUP_IMAGE"

docker build -t new-api-dev:latest .
docker compose -p new-api -f "$COMPOSE_FILE" up -d --no-deps --no-build --pull never new-api
docker inspect --format 'image_id={{.Image}} started={{.State.StartedAt}} status={{.State.Status}}' new-api
SH
```

### 验证

```bash
docker ps --filter name='^/new-api$' --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'
docker logs --since 10m --tail 50000 --timestamps new-api 2>&1 | grep -E '渠道监控 Redis 事件暂缓处理|渠道监控 Redis 消息已隔离|渠道监控 Redis 消费者中断' | tail -n 50
```

同时重新执行上面的 Redis 计数脚本，并在页面查看处理延迟与最早待处理时间。成功标准是新日志中配置冲突不再进入隔离，积压能继续消化；累计隔离数不要求归零。如果冲突持续，把包含 `detail=` 的暂缓处理日志贴回，它会显示具体渠道、字段及新旧值。

### 必要时回退应用

把 `BACKUP_IMAGE` 改成构建步骤打印的完整回退镜像名，再执行：

```bash
BACKUP_IMAGE='new-api-dev:before-monitor-conflict-替换为实际时间'
docker image inspect "$BACKUP_IMAGE" >/dev/null && docker image tag "$BACKUP_IMAGE" new-api-dev:latest && docker compose -p new-api -f /www/server/panel/data/compose/new-api/docker-compose.yaml up -d --no-deps --no-build --pull never new-api
```

## 本地验证记录

本次新增后台刷新使用的数据库读取入口，复用原有 SQL 查询和事务；没有修改模型字段、迁移、数据库驱动或账本规则。原有 SQL 乐观校验和事务回滚保持生效。读取的是主库调度配置，不涉及独立日志库。

- 先用回归复现配置冲突越过隔离上限后被隔离，修复后保留 pending，解除冲突后正常确认。
- 覆盖批处理阶段和最终单条隔离复核阶段；验证其他分区的正常消息不重复处理，原有坏消息隔离测试通过。
- 真实 SQLite 3.50.4、MySQL 8.4.10、PostgreSQL 16.15 验证通过：冲突保留原路由状态和事件水位，重试成功后推进水位，重复事件保持幂等。
- 三种数据库均覆盖：页面快照未发布时连续正常事件成功处理；数据库经济版本已变、页面仍显示旧快照时后台使用当前经济版本成功写入。
- Redis 8.8.0 验证通知状态持久化、并发通知租约、自动恢复及通知回归；事件消费隔离回归使用 miniredis。
- 新增确定性通知测试先复现短暂积压错误发信，再确认自动恢复无需邮件、300 秒持续故障仍告警、关键故障不被延迟。
- 复现并修正连接池超时被当作坏事件隔离、处理超时后标记未释放、新增隔离漏报以及升级后重复发送历史通知；Redis 断开再连上不会把旧隔离重新当成新增。
- 页面回归覆盖摘要与详情一致、历史计数不覆盖正常状态、正常短暂延迟不标黄、新增故障覆盖旧正常观测、已知晓状态升级兼容和再次恢复不要求重复确认。
- `bun run test src/features/channel-monitor --reporter=dot`：当前工作区 95 个测试文件、528 项通过；涉及页面的类型检查、lint、格式检查和生产构建通过。测试集中既有配置面板用例输出 React `act(...)` 环境警告，但没有失败断言。
- MySQL/PostgreSQL 使用专用一次性本地测试容器和空数据库；不涉及生产数据或线上操作。
- Go 1.26.5 执行消费与恢复回归、三库回归以及 `go build ./...`，均通过。改动仅涉及下游文件，未修改上游所有的文件。

```powershell
$env:MONITOR_CONFLICT_MYSQL_DSN='root:monitor_test_only@tcp(127.0.0.1:23316)/new_api_monitor_conflict_test?parseTime=true'
$env:MONITOR_CONFLICT_POSTGRES_DSN='postgres://postgres:monitor_test_only@127.0.0.1:25442/new_api_monitor_conflict_test?sslmode=disable'
& 'D:\Go\sdk\go1.26.5\bin\go.exe' test ./controller -run '^TestRedisAdaptiveRefreshConfigurationConflictDatabaseMatrix$' -count=1 -v
& 'D:\Go\sdk\go1.26.5\bin\go.exe' test ./controller -run '^(TestAdaptiveRefresh|TestRuntimeRefresh|TestRedisAdaptiveRefresh|TestRedisRuntime|TestFullSchedulePreservesRuntimeOverlay)' -count=1
$env:TEST_MONITOR_REDIS_ADDR='127.0.0.1:26389'
$env:TEST_MYSQL_DSN=$env:MONITOR_CONFLICT_MYSQL_DSN
$env:TEST_POSTGRES_DSN=$env:MONITOR_CONFLICT_POSTGRES_DSN
& 'D:\Go\sdk\go1.26.5\bin\go.exe' test ./service -run '^TestChannelMonitorHealthObservationDatabaseMatrix$' -count=1 -v
& 'D:\Go\sdk\go1.26.5\bin\go.exe' test ./service -run '^(TestChannelMonitorRecovery|TestChannelMonitorRedis|TestBuildChannelMonitorRecovery)' -count=1
& 'D:\Go\sdk\go1.26.5\bin\go.exe' test ./model -run '^(TestChannelSmartScheduleMonitorUsesPublished|TestChannelSmartScheduleMonitorPublished|TestChannelSmartScheduleRecoveryConflict|TestChannelMonitorEconomicRevision|TestApplyChannelSmartScheduleRouteResults)' -count=1
& 'D:\Go\sdk\go1.26.5\bin\go.exe' build ./...
```

前端验证命令在 `web` 目录执行：

```powershell
bun run test src/features/channel-monitor --reporter=dot
bun run typecheck
bun run build
bun x --no-install oxlint -c .oxlintrc.json src/features/channel-monitor/lib/runtime-status.ts src/features/channel-monitor/types-recovery.ts src/features/channel-monitor/components/channel-monitor-history-notice.tsx src/features/channel-monitor/components/channel-monitor-realtime-status.tsx src/features/channel-monitor/components/channel-monitor-runtime-details.tsx src/features/channel-monitor/components/__tests__/runtime-contract.test.tsx src/features/channel-monitor/components/__tests__/history-acknowledgment.test.tsx src/features/channel-monitor/components/__tests__/realtime-status.test.tsx
bun x --no-install oxfmt --check src/features/channel-monitor/lib/runtime-status.ts src/features/channel-monitor/types-recovery.ts src/features/channel-monitor/components/channel-monitor-history-notice.tsx src/features/channel-monitor/components/channel-monitor-realtime-status.tsx src/features/channel-monitor/components/channel-monitor-runtime-details.tsx src/features/channel-monitor/components/__tests__/runtime-contract.test.tsx src/features/channel-monitor/components/__tests__/history-acknowledgment.test.tsx src/features/channel-monitor/components/__tests__/realtime-status.test.tsx
```
