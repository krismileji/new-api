package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"golang.org/x/sync/singleflight"
)

const channelStatusProbeOverviewQueryTimeout = 30 * time.Second

var channelStatusProbeOverviewQuerySingleflight singleflight.Group
var channelStatusProbeOverviewQueryGeneration atomic.Uint64

// queryChannelStatusProbeOverview always reads the current database state.
// Concurrent requests for the same filter and generation share only the
// in-flight query; completed results are never retained.
func queryChannelStatusProbeOverview(
	ctx context.Context,
	selectedModel string,
) (channelStatusProbeOverviewResponse, error) {
	for {
		db := model.DB
		generation := channelStatusProbeOverviewQueryGeneration.Load()
		modelDigest := sha256.Sum256([]byte(selectedModel))
		key := fmt.Sprintf("%p:%d:%x", db, generation, modelDigest)
		resultChannel := channelStatusProbeOverviewQuerySingleflight.DoChan(key, func() (any, error) {
			queryCtx, cancel := context.WithTimeout(context.Background(), channelStatusProbeOverviewQueryTimeout)
			defer cancel()
			return buildChannelStatusProbeOverview(queryCtx, selectedModel, common.GetTimestamp())
		})

		select {
		case <-ctx.Done():
			return channelStatusProbeOverviewResponse{}, ctx.Err()
		case result := <-resultChannel:
			if generation != channelStatusProbeOverviewQueryGeneration.Load() || db != model.DB {
				continue
			}
			if result.Err != nil {
				return channelStatusProbeOverviewResponse{}, result.Err
			}
			return result.Val.(channelStatusProbeOverviewResponse), nil
		}
	}
}

// notifyChannelStatusProbeOverviewChanged prevents requests after a write
// from joining a query that started before that write.
func notifyChannelStatusProbeOverviewChanged() {
	channelStatusProbeOverviewQueryGeneration.Add(1)
}
