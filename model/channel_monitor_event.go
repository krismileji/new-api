package model

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/google/uuid"
)

const (
	ChannelMonitorEventSchemaVersion           = 1
	ChannelMonitorEventMaxIdentityLength       = 128
	ChannelMonitorEventMaxNameLength           = 255
	ChannelMonitorEventMaxOtherJsonBytes       = 64 * 1024
	ChannelMonitorSchedulingEligibleContextKey = "channel_monitor_scheduling_eligible"
)

type ChannelMonitorEventSource string

const (
	ChannelMonitorEventSourceBusiness       ChannelMonitorEventSource = "business"
	ChannelMonitorEventSourceStatusProbe    ChannelMonitorEventSource = "status_probe"
	ChannelMonitorEventSourceSmartProbe     ChannelMonitorEventSource = "smart_probe"
	ChannelMonitorEventSourceManualTest     ChannelMonitorEventSource = "manual_test"
	ChannelMonitorEventSourceModelDetection ChannelMonitorEventSource = "model_detection"
)

type ChannelMonitorEventOutcome string

const (
	ChannelMonitorEventOutcomeSuccess    ChannelMonitorEventOutcome = "success"
	ChannelMonitorEventOutcomeFailure    ChannelMonitorEventOutcome = "failure"
	ChannelMonitorEventOutcomeUnresolved ChannelMonitorEventOutcome = "unresolved"
	ChannelMonitorEventOutcomeCanceled   ChannelMonitorEventOutcome = "canceled"
)

type ChannelMonitorEventCostStatus string

const (
	ChannelMonitorEventCostNone       ChannelMonitorEventCostStatus = "none"
	ChannelMonitorEventCostSettled    ChannelMonitorEventCostStatus = "settled"
	ChannelMonitorEventCostUnresolved ChannelMonitorEventCostStatus = "unresolved"
)

// ChannelMonitorEvent is the shared request-level input for channel monitor
// projections. Optional scalar measurements use pointers so an explicit zero
// remains distinguishable from an absent measurement.
type ChannelMonitorEvent struct {
	EventId       string `json:"event_id"`
	EventSequence uint64 `json:"event_sequence"`
	SchemaVersion int    `json:"schema_version"`
	OccurredAt    int64  `json:"occurred_at"`
	CreatedAt     int64  `json:"created_at"`
	ProcessedAt   int64  `json:"processed_at,omitempty"`

	ChannelId  int    `json:"channel_id"`
	GroupName  string `json:"group,omitempty"`
	ModelName  string `json:"model,omitempty"`
	RequestId  string `json:"request_id,omitempty"`
	NodeId     string `json:"node_id,omitempty"`
	APIKeyId   int    `json:"api_key_id,omitempty"`
	APIKeyName string `json:"api_key_name,omitempty"`

	Source     ChannelMonitorEventSource     `json:"source"`
	Outcome    ChannelMonitorEventOutcome    `json:"outcome"`
	CostStatus ChannelMonitorEventCostStatus `json:"cost_status"`

	IsStream                  bool `json:"is_stream"`
	IsRetryAttempt            bool `json:"is_retry_attempt"`
	IsFinalAttempt            bool `json:"is_final_attempt"`
	FinalRetrySummary         bool `json:"final_retry_summary"`
	RequestDispatched         bool `json:"request_dispatched"`
	SchedulingEligible        bool `json:"scheduling_eligible"`
	RuntimeProtectionEligible bool `json:"runtime_protection_eligible"`

	StatusCode        *int     `json:"status_code,omitempty"`
	ErrorType         string   `json:"error_type,omitempty"`
	ErrorCode         string   `json:"error_code,omitempty"`
	ErrorMessage      string   `json:"error_message,omitempty"`
	FirstTokenMs      *float64 `json:"first_token_ms,omitempty"`
	TPS               *float64 `json:"tps,omitempty"`
	PromptTokens      *int64   `json:"prompt_tokens,omitempty"`
	CompletionTokens  *int64   `json:"completion_tokens,omitempty"`
	CacheReadTokens   *int64   `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens  *int64   `json:"cache_write_tokens,omitempty"`
	InputTokens       *int64   `json:"input_tokens,omitempty"`
	AttemptDurationMs *int64   `json:"attempt_duration_ms,omitempty"`

	SettledCostNanoCNY    int64  `json:"settled_cost_nano_cny,omitempty"`
	UnresolvedCostNanoCNY int64  `json:"unresolved_cost_nano_cny,omitempty"`
	OtherJson             string `json:"other_json,omitempty"`
}

// TPSMeasurement returns the output-token and generation-time totals needed
// for the upstream performance aggregation formula. Older events did not
// persist generation time, so derive it from their per-request TPS.
func (event ChannelMonitorEvent) TPSMeasurement() (int64, int64, bool) {
	if event.TPS == nil || event.CompletionTokens == nil || *event.CompletionTokens <= 0 ||
		*event.TPS <= 0 || math.IsNaN(*event.TPS) || math.IsInf(*event.TPS, 0) {
		return 0, 0, false
	}
	durationMs := float64(*event.CompletionTokens) / *event.TPS * 1000
	if math.IsNaN(durationMs) || math.IsInf(durationMs, 0) || durationMs <= 0 || durationMs > float64(math.MaxInt64) {
		return 0, 0, false
	}
	convertedDurationMs := int64(math.Round(durationMs))
	if convertedDurationMs <= 0 {
		return 0, 0, false
	}
	return *event.CompletionTokens, convertedDurationMs, true
}

func NewChannelMonitorEvent(channelId int, source ChannelMonitorEventSource, outcome ChannelMonitorEventOutcome, occurredAt int64) ChannelMonitorEvent {
	now := time.Now().Unix()
	return ChannelMonitorEvent{
		EventId:       uuid.NewString(),
		SchemaVersion: ChannelMonitorEventSchemaVersion,
		OccurredAt:    occurredAt,
		CreatedAt:     now,
		ChannelId:     channelId,
		NodeId:        common.GetNodeIdentity().Name,
		Source:        source,
		Outcome:       outcome,
		CostStatus:    ChannelMonitorEventCostNone,
		SchedulingEligible: source == ChannelMonitorEventSourceBusiness ||
			source == ChannelMonitorEventSourceSmartProbe,
	}
}

func (event ChannelMonitorEvent) Validate() error {
	eventId := strings.TrimSpace(event.EventId)
	if eventId == "" || len(eventId) > ChannelMonitorEventMaxIdentityLength {
		return errors.New("渠道监控事件 ID 不能为空且长度不能超过 128")
	}
	if event.SchemaVersion != ChannelMonitorEventSchemaVersion {
		return fmt.Errorf("不支持的渠道监控事件版本: %d", event.SchemaVersion)
	}
	if event.OccurredAt <= 0 || event.CreatedAt <= 0 {
		return errors.New("渠道监控事件时间无效")
	}
	if event.ChannelId <= 0 {
		return errors.New("渠道监控事件渠道 ID 无效")
	}
	if len(event.GroupName) > ChannelMonitorEventMaxNameLength || len(event.ModelName) > ChannelMonitorEventMaxNameLength {
		return errors.New("渠道监控事件模型或分组名称过长")
	}
	if len(event.APIKeyName) > ChannelMonitorEventMaxNameLength {
		return errors.New("渠道监控事件 API 令牌名称过长")
	}
	if len(event.RequestId) > ChannelMonitorEventMaxIdentityLength || len(event.NodeId) > ChannelMonitorEventMaxIdentityLength {
		return errors.New("渠道监控事件请求或节点标识过长")
	}
	if len(event.ErrorType) > ChannelMonitorEventMaxIdentityLength || len(event.ErrorCode) > ChannelMonitorEventMaxIdentityLength {
		return errors.New("渠道监控事件错误类型或错误码过长")
	}
	if len(event.ErrorMessage) > 2048 {
		return errors.New("渠道监控事件错误消息过长")
	}
	if !event.Source.valid() {
		return fmt.Errorf("渠道监控事件来源无效: %s", event.Source)
	}
	if !event.Outcome.valid() {
		return fmt.Errorf("渠道监控事件结果无效: %s", event.Outcome)
	}
	if !event.CostStatus.valid() {
		return fmt.Errorf("渠道监控事件成本状态无效: %s", event.CostStatus)
	}
	if event.FirstTokenMs != nil && (*event.FirstTokenMs < 0 || math.IsNaN(*event.FirstTokenMs) || math.IsInf(*event.FirstTokenMs, 0)) {
		return errors.New("渠道监控事件首字时间无效")
	}
	if event.TPS != nil && (*event.TPS < 0 || math.IsNaN(*event.TPS) || math.IsInf(*event.TPS, 0)) {
		return errors.New("渠道监控事件 TPS 无效")
	}
	if event.PromptTokens != nil && *event.PromptTokens < 0 {
		return errors.New("渠道监控事件输入 token 不能为负数")
	}
	if event.CompletionTokens != nil && *event.CompletionTokens < 0 {
		return errors.New("渠道监控事件输出 token 不能为负数")
	}
	if event.CacheReadTokens != nil && *event.CacheReadTokens < 0 {
		return errors.New("渠道监控事件缓存读取 token 不能为负数")
	}
	if event.CacheWriteTokens != nil && *event.CacheWriteTokens < 0 {
		return errors.New("渠道监控事件缓存写入 token 不能为负数")
	}
	if event.InputTokens != nil && *event.InputTokens < 0 {
		return errors.New("渠道监控事件总输入 token 不能为负数")
	}
	if event.AttemptDurationMs != nil && *event.AttemptDurationMs < 0 {
		return errors.New("渠道监控事件请求耗时不能为负数")
	}
	if event.SettledCostNanoCNY < 0 || event.UnresolvedCostNanoCNY < 0 {
		return errors.New("渠道监控事件成本不能为负数")
	}
	switch event.CostStatus {
	case ChannelMonitorEventCostNone:
		if event.SettledCostNanoCNY != 0 || event.UnresolvedCostNanoCNY != 0 {
			return errors.New("无成本事件不能携带成本金额")
		}
	case ChannelMonitorEventCostSettled:
		if event.UnresolvedCostNanoCNY != 0 {
			return errors.New("已结算成本事件不能携带未解析成本")
		}
	case ChannelMonitorEventCostUnresolved:
		if event.SettledCostNanoCNY != 0 {
			return errors.New("未解析成本事件不能携带已结算成本")
		}
	}
	if len(event.OtherJson) > ChannelMonitorEventMaxOtherJsonBytes {
		return errors.New("渠道监控事件扩展数据过大")
	}
	if strings.TrimSpace(event.OtherJson) != "" {
		var other any
		if err := common.UnmarshalJsonStr(event.OtherJson, &other); err != nil {
			return fmt.Errorf("渠道监控事件扩展数据不是有效 JSON: %w", err)
		}
	}
	return nil
}

func (event ChannelMonitorEvent) Marshal() ([]byte, error) {
	if err := event.Validate(); err != nil {
		return nil, err
	}
	return common.Marshal(event)
}

// Clone freezes pointer-backed optional measurements before an event crosses
// the asynchronous queue boundary.
func (event ChannelMonitorEvent) Clone() ChannelMonitorEvent {
	if event.StatusCode != nil {
		value := *event.StatusCode
		event.StatusCode = &value
	}
	if event.FirstTokenMs != nil {
		value := *event.FirstTokenMs
		event.FirstTokenMs = &value
	}
	if event.TPS != nil {
		value := *event.TPS
		event.TPS = &value
	}
	if event.PromptTokens != nil {
		value := *event.PromptTokens
		event.PromptTokens = &value
	}
	if event.CompletionTokens != nil {
		value := *event.CompletionTokens
		event.CompletionTokens = &value
	}
	if event.CacheReadTokens != nil {
		value := *event.CacheReadTokens
		event.CacheReadTokens = &value
	}
	if event.CacheWriteTokens != nil {
		value := *event.CacheWriteTokens
		event.CacheWriteTokens = &value
	}
	if event.InputTokens != nil {
		value := *event.InputTokens
		event.InputTokens = &value
	}
	if event.AttemptDurationMs != nil {
		value := *event.AttemptDurationMs
		event.AttemptDurationMs = &value
	}
	return event
}

func UnmarshalChannelMonitorEvent(data []byte) (ChannelMonitorEvent, error) {
	var event ChannelMonitorEvent
	if err := common.Unmarshal(data, &event); err != nil {
		return ChannelMonitorEvent{}, err
	}
	if err := event.Validate(); err != nil {
		return ChannelMonitorEvent{}, err
	}
	return event, nil
}

func (source ChannelMonitorEventSource) valid() bool {
	switch source {
	case ChannelMonitorEventSourceBusiness,
		ChannelMonitorEventSourceStatusProbe,
		ChannelMonitorEventSourceSmartProbe,
		ChannelMonitorEventSourceManualTest,
		ChannelMonitorEventSourceModelDetection:
		return true
	default:
		return false
	}
}

func (outcome ChannelMonitorEventOutcome) valid() bool {
	switch outcome {
	case ChannelMonitorEventOutcomeSuccess,
		ChannelMonitorEventOutcomeFailure,
		ChannelMonitorEventOutcomeUnresolved,
		ChannelMonitorEventOutcomeCanceled:
		return true
	default:
		return false
	}
}

func (status ChannelMonitorEventCostStatus) valid() bool {
	switch status {
	case ChannelMonitorEventCostNone,
		ChannelMonitorEventCostSettled,
		ChannelMonitorEventCostUnresolved:
		return true
	default:
		return false
	}
}
