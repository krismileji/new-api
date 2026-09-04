package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
)

func TestSelectChannelModelDetectorEndpointPath(t *testing.T) {
	tests := []struct {
		name     string
		channel  *model.Channel
		wantPath string
	}{
		{
			name:     "nil channel returns default /v1/responses path",
			channel:  nil,
			wantPath: "/v1/responses",
		},
		{
			name: "Anthropic channel returns /v1/messages path",
			channel: &model.Channel{
				Type: constant.ChannelTypeAnthropic,
			},
			wantPath: "/v1/messages",
		},
		{
			name: "OpenAI channel returns /v1/responses path",
			channel: &model.Channel{
				Type: constant.ChannelTypeOpenAI,
			},
			wantPath: "/v1/responses",
		},
		{
			name: "Azure channel returns /v1/responses path",
			channel: &model.Channel{
				Type: constant.ChannelTypeAzure,
			},
			wantPath: "/v1/responses",
		},
		{
			name: "DeepSeek channel returns /v1/responses path",
			channel: &model.Channel{
				Type: constant.ChannelTypeDeepSeek,
			},
			wantPath: "/v1/responses",
		},
		{
			name: "Gemini channel returns /v1/responses path",
			channel: &model.Channel{
				Type: constant.ChannelTypeGemini,
			},
			wantPath: "/v1/responses",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectChannelModelDetectorEndpointPath(tt.channel)
			assert.Equal(t, tt.wantPath, got)
		})
	}
}
