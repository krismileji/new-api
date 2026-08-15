package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateOptionMapFromDatabaseReloadsLatestChannelMonitorValue(t *testing.T) {
	db := setupChannelMonitorOptionTestDB(t, `{"vip":1}`, `{}`)
	const key = ChannelMonitorSmartSchedulePerformanceWindowOption
	require.NoError(t, db.Create(&Option{Key: key, Value: "60"}).Error)
	require.NoError(t, db.Model(&Option{}).Where(&Option{Key: key}).Update("value", "120").Error)

	require.NoError(t, updateOptionMapFromDatabase(key, "60"))
	common.OptionMapRWMutex.RLock()
	value := common.OptionMap[key]
	common.OptionMapRWMutex.RUnlock()
	assert.Equal(t, "120", value)
}

func TestUpdateOptionMapFromDatabaseReloadsLatestGroupRatio(t *testing.T) {
	db := setupChannelMonitorOptionTestDB(t, `{"vip":1}`, `{}`)
	require.NoError(t, db.Model(&Option{}).
		Where(&Option{Key: "GroupRatio"}).
		Update("value", `{"vip":2}`).Error)

	require.NoError(t, updateOptionMapFromDatabase("GroupRatio", `{"vip":1}`))
	assert.Equal(t, 2.0, ratio_setting.GetGroupRatio("vip"))
	common.OptionMapRWMutex.RLock()
	value := common.OptionMap["GroupRatio"]
	common.OptionMapRWMutex.RUnlock()
	assert.JSONEq(t, `{"vip":2}`, value)
}
