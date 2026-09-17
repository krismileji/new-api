package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const channelPassiveMaxTargets = 5000

type channelPassiveTargetSnapshot struct {
	LoadedAt  int64
	Targets   []model.ChannelPassiveTarget
	ByChannel map[int][]model.ChannelPassiveTarget
}

var channelPassiveTargets atomic.Pointer[channelPassiveTargetSnapshot]

func RefreshChannelPassiveTargets(ctx context.Context) error {
	if model.DB == nil {
		return errors.New("业务周期监测配置数据库不可用")
	}
	var policies []model.ChannelRatioMonitor
	if err := model.DB.WithContext(ctx).Select("channel_id", "auto_probe_disabled", "probe_policy_revision", "probe_policy_updated_at").Where("auto_probe_disabled = ?", true).Limit(channelPassiveMaxTargets + 1).Find(&policies).Error; err != nil {
		return err
	}
	if len(policies) > channelPassiveMaxTargets {
		return errors.New("业务周期监测超过 5000 个目标的容量限制")
	}
	snapshot := &channelPassiveTargetSnapshot{LoadedAt: time.Now().Unix(), ByChannel: make(map[int][]model.ChannelPassiveTarget)}
	byChannel := make(map[int]model.ChannelRatioMonitor, len(policies))
	for _, policy := range policies {
		byChannel[policy.ChannelId] = policy
	}
	if len(policies) == 0 {
		channelPassiveTargets.Store(snapshot)
		return nil
	}
	relations, err := model.LoadChannelStatusProbeOverviewRelations(ctx, model.DB)
	if err != nil {
		return err
	}
	configs, err := model.GetChannelStatusProbeConfigsForOverview(ctx, model.DB, relations)
	if err != nil {
		return err
	}
	for _, config := range configs {
		policy, disabled := byChannel[config.ChannelId]
		if !disabled || !config.Enabled {
			continue
		}
		models, err := config.Models()
		if err != nil {
			return err
		}
		for _, modelName := range models {
			target := model.ChannelPassiveTarget{Scope: "status", ChannelID: config.ChannelId, ModelName: modelName,
				IntervalSeconds: config.IntervalSeconds, ConfigRevision: config.Revision, PolicyRevision: policy.ProbePolicyRevision,
				LogicalRevision: config.LogicalRevision, EffectiveAt: max(config.UpdatedAt, policy.ProbePolicyUpdatedAt)}
			target.ID = channelPassiveTargetID(target)
			snapshot.Targets = append(snapshot.Targets, target)
			snapshot.ByChannel[target.ChannelID] = append(snapshot.ByChannel[target.ChannelID], target)
		}
	}
	groupConfig, err := model.GetChannelGroupMonitorConfigOrDefaultWithContext(ctx)
	if err != nil {
		return err
	}
	if groupConfig.Enabled {
		groups, err := groupConfig.EnabledGroups()
		if err != nil {
			return err
		}
		var abilities []model.Ability
		// Include disabled abilities too: temporary routing state must not turn a
		// mixed group into a purported all-passive group.
		if err := model.DB.WithContext(ctx).Select("channel_id", "group", "model").Order("channel_id").Limit(model.ChannelGroupMonitorCandidateMaxAbilities + 1).Find(&abilities).Error; err != nil {
			return err
		}
		if len(abilities) > model.ChannelGroupMonitorCandidateMaxAbilities {
			return model.ErrChannelGroupMonitorCandidatesTooLarge
		}
		for _, group := range groups {
			var members []int
			var passive []int
			effectiveAt := groupConfig.UpdatedAt
			for _, ability := range abilities {
				if ability.Group != group.GroupName || ability.Model != group.ProbeModel {
					continue
				}
				members = append(members, ability.ChannelId)
				if policy, ok := byChannel[ability.ChannelId]; ok {
					passive = append(passive, ability.ChannelId)
					effectiveAt = max(effectiveAt, policy.ProbePolicyUpdatedAt)
				}
			}
			if len(passive) == 0 {
				continue
			}
			roster := make([]string, 0, len(members))
			for _, id := range members {
				roster = append(roster, fmt.Sprintf("%d:%d", id, byChannel[id].ProbePolicyRevision))
			}
			sort.Strings(roster)
			rosterJSON, _ := common.Marshal(roster)
			rosterHash := sha256.Sum256(rosterJSON)
			membership := hex.EncodeToString(rosterHash[:16])
			for _, id := range passive {
				target := model.ChannelPassiveTarget{Scope: "group_member", ChannelID: id, GroupName: group.GroupName, ModelName: group.ProbeModel,
					IntervalSeconds: groupConfig.IntervalSeconds, ConfigRevision: groupConfig.Revision, PolicyRevision: byChannel[id].ProbePolicyRevision,
					MembershipRevision: membership, EffectiveAt: effectiveAt}
				target.ID = channelPassiveTargetID(target)
				snapshot.Targets = append(snapshot.Targets, target)
				snapshot.ByChannel[id] = append(snapshot.ByChannel[id], target)
			}
			if len(members) == len(passive) {
				target := model.ChannelPassiveTarget{Scope: "group_final", GroupName: group.GroupName, ModelName: group.ProbeModel,
					IntervalSeconds: groupConfig.IntervalSeconds, ConfigRevision: groupConfig.Revision, MembershipRevision: membership, EffectiveAt: effectiveAt}
				target.ID = channelPassiveTargetID(target)
				snapshot.Targets = append(snapshot.Targets, target)
				for _, id := range passive {
					snapshot.ByChannel[id] = append(snapshot.ByChannel[id], target)
				}
			}
		}
	}
	if len(snapshot.Targets) > channelPassiveMaxTargets {
		return errors.New("业务周期监测超过 5000 个目标的容量限制")
	}
	channelPassiveTargets.Store(snapshot)
	return nil
}

func channelPassiveTargetID(target model.ChannelPassiveTarget) string {
	target.ID = ""
	data, _ := common.Marshal(target)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func captureChannelPassiveTargets(event *model.ChannelMonitorEvent) {
	if event.PassiveConfigReady {
		return
	}
	if event.Source != model.ChannelMonitorEventSourceBusiness && event.Source != model.ChannelMonitorEventSourceLocalResponse {
		return
	}
	snapshot := channelPassiveTargets.Load()
	if snapshot == nil || time.Now().Unix()-snapshot.LoadedAt > 15 {
		return
	}
	event.PassiveConfigReady = true
	for _, target := range snapshot.ByChannel[event.ChannelId] {
		if target.ModelName != event.ModelName || target.Scope != "status" && target.GroupName != event.GroupName || event.OccurredAt < target.EffectiveAt {
			continue
		}
		event.PassiveTargets = append(event.PassiveTargets, target)
	}
}

func channelPassiveSubject(target model.ChannelPassiveTarget) string {
	if target.Scope == "status" {
		return "status:" + strconv.Itoa(target.ChannelID)
	}
	hash := sha256.Sum256([]byte(target.GroupName))
	return "group:" + hex.EncodeToString(hash[:16])
}
