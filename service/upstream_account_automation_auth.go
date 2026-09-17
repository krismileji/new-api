package service

import (
	"context"
	"net/http"
	"strconv"
	"strings"
)

// Built-in accounts supply authentication for actions against their own base
// URL. Custom accounts resolve their explicitly configured header templates.
func authorizeUpstreamAccountAction(ctx context.Context, client *http.Client, config UpstreamAutomationConfig, request ChannelMonitorCustomRequestConfig) (ChannelMonitorCustomRequestConfig, error) {
	upstream, err := ResolveUpstreamAccountRequest(ctx, ChannelMonitorUpstreamConfig{AccountID: config.AccountID, AccountRevision: config.AccountRevision})
	if err != nil || upstream.Type == CustomUpstreamType {
		return request, err
	}
	for _, header := range request.Headers {
		if strings.EqualFold(header.Key, "Authorization") {
			return request, nil
		}
	}
	token := upstream.AccessToken
	if upstream.Type == NewAPIUpstreamType && upstream.AuthType == NewAPIUpstreamAuthUser {
		request.Headers = append(request.Headers, ChannelMonitorCustomKeyValue{Key: "New-Api-User", Value: strconv.Itoa(upstream.UserID)})
	}
	if upstream.Type == Sub2APIUpstreamType {
		credentials := Sub2APIGroupRatioConfig{BaseURL: upstream.BaseURL, AuthType: upstream.AuthType,
			AccessToken: upstream.AccessToken, RefreshToken: upstream.RefreshToken, RefreshTokenStoredSeparately: upstream.RefreshTokenStoredSeparately,
			Account: upstream.Account, Password: upstream.Password, Proxy: upstream.Proxy,
			AccountID: config.AccountID, CredentialID: upstream.CredentialID, Revision: upstream.Revision}
		switch upstream.AuthType {
		case Sub2APIAuthRefreshToken:
			credentials, err = resolveSub2APIRefreshTokenConfig(ctx, client, credentials, ValidateSSRFProtectedFetchURL)
			token = credentials.AccessToken
		case Sub2APIAuthAccount:
			credentials, err = resolveSub2APIAccountTokenConfig(ctx, client, credentials, ValidateSSRFProtectedFetchURL)
			token = credentials.AccessToken
		case Sub2APIAuthAPIKey:
			token = upstream.ChannelKeys[0]
		}
	}
	if err != nil {
		return request, err
	}
	if token != "" {
		request.Headers = append(request.Headers, ChannelMonitorCustomKeyValue{Key: "Authorization", Value: "Bearer " + strings.TrimPrefix(token, "Bearer "), Secret: true})
	}
	return request, nil
}
