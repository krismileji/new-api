package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

type relayRetryRouting struct {
	excluded                    map[int]struct{}
	excludedOrder               []int
	exhausted                   bool
	sameChannelID               int // Reload before retry because the first-attempt channel may be a sparse context stub.
	sameGroup                   string
	sameChannelRetryUnavailable bool
}

func newRelayRetryRouting() *relayRetryRouting {
	return &relayRetryRouting{
		excluded: make(map[int]struct{}),
	}
}

func (routing *relayRetryRouting) exclude(channelID int) {
	if routing == nil || channelID <= 0 {
		return
	}
	if routing.sameChannelID == channelID {
		routing.sameChannelID = 0
		routing.sameGroup = ""
		routing.sameChannelRetryUnavailable = false
	}
	if _, exists := routing.excluded[channelID]; exists {
		return
	}
	routing.excluded[channelID] = struct{}{}
	routing.excludedOrder = append(routing.excludedOrder, channelID)
	routing.exhausted = false
}

// restore removes a temporary exclusion made while a channel was saturated.
// Permanent retry exclusions remain untouched, so waiting for capacity does
// not bring a channel that already failed upstream back into the round.
func (routing *relayRetryRouting) restore(channelID int) {
	if routing == nil || channelID <= 0 {
		return
	}
	if _, exists := routing.excluded[channelID]; !exists {
		return
	}
	delete(routing.excluded, channelID)
	filtered := routing.excludedOrder[:0]
	for _, excludedID := range routing.excludedOrder {
		if excludedID != channelID {
			filtered = append(filtered, excludedID)
		}
	}
	routing.excludedOrder = filtered
	routing.exhausted = false
}

func (routing *relayRetryRouting) retrySameChannel(channel *model.Channel, group string) {
	if routing == nil || channel == nil || channel.Id <= 0 {
		return
	}
	routing.sameChannelID = channel.Id
	routing.sameGroup = group
	routing.sameChannelRetryUnavailable = false
	routing.exhausted = false
}

func (routing *relayRetryRouting) takeSameChannelRetryUnavailable() bool {
	if routing == nil || !routing.sameChannelRetryUnavailable {
		return false
	}
	routing.sameChannelRetryUnavailable = false
	return true
}

func (routing *relayRetryRouting) selectionOptions() (model.ChannelSelectionOptions, bool) {
	if routing == nil || len(routing.excludedOrder) == 0 {
		return model.ChannelSelectionOptions{}, false
	}
	channelIDs := append([]int(nil), routing.excludedOrder...)
	return model.ChannelSelectionOptions{ExcludedChannelIds: channelIDs}, true
}

func (routing *relayRetryRouting) candidatesExhausted() bool {
	return routing != nil && routing.exhausted
}

func (routing *relayRetryRouting) restartRound(retryParam *service.RetryParam) {
	routing.excluded = make(map[int]struct{})
	routing.excludedOrder = nil
	routing.exhausted = false
	routing.sameChannelID = 0
	routing.sameGroup = ""
	routing.sameChannelRetryUnavailable = false
	if retryParam.TokenGroup == "auto" && retryParam.Ctx != nil {
		common.SetContextKey(retryParam.Ctx, constant.ContextKeyAutoGroupIndex, 0)
		common.SetContextKey(retryParam.Ctx, constant.ContextKeyAutoGroupRetryIndex, 0)
	}
}

func (routing *relayRetryRouting) selectChannel(retryParam *service.RetryParam) (*model.Channel, string, error) {
	if routing == nil {
		return service.CacheGetRandomSatisfiedChannel(retryParam)
	}
	routing.exhausted = false
	if routing.sameChannelID > 0 {
		channelID := routing.sameChannelID
		group := routing.sameGroup
		routing.sameChannelID = 0
		routing.sameGroup = ""
		channel, err := model.CacheGetChannel(channelID)
		sameChannelOptions := retryParam.SelectionOptions
		sameChannelOptions.ExcludedChannelIds = append(
			append([]int(nil), sameChannelOptions.ExcludedChannelIds...),
			routing.excludedOrder...,
		)
		groupEligible := group != "" && model.ChannelSmartScheduleAffinityEligibility(
			group,
			retryParam.ModelName,
			channelID,
			retryParam.RequestPath,
			sameChannelOptions,
		) == model.ChannelSmartScheduleAffinityEligible
		if retryParam.TokenGroup == "auto" {
			groupAllowed := false
			if retryParam.Ctx != nil {
				userGroup := common.GetContextKeyString(retryParam.Ctx, constant.ContextKeyUserGroup)
				for _, autoGroup := range service.GetRequestAutoGroups(retryParam.Ctx, userGroup) {
					if autoGroup == group {
						groupAllowed = true
						break
					}
				}
			}
			groupEligible = groupAllowed && model.ChannelSmartScheduleAffinityEligibility(
				group,
				retryParam.ModelName,
				channelID,
				retryParam.RequestPath,
				sameChannelOptions,
			) == model.ChannelSmartScheduleAffinityEligible
		}
		if err == nil && channel != nil && channel.Status == common.ChannelStatusEnabled &&
			groupEligible &&
			middleware.ChannelSupportsRequestPath(channel, retryParam.RequestPath, retryParam.ModelName) {
			return channel, group, nil
		}
		// Switching channels here must consume ordinary retry budget. Report the
		// unavailable pinned retry to the controller before selecting a replacement.
		routing.exclude(channelID)
		routing.sameChannelRetryUnavailable = true
		return nil, group, nil
	}
	return routing.selectChannelCandidates(retryParam, true)
}

// selectChannelCurrentRound chooses another candidate without starting a new
// routing round. It is used when a channel is saturated: retrying the same
// round is useful, but restarting it would immediately select the saturated
// channel again when it is the only candidate.
func (routing *relayRetryRouting) selectChannelCurrentRound(retryParam *service.RetryParam) (*model.Channel, string, error) {
	if routing == nil {
		return service.CacheGetRandomSatisfiedChannel(retryParam)
	}
	routing.exhausted = false
	return routing.selectChannelCandidates(retryParam, false)
}

func (routing *relayRetryRouting) selectChannelCandidates(retryParam *service.RetryParam, allowRestart bool) (*model.Channel, string, error) {

	selectionOptions, hasExcludedChannels := routing.selectionOptions()
	if !hasExcludedChannels {
		return service.CacheGetRandomSatisfiedChannel(retryParam)
	}
	originalRetry := retryParam.GetRetry()
	var originalAutoGroup any
	var originalAutoGroupExists bool
	var originalAutoGroupIndexValue any
	var originalAutoGroupIndexExists bool
	var originalAutoGroupRetryIndex any
	var originalAutoGroupRetryIndexExists bool
	if retryParam.TokenGroup == "auto" && retryParam.Ctx != nil {
		originalAutoGroup, originalAutoGroupExists = common.GetContextKey(retryParam.Ctx, constant.ContextKeyAutoGroup)
		originalAutoGroupIndexValue, originalAutoGroupIndexExists = common.GetContextKey(retryParam.Ctx, constant.ContextKeyAutoGroupIndex)
		originalAutoGroupRetryIndex, originalAutoGroupRetryIndexExists = common.GetContextKey(retryParam.Ctx, constant.ContextKeyAutoGroupRetryIndex)
	}
	channel, selectGroup, err := service.CacheGetRandomSatisfiedChannel(retryParam, selectionOptions)
	if err != nil || channel != nil {
		return channel, selectGroup, err
	}

	// Request-size limits are a soft preference. Once the normal candidates
	// have been exhausted, give deferred exploration/stability-release routes
	// a chance before declaring the round exhausted or restarting it.
	if retryParam.TokenGroup == "auto" && retryParam.Ctx != nil {
		// The first selection may advance auto-group state while it searches. The
		// relaxed-limit probe must start from the same state, but it must not mutate
		// the caller's retry counter or selection options.
		if originalAutoGroupExists {
			common.SetContextKey(retryParam.Ctx, constant.ContextKeyAutoGroup, originalAutoGroup)
		} else {
			common.SetContextKey(retryParam.Ctx, constant.ContextKeyAutoGroup, nil)
		}
		if originalAutoGroupIndexExists {
			common.SetContextKey(retryParam.Ctx, constant.ContextKeyAutoGroupIndex, originalAutoGroupIndexValue)
		} else {
			common.SetContextKey(retryParam.Ctx, constant.ContextKeyAutoGroupIndex, nil)
		}
		if originalAutoGroupRetryIndexExists {
			common.SetContextKey(retryParam.Ctx, constant.ContextKeyAutoGroupRetryIndex, originalAutoGroupRetryIndex)
		} else {
			common.SetContextKey(retryParam.Ctx, constant.ContextKeyAutoGroupRetryIndex, nil)
		}
	}
	fallbackRetry := originalRetry
	fallbackParam := *retryParam
	fallbackParam.Retry = &fallbackRetry
	fallbackParam.SelectionOptions = retryParam.SelectionOptions
	fallbackParam.SelectionOptions.IgnoreSmartScheduleRequestLimits = true
	channel, selectGroup, err = service.CacheGetRandomSatisfiedChannel(&fallbackParam, selectionOptions)
	if err != nil || channel != nil {
		return channel, selectGroup, err
	}
	if retryParam.TokenGroup == "auto" &&
		common.GetContextKeyBool(retryParam.Ctx, constant.ContextKeyTokenCrossGroupRetry) {
		userGroup := common.GetContextKeyString(retryParam.Ctx, constant.ContextKeyUserGroup)
		autoGroups := service.GetRequestAutoGroups(retryParam.Ctx, userGroup)
		if common.GetContextKeyInt(retryParam.Ctx, constant.ContextKeyAutoGroupIndex) >= len(autoGroups) {
			routing.exhausted = true
			return nil, selectGroup, nil
		}
	}
	// A token without cross-group retry permission must stay in the group
	// selected for this request. Restarting here would clear the exclusions and
	// select the already-failed channel again, defeating the retry boundary.
	if retryParam.TokenGroup == "auto" && retryParam.IsRetry && retryParam.Ctx != nil &&
		!common.GetContextKeyBool(retryParam.Ctx, constant.ContextKeyTokenCrossGroupRetry) {
		routing.exhausted = true
		return nil, selectGroup, nil
	}
	if !allowRestart {
		routing.exhausted = true
		return nil, selectGroup, nil
	}

	routing.restartRound(retryParam)
	roundRetry := 0
	roundRetryParam := *retryParam
	roundRetryParam.Retry = &roundRetry
	channel, selectGroup, err = service.CacheGetRandomSatisfiedChannel(&roundRetryParam)
	if err == nil && channel == nil {
		routing.exhausted = true
	}
	return channel, selectGroup, err
}
