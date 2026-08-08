package model

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
)

func TestGetBaseURLHandlesUnknownChannelType(t *testing.T) {
	emptyBaseURL := ""
	channel := &Channel{
		Type:    len(constant.ChannelBaseURLs) + 100,
		BaseURL: &emptyBaseURL,
	}

	assert.NotPanics(t, func() {
		assert.Empty(t, channel.GetBaseURL())
	})
}

func TestGetBaseURLFallsBackWhenBaseURLIsNil(t *testing.T) {
	channel := &Channel{Type: constant.ChannelTypeOpenAI}

	assert.Equal(t, constant.ChannelBaseURLs[constant.ChannelTypeOpenAI], channel.GetBaseURL())
}

func TestGetBaseURLHandlesNilChannel(t *testing.T) {
	var channel *Channel

	assert.Empty(t, channel.GetBaseURL())
}
