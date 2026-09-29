package service

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wsmanager"
)

func DrainChannelWebSockets(channelID int) {
	wsmanager.DrainChannelsAndBroadcast([]int{channelID}, "渠道已自动禁用，当前生成结束后关闭连接")
}

// Registration follows the upstream handshake. Recheck persisted status to
// cover a notification sent before registration, without trusting stale cache.
func EnforceChannelWebSocketStatus(channelID int, drain, closeConnection func(string)) {
	if channelID <= 0 {
		return
	}
	channel, err := model.GetChannelById(channelID, false)
	if err != nil {
		closeConnection("无法确认渠道状态，请重新连接")
		return
	}
	if channel.Status == common.ChannelStatusEnabled {
		return
	}
	reason, _ := channel.GetOtherInfo()["status_reason"].(string)
	if channel.Status == common.ChannelStatusAutoDisabled && strings.HasPrefix(reason, "渠道监控：") {
		drain("渠道已自动禁用，当前生成结束后关闭连接")
	} else {
		closeConnection(ChannelDisabledCloseReason)
	}
}
