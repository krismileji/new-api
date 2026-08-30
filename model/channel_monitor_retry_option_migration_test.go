package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigrateRetiredChannelMonitorRetryOptionsRemovesLegacyRows(t *testing.T) {
	db := useFrontendOptionMigrationDB(t)
	legacy := []Option{
		{Key: "ChannelMonitorRetrySkipErrorCodes", Value: "429\n500"},
		{Key: "ChannelMonitorRetrySkipErrorMessages", Value: "quota exceeded"},
		{Key: "ChannelMonitorErrorMessageWhitelist", Value: "429"},
	}
	require.NoError(t, db.Create(&legacy).Error)

	require.NoError(t, MigrateRetiredChannelMonitorRetryOptions())

	for _, key := range retiredChannelMonitorRetryOptionKeys {
		requireOptionMissing(t, db, key)
	}
	require.Equal(t, "429", requireOptionValue(t, db, "ChannelMonitorErrorMessageWhitelist"))
	require.NoError(t, MigrateRetiredChannelMonitorRetryOptions())
}
