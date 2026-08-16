# 渠道监控 Redis 清理与命名 Runbook

本文只用于渠道监控 Redis 实时链路的停机清理和 key 所有权核验。当前版本契约来自 `service/channel_monitor_redis_keys.go`，所有可清理对象必须位于精确前缀 `channel_monitor:v1:` 下，并且还必须命中本文的类别白名单。

本文不包含 MySQL 清理。禁止执行 `FLUSHDB`、`FLUSHALL`、生产 `KEYS *`、未限定前缀的删除，以及对其他业务 key 的重命名或过期时间修改。

## 1. 执行条件

执行前必须同时满足：

1. 已停止所有连接该 Redis 数据库的应用实例、渠道监控 worker 和滚动发布中的旧实例。
2. 已确认不会在清理过程中自动拉起应用；应用停止后，`channel_monitor:v1:consumer:heartbeat` 不应继续刷新。
3. 已从本次部署实际使用的 `REDIS_CONN_STRING` 核对 Redis 主机、端口、TLS、用户名和逻辑库号。URI 未显式写库号时按数据库 `0` 处理。
4. 已确认 Redis 服务版本至少为 6.2，并且当前连接具有 `PING`、`INFO`、`CLIENT INFO`、`DBSIZE`、`SCAN`、`TYPE`、`XLEN`、`XINFO`、`XGROUP`、`UNLINK`、`EXISTS` 和 `HGETALL` 权限；若使用同步删除后备方案，还需要 `DEL` 权限。
5. 已记录清理前的连接目标、库号、`DBSIZE`、目标 key 数、Stream 长度和消费组状态。不要把 Redis 密码或 key value 写入操作记录。

如果 Redis 与其他业务共用实例或逻辑库，仍然只能按本 Runbook 的精确白名单删除；不能因为已经停掉渠道监控应用而清空整个库。

## 2. 命名与所有权

所有权边界是带结尾冒号的 `channel_monitor:v1:`。`channel_monitor:v1`、`channel_monitor:v10:*`、其他版本和其他业务前缀都不属于本次清理范围。

| 用途 | 名称或模式 | Redis 类型/说明 |
| --- | --- | --- |
| 原始事件 Stream | `channel_monitor:v1:events` | Stream |
| 消费组 | `channel_monitor:v1:aggregators` | Stream 内部元数据，不是独立 key |
| 消费者名称 | `channel_monitor:v1:consumer:<identity>` | 消费组内部名称，不是独立 key；随消费组或 Stream 删除 |
| 聚合器租约 | `channel_monitor:v1:aggregator:lease` | 临时租约 key |
| 消费者心跳 | `channel_monitor:v1:consumer:heartbeat` | 临时心跳 key |
| 链路观测 | `channel_monitor:v1:observability` | 处理水位、重试和接管计数 |
| 路由健康投影 | `channel_monitor:v1:projection:route:*` | 路由窗口及 `health:index` |
| 看板投影 | `channel_monitor:v1:projection:dashboard:*` | 分钟紧凑投影 |
| 成本投影 | `channel_monitor:v1:projection:cost:*` | 当日成本及结算状态 |
| 消费幂等标记 | `channel_monitor:v1:projection:dedup:*` | `event_id` 消费标记 |
| 共享投影幂等标记 | `channel_monitor:v1:projection:shared:event:*` | 看板/成本投影提交标记 |
| 运行时副作用标记 | `channel_monitor:v1:projection:runtime:event:*` | 保护和刷新副作用标记 |
| 完整调度幂等标记 | `channel_monitor:v1:projection:schedule:event:*` | 完整调度成功入队标记 |

动态名称片段只保留 ASCII 字母、数字、点、短横线和下划线；其他字节编码为 `~XX`。新增不兼容的 Redis 数据结构必须使用新版本前缀，例如 `channel_monitor:v2:`，不能复用 `v1` 并改变旧 key 的含义。

当前代码中的 `ChannelMonitorRedisConsumerPrefix` 用于构造消费组内的消费者名称，不代表可以删除任意 `channel_monitor:v1:consumer:*` key。现有可删除的消费者相关 key 只有精确名称 `channel_monitor:v1:consumer:heartbeat`。

## 3. 清理前只读核验

以下命令以 PowerShell 和 `redis-cli` 为例。必须使用应用实际的连接串，不要手工省略 URI 中的库号。

```powershell
$ErrorActionPreference = 'Stop'

$redisUrl = $env:REDIS_CONN_STRING
if ([string]::IsNullOrWhiteSpace($redisUrl)) {
    throw 'REDIS_CONN_STRING 未设置'
}

redis-cli -u $redisUrl --raw PING
redis-cli -u $redisUrl --raw INFO server | Select-String '^redis_version:'
redis-cli -u $redisUrl --raw CLIENT INFO
redis-cli -u $redisUrl --raw DBSIZE
```

`CLIENT INFO` 中的 `db=` 必须与部署连接串一致。主机、端口、TLS、用户或库号有任一项不确定时立即停止，不进入删除步骤。

只用 `SCAN` 获取目标 key，不使用 `KEYS`：

```powershell
$exactPrefix = 'channel_monitor:v1:'
$targetKeys = @(
    redis-cli -u $redisUrl --raw --scan --pattern "$exactPrefix*" |
        Where-Object { -not [string]::IsNullOrWhiteSpace($_) } |
        Sort-Object -Unique
)

$targetKeys.Count
$targetKeys
```

输出只应包含第 2 节列出的名称和模式。不要读取或导出 value。再核对 Stream 和消费组：

```powershell
$streamKey = 'channel_monitor:v1:events'
$streamType = (redis-cli -u $redisUrl --raw TYPE $streamKey).Trim()
if ($streamType -notin @('none', 'stream')) {
    throw "目标 Stream key 类型异常：$streamType"
}

if ($streamType -eq 'stream') {
    redis-cli -u $redisUrl --raw XLEN $streamKey
    redis-cli -u $redisUrl --raw XINFO GROUPS $streamKey
}
```

清理前应保存以下只读结果：

- `CLIENT INFO` 中的连接地址和 `db=`，但不保存认证信息。
- `DBSIZE`。
- `channel_monitor:v1:*` 的去重 key 数和类别计数。
- Stream 的 `XLEN`，以及消费组 `channel_monitor:v1:aggregators` 的 pending、消费者数和最后投递信息。
- 应用停止后再次观察心跳 TTL；若仍在续期，说明至少还有一个实例未停止。

## 4. 白名单清理脚本

下面的脚本会重新 `SCAN`，逐个执行精确前缀和类别白名单校验。只要发现一个未知 key，就在任何删除发生前终止。脚本默认使用非阻塞的 `UNLINK`，每批最多 200 个 key；Redis 6.2 原生支持该命令。

执行前必须先完成第 3 节并人工确认清单。运行时需要输入完整确认文本 `DELETE channel_monitor:v1:`。

```powershell
$ErrorActionPreference = 'Stop'

$redisUrl = $env:REDIS_CONN_STRING
if ([string]::IsNullOrWhiteSpace($redisUrl)) {
    throw 'REDIS_CONN_STRING 未设置'
}

$exactPrefix = 'channel_monitor:v1:'
$streamKey = 'channel_monitor:v1:events'
$consumerGroup = 'channel_monitor:v1:aggregators'

$allowedExact = [System.Collections.Generic.HashSet[string]]::new(
    [System.StringComparer]::Ordinal
)
@(
    $streamKey,
    'channel_monitor:v1:aggregator:lease',
    'channel_monitor:v1:consumer:heartbeat',
    'channel_monitor:v1:observability'
) | ForEach-Object { [void]$allowedExact.Add($_) }

$allowedPrefixes = @(
    'channel_monitor:v1:projection:route:',
    'channel_monitor:v1:projection:dashboard:',
    'channel_monitor:v1:projection:cost:',
    'channel_monitor:v1:projection:dedup:',
    'channel_monitor:v1:projection:shared:event:',
    'channel_monitor:v1:projection:runtime:event:',
    'channel_monitor:v1:projection:schedule:event:'
)

$targetKeys = @(
    redis-cli -u $redisUrl --raw --scan --pattern "$exactPrefix*" |
        Where-Object { -not [string]::IsNullOrWhiteSpace($_) } |
        Sort-Object -Unique
)
if ($LASTEXITCODE -ne 0) {
    throw 'SCAN 失败，未执行删除'
}

$unexpectedKeys = [System.Collections.Generic.List[string]]::new()
foreach ($key in $targetKeys) {
    if (-not $key.StartsWith($exactPrefix, [System.StringComparison]::Ordinal)) {
        $unexpectedKeys.Add($key)
        continue
    }

    $isAllowed = $allowedExact.Contains($key)
    if (-not $isAllowed) {
        foreach ($allowedPrefix in $allowedPrefixes) {
            if ($key.StartsWith($allowedPrefix, [System.StringComparison]::Ordinal)) {
                $isAllowed = $true
                break
            }
        }
    }
    if (-not $isAllowed) {
        $unexpectedKeys.Add($key)
    }
}

if ($unexpectedKeys.Count -gt 0) {
    $unexpectedKeys | ForEach-Object { Write-Error "未知渠道监控 key：$_" }
    throw '发现白名单外 key，未执行任何删除；先核对代码所有权并更新 Runbook'
}

$dbSizeBefore = [int64](redis-cli -u $redisUrl --raw DBSIZE)
if ($LASTEXITCODE -ne 0) {
    throw 'DBSIZE 失败，未执行删除'
}

Write-Host "逻辑库清理前 key 总数：$dbSizeBefore"
Write-Host "本次白名单目标 key 数：$($targetKeys.Count)"
$confirmation = Read-Host '输入 DELETE channel_monitor:v1: 继续'
if ($confirmation -cne 'DELETE channel_monitor:v1:') {
    throw '确认文本不匹配，未执行删除'
}

$streamType = (redis-cli -u $redisUrl --raw TYPE $streamKey).Trim()
if ($LASTEXITCODE -ne 0 -or $streamType -notin @('none', 'stream')) {
    throw "目标 Stream key 类型异常：$streamType"
}
if ($streamType -eq 'stream') {
    redis-cli -u $redisUrl --raw XGROUP DESTROY $streamKey $consumerGroup
    if ($LASTEXITCODE -ne 0) {
        throw '消费组删除失败；应用保持停止，修复后重跑本脚本'
    }
}

$batchSize = 200
for ($offset = 0; $offset -lt $targetKeys.Count; $offset += $batchSize) {
    $lastIndex = [Math]::Min($offset + $batchSize - 1, $targetKeys.Count - 1)
    $batch = [string[]]$targetKeys[$offset..$lastIndex]
    redis-cli -u $redisUrl --raw UNLINK @batch
    if ($LASTEXITCODE -ne 0) {
        throw 'UNLINK 失败；应用保持停止，修复后重跑本脚本'
    }
}

Write-Host '白名单删除命令已完成，继续执行清理后核验'
```

如果受控环境明确禁用了 `UNLINK`，只能在确认 Redis 负载允许同步删除后，把脚本中的 `UNLINK` 原样替换为 `DEL`。仍须保留同一 `SCAN`、精确前缀、类别白名单、批次和人工确认流程，不能改成通配符删除。

脚本可幂等重跑：第二次执行时消费组或 Stream 可以不存在，`SCAN` 可以返回空集合，删除循环会跳过。若在某个批次中断，保持应用停止，修复连接或权限后直接重跑；不得临时扩大匹配范围。

## 5. 清理后只读核验

使用同一连接串和同一逻辑库执行：

```powershell
$remainingKeys = @(
    redis-cli -u $redisUrl --raw --scan --pattern 'channel_monitor:v1:*' |
        Where-Object { -not [string]::IsNullOrWhiteSpace($_) } |
        Sort-Object -Unique
)
if ($remainingKeys.Count -ne 0) {
    $remainingKeys
    throw '渠道监控 v1 key 未清理完整，应用保持停止'
}

redis-cli -u $redisUrl --raw TYPE channel_monitor:v1:events
$dbSizeAfter = [int64](redis-cli -u $redisUrl --raw DBSIZE)
Write-Host "逻辑库清理后 key 总数：$dbSizeAfter"
```

验收结果必须满足：

- `SCAN MATCH channel_monitor:v1:*` 返回 0 个 key。
- `TYPE channel_monitor:v1:events` 返回 `none`；消费组随 Stream 一并不存在。
- 清理脚本再次运行不报错且仍然删除 0 个 key。
- 删除参数全部来自已经通过白名单校验的 `targetKeys`，没有向 `UNLINK`/`DEL` 传入其他业务 key。
- 若清理期间该逻辑库没有其他写入，`DBSIZE` 的减少量应等于清理前目标 key 数；若共享库仍有其他业务写入，只能把 `DBSIZE` 作为辅助记录，不能据此扩大删除范围。

## 6. 启动后核验

清理完成后再启动新版本应用。应用启动会重新创建 `channel_monitor:v1:events` 和消费组 `channel_monitor:v1:aggregators`，不会从本地队列降级。

启动后只读检查：

```powershell
redis-cli -u $redisUrl --raw TYPE channel_monitor:v1:events
redis-cli -u $redisUrl --raw XINFO GROUPS channel_monitor:v1:events
redis-cli -u $redisUrl --raw EXISTS channel_monitor:v1:consumer:heartbeat
redis-cli -u $redisUrl --raw HGETALL channel_monitor:v1:observability
```

应确认 Stream 类型为 `stream`、消费组名称为 `channel_monitor:v1:aggregators`、消费者心跳恢复，并在产生新监控事件后重新生成路由健康、看板和成本投影。旧 Stream、pending、幂等标记和共享投影不做恢复、回填、双读或双写。

## 7. 中止条件

出现以下任一情况立即中止，应用保持停止：

- 连接目标、TLS、用户名或逻辑库号与部署配置不一致。
- 应用停止后心跳仍持续续期。
- `SCAN` 发现不在第 2 节白名单内的 `channel_monitor:v1:*` key。
- `channel_monitor:v1:events` 存在但类型不是 `stream`。
- 消费组或批量删除命令返回权限、连接或服务端错误。
- 清理后仍有 `channel_monitor:v1:*` key，或者发现删除参数包含其他业务前缀。

不得用 `FLUSHDB`、`FLUSHALL`、`KEYS *`、更宽的 `channel_monitor:*` 或 `channel_monitor:v*` 匹配来绕过这些中止条件。
