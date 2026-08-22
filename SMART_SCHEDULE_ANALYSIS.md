# 智能调度异常分析

## 现象

- “预计流量分布”长期只有一个渠道。
- 探索流量没有把备用渠道提升到主渠道同层。
- 稳定性保护和自适应备援看起来没有产生运行时调整。
- 智能调度执行记录显示成功。

## 当前结论

最新线上截图确认了一个与探索流量、稳定性保护和自适应备援无关的独立缺陷：**固定主渠道在完整调度更新池内基础优先级后，没有按新的最高优先级重新置顶。**

截图中的实际状态是：

- `md`：基础 P/W 为 `P3 / W1000`，当前 P/W 为 `P3 / W1000`。
- 已固定的 `co`：基础 P/W 为 `P2 / W1000`，当前 P/W 为 `P3 / W1000`。
- 两个渠道因此同时处于实际最高层 `P3`，且权重相同，页面按真实 Ability P/W 正确计算为各 `50%`。
- 两个渠道都显示“当前未采样”，`co` 也没有稳定性降级，所以本次 `50% / 50%` 不是探索、稳定性或自适应备援造成的。

按照固定主渠道的现有语义，`co` 应始终高于池内其他参与渠道的当前最高优先级。既然 `md` 已是 `P3`，`co` 应被重新提升为 `P4 / W1000`，预计流量应为 `co 100%`。

执行记录在故障期间持续成功，且记录内确实存在有效的 `Planned` 或 `Updated` 路由时，可以排除“智能调度配置整体无效”作为本次故障的直接原因。

完整调度成功只证明基础评分、基础排名和基础 P/W 计算能够执行。探索流量、自适应备援、运行时稳定性保护属于另一条请求事件驱动链路，完整调度任务成功不能证明这些运行时功能生效。

在 `priority_weight` 模式下，完整调度本来就会生成唯一最高优先级主渠道，因此基础状态显示单渠道 `100%` 并不异常。只有探索或自适应临时流量生效后，备用渠道才会被临时提升到最高优先级层，预计流量分布才会同时出现两个渠道。

综合现有证据，本次问题应优先定位 Redis 请求事件到运行时路由覆盖的链路，而不是基础调度任务。

上述判断仍适用于“探索、稳定性、自适应备援同时长期不生效”的原始现象；但最新截图中的“固定 `co` 后仍为 `50% / 50%`”已经由固定主渠道重放缺陷单独解释，不需要 Redis 运行时链路异常这一前提。

## 本次已实施修复

已在代码中完成固定主渠道重放修复，并修复了另外两项确定性风险：

- `model/channel_smart_schedule_route.go`：完整调度整池结果写入后，在同一事务内调用固定主渠道重放；运行时自适应覆盖（`AdaptiveOverlayOnly = true`）不调用该逻辑。
- `model/channel_smart_schedule_primary_reapply.go`：保留原有函数签名，增加带变更路由键的内部实现，使固定渠道重放或临时流量清理造成的 Ability P/W 变化能够传递为 `RoutingChanged`，从而刷新控制器缓存。
- `controller/channel_ratio_monitor_schedule_route.go`：固定主渠道目标优先级只统计参与智能调度、启用且未暂停的路由；`Excluded` 或未纳入智能调度的手工渠道不会再抬高固定渠道目标。
- `model/channel_smart_schedule_traffic_policy.go` 与 `controller/channel_ratio_monitor_runtime_settings_cache.go`：请求选路与调度配置共用完整策略有效性校验；无效配置统一 fail-closed。
- `model/channel_smart_schedule_option_migration.go` 与 `main.go`：启动时迁移缺少稳定性窗口的旧策略，默认补回 5 分钟。

验证结果：

- `go test ./model -run 'TestApplyChannelSmartScheduleRouteResults|TestSaveChannelSmartScheduleRoutePrimary|TestProtectFixedPrimary' -count=1` 通过。
- `go test ./controller -run 'TestRunChannelSmartScheduleFixedPrimaryIgnoresUnmanagedManualRoute|Test.*SmartSchedule' -count=1` 通过。
- 新增回归测试覆盖：完整调度提升其他参与渠道后，固定渠道自动重新置顶为更高 P/W，并将 `RoutingChanged` 标记为 `true`。
- 全量 `go test ./controller` 通过；相关 `model` 测试通过。
- 全量 `go test ./model` 通过；两个既有路由继承/参与变更测试已同步到当前生产契约。
- `TestMigrateChannelSmartScheduleGroupPoliciesAddsMissingStabilityWindows`、`TestMigrateChannelSmartScheduleGroupPoliciesPreservesMalformedJSON` 通过，确认迁移可重复执行且不会覆盖损坏配置。

该修复不会改变探索、自适应备援或稳定性保护的触发条件；发布后仍需对已出现并列最高层的受影响池执行一次完整智能调度，或等待下一次完整调度周期，以重放固定主渠道并刷新缓存。

## 固定主渠道未重新置顶

### 正确计算已经存在

设置固定主渠道时，`model/channel_smart_schedule_route.go` 中的 `channelSmartScheduleManualPrimaryPriority()` 会读取同池其他参与渠道的最高当前优先级，并返回：

```text
max(其他渠道最高优先级 + 1, 最低目标优先级)
```

完整调度规划在 `controller/channel_ratio_monitor_schedule_route.go` 中也会先计算所有参与路由的基础优先级最大值，再将固定渠道的目标优先级至少设为该最大值加一。因此截图所示基础状态下，规划目标应为：

```text
md: 基础 P3 / W1000
co: 固定目标 P4 / W1000
```

### 完整调度写回时丢失了这次置顶

问题发生在完整调度结果写回阶段：

1. `controller/channel_ratio_monitor_schedule_route.go` 将有效的 `ManualPrimaryUntil` 视为运行时覆盖。
2. 只要固定仍有效，`ApplyPriorityWeight` 就会被设为 `false`，目的是避免完整调度直接覆盖运行时状态。
3. `model/channel_smart_schedule_route.go` 的 `ApplyChannelSmartScheduleRouteResults()` 仍会成功保存新的 `BaseRank`、`BasePriority`、`BaseWeight` 和规划出的 `LastSchedulePriority`。
4. 但该事务结束前没有调用已经存在的 `reapplyChannelSmartScheduleRoutePrimariesTx()`，因此规划出的固定目标 P/W 没有重新写回 Ability。

这会形成截图中的状态分裂：调度记录成功、决策原因也知道 `co` 是固定主渠道，但 Ability 仍保留固定发生时的旧 `P3`；与此同时，`md` 的基础和当前优先级已经升到 `P3`，最终两者并列最高层。

### 已存在的重放函数能处理该状态

`model/channel_smart_schedule_primary_reapply.go` 中的 `reapplyChannelSmartScheduleRoutePrimariesTx()` 会重新读取整池状态和 Ability，计算“其他渠道最高 P + 1”，再把固定渠道写为 `W1000`。该函数已经用于渠道状态、Ability 状态和渠道编辑等变更路径，但完整调度整池结果写回后没有调用它。

因此这不是前端预计流量算法错误。前端只在实际最高优先级层内按当前权重分配预计流量；当前两个渠道均为 `P3 / W1000`，显示各 `50%` 符合当前数据库路由状态。

### 最小修复与回归条件

最小修复应保证完整调度原子写入整池基础状态后，在同一事务内重新应用仍有效的固定主渠道，使固定渠道相对最新池状态始终唯一置顶。

### 具体改法（推荐实现）

#### 1. 在完整调度事务内重放固定主渠道

修改 `model/channel_smart_schedule_route.go` 的 `ApplyChannelSmartScheduleRouteResults()`：在整池 `states` 已经写入、但调用 `advanceChannelMonitorRedisEffectStateTx()` 之前，增加：

```go
if poolGuarded && !adaptiveOverlayOnly {
	changedKeys, err := reapplyChannelSmartScheduleRoutePrimariesTxWithChanges(
		tx,
		[]channelSmartScheduleRoutePool{{group: group, model: modelName}},
	)
	if err != nil {
		return err
	}
	for index := range outcomes {
		if _, changed := changedKeys[outcomes[index].Key]; changed {
			outcomes[index].RoutingChanged = true
		}
	}
}
```

这里必须满足三个条件：

- 只对完整调度执行，即 `AdaptiveOverlayOnly == false`。
- 必须在同一个 GORM 事务中执行，固定重放失败时整池基础状态和 Ability P/W 一起回滚。
- 必须放在所有路由状态保存之后，这样 `reapply` 读取到的是本轮最新的 `BasePriority`、`BaseWeight` 和 `LastSchedulePriority`。

不要简单地把固定渠道的 `ApplyPriorityWeight` 改回 `true`。固定期间当前 Ability 可能还叠加了探索、自适应或稳定性运行时覆盖，直接写入规划目标会覆盖这些运行时状态。

#### 2. 给现有重放函数增加“变更键”返回值

现有 `reapplyChannelSmartScheduleRoutePrimariesTx()` 只有 `error` 返回值，但控制器需要知道它是否修改了 Ability，才能刷新池缓存。建议保留旧函数作为兼容包装，再增加一个内部函数：

```go
func reapplyChannelSmartScheduleRoutePrimariesTx(
	tx *gorm.DB,
	pools []channelSmartScheduleRoutePool,
) error {
	_, err := reapplyChannelSmartScheduleRoutePrimariesTxWithChanges(tx, pools)
	return err
}
```

`...WithChanges` 返回 `map[ChannelSmartScheduleRouteKey]struct{}`。在现有 `updateAbilitySmartSchedulePriorityWeightTx()` 实际成功修改固定主渠道时，把该路由键加入集合；如果该函数同时清理了池内临时流量，也应把被修改的路由键一并加入集合。这样已有的渠道编辑、状态变更等调用方无需改签名，完整调度则可以精确设置 `outcomes[index].RoutingChanged`。

如果实现阶段不想增加变更键集合，至少在完整调度成功且池内存在有效固定主渠道时保守地将 `cacheDirty` 设为 `true`；不能只写数据库而不刷新缓存，否则页面和请求选路可能继续看到旧的 `P3 / W1000`。

#### 3. 复用现有置顶算法，不新增另一套优先级计算

固定渠道的目标仍由 `reapplyChannelSmartScheduleRoutePrimariesTx()` 内已有逻辑计算：

```text
max(其他参与且启用渠道的当前最高 P + 1,
    固定渠道当前 P,
    固定渠道本轮 LastSchedulePriority)
```

然后通过现有的 `updateAbilitySmartSchedulePriorityWeightTx()` 写入 `P/W`，并同步更新固定路由状态的 `LastSchedulePriority`、`LastScheduleWeight`、`LastScheduleStatus`、`LastScheduleTime` 和 `Revision`。不要在控制器中直接更新 `abilities` 表，也不要用固定时保存的旧 P/W 覆盖本轮基础状态。

#### 4. 不要在运行时自适应覆盖路径调用重放

`controller/channel_ratio_monitor_schedule_runtime.go` 传入的 `AdaptiveOverlayOnly == true` 属于探索、自适应备援或稳定性运行时写入。这条路径必须继续只更新临时覆盖和健康字段，不能调用固定主渠道重放，否则可能清除临时流量或把运行时 P/W 提前恢复成基础值。完整调度和运行时覆盖必须保持这条边界。

#### 5. 缓存与执行记录处理

控制器 `controller/channel_ratio_monitor_schedule_route.go` 已根据 `outcomes[index].RoutingChanged` 设置 `cacheDirty`，并在任务结束时调用 `model.InitChannelCache()`。因此上述“变更键 -> `RoutingChanged`”传递必须保留；否则数据库虽然已经变为 `co P4`，进程内路由池仍可能短时间显示旧值。重放错误应继续向上返回，让本轮整池执行记录进入失败，而不是记录成功但只写入部分状态。

回归测试至少覆盖：

1. `co` 固定时当前为 `P3`。
2. 下一次完整调度把另一参与渠道的基础和当前优先级提升到 `P3`。
3. 调度提交后断言 `co` 自动变为 `P4 / W1000`，另一渠道仍为 `P3 / W1000`。
4. 断言实际最高层仅包含 `co`，预计流量为 `co 100%`。
5. 断言 `co` 对应的 `ApplyOutcome.RoutingChanged` 为 `true`，从而控制器刷新池缓存。
6. 使用 `AdaptiveOverlayOnly == true` 的运行时覆盖结果回归一次，确认该路径不会执行固定主渠道重放，也不会清掉备用渠道的临时流量。

### 上线后的存量修复

该修复不会自动把已经错误保存的 `co P3 / md P3` 立即改正。发布后应对受影响的 `(group, model)` 手动触发一次完整智能调度，或者等待下一次完整调度周期；不要直接手工改 `abilities.priority`，以免遗漏路由状态修订号和缓存刷新。

验收标准：

- 固定 `co` 的池显示 `co` 为实际最高层，`P4 / W1000`（具体 P 值随池内最高层变化）。
- `md` 保持基础层 `P3 / W1000`，预计流量为 `co 100%`、`md 0%`。
- 执行记录的 `Failed = 0`，且固定渠道的调整明细显示已重新置顶或 `Unchanged`。
- 随后触发一次确有样本欠账或健康压力的运行时覆盖，确认探索、自适应备援和稳定性保护仍能按原规则产生临时流量，不被固定重放逻辑吞掉。

## 两条独立执行链路

### 完整调度

入口位于：

- `controller/channel_ratio_monitor_schedule.go`
- `controller/channel_ratio_monitor_schedule_route.go`
- `model/channel_smart_schedule_route.go`

该链路负责：

- 读取窗口指标并评分。
- 生成基础排名、基础优先级和基础权重。
- 写入 Ability P/W 和路由状态。
- 生成智能调度执行记录。

任务状态为 `succeeded` 只能说明该链路没有返回任务级错误。仍需查看记录中的 `Total`、`Planned`、`Updated`、`Unchanged`、`Skipped` 和 `Failed`，避免把“全部跳过但任务正常结束”误判为实际完成了路由调整。

### 请求事件运行时

入口主要位于：

- `service/channel_monitor_event_emit.go`
- `service/channel_monitor_event_publisher.go`
- `service/channel_monitor_redis_runtime.go`
- `service/channel_monitor_redis_aggregator.go`
- `controller/channel_ratio_monitor_schedule_redis_runtime.go`
- `controller/channel_ratio_monitor_schedule_runtime.go`

该链路负责：

- 将真实请求成功、失败和延迟指标写入 Redis Stream。
- 消费事件并刷新对应 `(group, model)` 调度池。
- 计算探索流量和自适应备援预算。
- 触发连续失败或窗口失败率保护。
- 写入 `temporary_traffic_kind`、临时 P/W、健康状态和稳定性状态。

探索流量、自适应备援和运行时稳定性保护同时失效时，这条共享链路是当前首要检查对象。

## 当前重点检查项

### 1. 执行记录是否真正应用了路由

检查故障期间最近几条执行记录：

- `Planned` 是否大于 0。
- `Updated` 是否大于 0，或在路由已稳定时合理地显示 `Unchanged`。
- `Skipped` 的原因是否为样本不足、配置模型不匹配、未参与或经济快照冲突。
- `Failed` 是否为 0。

如果任务只是显示 `succeeded`，但所有路由都在 `Skipped` 中，则基础状态也可能一直保留旧值。

### 2. 路由是否具备运行时基础状态

运行时软刷新要求路由已经完成一次基础调度。重点检查 `channel_smart_schedule_route_states`：

- `participation_set = true` 且 `excluded = false`。
- `base_rank > 0`。
- `base_priority`、`base_weight` 已写入。
- `last_schedule_status = succeeded`。
- `last_schedule_time` 是近期时间。

`base_rank = 0` 的路由不会成为探索或自适应候选，即使完整任务本身最终显示成功。

### 3. 探索流量是否存在样本欠账

探索流量只会选择仍有必需指标样本欠账的健康备用渠道。检查：

- 策略 `apply_mode = priority_weight`。
- `sample_mode = traffic`。
- `exploration_traffic_percent > 0`。
- 备用渠道 `sampling_debt > 0`。
- 备用渠道不是保本兜底、稳定性降级或 429 冷却状态。

所有备用渠道样本已经充足时，探索流量停止是预期行为。

### 4. 自适应备援是否满足压力条件

自适应备援不是常驻分流。它要求：

- `adaptive_sampling_enabled = true`。
- 主渠道滚动健康状态不是空、`unknown` 或 `healthy`。
- 至少一个备用渠道仍有样本欠账。
- 计算出的备用预算大于 0。

应同时检查路由状态中的：

- `adaptive_health_state`
- `adaptive_health_pressure`
- `adaptive_health_sample_count`
- `adaptive_health_last_sample_at`

如果这些字段长期不更新，问题更可能位于事件发布、消费或软刷新调度，而不是预算公式。

### 5. 稳定性保护是否收到有效失败事件

运行时保护只统计真正到达上游且符合分类的失败。429、本地转换失败、跳过或最终汇总错误不会按普通稳定性失败处理。

检查：

- Redis 中是否持续出现请求事件。
- 消费组 pending 数量是否持续增长。
- 事件中的渠道 ID、归一化模型名和控制版本是否正确。
- 连续失败数或请求窗口失败率是否达到策略阈值。
- `stability_state`、`runtime_protection_until` 是否发生变化。

### 6. 临时覆盖是否写入又被撤回

探索或自适应生效时应看到：

- `temporary_traffic_kind = insufficient_samples`，或
- `temporary_traffic_kind = adaptive_sampling`。

还应同时更新：

- `temporary_traffic_since`
- `temporary_traffic_target_percent`
- Ability 当前优先级和权重

如果状态短暂出现后立即消失，应检查完整调度重放、控制版本冲突、经济修订冲突、429 冷却以及池级缓存刷新。

## 已确认风险的修复状态

### 配置解析与请求选路使用不同契约

该风险已修复。控制器在初始化时向模型层注册同一个完整策略校验器：

- `controller/channel_ratio_monitor_schedule_group_policy.go`
- `controller/channel_ratio_monitor_runtime_settings_cache.go`

请求选路仍只读取 `group` 和 `models` 来建立池索引，但在生产进程中会先经过控制器的完整策略校验。某些语义无效但 JSON 结构仍可解析的配置不再被当作正常智能调度策略，而是统一进入安全的 fail-closed 状态：

- 完整调度及运行时功能被关闭。
- 请求选路统一进入 fail-closed，只允许已有 `participation_set=true && excluded=false` 的路由。
- 未参与渠道不会因为无效配置重新进入候选池；修复不擅自改写已有 Ability P/W。

相关实现位于 `model/channel_smart_schedule_traffic_policy.go` 和 `controller/channel_ratio_monitor_runtime_settings_cache.go`。

### 旧稳定性窗口配置缺少迁移

该风险已修复。新增 `model.MigrateChannelSmartScheduleGroupPolicies()`，在主节点初始化选项前为缺少 `stability_window_minutes` 的旧策略写入历史默认值 5 分钟；迁移对无效 JSON 保持原值并记录日志，且可重复执行。

启动入口 `main.go` 已在加载选项前调用该迁移。

### 测试基线已同步

`TestSaveChannelSmartScheduleRouteConfigReportsParticipationChange` 和
`TestIncludeChannelSmartScheduleRouteCreatesAndPreservesOverride` 已同步到提交
`13155198a` 引入的“智能调度路由不再继承渠道默认 P/W”生产语义，当前均通过。

## 本地验证结果与限制

- `service` 包测试通过。
- `controller` 包全量测试通过。
- `model` 包全量测试通过。
- 单独运行 `TestNormalizeChannelSmartScheduleGroupPolicyRequiresStabilityWindow` 通过，确认当前代码会拒绝缺少稳定性窗口的策略。
- 本地数据库没有实际智能调度配置和路由状态，不能代替故障实例验证 Redis Stream、生产策略和实时路由状态。

## 建议的下一步取证顺序

1. 导出一条故障期间的成功执行记录，包含汇总计数和路由调整明细。
2. 导出对应 `(group, model)` 的所有 Ability P/W 与路由状态字段。
3. 确认最近请求事件的 Stream 长度、消费组 pending、最后消费时间和消费者错误日志。
4. 对一次明确达到阈值的失败请求追踪：事件发布、聚合、运行时保护、数据库更新、池缓存刷新。
5. 对一条有样本欠账的备用渠道追踪一次软刷新，确认在哪个门禁条件退出。

完成以上取证后，可以明确区分以下情况：

- 没有事件：发布链路问题。
- 有事件但没有消费：Redis 消费组或消费者问题。
- 已消费但健康字段不更新：事件投影或控制版本问题。
- 健康字段更新但没有临时 P/W：候选门禁或预算条件问题。
- 临时 P/W 已写入但页面仍单渠道：池缓存刷新或前端快照问题。
