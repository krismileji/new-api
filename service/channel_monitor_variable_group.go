package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"golang.org/x/net/http/httpguts"
)

type ChannelMonitorVariableGroupConfig struct {
	ID               int                                   `json:"id"`
	Name             string                                `json:"name"`
	BaseURL          string                                `json:"base_url"`
	Proxy            string                                `json:"proxy"`
	RequestTimeout   int                                   `json:"request_timeout"`
	Revision         int64                                 `json:"revision"`
	VariableRequests []ChannelMonitorCustomVariableRequest `json:"variable_requests"`
	SourceChannelID  int                                   `json:"source_channel_id,omitempty"`
}

func channelMonitorVariableGroupConfig(requests []ChannelMonitorCustomVariableRequest) ChannelMonitorCustomUpstreamConfig {
	ratio, balance := 1.0, 0.0
	return ChannelMonitorCustomUpstreamConfig{
		Version:          1,
		Ratio:            ChannelMonitorCustomMetricConfig{Source: ChannelMonitorCustomSourceFixed, FixedValue: &ratio},
		Balance:          ChannelMonitorCustomMetricConfig{Source: ChannelMonitorCustomSourceFixed, FixedValue: &balance},
		VariableRequests: requests,
	}
}

func ChannelMonitorVariableGroupView(group model.ChannelMonitorVariableGroup) (ChannelMonitorVariableGroupConfig, error) {
	config, err := ParseChannelMonitorCustomUpstreamConfig(group.Config)
	if err != nil {
		return ChannelMonitorVariableGroupConfig{}, err
	}
	return ChannelMonitorVariableGroupConfig{
		ID: group.ID, Name: group.Name, BaseURL: group.BaseURL, Proxy: group.Proxy,
		RequestTimeout: group.RequestTimeout, Revision: group.Revision,
		VariableRequests: SanitizeChannelMonitorCustomUpstreamConfig(config).VariableRequests,
	}, nil
}

// Preparation also serves draft extraction. It restores masked values only
// from the same request/base URL, never from a different shared configuration.
func PrepareChannelMonitorVariableGroup(ctx context.Context, input ChannelMonitorVariableGroupConfig) (model.ChannelMonitorVariableGroup, *model.ChannelMonitorVariableGroup, error) {
	group := model.ChannelMonitorVariableGroup{ID: input.ID, Name: strings.TrimSpace(input.Name), Proxy: strings.TrimSpace(input.Proxy), RequestTimeout: input.RequestTimeout, Revision: input.Revision}
	if group.ID < 0 || group.Name == "" || utf8.RuneCountInString(group.Name) > 80 || !httpguts.ValidHeaderFieldValue(group.Name) {
		return group, nil, errors.New("请输入有效的共享配置名称，最多 80 个字符")
	}
	var err error
	group.BaseURL, err = NormalizeChannelMonitorCustomBaseURL(input.BaseURL)
	if err != nil {
		return group, nil, err
	}
	if group.RequestTimeout < 1 || group.RequestTimeout > 120 {
		return group, nil, errors.New("共享请求超时须为 1 到 120 秒")
	}
	if _, _, err := common.ParseProxyURLRuntime(group.Proxy); err != nil || len(group.Proxy) > 2048 {
		return group, nil, errors.New("共享请求代理地址无效")
	}
	var existing *model.ChannelMonitorVariableGroup
	var saved *ChannelMonitorCustomUpstreamConfig
	if group.ID > 0 {
		current, err := model.GetChannelMonitorVariableGroup(ctx, group.ID)
		if err != nil {
			return group, nil, err
		}
		if current.Revision != group.Revision {
			return group, nil, model.ErrChannelMonitorVariableGroupChanged
		}
		existing = &current
		if current.BaseURL == group.BaseURL && current.Proxy == group.Proxy {
			parsed, err := ParseChannelMonitorCustomUpstreamConfig(current.Config)
			if err != nil {
				return group, nil, err
			}
			saved = &parsed
		}
	} else if input.SourceChannelID > 0 {
		monitor, err := model.GetChannelRatioMonitorWithContext(ctx, input.SourceChannelID)
		if err != nil {
			return group, nil, err
		}
		if monitor.UpstreamType != CustomUpstreamType || monitor.UpstreamBaseURL != group.BaseURL {
			return group, nil, errors.New("来源渠道的上游地址已变更，请重新填写凭据")
		}
		parsed, err := ParseChannelMonitorCustomUpstreamConfig(monitor.CustomUpstreamConfig)
		if err != nil {
			return group, nil, err
		}
		saved = &parsed
	}
	if len(input.VariableRequests) == 0 {
		return group, nil, errors.New("共享配置至少需要一个独立请求")
	}
	config, err := NormalizeChannelMonitorCustomUpstreamConfigWithExisting(channelMonitorVariableGroupConfig(input.VariableRequests), saved)
	if err != nil {
		return group, nil, err
	}
	group.Config, err = MarshalChannelMonitorCustomUpstreamConfig(config)
	return group, existing, err
}

func SaveChannelMonitorVariableGroup(ctx context.Context, input ChannelMonitorVariableGroupConfig) (ChannelMonitorVariableGroupConfig, error) {
	group, existing, err := PrepareChannelMonitorVariableGroup(ctx, input)
	if err != nil {
		return ChannelMonitorVariableGroupConfig{}, err
	}
	err = model.SaveChannelMonitorVariableGroup(ctx, &group, existing, func(monitors []model.ChannelRatioMonitor) error {
		for _, monitor := range monitors {
			config, err := ParseChannelMonitorCustomUpstreamConfig(monitor.CustomUpstreamConfig)
			if err != nil {
				return err
			}
			if _, err := resolveChannelMonitorVariableGroup(config, group); err != nil {
				return fmt.Errorf("渠道 %d 仍在使用该配置：%w", monitor.ChannelId, err)
			}
		}
		return nil
	})
	if err != nil {
		return ChannelMonitorVariableGroupConfig{}, err
	}
	return ChannelMonitorVariableGroupView(group)
}

func resolveChannelMonitorVariableGroup(config ChannelMonitorCustomUpstreamConfig, group model.ChannelMonitorVariableGroup) (ChannelMonitorCustomUpstreamConfig, error) {
	shared, err := ParseChannelMonitorCustomUpstreamConfig(group.Config)
	if err != nil {
		return config, err
	}
	config.VariableGroupID = 0
	config.VariableRequests = shared.VariableRequests
	return config, validateChannelMonitorCustomTemplates(config)
}

func ValidateChannelMonitorVariableGroup(ctx context.Context, config *ChannelMonitorCustomUpstreamConfig) error {
	if config.VariableGroupID == 0 {
		return nil
	}
	group, err := model.GetChannelMonitorVariableGroup(ctx, config.VariableGroupID)
	if err != nil {
		return err
	}
	_, err = resolveChannelMonitorVariableGroup(*config, group)
	if err == nil {
		config.VariableGroupRevision = group.Revision
	}
	return err
}

// Group locks are distinct from channel locks. Hold the group lock through the
// metric request and its single retry so other channels reuse the new values.
func (session *channelMonitorCustomVariableSession) loadShared(ctx context.Context) (func(), error) {
	if session.config.VariableGroupID == 0 {
		return func() {}, nil
	}
	id := session.config.VariableGroupID
	lock, _ := channelMonitorCustomVariableLocks.LoadOrStore(fmt.Sprintf("group:%d", id), make(chan struct{}, 1))
	gate := lock.(chan struct{})
	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	release := func() { <-gate }
	group, err := model.GetChannelMonitorVariableGroup(ctx, id)
	if err == nil {
		session.config, err = resolveChannelMonitorVariableGroup(session.config, group)
	}
	if err != nil {
		release()
		return nil, err
	}
	session.shared = &group
	return release, nil
}

func (session *channelMonitorCustomVariableSession) fetchSharedVariables(ctx context.Context, request ChannelMonitorCustomVariableRequest) ([]ChannelMonitorCustomVariable, error) {
	client, err := NewSSRFProtectedHTTPClientWithProxy(session.shared.Proxy)
	if err != nil {
		return nil, err
	}
	requestContext, cancel := context.WithTimeout(ctx, channelMonitorUpstreamRequestTimeout(time.Duration(session.shared.RequestTimeout)*time.Second))
	defer cancel()
	return fetchChannelMonitorCustomVariables(requestContext, client, session.shared.BaseURL, request)
}

func FetchChannelMonitorVariableGroupDraft(ctx context.Context, input ChannelMonitorVariableGroupConfig, requestID string) ([]ChannelMonitorCustomVariable, error) {
	// Unfinished sibling requests should not prevent testing the selected one.
	for _, request := range input.VariableRequests {
		if request.ID != requestID {
			continue
		}
		input.VariableRequests = []ChannelMonitorCustomVariableRequest{request}
		group, _, err := PrepareChannelMonitorVariableGroup(ctx, input)
		if err != nil {
			return nil, err
		}
		config, err := ParseChannelMonitorCustomUpstreamConfig(group.Config)
		if err != nil {
			return nil, err
		}
		session := channelMonitorCustomVariableSession{shared: &group}
		return session.fetchSharedVariables(ctx, config.VariableRequests[0])
	}
	return nil, errors.New("独立请求不存在，请重新选择")
}
