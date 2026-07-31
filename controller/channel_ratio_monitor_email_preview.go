package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"

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

	summary := channelRatioMonitorTaskResult{
		Failed:            1,
		GroupUpdateFailed: true,
		Failures: []channelRatioMonitorTaskFailure{{
			ChannelId:     1006,
			ChannelName:   "示例渠道 F",
			ChannelRemark: "备用线路",
			Error:         "请求上游接口超时",
		}},
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
		errors.New("自动写入分组倍率失败：数据库暂时不可用"),
	)
	common.ApiSuccess(c, gin.H{
		"subject":            subject,
		"html":               content,
		"notification_types": notificationTypes,
	})
}
