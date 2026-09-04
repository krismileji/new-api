package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
)

func TestSelectChannelGroupMonitorEndpointType(t *testing.T) {
	tests := []struct {
		name         string
		channel      *model.Channel
		wantEndpoint string
	}{
		{
			name:         "nil channel returns default OpenAI Response endpoint",
			channel:      nil,
			wantEndpoint: string(constant.EndpointTypeOpenAIResponse),
		},
		{
			name: "Anthropic channel returns Anthropic endpoint",
			channel: &model.Channel{
				Type: constant.ChannelTypeAnthropic,
			},
			wantEndpoint: string(constant.EndpointTypeAnthropic),
		},
		{
			name: "OpenAI channel returns OpenAI Response endpoint",
			channel: &model.Channel{
				Type: constant.ChannelTypeOpenAI,
			},
			wantEndpoint: string(constant.EndpointTypeOpenAIResponse),
		},
		{
			name: "Azure channel returns OpenAI Response endpoint",
			channel: &model.Channel{
				Type: constant.ChannelTypeAzure,
			},
			wantEndpoint: string(constant.EndpointTypeOpenAIResponse),
		},
		{
			name: "DeepSeek channel returns OpenAI Response endpoint",
			channel: &model.Channel{
				Type: constant.ChannelTypeDeepSeek,
			},
			wantEndpoint: string(constant.EndpointTypeOpenAIResponse),
		},
		{
			name: "Gemini channel returns OpenAI Response endpoint",
			channel: &model.Channel{
				Type: constant.ChannelTypeGemini,
			},
			wantEndpoint: string(constant.EndpointTypeOpenAIResponse),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectChannelGroupMonitorEndpointType(tt.channel)
			assert.Equal(t, tt.wantEndpoint, got)
		})
	}
}
