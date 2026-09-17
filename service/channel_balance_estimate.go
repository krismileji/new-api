package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	"github.com/shopspring/decimal"
)

const (
	channelBalanceMicroPerCredit   int64 = 1_000_000
	channelBalanceMaxMicro         int64 = (1<<53 - 1) / 4
	channelBalanceOperationTimeout       = 250 * time.Millisecond
)

var ErrChannelBalanceUnavailable = errors.New("渠道余额预估不可用，等待 Redis 和有效余额同步")

// ChannelBalanceConfig contains no credentials. Account isolates upstream
// accounts; Revision fences configuration changes without repricing old attempts.
type ChannelBalanceConfig struct {
	ChannelID int
	Account   string
	Revision  int64
	Enabled   bool
	Warning   *float64
	Threshold *float64
}

type ChannelBalanceEstimate struct {
	Available            bool    `json:"available"`
	Complete             bool    `json:"complete"`
	UpstreamBalance      float64 `json:"upstream_balance"`
	EstimatedBalance     float64 `json:"estimated_balance"`
	CompletedConsumption float64 `json:"completed_consumption"`
	InFlightConsumption  float64 `json:"in_flight_consumption"`
	UncertainConsumption float64 `json:"uncertain_consumption"`
	InFlightCount        int64   `json:"in_flight_count"`
	AverageCount         int64   `json:"average_count"`
	BudgetCount          int64   `json:"budget_count"`
	UnknownCount         int64   `json:"unknown_count"`
	SyncedAt             int64   `json:"synced_at"`
	LastSampleCount      int64   `json:"last_sample_count"`
	LastEstimateModel    string  `json:"last_estimate_model"`
	LastEstimateAmount   float64 `json:"last_estimate_amount"`
	LastEstimateSource   string  `json:"last_estimate_source"`
	Reason               string  `json:"reason,omitempty"`
	Revision             int64   `json:"-"`
	Epoch                string  `json:"-"`
	BaselineID           string  `json:"-"`
	Decision             string  `json:"-"`
	AppliedDecision      string  `json:"-"`
	Coverage             bool    `json:"-"`
	PolicyBalance        float64 `json:"-"`
	PolicyConsumption    float64 `json:"-"`
}

type ChannelBalanceSync struct {
	Config       ChannelBalanceConfig
	ID           string
	Epoch        string
	IdleCoverage bool
}

func ChannelBalanceConfigForMonitor(monitor model.ChannelRatioMonitor) ChannelBalanceConfig {
	// Price/conversion changes do not change the balance unit or account. They
	// belong in the sample fingerprint, not in the key holding active requests.
	var customAccount any
	if monitor.CustomUpstreamConfig != "" {
		var custom ChannelMonitorCustomUpstreamConfig
		if common.UnmarshalJsonStr(monitor.CustomUpstreamConfig, &custom) == nil {
			request := custom.Balance.Request
			if custom.BalanceReuseRatioRequest {
				request = custom.Ratio.Request
			}
			customAccount = []any{request, custom.Balance.Result, custom.VariableGroupID, custom.VariableRequests, custom.VariableRequest}
		}
	}
	identity, _ := common.Marshal([]any{monitor.UpstreamType, monitor.UpstreamBaseURL,
		monitor.UpstreamAuthType, monitor.UpstreamUserId, monitor.UpstreamAccount,
		monitor.UpstreamAccessToken, customAccount})
	return ChannelBalanceConfig{
		ChannelID: monitor.ChannelId, Account: fmt.Sprintf("%x", sha256.Sum256(identity)),
		Revision: monitor.UpstreamRevision, Enabled: !monitor.UpstreamBalanceSyncDisabled,
		Warning: monitor.BalanceWarningThreshold, Threshold: monitor.BalanceAutoDisableThreshold,
	}
}

func (config ChannelBalanceConfig) key() string {
	return fmt.Sprintf("channel_balance:{%d}:%s", config.ChannelID, config.Account)
}

func channelBalanceMicro(amount float64) (int64, error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) {
		return 0, errors.New("渠道余额预估金额不是有效数字")
	}
	value := decimal.NewFromFloat(amount).Mul(decimal.NewFromInt(channelBalanceMicroPerCredit)).Round(0)
	if value.Abs().GreaterThan(decimal.NewFromInt(channelBalanceMaxMicro)) {
		return 0, errors.New("渠道余额预估金额超出安全范围")
	}
	return value.IntPart(), nil
}

func channelBalanceCostMicro(costNanoCNY int64, conversionFactor float64) (int64, error) {
	if costNanoCNY < 0 || conversionFactor <= 0 || math.IsNaN(conversionFactor) || math.IsInf(conversionFactor, 0) {
		return 0, errors.New("渠道费用或换算系数无效")
	}
	value := decimal.NewFromInt(costNanoCNY).Div(decimal.NewFromInt(model.ChannelDailyCostNanoPerCNY)).
		Div(decimal.NewFromFloat(conversionFactor)).Mul(decimal.NewFromInt(channelBalanceMicroPerCredit)).Round(0)
	if value.GreaterThan(decimal.NewFromInt(channelBalanceMaxMicro)) {
		return 0, errors.New("渠道费用预估超出安全范围")
	}
	return value.IntPart(), nil
}

func runChannelBalanceOperation(ctx context.Context, config ChannelBalanceConfig, operation, eventID, sample string, args ...any) (string, error) {
	client := common.RedisMonitorWriteClient()
	if !common.RedisEnabled || client == nil || config.ChannelID <= 0 || config.Account == "" {
		return "", ErrChannelBalanceUnavailable
	}
	opCtx, cancel := context.WithTimeout(context.WithoutCancel(nonNilContext(ctx)), channelBalanceOperationTimeout)
	defer cancel()
	key := config.key()
	keys := []string{key + ":state", key + ":attempt:" + eventID, key + ":active",
		key + ":samples:" + sample, key + ":sample_costs:" + sample, key + ":sample_sum:" + sample,
		fmt.Sprintf("channel_balance:{%d}:recovery:%s", config.ChannelID, eventID),
		fmt.Sprintf("channel_balance:{%d}:config", config.ChannelID)}
	arguments := append([]any{operation}, args...)
	result, err := channelBalanceScript.Run(opCtx, client, keys, arguments...).Text()
	if err != nil {
		gapCtx, gapCancel := context.WithTimeout(context.Background(), channelBalanceOperationTimeout)
		defer gapCancel()
		_ = channelBalanceMarkGapScript.Run(gapCtx, client, []string{keys[0]}).Err()
		return "", fmt.Errorf("渠道余额预估 Redis 操作失败: %w", err)
	}
	if result == "stale" {
		return "", model.ErrChannelRatioMonitorConfigChanged
	}
	return result, nil
}

var channelBalanceMarkGapScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 1 then
  redis.call('HSET', KEYS[1], 'coverage', '0')
  redis.call('HINCRBY', KEYS[1], 'gap_revision', 1)
end
return 0
`)

func channelBalanceConfigArguments(config ChannelBalanceConfig, token string) ([]any, error) {
	warning, threshold := "", ""
	for _, value := range []struct {
		amount *float64
		target *string
	}{{config.Warning, &warning}, {config.Threshold, &threshold}} {
		if value.amount == nil {
			continue
		}
		amount, err := channelBalanceMicro(*value.amount)
		if err != nil || amount < 0 {
			return nil, errors.New("渠道余额阈值无效")
		}
		*value.target = strconv.FormatInt(amount, 10)
	}
	enabled := "0"
	if config.Enabled {
		enabled = "1"
	}
	encoded, err := common.Marshal(config)
	if err != nil {
		return nil, err
	}
	return []any{token, config.Revision, enabled, warning, threshold, string(encoded)}, nil
}

func ConfigureChannelBalanceEstimate(ctx context.Context, monitor model.ChannelRatioMonitor) error {
	config := ChannelBalanceConfigForMonitor(monitor)
	args, err := channelBalanceConfigArguments(config, common.GetUUID())
	if err != nil {
		return err
	}
	_, err = runChannelBalanceOperation(ctx, config, "configure", "", "", args...)
	return err
}

func BeginChannelBalanceSync(ctx context.Context, monitor model.ChannelRatioMonitor) (ChannelBalanceSync, error) {
	sync := ChannelBalanceSync{Config: ChannelBalanceConfigForMonitor(monitor), ID: common.GetUUID()}
	args, err := channelBalanceConfigArguments(sync.Config, sync.ID)
	if err != nil {
		return sync, err
	}
	sync.IdleCoverage = ChannelBalanceHasIdleRequestCoverage(ctx, monitor.ChannelId)
	sync.Epoch, err = runChannelBalanceOperation(ctx, sync.Config, "sync_begin", "", "", args...)
	return sync, err
}

func CommitChannelBalanceSync(ctx context.Context, sync ChannelBalanceSync, balance float64, coverageComplete bool) (ChannelBalanceEstimate, error) {
	amount, err := channelBalanceMicro(balance)
	if err != nil {
		return ChannelBalanceEstimate{}, err
	}
	coverage := "0"
	if coverageComplete {
		coverage = "1"
	}
	// Existing complete coverage is not evidence that old reservations ended.
	// Reclaim active orphans only after actual requests were idle on both sides
	// of this successful upstream query, including direct channel tests.
	idle := "0"
	if coverageComplete && sync.IdleCoverage && ChannelBalanceHasIdleRequestCoverage(ctx, sync.Config.ChannelID) {
		idle = "1"
	}
	raw, err := runChannelBalanceOperation(ctx, sync.Config, "sync_commit", "", "",
		sync.ID, sync.Config.Revision, amount, coverage, idle)
	if err != nil {
		return ChannelBalanceEstimate{}, err
	}
	return decodeChannelBalanceEstimate(raw)
}

func FailChannelBalanceSync(ctx context.Context, sync ChannelBalanceSync) error {
	_, err := runChannelBalanceOperation(ctx, sync.Config, "sync_fail", "", "", sync.ID, sync.Config.Revision)
	return err
}

func GetChannelBalanceEstimate(ctx context.Context, config ChannelBalanceConfig) (ChannelBalanceEstimate, error) {
	raw, err := runChannelBalanceOperation(ctx, config, "read", "", "", common.GetUUID())
	if err != nil {
		return ChannelBalanceEstimate{}, err
	}
	return decodeChannelBalanceEstimate(raw)
}

// After lost Redis state, only a confirmed idle interval can establish that
// no older, untracked transport remains. Do not infer this from a missing key
// alone: the existing concurrency runtime must also have initialized.
func ChannelBalanceHasIdleRequestCoverage(ctx context.Context, channelID int) bool {
	// A replica may still report idle after another node acquired a lease.
	client := common.RedisMonitorWriteClient()
	if !common.RedisEnabled || client == nil {
		return false
	}
	opCtx, cancel := context.WithTimeout(nonNilContext(ctx), channelBalanceOperationTimeout)
	defer cancel()
	pipeline := client.Pipeline()
	loaded := pipeline.HGet(opCtx, channelConcurrencyRedisConfigKey, channelConcurrencyRedisLoadedField)
	active := pipeline.ZCard(opCtx, channelConcurrencyRedisActivePrefix+strconv.Itoa(channelID))
	probes := pipeline.ZCard(opCtx, fmt.Sprintf("channel_balance:{%d}:probe_active", channelID))
	_, err := pipeline.Exec(opCtx)
	return err == nil && loaded.Val() == "1" && active.Val() == 0 && probes.Val() == 0
}

func decodeChannelBalanceEstimate(raw string) (ChannelBalanceEstimate, error) {
	if raw == "" {
		return ChannelBalanceEstimate{}, ErrChannelBalanceUnavailable
	}
	var fields map[string]string
	if err := common.UnmarshalJsonStr(raw, &fields); err != nil {
		return ChannelBalanceEstimate{}, err
	}
	return channelBalanceEstimateFromFields(fields)
}

func channelBalanceEstimateFromFields(fields map[string]string) (ChannelBalanceEstimate, error) {
	numbers := make(map[string]int64, len(fields))
	for _, field := range []string{"revision", "balance", "baseline_end", "completed", "uncertain", "inflight", "active",
		"average_active", "budget_active", "unknown_active", "unknown_completed", "last_samples", "last_amount"} {
		if fields[field] == "" {
			continue
		}
		value, err := strconv.ParseInt(fields[field], 10, 64)
		if err != nil || value > channelBalanceMaxMicro || value < -channelBalanceMaxMicro || (field != "balance" && value < 0) {
			return ChannelBalanceEstimate{}, errors.New("渠道余额预估 Redis 数据无效")
		}
		numbers[field] = value
	}
	if numbers["uncertain"] > numbers["completed"] || numbers["unknown_active"] > numbers["active"] ||
		numbers["average_active"]+numbers["budget_active"] > numbers["active"] {
		return ChannelBalanceEstimate{}, errors.New("渠道余额预估 Redis 汇总不一致")
	}
	scale := float64(channelBalanceMicroPerCredit)
	estimate := ChannelBalanceEstimate{
		Available:            fields["balance"] != "" && fields["damaged"] != "1",
		UpstreamBalance:      float64(numbers["balance"]) / scale,
		EstimatedBalance:     float64(numbers["balance"]-numbers["completed"]-numbers["inflight"]) / scale,
		PolicyBalance:        float64(numbers["balance"]-numbers["completed"]-numbers["inflight"]+numbers["uncertain"]) / scale,
		PolicyConsumption:    float64(numbers["completed"]+numbers["inflight"]-numbers["uncertain"]) / scale,
		CompletedConsumption: float64(numbers["completed"]) / scale, InFlightConsumption: float64(numbers["inflight"]) / scale,
		UncertainConsumption: float64(numbers["uncertain"]) / scale,
		InFlightCount:        numbers["active"], AverageCount: numbers["average_active"], BudgetCount: numbers["budget_active"],
		UnknownCount: numbers["unknown_active"] + numbers["unknown_completed"], SyncedAt: numbers["baseline_end"] / 1000,
		Revision: numbers["revision"], Epoch: fields["epoch"], BaselineID: fields["baseline_id"],
		Decision: fields["decision"], AppliedDecision: fields["applied_decision"], Coverage: fields["coverage"] == "1",
		LastSampleCount: numbers["last_samples"], LastEstimateModel: fields["last_model"],
		LastEstimateAmount: float64(numbers["last_amount"]) / scale, LastEstimateSource: fields["last_source"],
	}
	estimate.Complete = estimate.Available && estimate.Coverage && estimate.UnknownCount == 0 &&
		estimate.UncertainConsumption == 0 && fields["sync_failed"] != "1"
	switch {
	case !estimate.Available:
		estimate.Reason = "等待有效余额同步，或预估数据超出安全范围"
	case !estimate.Coverage:
		estimate.Reason = "请求覆盖不完整，等待确认进行中请求并重新同步"
	case estimate.UnknownCount > 0:
		estimate.Reason = "部分请求费用尚未确认"
	case fields["sync_failed"] == "1":
		estimate.Reason = "最近一次余额同步失败，等待重新同步"
	case estimate.UncertainConsumption > 0:
		estimate.Reason = "余额查询期间有请求完成，部分消费是否已扣待下次同步确认"
	}
	return estimate, nil
}

// The overview reads one bounded aggregate hash per configured channel in a
// pipeline. It never scans attempts, model samples, logs or daily cost tables.
func GetChannelBalanceEstimates(ctx context.Context, monitors []model.ChannelRatioMonitor) map[int]ChannelBalanceEstimate {
	estimates := make(map[int]ChannelBalanceEstimate)
	client := common.RedisMonitorReadClient()
	if !common.RedisEnabled || client == nil {
		return estimates
	}
	opCtx, cancel := context.WithTimeout(nonNilContext(ctx), channelBalanceOperationTimeout)
	defer cancel()
	pipeline := client.Pipeline()
	commands := make(map[int]*redis.StringStringMapCmd)
	for _, monitor := range monitors {
		if monitor.UpstreamType == "" || monitor.UpstreamBalanceSyncDisabled {
			continue
		}
		config := ChannelBalanceConfigForMonitor(monitor)
		commands[monitor.ChannelId] = pipeline.HGetAll(opCtx, config.key()+":state")
	}
	_, _ = pipeline.Exec(opCtx)
	for _, monitor := range monitors {
		command := commands[monitor.ChannelId]
		if command == nil {
			continue
		}
		fields, err := command.Result()
		if err != nil {
			estimates[monitor.ChannelId] = ChannelBalanceEstimate{Reason: "预估数据暂不可用"}
			continue
		}
		estimate, err := channelBalanceEstimateFromFields(fields)
		if err != nil || estimate.Revision != monitor.UpstreamRevision || monitor.UpstreamBalance == nil ||
			math.Abs(estimate.UpstreamBalance-*monitor.UpstreamBalance) > 0.000001 {
			estimate = ChannelBalanceEstimate{Reason: "等待同一轮余额同步完成"}
		}
		estimates[monitor.ChannelId] = estimate
	}
	return estimates
}

// Prevent an older HTTP response from overwriting a newer persisted snapshot.
// The lease covers only the short Redis-install/database-save section, not HTTP.
func LockChannelBalanceSyncResult(ctx context.Context, config ChannelBalanceConfig) (func(), error) {
	client := common.RedisMonitorWriteClient()
	if !common.RedisEnabled || client == nil {
		return nil, ErrChannelBalanceUnavailable
	}
	token := common.GetUUID()
	key := config.key() + ":persist"
	locked, err := client.SetNX(nonNilContext(ctx), key, token, 10*time.Second).Result()
	if err != nil {
		return nil, err
	}
	if !locked {
		return nil, errors.New("该渠道正在保存另一笔余额同步结果")
	}
	return func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), channelBalanceOperationTimeout)
		defer cancel()
		_ = channelBalanceReleaseLockScript.Run(releaseCtx, client, []string{key}, token).Err()
	}, nil
}

var channelBalanceReleaseLockScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then return redis.call('DEL', KEYS[1]) end
return 0
`)
