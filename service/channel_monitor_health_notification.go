package service

import (
	"strings"
	"sync"
	"time"
)

const channelMonitorHealthNotificationCooldown = 15 * time.Minute

const ChannelMonitorHealthNotificationType = "monitoring_health"

type ChannelMonitorHealthNotificationConfig struct {
	Enabled           bool
	Receiver          string
	NotificationTypes []string
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

// NotifyChannelMonitorHealthAsync wakes the shared health sampler. Partial
// request observations cannot independently declare failure or recovery.
func NotifyChannelMonitorHealthAsync(enabled bool, receiver, status string, reasons []string, dropped int64, notificationTypes ...string) {
	if !enabled || strings.TrimSpace(receiver) == "" || len(reasons) == 0 {
		return
	}
	if len(notificationTypes) > 0 && !channelMonitorHealthNotificationTypeEnabled(notificationTypes, ChannelMonitorHealthNotificationType) {
		return
	}
	select {
	case channelMonitorHealthWake <- struct{}{}:
	default:
	}
}

func channelMonitorHealthNotificationTypeEnabled(notificationTypes []string, target string) bool {
	for _, notificationType := range notificationTypes {
		if notificationType == target {
			return true
		}
	}
	return false
}
