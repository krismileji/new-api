# 分组说明保留

分组定价使用独立的 `GroupDescriptions` 配置保存说明。取消“用户可选”后，说明灰显并保留；保存后刷新页面或往返切换 JSON 模式，也不会清空说明。重新勾选后恢复原说明及编辑能力。

## 配置与兼容

- `UserUsableGroups` 继续使用原来的“分组名 → 说明”结构，且只包含用户可选的分组。普通用户的分组可选范围不变。
- `GroupDescriptions` 是“分组名 → 说明”的 JSON 对象，通过既有 `/api/option/` 和 `options` 表保存，无需修改后端接口、表结构或迁移。
- 旧配置首次加载时从 `UserUsableGroups` 补齐说明，保存时写入独立配置。可选分组仍以 `UserUsableGroups` 的当前说明为准，以兼容 JSON 编辑及旧格式。
- JSON 编辑器修改可选分组时，会保留刚移除的说明。保存先写入说明；接口返回失败时停止后续更新。
- 分组改名时说明跟随新名称，删除分组时清理其说明。管理员主动清空说明时保留空字符串，不恢复旧文字。
- 此修复无法找回此前已经丢失且没有备份的说明。直接调用旧接口删除 `UserUsableGroups` 中的条目时，调用方仍需自行先保存对应的 `GroupDescriptions`。

## 上游文件修改范围

这些文件在 `upstream/main` 中均已存在。既有表单没有可挂接的说明持久化入口，需要以下最小修改完成修复：

| 文件（相对 `web/src/features/system-settings/`） | 必要修改 |
| --- | --- |
| `billing/index.tsx` | 提供旧安装缺少新配置时的默认值 |
| `billing/section-registry.tsx` | 将新配置传入定价表单 |
| `types.ts` | 声明账单设置中的新配置类型 |
| `models/ratio-settings-card.tsx` | 兼容旧值、保存说明、清理已删除分组，并在保存失败时停止 |
| `models/group-ratio-form.tsx` | 传递说明配置，在 JSON 编辑中保留移除的说明 |
| `models/group-ratio-visual-editor.tsx` | 独立读写说明，并在未勾选时显示灰显说明及详情 |

新增合并逻辑与回归测试独立放置。没有修改现有后端生产代码或语言文件。

## 验证记录

验证时间：2026-09-20 至 2026-09-21（Asia/Shanghai）。

前端使用真实表单、控件和设置保存逻辑，仅替换网络边界。新增用例先在旧实现上验证失败，再实现修复。

```powershell
Set-Location D:/GoProjects/new-api/web
bun run test src/features/system-settings/models/__tests__
bun run typecheck
bun run build

$files = @(
  'src/features/system-settings/billing/index.tsx',
  'src/features/system-settings/billing/section-registry.tsx',
  'src/features/system-settings/types.ts',
  'src/features/system-settings/models/group-descriptions.ts',
  'src/features/system-settings/models/group-ratio-form.tsx',
  'src/features/system-settings/models/group-ratio-visual-editor.tsx',
  'src/features/system-settings/models/ratio-settings-card.tsx',
  'src/features/system-settings/models/__tests__/group-descriptions.test.tsx'
)
bunx --no-install oxlint -c .oxlintrc.json @files
bunx --no-install oxfmt --check @files
```

结果：相关 6 个测试文件、19 项测试通过，其中说明保留回归 11 项；类型检查、lint、格式检查和生产构建通过。

数据库验证使用临时 SQLite 文件和独立 Docker 容器。MySQL、PostgreSQL 使用空的专用数据库 `new_api_group_descriptions_test`，不连接现有业务数据库。下面的口令仅用于本次临时容器。

```powershell
Set-Location D:/GoProjects/new-api
docker run --detach --name codex-group-descriptions-mysql-20260920 --publish 127.0.0.1::3306 --env MYSQL_ROOT_PASSWORD=codex_group_descriptions_test --env MYSQL_DATABASE=new_api_group_descriptions_test mysql:5.7.44 --character-set-server=utf8mb4 --collation-server=utf8mb4_unicode_ci
docker run --detach --name codex-group-descriptions-postgres-20260920 --publish 127.0.0.1::5432 --env POSTGRES_PASSWORD=codex_group_descriptions_test --env POSTGRES_DB=new_api_group_descriptions_test postgres:9.6
docker port codex-group-descriptions-mysql-20260920
docker port codex-group-descriptions-postgres-20260920

# 本次分配端口分别为 58988、58997；复现时使用实际分配端口。
$env:GROUP_DESCRIPTIONS_MYSQL_DSN = 'root:codex_group_descriptions_test@tcp(127.0.0.1:58988)/new_api_group_descriptions_test?charset=utf8mb4&parseTime=true'
$env:GROUP_DESCRIPTIONS_POSTGRES_DSN = 'postgres://postgres:codex_group_descriptions_test@127.0.0.1:58997/new_api_group_descriptions_test?sslmode=disable'
& 'D:/Go/sdk/go1.26.5/bin/go.exe' test ./model -run '^TestGroupDescriptionsOptionPersistence$' -count=1 -v
```

| 数据库实际版本 | 结果 |
| --- | --- |
| SQLite 3.50.4 | 通过 |
| MySQL 5.7.44 | 通过 |
| PostgreSQL 9.6.24 | 通过 |

测试覆盖旧格式已有数据、独立保存说明、取消可选、两次清空内存配置后从数据库加载、重新启用、Unicode 与引号、空说明及更新既有记录。此变更没有 schema/migration 或日志数据库路径变更。
