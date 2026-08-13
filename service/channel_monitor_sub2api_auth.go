package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	maxSub2APIAccountLength           = 320
	maxSub2APIPasswordLength          = 4096
	maxSub2APIAccessTokenLength       = 4096
	maxSub2APIRefreshTokenLength      = 4096
	sub2APIRefreshTokenAliasRetention = 24 * time.Hour
)

type sub2APIAccountTokenCacheEntry struct {
	accessToken string
	expiresAt   time.Time
}

type sub2APIAccountTokenCall struct {
	done        chan struct{}
	accessToken string
	err         error
}

var sub2APIAccountTokenCache = struct {
	sync.Mutex
	tokens  map[[32]byte]sub2APIAccountTokenCacheEntry
	pending map[[32]byte]*sub2APIAccountTokenCall
}{
	tokens:  make(map[[32]byte]sub2APIAccountTokenCacheEntry),
	pending: make(map[[32]byte]*sub2APIAccountTokenCall),
}

type sub2APIAccountLoginRequest struct {
	Email          string `json:"email"`
	Password       string `json:"password"`
	TurnstileToken string `json:"turnstile_token"`
}

type sub2APIAccountLoginResult struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	Requires2FA bool   `json:"requires_2fa"`
}

type sub2APIRefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type sub2APIRefreshTokenResult struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

type sub2APIRefreshTokenCacheEntry struct {
	accessToken  string
	refreshToken string
	expiresAt    time.Time
	familyKey    [32]byte
}

type sub2APIRefreshTokenCall struct {
	done  chan struct{}
	entry sub2APIRefreshTokenCacheEntry
	err   error
}

var sub2APIRefreshTokenCache = struct {
	sync.Mutex
	tokens  map[[32]byte]sub2APIRefreshTokenCacheEntry
	pending map[[32]byte]*sub2APIRefreshTokenCall
}{
	tokens:  make(map[[32]byte]sub2APIRefreshTokenCacheEntry),
	pending: make(map[[32]byte]*sub2APIRefreshTokenCall),
}

func normalizeSub2APIRefreshTokenConfig(config Sub2APIGroupRatioConfig) (string, string, error) {
	baseURL, err := normalizeSub2APIBaseURL(config.BaseURL)
	if err != nil {
		return "", "", err
	}
	refreshToken := strings.TrimSpace(config.AccessToken)
	if len([]rune(refreshToken)) > maxSub2APIRefreshTokenLength {
		return "", "", errors.New("Sub2API Refresh Token 过长")
	}
	if refreshToken == "" {
		return "", "", errors.New("请输入 Sub2API Refresh Token")
	}
	return baseURL, refreshToken, nil
}

func sub2APIRefreshTokenCacheKey(baseURL string, refreshToken string, proxy string, credentialID int, revision int64) [32]byte {
	return sha256.Sum256([]byte(baseURL + "\x00" + refreshToken + "\x00" + strings.TrimSpace(proxy) + "\x00" + fmt.Sprint(credentialID) + "\x00" + fmt.Sprint(revision)))
}

func sub2APIRefreshTokenTTL(expiresIn int) time.Duration {
	ttl := 5 * time.Minute
	if expiresIn <= 0 {
		return ttl
	}
	const maxTokenCacheTTL = 24 * time.Hour
	maxTokenCacheSeconds := int(maxTokenCacheTTL / time.Second)
	if expiresIn >= maxTokenCacheSeconds {
		ttl = maxTokenCacheTTL
	} else {
		ttl = time.Duration(expiresIn) * time.Second
	}
	safetyWindow := time.Minute
	if ttl <= 2*time.Minute {
		safetyWindow = ttl / 4
	}
	ttl -= safetyWindow
	if ttl <= 0 {
		ttl = time.Second
	}
	return ttl
}

func resolveSub2APIRefreshTokenConfig(ctx context.Context, client *http.Client, config Sub2APIGroupRatioConfig, validateURL func(string) error) (Sub2APIGroupRatioConfig, error) {
	baseURL, refreshToken, err := normalizeSub2APIRefreshTokenConfig(config)
	if err != nil {
		return Sub2APIGroupRatioConfig{}, err
	}
	cacheKey := sub2APIRefreshTokenCacheKey(baseURL, refreshToken, config.Proxy, config.CredentialID, config.Revision)
	originalCacheKey := cacheKey
	familyKey := cacheKey
	for {
		now := time.Now()
		sub2APIRefreshTokenCache.Lock()
		aliasKeys := make([][32]byte, 0, 3)
		visited := make(map[[32]byte]struct{})
		for {
			if _, seen := visited[cacheKey]; seen {
				break
			}
			visited[cacheKey] = struct{}{}
			aliasKeys = append(aliasKeys, cacheKey)
			entry, ok := sub2APIRefreshTokenCache.tokens[cacheKey]
			if !ok {
				break
			}
			if entry.familyKey != ([32]byte{}) {
				familyKey = entry.familyKey
			}
			if entry.refreshToken == "" || entry.refreshToken == refreshToken {
				break
			}
			refreshToken = entry.refreshToken
			cacheKey = sub2APIRefreshTokenCacheKey(baseURL, refreshToken, config.Proxy, config.CredentialID, config.Revision)
		}
		for key, entry := range sub2APIRefreshTokenCache.tokens {
			if now.After(entry.expiresAt.Add(sub2APIRefreshTokenAliasRetention)) {
				delete(sub2APIRefreshTokenCache.tokens, key)
			}
		}
		if entry, ok := sub2APIRefreshTokenCache.tokens[cacheKey]; ok && now.Before(entry.expiresAt) {
			sub2APIRefreshTokenCache.Unlock()
			config.BaseURL = baseURL
			config.AuthType = Sub2APIAuthToken
			config.AccessToken = entry.accessToken
			return config, nil
		}
		if call, ok := sub2APIRefreshTokenCache.pending[cacheKey]; ok {
			sub2APIRefreshTokenCache.Unlock()
			select {
			case <-ctx.Done():
				return Sub2APIGroupRatioConfig{}, ctx.Err()
			case <-call.done:
				if call.err != nil {
					return Sub2APIGroupRatioConfig{}, call.err
				}
				config.BaseURL = baseURL
				config.AuthType = Sub2APIAuthToken
				config.AccessToken = call.entry.accessToken
				return config, nil
			}
		}
		call := &sub2APIRefreshTokenCall{done: make(chan struct{})}
		sub2APIRefreshTokenCache.pending[cacheKey] = call
		sub2APIRefreshTokenCache.Unlock()

		entry, refreshErr := refreshSub2APIToken(ctx, client, baseURL, refreshToken, validateURL)
		if refreshErr == nil && entry.refreshToken != refreshToken && config.CredentialID > 0 {
			var rotated bool
			var persistErr error
			if config.RefreshTokenStoredSeparately {
				rotated, persistErr = model.RotateChannelRatioUpstreamRefreshToken(
					config.CredentialID,
					Sub2APIUpstreamType,
					Sub2APIAuthToken,
					config.Revision,
					refreshToken,
					entry.refreshToken,
				)
			} else {
				rotated, persistErr = model.RotateChannelRatioUpstreamCredential(
					config.CredentialID,
					Sub2APIUpstreamType,
					Sub2APIAuthRefreshToken,
					config.Revision,
					refreshToken,
					entry.refreshToken,
				)
			}
			if persistErr != nil {
				refreshErr = fmt.Errorf("Sub2API Refresh Token 轮换保存失败: %w", persistErr)
			} else if !rotated {
				refreshErr = errors.New("渠道监控配置已变更，未保存新的 Sub2API Refresh Token")
			}
		}
		if refreshErr != nil {
			refreshErr = redactUpstreamGroupRatioSecrets(refreshErr, refreshToken, entry.refreshToken)
		}
		entry.familyKey = familyKey
		sub2APIRefreshTokenCache.Lock()
		delete(sub2APIRefreshTokenCache.pending, cacheKey)
		call.entry = entry
		call.err = refreshErr
		if refreshErr == nil {
			for _, aliasKey := range aliasKeys {
				sub2APIRefreshTokenCache.tokens[aliasKey] = entry
			}
			sub2APIRefreshTokenCache.tokens[originalCacheKey] = entry
			if entry.refreshToken != refreshToken {
				rotatedKey := sub2APIRefreshTokenCacheKey(baseURL, entry.refreshToken, config.Proxy, config.CredentialID, config.Revision)
				sub2APIRefreshTokenCache.tokens[rotatedKey] = entry
			}
		}
		close(call.done)
		sub2APIRefreshTokenCache.Unlock()
		if refreshErr != nil {
			return Sub2APIGroupRatioConfig{}, refreshErr
		}
		config.BaseURL = baseURL
		config.AuthType = Sub2APIAuthToken
		config.AccessToken = entry.accessToken
		return config, nil
	}
}

func invalidateSub2APIRefreshToken(config Sub2APIGroupRatioConfig) {
	baseURL, refreshToken, err := normalizeSub2APIRefreshTokenConfig(config)
	if err != nil {
		return
	}
	key := sub2APIRefreshTokenCacheKey(baseURL, refreshToken, config.Proxy, config.CredentialID, config.Revision)
	sub2APIRefreshTokenCache.Lock()
	familyKey := key
	if entry, ok := sub2APIRefreshTokenCache.tokens[key]; ok && entry.familyKey != ([32]byte{}) {
		familyKey = entry.familyKey
	}
	now := time.Now()
	for cachedKey, entry := range sub2APIRefreshTokenCache.tokens {
		if cachedKey == key || entry.familyKey == familyKey {
			// Keep the rotation alias briefly after invalidating the access token.
			// Concurrent requests may still hold the previous one-time credential.
			entry.expiresAt = now.Add(-time.Second)
			sub2APIRefreshTokenCache.tokens[cachedKey] = entry
		}
	}
	sub2APIRefreshTokenCache.Unlock()
}

// CanonicalSub2APIRefreshToken returns the latest rotated credential when a
// refresh was already performed for this monitor configuration. It is used by
// save paths after a test or group discovery request has consumed a one-time
// refresh token.
func CanonicalSub2APIRefreshToken(config ChannelMonitorUpstreamConfig) string {
	baseURL, refreshToken, err := normalizeSub2APIRefreshTokenConfig(Sub2APIGroupRatioConfig{
		BaseURL:      config.BaseURL,
		AccessToken:  config.AccessToken,
		Proxy:        config.Proxy,
		CredentialID: config.CredentialID,
		Revision:     config.Revision,
	})
	if err != nil {
		return strings.TrimSpace(config.AccessToken)
	}
	key := sub2APIRefreshTokenCacheKey(baseURL, refreshToken, config.Proxy, config.CredentialID, config.Revision)
	now := time.Now()
	sub2APIRefreshTokenCache.Lock()
	defer sub2APIRefreshTokenCache.Unlock()
	visited := make(map[[32]byte]struct{})
	for {
		if _, seen := visited[key]; seen {
			return refreshToken
		}
		visited[key] = struct{}{}
		entry, ok := sub2APIRefreshTokenCache.tokens[key]
		if !ok || !now.Before(entry.expiresAt.Add(sub2APIRefreshTokenAliasRetention)) || entry.refreshToken == "" || entry.refreshToken == refreshToken {
			return refreshToken
		}
		refreshToken = entry.refreshToken
		key = sub2APIRefreshTokenCacheKey(baseURL, refreshToken, config.Proxy, config.CredentialID, config.Revision)
	}
}

// ForgetSub2APIRefreshTokenCache drops cached access credentials for a monitor
// after its configuration is replaced or disabled.
func ForgetSub2APIRefreshTokenCache(config ChannelMonitorUpstreamConfig) {
	baseURL, refreshToken, err := normalizeSub2APIRefreshTokenConfig(Sub2APIGroupRatioConfig{
		BaseURL:      config.BaseURL,
		AccessToken:  config.AccessToken,
		Proxy:        config.Proxy,
		CredentialID: config.CredentialID,
		Revision:     config.Revision,
	})
	if err != nil {
		return
	}
	key := sub2APIRefreshTokenCacheKey(baseURL, refreshToken, config.Proxy, config.CredentialID, config.Revision)
	sub2APIRefreshTokenCache.Lock()
	familyKey := key
	if entry, ok := sub2APIRefreshTokenCache.tokens[key]; ok && entry.familyKey != ([32]byte{}) {
		familyKey = entry.familyKey
	}
	for cachedKey, entry := range sub2APIRefreshTokenCache.tokens {
		if cachedKey == key || entry.familyKey == familyKey {
			delete(sub2APIRefreshTokenCache.tokens, cachedKey)
		}
	}
	sub2APIRefreshTokenCache.Unlock()
}

func refreshSub2APIToken(ctx context.Context, client *http.Client, baseURL string, refreshToken string, validateURL func(string) error) (sub2APIRefreshTokenCacheEntry, error) {
	requestBody, err := common.Marshal(sub2APIRefreshTokenRequest{RefreshToken: refreshToken})
	if err != nil {
		return sub2APIRefreshTokenCacheEntry{}, errors.New("Sub2API Refresh Token 请求生成失败")
	}
	requestURL := baseURL + "/api/v1/auth/refresh"
	if validateURL != nil {
		if err := validateURL(requestURL); err != nil {
			return sub2APIRefreshTokenCacheEntry{}, err
		}
	}
	requestContext, cancel := continueChannelMonitorUpstreamRequest(ctx)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodPost, requestURL, bytes.NewReader(requestBody))
	if err != nil {
		return sub2APIRefreshTokenCacheEntry{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return sub2APIRefreshTokenCacheEntry{}, fmt.Errorf("Sub2API Refresh Token 请求失败: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxUpstreamGroupRatioResponseBytes+1))
	if err != nil {
		return sub2APIRefreshTokenCacheEntry{}, fmt.Errorf("Sub2API Refresh Token 请求失败: %w", err)
	}
	if len(body) > maxUpstreamGroupRatioResponseBytes {
		return sub2APIRefreshTokenCacheEntry{}, errors.New("Sub2API Refresh Token 响应过大")
	}
	var payload sub2APIResponse
	if err := common.Unmarshal(body, &payload); err != nil {
		upstreamErr := fmt.Errorf("Sub2API Refresh Token 响应格式无效: 上游返回 %s", response.Status)
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			return sub2APIRefreshTokenCacheEntry{}, &channelMonitorUpstreamAuthenticationError{cause: upstreamErr}
		}
		return sub2APIRefreshTokenCacheEntry{}, upstreamErr
	}
	if response.StatusCode != http.StatusOK || payload.Code != 0 {
		message := strings.TrimSpace(payload.Message)
		if message == "" {
			message = response.Status
		}
		upstreamErr := fmt.Errorf("Sub2API Refresh Token 失败: %w", upstreamGroupRatioMessage(message))
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden ||
			payload.Code == http.StatusUnauthorized || payload.Code == http.StatusForbidden {
			return sub2APIRefreshTokenCacheEntry{}, &channelMonitorUpstreamAuthenticationError{cause: upstreamErr}
		}
		return sub2APIRefreshTokenCacheEntry{}, upstreamErr
	}
	var result sub2APIRefreshTokenResult
	if err := common.Unmarshal(payload.Data, &result); err != nil {
		return sub2APIRefreshTokenCacheEntry{}, errors.New("Sub2API Refresh Token 响应格式无效")
	}
	accessToken := strings.TrimSpace(result.AccessToken)
	if accessToken == "" {
		return sub2APIRefreshTokenCacheEntry{}, errors.New("Sub2API Refresh Token 刷新成功但未返回访问 Token")
	}
	if len([]rune(accessToken)) > maxSub2APIAccessTokenLength {
		return sub2APIRefreshTokenCacheEntry{}, errors.New("Sub2API 返回的访问 Token 过长")
	}
	rotatedRefreshToken := strings.TrimSpace(result.RefreshToken)
	if rotatedRefreshToken == "" {
		rotatedRefreshToken = refreshToken
	}
	if len([]rune(rotatedRefreshToken)) > maxSub2APIRefreshTokenLength {
		return sub2APIRefreshTokenCacheEntry{}, errors.New("Sub2API 返回的 Refresh Token 过长")
	}
	return sub2APIRefreshTokenCacheEntry{
		accessToken:  accessToken,
		refreshToken: rotatedRefreshToken,
		expiresAt:    time.Now().Add(sub2APIRefreshTokenTTL(result.ExpiresIn)),
	}, nil
}

func normalizeSub2APIAccountConfig(config Sub2APIGroupRatioConfig) (string, string, string, error) {
	baseURL, err := normalizeSub2APIBaseURL(config.BaseURL)
	if err != nil {
		return "", "", "", err
	}
	account := strings.TrimSpace(config.Account)
	if account == "" {
		return "", "", "", errors.New("请输入 Sub2API 登录邮箱")
	}
	if len([]rune(account)) > maxSub2APIAccountLength {
		return "", "", "", errors.New("Sub2API 登录邮箱过长")
	}
	if config.Password == "" {
		return "", "", "", errors.New("请输入 Sub2API 登录密码")
	}
	if len([]rune(config.Password)) > maxSub2APIPasswordLength {
		return "", "", "", errors.New("Sub2API 登录密码过长")
	}
	return baseURL, account, config.Password, nil
}

func sub2APIAccountTokenCacheKey(baseURL string, account string, password string, proxy string) [32]byte {
	return sha256.Sum256([]byte(baseURL + "\x00" + account + "\x00" + password + "\x00" + strings.TrimSpace(proxy)))
}

func resolveSub2APIAccountTokenConfig(ctx context.Context, client *http.Client, config Sub2APIGroupRatioConfig, validateURL func(string) error) (Sub2APIGroupRatioConfig, error) {
	baseURL, account, password, err := normalizeSub2APIAccountConfig(config)
	if err != nil {
		return Sub2APIGroupRatioConfig{}, err
	}
	cacheKey := sub2APIAccountTokenCacheKey(baseURL, account, password, config.Proxy)

	for {
		now := time.Now()
		sub2APIAccountTokenCache.Lock()
		for key, entry := range sub2APIAccountTokenCache.tokens {
			if !now.Before(entry.expiresAt) {
				delete(sub2APIAccountTokenCache.tokens, key)
			}
		}
		if entry, ok := sub2APIAccountTokenCache.tokens[cacheKey]; ok {
			sub2APIAccountTokenCache.Unlock()
			config.BaseURL = baseURL
			config.AuthType = Sub2APIAuthToken
			config.AccessToken = entry.accessToken
			return config, nil
		}
		if call, ok := sub2APIAccountTokenCache.pending[cacheKey]; ok {
			sub2APIAccountTokenCache.Unlock()
			select {
			case <-ctx.Done():
				return Sub2APIGroupRatioConfig{}, ctx.Err()
			case <-call.done:
				if call.err != nil {
					return Sub2APIGroupRatioConfig{}, call.err
				}
				config.BaseURL = baseURL
				config.AuthType = Sub2APIAuthToken
				config.AccessToken = call.accessToken
				return config, nil
			}
		}
		call := &sub2APIAccountTokenCall{done: make(chan struct{})}
		sub2APIAccountTokenCache.pending[cacheKey] = call
		sub2APIAccountTokenCache.Unlock()

		accessToken, expiresIn, loginErr := loginSub2APIAccount(ctx, client, baseURL, account, password, validateURL)
		if loginErr != nil {
			loginErr = redactUpstreamGroupRatioSecrets(loginErr, account, password)
		}
		sub2APIAccountTokenCache.Lock()
		delete(sub2APIAccountTokenCache.pending, cacheKey)
		call.accessToken = accessToken
		call.err = loginErr
		if loginErr == nil {
			ttl := 5 * time.Minute
			if expiresIn > 0 {
				const maxTokenCacheTTL = 24 * time.Hour
				ttl = time.Duration(expiresIn) * time.Second
				if ttl <= 0 || ttl > maxTokenCacheTTL {
					ttl = maxTokenCacheTTL
				}
				safetyWindow := time.Minute
				if ttl <= 2*time.Minute {
					safetyWindow = ttl / 4
				}
				ttl -= safetyWindow
				if ttl <= 0 {
					ttl = time.Second
				}
			}
			sub2APIAccountTokenCache.tokens[cacheKey] = sub2APIAccountTokenCacheEntry{
				accessToken: accessToken,
				expiresAt:   time.Now().Add(ttl),
			}
		}
		close(call.done)
		sub2APIAccountTokenCache.Unlock()
		if loginErr != nil {
			return Sub2APIGroupRatioConfig{}, loginErr
		}

		config.BaseURL = baseURL
		config.AuthType = Sub2APIAuthToken
		config.AccessToken = accessToken
		return config, nil
	}
}

func invalidateSub2APIAccountToken(config Sub2APIGroupRatioConfig) {
	baseURL, account, password, err := normalizeSub2APIAccountConfig(config)
	if err != nil {
		return
	}
	cacheKey := sub2APIAccountTokenCacheKey(baseURL, account, password, config.Proxy)
	sub2APIAccountTokenCache.Lock()
	delete(sub2APIAccountTokenCache.tokens, cacheKey)
	sub2APIAccountTokenCache.Unlock()
}

func loginSub2APIAccount(ctx context.Context, client *http.Client, baseURL string, account string, password string, validateURL func(string) error) (string, int, error) {
	requestBody, err := common.Marshal(sub2APIAccountLoginRequest{
		Email:    account,
		Password: password,
	})
	if err != nil {
		return "", 0, errors.New("Sub2API 登录请求生成失败")
	}
	requestURL := baseURL + "/api/v1/auth/login"
	if validateURL != nil {
		if err := validateURL(requestURL); err != nil {
			return "", 0, err
		}
	}
	requestContext, cancel := continueChannelMonitorUpstreamRequest(ctx)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(requestContext, http.MethodPost, requestURL, bytes.NewReader(requestBody))
	if err != nil {
		return "", 0, err
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Content-Type", "application/json")

	response, err := client.Do(httpRequest)
	if err != nil {
		return "", 0, fmt.Errorf("Sub2API 账号密码自动登录失败: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxUpstreamGroupRatioResponseBytes+1))
	if err != nil {
		return "", 0, fmt.Errorf("Sub2API 账号密码自动登录失败: %w", err)
	}
	if len(responseBody) > maxUpstreamGroupRatioResponseBytes {
		return "", 0, errors.New("Sub2API 登录响应过大")
	}
	bodyText := strings.ToLower(string(responseBody))
	cloudflareChallenge := strings.EqualFold(strings.TrimSpace(response.Header.Get("cf-mitigated")), "challenge") ||
		((response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusServiceUnavailable) &&
			(strings.Contains(bodyText, "/cdn-cgi/challenge-platform") ||
				strings.Contains(bodyText, "cf-chl-") ||
				(strings.Contains(bodyText, "cloudflare") && strings.Contains(bodyText, "challenge"))))
	if cloudflareChallenge {
		return "", 0, &channelMonitorUpstreamAuthenticationError{cause: errors.New("Sub2API 账号密码自动登录触发了 Cloudflare 人机验证，无法进行无人值守登录，请改用 Refresh Token 或手动 Token")}
	}

	var payload sub2APIResponse
	if err := common.Unmarshal(responseBody, &payload); err != nil {
		if response.StatusCode == http.StatusForbidden {
			return "", 0, &channelMonitorUpstreamAuthenticationError{cause: errors.New("Sub2API 账号密码自动登录被上游拒绝；如果启用了 Cloudflare Turnstile、WAF 人机验证或其他验证码，无法进行无人值守登录，请改用 Refresh Token 或手动 Token")}
		}
		return "", 0, fmt.Errorf("Sub2API 登录响应格式无效: 上游返回 %s", response.Status)
	}
	if response.StatusCode != http.StatusOK || payload.Code != 0 {
		reason := strings.ToUpper(strings.TrimSpace(payload.Reason))
		message := strings.TrimSpace(payload.Message)
		lowerMessage := strings.ToLower(message)
		if strings.Contains(reason, "TOTP") || strings.Contains(reason, "2FA") ||
			strings.Contains(lowerMessage, "totp") || strings.Contains(lowerMessage, "2fa") ||
			strings.Contains(message, "两步验证") || strings.Contains(message, "二次验证") {
			return "", 0, &channelMonitorUpstreamAuthenticationError{cause: errors.New("Sub2API 账号已开启 TOTP 两步验证，无法仅凭账号密码自动登录，请改用 Refresh Token、手动 Token 或未启用两步验证的专用账号")}
		}
		if strings.Contains(reason, "TURNSTILE") || strings.Contains(reason, "CAPTCHA") ||
			strings.Contains(lowerMessage, "turnstile") || strings.Contains(lowerMessage, "captcha") ||
			strings.Contains(message, "验证码") {
			return "", 0, &channelMonitorUpstreamAuthenticationError{cause: errors.New("上游已开启 Cloudflare Turnstile 或验证码，账号密码无法完成无人值守登录，请改用 Refresh Token、手动 Token 或为监控使用未启用交互验证的专用账号")}
		}
		if message == "" {
			message = response.Status
		}
		upstreamErr := fmt.Errorf("Sub2API 账号密码自动登录失败: %w", upstreamGroupRatioMessage(message))
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden ||
			payload.Code == http.StatusUnauthorized || payload.Code == http.StatusForbidden || reason == "INVALID_CREDENTIALS" {
			return "", 0, &channelMonitorUpstreamAuthenticationError{cause: upstreamErr}
		}
		return "", 0, upstreamErr
	}

	var result sub2APIAccountLoginResult
	if err := common.Unmarshal(payload.Data, &result); err != nil {
		return "", 0, errors.New("Sub2API 登录响应格式无效")
	}
	if result.Requires2FA {
		return "", 0, &channelMonitorUpstreamAuthenticationError{cause: errors.New("Sub2API 账号已开启 TOTP 两步验证，无法仅凭账号密码自动登录，请改用 Refresh Token、手动 Token 或未启用两步验证的专用账号")}
	}
	accessToken := strings.TrimSpace(result.AccessToken)
	if accessToken == "" {
		return "", 0, errors.New("Sub2API 登录成功但未返回访问 Token")
	}
	return accessToken, result.ExpiresIn, nil
}
