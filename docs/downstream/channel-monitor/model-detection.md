# 渠道模型检测

渠道模型检测由独立检测器提供模型能力判断，管理接口位于 `/api/channel_monitor/model_detection`，内部中继接口为 `/internal/model-detector/v1/responses`。管理接口使用 RootAuth。

## 请求链路

Worker 调用检测器的 health、bootstrap、estimate、start 和 status 接口；检测器通过内部中继请求固定渠道，内部中继使用短期 Bearer。渠道 Key、会话 Token 和代理 Token 不写入数据库或管理响应。

内部中继会冻结物理渠道、声明模型、请求模型和执行快照。物理单渠道执行保持固定；逻辑组快照允许在冻结成员内按可用性和 weight 重选物理成员。

## 设置和轮次

检测器地址来自数据库设置；环境变量 `GPT56_DETECTOR_URL` 只在数据库没有设置时提供初始默认值。地址校验限制私网/回环解析、禁止混合公网结果，并要求内部 Relay 路径符合约定。设置使用 revision，活动轮次结束前不会切换正在使用的地址。

定时任务按配置间隔创建批次，使用数据库 lease 和唯一 scheduled_for 防止多节点重复。Worker 使用进程和数据库 lease 管理 start、resume、status、report、stop；旧活动轮次会超时并恢复。

bootstrap 要求 `schema_version=2`。完成报告保存未知字段，但必须与执行快照的 `claimed_model` 和 `request_model` 一致；报告 schema 和评分版本不使用文档白名单限制。

## 成本和清理

检测请求的 prepared、dispatched、settled 和 unresolved 状态分别记录；成本归属实际物理渠道。只清理 terminal/resolved 的历史，prepared 或 pending 成本不会被直接删除。
