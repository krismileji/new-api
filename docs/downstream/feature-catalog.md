# 二开功能总览

## 当前功能

| 功能 | 状态 | 主要入口 | 详细文档 |
| --- | --- | --- | --- |
| 渠道监控与智能调度 | 使用中 | 管理端 `/channel-monitor`、`/api/channel_monitor` | [渠道监控索引](channel-monitor/README.md) |
| 渠道上游倍率与余额同步 | 使用中 | 渠道监控的“上游配置与策略” | [上游同步与策略](channel-monitor/upstream-and-policies.md) |
| 渠道成本、性能和成功率统计 | 使用中 | 渠道监控的渠道、分组、模型和成本视图 | [成本、性能与成功率](channel-monitor/cost-performance.md) |
| 渠道连通性测试与并发限制 | 使用中 | 渠道监控、渠道管理 | [测试与并发限制](channel-monitor/testing-and-concurrency.md) |
| 中继失败切换与错误日志隔离 | 使用中 | Chat Completions、Responses 和任务中继 | [中继重试与错误可见性](relay-reliability/README.md) |
| 管理员访问全部已配置分组 | 使用中 | 令牌鉴权、分组、模型和定价接口 | [管理员分组访问](admin-group-access/README.md) |
| 未定价图像生成保护 | 使用中 | `/v1/images/generations` | [图像生成定价保护](image-pricing-guard/README.md) |

## 渠道监控能力范围

渠道监控是当前最大的二开模块，包含以下能力：

- 渠道、分组和模型三个监控视图，以及筛选、搜索和自定义排序。
- New API、Sub2API 和自定义上游的倍率、余额、分组与版本读取。
- 成本倍率换算、倍率历史、低余额预警和自动禁用。
- 单渠道与多渠道分组策略，包括更新分组倍率、禁用渠道和移除分组关联。
- 自动更新、失败重试、邮件通知、失败自动禁用及条件恢复。
- 基于成本倍率、首字时间、TPS 和成功率的智能调度与稳定性保护。
- 按渠道、日期和 API Key 汇总的人民币成本统计与保留策略。
- 真实调用成功率、最终成功率、失败分类及性能指标。
- 单次、批量和并发循环连通性测试。
- 单渠道并发上限、分布式租约和满载渠道重选。
- Root 权限 API、管理操作审计和系统任务历史。

完整说明统一放在 [渠道监控目录](channel-monitor/README.md)，本文件只维护一级清单。
