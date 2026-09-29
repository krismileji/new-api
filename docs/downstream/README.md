# 下游功能文档

本目录介绍 new-api 下游分支的功能与用法。上游项目为 QuantumNous/new-api，通用功能参阅项目 README 和官方文档。

## 从这里开始

- [使用指南](integration-guide.md)：功能依赖、启用顺序、多实例限制和关闭后的行为。
- [配置参考](configuration-reference.md)：环境变量、Option、配置入口与默认值。

## 功能目录

| 功能 | 内容 |
| --- | --- |
| [渠道监控](channel-monitor/README.md) | 上游同步与账户、自动任务、变量、余额预估、成本、利润与分析、智能调度、探测和共享限流 |
| [逻辑归组](channel-logical-group/README.md) | 多个物理渠道共享调度、探测与模型检测身份 |
| [使用日志](usage-logs/README.md) | 用户侧预览、管理员诊断范围、脱敏和费用来源展示 |
| [中继可靠性](relay-reliability/README.md) | 失败切换、快速失败重试、错误与响应模型诊断和响应头超时 |
| [管理员分组访问](admin-group-access/README.md) | 管理员可用分组及模型广场的权限过滤 |
| [分组说明保留](group-pricing-descriptions.md) | 取消用户可选、切换 JSON 或改名后仍保留分组说明 |
| [图像生成定价保护](image-pricing-guard/README.md) | 原始模型未配置图像倍率时的请求保护，包含图片任务插件入口 |
