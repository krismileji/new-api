package controller

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

type channelMonitorBalanceEvaluation struct {
	EffectiveBalance     float64
	EstimatedConsumption float64
	Complete             bool
	Estimate             *service.ChannelBalanceEstimate
}

func formatChannelMonitorBalanceAmount(amount float64) string {
	if math.Abs(amount) < 0.0000005 {
		return "0"
	}
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(amount, 'f', 6, 64), "0"), ".")
}

func evaluateChannelMonitorBalance(ctx context.Context, monitor model.ChannelRatioMonitor, balance float64) (channelMonitorBalanceEvaluation, error) {
	evaluation := channelMonitorBalanceEvaluation{EffectiveBalance: balance, Complete: true}
	if math.IsNaN(balance) || math.IsInf(balance, 0) {
		return evaluation, errors.New("上游余额不是有效数字")
	}
	if monitor.BalanceAutoDisableThreshold == nil || monitor.UpstreamBalanceSyncDisabled {
		return evaluation, nil
	}
	estimate, err := service.GetChannelBalanceEstimate(ctx, service.ChannelBalanceConfigForMonitor(monitor))
	if err != nil || !estimate.Available || estimate.Revision != service.ChannelBalanceConfigForMonitor(monitor).Revision || math.Abs(estimate.UpstreamBalance-balance) > 0.000001 {
		evaluation.Complete = false
		return evaluation, service.ErrChannelBalanceUnavailable
	}
	evaluation.Estimate = &estimate
	evaluation.Complete = estimate.Complete
	if estimate.Coverage {
		// A query-window debit may already be in the raw balance. It can block
		// recovery, but must not be the only reason to disable the channel.
		evaluation.EstimatedConsumption = estimate.PolicyConsumption
		evaluation.EffectiveBalance = estimate.PolicyBalance
	}
	return evaluation, nil
}

func recordChannelMonitorBalanceUpdate(ctx context.Context, monitor model.ChannelRatioMonitor, balance *float64, fetchError string, syncs ...service.ChannelBalanceSync) (*channelMonitorBalanceEvaluation, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// This deadline is shorter than the Redis lease. HTTP has already finished;
	// a cancelled client must not interrupt the atomic snapshot/save boundary.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if balance != nil && (math.IsNaN(*balance) || math.IsInf(*balance, 0)) {
		fetchError = "上游余额不是有效数字"
	}
	if fetchError != "" {
		balance = nil
	} else if balance == nil {
		fetchError = "上游未返回余额"
	}
	// Upstream snapshots also work in installations that explicitly disable
	// Redis. Only the estimate is unavailable; no SQL cost aggregation replaces
	// it. A configured Redis outage still fails the fenced path below.
	if !common.RedisEnabled {
		applied, err := model.RecordChannelRatioMonitorBalanceIfRevision(ctx, monitor.ChannelId, monitor.UpstreamRevision, balance, fetchError)
		if err != nil || !applied || balance == nil {
			return nil, applied, err
		}
		evaluation, _ := evaluateChannelMonitorBalance(ctx, monitor, *balance)
		return &evaluation, true, nil
	}
	var sync service.ChannelBalanceSync
	var err error
	if len(syncs) > 0 {
		sync = syncs[0]
	} else {
		sync, err = service.BeginChannelBalanceSync(ctx, monitor)
	}
	if err != nil {
		return nil, false, err
	}
	if sync.Epoch == "" {
		return nil, false, service.ErrChannelBalanceUnavailable
	}
	unlock, err := service.LockChannelBalanceSyncResult(ctx, sync.Config)
	if err != nil {
		return nil, false, err
	}
	defer unlock()
	if balance == nil {
		err = service.FailChannelBalanceSync(ctx, sync)
	} else {
		current, readErr := service.GetChannelBalanceEstimate(ctx, sync.Config)
		if readErr != nil {
			return nil, false, readErr
		}
		coverage := current.Coverage && current.Epoch == sync.Epoch
		if !coverage && sync.IdleCoverage {
			// Unresolved balance reservations can outlive the transport. Check
			// actual request leases on both sides of the query, not that backlog.
			coverage = service.ChannelBalanceConfigHasIdleRequestCoverage(ctx, sync.Config)
		}
		_, err = service.CommitChannelBalanceSync(ctx, sync, *balance, coverage)
	}
	if errors.Is(err, model.ErrChannelRatioMonitorConfigChanged) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	applied, err := model.RecordChannelRatioMonitorBalanceIfRevision(ctx, monitor.ChannelId, monitor.UpstreamRevision, balance, fetchError)
	if err != nil || !applied {
		return nil, applied, err
	}
	if balance == nil {
		return nil, true, nil
	}
	evaluation, _ := evaluateChannelMonitorBalance(ctx, monitor, *balance)
	return &evaluation, true, nil
}

func applyChannelBalanceRealtimePolicy(ctx context.Context, config service.ChannelBalanceConfig, expected service.ChannelBalanceEstimate) (bool, error) {
	if config.AccountID > 0 {
		members, err := model.GetUpstreamAccountMonitors(ctx, config.AccountID)
		if err != nil {
			return false, err
		}
		for _, member := range members {
			if member.UpstreamBalanceSyncDisabled || member.BalanceAutoDisableThreshold == nil {
				continue
			}
			memberConfig := service.ChannelBalanceConfigForMonitor(member)
			handled, err := applyChannelBalancePolicyToChannel(ctx, memberConfig, expected)
			if err != nil || !handled {
				return false, err
			}
		}
		return true, nil
	}
	return applyChannelBalancePolicyToChannel(ctx, config, expected)
}

func applyChannelBalancePolicyToChannel(ctx context.Context, config service.ChannelBalanceConfig, expected service.ChannelBalanceEstimate) (bool, error) {
	estimate, err := service.GetChannelBalanceEstimate(ctx, config)
	if err != nil || !estimate.Available || estimate.Revision != config.Revision || estimate.Decision == "unknown" ||
		estimate.Epoch != expected.Epoch || estimate.Decision != expected.Decision {
		return false, err
	}
	monitor, err := model.GetChannelRatioMonitorWithContext(ctx, config.ChannelID)
	if err != nil || monitor.UpstreamAccountID != config.AccountID || service.ChannelBalanceConfigForMonitor(monitor).Revision != config.Revision || monitor.UpstreamBalanceSyncDisabled {
		return false, err
	}
	channel, err := model.GetChannelById(config.ChannelID, true)
	if err != nil {
		return false, err
	}
	evaluation, err := evaluateChannelMonitorBalance(ctx, monitor, estimate.UpstreamBalance)
	if err != nil {
		return false, err
	}
	changed := false
	if monitor.BalanceAutoDisableThreshold != nil && evaluation.EffectiveBalance < *monitor.BalanceAutoDisableThreshold {
		changed, err = autoDisableChannelMonitorAtEffectiveBalance(monitor, channel, estimate.UpstreamBalance,
			evaluation.EffectiveBalance, evaluation.EstimatedConsumption, &evaluation)
	} else if evaluation.Complete && monitor.BalanceAutoDisableThreshold != nil && evaluation.EffectiveBalance >= *monitor.BalanceAutoDisableThreshold && channelMonitorAutoDisabledForLowBalance(channel) &&
		(config.AccountID == 0 || (getChannelMonitorSettings().AutoEnableOnBalanceRecovery && monitor.UpdatedTime > 0)) {
		var allowed bool
		allowed, err = channelMonitorAllowsHealthCheckAutoEnable(channel.Id)
		if err == nil && allowed {
			costRatio, _, ratioErr := channelMonitorCostRatioFromModel(monitor, monitor.Ratio)
			if ratioErr != nil {
				return false, ratioErr
			}
			if channelMonitorCostRatioMeetsEveryGroup(channel, costRatio, ratio_setting.GetGroupRatioCopy(), getChannelMonitorGroupCoefficients(), true) {
				changed, _, _, err = model.UpdateChannelMonitorStatusIfSnapshotRevision(channel.Id, monitor.UpstreamRevision,
					model.CaptureChannelMonitorStatus(channel), common.ChannelStatusEnabled, "")
			}
		}
	}
	if changed {
		model.InitChannelCache()
		service.ResetProxyClientCache()
	}
	return err == nil, err
}
