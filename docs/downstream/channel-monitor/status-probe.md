# 渠道状态探测

状态探测通过 `/api/channel_monitor/status` 查看渠道和模型健康状态，通过配置接口设置探测模型、间隔、展示窗口和是否记录样本。所有管理接口使用 RootAuth。

## 配置

配置要求启用状态、具体文本模型、30 到 86400 秒的间隔、展示数值和单位。记录样本时，间隔至少为 60 秒；模型必须属于渠道当前支持的文本模型。配置使用 revision 并发控制，旧 revision 返回冲突。

## 执行

定时探测由主节点执行。同一渠道的模型在一轮内依次探测，不同渠道可以并行。渠道并发额度不足时记录 skipped，不发送请求。逻辑归组每个目标每轮只产生一条逻辑执行结果。

探测结果包括 success、upstream_failure、rate_limited、local_failure、skipped 和 canceled。启用样本记录时，成功样本异步写入智能调度观测；样本写入失败会重试，但不会重新请求上游。探测请求沿用测试和成本链路，可能产生上游费用。

## 状态

状态按最近完成结果、配置和数据新鲜度计算：unconfigured、paused、pending、healthy、partial、unhealthy、rate_limited 和 stale。窗口数据按渠道、模型和时间桶返回；无样本不转换为成功或失败。

## 管理接口

- `GET /api/channel_monitor/status`
- `PUT /api/channel_monitor/status/channel/:id/config`
- `POST /api/channel_monitor/status/channel/:id/run`
- `GET /api/channel_monitor/status/channel/:id/executions`

## 探测策略与共享限额

“禁止自动探测”会阻止相应自动请求；手动执行仍真实访问上游。请求始终按实际物理渠道执行准入，加入共享限流组后还受组总额与资源预留约束。禁探测渠道的业务周期数据见[探测策略](probe-policy.md)，限额口径见[共享限流](shared-limits.md)。
