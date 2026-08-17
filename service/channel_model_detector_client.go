package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	channelModelDetectorHealthPath    = "/api/health"
	channelModelDetectorBootstrapPath = "/api/bootstrap"
	channelModelDetectorEstimatePath  = "/api/detector/estimate"
	channelModelDetectorStartPath     = "/api/detector/start"
	channelModelDetectorStatusPath    = "/api/detector/status"
	channelModelDetectorReportPath    = "/api/detector/report"
	channelModelDetectorStopPath      = "/api/detector/stop"

	channelModelDetectorHealthTimeout    = 10 * time.Second
	channelModelDetectorBootstrapTimeout = 10 * time.Second
	channelModelDetectorEstimateTimeout  = 30 * time.Second
	channelModelDetectorStartTimeout     = 30 * time.Second
	channelModelDetectorStatusTimeout    = 15 * time.Second
	channelModelDetectorReportTimeout    = 30 * time.Second
	channelModelDetectorStopTimeout      = 15 * time.Second

	// Reports are persisted by a later task with a 1 MiB application limit. The
	// client keeps a slightly larger default for bootstrap/status compatibility
	// while allowing callers to tighten the limit when required.
	channelModelDetectorDefaultMaxResponseBytes int64 = 8 << 20
	channelModelDetectorDefaultMaxReportBytes   int64 = 1 << 20

	channelModelDetectorBootstrapSchemaVersion = 2
	channelModelDetectorReportSchemaMin        = 3
	channelModelDetectorReportSchemaMax        = 4
)

// ChannelModelDetectorPresetConfig is an opaque official detector preset.
// Values are kept as RawMessage so fields added by a newer detector can be
// passed back without this client having to know their schema.
type ChannelModelDetectorPresetConfig map[string]json.RawMessage

// ChannelModelDetectorJSON is used for fields that are intentionally open to
// additions by the independently deployed detector.
type ChannelModelDetectorJSON map[string]json.RawMessage

// ChannelModelDetectorErrorKind classifies transport and contract failures.
type ChannelModelDetectorErrorKind string

const (
	ChannelModelDetectorErrorUnavailable       ChannelModelDetectorErrorKind = "unavailable"
	ChannelModelDetectorErrorBusy              ChannelModelDetectorErrorKind = "busy"
	ChannelModelDetectorErrorIncompatible      ChannelModelDetectorErrorKind = "incompatible"
	ChannelModelDetectorErrorUnauthorized      ChannelModelDetectorErrorKind = "unauthorized"
	ChannelModelDetectorErrorInvalidRequest    ChannelModelDetectorErrorKind = "invalid_request"
	ChannelModelDetectorErrorSessionRequired   ChannelModelDetectorErrorKind = "session_required"
	ChannelModelDetectorErrorSubmissionUnknown ChannelModelDetectorErrorKind = "submission_unknown"
	ChannelModelDetectorErrorResponseTooLarge  ChannelModelDetectorErrorKind = "response_too_large"
)

// ChannelModelDetectorError is returned for an HTTP or contract failure. The
// response body is deliberately reduced to a safe message and never includes
// session, proxy, task, or channel credentials.
type ChannelModelDetectorError struct {
	Kind                ChannelModelDetectorErrorKind
	Endpoint            string
	StatusCode          int
	Message             string
	Err                 error
	ReconciledStatus    *ChannelModelDetectorStatusResponse
	ReconciliationError error
}

func (e *ChannelModelDetectorError) Error() string {
	if e == nil {
		return ""
	}
	message := strings.TrimSpace(e.Message)
	if message == "" && e.Err != nil {
		message = e.Err.Error()
	}
	if message == "" {
		message = string(e.Kind)
	}
	if e.Endpoint == "" {
		return message
	}
	return fmt.Sprintf("GPT-5.6 检测器 %s: %s", e.Endpoint, message)
}

func (e *ChannelModelDetectorError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Is allows callers to use errors.Is with the stable kind sentinels below.
func (e *ChannelModelDetectorError) Is(target error) bool {
	other, ok := target.(*ChannelModelDetectorError)
	return ok && e != nil && other != nil && e.Kind == other.Kind
}

var (
	ErrChannelModelDetectorUnavailable       = &ChannelModelDetectorError{Kind: ChannelModelDetectorErrorUnavailable}
	ErrChannelModelDetectorBusy              = &ChannelModelDetectorError{Kind: ChannelModelDetectorErrorBusy}
	ErrChannelModelDetectorIncompatible      = &ChannelModelDetectorError{Kind: ChannelModelDetectorErrorIncompatible}
	ErrChannelModelDetectorUnauthorized      = &ChannelModelDetectorError{Kind: ChannelModelDetectorErrorUnauthorized}
	ErrChannelModelDetectorSubmissionUnknown = &ChannelModelDetectorError{Kind: ChannelModelDetectorErrorSubmissionUnknown}
)

// ChannelModelDetectorHealthResponse is the small stable portion of /health.
type ChannelModelDetectorHealthResponse struct {
	Status         string                   `json:"status"`
	Binding        string                   `json:"binding"`
	StateTransport string                   `json:"state_transport"`
	Raw            ChannelModelDetectorJSON `json:"-"`
}

// ChannelModelDetectorBootstrapResponse contains the current detector
// capability catalog. SessionToken is process-local and must never be stored
// in a database, report, log, or browser response.
type ChannelModelDetectorBootstrapResponse struct {
	SessionToken       string                                      `json:"-"`
	SchemaVersion      int                                         `json:"schema_version"`
	SinglePresets      map[string]ChannelModelDetectorPresetConfig `json:"single_presets"`
	ContinuousPresets  map[string]ChannelModelDetectorPresetConfig `json:"continuous_presets"`
	Schema             ChannelModelDetectorJSON                    `json:"schema"`
	ProbeCatalog       []ChannelModelDetectorJSON                  `json:"probe_catalog"`
	PendingCustomProbe json.RawMessage                             `json:"pending_custom_probe"`
	Raw                ChannelModelDetectorJSON                    `json:"-"`
}

// Preset returns an opaque preset configuration and whether it was present.
func (b ChannelModelDetectorBootstrapResponse) Preset(name string) (ChannelModelDetectorPresetConfig, bool) {
	preset, ok := b.SinglePresets[name]
	return preset, ok
}

// ChannelModelDetectorEstimateResponse mirrors known fields from
// /api/detector/estimate while retaining the complete response in Raw.
type ChannelModelDetectorEstimateResponse struct {
	TotalRequests                  *int64                   `json:"total_requests"`
	Fixed32KRequests               *int64                   `json:"fixed_32k_requests"`
	ApproximateFixed32KInputTokens *int64                   `json:"approximate_fixed_32k_input_tokens"`
	Continuous                     *bool                    `json:"continuous"`
	Profiles                       *int64                   `json:"profiles"`
	Official                       *bool                    `json:"official"`
	ConfigHash                     string                   `json:"config_hash"`
	Raw                            ChannelModelDetectorJSON `json:"-"`
}

// ChannelModelDetectorStartRequest is the official /start payload. The
// PreviousSessionID field is local reconciliation metadata and is never sent.
type ChannelModelDetectorStartRequest struct {
	BaseURL           string                           `json:"base_url"`
	APIKey            string                           `json:"api_key"`
	Model             string                           `json:"model"`
	ClaimedModel      string                           `json:"claimed_model"`
	RequestModel      string                           `json:"request_model"`
	Config            ChannelModelDetectorPresetConfig `json:"config"`
	ResumeSessionID   string                           `json:"resume_session_id,omitempty"`
	PreviousSessionID string                           `json:"-"`
}

func (r ChannelModelDetectorStartRequest) models() (string, string) {
	claimedModel := strings.TrimSpace(r.ClaimedModel)
	if claimedModel == "" {
		claimedModel = strings.TrimSpace(r.Model)
	}
	requestModel := strings.TrimSpace(r.RequestModel)
	if requestModel == "" {
		requestModel = claimedModel
	}
	return claimedModel, requestModel
}

// MarshalJSON prevents the task API key from being accidentally serialized
// into logs or persistence. Start builds its private wire payload explicitly.
func (r ChannelModelDetectorStartRequest) MarshalJSON() ([]byte, error) {
	type safeStartRequest struct {
		BaseURL           string                           `json:"base_url"`
		Model             string                           `json:"model"`
		ClaimedModel      string                           `json:"claimed_model"`
		RequestModel      string                           `json:"request_model"`
		Config            ChannelModelDetectorPresetConfig `json:"config"`
		ResumeSessionID   string                           `json:"resume_session_id,omitempty"`
		PreviousSessionID string                           `json:"previous_session_id,omitempty"`
	}
	claimedModel, requestModel := r.models()
	return common.Marshal(safeStartRequest{
		BaseURL: r.BaseURL, Model: claimedModel, ClaimedModel: claimedModel, RequestModel: requestModel, Config: r.Config,
		ResumeSessionID: r.ResumeSessionID, PreviousSessionID: r.PreviousSessionID,
	})
}

// ChannelModelDetectorStartResponse is returned after a confirmed start. A
// timeout may still produce a response when status reconciliation confirms a
// matching newly-created session.
type ChannelModelDetectorStartResponse struct {
	Started          bool                                `json:"started"`
	SessionID        string                              `json:"session_id"`
	Official         *bool                               `json:"official"`
	ConfigHash       string                              `json:"config_hash"`
	Reconciled       bool                                `json:"-"`
	ReconciledStatus *ChannelModelDetectorStatusResponse `json:"-"`
	Raw              ChannelModelDetectorJSON            `json:"-"`
}

// ChannelModelDetectorProgress is the stable progress subset returned by the
// detector. Pointer counters distinguish an omitted field from an explicit 0.
type ChannelModelDetectorProgress struct {
	Planned          *int64                   `json:"planned"`
	LogicalCompleted *int64                   `json:"logical_completed"`
	Successful       *int64                   `json:"successful"`
	Errors           *int64                   `json:"errors"`
	Cancelled        *int64                   `json:"cancelled"`
	HTTPAttempts     *int64                   `json:"http_attempts"`
	Retries          *int64                   `json:"retries"`
	Raw              ChannelModelDetectorJSON `json:"-"`
}

// ChannelModelDetectorStatusResponse mirrors the fields used for session
// ownership and progress checks. Unknown fields remain in Raw.
type ChannelModelDetectorStatusResponse struct {
	Status          string                       `json:"status"`
	SessionID       string                       `json:"session_id"`
	UpdatedAt       string                       `json:"updated_at"`
	Mode            string                       `json:"mode"`
	Preset          string                       `json:"preset"`
	Official        *bool                        `json:"official"`
	ConfigHash      string                       `json:"config_hash"`
	ClaimedModel    string                       `json:"claimed_model"`
	RequestModel    string                       `json:"request_model"`
	SafeEndpoint    string                       `json:"safe_endpoint"`
	ReportAvailable *bool                        `json:"report_available"`
	Verdict         string                       `json:"verdict"`
	Error           string                       `json:"error"`
	Progress        ChannelModelDetectorProgress `json:"progress"`
	Raw             ChannelModelDetectorJSON     `json:"-"`
}

// ChannelModelDetectorReportResponse keeps the full report while exposing the
// stable identity fields required by the normalized application contract.
type ChannelModelDetectorReportResponse struct {
	SessionID                string `json:"session_id"`
	SchemaVersion            *int64 `json:"schema_version"`
	ScoringVersion           string `json:"scoring_version"`
	ConfigHash               string `json:"config_hash"`
	BaselineID               string `json:"baseline_id"`
	BaselineSHA256           string `json:"baseline_sha256"`
	BuildHash                string `json:"build_hash"`
	Official                 *bool  `json:"official"`
	ClaimedModel             string `json:"claimed_model"`
	RequestModel             string `json:"request_model"`
	OverallVerdict           string `json:"overall_verdict"`
	TitleCN                  string `json:"title_cn"`
	SubtitleCN               string `json:"subtitle_cn"`
	OutcomeCode              string `json:"outcome_code"`
	JuiceVerdictState        string `json:"juice_verdict_state"`
	FingerprintVerdictState  string `json:"fingerprint_verdict_state"`
	FingerprintModel         string `json:"fingerprint_model"`
	FingerprintClaimMismatch *bool  `json:"fingerprint_claim_mismatch"`
	Candidate                struct {
		BaseURL      string `json:"base_url"`
		Model        string `json:"model"`
		ClaimedModel string `json:"claimed_model"`
		RequestModel string `json:"request_model"`
	} `json:"candidate_configuration_without_key"`
	Raw ChannelModelDetectorJSON `json:"-"`
}

func channelModelDetectorReportCompatibilityError(report ChannelModelDetectorReportResponse, expectedClaimedModel, expectedRequestModel string) error {
	if report.SchemaVersion == nil {
		return errors.New("检测报告未返回 schema_version")
	}
	if *report.SchemaVersion < channelModelDetectorReportSchemaMin || *report.SchemaVersion > channelModelDetectorReportSchemaMax {
		return fmt.Errorf(
			"检测报告 schema_version %d 不受支持，主系统当前支持版本 %d-%d",
			*report.SchemaVersion,
			channelModelDetectorReportSchemaMin,
			channelModelDetectorReportSchemaMax,
		)
	}
	if report.ClaimedModel == "" || report.ClaimedModel != expectedClaimedModel {
		return fmt.Errorf("检测报告 claimed_model %q 与执行快照 %q 不一致", report.ClaimedModel, expectedClaimedModel)
	}
	if report.RequestModel == "" || report.RequestModel != expectedRequestModel {
		return fmt.Errorf(
			"检测报告 request_model %q 与执行快照 %q 不一致，检测器版本可能不支持独立请求模型",
			report.RequestModel,
			expectedRequestModel,
		)
	}
	return nil
}

// MarshalJSON emits the complete official report when it is available. The
// report type itself contains no session token, proxy token, or task API key.
func (r ChannelModelDetectorReportResponse) MarshalJSON() ([]byte, error) {
	if r.Raw != nil {
		return common.Marshal(r.Raw)
	}
	type reportAlias ChannelModelDetectorReportResponse
	return common.Marshal(reportAlias(r))
}

// ChannelModelDetectorStopResponse mirrors the official stop result.
type ChannelModelDetectorStopResponse struct {
	Accepted                *bool                    `json:"accepted"`
	Stopping                *bool                    `json:"stopping"`
	SessionID               string                   `json:"session_id"`
	PreviousStatus          string                   `json:"previous_status"`
	CurrentStatus           string                   `json:"current_status"`
	StopRequestedAt         string                   `json:"stop_requested_at"`
	ActiveRequestsCancelled *int64                   `json:"active_requests_cancelled"`
	Raw                     ChannelModelDetectorJSON `json:"-"`
}

// ChannelModelDetectorCompatibility is the result of a health/bootstrap/
// low-preset estimate contract check.
type ChannelModelDetectorCompatibility struct {
	Health      ChannelModelDetectorHealthResponse
	Bootstrap   ChannelModelDetectorBootstrapResponse
	LowEstimate ChannelModelDetectorEstimateResponse
}

// ChannelModelDetectorClientOptions customizes transport behavior. HTTPClient
// is primarily useful for contract tests; production callers should leave it
// nil so the detector-only private-network transport is used.
type ChannelModelDetectorClientOptions struct {
	HTTPClient       *http.Client
	ProxyToken       string
	MaxResponseBytes int64
	MaxReportBytes   int64
	RequestTimeout   time.Duration
}

// ChannelModelDetectorClient calls only the public HTTP API of the separately
// deployed detector. The session token is held in memory and refreshed by
// every successful Bootstrap call.
type ChannelModelDetectorClient struct {
	baseURL          *url.URL
	httpClient       *http.Client
	proxyToken       string
	maxResponseBytes int64
	maxReportBytes   int64
	requestTimeout   time.Duration

	sessionMu    sync.RWMutex
	sessionToken string
}

// NormalizeChannelModelDetectorURL validates and canonicalizes a detector
// address while preserving an optional reverse-proxy path prefix.
func NormalizeChannelModelDetectorURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", errors.New("检测器地址不能为空")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed == nil {
		return "", errors.New("检测器地址格式无效")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("检测器地址必须使用 http 或 https")
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return "", errors.New("检测器地址缺少主机")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("检测器地址不得包含用户信息、查询参数或片段")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

// NewChannelModelDetectorClient constructs a client for one detector address.
func NewChannelModelDetectorClient(baseURL string, options ...ChannelModelDetectorClientOptions) (*ChannelModelDetectorClient, error) {
	normalized, err := NormalizeChannelModelDetectorURL(baseURL)
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return nil, errors.New("检测器地址格式无效")
	}
	option := ChannelModelDetectorClientOptions{}
	if len(options) > 0 {
		option = options[0]
	}
	client := option.HTTPClient
	if client == nil {
		client = newChannelModelDetectorHTTPClient()
	}
	maxResponseBytes := option.MaxResponseBytes
	if maxResponseBytes <= 0 {
		maxResponseBytes = channelModelDetectorDefaultMaxResponseBytes
	}
	maxReportBytes := option.MaxReportBytes
	if maxReportBytes <= 0 {
		maxReportBytes = channelModelDetectorDefaultMaxReportBytes
	}
	requestTimeout := option.RequestTimeout
	return &ChannelModelDetectorClient{
		baseURL:          parsed,
		httpClient:       client,
		proxyToken:       channelModelDetectorProxyToken(option.ProxyToken),
		maxResponseBytes: maxResponseBytes,
		maxReportBytes:   maxReportBytes,
		requestTimeout:   requestTimeout,
	}, nil
}

func channelModelDetectorProxyToken(configured string) string {
	if token := strings.TrimSpace(configured); token != "" {
		return token
	}
	return strings.TrimSpace(os.Getenv("GPT56_DETECTOR_PROXY_TOKEN"))
}

// NewChannelModelDetectorClientWithHTTPClient is a convenience constructor for
// callers that already own an HTTP transport (including tests).
func NewChannelModelDetectorClientWithHTTPClient(baseURL string, client *http.Client) (*ChannelModelDetectorClient, error) {
	return NewChannelModelDetectorClient(baseURL, ChannelModelDetectorClientOptions{HTTPClient: client})
}

// Bootstrap obtains the current process session token and capability catalog.
// The token is retained only in this client instance's memory.
func (c *ChannelModelDetectorClient) Bootstrap(ctx context.Context) (ChannelModelDetectorBootstrapResponse, error) {
	var response ChannelModelDetectorBootstrapResponse
	raw, err := c.doJSONWithTimeout(ctx, http.MethodGet, channelModelDetectorBootstrapPath, nil, false, c.maxResponseBytes, &response, c.timeout(channelModelDetectorBootstrapTimeout))
	if err != nil {
		c.clearSession()
		return response, err
	}
	if tokenRaw, ok := raw["session_token"]; ok {
		if err := common.Unmarshal(tokenRaw, &response.SessionToken); err != nil {
			c.clearSession()
			return response, c.contractError(channelModelDetectorBootstrapPath, "bootstrap 的 session_token 类型无效", err)
		}
		delete(raw, "session_token")
	}
	response.Raw = raw
	if strings.TrimSpace(response.SessionToken) == "" {
		c.clearSession()
		return response, c.contractError(channelModelDetectorBootstrapPath, "bootstrap 未返回 session_token", nil)
	}
	if response.SchemaVersion != channelModelDetectorBootstrapSchemaVersion {
		c.clearSession()
		return response, c.contractError(
			channelModelDetectorBootstrapPath,
			fmt.Sprintf("bootstrap schema_version %d 不受支持，主系统当前支持版本 %d", response.SchemaVersion, channelModelDetectorBootstrapSchemaVersion),
			nil,
		)
	}
	for _, preset := range []string{"low", "medium", "high"} {
		if _, ok := response.SinglePresets[preset]; !ok {
			c.clearSession()
			return response, c.contractError(channelModelDetectorBootstrapPath, "bootstrap 缺少 "+preset+" 档位", nil)
		}
	}
	c.sessionMu.Lock()
	c.sessionToken = response.SessionToken
	c.sessionMu.Unlock()
	return response, nil
}

// Health queries the detector health endpoint. Health is intentionally public
// and does not send a stale session token.
func (c *ChannelModelDetectorClient) Health(ctx context.Context) (ChannelModelDetectorHealthResponse, error) {
	var response ChannelModelDetectorHealthResponse
	raw, err := c.doJSONWithTimeout(ctx, http.MethodGet, channelModelDetectorHealthPath, nil, false, c.maxResponseBytes, &response, c.timeout(channelModelDetectorHealthTimeout))
	response.Raw = raw
	if err != nil {
		return response, err
	}
	if strings.TrimSpace(response.Status) == "" {
		return response, c.contractError(channelModelDetectorHealthPath, "health 未返回 status", nil)
	}
	return response, nil
}

// Estimate validates and estimates an opaque official configuration.
func (c *ChannelModelDetectorClient) Estimate(ctx context.Context, config ChannelModelDetectorPresetConfig) (ChannelModelDetectorEstimateResponse, error) {
	var response ChannelModelDetectorEstimateResponse
	if err := c.requireSession(); err != nil {
		return response, err
	}
	raw, err := c.doJSONWithTimeout(ctx, http.MethodPost, channelModelDetectorEstimatePath, map[string]any{"config": config}, true, c.maxResponseBytes, &response, c.timeout(channelModelDetectorEstimateTimeout))
	response.Raw = raw
	return response, err
}

// Start submits one official detector session. On a transport timeout it first
// polls status and only returns success when the response identifies a
// matching new session; otherwise the returned error is submission_unknown.
func (c *ChannelModelDetectorClient) Start(ctx context.Context, request ChannelModelDetectorStartRequest) (ChannelModelDetectorStartResponse, error) {
	var response ChannelModelDetectorStartResponse
	if err := c.requireSession(); err != nil {
		return response, err
	}
	claimedModel, requestModel := request.models()
	if strings.TrimSpace(request.BaseURL) == "" || strings.TrimSpace(request.APIKey) == "" || claimedModel == "" || requestModel == "" || request.Config == nil {
		return response, &ChannelModelDetectorError{Kind: ChannelModelDetectorErrorInvalidRequest, Endpoint: channelModelDetectorStartPath, Message: "start 请求缺少必要字段"}
	}
	before, err := c.Status(ctx)
	if err != nil {
		return response, err
	}
	if before.Status == "running" || before.Status == "stopping" {
		return response, &ChannelModelDetectorError{Kind: ChannelModelDetectorErrorBusy, Endpoint: channelModelDetectorStartPath, Message: "检测正在运行或停止中，请等待当前会话结束"}
	}
	if before.Status == "interrupted" && strings.TrimSpace(request.ResumeSessionID) == "" {
		return response, &ChannelModelDetectorError{Kind: ChannelModelDetectorErrorInvalidRequest, Endpoint: channelModelDetectorStartPath, Message: "检测器存在中断会话，必须显式指定 resume_session_id"}
	}
	if request.PreviousSessionID != "" && before.SessionID != "" && request.PreviousSessionID != before.SessionID {
		return response, &ChannelModelDetectorError{Kind: ChannelModelDetectorErrorSubmissionUnknown, Endpoint: channelModelDetectorStartPath, Message: "启动前官方会话已变化，未提交新任务", ReconciledStatus: &before}
	}
	request.PreviousSessionID = before.SessionID
	if request.ResumeSessionID != "" && before.SessionID != "" && request.ResumeSessionID != before.SessionID {
		return response, &ChannelModelDetectorError{Kind: ChannelModelDetectorErrorInvalidRequest, Endpoint: channelModelDetectorStartPath, Message: "resume_session_id 与当前中断会话不一致"}
	}
	normalizedBaseURL, err := NormalizeChannelModelDetectorURL(request.BaseURL)
	if err != nil {
		return response, &ChannelModelDetectorError{Kind: ChannelModelDetectorErrorInvalidRequest, Endpoint: channelModelDetectorStartPath, Message: err.Error(), Err: err}
	}
	payload := struct {
		BaseURL         string                           `json:"base_url"`
		APIKey          string                           `json:"api_key"`
		Model           string                           `json:"model"`
		ClaimedModel    string                           `json:"claimed_model"`
		RequestModel    string                           `json:"request_model"`
		Config          ChannelModelDetectorPresetConfig `json:"config"`
		ResumeSessionID string                           `json:"resume_session_id,omitempty"`
	}{
		BaseURL: normalizedBaseURL, APIKey: request.APIKey, Model: claimedModel,
		ClaimedModel: claimedModel, RequestModel: requestModel,
		Config: request.Config, ResumeSessionID: request.ResumeSessionID,
	}
	submittedAt := time.Now().UTC()
	raw, err := c.doJSONWithTimeout(ctx, http.MethodPost, channelModelDetectorStartPath, payload, true, c.maxResponseBytes, &response, c.timeout(channelModelDetectorStartTimeout))
	response.Raw = raw
	if err == nil {
		expectedHash := detectorConfigHash(request.Config)
		if !response.Started || response.SessionID == "" || expectedHash == "" || response.ConfigHash != expectedHash {
			return response, c.contractError(channelModelDetectorStartPath, "start 响应缺少匹配的 session_id 或 config_hash", nil)
		}
		if request.ResumeSessionID != "" && response.SessionID != request.ResumeSessionID {
			return response, c.contractError(channelModelDetectorStartPath, "恢复会话返回了不同的 session_id", nil)
		}
		return response, nil
	}
	if !isChannelModelDetectorTimeout(err) {
		return response, err
	}

	statusContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.timeout(channelModelDetectorStatusTimeout))
	defer cancel()
	status, statusErr := c.Status(statusContext)
	if statusErr == nil && c.startReconciles(request, normalizedBaseURL, submittedAt, status) {
		response.Started = true
		response.SessionID = status.SessionID
		response.ConfigHash = status.ConfigHash
		response.Reconciled = true
		response.ReconciledStatus = &status
		return response, nil
	}
	unknown := &ChannelModelDetectorError{
		Kind:                ChannelModelDetectorErrorSubmissionUnknown,
		Endpoint:            channelModelDetectorStartPath,
		Message:             "start 响应超时，无法唯一确认官方会话；已停止自动重提",
		Err:                 err,
		ReconciledStatus:    nil,
		ReconciliationError: statusErr,
	}
	if statusErr == nil {
		unknown.ReconciledStatus = &status
	}
	return response, unknown
}

// Status returns the current official session status. It does not require a
// session header because the official endpoint is a read-only public API.
func (c *ChannelModelDetectorClient) Status(ctx context.Context) (ChannelModelDetectorStatusResponse, error) {
	var response ChannelModelDetectorStatusResponse
	raw, err := c.doJSONWithTimeout(ctx, http.MethodGet, channelModelDetectorStatusPath, nil, false, c.maxResponseBytes, &response, c.timeout(channelModelDetectorStatusTimeout))
	response.Raw = raw
	return response, err
}

// Report fetches the complete current report. The raw object is retained for
// downstream normalization, while stable identity fields are decoded above.
func (c *ChannelModelDetectorClient) Report(ctx context.Context) (ChannelModelDetectorReportResponse, error) {
	var response ChannelModelDetectorReportResponse
	raw, err := c.doJSONWithTimeout(ctx, http.MethodGet, channelModelDetectorReportPath, nil, false, c.maxReportBytes, &response, c.timeout(channelModelDetectorReportTimeout))
	response.Raw = raw
	if response.ClaimedModel == "" {
		response.ClaimedModel = response.Candidate.ClaimedModel
		if response.ClaimedModel == "" {
			response.ClaimedModel = response.Candidate.Model
		}
	}
	if response.RequestModel == "" {
		response.RequestModel = response.Candidate.RequestModel
		if response.RequestModel == "" {
			response.RequestModel = response.Candidate.Model
		}
	}
	return response, err
}

// Stop requests cancellation of the current official session. Stop is
// idempotent at the official API and can safely be called more than once.
func (c *ChannelModelDetectorClient) Stop(ctx context.Context) (ChannelModelDetectorStopResponse, error) {
	var response ChannelModelDetectorStopResponse
	if err := c.requireSession(); err != nil {
		return response, err
	}
	raw, err := c.doJSONWithTimeout(ctx, http.MethodPost, channelModelDetectorStopPath, map[string]any{}, true, c.maxResponseBytes, &response, c.timeout(channelModelDetectorStopTimeout))
	response.Raw = raw
	return response, err
}

// CheckCompatibility performs the documented health/bootstrap/low-estimate
// contract check. It refreshes the in-memory session as part of Bootstrap.
func (c *ChannelModelDetectorClient) CheckCompatibility(ctx context.Context) (ChannelModelDetectorCompatibility, error) {
	var result ChannelModelDetectorCompatibility
	health, err := c.Health(ctx)
	result.Health = health
	if err != nil {
		return result, err
	}
	bootstrap, err := c.Bootstrap(ctx)
	result.Bootstrap = bootstrap
	if err != nil {
		return result, err
	}
	low, ok := bootstrap.Preset("low")
	if !ok {
		return result, c.contractError(channelModelDetectorEstimatePath, "bootstrap 缺少 low 档位", nil)
	}
	estimate, err := c.Estimate(ctx, low)
	result.LowEstimate = estimate
	if err != nil {
		return result, err
	}
	if estimate.TotalRequests == nil || *estimate.TotalRequests < 0 || estimate.Fixed32KRequests == nil || *estimate.Fixed32KRequests < 0 {
		return result, c.contractError(channelModelDetectorEstimatePath, "estimate 缺少有效请求量", nil)
	}
	return result, nil
}

func (c *ChannelModelDetectorClient) requireSession() error {
	c.sessionMu.RLock()
	token := strings.TrimSpace(c.sessionToken)
	c.sessionMu.RUnlock()
	if token == "" {
		return &ChannelModelDetectorError{Kind: ChannelModelDetectorErrorSessionRequired, Endpoint: "", Message: "请先获取检测器 bootstrap 会话"}
	}
	return nil
}

func (c *ChannelModelDetectorClient) sessionHeader() string {
	c.sessionMu.RLock()
	defer c.sessionMu.RUnlock()
	return c.sessionToken
}

func (c *ChannelModelDetectorClient) clearSession() {
	c.sessionMu.Lock()
	c.sessionToken = ""
	c.sessionMu.Unlock()
}

func (c *ChannelModelDetectorClient) endpoint(path string) string {
	cloned := *c.baseURL
	basePath := strings.TrimRight(cloned.Path, "/")
	cloned.Path = basePath + path
	cloned.RawPath = ""
	cloned.RawQuery = ""
	cloned.Fragment = ""
	return cloned.String()
}

func (c *ChannelModelDetectorClient) timeout(defaultTimeout time.Duration) time.Duration {
	if c.requestTimeout > 0 {
		return c.requestTimeout
	}
	return defaultTimeout
}

func (c *ChannelModelDetectorClient) doJSONWithTimeout(ctx context.Context, method, path string, payload any, requireSession bool, maxBytes int64, output any, timeout time.Duration) (ChannelModelDetectorJSON, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	requestContext := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		requestContext, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	var body io.Reader
	if payload != nil {
		encoded, err := common.Marshal(payload)
		if err != nil {
			return nil, &ChannelModelDetectorError{Kind: ChannelModelDetectorErrorInvalidRequest, Endpoint: path, Message: "请求编码失败", Err: err}
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(requestContext, method, c.endpoint(path), body)
	if err != nil {
		return nil, &ChannelModelDetectorError{Kind: ChannelModelDetectorErrorInvalidRequest, Endpoint: path, Message: "创建请求失败", Err: err}
	}
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if requireSession {
		token := c.sessionHeader()
		if token == "" {
			return nil, &ChannelModelDetectorError{Kind: ChannelModelDetectorErrorSessionRequired, Endpoint: path, Message: "请先获取检测器 bootstrap 会话"}
		}
		request.Header.Set("X-GPT56-Session", token)
	}
	if c.proxyToken != "" {
		request.Header.Set("Authorization", "Bearer "+c.proxyToken)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, &ChannelModelDetectorError{Kind: ChannelModelDetectorErrorUnavailable, Endpoint: path, Message: "无法连接检测器", Err: err}
	}
	defer response.Body.Close()
	if maxBytes <= 0 {
		maxBytes = channelModelDetectorDefaultMaxResponseBytes
	}
	data, readErr := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if readErr != nil {
		return nil, &ChannelModelDetectorError{Kind: ChannelModelDetectorErrorUnavailable, Endpoint: path, Message: "读取检测器响应失败", Err: readErr}
	}
	if int64(len(data)) > maxBytes {
		return nil, &ChannelModelDetectorError{Kind: ChannelModelDetectorErrorResponseTooLarge, Endpoint: path, Message: "检测器响应过大"}
	}
	var raw ChannelModelDetectorJSON
	if len(bytes.TrimSpace(data)) > 0 {
		if rawErr := common.Unmarshal(data, &raw); rawErr != nil {
			if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
				return nil, &ChannelModelDetectorError{Kind: ChannelModelDetectorErrorIncompatible, Endpoint: path, StatusCode: response.StatusCode, Message: "检测器响应不是有效 JSON", Err: rawErr}
			}
		}
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return raw, c.httpError(path, response.StatusCode, response.Status, data)
	}
	if output != nil && len(data) > 0 {
		if err := common.Unmarshal(data, output); err != nil {
			return raw, &ChannelModelDetectorError{Kind: ChannelModelDetectorErrorIncompatible, Endpoint: path, StatusCode: response.StatusCode, Message: "检测器响应字段格式不兼容", Err: err}
		}
	}
	return raw, nil
}

func (c *ChannelModelDetectorClient) httpError(path string, statusCode int, status string, data []byte) error {
	message := detectorResponseMessage(data)
	kind := ChannelModelDetectorErrorUnavailable
	switch statusCode {
	case http.StatusBadRequest:
		kind = ChannelModelDetectorErrorInvalidRequest
	case http.StatusUnauthorized, http.StatusForbidden:
		kind = ChannelModelDetectorErrorUnauthorized
	case http.StatusConflict, http.StatusLocked:
		kind = ChannelModelDetectorErrorBusy
	case http.StatusNotFound, http.StatusNotImplemented:
		kind = ChannelModelDetectorErrorIncompatible
	case http.StatusRequestEntityTooLarge:
		kind = ChannelModelDetectorErrorResponseTooLarge
	default:
		if statusCode >= http.StatusInternalServerError {
			kind = ChannelModelDetectorErrorUnavailable
		}
	}
	if path == channelModelDetectorStartPath && statusCode == http.StatusBadRequest && isChannelModelDetectorBusyMessage(message) {
		kind = ChannelModelDetectorErrorBusy
	}
	if message == "" {
		message = strings.TrimSpace(status)
	}
	return &ChannelModelDetectorError{Kind: kind, Endpoint: path, StatusCode: statusCode, Message: message}
}

func isChannelModelDetectorBusyMessage(message string) bool {
	normalized := strings.ToLower(strings.TrimSpace(message))
	return strings.Contains(normalized, "already running") ||
		strings.Contains(normalized, "running or stopping") ||
		strings.Contains(normalized, "检测正在运行") ||
		strings.Contains(normalized, "检测正在停止")
}

func (c *ChannelModelDetectorClient) contractError(path, message string, err error) error {
	return &ChannelModelDetectorError{Kind: ChannelModelDetectorErrorIncompatible, Endpoint: path, Message: message, Err: err}
}

func detectorResponseMessage(data []byte) string {
	var value struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if len(data) > 0 && common.Unmarshal(data, &value) == nil {
		message := strings.TrimSpace(value.Error)
		if message == "" {
			message = strings.TrimSpace(value.Message)
		}
		return redactDetectorMessage(message)
	}
	return ""
}

func redactDetectorMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	for _, prefix := range []string{"Bearer ", "bearer ", "sk-"} {
		if index := strings.Index(message, prefix); index >= 0 {
			end := index + len(prefix)
			for end < len(message) && message[end] != ' ' && message[end] != ',' && message[end] != '}' {
				end++
			}
			message = message[:index] + prefix + "<redacted>" + message[end:]
		}
	}
	if len(message) > 600 {
		message = message[:600]
	}
	return message
}

func isChannelModelDetectorTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func (c *ChannelModelDetectorClient) startReconciles(request ChannelModelDetectorStartRequest, normalizedBaseURL string, submittedAt time.Time, status ChannelModelDetectorStatusResponse) bool {
	if strings.TrimSpace(status.SessionID) == "" || status.Status == "" || status.Status == "idle" || status.Status == "error" {
		return false
	}
	if request.ResumeSessionID != "" {
		if status.SessionID != request.ResumeSessionID {
			return false
		}
	} else if request.PreviousSessionID != "" && status.SessionID == request.PreviousSessionID {
		return false
	}
	if expectedHash := detectorConfigHash(request.Config); expectedHash == "" || status.ConfigHash == "" || status.ConfigHash != expectedHash {
		return false
	}
	claimedModel, requestModel := request.models()
	if status.ClaimedModel == "" || status.ClaimedModel != claimedModel {
		return false
	}
	if status.RequestModel != "" && status.RequestModel != requestModel {
		return false
	}
	if status.SafeEndpoint == "" || !sameChannelModelDetectorSafeEndpoint(status.SafeEndpoint, normalizedBaseURL) {
		return false
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, status.UpdatedAt)
	if err != nil {
		return false
	}
	const reconciliationClockSkew = 5 * time.Second
	if updatedAt.Before(submittedAt.Add(-reconciliationClockSkew)) || updatedAt.After(time.Now().UTC().Add(reconciliationClockSkew)) {
		return false
	}
	return true
}

func sameChannelModelDetectorSafeEndpoint(left, right string) bool {
	leftURL, leftErr := url.Parse(strings.TrimRight(strings.TrimSpace(left), "/"))
	rightURL, rightErr := url.Parse(strings.TrimRight(strings.TrimSpace(right), "/"))
	if leftErr != nil || rightErr != nil || leftURL.Hostname() == "" || rightURL.Hostname() == "" {
		return strings.TrimRight(strings.TrimSpace(left), "/") == strings.TrimRight(strings.TrimSpace(right), "/")
	}
	return strings.EqualFold(leftURL.Scheme, rightURL.Scheme) &&
		strings.EqualFold(leftURL.Host, rightURL.Host) &&
		strings.TrimRight(leftURL.Path, "/") == strings.TrimRight(rightURL.Path, "/")
}

func detectorConfigHash(config ChannelModelDetectorPresetConfig) string {
	raw, ok := config["config_hash"]
	if !ok {
		return ""
	}
	var value string
	if common.Unmarshal(raw, &value) != nil {
		return ""
	}
	return strings.TrimSpace(value)
}
