package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/tidwall/gjson"
	"golang.org/x/net/http/httpguts"
)

const (
	ChannelMonitorCustomRefreshAlways    = "always"
	ChannelMonitorCustomRefreshOnFailure = "on_failure"
	maxChannelMonitorVariableRequests    = 8
	maxChannelMonitorVariables           = 32
)

type ChannelMonitorCustomVariable struct {
	Name      string `json:"name"`
	ValuePath string `json:"value_path,omitempty"`
	Value     string `json:"value,omitempty"`
	HasValue  bool   `json:"has_value,omitempty"`
}

type ChannelMonitorCustomVariableRequest struct {
	ID            string                            `json:"id"`
	Name          string                            `json:"name"`
	BaseURL       string                            `json:"base_url,omitempty"`
	RefreshPolicy string                            `json:"refresh_policy"`
	Request       ChannelMonitorCustomRequestConfig `json:"request"`
	ResponseType  string                            `json:"response_type"`
	Variables     []ChannelMonitorCustomVariable    `json:"variables"`
}

// Legacy configurations are upgraded in memory and written in the new format
// on the next save, without changing the database schema.
type ChannelMonitorCustomLegacyVariableRequest struct {
	Name          string                            `json:"name"`
	Value         string                            `json:"value,omitempty"`
	HasValue      bool                              `json:"has_value,omitempty"`
	BaseURL       string                            `json:"base_url,omitempty"`
	RefreshPolicy string                            `json:"refresh_policy"`
	Request       ChannelMonitorCustomRequestConfig `json:"request"`
	Result        ChannelMonitorCustomResultConfig  `json:"result"`
}

var channelMonitorCustomVariableName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
var channelMonitorCustomVariableRequestID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var channelMonitorCustomVariablePlaceholder = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

func channelMonitorCustomVariableRequests(config ChannelMonitorCustomUpstreamConfig) []ChannelMonitorCustomVariableRequest {
	if config.VariableRequest == nil {
		return config.VariableRequests
	}
	legacy := config.VariableRequest
	return []ChannelMonitorCustomVariableRequest{{
		ID: "legacy-variable", Name: "获取 " + legacy.Name, BaseURL: legacy.BaseURL,
		RefreshPolicy: legacy.RefreshPolicy, Request: legacy.Request, ResponseType: legacy.Result.ResponseType,
		Variables: []ChannelMonitorCustomVariable{{Name: legacy.Name, Value: legacy.Value, HasValue: legacy.HasValue, ValuePath: legacy.Result.ValuePath}},
	}}
}

func normalizeChannelMonitorCustomVariableRequests(config ChannelMonitorCustomUpstreamConfig, existing *ChannelMonitorCustomUpstreamConfig) ([]ChannelMonitorCustomVariableRequest, error) {
	if config.VariableRequest != nil && len(config.VariableRequests) > 0 {
		return nil, errors.New("不能同时提交旧版和新版独立请求配置")
	}
	requests := channelMonitorCustomVariableRequests(config)
	if len(requests) > maxChannelMonitorVariableRequests {
		return nil, errors.New("独立请求不能超过 8 个")
	}
	var savedRequests []ChannelMonitorCustomVariableRequest
	if existing != nil {
		savedRequests = channelMonitorCustomVariableRequests(*existing)
	}
	ids, names := make(map[string]bool), make(map[string]bool)
	var normalized []ChannelMonitorCustomVariableRequest
	for _, request := range requests {
		request.ID = strings.TrimSpace(request.ID)
		request.Name = strings.TrimSpace(request.Name)
		if !channelMonitorCustomVariableRequestID.MatchString(request.ID) || ids[request.ID] {
			return nil, errors.New("独立请求标识无效或重复")
		}
		ids[request.ID] = true
		if request.Name == "" || utf8.RuneCountInString(request.Name) > 80 || !httpguts.ValidHeaderFieldValue(request.Name) {
			return nil, errors.New("独立请求名称不能为空，不能包含控制字符且最多 80 个字符")
		}
		if request.RefreshPolicy != ChannelMonitorCustomRefreshAlways && request.RefreshPolicy != ChannelMonitorCustomRefreshOnFailure {
			return nil, fmt.Errorf("独立请求 %s 的刷新策略必须是每次更新或更新失败时", request.Name)
		}
		request.BaseURL = strings.TrimSpace(request.BaseURL)
		var err error
		if request.BaseURL != "" {
			request.BaseURL, err = NormalizeChannelMonitorCustomBaseURL(request.BaseURL)
			if err != nil {
				return nil, err
			}
		}
		var saved *ChannelMonitorCustomVariableRequest
		for index := range savedRequests {
			if savedRequests[index].ID == request.ID && savedRequests[index].BaseURL == request.BaseURL {
				saved = &savedRequests[index]
				break
			}
		}
		var savedRequest *ChannelMonitorCustomRequestConfig
		if saved != nil {
			savedRequest = &saved.Request
		}
		request.Request, err = normalizeChannelMonitorCustomRequest(request.Request, savedRequest)
		if err != nil {
			return nil, fmt.Errorf("独立请求 %s 配置无效: %w", request.Name, err)
		}
		for _, entries := range [][]ChannelMonitorCustomKeyValue{request.Request.Query, request.Request.Headers, request.Request.Form} {
			for _, item := range entries {
				if item.ValueTemplate != "" {
					return nil, errors.New("独立请求不能引用变量模板")
				}
			}
		}
		request.ResponseType = strings.TrimSpace(request.ResponseType)
		if request.ResponseType == "" {
			request.ResponseType = ChannelMonitorCustomResponseJSON
		}
		if request.ResponseType != ChannelMonitorCustomResponseJSON && request.ResponseType != ChannelMonitorCustomResponseText {
			return nil, errors.New("独立请求响应格式必须是 JSON 或纯文本")
		}
		if len(request.Variables) == 0 || len(request.Variables) > maxChannelMonitorVariables {
			return nil, errors.New("每个独立请求需配置 1 到 32 个变量")
		}
		request.Variables = append([]ChannelMonitorCustomVariable(nil), request.Variables...)
		for index := range request.Variables {
			variable := &request.Variables[index]
			variable.Name = strings.TrimSpace(variable.Name)
			if !channelMonitorCustomVariableName.MatchString(variable.Name) {
				return nil, errors.New("变量名须以字母或下划线开头，只能包含字母、数字和下划线，最多 64 个字符")
			}
			if names[variable.Name] {
				return nil, fmt.Errorf("变量 %s 重复，不同独立请求的变量也不能重名", variable.Name)
			}
			names[variable.Name] = true
			variable.ValuePath = strings.TrimSpace(variable.ValuePath)
			if request.ResponseType == ChannelMonitorCustomResponseText {
				variable.ValuePath = ""
			} else if variable.ValuePath == "" || len(variable.ValuePath) > maxChannelMonitorCustomResultPath {
				return nil, fmt.Errorf("变量 %s 的 JSON 取值路径不能为空且不能超过 512 个字符", variable.Name)
			}
			if saved != nil && variable.Value == "" && variable.HasValue {
				for _, old := range saved.Variables {
					if old.Name == variable.Name {
						variable.Value = old.Value
						break
					}
				}
			}
			if len(variable.Value) > maxChannelMonitorCustomValueLength || !httpguts.ValidHeaderFieldValue(variable.Value) {
				return nil, fmt.Errorf("变量 %s 的值不能包含控制字符且不能超过 8192 字节", variable.Name)
			}
			variable.HasValue = variable.Value != ""
		}
		normalized = append(normalized, request)
	}
	if len(names) > maxChannelMonitorVariables {
		return nil, errors.New("所有独立请求的变量合计不能超过 32 个")
	}
	return normalized, nil
}

func validateChannelMonitorCustomTemplates(config ChannelMonitorCustomUpstreamConfig) error {
	names := make(map[string]string)
	for _, request := range config.VariableRequests {
		for _, variable := range request.Variables {
			names[variable.Name] = variable.Value
		}
	}
	for _, metric := range []ChannelMonitorCustomMetricConfig{config.Ratio, config.Balance} {
		if metric.Request == nil {
			continue
		}
		for _, item := range metric.Request.Form {
			if item.ValueTemplate != "" {
				return errors.New("变量模板仅支持查询参数和请求头")
			}
		}
		for _, entries := range [][]ChannelMonitorCustomKeyValue{metric.Request.Query, metric.Request.Headers} {
			for _, item := range entries {
				if item.ValueTemplate == "" {
					continue
				}
				matches := channelMonitorCustomVariablePlaceholder.FindAllStringSubmatch(item.ValueTemplate, -1)
				if len(matches) == 0 || strings.ContainsAny(channelMonitorCustomVariablePlaceholder.ReplaceAllString(item.ValueTemplate, ""), "{}") {
					return errors.New("变量模板格式无效，请使用 {{变量名}}")
				}
				for _, match := range matches {
					if _, exists := names[match[1]]; !exists {
						return fmt.Errorf("变量 %s 未配置独立请求", match[1])
					}
				}
				rendered := channelMonitorCustomVariablePlaceholder.ReplaceAllStringFunc(item.ValueTemplate, func(match string) string {
					return names[channelMonitorCustomVariablePlaceholder.FindStringSubmatch(match)[1]]
				})
				if len(rendered) > maxChannelMonitorCustomValueLength || !httpguts.ValidHeaderFieldValue(rendered) {
					return errors.New("应用变量后的参数值无效或超过 8192 字节")
				}
			}
		}
	}
	return nil
}

func resolveChannelMonitorCustomTemplates(config ChannelMonitorCustomUpstreamConfig, ratio, balance bool) (ChannelMonitorCustomUpstreamConfig, error) {
	values := make(map[string]string)
	for _, request := range config.VariableRequests {
		for _, variable := range request.Variables {
			values[variable.Name] = variable.Value
		}
	}
	resolved := config
	for index, metric := range []*ChannelMonitorCustomMetricConfig{&resolved.Ratio, &resolved.Balance} {
		if metric.Request == nil || (index == 0 && !ratio && !(balance && config.BalanceReuseRatioRequest)) || (index == 1 && !balance) {
			continue
		}
		request := *metric.Request
		request.Query = append([]ChannelMonitorCustomKeyValue(nil), request.Query...)
		request.Headers = append([]ChannelMonitorCustomKeyValue(nil), request.Headers...)
		for _, entries := range [][]ChannelMonitorCustomKeyValue{request.Query, request.Headers} {
			for index := range entries {
				item := &entries[index]
				if item.ValueTemplate == "" {
					continue
				}
				for _, match := range channelMonitorCustomVariablePlaceholder.FindAllStringSubmatch(item.ValueTemplate, -1) {
					if values[match[1]] == "" {
						return resolved, fmt.Errorf("变量 %s 尚无可用值，请先获取或填写", match[1])
					}
				}
				item.Value = channelMonitorCustomVariablePlaceholder.ReplaceAllStringFunc(item.ValueTemplate, func(match string) string {
					return values[channelMonitorCustomVariablePlaceholder.FindStringSubmatch(match)[1]]
				})
				item.ValueTemplate, item.Secret, request.HideResponse = "", true, true
				if len(item.Value) > maxChannelMonitorCustomValueLength || !httpguts.ValidHeaderFieldValue(item.Value) {
					return resolved, errors.New("应用变量后的参数值无效或超过 8192 字节")
				}
			}
		}
		metric.Request = &request
	}
	return resolved, nil
}

// FetchChannelMonitorCustomVariables returns only the mapped values for one
// draft request; the editor decides whether to save the result.
func FetchChannelMonitorCustomVariables(ctx context.Context, config ChannelMonitorUpstreamConfig, requestID string) ([]ChannelMonitorCustomVariable, error) {
	normalized, err := NormalizeChannelMonitorCustomUpstreamConfig(config.CustomConfig)
	if err != nil {
		return nil, err
	}
	for _, request := range normalized.VariableRequests {
		if request.ID != requestID {
			continue
		}
		client, err := NewSSRFProtectedHTTPClientWithProxy(config.Proxy)
		if err != nil {
			return nil, err
		}
		requestContext, cancel := context.WithTimeout(ctx, channelMonitorUpstreamRequestTimeout(config.RequestTimeout))
		defer cancel()
		return fetchChannelMonitorCustomVariables(requestContext, client, config.BaseURL, request)
	}
	return nil, errors.New("独立请求不存在，请重新选择")
}

func fetchChannelMonitorCustomVariables(ctx context.Context, client *http.Client, baseURL string, config ChannelMonitorCustomVariableRequest) ([]ChannelMonitorCustomVariable, error) {
	if config.BaseURL != "" {
		baseURL = config.BaseURL
	}
	request := config.Request
	request.HideResponse = true
	response, err := requestChannelMonitorCustomUpstream(ctx, client, baseURL, request, false)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("独立请求 %s 未完成: %w", config.Name, ctx.Err())
		}
		if response.debug != nil {
			return nil, fmt.Errorf("独立请求 %s 失败，HTTP 状态码 %d", config.Name, response.debug.StatusCode)
		}
		return nil, fmt.Errorf("独立请求 %s 失败，请检查地址、网络和请求配置", config.Name)
	}
	if config.ResponseType == ChannelMonitorCustomResponseJSON && !gjson.ValidBytes(response.body) {
		return nil, fmt.Errorf("独立请求 %s 响应不是有效 JSON", config.Name)
	}
	variables := append([]ChannelMonitorCustomVariable(nil), config.Variables...)
	for index := range variables {
		variable := &variables[index]
		value := strings.TrimSpace(string(response.body))
		if config.ResponseType == ChannelMonitorCustomResponseJSON {
			extracted := gjson.GetBytes(response.body, variable.ValuePath)
			if !extracted.Exists() || (extracted.Type != gjson.String && extracted.Type != gjson.Number && extracted.Type != gjson.True && extracted.Type != gjson.False) {
				return nil, fmt.Errorf("变量 %s 的结果路径不存在或不是字符串、数字、布尔值", variable.Name)
			}
			value = extracted.String()
		}
		if strings.TrimSpace(value) == "" || len(value) > maxChannelMonitorCustomValueLength || !httpguts.ValidHeaderFieldValue(value) {
			return nil, fmt.Errorf("变量 %s 的返回值为空、过长或包含非法字符", variable.Name)
		}
		variable.Value, variable.HasValue = value, true
	}
	return variables, nil
}
