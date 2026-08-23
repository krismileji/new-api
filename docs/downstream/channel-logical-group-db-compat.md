# 逻辑渠道组数据库兼容验证记录

任务编号：P11

## 验证范围

- SQLite：`Channel.logical_channel_id` 可空索引、逻辑组/成员表 `AutoMigrate` 可重复执行。
- 成员约束：`channel_logical_group_members.channel_id` 使用唯一索引，一个物理渠道最多属于一个逻辑组；服务层仍负责校验逻辑组是否存在，数据库不创建外键。
- Revision CAS：使用 `WHERE id = ? AND revision = ?` 的 GORM 更新，首次更新只影响一行，旧 revision 重试影响零行。
- 分页和事务：成员查询使用普通 GORM `LIMIT/OFFSET`；失败事务不会留下成员行。
- 完整迁移面还包括逻辑调度两表、逻辑状态探测两表、模型检测逻辑配置/目标两表，以及状态探测/模型检测运行与执行表新增的逻辑 ID、revision、冻结成员快照和逻辑目标来源字段。
- 升级兼容：迁移前已存在的物理状态探测配置/状态和模型检测 run/execution 行必须保留；新增列使用可兼容旧行的零值或空快照，不能要求破坏性重建。
- MySQL 5.7.8+、PostgreSQL 9.6+：先用 DryRun DDL 检查标识符、字段和无外键约束；配置 DSN 后对上述完整迁移面执行真实 `AutoMigrate`、幂等迁移、唯一约束和 CAS。

## 新增验证资产

- `model/channel_logical_group_db_compat_test.go`
  - `TestChannelLogicalGroupSchemaMigrationSQLite`
  - `TestChannelLogicalGroupRevisionCASSQLite`
  - `TestChannelLogicalGroupSchemaDialectDDLIsPortable`
  - `TestChannelLogicalGroupSchemaMigrationConfiguredDatabases`（未配置 DSN 时跳过）

当前测试文件覆盖完整功能 schema，并在同一路径验证全新逻辑表创建与已有物理表增列升级。

## 执行方式

```text
go test ./model -run 'TestChannelLogicalGroup(SchemaMigrationSQLite|RevisionCASSQLite|SchemaDialectDDLIsPortable|SchemaMigrationConfiguredDatabases)$'

# 可选真实数据库验证（版本要求：MySQL >= 5.7.8、PostgreSQL >= 9.6）
TEST_MYSQL_DSN='...' TEST_POSTGRES_DSN='...' \
  go test ./model -run TestChannelLogicalGroupSchemaMigrationConfiguredDatabases -count=1
```

测试使用临时 SQLite 文件；MySQL 和 PostgreSQL 均使用随机表前缀，测试结束后清理，不修改业务数据库中的既有表。

## 当前环境结果

- SQLite 实际旧表升级、重复 `AutoMigrate`、成员唯一索引、revision CAS、分页、事务回滚和旧行保留均通过。
- MySQL 5.7 与 PostgreSQL 9.6 已使用临时真实数据库执行相同升级路径两次，唯一约束和 revision CAS 均通过。
- MySQL/PostgreSQL/SQLite 三方言 DDL 均通过，未引入外键、JSONB、TIMESTAMPTZ 或不兼容的无符号字段。
- 最终四包测试、全仓测试和 `git diff --check` 均已通过。
