# Redis Stream 消费与事件 outbox 补偿轮询

监控事件正常经过“内存队列 → Redis Stream → 消费组 → 监控统计”。Stream 消费使用 `XREADGROUP`；事件 writer 的数据库扫描用于补发未能写入 Redis 的持久化事件，补发成功后仍由原 Stream 消费组处理。

成本使用另一条可靠链路：成本 Stream 消费者先提交数据库 outbox，再直接使用返回的记录更新 Redis 今日成本统计，成本日账本另行按分钟批处理。数据库扫描每 5 秒补偿直接落库的兜底事件、探测成本、任务成本修正以及即时更新失败的事件。详见[成本 Stream 即时更新统计](cost-stream-projection.md)。

## 事件 outbox 的轮询策略

- 启动时确认表存在；补发循环不再调用 `HasTable()`。MySQL 的数据库名称、schema 和表存在性查询只在启动检查时发生。
- 启动后立即扫描。空结果或领取失败时，后续间隔依次为 1、2、4、5 秒，之后维持 5 秒。
- 领取到待处理事件后恢复 500 毫秒间隔；每批最多领取 100 条，继续使用原有事务、租约和重试逻辑。
- 本进程成功提交新的 outbox 记录后，通过容量为 1 的通知通道唤醒扫描。通知可以合并，发送通知不等待消费；重复事件不反复唤醒。
- 保留定时扫描，以发现其他实例写入、进程重启前留下、或调用方超时后才提交的记录。空闲时跨实例发现延迟可增加约 5 秒，不含数据库和 Redis 操作耗时。
- 停机取消计时等待；启动阶段的 schema 查询不持有停机需要的互斥锁。

成本统计由 Stream 消费即时更新，成本补偿扫描每 5 秒一次。只计算此前刷屏的这两组查询，在 MySQL 上空闲稳定后，单进程约从 9 条 SELECT/秒降到 0.4 条/秒；其他后台任务和统计重建的查询不包含在此数字中。`DEBUG=true` 仍会输出实际执行的 SQL。

## 事件补发验证记录

2026-09-13，使用 Go 1.26.5 windows/amd64 验证：

| 环境 | 结果 |
| --- | --- |
| 虚拟时钟 + SQLite | 空闲退避、启动检查复用、本机即时唤醒、跨实例定时发现、重复事件、停机、数据库和 Redis 故障恢复通过 |
| SQLite 3.50.4 | 真实数据库补发验证通过 |
| MySQL 5.7.44，utf8mb4 / utf8mb4_unicode_ci | 真实数据库补发验证通过 |
| PostgreSQL 9.6.24 | 真实数据库补发验证通过 |
| Redis 8.8.0 | 三库分别完成失败后重试、保留其他 worker 的有效租约、补发到 Stream、消费组读取与确认，重复扫描没有再次发布已完成事件 |

事件补发优化仅修改下游事件 writer 的调度和唤醒行为，没有改模型、索引、迁移、驱动或账本。该路径使用主数据库，不使用单独配置的日志数据库，不涉及迁移升级验证。与 `upstream/main` 对比，修改的现有文件 `service/channel_monitor_event_writer.go` 为下游新增文件，没有修改上游拥有的文件。成本更新链路的改动和验证记录见[成本 Stream 即时更新统计](cost-stream-projection.md)。

数据库与 Redis 使用独立临时容器，SQLite 使用测试临时目录。以下凭据仅用于这些本地测试实例。MySQL 必须使用支持中文的字符集；最初默认 latin1 的测试实例已重建为 utf8mb4 后重新验证通过。

```powershell
docker run --rm -d --pull never --name new-api-outbox-poll-mysql -p 127.0.0.1:23318:3306 -e MYSQL_ROOT_PASSWORD=outbox_poll_test_only -e MYSQL_DATABASE=new_api_outbox_poll_test mysql:5.7.44 --character-set-server=utf8mb4 --collation-server=utf8mb4_unicode_ci
docker run --rm -d --pull never --name new-api-outbox-poll-postgres -p 127.0.0.1:25444:5432 -e POSTGRES_PASSWORD=outbox_poll_test_only -e POSTGRES_DB=new_api_outbox_poll_test postgres:9.6
docker run --rm -d --pull never --name new-api-outbox-poll-redis -p 127.0.0.1:26382:6379 redis:latest

# 确认实例就绪后，在项目根目录执行。
$env:PATH = 'D:/Go/sdk/go1.26.5/bin;' + $env:PATH
$env:TEST_EVENT_OUTBOX_REDIS_ADDR = '127.0.0.1:26382'
$env:TEST_EVENT_OUTBOX_MYSQL_DSN = 'root:outbox_poll_test_only@tcp(127.0.0.1:23318)/new_api_outbox_poll_test?parseTime=true'
$env:TEST_EVENT_OUTBOX_POSTGRES_DSN = 'postgres://postgres:outbox_poll_test_only@127.0.0.1:25444/new_api_outbox_poll_test?sslmode=disable'
go test ./model ./service -run '^(TestChannelMonitorEvent|TestStartChannelMonitorEventWriter|TestEnqueueChannelMonitorEvent|TestPublishChannelMonitorEvent|TestCM08|TestCM07ChannelDailyCost|TestReliableDailyCost)' -count=1 -timeout 120s
go build ./...

docker stop new-api-outbox-poll-mysql new-api-outbox-poll-postgres new-api-outbox-poll-redis
```

回归测试及三库矩阵均通过，MySQL、PostgreSQL 子测试未跳过；全项目构建通过。
