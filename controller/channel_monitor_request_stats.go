package controller

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetChannelMonitorRequestStats(c *gin.Context) {
	var query service.RequestStatsQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "统计查询参数无效"})
		return
	}
	if _, err := query.Validate(time.Now()); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	_, userProvided := c.GetQuery("user_id")
	_, keyProvided := c.GetQuery("api_key_id")
	if query.Level != "user" && !userProvided || query.Level == "channel" && !keyProvided {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请指定统计父级"})
		return
	}
	result, err := service.QueryChannelMonitorRequestStats(c.Request.Context(), query)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func GetChannelMonitorRequestStatsSettings(c *gin.Context) {
	config, err := model.GetChannelMonitorRequestStatsConfig(c.Request.Context())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, config)
}

func SaveChannelMonitorRequestStatsSettings(c *gin.Context) {
	var request struct {
		Revision                  int64  `json:"revision"`
		LowInputThresholdK        *int   `json:"low_input_threshold_k"`
		Enabled                   *bool  `json:"enabled"`
		DisableRatePercent        *int   `json:"disable_rate_percent"`
		MinimumKnownInputRequests *int64 `json:"minimum_known_input_requests"`
	}
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10), &request); err != nil || request.Revision < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "统计配置无效"})
		return
	}
	values := map[string]any{}
	if c.FullPath() == "/api/channel_monitor/request-stats/settings" {
		if request.LowInputThresholdK == nil || *request.LowInputThresholdK < 1 || *request.LowInputThresholdK > 1000 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "输入阈值须为 1–1000K 的整数"})
			return
		}
		values["low_input_threshold_k"] = *request.LowInputThresholdK
	} else {
		if request.Enabled == nil || request.DisableRatePercent == nil || request.MinimumKnownInputRequests == nil || *request.DisableRatePercent < 0 || *request.DisableRatePercent > 100 || *request.MinimumKnownInputRequests < 1 || *request.MinimumKnownInputRequests > 1000000000 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "禁用占比须为 0–100%，最少样本数须为 1–1000000000 的整数"})
			return
		}
		values["enabled"] = *request.Enabled
		values["disable_rate_percent"] = *request.DisableRatePercent
		values["minimum_known_input_requests"] = *request.MinimumKnownInputRequests
	}
	config, err := model.SaveChannelMonitorRequestStatsConfig(c.Request.Context(), request.Revision, values)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, model.ErrRequestStatsConfigConflict) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"success": false, "message": err.Error()})
		return
	}
	model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), "更新用户请求统计配置", c.ClientIP(), "request_stats_settings", map[string]any{"revision": config.Revision, "values": values}, auditOperatorInfo(c), nil, c)
	common.ApiSuccess(c, config)
}

func ListChannelMonitorRequestStatsDisableHistory(c *gin.Context) {
	filters := []int{-1, -1, 1, 20}
	for index, name := range []string{"user_id", "api_key_id", "page", "page_size"} {
		value, provided := c.GetQuery(name)
		if !provided {
			continue
		}
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 || index >= 2 && parsed < 1 || index == 2 && parsed > 1000000 || index == 3 && parsed > 100 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "历史查询参数无效"})
			return
		}
		filters[index] = parsed
	}
	rows, total, err := model.ListRequestStatsDisableHistory(c.Request.Context(), filters[0], filters[1], (filters[2]-1)*filters[3], filters[3])
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": rows, "total": total, "page": filters[2], "page_size": filters[3]})
}
