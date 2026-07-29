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
	modelName = strings.TrimSpace(modelName)
	if channelId <= 0 || group == "" || modelName == "" {
		return ChannelSmartScheduleRoute{}, nil, false, nil
	}

	var ability Ability
	if err := DB.Where(&Ability{ChannelId: channelId, Group: group, Model: modelName}).First(&ability).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ChannelSmartScheduleRoute{}, nil, false, nil
		}
		return ChannelSmartScheduleRoute{}, nil, false, err
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
		ModelName: modelName,
	}).First(&state).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ChannelSmartScheduleRoute{}, nil, false, nil
		}
		return ChannelSmartScheduleRoute{}, nil, false, err
	}

	return ChannelSmartScheduleRoute{
		ChannelId:       ability.ChannelId,
		ChannelName:     channel.Name,
		ChannelStatus:   channel.Status,
		ChannelPriority: channel.GetPriority(),
		ChannelWeight:   uint(channel.GetWeight()),
		Group:           ability.Group,
		Model:           ability.Model,
		Enabled:         ability.Enabled,
		Priority:        abilityPriority(ability),
		Weight:          ability.Weight,
		State:           state,
	}, channel, true, nil
}
