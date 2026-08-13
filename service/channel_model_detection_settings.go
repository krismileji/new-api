package service

import (
	"context"
	"errors"
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
	ScheduledPreset  string
	ConfirmHighCost  bool
	ScheduleEnabled  bool
	IntervalHours    int
	ScheduleTime     string
	Timezone         string
	ExpectedRevision int64
}

type ChannelModelDetectionSettingsResponse struct {
	DetectorURLConfigured        bool   `json:"detector_url_configured"`
	DetectorURLMasked            string `json:"detector_url_masked"`
	PendingDetectorURLConfigured bool   `json:"pending_detector_url_configured"`
	PendingDetectorURLMasked     string `json:"pending_detector_url_masked"`
	DetectorURLSwitchPending     bool   `json:"detector_url_switch_pending"`
	ScheduledPreset              string `json:"scheduled_preset"`
	ScheduleEnabled              bool   `json:"schedule_enabled"`
	IntervalHours                int    `json:"interval_hours"`
	ScheduleTime                 string `json:"schedule_time"`
	Timezone                     string `json:"timezone"`
	ScheduleAnchorAt             int64  `json:"schedule_anchor_at"`
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

	var saved model.ChannelModelDetectionGlobalConfig
	addressChanged := false
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
		candidate.IntervalHours = input.IntervalHours
		candidate.ScheduleTime = strings.TrimSpace(input.ScheduleTime)
		candidate.Timezone = strings.TrimSpace(input.Timezone)
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

		scheduleChanged := candidate.ScheduleEnabled != current.ScheduleEnabled ||
			candidate.IntervalHours != current.IntervalHours || candidate.ScheduleTime != current.ScheduleTime || candidate.Timezone != current.Timezone
		if !candidate.ScheduleEnabled {
			candidate.ScheduleAnchorAt = 0
			candidate.NextBatchAt = 0
		} else if scheduleChanged || current.ScheduleAnchorAt <= 0 || current.NextBatchAt <= 0 {
			anchor, err := CalculateChannelModelDetectionScheduleAnchor(now, candidate.ScheduleTime, candidate.Timezone)
			if err != nil {
				return err
			}
			candidate.ScheduleAnchorAt = anchor.Unix()
			candidate.NextBatchAt = anchor.Unix()
		}
		if err := candidate.ApplyScheduledHighCostConfirmation(input.ConfirmHighCost); err != nil {
			return err
		}

		updates := map[string]any{
			"detector_url": candidate.DetectorURL, "pending_detector_url": candidate.PendingDetectorURL,
			"scheduled_preset": candidate.ScheduledPreset, "schedule_enabled": candidate.ScheduleEnabled,
			"interval_hours": candidate.IntervalHours, "schedule_time": candidate.ScheduleTime, "timezone": candidate.Timezone,
			"schedule_anchor_at": candidate.ScheduleAnchorAt, "next_batch_at": candidate.NextBatchAt,
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
	if addressChanged {
		ResetChannelModelDetectionServiceCache()
	}
	response := channelModelDetectionSettingsResponse(saved, addressChanged)
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
	client, err := NewChannelModelDetectorClient(config.DetectorURL, options...)
	if err != nil {
		return ChannelModelDetectionServiceResponse{}, err
	}
	response := ChannelModelDetectionServiceResponse{
		State: "available", DetectorURLConfigured: true, DetectorURLMasked: MaskChannelModelDetectorURL(config.DetectorURL),
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
		} else {
			response.State = "offline"
		}
	}
	channelModelDetectionServiceCache.Lock()
	channelModelDetectionServiceCache.value = response
	channelModelDetectionServiceCache.Unlock()
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
		ScheduledPreset: model.ChannelModelDetectionPresetMedium, IntervalHours: model.ChannelModelDetectionDefaultIntervalHours,
		ScheduleTime: model.ChannelModelDetectionDefaultScheduleTime, Timezone: model.ChannelModelDetectionDefaultTimezone,
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
	return ChannelModelDetectionSettingsResponse{
		DetectorURLConfigured: strings.TrimSpace(config.DetectorURL) != "", DetectorURLMasked: MaskChannelModelDetectorURL(config.DetectorURL),
		PendingDetectorURLConfigured: strings.TrimSpace(config.PendingDetectorURL) != "", PendingDetectorURLMasked: MaskChannelModelDetectorURL(config.PendingDetectorURL),
		DetectorURLSwitchPending: strings.TrimSpace(config.PendingDetectorURL) != "",
		ScheduledPreset:          config.ScheduledPreset, ScheduleEnabled: config.ScheduleEnabled, IntervalHours: config.IntervalHours,
		ScheduleTime: config.ScheduleTime, Timezone: config.Timezone, ScheduleAnchorAt: config.ScheduleAnchorAt,
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
