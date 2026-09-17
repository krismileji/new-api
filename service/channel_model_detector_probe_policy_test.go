package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelProbePolicyModelDetectorDoesNotReserveSkippedAttempt(t *testing.T) {
	db := setupChannelModelDetectionSchedulerTestDB(t)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	require.NoError(t, db.Create(&model.Channel{Id: 23, Name: "policy", Key: "test"}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{ChannelId: 23, AutoProbeDisabled: true}).Error)
	now := time.Unix(1_800_000_000, 0)
	store := newTestChannelModelDetectorTokenStore(t, &now)
	credential, err := store.Issue(ChannelModelDetectorTokenSpec{
		Trigger: model.ChannelStatusProbeTriggerScheduled, RunID: "policy", TargetID: 11, ExecutionID: 1011, ChannelID: 23,
		RequestModel: "channel-alias", ClaimedModel: model.ChannelModelDetectionClaimedModelSol, Preset: model.ChannelModelDetectionPresetLow,
		RelayBaseURL: "http://127.0.0.1:3000/internal/model-detector", MaxHTTPAttempts: 1, ExpiresAt: now.Add(time.Hour).Unix(),
	})
	require.NoError(t, err)
	executor := &channelModelDetectorRelayExecutorStub{}
	relay, err := newChannelModelDetectorRelay(store, executor, func(context.Context, int) (channelModelDetectorConcurrencyLease, bool, ChannelConcurrencyStatus, error) {
		t.Fatal("skipped probe acquired concurrency")
		return nil, false, ChannelConcurrencyStatus{}, nil
	})
	require.NoError(t, err)
	_, err = relay.Execute(t.Context(), ChannelModelDetectorRelayRequest{BearerToken: credential.BearerToken(), DetectorRequestID: "skipped", Body: []byte(`{"model":"channel-alias","input":"hello"}`)})
	require.ErrorIs(t, err, ErrChannelAutoProbeDisabled)
	assert.Empty(t, executor.executions)
	authorization, err := store.AuthorizeAttempt(credential.BearerToken(), "channel-alias", "still-available")
	require.NoError(t, err)
	assert.Equal(t, 1, authorization.AttemptNo)
}
