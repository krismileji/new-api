package service

import (
	"context"
	"database/sql"
	"encoding/base64"
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

const channelMonitorReliableCostVersionField = "meta:reliable_cost_version"

const channelMonitorReliableCostStatusKey = "channel_cost:v1:projection:status"

type ChannelMonitorReliableCostStatus struct {
	CheckedAt int64 `json:"checked_at"`
	Pending   bool  `json:"pending"`
	Failed    bool  `json:"failed"`
}

type channelMonitorReliableCostIdentity struct {
	ChannelID   int
	UserID      int
	APIKeyID    int
	Model       string
	Fingerprint string
	Source      string
}

type channelMonitorReliableCostState struct {
	Identity   channelMonitorReliableCostIdentity
	Version    int64
	APIKeyName string
	KeyDisplay string
	Cost       int64
	Probe      int64
	GroupProbe int64
	Detection  int64
	Settled    int64
	Unresolved int64
}

func channelMonitorReliableCostFromOutbox(row model.ChannelDailyCostOutbox) (string, channelMonitorReliableCostState) {
	identity := row.ProjectionEventId
	if identity == "" {
		identity = row.EventId
	}
	source := strings.TrimSpace(row.SourceKind)
	if source == "" {
		source = string(model.ChannelMonitorEventSourceBusiness)
	}
	return identity, channelMonitorReliableCostState{
		Identity: channelMonitorReliableCostIdentity{
			ChannelID: row.ChannelId, UserID: row.UserId, APIKeyID: row.APIKeyId,
			Model:       ratio_setting.FormatMatchingModelName(strings.TrimSpace(row.ModelName)),
			Fingerprint: row.KeyFingerprint, Source: source,
		},
		Version: row.Id, APIKeyName: row.APIKeyName, KeyDisplay: row.KeyDisplay, Cost: row.CostNanoCNY,
		Probe: row.ProbeCostNanoCNY, GroupProbe: row.GroupProbeCostNanoCNY,
		Detection: row.ModelDetectionCostNanoCNY, Settled: row.SettledDelta, Unresolved: row.UnresolvedDelta,
	}
}

func channelMonitorReliableCostScopes(state channelMonitorReliableCostState) []string {
	id := state.Identity
	scopes := channelMonitorRedisSuccessDayScopes(model.ChannelMonitorEvent{
		ChannelId: id.ChannelID, UserId: id.UserID, APIKeyId: id.APIKeyID, ModelName: id.Model,
	})
	// Canonical request facts belong only in the statistics hash.
	filtered := scopes[:0]
	for _, scope := range scopes {
		if !strings.HasPrefix(scope, "fact:") {
			filtered = append(filtered, scope)
		}
	}
	scopes = filtered
	encoded, _ := common.Marshal(id) // Only scalar, bounded event identity fields.
	scopes = append(scopes, "cost_detail:"+base64.RawURLEncoding.EncodeToString(encoded))
	if id.Fingerprint != "" {
		keyIdentity := channelMonitorReliableCostIdentity{ChannelID: id.ChannelID, APIKeyID: id.APIKeyID, Fingerprint: id.Fingerprint}
		keyData, _ := common.Marshal(keyIdentity)
		scopes = append(scopes, "cost_key:"+base64.RawURLEncoding.EncodeToString(keyData))
	}
	return scopes
}

func channelMonitorReliableCostFields(state channelMonitorReliableCostState) map[string]int64 {
	return map[string]int64{
		channelMonitorRedisSharedMetricSettledCost:           state.Cost,
		channelMonitorRedisSharedMetricProbeSettledCost:      state.Probe,
		channelMonitorRedisSharedMetricGroupProbeSettledCost: state.GroupProbe,
		channelMonitorRedisSharedMetricDetectionSettledCost:  state.Detection,
		channelMonitorRedisSharedMetricSettledRequests:       state.Settled,
		channelMonitorRedisSharedMetricUnresolvedRequests:    state.Unresolved,
	}
}

// applyChannelMonitorReliableCostDelta checks every resulting integer before
// publishing. Redis Lua numbers and floating point never carry monetary sums.
func applyChannelMonitorReliableCostDelta(fields map[string]string, state channelMonitorReliableCostState, sign int64) error {
	for _, scope := range channelMonitorReliableCostScopes(state) {
		for metric, amount := range channelMonitorReliableCostFields(state) {
			if amount == 0 {
				continue
			}
			key := scope + ":" + metric
			var previous int64
			if raw := fields[key]; raw != "" {
				var err error
				previous, err = strconv.ParseInt(raw, 10, 64)
				if err != nil {
					return fmt.Errorf("渠道成本 Redis 数值无效: %w", err)
				}
			}
			next, err := channelMonitorRedisSharedCheckedAddInt64(previous, sign*amount)
			if err != nil || next < 0 {
				return errors.New("渠道成本 Redis 汇总溢出或修正计数不足")
			}
			fields[key] = strconv.FormatInt(next, 10)
		}
		if state.APIKeyName != "" {
			fields[scope+":"+channelMonitorRedisSharedMetricAPIKeyName] = state.APIKeyName
		}
		if state.KeyDisplay != "" && strings.HasPrefix(scope, "cost_key:") {
			fields[scope+":key_display"] = state.KeyDisplay
		}
	}
	return nil
}

func projectChannelDailyCostOutboxRows(ctx context.Context, client *redis.Client, rows []model.ChannelDailyCostOutbox) error {
	if client == nil {
		return ErrChannelMonitorRedisSharedProjectionUnavailable
	}
	byDay := make(map[int64][]model.ChannelDailyCostOutbox)
	for _, row := range rows {
		day := model.ChannelDailyCostDayStart(row.OccurredAt)
		byDay[day] = append(byDay[day], row)
	}
	for day, batch := range byDay {
		key := ChannelMonitorRedisCostDayKey(day)
		stateKey := key + ":events"
		version, err := client.HGet(ctx, key, channelMonitorReliableCostVersionField).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return err
		}
		stateVersion, stateErr := client.HGet(ctx, stateKey, "_version").Result()
		if stateErr != nil && !errors.Is(stateErr, redis.Nil) {
			return stateErr
		}
		if version != "1" || stateVersion != "1" {
			if err := rebuildChannelMonitorReliableDailyCosts(ctx, client, day); err != nil {
				return err
			}
		}
		applied := false
		for attempt := 0; attempt < channelMonitorRedisSharedWriteRetries; attempt++ {
			err = client.Watch(ctx, func(tx *redis.Tx) error {
				version, err := tx.HGet(ctx, key, channelMonitorReliableCostVersionField).Result()
				if err != nil {
					return err
				}
				if version != "1" {
					return ErrChannelMonitorRedisSharedProjectionUnavailable
				}
				stateVersion, err := tx.HGet(ctx, stateKey, "_version").Result()
				if err != nil || stateVersion != "1" {
					return ErrChannelMonitorRedisSharedProjectionUnavailable
				}
				identities := make([]string, 0, len(batch))
				for _, row := range batch {
					id, _ := channelMonitorReliableCostFromOutbox(row)
					identities = append(identities, id)
				}
				stored, err := tx.HMGet(ctx, stateKey, identities...).Result()
				if err != nil {
					return err
				}
				states := make(map[string]channelMonitorReliableCostState)
				fieldSet := map[string]struct{}{"meta:revision": {}, "meta:data_cutoff_at": {}}
				for index, row := range batch {
					id, next := channelMonitorReliableCostFromOutbox(row)
					versions := []channelMonitorReliableCostState{next}
					if stored[index] != nil {
						var previous channelMonitorReliableCostState
						if err := common.UnmarshalJsonStr(channelMonitorRedisSharedRedisValueString(stored[index]), &previous); err != nil {
							return err
						}
						states[id] = previous
						versions = append(versions, previous)
					}
					for _, state := range versions {
						for _, scope := range channelMonitorReliableCostScopes(state) {
							for metric := range channelMonitorReliableCostFields(state) {
								fieldSet[scope+":"+metric] = struct{}{}
							}
						}
					}
				}
				names := make([]string, 0, len(fieldSet))
				for name := range fieldSet {
					names = append(names, name)
				}
				values, err := tx.HMGet(ctx, key, names...).Result()
				if err != nil {
					return err
				}
				fields := make(map[string]string, len(names))
				for index, value := range values {
					if value != nil {
						fields[names[index]] = channelMonitorRedisSharedRedisValueString(value)
					}
				}
				updates := make(map[string]interface{})
				for index, row := range batch {
					id, next := channelMonitorReliableCostFromOutbox(row)
					previous, exists := states[id]
					if !exists && stored[index] != nil {
						if err := common.UnmarshalJsonStr(channelMonitorRedisSharedRedisValueString(stored[index]), &previous); err != nil {
							return err
						}
						exists = true
					}
					if exists && previous.Version >= next.Version {
						states[id] = previous
						continue
					}
					if exists {
						if err := applyChannelMonitorReliableCostDelta(fields, previous, -1); err != nil {
							return err
						}
					}
					if err := applyChannelMonitorReliableCostDelta(fields, next, 1); err != nil {
						return err
					}
					encoded, err := common.Marshal(next)
					if err != nil {
						return err
					}
					updates[id] = string(encoded)
					states[id] = next
					cutoff, _ := strconv.ParseInt(fields["meta:data_cutoff_at"], 10, 64)
					fields["meta:data_cutoff_at"] = strconv.FormatInt(max(cutoff, row.OccurredAt), 10)
				}
				if len(updates) == 0 {
					return nil
				}
				revision, err := strconv.ParseInt(fields["meta:revision"], 10, 64)
				if err != nil {
					return err
				}
				revision, err = channelMonitorRedisSharedCheckedAddInt64(revision, 1)
				if err != nil {
					return err
				}
				fields["meta:revision"] = strconv.FormatInt(revision, 10)
				fields["meta:processed_at"] = strconv.FormatInt(time.Now().Unix(), 10)
				_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
					pipe.HSet(ctx, key, fields)
					pipe.HSet(ctx, stateKey, updates)
					pipe.Expire(ctx, key, channelMonitorRedisSharedSuccessDayTTL)
					pipe.Expire(ctx, stateKey, channelMonitorRedisSharedSuccessDayTTL)
					pipe.Incr(ctx, ChannelMonitorRedisSharedRevisionKey)
					return nil
				})
				return err
			}, key, stateKey)
			if !errors.Is(err, redis.TxFailedErr) {
				if err != nil {
					return err
				}
				applied = true
				break
			}
		}
		if !applied {
			return redis.TxFailedErr
		}
	}
	return nil
}

// A repeatable database snapshot includes the exact set of already-applied
// outbox events. Pending events form its tail, including costs visible in Redis
// before the minute ledger commit. A task's durable version survives outbox
// retention, so a later correction can replace its original cost safely.
func rebuildChannelMonitorReliableDailyCosts(ctx context.Context, client *redis.Client, day int64) error {
	if client == nil || model.DB == nil {
		return ErrChannelMonitorRedisSharedProjectionUnavailable
	}
	key := ChannelMonitorRedisCostDayKey(day)
	stateKey := key + ":events"
	leaseKey := key + ":rebuild:lease"
	token := common.GetUUID()
	acquired, err := client.SetNX(ctx, leaseKey, token, channelMonitorRedisDailyRebuildLeaseTTL).Result()
	if err != nil {
		return err
	}
	if !acquired {
		return errors.New("渠道成本日汇总正在重建")
	}
	defer client.Eval(context.WithoutCancel(ctx), channelMonitorRedisLeaseReleaseScript, []string{leaseKey}, token)
	for attempt := 0; attempt < channelMonitorRedisSharedWriteRetries; attempt++ {
		err = client.Watch(ctx, func(tx *redis.Tx) error {
			var fields map[string]string
			var rows []model.ChannelDailyCostOutbox
			var tasks []model.ChannelTaskCostEvent
			options := &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}
			if model.DB.Dialector.Name() == "sqlite" {
				options = &sql.TxOptions{}
			}
			err := model.DB.WithContext(ctx).Transaction(func(db *gorm.DB) error {
				var err error
				fields, _, err = loadChannelMonitorRedisDailyCostFields(ctx, day, db)
				if err != nil {
					return err
				}
				if db.Migrator().HasTable(&model.ChannelDailyCostOutbox{}) {
					if err := db.Where("occurred_at >= ? AND occurred_at < ?", day, day+86400).Order("id ASC").Find(&rows).Error; err != nil {
						return err
					}
				}
				if db.Migrator().HasTable(&model.ChannelTaskCostEvent{}) {
					return db.Where("day_start = ?", day).Find(&tasks).Error
				}
				return nil
			}, options)
			if err != nil {
				return err
			}
			if fields == nil {
				fields = make(map[string]string)
			}
			states := make(map[string]channelMonitorReliableCostState)
			for _, row := range rows {
				if row.ProcessedAt == 0 {
					continue
				}
				id, state := channelMonitorReliableCostFromOutbox(row)
				states[id] = state
			}
			for _, task := range tasks {
				_, state := channelMonitorReliableCostFromOutbox(model.ChannelDailyCostOutbox{
					Id: task.ProjectionVersion, ProjectionEventId: task.CostEventId,
					ChannelId: task.ChannelId, UserId: task.UserId, APIKeyId: task.APIKeyId,
					ModelName: task.ModelName, KeyFingerprint: task.KeyFingerprint,
					APIKeyName: task.APIKeyName, KeyDisplay: task.KeyDisplay, CostNanoCNY: task.CostNanoCNY, SettledDelta: 1, SourceKind: "business",
				})
				states[task.CostEventId] = state
			}
			for _, row := range rows {
				if row.ProcessedAt != 0 {
					continue
				}
				id, state := channelMonitorReliableCostFromOutbox(row)
				if previous, exists := states[id]; exists {
					if previous.Version >= state.Version {
						continue
					}
					if err := applyChannelMonitorReliableCostDelta(fields, previous, -1); err != nil {
						return err
					}
				}
				if err := applyChannelMonitorReliableCostDelta(fields, state, 1); err != nil {
					return err
				}
				states[id] = state
			}
			encodedStates := make(map[string]interface{}, len(states)+1)
			encodedStates["_version"] = "1"
			for id, state := range states {
				encoded, err := common.Marshal(state)
				if err != nil {
					return err
				}
				encodedStates[id] = string(encoded)
			}
			now := time.Now().Unix()
			fields[channelMonitorReliableCostVersionField] = "1"
			fields["meta:database_snapshot_at"] = strconv.FormatInt(now, 10)
			fields["meta:processed_at"] = strconv.FormatInt(now, 10)
			previousRevision, err := tx.HGet(ctx, key, "meta:revision").Int64()
			if err != nil && !errors.Is(err, redis.Nil) {
				return err
			}
			revision, err := channelMonitorRedisSharedCheckedAddInt64(previousRevision, 1)
			if err != nil {
				return err
			}
			fields["meta:revision"] = strconv.FormatInt(revision, 10)
			if owner, err := tx.Get(ctx, leaseKey).Result(); err != nil || owner != token {
				return errors.New("渠道成本日汇总重建租约已失效")
			}
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Del(ctx, key, stateKey)
				pipe.HSet(ctx, key, fields)
				pipe.HSet(ctx, stateKey, encodedStates)
				pipe.Expire(ctx, key, channelMonitorRedisSharedSuccessDayTTL)
				pipe.Expire(ctx, stateKey, channelMonitorRedisSharedSuccessDayTTL)
				pipe.Incr(ctx, ChannelMonitorRedisSharedRevisionKey)
				return nil
			})
			return err
		}, key, stateKey, leaseKey)
		if !errors.Is(err, redis.TxFailedErr) {
			return err
		}
	}
	return redis.TxFailedErr
}

func (runtime *ChannelDailyCostOutboxRuntime) runRedisProjection(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		status := ChannelMonitorReliableCostStatus{CheckedAt: time.Now().Unix()}
		deadline := time.Now().Add(2 * time.Second)
		for ctx.Err() == nil {
			opCtx, cancel := context.WithTimeout(ctx, channelDailyCostOutboxDBOperationTimeout)
			day := model.ChannelDailyCostDayStart(time.Now().Unix())
			version, err := runtime.redisClient.HGet(opCtx, ChannelMonitorRedisCostDayKey(day), channelMonitorReliableCostVersionField).Result()
			if errors.Is(err, redis.Nil) || err == nil && version != "1" {
				err = rebuildChannelMonitorReliableDailyCosts(opCtx, runtime.redisClient, day)
			}
			var rows []model.ChannelDailyCostOutbox
			if err == nil {
				rows, err = model.PendingChannelDailyCostProjections(opCtx, channelDailyCostOutboxBatchSize)
			}
			if err == nil && len(rows) > 0 {
				err = projectChannelDailyCostOutboxRows(opCtx, runtime.redisClient, rows)
				if err == nil {
					ids := make([]int64, 0, len(rows))
					for _, row := range rows {
						ids = append(ids, row.Id)
					}
					err = model.MarkChannelDailyCostProjectionsApplied(opCtx, ids, time.Now().Unix())
				}
			}
			cancel()
			status.Pending = len(rows) >= channelDailyCostOutboxBatchSize || err != nil
			status.Failed = err != nil
			if err != nil {
				common.SysError("渠道成本 Redis 投影更新失败，将重试: " + err.Error())
				break
			}
			if len(rows) < channelDailyCostOutboxBatchSize || time.Now().After(deadline) {
				break
			}
		}
		if payload, err := common.Marshal(status); err == nil && ctx.Err() == nil {
			opCtx, cancel := context.WithTimeout(ctx, channelMonitorRedisSharedOperationTimeout)
			if length, err := runtime.redisClient.XLen(opCtx, ChannelDailyCostRedisStream).Result(); err != nil {
				status.Failed = true
			} else if length > 0 {
				status.Pending = true
			}
			payload, _ = common.Marshal(status)
			if err := runtime.redisClient.Set(opCtx, channelMonitorReliableCostStatusKey, payload, 30*time.Second).Err(); err != nil {
				common.SysError("渠道成本 Redis 投影状态更新失败: " + err.Error())
			}
			cancel()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
