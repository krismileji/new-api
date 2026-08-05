package model

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

// LookupChannelSmartScheduleProbeRoute reloads one route immediately before a
// probe is sent. It intentionally avoids the full route list because a probe
// run can contain many independent routes.
func LookupChannelSmartScheduleProbeRoute(
	channelId int,
	group string,
	modelName string,
) (ChannelSmartScheduleRoute, *Channel, bool, error) {
	group = strings.TrimSpace(group)
	modelNames := channelSmartScheduleRouteModelNames(modelName)
	if channelId <= 0 || group == "" || len(modelNames) == 0 {
		return ChannelSmartScheduleRoute{}, nil, false, nil
	}

	var ability Ability
	foundAbility := false
	for _, candidateModel := range modelNames {
		err := DB.Where(&Ability{
			ChannelId: channelId,
			Group:     group,
			Model:     candidateModel,
			Enabled:   true,
		}).First(&ability).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return ChannelSmartScheduleRoute{}, nil, false, err
		}
		foundAbility = true
		break
	}
	if !foundAbility {
		return ChannelSmartScheduleRoute{}, nil, false, nil
	}

	channel, err := GetChannelById(channelId, true)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ChannelSmartScheduleRoute{}, nil, false, nil
		}
		return ChannelSmartScheduleRoute{}, nil, false, err
	}

	var state ChannelSmartScheduleRouteState
	if err := DB.Where(&ChannelSmartScheduleRouteState{
		ChannelId: channelId,
		GroupName: group,
		ModelName: ability.Model,
	}).First(&state).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ChannelSmartScheduleRoute{}, nil, false, nil
		}
		return ChannelSmartScheduleRoute{}, nil, false, err
	}

	priority, weight := channelSmartScheduleAbilityRouting(ability, channel)
	return ChannelSmartScheduleRoute{
		ChannelId:       ability.ChannelId,
		ChannelName:     channel.Name,
		ChannelStatus:   channel.Status,
		ChannelPriority: channel.GetPriority(),
		ChannelWeight:   uint(channel.GetWeight()),
		Group:           ability.Group,
		Model:           ability.Model,
		Enabled:         ability.Enabled,
		Priority:        priority,
		Weight:          weight,
		State:           state,
	}, channel, true, nil
}
