package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
)

type ChannelPassivePeriod struct {
	Source              string   `json:"source"`
	PeriodStart         int64    `json:"period_start"`
	PeriodEnd           int64    `json:"period_end"`
	Resolution          string   `json:"resolution"`
	Coverage            string   `json:"coverage"`
	Reason              string   `json:"reason,omitempty"`
	Success             int64    `json:"success"`
	Failure             int64    `json:"failure"`
	LocalResponses      int64    `json:"local_responses"`
	FirstTokenSamples   int64    `json:"first_token_samples"`
	DurationSamples     int64    `json:"duration_samples"`
	TPSSamples          int64    `json:"tps_samples"`
	AverageFirstTokenMs *float64 `json:"avg_first_token_ms"`
	AverageDurationMs   *float64 `json:"avg_duration_ms"`
	AverageTPS          *float64 `json:"avg_tps"`
	SuccessRate         *float64 `json:"success_rate"`
	ProcessedAt         int64    `json:"processed_at"`
	DataCutoffAt        int64    `json:"data_cutoff_at"`
	Version             int64    `json:"version"`
	SampleWindowStart   int64    `json:"sample_window_start"`
	SampleWindowEnd     int64    `json:"sample_window_end"`
}

type ChannelPassiveMonitorView struct {
	Target  model.ChannelPassiveTarget `json:"target"`
	Periods []ChannelPassivePeriod     `json:"periods"`
}

type ChannelPassiveMonitorOverview struct {
	Items             []ChannelPassiveMonitorView `json:"items"`
	Total             int                         `json:"total"`
	Page              int                         `json:"page"`
	UnavailableReason string                      `json:"unavailable_reason,omitempty"`
	ServerNow         int64                       `json:"server_now"`
}

var channelPassiveLastResults = struct {
	sync.Mutex
	Periods map[string]ChannelPassivePeriod
}{Periods: make(map[string]ChannelPassivePeriod)}

const channelPassiveCoverageScript = `
local total = 0
for i,key in ipairs(KEYS) do
  local first = tonumber(ARGV[(i-1)*2+1])
  local last = tonumber(ARGV[(i-1)*2+2])
  while first < last and first%8 ~= 0 do total = total + redis.call('GETBIT',key,first); first=first+1 end
  local complete = math.floor(last/8)*8
  if complete > first then total=total+redis.call('BITCOUNT',key,first/8,complete/8-1); first=complete end
  while first < last do total=total+redis.call('GETBIT',key,first); first=first+1 end
end
return total`

func ReadChannelPassiveMonitorOverview(ctx context.Context, scope string, channelID int, group, modelName string, page int) ChannelPassiveMonitorOverview {
	page = max(page, 1)
	result := ChannelPassiveMonitorOverview{Items: []ChannelPassiveMonitorView{}, Page: page, ServerNow: time.Now().Unix()}
	snapshot := channelPassiveTargets.Load()
	if snapshot == nil {
		result.UnavailableReason = "业务周期监测配置尚未就绪或已过期"
		return result
	}
	if result.ServerNow-snapshot.LoadedAt > 15 {
		result.UnavailableReason = "业务周期监测配置已过期，以下为上次配置的结果"
	}
	var targets []model.ChannelPassiveTarget
	for _, target := range snapshot.Targets {
		if scope == "status" && target.Scope != "status" || scope == "group" && target.Scope == "status" {
			continue
		}
		if channelID > 0 && target.ChannelID != channelID || group != "" && target.GroupName != group || modelName != "" && target.ModelName != modelName {
			continue
		}
		targets = append(targets, target)
	}
	result.Total = len(targets)
	start := min((page-1)*50, len(targets))
	targets = targets[start:min(start+50, len(targets))]
	for _, target := range targets {
		interval := int64(target.IntervalSeconds)
		if interval < 30 || interval > 86400 {
			continue
		}
		current := result.ServerNow - result.ServerNow%interval
		result.Items = append(result.Items, ChannelPassiveMonitorView{Target: target, Periods: []ChannelPassivePeriod{
			{PeriodStart: current - interval, PeriodEnd: current, Resolution: "period"},
			{PeriodStart: current, PeriodEnd: current + interval, Resolution: "period"},
		}})
	}
	readChannelPassivePeriods(ctx, result.Items, result.ServerNow)
	if result.UnavailableReason != "" {
		for i := range result.Items {
			for j := range result.Items[i].Periods {
				result.Items[i].Periods[j].Coverage = "unavailable"
				result.Items[i].Periods[j].Reason = result.UnavailableReason
			}
		}
	}
	return result
}

// Public group cards need at most 100 final-request targets. Read them in one
// pipeline instead of issuing one Redis round trip for each group/member.
func ReadChannelPassiveGroupSummaries(ctx context.Context) (map[string]ChannelPassiveMonitorView, map[string]bool) {
	viewsByGroup := make(map[string]ChannelPassiveMonitorView)
	members := make(map[string]bool)
	snapshot := channelPassiveTargets.Load()
	now := time.Now().Unix()
	if snapshot == nil || now-snapshot.LoadedAt > 15 {
		return viewsByGroup, members
	}
	var views []ChannelPassiveMonitorView
	for _, target := range snapshot.Targets {
		if target.Scope == "group_member" {
			members[target.GroupName] = true
		}
		if target.Scope != "group_final" {
			continue
		}
		interval := int64(target.IntervalSeconds)
		if interval < 30 || interval > 86400 {
			continue
		}
		end := now - now%interval
		views = append(views, ChannelPassiveMonitorView{Target: target, Periods: []ChannelPassivePeriod{{PeriodStart: end - interval, PeriodEnd: end, Resolution: "period"}}})
	}
	readChannelPassivePeriods(ctx, views, now)
	for _, view := range views {
		viewsByGroup[view.Target.GroupName] = view
	}
	return viewsByGroup, members
}

func ReadChannelPassiveMonitorHistory(ctx context.Context, targetID string, days int) (ChannelPassiveMonitorView, error) {
	client := common.RedisMonitorReadClient()
	if !common.RedisEnabled || client == nil {
		return ChannelPassiveMonitorView{}, ErrChannelMonitorEventRedisUnavailable
	}
	data, err := client.Get(ctx, channelPassivePrefix+"meta:"+targetID).Bytes()
	if err != nil {
		return ChannelPassiveMonitorView{}, err
	}
	var target model.ChannelPassiveTarget
	if err := common.Unmarshal(data, &target); err != nil {
		return ChannelPassiveMonitorView{}, err
	}
	if target.ID != targetID || target.IntervalSeconds < 30 || target.IntervalSeconds > 86400 || days < 1 || days > 30 {
		return ChannelPassiveMonitorView{}, errors.New("业务周期监测历史参数无效")
	}
	now := time.Now().Unix()
	view := ChannelPassiveMonitorView{Target: target, Periods: []ChannelPassivePeriod{}}
	end := now/3600*3600 + 3600
	for start := end - int64(days)*86400; start < end; start += 3600 {
		view.Periods = append(view.Periods, ChannelPassivePeriod{PeriodStart: start, PeriodEnd: start + 3600, Resolution: "hour"})
	}
	views := []ChannelPassiveMonitorView{view}
	readChannelPassivePeriods(ctx, views, now)
	return views[0], nil
}

// Metadata keeps old revisions discoverable without interpreting their data
// with today's interval, member roster or model configuration.
func ReadChannelPassiveMonitorVersions(ctx context.Context, targetID string) ([]model.ChannelPassiveTarget, error) {
	client := common.RedisMonitorReadClient()
	if !common.RedisEnabled || client == nil {
		return nil, ErrChannelMonitorEventRedisUnavailable
	}
	data, err := client.Get(ctx, channelPassivePrefix+"meta:"+targetID).Bytes()
	if err != nil {
		return nil, err
	}
	var target model.ChannelPassiveTarget
	if err := common.Unmarshal(data, &target); err != nil {
		return nil, err
	}
	ids, err := client.ZRevRange(ctx, channelPassivePrefix+"index:"+channelPassiveSubject(target), 0, 99).Result()
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []model.ChannelPassiveTarget{target}, nil
	}
	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, channelPassivePrefix+"meta:"+id)
	}
	values, err := client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	result := make([]model.ChannelPassiveTarget, 0, len(values))
	for _, value := range values {
		raw, _ := value.(string)
		var previous model.ChannelPassiveTarget
		if common.UnmarshalJsonStr(raw, &previous) != nil || previous.Scope != target.Scope || previous.ChannelID != target.ChannelID || previous.ID != channelPassiveTargetID(previous) {
			continue
		}
		result = append(result, previous)
	}
	return result, nil
}

func readChannelPassivePeriods(ctx context.Context, views []ChannelPassiveMonitorView, now int64) {
	client := common.RedisMonitorReadClient()
	for i := range views {
		for j := range views[i].Periods {
			period := &views[i].Periods[j]
			period.Source, period.Coverage, period.Reason = "redis_business", "unavailable", "Redis 数据不可用"
		}
	}
	defer func() {
		// Retain one last complete period per target when Redis fails, with its
		// original timestamps. Never substitute it for a collecting/history row.
		channelPassiveLastResults.Lock()
		defer channelPassiveLastResults.Unlock()
		for i := range views {
			if len(views[i].Periods) == 0 {
				continue
			}
			period := &views[i].Periods[0]
			if period.Resolution != "period" || period.Coverage != "unavailable" {
				continue
			}
			if saved, ok := channelPassiveLastResults.Periods[views[i].Target.ID]; ok && now-saved.PeriodEnd <= channelPassiveRetention {
				*period = saved
				period.Coverage, period.Reason = "unavailable", "Redis 数据不可用，保留上次完整结果（时间范围见上方）"
			}
		}
	}()
	if !common.RedisEnabled || client == nil || len(views) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	pipe := client.Pipeline()
	type pendingPeriod struct {
		view, period               int
		coverageStart, coverageEnd int64
		data                       *redis.StringStringMapCmd
		coverage                   *redis.Cmd
		since                      *redis.StringCmd
		until                      *redis.StringCmd
	}
	var pending []pendingPeriod
	for i := range views {
		target := views[i].Target
		since := pipe.Get(ctx, channelPassivePrefix+"since:"+target.ID)
		until := pipe.Get(ctx, channelPassivePrefix+"until:"+target.ID)
		for j, period := range views[i].Periods {
			keys := []string{}
			args := []any{}
			coverageStart, coverageEnd := period.PeriodStart, min(period.PeriodEnd, now)
			if period.Resolution == "hour" {
				interval := int64(target.IntervalSeconds)
				coverageStart = period.PeriodStart / interval * interval
				coverageEnd = min(period.PeriodEnd/interval*interval, now)
			}
			for start := coverageStart; start < coverageEnd; {
				day := start / 86400 * 86400
				end := min(day+86400, coverageEnd)
				keys = append(keys, fmt.Sprintf("%scoverage:%d", channelPassivePrefix, day))
				args = append(args, start-day, end-day)
				start = end
			}
			data := pipe.HGetAll(ctx, channelPassivePrefix+target.ID+":"+period.Resolution+":"+strconv.FormatInt(period.PeriodStart, 10))
			coverage := pipe.Eval(ctx, channelPassiveCoverageScript, keys, args...)
			pending = append(pending, pendingPeriod{view: i, period: j, coverageStart: coverageStart, coverageEnd: coverageEnd, data: data, coverage: coverage, since: since, until: until})
		}
	}
	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, redis.Nil) {
		return
	}
	for _, item := range pending {
		period := &views[item.view].Periods[item.period]
		target := views[item.view].Target
		values, err := item.data.Result()
		if err != nil {
			continue
		}
		covered, err := item.coverage.Int64()
		if err != nil {
			continue
		}
		since, _ := item.since.Int64()
		until, _ := item.until.Int64()
		// Reset metrics if the caller reuses a live period for a later read.
		if period.Resolution == "period" {
			*period = ChannelPassivePeriod{Source: "redis_business", Resolution: "period", PeriodStart: item.coverageStart, PeriodEnd: item.coverageStart + int64(target.IntervalSeconds)}
		}
		period.SampleWindowStart, period.SampleWindowEnd = item.coverageStart, item.coverageEnd
		period.Coverage, period.Reason = "complete", ""
		if now >= period.PeriodEnd+48*3600 && period.Resolution == "period" {
			period.Coverage, period.Reason = "unavailable", "精确周期已过保留期，请查看小时汇总"
		} else if period.PeriodEnd > now {
			period.Coverage, period.Reason = "collecting", "本周期采集中"
		} else if covered != item.coverageEnd-item.coverageStart || since == 0 || item.coverageEnd > until || item.coverageStart < max(target.EffectiveAt, since) || values["invalid"] != "" {
			period.Coverage, period.Reason = "partial", "配置启用不足一个周期、监控链路存在缺口或数据尚未处理完成"
		}
		if item.coverageEnd == item.coverageStart && period.Resolution == "hour" {
			period.Reason = "此小时没有结束的监测周期"
		}
		parse := func(key string) float64 {
			raw := values[key]
			if raw == "" {
				return 0
			}
			value, err := strconv.ParseFloat(raw, 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 9007199254740991 {
				period.Coverage = "partial"
				period.Reason = "Redis 周期数据无效"
				return 0
			}
			return value
		}
		period.Success, period.Failure, period.LocalResponses = int64(parse("success")), int64(parse("failure")), int64(parse("local"))
		period.FirstTokenSamples, period.DurationSamples, period.TPSSamples = int64(parse("first_n")), int64(parse("duration_n")), int64(parse("tps_n"))
		period.ProcessedAt, period.Version = int64(parse("processed_at")), int64(parse("version"))
		period.DataCutoffAt = min(period.PeriodEnd, now-10)
		if period.Success+period.Failure > 0 {
			period.SuccessRate = common.GetPointer(float64(period.Success) / float64(period.Success+period.Failure))
		}
		if period.FirstTokenSamples > 0 {
			period.AverageFirstTokenMs = common.GetPointer(parse("first_sum") / float64(period.FirstTokenSamples))
		}
		if period.DurationSamples > 0 {
			period.AverageDurationMs = common.GetPointer(parse("duration_sum") / float64(period.DurationSamples))
		}
		if parse("generation_ms") > 0 {
			period.AverageTPS = common.GetPointer(parse("output") * 1000 / parse("generation_ms"))
		}
		if period.Coverage == "complete" && period.Resolution == "period" {
			channelPassiveLastResults.Lock()
			if len(channelPassiveLastResults.Periods) >= channelPassiveMaxTargets {
				for id, saved := range channelPassiveLastResults.Periods {
					if now-saved.PeriodEnd > 86400 {
						delete(channelPassiveLastResults.Periods, id)
					}
				}
			}
			_, exists := channelPassiveLastResults.Periods[target.ID]
			if exists || len(channelPassiveLastResults.Periods) < channelPassiveMaxTargets {
				channelPassiveLastResults.Periods[target.ID] = *period
			}
			channelPassiveLastResults.Unlock()
		}
	}
}
