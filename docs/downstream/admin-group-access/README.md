# 管理员分组访问

## 行为

管理员及更高角色可以使用 `GroupRatio` 中全部已配置的真实分组，不再受普通用户 `UserUsableGroups` 映射限制。普通用户的可用分组规则保持不变。

`auto` 是伪分组，不会因为管理员角色自动出现；它仍需在原有用户可用分组配置中显式启用。管理员使用 `auto` 时，候选目标会从当前全部可用真实分组与系统自动分组配置的交集中产生。

## 覆盖范围

角色感知的分组集合用于以下入口：

- 令牌鉴权时校验固定分组。
- 用户可选分组接口。
- 模型列表和用户模型查询。
- 定价列表、分组倍率和自动分组返回值。
- 中继渠道选择与自动分组路由。

因此，管理员创建或使用指向任意已配置真实分组的令牌时，鉴权、模型可见性、定价展示和实际路由保持一致。

## 安全边界

该功能扩大的是管理员的业务分组可用范围，不改变角色认证本身。请求仍必须通过原有用户或令牌鉴权，且目标分组必须存在于当前 `GroupRatio` 配置中。

普通用户不会继承管理员范围；调用方角色从已认证用户缓存写入请求上下文，不信任客户端自行提交的角色值。

## 关键实现

- `service/role_group.go`：角色感知的可用分组和自动分组集合。
- `middleware/auth.go`：令牌固定分组校验。
- `middleware/distributor.go`、`service/channel_select.go`：中继路由。
- `controller/group.go`、`controller/model.go`、`controller/pricing.go`、`controller/user.go`：页面和 API 可见性。

## 验证

```powershell
go test ./service ./middleware ./controller
```
