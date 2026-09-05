package service

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const channelMonitorHealthNotificationCooldown = 15 * time.Minute

const ChannelMonitorHealthNotificationType = "monitoring_health"

type ChannelMonitorHealthNotificationConfig struct {
	Enabled           bool
	Receiver          string
	NotificationTypes []string
}

var channelMonitorHealthNotificationState struct {
	sync.Mutex
	lastSent map[string]time.Time
}

var channelMonitorHealthNotificationConfigProvider struct {
	sync.RWMutex
	provider func() ChannelMonitorHealthNotificationConfig
}

// SetChannelMonitorHealthNotificationConfigProvider supplies the current
// channel-monitor settings without coupling the service package to controller.
func SetChannelMonitorHealthNotificationConfigProvider(provider func() ChannelMonitorHealthNotificationConfig) {
	channelMonitorHealthNotificationConfigProvider.Lock()
	channelMonitorHealthNotificationConfigProvider.provider = provider
	channelMonitorHealthNotificationConfigProvider.Unlock()
}

// NotifyChannelMonitorHealthFromCurrentConfig schedules an alert using the
// latest settings. It is safe to call from request, writer, and consumer paths.
func NotifyChannelMonitorHealthFromCurrentConfig(status string, reasons []string, dropped int64) {
	channelMonitorHealthNotificationConfigProvider.RLock()
	provider := channelMonitorHealthNotificationConfigProvider.provider
	channelMonitorHealthNotificationConfigProvider.RUnlock()
	if provider == nil {
		return
	}
	config := provider()
	NotifyChannelMonitorHealthAsync(config.Enabled, config.Receiver, status, reasons, dropped, config.NotificationTypes...)
}

// NotifyChannelMonitorHealthAsync sends a deduplicated health email without
// blocking a page request, Stream consumer, or monitoring worker.
func NotifyChannelMonitorHealthAsync(enabled bool, receiver, status string, reasons []string, dropped int64, notificationTypes ...string) {
	if !enabled || strings.TrimSpace(receiver) == "" || len(reasons) == 0 {
		return
	}
	if len(notificationTypes) > 0 && !channelMonitorHealthNotificationTypeEnabled(notificationTypes, ChannelMonitorHealthNotificationType) {
		return
	}
	receiver = strings.TrimSpace(receiver)
	key := receiver + "\x00" + strings.Join(reasons, ",")
	now := time.Now()
	channelMonitorHealthNotificationState.Lock()
	if channelMonitorHealthNotificationState.lastSent == nil {
		channelMonitorHealthNotificationState.lastSent = make(map[string]time.Time)
	}
	if last := channelMonitorHealthNotificationState.lastSent[key]; !last.IsZero() && now.Sub(last) < channelMonitorHealthNotificationCooldown {
		channelMonitorHealthNotificationState.Unlock()
		return
	}
	channelMonitorHealthNotificationState.lastSent[key] = now
	channelMonitorHealthNotificationState.Unlock()

	reasonsText := strings.Join(reasons, "、")
	go func() {
		statusText := status
		switch status {
		case "degraded":
			statusText = "已降级"
		case "unavailable":
			statusText = "不可用"
		case "healthy":
			statusText = "正常"
		}
		subject := "渠道监控异常：" + statusText
		content := fmt.Sprintf("<p>渠道监控状态：%s</p><p>异常原因：%s</p><p>丢弃样本数：%d</p>", statusText, reasonsText, dropped)
		_ = common.SendEmail(subject, receiver, content)
	}()
}

func channelMonitorHealthNotificationTypeEnabled(notificationTypes []string, target string) bool {
	for _, notificationType := range notificationTypes {
		if notificationType == target {
			return true
		}
	}
	return false
}
