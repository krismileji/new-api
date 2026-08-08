package controller

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
)

// refreshLockedTaskChannel keeps remix and continuation attempts pinned to the
// origin channel while using its latest status, keys, and connection settings.
func refreshLockedTaskChannel(info *relaycommon.RelayInfo, locked *model.Channel) (*model.Channel, *taskdto.TaskError) {
	if info == nil || locked == nil || locked.Id <= 0 {
		return nil, service.TaskErrorWrapperLocal(
			errors.New("原任务渠道信息无效"),
			"task_channel_unavailable",
			http.StatusServiceUnavailable,
		)
	}
	channel, err := model.CacheGetChannel(locked.Id)
	if err != nil {
		return nil, service.TaskErrorWrapperLocal(
			fmt.Errorf("重新加载原任务渠道失败: %w", err),
			"task_channel_unavailable",
			http.StatusServiceUnavailable,
		)
	}
	if channel.Status != common.ChannelStatusEnabled {
		return nil, service.TaskErrorWrapperLocal(
			errors.New("原任务渠道已被禁用"),
			"task_channel_unavailable",
			http.StatusServiceUnavailable,
		)
	}
	info.LockedChannel = channel
	return channel, nil
}
