# 使用日志“用户侧”视图设计

## 背景

使用日志当前提供“全部”和“仅自己”两种查看范围。“全部”用于管理员排查完整日志，“仅自己”使用用户侧可见性规则并限定为当前用户。

需要新增管理员专属的“用户侧”视图，让管理员按照普通用户可见的日志规则查看所有用户的数据。

## 视图语义

| 视图 | 数据范围 | 数据表现 | 使用者 |
| --- | --- | --- | --- |
| 全部 | 所有用户 | 完整管理日志，包括渠道、重试过程和管理员诊断信息 | 管理员 |
| 用户侧 | 所有用户 | 按普通用户可见规则筛选，只保留最终结果；保留完整管理员字段和聚合表格布局 | 管理员 |
| 仅自己 | 当前用户 | 用户侧过滤和脱敏规则 | 当前登录用户 |

“用户侧”的业务契约是复用“仅自己”的可见记录筛选规则，并取消当前用户限制；它不复用普通用户的数据脱敏，因为该视图和接口仅供管理员使用。筛选逻辑仍须共享，避免最终结果规则分叉。

## 权限

- 普通用户继续隐式使用“仅自己”，不显示范围切换器。
- 管理员显示“全部 / 用户侧 / 仅自己”三个选项，默认保持“全部”。
- “用户侧”接口必须使用服务端管理员鉴权，不能依赖前端隐藏。
- 管理员权限失效时，有效范围立即降级为“仅自己”。

## 后端设计

新增管理员接口：

```text
GET /api/log/user-visible
GET /api/log/user-visible/stat
GET /api/mj/user-visible
GET /api/task/user-visible
```

通用日志查询复用现有用户侧规则：

- 排除仅用于内部诊断的重试过程日志。
- 只保留消费和最终错误等正常请求，充值、管理及渠道维护探测不进入用户侧视图。
- 不调用普通用户格式化逻辑，保留渠道名称、管理员信息、操作审计信息和完整错误详情。
- 渠道名称按完整管理日志相同的规则从渠道表或缓存补充。
- 支持时间、类型、用户名、API 密钥名称、模型、分组和请求 ID 等聚合查询条件。
- 用户侧额外支持与“全部”一致的渠道筛选；统计同样排除重试过程记录。

绘图和任务日志沿用各自用户侧的结果内容，只扩大用户范围；聚合视图保留渠道编号并补充用户标识，便于与“全部”表格保持一致。

## 前端设计

查看范围使用明确枚举：

```ts
type LogsViewScope = 'all' | 'user-visible' | 'self'
```

不同能力分别派生，不能继续由单一管理员布尔值同时控制数据源、列和筛选器：

- `all`：显示管理员详情、用户列和渠道列。
- `user-visible`：显示用户列、渠道列和管理员详情，使用与“全部”相同的完整数据表现与聚合筛选，仅日志行范围采用用户可见规则。
- `self`：不显示用户列、渠道及管理员详情。

切换范围时：

- 返回第一页。
- 保留三种范围都支持的筛选条件。
- 仅切换到“仅自己”时清理渠道和用户名筛选条件；“用户侧”保留聚合筛选条件。
- React Query 查询键必须包含完整范围值。
- 仅允许相同日志分类和相同范围复用占位数据，避免切换时短暂显示其他范围的日志。
- “全部”和“用户侧”共用聚合表格的列可见性配置；“仅自己”单独保存。

## 下游改动边界

已对照可用的 `upstream/main`。本功能新增的 controller、model、测试及本文档均放在下游新文件中；以下 upstream-owned 文件因现有查询入口、路由或页面组件需要接入新范围而必须做最小修改：

| 文件 | 修改必要性 |
| --- | --- |
| `model/log.go` | 让完整统计和用户侧统计共享统计查询实现，并由用户侧入口复用最终结果过滤。 |
| `router/api-router.go` | 在现有日志、绘图和任务路由组注册管理员专属接口。 |
| `web/src/features/usage-logs/api.ts` | 将现有二态接口选择扩展为三态，并拆出无循环依赖的查询参数模块。 |
| `web/src/features/usage-logs/components/columns/common-logs-columns.tsx` | 在“用户侧”显示与“全部”一致的用户、渠道列和管理员详情。 |
| `web/src/features/usage-logs/components/columns/drawing-logs-columns.tsx` | 为跨用户绘图日志补充用户列。 |
| `web/src/features/usage-logs/components/columns/task-logs-columns.tsx` | 将用户列和渠道列的显示能力拆开。 |
| `web/src/features/usage-logs/components/common-logs-filter-bar.tsx` | 让“用户侧”使用与“全部”一致的用户名和渠道筛选。 |
| `web/src/features/usage-logs/components/common-logs-stats.tsx` | 按完整范围选择统计接口并隔离查询缓存。 |
| `web/src/features/usage-logs/components/task-logs-filter-bar.tsx` | 让“用户侧”保留与“全部”一致的渠道筛选。 |
| `web/src/features/usage-logs/components/usage-logs-mobile-card.tsx` | 在移动端跨用户绘图日志中呈现用户字段。 |
| `web/src/features/usage-logs/components/usage-logs-provider.tsx` | 将有效范围和数据、用户列、渠道列能力分别派生。 |
| `web/src/features/usage-logs/components/usage-logs-table.tsx` | 按完整范围隔离查询、占位数据和列可见性配置。 |
| `web/src/features/usage-logs/index.tsx` | 增加管理员专属 Tab，并在切换时重置分页和清理不兼容筛选。 |
| `web/src/features/usage-logs/lib/columns.ts` | 按范围能力生成三类日志列。 |
| `web/src/features/usage-logs/lib/utils.ts` | 按范围构造查询参数并选择对应数据接口。 |
| `web/src/features/usage-logs/types.ts` | 定义三态范围类型并更新获取配置契约。 |

## 验收标准

- 管理员能看到并切换三个范围，普通用户看不到范围切换器。
- 普通用户调用任一 `user-visible` 接口会被拒绝。
- “用户侧”能返回多个用户的数据，并支持按用户名定位。
- “用户侧”和“仅自己”采用相同的最终结果过滤；“用户侧”保留完整管理员数据，“仅自己”继续执行普通用户脱敏。
- “全部”的管理诊断能力保持不变。
- 通用、绘图和任务日志的范围行为一致。
- 切换范围不会复用错误的缓存、分页或列配置。
