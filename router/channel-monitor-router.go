package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

func registerChannelMonitorRoutes(apiRouter *gin.RouterGroup) {
	monitorRoute := apiRouter.Group("/channel_monitor")
	monitorRoute.Use(
		middleware.DisableCache(),
		middleware.RootAuth(),
		middleware.SkipAdminAuditFallback(),
	)
	{
		registerChannelModelDetectionRoutes(monitorRoute)
		monitorRoute.GET("/", controller.GetChannelMonitorOverview)
		monitorRoute.GET("/concurrency", controller.GetChannelMonitorConcurrency)
		monitorRoute.GET("/cost", controller.GetChannelMonitorCostOverview)
		monitorRoute.GET("/performance", controller.GetChannelMonitorPerformance)
		monitorRoute.GET("/status", controller.GetChannelStatusProbeOverview)
		monitorRoute.PUT("/status/channel/:id/config", controller.UpdateChannelStatusProbeConfig)
		monitorRoute.POST("/status/channel/:id/run", controller.RunChannelStatusProbeNow)
		monitorRoute.GET("/status/channel/:id/executions", controller.ListChannelStatusProbeExecutions)
		monitorRoute.GET("/group_monitor/settings", controller.GetChannelGroupMonitorSettings)
		monitorRoute.PUT("/group_monitor/settings", controller.UpdateChannelGroupMonitorSettings)
		monitorRoute.GET("/group_monitor/overview", controller.GetChannelGroupMonitorOverview)
		monitorRoute.POST("/group_monitor/run", controller.RunChannelGroupMonitorNow)
		monitorRoute.GET("/group_monitor/executions", controller.ListChannelGroupMonitorExecutions)
		monitorRoute.GET("/success/today", controller.GetChannelMonitorTodaySuccess)
		monitorRoute.GET("/success/detail", controller.GetChannelMonitorSuccessDetail)
		monitorRoute.GET("/tasks", controller.ListChannelMonitorTasks)
		monitorRoute.GET("/tasks/:task_id/details", controller.GetChannelMonitorSmartScheduleExecutionDetails)
		monitorRoute.GET("/tasks/:task_id/ratio-details", controller.GetChannelMonitorRatioTaskDetails)
		monitorRoute.PUT("/settings", controller.UpdateChannelMonitorSettings)
		monitorRoute.POST("/settings/email-preview", controller.PreviewChannelMonitorNotificationEmail)
		monitorRoute.POST("/ratio/run", controller.RunChannelMonitorRatioUpdate)
		monitorRoute.POST("/schedule/run", controller.RunChannelMonitorSmartSchedule)
		monitorRoute.GET("/schedule", controller.GetChannelMonitorSmartScheduleRoutes)
		monitorRoute.PUT("/order", controller.UpdateChannelMonitorChannelOrder)
		monitorRoute.PUT("/channel/:id", controller.UpdateChannelMonitorRatio)
		monitorRoute.PUT("/channel/:id/schedule/routes", controller.UpdateChannelMonitorSmartScheduleChannelConfig)
		monitorRoute.PUT("/channel/:id/schedule/route", controller.UpdateChannelMonitorSmartScheduleRouteConfig)
		monitorRoute.PUT("/channel/:id/schedule/route/pause", controller.UpdateChannelMonitorSmartScheduleGroupPause)
		monitorRoute.PUT("/channel/:id/schedule/route/rate-limit-cooldown", controller.UpdateChannelMonitorSmartScheduleRateLimitCooldown)
		monitorRoute.PUT("/channel/:id/schedule/route/primary", controller.UpdateChannelMonitorSmartScheduleRoutePrimary)
		monitorRoute.POST("/channel/:id/schedule/route/stability/clear", controller.ClearChannelMonitorSmartScheduleRouteStability)
		monitorRoute.POST("/channel/:id/schedule/route/exploration/clear", controller.ClearChannelMonitorSmartScheduleRouteExploration)
		monitorRoute.PUT("/channel/:id/concurrency", controller.UpdateChannelMonitorConcurrencyLimit)
		monitorRoute.GET("/channel/:id/history", controller.GetChannelMonitorHistory)
		monitorRoute.PUT("/channel/:id/upstream", controller.SaveChannelMonitorUpstreamConfig)
		monitorRoute.POST("/channel/:id/upstream/groups", controller.ListChannelMonitorUpstreamGroups)
		monitorRoute.POST("/channel/:id/upstream/version", controller.FetchChannelMonitorSub2APIUpstreamVersion)
		monitorRoute.POST("/channel/:id/upstream/test", controller.TestChannelMonitorUpstreamConfig)
		monitorRoute.POST("/channel/:id/upstream/fetch", controller.FetchChannelMonitorUpstreamRatio)
		monitorRoute.POST("/channel/:id/upstream/balance/fetch", controller.FetchChannelMonitorUpstreamBalance)
		monitorRoute.POST("/channel/:id/upstream/group/apply", controller.ApplyChannelMonitorUpstreamGroup)
		monitorRoute.PUT("/group", controller.UpdateChannelMonitorGroupRatio)
		monitorRoute.PUT("/group/channels", controller.UpdateChannelMonitorGroupChannels)
		monitorRoute.PUT("/group/sync", controller.SyncChannelMonitorGroupRatio)
	}
}
