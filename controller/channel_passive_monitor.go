package controller

import (
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetChannelPassiveMonitor(c *gin.Context) {
	scope := c.DefaultQuery("scope", "status")
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	channelID, idErr := strconv.Atoi(c.DefaultQuery("channel_id", "0"))
	if scope != "status" && scope != "group" || err != nil || idErr != nil || page < 1 || page > 10000 || channelID < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "业务周期监测查询参数无效"})
		return
	}
	result := service.ReadChannelPassiveMonitorOverview(c.Request.Context(), scope, channelID, strings.TrimSpace(c.Query("group")), strings.TrimSpace(c.Query("model")), page)
	common.ApiSuccess(c, result)
}

func GetChannelPassiveMonitorHistory(c *gin.Context) {
	targetID := c.Param("target")
	_, err := hex.DecodeString(targetID)
	days, dayErr := strconv.Atoi(c.DefaultQuery("days", "1"))
	if err != nil || len(targetID) != 64 || dayErr != nil || days < 1 || days > 30 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "历史范围应为 1～30 天"})
		return
	}
	result, err := service.ReadChannelPassiveMonitorHistory(c.Request.Context(), targetID, days)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func GetChannelPassiveMonitorVersions(c *gin.Context) {
	targetID := c.Param("target")
	if _, err := hex.DecodeString(targetID); err != nil || len(targetID) != 64 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "业务周期监测目标无效"})
		return
	}
	result, err := service.ReadChannelPassiveMonitorVersions(c.Request.Context(), targetID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}
