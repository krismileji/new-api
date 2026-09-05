package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// These names are reserved by at least one supported SQL database. Existing
// legacy tables may still contain some of them, so this guard is limited to
// channel-monitor projection tables introduced for the analytics feature.
var channelMonitorReservedSchemaNames = map[string]struct{}{
	"all": {}, "alter": {}, "and": {}, "as": {}, "asc": {}, "between": {},
	"by": {}, "case": {}, "check": {}, "column": {}, "constraint": {},
	"create": {}, "delete": {}, "desc": {}, "distinct": {}, "drop": {},
	"exists": {}, "false": {}, "from": {}, "full": {}, "group": {},
	"having": {}, "in": {}, "index": {}, "inner": {}, "insert": {},
	"intersect": {}, "into": {}, "is": {}, "join": {}, "key": {},
	"like": {}, "limit": {}, "not": {}, "null": {}, "on": {}, "or": {},
	"order": {}, "outer": {}, "primary": {}, "references": {},
	"select": {}, "table": {}, "then": {}, "true": {}, "union": {},
	"unique": {}, "update": {}, "user": {}, "using": {}, "values": {},
	"when": {}, "where": {}, "with": {}, "window": {},
}

func TestChannelMonitorProjectionColumnsAvoidReservedSchemaNames(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	models := []any{
		&ChannelDailyCostOutbox{},
		&ChannelMonitorEventOutbox{},
		&ChannelTaskCostEvent{},
		&ChannelMonitorMinuteRouteMetric{},
		&ChannelMonitorMinuteAPIKeyMetric{},
		&ChannelMonitorMinuteDurationBucket{},
		&ChannelMonitorDailySuccessLedger{},
		&ChannelMonitorDailySuccessMinute{},
		&ChannelMonitorDailyCostDetail{},
		&ChannelMonitorCostBackfillCheckpoint{},
		&ChannelMonitorCostReconciliation{},
		&ChannelMonitorDirtyMinute{},
		&ChannelMonitorDirtyMinutePending{},
		&ChannelMonitorAggregationState{},
		&ChannelMonitorRedisEffectState{},
	}

	for _, value := range models {
		statement := &gorm.Statement{DB: db}
		require.NoError(t, statement.Parse(value))
		assert.NotContains(t, channelMonitorReservedSchemaNames, strings.ToLower(statement.Schema.Table), statement.Schema.Table)
		for _, field := range statement.Schema.Fields {
			_, reserved := channelMonitorReservedSchemaNames[strings.ToLower(field.DBName)]
			assert.False(t, reserved, "%s.%s uses a reserved SQL identifier", statement.Schema.Table, field.DBName)
		}
	}
}

func TestQuotedMainKeyColumnMatchesSupportedDialects(t *testing.T) {
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() {
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		initCol()
	})

	for _, test := range []struct {
		name   string
		dbType common.DatabaseType
		want   string
	}{
		{name: "mysql", dbType: common.DatabaseTypeMySQL, want: "`key`"},
		{name: "sqlite", dbType: common.DatabaseTypeSQLite, want: "`key`"},
		{name: "postgres", dbType: common.DatabaseTypePostgreSQL, want: `"key"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			common.SetDatabaseTypes(test.dbType, originalLogDatabaseType)
			initCol()
			assert.Equal(t, test.want, QuotedMainKeyColumn())
		})
	}
}

func TestChannelMonitorOptionQueriesQuoteReservedKeyColumn(t *testing.T) {
	originalDB := DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() {
		DB = originalDB
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		initCol()
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DryRun: true})
	require.NoError(t, err)
	DB = db

	for _, test := range []struct {
		name   string
		dbType common.DatabaseType
		quoted string
	}{
		{name: "mysql", dbType: common.DatabaseTypeMySQL, quoted: "`key`"},
		{name: "postgres", dbType: common.DatabaseTypePostgreSQL, quoted: `"key"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			common.SetDatabaseTypes(test.dbType, originalLogDatabaseType)
			initCol()
			statement := DB.Where(QuotedMainKeyColumn()+" IN ?", []string{"test"}).Find(&[]Option{}).Statement
			assert.Contains(t, statement.SQL.String(), "WHERE "+test.quoted+" IN")
		})
	}
}
