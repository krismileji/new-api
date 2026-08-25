package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const (
	channelMonitorPageSnapshotContractVersion = "v1"
	channelMonitorPageSnapshotBypassKey       = "channel_monitor_page_snapshot_bypass"
	channelMonitorPageSnapshotInvalidFilter   = "_invalid_request"

	channelMonitorPageSnapshotOverview          = "overview"
	channelMonitorPageSnapshotCost              = "cost"
	channelMonitorPageSnapshotPerformance       = "performance"
	channelMonitorPageSnapshotSuccess           = "success"
	channelMonitorPageSnapshotSuccessDetail     = "success-detail"
	channelMonitorPageSnapshotSchedule          = "schedule"
	channelMonitorPageSnapshotResponseBodyLimit = 4096
)

type channelMonitorPageSnapshotRequest struct {
	request *http.Request
	params  gin.Params
	keys    map[string]any
}

type channelMonitorPageSnapshotSyncSpec struct {
	page     string
	rawQuery string
	handler  gin.HandlerFunc
}

type channelMonitorPageSnapshotResponseWriter struct {
	gin.ResponseWriter
	body []byte
}

func (writer *channelMonitorPageSnapshotResponseWriter) Write(body []byte) (int, error) {
	writer.capture(body)
	return writer.ResponseWriter.Write(body)
}

func (writer *channelMonitorPageSnapshotResponseWriter) WriteString(body string) (int, error) {
	writer.capture([]byte(body))
	return writer.ResponseWriter.WriteString(body)
}

func (writer *channelMonitorPageSnapshotResponseWriter) capture(body []byte) {
	remaining := channelMonitorPageSnapshotResponseBodyLimit - len(writer.body)
	if remaining <= 0 {
		return
	}
	if len(body) > remaining {
		body = body[:remaining]
	}
	writer.body = append(writer.body, body...)
}

// ChannelMonitorPageSnapshotSyncMiddleware rebuilds the current operator's
// standard monitor snapshots after a successful channel-monitor mutation.
func ChannelMonitorPageSnapshotSyncMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		originalWriter := c.Writer
		captureWriter := &channelMonitorPageSnapshotResponseWriter{
			ResponseWriter: originalWriter,
		}
		c.Writer = captureWriter
		c.Next()
		c.Writer = originalWriter
		if !shouldSyncChannelMonitorPageSnapshots(c, captureWriter.body) {
			return
		}
		syncChannelMonitorPageSnapshots(c)
	}
}

func shouldSyncChannelMonitorPageSnapshots(c *gin.Context, responseBody []byte) bool {
	if c == nil || c.Request == nil || c.Writer == nil ||
		c.Writer.Status() < http.StatusOK || c.Writer.Status() >= http.StatusMultipleChoices {
		return false
	}
	if len(responseBody) > 0 {
		var response struct {
			Success *bool `json:"success"`
		}
		if common.Unmarshal(responseBody, &response) == nil && response.Success != nil && !*response.Success {
			return false
		}
	}
	if c.Request.Method != http.MethodGet {
		return true
	}
	path := c.Request.URL.Path
	for _, suffix := range []string{
		"/test",
		"/update_balance",
		"/fetch_models",
	} {
		if strings.HasSuffix(path, suffix) || strings.Contains(path, suffix+"/") {
			return true
		}
	}
	return false
}

func syncChannelMonitorPageSnapshots(c *gin.Context) {
	if c == nil || c.Request == nil || !common.RedisEnabled {
		return
	}
	specs := []channelMonitorPageSnapshotSyncSpec{
		{
			page:    channelMonitorPageSnapshotOverview,
			handler: GetChannelMonitorOverview,
		},
		{
			page:     channelMonitorPageSnapshotCost,
			rawQuery: "days=2&page=1&summary_only=true",
			handler:  GetChannelMonitorCostOverview,
		},
		{
			page:    channelMonitorPageSnapshotPerformance,
			handler: GetChannelMonitorPerformance,
		},
		{
			page:    channelMonitorPageSnapshotSuccess,
			handler: GetChannelMonitorTodaySuccess,
		},
		{
			page:     channelMonitorPageSnapshotSchedule,
			rawQuery: "metrics=false",
			handler:  GetChannelMonitorSmartScheduleRoutes,
		},
		{
			page:     channelMonitorPageSnapshotSchedule,
			rawQuery: "metrics=true",
			handler:  GetChannelMonitorSmartScheduleRoutes,
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var waitGroup sync.WaitGroup
	for _, spec := range specs {
		spec := spec
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			request := copyChannelMonitorPageSnapshotRequest(c)
			request.request.Method = http.MethodGet
			request.request.URL.RawQuery = spec.rawQuery
			target := channelMonitorPageSnapshotRequestContext(
				request,
				httptest.NewRecorder(),
			)
			query := channelMonitorPageSnapshotQuery(target, spec.page)
			builder := func(buildContext context.Context) (service.ChannelMonitorPageSnapshot, error) {
				return buildChannelMonitorPageSnapshot(buildContext, spec.page, request, spec.handler)
			}
			if _, err := service.RefreshChannelMonitorPageSnapshot(ctx, query, builder); err != nil {
				common.SysError(fmt.Sprintf("同步渠道监控页面快照失败: page=%s err=%v", spec.page, err))
			}
		}()
	}
	waitGroup.Wait()
}

func serveChannelMonitorPageSnapshot(
	c *gin.Context,
	page string,
	handler gin.HandlerFunc,
) bool {
	if c == nil || c.GetBool(channelMonitorPageSnapshotBypassKey) ||
		c.Request == nil || handler == nil {
		return false
	}
	// Production routes pass through RootAuth, which always supplies both
	// values. Missing identity means this is an internal/test invocation and
	// must not share a privileged snapshot under an anonymous key.
	if c.GetInt("id") <= 0 || c.GetInt("role") < common.RoleRootUser {
		return false
	}
	query := channelMonitorPageSnapshotQuery(c, page)
	if len(query.Filters[channelMonitorPageSnapshotInvalidFilter]) > 0 {
		// Preserve the endpoint's normal 4xx validation boundary. Invalid
		// requests must not create unbounded cache identities or submit
		// background builds that can never publish a snapshot.
		return false
	}
	request := copyChannelMonitorPageSnapshotRequest(c)
	builder := func(ctx context.Context) (service.ChannelMonitorPageSnapshot, error) {
		return buildChannelMonitorPageSnapshot(ctx, page, request, handler)
	}
	snapshot, state, err := service.LoadChannelMonitorPageSnapshot(c.Request.Context(), query)
	if err == nil {
		if state == service.ChannelMonitorPageSnapshotStale {
			service.RequestChannelMonitorPageSnapshotRefresh(query, builder)
		}
		writeChannelMonitorPageSnapshot(c, snapshot, state == service.ChannelMonitorPageSnapshotStale)
		return true
	}
	if !errors.Is(err, service.ErrChannelMonitorPageSnapshotMissing) &&
		!errors.Is(err, service.ErrChannelMonitorPageSnapshotUnavailable) {
		writeChannelMonitorPageSnapshotUnavailable(c)
		return true
	}

	// A cold or expired snapshot must never synchronously execute the page
	// handler. These handlers can fan out to several database queries; doing
	// that on every miss would turn a Redis outage or cache expiry into a
	// database request storm. Submit one fenced background rebuild instead and
	// keep the HTTP boundary explicit until a complete snapshot is available.
	service.RequestChannelMonitorPageSnapshotRefresh(query, builder)
	writeChannelMonitorPageSnapshotUnavailable(c)
	return true
}

func channelMonitorPageSnapshotQuery(
	c *gin.Context,
	page string,
) service.ChannelMonitorPageSnapshotQuery {
	filters := make(map[string][]string)
	markInvalid := func() {
		filters[channelMonitorPageSnapshotInvalidFilter] = []string{"true"}
	}
	copyFilter := func(name string, defaultValue string) string {
		value, exists := c.GetQuery(name)
		if !exists || value == "" {
			value = defaultValue
		}
		filters[name] = []string{value}
		return value
	}
	copyTrimmedFilter := func(name string, defaultValue string) string {
		value, exists := c.GetQuery(name)
		value = strings.TrimSpace(value)
		if !exists || value == "" {
			value = defaultValue
		}
		filters[name] = []string{value}
		return value
	}
	copyStrictFilter := func(name string, defaultValue string) string {
		value := copyFilter(name, defaultValue)
		if value != strings.TrimSpace(value) {
			filters[name] = []string{"invalid:" + value}
			markInvalid()
		}
		return value
	}
	copyIntFilter := func(name string, defaultValue int) int {
		raw := copyFilter(name, strconv.Itoa(defaultValue))
		value, err := strconv.Atoi(raw)
		if err != nil {
			filters[name] = []string{"invalid:" + raw}
			markInvalid()
			return defaultValue
		}
		filters[name] = []string{strconv.Itoa(value)}
		return value
	}
	copyBoolFilter := func(name string, defaultValue bool) bool {
		raw := copyFilter(name, strconv.FormatBool(defaultValue))
		value, err := strconv.ParseBool(raw)
		if err != nil {
			filters[name] = []string{"invalid:" + raw}
			markInvalid()
			return defaultValue
		}
		filters[name] = []string{strconv.FormatBool(value)}
		return value
	}
	var windowEnd int64
	switch page {
	case channelMonitorPageSnapshotCost:
		days := copyIntFilter("days", channelMonitorCostDefaultDays)
		if days < 1 || days > channelMonitorCostMaxDays {
			markInvalid()
		}
		channelID := copyIntFilter("channel_id", 0)
		if c.Query("channel_id") != "" && channelID <= 0 {
			filters["channel_id"] = []string{"invalid:" + c.Query("channel_id")}
			markInvalid()
		}
		copyBoolFilter("summary_only", false)
		if copyIntFilter("page", 1) <= 0 {
			markInvalid()
		}
		rawDate := copyStrictFilter("date", "")
		if rawDate != "" {
			todayStart := channelMonitorCostDayStart(common.GetTimestamp())
			parsedDayStart, err := channelMonitorCostDateStart(rawDate)
			if err != nil || parsedDayStart < todayStart-int64(days-1)*channelMonitorCostDaySeconds ||
				parsedDayStart > todayStart {
				markInvalid()
			}
		}
		windowEnd = int64(days) * channelMonitorCostDaySeconds
	case channelMonitorPageSnapshotPerformance:
		settings := getChannelMonitorSettings()
		minutes := defaultChannelMonitorPerformanceMinutes
		filters["range_source"] = []string{channelMonitorPerformanceRangeManual}
		if settings.SmartScheduleEnabled && len(settings.SmartScheduleGroupPolicies) > 0 {
			minutes = settings.SmartSchedulePerformanceWindowMinutes
			filters["minutes"] = []string{strconv.Itoa(minutes)}
			filters["range_source"] = []string{channelMonitorPerformanceRangeSmart}
		} else {
			minutes = copyIntFilter("minutes", defaultChannelMonitorPerformanceMinutes)
			if minutes < minChannelMonitorPerformanceMinutes ||
				minutes > maxChannelMonitorPerformanceMinutes {
				markInvalid()
			}
		}
		windowEnd = int64(minutes) * 60
	case channelMonitorPageSnapshotSuccessDetail:
		minutes := copyIntFilter("minutes", defaultChannelMonitorPerformanceMinutes)
		if minutes < minChannelMonitorPerformanceMinutes ||
			minutes > maxChannelMonitorPerformanceMinutes {
			markInvalid()
		}
		rawChannelID := strings.TrimSpace(c.Query("channel_id"))
		if rawChannelID == "" {
			filters["channel_id"] = []string{"0"}
		} else if channelID, err := strconv.Atoi(rawChannelID); err != nil || channelID <= 0 {
			filters["channel_id"] = []string{"invalid:" + rawChannelID}
			markInvalid()
		} else {
			filters["channel_id"] = []string{strconv.Itoa(channelID)}
		}
		group := copyTrimmedFilter("group", "")
		if (rawChannelID == "" && group == "") || (rawChannelID != "" && group != "") {
			markInvalid()
		}
		if rawChannelID == "" {
			filters["model_name"] = []string{""}
		} else {
			copyTrimmedFilter("model_name", "")
		}
		windowEnd = int64(minutes) * 60
	case channelMonitorPageSnapshotSuccess:
		days := copyIntFilter("days", 1)
		if days < 1 || days > channelMonitorCostMaxDays {
			markInvalid()
		}
		rawDate := copyStrictFilter("date", "")
		if rawDate != "" {
			todayStart := channelMonitorCostDayStart(common.GetTimestamp())
			parsedDayStart, err := channelMonitorCostDateStart(rawDate)
			if err != nil || parsedDayStart < todayStart-int64(days-1)*channelMonitorCostDaySeconds ||
				parsedDayStart > todayStart {
				markInvalid()
			}
		}
		windowEnd = int64(days) * channelMonitorCostDaySeconds
	case channelMonitorPageSnapshotSchedule:
		copyBoolFilter("metrics", true)
	}
	permissionScope, _ := common.Marshal(struct {
		Role        int    `json:"role"`
		UserID      int    `json:"user_id"`
		Group       string `json:"group"`
		UserGroup   string `json:"user_group"`
		AuthVersion int64  `json:"auth_version"`
	}{
		Role:        c.GetInt("role"),
		UserID:      c.GetInt("id"),
		Group:       strings.TrimSpace(c.GetString("group")),
		UserGroup:   strings.TrimSpace(c.GetString("user_group")),
		AuthVersion: c.GetInt64("auth_version"),
	})
	return service.ChannelMonitorPageSnapshotQuery{
		Page:            page,
		Version:         channelMonitorPageSnapshotContractVersion,
		PermissionScope: string(permissionScope),
		WindowStart:     0,
		WindowEnd:       windowEnd,
		Filters:         filters,
	}
}

func copyChannelMonitorPageSnapshotRequest(c *gin.Context) channelMonitorPageSnapshotRequest {
	keys := make(map[string]any, len(c.Keys)+1)
	for key, value := range c.Keys {
		keys[key] = value
	}
	keys[channelMonitorPageSnapshotBypassKey] = true
	return channelMonitorPageSnapshotRequest{
		request: c.Request.Clone(context.Background()),
		params:  append(gin.Params(nil), c.Params...),
		keys:    keys,
	}
}

func channelMonitorPageSnapshotRequestContext(
	request channelMonitorPageSnapshotRequest,
	recorder *httptest.ResponseRecorder,
) *gin.Context {
	target, _ := gin.CreateTestContext(recorder)
	target.Request = request.request.Clone(context.Background())
	target.Params = append(gin.Params(nil), request.params...)
	target.Keys = make(map[string]any, len(request.keys))
	for key, value := range request.keys {
		target.Keys[key] = value
	}
	return target
}

func buildChannelMonitorPageSnapshot(
	ctx context.Context,
	page string,
	request channelMonitorPageSnapshotRequest,
	handler gin.HandlerFunc,
) (service.ChannelMonitorPageSnapshot, error) {
	recorder := httptest.NewRecorder()
	target := channelMonitorPageSnapshotRequestContext(request, recorder)
	target.Request = request.request.Clone(ctx)
	handler(target)
	response := recorder.Result()
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json; charset=utf-8"
	}
	snapshot := service.ChannelMonitorPageSnapshot{
		StatusCode:  response.StatusCode,
		ContentType: contentType,
		Payload:     append([]byte(nil), recorder.Body.Bytes()...),
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return snapshot, service.ErrChannelMonitorPageSnapshotNotCacheable
	}
	payload, generatedAt, revision, dataCutoffAt, eventWatermark, err :=
		normalizeChannelMonitorPageSnapshotPayload(page, snapshot.Payload)
	if err != nil {
		return snapshot, service.ErrChannelMonitorPageSnapshotNotCacheable
	}
	snapshot.Payload = payload
	snapshot.GeneratedAt = generatedAt
	snapshot.Revision = revision
	snapshot.DataCutoffAt = dataCutoffAt
	snapshot.EventWatermark = eventWatermark
	return snapshot, nil
}

func normalizeChannelMonitorPageSnapshotPayload(
	page string,
	payload []byte,
) ([]byte, int64, uint64, int64, uint64, error) {
	var response map[string]json.RawMessage
	if err := common.Unmarshal(payload, &response); err != nil {
		return nil, 0, 0, 0, 0, err
	}
	var success bool
	if err := common.Unmarshal(response["success"], &success); err != nil || !success {
		return nil, 0, 0, 0, 0, service.ErrChannelMonitorPageSnapshotNotCacheable
	}
	var data map[string]json.RawMessage
	if err := common.Unmarshal(response["data"], &data); err != nil {
		return nil, 0, 0, 0, 0, err
	}
	if channelMonitorPageSnapshotContainsSensitiveField(page, payload) {
		return nil, 0, 0, 0, 0, service.ErrChannelMonitorPageSnapshotNotCacheable
	}
	generatedAt := time.Now().Unix()
	data["generated_at"] = channelMonitorPageSnapshotJSON(generatedAt)
	data["stale"] = channelMonitorPageSnapshotJSON(false)
	if _, exists := data["data_cutoff_at"]; !exists {
		data["data_cutoff_at"] = channelMonitorPageSnapshotJSON(int64(0))
	}
	if _, exists := data["event_watermark"]; !exists {
		data["event_watermark"] = channelMonitorPageSnapshotJSON(uint64(0))
	}
	var revision uint64
	if rawRevision, exists := data["revision"]; exists &&
		common.Unmarshal(rawRevision, &revision) != nil {
		return nil, 0, 0, 0, 0, service.ErrChannelMonitorPageSnapshotNotCacheable
	}
	var dataCutoffAt int64
	if common.Unmarshal(data["data_cutoff_at"], &dataCutoffAt) != nil {
		return nil, 0, 0, 0, 0, service.ErrChannelMonitorPageSnapshotNotCacheable
	}
	var eventWatermark uint64
	if common.Unmarshal(data["event_watermark"], &eventWatermark) != nil {
		return nil, 0, 0, 0, 0, service.ErrChannelMonitorPageSnapshotNotCacheable
	}
	dataPayload, err := common.Marshal(data)
	if err != nil {
		return nil, 0, 0, 0, 0, err
	}
	response["data"] = dataPayload
	normalized, err := common.Marshal(response)
	return normalized, generatedAt, revision, dataCutoffAt, eventWatermark, err
}

func channelMonitorPageSnapshotContainsSensitiveField(
	page string,
	raw json.RawMessage,
) bool {
	switch common.GetJsonType(raw) {
	case "object":
		var object map[string]json.RawMessage
		if common.Unmarshal(raw, &object) != nil {
			return true
		}
		for marker, field := range map[string]string{"secret": "value", "body_secret": "body"} {
			var marked bool
			if markerValue, exists := object[marker]; exists &&
				common.Unmarshal(markerValue, &marked) == nil && marked {
				var secretValue string
				if rawValue, exists := object[field]; exists &&
					(common.Unmarshal(rawValue, &secretValue) != nil || secretValue != "") {
					return true
				}
			}
		}
		if rawConfiguredKey, exists := object["key"]; exists {
			var configuredKey string
			if common.Unmarshal(rawConfiguredKey, &configuredKey) != nil {
				return true
			}
			configuredKey = strings.ToLower(strings.TrimSpace(configuredKey))
			dynamicSecret := strings.Contains(configuredKey, "authorization") ||
				strings.Contains(configuredKey, "token") ||
				strings.Contains(configuredKey, "secret") ||
				strings.Contains(configuredKey, "api-key") ||
				strings.Contains(configuredKey, "api_key") ||
				strings.Contains(configuredKey, "apikey") ||
				strings.Contains(configuredKey, "password") ||
				strings.Contains(configuredKey, "passwd") ||
				strings.Contains(configuredKey, "cookie") ||
				strings.Contains(configuredKey, "credential") ||
				strings.Contains(configuredKey, "session")
			if rawValue, valueExists := object["value"]; dynamicSecret && valueExists {
				var configuredValue string
				if common.Unmarshal(rawValue, &configuredValue) != nil || configuredValue != "" {
					return true
				}
			}
		}
		for key, value := range object {
			normalizedKey := strings.NewReplacer("-", "_", " ", "_", ".", "_").Replace(
				strings.ToLower(strings.TrimSpace(key)),
			)
			valueType := common.GetJsonType(value)
			apiKeyField := normalizedKey == "api_key" || normalizedKey == "apikey" ||
				strings.HasSuffix(normalizedKey, "_api_key")
			if apiKeyField && valueType != "boolean" {
				// Cost rows expose only model.ChannelDailyCostAPIKeyIdentity's
				// masked display. No other page may persist an api_key field.
				if page != channelMonitorPageSnapshotCost || normalizedKey != "api_key" {
					return true
				}
				var display string
				if common.Unmarshal(value, &display) != nil {
					return true
				}
				runes := []rune(display)
				masked := len(runes) == 0 ||
					(len(runes) <= 4 && strings.Trim(display, "*") == "") ||
					(len(runes) == 8 && string(runes[2:6]) == "****") ||
					(len(runes) == 18 && string(runes[4:14]) == "**********")
				if !masked {
					return true
				}
			}
			sensitiveField := strings.HasSuffix(normalizedKey, "token") ||
				strings.HasSuffix(normalizedKey, "password") ||
				strings.HasSuffix(normalizedKey, "passwd") ||
				strings.HasSuffix(normalizedKey, "secret") ||
				strings.HasSuffix(normalizedKey, "authorization") ||
				strings.HasSuffix(normalizedKey, "cookie") ||
				strings.HasSuffix(normalizedKey, "credential") ||
				strings.HasSuffix(normalizedKey, "credentials") ||
				strings.HasSuffix(normalizedKey, "session_id") ||
				normalizedKey == "private_key" || normalizedKey == "privatekey"
			// Sanitized response metadata deliberately retains flags such as
			// secret, body_secret, and has_access_token. A boolean cannot carry
			// credential material, while any other value under these names can.
			if sensitiveField && valueType != "boolean" {
				return true
			}
			if channelMonitorPageSnapshotContainsSensitiveField(page, value) {
				return true
			}
		}
	case "array":
		var items []json.RawMessage
		if common.Unmarshal(raw, &items) != nil {
			return true
		}
		for _, item := range items {
			if channelMonitorPageSnapshotContainsSensitiveField(page, item) {
				return true
			}
		}
	}
	return false
}

func channelMonitorPageSnapshotJSON(value any) json.RawMessage {
	payload, _ := common.Marshal(value)
	return payload
}

func writeChannelMonitorPageSnapshot(
	c *gin.Context,
	snapshot service.ChannelMonitorPageSnapshot,
	stale bool,
) {
	payload := snapshot.Payload
	var response map[string]json.RawMessage
	if err := common.Unmarshal(payload, &response); err == nil {
		var data map[string]json.RawMessage
		if err := common.Unmarshal(response["data"], &data); err == nil {
			data["generated_at"] = channelMonitorPageSnapshotJSON(snapshot.GeneratedAt)
			data["data_cutoff_at"] = channelMonitorPageSnapshotJSON(snapshot.DataCutoffAt)
			data["event_watermark"] = channelMonitorPageSnapshotJSON(snapshot.EventWatermark)
			data["stale"] = channelMonitorPageSnapshotJSON(stale)
			if dataPayload, err := common.Marshal(data); err == nil {
				response["data"] = dataPayload
				if normalized, err := common.Marshal(response); err == nil {
					payload = normalized
				}
			}
		}
	}
	c.Data(snapshot.StatusCode, snapshot.ContentType, payload)
}

func writeChannelMonitorPageSnapshotUnavailable(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
		"success": false,
		"code":    "CHANNEL_MONITOR_SNAPSHOT_REFRESHING",
		"message": "监控页面快照正在生成，请稍后重试",
	})
}
