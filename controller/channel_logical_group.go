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

type logicalChannelGroupMemberRequest struct {
	ChannelID int   `json:"channel_id"`
	Weight    *uint `json:"weight,omitempty"`
}

type createLogicalChannelGroupRequest struct {
	Name    string                             `json:"name"`
	Remark  string                             `json:"remark"`
	Status  int                                `json:"status,omitempty"`
	Members []logicalChannelGroupMemberRequest `json:"members"`
}

type replaceLogicalChannelGroupMembersRequest struct {
	Revision int64                              `json:"revision"`
	Members  []logicalChannelGroupMemberRequest `json:"members"`
}

type logicalChannelGroupDeleteRequest struct {
	Revision int64 `json:"revision"`
}

type logicalChannelGroupStatusRequest struct {
	Revision int64 `json:"revision"`
	Status   int   `json:"status"`
}

func GetLogicalChannelGroups(c *gin.Context) {
	groups, err := service.ListLogicalChannelGroups()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, groups)
}

func GetLogicalChannelGroup(c *gin.Context) {
	id, err := parseLogicalChannelGroupID(c)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	group, err := service.GetLogicalChannelGroup(id)
	if err != nil {
		writeLogicalChannelGroupError(c, err)
		return
	}
	common.ApiSuccess(c, group)
}

func CreateLogicalChannelGroup(c *gin.Context) {
	var request createLogicalChannelGroupRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "逻辑渠道组请求格式无效"})
		return
	}
	inputs := make([]service.LogicalChannelGroupMemberInput, 0, len(request.Members))
	for _, member := range request.Members {
		inputs = append(inputs, service.LogicalChannelGroupMemberInput{ChannelID: member.ChannelID, Weight: member.Weight})
	}
	group, err := service.CreateLogicalChannelGroup(request.Name, request.Remark, request.Status, inputs)
	if err != nil {
		writeLogicalChannelGroupError(c, err)
		return
	}
	recordManageAudit(c, "channel.logical_group_create", map[string]interface{}{"id": group.ID, "revision": group.Revision, "member_count": len(group.Members)})
	common.ApiSuccess(c, group)
}

func PrecheckLogicalChannelGroup(c *gin.Context) {
	var request struct {
		ChannelIDs []int `json:"channel_ids"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "成员预检请求格式无效"})
		return
	}
	common.ApiSuccess(c, service.PrecheckLogicalChannelGroup(request.ChannelIDs))
}

func ReplaceLogicalChannelGroupMembers(c *gin.Context) {
	id, err := parseLogicalChannelGroupID(c)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	var request replaceLogicalChannelGroupMembersRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "逻辑渠道组成员请求格式无效"})
		return
	}
	inputs := make([]service.LogicalChannelGroupMemberInput, 0, len(request.Members))
	for _, member := range request.Members {
		inputs = append(inputs, service.LogicalChannelGroupMemberInput{ChannelID: member.ChannelID, Weight: member.Weight})
	}
	group, err := service.ReplaceLogicalChannelGroupMembers(id, request.Revision, inputs)
	if err != nil {
		writeLogicalChannelGroupError(c, err)
		return
	}
	recordManageAudit(c, "channel.logical_group_members_replace", map[string]interface{}{"id": id, "revision": group.Revision, "member_count": len(group.Members)})
	common.ApiSuccess(c, group)
}

func UpdateLogicalChannelGroupStatus(c *gin.Context) {
	id, err := parseLogicalChannelGroupID(c)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	var request logicalChannelGroupStatusRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.Revision <= 0 {
		writeLogicalChannelGroupError(c, service.ErrLogicalChannelGroupInvalidRevision)
		return
	}
	group, err := service.UpdateLogicalChannelGroupStatus(id, request.Revision, request.Status)
	if err != nil {
		writeLogicalChannelGroupError(c, err)
		return
	}
	recordManageAudit(c, "channel.logical_group_status_update", map[string]interface{}{"id": id, "status": request.Status, "revision": group.Revision})
	common.ApiSuccess(c, group)
}

func DeleteLogicalChannelGroup(c *gin.Context) {
	id, err := parseLogicalChannelGroupID(c)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	revision, err := parseRevision(c)
	if err != nil {
		writeLogicalChannelGroupError(c, err)
		return
	}
	if err := service.DeleteLogicalChannelGroup(id, revision); err != nil {
		writeLogicalChannelGroupError(c, err)
		return
	}
	recordManageAudit(c, "channel.logical_group_delete", map[string]interface{}{"id": id, "revision": revision})
	common.ApiSuccess(c, gin.H{"id": id})
}

func parseLogicalChannelGroupID(c *gin.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("逻辑渠道组 ID 无效")
	}
	return id, nil
}

func parseRevision(c *gin.Context) (int64, error) {
	if value := c.Query("revision"); value != "" {
		revision, err := strconv.ParseInt(value, 10, 64)
		if err == nil {
			return revision, nil
		}
	}
	var request logicalChannelGroupDeleteRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			return 0, errors.New("逻辑渠道组 revision 无效")
		}
	}
	if request.Revision <= 0 {
		return 0, service.ErrLogicalChannelGroupInvalidRevision
	}
	return request.Revision, nil
}

func writeLogicalChannelGroupError(c *gin.Context, err error) {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, service.ErrLogicalChannelGroupNotFound):
		status = http.StatusNotFound
	case errors.Is(err, model.ErrChannelLogicalGroupRevisionConflict):
		status = http.StatusConflict
	case errors.Is(err, service.ErrLogicalChannelGroupInvalidRevision), errors.Is(err, model.ErrChannelLogicalGroupInvalidRevision):
		status = http.StatusBadRequest
	}
	c.JSON(status, gin.H{"success": false, "message": err.Error()})
}
