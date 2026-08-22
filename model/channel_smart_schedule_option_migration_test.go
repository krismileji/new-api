package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrateChannelSmartScheduleGroupPoliciesAddsMissingStabilityWindows(t *testing.T) {
	db := useFrontendOptionMigrationDB(t)
	raw := `[{
		"group":"vip","strategy":"ratio"
	},{
		"group":"standard","stability_window_minutes":12
	},{
		"group":"null-window","stability_window_minutes":null
	},null]`
	require.NoError(t, db.Create(&Option{
		Key:   ChannelMonitorSmartScheduleGroupPoliciesOption,
		Value: raw,
	}).Error)

	require.NoError(t, MigrateChannelSmartScheduleGroupPolicies())
	value := requireOptionValue(t, db, ChannelMonitorSmartScheduleGroupPoliciesOption)
	var policies []map[string]any
	require.NoError(t, common.UnmarshalJsonStr(value, &policies))
	assert.Equal(t, float64(ChannelMonitorSmartScheduleDefaultStabilityWindowMinutes), policies[0]["stability_window_minutes"])
	assert.Equal(t, float64(12), policies[1]["stability_window_minutes"])
	assert.Equal(t, float64(ChannelMonitorSmartScheduleDefaultStabilityWindowMinutes), policies[2]["stability_window_minutes"])
	assert.Nil(t, policies[3])

	before := value
	require.NoError(t, MigrateChannelSmartScheduleGroupPolicies())
	assert.Equal(t, before, requireOptionValue(t, db, ChannelMonitorSmartScheduleGroupPoliciesOption))
}

func TestMigrateChannelSmartScheduleGroupPoliciesPreservesMalformedJSON(t *testing.T) {
	db := useFrontendOptionMigrationDB(t)
	const raw = `{`
	require.NoError(t, db.Create(&Option{
		Key:   ChannelMonitorSmartScheduleGroupPoliciesOption,
		Value: raw,
	}).Error)

	require.NoError(t, MigrateChannelSmartScheduleGroupPolicies())
	assert.Equal(t, raw, requireOptionValue(t, db, ChannelMonitorSmartScheduleGroupPoliciesOption))
}
