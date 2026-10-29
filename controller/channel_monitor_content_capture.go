package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetChannelMonitorContentCapture(c *gin.Context) {
	config, err := service.GetContentCaptureConfig(c.Request.Context())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items, err := service.ListContentCaptureSummaries(c.Request.Context())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"config": config, "items": items})
}

func SaveChannelMonitorContentCapture(c *gin.Context) {
	var request struct {
		Enabled   bool `json:"enabled"`
		Limit     int  `json:"limit"`
		MaxBodyKB int  `json:"max_body_kb"`
		Hours     int  `json:"hours"`
	}
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 4<<10), &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "抓取配置无效"})
		return
	}
	params := map[string]any{"enabled": request.Enabled}
	if !request.Enabled {
		if err := service.DisableContentCapture(c.Request.Context()); err != nil {
			common.ApiError(c, err)
			return
		}
		model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), "关闭请求内容抓取并清空记录", c.ClientIP(), "content_capture_settings", params, auditOperatorInfo(c), nil, c)
		common.ApiSuccess(c, gin.H{"config": service.ContentCaptureConfig{}, "items": []service.ContentCaptureSummary{}})
		return
	}
	if request.Limit < 1 || request.Limit > service.ContentCaptureMaxLimit ||
		request.MaxBodyKB < 1 || request.MaxBodyKB > service.ContentCaptureMaxBodyKB ||
		request.Hours < 1 || request.Hours > service.ContentCaptureMaxHours {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "记录条数须为 1–1000，单侧正文上限须为 1–256KB，自动关闭时间须为 1–72 小时"})
		return
	}
	config, err := service.EnableContentCapture(c.Request.Context(), request.Limit, request.MaxBodyKB, request.Hours)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	params["limit"], params["max_body_kb"], params["hours"] = request.Limit, request.MaxBodyKB, request.Hours
	model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), "开启请求内容抓取", c.ClientIP(), "content_capture_settings", params, auditOperatorInfo(c), nil, c)
	items, err := service.ListContentCaptureSummaries(c.Request.Context())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"config": config, "items": items})
}

func GetChannelMonitorContentCaptureRecord(c *gin.Context) {
	record, err := service.GetContentCaptureRecord(c.Request.Context(), c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if record == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "记录已被覆盖或清空"})
		return
	}
	common.ApiSuccess(c, record)
}
