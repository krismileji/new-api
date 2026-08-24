package main

import "fmt"

type endpointSpec struct {
	method string
	path   string
}

var adminViewEndpoints = map[string][]endpointSpec{
	"channels": {
		{method: "GET", path: "/api/channel_monitor/"},
		{method: "GET", path: "/api/channel_monitor/concurrency"},
		{method: "GET", path: "/api/channel_monitor/performance?minutes=15"},
		{method: "GET", path: "/api/channel_monitor/cost?days=2&p=1&summary_only=true"},
		{method: "GET", path: "/api/channel_monitor/success/today"},
		{method: "GET", path: "/api/channel_monitor/schedule?metrics=false"},
	},
	"groups": {
		{method: "GET", path: "/api/channel_monitor/"},
		{method: "GET", path: "/api/channel_monitor/performance?minutes=15"},
		{method: "GET", path: "/api/channel_monitor/cost?days=2&p=1&summary_only=true"},
		{method: "GET", path: "/api/channel_monitor/success/today"},
	},
	"models": {
		{method: "GET", path: "/api/channel_monitor/"},
		{method: "GET", path: "/api/channel_monitor/performance?minutes=15"},
		{method: "GET", path: "/api/channel_monitor/cost?days=2&p=1&summary_only=true"},
		{method: "GET", path: "/api/channel_monitor/success/today"},
	},
	"status-probe": {
		{method: "GET", path: "/api/channel_monitor/status"},
	},
	"model-detection": {
		{method: "GET", path: "/api/channel_monitor/model_detection"},
	},
	"smart-schedule": {
		{method: "GET", path: "/api/channel_monitor/"},
		{method: "GET", path: "/api/channel_monitor/schedule?metrics=false"},
		{method: "GET", path: "/api/channel_monitor/schedule"},
		{method: "GET", path: "/api/channel_monitor/cost?days=2&p=1&summary_only=true"},
		{method: "GET", path: "/api/channel_monitor/success/today"},
	},
	"task-history": {
		{method: "GET", path: "/api/channel_monitor/tasks?kind=ratio&p=1&page_size=25"},
	},
	"smart-schedule-history": {
		{method: "GET", path: "/api/channel_monitor/tasks?kind=schedule&p=1&page_size=20"},
	},
}

type reportConfig struct {
	BaseURL                string   `json:"base_url"`
	Environment            string   `json:"environment"`
	Scenario               string   `json:"scenario"`
	AdminView              string   `json:"admin_view"`
	UserConcurrency        []int    `json:"user_concurrency"`
	AdminUsers             []int    `json:"admin_users"`
	Duration               string   `json:"duration"`
	AdminRefreshInterval   string   `json:"admin_refresh_interval"`
	UserMethod             string   `json:"user_method"`
	UserPath               string   `json:"user_path"`
	AdminEndpoints         []string `json:"admin_endpoints"`
	RequestsPerRefresh     int      `json:"requests_per_admin_refresh"`
	SecretsLoadedFromEnv   bool     `json:"secrets_loaded_from_environment"`
	PublicTestHostAllowed  bool     `json:"public_test_host_allowed"`
	ExternalFaultInjection bool     `json:"external_fault_injection_required"`
	FaultEvidenceSHA256    string   `json:"fault_evidence_sha256,omitempty"`
	RequiredMatrixShape    bool     `json:"required_matrix_shape"`
}

func makeReportConfig(config acceptanceConfig) reportConfig {
	baseURL := "<dry-run：执行时提供 --base-url>"
	if config.baseURL != nil {
		baseURL = config.baseURL.String()
	}
	endpoints := adminViewEndpoints[config.adminView]
	endpointNames := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		endpointNames = append(endpointNames, fmt.Sprintf("%s %s", endpoint.method, endpoint.path))
	}
	return reportConfig{
		BaseURL:                baseURL,
		Environment:            config.environment,
		Scenario:               config.scenarioLabel,
		AdminView:              config.adminView,
		UserConcurrency:        append([]int(nil), config.userConcurrency...),
		AdminUsers:             append([]int(nil), config.adminUsers...),
		Duration:               config.duration.String(),
		AdminRefreshInterval:   config.adminRefreshInterval.String(),
		UserMethod:             config.userMethod,
		UserPath:               config.userPath,
		AdminEndpoints:         endpointNames,
		RequestsPerRefresh:     len(endpoints),
		SecretsLoadedFromEnv:   config.execute,
		PublicTestHostAllowed:  config.allowPublicTestHost,
		ExternalFaultInjection: config.scenarioLabel != "normal",
		FaultEvidenceSHA256:    config.faultEvidenceSHA256,
		RequiredMatrixShape:    isCM10RequiredLoadMatrix(config.userConcurrency, config.adminUsers),
	}
}

func isCM10RequiredLoadMatrix(userConcurrency, adminUsers []int) bool {
	return sameIntegerSet(userConcurrency, []int{100, 500, 1000}) &&
		sameIntegerSet(adminUsers, []int{10, 50})
}

func sameIntegerSet(actual, expected []int) bool {
	if len(actual) != len(expected) {
		return false
	}
	values := make(map[int]struct{}, len(actual))
	for _, value := range actual {
		values[value] = struct{}{}
	}
	if len(values) != len(expected) {
		return false
	}
	for _, value := range expected {
		if _, exists := values[value]; !exists {
			return false
		}
	}
	return true
}

type scenarioPlan struct {
	Name                         string `json:"name"`
	UserConcurrency              int    `json:"user_concurrency"`
	AdminUsers                   int    `json:"admin_users"`
	AdminView                    string `json:"admin_view"`
	ExpectedRequestsPerRefresh   int    `json:"expected_requests_per_refresh"`
	PlannedAdminRefreshes        int64  `json:"planned_admin_refreshes"`
	PlannedAdminRequests         int64  `json:"planned_admin_requests"`
	ExternalFaultInjectionNotice string `json:"external_fault_injection_notice,omitempty"`
}

func buildScenarioPlans(config acceptanceConfig) []scenarioPlan {
	refreshesPerAdmin := int64((config.duration + config.adminRefreshInterval - 1) / config.adminRefreshInterval)
	fanout := len(adminViewEndpoints[config.adminView])
	plans := make([]scenarioPlan, 0, len(config.userConcurrency)*len(config.adminUsers))
	for _, users := range config.userConcurrency {
		for _, admins := range config.adminUsers {
			plan := scenarioPlan{
				Name:                       fmt.Sprintf("%s-users-%d-admins-%d-%s", config.scenarioLabel, users, admins, config.adminView),
				UserConcurrency:            users,
				AdminUsers:                 admins,
				AdminView:                  config.adminView,
				ExpectedRequestsPerRefresh: fanout,
				PlannedAdminRefreshes:      int64(admins) * refreshesPerAdmin,
			}
			plan.PlannedAdminRequests = plan.PlannedAdminRefreshes * int64(fanout)
			if config.scenarioLabel != "normal" {
				plan.ExternalFaultInjectionNotice = "工具不会修改 Redis、数据库或服务进程；请在隔离测试环境外部注入并恢复故障"
			}
			plans = append(plans, plan)
		}
	}
	return plans
}
