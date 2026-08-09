package model

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

type ChannelSmartScheduleGroupPause struct {
	Id          int64  `json:"id"`
	ChannelId   int    `json:"channel_id" gorm:"not null;uniqueIndex:idx_channel_smart_schedule_route_pause"`
	GroupName   string `json:"group" gorm:"type:varchar(64);not null;uniqueIndex:idx_channel_smart_schedule_route_pause"`
	ModelName   string `json:"model" gorm:"type:varchar(255);not null;default:'';uniqueIndex:idx_channel_smart_schedule_route_pause"`
	PausedUntil int64  `json:"paused_until" gorm:"bigint;index"`
}

const ChannelSmartScheduleGroupPauseMaxMinutes = 525600

const channelSmartScheduleGroupPauseLegacyIndex = "idx_channel_smart_schedule_group_pause"

type ChannelSmartScheduleGroupPauseResult struct {
	ChannelId      int    `json:"channel_id"`
	Group          string `json:"group"`
	Model          string `json:"model"`
	PausedUntil    int64  `json:"paused_until"`
	AffectedRoutes int    `json:"affected_routes"`
	Changed        bool   `json:"changed"`
}

func (route ChannelSmartScheduleRoute) TrafficPaused(now int64) bool {
	if now <= 0 {
		now = common.GetTimestamp()
	}
	return route.TrafficPausedUntil > now
}

func SaveChannelSmartScheduleGroupPause(
	channelId int,
	group string,
	modelName string,
	durationMinutes int,
) (result ChannelSmartScheduleGroupPauseResult, err error) {
	group = strings.TrimSpace(group)
	modelName = strings.TrimSpace(modelName)
	if channelId <= 0 || group == "" || modelName == "" {
		return result, gorm.ErrRecordNotFound
	}
	if durationMinutes < 0 || durationMinutes > ChannelSmartScheduleGroupPauseMaxMinutes {
		return result, fmt.Errorf(
			"路由流量暂停时间必须在 0 到 %d 分钟之间",
			ChannelSmartScheduleGroupPauseMaxMinutes,
		)
	}

	now := common.GetTimestamp()
	pausedUntil := int64(0)
	if durationMinutes > 0 {
		pausedUntil = now + int64(durationMinutes)*60
	}
	result = ChannelSmartScheduleGroupPauseResult{
		ChannelId:   channelId,
		Group:       group,
		Model:       modelName,
		PausedUntil: pausedUntil,
	}

	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	err = DB.Transaction(func(tx *gorm.DB) error {
		var channel Channel
		if err := lockForUpdate(tx).
			Select("id").
			Where("id = ?", channelId).
			First(&channel).Error; err != nil {
			return err
		}

		var ability Ability
		if err := lockForUpdate(tx).
			Select("channel_id", "group", "model").
			Where(&Ability{ChannelId: channelId, Group: group, Model: modelName}).
			First(&ability).Error; err != nil {
			return err
		}
		result.AffectedRoutes = 1

		var pause ChannelSmartScheduleGroupPause
		findErr := lockForUpdate(tx).
			Where(&ChannelSmartScheduleGroupPause{
				ChannelId: channelId, GroupName: group, ModelName: modelName,
			}).
			First(&pause).Error
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}

		if durationMinutes == 0 {
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				return nil
			}
			if err := tx.Delete(&pause).Error; err != nil {
				return err
			}
			result.Changed = true
		} else if errors.Is(findErr, gorm.ErrRecordNotFound) {
			pause = ChannelSmartScheduleGroupPause{
				ChannelId: channelId, GroupName: group, ModelName: modelName, PausedUntil: pausedUntil,
			}
			if err := tx.Create(&pause).Error; err != nil {
				return err
			}
			result.Changed = true
		} else if pause.PausedUntil != pausedUntil {
			if err := tx.Model(&pause).Update("paused_until", pausedUntil).Error; err != nil {
				return err
			}
			result.Changed = true
		}

		if !result.Changed || !tx.Migrator().HasTable(&ChannelSmartScheduleRouteState{}) {
			return nil
		}
		var states []ChannelSmartScheduleRouteState
		if err := lockForUpdate(tx).
			Where("channel_id = ? AND group_name = ? AND model_name = ?", channelId, group, modelName).
			Find(&states).Error; err != nil {
			return err
		}
		for index := range states {
			if states[index].Revision == math.MaxInt64 {
				return errors.New("智能调度路由修订号已达上限")
			}
			states[index].Revision++
			if err := tx.Model(&states[index]).Update("revision", states[index].Revision).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return result, err
}

func prepareChannelSmartScheduleGroupPauseMigration(db *gorm.DB) error {
	// The first release indexed only channel and group. Remove that constraint
	// before AutoMigrate adds model_name and the route-scoped unique index.
	if db == nil || !db.Migrator().HasTable(&ChannelSmartScheduleGroupPause{}) ||
		!db.Migrator().HasIndex(&ChannelSmartScheduleGroupPause{}, channelSmartScheduleGroupPauseLegacyIndex) {
		return nil
	}
	if err := db.Migrator().DropIndex(
		&ChannelSmartScheduleGroupPause{}, channelSmartScheduleGroupPauseLegacyIndex,
	); err != nil {
		if !db.Migrator().HasIndex(&ChannelSmartScheduleGroupPause{}, channelSmartScheduleGroupPauseLegacyIndex) {
			return nil
		}
		return err
	}
	return nil
}

func loadActiveChannelSmartScheduleGroupPauses(
	db *gorm.DB,
	now int64,
) ([]ChannelSmartScheduleGroupPause, error) {
	if db == nil || !db.Migrator().HasTable(&ChannelSmartScheduleGroupPause{}) {
		return nil, nil
	}
	if now <= 0 {
		now = common.GetTimestamp()
	}
	var pauses []ChannelSmartScheduleGroupPause
	if err := db.
		Where("paused_until > ?", now).
		Order("channel_id ASC, group_name ASC, model_name ASC").
		Find(&pauses).Error; err != nil {
		return nil, err
	}
	return pauses, nil
}

func channelSmartScheduleGroupPauseUntilByKey(
	pauses []ChannelSmartScheduleGroupPause,
) map[ChannelSmartScheduleRouteKey]int64 {
	pausedUntilByKey := make(map[ChannelSmartScheduleRouteKey]int64, len(pauses))
	for _, pause := range pauses {
		if pause.ChannelId <= 0 || pause.GroupName == "" || pause.ModelName == "" {
			continue
		}
		pausedUntilByKey[channelSmartScheduleRouteKey(
			pause.ChannelId, pause.GroupName, pause.ModelName,
		)] = pause.PausedUntil
	}
	return pausedUntilByKey
}

func loadActiveChannelSmartSchedulePausedChannelIds(
	db *gorm.DB,
	group string,
	modelName string,
	channelIds []int,
	now int64,
) (map[int]struct{}, error) {
	paused := make(map[int]struct{})
	if db == nil || group == "" || modelName == "" || len(channelIds) == 0 ||
		!db.Migrator().HasTable(&ChannelSmartScheduleGroupPause{}) {
		return paused, nil
	}
	if now <= 0 {
		now = common.GetTimestamp()
	}
	var pausedChannelIds []int
	if err := db.Model(&ChannelSmartScheduleGroupPause{}).
		Where(
			"group_name = ? AND model_name = ? AND channel_id IN ? AND paused_until > ?",
			group, modelName, channelIds, now,
		).
		Pluck("channel_id", &pausedChannelIds).Error; err != nil {
		return nil, err
	}
	for _, channelId := range pausedChannelIds {
		paused[channelId] = struct{}{}
	}
	return paused, nil
}
