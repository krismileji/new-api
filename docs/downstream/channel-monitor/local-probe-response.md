# 本地探针响应

渠道监控可以通过 `ChannelMonitorProbeResponseEnabled` 开启本地探针响应，默认关闭。开关和响应参数位于“渠道监控设置 > 探针响应”。历史部署未保存新参数时，会继续使用下表默认值。

## 命中规则

本地响应仅处理 `/v1/responses` 和 `/v1/chat/completions`，并要求请求是单轮纯文本探针：

- 唯一的用户输入在去除首尾空白后等于配置的“匹配输入”（默认 `hi`），匹配不区分大小写。
- 允许请求携带 system、developer 或 Responses `instructions` 指令。
- 存在历史 assistant 消息、多个 user 消息、`previous_response_id`、conversation、图片、文件、音频、工具结果或其他文本时不命中。
- 其他端点和未命中的请求继续执行正常渠道选择、计费和中继流程。

渠道管理和渠道监控发起的连通性测试直接调用渠道适配器，不经过公开中继入口，因此始终真实请求上游，不会被本功能误判为成功。它们属于 automated probe，始终按测试链路写入带监控标记的消费日志，并记录渠道成本；本地响应开关不会改变这些后台探测的真实请求行为。

## 返回行为

命中后，服务在配置的最小和最大延迟（默认 `500-2000` 毫秒）之间随机等待，并返回配置的响应文本（默认）：

```text
Hi. What are you working on?
```

Responses API 的非流式返回按请求模型填充响应字段，包括完整 usage、tool_usage、moderation 和惩罚参数，不固定为某个模型；流式返回按 Responses 协议依次发送 created、in_progress、output item、content part、text delta/done 和 completed 事件，并带连续 sequence_number。Chat Completions 返回 assistant message。两种接口同时支持流式和非流式请求，usage 的输入、缓存写、缓存命中和输出 Token 都可配置，总 Token 自动按输入加输出计算；默认值分别为 `4387`、`172`、`4001`、`12`，总计 `4399`。

配置对应的系统 Option 和默认值如下：

| 管理端字段 | Option 键 | 默认值 | 有效范围 |
| --- | --- | ---: | --- |
| 匹配输入 | `ChannelMonitorProbeResponseMatchInput` | `hi` | 去首尾空白后不能为空，最长 4096 个字符 |
| 响应文本 | `ChannelMonitorProbeResponseText` | `Hi. What are you working on?` | 去首尾空白后不能为空，最长 16384 个字符 |
| 最小延迟 | `ChannelMonitorProbeResponseMinDelayMilliseconds` | `500` | `0..600000` 毫秒，不能大于最大延迟 |
| 最大延迟 | `ChannelMonitorProbeResponseMaxDelayMilliseconds` | `2000` | `0..600000` 毫秒，不能小于最小延迟 |
| 输入 Token | `ChannelMonitorProbeResponseInputTokens` | `4387` | `0..1000000` |
| 缓存写 Token | `ChannelMonitorProbeResponseCacheWriteTokens` | `172` | `0..1000000` |
| 缓存命中 Token | `ChannelMonitorProbeResponseCachedTokens` | `4001` | `0..1000000` |
| 输出 Token | `ChannelMonitorProbeResponseOutputTokens` | `12` | `0..1000000` |

等待过程监听客户端请求上下文。客户端断开后立即停止，不继续占用计时器或写响应。

## 请求链路

拦截作为独立中间件运行在 API Key 鉴权和模型请求限流之后、渠道分配之前。中间件会先完成目标接口的 JSON 解析与请求校验；命中请求仍需通过正常 API Key 鉴权和请求限流，但不会：

- 选择或占用渠道并发额度；
- 请求任何上游；
- 扣减用户额度；
- 写入消费日志或渠道成本统计。

普通 HTTP 访问日志仍按全局中间件配置记录。
