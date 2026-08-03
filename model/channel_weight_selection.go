package model

import (
	"errors"
	"math"
	"math/rand"
)

// chooseChannelByWeights applies the same routing semantics to the database
// and memory-cache selectors. A zero weight is an explicit exclusion whenever
// another route has positive weight; only an all-zero layer falls back to
// equal selection.
func chooseChannelByWeights(channelIDs []int, weights []uint) (int, error) {
	if len(channelIDs) == 0 || len(channelIDs) != len(weights) {
		return 0, errors.New("渠道权重选择候选无效")
	}

	weightSum := uint64(0)
	for _, weight := range weights {
		if math.MaxUint64-weightSum < uint64(weight) {
			return 0, errors.New("渠道路由权重溢出")
		}
		weightSum += uint64(weight)
	}
	allZero := weightSum == 0
	if allZero {
		weightSum = uint64(len(weights))
	}
	if weightSum > math.MaxInt64 {
		return 0, errors.New("渠道路由权重超过随机选择范围")
	}

	selected := uint64(rand.Int63n(int64(weightSum)))
	for index, weight := range weights {
		effectiveWeight := uint64(weight)
		if allZero {
			effectiveWeight = 1
		}
		if selected < effectiveWeight {
			return channelIDs[index], nil
		}
		selected -= effectiveWeight
	}
	return 0, errors.New("未找到可用渠道")
}
