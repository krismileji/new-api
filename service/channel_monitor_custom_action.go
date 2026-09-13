package service

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata"
)

// Actions use the upstream metric after its extraction multiplier, before cost conversion.
type ChannelMonitorCustomAction struct {
	ID              string                            `json:"id"`
	Name            string                            `json:"name"`
	Enabled         bool                              `json:"enabled"`
	Metric          string                            `json:"metric"`
	Operator        string                            `json:"operator"`
	Threshold       *float64                          `json:"threshold"`
	Timezone        string                            `json:"timezone"`
	StartTime       string                            `json:"start_time"`
	EndTime         string                            `json:"end_time"`
	DailyLimit      int                               `json:"daily_limit"`
	CooldownMinutes int                               `json:"cooldown_minutes"`
	BaseURL         string                            `json:"base_url,omitempty"`
	Request         ChannelMonitorCustomRequestConfig `json:"request"`
	SuccessPath     string                            `json:"success_path,omitempty"`
	SuccessValue    string                            `json:"success_value,omitempty"`
}

var channelMonitorCustomActionID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func normalizeChannelMonitorCustomActions(actions []ChannelMonitorCustomAction, existing *ChannelMonitorCustomUpstreamConfig) ([]ChannelMonitorCustomAction, error) {
	if len(actions) > 8 {
		return nil, errors.New("触发规则不能超过 8 条")
	}
	normalized := append([]ChannelMonitorCustomAction(nil), actions...)
	seen := make(map[string]bool)
	for index := range normalized {
		action := &normalized[index]
		action.ID, action.Name = strings.TrimSpace(action.ID), strings.TrimSpace(action.Name)
		if !channelMonitorCustomActionID.MatchString(action.ID) || seen[action.ID] {
			return nil, errors.New("触发规则标识无效或重复")
		}
		seen[action.ID] = true
		if action.Name == "" || len([]rune(action.Name)) > 80 {
			return nil, errors.New("触发规则名称不能为空且不能超过 80 个字符")
		}
		if action.Metric != "balance" && action.Metric != "ratio" {
			return nil, errors.New("触发指标必须是余额或倍率")
		}
		switch action.Operator {
		case "lt", "lte", "gt", "gte":
		default:
			return nil, errors.New("触发比较方式无效")
		}
		if action.Threshold == nil || math.IsNaN(*action.Threshold) || math.IsInf(*action.Threshold, 0) || math.Abs(*action.Threshold) > maxChannelMonitorCustomBalance {
			return nil, errors.New("触发阈值必须是绝对值不超过 1000000000000000 的有效数字")
		}
		if action.Metric == "ratio" && (*action.Threshold < 0 || *action.Threshold > maxUpstreamGroupRatio) {
			return nil, errors.New("倍率触发阈值必须在 0 到 1000000 之间")
		}
		action.Timezone = strings.TrimSpace(action.Timezone)
		if action.Timezone == "" || action.Timezone == "Local" {
			return nil, errors.New("请明确设置触发规则时区，例如 Asia/Shanghai")
		}
		if _, err := time.LoadLocation(action.Timezone); err != nil {
			return nil, errors.New("触发规则时区无效")
		}
		start, startErr := time.Parse("15:04", action.StartTime)
		end, endErr := time.Parse("15:04", action.EndTime)
		if startErr != nil || endErr != nil || start.Format("15:04") != action.StartTime || end.Format("15:04") != action.EndTime || !start.Before(end) {
			return nil, errors.New("执行时段必须为同一天内的 HH:mm，开始时间须早于截止时间")
		}
		if action.DailyLimit < 1 || action.DailyLimit > 100 || action.CooldownMinutes < 1 || action.CooldownMinutes > 10080 {
			return nil, errors.New("每日次数须为 1 到 100，冷却时间须为 1 到 10080 分钟")
		}
		var savedRequest *ChannelMonitorCustomRequestConfig
		if existing != nil {
			for _, saved := range existing.Actions {
				if saved.ID == action.ID && saved.BaseURL == strings.TrimRight(strings.TrimSpace(action.BaseURL), "/") {
					savedRequest = &saved.Request
					break
				}
			}
		}
		var err error
		if action.BaseURL != "" {
			action.BaseURL, err = NormalizeChannelMonitorCustomBaseURL(action.BaseURL)
			if err != nil {
				return nil, err
			}
		}
		action.Request, err = normalizeChannelMonitorCustomRequest(action.Request, savedRequest)
		if err != nil {
			return nil, fmt.Errorf("触发规则 %s 的接口配置无效: %w", action.Name, err)
		}
		action.SuccessPath = strings.TrimSpace(action.SuccessPath)
		if len(action.SuccessPath) > maxChannelMonitorCustomResultPath || len(action.SuccessValue) > 256 {
			return nil, errors.New("成功判定路径或期望值过长")
		}
	}
	return normalized, nil
}

func (action ChannelMonitorCustomAction) matches(value float64) bool {
	if action.Threshold == nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return false
	}
	switch action.Operator {
	case "lt":
		return value < *action.Threshold
	case "lte":
		return value <= *action.Threshold
	case "gt":
		return value > *action.Threshold
	case "gte":
		return value >= *action.Threshold
	}
	return false
}

func (action ChannelMonitorCustomAction) executionWindow(now time.Time) (string, time.Time, bool) {
	location, err := time.LoadLocation(action.Timezone)
	if err != nil {
		return "", time.Time{}, false
	}
	local := now.In(location)
	day := local.Format("2006-01-02")
	cutoff, err := time.ParseInLocation("2006-01-02 15:04", day+" "+action.EndTime, location)
	if err != nil {
		return day, time.Time{}, false
	}
	clock := local.Format("15:04")
	return day, cutoff, clock >= action.StartTime && clock < action.EndTime
}
