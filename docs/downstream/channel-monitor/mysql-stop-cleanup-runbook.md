# 渠道监控 MySQL 停机清理 Runbook

本文用于渠道监控存储优化版本的一次性停机切换。旧渠道监控历史不迁移、不回填，不做双读或双写。本文只提供操作步骤和 SQL，仓库校验过程中不得对生产库执行其中的 `DROP` 或 `DELETE`。

适用范围：MySQL 5.7.8 及以上。示例库名为 `new-api`；生产库名不同时，必须先替换命令中的库名并重新核对 `SELECT DATABASE()` 输出。

## 1. 关键约束

- 必须先停止所有应用节点、后台任务节点和可能写入主库的临时脚本。
- Redis 清理由独立 Runbook 处理；本文不执行 `FLUSHDB`、`KEYS *` 或 Redis 删除。
- 不执行 `PURGE BINARY LOGS`，不临时关闭 binlog。
- `channels`、`abilities`、`logs` 必须保留。
- `system_tasks`、`system_task_locks`、`options` 是共享表，只允许按本文条件定向删除。
- `channel_test` 是通用渠道测试任务类型，必须保留。
- MySQL `DROP TABLE` 会隐式提交。本文脚本不能整体事务回滚，只能依靠备份恢复；脚本使用 `IF EXISTS` 和定向 `DELETE`，允许在应用保持停止时整段重跑。

## 2. 操作顺序

1. 停止全部应用节点和相关后台任务。
2. 确认连接的主机、端口和数据库名。
3. 记录目标表、定向任务、定向配置以及保留数据的行数。
4. 完成数据库备份并校验备份文件。
5. 在同一个明确指定数据库的 MySQL 会话中执行清理脚本。
6. 执行只读核验，确认目标数据已清理且共享数据未受影响。
7. 启动新版本，等待自动迁移完成，再执行新表只读核验。

任何一步不满足预期都应停止，不要继续启动应用。

## 3. 停机和数据库前置检查

先停止服务。具体命令由实际部署方式决定，例如 systemd、Docker Compose 或 Kubernetes；不要只停止入口节点而遗漏任务节点。

使用明确的生产连接参数进入目标库，密码通过交互输入，不要写入命令历史：

```bash
mysql --host=<MYSQL_HOST> --port=<MYSQL_PORT> --user=<MYSQL_USER> --password --database=new-api
```

进入后执行：

```sql
SELECT
    @@hostname AS mysql_host,
    @@port AS mysql_port,
    DATABASE() AS current_database,
    @@version AS mysql_version,
    @@read_only AS read_only,
    @@super_read_only AS super_read_only;

SELECT
    ID,
    USER,
    HOST,
    DB,
    COMMAND,
    TIME,
    STATE,
    LEFT(INFO, 200) AS INFO
FROM information_schema.PROCESSLIST
WHERE DB = DATABASE()
  AND ID <> CONNECTION_ID()
ORDER BY TIME DESC;
```

只有同时满足以下条件才能继续：

- `current_database` 与计划清理的生产库完全一致；本文示例应为 `new-api`。
- MySQL 实例和端口与变更单记录一致。
- 应用和任务节点均已停止。
- `PROCESSLIST` 中没有来自应用账户的持续写入、调度或清理会话。若无法判断，等待并再次查询，不要执行清理。

如果 `LOG_SQL_DSN` 指向独立日志库，`logs` 行数应在日志库单独记录；清理脚本仍只在主库执行。

## 4. 清理前行数记录

先记录必须保留的数据行数：

```sql
SELECT
    (SELECT COUNT(*) FROM `channels`) AS channels_rows,
    (SELECT COUNT(*) FROM `abilities`) AS abilities_rows,
    (SELECT COUNT(*) FROM `logs`) AS logs_rows,
    (SELECT COUNT(*) FROM `system_tasks` WHERE `type` = 'channel_test') AS channel_test_task_rows,
    (
        SELECT COUNT(*)
        FROM `system_tasks`
        WHERE `type` IS NULL
           OR `type` NOT IN (
               'channel_ratio_monitor',
               'channel_smart_schedule',
               'channel_smart_schedule_probe',
               'channel_monitor_cost_retention',
               'channel_model_detection'
           )
    ) AS retained_system_task_rows,
    (
        SELECT COUNT(*)
        FROM `system_task_locks`
        WHERE `type` IS NULL
           OR `type` NOT IN (
               'channel_ratio_monitor',
               'channel_smart_schedule',
               'channel_smart_schedule_probe',
               'channel_monitor_cost_retention',
               'channel_model_detection'
           )
    ) AS retained_system_task_lock_rows,
    (
        SELECT COUNT(*)
        FROM `options`
        WHERE (`key` IS NULL OR `key` NOT LIKE 'ChannelMonitor%')
          AND (`key` IS NULL OR `key` NOT LIKE 'ChannelModelDetection%')
    ) AS retained_option_rows;
```

再记录待清理表的存在状态、估算行数和空间。`TABLE_ROWS` 是 InnoDB 估算值，用于确认清理范围，不作为业务精确计数：

```sql
SELECT
    TABLE_NAME,
    TABLE_ROWS,
    ROUND(DATA_LENGTH / 1024 / 1024 / 1024, 2) AS data_gb,
    ROUND(INDEX_LENGTH / 1024 / 1024 / 1024, 2) AS index_gb
FROM information_schema.TABLES
WHERE TABLE_SCHEMA = DATABASE()
  AND TABLE_NAME IN (
      'channel_ratio_monitors',
      'channel_ratio_histories',
      'channel_daily_costs',
      'channel_daily_api_key_costs',
      'channel_monitor_minute_metrics',
      'channel_monitor_minute_route_metrics',
      'channel_monitor_minute_api_key_metrics',
      'channel_monitor_minute_duration_buckets',
      'channel_monitor_dirty_minutes',
      'channel_monitor_aggregation_states',
      'channel_monitor_redis_effect_states',
      'channel_smart_schedule_route_states',
      'channel_smart_schedule_group_pauses',
      'channel_smart_schedule_model_sample_states',
      'channel_smart_schedule_execution_details',
      'channel_status_probe_configs',
      'channel_status_probe_states',
      'channel_status_probe_executions',
      'channel_model_detection_global_configs',
      'channel_model_detection_configs',
      'channel_model_detection_targets',
      'channel_model_detection_batches',
      'channel_model_detection_runs',
      'channel_model_detection_executions',
      'channel_model_detection_cost_events'
  )
ORDER BY TABLE_NAME;

SELECT `type`, COUNT(*) AS rows_count
FROM `system_tasks`
WHERE `type` IN (
    'channel_ratio_monitor',
    'channel_smart_schedule',
    'channel_smart_schedule_probe',
    'channel_monitor_cost_retention',
    'channel_model_detection'
)
GROUP BY `type`
ORDER BY `type`;

SELECT `type`, COUNT(*) AS rows_count
FROM `system_task_locks`
WHERE `type` IN (
    'channel_ratio_monitor',
    'channel_smart_schedule',
    'channel_smart_schedule_probe',
    'channel_monitor_cost_retention',
    'channel_model_detection'
)
GROUP BY `type`
ORDER BY `type`;

SELECT `key`, `value`
FROM `options`
WHERE `key` LIKE 'ChannelMonitor%'
   OR `key` LIKE 'ChannelModelDetection%'
ORDER BY `key`;
```

将上述结果随变更记录保存。尤其要保存第一条查询的保留行数，供清理后逐项比较。

## 5. 备份

由于 DDL 无法整体事务回滚，执行前必须至少有一个已完成且可定位的数据库备份。优先使用平台的磁盘/实例快照；没有快照能力时，执行完整逻辑备份：

```bash
mysqldump --host=<MYSQL_HOST> --port=<MYSQL_PORT> --user=<MYSQL_USER> --password \
  --single-transaction --quick --routines --triggers --events --hex-blob \
  --set-gtid-purged=OFF new-api > new-api-before-channel-monitor-cleanup.sql
```

备份完成后至少确认文件非空并生成校验值：

```bash
test -s new-api-before-channel-monitor-cleanup.sql
sha256sum new-api-before-channel-monitor-cleanup.sql
```

记录备份时间、文件或快照标识、文件大小和 SHA-256。备份命令失败、文件为空或校验信息未记录时，不得执行清理。恢复操作应恢复到独立实例验证，不要在本次清理会话中尝试覆盖恢复。

## 6. 幂等清理脚本

下列脚本包含 21 张旧生产表。另包含 4 张最终模型新增表，用于处理新版本曾误启动或迁移中断后再停机重跑的情况：

- `channel_monitor_minute_route_metrics`
- `channel_monitor_minute_api_key_metrics`
- `channel_monitor_dirty_minutes`
- `channel_monitor_redis_effect_states`

它们都属于可丢弃的渠道监控数据。正常首次升级时这 4 张表尚不存在，`DROP TABLE IF EXISTS` 不会报错。

再次确认应用保持停止、备份已完成、`SELECT DATABASE()` 输出正确，然后在目标主库执行：

```sql
USE `new-api`;

SELECT DATABASE() AS cleanup_database;

-- 必须人工确认 cleanup_database 正确后，才继续执行下面的语句。
SET SESSION autocommit = 1;

-- 模型检测子表、历史表和状态表。
DROP TABLE IF EXISTS `channel_model_detection_cost_events`;
DROP TABLE IF EXISTS `channel_model_detection_executions`;
DROP TABLE IF EXISTS `channel_model_detection_runs`;
DROP TABLE IF EXISTS `channel_model_detection_batches`;
DROP TABLE IF EXISTS `channel_model_detection_targets`;
DROP TABLE IF EXISTS `channel_model_detection_configs`;
DROP TABLE IF EXISTS `channel_model_detection_global_configs`;

-- 状态探测表。
DROP TABLE IF EXISTS `channel_status_probe_executions`;
DROP TABLE IF EXISTS `channel_status_probe_states`;
DROP TABLE IF EXISTS `channel_status_probe_configs`;

-- 智能调度表。执行明细同名表会由新版本按 gzip 快照结构重建。
DROP TABLE IF EXISTS `channel_smart_schedule_execution_details`;
DROP TABLE IF EXISTS `channel_smart_schedule_model_sample_states`;
DROP TABLE IF EXISTS `channel_smart_schedule_group_pauses`;
DROP TABLE IF EXISTS `channel_smart_schedule_route_states`;

-- 分钟指标、成本、比例历史和 Redis 副作用水位表。
DROP TABLE IF EXISTS `channel_monitor_minute_api_key_metrics`;
DROP TABLE IF EXISTS `channel_monitor_minute_route_metrics`;
DROP TABLE IF EXISTS `channel_monitor_minute_metrics`;
DROP TABLE IF EXISTS `channel_monitor_minute_duration_buckets`;
DROP TABLE IF EXISTS `channel_monitor_dirty_minutes`;
DROP TABLE IF EXISTS `channel_monitor_aggregation_states`;
DROP TABLE IF EXISTS `channel_monitor_redis_effect_states`;
DROP TABLE IF EXISTS `channel_daily_api_key_costs`;
DROP TABLE IF EXISTS `channel_daily_costs`;
DROP TABLE IF EXISTS `channel_ratio_histories`;
DROP TABLE IF EXISTS `channel_ratio_monitors`;

-- 共享锁表只能删除渠道监控任务类型；channel_test 不在范围内。
DELETE FROM `system_task_locks`
WHERE `type` IN (
    'channel_ratio_monitor',
    'channel_smart_schedule',
    'channel_smart_schedule_probe',
    'channel_monitor_cost_retention',
    'channel_model_detection'
);

-- 共享任务表只能删除渠道监控任务类型；channel_test 不在范围内。
DELETE FROM `system_tasks`
WHERE `type` IN (
    'channel_ratio_monitor',
    'channel_smart_schedule',
    'channel_smart_schedule_probe',
    'channel_monitor_cost_retention',
    'channel_model_detection'
);

-- 渠道监控配置不兼容保留，启动后从默认值重新配置。
DELETE FROM `options`
WHERE `key` LIKE 'ChannelMonitor%'
   OR `key` LIKE 'ChannelModelDetection%';
```

不要在脚本外增加 `DROP TABLE channels`、`TRUNCATE` 共享表、无条件 `DELETE` 或 binlog 清理语句。

### 中断后的处理

- 不要尝试 `ROLLBACK` 已执行的 `DROP TABLE`；MySQL 已隐式提交这些 DDL。
- 保持应用停止，重新确认数据库名和备份标识。
- 重新执行第 6 节整段脚本。已删除的表由 `IF EXISTS` 跳过，已删除的共享行再次 `DELETE` 影响 0 行。
- 只有第 7 节全部通过后才能启动应用。

## 7. 清理后只读核验

清理完成后立即在同一个主库执行以下只读查询：

```sql
SELECT DATABASE() AS verified_database;

SELECT COUNT(*) AS remaining_cleanup_tables
FROM information_schema.TABLES
WHERE TABLE_SCHEMA = DATABASE()
  AND TABLE_NAME IN (
      'channel_ratio_monitors',
      'channel_ratio_histories',
      'channel_daily_costs',
      'channel_daily_api_key_costs',
      'channel_monitor_minute_metrics',
      'channel_monitor_minute_route_metrics',
      'channel_monitor_minute_api_key_metrics',
      'channel_monitor_minute_duration_buckets',
      'channel_monitor_dirty_minutes',
      'channel_monitor_aggregation_states',
      'channel_monitor_redis_effect_states',
      'channel_smart_schedule_route_states',
      'channel_smart_schedule_group_pauses',
      'channel_smart_schedule_model_sample_states',
      'channel_smart_schedule_execution_details',
      'channel_status_probe_configs',
      'channel_status_probe_states',
      'channel_status_probe_executions',
      'channel_model_detection_global_configs',
      'channel_model_detection_configs',
      'channel_model_detection_targets',
      'channel_model_detection_batches',
      'channel_model_detection_runs',
      'channel_model_detection_executions',
      'channel_model_detection_cost_events'
  );

SELECT COUNT(*) AS remaining_channel_monitor_tasks
FROM `system_tasks`
WHERE `type` IN (
    'channel_ratio_monitor',
    'channel_smart_schedule',
    'channel_smart_schedule_probe',
    'channel_monitor_cost_retention',
    'channel_model_detection'
);

SELECT COUNT(*) AS remaining_channel_monitor_locks
FROM `system_task_locks`
WHERE `type` IN (
    'channel_ratio_monitor',
    'channel_smart_schedule',
    'channel_smart_schedule_probe',
    'channel_monitor_cost_retention',
    'channel_model_detection'
);

SELECT COUNT(*) AS remaining_channel_monitor_options
FROM `options`
WHERE `key` LIKE 'ChannelMonitor%'
   OR `key` LIKE 'ChannelModelDetection%';

SELECT
    (SELECT COUNT(*) FROM `channels`) AS channels_rows,
    (SELECT COUNT(*) FROM `abilities`) AS abilities_rows,
    (SELECT COUNT(*) FROM `logs`) AS logs_rows,
    (SELECT COUNT(*) FROM `system_tasks` WHERE `type` = 'channel_test') AS channel_test_task_rows,
    (
        SELECT COUNT(*)
        FROM `system_tasks`
        WHERE `type` IS NULL
           OR `type` NOT IN (
               'channel_ratio_monitor',
               'channel_smart_schedule',
               'channel_smart_schedule_probe',
               'channel_monitor_cost_retention',
               'channel_model_detection'
           )
    ) AS retained_system_task_rows,
    (
        SELECT COUNT(*)
        FROM `system_task_locks`
        WHERE `type` IS NULL
           OR `type` NOT IN (
               'channel_ratio_monitor',
               'channel_smart_schedule',
               'channel_smart_schedule_probe',
               'channel_monitor_cost_retention',
               'channel_model_detection'
           )
    ) AS retained_system_task_lock_rows,
    (
        SELECT COUNT(*)
        FROM `options`
        WHERE (`key` IS NULL OR `key` NOT LIKE 'ChannelMonitor%')
          AND (`key` IS NULL OR `key` NOT LIKE 'ChannelModelDetection%')
    ) AS retained_option_rows;
```

验收条件：

- `verified_database` 是目标生产库。
- `remaining_cleanup_tables`、`remaining_channel_monitor_tasks`、`remaining_channel_monitor_locks`、`remaining_channel_monitor_options` 全部为 0。
- `channels_rows`、`abilities_rows`、`logs_rows`、`channel_test_task_rows` 与第 4 节完全一致。
- `retained_system_task_rows`、`retained_system_task_lock_rows`、`retained_option_rows` 与第 4 节完全一致。

任何保留行数变化都应停止上线并从 SQL 审计和备份检查原因。

## 8. 新版本启动后只读核验

第 7 节通过后启动新版本。等待标准或快速迁移完成，再执行：

```sql
SELECT
    expected.TABLE_NAME,
    CASE WHEN actual.TABLE_NAME IS NULL THEN 'MISSING' ELSE 'PRESENT' END AS table_status
FROM (
    SELECT 'channel_ratio_monitors' AS TABLE_NAME
    UNION ALL SELECT 'channel_ratio_histories'
    UNION ALL SELECT 'channel_daily_costs'
    UNION ALL SELECT 'channel_daily_api_key_costs'
    UNION ALL SELECT 'channel_monitor_minute_route_metrics'
    UNION ALL SELECT 'channel_monitor_minute_api_key_metrics'
    UNION ALL SELECT 'channel_monitor_minute_duration_buckets'
    UNION ALL SELECT 'channel_monitor_dirty_minutes'
    UNION ALL SELECT 'channel_monitor_aggregation_states'
    UNION ALL SELECT 'channel_monitor_redis_effect_states'
    UNION ALL SELECT 'channel_smart_schedule_route_states'
    UNION ALL SELECT 'channel_smart_schedule_group_pauses'
    UNION ALL SELECT 'channel_smart_schedule_model_sample_states'
    UNION ALL SELECT 'channel_smart_schedule_execution_details'
    UNION ALL SELECT 'channel_status_probe_configs'
    UNION ALL SELECT 'channel_status_probe_states'
    UNION ALL SELECT 'channel_status_probe_executions'
    UNION ALL SELECT 'channel_model_detection_global_configs'
    UNION ALL SELECT 'channel_model_detection_configs'
    UNION ALL SELECT 'channel_model_detection_targets'
    UNION ALL SELECT 'channel_model_detection_batches'
    UNION ALL SELECT 'channel_model_detection_runs'
    UNION ALL SELECT 'channel_model_detection_executions'
    UNION ALL SELECT 'channel_model_detection_cost_events'
) AS expected
LEFT JOIN information_schema.TABLES AS actual
  ON actual.TABLE_SCHEMA = DATABASE()
 AND actual.TABLE_NAME = expected.TABLE_NAME
ORDER BY expected.TABLE_NAME;

SELECT COUNT(*) AS legacy_minute_table_count
FROM information_schema.TABLES
WHERE TABLE_SCHEMA = DATABASE()
  AND TABLE_NAME = 'channel_monitor_minute_metrics';
```

验收条件：24 张最终模型表全部为 `PRESENT`，`legacy_minute_table_count` 为 0。此核验只确认迁移结果；完整调度、gzip 快照、分钟聚合、Redis 消费组和清理任务由后续上线验收计划验证。
