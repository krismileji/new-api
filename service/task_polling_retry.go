package service

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

// ResolveTaskPollingBaseURL validates the endpoint before a background task
// poll reaches net/http, including sparse retry channels with no BaseURL.
func ResolveTaskPollingBaseURL(ch *model.Channel) (string, error) {
	if ch == nil {
		return "", errors.New("任务轮询渠道为空")
	}
	baseURL := strings.TrimSpace(ch.GetBaseURL())
	if baseURL == "" && ch.Type >= 0 && ch.Type < len(constant.ChannelBaseURLs) {
		baseURL = strings.TrimSpace(constant.ChannelBaseURLs[ch.Type])
	}
	if baseURL == "" {
		return "", fmt.Errorf("渠道 #%d 上游地址为空", ch.Id)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("渠道 #%d 上游地址无效", ch.Id)
	}
	return strings.TrimRight(baseURL, "/"), nil
}
