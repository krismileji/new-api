package controller

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSettleTestQuotaSaturatesOverflowWithoutWrapping(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	quota, tieredResult := settleTestQuota(info, types.PriceData{
		CompletionRatio: math.MaxFloat64,
		ModelRatio:      1,
	}, &dto.Usage{PromptTokens: 1, CompletionTokens: 1})

	assert.Nil(t, tieredResult)
	assert.Equal(t, common.MaxQuota, quota)
	require.NotNil(t, info.QuotaClamp)
	assert.Equal(t, common.QuotaClampOverflow, info.QuotaClamp.Kind)
}

func TestSettleTestQuotaRejectsNegativeConfiguredPrice(t *testing.T) {
	quota, tieredResult := settleTestQuota(&relaycommon.RelayInfo{}, types.PriceData{
		ModelPrice: -1,
		UsePrice:   true,
	}, &dto.Usage{PromptTokens: 1})

	assert.Nil(t, tieredResult)
	assert.Zero(t, quota)
}

func TestSettleTestQuotaPreservesFixedPriceTruncation(t *testing.T) {
	quota, tieredResult := settleTestQuota(&relaycommon.RelayInfo{}, types.PriceData{
		ModelPrice: 1.9 / float64(common.QuotaPerUnit),
		UsePrice:   true,
	}, &dto.Usage{PromptTokens: 1})

	assert.Nil(t, tieredResult)
	assert.Equal(t, 1, quota)
}

func TestSettleTestQuotaRejectsInvalidRatio(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	quota, tieredResult := settleTestQuota(info, types.PriceData{
		CompletionRatio: math.NaN(),
		ModelRatio:      1,
	}, &dto.Usage{PromptTokens: 1, CompletionTokens: 1})

	assert.Nil(t, tieredResult)
	assert.Zero(t, quota)
	require.NotNil(t, info.QuotaClamp)
	assert.Equal(t, common.QuotaClampNaN, info.QuotaClamp.Kind)
}
