package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetSub2APIAccountTokenCache(t *testing.T) {
	t.Helper()
	reset := func() {
		sub2APIAccountTokenCache.Lock()
		sub2APIAccountTokenCache.tokens = make(map[[32]byte]sub2APIAccountTokenCacheEntry)
		sub2APIAccountTokenCache.pending = make(map[[32]byte]*sub2APIAccountTokenCall)
		sub2APIAccountTokenCache.Unlock()
	}
	reset()
	t.Cleanup(reset)
}

func resetSub2APIRefreshTokenCache(t *testing.T) {
	t.Helper()
	reset := func() {
		sub2APIRefreshTokenCache.Lock()
		sub2APIRefreshTokenCache.tokens = make(map[[32]byte]sub2APIRefreshTokenCacheEntry)
		sub2APIRefreshTokenCache.pending = make(map[[32]byte]*sub2APIRefreshTokenCall)
		sub2APIRefreshTokenCache.Unlock()
	}
	reset()
	t.Cleanup(reset)
}

func TestFetchSub2APIRefreshTokenRotatesAndCachesCredential(t *testing.T) {
	resetSub2APIRefreshTokenCache(t)
	var refreshRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			refreshRequests.Add(1)
			var request sub2APIRefreshTokenRequest
			require.NoError(t, common.DecodeJson(r.Body, &request))
			assert.Equal(t, "rt-original", request.RefreshToken)
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"access_token":"access-fresh","refresh_token":"rt-rotated","expires_in":3600}}`))
		case "/api/v1/groups/available":
			assert.Equal(t, "Bearer access-fresh", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[{"id":7,"name":"vip","rate_multiplier":1.25}]}`))
		case "/api/v1/groups/rates":
			assert.Equal(t, "Bearer access-fresh", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	config := Sub2APIGroupRatioConfig{
		BaseURL:     server.URL,
		Group:       "vip",
		AuthType:    Sub2APIAuthRefreshToken,
		AccessToken: "rt-original",
		Revision:    1,
		SkipBalance: true,
	}
	result, err := fetchSub2APIGroupRatio(context.Background(), server.Client(), config, nil)
	require.NoError(t, err)
	assert.InDelta(t, 1.25, result.Ratio, 1e-9)
	assert.EqualValues(t, 1, refreshRequests.Load())
	assert.Equal(t, "rt-rotated", CanonicalSub2APIRefreshToken(ChannelMonitorUpstreamConfig{
		BaseURL:     server.URL,
		AuthType:    Sub2APIAuthRefreshToken,
		AccessToken: "rt-original",
		Revision:    1,
	}))
}

func TestFetchSub2APIRefreshTokenRetriesAfterAccessTokenRejection(t *testing.T) {
	resetSub2APIRefreshTokenCache(t)
	var refreshRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			requestNumber := refreshRequests.Add(1)
			accessToken := "access-fresh"
			refreshToken := "rt-next"
			if requestNumber > 1 {
				accessToken = "access-fresh-2"
				refreshToken = "rt-final"
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"access_token":"` + accessToken + `","refresh_token":"` + refreshToken + `","expires_in":3600}}`))
		case "/api/v1/groups/available":
			if r.Header.Get("Authorization") == "Bearer access-fresh" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"code":401,"message":"token expired"}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[{"id":7,"name":"vip","rate_multiplier":1.5}]}`))
		case "/api/v1/groups/rates":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := fetchSub2APIGroupRatio(context.Background(), server.Client(), Sub2APIGroupRatioConfig{
		BaseURL:     server.URL,
		Group:       "vip",
		AuthType:    Sub2APIAuthRefreshToken,
		AccessToken: "rt-original",
		Revision:    2,
		SkipBalance: true,
	}, nil)
	require.NoError(t, err)
	assert.EqualValues(t, 2, refreshRequests.Load())
	assert.Equal(t, "rt-final", CanonicalSub2APIRefreshToken(ChannelMonitorUpstreamConfig{
		BaseURL:     server.URL,
		AuthType:    Sub2APIAuthRefreshToken,
		AccessToken: "rt-original",
		Revision:    2,
	}))
}

func TestFetchSub2APITokenUsesRefreshTokenOnlyAfterAccessTokenRejection(t *testing.T) {
	resetSub2APIRefreshTokenCache(t)
	var refreshRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			refreshRequests.Add(1)
			var request sub2APIRefreshTokenRequest
			require.NoError(t, common.DecodeJson(r.Body, &request))
			assert.Equal(t, "refresh-original", request.RefreshToken)
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"access_token":"access-fresh","refresh_token":"refresh-next","expires_in":3600}}`))
		case "/api/v1/groups/available":
			if r.Header.Get("Authorization") == "Bearer access-expired" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"code":401,"message":"token expired"}`))
				return
			}
			assert.Equal(t, "Bearer access-fresh", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[{"id":7,"name":"vip","rate_multiplier":1.25}]}`))
		case "/api/v1/groups/rates":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, err := fetchSub2APIGroupRatio(context.Background(), server.Client(), Sub2APIGroupRatioConfig{
		BaseURL:      server.URL,
		Group:        "vip",
		AuthType:     Sub2APIAuthToken,
		AccessToken:  "access-expired",
		RefreshToken: "refresh-original",
		Revision:     3,
		SkipBalance:  true,
	}, nil)
	require.NoError(t, err)
	assert.InDelta(t, 1.25, result.Ratio, 1e-9)
	assert.EqualValues(t, 1, refreshRequests.Load())
}

func TestFetchSub2APITokenPersistsRotatedSeparateRefreshToken(t *testing.T) {
	resetSub2APIRefreshTokenCache(t)
	require.NoError(t, model.DB.AutoMigrate(&model.ChannelRatioMonitor{}))
	require.NoError(t, model.DB.Create(&model.Channel{Id: 9019, Name: "token and refresh-token monitor"}).Error)
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("channel_id = ?", 9019).Delete(&model.ChannelRatioMonitor{}).Error)
		require.NoError(t, model.DB.Where("id = ?", 9019).Delete(&model.Channel{}).Error)
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"access_token":"access-fresh","refresh_token":"refresh-new","expires_in":3600}}`))
		case "/api/v1/groups/available":
			if r.Header.Get("Authorization") == "Bearer access-expired" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"code":401,"message":"token expired"}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[{"id":7,"name":"vip","rate_multiplier":1.25}]}`))
		case "/api/v1/groups/rates":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	require.NoError(t, model.DB.Create(&model.ChannelRatioMonitor{
		ChannelId:            9019,
		UpstreamType:         Sub2APIUpstreamType,
		UpstreamBaseURL:      server.URL,
		UpstreamAuthType:     Sub2APIAuthToken,
		UpstreamAccessToken:  "access-expired",
		UpstreamRefreshToken: "refresh-old",
		UpstreamRevision:     8,
	}).Error)

	_, err := fetchSub2APIGroupRatio(context.Background(), server.Client(), Sub2APIGroupRatioConfig{
		BaseURL:                      server.URL,
		Group:                        "vip",
		AuthType:                     Sub2APIAuthToken,
		AccessToken:                  "access-expired",
		RefreshToken:                 "refresh-old",
		RefreshTokenStoredSeparately: true,
		CredentialID:                 9019,
		Revision:                     8,
		SkipBalance:                  true,
	}, nil)
	require.NoError(t, err)
	monitor, err := model.GetChannelRatioMonitor(9019)
	require.NoError(t, err)
	assert.Equal(t, "access-expired", monitor.UpstreamAccessToken)
	assert.Equal(t, "refresh-new", monitor.UpstreamRefreshToken)
	assert.Equal(t, int64(8), monitor.UpstreamRevision)
}

func TestFetchSub2APIRefreshTokenRetriesAfterMultipleRotations(t *testing.T) {
	resetSub2APIRefreshTokenCache(t)
	var refreshRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			requestNumber := refreshRequests.Add(1)
			_, _ = w.Write([]byte(fmt.Sprintf(
				`{"code":0,"message":"success","data":{"access_token":"access-%d","refresh_token":"refresh-%d","expires_in":3600}}`,
				requestNumber,
				requestNumber,
			)))
		case "/api/v1/groups/available":
			if r.Header.Get("Authorization") != "Bearer access-3" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"code":401,"message":"token expired"}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[{"id":7,"name":"vip","rate_multiplier":1.5}]}`))
		case "/api/v1/groups/rates":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	config := Sub2APIGroupRatioConfig{
		BaseURL:     server.URL,
		Group:       "vip",
		AuthType:    Sub2APIAuthRefreshToken,
		AccessToken: "refresh-original",
		Revision:    8,
		SkipBalance: true,
	}
	_, err := fetchSub2APIGroupRatio(context.Background(), server.Client(), config, nil)
	require.Error(t, err)
	assert.EqualValues(t, 2, refreshRequests.Load())

	result, err := fetchSub2APIGroupRatio(context.Background(), server.Client(), config, nil)
	require.NoError(t, err)
	assert.InDelta(t, 1.5, result.Ratio, 1e-9)
	assert.EqualValues(t, 3, refreshRequests.Load())
}

func TestFetchSub2APIRefreshTokenRetriesBalanceAuthenticationFailure(t *testing.T) {
	resetSub2APIRefreshTokenCache(t)
	var refreshRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			requestNumber := refreshRequests.Add(1)
			_, _ = w.Write([]byte(fmt.Sprintf(
				`{"code":0,"message":"success","data":{"access_token":"access-%d","refresh_token":"refresh-%d","expires_in":3600}}`,
				requestNumber,
				requestNumber,
			)))
		case "/api/v1/groups/available":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[{"id":7,"name":"vip","rate_multiplier":1.25}]}`))
		case "/api/v1/groups/rates":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{}}`))
		case "/api/v1/user/profile":
			if r.Header.Get("Authorization") == "Bearer access-1" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"code":401,"message":"token expired"}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"balance":42.5}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, err := fetchSub2APIGroupRatio(context.Background(), server.Client(), Sub2APIGroupRatioConfig{
		BaseURL:     server.URL,
		Group:       "vip",
		AuthType:    Sub2APIAuthRefreshToken,
		AccessToken: "refresh-original",
		Revision:    9,
	}, nil)
	require.NoError(t, err)
	require.NotNil(t, result.Balance.Amount)
	assert.InDelta(t, 42.5, *result.Balance.Amount, 1e-9)
	assert.EqualValues(t, 2, refreshRequests.Load())
}

func TestFetchSub2APIRefreshTokenRetriesAppliedGroupAuthenticationFailure(t *testing.T) {
	resetSub2APIRefreshTokenCache(t)
	var refreshRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			requestNumber := refreshRequests.Add(1)
			_, _ = w.Write([]byte(fmt.Sprintf(
				`{"code":0,"message":"success","data":{"access_token":"access-%d","refresh_token":"refresh-%d","expires_in":3600}}`,
				requestNumber,
				requestNumber,
			)))
		case "/api/v1/groups/available":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[{"id":7,"name":"vip","rate_multiplier":1.25}]}`))
		case "/api/v1/groups/rates":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{}}`))
		case "/api/v1/keys":
			if r.Header.Get("Authorization") == "Bearer access-1" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"code":401,"message":"token expired"}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"items":[{"id":11,"key":"sk-channel","group_id":7}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, err := fetchSub2APIUpstreamGroups(context.Background(), server.Client(), Sub2APIGroupRatioConfig{
		BaseURL:     server.URL,
		AuthType:    Sub2APIAuthRefreshToken,
		AccessToken: "refresh-original",
		Revision:    10,
		SkipBalance: true,
	}, []string{"sk-channel"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "vip", result.AppliedGroup)
	assert.Empty(t, result.AppliedGroupError)
	assert.EqualValues(t, 2, refreshRequests.Load())
}

func TestRefreshSub2APITokenClassifiesOnlyCredentialRejectionsAsAuthenticationFailures(t *testing.T) {
	tests := []struct {
		name               string
		statusCode         int
		body               string
		wantAuthentication bool
	}{
		{
			name:               "invalid refresh token",
			statusCode:         http.StatusUnauthorized,
			body:               `{"code":401,"message":"invalid refresh token","reason":"REFRESH_TOKEN_INVALID"}`,
			wantAuthentication: true,
		},
		{
			name:       "rate limited",
			statusCode: http.StatusTooManyRequests,
			body:       `{"code":429,"message":"too many requests"}`,
		},
		{
			name:       "service unavailable html",
			statusCode: http.StatusServiceUnavailable,
			body:       `<html>temporarily unavailable</html>`,
		},
		{
			name:       "malformed success payload",
			statusCode: http.StatusOK,
			body:       `{"code":0,"message":"success","data":{"expires_in":3600}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/api/v1/auth/refresh", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.statusCode)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()

			_, err := refreshSub2APIToken(context.Background(), server.Client(), server.URL, "refresh-secret", nil)
			require.Error(t, err)
			assert.Equal(t, test.wantAuthentication, errors.Is(err, ErrChannelMonitorUpstreamAuthentication))
		})
	}
}

func TestSub2APIRefreshTokenTTLBoundsUntrustedExpiry(t *testing.T) {
	assert.Equal(t, 5*time.Minute, sub2APIRefreshTokenTTL(0))
	assert.Equal(t, 90*time.Second, sub2APIRefreshTokenTTL(120))
	assert.Equal(t, 23*time.Hour+59*time.Minute, sub2APIRefreshTokenTTL(math.MaxInt))
}

func TestCanonicalSub2APIRefreshTokenSurvivesAccessTokenInvalidation(t *testing.T) {
	resetSub2APIRefreshTokenCache(t)
	config := ChannelMonitorUpstreamConfig{
		BaseURL:     "https://upstream.example",
		AuthType:    Sub2APIAuthRefreshToken,
		AccessToken: "refresh-old",
		Revision:    3,
	}
	key := sub2APIRefreshTokenCacheKey(config.BaseURL, config.AccessToken, config.Proxy, config.CredentialID, config.Revision)
	sub2APIRefreshTokenCache.Lock()
	sub2APIRefreshTokenCache.tokens[key] = sub2APIRefreshTokenCacheEntry{
		accessToken:  "access-expired",
		refreshToken: "refresh-new",
		expiresAt:    time.Now().Add(time.Hour),
	}
	sub2APIRefreshTokenCache.Unlock()

	invalidateSub2APIRefreshToken(Sub2APIGroupRatioConfig{
		BaseURL:     config.BaseURL,
		AuthType:    config.AuthType,
		AccessToken: config.AccessToken,
		Revision:    config.Revision,
	})

	assert.Equal(t, "refresh-new", CanonicalSub2APIRefreshToken(config))
}

func TestFetchSub2APIRefreshTokenReusesRotatedAliasAfterInvalidation(t *testing.T) {
	resetSub2APIRefreshTokenCache(t)
	var refreshRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/auth/refresh" {
			refreshRequests.Add(1)
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"access_token":"access-fresh","refresh_token":"refresh-new","expires_in":3600}}`))
			return
		}
		if r.URL.Path == "/api/v1/groups/available" {
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[{"id":7,"name":"vip","rate_multiplier":1.25}]}`))
			return
		}
		if r.URL.Path == "/api/v1/groups/rates" {
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	config := Sub2APIGroupRatioConfig{
		BaseURL:     server.URL,
		Group:       "vip",
		AuthType:    Sub2APIAuthRefreshToken,
		AccessToken: "refresh-old",
		Revision:    4,
		SkipBalance: true,
	}
	_, err := fetchSub2APIGroupRatio(context.Background(), server.Client(), config, nil)
	require.NoError(t, err)
	invalidateSub2APIRefreshToken(config)
	_, err = fetchSub2APIGroupRatio(context.Background(), server.Client(), config, nil)
	require.NoError(t, err)
	assert.EqualValues(t, 2, refreshRequests.Load())
}

func TestFetchSub2APIRefreshTokenCachesAcrossConcurrentRequests(t *testing.T) {
	resetSub2APIRefreshTokenCache(t)
	var refreshRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/auth/refresh" {
			refreshRequests.Add(1)
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"access_token":"access-fresh","refresh_token":"refresh-new","expires_in":3600}}`))
			return
		}
		if r.URL.Path == "/api/v1/groups/available" {
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[{"id":7,"name":"vip","rate_multiplier":1.25}]}`))
			return
		}
		if r.URL.Path == "/api/v1/groups/rates" {
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	config := Sub2APIGroupRatioConfig{
		BaseURL:     server.URL,
		Group:       "vip",
		AuthType:    Sub2APIAuthRefreshToken,
		AccessToken: "refresh-old",
		Revision:    5,
		SkipBalance: true,
	}
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := fetchSub2APIGroupRatio(context.Background(), server.Client(), config, nil)
			results <- err
		}()
	}
	for range 2 {
		require.NoError(t, <-results)
	}
	assert.EqualValues(t, 1, refreshRequests.Load())
}

func TestFetchSub2APIRefreshTokenPersistsRotationForSavedMonitor(t *testing.T) {
	resetSub2APIRefreshTokenCache(t)
	require.NoError(t, model.DB.AutoMigrate(&model.ChannelRatioMonitor{}))
	require.NoError(t, model.DB.Create(&model.Channel{Id: 9017, Name: "refresh-token monitor"}).Error)
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("channel_id = ?", 9017).Delete(&model.ChannelRatioMonitor{}).Error)
		require.NoError(t, model.DB.Where("id = ?", 9017).Delete(&model.Channel{}).Error)
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"access_token":"access-fresh","refresh_token":"refresh-new","expires_in":3600}}`))
		case "/api/v1/groups/available":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[{"id":7,"name":"vip","rate_multiplier":1.25}]}`))
		case "/api/v1/groups/rates":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	require.NoError(t, model.DB.Create(&model.ChannelRatioMonitor{
		ChannelId:           9017,
		UpstreamType:        Sub2APIUpstreamType,
		UpstreamBaseURL:     server.URL,
		UpstreamAuthType:    Sub2APIAuthRefreshToken,
		UpstreamAccessToken: "refresh-old",
		UpstreamRevision:    6,
	}).Error)

	_, err := fetchSub2APIGroupRatio(context.Background(), server.Client(), Sub2APIGroupRatioConfig{
		BaseURL:      server.URL,
		Group:        "vip",
		AuthType:     Sub2APIAuthRefreshToken,
		AccessToken:  "refresh-old",
		CredentialID: 9017,
		Revision:     6,
		SkipBalance:  true,
	}, nil)
	require.NoError(t, err)
	monitor, err := model.GetChannelRatioMonitor(9017)
	require.NoError(t, err)
	assert.Equal(t, "refresh-new", monitor.UpstreamAccessToken)
	assert.Equal(t, int64(6), monitor.UpstreamRevision)
}

func TestFetchSub2APIRefreshTokenRejectsStaleSavedMonitorRotation(t *testing.T) {
	resetSub2APIRefreshTokenCache(t)
	require.NoError(t, model.DB.AutoMigrate(&model.ChannelRatioMonitor{}))
	require.NoError(t, model.DB.Create(&model.Channel{Id: 9018, Name: "stale refresh-token monitor"}).Error)
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("channel_id = ?", 9018).Delete(&model.ChannelRatioMonitor{}).Error)
		require.NoError(t, model.DB.Where("id = ?", 9018).Delete(&model.Channel{}).Error)
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/auth/refresh" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"access_token":"access-fresh","refresh_token":"refresh-new","expires_in":3600}}`))
	}))
	defer server.Close()
	require.NoError(t, model.DB.Create(&model.ChannelRatioMonitor{
		ChannelId:           9018,
		UpstreamType:        Sub2APIUpstreamType,
		UpstreamBaseURL:     server.URL,
		UpstreamAuthType:    Sub2APIAuthRefreshToken,
		UpstreamAccessToken: "refresh-current",
		UpstreamRevision:    7,
	}).Error)

	_, err := fetchSub2APIGroupRatio(context.Background(), server.Client(), Sub2APIGroupRatioConfig{
		BaseURL:      server.URL,
		Group:        "vip",
		AuthType:     Sub2APIAuthRefreshToken,
		AccessToken:  "refresh-stale",
		CredentialID: 9018,
		Revision:     6,
		SkipBalance:  true,
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "配置已变更")
	monitor, findErr := model.GetChannelRatioMonitor(9018)
	require.NoError(t, findErr)
	assert.Equal(t, "refresh-current", monitor.UpstreamAccessToken)
}

func TestFetchSub2APIAccountLogsInAndCachesToken(t *testing.T) {
	resetSub2APIAccountTokenCache(t)
	var loginRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/login":
			loginRequests.Add(1)
			var request sub2APIAccountLoginRequest
			require.NoError(t, common.DecodeJson(r.Body, &request))
			assert.Equal(t, "monitor@example.com", request.Email)
			assert.Equal(t, "secret-password", request.Password)
			assert.Empty(t, request.TurnstileToken)
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"access_token":"auto-jwt","expires_in":3600,"token_type":"Bearer"}}`))
		case "/api/v1/groups/available":
			assert.Equal(t, "Bearer auto-jwt", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[{"id":7,"name":"vip","rate_multiplier":1.25}]}`))
		case "/api/v1/groups/rates":
			assert.Equal(t, "Bearer auto-jwt", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"7":1.75}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	config := Sub2APIGroupRatioConfig{
		BaseURL:     server.URL,
		Group:       "vip",
		AuthType:    Sub2APIAuthAccount,
		Account:     "monitor@example.com",
		Password:    "secret-password",
		SkipBalance: true,
	}
	for range 2 {
		result, err := fetchSub2APIGroupRatio(context.Background(), server.Client(), config, nil)
		require.NoError(t, err)
		assert.InDelta(t, 1.75, result.Ratio, 1e-9)
	}
	assert.EqualValues(t, 1, loginRequests.Load())
}

func TestFetchSub2APIAccountRefreshesRejectedCachedToken(t *testing.T) {
	resetSub2APIAccountTokenCache(t)
	var loginRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/login":
			token := "expired-jwt"
			if loginRequests.Add(1) == 2 {
				token = "fresh-jwt"
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"access_token":"` + token + `","expires_in":3600}}`))
		case "/api/v1/groups/available":
			if r.Header.Get("Authorization") == "Bearer expired-jwt" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"code":401,"message":"token expired","data":null}`))
				return
			}
			assert.Equal(t, "Bearer fresh-jwt", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[{"id":7,"name":"vip","rate_multiplier":1.5}]}`))
		case "/api/v1/groups/rates":
			assert.Equal(t, "Bearer fresh-jwt", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, err := fetchSub2APIGroupRatio(context.Background(), server.Client(), Sub2APIGroupRatioConfig{
		BaseURL:     server.URL,
		Group:       "vip",
		AuthType:    Sub2APIAuthAccount,
		Account:     "monitor@example.com",
		Password:    "secret-password",
		SkipBalance: true,
	}, nil)
	require.NoError(t, err)
	assert.InDelta(t, 1.5, result.Ratio, 1e-9)
	assert.EqualValues(t, 2, loginRequests.Load())
}

func TestFetchSub2APIAccountExplainsInteractiveLoginBlockers(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		contains   string
	}{
		{
			name:       "turnstile",
			statusCode: http.StatusBadRequest,
			body:       `{"code":400,"message":"turnstile verification failed","reason":"TURNSTILE_VERIFICATION_FAILED"}`,
			contains:   "Turnstile",
		},
		{
			name:       "totp",
			statusCode: http.StatusOK,
			body:       `{"code":0,"message":"success","data":{"requires_2fa":true,"temp_token":"temporary"}}`,
			contains:   "TOTP",
		},
		{
			name:       "cloudflare challenge",
			statusCode: http.StatusServiceUnavailable,
			body:       `<html>cloudflare challenge</html>`,
			contains:   "Cloudflare",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resetSub2APIAccountTokenCache(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/auth/login" {
					http.NotFound(w, r)
					return
				}
				if test.name == "cloudflare challenge" {
					w.Header().Set("cf-mitigated", "challenge")
				}
				w.WriteHeader(test.statusCode)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()

			_, err := fetchSub2APIGroupRatio(context.Background(), server.Client(), Sub2APIGroupRatioConfig{
				BaseURL:     server.URL,
				Group:       "vip",
				AuthType:    Sub2APIAuthAccount,
				Account:     "monitor@example.com",
				Password:    "secret-password",
				SkipBalance: true,
			}, nil)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrChannelMonitorUpstreamAuthentication)
			assert.True(t, strings.Contains(err.Error(), test.contains), err.Error())
			assert.NotContains(t, err.Error(), "secret-password")
		})
	}
}
