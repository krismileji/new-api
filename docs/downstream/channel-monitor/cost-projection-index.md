# 成本统计补偿查询组合索引

## 问题与改动

2026-09-15 提供的 MySQL 8.2.0 执行计划显示，成本统计补偿查询使用 `idx_channel_daily_cost_outbox_projection` 单列索引，预计检查 185,509 行，再筛选 `occurred_at`。相关日志中 99 次查询耗时约 246～1,306 毫秒，其中 71 次返回空结果。这些行数是执行计划估计值，不是待处理事件数。

`ChannelDailyCostOutbox` 增加组合索引声明：

```text
idx_channel_daily_cost_outbox_projection_time (redis_projected_at, occurred_at)
```

索引名称和列顺序与本次给管理员的 SQL 一致。具备数据库迁移职责的主节点启动时，现有 `AutoMigrate` 会创建缺失索引；已经手动创建正确同名索引的数据库可以直接升级。原单列索引、事件 ID 唯一索引、领取索引和租约索引均保留。

生产代码只改模型中的两处 GORM 标签。查询的保留时间、按 ID 分批的顺序、确认方式和账本行为不变。该表只由主数据库迁移；独立日志数据库的迁移仅涉及 `Log`，不会创建或查询此表。

对照 `upstream/main`，被修改的现有文件 `model/channel_daily_cost_outbox.go` 为下游新增文件，未修改上游拥有的文件。

## 验证结果

2026-09-16，使用 Go 1.26.5，在基于 `39485b5be` 的独立检出目录验证本次改动，避免共享工作区其他未提交修改干扰结果。

| 数据库 | 实际版本 | 结果 |
| --- | --- | --- |
| SQLite | 3.50.4 | 通过 |
| MySQL | 5.7.44，utf8mb4 / utf8mb4_unicode_ci | 通过 |
| PostgreSQL | 9.6.24 | 通过 |

三种真实数据库均覆盖以下四种起点，每种场景至少重复执行两次目标表迁移：

1. 全新数据库。
2. 上游 `v1.0.0-rc.35` 的代表性已配置数据库：冻结该版本的 `Option` 表结构并保留配置数据。该上游版本没有本下游成本 outbox 表，此场景验证新增表且保留已有配置。
3. 修复前 `39485b5be` 的完整 outbox 表结构，预先写入成本金额、身份信息、投影状态、租约与重试数据；索引结构与用户提供的线上结果一致。
4. 在旧 outbox 表上先手动建立同名组合索引，再执行模型迁移；MySQL 使用 `ALGORITHM=INPLACE LOCK=NONE` 创建该索引。

验证实际索引列顺序与唯一性、所有既有索引保留、全部已有记录保持一致；继续调用真实待投递查询及确认操作，检查日期边界、批次限制、按 ID 排序、已完成与过期事件排除，并验证事件 ID 仍不可重复插入。MySQL、PostgreSQL 还验证重复迁移和已存在运维索引时不产生额外 DDL。

SQLite 的现有 `glebarez/sqlite v1.9.0` 与 GORM 组合会在重复迁移此表时重建表。冻结的旧模型对照同样复现，属于已有行为；本次验证的是重复迁移后数据、索引及唯一性保持一致，不宣称 SQLite 重启迁移完全没有 DDL。没有为此调整数据库依赖或其他模型。

新增回归在补充模型标签前复现缺少组合索引，修改后完整三库矩阵通过。已有成本 outbox、投影和参数校验回归通过，`GOWORK=off go build ./...` 通过。构建使用已有 `web/dist`，未修改前端。

## 实际验证命令

下列凭据只用于本次创建的独立本地测试容器，不是生产配置。测试数据库必须为空；测试会清理自己创建的表。

```powershell
docker run --rm -d --pull never --name new-api-projection-index-mysql-test -p 127.0.0.1:13393:3306 -e MYSQL_ROOT_PASSWORD=projection_index_test_only -e MYSQL_DATABASE=new_api_projection_index_test mysql:5.7.44 --character-set-server=utf8mb4 --collation-server=utf8mb4_unicode_ci
docker run --rm -d --pull never --name new-api-projection-index-postgres-test -p 127.0.0.1:15493:5432 -e POSTGRES_PASSWORD=projection_index_test_only -e POSTGRES_DB=new_api_projection_index_test postgres:9.6

# 待两个独立数据库启动完成后，在包含本次改动的源码目录运行。
$env:GOWORK='off'
$env:TEST_COST_INDEX_MYSQL_DSN='root:projection_index_test_only@tcp(127.0.0.1:13393)/new_api_projection_index_test?parseTime=true&charset=utf8mb4'
$env:TEST_COST_INDEX_POSTGRES_DSN='postgres://postgres:projection_index_test_only@127.0.0.1:15493/new_api_projection_index_test?sslmode=disable'
$env:TEST_MYSQL_DSN=$env:TEST_COST_INDEX_MYSQL_DSN
$env:TEST_POSTGRES_DSN=$env:TEST_COST_INDEX_POSTGRES_DSN

& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./model -run '^TestChannelDailyCostProjectionIndexMigration$' -count=1 -timeout=180s -v
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./model -run '^TestChannelDailyCost(Outbox|Projection|Delta)' -count=1 -timeout=180s
& 'D:/Go/sdk/go1.26.5/bin/go.exe' build ./...

docker stop new-api-projection-index-mysql-test new-api-projection-index-postgres-test
```

## 上线检查

本地没有连接或修改生产数据库。上线后核对实际索引列顺序和查询执行计划，确认预计扫描行数及查询耗时改善；本地功能验证不代替生产执行计划验证。

这项改动针对成本统计补偿查询。Redis 429 冷却同步超时、快照事务冲突及历史隔离记录仍按各自原因处理，不能以新增索引作为整个监控已恢复的证明。
