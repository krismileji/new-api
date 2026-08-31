package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm/schema"
)

func TestUserQuotaFieldsUseBigIntForSupportedDialects(t *testing.T) {
	parsed, err := schema.Parse(&User{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)

	for _, fieldName := range userQuotaColumns {
		field := parsed.LookUpField(fieldName)
		require.NotNil(t, field, fieldName)
		assert.Equal(t, "bigint", mysql.Dialector{}.DataTypeOf(field), fieldName)
		assert.Equal(t, "bigint", postgres.Dialector{}.DataTypeOf(field), fieldName)
	}
}

func TestLegacyUserQuotaSchemaTypesAreRecognized(t *testing.T) {
	legacyTypes := map[common.DatabaseType][]string{
		common.DatabaseTypeMySQL:      {"tinyint", "smallint", "mediumint", "int", "integer", "int unsigned"},
		common.DatabaseTypePostgreSQL: {"smallint", "int2", "integer", "int4"},
	}

	for dbType, types := range legacyTypes {
		for _, dataType := range types {
			assert.True(t, isLegacyUserQuotaIntegerType(dbType, dataType), "%s/%s", dbType, dataType)
		}
	}
	assert.False(t, isLegacyUserQuotaIntegerType(common.DatabaseTypeMySQL, "bigint"))
	assert.False(t, isLegacyUserQuotaIntegerType(common.DatabaseTypePostgreSQL, "text"))
}
