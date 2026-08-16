package middleware

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
)

// SkipAdminAuditFallback disables the generic METHOD+route audit entry for a
// route group. Handlers can still record selected business changes explicitly.
func SkipAdminAuditFallback() gin.HandlerFunc {
	return func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyAuditLogged, true)
		c.Next()
	}
}
