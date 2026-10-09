# 渠道监控

Root 管理入口为 `/channel-monitor`，API 前缀为 `/api/channel_monitor`。从[监控总览](dashboard.md)了解常用操作、移动端弹窗滚动、截图隐私模式、运行记录与配置管理入口，从[使用指南](../integration-guide.md)了解功能启用顺序。

## 上游配置与余额

- [上游同步](upstream-sync.md)：New API、Sub2API、自定义接口、倍率换算与自动处置。
- [共享上游账户](upstream-accounts.md)：共用余额池、余额刷新跟随渠道监控设置、账户配置保存与关联渠道确认，渠道独立保留倍率和转发 Key。
- [余额预估与恢复](balance-estimation.md)：未启用渠道的首字等待优化、请求占用、Responses 单次生成的完成确认、近期均值、独立于用户预扣的成本预算、同步边界、自动禁用后完成当前 WebSocket 生成并关闭、恢复条件。
- [独立请求与变量](custom-variable-request.md)：登录取值、变量模板与刷新策略。
- [共享请求与变量](shared-variables.md)：多渠道和自动任务共用请求、凭据及变量。
- [上游自动任务](upstream-automation.md)：独立检查、条件触发、次数重置、待确认处理与旧规则迁移。

## 统计与运行状态

- [成本统计](cost-statistics.md)：金额口径、费用分类、流式及重排估算用量、任务表达式结算与历史查询。
- [利润统计](profit.md)：平台 1:1 收入口径、暂估利润与统一展示、折叠统计说明、任务退款与补扣核对、预扣及最终资金与收入共同提交、中断请求保留预扣待人工核对、资金/成本/缓存/退款独立后台恢复与跨批扫描、订阅退款周期与旧记录边界、统一成本快照、跨日缺口归属、缺口目录异常降级与多节点共享配置、独立渠道行查询重试、跨日汇总与逐日独立确认、已确认亏损筛选与逐级明细、历史范围。
- [分析与下钻](analytics.md)：渠道、用户、API Key、智能调度探测与模型测试合并、模型、分页、按查询范围判断待处理成本、指标分母与历史回填。
- [刷新与数据状态](realtime-monitoring.md)：刷新方式、日统计共享初始化、数据库临时基线、统计时间和覆盖提示。
- [统计口径与完整性](data-consistency.md)：跨日、数据缺口、清理与不同共享关系的边界。
- [运行状态与诊断](recovery-troubleshooting.md)：健康状态、今日诊断、系统任务历史筛选与清理、异常处理和告警。

## 路由与限流

- [智能调度](smart-scheduling.md)：准入、Responses WebSocket 首次生成的输入规模选路、删除策略后的状态清理与默认选路恢复、稳定性保护与立即摘除开关、全降级时按最高评分托底（有效期跟随实时样本保留时间）、托底状态与预计流量展示、关闭保护后的状态恢复、运行快照、固定主渠道与流量/429 暂停（支持永久）、重试和写回冲突。
- [调度算法](scheduling-algorithm.md)：评分、经济分类、稳定性、429 冷却、采样与探索。
- [渠道并发与 RPM](channel-concurrency.md)：物理渠道租约、Responses WebSocket 逐请求准入、用量、满载重选与等待。
- [共享上游限流](shared-limits.md)：组总额、资源等级预留、优先级等待与在线配置。
- [用户 API Key 自动禁用](token-auto-disable.md)：错误规则、同 Key 在途取消、Responses WebSocket 生成及空闲期保护、持久化与解除。

## 探测与检测

- [状态探测](status-probe.md)：渠道模型的定时/手动探测、状态与调度样本。
- [模型广场分组监控](model-market-monitoring.md)：分类、独立开关、无可用路由与配置失效的区分、展示周期、以用户 API Key 最高值展示缓存率、采集阈值、Redis 增量窗口、按分组更新快照、去重期限与配置变更重置。
- [探测策略与业务周期监测](probe-policy.md)：禁止自动探测、小输入本地响应和 Redis 被动监测。
- [模型检测](model-detection.md)：独立检测器、模型核验、证据与成本。
- [连通性测试](connectivity-test.md)：单次、批量、并发循环测试，沿用渠道视图排序。
- [全局本地探针响应](local-probe-response.md)：公开接口多组输入与输出一一对应的本地回应，包含 Responses 插件模型，与渠道小输入策略分别配置。

## 权限与数据边界

管理 API 统一使用 RootAuth 并禁止 HTTP 缓存。用户接口 `/api/pricing/group-monitor` 遵循 pricing 模块的访问要求，展示配置中的分类与分组；这不授予模型调用权限，也不暴露渠道、Key、成本和错误详情。

逻辑归组、共享余额账户、共享限流组是三种独立关系，不会自动互相建立。逻辑归组只共享调度、探测和检测身份；成本仍归属物理渠道，余额仅在显式关联账户后汇总，并发与 RPM 始终按物理渠道登记。

功能依赖见[使用指南](../integration-guide.md)，参数见[配置参考](../configuration-reference.md)。
