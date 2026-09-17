package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetTokenProtectionSettings(c *gin.Context) {
	settings, err := service.TokenAutoDisableSettingsSnapshot()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, settings)
}

func SaveTokenProtectionSettings(c *gin.Context) {
	var request service.TokenAutoDisableSettings
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 256<<10), &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "API Key 自动禁用配置无效或过大"})
		return
	}
	settings, err := service.SaveTokenAutoDisableSettings(c.Request.Context(), request)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, model.ErrTokenAutoDisableConflict) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"success": false, "message": err.Error()})
		return
	}
	model.RecordOperationAuditLog(c.GetInt("id"), "更新 API Key 自动禁用规则", c.ClientIP(), "token_auto_disable_settings", nil,
		map[string]interface{}{"revision": settings.Revision, "enabled": settings.Enabled, "rule_count": len(settings.Rules)}, nil)
	common.ApiSuccess(c, settings)
}

func ListTokenProtectionRecords(c *gin.Context) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 || page > 1000000 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "页码无效"})
		return
	}
	records, total, err := model.ListTokenAutoDisables(c.Request.Context(), (page-1)*20, 20)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if records == nil {
		records = []model.TokenAutoDisableRecord{}
	}
	common.ApiSuccess(c, gin.H{"records": records, "total": total, "pending": service.PendingTokenAutoDisables()})
}

func ReleaseTokenProtection(c *gin.Context) {
	if err := service.ReleaseTokenProtection(c.Request.Context(), c.Param("id"), c.GetInt("id")); err != nil {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error()})
		return
	}
	model.RecordOperationAuditLog(c.GetInt("id"), "解除 API Key 自动禁用", c.ClientIP(), "token_auto_disable_release",
		map[string]interface{}{"record_id": c.Param("id")}, nil, nil)
	common.ApiSuccess(c, nil)
}
