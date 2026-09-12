# 渠道监控延迟告警排查与修复

## 邮件证据

用户提供的三封邮件来自节点 `db00065a07b5`，时间均为 2026-09-12、UTC+08:00：

| 时间 | 内容 |
| --- | --- |
| 23:33:43 | 统计更新有延迟；需要人工处理 |
| 23:38:53 | 成本记录保存延迟；需要人工处理 |
| 23:40:13 | 监控运行已恢复，部分历史统计仍不完整 |

成本告警与恢复邮件相隔 80 秒。当前实现每 10 秒检查健康状态，相关链路连续正常 60 秒后确认恢复，因此这个间隔符合短暂异常后确认恢复的节奏。邮件本身不能证明线上成本写入曾失败，也不能证明所有统计已经补齐。

统计事件延迟通常在最早待处理事件达到 30 秒，或积压持续无进展达到 30 秒时触发。成本账本默认每分钟处理一轮；待处理记录出现失败或租约超时，或最早记录超过 120 秒时报告成本保存延迟。

## 已复现并修正

1. 成本 outbox 领取记录时，在真正写账本前就递增 `attempt_count`。原统计直接将它计为重试，健康检查碰到正常首次处理也会告警。现在从有效租约中的尝试次数扣除正在进行的这一次；已失败、租约过期、正在重试且曾失败的记录仍然可见。超过 120 秒的记录不会因为租约有效而被当作正常。
2. 原恢复判定只要看到累计隔离数或成本异常队列数大于零，就把后续任何短暂异常标为“需要人工处理”。现在历史记录继续通过 `data_gap_reasons` 提示复核；当前短暂积压显示等待重试或正在恢复。观测失败、后台任务停止、当前异常持续达到 300 秒，仍要求人工检查。

这两处问题均先通过回归测试复现失败，再验证修复通过。尚未核验服务器镜像版本和对应时段日志，因此不能把本次线上统计延迟全部归因于它们。

## 继续定位统计延迟

在应用服务器执行以下只读命令，容器 ID 按实际部署核对。结果中的凭据和请求敏感内容需遮盖后再分享。

```bash
docker logs --since '2026-09-12T23:30:00+08:00' --until '2026-09-12T23:42:00+08:00' --timestamps db00065a07b5 2>&1 \
  | grep -E '渠道监控 Redis (事件暂缓处理|消息已隔离|消费者中断)|渠道成本 (outbox 恢复失败|Stream 写入失败|Stream 消费失败)|配置冲突|context deadline exceeded' \
  | tail -n 100
```

若容器已重建，需从旧容器日志或日志平台读取这一时间段；没有匹配输出不能证明当时没有异常。若出现配置冲突或事件隔离，保留具体错误、重试次数和事件标识，以便定位统计延迟的来源。

## 本地验证

仅更改成本 outbox 统计的读取与计数、恢复状态判定及对应测试。没有修改数据库模型字段、迁移、驱动、账本写入、扣费或重试调度。该统计读取主库，日志库不调用这一路径。

对照 `upstream/main`（`0ed497f066a68613375124303ef54f220267b334`），本次所有改动文件均为下游所有，未修改上游所有的文件。

| 验证环境 | 结果 |
| --- | --- |
| Go 1.26.5 windows/amd64 | 针对性回归及 `go build ./...` 通过 |
| SQLite 3.50.4 | 首次领取、真实失败、有效重试租约、超龄积压的健康观测通过 |
| MySQL 5.7.44 | 同上，真实实例通过 |
| PostgreSQL 9.6.24 | 同上，真实实例通过 |
| Redis 8.8.0 | 健康观测、恢复通知持久化与通知租约验证通过 |

数据库与 Redis 均使用本次创建的独立临时容器；SQLite 使用测试临时目录。以下密码仅用于这些本地临时实例。

```powershell
docker run --rm -d --name new-api-health-alert-mysql -p 127.0.0.1:23317:3306 -e MYSQL_ROOT_PASSWORD=monitor_health_test_only -e MYSQL_DATABASE=new_api_monitor_health_test mysql:5.7.44
docker run --rm -d --name new-api-health-alert-postgres -p 127.0.0.1:25443:5432 -e POSTGRES_PASSWORD=monitor_health_test_only -e POSTGRES_DB=new_api_monitor_health_test postgres:9.6
docker run --rm -d --name new-api-health-alert-redis -p 127.0.0.1:26381:6379 redis:latest

# 确认各实例就绪后执行。
$env:TEST_MYSQL_DSN='root:monitor_health_test_only@tcp(127.0.0.1:23317)/new_api_monitor_health_test?parseTime=true'
$env:TEST_POSTGRES_DSN='postgres://postgres:monitor_health_test_only@127.0.0.1:25443/new_api_monitor_health_test?sslmode=disable'
$env:TEST_MONITOR_REDIS_ADDR='127.0.0.1:26381'
& 'D:\Go\sdk\go1.26.5\bin\go.exe' test ./service -run '^(TestChannelMonitorHealthObservationDatabaseMatrix|TestChannelMonitorRecovery)' -count=1 -v

# 针对性本地回归在未设置上述 TEST_* 环境变量的终端执行。
& 'D:\Go\sdk\go1.26.5\bin\go.exe' test ./model ./service -run '^(TestChannelDailyCostOutbox|TestCM07ChannelDailyCost|TestChannelMonitorRecovery|TestChannelMonitorHealth|TestBuildChannelMonitorRecoveryEmail)' -count=1
& 'D:\Go\sdk\go1.26.5\bin\go.exe' build ./...

docker stop new-api-health-alert-mysql new-api-health-alert-postgres new-api-health-alert-redis
```

三库测试输出依次报告以上数据库版本，所有子测试通过，没有跳过 MySQL 或 PostgreSQL。最终 `git diff --check` 通过。
