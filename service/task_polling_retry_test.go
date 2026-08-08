package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type taskPollingResponseAdaptor struct {
	status int
	body   io.ReadCloser
}

func (a *taskPollingResponseAdaptor) Init(_ *relaycommon.RelayInfo) {}

func (a *taskPollingResponseAdaptor) FetchTask(_ string, _ string, _ map[string]any, _ string) (*http.Response, error) {
	if a.status == 0 {
		return nil, nil
	}
	body := a.body
	if body == nil {
		body = io.NopCloser(bytes.NewReader(nil))
	}
	return &http.Response{StatusCode: a.status, Body: body}, nil
}

func (a *taskPollingResponseAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return nil, nil
}

func (a *taskPollingResponseAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return 0
}

type taskPollingCloseTrackingBody struct {
	io.Reader
	closed bool
}

func (b *taskPollingCloseTrackingBody) Close() error {
	b.closed = true
	return nil
}

func TestResolveTaskPollingBaseURLValidatesDefaultsAndUnknownTypes(t *testing.T) {
	baseURL, err := ResolveTaskPollingBaseURL(&model.Channel{Type: constant.ChannelTypeKling})
	require.NoError(t, err)
	assert.Equal(t, constant.ChannelBaseURLs[constant.ChannelTypeKling], baseURL)

	_, err = ResolveTaskPollingBaseURL(nil)
	require.Error(t, err)

	emptyBaseURL := ""
	_, err = ResolveTaskPollingBaseURL(&model.Channel{
		Id:      808,
		Type:    len(constant.ChannelBaseURLs) + 1,
		BaseURL: &emptyBaseURL,
	})
	require.Error(t, err)
}

func TestUpdateVideoSingleTaskRejectsEmptyUpstreamResponses(t *testing.T) {
	task := &model.Task{
		TaskID: "public-task",
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "upstream-task",
		},
	}
	channel := &model.Channel{Id: 809, Type: constant.ChannelTypeKling}
	taskMap := map[string]*model.Task{"upstream-task": task}

	err := updateVideoSingleTask(context.Background(), &taskPollingResponseAdaptor{}, channel, "upstream-task", taskMap)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty response")

	err = updateVideoSingleTask(context.Background(), &taskPollingResponseAdaptor{status: http.StatusBadGateway}, channel, "upstream-task", taskMap)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 502")
}

func TestUpdateSunoTasksClosesNonSuccessResponseBody(t *testing.T) {
	truncate(t)

	const channelID = 810
	baseURL := "https://suno.invalid"
	require.NoError(t, model.DB.Create(&model.Channel{
		Id:      channelID,
		Type:    constant.ChannelTypeSunoAPI,
		Name:    "suno_non_success",
		Key:     "sk-suno-channel",
		Status:  common.ChannelStatusEnabled,
		BaseURL: &baseURL,
	}).Error)

	body := &taskPollingCloseTrackingBody{Reader: bytes.NewReader(nil)}
	adaptor := &taskPollingResponseAdaptor{status: http.StatusBadGateway, body: body}
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })

	err := updateSunoTasks(context.Background(), channelID, []string{"upstream-task"}, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "status code: 502")
	assert.True(t, body.closed)
}
