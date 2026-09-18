package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func ListChannelLimitGroups(c *gin.Context) {
	groups, err := service.ListChannelLimitGroupViews(c.Request.Context())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, groups)
}

func SaveChannelLimitGroup(c *gin.Context) {
	var request struct {
		Name             string                          `json:"name"`
		Revision         *int64                          `json:"revision"`
		ConcurrencyLimit *int                            `json:"concurrency_limit"`
		RPMLimit         *int                            `json:"rpm_limit"`
		Enabled          *bool                           `json:"enabled"`
		Tiers            []model.ChannelLimitGroupTier   `json:"tiers"`
		Members          []model.ChannelLimitGroupMember `json:"members"`
	}
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 128*1024), &request); err != nil || request.Revision == nil || request.ConcurrencyLimit == nil || request.RPMLimit == nil || request.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请提供完整的共享限流配置和版本"})
		return
	}
	group := model.ChannelLimitGroup{Name: request.Name, Revision: *request.Revision, ConcurrencyLimit: *request.ConcurrencyLimit, RPMLimit: *request.RPMLimit, Enabled: *request.Enabled, Tiers: request.Tiers, Members: request.Members}
	if id := c.Param("id"); id != "" {
		parsed, err := strconv.ParseInt(id, 10, 64)
		if err != nil || parsed <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "共享限流组 ID 无效"})
			return
		}
		group.ID = parsed
	}
	before, err := channelLimitGroupAuditSnapshot(c, group.ID)
	if err != nil {
		writeChannelLimitGroupError(c, err)
		return
	}
	if err := service.SaveChannelLimitGroup(c.Request.Context(), &group, false); err != nil {
		recordManageAudit(c, "channel.limit_group_save", map[string]any{"id": group.ID, "before": before, "requested": group, "error": err.Error()})
		writeChannelLimitGroupError(c, err)
		return
	}
	recordManageAudit(c, "channel.limit_group_save", map[string]any{"id": group.ID, "revision": group.Revision, "before": before, "after": group, "published": true})
	common.ApiSuccess(c, group)
}

func DeleteChannelLimitGroup(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	revision, revisionErr := strconv.ParseInt(c.Query("revision"), 10, 64)
	if err != nil || revisionErr != nil || id <= 0 || revision <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "共享限流组 ID 或版本无效"})
		return
	}
	group := model.ChannelLimitGroup{ID: id, Revision: revision}
	before, err := channelLimitGroupAuditSnapshot(c, id)
	if err != nil {
		writeChannelLimitGroupError(c, err)
		return
	}
	if err := service.SaveChannelLimitGroup(c.Request.Context(), &group, true); err != nil {
		recordManageAudit(c, "channel.limit_group_delete", map[string]any{"id": id, "before": before, "error": err.Error()})
		writeChannelLimitGroupError(c, err)
		return
	}
	recordManageAudit(c, "channel.limit_group_delete", map[string]any{"id": id, "revision": group.Revision, "before": before, "published": true})
	common.ApiSuccess(c, gin.H{"id": id})
}

func writeChannelLimitGroupError(c *gin.Context, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, model.ErrChannelLimitConflict) {
		status = http.StatusConflict
	}
	c.JSON(status, gin.H{"success": false, "message": err.Error()})
}

func channelLimitGroupAuditSnapshot(c *gin.Context, id int64) (*model.ChannelLimitGroup, error) {
	if id == 0 {
		return nil, nil
	}
	groups, _, err := model.ReadChannelLimitGroups(model.DB.WithContext(c.Request.Context()))
	if err != nil {
		return nil, err
	}
	for _, group := range groups {
		if group.ID == id {
			return &group, nil
		}
	}
	return nil, model.ErrChannelLimitConflict
}
