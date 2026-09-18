package service

import (
	"context"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type ChannelLimitGroupView struct {
	model.ChannelLimitGroup
	Runtime      ChannelLimitStatus               `json:"runtime"`
	ChannelUsage map[int]ChannelConcurrencyStatus `json:"channel_usage"`
}

func ListChannelLimitGroupViews(ctx context.Context) ([]ChannelLimitGroupView, error) {
	groups, revision, err := model.ReadChannelLimitGroups(model.DB.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	views := make([]ChannelLimitGroupView, 0, len(groups))
	if len(groups) == 0 {
		return views, nil
	}
	if err := ensureChannelLimitRegistry(ctx); err != nil {
		for _, group := range groups {
			views = append(views, ChannelLimitGroupView{ChannelLimitGroup: group, Runtime: ChannelLimitStatus{GroupID: group.ID, Reason: "unavailable"}})
		}
		return views, nil
	}
	for _, group := range groups {
		view := ChannelLimitGroupView{ChannelLimitGroup: group, Runtime: ChannelLimitStatus{GroupID: group.ID}, ChannelUsage: make(map[int]ChannelConcurrencyStatus)}
		ids := make([]int, 0, len(group.Members))
		for _, member := range group.Members {
			ids = append(ids, member.ChannelID)
		}
		usage, err := GetChannelConcurrencySnapshotForChannelIDs(ctx, ids)
		if err != nil {
			view.Runtime.Reason = "unavailable"
			views = append(views, view)
			continue
		}
		view.ChannelUsage = usage
		if common.RedisEnabled {
			raw, err := common.RDB.Eval(ctx, channelLimitRuntimeScript, []string{channelLimitRegistryKey}, "snapshot", group.ID, 0, "", 0, 0).Text()
			var reply channelLimitReply
			if err == nil {
				err = common.UnmarshalJsonStr(raw, &reply)
			}
			if err != nil {
				view.Runtime.Reason = "unavailable"
			} else {
				view.Runtime = ChannelLimitStatus{GroupID: group.ID, Active: reply.GroupActive, RPM: reply.GroupRPM, Waiting: reply.Waiting, Reason: reply.Reason, Tiers: reply.Tiers}
			}
			runtimeRevision, err := common.RDB.HGet(ctx, channelLimitRegistryKey, "revision").Int64()
			if err != nil || runtimeRevision != revision {
				view.Runtime.Reason = "publishing"
			}
		} else {
			channelLimitConfig.Lock()
			channelConcurrency.Lock()
			active, rpm, totalActive, totalRPM := channelLimitLocalCounts(group, time.Now().UnixMilli())
			view.Runtime.Active, view.Runtime.RPM = totalActive, totalRPM
			for _, tier := range group.Tiers {
				view.Runtime.Tiers = append(view.Runtime.Tiers, ChannelLimitTierUsage{Priority: tier.Priority, Active: active[tier.Priority], RPM: rpm[tier.Priority]})
			}
			view.Runtime.Tiers = append(view.Runtime.Tiers, ChannelLimitTierUsage{Priority: -1, Active: active[-1], RPM: rpm[-1]})
			view.Runtime.Waiting = len(channelLimitConfig.local[group.ID].waiters)
			channelConcurrency.Unlock()
			current := channelLimitConfig.registry.Groups[strconv.FormatInt(group.ID, 10)]
			if current.Revision != group.Revision {
				view.Runtime.Reason = "publishing"
			} else if !group.Enabled && view.Runtime.Reason == "" {
				view.Runtime.Reason = "paused"
			}
			channelLimitConfig.Unlock()
		}
		views = append(views, view)
	}
	return views, nil
}

func ChannelLimitReasonText(reason string) string {
	switch reason {
	case "paused":
		return "共享限流组已暂停新请求"
	case "group_concurrency":
		return "共享限流组并发已满"
	case "group_rpm":
		return "共享限流组 RPM 已满"
	case "reserved_concurrency":
		return "当前等级并发额度已满，其余额度为更高等级预留"
	case "reserved_rpm":
		return "当前等级 RPM 额度已满，其余额度为更高等级预留"
	case "priority_wait":
		return "正在等待同级或更高等级请求先获得额度"
	case "queue_full":
		return "共享限流组等待队列已满"
	case "wait_timeout":
		return "等待共享限流额度超时，请稍后重试"
	case "channel_concurrency":
		return "渠道自身并发额度已满"
	case "channel_rpm":
		return "渠道自身 RPM 额度已满"
	default:
		return "共享限流运行状态不可用"
	}
}
