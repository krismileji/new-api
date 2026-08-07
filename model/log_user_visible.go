package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
)

type userVisibleLogQueryParams struct {
	userID            *int
	logType           int
	startTimestamp    int64
	endTimestamp      int64
	modelName         string
	username          string
	tokenName         string
	startIdx          int
	num               int
	group             string
	requestID         string
	upstreamRequestID string
}

func queryUserVisibleLogs(params userVisibleLogQueryParams) (logs []*Log, total int64, err error) {
	tx := userVisibleLogs(LOG_DB)
	if params.userID != nil {
		tx = tx.Where("logs.user_id = ?", *params.userID)
	}
	if params.logType != LogTypeUnknown {
		tx = tx.Where("logs.type = ?", params.logType)
	}
	if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", params.modelName); err != nil {
		return nil, 0, err
	}
	if tx, err = applyExplicitLogTextFilter(tx, "logs.username", params.username); err != nil {
		return nil, 0, err
	}
	if params.tokenName != "" {
		tx = tx.Where("logs.token_name = ?", params.tokenName)
	}
	if params.requestID != "" {
		tx = tx.Where("logs.request_id = ?", params.requestID)
	}
	if params.upstreamRequestID != "" {
		tx = tx.Where("logs.upstream_request_id = ?", params.upstreamRequestID)
	}
	if params.startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", params.startTimestamp)
	}
	if params.endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", params.endTimestamp)
	}
	if params.group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", params.group)
	}
	if err = tx.Model(&Log{}).Limit(logSearchCountLimit).Count(&total).Error; err != nil {
		common.SysError("failed to count user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}

	order := "logs.id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("logs.")
	}
	if err = tx.Order(order).Limit(params.num).Offset(params.startIdx).Find(&logs).Error; err != nil {
		common.SysError("failed to search user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}

	formatUserLogs(logs, params.startIdx)
	return logs, total, nil
}

func GetAllUserVisibleLogs(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, startIdx int, num int, group string, requestID string, upstreamRequestID string) (logs []*Log, total int64, err error) {
	return queryUserVisibleLogs(userVisibleLogQueryParams{
		logType:           logType,
		startTimestamp:    startTimestamp,
		endTimestamp:      endTimestamp,
		modelName:         modelName,
		username:          username,
		tokenName:         tokenName,
		startIdx:          startIdx,
		num:               num,
		group:             group,
		requestID:         requestID,
		upstreamRequestID: upstreamRequestID,
	})
}
