# 使用日志用户侧视图

使用日志提供 `all`、`user-visible` 和 `self` 三种范围，页面入口为 `/usage-logs/{common|drawing|task}`。管理员可以切换全部、用户侧和仅自己；普通用户只能查看自己的范围。

## 接口和权限

| 范围 | 权限 | common | 绘图 | 任务 |
| --- | --- | --- | --- | --- |
| `all` | AdminAuth | `GET /api/log/` 及 `/stat` | `GET /api/mj/` | `GET /api/task/` |
| `user-visible` | AdminAuth | `GET /api/log/user-visible` 及 `/user-visible/stat` | `GET /api/mj/user-visible` | `GET /api/task/user-visible` |
| `self` | UserAuth | `GET /api/log/self` 及 `/self/stat`、`/self/search` | `GET /api/mj/self` | `GET /api/task/self` |

普通用户直接请求管理员范围会被拒绝。

## 可见性

common 日志的用户侧范围只保留消费和错误记录，排除重试尝试、渠道监控测试、智能探测、状态探测、分组探测和违规费用记录。self 返回当前用户记录并清除 `admin_info`、`audit_info`、渠道名和内部错误；管理员的 user-visible 保留完整诊断字段、渠道和用户列。

用户侧错误优先使用 `user_visible_error_message`，否则只返回 HTTP 状态错误。管理员可按规则恢复请求 IP；普通用户不能看到管理员诊断字段。错误映射、白名单和关键字见[中继可靠性](../relay-reliability/README.md)。

## 统计

common 用户侧统计与列表使用相同过滤，只统计 consume quota；RPM 和 TPM 使用最近 60 秒窗口。all 统计使用管理员全量范围。绘图和任务的管理员 user-visible 查询保持各自当前的全量跨用户语义。

## 前端行为

非管理员选择管理员范围时，前端降级为 `self`。切换到 self 会清除用户名和渠道筛选；范围、分页、筛选和日志类别共同组成查询缓存键。
