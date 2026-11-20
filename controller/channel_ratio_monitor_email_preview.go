package controller

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type channelMonitorEmailPreviewRequest struct {
	NotificationTypes []string `json:"notification_types"`
}

func PreviewChannelMonitorNotificationEmail(c *gin.Context) {
	var request channelMonitorEmailPreviewRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的参数"})
		return
	}
	notificationTypes, err := normalizeChannelMonitorEmailNotificationTypes(request.NotificationTypes)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if len(notificationTypes) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请至少选择一种通知类型后再预览"})
		return
	}
	if len(notificationTypes) == 1 && notificationTypes[0] == channelMonitorEmailTypeMonitoringHealth {
		subject, content := service.BuildChannelMonitorHealthNotificationEmail(
			service.ChannelMonitorRedisStatusAvailable,
			[]string{service.ChannelMonitorRedisDegradedReasonContextDeadline, service.ChannelMonitorRedisDegradedReasonCostOutboxBacklog},
			0,
			time.Now(),
		)
		common.ApiSuccess(c, gin.H{
			"subject":            subject,
			"html":               content,
			"notification_types": notificationTypes,
		})
		return
	}

	startedAt := time.Now().Truncate(time.Minute).Add(-2 * time.Minute)
	summary := channelRatioMonitorTaskResult{
		Total:          4,
		BalanceUpdated: 2,
		Changed:        1,
		Skipped:        1,
		Failed:         1,
		Failures: []channelRatioMonitorTaskFailure{{
			ChannelId:     1006,
			ChannelName:   "示例渠道 F",
			ChannelRemark: "备用线路",
			Kind:          model.ChannelRatioFailureAlertRatio,
			Error:         "读取上游分组倍率超时",
		}},
		notificationTiming: channelRatioMonitorTaskTiming{
			startedAt: startedAt,
			deadline:  startedAt.Add(time.Minute),
			elapsed:   time.Minute + 3*time.Second,
		},
	}
	var sections []channelMonitorNotificationEmailSection
	if channelMonitorEmailNotificationTypeEnabled(notificationTypes, channelMonitorEmailTypeSmartScheduleFailed) {
		sections = append(sections, channelSmartScheduleFailureEmailSection("示例智能调度任务", channelSmartScheduleTaskResult{
			Total: 2, Failed: 1, Updated: 1,
			Failures: []channelSmartScheduleTaskFailure{{
				ChannelId: 1007, ChannelName: "示例渠道 G", Group: "vip", Model: "model-a",
				Stage: "configuration_conflict", Error: "渠道 1007：路由状态版本已变化（快照 7，当前 8）；整池保留上一轮结果",
			}},
		}, errors.New("1 条智能调度路由未能应用，失败池已保留上一轮结果")))
	}
	subject, content := buildChannelRatioMonitorNotificationEmail(
		notificationTypes,
		[]channelRatioMonitorEmailChange{{
			ChannelId: 1001, ChannelName: "示例渠道 A", ChannelRemark: "主线路",
			UpstreamType: "new_api", UpstreamGroup: "default",
			OldRatio: 1, NewRatio: 0.85, ConversionFactor: 1,
			OldCostRatio: 1, NewCostRatio: 0.85,
		}},
		[]channelRatioMonitorBalanceWarning{{
			ChannelId: 1002, ChannelName: "示例渠道 B", ChannelRemark: "低余额演示",
			UpstreamType: "new_api", Balance: 8.6, Threshold: 10,
		}},
		[]channelRatioMonitorDisabledChannel{{
			ChannelId: 1003, ChannelName: "示例渠道 C", ChannelRemark: "故障线路",
			Reason: "上游倍率或余额连续更新失败",
		}},
		[]channelRatioMonitorRemovedGroupMembership{{
			ChannelId: 1004, ChannelName: "示例渠道 D", ChannelRemark: "成本策略演示",
			Group: "vip",
		}},
		summary,
		context.DeadlineExceeded,
		sections...,
	)
	common.ApiSuccess(c, gin.H{
		"subject":            subject,
		"html":               content,
		"notification_types": notificationTypes,
	})
}
