package model

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"

	"github.com/samber/lo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Ability struct {
	Group     string  `json:"group" gorm:"type:varchar(64);primaryKey;autoIncrement:false"`
	Model     string  `json:"model" gorm:"type:varchar(255);primaryKey;autoIncrement:false"`
	ChannelId int     `json:"channel_id" gorm:"primaryKey;autoIncrement:false;index"`
	Enabled   bool    `json:"enabled"`
	Priority  *int64  `json:"priority" gorm:"bigint;default:0;index"`
	Weight    uint    `json:"weight" gorm:"default:0;index"`
	Tag       *string `json:"tag" gorm:"index"`
}

type AbilityWithChannel struct {
	Ability
	ChannelType int `json:"channel_type"`
}

func GetAllEnableAbilityWithChannels() ([]AbilityWithChannel, error) {
	var abilities []AbilityWithChannel
	err := DB.Table("abilities").
		Select("abilities.*, channels.type as channel_type").
		Joins("left join channels on abilities.channel_id = channels.id").
		Where("abilities.enabled = ?", true).
		Scan(&abilities).Error
	return abilities, err
}

func GetGroupEnabledModels(group string) []string {
	var models []string
	// Find distinct models
	DB.Table("abilities").Where(commonGroupCol+" = ? and enabled = ?", group, true).Distinct("model").Pluck("model", &models)
	return models
}

func GetEnabledModels() []string {
	var models []string
	// Find distinct models
	DB.Table("abilities").Where("enabled = ?", true).Distinct("model").Pluck("model", &models)
	return models
}

func GetAllEnableAbilities() []Ability {
	var abilities []Ability
	DB.Find(&abilities, "enabled = ?", true)
	return abilities
}

func getPriority(group string, model string, retry int, options ChannelSelectionOptions) (int, error) {

	var priorities []int
	priorityQuery := DB.Model(&Ability{}).
		Select("DISTINCT(priority)").
		Where(commonGroupCol+" = ? and model = ? and enabled = ?", group, model, true)
	priorityQuery = applyChannelSelectionOptions(priorityQuery, options)
	err := priorityQuery.Order("priority DESC").Pluck("priority", &priorities).Error

	if err != nil {
		// 处理错误
		return 0, err
	}

	if len(priorities) == 0 {
		// 如果没有查询到优先级，则返回错误
		return 0, errors.New("数据库一致性被破坏")
	}

	// 确定要使用的优先级
	var priorityToUse int
	if retry >= len(priorities) {
		// 如果重试次数大于优先级数，则使用最小的优先级
		priorityToUse = priorities[len(priorities)-1]
	} else {
		priorityToUse = priorities[retry]
	}
	return priorityToUse, nil
}

func getChannelQuery(group string, model string, retry int, options ChannelSelectionOptions) (*gorm.DB, error) {
	maxPrioritySubQuery := applyChannelSelectionOptions(
		DB.Model(&Ability{}).Select("MAX(priority)").Where(commonGroupCol+" = ? and model = ? and enabled = ?", group, model, true),
		options,
	)
	channelQuery := applyChannelSelectionOptions(
		DB.Where(commonGroupCol+" = ? and model = ? and enabled = ? and priority = (?)", group, model, true, maxPrioritySubQuery),
		options,
	)
	if retry != 0 {
		priority, err := getPriority(group, model, retry, options)
		if err != nil {
			return nil, err
		} else {
			channelQuery = applyChannelSelectionOptions(
				DB.Where(commonGroupCol+" = ? and model = ? and enabled = ? and priority = ?", group, model, true, priority),
				options,
			)
		}
	}

	return channelQuery, nil
}

func GetChannel(group string, model string, retry int, requestPath string, options ...ChannelSelectionOptions) (*Channel, error) {
	var abilities []Ability

	var err error = nil
	selectionOptions := channelSelectionOptions(options)
	if selectionOptions.HasExcludedChannels() {
		retry = 0
	}
	channelQuery, err := getChannelQuery(group, model, retry, selectionOptions)
	if err != nil {
		return nil, err
	}
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) || common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		err = channelQuery.Order("weight DESC").Find(&abilities).Error
	} else {
		err = channelQuery.Order("weight DESC").Find(&abilities).Error
	}
	if err != nil {
		return nil, err
	}
	abilities = filterAbilitiesByRequestPathAndModel(abilities, requestPath, model)
	channel := Channel{}
	if len(abilities) > 0 {
		channelIDs := make([]int, len(abilities))
		weights := make([]uint, len(abilities))
		for index, ability := range abilities {
			channelIDs[index] = ability.ChannelId
			weights[index] = ability.Weight
		}
		channel.Id, err = chooseChannelByWeights(channelIDs, weights)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, nil
	}
	err = DB.First(&channel, "id = ?", channel.Id).Error
	return &channel, err
}

// filterAbilitiesByRequestPathAndModel restricts candidates by request path and
// model for the DB (non-memory-cache) selection path. Only Advanced Custom
// (type 58) channels are path-checked: kept only when one of their routes matches
// requestPath and model; all other channel types always pass. When requestPath is
// empty, filtering is skipped.
func filterAbilitiesByRequestPathAndModel(abilities []Ability, requestPath string, model string) []Ability {
	if requestPath == "" || len(abilities) == 0 {
		return abilities
	}

	channelIds := make([]int, 0, len(abilities))
	seen := make(map[int]struct{}, len(abilities))
	for _, ability := range abilities {
		if _, ok := seen[ability.ChannelId]; ok {
			continue
		}
		seen[ability.ChannelId] = struct{}{}
		channelIds = append(channelIds, ability.ChannelId)
	}

	var channels []*Channel
	if err := DB.Where("id IN ?", channelIds).Find(&channels).Error; err != nil {
		// On error, fall back to unfiltered candidates to avoid blocking selection
		return abilities
	}

	advancedConfigs := make(map[int]*dto.AdvancedCustomConfig)
	for _, channel := range channels {
		if channel.Type == constant.ChannelTypeAdvancedCustom {
			advancedConfigs[channel.Id] = channel.GetOtherSettings().AdvancedCustom
		}
	}

	filtered := make([]Ability, 0, len(abilities))
	for _, ability := range abilities {
		config, isAdvancedCustom := advancedConfigs[ability.ChannelId]
		if !isAdvancedCustom {
			filtered = append(filtered, ability)
			continue
		}
		if config != nil && config.SupportsPathForModel(requestPath, model) {
			filtered = append(filtered, ability)
		}
	}
	return filtered
}

func (channel *Channel) AddAbilities(tx *gorm.DB) error {
	models_ := strings.Split(channel.Models, ",")
	groups_ := strings.Split(channel.Group, ",")
	abilitySet := make(map[string]struct{})
	abilities := make([]Ability, 0, len(models_))
	for _, model := range models_ {
		for _, group := range groups_ {
			key := group + "|" + model
			if _, exists := abilitySet[key]; exists {
				continue
			}
			abilitySet[key] = struct{}{}
			ability := Ability{
				Group:     group,
				Model:     model,
				ChannelId: channel.Id,
				Enabled:   channel.Status == common.ChannelStatusEnabled,
				Priority:  channel.Priority,
				Weight:    uint(channel.GetWeight()),
				Tag:       channel.Tag,
			}
			abilities = append(abilities, ability)
		}
	}
	if len(abilities) == 0 {
		return nil
	}
	// choose DB or provided tx
	useDB := DB
	if tx != nil {
		useDB = tx
	}
	for _, chunk := range lo.Chunk(abilities, 50) {
		err := useDB.Clauses(clause.OnConflict{DoNothing: true}).Create(&chunk).Error
		if err != nil {
			return err
		}
	}
	return nil
}

func (channel *Channel) DeleteAbilities() error {
	return deleteChannelAbilities(channel.Id)
}

// UpdateAbilities updates abilities of this channel.
// Make sure the channel is completed before calling this function.
func (channel *Channel) UpdateAbilities(tx *gorm.DB) error {
	isNewTx := false
	// 如果没有传入事务，创建新的事务
	if tx == nil {
		channelStatusLock.Lock()
		defer channelStatusLock.Unlock()
		tx = DB.Begin()
		if tx.Error != nil {
			return tx.Error
		}
		isNewTx = true
		defer func() {
			if r := recover(); r != nil {
				tx.Rollback()
				panic(r)
			}
		}()
		var current Channel
		if err := lockForUpdate(tx).Where("id = ?", channel.Id).First(&current).Error; err != nil {
			tx.Rollback()
			return err
		}
		channel = &current
	}
	var currentAbilities []Ability
	if err := tx.Select("group", "model").Where("channel_id = ?", channel.Id).
		Find(&currentAbilities).Error; err != nil {
		if isNewTx {
			tx.Rollback()
		}
		return err
	}
	smartSchedulePools := channelSmartScheduleRoutePoolsFromAbilities(
		currentAbilities,
		channelSmartScheduleRoutePools(channel.Group, channel.Models)...,
	)
	if err := lockChannelSmartScheduleRoutePoolsTx(tx, smartSchedulePools); err != nil {
		if isNewTx {
			tx.Rollback()
		}
		return err
	}
	routingByKey, err := getChannelSmartScheduleRouteRouting(tx, channel.Id)
	if err != nil {
		if isNewTx {
			tx.Rollback()
		}
		return err
	}

	// First delete all abilities of this channel
	err = tx.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error
	if err != nil {
		if isNewTx {
			tx.Rollback()
		}
		return err
	}

	// Then add new abilities
	models_ := strings.Split(channel.Models, ",")
	groups_ := strings.Split(channel.Group, ",")
	abilitySet := make(map[string]struct{})
	activeRoutes := make(map[ChannelSmartScheduleRouteKey]struct{})
	abilities := make([]Ability, 0, len(models_))
	for _, model := range models_ {
		for _, group := range groups_ {
			key := group + "|" + model
			if _, exists := abilitySet[key]; exists {
				continue
			}
			abilitySet[key] = struct{}{}
			activeRoutes[channelSmartScheduleRouteKey(channel.Id, group, model)] = struct{}{}
			ability := Ability{
				Group:     group,
				Model:     model,
				ChannelId: channel.Id,
				Enabled:   channel.Status == common.ChannelStatusEnabled,
				Priority:  channel.Priority,
				Weight:    uint(channel.GetWeight()),
				Tag:       channel.Tag,
			}
			if routing, ok := routingByKey[channelSmartScheduleRouteKey(channel.Id, group, model)]; ok {
				ability.Priority = routing.priority
				ability.Weight = routing.weight
			}
			abilities = append(abilities, ability)
		}
	}

	if len(abilities) > 0 {
		for _, chunk := range lo.Chunk(abilities, 50) {
			err = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&chunk).Error
			if err != nil {
				if isNewTx {
					tx.Rollback()
				}
				return err
			}
		}
	}
	// Reconcile the old pools while removed fixed-primary states still exist.
	// This lets a moved fixed route withdraw pool-wide temporary traffic before
	// its obsolete state is deleted.
	if err = reapplyChannelSmartScheduleRoutePrimariesTx(tx, smartSchedulePools); err != nil {
		if isNewTx {
			tx.Rollback()
		}
		return err
	}
	if err = deleteObsoleteChannelSmartScheduleRouteStates(tx, channel.Id, activeRoutes); err != nil {
		if isNewTx {
			tx.Rollback()
		}
		return err
	}

	// 如果是新创建的事务，需要提交
	if isNewTx {
		return tx.Commit().Error
	}

	return nil
}

func UpdateAbilityStatus(channelId int, status bool) error {
	return updateAbilityStatusWithPrimaries(channelId, status)
}

func UpdateAbilityStatusByTag(tag string, status bool) error {
	return updateAbilitiesByTagWithPrimaries(tag, status)
}

var fixLock = sync.Mutex{}

func FixAbility() (int, int, error) {
	lock := fixLock.TryLock()
	if !lock {
		return 0, 0, errors.New("已经有一个修复任务在运行中，请稍后再试")
	}
	defer fixLock.Unlock()
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	var channels []*Channel
	// Find all channels
	err := DB.Model(&Channel{}).Find(&channels).Error
	if err != nil {
		return 0, 0, err
	}
	successCount := 0
	failCount := 0
	for _, channel := range channels {
		err = DB.Transaction(func(tx *gorm.DB) error {
			var current Channel
			if err := lockForUpdate(tx).Where("id = ?", channel.Id).First(&current).Error; err != nil {
				return err
			}
			return current.UpdateAbilities(tx)
		})
		if err != nil {
			common.SysLog(fmt.Sprintf("Update abilities for channel %d failed: %s", channel.Id, err.Error()))
			failCount++
			continue
		}
		successCount++
	}
	if err = DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where(
			"NOT EXISTS (?)",
			tx.Model(&Channel{}).Select("1").Where("channels.id = abilities.channel_id"),
		).Delete(&Ability{}).Error; err != nil {
			return err
		}
		return deleteChannelSmartScheduleRouteStatesForMissingChannels(tx)
	}); err != nil {
		return successCount, failCount, err
	}
	InitChannelCache()
	return successCount, failCount, nil
}
