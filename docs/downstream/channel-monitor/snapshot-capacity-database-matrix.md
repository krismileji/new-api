# VERIFY-01 调度快照容量与数据库矩阵验收

## 验收范围

本报告只验证 `channel_smart_schedule_execution_details` 的任务级 gzip 快照，不修改调度业务语义。样本使用与真实 adjustment 等结构的脱敏数据，包含渠道、分组、模型、调度动作、原因、完整评分输入、评分分量、健康度和决策字段；单条未压缩 JSON 平均约 3.16 KiB。

覆盖项：

- 120、500、1,000、5,000 个 adjustment 的 JSON 精确往返和容量测量。
- gzip 比例、往返耗时、`allocs/op`、每次操作分配字节和观测堆内存峰值。
- SQLite、MySQL、PostgreSQL 的建表、写入、读取、精确往返和 3 天清理。
- 损坏 gzip、解压后超过 64 MiB 和 `item_count` 数量不一致。
- MySQL `max_allowed_packet` 与最大测试快照的余量。

所有外部数据库对象都使用 `verify01_<唯一值>_` 表名前缀；PostgreSQL 额外使用同名前缀的独立 schema。测试结束后仅删除这些临时对象。未执行生产清理命令。

## 容量结果

环境：Windows amd64、AMD Ryzen 9 9950X3D、Go 1.26.5。benchmark 使用 `-count=3`，耗时、分配字节和分配次数取三次中位数。峰值为测试进程在单次压缩加解压期间轮询到的 `HeapAlloc` 相对基线增量，仅用于容量规划，不作为性能阈值。

| adjustments | 未压缩 JSON | gzip | gzip/raw | 往返耗时中位数 | B/op 中位数 | allocs/op 中位数 | 观测堆峰值增量 |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 120 | 379,533 B | 18,079 B | 4.763% | 5.71 ms | 3,423,110 B | 348 | 3,694,272 B |
| 500 | 1,581,712 B | 69,553 B | 4.397% | 27.04 ms | 12,725,216 B | 1,134 | 8,192,792 B |
| 1,000 | 3,162,995 B | 137,084 B | 4.334% | 53.78 ms | 25,058,629 B | 2,150 | 17,589,432 B |
| 5,000 | 15,814,570 B | 677,692 B | 4.285% | 268.52 ms | 112,629,744 B | 10,250 | 89,254,688 B |

结论：等结构样本的压缩后体积约为原始 JSON 的 4.3%～4.8%，节省约 95%。5,000 adjustment 的压缩快照约 662 KiB，未触及 64 MiB 未压缩 JSON 上限，但单轮压缩加解压的瞬时堆内存可接近 90 MiB。后续扩大调度规模时应同时观察 adjustment 数、未压缩字节和进程堆内存，不能只看 MySQL 表大小。

## 数据库矩阵

本地测试端点：MySQL `127.0.0.1:13306/new_api_test`，PostgreSQL `127.0.0.1:15432/new_api_test`。当前 PostgreSQL 容器未设置 `POSTGRES_USER`，实际管理员用户为镜像默认的 `postgres`；使用 `root` 会认证失败。

| 数据库 | 临时对象隔离 | 5,000 条写入/读取 | JSON 精确往返 | 3 天清理 | `max_allowed_packet` |
| --- | --- | --- | --- | --- | ---: |
| SQLite | `verify01_` 表前缀和独立临时文件 | 通过 | 通过 | 通过，删除 1 条过期快照 | 不适用 |
| MySQL 8 | `verify01_` 表前缀 | 通过 | 通过 | 通过，删除 1 条过期快照 | 67,108,864 B |
| PostgreSQL 16 | `verify01_` 独立 schema 和表前缀 | 通过 | 通过 | 通过，删除 1 条过期快照 | 不适用 |

MySQL 最大测试快照为 677,692 B，仅占当前 `max_allowed_packet` 的约 1.01%，余量充足。测试完成后复查 MySQL 表和 PostgreSQL schema，未发现残留的 `verify01_` 对象。

## 异常与边界

以下行为均通过：

- 每个 adjustment 的 JSON 字节和数组顺序精确往返。
- 非 gzip 数据读取失败。
- gzip 解压后超过 64 MiB 时读取失败。
- `item_count` 与解压数组长度不一致时读取失败。
- 3 天边界使用 `created_at < cutoff`，仅删除边界前数据。
- SQLite、MySQL、PostgreSQL 均能重复建表、写入最大测试快照、读取并执行批量清理。

## 重复执行

先配置本地测试数据库 DSN，禁止指向生产数据库：

```powershell
$env:CHANNEL_MONITOR_VERIFY01 = '1'
$env:TEST_MYSQL_DSN = '<本地 MySQL 测试 DSN>'
$env:TEST_POSTGRES_DSN = '<本地 PostgreSQL 测试 DSN，当前容器用户为 postgres>'
go test ./model -run '^(TestVerify01SnapshotCapacityAndExactRoundTrip|TestVerify01SnapshotDatabaseMatrix|TestChannelSmartScheduleExecutionDetailsRejectsLimitsAndCorruption|TestChannelSmartScheduleExecutionDetailsRejectsDecompressedDataOverLimit)$' -count=1 -v
```

容量 benchmark：

```powershell
go test ./model -run '^$' -bench '^BenchmarkVerify01SnapshotRoundTrip$' -benchmem -count=3
```

容量和数据库矩阵属于显式验收，不进入日常 `go test ./model`：未设置 `CHANNEL_MONITOR_VERIFY01=1` 时两个测试会直接跳过。开启验收开关后，即使外部数据库 DSN 未配置，矩阵测试仍会强制执行 SQLite，并明确跳过 MySQL、PostgreSQL。正式 VERIFY-01 验收必须同时配置开关和两项 DSN，确认三个数据库子测试全部为 `PASS`。
