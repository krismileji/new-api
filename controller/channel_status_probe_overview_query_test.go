package controller

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type channelStatusProbeObservedContext struct {
	context.Context
	reached chan struct{}
	once    sync.Once
}

func (ctx *channelStatusProbeObservedContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.reached) })
	return ctx.Context.Done()
}

func TestChannelStatusProbeOverviewQueryMergesConcurrentReads(t *testing.T) {
	channelStatusProbeOverviewQueryGeneration.Add(1)
	started := make(chan struct{})
	release := make(chan struct{})
	var buildCount atomic.Int64
	build := func(context.Context, *gorm.DB, string, int64) (channelStatusProbeOverviewResponse, error) {
		if buildCount.Add(1) == 1 {
			close(started)
		}
		<-release
		return channelStatusProbeOverviewResponse{}, nil
	}

	firstResult := make(chan error, 1)
	go func() {
		_, err := queryChannelStatusProbeOverviewWithBuild(context.Background(), "model-a", build)
		firstResult <- err
	}()
	<-started

	secondContext := &channelStatusProbeObservedContext{
		Context: context.Background(),
		reached: make(chan struct{}),
	}
	secondResult := make(chan error, 1)
	go func() {
		_, err := queryChannelStatusProbeOverviewWithBuild(secondContext, "model-a", build)
		secondResult <- err
	}()
	<-secondContext.reached
	close(release)

	require.NoError(t, <-firstResult)
	require.NoError(t, <-secondResult)
	assert.Equal(t, int64(1), buildCount.Load())
}

func TestChannelStatusProbeOverviewQueryGenerationFencesInflightReads(t *testing.T) {
	channelStatusProbeOverviewQueryGeneration.Add(1)
	started := make(chan struct{})
	release := make(chan struct{})
	var buildCount atomic.Int64
	build := func(context.Context, *gorm.DB, string, int64) (channelStatusProbeOverviewResponse, error) {
		if buildCount.Add(1) == 1 {
			close(started)
			<-release
		}
		return channelStatusProbeOverviewResponse{}, nil
	}

	firstResult := make(chan error, 1)
	go func() {
		_, err := queryChannelStatusProbeOverviewWithBuild(context.Background(), "model-b", build)
		firstResult <- err
	}()
	<-started
	notifyChannelStatusProbeOverviewChanged()

	secondContext := &channelStatusProbeObservedContext{
		Context: context.Background(),
		reached: make(chan struct{}),
	}
	secondResult := make(chan error, 1)
	go func() {
		_, err := queryChannelStatusProbeOverviewWithBuild(secondContext, "model-b", build)
		secondResult <- err
	}()
	<-secondContext.reached
	close(release)

	require.NoError(t, <-firstResult)
	require.NoError(t, <-secondResult)
	assert.GreaterOrEqual(t, buildCount.Load(), int64(2))
}

func TestChannelStatusProbeOverviewQueryDoesNotRetainCompletedResults(t *testing.T) {
	channelStatusProbeOverviewQueryGeneration.Add(1)
	var buildCount atomic.Int64
	build := func(context.Context, *gorm.DB, string, int64) (channelStatusProbeOverviewResponse, error) {
		buildCount.Add(1)
		return channelStatusProbeOverviewResponse{}, nil
	}

	_, err := queryChannelStatusProbeOverviewWithBuild(context.Background(), "model-c", build)
	require.NoError(t, err)
	_, err = queryChannelStatusProbeOverviewWithBuild(context.Background(), "model-c", build)
	require.NoError(t, err)
	assert.Equal(t, int64(2), buildCount.Load())
}

func TestChannelStatusProbeOverviewGenerationTracksSharedChannelWrites(t *testing.T) {
	before := channelStatusProbeOverviewQueryGeneration.Load()
	service.NotifyChannelModelDetectionOverviewChanged()
	assert.Equal(t, before+1, channelStatusProbeOverviewQueryGeneration.Load())
}

func TestChannelStatusProbeOverviewBuildUsesQueryContext(t *testing.T) {
	setupChannelStatusProbeControllerTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := buildChannelStatusProbeOverview(ctx, model.DB, "", 1_700_000_000)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestChannelStatusProbeOverviewBuildFiltersDirectQueriesByModel(t *testing.T) {
	firstChannel := setupChannelStatusProbeControllerTest(t)
	secondChannel := &model.Channel{
		Id: 8802, Name: "状态探测筛选渠道", Type: constant.ChannelTypeOpenAI,
		Status: common.ChannelStatusEnabled, Models: "model-b", Group: "archive",
	}
	require.NoError(t, model.DB.Create(secondChannel).Error)

	firstModels, err := common.Marshal([]string{"model-a"})
	require.NoError(t, err)
	secondModels, err := common.Marshal([]string{"model-b"})
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&[]model.ChannelStatusProbeConfig{
		{
			ChannelId: firstChannel.Id, Enabled: true, ModelsJSON: string(firstModels),
			IntervalSeconds: 300, DisplayValue: 60,
			DisplayUnit: model.ChannelStatusProbeDisplayUnitMinute, Revision: 1,
		},
		{
			ChannelId: secondChannel.Id, Enabled: true, ModelsJSON: string(secondModels),
			IntervalSeconds: 300, DisplayValue: 24,
			DisplayUnit: model.ChannelStatusProbeDisplayUnitHour, Revision: 1,
		},
	}).Error)
	require.NoError(t, model.DB.Create(&[]model.ChannelStatusProbeState{
		{ChannelId: firstChannel.Id, ModelName: "model-a", Result: model.ChannelStatusProbeResultSuccess, FinishedAt: 1_699_999_990},
		{ChannelId: secondChannel.Id, ModelName: "model-b", Result: model.ChannelStatusProbeResultUpstreamFailure, FinishedAt: 1_699_999_990},
	}).Error)
	require.NoError(t, model.DB.Create(&[]model.ChannelRatioMonitor{
		{ChannelId: firstChannel.Id, Ratio: 1.25, UpdatedTime: 1_699_999_900},
		{ChannelId: secondChannel.Id, Ratio: 2.5, UpdatedTime: 1_699_999_900},
	}).Error)
	dayStart := model.ChannelDailyCostDayStart(1_700_000_000)
	require.NoError(t, model.DB.Create(&[]model.ChannelDailyCost{
		{ChannelId: firstChannel.Id, DayStart: dayStart, ProbeCostNanoCNY: 100},
		{ChannelId: secondChannel.Id, DayStart: dayStart, ProbeCostNanoCNY: 200},
	}).Error)

	overview, err := buildChannelStatusProbeOverview(
		context.Background(), model.DB, "model-a", 1_700_000_000,
	)
	require.NoError(t, err)
	require.Len(t, overview.Channels, 1)
	assert.Equal(t, firstChannel.Id, overview.Channels[0].Id)
	require.Len(t, overview.Channels[0].ModelStatuses, 1)
	assert.Equal(t, "model-a", overview.Channels[0].ModelStatuses[0].ModelName)
	assert.Equal(t, []string{"model-a", "model-b"}, overview.Models)
	assert.Equal(t, []string{"archive", "default", "vip"}, overview.Groups)
	assert.Equal(t, []string{"model-a"}, overview.ModelsByGroup["default"])
	assert.Equal(t, []string{"model-a"}, overview.ModelsByGroup["vip"])
	assert.Equal(t, []string{"model-b"}, overview.ModelsByGroup["archive"])
}
