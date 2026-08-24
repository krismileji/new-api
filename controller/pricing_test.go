package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterPricingByUsableGroupsRedactsNonSelectableGroups(t *testing.T) {
	pricing := []model.Pricing{
		{ModelName: "shared-model", EnableGroup: []string{"public", "private"}},
		{ModelName: "private-model", EnableGroup: []string{"private"}},
		{ModelName: "wildcard-model", EnableGroup: []string{"all", "private"}},
	}

	filtered := filterPricingByUsableGroups(pricing, map[string]string{"public": "Public"})

	require.Len(t, filtered, 2)
	assert.Equal(t, "shared-model", filtered[0].ModelName)
	assert.Equal(t, []string{"public"}, filtered[0].EnableGroup)
	assert.Equal(t, "wildcard-model", filtered[1].ModelName)
	assert.Equal(t, []string{"all"}, filtered[1].EnableGroup)
}

func TestFilterPricingByUsableGroupsReturnsEmptyWhenNoneAreSelectable(t *testing.T) {
	pricing := []model.Pricing{{ModelName: "private-model", EnableGroup: []string{"private"}}}

	assert.Empty(t, filterPricingByUsableGroups(pricing, map[string]string{"public": "Public"}))
}
