package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/google/uuid"
)

const TokenAutoDisabledCode types.ErrorCode = "api_key_auto_disabled"

type TokenAutoDisableRule struct {
	Id              string   `json:"id"`
	Name            string   `json:"name"`
	Enabled         bool     `json:"enabled"`
	ChannelIds      []int    `json:"channel_ids"`
	StatusCodes     []int    `json:"status_codes"`
	Keywords        []string `json:"keywords"`
	MatchAll        bool     `json:"match_all"`
	CaseSensitive   bool     `json:"case_sensitive"`
	ResponseStatus  int      `json:"response_status"`
	ResponseMessage string   `json:"response_message"`
}

type TokenAutoDisableSettings struct {
	Enabled  bool                   `json:"enabled"`
	Revision int64                  `json:"revision"`
	Rules    []TokenAutoDisableRule `json:"rules"`
}

func (settings TokenAutoDisableSettings) Validate() error {
	if settings.Revision < 0 || len(settings.Rules) > 64 {
		return errors.New("修订号无效或规则超过 64 条")
	}
	ids := make(map[string]bool)
	for _, rule := range settings.Rules {
		if strings.TrimSpace(rule.Id) == "" || len(rule.Id) > 64 || ids[rule.Id] {
			return errors.New("规则标识不能为空、重复或超过 64 字节")
		}
		ids[rule.Id] = true
		if strings.TrimSpace(rule.Name) == "" || len([]rune(rule.Name)) > 128 {
			return errors.New("规则名称不能为空或超过 128 个字符")
		}
		if len(rule.StatusCodes) == 0 || len(rule.StatusCodes) > 100 || len(rule.ChannelIds) > 200 {
			return errors.New("每条规则必须指定状态码，最多 100 个状态码和 200 个渠道")
		}
		for _, code := range rule.StatusCodes {
			if code < 100 || code > 599 {
				return errors.New("上游状态码必须在 100 到 599 之间")
			}
		}
		for _, id := range rule.ChannelIds {
			if id <= 0 {
				return errors.New("渠道编号必须为正整数")
			}
		}
		if len(rule.Keywords) == 0 || len(rule.Keywords) > 32 {
			return errors.New("每条规则需要 1 到 32 个错误关键词")
		}
		for _, keyword := range rule.Keywords {
			if strings.TrimSpace(keyword) == "" || len(keyword) > 512 {
				return errors.New("错误关键词不能为空或超过 512 字节")
			}
		}
		if rule.ResponseStatus < 400 || rule.ResponseStatus > 599 || strings.TrimSpace(rule.ResponseMessage) == "" || len(rule.ResponseMessage) > 4096 {
			return errors.New("返回状态码必须在 400 到 599 之间，错误信息不能为空或超过 4096 字节")
		}
	}
	data, err := common.Marshal(settings.Rules)
	if err != nil {
		return err
	}
	// Keep the same accepted configuration size on all databases, including
	// MySQL TEXT's 65,535-byte limit.
	if len(data) > 65535 {
		return errors.New("规则配置总大小不能超过 65535 字节，请减少规则或关键词")
	}
	return nil
}

func (rule TokenAutoDisableRule) matches(channelId, status int, message string) bool {
	if !rule.Enabled {
		return false
	}
	channelMatches := len(rule.ChannelIds) == 0
	for _, id := range rule.ChannelIds {
		channelMatches = channelMatches || id == channelId
	}
	statusMatches := false
	for _, code := range rule.StatusCodes {
		statusMatches = statusMatches || code == status
	}
	if !channelMatches || !statusMatches {
		return false
	}
	if !rule.CaseSensitive {
		message = strings.ToLower(message)
	}
	for _, keyword := range rule.Keywords {
		if !rule.CaseSensitive {
			keyword = strings.ToLower(keyword)
		}
		matched := strings.Contains(message, keyword)
		if rule.MatchAll && !matched {
			return false
		}
		if !rule.MatchAll && matched {
			return true
		}
	}
	return rule.MatchAll && len(rule.Keywords) > 0
}

type TokenAutoDisableCause struct{ Record model.TokenAutoDisableRecord }

func (cause *TokenAutoDisableCause) Error() string { return cause.Record.ResponseMessage }

type tokenProtectionContextKey struct{}
type tokenProtectionRequest struct {
	channelId            atomic.Int64
	manager              *tokenProtectionManager
	epoch                *tokenProtectionEpoch
	tokenId, userId      int
	tokenName, requestId string
	cancel               context.CancelCauseFunc
}

func SetTokenProtectionChannel(ctx context.Context, channelId int) {
	if request, ok := ctx.Value(tokenProtectionContextKey{}).(*tokenProtectionRequest); ok {
		request.channelId.Store(int64(channelId))
	}
}

func TokenProtectionChannel(ctx context.Context) int {
	if request, ok := ctx.Value(tokenProtectionContextKey{}).(*tokenProtectionRequest); ok {
		return int(request.channelId.Load())
	}
	return 0
}

type tokenProtectionEpoch struct {
	requests map[*tokenProtectionRequest]struct{}
}
type tokenProtectionBlock struct {
	cause     *TokenAutoDisableCause
	persisted bool
	err       string
}
type tokenProtectionManager struct {
	journalDir string
	mu         sync.Mutex
	mutationMu sync.Mutex
	settings   TokenAutoDisableSettings
	epochs     map[int]*tokenProtectionEpoch
	blocks     map[int]*tokenProtectionBlock
}

var tokenProtection atomic.Pointer[tokenProtectionManager]

// InitTokenAutoDisable runs before accepting traffic. A failed load fails startup
// rather than treating an unavailable deny list as an empty one.
func InitTokenAutoDisable(ctx context.Context) error {
	config, err := model.LoadTokenAutoDisableConfig(ctx)
	if err != nil {
		return err
	}
	settings := TokenAutoDisableSettings{Enabled: config.Enabled, Revision: config.Revision, Rules: []TokenAutoDisableRule{}}
	if err := common.UnmarshalJsonStr(config.Rules, &settings.Rules); err != nil {
		return err
	}
	if err := settings.Validate(); err != nil {
		return err
	}
	records, err := model.LoadActiveTokenAutoDisables(ctx)
	if err != nil {
		return err
	}
	m := &tokenProtectionManager{settings: settings, epochs: make(map[int]*tokenProtectionEpoch), blocks: make(map[int]*tokenProtectionBlock)}
	for _, record := range records {
		m.blocks[record.TokenId] = &tokenProtectionBlock{cause: &TokenAutoDisableCause{Record: record}, persisted: true}
	}
	if err := m.openJournal(ctx); err != nil {
		return err
	}
	tokenProtection.Store(m)
	go m.retryPending(ctx)
	return nil
}

func TokenAutoDisableSettingsSnapshot() (TokenAutoDisableSettings, error) {
	m := tokenProtection.Load()
	if m == nil {
		return TokenAutoDisableSettings{}, errors.New("API Key 自动禁用尚未初始化")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings, nil // immutable after publication
}

func SaveTokenAutoDisableSettings(ctx context.Context, settings TokenAutoDisableSettings) (TokenAutoDisableSettings, error) {
	if err := settings.Validate(); err != nil {
		return settings, err
	}
	m := tokenProtection.Load()
	if m == nil {
		return settings, errors.New("API Key 自动禁用尚未初始化")
	}
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	data, err := common.Marshal(settings.Rules)
	if err != nil {
		return settings, err
	}
	if err := model.SaveTokenAutoDisableConfig(ctx, model.TokenAutoDisableConfig{Id: 1, Enabled: settings.Enabled, Revision: settings.Revision, Rules: string(data)}); err != nil {
		return settings, err
	}
	settings.Revision++
	m.mu.Lock()
	m.settings = settings
	m.mu.Unlock()
	return settings, nil
}

// Registration and revocation share the same lock. No admitted request can miss
// the cancellation snapshot. Epoch identity rejects late errors after release.
func RegisterTokenProtection(ctx context.Context, token *model.Token, requestId string) (context.Context, func(), *TokenAutoDisableCause) {
	m := tokenProtection.Load()
	if m == nil || token == nil || token.Id <= 0 {
		return ctx, func() {}, nil
	}
	m.mu.Lock()
	if block := m.blocks[token.Id]; block != nil {
		m.mu.Unlock()
		return ctx, func() {}, block.cause
	}
	epoch := m.epochs[token.Id]
	if epoch == nil {
		epoch = &tokenProtectionEpoch{requests: make(map[*tokenProtectionRequest]struct{})}
		m.epochs[token.Id] = epoch
	}
	workCtx, cancel := context.WithCancelCause(ctx)
	request := &tokenProtectionRequest{manager: m, epoch: epoch, tokenId: token.Id, userId: token.UserId, tokenName: token.Name, requestId: requestId, cancel: cancel}
	epoch.requests[request] = struct{}{}
	m.mu.Unlock()
	return context.WithValue(workCtx, tokenProtectionContextKey{}, request), func() {
		m.mu.Lock()
		delete(epoch.requests, request)
		if len(epoch.requests) == 0 && m.epochs[token.Id] == epoch {
			delete(m.epochs, token.Id)
		}
		m.mu.Unlock()
		cancel(nil)
	}, nil
}

func TokenAutoDisableFromContext(ctx context.Context) *TokenAutoDisableCause {
	if ctx == nil {
		return nil
	}
	cause, _ := context.Cause(ctx).(*TokenAutoDisableCause)
	return cause
}

func TokenAutoDisableError(ctx context.Context) *types.NewAPIError {
	cause := TokenAutoDisableFromContext(ctx)
	if cause == nil {
		return nil
	}
	return types.NewErrorWithStatusCode(cause, TokenAutoDisabledCode, cause.Record.ResponseStatus, types.ErrOptionWithSkipRetry())
}

// ObserveTokenAutoDisableError accepts only an actual upstream error, before
// status/message mappings. Normal model output and local errors never call it.
func ObserveTokenAutoDisableError(ctx context.Context, channelId, status int, message string) {
	request, _ := ctx.Value(tokenProtectionContextKey{}).(*tokenProtectionRequest)
	if request == nil || ctx.Err() != nil {
		return
	}
	m := request.manager
	m.mu.Lock()
	if !m.settings.Enabled || m.epochs[request.tokenId] != request.epoch || m.blocks[request.tokenId] != nil {
		m.mu.Unlock()
		return
	}
	if _, registered := request.epoch.requests[request]; !registered {
		m.mu.Unlock()
		return
	}
	var match *TokenAutoDisableRule
	for i := range m.settings.Rules {
		if m.settings.Rules[i].matches(channelId, status, message) {
			match = &m.settings.Rules[i]
			break
		}
	}
	if match == nil {
		m.mu.Unlock()
		return
	}
	ruleSnapshot, marshalErr := common.Marshal(match)
	if marshalErr != nil {
		m.mu.Unlock()
		common.SysError("自动禁用规则序列化失败: " + marshalErr.Error())
		return
	}
	summary := []rune(common.MaskSensitiveInfo(message))
	if len(summary) > 2048 {
		summary = summary[:2048]
	}
	record := model.TokenAutoDisableRecord{Id: uuid.NewString(), TokenId: request.tokenId, UserId: request.userId, TokenName: request.tokenName,
		RuleId: match.Id, RuleName: match.Name, RuleSnapshot: string(ruleSnapshot), ChannelId: channelId, RequestId: request.requestId, UpstreamStatus: status,
		ErrorSummary: string(summary), ResponseStatus: match.ResponseStatus, ResponseMessage: match.ResponseMessage,
		CanceledRequests: len(request.epoch.requests), CreatedAt: time.Now().Unix()}
	cause := &TokenAutoDisableCause{Record: record}
	m.blocks[request.tokenId] = &tokenProtectionBlock{cause: cause}
	cancels := make([]context.CancelCauseFunc, 0, len(request.epoch.requests))
	for active := range request.epoch.requests {
		cancels = append(cancels, active.cancel)
	}
	m.mu.Unlock()
	for _, cancel := range cancels {
		cancel(cause)
	}
	if err := m.journal(record); err != nil {
		common.SysError(fmt.Sprintf("API Key 自动禁用本地恢复记录保存失败 token_id=%d: %v", record.TokenId, err))
	}
	// Cancellation precedes SQL work, including when SQL is slow or unavailable.
	m.persist(request.tokenId)
}

func (m *tokenProtectionManager) persist(tokenId int) {
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	m.mu.Lock()
	block := m.blocks[tokenId]
	if block == nil || block.persisted {
		m.mu.Unlock()
		return
	}
	record := block.cause.Record
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := model.PersistTokenAutoDisable(ctx, record)
	m.mu.Lock()
	block.persisted = err == nil
	block.err = ""
	if err != nil {
		block.err = "禁用已在本机生效，数据库保存失败，正在重试"
	}
	m.mu.Unlock()
	if err != nil {
		common.SysError(fmt.Sprintf("API Key 自动禁用保存失败 token_id=%d request_id=%s: %v", tokenId, record.RequestId, err))
	} else if err := os.Remove(filepath.Join(m.journalDir, record.Id+".json")); err != nil && !os.IsNotExist(err) {
		common.SysError("API Key 自动禁用恢复记录清理失败: " + err.Error())
	}
}

func (m *tokenProtectionManager) retryPending(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.mu.Lock()
			ids := make([]int, 0)
			for id, block := range m.blocks {
				if !block.persisted {
					ids = append(ids, id)
				}
			}
			m.mu.Unlock()
			for _, id := range ids {
				m.persist(id)
			}
		}
	}
}

type TokenAutoDisablePending struct {
	model.TokenAutoDisableRecord
	PersistenceError string `json:"persistence_error"`
}

func PendingTokenAutoDisables() []TokenAutoDisablePending {
	result := []TokenAutoDisablePending{}
	m := tokenProtection.Load()
	if m == nil {
		return result
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, block := range m.blocks {
		if !block.persisted {
			result = append(result, TokenAutoDisablePending{block.cause.Record, block.err})
		}
	}
	return result
}

func ReleaseTokenProtection(ctx context.Context, id string, actor int) error {
	m := tokenProtection.Load()
	if m == nil {
		return errors.New("API Key 自动禁用尚未初始化")
	}
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	m.mu.Lock()
	var block *tokenProtectionBlock
	for _, candidate := range m.blocks {
		if candidate.cause.Record.Id == id {
			block = candidate
			break
		}
	}
	if block == nil || !block.persisted {
		m.mu.Unlock()
		return errors.New("禁用记录不存在或尚未保存，请稍后刷新")
	}
	tokenId := block.cause.Record.TokenId
	m.mu.Unlock()
	if err := model.ReleaseTokenAutoDisable(ctx, id, actor); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.blocks, tokenId)
	delete(m.epochs, tokenId)
	m.mu.Unlock()
	return nil
}

// User-facing token edits cannot race a disable or release into re-enabling a key.
func UpdateTokenWithProtection(token *model.Token) error {
	m := tokenProtection.Load()
	if m == nil {
		return token.Update()
	}
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	m.mu.Lock()
	blocked := m.blocks[token.Id] != nil
	m.mu.Unlock()
	if blocked && token.Status == common.TokenStatusEnabled {
		return errors.New("此 API Key 已被自动禁用，仅管理员可以解除")
	}
	return token.Update()
}
