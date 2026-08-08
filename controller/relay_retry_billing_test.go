package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type relayRetryBillingSettler struct {
	preConsumedQuota int
	reserveTargets   []int
}

func (*relayRetryBillingSettler) Settle(int) error { return nil }

func (*relayRetryBillingSettler) Refund(*gin.Context) {}

func (*relayRetryBillingSettler) NeedsRefund() bool { return false }

func (s *relayRetryBillingSettler) GetPreConsumedQuota() int { return s.preConsumedQuota }

func (s *relayRetryBillingSettler) Reserve(targetQuota int) error {
	s.reserveTargets = append(s.reserveTargets, targetQuota)
	if targetQuota > s.preConsumedQuota {
		s.preConsumedQuota = targetQuota
	}
	return nil
}

func TestPrepareRelayBillingRaisesReservationForMoreExpensiveRetryGroup(t *testing.T) {
	originalModelPrices := ratio_setting.ModelPrice2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(originalModelPrices))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"retry-billing-model":0.001}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"cheap":1,"expensive":3}`))

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyAutoGroup, "expensive")
	billing := &relayRetryBillingSettler{preConsumedQuota: 500}
	info := &relaycommon.RelayInfo{
		OriginModelName: "retry-billing-model",
		UserGroup:       "billing-test-user",
		UsingGroup:      "cheap",
		Billing:         billing,
	}
	info.PriceData.GroupRatioInfo.GroupRatio = 1
	info.PriceData.QuotaToPreConsume = 500

	apiErr := prepareRelayBillingForSelectedGroup(c, info, 0, &types.TokenCountMeta{})

	require.Nil(t, apiErr)
	assert.Equal(t, "expensive", info.UsingGroup)
	assert.Equal(t, 3.0, info.PriceData.GroupRatioInfo.GroupRatio)
	assert.Equal(t, 1500, info.PriceData.QuotaToPreConsume)
	assert.Equal(t, []int{1500}, billing.reserveTargets)
	assert.Equal(t, 1500, info.FinalPreConsumedQuota)
}
