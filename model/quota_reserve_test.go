package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clearPendingTokenQuotaForTest() {
	tokenQuotaBatchMutationLock.Lock()
	batchUpdateLocks[BatchUpdateTypeTokenQuota].Lock()
	batchUpdateStores[BatchUpdateTypeTokenQuota] = make(map[int]int)
	batchUpdateLocks[BatchUpdateTypeTokenQuota].Unlock()
	tokenQuotaBatchMutationLock.Unlock()
}

func TestDecreaseTokenQuotaIfEnoughIncludesPendingBatchDelta(t *testing.T) {
	truncateTables(t)
	clearPendingTokenQuotaForTest()

	const (
		tokenID = 901
		userID  = 902
		key     = "sk-token-quota-reserve"
	)
	require.NoError(t, DB.Create(&User{Id: userID, Username: "quota_reserve_user", Quota: 1000}).Error)
	require.NoError(t, DB.Create(&Token{
		Id:          tokenID,
		UserId:      userID,
		Key:         key,
		Name:        "quota_reserve_token",
		Status:      common.TokenStatusEnabled,
		RemainQuota: 100,
	}).Error)

	previousBatchUpdate := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = true
	t.Cleanup(func() {
		clearPendingTokenQuotaForTest()
		common.BatchUpdateEnabled = previousBatchUpdate
	})

	// The queued deduction is not in the database yet, but must be included by
	// an atomic reserve so two concurrent pre-consumes cannot spend it twice.
	require.NoError(t, DecreaseTokenQuota(tokenID, key, 60))
	assert.Equal(t, 100, tokenRemainQuotaForTest(t, tokenID))

	reserved, err := DecreaseTokenQuotaIfEnough(tokenID, key, 50)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, 100, tokenRemainQuotaForTest(t, tokenID))

	reserved, err = DecreaseTokenQuotaIfEnough(tokenID, key, 40)
	require.NoError(t, err)
	assert.True(t, reserved)
	assert.Equal(t, 0, tokenRemainQuotaForTest(t, tokenID))
	assert.Equal(t, 100, tokenUsedQuotaForTest(t, tokenID))
}

func tokenRemainQuotaForTest(t *testing.T, tokenID int) int {
	t.Helper()
	var token Token
	require.NoError(t, DB.Select("remain_quota").Where("id = ?", tokenID).First(&token).Error)
	return token.RemainQuota
}

func tokenUsedQuotaForTest(t *testing.T, tokenID int) int {
	t.Helper()
	var token Token
	require.NoError(t, DB.Select("used_quota").Where("id = ?", tokenID).First(&token).Error)
	return token.UsedQuota
}
