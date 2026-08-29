package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	maxChannelMonitorRatio            = 1_000_000
	maxChannelMonitorBalanceThreshold = 1_000_000_000_000
)

type channelRatioUpdateRequest struct {
	Ratio  *float64 `json:"ratio"`
	Remark string   `json:"remark"`
}

type groupRatioUpdateRequest struct {
	Group string   `json:"group"`
	Ratio *float64 `json:"ratio"`
}

type groupRatioSyncRequest struct {
	Group       string   `json:"group"`
	Coefficient *float64 `json:"coefficient"`
}

type channelMonitorUpstreamRequest struct {
	Type                        string                                      `json:"type"`
	BaseURL                     string                                      `json:"base_url"`
	Group                       string                                      `json:"group"`
	AuthType                    string                                      `json:"auth_type"`
	UserId                      int                                         `json:"user_id"`
	AccessToken                 string                                      `json:"access_token"`
	RefreshToken                *string                                     `json:"refresh_token"`
	Account                     string                                      `json:"account"`
	Password                    string                                      `json:"password"`
	SingleChannelAction         string                                      `json:"single_channel_action"`
	MultipleChannelsAction      string                                      `json:"multiple_channels_action"`
	BalanceWarningThreshold     json.RawMessage                             `json:"balance_warning_threshold"`
	BalanceAutoDisableThreshold json.RawMessage                             `json:"balance_auto_disable_threshold"`
	RatioSyncEnabled            *bool                                       `json:"ratio_sync_enabled"`
	BalanceSyncEnabled          *bool                                       `json:"balance_sync_enabled"`
	CostConversion              *service.ChannelMonitorCostConversion       `json:"cost_conversion"`
	CustomConfig                *service.ChannelMonitorCustomUpstreamConfig `json:"custom_config"`
}

type channelMonitorUpstreamConfig struct {
	Type                        string                                      `json:"type"`
	BaseURL                     string                                      `json:"base_url"`
	Group                       string                                      `json:"group"`
	AuthType                    string                                      `json:"auth_type"`
	UserId                      int                                         `json:"user_id"`
	HasAccessToken              bool                                        `json:"has_access_token"`
	HasRefreshToken             bool                                        `json:"has_refresh_token"`
	Account                     string                                      `json:"account"`
	HasPassword                 bool                                        `json:"has_password"`
	SingleChannelAction         string                                      `json:"single_channel_action"`
	MultipleChannelsAction      string                                      `json:"multiple_channels_action"`
	BalanceWarningThreshold     *float64                                    `json:"balance_warning_threshold"`
	BalanceAutoDisableThreshold *float64                                    `json:"balance_auto_disable_threshold"`
	RatioSyncEnabled            bool                                        `json:"ratio_sync_enabled"`
	BalanceSyncEnabled          bool                                        `json:"balance_sync_enabled"`
	CostConversion              service.ChannelMonitorCostConversion        `json:"cost_conversion"`
	CustomConfig                *service.ChannelMonitorCustomUpstreamConfig `json:"custom_config,omitempty"`
}

type channelMonitorItem struct {
	Id                       int                           `json:"id"`
	Name                     string                        `json:"name"`
	Type                     int                           `json:"type"`
	Status                   int                           `json:"status"`
	StatusReason             string                        `json:"status_reason"`
	Priority                 int64                         `json:"priority"`
	Weight                   int                           `json:"weight"`
	BaseURL                  string                        `json:"base_url"`
	Models                   string                        `json:"models"`
	TestModel                *string                       `json:"test_model"`
	Groups                   []string                      `json:"groups"`
	Ratio                    *float64                      `json:"ratio"`
	PreviousRatio            *float64                      `json:"previous_ratio"`
	CostRatio                *float64                      `json:"cost_ratio"`
	PreviousCostRatio        *float64                      `json:"previous_cost_ratio"`
	ConversionFactor         *float64                      `json:"conversion_factor"`
	Remark                   string                        `json:"remark"`
	ChannelRemark            string                        `json:"channel_remark"`
	UpdatedTime              int64                         `json:"updated_time"`
	UpdatedBy                int                           `json:"updated_by"`
	UpdatedByUsername        string                        `json:"updated_by_username"`
	LastFetchStatus          string                        `json:"last_fetch_status"`
	LastFetchError           string                        `json:"last_fetch_error"`
	LastFetchTime            int64                         `json:"last_fetch_time"`
	ConsecutiveFailures      int                           `json:"consecutive_failures"`
	UpstreamBalance          *float64                      `json:"upstream_balance"`
	LastBalanceTime          int64                         `json:"last_balance_time"`
	LastBalanceError         string                        `json:"last_balance_error"`
	TodayCostCNY             float64                       `json:"today_cost_cny"`
	TodayCostConfigured      bool                          `json:"today_cost_configured"`
	TodayCostComplete        bool                          `json:"today_cost_complete"`
	TodayCostUnresolvedCount int64                         `json:"today_cost_unresolved_count"`
	ConcurrencyLimit         int                           `json:"concurrency_limit"`
	RPMLimit                 int                           `json:"rpm_limit"`
	ConcurrencyActive        int                           `json:"concurrency_active"`
	CurrentRPM               int                           `json:"current_rpm"`
	Upstream                 *channelMonitorUpstreamConfig `json:"upstream"`
}

func validateChannelMonitorRatio(ratio *float64) bool {
	return ratio != nil && !math.IsNaN(*ratio) && !math.IsInf(*ratio, 0) && *ratio >= 0 && *ratio <= maxChannelMonitorRatio
}

func channelMonitorUpstreamFromModel(monitor model.ChannelRatioMonitor) *channelMonitorUpstreamConfig {
	if monitor.UpstreamType == "" {
		return nil
	}
	costConversion, err := service.ParseChannelMonitorCostConversion(monitor.CostConversion)
	if err != nil {
		costConversion = service.ChannelMonitorCostConversion{Mode: service.ChannelMonitorCostConversionNone}
	}
	var customConfig *service.ChannelMonitorCustomUpstreamConfig
	if monitor.UpstreamType == service.CustomUpstreamType {
		parsed, parseErr := service.ParseChannelMonitorCustomUpstreamConfig(monitor.CustomUpstreamConfig)
		if parseErr == nil {
			sanitized := service.SanitizeChannelMonitorCustomUpstreamConfig(parsed)
			customConfig = &sanitized
		}
	}
	authType := monitor.UpstreamAuthType
	if authType == service.Sub2APIAuthRefreshToken {
		authType = service.Sub2APIAuthToken
	}
	return &channelMonitorUpstreamConfig{
		Type:                        monitor.UpstreamType,
		BaseURL:                     monitor.UpstreamBaseURL,
		Group:                       monitor.UpstreamGroup,
		AuthType:                    authType,
		UserId:                      monitor.UpstreamUserId,
		HasAccessToken:              monitor.UpstreamAuthType != service.Sub2APIAuthRefreshToken && monitor.UpstreamAccessToken != "",
		HasRefreshToken:             monitor.UpstreamRefreshToken != "" || monitor.UpstreamAuthType == service.Sub2APIAuthRefreshToken,
		Account:                     monitor.UpstreamAccount,
		HasPassword:                 monitor.UpstreamPassword != "",
		SingleChannelAction:         normalizeChannelMonitorPolicyAction(monitor.SingleChannelAction),
		MultipleChannelsAction:      normalizeChannelMonitorPolicyAction(monitor.MultipleChannelsAction),
		BalanceWarningThreshold:     monitor.BalanceWarningThreshold,
		BalanceAutoDisableThreshold: monitor.BalanceAutoDisableThreshold,
		RatioSyncEnabled:            !monitor.UpstreamRatioSyncDisabled,
		BalanceSyncEnabled:          !monitor.UpstreamBalanceSyncDisabled,
		CostConversion:              costConversion,
		CustomConfig:                customConfig,
	}
}

func channelMonitorCostRatioFromModel(monitor model.ChannelRatioMonitor, upstreamRatio float64) (float64, float64, error) {
	costConversion, err := service.ParseChannelMonitorCostConversion(monitor.CostConversion)
	if err != nil {
		return 0, 0, err
	}
	return service.CalculateChannelMonitorCostRatio(upstreamRatio, costConversion)
}

func channelMonitorCostTrackingConfigured(monitor model.ChannelRatioMonitor) bool {
	if monitor.UpdatedTime <= 0 {
		return false
	}
	costConversion, err := service.ParseChannelMonitorCostConversion(monitor.CostConversion)
	if err != nil {
		return false
	}
	_, _, err = service.CalculateChannelMonitorCostRatio(monitor.Ratio, costConversion)
	return err == nil
}

func channelMonitorCostConversionLabel(config service.ChannelMonitorCostConversion) string {
	switch config.Mode {
	case service.ChannelMonitorCostConversionRecharge:
		return "充值换算"
	case service.ChannelMonitorCostConversionSubscription:
		return "订阅换算"
	default:
		return "不换算"
	}
}

func channelMonitorUpstreamTypeLabel(upstreamType string) string {
	switch upstreamType {
	case service.NewAPIUpstreamType:
		return "New API"
	case service.Sub2APIUpstreamType:
		return "Sub2API"
	case service.CustomUpstreamType:
		return "自定义上游"
	default:
		return upstreamType
	}
}

func resolveChannelMonitorBalanceThreshold(raw json.RawMessage, existing *float64, invalidMessage string) (*float64, error) {
	if len(raw) == 0 {
		if existing == nil {
			return nil, nil
		}
		value := *existing
		return &value, nil
	}
	if strings.TrimSpace(string(raw)) == "null" {
		return nil, nil
	}

	var threshold float64
	if err := common.Unmarshal(raw, &threshold); err != nil ||
		math.IsNaN(threshold) || math.IsInf(threshold, 0) ||
		threshold < 0 || threshold > maxChannelMonitorBalanceThreshold {
		return nil, errors.New(invalidMessage)
	}
	return &threshold, nil
}

func resolveChannelMonitorUpstreamRequest(channel *model.Channel, request channelMonitorUpstreamRequest, requireGroup bool) (service.ChannelMonitorUpstreamConfig, error) {
	request.Type = strings.TrimSpace(request.Type)
	if request.Type == "" {
		request.Type = service.NewAPIUpstreamType
	}
	request.Group = strings.TrimSpace(request.Group)
	if (requireGroup && request.Type != service.CustomUpstreamType && request.Group == "") || utf8.RuneCountInString(request.Group) > 64 {
		return service.ChannelMonitorUpstreamConfig{}, errors.New("上游分组名称无效")
	}

	baseURL := strings.TrimSpace(request.BaseURL)
	if baseURL == "" {
		baseURL = channel.GetBaseURL()
	}
	var normalizedBaseURL string
	var err error
	if request.Type == service.CustomUpstreamType {
		normalizedBaseURL, err = service.NormalizeChannelMonitorCustomBaseURL(baseURL)
	} else {
		normalizedBaseURL, err = service.NormalizeNewAPIBaseURL(baseURL)
	}
	if err != nil {
		return service.ChannelMonitorUpstreamConfig{}, err
	}

	costConversion := service.ChannelMonitorCostConversion{Mode: service.ChannelMonitorCostConversionNone}
	if request.CostConversion != nil {
		costConversion, err = service.NormalizeChannelMonitorCostConversion(*request.CostConversion)
		if err != nil {
			return service.ChannelMonitorUpstreamConfig{}, err
		}
	}

	request.AuthType = strings.TrimSpace(request.AuthType)
	config := service.ChannelMonitorUpstreamConfig{
		Type:           request.Type,
		BaseURL:        normalizedBaseURL,
		Group:          request.Group,
		AuthType:       request.AuthType,
		Proxy:          channel.GetSetting().Proxy,
		RequestTimeout: getChannelMonitorSettings().upstreamRequestTimeout(),
		SkipBalance:    request.BalanceSyncEnabled != nil && !*request.BalanceSyncEnabled,
		CostConversion: costConversion,
	}
	switch request.Type {
	case service.NewAPIUpstreamType:
		if request.AuthType != service.NewAPIUpstreamAuthPublic && request.AuthType != service.NewAPIUpstreamAuthUser {
			return service.ChannelMonitorUpstreamConfig{}, errors.New("New API 认证方式无效")
		}
		if request.AuthType == service.NewAPIUpstreamAuthPublic {
			return config, nil
		}
		if request.UserId <= 0 {
			return service.ChannelMonitorUpstreamConfig{}, errors.New("上游用户 ID 必须大于 0")
		}
		config.UserID = request.UserId
		config.AccessToken = strings.TrimSpace(request.AccessToken)
		if utf8.RuneCountInString(config.AccessToken) > 4096 {
			return service.ChannelMonitorUpstreamConfig{}, errors.New("上游访问令牌过长")
		}
		if config.AccessToken == "" {
			monitor, findErr := model.GetChannelRatioMonitor(channel.Id)
			if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
				return service.ChannelMonitorUpstreamConfig{}, findErr
			}
			if findErr == nil &&
				monitor.UpstreamType == config.Type &&
				monitor.UpstreamBaseURL == config.BaseURL &&
				monitor.UpstreamAuthType == config.AuthType &&
				monitor.UpstreamUserId == config.UserID {
				config.AccessToken = monitor.UpstreamAccessToken
			}
		}
		if config.AccessToken == "" {
			return service.ChannelMonitorUpstreamConfig{}, errors.New("上游访问令牌不能为空")
		}
		return config, nil
	case service.Sub2APIUpstreamType:
		if request.AuthType == service.Sub2APIAuthAPIKey {
			if len(channel.GetKeys()) == 0 {
				return service.ChannelMonitorUpstreamConfig{}, errors.New("Sub2API API Key 认证需要先在渠道中配置上游 API Key")
			}
			config.ChannelKeys = channel.GetKeys()
			return config, nil
		}
		if request.AuthType == service.Sub2APIAuthAccount {
			config.Account = strings.TrimSpace(request.Account)
			if config.Account == "" {
				return service.ChannelMonitorUpstreamConfig{}, errors.New("Sub2API 登录邮箱不能为空")
			}
			if utf8.RuneCountInString(config.Account) > 320 {
				return service.ChannelMonitorUpstreamConfig{}, errors.New("Sub2API 登录邮箱过长")
			}
			config.Password = request.Password
			if utf8.RuneCountInString(config.Password) > 4096 {
				return service.ChannelMonitorUpstreamConfig{}, errors.New("Sub2API 登录密码过长")
			}
			if config.Password == "" {
				monitor, findErr := model.GetChannelRatioMonitor(channel.Id)
				if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
					return service.ChannelMonitorUpstreamConfig{}, findErr
				}
				if findErr == nil &&
					monitor.UpstreamType == config.Type &&
					monitor.UpstreamBaseURL == config.BaseURL &&
					monitor.UpstreamAuthType == config.AuthType &&
					monitor.UpstreamAccount == config.Account {
					config.Password = monitor.UpstreamPassword
				}
			}
			if config.Password == "" {
				return service.ChannelMonitorUpstreamConfig{}, errors.New("Sub2API 登录密码不能为空")
			}
			return config, nil
		}
		if request.AuthType != service.Sub2APIAuthToken && request.AuthType != service.Sub2APIAuthRefreshToken {
			return service.ChannelMonitorUpstreamConfig{}, errors.New("Sub2API 认证方式无效")
		}
		if request.AuthType == service.Sub2APIAuthToken {
			config.AccessToken = strings.TrimSpace(request.AccessToken)
			if request.RefreshToken != nil {
				config.RefreshToken = strings.TrimSpace(*request.RefreshToken)
			}
			config.RefreshTokenStoredSeparately = true
			if utf8.RuneCountInString(config.AccessToken) > 4096 {
				return service.ChannelMonitorUpstreamConfig{}, errors.New("Sub2API Token 过长")
			}
			if utf8.RuneCountInString(config.RefreshToken) > 4096 {
				return service.ChannelMonitorUpstreamConfig{}, errors.New("Sub2API Refresh Token 过长")
			}
			monitor, findErr := model.GetChannelRatioMonitor(channel.Id)
			if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
				return service.ChannelMonitorUpstreamConfig{}, findErr
			}
			if findErr == nil && monitor.UpstreamType == config.Type && monitor.UpstreamBaseURL == config.BaseURL && (monitor.UpstreamAuthType == service.Sub2APIAuthToken || monitor.UpstreamAuthType == service.Sub2APIAuthRefreshToken) {
				if config.AccessToken == "" {
					if monitor.UpstreamAuthType == service.Sub2APIAuthToken {
						config.AccessToken = monitor.UpstreamAccessToken
					}
				}
				if config.RefreshToken == "" && request.RefreshToken == nil {
					if monitor.UpstreamAuthType == service.Sub2APIAuthToken {
						config.RefreshToken = monitor.UpstreamRefreshToken
					} else {
						config.RefreshToken = monitor.UpstreamAccessToken
					}
				}
				config.CredentialID = monitor.ChannelId
				config.Revision = monitor.UpstreamRevision
			}
			if config.AccessToken == "" {
				return service.ChannelMonitorUpstreamConfig{}, errors.New("Sub2API Token 不能为空")
			}
			return config, nil
		}
		var savedMonitor model.ChannelRatioMonitor
		hasSavedMonitor := false
		monitor, findErr := model.GetChannelRatioMonitor(channel.Id)
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return service.ChannelMonitorUpstreamConfig{}, findErr
		}
		if findErr == nil && monitor.UpstreamType == config.Type && monitor.UpstreamBaseURL == config.BaseURL {
			if monitor.UpstreamAuthType == config.AuthType {
				savedMonitor = monitor
				hasSavedMonitor = true
			} else if request.AuthType == service.Sub2APIAuthRefreshToken && monitor.UpstreamAuthType == service.Sub2APIAuthToken && monitor.UpstreamRefreshToken != "" {
				savedMonitor = monitor
				hasSavedMonitor = true
				config.RefreshTokenStoredSeparately = true
			}
		}
		config.AccessToken = strings.TrimSpace(request.AccessToken)
		if utf8.RuneCountInString(config.AccessToken) > 4096 {
			if request.AuthType == service.Sub2APIAuthRefreshToken {
				return service.ChannelMonitorUpstreamConfig{}, errors.New("Sub2API Refresh Token 过长")
			}
			return service.ChannelMonitorUpstreamConfig{}, errors.New("Sub2API Token 过长")
		}
		if request.AuthType == service.Sub2APIAuthRefreshToken && config.AccessToken != "" && hasSavedMonitor {
			config.AccessToken = service.CanonicalSub2APIRefreshToken(service.ChannelMonitorUpstreamConfig{
				BaseURL:      config.BaseURL,
				AuthType:     config.AuthType,
				AccessToken:  config.AccessToken,
				CredentialID: savedMonitor.ChannelId,
				Revision:     savedMonitor.UpstreamRevision,
				Proxy:        config.Proxy,
			})
		}
		if config.AccessToken == "" {
			if hasSavedMonitor {
				if config.RefreshTokenStoredSeparately {
					config.AccessToken = savedMonitor.UpstreamRefreshToken
				} else {
					config.AccessToken = savedMonitor.UpstreamAccessToken
				}
				config.CredentialID = savedMonitor.ChannelId
				config.Revision = savedMonitor.UpstreamRevision
			}
		} else if hasSavedMonitor && config.AccessToken == savedMonitor.UpstreamAccessToken {
			config.CredentialID = savedMonitor.ChannelId
			config.Revision = savedMonitor.UpstreamRevision
		}
		if request.AuthType == service.Sub2APIAuthRefreshToken && config.AccessToken != "" && config.CredentialID == 0 {
			// A new unsaved credential must be allowed to refresh first; the
			// save path canonicalizes any rotated value afterward.
			config.Revision = 0
		}
		if config.AccessToken == "" {
			if request.AuthType == service.Sub2APIAuthRefreshToken {
				return service.ChannelMonitorUpstreamConfig{}, errors.New("Sub2API Refresh Token 不能为空")
			}
			return service.ChannelMonitorUpstreamConfig{}, errors.New("Sub2API Token 不能为空")
		}
		return config, nil
	case service.CustomUpstreamType:
		config.AuthType = service.CustomUpstreamAuthType
		var existingConfig *service.ChannelMonitorCustomUpstreamConfig
		monitor, findErr := model.GetChannelRatioMonitor(channel.Id)
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return service.ChannelMonitorUpstreamConfig{}, findErr
		}
		if findErr == nil && monitor.UpstreamType == service.CustomUpstreamType && monitor.UpstreamBaseURL == normalizedBaseURL {
			parsed, parseErr := service.ParseChannelMonitorCustomUpstreamConfig(monitor.CustomUpstreamConfig)
			if parseErr == nil {
				existingConfig = &parsed
			}
		}
		if request.CustomConfig == nil {
			if existingConfig == nil {
				return service.ChannelMonitorUpstreamConfig{}, errors.New("自定义上游配置不能为空")
			}
			config.CustomConfig = *existingConfig
			return config, nil
		}
		customConfig, normalizeErr := service.NormalizeChannelMonitorCustomUpstreamConfigWithExisting(*request.CustomConfig, existingConfig)
		if normalizeErr != nil {
			return service.ChannelMonitorUpstreamConfig{}, normalizeErr
		}
		config.CustomConfig = customConfig
		return config, nil
	default:
		return service.ChannelMonitorUpstreamConfig{}, errors.New("上游类型无效")
	}
}

func getChannelMonitorOperator(c *gin.Context) (int, string) {
	operatorId := c.GetInt("id")
	operatorUsername := c.GetString("username")
	if operatorUsername == "" {
		operatorUsername, _ = model.GetUsernameById(operatorId, false)
	}
	return operatorId, operatorUsername
}

func GetChannelMonitorOverview(c *gin.Context) {
	if serveChannelMonitorPageSnapshot(c, channelMonitorPageSnapshotOverview, GetChannelMonitorOverview) {
		return
	}
	channels, err := model.GetAllChannelsForMonitor()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	monitors, err := model.GetChannelRatioMonitors()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	settings, err := loadChannelMonitorSettingsSnapshot(c.Request.Context())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	todayStart := channelMonitorCostDayStart(common.GetTimestamp())

	monitorByChannel := make(map[int]model.ChannelRatioMonitor, len(monitors))
	for _, monitor := range monitors {
		monitorByChannel[monitor.ChannelId] = monitor
	}
	todayCostByChannel, err := channelMonitorRealtimeTodayCosts(c.Request.Context(), 0, todayStart)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	groupRatios := ratio_setting.GetGroupRatioCopy()
	channelOrder := getChannelMonitorChannelOrder(channels)
	channelIDs := make([]int, 0, len(channels))
	for _, channel := range channels {
		if channel != nil {
			channelIDs = append(channelIDs, channel.Id)
		}
	}
	// Include zero-value entries for channels without a monitor row. This lets
	// the request-scoped snapshot clear a previously cached limit when an admin
	// removes/disables that row; omitting the key would otherwise preserve the
	// stale in-process configuration.
	concurrencyConfigs := make(map[int]model.ChannelConcurrencyConfig, len(channelIDs))
	for _, channelID := range channelIDs {
		concurrencyConfigs[channelID] = model.ChannelConcurrencyConfig{}
	}
	for _, monitor := range monitors {
		concurrencyConfigs[monitor.ChannelId] = model.ChannelConcurrencyConfig{
			Limit:    monitor.ConcurrencyLimit,
			RPMLimit: monitor.RPMLimit,
			Revision: monitor.ConcurrencyRevision,
		}
	}
	concurrencyByChannel, err := service.GetChannelConcurrencySnapshotWithRPMForChannelIDsAndConfigs(
		c.Request.Context(), channelIDs, concurrencyConfigs,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]channelMonitorItem, 0, len(channels))
	for _, channel := range channels {
		groups := channel.GetGroups()
		for _, group := range groups {
			if _, exists := groupRatios[group]; !exists {
				groupRatios[group] = 1
			}
		}
		channelRemark := ""
		if channel.Remark != nil {
			channelRemark = strings.TrimSpace(*channel.Remark)
		}
		statusReason := ""
		if channel.Status == common.ChannelStatusAutoDisabled {
			if reason, ok := channel.GetOtherInfo()["status_reason"].(string); ok {
				statusReason = strings.TrimSpace(reason)
			}
		}
		item := channelMonitorItem{
			Id:            channel.Id,
			Name:          channel.Name,
			Type:          channel.Type,
			Status:        channel.Status,
			StatusReason:  statusReason,
			Priority:      channel.GetPriority(),
			Weight:        channel.GetWeight(),
			BaseURL:       channel.GetBaseURL(),
			Models:        channel.Models,
			TestModel:     channel.TestModel,
			Groups:        groups,
			ChannelRemark: channelRemark,
		}
		if cost, exists := todayCostByChannel[channel.Id]; exists {
			item.TodayCostCNY = channelMonitorCostCNY(cost.CostNanoCNY)
			// An unresolved row is considered configured only when it carries a
			// positive conservative estimate. A zero-cost unresolved row usually
			// means the channel has no usable conversion configuration.
			item.TodayCostConfigured = cost.SettledCount > 0 || cost.CostNanoCNY > 0
			item.TodayCostComplete = cost.UnresolvedCount == 0
			item.TodayCostUnresolvedCount = cost.UnresolvedCount
		}
		if monitor, exists := monitorByChannel[channel.Id]; exists {
			item.ConcurrencyLimit = monitor.ConcurrencyLimit
			item.RPMLimit = monitor.RPMLimit
			if channelMonitorCostTrackingConfigured(monitor) {
				item.TodayCostConfigured = true
				if item.TodayCostUnresolvedCount == 0 {
					item.TodayCostComplete = true
				}
			}
			item.LastFetchStatus = monitor.LastFetchStatus
			item.LastFetchError = monitor.LastFetchError
			item.LastFetchTime = monitor.LastFetchTime
			item.ConsecutiveFailures = monitor.ConsecutiveFailures
			item.UpstreamBalance = monitor.UpstreamBalance
			item.LastBalanceTime = monitor.LastBalanceTime
			item.LastBalanceError = monitor.LastBalanceError
			if monitor.UpdatedTime > 0 {
				item.Ratio = &monitor.Ratio
				item.PreviousRatio = monitor.PreviousRatio
				costRatio, factor, conversionErr := channelMonitorCostRatioFromModel(monitor, monitor.Ratio)
				if conversionErr == nil {
					item.CostRatio = &costRatio
					item.ConversionFactor = &factor
					if monitor.PreviousRatio != nil {
						previousCostRatio, _, previousErr := channelMonitorCostRatioFromModel(monitor, *monitor.PreviousRatio)
						if previousErr == nil {
							item.PreviousCostRatio = &previousCostRatio
						}
					}
				}
				item.Remark = monitor.Remark
				item.UpdatedTime = monitor.UpdatedTime
				item.UpdatedBy = monitor.UpdatedBy
				item.UpdatedByUsername = monitor.UpdatedByUsername
			}
			item.Upstream = channelMonitorUpstreamFromModel(monitor)
		}
		if concurrencyStatus, exists := concurrencyByChannel[channel.Id]; exists {
			item.ConcurrencyLimit = concurrencyStatus.Limit
			item.RPMLimit = concurrencyStatus.RPMLimit
			item.ConcurrencyActive = concurrencyStatus.Active
			item.CurrentRPM = concurrencyStatus.CurrentRPM
		}
		items = append(items, item)
	}

	realtimeMetadata := channelMonitorRealtimeMetadata(0)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"channels":                      items,
			"generated_at":                  common.GetTimestamp(),
			"data_cutoff_at":                realtimeMetadata.DataCutoffAt,
			"processed_at":                  realtimeMetadata.ProcessedAt,
			"projection_started_at":         realtimeMetadata.ProjectionStartedAt,
			"event_watermark":               realtimeMetadata.EventWatermark,
			"queue_depth":                   realtimeMetadata.QueueDepth,
			"redis_status":                  realtimeMetadata.RedisStatus,
			"redis_available":               realtimeMetadata.RedisAvailable,
			"redis_consumer_running":        realtimeMetadata.RedisConsumerRunning,
			"pending_count":                 realtimeMetadata.PendingCount,
			"writer_queue_depth":            realtimeMetadata.WriterQueueDepth,
			"writer_queue_capacity":         realtimeMetadata.WriterQueueCapacity,
			"writer_queued_events":          realtimeMetadata.WriterQueuedEvents,
			"writer_dropped_events":         realtimeMetadata.WriterDroppedEvents,
			"writer_retry_events":           realtimeMetadata.WriterRetryEvents,
			"writer_oldest_queued_at":       realtimeMetadata.WriterOldestQueuedAt,
			"writer_queue_age_seconds":      realtimeMetadata.WriterQueueAgeSeconds,
			"oldest_pending_at":             realtimeMetadata.OldestPendingAt,
			"consumer_lag_seconds":          realtimeMetadata.ConsumerLagSeconds,
			"last_published_at":             realtimeMetadata.LastPublishedAt,
			"last_processed_at":             realtimeMetadata.LastProcessedAt,
			"retry_count":                   realtimeMetadata.RetryCount,
			"takeover_count":                realtimeMetadata.TakeoverCount,
			"quarantine_count":              realtimeMetadata.QuarantineCount,
			"last_quarantined_at":           realtimeMetadata.LastQuarantinedAt,
			"runtime_marker_failure_count":  realtimeMetadata.RuntimeMarkerFailureCount,
			"schedule_marker_failure_count": realtimeMetadata.ScheduleMarkerFailureCount,
			"cost_stream_pending_count":     realtimeMetadata.CostStreamPendingCount,
			"cost_stream_unread_count":      realtimeMetadata.CostStreamUnreadCount,
			"cost_outbox_pending_count":     realtimeMetadata.CostOutboxPendingCount,
			"cost_outbox_oldest_pending_at": realtimeMetadata.CostOutboxOldestPendingAt,
			"cost_outbox_retry_count":       realtimeMetadata.CostOutboxRetryCount,
			"cost_ledger_failed_count":      realtimeMetadata.CostLedgerFailedCount,
			"cost_publish_failed_count":     realtimeMetadata.CostPublishFailedCount,
			"cost_dead_letter_count":        realtimeMetadata.CostDeadLetterCount,
			"marker_release_failure_count":  realtimeMetadata.MarkerReleaseFailureCount,
			"marker_release_failure_active": realtimeMetadata.MarkerReleaseFailureActive,
			"stream_trim_failure_count":     realtimeMetadata.StreamTrimFailureCount,
			"stream_trim_failure_active":    realtimeMetadata.StreamTrimFailureActive,
			"redis_pool_stats":              realtimeMetadata.RedisPoolStats,
			"realtime_degraded":             realtimeMetadata.RealtimeDegraded,
			"channel_order":                 channelOrder,
			"group_ratios":                  groupRatios,
			"group_coefficients":            getChannelMonitorGroupCoefficients(),
			"settings":                      settings,
		},
	})
}

func SyncChannelMonitorGroupRatio(c *gin.Context) {
	var request groupRatioSyncRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的参数"})
		return
	}
	request.Group = strings.TrimSpace(request.Group)
	if request.Group == "" || utf8.RuneCountInString(request.Group) > 64 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "分组名称无效"})
		return
	}
	if !validateChannelMonitorRatio(request.Coefficient) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "系数必须在 0 到 1000000 之间"})
		return
	}

	channels, err := model.GetAllChannelsForMonitor()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	monitors, err := model.GetChannelRatioMonitors()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	monitorByChannel := make(map[int]model.ChannelRatioMonitor, len(monitors))
	for _, monitor := range monitors {
		monitorByChannel[monitor.ChannelId] = monitor
	}

	highestUpstreamRatio := -1.0
	highestCostRatio := -1.0
	highestConversionFactor := 1.0
	for _, channel := range channels {
		if channel.Status != common.ChannelStatusEnabled {
			continue
		}
		associated := false
		for _, group := range channel.GetGroups() {
			if group == request.Group {
				associated = true
				break
			}
		}
		if !associated {
			continue
		}
		monitor, exists := monitorByChannel[channel.Id]
		if !exists || monitor.UpdatedTime <= 0 {
			continue
		}
		costRatio, factor, conversionErr := channelMonitorCostRatioFromModel(monitor, monitor.Ratio)
		if conversionErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": fmt.Sprintf("渠道 %s（ID %d）倍率换算失败：%s", channel.Name, channel.Id, conversionErr.Error()),
			})
			return
		}
		if costRatio > highestCostRatio {
			highestCostRatio = costRatio
			highestUpstreamRatio = monitor.Ratio
			highestConversionFactor = factor
		}
	}
	if highestCostRatio < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "该分组没有已记录倍率的启用渠道"})
		return
	}
	targetRatio := highestCostRatio * *request.Coefficient
	if !validateChannelMonitorRatio(&targetRatio) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "成本倍率乘以系数后的结果超出范围"})
		return
	}

	groupsUpdated, err := model.MergeChannelMonitorGroupOptions(
		map[string]float64{request.Group: targetRatio},
		map[string]float64{request.Group: *request.Coefficient},
		false,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if groupsUpdated > 0 {
		_ = requestChannelSmartScheduleRun(c.Request.Context())
		recordManageAudit(c, "channel.monitor_group_ratio_sync", map[string]interface{}{
			"group": request.Group, "upstream_ratio": highestUpstreamRatio,
			"conversion_factor": highestConversionFactor, "cost_ratio": highestCostRatio,
			"coefficient": *request.Coefficient, "ratio": targetRatio,
		})
	}
	common.ApiSuccess(c, gin.H{
		"group": request.Group, "upstream_ratio": highestUpstreamRatio,
		"conversion_factor": highestConversionFactor, "cost_ratio": highestCostRatio,
		"coefficient": *request.Coefficient, "ratio": targetRatio,
	})
}

func UpdateChannelMonitorRatio(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelId <= 0 {
		common.ApiErrorMsg(c, "无效的渠道 ID")
		return
	}
	if _, err := model.GetChannelById(channelId, false); err != nil {
		common.ApiError(c, err)
		return
	}

	var request channelRatioUpdateRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的参数"})
		return
	}
	request.Remark = strings.TrimSpace(request.Remark)
	if !validateChannelMonitorRatio(request.Ratio) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "倍率必须在 0 到 1000000 之间"})
		return
	}
	if utf8.RuneCountInString(request.Remark) > 255 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "备注不能超过 255 个字符"})
		return
	}

	operatorId, operatorUsername := getChannelMonitorOperator(c)
	monitor, created, changed, err := model.UpdateChannelRatioMonitor(
		channelId,
		*request.Ratio,
		request.Remark,
		operatorId,
		operatorUsername,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	service.NotifyChannelModelDetectionOverviewChanged()
	service.InvalidateChannelDailyCostSnapshot(channelId)
	if err := applyChannelMonitorRatioPolicy(c.Request.Context(), monitor); err != nil {
		common.ApiError(c, fmt.Errorf("倍率已保存，但分组策略执行失败: %w", err))
		return
	}
	_ = requestChannelSmartScheduleRun(c.Request.Context())
	if created || changed {
		recordManageAudit(c, "channel.monitor_ratio_update", map[string]interface{}{
			"id": channelId, "ratio": *request.Ratio, "created": created,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"monitor": monitor,
			"created": created,
			"changed": changed,
		},
	})
}

func SaveChannelMonitorUpstreamConfig(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelId <= 0 {
		common.ApiErrorMsg(c, "无效的渠道 ID")
		return
	}
	channel, err := model.GetChannelById(channelId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	var request channelMonitorUpstreamRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的参数"})
		return
	}
	config, err := resolveChannelMonitorUpstreamRequest(channel, request, true)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	existingMonitor, findErr := model.GetChannelRatioMonitor(channelId)
	if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
		common.ApiError(c, findErr)
		return
	}
	hasExistingMonitor := findErr == nil
	if request.CostConversion == nil && hasExistingMonitor {
		config.CostConversion, err = service.ParseChannelMonitorCostConversion(existingMonitor.CostConversion)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
	}
	ratioSyncEnabled := true
	balanceSyncEnabled := true
	if hasExistingMonitor {
		ratioSyncEnabled = !existingMonitor.UpstreamRatioSyncDisabled
		balanceSyncEnabled = !existingMonitor.UpstreamBalanceSyncDisabled
	}
	if request.RatioSyncEnabled != nil {
		ratioSyncEnabled = *request.RatioSyncEnabled
	}
	if request.BalanceSyncEnabled != nil {
		balanceSyncEnabled = *request.BalanceSyncEnabled
	}

	singleChannelAction := strings.TrimSpace(request.SingleChannelAction)
	multipleChannelAction := strings.TrimSpace(request.MultipleChannelsAction)
	if singleChannelAction == "" || multipleChannelAction == "" {
		if hasExistingMonitor {
			if singleChannelAction == "" {
				singleChannelAction = normalizeChannelMonitorPolicyAction(existingMonitor.SingleChannelAction)
			}
			if multipleChannelAction == "" {
				multipleChannelAction = normalizeChannelMonitorPolicyAction(existingMonitor.MultipleChannelsAction)
			}
		}
	}
	if singleChannelAction == "" {
		singleChannelAction = channelMonitorPolicyActionNone
	}
	if multipleChannelAction == "" {
		multipleChannelAction = channelMonitorPolicyActionNone
	}
	if normalizeChannelMonitorPolicyAction(singleChannelAction) != singleChannelAction ||
		singleChannelAction == channelMonitorPolicyActionRemoveFromGroup {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "单渠道处理策略无效"})
		return
	}
	if normalizeChannelMonitorPolicyAction(multipleChannelAction) != multipleChannelAction {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "多渠道处理策略无效"})
		return
	}
	var existingBalanceWarningThreshold *float64
	if hasExistingMonitor {
		existingBalanceWarningThreshold = existingMonitor.BalanceWarningThreshold
	}
	balanceWarningThreshold, err := resolveChannelMonitorBalanceThreshold(
		request.BalanceWarningThreshold,
		existingBalanceWarningThreshold,
		"余额预警值无效",
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	var existingBalanceAutoDisableThreshold *float64
	if hasExistingMonitor {
		existingBalanceAutoDisableThreshold = existingMonitor.BalanceAutoDisableThreshold
	}
	balanceAutoDisableThreshold, err := resolveChannelMonitorBalanceThreshold(
		request.BalanceAutoDisableThreshold,
		existingBalanceAutoDisableThreshold,
		"余额自动禁用阈值无效",
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if hasExistingMonitor && existingMonitor.UpdatedTime > 0 {
		if _, _, err := service.CalculateChannelMonitorCostRatio(existingMonitor.Ratio, config.CostConversion); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
	}
	costConversion, err := service.MarshalChannelMonitorCostConversion(config.CostConversion)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	conversionFactor, err := service.ChannelMonitorCostConversionFactor(config.CostConversion)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	customConfig := ""
	if config.Type == service.CustomUpstreamType {
		if config.CustomConfig.Ratio.Source == service.ChannelMonitorCustomSourceFixed {
			if _, _, err := service.CalculateChannelMonitorCostRatio(*config.CustomConfig.Ratio.FixedValue, config.CostConversion); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
				return
			}
		}
		customConfig, err = service.MarshalChannelMonitorCustomUpstreamConfig(config.CustomConfig)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
	}
	if config.Type == service.Sub2APIUpstreamType && config.AuthType == service.Sub2APIAuthRefreshToken {
		config.AccessToken = service.CanonicalSub2APIRefreshToken(config)
	}
	if config.Type == service.Sub2APIUpstreamType && config.AuthType == service.Sub2APIAuthToken && config.RefreshToken != "" {
		config.RefreshToken = service.CanonicalSub2APIRefreshToken(service.ChannelMonitorUpstreamConfig{
			Type:         config.Type,
			BaseURL:      config.BaseURL,
			AuthType:     config.AuthType,
			AccessToken:  config.RefreshToken,
			CredentialID: config.CredentialID,
			Revision:     config.Revision,
			Proxy:        config.Proxy,
		})
	}
	if hasExistingMonitor && existingMonitor.UpstreamType == service.Sub2APIUpstreamType && existingMonitor.UpstreamAuthType == service.Sub2APIAuthRefreshToken &&
		(config.Type != service.Sub2APIUpstreamType || config.AuthType != service.Sub2APIAuthRefreshToken || config.BaseURL != existingMonitor.UpstreamBaseURL || config.AccessToken != existingMonitor.UpstreamAccessToken) {
		service.ForgetSub2APIRefreshTokenCache(service.ChannelMonitorUpstreamConfig{
			Type:         existingMonitor.UpstreamType,
			BaseURL:      existingMonitor.UpstreamBaseURL,
			AuthType:     existingMonitor.UpstreamAuthType,
			AccessToken:  existingMonitor.UpstreamAccessToken,
			CredentialID: existingMonitor.ChannelId,
			Revision:     existingMonitor.UpstreamRevision,
			Proxy:        channel.GetSetting().Proxy,
		})
	}

	monitor, err := model.SaveChannelRatioUpstreamConfig(
		channelId,
		config.Type,
		config.BaseURL,
		config.Group,
		config.AuthType,
		config.UserID,
		config.AccessToken,
		model.ChannelRatioUpstreamOptions{
			SingleChannelAction:         singleChannelAction,
			MultipleChannelsAction:      multipleChannelAction,
			BalanceWarningThreshold:     balanceWarningThreshold,
			BalanceAutoDisableThreshold: balanceAutoDisableThreshold,
			RatioSyncEnabled:            ratioSyncEnabled,
			BalanceSyncEnabled:          balanceSyncEnabled,
			CostConversion:              costConversion,
			CustomUpstreamConfig:        customConfig,
			UpstreamAccount:             config.Account,
			UpstreamPassword:            config.Password,
			UpstreamRefreshToken:        config.RefreshToken,
		},
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	service.NotifyChannelModelDetectionOverviewChanged()
	service.InvalidateChannelDailyCostSnapshot(channelId)
	balanceAutoDisabled := false
	if config.Type == service.CustomUpstreamType {
		operatorId, operatorUsername := getChannelMonitorOperator(c)
		if config.CustomConfig.Ratio.Source == service.ChannelMonitorCustomSourceFixed {
			var applied bool
			monitor, _, _, applied, err = model.UpdateChannelRatioMonitorFromUpstreamIfRevision(
				channelId,
				monitor.UpstreamRevision,
				*config.CustomConfig.Ratio.FixedValue,
				"已应用自定义上游固定倍率",
				operatorId,
				operatorUsername,
			)
			if err != nil {
				common.ApiError(c, fmt.Errorf("自定义上游配置已保存，但固定倍率写入失败: %w", err))
				return
			}
			if !applied {
				common.ApiError(c, model.ErrChannelRatioMonitorConfigChanged)
				return
			}
		}
		if config.CustomConfig.Balance.Source == service.ChannelMonitorCustomSourceFixed {
			baselineMonitor := monitor
			baselineMonitor.LastBalanceTime = 0
			baselineMonitor.LastBalanceCostNanoCNY = nil
			baselineMonitor.BalancePendingConsumption = 0
			balanceEvaluation, applied, recordErr := recordChannelMonitorBalanceUpdate(
				c.Request.Context(),
				baselineMonitor,
				config.CustomConfig.Balance.FixedValue,
				"",
			)
			if recordErr != nil {
				common.ApiError(c, fmt.Errorf("自定义上游配置已保存，但固定余额写入失败: %w", recordErr))
				return
			}
			if !applied {
				common.ApiError(c, model.ErrChannelRatioMonitorConfigChanged)
				return
			}
			monitor, err = model.GetChannelRatioMonitor(channelId)
			if err != nil {
				common.ApiError(c, err)
				return
			}
			effectiveBalance := *config.CustomConfig.Balance.FixedValue
			estimatedConsumption := 0.0
			if balanceEvaluation != nil {
				effectiveBalance = balanceEvaluation.EffectiveBalance
				estimatedConsumption = balanceEvaluation.EstimatedConsumption
			}
			balanceAutoDisabled, err = autoDisableChannelMonitorAtEffectiveBalance(
				monitor,
				channel,
				*config.CustomConfig.Balance.FixedValue,
				effectiveBalance,
				estimatedConsumption,
			)
			if err != nil {
				common.ApiError(c, fmt.Errorf("自定义上游配置已保存，但余额自动禁用失败: %w", err))
				return
			}
			if balanceAutoDisabled {
				model.InitChannelCache()
				service.ResetProxyClientCache()
			}
		}
	}
	if err := applyChannelMonitorRatioPolicy(c.Request.Context(), monitor); err != nil {
		common.ApiError(c, fmt.Errorf("上游配置已保存，但分组策略执行失败: %w", err))
		return
	}
	auditDetails := map[string]interface{}{
		"id": channelId, "upstream_type": config.Type, "upstream_type_label": channelMonitorUpstreamTypeLabel(config.Type), "group": config.Group, "auth_type": config.AuthType,
		"single_channel_action": singleChannelAction, "multiple_channels_action": multipleChannelAction,
		"balance_warning_threshold":      balanceWarningThreshold,
		"balance_auto_disable_threshold": balanceAutoDisableThreshold,
		"balance_auto_disabled":          balanceAutoDisabled,
		"ratio_sync_enabled":             ratioSyncEnabled, "balance_sync_enabled": balanceSyncEnabled,
		"cost_conversion":   channelMonitorCostConversionLabel(config.CostConversion),
		"conversion_factor": conversionFactor,
	}
	if config.Type == service.CustomUpstreamType {
		auditDetails["custom_ratio_source"] = config.CustomConfig.Ratio.Source
		auditDetails["custom_balance_source"] = config.CustomConfig.Balance.Source
	}
	recordManageAudit(c, "channel.monitor_upstream_config_update", auditDetails)
	_ = requestChannelSmartScheduleRun(c.Request.Context())
	common.ApiSuccess(c, channelMonitorUpstreamFromModel(monitor))
}

func ListChannelMonitorUpstreamGroups(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelId <= 0 {
		common.ApiErrorMsg(c, "无效的渠道 ID")
		return
	}
	channel, err := model.GetChannelById(channelId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	var request channelMonitorUpstreamRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的参数"})
		return
	}
	config, err := resolveChannelMonitorUpstreamRequest(channel, request, false)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if config.Type == service.Sub2APIUpstreamType {
		if config.AuthType != service.Sub2APIAuthToken && config.AuthType != service.Sub2APIAuthRefreshToken && config.AuthType != service.Sub2APIAuthAccount {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "Sub2API API Key 认证不支持获取或应用分组，请手动填写分组或切换为账号密码、Refresh Token 或手动 Token 认证",
			})
			return
		}
	}
	if config.Type == service.CustomUpstreamType {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "自定义上游不支持自动获取分组，请手动填写上游分组",
		})
		return
	}

	result, fetchErr := service.FetchChannelMonitorUpstreamGroups(c.Request.Context(), config, channel.GetKeys())
	if fetchErr != nil {
		common.ApiError(c, fetchErr)
		return
	}
	common.ApiSuccess(c, result)
}

func TestChannelMonitorUpstreamConfig(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelId <= 0 {
		common.ApiErrorMsg(c, "无效的渠道 ID")
		return
	}
	channel, err := model.GetChannelById(channelId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	var request channelMonitorUpstreamRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的参数"})
		return
	}
	config, err := resolveChannelMonitorUpstreamRequest(channel, request, true)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if config.Type != service.CustomUpstreamType && request.RatioSyncEnabled != nil && !*request.RatioSyncEnabled {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "上游倍率同步已关闭，无需测试获取"})
		return
	}
	if config.Type == service.Sub2APIUpstreamType {
		config.ChannelKeys = channel.GetKeys()
	}
	config.CustomDebug = config.Type == service.CustomUpstreamType
	result, err := service.FetchChannelMonitorUpstreamGroupRatio(c.Request.Context(), config)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

type channelMonitorFetchOutcome struct {
	Result            service.NewAPIGroupRatioResult
	Monitor           model.ChannelRatioMonitor
	Created           bool
	Changed           bool
	BalanceRecorded   bool
	BalanceEvaluation *channelMonitorBalanceEvaluation
}

type channelMonitorBalanceEvaluation struct {
	EffectiveBalance     float64
	EstimatedConsumption float64
	EstimateState        *model.ChannelRatioMonitorBalanceEstimateState
}

func evaluateChannelMonitorBalance(ctx context.Context, monitor model.ChannelRatioMonitor, balance float64) (channelMonitorBalanceEvaluation, error) {
	evaluation := channelMonitorBalanceEvaluation{EffectiveBalance: balance}
	if math.IsNaN(balance) || math.IsInf(balance, 0) {
		return evaluation, errors.New("上游余额不是有效数字")
	}
	if monitor.BalanceWarningThreshold == nil || monitor.BalanceAutoDisableThreshold == nil {
		return evaluation, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := service.FlushChannelDailyCostEvents(); err != nil {
		return evaluation, fmt.Errorf("刷新渠道消费记录失败: %w", err)
	}

	capturedAt := common.GetTimestamp()
	var previousBaseline *model.ChannelDailyCostBaseline
	if monitor.LastBalanceTime > 0 && monitor.LastBalanceCostNanoCNY != nil {
		previousBaseline = &model.ChannelDailyCostBaseline{
			Timestamp:   monitor.LastBalanceTime,
			CostNanoCNY: *monitor.LastBalanceCostNanoCNY,
		}
	}
	currentBaseline, deltaNanoCNY, err := model.GetChannelDailyCostDelta(
		ctx,
		monitor.ChannelId,
		capturedAt,
		previousBaseline,
	)
	if currentBaseline.Timestamp > 0 {
		evaluation.EstimateState = &model.ChannelRatioMonitorBalanceEstimateState{
			CostBaseline: currentBaseline,
		}
	}
	if err != nil {
		return evaluation, err
	}
	if previousBaseline == nil {
		return evaluation, nil
	}

	// Carry local cost forward while the provider keeps returning a stale
	// balance; a downward provider update settles the portion it has reflected.
	pendingConsumption := monitor.BalancePendingConsumption
	if math.IsNaN(pendingConsumption) || math.IsInf(pendingConsumption, 0) || pendingConsumption < 0 {
		return evaluation, errors.New("已保存的余额消费估算无效")
	}
	if deltaNanoCNY > 0 {
		conversion, parseErr := service.ParseChannelMonitorCostConversion(monitor.CostConversion)
		if parseErr != nil {
			return evaluation, parseErr
		}
		factor, factorErr := service.ChannelMonitorCostConversionFactor(conversion)
		if factorErr != nil {
			return evaluation, factorErr
		}
		deltaCNY := float64(deltaNanoCNY) / float64(model.ChannelDailyCostNanoPerCNY)
		deltaConsumption := deltaCNY / factor
		if math.IsNaN(deltaConsumption) || math.IsInf(deltaConsumption, 0) || deltaConsumption <= 0 {
			return evaluation, errors.New("本地消费增量估算结果无效")
		}
		pendingConsumption += deltaConsumption
		if math.IsNaN(pendingConsumption) || math.IsInf(pendingConsumption, 0) {
			return evaluation, errors.New("累计余额消费估算结果无效")
		}
	}
	if monitor.UpstreamBalance != nil && !math.IsNaN(*monitor.UpstreamBalance) && !math.IsInf(*monitor.UpstreamBalance, 0) && balance < *monitor.UpstreamBalance {
		pendingConsumption -= *monitor.UpstreamBalance - balance
		if pendingConsumption < 0 {
			pendingConsumption = 0
		}
	}
	evaluation.EstimateState.PendingConsumption = pendingConsumption
	if balance >= *monitor.BalanceWarningThreshold || pendingConsumption <= 0 {
		return evaluation, nil
	}
	evaluation.EstimatedConsumption = pendingConsumption
	evaluation.EffectiveBalance = balance - pendingConsumption
	if math.IsNaN(evaluation.EffectiveBalance) || math.IsInf(evaluation.EffectiveBalance, 0) {
		return channelMonitorBalanceEvaluation{
			EffectiveBalance: balance,
			EstimateState:    evaluation.EstimateState,
		}, errors.New("本地消费估算余额无效")
	}
	return evaluation, nil
}

func recordChannelMonitorBalanceUpdate(
	ctx context.Context,
	monitor model.ChannelRatioMonitor,
	balance *float64,
	fetchError string,
) (*channelMonitorBalanceEvaluation, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var evaluation *channelMonitorBalanceEvaluation
	var estimateState *model.ChannelRatioMonitorBalanceEstimateState
	if balance != nil && !math.IsNaN(*balance) && !math.IsInf(*balance, 0) {
		value, estimateErr := evaluateChannelMonitorBalance(ctx, monitor, *balance)
		evaluation = &value
		if estimateErr != nil {
			logger.LogWarn(ctx, fmt.Sprintf(
				"channel ratio monitor: channel_id=%d local balance consumption estimate failed: %v",
				monitor.ChannelId,
				estimateErr,
			))
		} else {
			estimateState = value.EstimateState
		}
	}
	applied, err := model.RecordChannelRatioMonitorBalanceWithEstimateIfRevision(
		monitor.ChannelId,
		monitor.UpstreamRevision,
		balance,
		fetchError,
		estimateState,
	)
	return evaluation, applied, err
}

func fetchAndRecordChannelMonitorUpstreamRatio(ctx context.Context, monitor model.ChannelRatioMonitor, channelKeys []string, proxyURL string, requestTimeout time.Duration, includeSeparateBalance bool, operatorId int, operatorUsername string) (outcome channelMonitorFetchOutcome, err error) {
	if monitor.UpstreamType != service.NewAPIUpstreamType && monitor.UpstreamType != service.Sub2APIUpstreamType && monitor.UpstreamType != service.CustomUpstreamType {
		return outcome, errors.New("请先保存上游配置")
	}
	if monitor.UpstreamRatioSyncDisabled {
		return outcome, errors.New("该渠道已关闭上游倍率同步")
	}
	defer func() {
		if err == nil {
			return
		}
		applied, statusErr := model.RecordChannelRatioMonitorFetchFailureIfRevision(
			monitor.ChannelId,
			monitor.UpstreamRevision,
			err.Error(),
		)
		if statusErr != nil {
			err = fmt.Errorf("%w（记录失败状态失败：%v）", err, statusErr)
			return
		}
		if !applied {
			err = model.ErrChannelRatioMonitorConfigChanged
		}
	}()
	if monitor.UpstreamType == service.Sub2APIUpstreamType {
		switch monitor.UpstreamAuthType {
		case service.Sub2APIAuthAPIKey:
			if len(channelKeys) == 0 {
				return outcome, errors.New("Sub2API API Key 认证需要当前渠道配置上游 API Key")
			}
		case service.Sub2APIAuthToken, service.Sub2APIAuthRefreshToken:
			if monitor.UpstreamAccessToken == "" {
				if monitor.UpstreamAuthType == service.Sub2APIAuthRefreshToken {
					return outcome, errors.New("请重新保存 Sub2API Refresh Token 配置")
				}
				return outcome, errors.New("请重新保存 Sub2API Token 配置")
			}
		case service.Sub2APIAuthAccount:
			if monitor.UpstreamAccount == "" || monitor.UpstreamPassword == "" {
				return outcome, errors.New("请重新保存 Sub2API 账号密码配置")
			}
		default:
			return outcome, errors.New("Sub2API 认证方式无效")
		}
	}
	costConversion, err := service.ParseChannelMonitorCostConversion(monitor.CostConversion)
	if err != nil {
		return outcome, err
	}
	customConfig := service.ChannelMonitorCustomUpstreamConfig{}
	fetchBalance := includeSeparateBalance
	if monitor.UpstreamType == service.CustomUpstreamType {
		customConfig, err = service.ParseChannelMonitorCustomUpstreamConfig(monitor.CustomUpstreamConfig)
		if err != nil {
			return outcome, err
		}
		fetchBalance = fetchBalance || customConfig.BalanceReuseRatioRequest
	}

	result, fetchErr := service.FetchChannelMonitorUpstreamGroupRatio(ctx, service.ChannelMonitorUpstreamConfig{
		Type:                         monitor.UpstreamType,
		BaseURL:                      monitor.UpstreamBaseURL,
		Group:                        monitor.UpstreamGroup,
		AuthType:                     monitor.UpstreamAuthType,
		UserID:                       monitor.UpstreamUserId,
		AccessToken:                  monitor.UpstreamAccessToken,
		RefreshToken:                 monitor.UpstreamRefreshToken,
		RefreshTokenStoredSeparately: monitor.UpstreamRefreshToken != "",
		CredentialID:                 monitor.ChannelId,
		Revision:                     monitor.UpstreamRevision,
		Account:                      monitor.UpstreamAccount,
		Password:                     monitor.UpstreamPassword,
		ChannelKeys:                  channelKeys,
		Proxy:                        proxyURL,
		RequestTimeout:               requestTimeout,
		SkipBalance:                  monitor.UpstreamBalanceSyncDisabled || !fetchBalance,
		CostConversion:               costConversion,
		CustomConfig:                 customConfig,
	})
	outcome.Result = result
	if result.Balance.Amount != nil || strings.TrimSpace(result.Balance.Error) != "" {
		balanceEvaluation, applied, balanceErr := recordChannelMonitorBalanceUpdate(
			ctx,
			monitor,
			result.Balance.Amount,
			result.Balance.Error,
		)
		if balanceErr != nil {
			return outcome, fmt.Errorf("记录上游余额失败: %w", balanceErr)
		}
		if !applied {
			return outcome, model.ErrChannelRatioMonitorConfigChanged
		}
		outcome.BalanceRecorded = result.Balance.Amount != nil
		outcome.BalanceEvaluation = balanceEvaluation
	}
	if fetchErr != nil {
		return outcome, fetchErr
	}

	upstreamName := channelMonitorUpstreamTypeLabel(monitor.UpstreamType)
	remark := fmt.Sprintf("从上游 %s 获取倍率", upstreamName)
	if strings.TrimSpace(monitor.UpstreamGroup) != "" {
		remark += fmt.Sprintf("（分组 %s）", monitor.UpstreamGroup)
	}
	updatedMonitor, created, changed, applied, err := model.UpdateChannelRatioMonitorFromUpstreamIfRevision(
		monitor.ChannelId,
		monitor.UpstreamRevision,
		result.Ratio,
		remark,
		operatorId,
		operatorUsername,
	)
	if err != nil {
		return outcome, err
	}
	if !applied {
		return outcome, model.ErrChannelRatioMonitorConfigChanged
	}
	service.InvalidateChannelDailyCostSnapshot(monitor.ChannelId)
	outcome.Monitor = updatedMonitor
	outcome.Created = created
	outcome.Changed = changed
	return outcome, nil
}

func channelMonitorSharesRatioBalanceRequest(monitor model.ChannelRatioMonitor) (bool, error) {
	if monitor.UpstreamType != service.CustomUpstreamType {
		return false, nil
	}
	config, err := service.ParseChannelMonitorCustomUpstreamConfig(monitor.CustomUpstreamConfig)
	if err != nil {
		return false, err
	}
	return config.BalanceReuseRatioRequest, nil
}

func fetchAndRecordChannelMonitorUpstreamBalance(ctx context.Context, monitor model.ChannelRatioMonitor, channelKeys []string, proxyURL string, requestTimeout time.Duration) (result service.ChannelMonitorUpstreamBalanceResult, evaluation *channelMonitorBalanceEvaluation, err error) {
	if monitor.UpstreamType != service.NewAPIUpstreamType && monitor.UpstreamType != service.Sub2APIUpstreamType && monitor.UpstreamType != service.CustomUpstreamType {
		return result, nil, errors.New("请先保存上游配置")
	}
	if monitor.UpstreamBalanceSyncDisabled {
		return result, nil, errors.New("该渠道已关闭上游余额同步")
	}

	customConfig := service.ChannelMonitorCustomUpstreamConfig{}
	if monitor.UpstreamType == service.CustomUpstreamType {
		customConfig, err = service.ParseChannelMonitorCustomUpstreamConfig(monitor.CustomUpstreamConfig)
		if err != nil {
			return result, nil, err
		}
	}
	result, fetchErr := service.FetchChannelMonitorUpstreamBalance(
		ctx,
		service.ChannelMonitorUpstreamConfig{
			Type:                         monitor.UpstreamType,
			BaseURL:                      monitor.UpstreamBaseURL,
			AuthType:                     monitor.UpstreamAuthType,
			UserID:                       monitor.UpstreamUserId,
			AccessToken:                  monitor.UpstreamAccessToken,
			RefreshToken:                 monitor.UpstreamRefreshToken,
			RefreshTokenStoredSeparately: monitor.UpstreamRefreshToken != "",
			CredentialID:                 monitor.ChannelId,
			Revision:                     monitor.UpstreamRevision,
			Account:                      monitor.UpstreamAccount,
			Password:                     monitor.UpstreamPassword,
			ChannelKeys:                  channelKeys,
			Proxy:                        proxyURL,
			RequestTimeout:               requestTimeout,
			CustomConfig:                 customConfig,
		},
	)
	if fetchErr == nil && result.Amount == nil {
		fetchErr = errors.New("上游未返回余额")
	}
	if fetchErr != nil {
		_, applied, recordErr := recordChannelMonitorBalanceUpdate(
			ctx,
			monitor,
			nil,
			fetchErr.Error(),
		)
		if recordErr != nil {
			fetchErr = fmt.Errorf("%w（记录余额失败状态失败：%v）", fetchErr, recordErr)
		} else if !applied {
			fetchErr = model.ErrChannelRatioMonitorConfigChanged
		}
		return result, nil, fetchErr
	}
	evaluation, applied, recordErr := recordChannelMonitorBalanceUpdate(
		ctx,
		monitor,
		result.Amount,
		"",
	)
	if recordErr != nil {
		return result, evaluation, recordErr
	}
	if !applied {
		return result, evaluation, model.ErrChannelRatioMonitorConfigChanged
	}
	return result, evaluation, nil
}

func autoDisableChannelMonitorForLowBalance(monitor model.ChannelRatioMonitor, channel *model.Channel, balance float64) (bool, error) {
	return autoDisableChannelMonitorForLowBalanceWithContext(context.Background(), monitor, channel, balance)
}

func autoDisableChannelMonitorForLowBalanceWithContext(ctx context.Context, monitor model.ChannelRatioMonitor, channel *model.Channel, balance float64) (bool, error) {
	if monitor.BalanceAutoDisableThreshold == nil || channel == nil ||
		channel.Id != monitor.ChannelId || channel.Status != common.ChannelStatusEnabled {
		return false, nil
	}
	evaluation, estimateErr := evaluateChannelMonitorBalance(ctx, monitor, balance)
	if estimateErr != nil {
		logger.LogWarn(ctx, fmt.Sprintf("channel ratio monitor: channel_id=%d local balance consumption estimate failed: %v", monitor.ChannelId, estimateErr))
	}
	return autoDisableChannelMonitorAtEffectiveBalance(
		monitor,
		channel,
		balance,
		evaluation.EffectiveBalance,
		evaluation.EstimatedConsumption,
	)
}

func autoDisableChannelMonitorAtEffectiveBalance(
	monitor model.ChannelRatioMonitor,
	channel *model.Channel,
	balance float64,
	effectiveBalance float64,
	estimatedConsumption float64,
) (bool, error) {
	if monitor.BalanceAutoDisableThreshold == nil || channel == nil ||
		channel.Id != monitor.ChannelId || channel.Status != common.ChannelStatusEnabled {
		return false, nil
	}
	threshold := *monitor.BalanceAutoDisableThreshold
	if math.IsNaN(balance) || math.IsInf(balance, 0) ||
		math.IsNaN(effectiveBalance) || math.IsInf(effectiveBalance, 0) ||
		math.IsNaN(threshold) || math.IsInf(threshold, 0) || threshold < 0 ||
		threshold > maxChannelMonitorBalanceThreshold ||
		effectiveBalance >= threshold {
		return false, nil
	}

	reason := channelMonitorBalancePolicyDisableReasonPrefix +
		strconv.FormatFloat(balance, 'f', -1, 64)
	if estimatedConsumption > 0 {
		reason += "（本地消费估算 " + strconv.FormatFloat(estimatedConsumption, 'f', -1, 64) +
			"，估算余额 " + strconv.FormatFloat(effectiveBalance, 'f', -1, 64) + "）"
	}
	reason +=
		channelMonitorBalancePolicyDisableThresholdMarker +
			strconv.FormatFloat(threshold, 'f', -1, 64)
	changed, revisionCurrent, _, updateErr := model.UpdateChannelMonitorStatusIfSnapshotRevision(
		channel.Id,
		monitor.UpstreamRevision,
		model.CaptureChannelMonitorStatus(channel),
		common.ChannelStatusAutoDisabled,
		reason,
	)
	if updateErr != nil {
		return false, fmt.Errorf("余额低于自动禁用阈值，但渠道禁用失败: %w", updateErr)
	}
	if !revisionCurrent {
		return false, nil
	}
	if changed {
		channel.Status = common.ChannelStatusAutoDisabled
		return true, nil
	}
	storedChannel, err := model.GetChannelById(channel.Id, true)
	if err != nil {
		return false, fmt.Errorf("余额低于自动禁用阈值，但读取渠道状态失败: %w", err)
	}
	if storedChannel.Status == common.ChannelStatusEnabled {
		return false, errors.New("余额低于自动禁用阈值，但渠道禁用失败")
	}
	channel.Status = storedChannel.Status
	return false, nil
}

func FetchChannelMonitorUpstreamRatio(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelId <= 0 {
		common.ApiErrorMsg(c, "无效的渠道 ID")
		return
	}
	channel, err := model.GetChannelById(channelId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	monitor, err := model.GetChannelRatioMonitor(channelId)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		common.ApiErrorMsg(c, "请先保存上游配置")
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if monitor.UpstreamRatioSyncDisabled {
		common.ApiErrorMsg(c, "该渠道已关闭上游倍率同步")
		return
	}
	operatorId, operatorUsername := getChannelMonitorOperator(c)
	outcome, err := fetchAndRecordChannelMonitorUpstreamRatio(c.Request.Context(), monitor, channel.GetKeys(), channel.GetSetting().Proxy, getChannelMonitorSettings().upstreamRequestTimeout(), false, operatorId, operatorUsername)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	service.NotifyChannelModelDetectionOverviewChanged()
	monitor = outcome.Monitor
	balanceAutoDisabled := false
	if outcome.BalanceRecorded && outcome.Result.Balance.Amount != nil {
		effectiveBalance := *outcome.Result.Balance.Amount
		estimatedConsumption := 0.0
		if outcome.BalanceEvaluation != nil {
			effectiveBalance = outcome.BalanceEvaluation.EffectiveBalance
			estimatedConsumption = outcome.BalanceEvaluation.EstimatedConsumption
		}
		balanceAutoDisabled, err = autoDisableChannelMonitorAtEffectiveBalance(
			monitor,
			channel,
			*outcome.Result.Balance.Amount,
			effectiveBalance,
			estimatedConsumption,
		)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if balanceAutoDisabled {
			model.InitChannelCache()
			service.ResetProxyClientCache()
		}
	}
	if err := applyChannelMonitorRatioPolicy(c.Request.Context(), monitor); err != nil {
		common.ApiError(c, fmt.Errorf("上游倍率已更新，但分组策略执行失败: %w", err))
		return
	}
	_ = requestChannelSmartScheduleRun(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"result":                outcome.Result,
			"monitor":               outcome.Monitor,
			"created":               outcome.Created,
			"changed":               outcome.Changed,
			"balance_auto_disabled": balanceAutoDisabled,
		},
	})
}

func FetchChannelMonitorUpstreamBalance(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelId <= 0 {
		common.ApiErrorMsg(c, "无效的渠道 ID")
		return
	}
	channel, err := model.GetChannelById(channelId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	monitor, err := model.GetChannelRatioMonitor(channelId)
	if errors.Is(err, gorm.ErrRecordNotFound) || monitor.UpstreamType == "" {
		common.ApiErrorMsg(c, "请先保存上游配置")
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if monitor.UpstreamBalanceSyncDisabled {
		common.ApiErrorMsg(c, "该渠道已关闭上游余额同步")
		return
	}

	sharedRequest, err := channelMonitorSharesRatioBalanceRequest(monitor)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	requestTimeout := getChannelMonitorSettings().upstreamRequestTimeout()
	result := service.ChannelMonitorUpstreamBalanceResult{}
	var balanceEvaluation *channelMonitorBalanceEvaluation
	ratioRefreshed := sharedRequest && !monitor.UpstreamRatioSyncDisabled
	if ratioRefreshed {
		operatorId, operatorUsername := getChannelMonitorOperator(c)
		outcome, fetchErr := fetchAndRecordChannelMonitorUpstreamRatio(
			c.Request.Context(),
			monitor,
			channel.GetKeys(),
			channel.GetSetting().Proxy,
			requestTimeout,
			false,
			operatorId,
			operatorUsername,
		)
		if fetchErr != nil {
			common.ApiError(c, fetchErr)
			return
		}
		result = outcome.Result.Balance
		balanceEvaluation = outcome.BalanceEvaluation
		monitor = outcome.Monitor
		service.NotifyChannelModelDetectionOverviewChanged()
	} else {
		result, balanceEvaluation, err = fetchAndRecordChannelMonitorUpstreamBalance(c.Request.Context(), monitor, channel.GetKeys(), channel.GetSetting().Proxy, requestTimeout)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	if result.Amount == nil {
		common.ApiErrorMsg(c, "上游未返回余额")
		return
	}
	effectiveBalance := *result.Amount
	estimatedConsumption := 0.0
	if balanceEvaluation != nil {
		effectiveBalance = balanceEvaluation.EffectiveBalance
		estimatedConsumption = balanceEvaluation.EstimatedConsumption
	}
	balanceAutoDisabled, err := autoDisableChannelMonitorAtEffectiveBalance(
		monitor,
		channel,
		*result.Amount,
		effectiveBalance,
		estimatedConsumption,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if balanceAutoDisabled {
		model.InitChannelCache()
		service.ResetProxyClientCache()
		service.NotifyChannelModelDetectionOverviewChanged()
	}
	if ratioRefreshed {
		if err := applyChannelMonitorRatioPolicy(c.Request.Context(), monitor); err != nil {
			common.ApiError(c, fmt.Errorf("上游倍率已更新，但分组策略执行失败: %w", err))
			return
		}
	}
	common.ApiSuccess(c, result)
}

func ApplyChannelMonitorUpstreamGroup(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelId <= 0 {
		common.ApiErrorMsg(c, "无效的渠道 ID")
		return
	}
	channel, err := model.GetChannelById(channelId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	monitor, err := model.GetChannelRatioMonitor(channelId)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		common.ApiErrorMsg(c, "请先保存上游配置")
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	costConversion, err := service.ParseChannelMonitorCostConversion(monitor.CostConversion)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	applyResult, applyErr := service.ApplyChannelMonitorUpstreamGroup(
		c.Request.Context(),
		service.ChannelMonitorUpstreamConfig{
			Type:                         monitor.UpstreamType,
			BaseURL:                      monitor.UpstreamBaseURL,
			Group:                        monitor.UpstreamGroup,
			AuthType:                     monitor.UpstreamAuthType,
			UserID:                       monitor.UpstreamUserId,
			AccessToken:                  monitor.UpstreamAccessToken,
			RefreshToken:                 monitor.UpstreamRefreshToken,
			RefreshTokenStoredSeparately: monitor.UpstreamRefreshToken != "",
			CredentialID:                 monitor.ChannelId,
			Revision:                     monitor.UpstreamRevision,
			Account:                      monitor.UpstreamAccount,
			Password:                     monitor.UpstreamPassword,
			Proxy:                        channel.GetSetting().Proxy,
			CostConversion:               costConversion,
		},
		channel.GetKeys(),
	)
	if applyErr != nil {
		if applyResult.KeysUpdated > 0 {
			applyErr = fmt.Errorf("已切换 %d 个上游令牌，但后续操作失败: %w", applyResult.KeysUpdated, applyErr)
		}
		applied, statusErr := model.RecordChannelRatioMonitorFetchFailureIfRevision(
			channelId,
			monitor.UpstreamRevision,
			applyErr.Error(),
		)
		if statusErr != nil {
			applyErr = fmt.Errorf("%w（记录失败状态失败：%v）", applyErr, statusErr)
		} else if !applied {
			applyErr = fmt.Errorf("%w（上游配置已变化，未将旧请求的失败状态写入新配置）", applyErr)
		}
		common.ApiError(c, applyErr)
		return
	}

	upstreamName := "New API"
	if monitor.UpstreamType == service.Sub2APIUpstreamType {
		upstreamName = "Sub2API"
	}
	operatorId, operatorUsername := getChannelMonitorOperator(c)
	remark := fmt.Sprintf(
		"已将 %d 个上游 %s 令牌切换到分组 %s",
		applyResult.KeysUpdated,
		upstreamName,
		monitor.UpstreamGroup,
	)
	updatedMonitor, created, changed, applied, err := model.UpdateChannelRatioMonitorFromUpstreamIfRevision(
		channelId,
		monitor.UpstreamRevision,
		applyResult.Result.Ratio,
		remark,
		operatorId,
		operatorUsername,
	)
	if err != nil {
		common.ApiError(c, fmt.Errorf("上游令牌已切换，但记录本地倍率失败: %w", err))
		return
	}
	if !applied {
		common.ApiError(c, errors.New("上游令牌已切换，但本地上游配置已变更，未覆盖新的倍率配置"))
		return
	}
	service.NotifyChannelModelDetectionOverviewChanged()
	service.InvalidateChannelDailyCostSnapshot(channelId)
	if err := applyChannelMonitorRatioPolicy(c.Request.Context(), updatedMonitor); err != nil {
		common.ApiError(c, fmt.Errorf("上游令牌已切换且倍率已记录，但分组策略执行失败: %w", err))
		return
	}
	recordManageAudit(c, "channel.monitor_upstream_group_apply", map[string]interface{}{
		"id":                channelId,
		"upstream_type":     monitor.UpstreamType,
		"group":             monitor.UpstreamGroup,
		"keys_updated":      applyResult.KeysUpdated,
		"ratio":             applyResult.Result.Ratio,
		"cost_ratio":        applyResult.Result.CostRatio,
		"conversion_factor": applyResult.Result.ConversionFactor,
		"changed":           changed,
	})
	_ = requestChannelSmartScheduleRun(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"result":       applyResult.Result,
			"keys_updated": applyResult.KeysUpdated,
			"monitor":      updatedMonitor,
			"created":      created,
			"changed":      changed,
		},
	})
}

func GetChannelMonitorHistory(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelId <= 0 {
		common.ApiErrorMsg(c, "无效的渠道 ID")
		return
	}
	if _, err := model.GetChannelById(channelId, false); err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo := common.GetPageQuery(c)
	history, total, err := model.GetChannelRatioHistory(channelId, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(history)
	common.ApiSuccess(c, pageInfo)
}

func UpdateChannelMonitorGroupRatio(c *gin.Context) {
	var request groupRatioUpdateRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的参数"})
		return
	}
	request.Group = strings.TrimSpace(request.Group)
	if request.Group == "" || utf8.RuneCountInString(request.Group) > 64 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "分组名称无效"})
		return
	}
	if !validateChannelMonitorRatio(request.Ratio) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "倍率必须在 0 到 1000000 之间"})
		return
	}

	groupsUpdated, err := model.MergeChannelMonitorGroupOptions(
		map[string]float64{request.Group: *request.Ratio},
		nil,
		false,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if groupsUpdated > 0 {
		_ = requestChannelSmartScheduleRun(c.Request.Context())
		recordManageAudit(c, "channel.monitor_group_ratio_update", map[string]interface{}{
			"group": request.Group,
			"ratio": *request.Ratio,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"group": request.Group,
			"ratio": *request.Ratio,
		},
	})
}
