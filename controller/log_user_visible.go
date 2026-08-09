package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

func GetAllUserVisibleLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	requestID := c.Query("request_id")
	upstreamRequestID := c.Query("upstream_request_id")

	logs, total, err := model.GetAllUserVisibleLogsWithChannel(
		logType,
		startTimestamp,
		endTimestamp,
		modelName,
		username,
		tokenName,
		pageInfo.GetStartIdx(),
		pageInfo.GetPageSize(),
		channel,
		group,
		requestID,
		upstreamRequestID,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
}

func GetAllUserVisibleLogsStat(c *gin.Context) {
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	requestID := c.Query("request_id")
	upstreamRequestID := c.Query("upstream_request_id")

	stat, err := model.SumUserVisibleQuota(logType, startTimestamp, endTimestamp, modelName, username, tokenName, channel, group, requestID, upstreamRequestID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"quota": stat.Quota,
		"rpm":   stat.Rpm,
		"tpm":   stat.Tpm,
	})
}
