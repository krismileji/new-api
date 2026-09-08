package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"html"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

type channelMonitorRecoveryNotice struct {
	Alerted        bool
	LastSentAt     int64
	LastSeverity   ChannelMonitorHealthStatus
	GapFingerprint string
	NextAttemptAt  int64
	Failures       int
}

var channelMonitorRecoveryNotices = struct {
	sync.Mutex
	inFlight bool
	local    map[string]channelMonitorRecoveryNotice
}{local: make(map[string]channelMonitorRecoveryNotice)}

func channelMonitorRecoveryNoticeKind(snapshot ChannelMonitorRecovery, state channelMonitorRecoveryNotice, now int64) string {
	if snapshot.CheckedAt == 0 || now-snapshot.CheckedAt > channelMonitorRecoveryStaleSeconds || state.NextAttemptAt > now {
		return ""
	}
	if snapshot.Status != ChannelMonitorHealthHealthy {
		if !state.Alerted || snapshot.Status == ChannelMonitorHealthUnavailable && state.LastSeverity != ChannelMonitorHealthUnavailable || now-state.LastSentAt >= int64(channelMonitorHealthNotificationCooldown/time.Second) {
			return "alert"
		}
		return ""
	}
	if state.Alerted {
		if !snapshot.RecoveryConfirmed {
			return ""
		}
		return "recovery"
	}
	if len(snapshot.DataGapReasons) > 0 && strings.Join(normalizeChannelMonitorHealthReasons(snapshot.DataGapReasons), ",") != state.GapFingerprint {
		return "gap"
	}
	return ""
}

func finishChannelMonitorRecoveryNotice(state channelMonitorRecoveryNotice, snapshot ChannelMonitorRecovery, kind string, now int64, err error) channelMonitorRecoveryNotice {
	if err != nil {
		state.Failures++
		delay := int64(30 * (1 << min(state.Failures-1, 4)))
		if state.Failures >= 3 {
			delay = int64(channelMonitorHealthNotificationCooldown / time.Second)
		}
		state.NextAttemptAt = now + delay
		return state
	}
	state.Alerted = kind == "alert"
	state.LastSentAt = now
	state.LastSeverity = snapshot.Status
	state.GapFingerprint = strings.Join(normalizeChannelMonitorHealthReasons(snapshot.DataGapReasons), ",")
	state.Failures, state.NextAttemptAt = 0, 0
	return state
}

func notifyChannelMonitorRecovery(snapshot ChannelMonitorRecovery) {
	channelMonitorHealthNotificationConfigProvider.RLock()
	provider := channelMonitorHealthNotificationConfigProvider.provider
	channelMonitorHealthNotificationConfigProvider.RUnlock()
	if provider == nil {
		return
	}
	config := provider()
	config.Receiver = strings.TrimSpace(config.Receiver)
	if !config.Enabled || config.Receiver == "" || len(config.NotificationTypes) > 0 && !channelMonitorHealthNotificationTypeEnabled(config.NotificationTypes, ChannelMonitorHealthNotificationType) {
		return
	}
	channelMonitorRecoveryNotices.Lock()
	if channelMonitorRecoveryNotices.inFlight {
		channelMonitorRecoveryNotices.Unlock()
		return
	}
	channelMonitorRecoveryNotices.inFlight = true
	channelMonitorRecoveryNotices.Unlock()
	go deliverChannelMonitorRecoveryNotice(snapshot, config.Receiver)
}

func deliverChannelMonitorRecoveryNotice(snapshot ChannelMonitorRecovery, receiver string) {
	defer func() {
		channelMonitorRecoveryNotices.Lock()
		channelMonitorRecoveryNotices.inFlight = false
		channelMonitorRecoveryNotices.Unlock()
	}()
	key := fmt.Sprintf("channel_monitor:v1:health:notice:%x", sha256.Sum256([]byte(snapshot.NodeID+"\x00"+receiver)))
	channelMonitorRecoveryNotices.Lock()
	state := channelMonitorRecoveryNotices.local[key]
	channelMonitorRecoveryNotices.Unlock()
	client := common.RedisMonitorWriteClient()
	shared := false
	if client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		payload, err := client.Get(ctx, key).Bytes()
		cancel()
		if err == nil {
			var stored channelMonitorRecoveryNotice
			if err = common.Unmarshal(payload, &stored); err != nil {
				common.SysError("渠道监控通知状态读取失败: " + err.Error())
				return
			}
			if stored.LastSentAt > state.LastSentAt || stored.LastSentAt == state.LastSentAt && stored.NextAttemptAt > state.NextAttemptAt {
				state = stored
			}
			shared = true
		} else if errors.Is(err, redis.Nil) {
			shared = true
		}
	}
	now := time.Now().Unix()
	kind := channelMonitorRecoveryNoticeKind(snapshot, state, now)
	if kind == "" && !shared {
		return
	}
	// Recovery must not be announced while notification coordination is offline.
	if kind == "recovery" && !shared {
		return
	}
	if shared {
		token := common.GetUUID()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		locked, err := client.SetNX(ctx, key+":lease", token, 2*time.Minute).Result()
		cancel()
		if err != nil || !locked {
			return
		}
		// The lease exceeds the bounded 30-second SMTP exchange and state I/O.
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = client.Eval(ctx, channelMonitorRedisLeaseReleaseScript, []string{key + ":lease"}, token).Err()
		}()
		// Re-read after acquiring the lease; another process may have just sent.
		ctx, cancel = context.WithTimeout(context.Background(), time.Second)
		payload, err := client.Get(ctx, key).Bytes()
		cancel()
		if err != nil && !errors.Is(err, redis.Nil) {
			return
		}
		if err == nil {
			var stored channelMonitorRecoveryNotice
			if common.Unmarshal(payload, &stored) != nil {
				return
			}
			if stored.LastSentAt > state.LastSentAt || stored.LastSentAt == state.LastSentAt && stored.NextAttemptAt > state.NextAttemptAt {
				state = stored
			}
			kind = channelMonitorRecoveryNoticeKind(snapshot, state, time.Now().Unix())
		}
	}
	if kind != "" {
		subject, body := BuildChannelMonitorRecoveryEmail(snapshot, kind, time.Unix(now, 0))
		err := sendChannelMonitorRecoveryEmail(subject, receiver, body)
		state = finishChannelMonitorRecoveryNotice(state, snapshot, kind, time.Now().Unix(), err)
		channelMonitorHealthWorkerState.Lock()
		channelMonitorHealthWorkerState.state.Snapshot.NotificationError = ""
		if err != nil {
			channelMonitorHealthWorkerState.state.Snapshot.NotificationError = "邮件发送失败，将自动重试"
		}
		channelMonitorHealthWorkerState.Unlock()
		if err != nil {
			common.SysError("渠道监控通知发送失败，将重试: " + err.Error())
		}
	}
	channelMonitorRecoveryNotices.Lock()
	channelMonitorRecoveryNotices.local[key] = state
	channelMonitorRecoveryNotices.Unlock()
	if shared {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if payload, marshalErr := common.Marshal(state); marshalErr == nil {
			if saveErr := client.Set(ctx, key, payload, 30*24*time.Hour).Err(); saveErr != nil {
				common.SysError("渠道监控通知结果保存失败: " + saveErr.Error())
			}
		}
	}
}

var sendChannelMonitorRecoveryEmail = func(subject, receiver, content string) error {
	return common.SendEmailWithTimeout(subject, receiver, content, 30*time.Second)
}

func BuildChannelMonitorRecoveryEmail(snapshot ChannelMonitorRecovery, kind string, observedAt time.Time) (string, string) {
	node := html.EscapeString(snapshot.NodeID)
	if kind == "alert" || kind == "gap" {
		reasons := snapshot.DegradedReasons
		if kind == "gap" {
			reasons = snapshot.DataGapReasons
		}
		return buildChannelMonitorHealthEmail(string(snapshot.Status), reasons, snapshot.DroppedSampleCount, observedAt, snapshot.Message+"。"+snapshot.Action, snapshot.NodeID)
	}
	message := "相关监控链路已连续正常，后台事件处理已恢复。"
	if len(snapshot.DataGapReasons) > 0 {
		message = "监控运行已恢复，部分历史统计仍不完整，请复核丢弃或隔离记录。"
	}
	content := fmt.Sprintf(`<!doctype html><html lang="zh-CN"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1"></head><body style="margin:0;background:#fff;color:#111827;font-family:Arial,'Microsoft YaHei',sans-serif;font-size:14px;line-height:1.7"><div style="max-width:680px;margin:auto;padding:16px;overflow-wrap:anywhere"><p>%s</p><p>节点：%s</p><p>时间：%s</p></div></body></html>`, message, node, html.EscapeString(observedAt.Format("2006-01-02 15:04:05 UTC-07:00")))
	return "渠道监控运行已恢复", content
}
