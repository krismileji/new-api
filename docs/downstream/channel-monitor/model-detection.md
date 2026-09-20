# 渠道模型检测

渠道模型检测由独立检测器提供模型能力判断，管理接口位于 `/api/channel_monitor/model_detection`，内部中继接口为 `/internal/model-detector/v1/responses`。管理接口使用 RootAuth。

## 检测方式

配置独立检测器后，可以查看服务状态、预估检测费用、执行或取消检测，并查看历史报告。检测器通过内部中继访问指定渠道，管理响应不返回渠道 Key、会话 Token 或代理 Token。

单渠道检测始终使用该渠道；逻辑组检测可在本轮确定的成员内，按可用性和权重选择实际渠道。

## 设置和轮次

检测器地址来自数据库设置；环境变量 `GPT56_DETECTOR_URL` 只在数据库没有设置时提供初始默认值。地址校验限制私网/回环解析、禁止混合公网结果，并要求内部 Relay 路径符合约定。设置使用 revision，活动轮次结束前不会切换正在使用的地址。

定时任务按配置间隔创建检测批次，避免多节点重复执行。正在运行的轮次可以查看进度或取消；超时轮次会结束并恢复后续调度。

检测器需支持 `schema_version=2` 的初始化协议。检测报告必须对应本轮的声明模型和实际请求模型。

## 成本和清理

检测成本归属实际物理渠道，无法确认的费用标记为未解析。清理仅处理已结束且费用已确认的历史，不删除正在处理或待结算的费用。

## 管理接口

- `GET /api/channel_monitor/model_detection`
- `GET /api/channel_monitor/model_detection/settings`
- `PUT /api/channel_monitor/model_detection/settings`
- `GET /api/channel_monitor/model_detection/service`
- `POST /api/channel_monitor/model_detection/service/test`
- `PUT /api/channel_monitor/model_detection/channel/:id/config`
- `POST /api/channel_monitor/model_detection/channel/:id/estimate`
- `POST /api/channel_monitor/model_detection/channel/:id/run`
- `GET /api/channel_monitor/model_detection/channel/:id/runs`
- `GET /api/channel_monitor/model_detection/runs/:run_id`
- `POST /api/channel_monitor/model_detection/runs/:run_id/cancel`
- 内部中继 `POST /internal/model-detector/v1/responses`

## 探测策略与共享限额

“禁止自动探测”会阻止相应自动请求；手动执行仍真实访问上游。请求始终按实际物理渠道执行准入，加入共享限流组后还受组总额与资源预留约束。禁探测渠道的业务周期数据见[探测策略](probe-policy.md)，限额口径见[共享限流](shared-limits.md)。
