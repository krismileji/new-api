package service

import (
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEstimateRequestTokenForContextLimitUsesSemanticText(t *testing.T) {
	context, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{}

	for _, testCase := range []struct {
		name string
		want int
	}{
		{name: "at limit", want: 272000},
		{name: "over limit", want: 272001},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			meta := &types.TokenCountMeta{
				CombineText: strings.Repeat("a", testCase.want),
				TokenType:   types.TokenTypeTextNumber,
			}

			got, err := EstimateRequestTokenForContextLimit(context, meta, info)
			require.NoError(t, err)
			assert.Equal(t, testCase.want, got)
		})
	}
}
