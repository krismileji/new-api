package controller

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesWSSmartRoutingRequestSize(t *testing.T) {
	for _, cached := range []bool{false, true} {
		for _, kind := range []string{"exploration", "stability-release"} {
			for _, scenario := range []string{"small", "large", "only-limited"} {
				t.Run(fmt.Sprintf("cache=%t/%s/%s", cached, kind, scenario), func(t *testing.T) {
					var calls atomic.Int32
					fixture := newResponsesWSAdmissionTest(t, &calls)
					limited := addResponsesWSAdmissionChannel(t, fixture.channel, "limited", 200, true)
					require.NoError(t, model.DB.AutoMigrate(&model.ChannelSmartScheduleRouteState{}))
					state := &model.ChannelSmartScheduleRouteState{ChannelId: limited.Id, GroupName: "default", ModelName: "ws-billing", ParticipationSet: true}
					if kind == "exploration" {
						state.TemporaryTrafficKind = model.ChannelSmartScheduleTemporaryTrafficExploration
						state.ExplorationMaxPromptTokens = 100
					} else {
						state.StabilityState = model.ChannelSmartScheduleStabilityProbing
						state.StabilityReleaseMaxPromptTokens = 100
					}
					require.NoError(t, model.DB.Create(state).Error)
					t.Cleanup(func() { require.NoError(t, model.DB.Delete(state).Error) })
					if scenario == "only-limited" {
						require.NoError(t, model.DB.Where("channel_id = ?", fixture.channel.Id).Delete(&model.Ability{}).Error)
					}
					common.MemoryCacheEnabled = cached
					model.InitChannelCache()
					t.Cleanup(func() { fixture.closeAndWait(t) })
					expected := limited.Id
					if scenario == "large" {
						expected = fixture.channel.Id
					}
					for turn := 0; turn < 2; turn++ {
						input := "hi"
						if (scenario != "small") == (turn == 0) {
							input = strings.Repeat("a", 1000)
						}
						payload, err := common.Marshal(map[string]any{"type": "response.create", "model": "ws-billing", "input": input})
						require.NoError(t, err)
						require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, payload))
						require.Equal(t, "response.completed", readResponsesWSTestEvent(t, fixture.client)["type"])
						var logs []model.Log
						require.NoError(t, model.LOG_DB.Where("token_id = ? AND type = ?", fixture.token.Id, model.LogTypeConsume).Order("id").Find(&logs).Error)
						require.Len(t, logs, turn+1)
						assert.Equal(t, expected, logs[turn].ChannelId, "initial selection observes size; subsequent creates keep the established upstream connection")
					}
					fixture.closeAndWait(t)
					assert.Equal(t, int32(1), fixture.connections.Load())
					assertResponsesWSAccounting(t, fixture, []int{20, 20})
				})
			}
		}
	}
}
