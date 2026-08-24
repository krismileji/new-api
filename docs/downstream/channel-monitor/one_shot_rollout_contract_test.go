package channelmonitor_docs_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type rolloutConfiguration struct {
	Name        string `json:"name"`
	Default     string `json:"default"`
	Kind        string `json:"kind"`
	Implemented bool   `json:"implemented"`
	AliasFor    string `json:"alias_for"`
	Precedence  string `json:"precedence"`
}

type rolloutAlert struct {
	ID            string  `json:"id"`
	Severity      string  `json:"severity"`
	Signal        string  `json:"signal"`
	Condition     string  `json:"condition"`
	Threshold     float64 `json:"threshold"`
	WindowSeconds int     `json:"window_seconds"`
}

type rolloutGate struct {
	ID            string `json:"id"`
	Required      bool   `json:"required"`
	CurrentStatus string `json:"current_status"`
	Evidence      string `json:"evidence"`
}

type rolloutSignalSource struct {
	Signal   string `json:"signal"`
	API      string `json:"api"`
	JSONPath string `json:"json_path"`
	Kind     string `json:"kind"`
}

type alertDeliveryContract struct {
	RequiredLabels      []string `json:"required_labels"`
	RequiredAnnotations []string `json:"required_annotations"`
}

type redisRequirements struct {
	MinimumVersion   string   `json:"minimum_version"`
	RequiredCommands []string `json:"required_commands"`
	Persistence      string   `json:"persistence"`
	EvidenceCommands []string `json:"evidence_commands"`
}

type rolloutContract struct {
	SchemaVersion           int                    `json:"schema_version"`
	RolloutMode             string                 `json:"rollout_mode"`
	PartialRolloutForbidden bool                   `json:"partial_rollout_forbidden"`
	RedisRequirements       redisRequirements      `json:"redis_requirements"`
	Configuration           []rolloutConfiguration `json:"configuration"`
	RequiredSignals         []string               `json:"required_signals"`
	SignalSources           []rolloutSignalSource  `json:"signal_sources"`
	AlertDeliveryContract   alertDeliveryContract  `json:"alert_delivery_contract"`
	Alerts                  []rolloutAlert         `json:"alerts"`
	HardGates               []rolloutGate          `json:"hard_gates"`
}

func loadRolloutContract(t *testing.T) rolloutContract {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	require.True(t, ok)
	payload, err := os.ReadFile(filepath.Join(filepath.Dir(source), "one-shot-rollout-contract.json"))
	require.NoError(t, err)
	var contract rolloutContract
	require.NoError(t, common.Unmarshal(payload, &contract))
	return contract
}

func TestOneShotRolloutContractDeclaresHardGatesAndAlerts(t *testing.T) {
	contract := loadRolloutContract(t)
	assert.Equal(t, 2, contract.SchemaVersion)
	assert.Equal(t, "one_shot", contract.RolloutMode)
	assert.True(t, contract.PartialRolloutForbidden)
	assert.Equal(t, "6.2", contract.RedisRequirements.MinimumVersion)
	assert.Contains(t, contract.RedisRequirements.RequiredCommands, "XAUTOCLAIM")
	assert.Equal(t, "aof_or_managed_equivalent", contract.RedisRequirements.Persistence)
	assert.Contains(t, contract.RedisRequirements.EvidenceCommands, "redis-cli INFO server")
	assert.Contains(t, contract.RedisRequirements.EvidenceCommands, "redis-cli COMMAND INFO XAUTOCLAIM")
	assert.Contains(t, contract.RedisRequirements.EvidenceCommands, "redis-cli INFO persistence")
	require.NotEmpty(t, contract.HardGates)
	assert.ElementsMatch(t, []string{"instance", "chain", "severity"}, contract.AlertDeliveryContract.RequiredLabels)
	assert.ElementsMatch(t, []string{"current_value", "watermark_or_revision", "runbook"}, contract.AlertDeliveryContract.RequiredAnnotations)
	for _, gate := range contract.HardGates {
		assert.True(t, gate.Required, gate.ID)
		assert.Contains(t, []string{"complete", "partial", "missing"}, gate.CurrentStatus, gate.ID)
	}
	blocked := false
	for _, gate := range contract.HardGates {
		blocked = blocked || gate.CurrentStatus != "complete"
	}
	assert.True(t, blocked, "当前契约必须保持上线阻断，直至所有硬闸门提交证据")

	alertSignals := make(map[string]bool, len(contract.Alerts))
	signalSources := make(map[string]rolloutSignalSource, len(contract.SignalSources))
	for _, source := range contract.SignalSources {
		assert.NotEmpty(t, source.Signal)
		assert.NotEmpty(t, source.API, source.Signal)
		assert.NotEmpty(t, source.JSONPath, source.Signal)
		assert.NotEmpty(t, source.Kind, source.Signal)
		signalSources[source.Signal] = source
	}
	for _, alert := range contract.Alerts {
		assert.NotEmpty(t, alert.ID)
		assert.Contains(t, []string{"warning", "critical"}, alert.Severity)
		assert.Positive(t, alert.WindowSeconds, alert.ID)
		alertSignals[alert.Signal] = true
		assert.Contains(t, signalSources, alert.Signal, alert.ID)
	}
	for _, signal := range contract.RequiredSignals {
		assert.Contains(t, signalSources, signal)
	}
	for _, signal := range []string{
		"writer_dropped_events",
		"writer_retry_events",
		"consumer_lag_seconds",
		"page_snapshot_age_seconds",
		"event_watermark",
		"status_probe_snapshot_age_seconds",
		"status_probe_snapshot_revision",
		"model_detection_snapshot_age_seconds",
		"model_detection_snapshot_revision",
		"route_snapshot_age_seconds",
		"route_snapshot_revision",
		"cost_stream_pending_count",
		"cost_stream_unread_count",
		"cost_outbox_pending_count",
		"cost_outbox_oldest_pending_at",
		"cost_outbox_retry_count",
		"cost_publish_failed_count",
		"cost_dead_letter_count",
		"cost_ledger_failed_count",
		"redis_pool_stats.*.timeouts",
	} {
		assert.True(t, alertSignals[signal], signal)
	}

	gateStatus := make(map[string]string, len(contract.HardGates))
	for _, gate := range contract.HardGates {
		gateStatus[gate.ID] = gate.CurrentStatus
	}
	completeGateIDs := []string{
		"cm01_bounded_event_writer",
		"cm02_versioned_route_snapshot",
		"cm03_redis_pool_isolation",
		"cm04_unified_page_snapshot",
		"cm05_redis_task_snapshots",
		"cm06_frontend_refresh_convergence",
		"cm07_recoverable_cost_outbox",
		"cm08_deterministic_failure_recovery_tests",
	}
	for _, gateID := range completeGateIDs {
		assert.Equal(t, "complete", gateStatus[gateID], gateID)
	}
	missingGateIDs := []string{
		"alert_rules_installed",
		"alert_delivery_test",
		"target_redis_compatibility_and_persistence_evidence",
		"cm10_load_matrix_100_500_1000_by_10_50",
		"cm10_real_fault_and_recovery_matrix",
		"cm10_cross_instance_takeover",
		"cm10_zero_difference_reconciliation",
		"cm10_one_shot_rollback_drill",
	}
	for _, gateID := range missingGateIDs {
		assert.Equal(t, "missing", gateStatus[gateID], gateID)
	}
	actualMissingGateIDs := make([]string, 0, len(missingGateIDs))
	for _, gate := range contract.HardGates {
		if gate.CurrentStatus == "missing" {
			actualMissingGateIDs = append(actualMissingGateIDs, gate.ID)
		}
	}
	assert.ElementsMatch(t, missingGateIDs, actualMissingGateIDs)
	assert.Len(t, contract.HardGates, len(completeGateIDs)+len(missingGateIDs))
}

func TestOneShotRolloutContractDefaultsMatchProductionSource(t *testing.T) {
	contract := loadRolloutContract(t)
	_, source, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
	sourceByConfig := map[string]string{
		"REDIS_CLIENT_POOL_ISOLATION":                           "common/redis.go",
		"REDIS_POOL_SIZE":                                       "common/redis.go",
		"REDIS_MONITOR_WRITE_POOL_SIZE":                         "common/redis.go",
		"REDIS_MONITOR_READ_POOL_SIZE":                          "common/redis.go",
		"REDIS_MONITOR_CONSUMER_POOL_SIZE":                      "common/redis.go",
		"REDIS_CONSUMER_POOL_SIZE":                              "common/redis.go",
		"CHANNEL_MONITOR_EVENT_WRITER_QUEUE_CAPACITY":           "service/channel_monitor_event_writer.go",
		"CHANNEL_STATUS_PROBE_OVERVIEW_CACHE_TTL_MS":            "controller/channel_status_probe_overview_cache.go",
		"CHANNEL_STATUS_PROBE_OVERVIEW_STALE_TTL_MS":            "controller/channel_status_probe_overview_cache.go",
		"CHANNEL_STATUS_PROBE_OVERVIEW_REDIS_TTL_SECONDS":       "controller/channel_status_probe_overview_cache.go",
		"CHANNEL_MODEL_DETECTION_OVERVIEW_CACHE_TTL_MS":         "service/channel_model_detection_overview_cache.go",
		"CHANNEL_MODEL_DETECTION_OVERVIEW_STALE_TTL_MS":         "service/channel_model_detection_overview_cache.go",
		"CHANNEL_MODEL_DETECTION_OVERVIEW_REDIS_TTL_SECONDS":    "service/channel_model_detection_overview_cache.go",
		"CHANNEL_DAILY_COST_RELIABLE_OUTBOX":                    "service/channel_daily_cost_outbox.go",
		"CHANNEL_SMART_SCHEDULE_ROUTE_SNAPSHOT_MAX_AGE_SECONDS": "model/channel_smart_schedule_redis_snapshot.go",
	}
	seen := make(map[string]bool, len(contract.Configuration))
	for _, config := range contract.Configuration {
		require.True(t, config.Implemented, config.Name)
		path, exists := sourceByConfig[config.Name]
		require.True(t, exists, config.Name)
		payload, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(path)))
		require.NoError(t, err)
		text := string(payload)
		assert.Contains(t, text, config.Name, config.Name)
		assert.True(t,
			strings.Contains(text, config.Default) ||
				(strings.Contains(text, "defaultRedisMonitorWritePoolSize") && config.Default == "4") ||
				(strings.Contains(text, "defaultRedisMonitorReadPoolSize") && config.Default == "8") ||
				(strings.Contains(text, "defaultRedisMonitorConsumerPoolSize") && config.Default == "4") ||
				(strings.Contains(text, "channelMonitorEventWriterDefaultQueueCapacity") && config.Default == "8192") ||
				(strings.Contains(text, "channelStatusProbeOverviewCacheDefaultTTLMilliseconds") && config.Default == "3000") ||
				(strings.Contains(text, "channelStatusProbeOverviewStaleDefaultTTLMilliseconds") && config.Default == "30000") ||
				(strings.Contains(text, "channelStatusProbeOverviewRedisDefaultTTLSeconds") && config.Default == "60") ||
				(strings.Contains(text, "channelModelDetectionOverviewCacheDefaultTTL") && config.Default == "1000") ||
				(strings.Contains(text, "channelModelDetectionOverviewStaleDefaultTTL") && config.Default == "300000") ||
				(strings.Contains(text, "channelModelDetectionOverviewRedisDefaultTTL") && config.Default == "600") ||
				(strings.Contains(text, "channelSmartScheduleRouteSnapshotMaxAge") && config.Default == "300") ||
				(strings.Contains(text, "GetEnvOrDefaultBool") && config.Default == "true"),
			config.Name,
		)
		seen[config.Name] = true
	}
	legacyConsumerPool := rolloutConfiguration{}
	for _, config := range contract.Configuration {
		if config.Name == "REDIS_CONSUMER_POOL_SIZE" {
			legacyConsumerPool = config
			break
		}
	}
	assert.Equal(t, "legacy_capacity_alias", legacyConsumerPool.Kind)
	assert.Equal(t, "REDIS_MONITOR_CONSUMER_POOL_SIZE", legacyConsumerPool.AliasFor)
	assert.Equal(t, "used_when_primary_is_unset_or_non_positive", legacyConsumerPool.Precedence)
	assert.Equal(t, len(sourceByConfig), len(seen))
}
