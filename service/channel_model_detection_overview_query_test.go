package service

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type channelModelDetectionObservedContext struct {
	context.Context
	reached chan struct{}
	once    sync.Once
}

func (ctx *channelModelDetectionObservedContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.reached) })
	return ctx.Context.Done()
}

func TestChannelModelDetectionOverviewQueryMergesConcurrentReads(t *testing.T) {
	channelModelDetectionOverviewQueryGeneration.Add(1)
	started := make(chan struct{})
	release := make(chan struct{})
	var buildCount atomic.Int64
	build := func(context.Context, *gorm.DB, int64) (ChannelModelDetectionOverviewResponse, error) {
		if buildCount.Add(1) == 1 {
			close(started)
		}
		<-release
		return ChannelModelDetectionOverviewResponse{}, nil
	}

	firstResult := make(chan error, 1)
	go func() {
		_, err := getCurrentChannelModelDetectionOverviewWithBuild(context.Background(), build)
		firstResult <- err
	}()
	<-started

	secondContext := &channelModelDetectionObservedContext{
		Context: context.Background(),
		reached: make(chan struct{}),
	}
	secondResult := make(chan error, 1)
	go func() {
		_, err := getCurrentChannelModelDetectionOverviewWithBuild(secondContext, build)
		secondResult <- err
	}()
	<-secondContext.reached
	close(release)

	require.NoError(t, <-firstResult)
	require.NoError(t, <-secondResult)
	assert.Equal(t, int64(1), buildCount.Load())
}

func TestCurrentChannelModelDetectionOverviewRejectsOversizedResponse(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	require.NoError(t, db.Create(&model.Channel{
		Id: 890, Name: strings.Repeat("x", channelModelDetectionResponseMaxBytes), Status: common.ChannelStatusEnabled,
	}).Error)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	_, err := GetCurrentChannelModelDetectionOverview(context.Background())
	assert.ErrorIs(t, err, ErrChannelModelDetectionResponseTooLarge)
}

func TestChannelModelDetectionOverviewQueryGenerationFencesInflightReads(t *testing.T) {
	channelModelDetectionOverviewQueryGeneration.Add(1)
	started := make(chan struct{})
	release := make(chan struct{})
	var buildCount atomic.Int64
	build := func(context.Context, *gorm.DB, int64) (ChannelModelDetectionOverviewResponse, error) {
		if buildCount.Add(1) == 1 {
			close(started)
			<-release
		}
		return ChannelModelDetectionOverviewResponse{}, nil
	}

	firstResult := make(chan error, 1)
	go func() {
		_, err := getCurrentChannelModelDetectionOverviewWithBuild(context.Background(), build)
		firstResult <- err
	}()
	<-started
	NotifyChannelModelDetectionOverviewChanged()

	secondContext := &channelModelDetectionObservedContext{
		Context: context.Background(),
		reached: make(chan struct{}),
	}
	secondResult := make(chan error, 1)
	go func() {
		_, err := getCurrentChannelModelDetectionOverviewWithBuild(secondContext, build)
		secondResult <- err
	}()
	<-secondContext.reached
	close(release)

	require.NoError(t, <-firstResult)
	require.NoError(t, <-secondResult)
	assert.GreaterOrEqual(t, buildCount.Load(), int64(2))
}

func TestChannelModelDetectionOverviewQueryDoesNotRetainCompletedResults(t *testing.T) {
	channelModelDetectionOverviewQueryGeneration.Add(1)
	var buildCount atomic.Int64
	build := func(context.Context, *gorm.DB, int64) (ChannelModelDetectionOverviewResponse, error) {
		buildCount.Add(1)
		return ChannelModelDetectionOverviewResponse{}, nil
	}

	_, err := getCurrentChannelModelDetectionOverviewWithBuild(context.Background(), build)
	require.NoError(t, err)
	_, err = getCurrentChannelModelDetectionOverviewWithBuild(context.Background(), build)
	require.NoError(t, err)
	assert.Equal(t, int64(2), buildCount.Load())
}
