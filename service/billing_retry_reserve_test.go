package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBillingSessionReserveRejectsWalletArrearsForForcedPreConsume(t *testing.T) {
	truncate(t)
	const userID = 702
	seedUser(t, userID, 20_000)

	info := &relaycommon.RelayInfo{UserId: userID, IsPlayground: true, ForcePreConsume: true}
	session := &BillingSession{
		relayInfo:        info,
		funding:          &WalletFunding{userId: userID, consumed: 50_000},
		preConsumedQuota: 50_000,
	}
	err := session.Reserve(100_000)
	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	assert.Equal(t, 50_000, session.GetPreConsumedQuota())
	userQuota, queryErr := model.GetUserQuota(userID, false)
	require.NoError(t, queryErr)
	assert.Equal(t, 20_000, userQuota)
}

func TestForcedWalletReserveUsesAtomicDatabaseBalanceWithBatchUpdates(t *testing.T) {
	truncate(t)
	const userID = 703
	seedUser(t, userID, 100)

	previousBatchUpdate := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = true
	t.Cleanup(func() {
		common.BatchUpdateEnabled = previousBatchUpdate
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId:          userID,
		IsPlayground:    true,
		ForcePreConsume: true,
		UserSetting: dto.UserSetting{
			BillingPreference: "wallet_only",
		},
	}
	session, apiErr := NewBillingSession(c, info, 80)
	require.Nil(t, apiErr)
	require.NotNil(t, session)

	userQuota, err := model.GetUserQuota(userID, true)
	require.NoError(t, err)
	assert.Equal(t, 20, userQuota)

	require.NoError(t, session.Reserve(90))
	userQuota, err = model.GetUserQuota(userID, true)
	require.NoError(t, err)
	assert.Equal(t, 10, userQuota)

	err = session.Reserve(110)
	require.Error(t, err)
	var reserveErr *types.NewAPIError
	require.ErrorAs(t, err, &reserveErr)
	assert.Equal(t, types.ErrorCodeInsufficientUserQuota, reserveErr.GetErrorCode())
	assert.Equal(t, 90, session.GetPreConsumedQuota())
	userQuota, queryErr := model.GetUserQuota(userID, true)
	require.NoError(t, queryErr)
	assert.Equal(t, 10, userQuota)
}

func TestWalletPreConsumeUsesAtomicDatabaseBalanceWithBatchUpdates(t *testing.T) {
	truncate(t)
	const userID = 705
	seedUser(t, userID, 100)

	previousBatchUpdate := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = true
	t.Cleanup(func() {
		common.BatchUpdateEnabled = previousBatchUpdate
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId:       userID,
		IsPlayground: true,
		UserSetting: dto.UserSetting{
			BillingPreference: "wallet_only",
		},
	}
	session := &BillingSession{
		relayInfo: info,
		funding:   &WalletFunding{userId: userID},
	}
	apiErr := session.preConsume(c, 110)
	var reserveErr *types.NewAPIError
	require.ErrorAs(t, apiErr, &reserveErr)
	assert.Equal(t, types.ErrorCodeInsufficientUserQuota, reserveErr.GetErrorCode())
	assert.Equal(t, 0, session.GetPreConsumedQuota())
	userQuota, err := model.GetUserQuota(userID, true)
	require.NoError(t, err)
	assert.Equal(t, 100, userQuota)
}

func TestForcedWalletPreConsumeIncludesPendingBatchDeductions(t *testing.T) {
	truncate(t)
	const userID = 704
	seedUser(t, userID, 100)

	previousBatchUpdate := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = true
	t.Cleanup(func() {
		common.BatchUpdateEnabled = previousBatchUpdate
	})
	require.NoError(t, model.DecreaseUserQuota(userID, 60, false))

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId:          userID,
		IsPlayground:    true,
		ForcePreConsume: true,
		UserSetting: dto.UserSetting{
			BillingPreference: "wallet_only",
		},
	}
	session, apiErr := NewBillingSession(c, info, 50)

	assert.Nil(t, session)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
	userQuota, err := model.GetUserQuota(userID, true)
	require.NoError(t, err)
	assert.Equal(t, 100, userQuota)
}
