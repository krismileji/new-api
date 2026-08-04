package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// ChannelSelectionOptionsForRequest builds the request-size part of channel
// selection without consuming the request body. The exact token estimate is
// available after relay parsing; the body size covers the initial distributor
// selection and schemas that token counters may undercount.
func ChannelSelectionOptionsForRequest(c *gin.Context, estimatedPromptTokens int) model.ChannelSelectionOptions {
	options := model.ChannelSelectionOptions{EstimatedPromptTokens: estimatedPromptTokens}
	if c == nil || c.Request == nil {
		return options
	}
	if storage, exists := c.Get(common.KeyBodyStorage); exists {
		if bodyStorage, ok := storage.(common.BodyStorage); ok {
			options.RequestBodyBytes = bodyStorage.Size()
			return options
		}
	}
	if c.Request.ContentLength > 0 {
		options.RequestBodyBytes = c.Request.ContentLength
	}
	return options
}
