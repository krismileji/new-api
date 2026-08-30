package service

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

const channelModelDetectionOverviewQueryTimeout = 30 * time.Second

var channelModelDetectionOverviewQuerySingleflight singleflight.Group
var channelModelDetectionOverviewQueryGeneration atomic.Uint64

type channelModelDetectionOverviewBuildFunc func(
	context.Context,
	*gorm.DB,
	int64,
) (ChannelModelDetectionOverviewResponse, error)

// GetCurrentChannelModelDetectionOverview always reads the current database
// state. Concurrent requests for the same generation share only the in-flight
// query; completed results are never retained.
func GetCurrentChannelModelDetectionOverview(ctx context.Context) (ChannelModelDetectionOverviewResponse, error) {
	return getCurrentChannelModelDetectionOverviewWithBuild(ctx, GetChannelModelDetectionOverview)
}

func getCurrentChannelModelDetectionOverviewWithBuild(
	ctx context.Context,
	build channelModelDetectionOverviewBuildFunc,
) (ChannelModelDetectionOverviewResponse, error) {
	for {
		db := model.DB
		generation := channelModelDetectionOverviewQueryGeneration.Load()
		key := fmt.Sprintf("%p:%d", db, generation)
		resultChannel := channelModelDetectionOverviewQuerySingleflight.DoChan(key, func() (any, error) {
			queryCtx, cancel := context.WithTimeout(context.Background(), channelModelDetectionOverviewQueryTimeout)
			defer cancel()
			return build(queryCtx, db, common.GetTimestamp())
		})

		select {
		case <-ctx.Done():
			return ChannelModelDetectionOverviewResponse{}, ctx.Err()
		case result := <-resultChannel:
			if generation != channelModelDetectionOverviewQueryGeneration.Load() || db != model.DB {
				continue
			}
			if result.Err != nil {
				return ChannelModelDetectionOverviewResponse{}, result.Err
			}
			return result.Val.(ChannelModelDetectionOverviewResponse), nil
		}
	}
}

// NotifyChannelModelDetectionOverviewChanged prevents requests after a write
// from joining a query that started before that write.
func NotifyChannelModelDetectionOverviewChanged() {
	channelModelDetectionOverviewQueryGeneration.Add(1)
}
