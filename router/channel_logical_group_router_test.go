package router

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestLogicalChannelGroupRoutesRegisterContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	registerChannelRoutes(engine.Group("/api"))
	want := map[string]bool{
		http.MethodGet + " /api/channel/logical-groups":             false,
		http.MethodGet + " /api/channel/logical-groups/:id":         false,
		http.MethodPost + " /api/channel/logical-groups/precheck":   false,
		http.MethodPost + " /api/channel/logical-groups":            false,
		http.MethodPut + " /api/channel/logical-groups/:id/members": false,
		http.MethodPut + " /api/channel/logical-groups/:id/status":  false,
		http.MethodDelete + " /api/channel/logical-groups/:id":      false,
	}
	for _, route := range engine.Routes() {
		if _, ok := want[route.Method+" "+route.Path]; ok {
			want[route.Method+" "+route.Path] = true
		}
	}
	for route, found := range want {
		assert.True(t, found, "missing route %s", route)
	}
}

func TestLogicalChannelGroupMutationPermissions(t *testing.T) {
	assertChannelRoutePermission(t, http.MethodPut, "/logical-groups/:id/status", authz.ChannelWrite, controller.UpdateLogicalChannelGroupStatus)
	assertChannelRoutePermission(t, http.MethodDelete, "/logical-groups/:id", authz.ChannelSensitiveWrite, controller.DeleteLogicalChannelGroup)
}
