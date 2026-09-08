package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetChannelMonitorRecovery(c *gin.Context) {
	common.ApiSuccess(c, service.GetChannelMonitorRecovery())
}
