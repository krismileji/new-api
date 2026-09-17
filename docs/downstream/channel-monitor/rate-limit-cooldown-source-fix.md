# 智能调度误报 429 冷却修复

## 原因与行为

非 429 的上游错误达到稳定性保护阈值后，会先保存降级状态，再启动一段等待路由缓存同步的临时冷却。该期限为 `SyncFrequency + 5` 秒，默认 65 秒。原实现把它写入与真实 429 相同的冷却记录，监控接口因而返回 `rate_limit_cooldown_until`，页面显示“429 冷却”。两种冷却重叠时，稳定性保护还会延长页面显示的 429 期限。

修复后，两种来源分别保存期限及 Redis 事件顺序。路由选择、探测避让和暂停限制继续考虑原有保护；监控响应的 `rate_limit_cooldown_until` 和 `rate_limit_cooling_down` 只反映真实限流冷却，稳定性保护通过已有降级状态展示。未修改 SQL 表结构、迁移、持久化路由保护逻辑或前端文案。

旧的 Redis 记录没有来源信息，升级时保留至原期限自然到期。多实例应全部更新，旧实例仍可能产生原格式的稳定性冷却记录。

## 验证记录

日期：2026-09-17。工具链：Go 1.25.1，Windows amd64。

工作区同时存在其他任务的未提交修改，验证使用基线 `00c966e55259ceb147e87a3775fd4e525877934e` 的临时独立工作区，仅叠加本次四个生产文件和两个回归测试文件。下列 `go` 对应 `D:/go-toolchain-1.25.1/go/bin/go.exe`，`GOWORK=off`。

| 验证环境 | 精确版本 | 结果 |
| --- | --- | --- |
| SQLite + 真实 Redis | SQLite 3.50.4；Redis 8.10.1 | 四项接口回归通过 |
| MySQL + 真实 Redis | MySQL 5.7.44，数据库字符集 utf8mb4；Redis 8.10.1 | 四项接口回归通过 |
| PostgreSQL + 真实 Redis | PostgreSQL 9.6.24；Redis 8.10.1 | 四项接口回归通过 |
| 本地缓存与 Redis 服务层 | 本地缓存、miniredis；另使用真实 Redis 8.10.1 | 冷却来源、期限隔离、事件重放、同步、暂停和清理通过 |

接口回归分别覆盖直接执行、Redis 事件执行下的 503 稳定性保护和真实 429，检查精确模型及通配模型响应，且确认流量避让仍生效。

执行命令：

```powershell
go test ./service -run '^TestChannel(RateLimit|StabilityBridge)' -count=1

go test ./controller -run '^Test(SmartScheduleCooldownResponseDistinguishesStabilityProtectionFrom429|ProtectChannelSmartScheduleRuntimeFailure|ChannelSmartScheduleRouteResponsesExpose|RunChannelSmartScheduleProbeRateLimit|RunChannelSmartScheduleProbeSkipsActive429)' -skip '^TestProtectChannelSmartScheduleRuntimeFailure(IgnoresMinimumSamples|DoesNotRecountPersistedErrors)$' -count=1

# 临时测试夹具只替换数据库、Redis 连接和必要初始化，不替换生产逻辑。
foreach ($engine in @('sqlite', 'mysql', 'postgres')) {
  $env:COOLDOWN_TEST_DATABASE = $engine
  go test -overlay ../controller-overlay.json ./controller -run '^TestSmartScheduleCooldownResponseDistinguishesStabilityProtectionFrom429$' -count=1 -v
}

go test -overlay ../service-overlay.json ./service -run '^TestChannelStabilityBridge' -count=1 -v
```

夹具生成脚本及原始三库/Redis 输出保存在本地 `.local-tests/429-cooldown-verification/`。测试使用独立容器与空数据库，不访问业务实例。MySQL 使用独立的 `new_api_cooldown_test` 数据库，预先设置 utf8mb4；SQL 连接列引用按数据库类型初始化。

扩展检查中的以下两个既有测试仍失败；用修改前的四个生产文件覆盖后可复现同样失败，因此未随本次修复改动。上面的通过命令明确排除了这两项，不能据此声称完整测试集通过。

- `TestProtectChannelSmartScheduleRuntimeFailureIgnoresMinimumSamples`
- `TestProtectChannelSmartScheduleRuntimeFailureDoesNotRecountPersistedErrors`

## 下游范围

本次修改的四个既有生产文件均不存在于本地 `upstream/main`，属于下游功能；没有修改上游所有的文件。新增两个回归测试和本说明。`git diff --check` 通过。
