package setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupOrderRoundTripAndSortsUnconfiguredGroups(t *testing.T) {
	original := GroupOrder2JsonString()
	t.Cleanup(func() { require.NoError(t, UpdateGroupOrderByJsonString(original)) })

	require.NoError(t, UpdateGroupOrderByJsonString(`["vip","default"]`))
	assert.Equal(t, []string{"vip", "default", "other"}, SortGroupNames([]string{"other", "default", "vip"}))
	assert.Equal(t, `["vip","default"]`, GroupOrder2JsonString())
}

func TestUpdateGroupOrderRejectsDuplicateOrEmptyNames(t *testing.T) {
	original := GroupOrder2JsonString()
	t.Cleanup(func() { require.NoError(t, UpdateGroupOrderByJsonString(original)) })

	assert.Error(t, UpdateGroupOrderByJsonString(`["vip","vip"]`))
	assert.Error(t, UpdateGroupOrderByJsonString(`["default",""]`))
	assert.Equal(t, original, GroupOrder2JsonString())
}
