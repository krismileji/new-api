package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func init() { service.RegisterUpstreamAccountBalanceFetcher(fetchUpstreamAccountAutomationBalance) }

func fetchUpstreamAccountAutomationBalance(ctx context.Context, config service.UpstreamAutomationConfig) (*float64, error) {
	account, err := model.GetChannelMonitorUpstreamAccount(ctx, config.AccountID)
	if err != nil {
		return nil, err
	}
	if account.Revision != config.AccountRevision || account.LeaseID != config.AccountLeaseID {
		return nil, model.ErrUpstreamAccountChanged
	}
	members, err := model.GetUpstreamAccountMonitors(ctx, config.AccountID)
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		result, err := service.FetchChannelMonitorUpstreamBalance(ctx, service.ChannelMonitorUpstreamConfig{AccountID: config.AccountID, AccountRevision: config.AccountRevision, AccountLeaseID: config.AccountLeaseID, RequestTimeout: time.Duration(config.RequestTimeout) * time.Second})
		if err != nil {
			return nil, err
		}
		return result.Amount, model.RecordUpstreamAccountBalance(ctx, account.ID, account.Revision, config.AccountLeaseID, result.Amount, "")
	}
	outcome, err := refreshUpstreamAccountBalanceUnderLease(ctx, account, members[0], config.AccountLeaseID, time.Duration(config.RequestTimeout)*time.Second)
	if err != nil || outcome.Result.Balance.Amount == nil {
		return outcome.Result.Balance.Amount, err
	}
	for _, member := range members {
		if member.UpstreamBalanceSyncDisabled {
			continue
		}
		channel, err := model.GetChannelById(member.ChannelId, true)
		if err != nil {
			return outcome.Result.Balance.Amount, err
		}
		if err := recoverUpstreamAutomationChannel(ctx, member, channel, *outcome.Result.Balance.Amount); err != nil {
			return outcome.Result.Balance.Amount, err
		}
	}
	return outcome.Result.Balance.Amount, nil
}

type upstreamAccountBalanceRoundKey struct{}

type upstreamAccountBalanceRound struct {
	results      sync.Map
	leases       sync.Map
	forceRefresh bool
	start        int64
	bulkUpdate   bool
	leaseUntil   time.Time
}

type upstreamAccountRoundLease struct {
	done    chan struct{}
	token   string
	release func()
	err     error
}

func lockChannelMonitorRoundAccount(ctx context.Context, monitor model.ChannelRatioMonitor, timeout time.Duration) (string, func(), error) {
	config := service.ChannelMonitorUpstreamConfig{AccountID: monitor.UpstreamAccountID, AccountRevision: monitor.UpstreamAccountRevision, RequestTimeout: timeout}
	round, _ := ctx.Value(upstreamAccountBalanceRoundKey{}).(*upstreamAccountBalanceRound)
	if round == nil || !round.bulkUpdate {
		return service.LockUpstreamAccountRequest(ctx, config)
	}
	key := [2]int64{int64(monitor.UpstreamAccountID), monitor.UpstreamAccountRevision}
	entry := &upstreamAccountRoundLease{done: make(chan struct{})}
	value, loaded := round.leases.LoadOrStore(key, entry)
	if loaded {
		entry = value.(*upstreamAccountRoundLease)
		select {
		case <-entry.done:
		case <-ctx.Done():
			return "", nil, ctx.Err()
		}
	} else {
		entry.token, entry.release, entry.err = service.LockUpstreamAccountRequestUntil(ctx, config, round.leaseUntil)
		close(entry.done)
		if entry.err != nil {
			round.leases.CompareAndDelete(key, entry)
		}
	}
	return entry.token, func() {}, entry.err
}

type upstreamAccountBalanceResult struct {
	done    chan struct{}
	outcome channelMonitorFetchOutcome
	err     error
}

func mergeUpstreamAccountBalanceWarnings(warnings []channelRatioMonitorBalanceWarning) []channelRatioMonitorBalanceWarning {
	merged := make([]channelRatioMonitorBalanceWarning, 0, len(warnings))
	indexes := make(map[int]int)
	for _, warning := range warnings {
		if warning.AccountID == 0 {
			merged = append(merged, warning)
			continue
		}
		if index, ok := indexes[warning.AccountID]; ok {
			merged[index].ChannelRemark = strings.Trim(merged[index].ChannelRemark+"、"+warning.ChannelName, "、")
			continue
		}
		indexes[warning.AccountID] = len(merged)
		warning.ChannelRemark = "关联渠道：" + warning.ChannelName
		warning.ChannelName = fmt.Sprintf("共享上游账户 #%d", warning.AccountID)
		merged = append(merged, warning)
	}
	return merged
}

// Each bulk run owns a memo, so every account is polled once even when its
// channels run concurrently. Manual refreshes have their own fresh scope.
func withUpstreamAccountBalanceRound(ctx context.Context) context.Context {
	if _, ok := ctx.Value(upstreamAccountBalanceRoundKey{}).(*upstreamAccountBalanceRound); ok {
		return ctx
	}
	return context.WithValue(ctx, upstreamAccountBalanceRoundKey{}, &upstreamAccountBalanceRound{})
}

func fetchAndRecordUpstreamAccountBalance(ctx context.Context, monitor model.ChannelRatioMonitor, timeout time.Duration) (channelMonitorFetchOutcome, error) {
	if round, ok := ctx.Value(upstreamAccountBalanceRoundKey{}).(*upstreamAccountBalanceRound); ok {
		entry := &upstreamAccountBalanceResult{done: make(chan struct{})}
		key := [2]int64{int64(monitor.UpstreamAccountID), monitor.UpstreamAccountRevision}
		value, loaded := round.results.LoadOrStore(key, entry)
		if loaded {
			entry = value.(*upstreamAccountBalanceResult)
			select {
			case <-entry.done:
				return entry.outcome, entry.err
			case <-ctx.Done():
				return channelMonitorFetchOutcome{}, ctx.Err()
			}
		}
		entry.outcome, entry.err = pollUpstreamAccountBalance(ctx, monitor, timeout)
		close(entry.done)
		return entry.outcome, entry.err
	}
	return pollUpstreamAccountBalance(ctx, monitor, timeout)
}

func pollUpstreamAccountBalance(ctx context.Context, monitor model.ChannelRatioMonitor, timeout time.Duration) (outcome channelMonitorFetchOutcome, err error) {
	account, err := model.GetChannelMonitorUpstreamAccount(ctx, monitor.UpstreamAccountID)
	if err != nil {
		return outcome, err
	}
	if account.Revision != monitor.UpstreamAccountRevision {
		return outcome, model.ErrUpstreamAccountChanged
	}
	round, scheduled := ctx.Value(upstreamAccountBalanceRoundKey{}).(*upstreamAccountBalanceRound)
	scheduled = scheduled && !round.forceRefresh && !round.bulkUpdate
	intervalMinutes := getChannelMonitorSettings().AutoUpdateIntervalMinutes
	if scheduled && intervalMinutes == 0 {
		outcome.Result.Balance.Amount = account.Balance
		return outcome, nil
	}
	token, release, err := lockChannelMonitorRoundAccount(ctx, monitor, timeout)
	if err != nil {
		return outcome, err
	}
	defer release()
	account, err = model.GetChannelMonitorUpstreamAccount(ctx, account.ID)
	if err != nil {
		return outcome, err
	}
	if scheduled && account.LastBalanceCheck > 0 && upstreamAccountBalanceChecked(account.LastBalanceCheck, round.start, intervalMinutes) {
		outcome.Result.Balance.Error = account.LastBalanceError
		if account.LastBalanceError != "" {
			return outcome, errors.New(account.LastBalanceError)
		}
		if account.Balance == nil {
			return outcome, errors.New("上游账户未返回余额")
		}
		outcome.Result.Balance.Amount = account.Balance
		evaluation, _ := evaluateChannelMonitorBalance(ctx, monitor, *account.Balance)
		outcome.BalanceRecorded, outcome.BalanceEvaluation = true, &evaluation
		return outcome, nil
	}
	return refreshUpstreamAccountBalanceUnderLease(ctx, account, monitor, token, timeout)
}

func upstreamAccountBalanceChecked(lastCheck, roundStart int64, intervalMinutes int) bool {
	if roundStart > 0 {
		return lastCheck >= roundStart
	}
	return common.GetTimestamp() < lastCheck+int64(intervalMinutes)*60
}

func refreshUpstreamAccountBalanceUnderLease(ctx context.Context, account model.ChannelMonitorUpstreamAccount, monitor model.ChannelRatioMonitor, token string, timeout time.Duration) (outcome channelMonitorFetchOutcome, err error) {
	var balanceSync service.ChannelBalanceSync
	if common.RedisEnabled {
		members, err := model.GetUpstreamAccountMonitors(ctx, account.ID)
		if err != nil {
			return outcome, err
		}
		for _, member := range members {
			if err := service.ConfigureChannelBalanceEstimate(ctx, member); err != nil {
				return outcome, err
			}
		}
		balanceSync, err = service.BeginChannelBalanceSync(ctx, monitor)
		if err != nil {
			return outcome, err
		}
	}
	result, fetchErr := service.FetchChannelMonitorUpstreamBalance(ctx, service.ChannelMonitorUpstreamConfig{
		AccountID: account.ID, AccountRevision: account.Revision, AccountLeaseID: token, RequestTimeout: timeout,
	})
	if fetchErr == nil && result.Amount == nil {
		fetchErr = errors.New("上游未返回余额")
	}
	message := ""
	if fetchErr != nil {
		message = fetchErr.Error()
		result.Amount = nil
		result.Error = message
	}
	outcome.Result.Balance = result
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if common.RedisEnabled {
		unlock, lockErr := service.LockChannelBalanceSyncResult(finish, balanceSync.Config)
		if lockErr != nil {
			return outcome, lockErr
		}
		defer unlock()
		if fetchErr != nil {
			err = service.FailChannelBalanceSync(finish, balanceSync)
		} else {
			current, readErr := service.GetChannelBalanceEstimate(finish, balanceSync.Config)
			if readErr != nil {
				return outcome, readErr
			}
			coverage := current.Coverage && current.Epoch == balanceSync.Epoch
			if !coverage && balanceSync.IdleCoverage {
				coverage = service.ChannelBalanceConfigHasIdleRequestCoverage(finish, balanceSync.Config)
			}
			_, err = service.CommitChannelBalanceSync(finish, balanceSync, *result.Amount, coverage)
		}
		if err != nil {
			return outcome, err
		}
	}
	round, _ := ctx.Value(upstreamAccountBalanceRoundKey{}).(*upstreamAccountBalanceRound)
	bulkUpdate := round != nil && round.bulkUpdate
	if err := model.RecordUpstreamAccountBalance(finish, account.ID, account.Revision, token, result.Amount, message, bulkUpdate); err != nil {
		return outcome, err
	}
	if fetchErr != nil {
		return outcome, fetchErr
	}
	monitor.UpstreamBalance = result.Amount
	evaluation, _ := evaluateChannelMonitorBalance(finish, monitor, *result.Amount)
	outcome.BalanceEvaluation, outcome.BalanceRecorded = &evaluation, true
	if bulkUpdate {
		return outcome, nil
	}
	members, err := model.GetUpstreamAccountMonitors(finish, account.ID)
	if err != nil {
		return outcome, err
	}
	if err := notifyUpstreamAccountBalance(finish, account, members, *result.Amount, common.SendEmail); err != nil {
		logger.LogWarn(finish, "上游账户余额告警发送失败："+err.Error())
	}
	statusChanged := false
	defer func() {
		if statusChanged {
			model.InitChannelCache()
			service.ResetProxyClientCache()
			service.NotifyChannelModelDetectionOverviewChanged()
		}
	}()
	for _, member := range members {
		if member.UpstreamBalanceSyncDisabled {
			continue
		}
		channel, err := model.GetChannelById(member.ChannelId, true)
		if err != nil {
			return outcome, err
		}
		changed, err := autoDisableChannelMonitorForLowBalanceWithContext(finish, member, channel, *result.Amount)
		statusChanged = statusChanged || changed
		if err != nil {
			return outcome, fmt.Errorf("账户余额已同步，渠道保护失败: %w", err)
		}
		memberEvaluation, _ := evaluateChannelMonitorBalance(finish, member, *result.Amount)
		if memberEvaluation.Complete && getChannelMonitorSettings().AutoEnableOnBalanceRecovery && member.UpdatedTime > 0 {
			costRatio, _, err := channelMonitorCostRatioFromModel(member, member.Ratio)
			if err != nil {
				return outcome, err
			}
			input := map[int]channelMonitorPolicyInput{member.ChannelId: {UpstreamRevision: member.UpstreamRevision, CostRatio: costRatio,
				BalanceBelowAutoDisableThreshold: member.BalanceAutoDisableThreshold != nil && memberEvaluation.EffectiveBalance < *member.BalanceAutoDisableThreshold}}
			_, err = autoEnableChannelsAfterBalanceRecovery(finish, []*model.Channel{channel}, input, ratio_setting.GetGroupRatioCopy(), getChannelMonitorGroupCoefficients())
			if err != nil {
				return outcome, err
			}
		}
	}
	if common.RedisEnabled {
		estimate, readErr := service.GetChannelBalanceEstimate(finish, service.ChannelBalanceConfigForMonitor(monitor))
		if readErr == nil {
			_, _ = applyChannelBalanceRealtimePolicy(finish, service.ChannelBalanceConfigForMonitor(monitor), estimate)
		}
	}
	return outcome, nil
}

// Called while holding the account lease, so concurrent channel/task refreshes
// cannot each notify for the same wallet. Failed mail delivery remains retryable.
func notifyUpstreamAccountBalance(ctx context.Context, account model.ChannelMonitorUpstreamAccount, members []model.ChannelRatioMonitor, balance float64, sendEmail func(string, string, string) error) error {
	settings := getChannelMonitorSettings()
	if !settings.EmailNotificationEnabled || settings.NotificationEmail == "" || !channelMonitorEmailNotificationTypeEnabled(settings.EmailNotificationTypes, channelMonitorEmailTypeBalanceWarning) || len(members) == 0 {
		return nil
	}
	var monitor *model.ChannelRatioMonitor
	for i := range members {
		member := &members[i]
		if member.UpstreamBalanceSyncDisabled || member.BalanceWarningThreshold == nil || balance >= *member.BalanceWarningThreshold || member.BalanceAlertNotified {
			continue
		}
		if monitor == nil || *member.BalanceWarningThreshold > *monitor.BalanceWarningThreshold {
			monitor = member
		}
	}
	if monitor == nil {
		return nil
	}
	names := make([]string, 0, len(members))
	for _, member := range members {
		channel, err := model.GetChannelById(member.ChannelId, false)
		if err != nil {
			return err
		}
		names = append(names, channel.Name)
	}
	warning := channelRatioMonitorBalanceWarning{AccountID: account.ID, ChannelId: monitor.ChannelId, UpstreamRevision: monitor.UpstreamRevision,
		ChannelName: account.Name, ChannelRemark: "关联渠道：" + strings.Join(names, "、"), UpstreamType: monitor.UpstreamType, Balance: balance, Threshold: *monitor.BalanceWarningThreshold}
	if err := sendChannelRatioMonitorNotificationEmailForTypes(settings.NotificationEmail, settings.EmailNotificationTypes, nil, []channelRatioMonitorBalanceWarning{warning}, nil, nil, channelRatioMonitorTaskResult{}, nil, sendEmail); err != nil {
		return err
	}
	return model.MarkChannelRatioMonitorBalanceAlertsNotified([]model.ChannelRatioMonitorBalanceAlertGuard{{ChannelId: monitor.ChannelId, UpstreamRevision: monitor.UpstreamRevision, WarningThreshold: *monitor.BalanceWarningThreshold}})
}
