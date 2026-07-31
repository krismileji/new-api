package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

const (
	channelMonitorSmartScheduleModelTestConcurrency = 4
	maxChannelMonitorSmartScheduleModelTestChannels = 500
)

type channelSmartScheduleModelTestRequest struct {
	Group        string `json:"group"`
	Model        string `json:"model"`
	Stream       *bool  `json:"stream"`
	EndpointType string `json:"endpoint_type"`
	ChannelIds   []int  `json:"channel_ids"`
}

func (request *channelSmartScheduleModelTestRequest) normalize() error {
	request.Group = strings.TrimSpace(request.Group)
	if request.Group == "" {
		return errors.New("分组不能为空")
	}
	if utf8.RuneCountInString(request.Group) > maxChannelMonitorSmartScheduleGroupLength {
		return fmt.Errorf("分组不能超过 %d 个字符", maxChannelMonitorSmartScheduleGroupLength)
	}
	request.Model = strings.TrimSpace(request.Model)
	if request.Model == "" {
		return errors.New("模型不能为空")
	}
	if utf8.RuneCountInString(request.Model) > maxChannelMonitorSmartScheduleModelLength {
		return fmt.Errorf("模型不能超过 %d 个字符", maxChannelMonitorSmartScheduleModelLength)
	}

	request.EndpointType = strings.ToLower(strings.TrimSpace(request.EndpointType))
	if request.EndpointType == "" {
		request.EndpointType = "auto"
	}
	switch constant.EndpointType(request.EndpointType) {
	case constant.EndpointType("auto"),
		constant.EndpointTypeOpenAI,
		constant.EndpointTypeOpenAIResponse,
		constant.EndpointTypeOpenAIResponseCompact,
		constant.EndpointTypeAnthropic,
		constant.EndpointTypeGemini,
		constant.EndpointTypeJinaRerank,
		constant.EndpointTypeImageGeneration,
		constant.EndpointTypeEmbeddings:
	default:
		return errors.New("不支持的测试端点类型")
	}
	if request.Stream == nil {
		request.Stream = common.GetPointer(true)
	}
	if len(request.ChannelIds) > maxChannelMonitorSmartScheduleModelTestChannels {
		return fmt.Errorf("单次模型测试不能超过 %d 个渠道", maxChannelMonitorSmartScheduleModelTestChannels)
	}
	seenChannelIds := make(map[int]struct{}, len(request.ChannelIds))
	normalizedChannelIds := make([]int, 0, len(request.ChannelIds))
	for _, channelId := range request.ChannelIds {
		if channelId <= 0 {
			return errors.New("渠道 ID 必须大于 0")
		}
		if _, exists := seenChannelIds[channelId]; exists {
			continue
		}
		seenChannelIds[channelId] = struct{}{}
		normalizedChannelIds = append(normalizedChannelIds, channelId)
	}
	request.ChannelIds = normalizedChannelIds
	return nil
}

type channelSmartScheduleModelTestItem struct {
	ChannelId    int      `json:"channel_id"`
	ChannelName  string   `json:"channel_name"`
	Participates bool     `json:"participates"`
	Available    bool     `json:"available"`
	Status       string   `json:"status"`
	TotalMs      float64  `json:"total_ms"`
	FirstTokenMs *float64 `json:"first_token_ms,omitempty"`
	TPS          *float64 `json:"tps,omitempty"`
	Error        string   `json:"error,omitempty"`
	ErrorCode    string   `json:"error_code,omitempty"`
}

type channelSmartScheduleModelTestResult struct {
	Group        string                              `json:"group"`
	Model        string                              `json:"model"`
	Stream       bool                                `json:"stream"`
	EndpointType string                              `json:"endpoint_type"`
	Total        int                                 `json:"total"`
	Succeeded    int                                 `json:"succeeded"`
	Failed       int                                 `json:"failed"`
	Skipped      int                                 `json:"skipped"`
	Results      []channelSmartScheduleModelTestItem `json:"results"`
}

type channelSmartScheduleModelTestExecutor func(
	ctx context.Context,
	channel *model.Channel,
	testUserID int,
	testModel string,
	endpointType string,
	isStream bool,
) testResult

type channelSmartScheduleModelTestJob struct {
	index   int
	channel *model.Channel
}

type channelSmartScheduleModelTestExecution struct {
	performed bool
	channel   *model.Channel
	result    testResult
	duration  float64
}

func RunChannelMonitorSmartScheduleModelTest(c *gin.Context) {
	serveChannelMonitorSmartScheduleModelTest(c, testChannel)
}

func serveChannelMonitorSmartScheduleModelTest(
	c *gin.Context,
	executor channelSmartScheduleModelTestExecutor,
) {
	var request channelSmartScheduleModelTestRequest
	if c.Request == nil || c.Request.Body == nil {
		common.ApiErrorMsg(c, "请求体不能为空")
		return
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorMsg(c, "请求参数格式错误")
		return
	}
	if err := request.normalize(); err != nil {
		common.ApiError(c, err)
		return
	}
	testUserID, err := resolveChannelTestUserID(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := runChannelSmartScheduleModelTest(
		c.Request.Context(), request, testUserID, executor,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func runChannelSmartScheduleModelTest(
	ctx context.Context,
	request channelSmartScheduleModelTestRequest,
	testUserID int,
	executor channelSmartScheduleModelTestExecutor,
) (channelSmartScheduleModelTestResult, error) {
	result := channelSmartScheduleModelTestResult{
		Group:        request.Group,
		Model:        request.Model,
		Stream:       *request.Stream,
		EndpointType: request.EndpointType,
		Results:      []channelSmartScheduleModelTestItem{},
	}
	if executor == nil {
		return result, errors.New("模型测试执行器不可用")
	}
	if err := model.InitializeChannelSmartScheduleRouteStates(); err != nil {
		return result, err
	}
	routes, err := model.GetChannelSmartScheduleRoutes()
	if err != nil {
		return result, err
	}
	selectedChannelIds := make(map[int]struct{}, len(request.ChannelIds))
	for _, channelId := range request.ChannelIds {
		selectedChannelIds[channelId] = struct{}{}
	}
	selectedRoutes := make([]model.ChannelSmartScheduleRoute, 0)
	for _, route := range routes {
		if route.Group != request.Group || route.Model != request.Model {
			continue
		}
		if len(selectedChannelIds) > 0 {
			if _, selected := selectedChannelIds[route.ChannelId]; !selected {
				continue
			}
		}
		selectedRoutes = append(selectedRoutes, route)
	}
	if len(selectedRoutes) == 0 {
		return result, errors.New("未找到指定分组和模型的渠道路由")
	}

	channelIds := make([]int, 0, len(selectedRoutes))
	for _, route := range selectedRoutes {
		channelIds = append(channelIds, route.ChannelId)
	}
	channels, err := model.GetChannelsByIds(channelIds)
	if err != nil {
		return result, err
	}
	channelById := make(map[int]*model.Channel, len(channels))
	for _, channel := range channels {
		channelById[channel.Id] = channel
	}

	result.Results = make([]channelSmartScheduleModelTestItem, len(selectedRoutes))
	jobs := make([]channelSmartScheduleModelTestJob, 0, len(selectedRoutes))
	for index, route := range selectedRoutes {
		channel := channelById[route.ChannelId]
		participates := route.State.Participates()
		available := channel != nil && route.ChannelStatus == common.ChannelStatusEnabled && route.Enabled
		item := channelSmartScheduleModelTestItem{
			ChannelId:    route.ChannelId,
			ChannelName:  route.ChannelName,
			Participates: participates,
			Available:    available,
		}
		switch {
		case !participates:
			item.Status = "skipped"
			item.Error = "渠道未参与智能调度"
		case channel == nil:
			item.Status = "skipped"
			item.Error = "渠道不存在"
		case route.ChannelStatus != common.ChannelStatusEnabled:
			item.Status = "skipped"
			item.Error = "渠道未启用"
		case !route.Enabled:
			item.Status = "skipped"
			item.Error = "分组模型路由未启用"
		default:
			jobs = append(jobs, channelSmartScheduleModelTestJob{index: index, channel: channel})
		}
		result.Results[index] = item
	}

	executions := make([]channelSmartScheduleModelTestExecution, len(selectedRoutes))
	if len(jobs) > 0 {
		workerCount := min(channelMonitorSmartScheduleModelTestConcurrency, len(jobs))
		jobQueue := make(chan channelSmartScheduleModelTestJob, len(jobs))
		for _, job := range jobs {
			jobQueue <- job
		}
		close(jobQueue)

		var workers sync.WaitGroup
		workers.Add(workerCount)
		for range workerCount {
			go func() {
				defer workers.Done()
				for job := range jobQueue {
					if ctx.Err() != nil {
						result.Results[job.index].Status = "skipped"
						result.Results[job.index].Error = "模型测试已取消"
						continue
					}
					lease, acquired, concurrencyStatus, acquireErr := service.AcquireChannelConcurrency(ctx, job.channel.Id)
					if acquireErr != nil {
						result.Results[job.index].Status = "failure"
						result.Results[job.index].Error = "获取渠道并发配额失败：" + acquireErr.Error()
						continue
					}
					if !acquired {
						result.Results[job.index].Status = "skipped"
						result.Results[job.index].Error = fmt.Sprintf(
							"渠道并发已满（%d/%d）", concurrencyStatus.Active, concurrencyStatus.Limit,
						)
						continue
					}

					testCtx := withChannelSmartScheduleModelTestContext(ctx, request.Group)
					startedAt := time.Now()
					testOutcome := executor(
						testCtx, job.channel, testUserID, request.Model, request.EndpointType, *request.Stream,
					)
					durationMs := float64(time.Since(startedAt)) / float64(time.Millisecond)
					lease.Release()
					executions[job.index] = channelSmartScheduleModelTestExecution{
						performed: true,
						channel:   job.channel,
						result:    testOutcome,
						duration:  durationMs,
					}

					item := &result.Results[job.index]
					item.TotalMs = durationMs
					item.FirstTokenMs = testOutcome.firstResponseMilliseconds
					item.TPS = testOutcome.tokensPerSecond
					if testOutcome.localErr == nil && testOutcome.newAPIError == nil {
						item.Status = "success"
						continue
					}
					item.Status = "failure"
					if testOutcome.localErr != nil {
						item.Error = testOutcome.localErr.Error()
					} else {
						item.Error = testOutcome.newAPIError.Error()
					}
					if testOutcome.newAPIError != nil {
						item.ErrorCode = string(testOutcome.newAPIError.GetErrorCode())
					}
				}
			}()
		}
		workers.Wait()
	}

	for _, execution := range executions {
		if !execution.performed {
			continue
		}
		recordManualChannelSmartScheduleProbeResultForGroup(
			execution.channel, execution.result, execution.duration, request.Group,
		)
	}
	for _, item := range result.Results {
		switch item.Status {
		case "success":
			result.Succeeded++
		case "failure":
			result.Failed++
		case "skipped":
			result.Skipped++
		}
	}
	result.Total = len(result.Results)
	return result, nil
}
