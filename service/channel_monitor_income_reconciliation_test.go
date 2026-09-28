package service

import (
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorIncomeReconcilesTokenUsageAfterReservationAndSettlement(t *testing.T) {
	for _, preConsumed := range []int{0, 1_000_000, 10_500_000} {
		t.Run(strconv.Itoa(preConsumed), func(t *testing.T) {
			truncate(t)
			previousReady := model.ChannelMonitorIncomeReady.Load()
			previousUnit, previousRate := common.QuotaPerUnit, operation_setting.USDExchangeRate
			model.ChannelMonitorIncomeReady.Store(true)
			common.QuotaPerUnit, operation_setting.USDExchangeRate = 500_000, 3
			t.Cleanup(func() {
				model.ChannelMonitorIncomeReady.Store(previousReady)
				common.QuotaPerUnit, operation_setting.USDExchangeRate = previousUnit, previousRate
			})
			require.NoError(t, model.DB.AutoMigrate(&model.ChannelMonitorIncome{}, &model.ChannelDailyCostOutbox{}))
			const requestID = "income-reconciles-final-wallet-deduction"
			key := model.ChannelMonitorIncomeKey(requestID, "request")
			require.NoError(t, model.DB.Where("settlement_key = ?", key).Delete(&model.ChannelMonitorIncome{}).Error)
			t.Cleanup(func() {
				assert.NoError(t, model.DB.Where("settlement_key = ?", key).Delete(&model.ChannelMonitorIncome{}).Error)
			})
			const identity, initialQuota, finalQuota = 901, 20_000_000, 3_500_000
			seedUser(t, identity, initialQuota)
			seedToken(t, identity, identity, "profit-reconciliation", initialQuota)
			seedChannel(t, identity)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{
				RequestId: requestID, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: identity},
				UserId: identity, TokenId: identity, TokenKey: "profit-reconciliation", OriginModelName: "test-model",
				UsingGroup: "default", ForcePreConsume: true, UserSetting: dto.UserSetting{BillingPreference: "wallet_only"},
			}
			require.Nil(t, PreConsumeBilling(ctx, preConsumed, info))
			require.NoError(t, SettleBilling(ctx, info, finalQuota))
			require.NoError(t, SettleBilling(ctx, info, finalQuota), "重复确认不能重复扣费或增加收入")

			var user model.User
			var token model.Token
			var incomes []model.ChannelMonitorIncome
			require.NoError(t, model.DB.First(&user, identity).Error)
			require.NoError(t, model.DB.First(&token, identity).Error)
			require.NoError(t, model.DB.Where("settlement_key = ?", key).Find(&incomes).Error)
			require.Len(t, incomes, 1)
			assert.Equal(t, finalQuota, initialQuota-user.Quota)
			assert.Equal(t, finalQuota, token.UsedQuota)
			assert.Equal(t, int64(token.UsedQuota), incomes[0].Quota)
			assert.Equal(t, "settled", incomes[0].Status)
			assert.Equal(t, float64(7), float64(token.UsedQuota)/common.QuotaPerUnit, "用户侧美元展示")
			assert.Equal(t, "1", incomes[0].USDToCNY)
			assert.Equal(t, int64(7_000_000_000), incomes[0].IncomeNanoCNY, "平台一额度单位按一人民币入账，不乘展示汇率或重复累计预扣费")
		})
	}
}
