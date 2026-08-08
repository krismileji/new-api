package middleware

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// ChannelSupportsRequestPath exposes the upstream path check to retry routing
// without changing the upstream helper's ownership or name.
func ChannelSupportsRequestPath(channel *model.Channel, requestPath string, requestModel string) bool {
	return channelSupportsRequestPath(channel, requestPath, requestModel)
}

// SetupContextForRetry clears channel-specific metadata before delegating to
// the upstream setup path. A failed channel can leave optional values in the
// Gin context that the next channel does not define.
func SetupContextForRetry(c *gin.Context, channel *model.Channel, modelName string) *types.NewAPIError {
	common.SetContextKey(c, constant.ContextKeyChannelOrganization, "")
	common.SetContextKey(c, constant.ContextKeyChannelMultiKeyIndex, 0)
	c.Set("api_version", "")
	c.Set("region", "")
	c.Set("plugin", "")
	c.Set("bot_id", "")
	return SetupContextForSelectedChannel(c, channel, modelName)
}

func setupContextForInitialChannel(c *gin.Context, channel *model.Channel, modelName string, allowAlternative bool) (*model.Channel, *types.NewAPIError) {
	failedChannelIDs := make([]int, 0)
	for channel != nil {
		setupErr := SetupContextForRetry(c, channel, modelName)
		if setupErr == nil {
			return channel, nil
		}
		if !allowAlternative || !types.IsChannelError(setupErr) || types.IsSkipRetryError(setupErr) {
			return nil, setupErr
		}

		failedChannelIDs = append(failedChannelIDs, channel.Id)
		retry := 0
		selected, selectedGroup, selectErr := service.CacheGetRandomSatisfiedChannel(
			&service.RetryParam{
				Ctx:              c,
				TokenGroup:       common.GetContextKeyString(c, constant.ContextKeyUsingGroup),
				ModelName:        modelName,
				RequestPath:      c.Request.URL.Path,
				Retry:            &retry,
				SelectionOptions: service.ChannelSelectionOptionsForRequest(c, 0),
			},
			model.ChannelSelectionOptions{ExcludedChannelIds: failedChannelIDs},
		)
		if selectErr != nil {
			return nil, types.NewError(
				fmt.Errorf("failed to select a channel after channel setup failed: %w", selectErr),
				types.ErrorCodeGetChannelFailed,
				types.ErrOptionWithSkipRetry(),
			)
		}
		if selected == nil {
			return nil, setupErr
		}
		if common.GetContextKeyString(c, constant.ContextKeyUsingGroup) == "auto" && selectedGroup != "" {
			common.SetContextKey(c, constant.ContextKeyAutoGroup, selectedGroup)
		}
		channel = selected
	}
	return nil, types.NewError(errors.New("channel is nil"), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
}
