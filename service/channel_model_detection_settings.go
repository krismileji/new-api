package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

var (
	ErrChannelModelDetectionSettingsConflict      = errors.New("模型检测统一设置已被其他管理员更新，请刷新后重试")
	ErrChannelModelDetectionInvalidDetectorTarget = errors.New("检测器地址只允许静态回环或私网目标")
	ErrChannelModelDetectionDetectorNotConfigured = errors.New("尚未配置官方检测器地址")
)

type ChannelModelDetectionSettingsUpdate struct {
	DetectorURL      *string
	ClearDetectorURL bool
	RelayURL         *string
	ClearRelayURL    bool
	ScheduledPreset  string
	ConfirmHighCost  bool
	ScheduleEnabled  bool
	IntervalMinutes  int
	DisplayValue     int
	DisplayUnit      string
	// Legacy fields are accepted by internal callers while they migrate to
	// minute-based scheduling.
	IntervalHours    int
	ScheduleTime     string
	Timezone         string
	ExpectedRevision int64
}

type ChannelModelDetectionSettingsResponse struct {
	DetectorURLConfigured        bool   `json:"detector_url_configured"`
	DetectorURL                  string `json:"detector_url"`
	DetectorURLMasked            string `json:"detector_url_masked"`
	PendingDetectorURLConfigured bool   `json:"pending_detector_url_configured"`
	PendingDetectorURL           string `json:"pending_detector_url"`
	PendingDetectorURLMasked     string `json:"pending_detector_url_masked"`
	DetectorURLSwitchPending     bool   `json:"detector_url_switch_pending"`
	RelayURLConfigured           bool   `json:"relay_url_configured"`
	RelayURL                     string `json:"relay_url"`
	ScheduledPreset              string `json:"scheduled_preset"`
	ScheduleEnabled              bool   `json:"schedule_enabled"`
	IntervalMinutes              int    `json:"interval_minutes"`
	DisplayValue                 int    `json:"display_value"`
	DisplayUnit                  string `json:"display_unit"`
	IntervalHours                int    `json:"-"`
	ScheduleTime                 string `json:"-"`
	Timezone                     string `json:"-"`
	ScheduleAnchorAt             int64  `json:"-"`
	NextBatchAt                  int64  `json:"next_batch_at"`
	Revision                     int64  `json:"revision"`
	ConnectionTestRequired       bool   `json:"connection_test_required"`
	CreatedAt                    int64  `json:"created_at"`
	UpdatedAt                    int64  `json:"updated_at"`
}

type ChannelModelDetectionServiceResponse struct {
	State                 string                                                 `json:"state"`
	DetectorURLConfigured bool                                                   `json:"detector_url_configured"`
	DetectorURLMasked     string                                                 `json:"detector_url_masked"`
	Busy                  bool                                                   `json:"busy"`
	ActiveSessionOwned    bool                                                   `json:"active_session_owned"`
	DeploymentID          *string                                                `json:"deployment_id"`
	LastCheckedAt         int64                                                  `json:"last_checked_at"`
	LastError             string                                                 `json:"last_error"`
	CompatibilityMessage  string                                                 `json:"compatibility_message"`
	Estimates             map[string]ChannelModelDetectionPresetEstimateResponse `json:"estimates"`
}

var channelModelDetectionServiceCache = struct {
	sync.RWMutex
	value ChannelModelDetectionServiceResponse
}{}

func GetChannelModelDetectionSettings(ctx context.Context, tx *gorm.DB, now time.Time) (ChannelModelDetectionSettingsResponse, error) {
	db := tx
	if db == nil {
		db = model.DB
	}
	if db == nil {
		return ChannelModelDetectionSettingsResponse{}, errors.New("模型检测统一设置数据库不可用")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var config model.ChannelModelDetectionGlobalConfig
	err := db.WithContext(ctx).Where("id = ?", model.ChannelModelDetectionConfigID).First(&config).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		config = channelModelDetectionDefaultGlobalConfig(now)
		if err := db.WithContext(ctx).Create(&config).Error; err != nil {
			return ChannelModelDetectionSettingsResponse{}, err
		}
	} else if err != nil {
		return ChannelModelDetectionSettingsResponse{}, err
	}
	return channelModelDetectionSettingsResponse(config, false), nil
}

func UpdateChannelModelDetectionSettings(ctx context.Context, tx *gorm.DB, input ChannelModelDetectionSettingsUpdate, now time.Time) (ChannelModelDetectionSettingsResponse, error) {
	db := tx
	if db == nil {
		db = model.DB
	}
	if db == nil {
		return ChannelModelDetectionSettingsResponse{}, errors.New("模型检测统一设置数据库不可用")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if input.ExpectedRevision <= 0 {
		return ChannelModelDetectionSettingsResponse{}, ErrChannelModelDetectionSettingsConflict
	}
	if input.ClearDetectorURL && input.DetectorURL != nil {
		return ChannelModelDetectionSettingsResponse{}, errors.New("检测器地址与清除命令不能同时提交")
	}
	if input.ClearRelayURL && input.RelayURL != nil {
		return ChannelModelDetectionSettingsResponse{}, errors.New("内部 Relay 地址与清除命令不能同时提交")
	}
	var normalizedURL *string
	if input.DetectorURL != nil {
		value := strings.TrimSpace(*input.DetectorURL)
		if value == "" {
			return ChannelModelDetectionSettingsResponse{}, errors.New("检测器地址不能为空；如需删除请使用清除命令")
		}
		normalized, err := ValidateChannelModelDetectorTarget(ctx, value)
		if err != nil {
			return ChannelModelDetectionSettingsResponse{}, err
		}
		normalizedURL = &normalized
	}
	var normalizedRelayURL *string
	if input.RelayURL != nil {
		value := strings.TrimSpace(*input.RelayURL)
		if value == "" {
			return ChannelModelDetectionSettingsResponse{}, errors.New("内部 Relay 地址不能为空；如需删除请使用清除命令")
		}
		normalized, err := NormalizeChannelModelDetectionRelayURL(value)
		if err != nil {
			return ChannelModelDetectionSettingsResponse{}, err
		}
		normalizedRelayURL = &normalized
	}

	var saved model.ChannelModelDetectionGlobalConfig
	addressChanged := false
	activeDetectorChanged := false
	err := db.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		var current model.ChannelModelDetectionGlobalConfig
		if err := transaction.Where("id = ?", model.ChannelModelDetectionConfigID).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrChannelModelDetectionSettingsConflict
			}
			return err
		}
		if current.Revision != input.ExpectedRevision {
			return ErrChannelModelDetectionSettingsConflict
		}
		previousDetectorURL := current.DetectorURL
		// A previous address can remain pending while the official session
		// finishes. Promote it before applying this command once no active
		// execution still owns the detector session.
		if strings.TrimSpace(current.PendingDetectorURL) != "" {
			var activeSessions int64
			if err := transaction.Model(&model.ChannelModelDetectionExecution{}).
				Where("status IN ?", []string{model.ChannelModelDetectionExecutionStatusSubmitting, model.ChannelModelDetectionExecutionStatusRunning}).
				Count(&activeSessions).Error; err != nil {
				return err
			}
			if activeSessions == 0 {
				current.DetectorURL = current.PendingDetectorURL
				current.PendingDetectorURL = ""
			}
		}
		candidate := current
		candidate.Revision = current.Revision + 1
		candidate.ScheduledPreset = strings.ToLower(strings.TrimSpace(input.ScheduledPreset))
		candidate.ScheduleEnabled = input.ScheduleEnabled
		candidate.IntervalMinutes = input.IntervalMinutes
		if candidate.IntervalMinutes <= 0 && input.IntervalHours > 0 {
			candidate.IntervalMinutes = input.IntervalHours * 60
		}
		if candidate.IntervalMinutes <= 0 {
			candidate.IntervalMinutes = current.EffectiveIntervalMinutes()
		}
		if candidate.IntervalMinutes < model.ChannelModelDetectionMinIntervalMinutes || candidate.IntervalMinutes > model.ChannelModelDetectionMaxIntervalMinutes {
			return model.ErrChannelModelDetectionInvalidSchedule
		}
		if input.DisplayValue == 0 && strings.TrimSpace(input.DisplayUnit) == "" {
			candidate.DisplayValue, candidate.DisplayUnit = current.EffectiveDisplay()
		} else {
			candidate.DisplayValue = input.DisplayValue
			candidate.DisplayUnit = strings.TrimSpace(input.DisplayUnit)
			if !model.IsChannelModelDetectionDisplayAllowed(candidate.DisplayValue, candidate.DisplayUnit) {
				return model.ErrChannelModelDetectionInvalidDisplay
			}
		}
		candidate.IntervalHours = 0
		if candidate.IntervalMinutes%60 == 0 {
			candidate.IntervalHours = candidate.IntervalMinutes / 60
		}
		candidate.ScheduleTime = ""
		candidate.Timezone = ""
		candidate.LeaseToken = ""
		candidate.LeaseUntil = 0
		candidate.UpdatedAt = now.Unix()

		newURL := current.DetectorURL
		if input.ClearDetectorURL {
			newURL = ""
		} else if normalizedURL != nil {
			newURL = *normalizedURL
		}
		addressChanged = newURL != current.DetectorURL || (current.PendingDetectorURL != "" && (input.ClearDetectorURL || normalizedURL != nil))
		if addressChanged {
			var activeSessions int64
			if err := transaction.Model(&model.ChannelModelDetectionExecution{}).
				Where("status IN ?", []string{model.ChannelModelDetectionExecutionStatusSubmitting, model.ChannelModelDetectionExecutionStatusRunning}).
				Count(&activeSessions).Error; err != nil {
				return err
			}
			if activeSessions > 0 {
				if newURL == "" {
					// Clearing the address cannot affect an already-running execution;
					// its detector URL is frozen on the execution row.
					candidate.DetectorURL = ""
					candidate.PendingDetectorURL = ""
				} else {
					candidate.PendingDetectorURL = newURL
				}
			} else {
				candidate.DetectorURL = newURL
				candidate.PendingDetectorURL = ""
			}
		}
		activeDetectorChanged = candidate.DetectorURL != previousDetectorURL
		if input.ClearRelayURL {
			candidate.RelayURL = ""
		} else if normalizedRelayURL != nil {
			candidate.RelayURL = *normalizedRelayURL
		}

		scheduleChanged := candidate.ScheduleEnabled != current.ScheduleEnabled ||
			candidate.IntervalMinutes != current.EffectiveIntervalMinutes()
		if !candidate.ScheduleEnabled {
			candidate.ScheduleAnchorAt = 0
			candidate.NextBatchAt = 0
		} else if scheduleChanged || current.NextBatchAt <= 0 || current.NextBatchAt%(int64(candidate.IntervalMinutes)*int64(time.Minute/time.Second)) != 0 {
			next, err := nextChannelModelDetectionIntervalBoundary(now, candidate.IntervalMinutes)
			if err != nil {
				return err
			}
			candidate.ScheduleAnchorAt = 0
			candidate.NextBatchAt = next.Unix()
		}
		if err := candidate.ApplyScheduledHighCostConfirmation(input.ConfirmHighCost); err != nil {
			return err
		}

		updates := map[string]any{
			"detector_url": candidate.DetectorURL, "pending_detector_url": candidate.PendingDetectorURL,
			"relay_url":        candidate.RelayURL,
			"scheduled_preset": candidate.ScheduledPreset, "schedule_enabled": candidate.ScheduleEnabled,
			"interval_minutes": candidate.IntervalMinutes, "interval_hours": candidate.IntervalHours,
			"display_value": candidate.DisplayValue, "display_unit": candidate.DisplayUnit,
			"schedule_time": "", "timezone": "", "schedule_anchor_at": int64(0), "next_batch_at": candidate.NextBatchAt,
			"scheduled_high_confirmed_revision": candidate.ScheduledHighConfirmedRevision,
			"revision":                          candidate.Revision, "lease_token": "", "lease_until": int64(0), "updated_at": candidate.UpdatedAt,
		}
		updated := transaction.Model(&model.ChannelModelDetectionGlobalConfig{}).
			Where("id = ? AND revision = ?", current.Id, current.Revision).Updates(updates)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrChannelModelDetectionSettingsConflict
		}
		saved = candidate
		return nil
	})
	if err != nil {
		return ChannelModelDetectionSettingsResponse{}, err
	}
	if activeDetectorChanged {
		ResetChannelModelDetectionServiceCache()
	}
	if db == model.DB {
		NotifyChannelModelDetectionOverviewChanged()
	}
	response := channelModelDetectionSettingsResponse(saved, addressChanged || activeDetectorChanged)
	return response, nil
}

func ValidateChannelModelDetectorTarget(ctx context.Context, raw string) (string, error) {
	normalized, err := NormalizeChannelModelDetectorURL(raw)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(normalized)
	if err != nil || parsed == nil {
		return "", errors.New("检测器地址格式无效")
	}
	if parsed.Port() != "" {
		port, err := strconv.Atoi(parsed.Port())
		if err != nil || port < 1 || port > 65535 {
			return "", errors.New("检测器地址端口无效")
		}
	}
	host := parsed.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if !channelModelDetectionAllowedTargetIP(ip) {
			return "", ErrChannelModelDetectionInvalidDetectorTarget
		}
		return normalized, nil
	}
	if !channelModelDetectionStaticHostname(host) {
		return "", ErrChannelModelDetectionInvalidDetectorTarget
	}
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return normalized, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	lookupContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupIPAddr(lookupContext, host)
	if err != nil || len(addresses) == 0 {
		return "", errors.New("检测器地址主机无法解析为静态私网目标")
	}
	for _, address := range addresses {
		if !channelModelDetectionAllowedTargetIP(address.IP) {
			return "", ErrChannelModelDetectionInvalidDetectorTarget
		}
	}
	return normalized, nil
}

func NormalizeChannelModelDetectionRelayURL(raw string) (string, error) {
	normalized, err := NormalizeChannelModelDetectorURL(raw)
	if err != nil {
		return "", fmt.Errorf("模型检测内部 Relay 地址无效: %w", err)
	}
	parsed, err := url.Parse(normalized)
	if err != nil || parsed == nil {
		return "", errors.New("模型检测内部 Relay 地址格式无效")
	}
	if !strings.HasSuffix(parsed.Path, "/internal/model-detector/v1") {
		return "", errors.New("模型检测内部 Relay 地址必须以 /internal/model-detector/v1 结尾")
	}
	return normalized, nil
}

func ResolveChannelModelDetectionRelayBaseURL(ctx context.Context, tx *gorm.DB) (string, error) {
	db := tx
	if db == nil {
		db = model.DB
	}
	if db == nil {
		return "", errors.New("模型检测统一设置数据库不可用")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var config model.ChannelModelDetectionGlobalConfig
	err := db.WithContext(ctx).
		Select("relay_url").
		Where("id = ?", model.ChannelModelDetectionConfigID).
		First(&config).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", errors.New("尚未配置模型检测内部 Relay 地址")
	}
	if err != nil {
		return "", err
	}

	value := strings.TrimSpace(config.RelayURL)
	if value == "" {
		return "", errors.New("尚未配置模型检测内部 Relay 地址")
	}
	return NormalizeChannelModelDetectionRelayURL(value)
}

func GetChannelModelDetectionService(ctx context.Context, tx *gorm.DB, now time.Time) (ChannelModelDetectionServiceResponse, error) {
	settings, err := GetChannelModelDetectionSettings(ctx, tx, now)
	if err != nil {
		return ChannelModelDetectionServiceResponse{}, err
	}
	return ChannelModelDetectionServiceSnapshotFromSettings(settings), nil
}

func ChannelModelDetectionServiceSnapshot(detectorURL string) ChannelModelDetectionServiceResponse {
	return ChannelModelDetectionServiceSnapshotFromSettings(ChannelModelDetectionSettingsResponse{
		DetectorURLConfigured: strings.TrimSpace(detectorURL) != "",
		DetectorURLMasked:     MaskChannelModelDetectorURL(detectorURL),
	})
}

func ChannelModelDetectionServiceSnapshotFromSettings(settings ChannelModelDetectionSettingsResponse) ChannelModelDetectionServiceResponse {
	channelModelDetectionServiceCache.RLock()
	response := channelModelDetectionServiceCache.value
	channelModelDetectionServiceCache.RUnlock()
	response.DetectorURLConfigured = settings.DetectorURLConfigured
	response.DetectorURLMasked = settings.DetectorURLMasked
	if response.Estimates == nil {
		response.Estimates = map[string]ChannelModelDetectionPresetEstimateResponse{}
	}
	if !settings.DetectorURLConfigured {
		response = ChannelModelDetectionServiceResponse{
			State: "unconfigured", DetectorURLConfigured: false, DetectorURLMasked: "",
			Estimates: map[string]ChannelModelDetectionPresetEstimateResponse{},
		}
		return response
	}
	if response.State == "" {
		response.State = "unknown"
	}
	return response
}

func TestChannelModelDetectionService(ctx context.Context, tx *gorm.DB, now time.Time, options ...ChannelModelDetectorClientOptions) (ChannelModelDetectionServiceResponse, error) {
	db := tx
	if db == nil {
		db = model.DB
	}
	if db == nil {
		return ChannelModelDetectionServiceResponse{}, errors.New("模型检测统一设置数据库不可用")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var config model.ChannelModelDetectionGlobalConfig
	if err := db.WithContext(ctx).Where("id = ?", model.ChannelModelDetectionConfigID).First(&config).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ChannelModelDetectionServiceResponse{}, ErrChannelModelDetectionDetectorNotConfigured
		}
		return ChannelModelDetectionServiceResponse{}, err
	}
	if strings.TrimSpace(config.DetectorURL) == "" {
		return ChannelModelDetectionServiceResponse{}, ErrChannelModelDetectionDetectorNotConfigured
	}
	response, testErr := testChannelModelDetectionServiceURL(ctx, db, config.DetectorURL, now, true, options...)
	if db == model.DB {
		NotifyChannelModelDetectionOverviewChanged()
	}
	return response, testErr
}

func TestChannelModelDetectionServiceURL(ctx context.Context, tx *gorm.DB, rawURL string, now time.Time, options ...ChannelModelDetectorClientOptions) (ChannelModelDetectionServiceResponse, error) {
	db := tx
	if db == nil {
		db = model.DB
	}
	if db == nil {
		return ChannelModelDetectionServiceResponse{}, errors.New("模型检测统一设置数据库不可用")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	normalizedURL, err := ValidateChannelModelDetectorTarget(ctx, rawURL)
	if err != nil {
		return ChannelModelDetectionServiceResponse{}, err
	}
	return testChannelModelDetectionServiceURL(ctx, db, normalizedURL, now, false, options...)
}

func testChannelModelDetectionServiceURL(ctx context.Context, db *gorm.DB, detectorURL string, now time.Time, updateCache bool, options ...ChannelModelDetectorClientOptions) (ChannelModelDetectionServiceResponse, error) {
	client, err := NewChannelModelDetectorClient(detectorURL, options...)
	if err != nil {
		return ChannelModelDetectionServiceResponse{}, err
	}
	response := ChannelModelDetectionServiceResponse{
		State: "available", DetectorURLConfigured: true, DetectorURLMasked: MaskChannelModelDetectorURL(detectorURL),
		LastCheckedAt: now.Unix(), CompatibilityMessage: "官方检测器接口兼容", Estimates: map[string]ChannelModelDetectionPresetEstimateResponse{},
	}
	compatibility, compatibilityErr := client.CheckCompatibility(ctx)
	if compatibilityErr == nil {
		status, statusErr := client.Status(ctx)
		if statusErr == nil {
			response.Busy = status.Status == "running" || status.Status == "stopping"
			if status.SessionID != "" {
				var owned int64
				if err := db.WithContext(ctx).Model(&model.ChannelModelDetectionExecution{}).
					Where("official_session_id = ? AND status IN ?", status.SessionID, []string{model.ChannelModelDetectionExecutionStatusSubmitting, model.ChannelModelDetectionExecutionStatusRunning}).Count(&owned).Error; err == nil {
					response.ActiveSessionOwned = owned > 0
				}
			}
		}
		for _, presetName := range []string{model.ChannelModelDetectionPresetLow, model.ChannelModelDetectionPresetMedium, model.ChannelModelDetectionPresetHigh} {
			preset, ok := compatibility.Bootstrap.Preset(presetName)
			entry := ChannelModelDetectionPresetEstimateResponse{Preset: presetName, Available: ok}
			if !ok {
				entry.UnavailableReason = "官方检测器未提供该档位"
				response.State = "degraded"
				response.Estimates[presetName] = entry
				continue
			}
			estimate := compatibility.LowEstimate
			if presetName != model.ChannelModelDetectionPresetLow {
				estimate, err = client.Estimate(ctx, preset)
				if err != nil {
					entry.Available = false
					entry.UnavailableReason = sanitizeChannelModelDetectionServiceError(err.Error())
					response.State = "degraded"
					response.Estimates[presetName] = entry
					continue
				}
			}
			entry.LogicalRequests = estimate.TotalRequests
			entry.Fixed32KRequests = estimate.Fixed32KRequests
			entry.ConfigHash = estimate.ConfigHash
			response.Estimates[presetName] = entry
		}
	} else {
		response.LastError = sanitizeChannelModelDetectionServiceError(compatibilityErr.Error())
		response.CompatibilityMessage = "官方检测器检查失败"
		if errors.Is(compatibilityErr, ErrChannelModelDetectorIncompatible) {
			response.State = "incompatible"
			response.CompatibilityMessage = "官方检测器版本或接口不兼容：" + response.LastError
		} else {
			response.State = "offline"
		}
	}
	if updateCache {
		channelModelDetectionServiceCache.Lock()
		channelModelDetectionServiceCache.value = response
		channelModelDetectionServiceCache.Unlock()
	}
	return response, compatibilityErr
}

func ResetChannelModelDetectionServiceCache() {
	channelModelDetectionServiceCache.Lock()
	channelModelDetectionServiceCache.value = ChannelModelDetectionServiceResponse{}
	channelModelDetectionServiceCache.Unlock()
}

func MaskChannelModelDetectorURL(raw string) string {
	return maskChannelModelDetectorURL(raw)
}

func channelModelDetectionDefaultGlobalConfig(now time.Time) model.ChannelModelDetectionGlobalConfig {
	config := model.ChannelModelDetectionGlobalConfig{
		Id: model.ChannelModelDetectionConfigID, DetectorURL: strings.TrimSpace(os.Getenv("GPT56_DETECTOR_URL")),
		ScheduledPreset: model.ChannelModelDetectionPresetMedium, IntervalMinutes: model.ChannelModelDetectionDefaultIntervalMinutes,
		DisplayValue: model.ChannelModelDetectionDefaultDisplayValue, DisplayUnit: model.ChannelModelDetectionDefaultDisplayUnit,
		Revision: 1, CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
	}
	if config.DetectorURL != "" {
		if normalized, err := ValidateChannelModelDetectorTarget(context.Background(), config.DetectorURL); err == nil {
			config.DetectorURL = normalized
		} else {
			config.DetectorURL = ""
		}
	}
	return config
}

func channelModelDetectionSettingsResponse(config model.ChannelModelDetectionGlobalConfig, connectionTestRequired bool) ChannelModelDetectionSettingsResponse {
	displayValue, displayUnit := config.EffectiveDisplay()
	return ChannelModelDetectionSettingsResponse{
		DetectorURLConfigured: strings.TrimSpace(config.DetectorURL) != "", DetectorURL: strings.TrimSpace(config.DetectorURL), DetectorURLMasked: MaskChannelModelDetectorURL(config.DetectorURL),
		PendingDetectorURLConfigured: strings.TrimSpace(config.PendingDetectorURL) != "", PendingDetectorURL: strings.TrimSpace(config.PendingDetectorURL), PendingDetectorURLMasked: MaskChannelModelDetectorURL(config.PendingDetectorURL),
		DetectorURLSwitchPending: strings.TrimSpace(config.PendingDetectorURL) != "",
		RelayURLConfigured:       config.RelayURLConfigured(), RelayURL: strings.TrimSpace(config.RelayURL),
		ScheduledPreset: config.ScheduledPreset, ScheduleEnabled: config.ScheduleEnabled, IntervalMinutes: config.EffectiveIntervalMinutes(), DisplayValue: displayValue, DisplayUnit: displayUnit,
		IntervalHours: config.IntervalHours, ScheduleTime: config.ScheduleTime, Timezone: config.Timezone, ScheduleAnchorAt: config.ScheduleAnchorAt,
		NextBatchAt: config.NextBatchAt, Revision: config.Revision, ConnectionTestRequired: connectionTestRequired,
		CreatedAt: config.CreatedAt, UpdatedAt: config.UpdatedAt,
	}
}

func channelModelDetectionAllowedTargetIP(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || common.IsPrivateIP(ip)
}

func channelModelDetectionStaticHostname(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" || len(host) > 253 || strings.ContainsAny(host, "%/\\") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if character != '-' && (character < '0' || character > '9') && (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') {
				return false
			}
		}
	}
	return true
}

func sanitizeChannelModelDetectionServiceError(message string) string {
	message = redactDetectorMessage(strings.TrimSpace(message))
	if len(message) > 512 {
		message = message[:512]
	}
	return message
}
