package controller

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type upstreamAccountRequest struct {
	Proxy                  *string       `json:"proxy,omitempty"`
	BalanceKey             *string       `json:"balance_key,omitempty"`
	RefreshIntervalMinutes *int          `json:"refresh_interval_minutes"`
	ID                     int           `json:"id"`
	Revision               int64         `json:"revision"`
	Name                   string        `json:"name"`
	SourceChannelID        int           `json:"source_channel_id"`
	ChannelIDs             []int         `json:"channel_ids"`
	ChannelRevisions       map[int]int64 `json:"channel_revisions"`
}

func channelMonitorConfigurationForRequest(channelID int, accountRevision *int64) (model.ChannelRatioMonitor, error) {
	monitor, err := model.GetChannelRatioMonitor(channelID)
	if err != nil || accountRevision == nil {
		return monitor, err
	}
	if monitor.UpstreamAccountID <= 0 {
		return monitor, errors.New("渠道尚未关联账户")
	}
	account, err := model.GetChannelMonitorUpstreamAccount(context.Background(), monitor.UpstreamAccountID)
	if err != nil {
		return monitor, err
	}
	if account.Revision != *accountRevision {
		return monitor, model.ErrUpstreamAccountChanged
	}
	settings, err := account.MonitorSettings()
	if err != nil {
		return monitor, err
	}
	projection := model.ChannelRatioMonitor{ChannelId: monitor.ChannelId, UpstreamRevision: monitor.UpstreamRevision, UpstreamAccountID: account.ID, UpstreamAccountRevision: account.Revision}
	err = settings.Apply(&projection)
	return projection, err
}

type upstreamAccountView struct {
	HasBalanceKey          bool                            `json:"has_balance_key"`
	RefreshIntervalMinutes int                             `json:"refresh_interval_minutes"`
	ID                     int                             `json:"id"`
	Name                   string                          `json:"name"`
	Revision               int64                           `json:"revision"`
	ChannelIDs             []int                           `json:"channel_ids"`
	ChannelRevisions       map[int]int64                   `json:"channel_revisions"`
	Upstream               *channelMonitorUpstreamConfig   `json:"upstream"`
	Balance                *float64                        `json:"balance"`
	Estimate               *service.ChannelBalanceEstimate `json:"balance_estimate,omitempty"`
	LastBalanceTime        int64                           `json:"last_balance_time"`
	LastBalanceError       string                          `json:"last_balance_error"`
	Proxy                  string                          `json:"proxy"`
}

func channelMonitorAccountView(ctx context.Context, account model.ChannelMonitorUpstreamAccount) (upstreamAccountView, error) {
	view := upstreamAccountView{ID: account.ID, Name: account.Name, Revision: account.Revision,
		HasBalanceKey:          account.BalanceKey != "",
		RefreshIntervalMinutes: account.RefreshIntervalMinutes,
		Balance:                account.Balance, LastBalanceTime: account.LastBalanceTime, LastBalanceError: account.LastBalanceError,
		Proxy: account.Proxy, ChannelIDs: []int{}, ChannelRevisions: map[int]int64{}}
	settings, err := account.MonitorSettings()
	if err != nil {
		return view, err
	}
	monitor := model.ChannelRatioMonitor{UpstreamAccountID: account.ID, UpstreamAccountRevision: account.Revision}
	if err := settings.Apply(&monitor); err != nil {
		return view, err
	}
	view.Upstream = channelMonitorUpstreamFromModel(monitor)
	members, err := model.GetUpstreamAccountMonitors(ctx, account.ID)
	if err != nil {
		return view, err
	}
	for _, member := range members {
		view.ChannelIDs = append(view.ChannelIDs, member.ChannelId)
		view.ChannelRevisions[member.ChannelId] = member.UpstreamRevision
	}
	if len(members) > 0 {
		estimates := service.GetChannelBalanceEstimates(ctx, members[:1])
		if estimate, ok := estimates[members[0].ChannelId]; ok {
			view.Estimate = &estimate
		}
	}
	return view, nil
}

func ListChannelMonitorUpstreamAccounts(c *gin.Context) {
	accounts, err := model.ListChannelMonitorUpstreamAccounts(c.Request.Context())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	views := make([]upstreamAccountView, 0, len(accounts))
	for _, account := range accounts {
		view, err := channelMonitorAccountView(c.Request.Context(), account)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		views = append(views, view)
	}
	common.ApiSuccess(c, views)
}

func prepareChannelMonitorAccount(ctx context.Context, input upstreamAccountRequest) (account model.ChannelMonitorUpstreamAccount, expected *model.ChannelMonitorUpstreamAccount, differences []gin.H, err error) {
	differences = []gin.H{}
	if input.ID > 0 {
		account, err = model.GetChannelMonitorUpstreamAccount(ctx, input.ID)
		if err != nil {
			return
		}
		if account.Revision != input.Revision {
			err = model.ErrUpstreamAccountChanged
			return
		}
		original := account
		expected = &original
	} else {
		if !slices.Contains(input.ChannelIDs, input.SourceChannelID) {
			err = errors.New("创建账户时请保留配置来源渠道的关联")
			return
		}
		var source model.ChannelRatioMonitor
		source, err = model.GetChannelRatioMonitorWithContext(ctx, input.SourceChannelID)
		if err != nil {
			return
		}
		if source.UpstreamAccountID > 0 {
			err = errors.New("来源渠道已关联账户，请直接选择该账户")
			return
		}
		settings := model.ChannelMonitorAccountSettingsFromMonitor(source)
		account.SourceChannelID = source.ChannelId
		account.ExpectedSourceSettings, err = settings.SharedJSON()
		if err != nil {
			return
		}
		if settings.UpstreamType == service.CustomUpstreamType {
			var custom service.ChannelMonitorCustomUpstreamConfig
			custom, err = service.ParseChannelMonitorCustomUpstreamConfig(settings.CustomUpstreamConfig)
			if err != nil {
				return
			}
			if custom.BalanceReuseRatioRequest {
				custom.Balance.Request = custom.Ratio.Request
				custom.BalanceReuseRatioRequest = false
			}
			custom.Actions = nil
			settings.CustomUpstreamConfig, err = service.MarshalChannelMonitorCustomUpstreamConfig(custom)
			if err != nil {
				return
			}
		}
		var raw []byte
		raw, err = common.Marshal(settings)
		if err != nil {
			return
		}
		account.Settings = string(raw)
		var channel *model.Channel
		channel, err = model.GetChannelById(source.ChannelId, true)
		if err != nil {
			return
		}
		account.Proxy = channel.GetSetting().Proxy
		if source.UpstreamAuthType == service.Sub2APIAuthAPIKey {
			keys := channel.GetKeys()
			if len(keys) != 1 {
				err = errors.New("共享余额账户需要指定单一余额查询 API Key，请使用单 Key 渠道作为来源")
				return
			}
			account.BalanceKey = keys[0]
		}
	}
	account.Name = input.Name
	if input.Proxy != nil {
		account.Proxy = *input.Proxy
	}
	if _, _, parseErr := common.ParseProxyURLRuntime(account.Proxy); parseErr != nil || len(account.Proxy) > 2048 {
		err = errors.New("账户代理地址无效")
		return
	}
	if input.BalanceKey != nil && *input.BalanceKey != "" {
		account.BalanceKey = *input.BalanceKey
	}
	if len(account.BalanceKey) > 4096 {
		err = errors.New("余额查询 API Key 过长")
		return
	}
	if input.RefreshIntervalMinutes != nil {
		account.RefreshIntervalMinutes = *input.RefreshIntervalMinutes
	} else if input.ID == 0 {
		account.RefreshIntervalMinutes = max(1, getChannelMonitorSettings().AutoUpdateIntervalMinutes)
	}
	settings, parseErr := account.MonitorSettings()
	if parseErr != nil {
		err = parseErr
		return
	}
	for _, id := range input.ChannelIDs {
		var monitor model.ChannelRatioMonitor
		monitor, err = model.GetChannelRatioMonitorWithContext(ctx, id)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if _, err = model.GetChannelById(id, false); err != nil {
				return
			}
			monitor.ChannelId = id
		} else if err != nil {
			return
		}
		if !monitor.UsesIndependentUpstreamConfig() && monitor.UpstreamType != "" && monitor.UpstreamType != settings.UpstreamType {
			err = errors.New("关联渠道须使用相同上游类型")
			return
		}
		if monitor.UpstreamAccountID > 0 && monitor.UpstreamAccountID != input.ID {
			err = errors.New("渠道已属于其他账户，请先解除关联")
			return
		}
		before := model.ChannelMonitorAccountSettingsFromMonitor(monitor)
		if err = settings.Apply(&monitor); err != nil {
			return
		}
		if monitor.UpstreamType == service.CustomUpstreamType {
			var custom service.ChannelMonitorCustomUpstreamConfig
			custom, err = service.ParseChannelMonitorCustomUpstreamConfig(monitor.CustomUpstreamConfig)
			if err != nil {
				return
			}
			if err = service.ValidateChannelMonitorVariableGroup(ctx, &custom); err != nil {
				return
			}
		}
		conversion, conversionErr := service.ParseChannelMonitorCostConversion(monitor.CostConversion)
		if conversionErr != nil {
			err = conversionErr
			return
		}
		if _, _, err = service.CalculateChannelMonitorCostRatio(monitor.Ratio, conversion); err != nil {
			return
		}
		fields := []string{}
		after := model.ChannelMonitorAccountSettingsFromMonitor(monitor)
		if before.UpstreamBaseURL != after.UpstreamBaseURL {
			fields = append(fields, "监控地址")
		}
		if before.UpstreamAuthType != after.UpstreamAuthType || before.UpstreamUserId != after.UpstreamUserId || before.UpstreamAccount != after.UpstreamAccount || before.UpstreamPassword != after.UpstreamPassword || before.UpstreamAccessToken != after.UpstreamAccessToken || before.UpstreamRefreshToken != after.UpstreamRefreshToken {
			fields = append(fields, "监控认证")
		}
		if before.CostConversion != after.CostConversion {
			fields = append(fields, "充值／订阅换算")
		}
		if before.CustomUpstreamConfig != monitor.CustomUpstreamConfig {
			fields = append(fields, "余额查询及共享变量")
		}
		if !reflect.DeepEqual(before.BalanceWarningThreshold, after.BalanceWarningThreshold) || !reflect.DeepEqual(before.BalanceAutoDisableThreshold, after.BalanceAutoDisableThreshold) {
			fields = append(fields, "余额保护阈值")
		}
		differences = append(differences, gin.H{"channel_id": id, "revision": monitor.UpstreamRevision, "fields": fields})
	}
	return
}

func PreviewChannelMonitorUpstreamAccount(c *gin.Context) {
	var input upstreamAccountRequest
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10), &input); err != nil {
		common.ApiErrorMsg(c, "账户参数无效")
		return
	}
	if err := service.MigrateUpstreamAutomations(c.Request.Context(), getChannelMonitorSettings().AutoUpdateIntervalMinutes); err != nil {
		common.ApiError(c, err)
		return
	}
	_, _, differences, err := prepareChannelMonitorAccount(c.Request.Context(), input)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, differences)
}

func SaveChannelMonitorUpstreamAccount(c *gin.Context) {
	var input upstreamAccountRequest
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10), &input); err != nil {
		common.ApiErrorMsg(c, "账户参数无效")
		return
	}
	if err := service.MigrateUpstreamAutomations(c.Request.Context(), getChannelMonitorSettings().AutoUpdateIntervalMinutes); err != nil {
		common.ApiError(c, err)
		return
	}
	account, expected, _, err := prepareChannelMonitorAccount(c.Request.Context(), input)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if input.ChannelIDs == nil {
		common.ApiErrorMsg(c, "请提供关联渠道列表")
		return
	}
	members, err := model.SaveChannelMonitorUpstreamAccount(c.Request.Context(), &account, expected, input.ChannelIDs, input.ChannelRevisions)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	for _, member := range members {
		service.InvalidateChannelDailyCostSnapshot(member.ChannelId)
		if common.RedisEnabled {
			_ = service.ConfigureChannelBalanceEstimate(c.Request.Context(), member)
		}
	}
	service.NotifyChannelModelDetectionOverviewChanged()
	recordManageAudit(c, "channel.upstream_account_save", map[string]any{"account_id": account.ID, "channel_ids": input.ChannelIDs})
	view, err := channelMonitorAccountView(c.Request.Context(), account)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

func DeleteChannelMonitorUpstreamAccount(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	revision, _ := strconv.ParseInt(c.Query("revision"), 10, 64)
	if id <= 0 || revision <= 0 {
		common.ApiErrorMsg(c, "账户参数无效")
		return
	}
	if err := model.DeleteChannelMonitorUpstreamAccount(c.Request.Context(), id, revision); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "channel.upstream_account_delete", map[string]any{"account_id": id})
	common.ApiSuccess(c, nil)
}

func RefreshChannelMonitorUpstreamAccount(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		common.ApiErrorMsg(c, "账户参数无效")
		return
	}
	members, err := model.GetUpstreamAccountMonitors(c.Request.Context(), id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if len(members) == 0 {
		common.ApiErrorMsg(c, "请先关联渠道")
		return
	}
	_, err = fetchAndRecordUpstreamAccountBalance(c.Request.Context(), members[0], getChannelMonitorSettings().upstreamRequestTimeout())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
