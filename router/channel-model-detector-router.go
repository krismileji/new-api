package router

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

func registerChannelModelDetectionRoutes(monitorRoute *gin.RouterGroup) {
	modelDetectionRoute := monitorRoute.Group("/model_detection")
	{
		modelDetectionRoute.GET("", controller.GetChannelModelDetectionOverview)
		modelDetectionRoute.GET("/settings", controller.GetChannelModelDetectionSettings)
		modelDetectionRoute.PUT("/settings", controller.UpdateChannelModelDetectionSettings)
		modelDetectionRoute.GET("/service", controller.GetChannelModelDetectionService)
		modelDetectionRoute.POST("/service/test", controller.TestChannelModelDetectionService)
		modelDetectionRoute.PUT("/channel/:id/config", controller.UpdateChannelModelDetectionConfig)
		modelDetectionRoute.POST("/channel/:id/estimate", controller.EstimateChannelModelDetectionCost)
		modelDetectionRoute.POST("/channel/:id/run", controller.StartChannelModelDetectionManualRun)
		modelDetectionRoute.GET("/channel/:id/runs", controller.ListChannelModelDetectionRuns)
		modelDetectionRoute.GET("/runs/:run_id", controller.GetChannelModelDetectionRunDetail)
		modelDetectionRoute.POST("/runs/:run_id/cancel", controller.CancelChannelModelDetectionRun)
	}
}

func SetChannelModelDetectorRouter(router *gin.Engine) {
	handler, err := controller.GetChannelModelDetectorRelayHandler()
	if err != nil {
		common.SysError("初始化模型检测内部 Relay 失败: " + err.Error())
		handler = controller.NewChannelModelDetectorRelayHandler(nil)
	}
	registerChannelModelDetectorRelayRoutes(router, handler)
}

func registerChannelModelDetectorRelayRoutes(router *gin.Engine, handler *controller.ChannelModelDetectorRelayHandler) {
	internalRoute := router.Group("/internal/model-detector/v1")
	internalRoute.Use(middleware.RouteTag("model_detector"))
	internalRoute.POST("/responses", handler.PostChannelModelDetectorRelay)
}
