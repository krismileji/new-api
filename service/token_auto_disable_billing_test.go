package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenProtectionSettlementPreservesConsumedQuotaAndRefundsUnusedReservation(t *testing.T) {
	db, token := setupTokenProtectionTest(t)
	previousBatch := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = false
	t.Cleanup(func() { common.BatchUpdateEnabled = previousBatch })
	require.NoError(t, db.AutoMigrate(&model.User{}))
	require.NoError(t, db.Create(&model.User{Id: token.UserId, Username: "token-protection-billing", Quota: 1000}).Error)
	_, err := SaveTokenAutoDisableSettings(t.Context(), tokenProtectionTestSettings())
	require.NoError(t, err)
	ctx, finish, _ := RegisterTokenProtection(t.Context(), token, "partial-usage")
	defer finish()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil).WithContext(ctx)
	info := &relaycommon.RelayInfo{UserId: token.UserId, TokenId: token.Id, TokenKey: token.Key,
		ForcePreConsume: true, UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
	session, apiErr := NewBillingSession(c, info, 100)
	require.Nil(t, apiErr)
	require.NotNil(t, session)

	ObserveTokenAutoDisableError(ctx, 7, 403, "policy violation")
	require.NotNil(t, TokenAutoDisableFromContext(ctx))
	// The relay reports 30 units already consumed before cancellation. The
	// canceled request's deferred Refund must not undo this completed settlement.
	require.NoError(t, session.Settle(30))
	assert.False(t, session.NeedsRefund())
	session.Refund(c)
	require.NoError(t, session.Settle(30))
	var savedToken model.Token
	require.NoError(t, db.First(&savedToken, token.Id).Error)
	assert.Equal(t, 470, savedToken.RemainQuota)
	assert.Equal(t, 30, savedToken.UsedQuota)
	assert.Equal(t, common.TokenStatusDisabled, savedToken.Status)
	var user model.User
	require.NoError(t, db.First(&user, token.UserId).Error)
	assert.Equal(t, 970, user.Quota)
}
