package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/google/uuid"
)

type pricingGroupMonitorSnapshot struct {
	Enabled          bool                              `json:"enabled"`
	ServerNow        int64                             `json:"server_now"`
	DataCutoffAt     int64                             `json:"data_cutoff_at"`
	ShowCacheRate    bool                              `json:"show_cache_rate"`
	CacheMinContextK int                               `json:"cache_min_context_k"`
	DisplayValue     int                               `json:"display_value"`
	DisplayUnit      string                            `json:"display_unit"`
	Categories       []string                          `json:"categories"`
	Items            []pricingGroupMonitorItemResponse `json:"items"`
}

type channelGroupMonitorSnapshot struct {
	Generation          string                              `json:"generation"`
	EventVersion        string                              `json:"event_version"`
	GroupVersions       map[string]string                   `json:"group_versions"`
	MetadataFingerprint string                              `json:"metadata_fingerprint"`
	NextRefreshAt       int64                               `json:"next_refresh_at"`
	Overview            channelGroupMonitorOverviewResponse `json:"overview"`
	Public              pricingGroupMonitorSnapshot         `json:"public"`
	UserRatios          map[string]map[string]float64       `json:"user_ratios"`
}

// Refresh independently of page traffic, with a lease shared by all nodes.
// A slow/abandoned builder cannot overwrite the next lease owner's snapshot.
func refreshChannelGroupMonitorSnapshot(ctx context.Context, forceMetadata ...bool) error {
	client := common.RedisMonitorWriteClient()
	if !common.RedisEnabled || client == nil {
		return service.ErrChannelGroupMonitorSnapshotPending
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	config, err := model.GetChannelGroupMonitorConfigOrDefaultWithContext(ctx)
	if err != nil {
		return err
	}
	if err := service.UpdateChannelGroupMonitorCachePolicy(config); err != nil {
		return err
	}
	generation, err := service.SyncChannelGroupMonitorGeneration(ctx, config)
	if err != nil {
		return err
	}
	owner := uuid.NewString()
	lease := service.ChannelGroupMonitorRedisPrefix + "snapshot_lease"
	claimed, err := client.SetNX(ctx, lease, owner, 15*time.Second).Result()
	if err != nil || !claimed {
		return err
	}
	settings, err := channelGroupMonitorConfigToResponse(config)
	if err != nil {
		return err
	}
	groups, err := config.Groups()
	if err != nil {
		return err
	}
	metadataState, err := loadChannelGroupMonitorRoutingMetadata(ctx, config, generation, len(forceMetadata) > 0 && forceMetadata[0])
	if err != nil {
		return err
	}
	candidates, enabledCandidates := metadataState.Candidates, metadataState.EnabledCandidates
	generation, err = service.SyncChannelGroupMonitorGeneration(ctx, config, metadataState.Fingerprint)
	if err != nil {
		return err
	}
	if err := service.UpdateChannelGroupMonitorCachePolicy(config); err != nil {
		return err
	}
	now := common.GetTimestamp()
	version, err := service.ChannelGroupMonitorProjectionVersion(ctx, generation)
	if err != nil {
		return err
	}
	// Metadata may change without a request event (descriptions, pricing,
	// channel availability, manual-run state, or completed passive periods).
	// Check those inputs without reading any group statistics buckets.
	metadataItems := make([]channelGroupMonitorItemResponse, 0, len(groups))
	for _, group := range groups {
		metadataItems = append(metadataItems, channelGroupMonitorItemResponse{Group: group.GroupName, ProbeModel: group.ProbeModel, ConfigValid: groupMonitorModelIsCandidate(candidates, group.GroupName, group.ProbeModel)})
	}
	applyChannelGroupPassiveOverviewSince(ctx, metadataItems, generation.StartedAt)
	var userRatios map[string]map[string]float64
	if err := common.UnmarshalJsonStr(ratio_setting.GroupGroupRatio2JSONString(), &userRatios); err != nil {
		return err
	}
	metadata, err := common.Marshal(struct {
		Settings                      channelGroupMonitorConfigResponse
		Candidates, EnabledCandidates map[string][]string
		Items                         []pricingGroupMonitorItemResponse
		UserRatios                    map[string]map[string]float64
	}{settings, candidates, enabledCandidates, buildPricingGroupMonitorItems(config, metadataItems), userRatios})
	if err != nil {
		return err
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(metadata))
	var previous channelGroupMonitorSnapshot
	if err := service.ReadChannelGroupMonitorSnapshot(ctx, &previous); err == nil &&
		previous.Generation == generation.ID && previous.EventVersion == version &&
		previous.MetadataFingerprint == fingerprint && now < previous.NextRefreshAt {
		renewed, err := service.RenewChannelGroupMonitorSnapshot(ctx, generation, version, previous.NextRefreshAt, lease, owner)
		if err != nil {
			return err
		}
		if renewed {
			return nil
		}
	}
	versions, err := service.ChannelGroupMonitorGroupVersions(ctx, generation)
	if err != nil {
		return err
	}
	reuse := make(map[string]channelGroupMonitorItemResponse)
	if previous.Generation == generation.ID && previous.MetadataFingerprint == fingerprint && now < previous.NextRefreshAt {
		for _, item := range previous.Overview.Items {
			if previous.GroupVersions[item.Group] == versions[item.Group] {
				reuse[item.Group] = item
			}
		}
	}
	items, err := buildChannelGroupMonitorItems(ctx, config, candidates, now, channelGroupMonitorBuildInputs{Reuse: reuse, EnabledCandidates: enabledCandidates})
	if err != nil {
		return err
	}
	snapshot := channelGroupMonitorSnapshot{
		Generation: generation.ID, EventVersion: version, GroupVersions: versions, MetadataFingerprint: fingerprint,
		NextRefreshAt: channelGroupMonitorNextRefreshAt(config, items, now), UserRatios: userRatios,
		Overview: channelGroupMonitorOverviewResponse{ServerNow: now, Settings: settings, CandidateModelsByGroup: candidates, Items: items},
		Public:   pricingGroupMonitorSnapshot{Enabled: config.Enabled, ServerNow: now, DataCutoffAt: max(generation.StartedAt, now-channelGroupMonitorDisplaySeconds(settings.DisplayValue, settings.DisplayUnit)), ShowCacheRate: settings.ShowCacheRate, CacheMinContextK: settings.CacheMinContextK, DisplayValue: settings.DisplayValue, DisplayUnit: settings.DisplayUnit, Categories: settings.Categories, Items: buildPricingGroupMonitorItems(config, items)},
	}
	return service.PublishChannelGroupMonitorSnapshot(ctx, generation, snapshot, lease, owner)
}

// Rolling buckets and probe staleness must advance even without any new events.
func channelGroupMonitorNextRefreshAt(config model.ChannelGroupMonitorConfig, items []channelGroupMonitorItemResponse, now int64) int64 {
	_, unit := model.NormalizeChannelStatusProbeDisplay(config.DisplayValue, config.DisplayUnit)
	next := model.ChannelStatusProbeDisplayBucketStart(now, unit) + model.ChannelStatusProbeDisplayBucketSeconds(unit)
	for _, item := range items {
		if item.Status == channelGroupMonitorHealthPaused {
			continue
		}
		if item.Passive != nil && item.Passive.IntervalSeconds > 0 {
			interval := int64(item.Passive.IntervalSeconds)
			next = min(next, now-now%interval+interval)
		} else if item.LastFinishedAt > 0 {
			staleAt := item.LastFinishedAt + int64(config.IntervalSeconds)*2 + 1
			if staleAt > now {
				next = min(next, staleAt)
			}
		}
	}
	return next
}

// Only channel/ability metadata is cached here. Configuration saves, pricing,
// descriptions and passive results still participate in every snapshot check.
// The generation key prevents cross-configuration reuse, including after Redis loss.
type channelGroupMonitorRoutingMetadata struct {
	Generation                    string
	CheckedAt                     time.Time
	Candidates, EnabledCandidates map[string][]string
	Fingerprint                   string
}

var channelGroupMonitorRoutingCache struct {
	sync.Mutex
	Value channelGroupMonitorRoutingMetadata
}

func loadChannelGroupMonitorRoutingMetadata(ctx context.Context, config model.ChannelGroupMonitorConfig, generation service.ChannelGroupMonitorGeneration, force bool) (channelGroupMonitorRoutingMetadata, error) {
	channelGroupMonitorRoutingCache.Lock()
	cached := channelGroupMonitorRoutingCache.Value
	channelGroupMonitorRoutingCache.Unlock()
	if !force && cached.Generation == generation.ID && time.Since(cached.CheckedAt) < 15*time.Second {
		return cached, nil
	}
	result := channelGroupMonitorRoutingMetadata{Generation: generation.ID, CheckedAt: time.Now()}
	var err error
	result.Candidates, err = getChannelGroupMonitorCandidateModels(ctx, false)
	if err != nil {
		return result, err
	}
	result.EnabledCandidates, err = getChannelGroupMonitorCandidateModels(ctx, true)
	if err != nil {
		return result, err
	}
	groups, err := config.Groups()
	if err != nil {
		return result, err
	}
	routes := make(map[string][]model.Ability, len(groups))
	groupNames := make([]string, 0, len(groups))
	probeModels := make(map[string]string, len(groups))
	for _, group := range groups {
		groupNames = append(groupNames, group.GroupName)
		probeModels[group.GroupName] = group.ProbeModel
	}
	if len(groupNames) > 0 {
		var abilities []model.Ability
		if err := model.DB.WithContext(ctx).Select([]string{"group", "channel_id", "model"}).Where(map[string]any{"group": groupNames}).Order("channel_id ASC, model ASC").Find(&abilities).Error; err != nil {
			return result, err
		}
		for _, ability := range abilities {
			if probeModels[ability.Group] == ability.Model {
				routes[ability.Group] = append(routes[ability.Group], ability)
			}
		}
	}
	payload, err := common.Marshal(routes)
	if err != nil {
		return result, err
	}
	result.Fingerprint = fmt.Sprintf("%x", sha256.Sum256(payload))
	channelGroupMonitorRoutingCache.Lock()
	channelGroupMonitorRoutingCache.Value = result
	channelGroupMonitorRoutingCache.Unlock()
	return result, nil
}
