package controller

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const channelMonitorSmartScheduleExecutionDetailPageSize = 50

type channelMonitorSmartScheduleExecutionDetailPage struct {
	Page          int                                  `json:"page"`
	PageSize      int                                  `json:"page_size"`
	Total         int                                  `json:"total"`
	Items         []channelSmartScheduleTaskAdjustment `json:"items"`
	Groups        []string                             `json:"groups"`
	Models        []string                             `json:"models"`
	ModelsByGroup map[string][]string                  `json:"models_by_group"`
	ChannelNames  map[string]string                    `json:"channel_names"`
}

// GetChannelMonitorSmartScheduleExecutionDetails returns only one task's
// filtered detail page. The task list endpoint intentionally excludes these
// rows so opening the execution record center does not load every adjustment.
func GetChannelMonitorSmartScheduleExecutionDetails(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("task_id"))
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "执行任务 ID 不能为空",
		})
		return
	}
	task, err := model.GetSystemTaskByTaskID(taskID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if task == nil || task.Type != channelMonitorSmartScheduleTaskType {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "智能调度执行记录不存在",
		})
		return
	}

	detailsByTask, err := model.GetChannelSmartScheduleExecutionDetails([]string{taskID})
	if err != nil {
		common.ApiError(c, err)
		return
	}

	groupFilter := strings.TrimSpace(c.Query("group"))
	if groupFilter == "all" {
		groupFilter = ""
	}
	modelFilter := strings.TrimSpace(c.Query("model"))
	if modelFilter == "all" {
		modelFilter = ""
	}
	actionFilter := strings.TrimSpace(c.Query("action"))
	if actionFilter == "all" {
		actionFilter = ""
	}
	if actionFilter != "" {
		switch actionFilter {
		case channelSmartScheduleAdjustmentUpdated,
			channelSmartScheduleAdjustmentUnchanged,
			channelSmartScheduleAdjustmentSkipped,
			channelSmartScheduleAdjustmentFailed:
		default:
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "执行结果筛选条件无效",
			})
			return
		}
	}
	search := strings.ToLower(strings.TrimSpace(c.Query("q")))

	groups := make(map[string]struct{})
	models := make(map[string]struct{})
	modelSetsByGroup := make(map[string]map[string]struct{})
	filtered := make([]channelSmartScheduleTaskAdjustment, 0, len(detailsByTask[taskID]))
	for _, stored := range detailsByTask[taskID] {
		var adjustment channelSmartScheduleTaskAdjustment
		if err := common.UnmarshalJsonStr(stored.Payload, &adjustment); err != nil {
			common.ApiError(c, err)
			return
		}
		groups[adjustment.Group] = struct{}{}
		models[adjustment.Model] = struct{}{}
		if modelSetsByGroup[adjustment.Group] == nil {
			modelSetsByGroup[adjustment.Group] = make(map[string]struct{})
		}
		modelSetsByGroup[adjustment.Group][adjustment.Model] = struct{}{}
		if groupFilter != "" && adjustment.Group != groupFilter {
			continue
		}
		if modelFilter != "" && adjustment.Model != modelFilter {
			continue
		}
		if actionFilter != "" && adjustment.Action != actionFilter {
			continue
		}
		if search != "" {
			decisionReason := ""
			selectionReason := ""
			adjustmentReason := ""
			if adjustment.ScoreDetails != nil {
				decisionReason = adjustment.ScoreDetails.Decision.Reason
				selectionReason = adjustment.ScoreDetails.Decision.SelectionReason
				adjustmentReason = adjustment.ScoreDetails.Decision.AdjustmentReason
			}
			searchText := strings.ToLower(strings.Join([]string{
				adjustment.ChannelName,
				strconv.Itoa(adjustment.ChannelId),
				adjustment.Group,
				adjustment.Model,
				adjustment.Reason,
				adjustment.FailureStage,
				decisionReason,
				selectionReason,
				adjustmentReason,
			}, "\n"))
			if !strings.Contains(searchText, search) {
				continue
			}
		}
		filtered = append(filtered, adjustment)
	}
	sort.SliceStable(filtered, func(i int, j int) bool {
		leftPriority, leftWeight := channelSmartScheduleAdjustmentRoutingOrder(filtered[i])
		rightPriority, rightWeight := channelSmartScheduleAdjustmentRoutingOrder(filtered[j])
		if leftPriority != rightPriority {
			return leftPriority > rightPriority
		}
		if leftWeight != rightWeight {
			return leftWeight > rightWeight
		}
		return filtered[i].ChannelId < filtered[j].ChannelId
	})

	groupList := make([]string, 0, len(groups))
	for group := range groups {
		groupList = append(groupList, group)
	}
	sort.Strings(groupList)
	modelList := make([]string, 0, len(models))
	for modelName := range models {
		modelList = append(modelList, modelName)
	}
	sort.Strings(modelList)
	modelsByGroup := make(map[string][]string, len(modelSetsByGroup))
	for group, modelSet := range modelSetsByGroup {
		groupModels := make([]string, 0, len(modelSet))
		for modelName := range modelSet {
			groupModels = append(groupModels, modelName)
		}
		sort.Strings(groupModels)
		modelsByGroup[group] = groupModels
	}

	pageInfo := common.GetPageQuery(c)
	if c.Query("page_size") == "" || pageInfo.PageSize < 1 {
		pageInfo.PageSize = channelMonitorSmartScheduleExecutionDetailPageSize
	}
	start := pageInfo.GetStartIdx()
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + pageInfo.GetPageSize()
	if end > len(filtered) {
		end = len(filtered)
	}
	items := filtered[start:end]
	channelNames := make(map[string]string, len(items))
	for _, item := range items {
		channelNames[strconv.Itoa(item.ChannelId)] = item.ChannelName
	}
	page := channelMonitorSmartScheduleExecutionDetailPage{
		Page:          pageInfo.GetPage(),
		PageSize:      pageInfo.GetPageSize(),
		Total:         len(filtered),
		Items:         items,
		Groups:        groupList,
		Models:        modelList,
		ModelsByGroup: modelsByGroup,
		ChannelNames:  channelNames,
	}
	common.ApiSuccess(c, page)
}

func channelSmartScheduleAdjustmentRoutingOrder(
	adjustment channelSmartScheduleTaskAdjustment,
) (int64, uint) {
	if adjustment.Action == channelSmartScheduleAdjustmentFailed {
		return adjustment.OldPriority, adjustment.OldWeight
	}
	return adjustment.NewPriority, adjustment.NewWeight
}
