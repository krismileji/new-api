package service

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// PrepareTaskChannelCost runs before inserting a submitted task so polling can
// recover the cost event and frozen conversion without the HTTP request context.
func PrepareTaskChannelCost(ctx *gin.Context, task *model.Task) {
	if task == nil || task.PrivateData.BillingContext == nil {
		return
	}
	bc := task.PrivateData.BillingContext
	if bc.ChannelCostEventId != "" {
		return
	}
	bc.ChannelCostEventId = "task:" + task.TaskID
	if bc.TieredSnapshot != nil {
		snapshot := channelDailyCostSnapshotFromContext(ctx, task.ChannelId)
		if snapshot.Configured {
			bc.ChannelCostSnapshot = &model.TaskChannelCostSnapshot{CostRatioCNY: snapshot.CostRatioCNY}
		}
	}
}
