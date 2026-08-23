package model

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupLogicalGroupPhysicalDataTestDB deliberately migrates only the tables
// used by this regression. It keeps the assertions independent from the
// application's production database and makes the physical channel boundary
// explicit in every query.
func setupLogicalGroupPhysicalDataTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := DB
	previousMainDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "logical-group-physical-data.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&Channel{},
		&ChannelLogicalGroup{},
		&ChannelLogicalGroupMember{},
		&ChannelRatioMonitor{},
		&ChannelDailyCost{},
		&ChannelDailyAPIKeyCost{},
	))
	DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, previousLogDatabaseType)
	t.Cleanup(func() {
		DB = previousDB
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			assert.NoError(t, sqlDB.Close())
		}
	})
	return db
}

func TestLogicalGroupKeepsPhysicalRatioBalanceConcurrencyAndCostIndependent(t *testing.T) {
	db := setupLogicalGroupPhysicalDataTestDB(t)
	const (
		logicalGroupID int64 = 9900
		channelAID           = 9901
		channelBID           = 9902
	)
	fingerprint := strings.Repeat("a", 64)
	group := &ChannelLogicalGroup{Id: logicalGroupID, Name: "physical-data-isolation"}
	require.NoError(t, db.Create(group).Error)
	require.NoError(t, db.Create(&Channel{Id: channelAID, Name: "key-a", Key: "upstream-a", LogicalChannelID: &group.Id}).Error)
	require.NoError(t, db.Create(&Channel{Id: channelBID, Name: "key-b", Key: "upstream-b", LogicalChannelID: &group.Id}).Error)
	require.NoError(t, db.Create(&ChannelLogicalGroupMember{LogicalGroupID: logicalGroupID, ChannelID: channelAID, Weight: 3, AddressFingerprint: fingerprint}).Error)
	require.NoError(t, db.Create(&ChannelLogicalGroupMember{LogicalGroupID: logicalGroupID, ChannelID: channelBID, Weight: 1, AddressFingerprint: fingerprint}).Error)

	balanceA := 11.25
	balanceB := 22.5
	require.NoError(t, db.Create(&ChannelRatioMonitor{
		ChannelId: channelAID, Ratio: 0.5, UpstreamBalance: &balanceA, ConcurrencyLimit: 2, ConcurrencyRevision: 7,
	}).Error)
	require.NoError(t, db.Create(&ChannelRatioMonitor{
		ChannelId: channelBID, Ratio: 1.5, UpstreamBalance: &balanceB, ConcurrencyLimit: 5, ConcurrencyRevision: 9,
	}).Error)

	monitors, err := GetChannelRatioMonitors()
	require.NoError(t, err)
	require.Len(t, monitors, 2, "grouping must not create a logical-group ratio row")
	byChannel := make(map[int]ChannelRatioMonitor, len(monitors))
	for _, monitor := range monitors {
		byChannel[monitor.ChannelId] = monitor
	}
	assert.InDelta(t, 0.5, byChannel[channelAID].Ratio, 0.0001)
	assert.InDelta(t, 1.5, byChannel[channelBID].Ratio, 0.0001)
	require.NotNil(t, byChannel[channelAID].UpstreamBalance)
	require.NotNil(t, byChannel[channelBID].UpstreamBalance)
	assert.InDelta(t, balanceA, *byChannel[channelAID].UpstreamBalance, 0.0001)
	assert.InDelta(t, balanceB, *byChannel[channelBID].UpstreamBalance, 0.0001)

	concurrency, err := GetChannelConcurrencyConfigs()
	require.NoError(t, err)
	assert.Equal(t, ChannelConcurrencyConfig{Limit: 2, Revision: 7}, concurrency[channelAID])
	assert.Equal(t, ChannelConcurrencyConfig{Limit: 5, Revision: 9}, concurrency[channelBID])
	assert.NotContains(t, concurrency, int(logicalGroupID), "concurrency remains per physical key")

	occurredAt := int64(1_700_000_000)
	dayStart := ChannelDailyCostDayStart(occurredAt)
	require.NoError(t, AddChannelDailyCost(context.Background(), channelAID, occurredAt, 100, 1, 0))
	require.NoError(t, AddChannelDailyCost(context.Background(), channelBID, occurredAt, 250, 1, 0))
	costs, err := GetChannelDailyCosts(context.Background(), dayStart, dayStart+24*60*60)
	require.NoError(t, err)
	require.Len(t, costs, 2, "daily cost rows stay keyed by physical channel")
	assert.Equal(t, channelAID, costs[0].ChannelId)
	assert.Equal(t, channelBID, costs[1].ChannelId)
	var logicalCostRows int64
	require.NoError(t, db.Model(&ChannelDailyCost{}).Where("channel_id = ?", logicalGroupID).Count(&logicalCostRows).Error)
	assert.Zero(t, logicalCostRows, "no logical-group cost row may be generated")

	costA, err := GetChannelDailyCostsForChannel(context.Background(), dayStart, dayStart+24*60*60, channelAID)
	require.NoError(t, err)
	require.Len(t, costA, 1)
	assert.EqualValues(t, 100, costA[0].CostNanoCNY)
	costB, err := GetChannelDailyCostsForChannel(context.Background(), dayStart, dayStart+24*60*60, channelBID)
	require.NoError(t, err)
	require.Len(t, costB, 1)
	assert.EqualValues(t, 250, costB[0].CostNanoCNY)
}

func TestLogicalGroupDoesNotAddLogicalDimensionsToOrdinaryModels(t *testing.T) {
	ordinary := []struct {
		name        string
		reflectType reflect.Type
	}{
		{name: "ratio monitor", reflectType: reflect.TypeOf(ChannelRatioMonitor{})},
		{name: "ratio history", reflectType: reflect.TypeOf(ChannelRatioHistory{})},
		{name: "daily cost", reflectType: reflect.TypeOf(ChannelDailyCost{})},
		{name: "daily API key cost", reflectType: reflect.TypeOf(ChannelDailyAPIKeyCost{})},
		{name: "request log", reflectType: reflect.TypeOf(Log{})},
	}
	for _, item := range ordinary {
		t.Run(item.name, func(t *testing.T) {
			_, hasLogicalID := item.reflectType.FieldByName("LogicalChannelID")
			if !hasLogicalID {
				_, hasLogicalID = item.reflectType.FieldByName("LogicalChannelId")
			}
			assert.False(t, hasLogicalID, "ordinary records must not gain a logical-group aggregation dimension")
			_, hasPhysicalID := item.reflectType.FieldByName("ChannelId")
			if !hasPhysicalID {
				_, hasPhysicalID = item.reflectType.FieldByName("ChannelID")
			}
			assert.True(t, hasPhysicalID, "ordinary records remain addressable by physical channel_id")
		})
	}
}

func TestGroupedChannelJSONDoesNotChangeOrdinaryChannelResponseShape(t *testing.T) {
	logicalID := int64(9910)
	payload, err := common.Marshal(&Channel{Id: 9911, Name: "grouped-key", Key: "secret", LogicalChannelID: &logicalID})
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, common.Unmarshal(payload, &decoded))
	_, exposed := decoded["logical_channel_id"]
	assert.False(t, exposed, "existing channel APIs must not expose logical grouping internals")
}
