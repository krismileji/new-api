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
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/shopspring/decimal"
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
		if err := validateChannelModelDetectionTargets(&channel, input.Targets); err != nil {
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
		configErr := transaction.Where("channel_id = ?", channelID).First(&config).Error
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
			config = model.ChannelModelDetectionConfig{ChannelId: channelID, Revision: 1}
		}
		config.ScheduleEnabled = input.ScheduleEnabled
		config.UpdatedAt = now.Unix()
		if config.CreatedAt == 0 {
			config.CreatedAt = now.Unix()
		}

		var existing []model.ChannelModelDetectionTarget
		if exists {
			if err := transaction.Where("config_id = ? AND channel_id = ?", config.Id, channelID).Order("position ASC").Find(&existing).Error; err != nil {
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
				target = model.ChannelModelDetectionTarget{ConfigId: config.Id, ChannelId: channelID, TargetKey: key}
			}
			target.ConfigId = config.Id
			target.ChannelId = channelID
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
		response = ChannelModelDetectionConfigResponse{ChannelID: channelID, ScheduleEnabled: config.ScheduleEnabled, Revision: config.Revision, CreatedAt: config.CreatedAt, UpdatedAt: config.UpdatedAt, Targets: responses}
		change = ChannelModelDetectionConfigChange{ChannelID: channelID, OldRevision: oldRevision, NewRevision: config.Revision}
		return nil
	})
	if err != nil {
		return ChannelModelDetectionConfigResponse{}, err
	}
	if hook := channelModelDetectionConfigHook(); hook != nil {
		hook(ctx, change)
	}
	return response, nil
}

func validateChannelModelDetectionTargets(channel *model.Channel, inputs []ChannelModelDetectionTargetUpdateInput) error {
	if channel == nil {
		return ErrChannelModelDetectionChannelNotFound
	}
	models := channel.GetModels()
	supported := make(map[string]struct{}, len(models))
	for _, value := range models {
		supported[value] = struct{}{}
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
	var config model.ChannelModelDetectionConfig
	if err := db.WithContext(ctx).Where("channel_id = ?", channelID).First(&config).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ChannelModelDetectionEstimateResponse{}, ErrChannelModelDetectionConfigNotFound
		}
		return ChannelModelDetectionEstimateResponse{}, err
	}
	var targets []model.ChannelModelDetectionTarget
	if err := db.WithContext(ctx).Where("config_id = ? AND channel_id = ? AND enabled = ?", config.Id, channelID, true).Order("position ASC").Find(&targets).Error; err != nil {
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
	snapshot, snapshotErr := CaptureChannelModelDetectionCostSnapshot(channelID)
	if snapshotErr != nil {
		snapshot = ChannelModelDetectionCostSnapshot{}
	}

	results := make([]ChannelModelDetectionTargetEstimateResponse, 0, len(targets))
	var aggregateQuota decimal.Decimal
	var aggregateCost decimal.Decimal
	quotaUnknown := false
	costUnknown := false
	unknownCount := int64(0)
	for _, target := range targets {
		info, pricingKnown := channelModelDetectionPricingInfo(channelID, target.RequestModel)
		quota, quotaKnown := EstimateChannelModelDetectionQuota(info, 0, snapshot)
		if !pricingKnown {
			quotaKnown = false
		}
		var totalQuota *int64
		if quotaKnown {
			total, clamp := common.QuotaFromDecimalChecked(decimal.NewFromInt(quota).Mul(decimal.NewFromInt(logical)))
			if clamp == nil && total >= 0 {
				value := int64(total)
				totalQuota = &value
				aggregateQuota = aggregateQuota.Add(decimal.NewFromInt(value))
			} else {
				quotaKnown = false
			}
		}
		var costNano *int64
		if quotaKnown && snapshot.CostRatioCNY != nil && snapshot.QuotaPerUnit != nil && totalQuota != nil {
			value, costErr := CalculateChannelModelDetectionUnresolvedCostNanoCNY(*totalQuota, *snapshot.CostRatioCNY, *snapshot.QuotaPerUnit)
			if costErr == nil {
				costNano = &value
				aggregateCost = aggregateCost.Add(decimal.NewFromInt(value))
			} else {
				costUnknown = true
			}
		} else {
			costUnknown = true
		}
		if !quotaKnown {
			quotaUnknown = true
		}
		if costNano == nil {
			unknownCount += logical
		}
		results = append(results, ChannelModelDetectionTargetEstimateResponse{
			TargetKey: target.TargetKey, RequestModel: target.RequestModel, ClaimedModel: target.ClaimedModel,
			EstimatedLogicalRequests: logical, EstimatedHTTPAttempts: logical, EstimatedQuota: totalQuota,
			EstimatedCostNanoCNY: costNano, EstimatedCostCNY: FormatChannelModelDetectionCostCNY(costNano), CostEstimateUnknown: costNano == nil,
			EstimateBasis: "官方 estimate 请求量 × 渠道测试保守额度 × 当前渠道成本快照",
		})
	}
	var totalQuotaPtr *int64
	if !quotaUnknown {
		value := int64(aggregateQuota.IntPart())
		totalQuotaPtr = &value
	}
	var totalCostPtr *int64
	if !costUnknown {
		value := int64(aggregateCost.IntPart())
		totalCostPtr = &value
	}
	return ChannelModelDetectionEstimateResponse{Preset: preset, OfficialEstimate: officialResponse, Targets: results, EstimatedQuota: totalQuotaPtr, EstimatedCostNanoCNY: totalCostPtr, EstimatedCostCNY: FormatChannelModelDetectionCostCNY(totalCostPtr), CostEstimateUnknownCount: unknownCount}, nil
}

func channelModelDetectionPricingInfo(channelID int, modelName string) (*relaycommon.RelayInfo, bool) {
	groupRatio := ratio_setting.GetGroupRatio("default")
	if groupRatio <= 0 || math.IsNaN(groupRatio) || math.IsInf(groupRatio, 0) {
		groupRatio = 1
	}
	price, usePrice := ratio_setting.GetModelPrice(modelName, false)
	info := &relaycommon.RelayInfo{OriginModelName: modelName, RelayMode: relayconstant.RelayModeResponses}
	info.ChannelMeta = &relaycommon.ChannelMeta{ChannelId: channelID}
	info.SetEstimatePromptTokens(common.PreConsumedQuota)
	info.PriceData = hosttypes.PriceData{UsePrice: usePrice, ModelPrice: price, GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: groupRatio}, CompletionRatio: ratio_setting.GetCompletionRatio(modelName)}
	if usePrice {
		return info, true
	}
	ratio, ok := channelModelDetectionConfiguredModelRatio(modelName)
	if !ok || ratio < 0 || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		return info, false
	}
	info.PriceData.ModelRatio = ratio
	return info, true
}

func channelModelDetectionConfiguredModelRatio(modelName string) (float64, bool) {
	matched := ratio_setting.FormatMatchingModelName(modelName)
	ratios := ratio_setting.GetModelRatioCopy()
	if value, ok := ratios[matched]; ok {
		return value, true
	}
	return 0, false
}
