# 渠道成本统计

渠道成本统计记录渠道和 API Key 的实际成本、解析状态、成功率和性能指标，管理入口为 `/channel-monitor`，成本接口为 `/api/channel_monitor/cost`。

## 成本口径

- `ChannelDailyCost` 按北京时间自然日保存渠道总成本、探针成本、分组探针成本、模型检测成本、已结算数和未解析数。
- `ChannelDailyAPIKeyCost` 按渠道、日期和入站 API Key 指纹保存明细；无法关联的金额归入未识别 API Key，不静默丢失。
- 普通请求在开始时冻结渠道成本倍率和 QPU，结算时按实际用量分类为 settled 或 unresolved。非权威用量、缺少配置、NaN/Inf、负值和溢出不会被猜测为金额。
- 异步任务使用 `ChannelTaskCostEvent` 幂等登记初始成本，最终结算按原始日期和绝对目标修正日账及 API Key 账。

## 批处理和失败边界

普通请求成本默认进入独立的成本 Stream，并由成本 consumer 写入数据库 outbox；Redis 未接受时在有界超时内直接写数据库 outbox。outbox 以事件 ID 幂等去重，日账本更新和 outbox 完成状态在同一事务中提交，进程或 Redis 重启后由恢复任务继续处理。

`CHANNEL_DAILY_COST_RELIABLE_OUTBOX=false` 只作为整批紧急回滚路径，回到旧的内存 batcher；该路径不是默认语义，且仍需保留可靠链路已经写入的 Stream、outbox 和账务记录。成本可靠接受最多等待 250ms 的 Stream 写入和 750ms 的数据库 outbox fallback，二者均失败才报告可靠写入失败。

探针、分组探测和模型检测成本保留来源标记，并按实际物理渠道写入日账。Redis 实时投影不是金额事实源。

## 统计接口

成本接口返回今日、昨日和日期区间汇总、渠道排序、API Key 明细、已结算/未解析数量、解析率和数据完整性提示。金额及计数聚合拒绝负值并检查 `int64` 溢出。

性能和成功率数据由 Redis 实时投影与数据库分钟聚合共同提供；实时窗口缺少有效分母时显示无样本，不伪造零成功率。详见[实时监控](realtime-monitoring.md)。

## 数据边界

逻辑归组不生成逻辑组成本或余额汇总，成本始终归属实际物理渠道。渠道监控成本不改变用户扣费、充值或订阅结算。
