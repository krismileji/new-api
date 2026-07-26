# 本地探针响应

渠道监控可以通过 `ChannelMonitorProbeResponseEnabled` 开启本地探针响应，默认关闭。开关位于“渠道监控设置 > 探针响应”。

## 命中规则

本地响应仅处理 `/v1/responses` 和 `/v1/chat/completions`，并要求请求是单轮纯文本探针：

- 唯一的用户输入在去除首尾空白后等于 `hi`，匹配不区分大小写。
- 允许请求携带 system、developer 或 Responses `instructions` 指令。
- 存在历史 assistant 消息、多个 user 消息、`previous_response_id`、conversation、图片、文件、音频、工具结果或其他文本时不命中。
- 其他端点和未命中的请求继续执行正常渠道选择、计费和中继流程。

渠道管理和渠道监控发起的连通性测试直接调用渠道适配器，不经过公开中继入口，因此始终真实请求上游，不会被本功能误判为成功。

## 返回行为

命中后，服务随机等待 `0.5-2` 秒并固定返回：

```text
Hi. What are you working on?
```

Responses API 的非流式返回模拟真实 `gpt-5.6-sol` 响应字段，包括完整 usage、tool_usage、moderation 和惩罚参数；流式返回按 Responses 协议依次发送 created、in_progress、output item、content part、text delta/done 和 completed 事件，并带连续 sequence_number。Chat Completions 返回 assistant message。两种接口同时支持流式和非流式请求，响应中的 model 沿用客户端请求值，usage 使用真实样本的模拟值：输入 4387、缓存写 172、缓存命中 4001、输出 12、总计 4399。

等待过程监听客户端请求上下文。客户端断开后立即停止，不继续占用计时器或写响应。

## 请求链路

拦截作为独立中间件运行在 API Key 鉴权和模型请求限流之后、渠道分配之前。中间件会先完成目标接口的 JSON 解析与请求校验；命中请求仍需通过正常 API Key 鉴权和请求限流，但不会：

- 选择或占用渠道并发额度；
- 请求任何上游；
- 扣减用户额度；
- 写入消费日志或渠道成本统计。

普通 HTTP 访问日志仍按全局中间件配置记录。

## 实现位置

- `pkg/channelprobe/response.go`：独立中间件、匹配规则、可取消延迟以及 Responses/Chat 响应生成。
- `router/relay-router.go`：鉴权及模型限流后、渠道分配前的单一挂载点。
- `controller/channel_ratio_monitor_settings.go`：开关读取与管理接口。
- `web/src/features/channel-monitor/components/channel-monitor-probe-response-fields.tsx`：管理端独立设置页签。
