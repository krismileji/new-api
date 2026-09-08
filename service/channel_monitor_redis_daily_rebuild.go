package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

const channelMonitorRedisDailyRebuildLeaseTTL = 10 * time.Minute

// RebuildChannelMonitorRedisDailySuccess rebuilds the current-day success
// projection from the durable database read model. It is intended for startup
// recovery after the daily Redis key was introduced, expired, or lost.
func RebuildChannelMonitorRedisDailySuccess(ctx context.Context, now int64) error {
	if model.DB != nil && model.DB.Migrator().HasTable(&model.ChannelMonitorDailyCheckpoint{}) {
		return ensureChannelMonitorDailyMetrics(ctx, common.RedisMonitorConsumerClient(), model.ChannelDailyCostDayStart(now))
	}
	return rebuildChannelMonitorRedisDailySuccess(ctx, common.RedisMonitorConsumerClient(), now)
}

// RebuildChannelMonitorRedisDailyCosts rebuilds the current-day cost
// projection from the durable daily cost ledgers and drill-down table.
func RebuildChannelMonitorRedisDailyCosts(ctx context.Context, now int64) error {
	return rebuildChannelMonitorRedisDailyCosts(ctx, common.RedisMonitorConsumerClient(), now)
}

func rebuildChannelMonitorRedisDailySuccess(
	ctx context.Context,
	client *redis.Client,
	now int64,
) error {
	if client == nil {
		return ErrChannelMonitorRedisSharedProjectionUnavailable
	}
	if model.DB == nil {
		return errors.New("渠道监控日汇总数据库不可用")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dayStart := model.ChannelDailyCostDayStart(now)
	if model.DB != nil && model.DB.Migrator().HasTable(&model.ChannelMonitorDailyCheckpoint{}) {
		return rebuildChannelMonitorDailyMetrics(ctx, client, dayStart)
	}
	dayEnd := now - now%60
	if dayEnd < dayStart {
		dayEnd = dayStart
	}

	leaseToken := "channel-monitor-daily-rebuild:" + common.GetUUID()
	leaseCtx, acquired, err := acquireChannelMonitorRedisDailyRebuildLease(ctx, client, leaseToken)
	if err != nil {
		return err
	}
	if !acquired {
		return nil
	}
	defer releaseChannelMonitorRedisDailyRebuildLease(leaseCtx, client, leaseToken)

	fields, databaseThrough, hasData, err := loadChannelMonitorRedisDailySuccessFields(ctx, dayStart, dayEnd)
	if err != nil {
		return err
	}
	projection := NewChannelMonitorRedisSharedProjectionWithClient(client)
	if fields == nil {
		fields = make(map[string]string)
	}
	if databaseThrough < dayEnd {
		tail, tailErr := projection.query(ctx, databaseThrough, dayEnd, projection.limits, channelMonitorRedisSharedQuerySelection{
			patterns: []string{channelMonitorRedisSharedScopeRoute + ":*", channelMonitorRedisSharedScopeAPIKeyRoute + ":*"},
		})
		if tailErr != nil {
			return fmt.Errorf("读取渠道监控 Redis 日汇总实时尾部失败: %w", tailErr)
		}
		tailFields, tailErr := buildChannelMonitorRedisDailySuccessTailFields(ctx, tail)
		if tailErr != nil {
			return tailErr
		}
		if err := mergeChannelMonitorRedisDailySuccessFields(fields, tailFields); err != nil {
			return err
		}
	}
	if !hasData && len(fields) == 0 {
		return nil
	}
	dataCutoffAt, processedAt, eventWatermark, _, _, metadataErr := projection.QueryMetadata(ctx, dayStart, dayEnd)
	if metadataErr != nil {
		return fmt.Errorf("读取渠道监控 Redis 日汇总元数据失败: %w", metadataErr)
	}
	if dataCutoffAt > 0 {
		fields[channelMonitorRedisSharedScopeMetadata+":"+channelMonitorRedisSharedMetricDataCutoffAt] = strconv.FormatInt(dataCutoffAt, 10)
	}
	if processedAt > 0 {
		fields[channelMonitorRedisSharedScopeMetadata+":"+channelMonitorRedisSharedMetricProcessedAt] = strconv.FormatInt(processedAt, 10)
	}
	if eventWatermark > 0 {
		fields[channelMonitorRedisSharedScopeMetadata+":"+channelMonitorRedisSharedMetricEventWatermark] = strconv.FormatUint(eventWatermark, 10)
	}
	if databaseThrough > 0 {
		fields[channelMonitorRedisSharedScopeMetadata+":"+channelMonitorRedisSharedMetricDatabaseThrough] = strconv.FormatInt(databaseThrough, 10)
	}
	fields[channelMonitorRedisSharedScopeMetadata+":rebuild_version"] = "1"

	return replaceChannelMonitorRedisDailySuccessKey(ctx, client, dayStart, fields)
}

func acquireChannelMonitorRedisDailyRebuildLease(
	ctx context.Context,
	client *redis.Client,
	token string,
) (context.Context, bool, error) {
	opCtx, cancel := context.WithTimeout(ctx, channelMonitorRedisSharedOperationTimeout)
	defer cancel()
	acquired, err := client.SetNX(
		opCtx,
		ChannelMonitorRedisAggregatorLeaseKey,
		token,
		channelMonitorRedisDailyRebuildLeaseTTL,
	).Result()
	return ctx, acquired, err
}

func releaseChannelMonitorRedisDailyRebuildLease(
	ctx context.Context,
	client *redis.Client,
	token string,
) {
	opCtx, cancel := context.WithTimeout(ctx, channelMonitorRedisSharedOperationTimeout)
	defer cancel()
	_ = client.Eval(
		opCtx,
		channelMonitorRedisLeaseReleaseScript,
		[]string{ChannelMonitorRedisAggregatorLeaseKey},
		token,
	).Err()
}

func loadChannelMonitorDailyRebuildMinuteRows(
	ctx context.Context,
	dayStart int64,
	dayEnd int64,
) ([]model.ChannelMonitorMinuteAPIKeyMetric, error) {
	var rows []model.ChannelMonitorMinuteAPIKeyMetric
	err := model.DB.WithContext(ctx).
		Where("minute_start >= ? AND minute_start < ?", dayStart, dayEnd).
		Order("minute_start ASC, channel_id ASC, model_key ASC, group_key ASC, api_key_key ASC").
		Find(&rows).Error
	return rows, err
}

func loadChannelMonitorRedisDailySuccessFields(
	ctx context.Context,
	dayStart int64,
	dayEnd int64,
) (map[string]string, int64, bool, error) {
	databaseThrough, err := channelMonitorRedisDailySuccessDatabaseThrough(ctx, dayStart, dayEnd)
	if err != nil {
		return nil, 0, false, err
	}
	if model.DB.Migrator().HasTable(&model.ChannelMonitorMinuteAPIKeyMetric{}) {
		rows, err := loadChannelMonitorDailyRebuildMinuteRows(ctx, dayStart, databaseThrough)
		if err != nil {
			return nil, 0, false, err
		}
		if len(rows) > 0 {
			owners, err := loadChannelMonitorDailyRebuildTokenOwners(ctx, rows)
			if err != nil {
				return nil, 0, false, err
			}
			fields, err := buildChannelMonitorRedisDailySuccessFields(rows, owners)
			return fields, databaseThrough, true, err
		}
	}

	if model.DB.Migrator().HasTable(&model.ChannelMonitorDailySuccessLedger{}) {
		var rows []model.ChannelMonitorDailySuccessLedger
		err := model.DB.WithContext(ctx).
			Where("day_start = ?", dayStart).
			Order("channel_id ASC, user_id ASC, api_key_id ASC, model_key ASC, group_key ASC").
			Find(&rows).Error
		if err != nil {
			return nil, 0, false, err
		}
		if len(rows) > 0 {
			fields, err := buildChannelMonitorRedisDailySuccessLedgerFields(rows)
			return fields, dayEnd, true, err
		}
	}
	return nil, databaseThrough, false, nil
}

func channelMonitorRedisDailySuccessDatabaseThrough(
	ctx context.Context,
	dayStart int64,
	dayEnd int64,
) (int64, error) {
	through := dayEnd - 60
	if through < dayStart {
		through = dayStart
	}
	if !model.DB.Migrator().HasTable(&model.ChannelMonitorAggregationState{}) {
		return through, nil
	}
	var state model.ChannelMonitorAggregationState
	if err := model.DB.WithContext(ctx).Where("id = ?", 1).Find(&state).Error; err != nil {
		return 0, err
	}
	if state.CompletedThrough >= dayEnd {
		through = dayEnd
	} else if state.CompletedThrough > dayStart && state.CompletedThrough > through {
		through = state.CompletedThrough - state.CompletedThrough%60
	}
	if through < dayStart {
		through = dayStart
	}
	return through, nil
}

func buildChannelMonitorRedisDailySuccessTailFields(
	ctx context.Context,
	view ChannelMonitorRedisSharedProjectionView,
) (map[string]string, error) {
	apiKeyIDs := make([]int, 0, len(view.APIKeyScopes))
	seen := make(map[int]struct{})
	for _, row := range view.APIKeyScopes {
		if row.APIKeyID > 0 {
			if _, ok := seen[row.APIKeyID]; !ok {
				seen[row.APIKeyID] = struct{}{}
				apiKeyIDs = append(apiKeyIDs, row.APIKeyID)
			}
		}
	}
	owners, err := loadChannelMonitorDailyRebuildTokenOwnersForIDs(ctx, apiKeyIDs)
	if err != nil {
		return nil, err
	}
	aggregates := make(map[string]ChannelMonitorRedisSharedAggregate)
	for _, row := range view.Routes {
		source := channelMonitorRedisDailySuccessAggregate(row.ChannelMonitorRedisSharedAggregate)
		event := model.ChannelMonitorEvent{ChannelId: row.ChannelID, ModelName: row.ModelName}
		for _, scope := range channelMonitorRedisSuccessDayScopes(event) {
			target := aggregates[scope]
			if err := mergeChannelMonitorRedisSharedAggregate(&target, source); err != nil {
				return nil, err
			}
			aggregates[scope] = target
		}
	}
	for _, row := range view.APIKeyScopes {
		source := channelMonitorRedisDailySuccessAggregate(row.ChannelMonitorRedisSharedAggregate)
		source.APIKeyName = row.APIKeyName
		event := model.ChannelMonitorEvent{
			ChannelId: row.ChannelID, UserId: owners[row.APIKeyID], APIKeyId: row.APIKeyID,
			APIKeyName: row.APIKeyName, ModelName: row.ModelName,
		}
		scopes := []string{
			channelMonitorRedisSharedScopeAPIKey + ":" + strconv.Itoa(row.APIKeyID),
			channelMonitorRedisSharedScopeAPIKeyRoute + ":" + channelMonitorRedisSharedAPIKeyScopeIdentity(row.APIKeyID, row.ChannelID, row.ModelName, ""),
		}
		if event.UserId > 0 {
			scopes = append(scopes,
				"user:"+strconv.Itoa(event.UserId),
				"channel_user:"+strconv.Itoa(event.ChannelId)+"."+strconv.Itoa(event.UserId),
				"user_api_key:"+strconv.Itoa(event.UserId)+"."+strconv.Itoa(event.APIKeyId),
				"channel_user_api_key:"+strconv.Itoa(event.ChannelId)+"."+strconv.Itoa(event.UserId)+"."+strconv.Itoa(event.APIKeyId),
				"user_api_key_route:"+strconv.Itoa(event.UserId)+"."+strconv.Itoa(event.APIKeyId)+"."+strconv.Itoa(event.ChannelId)+"."+channelMonitorRedisSharedDimension(row.ModelName),
			)
		}
		for _, scope := range scopes {
			target := aggregates[scope]
			if err := mergeChannelMonitorRedisSharedAggregate(&target, source); err != nil {
				return nil, err
			}
			aggregates[scope] = target
		}
	}
	return encodeChannelMonitorRedisDailySuccessAggregates(aggregates), nil
}

func loadChannelMonitorDailyRebuildTokenOwnersForIDs(ctx context.Context, ids []int) (map[int]int, error) {
	owners := make(map[int]int, len(ids))
	if len(ids) == 0 || !model.DB.Migrator().HasTable(&model.Token{}) {
		return owners, nil
	}
	var tokens []model.Token
	if err := model.DB.WithContext(ctx).Model(&model.Token{}).
		Select("id", "user_id").Where("id IN ?", ids).Find(&tokens).Error; err != nil {
		return nil, err
	}
	for _, token := range tokens {
		if token.Id > 0 && token.UserId > 0 {
			owners[token.Id] = token.UserId
		}
	}
	return owners, nil
}

func channelMonitorRedisDailySuccessAggregate(source ChannelMonitorRedisSharedAggregate) ChannelMonitorRedisSharedAggregate {
	return ChannelMonitorRedisSharedAggregate{
		ActualSuccessCount: source.ActualSuccessCount, ActualFailureCount: source.ActualFailureCount,
		FinalSuccessCount: source.FinalSuccessCount, FinalFailureCount: source.FinalFailureCount,
		CacheHitCount: source.CacheHitCount, CacheSampleCount: source.CacheSampleCount,
		CacheReadTokens: source.CacheReadTokens, InputTokens: source.InputTokens,
		CacheWriteRequestCount: source.CacheWriteRequestCount, APIKeyName: source.APIKeyName,
	}
}

func mergeChannelMonitorRedisDailySuccessFields(base, delta map[string]string) error {
	for field, raw := range delta {
		if strings.HasSuffix(field, ":"+channelMonitorRedisSharedMetricAPIKeyName) {
			if base[field] == "" {
				base[field] = raw
			}
			continue
		}
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return fmt.Errorf("渠道监控 Redis 日成功率尾部字段无效: %s", field)
		}
		if existing := base[field]; existing != "" {
			parsed, parseErr := strconv.ParseInt(existing, 10, 64)
			if parseErr != nil {
				return fmt.Errorf("渠道监控 Redis 日成功率基础字段无效: %s", field)
			}
			value, err = channelMonitorRedisSharedCheckedAddInt64(parsed, value)
			if err != nil {
				return err
			}
		}
		base[field] = strconv.FormatInt(value, 10)
	}
	return nil
}

func buildChannelMonitorRedisDailySuccessLedgerFields(
	rows []model.ChannelMonitorDailySuccessLedger,
) (map[string]string, error) {
	aggregates := make(map[string]ChannelMonitorRedisSharedAggregate)
	for _, row := range rows {
		if err := validateChannelMonitorDailySuccessLedgerRow(row); err != nil {
			return nil, err
		}
		event := model.ChannelMonitorEvent{
			ChannelId:  row.ChannelId,
			UserId:     row.UserId,
			APIKeyId:   row.APIKeyId,
			APIKeyName: row.APIKeyName,
			ModelName:  row.ModelName,
		}
		source := ChannelMonitorRedisSharedAggregate{
			ActualSuccessCount:     row.ActualSuccessCount,
			ActualFailureCount:     row.ActualFailureCount,
			FinalSuccessCount:      row.FinalSuccessCount,
			FinalFailureCount:      row.FinalFailureCount,
			CacheHitCount:          row.CacheHitCount,
			CacheSampleCount:       row.CacheSampleCount,
			CacheReadTokens:        row.CacheReadTokens,
			InputTokens:            row.InputTokens,
			CacheWriteRequestCount: row.CacheWriteCount,
			APIKeyName:             row.APIKeyName,
		}
		for _, scope := range channelMonitorRedisSuccessDayScopes(event) {
			target := aggregates[scope]
			if err := mergeChannelMonitorRedisSharedAggregate(&target, source); err != nil {
				return nil, err
			}
			aggregates[scope] = target
		}
	}
	return encodeChannelMonitorRedisDailySuccessAggregates(aggregates), nil
}

func loadChannelMonitorDailyRebuildTokenOwners(
	ctx context.Context,
	rows []model.ChannelMonitorMinuteAPIKeyMetric,
) (map[int]int, error) {
	ids := make([]int, 0)
	seen := make(map[int]struct{})
	for _, row := range rows {
		if row.APIKeyId <= 0 {
			continue
		}
		if _, exists := seen[row.APIKeyId]; exists {
			continue
		}
		seen[row.APIKeyId] = struct{}{}
		ids = append(ids, row.APIKeyId)
	}
	owners := make(map[int]int, len(ids))
	if len(ids) == 0 {
		return owners, nil
	}
	var tokens []model.Token
	if err := model.DB.WithContext(ctx).Model(&model.Token{}).
		Select("id", "user_id").Where("id IN ?", ids).Find(&tokens).Error; err != nil {
		return nil, err
	}
	for _, token := range tokens {
		if token.Id > 0 && token.UserId > 0 {
			owners[token.Id] = token.UserId
		}
	}
	return owners, nil
}

func buildChannelMonitorRedisDailySuccessFields(
	rows []model.ChannelMonitorMinuteAPIKeyMetric,
	owners map[int]int,
) (map[string]string, error) {
	aggregates := make(map[string]ChannelMonitorRedisSharedAggregate)
	for _, row := range rows {
		if err := validateChannelMonitorDailyRebuildMetric(row); err != nil {
			return nil, err
		}
		userID := owners[row.APIKeyId]
		event := model.ChannelMonitorEvent{
			ChannelId:  row.ChannelId,
			UserId:     userID,
			APIKeyId:   row.APIKeyId,
			APIKeyName: row.APIKeyName,
			ModelName:  row.ModelName,
		}
		source := ChannelMonitorRedisSharedAggregate{
			ActualSuccessCount:     row.ActualSuccessCount,
			ActualFailureCount:     row.ActualFailureCount,
			FinalSuccessCount:      row.FinalSuccessCount,
			FinalFailureCount:      row.FinalFailureCount,
			CacheHitCount:          row.CacheHitCount,
			CacheSampleCount:       row.CacheSampleCount,
			CacheReadTokens:        row.CacheReadTokens,
			InputTokens:            row.InputTokens,
			CacheWriteRequestCount: row.CacheWriteCount,
			APIKeyName:             row.APIKeyName,
		}
		for _, scope := range channelMonitorRedisSuccessDayScopes(event) {
			target := aggregates[scope]
			if err := mergeChannelMonitorRedisSharedAggregate(&target, source); err != nil {
				return nil, err
			}
			aggregates[scope] = target
		}
	}
	return encodeChannelMonitorRedisDailySuccessAggregates(aggregates), nil
}

func encodeChannelMonitorRedisDailySuccessAggregates(
	aggregates map[string]ChannelMonitorRedisSharedAggregate,
) map[string]string {
	fields := make(map[string]string, len(aggregates)*9)
	for scope, aggregate := range aggregates {
		for metric, value := range map[string]int64{
			channelMonitorRedisSharedMetricEventCount:             aggregate.EventCount,
			channelMonitorRedisSharedMetricBusinessRequests:       aggregate.BusinessRequestCount,
			channelMonitorRedisSharedMetricFirstTokenSamples:      aggregate.FirstTokenSampleCount,
			channelMonitorRedisSharedMetricAttemptDurationSamples: aggregate.AttemptDurationSampleCount,
			channelMonitorRedisSharedMetricAttemptDurationTotalMs: aggregate.AttemptDurationTotalMs,
			channelMonitorRedisSharedMetricTPSSamples:             aggregate.TPSSampleCount,
			channelMonitorRedisSharedMetricTPSOutputTokens:        aggregate.TPSOutputTokens,
			channelMonitorRedisSharedMetricTPSGenerationMs:        aggregate.TPSGenerationDurationMs,
			channelMonitorRedisSharedMetricCacheWriteTokens:       aggregate.CacheWriteTokens,
			channelMonitorRedisSharedMetricLastUsedTime:           aggregate.LastUsedTime,
			channelMonitorRedisSharedMetricActualSuccess:          aggregate.ActualSuccessCount,
			channelMonitorRedisSharedMetricActualFailure:          aggregate.ActualFailureCount,
			channelMonitorRedisSharedMetricFinalSuccess:           aggregate.FinalSuccessCount,
			channelMonitorRedisSharedMetricFinalFailure:           aggregate.FinalFailureCount,
			channelMonitorRedisSharedMetricCacheHits:              aggregate.CacheHitCount,
			channelMonitorRedisSharedMetricCacheSamples:           aggregate.CacheSampleCount,
			channelMonitorRedisSharedMetricCacheReadTokens:        aggregate.CacheReadTokens,
			channelMonitorRedisSharedMetricInputTokens:            aggregate.InputTokens,
			channelMonitorRedisSharedMetricCacheWriteRequests:     aggregate.CacheWriteRequestCount,
		} {
			if value != 0 {
				fields[scope+":"+metric] = strconv.FormatInt(value, 10)
			}
		}
		if aggregate.APIKeyName != "" && isChannelMonitorRedisDailyAPIKeyScope(scope) {
			fields[scope+":"+channelMonitorRedisSharedMetricAPIKeyName] = aggregate.APIKeyName
		}
		if aggregate.FirstTokenSampleCount > 0 {
			fields[scope+":"+channelMonitorRedisSharedMetricFirstTokenTotalMs] = strconv.FormatFloat(aggregate.FirstTokenTotalMs, 'f', -1, 64)
		}
	}
	return fields
}

func validateChannelMonitorDailySuccessLedgerRow(row model.ChannelMonitorDailySuccessLedger) error {
	if row.ChannelId <= 0 || row.UserId < 0 || row.APIKeyId < 0 {
		return errors.New("渠道监控日成功率汇总维度无效")
	}
	values := []int64{
		row.ActualSuccessCount, row.ActualFailureCount, row.FinalSuccessCount,
		row.FinalFailureCount, row.CacheHitCount, row.CacheSampleCount,
		row.CacheReadTokens, row.InputTokens, row.CacheWriteCount,
	}
	for _, value := range values {
		if value < 0 {
			return errors.New("渠道监控日成功率汇总包含负数，无法重建 Redis 日汇总")
		}
	}
	return nil
}

func validateChannelMonitorDailyRebuildMetric(row model.ChannelMonitorMinuteAPIKeyMetric) error {
	values := []int64{
		row.ActualSuccessCount, row.ActualFailureCount, row.FinalSuccessCount,
		row.FinalFailureCount, row.CacheHitCount, row.CacheSampleCount,
		row.CacheReadTokens, row.InputTokens, row.CacheWriteCount,
	}
	for _, value := range values {
		if value < 0 {
			return errors.New("渠道监控分钟汇总包含负数，无法重建 Redis 日汇总")
		}
	}
	return nil
}

func isChannelMonitorRedisDailyAPIKeyScope(scope string) bool {
	if strings.HasPrefix(scope, "fact:") || strings.HasPrefix(scope, "cost_detail:") || strings.HasPrefix(scope, "cost_key:") {
		return true
	}
	return strings.HasPrefix(scope, "fact:") || strings.HasPrefix(scope, "cost_detail:") || strings.HasPrefix(scope, channelMonitorRedisSharedScopeAPIKey+":") ||
		strings.HasPrefix(scope, channelMonitorRedisSharedScopeAPIKeyRoute+":") ||
		strings.HasPrefix(scope, "user_api_key:") ||
		strings.HasPrefix(scope, "channel_user_api_key:") ||
		strings.HasPrefix(scope, "user_api_key_route:")
}

func replaceChannelMonitorRedisDailySuccessKey(
	ctx context.Context,
	client *redis.Client,
	dayStart int64,
	fields map[string]string,
) error {
	key := ChannelMonitorRedisSuccessDayKey(dayStart)
	temporaryKey := key + ":rebuild:" + common.GetUUID()
	cleanup := true
	defer func() {
		if cleanup {
			_ = client.Del(context.Background(), temporaryKey).Err()
		}
	}()

	entries := make([]interface{}, 0, len(fields)*2)
	for field, value := range fields {
		entries = append(entries, field, value)
	}
	opCtx, cancel := context.WithTimeout(ctx, channelMonitorRedisSharedOperationTimeout)
	defer cancel()
	if len(entries) == 0 {
		entries = append(entries, channelMonitorRedisSharedScopeMetadata+":rebuild_version", "1")
	}
	if err := client.HSet(opCtx, temporaryKey, entries...).Err(); err != nil {
		return err
	}
	if err := client.Expire(opCtx, temporaryKey, channelMonitorRedisSharedSuccessDayTTL).Err(); err != nil {
		return err
	}
	if err := client.Rename(opCtx, temporaryKey, key).Err(); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func rebuildChannelMonitorRedisDailyCosts(ctx context.Context, client *redis.Client, now int64) error {
	return rebuildChannelMonitorReliableDailyCosts(ctx, client, model.ChannelDailyCostDayStart(now))
}

func loadChannelMonitorRedisDailyCostFields(
	ctx context.Context,
	dayStart int64,
	databases ...*gorm.DB,
) (map[string]string, bool, error) {
	db := model.DB
	if len(databases) > 0 {
		db = databases[0]
	}
	if !db.Migrator().HasTable(&model.ChannelDailyCost{}) {
		return nil, false, nil
	}
	var channelRows []model.ChannelDailyCost
	if err := db.WithContext(ctx).Where("day_start = ?", dayStart).
		Order("channel_id ASC").Find(&channelRows).Error; err != nil {
		return nil, false, err
	}
	if len(channelRows) == 0 {
		return nil, false, nil
	}
	keyDisplays := make(map[string]string)
	aggregates := make(map[string]ChannelMonitorRedisSharedAggregate)
	detailAPIKeys := make(map[string]struct{})
	for _, row := range channelRows {
		if err := validateChannelMonitorRedisDailyCostRow(row); err != nil {
			return nil, false, err
		}
		source := ChannelMonitorRedisSharedAggregate{
			SettledCostNanoCNY:               row.CostNanoCNY,
			SettledRequestCount:              row.SettledCount,
			UnresolvedRequestCount:           row.UnresolvedCount,
			ProbeSettledCostNanoCNY:          row.ProbeCostNanoCNY,
			GroupProbeSettledCostNanoCNY:     row.GroupProbeCostNanoCNY,
			ModelDetectionSettledCostNanoCNY: row.ModelDetectionCostNanoCNY,
		}
		if err := addChannelMonitorRedisDailyCostAggregate(aggregates, channelMonitorRedisSharedScopeGlobal, source); err != nil {
			return nil, false, err
		}
		if err := addChannelMonitorRedisDailyCostAggregate(aggregates, channelMonitorRedisSharedScopeChannel+":"+strconv.Itoa(row.ChannelId), source); err != nil {
			return nil, false, err
		}
	}

	if db.Migrator().HasTable(&model.ChannelMonitorDailyCostDetail{}) {
		var detailRows []model.ChannelMonitorDailyCostDetail
		if err := db.WithContext(ctx).Where("day_start = ?", dayStart).
			Order("channel_id ASC, api_key_id ASC, model_key ASC, source_kind ASC").Find(&detailRows).Error; err != nil {
			return nil, false, err
		}
		for _, row := range detailRows {
			if err := validateChannelMonitorRedisDailyCostDetail(row); err != nil {
				return nil, false, err
			}
			source := channelMonitorRedisDailyCostDetailAggregate(row)
			state := channelMonitorReliableCostState{Identity: channelMonitorReliableCostIdentity{
				ChannelID: row.ChannelId, UserID: row.UserId, APIKeyID: row.APIKeyId,
				Model:       ratio_setting.FormatMatchingModelName(strings.TrimSpace(row.ModelName)),
				Fingerprint: row.APIKeyKey, Source: row.SourceKind,
			}, APIKeyName: row.APIKeyName}
			for _, scope := range channelMonitorReliableCostScopes(state) {
				if scope == channelMonitorRedisSharedScopeGlobal || scope == channelMonitorRedisSharedScopeChannel+":"+strconv.Itoa(row.ChannelId) {
					continue
				}
				if err := addChannelMonitorRedisDailyCostAggregate(aggregates, scope, source); err != nil {
					return nil, false, err
				}
			}
			if row.APIKeyId > 0 {
				detailAPIKeys[strconv.Itoa(row.ChannelId)+":"+strconv.Itoa(row.APIKeyId)] = struct{}{}
			}
		}
	}

	if db.Migrator().HasTable(&model.ChannelDailyAPIKeyCost{}) {
		var apiKeyRows []model.ChannelDailyAPIKeyCost
		if err := db.WithContext(ctx).Where("day_start = ?", dayStart).
			Order("channel_id ASC, api_key_id ASC, key_fingerprint ASC").Find(&apiKeyRows).Error; err != nil {
			return nil, false, err
		}
		for _, row := range apiKeyRows {
			if err := validateChannelMonitorRedisDailyAPIKeyCost(row); err != nil {
				return nil, false, err
			}
			keyState := channelMonitorReliableCostState{Identity: channelMonitorReliableCostIdentity{
				ChannelID: row.ChannelId, APIKeyID: row.APIKeyId, Fingerprint: row.KeyFingerprint,
			}}
			for _, scope := range channelMonitorReliableCostScopes(keyState) {
				if !strings.HasPrefix(scope, "cost_key:") {
					continue
				}
				keyDisplays[scope+":key_display"] = row.KeyDisplay
				aggregates[scope] = ChannelMonitorRedisSharedAggregate{SettledCostNanoCNY: row.CostNanoCNY,
					SettledRequestCount: row.SettledCount, UnresolvedRequestCount: row.UnresolvedCount, APIKeyName: row.APIKeyName}
			}
			key := channelMonitorRedisSharedScopeAPIKey + ":" + strconv.Itoa(row.APIKeyId)
			if row.APIKeyId <= 0 {
				continue
			}
			if _, exists := detailAPIKeys[strconv.Itoa(row.ChannelId)+":"+strconv.Itoa(row.APIKeyId)]; !exists {
				if err := addChannelMonitorRedisDailyCostAggregate(aggregates, key, ChannelMonitorRedisSharedAggregate{
					SettledCostNanoCNY:     row.CostNanoCNY,
					SettledRequestCount:    row.SettledCount,
					UnresolvedRequestCount: row.UnresolvedCount,
				}); err != nil {
					return nil, false, err
				}
			}
			aggregate := aggregates[key]
			if aggregate.APIKeyName == "" {
				aggregate.APIKeyName = row.APIKeyName
			}
			aggregates[key] = aggregate
		}
	}
	fields := encodeChannelMonitorRedisDailyCostAggregates(aggregates)
	for field, display := range keyDisplays {
		fields[field] = display
	}
	return fields, true, nil
}

func addChannelMonitorRedisDailyCostAggregate(
	aggregates map[string]ChannelMonitorRedisSharedAggregate,
	scope string,
	source ChannelMonitorRedisSharedAggregate,
) error {
	target := aggregates[scope]
	if err := mergeChannelMonitorRedisSharedAggregate(&target, source); err != nil {
		return err
	}
	aggregates[scope] = target
	return nil
}

func channelMonitorRedisDailyCostDetailAggregate(row model.ChannelMonitorDailyCostDetail) ChannelMonitorRedisSharedAggregate {
	source := ChannelMonitorRedisSharedAggregate{
		SettledCostNanoCNY:           row.CostNanoCNY,
		SettledRequestCount:          row.SettledCount,
		UnresolvedRequestCount:       row.UnresolvedCount,
		ProbeSettledCostNanoCNY:      row.ProbeCostNanoCNY,
		GroupProbeSettledCostNanoCNY: row.GroupProbeCostNanoCNY,
		APIKeyName:                   row.APIKeyName,
	}
	if row.SourceKind == string(model.ChannelMonitorEventSourceModelDetection) {
		source.ModelDetectionSettledCostNanoCNY = row.CostNanoCNY
	}
	return source
}

func validateChannelMonitorRedisDailyCostRow(row model.ChannelDailyCost) error {
	if row.ChannelId <= 0 || row.CostNanoCNY < 0 || row.ProbeCostNanoCNY < 0 ||
		row.GroupProbeCostNanoCNY < 0 || row.ModelDetectionCostNanoCNY < 0 ||
		row.SettledCount < 0 || row.UnresolvedCount < 0 {
		return errors.New("渠道监控日成本汇总包含无效值")
	}
	if row.ProbeCostNanoCNY > row.CostNanoCNY ||
		row.GroupProbeCostNanoCNY > row.ProbeCostNanoCNY ||
		row.ModelDetectionCostNanoCNY > row.CostNanoCNY {
		return errors.New("渠道监控日成本汇总分类值无效")
	}
	return nil
}

func validateChannelMonitorRedisDailyCostDetail(row model.ChannelMonitorDailyCostDetail) error {
	if row.ChannelId <= 0 || row.UserId < 0 || row.APIKeyId < 0 || row.CostNanoCNY < 0 ||
		row.ProbeCostNanoCNY < 0 || row.GroupProbeCostNanoCNY < 0 ||
		row.SettledCount < 0 || row.UnresolvedCount < 0 {
		return errors.New("渠道监控成本明细包含无效值")
	}
	if row.ProbeCostNanoCNY > row.CostNanoCNY || row.GroupProbeCostNanoCNY > row.ProbeCostNanoCNY {
		return errors.New("渠道监控成本明细分类值无效")
	}
	return nil
}

func validateChannelMonitorRedisDailyAPIKeyCost(row model.ChannelDailyAPIKeyCost) error {
	if row.ChannelId <= 0 || row.APIKeyId < 0 || row.CostNanoCNY < 0 ||
		row.SettledCount < 0 || row.UnresolvedCount < 0 {
		return errors.New("渠道 API Key 日成本包含无效值")
	}
	return nil
}

func encodeChannelMonitorRedisDailyCostAggregates(
	aggregates map[string]ChannelMonitorRedisSharedAggregate,
) map[string]string {
	fields := make(map[string]string, len(aggregates)*8)
	for scope, aggregate := range aggregates {
		for metric, value := range map[string]int64{
			channelMonitorRedisSharedMetricSettledCost:           aggregate.SettledCostNanoCNY,
			channelMonitorRedisSharedMetricSettledRequests:       aggregate.SettledRequestCount,
			channelMonitorRedisSharedMetricUnresolvedCost:        aggregate.UnresolvedCostNanoCNY,
			channelMonitorRedisSharedMetricUnresolvedRequests:    aggregate.UnresolvedRequestCount,
			channelMonitorRedisSharedMetricProbeSettledCost:      aggregate.ProbeSettledCostNanoCNY,
			channelMonitorRedisSharedMetricGroupProbeSettledCost: aggregate.GroupProbeSettledCostNanoCNY,
			channelMonitorRedisSharedMetricDetectionSettledCost:  aggregate.ModelDetectionSettledCostNanoCNY,
		} {
			if value != 0 {
				fields[scope+":"+metric] = strconv.FormatInt(value, 10)
			}
		}
		if aggregate.APIKeyName != "" && isChannelMonitorRedisDailyAPIKeyScope(scope) {
			fields[scope+":"+channelMonitorRedisSharedMetricAPIKeyName] = aggregate.APIKeyName
		}
	}
	return fields
}

func replaceChannelMonitorRedisDailyHashKey(
	ctx context.Context,
	client *redis.Client,
	key string,
	ttl time.Duration,
	fields map[string]string,
) error {
	temporaryKey := key + ":rebuild:" + common.GetUUID()
	cleanup := true
	defer func() {
		if cleanup {
			_ = client.Del(context.Background(), temporaryKey).Err()
		}
	}()
	entries := make([]interface{}, 0, len(fields)*2)
	for field, value := range fields {
		entries = append(entries, field, value)
	}
	if len(entries) == 0 {
		entries = append(entries, channelMonitorRedisSharedScopeMetadata+":rebuild_version", "1")
	}
	opCtx, cancel := context.WithTimeout(ctx, channelMonitorRedisSharedOperationTimeout)
	defer cancel()
	if err := client.HSet(opCtx, temporaryKey, entries...).Err(); err != nil {
		return err
	}
	if err := client.Expire(opCtx, temporaryKey, ttl).Err(); err != nil {
		return err
	}
	if err := client.Rename(opCtx, temporaryKey, key).Err(); err != nil {
		return err
	}
	cleanup = false
	return nil
}
