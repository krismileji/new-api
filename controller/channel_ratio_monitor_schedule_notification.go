package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"html"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const channelSmartScheduleFailureNotificationCooldown = 15 * time.Minute

type channelMonitorNotificationEmailSection struct {
	Summary string
	HTML    string
}

type channelSmartScheduleFailureNotifier struct {
	sync.Mutex
	receiver      string
	nextAttemptAt time.Time
}

var channelSmartScheduleFailureNotifications channelSmartScheduleFailureNotifier

func (notifier *channelSmartScheduleFailureNotifier) notify(
	ctx context.Context, settings channelMonitorSettings, taskID string,
	summary channelSmartScheduleTaskResult, runErr error, now time.Time,
	sendEmail func(subject, receiver, content string) error,
) error {
	receiver := strings.TrimSpace(settings.NotificationEmail)
	if ctx.Err() != nil || (runErr == nil && summary.Failed == 0) ||
		!settings.EmailNotificationEnabled || receiver == "" ||
		!channelMonitorEmailNotificationTypeEnabled(settings.EmailNotificationTypes, channelMonitorEmailTypeSmartScheduleFailed) {
		return nil
	}
	notifier.Lock()
	defer notifier.Unlock()
	if notifier.receiver == receiver && now.Before(notifier.nextAttemptAt) {
		return nil
	}

	// One reservation per recipient also limits alerts across scheduler nodes.
	key := fmt.Sprintf("channel_monitor:v1:smart_schedule:failure_notice:%x", sha256.Sum256([]byte(receiver)))
	token := common.GetUUID()
	client := common.RedisMonitorWriteClient()
	shared := false
	if client != nil {
		redisCtx, cancel := context.WithTimeout(ctx, time.Second)
		reserved, err := client.SetNX(redisCtx, key, token, channelSmartScheduleFailureNotificationCooldown).Result()
		cancel()
		if err == nil && !reserved {
			return nil
		}
		shared = err == nil
		if err != nil {
			common.SysError("智能调度失败通知去重暂不可用，使用本机限频: " + err.Error())
		}
	}
	section := channelSmartScheduleFailureEmailSection(taskID, summary, runErr)
	subject, content := buildChannelRatioMonitorNotificationEmail(
		[]string{channelMonitorEmailTypeSmartScheduleFailed}, nil, nil, nil, nil,
		channelRatioMonitorTaskResult{}, nil, section,
	)
	var err error
	if sendEmail == nil {
		err = common.SendEmailWithTimeout(subject, receiver, content, 30*time.Second)
	} else {
		err = sendEmail(subject, receiver, content)
	}
	notifier.receiver = receiver
	notifier.nextAttemptAt = now.Add(channelSmartScheduleFailureNotificationCooldown)
	if err != nil {
		// Failed SMTP delivery must be retryable without a notification storm.
		notifier.nextAttemptAt = now.Add(time.Minute)
		if shared {
			redisCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			const retryScript = `if redis.call('GET', KEYS[1]) == ARGV[1] then return redis.call('PEXPIRE', KEYS[1], ARGV[2]) end return 0`
			if retryErr := client.Eval(redisCtx, retryScript, []string{key}, token, time.Minute.Milliseconds()).Err(); retryErr != nil {
				common.SysError("更新智能调度通知重试时间失败: " + retryErr.Error())
			}
		}
		return fmt.Errorf("发送智能调度失败通知失败: %w", err)
	}
	return nil
}

func channelSmartScheduleFailureEmailSection(taskID string, summary channelSmartScheduleTaskResult, runErr error) channelMonitorNotificationEmailSection {
	var content strings.Builder
	content.WriteString("<h3>智能调度失败</h3>")
	if taskID != "" {
		fmt.Fprintf(&content, "<p>任务：%s</p>", html.EscapeString(taskID))
	}
	fmt.Fprintf(&content, "<p>本轮路由 %d 条，失败 %d 条，已更新 %d 条。</p>", summary.Total, summary.Failed, summary.Updated)
	if runErr != nil {
		fmt.Fprintf(&content, "<p>失败原因：%s</p>", html.EscapeString(runErr.Error()))
	}
	if len(summary.Failures) > 0 {
		content.WriteString(`<table style="border-collapse:collapse"><thead><tr>`)
		for _, heading := range []string{"渠道", "分组", "模型", "失败阶段", "具体原因"} {
			fmt.Fprintf(&content, `<th style="border:1px solid #ddd;padding:6px 10px;text-align:left">%s</th>`, heading)
		}
		content.WriteString("</tr></thead><tbody>")
		for _, failure := range summary.Failures[:min(len(summary.Failures), 20)] {
			stage := failure.Stage
			switch stage {
			case "configuration_conflict":
				stage = "配置冲突"
			case "write":
				stage = "结果写入"
			case "plan":
				stage = "计划计算"
			}
			content.WriteString("<tr>")
			for _, value := range []string{
				fmt.Sprintf("%s（ID: %d）", failure.ChannelName, failure.ChannelId),
				failure.Group, failure.Model, stage, failure.Error,
			} {
				fmt.Fprintf(&content, `<td style="border:1px solid #ddd;padding:6px 10px;overflow-wrap:anywhere">%s</td>`, html.EscapeString(value))
			}
			content.WriteString("</tr>")
		}
		content.WriteString("</tbody></table>")
		if summary.FailureDetailsTruncated || summary.Failed > min(len(summary.Failures), 20) {
			fmt.Fprintf(&content, "<p>邮件展示前 %d 条失败明细，完整结果见智能调度执行记录。</p>", min(len(summary.Failures), 20))
		}
	}
	return channelMonitorNotificationEmailSection{Summary: "智能调度失败", HTML: content.String()}
}
