package model

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

type userVisibleLogQueryParams struct {
	userID            *int
	formatForUser     bool
	logType           int
	startTimestamp    int64
	endTimestamp      int64
	modelName         string
	username          string
	tokenName         string
	startIdx          int
	num               int
	channel           int
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
	if params.channel != 0 {
		tx = tx.Where("logs.channel_id = ?", params.channel)
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

	if params.formatForUser {
		formatUserLogs(logs, params.startIdx)
		return logs, total, nil
	}
	if err = hydrateLogChannelNames(logs); err != nil {
		return logs, total, err
	}
	return logs, total, nil
}

func GetAllUserVisibleLogs(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, startIdx int, num int, group string, requestID string, upstreamRequestID string) (logs []*Log, total int64, err error) {
	return GetAllUserVisibleLogsWithChannel(
		logType,
		startTimestamp,
		endTimestamp,
		modelName,
		username,
		tokenName,
		startIdx,
		num,
		0,
		group,
		requestID,
		upstreamRequestID,
	)
}

// GetAllUserVisibleLogsWithChannel queries all users while applying the
// same row filtering and user-facing formatting as the self view. Channel
// names are restored for the administrator's aggregate table.
func GetAllUserVisibleLogsWithChannel(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, startIdx int, num int, channel int, group string, requestID string, upstreamRequestID string) (logs []*Log, total int64, err error) {
	logs, total, err = queryUserVisibleLogs(userVisibleLogQueryParams{
		formatForUser:     true,
		logType:           logType,
		startTimestamp:    startTimestamp,
		endTimestamp:      endTimestamp,
		modelName:         modelName,
		username:          username,
		tokenName:         tokenName,
		startIdx:          startIdx,
		num:               num,
		channel:           channel,
		group:             group,
		requestID:         requestID,
		upstreamRequestID: upstreamRequestID,
	})
	if err != nil {
		return nil, 0, err
	}
	if err = hydrateLogChannelNames(logs); err != nil {
		return logs, total, err
	}
	return logs, total, nil
}

// sumUsedQuotaFromQuery keeps aggregate statistics aligned with the log
// visibility query while allowing the complete and user-visible views to use
// different base scopes.
func sumUsedQuotaFromQuery(base *gorm.DB, logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, channel int, group string, requestID string, upstreamRequestID string) (stat Stat, err error) {
	_ = logType // Statistics intentionally cover consume logs, matching the existing API contract.
	tx := base.Session(&gorm.Session{}).Select("COALESCE(sum(quota), 0) quota")
	rpmTpmQuery := base.Session(&gorm.Session{}).Select("count(*) rpm, COALESCE(sum(prompt_tokens), 0) + COALESCE(sum(completion_tokens), 0) tpm")

	if tx, err = applyExplicitLogTextFilter(tx, "username", username); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyExplicitLogTextFilter(rpmTpmQuery, "username", username); err != nil {
		return stat, err
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
		rpmTpmQuery = rpmTpmQuery.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if tx, err = applyExplicitLogTextFilter(tx, "model_name", modelName); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyExplicitLogTextFilter(rpmTpmQuery, "model_name", modelName); err != nil {
		return stat, err
	}
	if channel != 0 {
		tx = tx.Where("channel_id = ?", channel)
		rpmTpmQuery = rpmTpmQuery.Where("channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where(logGroupCol+" = ?", group)
		rpmTpmQuery = rpmTpmQuery.Where(logGroupCol+" = ?", group)
	}
	if requestID != "" {
		tx = tx.Where("request_id = ?", requestID)
		rpmTpmQuery = rpmTpmQuery.Where("request_id = ?", requestID)
	}
	if upstreamRequestID != "" {
		tx = tx.Where("upstream_request_id = ?", upstreamRequestID)
		rpmTpmQuery = rpmTpmQuery.Where("upstream_request_id = ?", upstreamRequestID)
	}

	tx = tx.Where("type = ?", LogTypeConsume)
	rpmTpmQuery = rpmTpmQuery.Where("type = ?", LogTypeConsume)
	rpmTpmQuery = rpmTpmQuery.Where("created_at >= ?", time.Now().Add(-60*time.Second).Unix())

	if err := tx.Scan(&stat).Error; err != nil {
		common.SysError("failed to query log stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}
	var rateStat struct {
		Rpm int
		Tpm int
	}
	if err := rpmTpmQuery.Scan(&rateStat).Error; err != nil {
		common.SysError("failed to query rpm/tpm stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}
	stat.Rpm = rateStat.Rpm
	stat.Tpm = rateStat.Tpm

	return stat, nil
}

// SumUserVisibleQuota applies the same retry filtering and response scope as
// user-visible log rows before calculating aggregate usage statistics.
func SumUserVisibleQuota(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, channel int, group string, requestID string, upstreamRequestID string) (stat Stat, err error) {
	return sumUsedQuotaFromQuery(
		userVisibleLogs(LOG_DB.Table("logs")),
		logType,
		startTimestamp,
		endTimestamp,
		modelName,
		username,
		tokenName,
		channel,
		group,
		requestID,
		upstreamRequestID,
	)
}
