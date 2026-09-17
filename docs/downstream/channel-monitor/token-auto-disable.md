# 用户 API Key 自动禁用

本功能适用于单个后端实例。Root 管理员在「渠道监控 → API Key 自动禁用」配置规则、查看禁用记录和解除禁用。默认关闭，不预置生效规则。

## 规则与操作

一条规则必须同时匹配原始上游 HTTP 状态码和错误关键词，可以限定渠道编号。状态码在渠道状态码映射之前读取，正常模型输出不会作为错误匹配。

- 按列表顺序匹配，采用第一条命中的规则；可上移、下移、单独停用或删除。
- 渠道编号留空表示所有渠道；多个状态码为任选其一。
- 错误关键词每行一个，支持任一关键词或全部关键词，并可选择区分大小写。
- 自定义返回状态码为 400–599，错误信息最长 4096 字节。
- 最多 64 条规则，序列化后的规则总量最多 65535 字节，保证三种数据库的 TEXT 存储行为一致。
- 保存采用修订号校验。遇到其他管理员同时修改时会拒绝覆盖，应重新打开弹窗加载最新配置。
- 关闭全局开关或删除规则不会解除已有禁用。必须在「禁用记录」中确认解除。

例如：上游状态码填写 `403`，关键词填写 `policy violation`，返回状态码填写 `451`，返回信息填写「此 API Key 已被自动禁用，请联系管理员」。命中后只禁用本次请求使用的 Token ID；同一用户的其他 Key 不受影响。

## 请求生命周期

认证识别 Token ID 后，将请求登记到本机的请求集合。登记和禁用共享锁，避免并发请求漏过取消。禁用先在内存中拦截新请求，再向同一 Key 的全部已登记请求发送取消信号，随后保存恢复记录和数据库状态。上游 HTTP 请求使用可取消的请求上下文；正在读取上传正文或写入下游的请求通过连接期限中断阻塞 I/O。

取消后不再重试或切换渠道，不将本次取消计入渠道故障调度。已获得的用量仍走原有结算，已结算的扣费不会被后续退款撤销，未使用的预扣额度会退回。保护产生的自定义错误不会触发额外的违规扣费。

| 请求状态 | 返回行为 |
| --- | --- |
| 新请求，或尚未发送响应头 | 自定义 HTTP 状态码和协议对应的 JSON 错误 |
| SSE 已发送响应头 | 保留已发送的 HTTP 状态，尽力发送协议对应的错误事件后结束 |
| WebSocket 已完成升级 | 尽力发送错误事件，以 1008 关闭连接，并关闭上游连接 |
| 二进制响应或连接已经不可写 | 结束响应；已经发出的数据和状态无法撤回 |

错误码使用 `api_key_auto_disabled`。Claude、Responses 和 Gemini 的错误包遵循各自的基本协议形状。WebSocket 会话内的错误以原始握手状态 `101` 匹配；SSE 会话内的显式错误以实际 HTTP 状态（通常为 `200`）匹配。

识别支持非成功 HTTP 响应的错误正文，以及 JSON/SSE 中的显式 `error`、`response.failed`、Dify `event:error`。非流式正文只观察前 1 MiB；SSE 按 `data:` 行解析，每行最多 1 MiB。正常成功内容中的同名文本不会触发。尚未读到的上游用量无法凭空还原，继续使用适配器已有的部分用量或估算规则。

上游已接受的异步生成任务不能通过断开本地 HTTP 连接撤销；是否终止远端任务取决于提供商的取消接口。当前实现不会调用额外的远端任务取消 API。

## 状态持久化与恢复

新增主数据库表 `token_auto_disable_configs` 和 `token_auto_disable_records`。记录保存 Token ID、规则快照、渠道、请求编号、脱敏错误摘要、返回配置、已通知取消的请求数、解除时间和操作者，不保存完整 API Key。

即使 Redis 缓存仍保存旧的启用状态，本机独立拦截仍然生效。普通令牌编辑接口不能绕过自动禁用；Root 管理员解除后，仅允许新请求重新认证。过期、额度不足和软删除状态仍受原有规则限制，旧请求不会复活，也不能再次影响解除后的新请求。

数据库写入失败时，本机保持拦截，并每 5 秒重试。禁用记录尚未保存时在界面显示告警，并禁止提前解除。数据库保存前使用本地恢复文件；启动时先加载数据库和恢复文件，再接受请求。无法读取状态或恢复文件损坏时启动失败。

部署时将 `TOKEN_AUTO_DISABLE_JOURNAL_DIR` 设置为持久化、可写目录，例如挂载目录 `/data/token-auto-disable-journal`。默认位置为 `SQLITE_PATH` 同级目录下的 `token-auto-disable-journal`；使用外部数据库和临时容器文件系统时，应显式配置持久化目录。如果数据库和本地磁盘同时不可写，当前进程仍拦截，但无法保证未保存的禁用跨重启保留。

本机取消不依赖 Redis 广播，因此不支持多个后端实例之间同步在途取消。扩容前必须补充跨实例禁用传播。

## 接口

全部沿用渠道监控的 `RootAuth` 权限。配置更新和解除操作写入原有操作审计。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET / PUT | `/api/channel_monitor/token_protection/settings` | 读取或按修订号更新配置 |
| GET | `/api/channel_monitor/token_protection/records?page=1` | 每页 20 条历史记录，同时返回本机待保存记录 |
| POST | `/api/channel_monitor/token_protection/records/:id/release` | 解除指定禁用记录 |

## 验证记录

2026-09-17，Windows，Go 1.26.5。验证使用独立测试库，没有改动部署数据库或启用实际规则。

| 数据库 | 实际版本 | 新建及重复启动 | 发布版升级及重复启动 | 模型行为 |
| --- | --- | --- | --- | --- |
| SQLite | 3.50.4 | 通过 | 通过 | 通过 |
| MySQL | 5.7.44 | 通过 | 通过 | 通过 |
| PostgreSQL | 9.6.24 | 通过 | 通过 | 通过 |

发布版基线为 `v1.0.0-rc.37`（`385d2dfd1`）。在实际发布版工作树运行 seed，保留令牌名称、Key、余额、已用额度和 Option；再在本次工作树执行两次 `model.InitDB`，验证记录、唯一约束和原有数据保留。另使用空库执行 fresh 和 verify。新增表仅属于主库迁移，独立日志库迁移路径未修改。

模型测试验证新增表和索引重复迁移、规则乐观锁、禁用幂等、解除冲突、规则快照保留、令牌额度不变，以及令牌 Key 和事件 ID 的唯一性。以下 DSN 中的密码仅用于本次创建的临时测试容器：

```powershell
$env:TEST_TOKEN_MYSQL_DSN='root:token-test-only@tcp(127.0.0.1:33379)/new_api_token_test?charset=utf8mb4&parseTime=True&loc=Local'
$env:TEST_TOKEN_POSTGRES_DSN='postgres://postgres:token-test-only@127.0.0.1:35439/new_api_token_test?sslmode=disable'
go test ./model -run '^TestTokenAutoDisableDatabaseMatrix$' -count=1 -v
```

升级脚本为 `scripts/token-protection-upgrade/upgrade_test.go`。原样复制到发布版工作树的同一路径，以以下命令执行。MySQL、PostgreSQL 分别使用上面的连接参数，数据库名替换为 `new_api_token_upgrade_final`；SQLite 清空 `SQL_DSN`，设置 `SQLITE_PATH=D:/temp/token-upgrade-final-sqlite.db`。

```powershell
# 发布版工作树
$env:TOKEN_UPGRADE_MODE='seed'
go test ./scripts/token-protection-upgrade -run TestTokenProtectionUpgrade -count=1 -v

# 本次工作树，使用相同数据库
$env:TOKEN_UPGRADE_MODE='verify'
go test ./scripts/token-protection-upgrade -run TestTokenProtectionUpgrade -count=2 -v

# 另一个空库：new_api_token_fresh_final
# SQLite：D:/temp/token-fresh-final-sqlite.db
$env:TOKEN_UPGRADE_MODE='fresh'
go test ./scripts/token-protection-upgrade -run TestTokenProtectionUpgrade -count=1 -v
$env:TOKEN_UPGRADE_MODE='verify'
go test ./scripts/token-protection-upgrade -run TestTokenProtectionUpgrade -count=1 -v
```

后端验证命令：

```powershell
go test ./middleware ./service ./controller ./relay/helper ./relay/channel/... -run 'TokenProtection|ShouldRetry|RelayRetry|RelayFastFailure|WriteRelayErrorResponse|StreamScanner|BillingSession|DoRequest|Aws' -count=1 -timeout 120s
go build ./...
```

覆盖状态码与错误文本同时匹配、渠道范围、规则顺序、同 Key 并发登记与取消、其他 Key 不受影响、关闭开关不解封、重启恢复、数据库故障恢复、管理员解除、真实 HTTP/1.1 / HTTP/2 的在途请求与慢上传中断、SSE 协议错误、WebSocket 关闭，以及部分用量保留和结算后不重复退款。

前端在 `web/` 下通过 `bun run typecheck`、变更文件的 `oxlint` 与 `oxfmt --check`、`bun run test src/features/channel-monitor/components/__tests__/token-protection.test.tsx`（4 个用例）以及 `bun run build`。交互覆盖新增/校验/保存、失败保留草稿和确认解除后刷新。

## 上游文件集成说明

以本地 `upstream/main` 判断所有权，新增功能集中于独立文件。必须修改的上游文件及原因如下：

| 文件 | 必要集成 |
| --- | --- |
| `main.go` | 接受请求前恢复禁用状态 |
| `model/main.go` | 注册两张主库表的迁移 |
| `middleware/auth.go` | 识别 Token 后登记、拦截并接管终止响应 |
| `controller/token.go` | 防止普通令牌编辑绕过自动禁用 |
| `controller/relay.go` | 取消后停止重试、渠道故障处理和额外违规扣费；保留退款及终止响应归属 |
| `relay/channel/api_request.go` | 通用 HTTP 原始响应和 WebSocket 握手错误观察 |
| `relay/helper/stream_scanner.go` | 停止排队中的正常流事件，区分保护取消 |
| `relay/channel/openai/relay_realtime.go` | 取消实际读写、等待读协程退出、发送终止错误和关闭帧 |
| `service/error.go` | 统一错误处理入口补充原始错误观察 |
| `service/midjourney.go` | 请求继承取消上下文，观察独立 HTTP 路径 |
| `relay/channel/aws/relay-aws.go` | SDK 错误原始 HTTP 状态观察 |
| `relay/channel/ali/image.go` | 轮询 HTTP 响应观察 |
| `relay/channel/replicate/adaptor.go` | 文件上传 HTTP 响应观察 |
| `relay/channel/dify/relay-dify.go` | 文件上传错误观察及取消后的部分用量返回 |
| `relay/channel/coze/relay-coze.go` | 独立 HTTP 路径观察，保留错误正文供原调用方读取 |

下游已有文件 `router/channel-monitor-router.go`、`service/channel_monitor_event_emit.go`、`web/src/features/channel-monitor/index.tsx` 分别增加管理路由、已结算取消事件标记和按需加载的界面入口；本目录 `README.md` 添加文档索引。`relaykit/` 未修改，未增加根模块依赖。前端遵循下游中文文案约定，未新增翻译键或修改语言文件。
