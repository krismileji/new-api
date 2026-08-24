package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseConfigDryRunBuildsFullAcceptanceMatrixWithoutSecrets(t *testing.T) {
	config, err := parseConfig(
		[]string{"--admin-view=status-probe"},
		func(string) string { return "" },
		func(string) ([]byte, error) { return nil, errors.New("must not read files") },
	)
	require.NoError(t, err)
	assert.False(t, config.execute)
	assert.Equal(t, []int{100, 500, 1000}, config.userConcurrency)
	assert.Equal(t, []int{10, 50}, config.adminUsers)
	assert.True(t, makeReportConfig(config).RequiredMatrixShape)

	plans := buildScenarioPlans(config)
	require.Len(t, plans, 6)
	for _, plan := range plans {
		assert.Equal(t, 1, plan.ExpectedRequestsPerRefresh)
		assert.Equal(t, plan.PlannedAdminRefreshes, plan.PlannedAdminRequests)
	}
}

func TestParseConfigExecuteRequiresExplicitTestConfirmation(t *testing.T) {
	_, err := parseConfig(
		[]string{
			"--execute",
			"--base-url=http://127.0.0.1:3000",
			"--user-body-file=request.json",
		},
		func(name string) string { return "configured" },
		func(string) ([]byte, error) { return []byte(`{"model":"fixture"}`), nil },
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), executionConfirmation)
}

func TestParseConfigExecuteLoadsSecretsOnlyFromEnvironment(t *testing.T) {
	config, err := parseConfig(
		[]string{
			"--execute",
			"--confirm=" + executionConfirmation,
			"--base-url=http://127.0.0.1:3000",
			"--user-body-file=request.json",
			"--user-concurrency=2",
			"--admin-users=1",
		},
		func(name string) string {
			switch name {
			case "CM10_USER_TOKEN":
				return "user-secret"
			case "CM10_ADMIN_TOKEN":
				return "admin-secret"
			default:
				return ""
			}
		},
		func(path string) ([]byte, error) {
			assert.Equal(t, "request.json", path)
			return []byte(`{"model":"fixture"}`), nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, "user-secret", config.userToken)
	assert.Equal(t, "admin-secret", config.adminToken)
	assert.Equal(t, []byte(`{"model":"fixture"}`), config.userBody)
	assert.False(t, makeReportConfig(config).RequiredMatrixShape)
	reportJSON, err := common.Marshal(makeReportConfig(config))
	require.NoError(t, err)
	assert.NotContains(t, string(reportJSON), "user-secret")
	assert.NotContains(t, string(reportJSON), "admin-secret")
}

func TestParseConfigRejectsPublicHostWithoutExplicitOverride(t *testing.T) {
	_, err := parseConfig(
		[]string{
			"--execute",
			"--confirm=" + executionConfirmation,
			"--base-url=https://example.com",
			"--user-body-file=request.json",
		},
		func(string) string { return "configured" },
		func(string) ([]byte, error) { return []byte(`{}`), nil },
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "allow-public-test-host")
}

func TestParseConfigRejectsBaseURLQueryThatCouldLeakCredentials(t *testing.T) {
	_, err := parseConfig(
		[]string{"--base-url=http://127.0.0.1:3000?token=secret"},
		func(string) string { return "" },
		func(string) ([]byte, error) { return nil, errors.New("must not read files") },
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "查询参数")
}

func TestParseConfigRejectsCrossOriginRequestPath(t *testing.T) {
	_, err := parseConfig(
		[]string{"--user-path=https://unexpected.example/v1/chat/completions"},
		func(string) string { return "" },
		func(string) ([]byte, error) { return nil, errors.New("must not read files") },
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "同源相对路径")
}

func TestParseConfigRejectsUnknownScenario(t *testing.T) {
	_, err := parseConfig(
		[]string{"--scenario=redis-maybe-broken"},
		func(string) string { return "" },
		func(string) ([]byte, error) { return nil, errors.New("must not read files") },
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不支持的 scenario")
}

func TestParseConfigExecuteFaultScenarioRequiresEvidenceAndReportsHash(t *testing.T) {
	baseArgs := []string{
		"--execute",
		"--confirm=" + executionConfirmation,
		"--base-url=http://127.0.0.1:3000",
		"--user-body-file=request.json",
		"--user-concurrency=2",
		"--admin-users=1",
		"--scenario=redis-unavailable",
	}
	getenv := func(name string) string {
		if name == "CM10_USER_TOKEN" || name == "CM10_ADMIN_TOKEN" {
			return "configured"
		}
		return ""
	}
	readFile := func(path string) ([]byte, error) {
		switch path {
		case "request.json":
			return []byte(`{"model":"fixture"}`), nil
		case "fault.txt":
			return []byte("redis-cli ping failed at 2026-08-24T00:00:00Z"), nil
		default:
			return nil, fmt.Errorf("unexpected file: %s", path)
		}
	}

	_, err := parseConfig(baseArgs, getenv, readFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fault-evidence-file")

	config, err := parseConfig(append(baseArgs, "--fault-evidence-file=fault.txt"), getenv, readFile)
	require.NoError(t, err)
	assert.Len(t, config.faultEvidenceSHA256, 64)
	reportJSON, err := common.Marshal(makeReportConfig(config))
	require.NoError(t, err)
	assert.Contains(t, string(reportJSON), config.faultEvidenceSHA256)
	assert.NotContains(t, string(reportJSON), "redis-cli ping failed")
}

func TestParseConfigRejectsNonFiniteOrOutOfRangeSafetyLimits(t *testing.T) {
	for _, testCase := range []struct {
		name string
		args []string
	}{
		{name: "user infinity", args: []string{"--max-user-error-rate=+Inf"}},
		{name: "admin nan", args: []string{"--max-admin-error-rate=NaN"}},
		{name: "error rate over 100", args: []string{"--max-user-error-rate=100.1"}},
		{name: "writer infinity", args: []string{"--max-writer-dropped-delta=+Inf"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := parseConfig(testCase.args, func(string) string { return "" }, func(string) ([]byte, error) {
				return nil, errors.New("must not read files")
			})
			require.Error(t, err)
		})
	}
}
