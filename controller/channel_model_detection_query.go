package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GetChannelModelDetectionOverview(c *gin.Context) {
	response, err := service.GetCurrentChannelModelDetectionOverview(c.Request.Context())
	if err != nil {
		if errors.Is(err, service.ErrChannelModelDetectionResponseTooLarge) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"success": false, "message": err.Error()})
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, response)
}

func ListChannelModelDetectionRuns(c *gin.Context) {
	channelID, err := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if err != nil || channelID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "渠道 ID 必须为正整数"})
		return
	}
	page, ok := parseChannelModelDetectionPositiveQuery(c, "page", 1, 0)
	if !ok {
		return
	}
	pageSize, ok := parseChannelModelDetectionPositiveQuery(c, "page_size", service.ChannelModelDetectionHistoryDefaultPageSize, service.ChannelModelDetectionHistoryMaxPageSize)
	if !ok {
		return
	}
	response, err := service.ListChannelModelDetectionRuns(c.Request.Context(), nil, channelID, service.ChannelModelDetectionRunHistoryQuery{
		Page: page, PageSize: pageSize,
		Trigger: c.Query("trigger"), Status: c.Query("status"),
		Model: c.Query("model"), Outcome: c.Query("outcome"),
	})
	if err != nil {
		if errors.Is(err, service.ErrChannelModelDetectionInvalidHistoryQuery) {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "渠道不存在"})
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, response)
}

func GetChannelModelDetectionRunDetail(c *gin.Context) {
	runID := strings.TrimSpace(c.Param("run_id"))
	response, err := service.GetChannelModelDetectionRunDetail(c.Request.Context(), nil, runID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrChannelModelDetectionInvalidHistoryQuery):
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		case errors.Is(err, gorm.ErrRecordNotFound):
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "模型检测轮次不存在"})
		case errors.Is(err, service.ErrChannelModelDetectionReportTooLarge), errors.Is(err, service.ErrChannelModelDetectionResponseTooLarge):
			c.JSON(http.StatusUnprocessableEntity, gin.H{"success": false, "message": err.Error()})
		default:
			common.ApiError(c, err)
		}
		return
	}
	common.ApiSuccess(c, response)
}

func parseChannelModelDetectionPositiveQuery(c *gin.Context, name string, defaultValue int, maxValue int) (int, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return defaultValue, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 || maxValue > 0 && value > maxValue {
		message := "页码必须为正整数"
		if name == "page_size" {
			message = "每页数量必须在 1 到 100 之间"
		}
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": message})
		return 0, false
	}
	return value, true
}
