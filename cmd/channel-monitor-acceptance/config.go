package main

import (
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const executionConfirmation = "CM10_LOAD_TEST"

var supportedScenarioLabels = map[string]struct{}{
	"normal":               {},
	"redis-high-latency":   {},
	"redis-restart":        {},
	"redis-unavailable":    {},
	"writer-queue-full":    {},
	"database-unavailable": {},
	"recovered":            {},
}

const usageText = `渠道监控 CM-10 并发与故障验收工具

默认仅输出 dry-run 计划，不发送网络请求。

常用参数：
  --base-url URL                 测试环境根 URL
  --execute                      实际发送请求
  --confirm=CM10_LOAD_TEST       执行确认
  --environment=test|staging    目标环境
  --user-concurrency=100,500,1000
  --admin-users=10,50
  --duration=30s
  --admin-view=channels
  --user-body-file=PATH
  --fault-evidence-file=PATH     非 normal 场景的故障注入/恢复原始证据
  --report-file=PATH

令牌默认从 CM10_USER_TOKEN 和 CM10_ADMIN_TOKEN 环境变量读取。
`

type acceptanceConfig struct {
	baseURL                  *url.URL
	execute                  bool
	environment              string
	confirmation             string
	allowPublicTestHost      bool
	userConcurrency          []int
	adminUsers               []int
	duration                 time.Duration
	adminRefreshInterval     time.Duration
	requestTimeout           time.Duration
	userMethod               string
	userPath                 string
	userBody                 []byte
	userToken                string
	adminToken               string
	adminCookie              string
	adminView                string
	metricsPath              string
	prometheusPath           string
	prometheusMetricNames    []string
	scenarioLabel            string
	faultEvidenceSHA256      string
	reportFile               string
	maxLatencySamples        int
	maxUserP95               time.Duration
	maxUserP99               time.Duration
	maxUserErrorRatePercent  float64
	maxAdminP95              time.Duration
	maxAdminP99              time.Duration
	maxAdminErrorRatePercent float64
	maxFanoutMismatch        int64
	maxWriterDroppedDelta    float64
}

type configDependencies struct {
	getenv   func(string) string
	readFile func(string) ([]byte, error)
}

func parseConfig(
	args []string,
	getenv func(string) string,
	readFile func(string) ([]byte, error),
) (acceptanceConfig, error) {
	dependencies := configDependencies{getenv: getenv, readFile: readFile}
	var config acceptanceConfig
	var baseURL string
	var userConcurrency string
	var adminUsers string
	var userBodyFile string
	var userTokenEnv string
	var adminTokenEnv string
	var adminCookieEnv string
	var prometheusMetricNames string
	var faultEvidenceFile string

	flags := flag.NewFlagSet("channel-monitor-acceptance", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&baseURL, "base-url", "", "测试环境根 URL")
	flags.BoolVar(&config.execute, "execute", false, "实际发送请求；默认仅输出 dry-run 计划")
	flags.StringVar(&config.environment, "environment", "test", "目标环境：test 或 staging")
	flags.StringVar(&config.confirmation, "confirm", "", "执行确认字符串")
	flags.BoolVar(&config.allowPublicTestHost, "allow-public-test-host", false, "允许访问公网测试域名")
	flags.StringVar(&userConcurrency, "user-concurrency", "100,500,1000", "用户请求并发列表")
	flags.StringVar(&adminUsers, "admin-users", "10,50", "管理员并发列表")
	flags.DurationVar(&config.duration, "duration", 30*time.Second, "每组场景持续时间")
	flags.DurationVar(&config.adminRefreshInterval, "admin-refresh-interval", time.Second, "合成负载中每个管理员的刷新间隔")
	flags.DurationVar(&config.requestTimeout, "request-timeout", 30*time.Second, "单请求超时")
	flags.StringVar(&config.userMethod, "user-method", "POST", "用户请求方法")
	flags.StringVar(&config.userPath, "user-path", "/v1/chat/completions", "用户请求路径")
	flags.StringVar(&userBodyFile, "user-body-file", "", "用户请求 JSON 文件")
	flags.StringVar(&userTokenEnv, "user-token-env", "CM10_USER_TOKEN", "用户令牌环境变量名")
	flags.StringVar(&adminTokenEnv, "admin-token-env", "CM10_ADMIN_TOKEN", "管理员访问令牌环境变量名")
	flags.StringVar(&adminCookieEnv, "admin-cookie-env", "CM10_ADMIN_COOKIE", "可选管理员 Cookie 环境变量名")
	flags.StringVar(&config.adminView, "admin-view", "channels", "管理员刷新视图")
	flags.StringVar(&config.metricsPath, "metrics-path", "/api/channel_monitor/", "监控快照路径")
	flags.StringVar(&config.prometheusPath, "prometheus-path", "", "可选 Prometheus 指标路径")
	flags.StringVar(&prometheusMetricNames, "prometheus-metrics", "", "要采集的 Prometheus 指标名")
	flags.StringVar(&config.scenarioLabel, "scenario", "normal", "故障或正常场景标签")
	flags.StringVar(&faultEvidenceFile, "fault-evidence-file", "", "非 normal 场景的故障注入/恢复原始证据文件")
	flags.StringVar(&config.reportFile, "report-file", "", "可选 JSON 报告路径")
	flags.IntVar(&config.maxLatencySamples, "max-latency-samples", 1_000_000, "每类请求最大延迟样本数")
	flags.DurationVar(&config.maxUserP95, "max-user-p95", 0, "用户请求 P95 上限；0 表示不检查")
	flags.DurationVar(&config.maxUserP99, "max-user-p99", 0, "用户请求 P99 上限；0 表示不检查")
	flags.Float64Var(&config.maxUserErrorRatePercent, "max-user-error-rate", -1, "用户错误率百分比上限；负数表示不检查")
	flags.DurationVar(&config.maxAdminP95, "max-admin-p95", 0, "管理员请求 P95 上限；0 表示不检查")
	flags.DurationVar(&config.maxAdminP99, "max-admin-p99", 0, "管理员请求 P99 上限；0 表示不检查")
	flags.Float64Var(&config.maxAdminErrorRatePercent, "max-admin-error-rate", -1, "管理员错误率百分比上限；负数表示不检查")
	flags.Int64Var(&config.maxFanoutMismatch, "max-fanout-mismatch", 0, "页面请求扇出数量允许偏差")
	flags.Float64Var(&config.maxWriterDroppedDelta, "max-writer-dropped-delta", 0, "writer 丢弃事件允许增量；负数表示不检查")
	if err := flags.Parse(args); err != nil {
		return acceptanceConfig{}, err
	}

	var err error
	config.userConcurrency, err = parsePositiveIntList(userConcurrency, 5000)
	if err != nil {
		return acceptanceConfig{}, fmt.Errorf("user-concurrency: %w", err)
	}
	config.adminUsers, err = parsePositiveIntList(adminUsers, 500)
	if err != nil {
		return acceptanceConfig{}, fmt.Errorf("admin-users: %w", err)
	}
	config.prometheusMetricNames = parseStringList(prometheusMetricNames)
	config.userMethod = strings.ToUpper(strings.TrimSpace(config.userMethod))
	config.adminView = strings.TrimSpace(config.adminView)
	config.scenarioLabel = strings.TrimSpace(config.scenarioLabel)
	if _, ok := supportedScenarioLabels[config.scenarioLabel]; !ok {
		return acceptanceConfig{}, fmt.Errorf("不支持的 scenario %q", config.scenarioLabel)
	}
	if _, ok := adminViewEndpoints[config.adminView]; !ok {
		return acceptanceConfig{}, fmt.Errorf("不支持的 admin-view %q", config.adminView)
	}
	if config.duration <= 0 || config.duration > 30*time.Minute {
		return acceptanceConfig{}, errors.New("duration 必须大于 0 且不超过 30 分钟")
	}
	if config.adminRefreshInterval <= 0 || config.requestTimeout <= 0 {
		return acceptanceConfig{}, errors.New("刷新间隔和请求超时必须大于 0")
	}
	if config.maxLatencySamples < 1000 {
		return acceptanceConfig{}, errors.New("max-latency-samples 不能小于 1000")
	}
	if err := validateErrorRateLimit("max-user-error-rate", config.maxUserErrorRatePercent); err != nil {
		return acceptanceConfig{}, err
	}
	if err := validateErrorRateLimit("max-admin-error-rate", config.maxAdminErrorRatePercent); err != nil {
		return acceptanceConfig{}, err
	}
	if err := validateOptionalFiniteLimit("max-writer-dropped-delta", config.maxWriterDroppedDelta); err != nil {
		return acceptanceConfig{}, err
	}
	if config.maxFanoutMismatch < 0 {
		return acceptanceConfig{}, errors.New("max-fanout-mismatch 不能为负数")
	}
	for name, path := range map[string]string{
		"user-path":       config.userPath,
		"metrics-path":    config.metricsPath,
		"prometheus-path": config.prometheusPath,
	} {
		if path != "" && !isSafeRelativePath(path) {
			return acceptanceConfig{}, fmt.Errorf("%s 必须是以 / 开头的同源相对路径", name)
		}
	}

	if baseURL != "" {
		config.baseURL, err = url.Parse(baseURL)
		if err != nil || config.baseURL.Scheme == "" || config.baseURL.Host == "" {
			return acceptanceConfig{}, errors.New("base-url 必须是完整的 http/https URL")
		}
		if config.baseURL.Scheme != "http" && config.baseURL.Scheme != "https" {
			return acceptanceConfig{}, errors.New("base-url 仅支持 http 或 https")
		}
		if config.baseURL.User != nil {
			return acceptanceConfig{}, errors.New("base-url 不得包含用户名或密码")
		}
		if config.baseURL.RawQuery != "" || config.baseURL.Fragment != "" {
			return acceptanceConfig{}, errors.New("base-url 不得包含查询参数或片段")
		}
	}

	if !config.execute {
		return config, nil
	}
	if config.baseURL == nil {
		return acceptanceConfig{}, errors.New("execute 模式必须显式提供 base-url")
	}
	if config.environment != "test" && config.environment != "staging" {
		return acceptanceConfig{}, errors.New("execute 模式只允许 test 或 staging 环境")
	}
	if config.confirmation != executionConfirmation {
		return acceptanceConfig{}, fmt.Errorf("execute 模式必须提供 --confirm=%s", executionConfirmation)
	}
	if !isPrivateTestHost(config.baseURL.Hostname()) && !config.allowPublicTestHost {
		return acceptanceConfig{}, errors.New("公网测试域名必须显式提供 --allow-public-test-host")
	}
	if config.userMethod != "POST" && config.userMethod != "GET" {
		return acceptanceConfig{}, errors.New("user-method 仅支持 GET 或 POST")
	}
	if config.userMethod == "POST" {
		if userBodyFile == "" {
			return acceptanceConfig{}, errors.New("POST 压测必须提供 user-body-file")
		}
		config.userBody, err = dependencies.readFile(userBodyFile)
		if err != nil {
			return acceptanceConfig{}, fmt.Errorf("读取 user-body-file: %w", err)
		}
		if len(config.userBody) == 0 || len(config.userBody) > 4<<20 {
			return acceptanceConfig{}, errors.New("user-body-file 必须大于 0 且不超过 4 MiB")
		}
		if common.GetJsonType(config.userBody) != "object" {
			return acceptanceConfig{}, errors.New("user-body-file 必须是 JSON 对象")
		}
		var body map[string]any
		if err := common.Unmarshal(config.userBody, &body); err != nil {
			return acceptanceConfig{}, fmt.Errorf("user-body-file 不是有效 JSON: %w", err)
		}
	}
	if strings.TrimSpace(userTokenEnv) == "" || strings.TrimSpace(adminTokenEnv) == "" || strings.TrimSpace(adminCookieEnv) == "" {
		return acceptanceConfig{}, errors.New("令牌和 Cookie 环境变量名不能为空")
	}
	config.userToken = strings.TrimSpace(dependencies.getenv(userTokenEnv))
	config.adminToken = strings.TrimSpace(dependencies.getenv(adminTokenEnv))
	config.adminCookie = strings.TrimSpace(dependencies.getenv(adminCookieEnv))
	if config.userToken == "" {
		return acceptanceConfig{}, fmt.Errorf("环境变量 %s 未设置", userTokenEnv)
	}
	if config.adminToken == "" {
		return acceptanceConfig{}, fmt.Errorf("环境变量 %s 未设置", adminTokenEnv)
	}
	if config.scenarioLabel != "normal" {
		if strings.TrimSpace(faultEvidenceFile) == "" {
			return acceptanceConfig{}, errors.New("非 normal 场景必须提供 fault-evidence-file，不能只记录场景标签")
		}
		evidence, evidenceErr := dependencies.readFile(faultEvidenceFile)
		if evidenceErr != nil {
			return acceptanceConfig{}, fmt.Errorf("读取 fault-evidence-file: %w", evidenceErr)
		}
		if len(evidence) == 0 || len(evidence) > 4<<20 {
			return acceptanceConfig{}, errors.New("fault-evidence-file 必须大于 0 且不超过 4 MiB")
		}
		config.faultEvidenceSHA256 = fmt.Sprintf("%x", sha256.Sum256(evidence))
	}
	return config, nil
}

func validateErrorRateLimit(name string, value float64) error {
	if value < 0 {
		if math.IsInf(value, 0) || math.IsNaN(value) {
			return fmt.Errorf("%s 必须是有限数值；负数才表示不检查", name)
		}
		return nil
	}
	if math.IsNaN(value) || math.IsInf(value, 0) || value > 100 {
		return fmt.Errorf("%s 必须是 0..100 内的有限百分比，负数表示不检查", name)
	}
	return nil
}

func validateOptionalFiniteLimit(name string, value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("%s 必须是有限数值，负数表示不检查", name)
	}
	return nil
}

func parsePositiveIntList(value string, maximum int) ([]int, error) {
	parts := strings.Split(value, ",")
	result := make([]int, 0, len(parts))
	seen := make(map[int]struct{}, len(parts))
	for _, part := range parts {
		number, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || number <= 0 || number > maximum {
			return nil, fmt.Errorf("%q 不是 1..%d 内的整数", part, maximum)
		}
		if _, exists := seen[number]; exists {
			continue
		}
		seen[number] = struct{}{}
		result = append(result, number)
	}
	if len(result) == 0 {
		return nil, errors.New("列表不能为空")
	}
	return result, nil
}

func parseStringList(value string) []string {
	var result []string
	for _, part := range strings.Split(value, ",") {
		if item := strings.TrimSpace(part); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func isPrivateTestHost(host string) bool {
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") || strings.HasSuffix(strings.ToLower(host), ".test") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}

func isSafeRelativePath(path string) bool {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return false
	}
	parsed, err := url.Parse(path)
	return err == nil && parsed.Scheme == "" && parsed.Host == ""
}
