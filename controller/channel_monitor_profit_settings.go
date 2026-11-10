package controller

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const channelMonitorProfitSettingsOption = "ChannelMonitorProfitSettings"

type channelMonitorProfitSettings struct {
	CostOnlyAPIKeyIDs []int `json:"cost_only_api_key_ids"`
}

func (settings *channelMonitorProfitSettings) normalize() error {
	if settings.CostOnlyAPIKeyIDs == nil || len(settings.CostOnlyAPIKeyIDs) > 1000 {
		return errors.New("请提供自用 API Key ID 列表，最多 1000 个；清空请传空列表")
	}
	for _, id := range settings.CostOnlyAPIKeyIDs {
		if id <= 0 || int64(id) > 2147483647 {
			return errors.New("API Key ID 必须为 1–2147483647 的整数")
		}
	}
	slices.Sort(settings.CostOnlyAPIKeyIDs)
	settings.CostOnlyAPIKeyIDs = slices.Compact(settings.CostOnlyAPIKeyIDs)
	return nil
}

// Read from the reporting transaction so every total and row uses the same
// policy, including across nodes. Original income and wallet records stay intact.
func readChannelMonitorProfitSettings(ctx context.Context, db *gorm.DB) (channelMonitorProfitSettings, error) {
	settings := channelMonitorProfitSettings{CostOnlyAPIKeyIDs: []int{}}
	var option model.Option
	if err := db.WithContext(ctx).Where(&model.Option{Key: channelMonitorProfitSettingsOption}).Find(&option).Error; err != nil {
		return settings, err
	}
	if option.Key == "" {
		return settings, nil
	}
	if err := common.UnmarshalJsonStr(option.Value, &settings); err != nil {
		return settings, errors.New("自用 API Key 配置无法读取")
	}
	err := settings.normalize()
	return settings, err
}

func GetChannelMonitorProfitSettings(c *gin.Context) {
	settings, err := readChannelMonitorProfitSettings(c.Request.Context(), model.DB)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, settings)
}

func SaveChannelMonitorProfitSettings(c *gin.Context) {
	var settings channelMonitorProfitSettings
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10), &settings); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "自用 API Key 配置无效"})
		return
	}
	if err := settings.normalize(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	value, err := common.Marshal(settings)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpdateOptionsBulk(map[string]string{channelMonitorProfitSettingsOption: string(value)}); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), "更新自用 API Key 利润统计配置", c.ClientIP(), "channel_monitor_profit_settings",
		map[string]any{"cost_only_api_key_ids": settings.CostOnlyAPIKeyIDs}, auditOperatorInfo(c), nil, c)
	common.ApiSuccess(c, settings)
}
