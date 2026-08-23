package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeLogicalChannelAddressCanonicalizesOriginAndPath(t *testing.T) {
	got, err := NormalizeLogicalChannelAddress("  HTTPS://Example.COM:443/api/v1///#ignored  ")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/api/v1", got)

	got, err = NormalizeLogicalChannelAddress("http://[2001:DB8::1]:80///")
	require.NoError(t, err)
	assert.Equal(t, "http://[2001:db8::1]", got)

	got, err = NormalizeLogicalChannelAddress("https://example.com/v1%2F")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/v1%2F", got)
}

func TestNormalizeLogicalChannelAddressKeepsSemanticQueryAndDropsCredentials(t *testing.T) {
	got, err := NormalizeLogicalChannelAddress("https://API.Example/v1?z=2&api_key=sk-secret&api-version=2024-01&z=1&access_token=full-token")
	require.NoError(t, err)
	assert.Equal(t, "https://api.example/v1?api-version=2024-01&z=1&z=2", got)
	assert.NotContains(t, got, "sk-secret")
	assert.NotContains(t, got, "full-token")
	assert.NotContains(t, LogicalChannelAddressFingerprint(got), "sk-secret")
}

func TestNormalizeLogicalChannelAddressRejectsInvalidInputs(t *testing.T) {
	for _, value := range []string{"", "example.com", "ftp://example.com", "https://user:pass@example.com", "https://example.com/%zz"} {
		_, err := NormalizeLogicalChannelAddress(value)
		assert.Error(t, err, value)
	}
	_, err := NormalizeLogicalChannelAddress("https://example.com/" + strings.Repeat("a", maxLogicalChannelAddressLength))
	assert.Error(t, err)
}

func TestNormalizeLogicalChannelAddressForChannelUsesDefaultBaseURL(t *testing.T) {
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI}
	got, err := NormalizeLogicalChannelAddressForChannel(channel)
	require.NoError(t, err)
	assert.Equal(t, "https://api.openai.com", got)
}

func TestPrecheckLogicalChannelAddressesOnlyComparesNormalizedAddress(t *testing.T) {
	result := PrecheckLogicalChannelAddresses([]LogicalChannelAddressInput{
		{ChannelID: 1, Address: "HTTPS://Example.com:443/v1/"},
		{ChannelID: 2, Address: "https://example.com/v1"},
	})
	require.True(t, result.Compatible)
	assert.Len(t, result.Members, 2)
	assert.NotEmpty(t, result.AddressFingerprint)

	result = PrecheckLogicalChannelAddresses([]LogicalChannelAddressInput{
		{ChannelID: 1, Address: "https://example.com/v1"},
		{ChannelID: 2, Address: "https://example.com/v2"},
	})
	assert.False(t, result.Compatible)
	assert.Equal(t, "渠道请求地址不一致", result.Error)
}

func TestPrecheckLogicalChannelMembersUsesEffectiveBaseURL(t *testing.T) {
	baseOne := " https://example.com/v1/ "
	baseTwo := "https://EXAMPLE.com:443/v1"
	result := PrecheckLogicalChannelMembers([]*model.Channel{
		{Id: 1, BaseURL: &baseOne},
		{Id: 2, BaseURL: &baseTwo},
	})
	assert.True(t, result.Compatible)
}
