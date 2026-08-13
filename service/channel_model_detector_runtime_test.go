package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelModelDetectorRuntimeSharesTokenStore(t *testing.T) {
	first, err := GetChannelModelDetectorTokenStore()
	require.NoError(t, err)
	require.NotNil(t, first)

	second, err := GetChannelModelDetectorTokenStore()
	require.NoError(t, err)
	assert.Same(t, first, second)
}
