package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

var (
	ErrChannelModelDetectionChannelNotFound  = errors.New("渠道不存在")
	ErrChannelModelDetectionConfigNotFound   = errors.New("该渠道尚未配置模型检测目标")
	ErrChannelModelDetectionInvalidConfig    = errors.New("模型检测渠道配置无效")
	ErrChannelModelDetectionRevisionConflict = errors.New("模型检测渠道配置已被其他管理员更新，请刷新后重试")
	ErrChannelModelDetectionTargetNotFound   = errors.New("模型检测目标不存在")
	ErrChannelModelDetectionEstimateInvalid  = errors.New("模型检测估算结果无效")
)

type ChannelModelDetectionTargetUpdateInput struct {
	TargetKey    string
	RequestModel string
	ClaimedModel string
}

type ChannelModelDetectionConfigUpdateInput struct {
	ScheduleEnabled  bool
	Targets          []ChannelModelDetectionTargetUpdateInput
	ExpectedRevision int64
}

type ChannelModelDetectionTargetResponse struct {
	TargetKey    string `json:"target_key"`
	RequestModel string `json:"request_model"`
	ClaimedModel string `json:"claimed_model"`
	Enabled      bool   `json:"enabled"`
	Position     int    `json:"position"`
}

type ChannelModelDetectionConfigResponse struct {
	ChannelID       int                                   `json:"channel_id"`
	ScheduleEnabled bool                                  `json:"schedule_enabled"`
	Revision        int64                                 `json:"revision"`
	CreatedAt       int64                                 `json:"created_at"`
	UpdatedAt       int64                                 `json:"updated_at"`
	Targets         []ChannelModelDetectionTargetResponse `json:"targets"`
}

type ChannelModelDetectionConfigChange struct {
	ChannelID   int
	OldRevision int64
	NewRevision int64
}

// ChannelModelDetectionConfigChangeHook is deliberately independent of the
// detector/session implementation. It is invoked only after the DB commit.
type ChannelModelDetectionConfigChangeHook func(context.Context, ChannelModelDetectionConfigChange)

var channelModelDetectionConfigChangeHook struct {
	sync.RWMutex
	fn ChannelModelDetectionConfigChangeHook
}

// SetChannelModelDetectionConfigChangeHook installs a narrow post-commit hook.
// It is primarily used by the scheduler integration and deterministic tests.
func SetChannelModelDetectionConfigChangeHook(fn ChannelModelDetectionConfigChangeHook) func() {
	channelModelDetectionConfigChangeHook.Lock()
	previous := channelModelDetectionConfigChangeHook.fn
	channelModelDetectionConfigChangeHook.fn = fn
	channelModelDetectionConfigChangeHook.Unlock()
	return func() {
		channelModelDetectionConfigChangeHook.Lock()
		channelModelDetectionConfigChangeHook.fn = previous
		channelModelDetectionConfigChangeHook.Unlock()
	}
}

func channelModelDetectionConfigHook() ChannelModelDetectionConfigChangeHook {
	channelModelDetectionConfigChangeHook.RLock()
	defer channelModelDetectionConfigChangeHook.RUnlock()
	return channelModelDetectionConfigChangeHook.fn
}

func UpdateChannelModelDetectionConfig(ctx context.Context, tx *gorm.DB, channelID int, input ChannelModelDetectionConfigUpdateInput, now time.Time) (ChannelModelDetectionConfigResponse, error) {
	db := tx
	if db == nil {
		db = model.DB
	}
	if db == nil {
		return ChannelModelDetectionConfigResponse{}, errors.New("模型检测数据库不可用")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if channelID <= 0 {
		return ChannelModelDetectionConfigResponse{}, ErrChannelModelDetectionChannelNotFound
	}
	if len(input.Targets) < 1 || len(input.Targets) > 10 {
		return ChannelModelDetectionConfigResponse{}, fmt.Errorf("%w: 目标数量必须在 1 到 10 之间", ErrChannelModelDetectionInvalidConfig)
	}
	useLogicalRuntime := db == model.DB
	requestedChannelID := channelID

	var response ChannelModelDetectionConfigResponse
	var change ChannelModelDetectionConfigChange
	err := db.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		// Always reload the channel in the same transaction. The caller cannot
		// validate a stale model list supplied by the browser.
		var channel model.Channel
		if err := transaction.Where("id = ?", channelID).First(&channel).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrChannelModelDetectionChannelNotFound
			}
			return err
		}
		identity, err := channelModelDetectionLogicalIdentity(transaction, channelID, useLogicalRuntime)
		if err != nil {
			return err
		}
		if identity.Revision > 0 {
			logicalResponse, logicalChange, logicalErr := updateChannelModelDetectionLogicalConfig(transaction, identity, requestedChannelID, input, now)
			if logicalErr != nil {
				return logicalErr
			}
			response = logicalResponse
			change = logicalChange
			return nil
		}
		configChannel := channel
		configChannelID := configChannel.Id
		if err := validateChannelModelDetectionTargets(&configChannel, input.Targets); err != nil {
			return err
		}

		var global model.ChannelModelDetectionGlobalConfig
		if input.ScheduleEnabled {
			if err := transaction.Where("id = ?", model.ChannelModelDetectionConfigID).First(&global).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrChannelModelDetectionDetectorNotConfigured
				}
				return err
			}
			if !global.DetectorURLConfigured() {
				return ErrChannelModelDetectionDetectorNotConfigured
			}
		}

		var config model.ChannelModelDetectionConfig
		configErr := transaction.Where("channel_id = ?", configChannelID).First(&config).Error
		exists := configErr == nil
		if configErr != nil && !errors.Is(configErr, gorm.ErrRecordNotFound) {
			return configErr
		}
		if exists {
			if input.ExpectedRevision <= 0 || input.ExpectedRevision != config.Revision {
				return ErrChannelModelDetectionRevisionConflict
			}
		} else if input.ExpectedRevision != 0 {
			return ErrChannelModelDetectionRevisionConflict
		}

		oldRevision := int64(0)
		if exists {
			oldRevision = config.Revision
			if config.Revision == math.MaxInt64 {
				return fmt.Errorf("%w: 修订号已达上限", ErrChannelModelDetectionInvalidConfig)
			}
			config.Revision++
		} else {
			config = model.ChannelModelDetectionConfig{ChannelId: configChannelID, Revision: 1}
		}
		config.ScheduleEnabled = input.ScheduleEnabled
		config.UpdatedAt = now.Unix()
		if config.CreatedAt == 0 {
			config.CreatedAt = now.Unix()
		}

		var existing []model.ChannelModelDetectionTarget
		if exists {
			if err := transaction.Where("config_id = ? AND channel_id = ?", config.Id, configChannelID).Order("position ASC").Find(&existing).Error; err != nil {
				return err
			}
		}
		existingByKey := make(map[string]model.ChannelModelDetectionTarget, len(existing))
		for _, target := range existing {
			existingByKey[target.TargetKey] = target
		}
		resolvedKeys := make([]string, len(input.Targets))
		keep := make(map[string]bool, len(input.Targets))
		for position, targetInput := range input.Targets {
			key := strings.TrimSpace(targetInput.TargetKey)
			if key != "" {
				if _, ok := existingByKey[key]; !ok {
					return fmt.Errorf("%w: %s", ErrChannelModelDetectionTargetNotFound, key)
				}
			} else {
				key = common.GetUUID()
				for {
					_, exists := existingByKey[key]
					if !exists && !keep[key] {
						break
					}
					key = common.GetUUID()
				}
			}
			resolvedKeys[position] = key
			keep[key] = true
		}

		if exists {
			updated := transaction.Model(&model.ChannelModelDetectionConfig{}).
				Where("id = ? AND revision = ?", config.Id, oldRevision).
				Updates(map[string]any{"schedule_enabled": config.ScheduleEnabled, "revision": config.Revision, "updated_at": config.UpdatedAt})
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return ErrChannelModelDetectionRevisionConflict
			}
		} else if err := transaction.Create(&config).Error; err != nil {
			return err
		}
		// Release composite identities first so retained targets can safely
		// swap request/claimed pairs within the same transaction.
		for _, target := range existing {
			temporaryModel := "__channel_model_detection_update_" + target.TargetKey
			if err := transaction.Model(&model.ChannelModelDetectionTarget{}).Where("id = ?", target.Id).UpdateColumn("request_model", temporaryModel).Error; err != nil {
				return err
			}
			if !keep[target.TargetKey] {
				if err := transaction.Delete(&model.ChannelModelDetectionTarget{}, target.Id).Error; err != nil {
					return err
				}
			}
		}
		responses := make([]ChannelModelDetectionTargetResponse, 0, len(input.Targets))
		for position, targetInput := range input.Targets {
			key := resolvedKeys[position]
			var target model.ChannelModelDetectionTarget
			if existingTarget, ok := existingByKey[key]; ok {
				target = existingTarget
			} else {
				target = model.ChannelModelDetectionTarget{ConfigId: config.Id, ChannelId: configChannelID, TargetKey: key}
			}
			target.ConfigId = config.Id
			target.ChannelId = configChannelID
			target.TargetKey = key
			target.RequestModel = targetInput.RequestModel
			target.ClaimedModel = targetInput.ClaimedModel
			target.Position = position
			target.Enabled = true
			target.UpdatedAt = now.Unix()
			if target.CreatedAt == 0 {
				target.CreatedAt = now.Unix()
			}
			if err := transaction.Save(&target).Error; err != nil {
				return err
			}
			responses = append(responses, ChannelModelDetectionTargetResponse{TargetKey: key, RequestModel: target.RequestModel, ClaimedModel: target.ClaimedModel, Enabled: true, Position: position})
		}
		response = ChannelModelDetectionConfigResponse{ChannelID: requestedChannelID, ScheduleEnabled: config.ScheduleEnabled, Revision: config.Revision, CreatedAt: config.CreatedAt, UpdatedAt: config.UpdatedAt, Targets: responses}
		change = ChannelModelDetectionConfigChange{ChannelID: requestedChannelID, OldRevision: oldRevision, NewRevision: config.Revision}
		return nil
	})
	if err != nil {
		return ChannelModelDetectionConfigResponse{}, err
	}
	if db == model.DB {
		NotifyChannelModelDetectionOverviewChanged()
	}
	if hook := channelModelDetectionConfigHook(); hook != nil {
		hook(ctx, change)
	}
	return response, nil
}

func updateChannelModelDetectionLogicalConfig(tx *gorm.DB, identity LogicalChannelIdentity, requestedChannelID int, input ChannelModelDetectionConfigUpdateInput, now time.Time) (ChannelModelDetectionConfigResponse, ChannelModelDetectionConfigChange, error) {
	group, err := model.LockLogicalChannelGroupForMembership(tx, identity.LogicalChannelID)
	if err != nil {
		return ChannelModelDetectionConfigResponse{}, ChannelModelDetectionConfigChange{}, err
	}
	if !model.IsLogicalChannelGroupActive(group.Status) || group.Revision != identity.Revision {
		return ChannelModelDetectionConfigResponse{}, ChannelModelDetectionConfigChange{}, model.ErrChannelLogicalGroupRevisionConflict
	}
	members, err := model.LockLogicalChannelGroupMembers(tx, []int64{identity.LogicalChannelID})
	if err != nil {
		return ChannelModelDetectionConfigResponse{}, ChannelModelDetectionConfigChange{}, err
	}
	memberIDs := make([]int, 0, len(members))
	requestedMember := false
	for _, member := range members {
		memberIDs = append(memberIDs, member.ChannelID)
		requestedMember = requestedMember || member.ChannelID == requestedChannelID
	}
	if !requestedMember || len(memberIDs) == 0 {
		return ChannelModelDetectionConfigResponse{}, ChannelModelDetectionConfigChange{}, model.ErrChannelLogicalGroupInvalidMember
	}
	var channels []*model.Channel
	if err := tx.Where("id IN ?", memberIDs).Order("id ASC").Find(&channels).Error; err != nil {
		return ChannelModelDetectionConfigResponse{}, ChannelModelDetectionConfigChange{}, err
	}
	if len(channels) != len(memberIDs) {
		return ChannelModelDetectionConfigResponse{}, ChannelModelDetectionConfigChange{}, ErrChannelModelDetectionChannelNotFound
	}
	if err := validateChannelModelDetectionTargetsForChannels(channels, input.Targets); err != nil {
		return ChannelModelDetectionConfigResponse{}, ChannelModelDetectionConfigChange{}, err
	}
	if input.ScheduleEnabled {
		var global model.ChannelModelDetectionGlobalConfig
		if err := tx.Where("id = ?", model.ChannelModelDetectionConfigID).First(&global).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ChannelModelDetectionConfigResponse{}, ChannelModelDetectionConfigChange{}, ErrChannelModelDetectionDetectorNotConfigured
			}
			return ChannelModelDetectionConfigResponse{}, ChannelModelDetectionConfigChange{}, err
		}
		if !global.DetectorURLConfigured() {
			return ChannelModelDetectionConfigResponse{}, ChannelModelDetectionConfigChange{}, ErrChannelModelDetectionDetectorNotConfigured
		}
	}
	if err := model.EnsureChannelModelDetectionLogicalConfigTx(tx, identity.LogicalChannelID, memberIDs); err != nil {
		return ChannelModelDetectionConfigResponse{}, ChannelModelDetectionConfigChange{}, err
	}

	var config model.ChannelModelDetectionLogicalConfig
	configErr := tx.Where("logical_channel_id = ?", identity.LogicalChannelID).First(&config).Error
	exists := configErr == nil
	if configErr != nil && !errors.Is(configErr, gorm.ErrRecordNotFound) {
		return ChannelModelDetectionConfigResponse{}, ChannelModelDetectionConfigChange{}, configErr
	}
	if exists {
		if input.ExpectedRevision <= 0 || input.ExpectedRevision != config.Revision {
			return ChannelModelDetectionConfigResponse{}, ChannelModelDetectionConfigChange{}, ErrChannelModelDetectionRevisionConflict
		}
	} else if input.ExpectedRevision != 0 {
		return ChannelModelDetectionConfigResponse{}, ChannelModelDetectionConfigChange{}, ErrChannelModelDetectionRevisionConflict
	}
	oldRevision := int64(0)
	if exists {
		oldRevision = config.Revision
		if config.Revision == math.MaxInt64 {
			return ChannelModelDetectionConfigResponse{}, ChannelModelDetectionConfigChange{}, fmt.Errorf("%w: 修订号已达上限", ErrChannelModelDetectionInvalidConfig)
		}
		config.Revision++
	} else {
		config = model.ChannelModelDetectionLogicalConfig{LogicalChannelId: identity.LogicalChannelID, Revision: 1}
	}
	config.ScheduleEnabled = input.ScheduleEnabled
	config.UpdatedAt = now.Unix()
	if config.CreatedAt == 0 {
		config.CreatedAt = now.Unix()
	}

	var existing []model.ChannelModelDetectionLogicalTarget
	if exists {
		if err := tx.Where("config_id = ? AND logical_channel_id = ?", config.Id, identity.LogicalChannelID).Order("position ASC").Find(&existing).Error; err != nil {
			return ChannelModelDetectionConfigResponse{}, ChannelModelDetectionConfigChange{}, err
		}
	}
	existingByKey := make(map[string]model.ChannelModelDetectionLogicalTarget, len(existing))
	for _, target := range existing {
		existingByKey[target.TargetKey] = target
	}
	resolvedKeys := make([]string, len(input.Targets))
	keep := make(map[string]bool, len(input.Targets))
	for position, targetInput := range input.Targets {
		key := strings.TrimSpace(targetInput.TargetKey)
		if key != "" {
			if _, ok := existingByKey[key]; !ok {
				return ChannelModelDetectionConfigResponse{}, ChannelModelDetectionConfigChange{}, fmt.Errorf("%w: %s", ErrChannelModelDetectionTargetNotFound, key)
			}
		} else {
			key = common.GetUUID()
			for {
				_, alreadyExists := existingByKey[key]
				if !alreadyExists && !keep[key] {
					break
				}
				key = common.GetUUID()
			}
		}
		resolvedKeys[position] = key
		keep[key] = true
	}
	if exists {
		updated := tx.Model(&model.ChannelModelDetectionLogicalConfig{}).
			Where("id = ? AND logical_channel_id = ? AND revision = ?", config.Id, identity.LogicalChannelID, oldRevision).
			Updates(map[string]any{"schedule_enabled": config.ScheduleEnabled, "revision": config.Revision, "updated_at": config.UpdatedAt})
		if updated.Error != nil {
			return ChannelModelDetectionConfigResponse{}, ChannelModelDetectionConfigChange{}, updated.Error
		}
		if updated.RowsAffected != 1 {
			return ChannelModelDetectionConfigResponse{}, ChannelModelDetectionConfigChange{}, ErrChannelModelDetectionRevisionConflict
		}
	} else if err := tx.Create(&config).Error; err != nil {
		return ChannelModelDetectionConfigResponse{}, ChannelModelDetectionConfigChange{}, err
	}
	for _, target := range existing {
		temporaryModel := "__channel_model_detection_update_" + target.TargetKey
		if err := tx.Model(&model.ChannelModelDetectionLogicalTarget{}).Where("id = ?", target.Id).UpdateColumn("request_model", temporaryModel).Error; err != nil {
			return ChannelModelDetectionConfigResponse{}, ChannelModelDetectionConfigChange{}, err
		}
		if !keep[target.TargetKey] {
			if err := tx.Delete(&model.ChannelModelDetectionLogicalTarget{}, target.Id).Error; err != nil {
				return ChannelModelDetectionConfigResponse{}, ChannelModelDetectionConfigChange{}, err
			}
		}
	}
	responses := make([]ChannelModelDetectionTargetResponse, 0, len(input.Targets))
	for position, targetInput := range input.Targets {
		key := resolvedKeys[position]
		var target model.ChannelModelDetectionLogicalTarget
		if existingTarget, ok := existingByKey[key]; ok {
			target = existingTarget
		} else {
			target = model.ChannelModelDetectionLogicalTarget{ConfigId: config.Id, LogicalChannelId: identity.LogicalChannelID, TargetKey: key}
		}
		target.ConfigId = config.Id
		target.LogicalChannelId = identity.LogicalChannelID
		target.TargetKey = key
		target.RequestModel = targetInput.RequestModel
		target.ClaimedModel = targetInput.ClaimedModel
		target.Position = position
		target.Enabled = true
		target.UpdatedAt = now.Unix()
		if target.CreatedAt == 0 {
			target.CreatedAt = now.Unix()
		}
		if err := tx.Save(&target).Error; err != nil {
			return ChannelModelDetectionConfigResponse{}, ChannelModelDetectionConfigChange{}, err
		}
		responses = append(responses, ChannelModelDetectionTargetResponse{TargetKey: key, RequestModel: target.RequestModel, ClaimedModel: target.ClaimedModel, Enabled: true, Position: position})
	}
	var currentRevision int64
	if err := tx.Model(&model.ChannelLogicalGroup{}).Where("id = ?", identity.LogicalChannelID).Pluck("revision", &currentRevision).Error; err != nil {
		return ChannelModelDetectionConfigResponse{}, ChannelModelDetectionConfigChange{}, err
	}
	if currentRevision != identity.Revision {
		return ChannelModelDetectionConfigResponse{}, ChannelModelDetectionConfigChange{}, model.ErrChannelLogicalGroupRevisionConflict
	}
	response := ChannelModelDetectionConfigResponse{ChannelID: requestedChannelID, ScheduleEnabled: config.ScheduleEnabled, Revision: config.Revision, CreatedAt: config.CreatedAt, UpdatedAt: config.UpdatedAt, Targets: responses}
	change := ChannelModelDetectionConfigChange{ChannelID: requestedChannelID, OldRevision: oldRevision, NewRevision: config.Revision}
	return response, change, nil
}

func validateChannelModelDetectionTargets(channel *model.Channel, inputs []ChannelModelDetectionTargetUpdateInput) error {
	if channel == nil {
		return ErrChannelModelDetectionChannelNotFound
	}
	return validateChannelModelDetectionTargetsForChannels([]*model.Channel{channel}, inputs)
}

func validateChannelModelDetectionTargetsForChannels(channels []*model.Channel, inputs []ChannelModelDetectionTargetUpdateInput) error {
	if len(channels) == 0 {
		return ErrChannelModelDetectionChannelNotFound
	}
	supported := make(map[string]struct{})
	for _, channel := range channels {
		if channel == nil {
			continue
		}
		for _, value := range channel.GetModels() {
			supported[value] = struct{}{}
		}
	}
	keys := make(map[string]struct{}, len(inputs))
	pairs := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		if input.RequestModel == "" {
			return fmt.Errorf("%w: 请求模型不能为空", ErrChannelModelDetectionInvalidConfig)
		}
		if _, ok := supported[input.RequestModel]; !ok {
			return fmt.Errorf("%w: 请求模型不是该渠道支持的精确模型", ErrChannelModelDetectionInvalidConfig)
		}
		if input.ClaimedModel == "" || strings.TrimSpace(input.ClaimedModel) != input.ClaimedModel || !model.IsChannelModelDetectionClaimedModel(input.ClaimedModel) {
			return fmt.Errorf("%w: %w", ErrChannelModelDetectionInvalidConfig, model.ErrChannelModelDetectionInvalidClaimedModel)
		}
		key := strings.TrimSpace(input.TargetKey)
		if key != "" {
			if _, exists := keys[key]; exists {
				return fmt.Errorf("%w: 目标键不能重复", ErrChannelModelDetectionInvalidConfig)
			}
			keys[key] = struct{}{}
		}
		pair := input.RequestModel + "\x00" + input.ClaimedModel
		if _, exists := pairs[pair]; exists {
			return fmt.Errorf("%w: 请求模型和申报型号组合不能重复", ErrChannelModelDetectionInvalidConfig)
		}
		pairs[pair] = struct{}{}
	}
	return nil
}

type ChannelModelDetectionTargetEstimateResponse struct {
	TargetKey                string  `json:"target_key"`
	RequestModel             string  `json:"request_model"`
	ClaimedModel             string  `json:"claimed_model"`
	EstimatedLogicalRequests int64   `json:"estimated_logical_requests"`
	EstimatedHTTPAttempts    int64   `json:"estimated_http_attempts"`
	EstimatedQuota           *int64  `json:"estimated_quota"`
	EstimatedCostNanoCNY     *int64  `json:"estimated_cost_nano_cny"`
	EstimatedCostCNY         *string `json:"estimated_cost_cny"`
	CostEstimateUnknown      bool    `json:"cost_estimate_unknown"`
	EstimateBasis            string  `json:"estimate_basis"`
}

type ChannelModelDetectionEstimateResponse struct {
	Preset                   string                                        `json:"preset"`
	OfficialEstimate         ChannelModelDetectionPresetEstimateResponse   `json:"official_estimate"`
	Targets                  []ChannelModelDetectionTargetEstimateResponse `json:"targets"`
	EstimatedQuota           *int64                                        `json:"estimated_quota"`
	EstimatedCostNanoCNY     *int64                                        `json:"estimated_cost_nano_cny"`
	EstimatedCostCNY         *string                                       `json:"estimated_cost_cny"`
	CostEstimateUnknownCount int64                                         `json:"cost_estimate_unknown_count"`
}

func EstimateChannelModelDetectionCost(ctx context.Context, tx *gorm.DB, channelID int, preset string, options ...ChannelModelDetectorClientOptions) (ChannelModelDetectionEstimateResponse, error) {
	db := tx
	if db == nil {
		db = model.DB
	}
	if db == nil {
		return ChannelModelDetectionEstimateResponse{}, errors.New("模型检测数据库不可用")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	preset = strings.ToLower(strings.TrimSpace(preset))
	if !model.IsChannelModelDetectionPreset(preset) {
		return ChannelModelDetectionEstimateResponse{}, model.ErrChannelModelDetectionInvalidPreset
	}
	var channel model.Channel
	if err := db.WithContext(ctx).Where("id = ?", channelID).First(&channel).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ChannelModelDetectionEstimateResponse{}, ErrChannelModelDetectionChannelNotFound
		}
		return ChannelModelDetectionEstimateResponse{}, err
	}
	var global model.ChannelModelDetectionGlobalConfig
	if err := db.WithContext(ctx).Where("id = ?", model.ChannelModelDetectionConfigID).First(&global).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ChannelModelDetectionEstimateResponse{}, ErrChannelModelDetectionDetectorNotConfigured
		}
		return ChannelModelDetectionEstimateResponse{}, err
	}
	if !global.DetectorURLConfigured() {
		return ChannelModelDetectionEstimateResponse{}, ErrChannelModelDetectionDetectorNotConfigured
	}
	var targets []model.ChannelModelDetectionTarget
	if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		identity, err := channelModelDetectionLogicalIdentity(tx, channelID, db == model.DB)
		if err != nil {
			return err
		}
		_, targets, _, err = channelModelDetectionConfigForIdentity(tx, identity, channelID, db == model.DB)
		return err
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ChannelModelDetectionEstimateResponse{}, ErrChannelModelDetectionConfigNotFound
		}
		return ChannelModelDetectionEstimateResponse{}, err
	}
	if len(targets) == 0 {
		return ChannelModelDetectionEstimateResponse{}, ErrChannelModelDetectionConfigNotFound
	}
	var option ChannelModelDetectorClientOptions
	if len(options) > 0 {
		option = options[0]
	}
	detector, err := NewChannelModelDetectorClient(global.DetectorURL, option)
	if err != nil {
		return ChannelModelDetectionEstimateResponse{}, err
	}
	bootstrap, err := detector.Bootstrap(ctx)
	if err != nil {
		return ChannelModelDetectionEstimateResponse{}, err
	}
	officialConfig, ok := bootstrap.Preset(preset)
	if !ok {
		return ChannelModelDetectionEstimateResponse{}, ErrChannelModelDetectionEstimateInvalid
	}
	official, err := detector.Estimate(ctx, officialConfig)
	if err != nil {
		return ChannelModelDetectionEstimateResponse{}, err
	}
	if official.TotalRequests == nil || *official.TotalRequests < 0 {
		return ChannelModelDetectionEstimateResponse{}, ErrChannelModelDetectionEstimateInvalid
	}
	logical := *official.TotalRequests
	var fixed32k *int64
	if official.Fixed32KRequests != nil && *official.Fixed32KRequests >= 0 {
		value := *official.Fixed32KRequests
		fixed32k = &value
	}
	officialResponse := ChannelModelDetectionPresetEstimateResponse{Preset: preset, Available: true, LogicalRequests: &logical, Fixed32KRequests: fixed32k, ConfigHash: official.ConfigHash}
	results := make([]ChannelModelDetectionTargetEstimateResponse, 0, len(targets))
	for _, target := range targets {
		results = append(results, ChannelModelDetectionTargetEstimateResponse{
			TargetKey: target.TargetKey, RequestModel: target.RequestModel, ClaimedModel: target.ClaimedModel,
			EstimatedLogicalRequests: logical, EstimatedHTTPAttempts: logical,
			EstimateBasis: "官方 estimate 请求量；实际成本以请求完成后的上游 Usage 结算",
		})
	}
	return ChannelModelDetectionEstimateResponse{
		Preset: preset, OfficialEstimate: officialResponse, Targets: results,
	}, nil
}
