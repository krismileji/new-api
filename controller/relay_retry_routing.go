package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

type relayRetryRouting struct {
	excluded      map[int]struct{}
	excludedOrder []int
	exhausted     bool
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
	if _, exists := routing.excluded[channelID]; exists {
		return
	}
	routing.excluded[channelID] = struct{}{}
	routing.excludedOrder = append(routing.excludedOrder, channelID)
	routing.exhausted = false
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

	selectionOptions, hasExcludedChannels := routing.selectionOptions()
	if !hasExcludedChannels {
		return service.CacheGetRandomSatisfiedChannel(retryParam)
	}
	channel, selectGroup, err := service.CacheGetRandomSatisfiedChannel(retryParam, selectionOptions)
	if err != nil || channel != nil {
		return channel, selectGroup, err
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
