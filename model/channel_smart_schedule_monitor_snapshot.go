package model

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// This companion payload is committed with the routing snapshot pointer. The
// established routing JSON/checksum remains compatible with older nodes.
type channelSmartScheduleMonitorReadModel struct {
	Revision        int64
	SourceWatermark int64
	GeneratedAt     int64
	Routes          channelSmartScheduleMonitorRoutes
	Economics       ChannelSmartScheduleEconomicSnapshot
}

// Internal snapshots must retain fields hidden from the public route JSON.
// In particular, a lost state revision makes every guarded scheduler write fail.
type channelSmartScheduleMonitorRoutes []ChannelSmartScheduleRoute

type channelSmartScheduleMonitorRouteJSON struct {
	ChannelSmartScheduleRoute
	StateRevision              int64                           `json:"state_revision"`
	ManualPrimarySaved         bool                            `json:"manual_primary_saved"`
	ManualPrimarySavedPriority int64                           `json:"manual_primary_saved_priority"`
	ManualPrimarySavedWeight   uint                            `json:"manual_primary_saved_weight"`
	LogicalChannelId           int64                           `json:"logical_channel_id,omitempty"`
	LogicalRevision            int64                           `json:"logical_revision,omitempty"`
	LogicalMemberIds           []int                           `json:"logical_member_ids,omitempty"`
	SharedSamplesJSON          ChannelSmartScheduleSamplesJSON `json:"shared_samples_json,omitempty"`
}

func (routes channelSmartScheduleMonitorRoutes) MarshalJSON() ([]byte, error) {
	var payload []channelSmartScheduleMonitorRouteJSON
	if routes != nil {
		payload = make([]channelSmartScheduleMonitorRouteJSON, len(routes))
	}
	for index, route := range routes {
		payload[index] = channelSmartScheduleMonitorRouteJSON{
			ChannelSmartScheduleRoute: route,
			StateRevision:             route.State.Revision, ManualPrimarySaved: route.State.ManualPrimarySaved,
			ManualPrimarySavedPriority: route.State.ManualPrimarySavedPriority,
			ManualPrimarySavedWeight:   route.State.ManualPrimarySavedWeight,
			LogicalChannelId:           route.LogicalChannelId, LogicalRevision: route.LogicalRevision,
			LogicalMemberIds: route.LogicalMemberIds, SharedSamplesJSON: route.SharedSamples.SamplesJSON,
		}
	}
	return common.Marshal(payload)
}

func (routes *channelSmartScheduleMonitorRoutes) UnmarshalJSON(data []byte) error {
	var payload []channelSmartScheduleMonitorRouteJSON
	if err := common.Unmarshal(data, &payload); err != nil {
		return err
	}
	var result channelSmartScheduleMonitorRoutes
	if payload != nil {
		result = make(channelSmartScheduleMonitorRoutes, len(payload))
	}
	for index, stored := range payload {
		route := stored.ChannelSmartScheduleRoute
		route.State.Revision = stored.StateRevision
		route.State.ManualPrimarySaved = stored.ManualPrimarySaved
		route.State.ManualPrimarySavedPriority = stored.ManualPrimarySavedPriority
		route.State.ManualPrimarySavedWeight = stored.ManualPrimarySavedWeight
		route.LogicalChannelId = stored.LogicalChannelId
		route.LogicalRevision = stored.LogicalRevision
		route.LogicalMemberIds = stored.LogicalMemberIds
		route.SharedSamples.SamplesJSON = stored.SharedSamplesJSON
		result[index] = route
	}
	*routes = result
	return nil
}

var channelSmartScheduleMonitorReadCache *channelSmartScheduleMonitorReadModel

func buildChannelSmartScheduleMonitorReadModel(db *gorm.DB, snapshot channelCacheSnapshot) (*channelSmartScheduleMonitorReadModel, error) {
	result := &channelSmartScheduleMonitorReadModel{Routes: make([]ChannelSmartScheduleRoute, 0, len(snapshot.abilities))}
	channels := make(map[int]*Channel, len(snapshot.channels))
	for _, channel := range snapshot.channels {
		channels[channel.Id] = channel
	}
	states := make(map[ChannelSmartScheduleRouteKey]ChannelSmartScheduleRouteState, len(snapshot.smartScheduleStates))
	for _, state := range snapshot.smartScheduleStates {
		states[channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)] = state
	}
	pauses := channelSmartScheduleGroupPauseUntilByKey(snapshot.smartScheduleGroupPauses)
	for _, ability := range snapshot.abilities {
		channel := channels[ability.ChannelId]
		if channel == nil {
			continue
		}
		key := channelSmartScheduleRouteKey(ability.ChannelId, ability.Group, ability.Model)
		priority, weight := channelSmartScheduleAbilityRouting(*ability)
		result.Routes = append(result.Routes, ChannelSmartScheduleRoute{
			ChannelId: channel.Id, ChannelName: channel.Name, ChannelStatus: channel.Status,
			ChannelPriority: channel.GetPriority(), ChannelWeight: uint(channel.GetWeight()),
			Group: ability.Group, Model: ability.Model, Enabled: ability.Enabled, Priority: priority, Weight: weight,
			TrafficPausedUntil: pauses[key], State: states[key],
		})
	}
	result.Economics.GroupRatios = ratio_setting.GetGroupRatioCopy()
	if db.Migrator().HasTable(&Option{}) {
		var options []Option
		if err := db.Where(clause.IN{Column: clause.Column{Name: "key"}, Values: []any{ChannelMonitorEconomicRevisionOption, "GroupRatio"}}).Find(&options).Error; err != nil {
			return nil, err
		}
		for _, option := range options {
			switch option.Key {
			case ChannelMonitorEconomicRevisionOption:
				result.Economics.Revision = option.Value
			case "GroupRatio":
				if strings.TrimSpace(option.Value) == "" {
					continue
				}
				if err := common.UnmarshalJsonStr(option.Value, &result.Economics.GroupRatios); err != nil {
					return nil, err
				}
			}
		}
	}
	if db.Migrator().HasTable(&ChannelRatioMonitor{}) {
		if err := db.Select("channel_id", "ratio", "cost_conversion", "updated_time").Find(&result.Economics.Monitors).Error; err != nil {
			return nil, err
		}
	}
	return result, nil
}

func channelSmartScheduleUseSharedReadModel() bool {
	return common.RedisEnabled && common.MemoryCacheEnabled
}

func loadChannelSmartScheduleMonitorReadModel(ctx context.Context, client *redis.Client, snapshot *channelSmartScheduleRouteSnapshot) {
	data, err := client.Get(ctx, channelSmartScheduleRouteSnapshotVersionKey(snapshot.Revision)+":monitor").Bytes()
	if err != nil {
		return
	}
	var readModel channelSmartScheduleMonitorReadModel
	if common.Unmarshal(data, &readModel) != nil {
		return
	}
	if readModel.Revision != snapshot.Revision || readModel.SourceWatermark != snapshot.SourceWatermark || readModel.GeneratedAt != snapshot.GeneratedAt {
		return
	}
	snapshot.Monitor = &readModel
}

func channelSmartScheduleSharedRoutes(group, modelName string) ([]ChannelSmartScheduleRoute, error) {
	channelSyncLock.RLock()
	cached := channelSmartScheduleMonitorReadCache
	metadata := channelSmartScheduleLocalSnapshotMetadataCache
	if cached == nil || metadata == nil || cached.Revision != metadata.Revision ||
		time.Since(time.UnixMilli(metadata.GeneratedAt)) > channelSmartScheduleRouteSnapshotMaxAgeDuration() {
		channelSyncLock.RUnlock()
		return nil, ErrChannelSmartScheduleRouteSnapshotUnavailable
	}
	rows := make([]ChannelSmartScheduleRoute, 0, len(cached.Routes))
	for _, route := range cached.Routes {
		if group != "" && route.Group != group || modelName != "" && route.Model != modelName {
			continue
		}
		rows = append(rows, route)
	}
	channelSyncLock.RUnlock()
	return cloneChannelSmartScheduleMonitorRoutes(rows)
}

func cloneChannelSmartScheduleMonitorRoutes(rows []ChannelSmartScheduleRoute) ([]ChannelSmartScheduleRoute, error) {
	// Callers annotate economics and score details. Give them detached state,
	// including pointer fields, while keeping the published snapshot immutable.
	payload, err := common.Marshal(channelSmartScheduleMonitorRoutes(rows))
	if err != nil {
		return nil, err
	}
	var result channelSmartScheduleMonitorRoutes
	if err := common.Unmarshal(payload, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func channelSmartScheduleSharedEconomics() (ChannelSmartScheduleEconomicSnapshot, error) {
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()
	cached := channelSmartScheduleMonitorReadCache
	metadata := channelSmartScheduleLocalSnapshotMetadataCache
	if cached == nil || metadata == nil || cached.Revision != metadata.Revision ||
		time.Since(time.UnixMilli(metadata.GeneratedAt)) > channelSmartScheduleRouteSnapshotMaxAgeDuration() {
		return ChannelSmartScheduleEconomicSnapshot{}, errors.New("智能调度 Redis 运行快照尚未就绪")
	}
	return cloneChannelSmartScheduleMonitorEconomics(cached.Economics), nil
}

func cloneChannelSmartScheduleMonitorEconomics(source ChannelSmartScheduleEconomicSnapshot) ChannelSmartScheduleEconomicSnapshot {
	result := ChannelSmartScheduleEconomicSnapshot{Revision: source.Revision,
		Monitors: append([]ChannelRatioMonitor(nil), source.Monitors...), GroupRatios: make(map[string]float64, len(source.GroupRatios))}
	for group, ratio := range source.GroupRatios {
		result.GroupRatios[group] = ratio
	}
	return result
}

// Cost settings change outside the request-statistics stream. Refresh their
// shared read model after the configuration transaction has committed.
func InvalidateChannelSmartScheduleReadModel() {
	if channelSmartScheduleUseSharedReadModel() {
		markAllChannelSmartScheduleRoutePoolsDirty()
	}
}
