# 成本 Stream 即时更新统计

普通成本事件由 Redis Stream 消费者批量保存至数据库 outbox。保存成功后，消费者立即使用本次返回的已提交记录更新 Redis 成本统计，不再等待定时扫描重新读取这些记录；Redis 更新成功后确认 `redis_projected_at`。日账本继续按原有分钟节奏处理。

```text
成本 Stream → 消费者 → 数据库 outbox 提交成功
                          ├─ 返回已提交记录 → 立即更新 Redis 统计
                          └─ 原有分钟任务 → 数据库日成本账本

数据库补偿扫描（5 秒）→ 处理未同步记录 → Redis 统计
```

数据库 outbox 保留事件去重、可靠交接、失败重试及统计重建能力。新模型 API `StoreChannelDailyCostOutboxEventsWithRecords` 返回记录的数据库 ID 和已有投影状态；重复事件返回原记录，整个事务回滚时返回空记录集。Redis 继续使用原有事件标识、记录版本和事务逻辑去重。

## 正常更新与补偿

- 正常消费：提交数据库后直接更新 Redis，不增加一次待处理记录查询。Redis 统计尚未初始化或丢失时，首次更新仍需要读取数据库重建。
- 即时更新失败：已落库的事件仍可确认并从 Stream 删除，数据库记录保持未投影，后台在下一次秒级检查时尝试补偿。
- Redis 成功、数据库投影确认失败：补偿会再次收到同一记录，Redis 按相同版本去重，然后重试确认。
- Stream 确认失败：消息可以重放，已提交事件不会再次插入，已投影记录不会重复累计。
- 空闲补偿扫描：每 5 秒一次，覆盖 Redis 兜底直接落库、探测成本、任务成本修正、进程重启和其他实例遗留事件。这些路径的统计延迟可能增加至约 5 秒，不含处理时间。
- 存在积压或失败：下一次秒级检查继续处理；每批最多 256 条，沿用原有单轮处理预算。
- 运行状态：每秒检查 Redis、刷新状态及处理跨日或统计丢失后的重建，避免降低数据库扫描频率导致状态超过接口的 10 秒有效期。
- 旧事件：早于昨日的 Stream 消息保存到数据库，历史金额由日账本处理，不因补投影而重建过期 Redis 日期。

## 验证

2026-09-13，Go 1.26.5 windows/amd64：

| 环境 | 结果 |
| --- | --- |
| SQLite 3.50.4 | 通过 |
| MySQL 5.7.44，utf8mb4 / utf8mb4_unicode_ci，`clientFoundRows=true` | 通过 |
| PostgreSQL 9.6.24 | 通过 |
| Redis 8.8.0 | 以上三库均配合真实 Redis 验证通过 |

三库共用相同验证场景：即时更新且无额外数据库读取、重复消费、Stream 确认失败、Redis 投影失败、数据库投影标记失败、冲突批次回滚、直接落库补偿、任务金额修正、日账本和 Redis 重建一致、旧消息保留，以及统计丢失后新事件与重建不重复累计。

模型回归验证返回记录已提交、重复记录保留 ID 与投影状态、回滚批次不暴露记录。虚拟时钟测试验证空闲 5 秒扫描、秒级状态刷新、失败后下次秒级检查重试，以及停止后不继续查询。成本相关模型、服务和接口回归、全项目构建均通过。

这些改动只涉及主数据库；不使用独立日志数据库。没有修改表结构、索引、迁移、驱动、连接配置或费用计算，因此不涉及 schema 升级验证。与 `upstream/main` 比较，修改的现有代码文件均为下游拥有：`model/channel_daily_cost_outbox.go`、`service/channel_daily_cost_outbox.go`、`service/channel_monitor_reliable_cost_projection.go`；没有修改上游拥有的文件。

验证命令如下，数据库和 Redis 均为本次创建的独立临时容器，密码仅用于测试；测试要求指定数据库为空，并清理自己的测试数据。

```powershell
docker run --rm -d --pull never --name new-api-cost-projection-mysql -p 127.0.0.1:23318:3306 -e MYSQL_ROOT_PASSWORD=cost_projection_test_only -e MYSQL_DATABASE=new_api_cost_projection_test mysql:5.7.44 --character-set-server=utf8mb4 --collation-server=utf8mb4_unicode_ci
docker run --rm -d --pull never --name new-api-cost-projection-postgres -p 127.0.0.1:25444:5432 -e POSTGRES_PASSWORD=cost_projection_test_only -e POSTGRES_DB=new_api_cost_projection_test postgres:9.6
docker run --rm -d --pull never --name new-api-cost-projection-redis -p 127.0.0.1:26382:6379 redis:latest

# 确认三个实例就绪后执行。
$env:PATH = 'D:/Go/sdk/go1.26.5/bin;' + $env:PATH
$env:TEST_COST_PROJECTION_REDIS_ADDR = '127.0.0.1:26382'
$env:TEST_COST_PROJECTION_MYSQL_DSN = 'root:cost_projection_test_only@tcp(127.0.0.1:23318)/new_api_cost_projection_test?parseTime=true&clientFoundRows=true'
$env:TEST_COST_PROJECTION_POSTGRES_DSN = 'postgres://postgres:cost_projection_test_only@127.0.0.1:25444/new_api_cost_projection_test?sslmode=disable'
go test ./service -run '^(TestChannelDailyCostStreamProjection|TestChannelDailyCostProjectionPolling)' -count=1 -timeout 120s

# 以下回归在未设置 TEST_COST_PROJECTION_* 的终端执行。
go test ./model ./service ./controller -run '^(TestChannelDailyCost|TestCM07|TestReliableDailyCost|TestChannelMonitorEvent|TestChannelMonitorRedisDaily|TestChannelTaskCost|TestChannelMonitorAnalytics(Current|Cost|Historical)|TestChannelMonitorCurrentAndMixedCostAnalytics|TestChannelMonitorCurrentSuccessFiltersFacts)' -count=1 -timeout 120s
go build ./...

docker stop new-api-cost-projection-mysql new-api-cost-projection-postgres new-api-cost-projection-redis
```

真实三库矩阵无跳过；最终格式和 `git diff --check` 检查通过。
