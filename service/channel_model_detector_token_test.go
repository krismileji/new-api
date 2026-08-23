package service

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelModelDetectorTokenBindsIdentityAndHidesBearer(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := newTestChannelModelDetectorTokenStore(t, &now)
	credential, err := store.Issue(ChannelModelDetectorTokenSpec{
		RunID:           "run-1",
		TargetID:        11,
		ExecutionID:     1011,
		ChannelID:       23,
		RequestModel:    "channel-alias",
		ClaimedModel:    model.ChannelModelDetectionClaimedModelSol,
		Preset:          model.ChannelModelDetectionPresetMedium,
		RelayBaseURL:    "https://new-api.internal/internal/model-detector",
		MaxHTTPAttempts: 2,
		ExpiresAt:       now.Add(time.Hour).Unix(),
	})
	require.NoError(t, err)
	require.NotEmpty(t, credential.BearerToken())
	assert.NotContains(t, credential.BearerToken(), "run-1")
	assert.NotContains(t, credential.BearerToken(), "channel-alias")
	assert.NotContains(t, credential.BearerToken(), model.ChannelModelDetectionClaimedModelSol)
	assert.NotContains(t, fmt.Sprint(credential), credential.BearerToken())
	assert.NotContains(t, fmt.Sprintf("%#v", credential), credential.BearerToken())
	assert.NotContains(t, fmt.Sprint(credential.Claims), credential.Claims.Nonce)
	assert.NotContains(t, fmt.Sprintf("%#v", credential.Claims), credential.Claims.Nonce)

	encoded, err := common.Marshal(credential)
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(encoded))
	assert.NotContains(t, string(encoded), credential.BearerToken())
	claimsJSON, err := common.Marshal(credential.Claims)
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(claimsJSON))

	authorized, err := store.AuthorizeAttempt(credential.BearerToken(), "channel-alias", "detector-request-1")
	require.NoError(t, err)
	assert.Equal(t, "run-1", authorized.Claims.RunID)
	assert.EqualValues(t, 11, authorized.Claims.TargetID)
	assert.Equal(t, 23, authorized.Claims.ChannelID)
	assert.Equal(t, "channel-alias", authorized.Claims.RequestModel)
	assert.Equal(t, model.ChannelModelDetectionClaimedModelSol, authorized.Claims.ClaimedModel)
	assert.Equal(t, model.ChannelModelDetectionPresetMedium, authorized.Claims.Preset)
	assert.Equal(t, "https://new-api.internal/internal/model-detector", authorized.Claims.RelayBaseURL)
	assert.Equal(t, 1, authorized.AttemptNo)
	assert.Equal(t, 1, authorized.RemainingAttempts)
	assert.False(t, authorized.Replay)
}

func TestChannelModelDetectorTokenRejectsTamperingModelMismatchAndCrossStoreUse(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := newTestChannelModelDetectorTokenStore(t, &now)
	credential := issueTestChannelModelDetectorCredential(t, store, now, 2)

	_, err := store.AuthorizeAttempt(credential.BearerToken(), model.ChannelModelDetectionClaimedModelTerra, "wrong-model")
	assert.ErrorIs(t, err, ErrChannelModelDetectorTokenModelMismatch)

	tampered := credential.BearerToken()[:len(credential.BearerToken())-1] + "A"
	if tampered == credential.BearerToken() {
		tampered = credential.BearerToken()[:len(credential.BearerToken())-1] + "B"
	}
	_, err = store.AuthorizeAttempt(tampered, model.ChannelModelDetectionClaimedModelSol, "tampered")
	assert.ErrorIs(t, err, ErrChannelModelDetectorTokenInvalid)

	otherStore, err := newChannelModelDetectorTokenStore([]byte(strings.Repeat("b", 32)), func() time.Time { return now })
	require.NoError(t, err)
	_, err = otherStore.AuthorizeAttempt(credential.BearerToken(), model.ChannelModelDetectionClaimedModelSol, "cross-store")
	assert.ErrorIs(t, err, ErrChannelModelDetectorTokenInvalid)

	authorized, err := store.AuthorizeAttempt(credential.BearerToken(), model.ChannelModelDetectionClaimedModelSol, "valid-after-mismatch")
	require.NoError(t, err)
	assert.Equal(t, 1, authorized.AttemptNo, "型号不匹配不得消耗尝试预算")
}

func TestChannelModelDetectorTokenAttemptReplayAndBudgetAreAtomic(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := newTestChannelModelDetectorTokenStore(t, &now)
	credential := issueTestChannelModelDetectorCredential(t, store, now, 4)

	first, err := store.AuthorizeAttempt(credential.BearerToken(), model.ChannelModelDetectionClaimedModelSol, "same-request")
	require.NoError(t, err)
	replay, err := store.AuthorizeAttempt(credential.BearerToken(), model.ChannelModelDetectionClaimedModelSol, "same-request")
	require.NoError(t, err)
	assert.True(t, replay.Replay)
	assert.Equal(t, first.AttemptNo, replay.AttemptNo)
	assert.Equal(t, first.RemainingAttempts, replay.RemainingAttempts)

	const callers = 12
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	exhausted := 0
	attemptNumbers := make(map[int]struct{})
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			result, authorizeErr := store.AuthorizeAttempt(
				credential.BearerToken(),
				model.ChannelModelDetectionClaimedModelSol,
				fmt.Sprintf("request-%d", index),
			)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case authorizeErr == nil:
				successes++
				attemptNumbers[result.AttemptNo] = struct{}{}
			case errors.Is(authorizeErr, ErrChannelModelDetectorTokenBudgetExceeded):
				exhausted++
			default:
				assert.NoError(t, authorizeErr)
			}
		}(i)
	}
	wg.Wait()

	assert.Equal(t, 3, successes)
	assert.Equal(t, callers-3, exhausted)
	assert.Equal(t, map[int]struct{}{2: {}, 3: {}, 4: {}}, attemptNumbers)
}

func TestChannelModelDetectorTokenExpiryAndRevocation(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := newTestChannelModelDetectorTokenStore(t, &now)
	expiring := issueTestChannelModelDetectorCredential(t, store, now, 2)
	now = now.Add(2 * time.Hour)
	_, err := store.AuthorizeAttempt(expiring.BearerToken(), model.ChannelModelDetectionClaimedModelSol, "expired")
	assert.ErrorIs(t, err, ErrChannelModelDetectorTokenExpired)

	now = time.Unix(1_800_000_000, 0).UTC()
	revoked := issueTestChannelModelDetectorCredential(t, store, now, 2)
	require.NoError(t, store.Revoke(revoked.BearerToken()))
	_, err = store.AuthorizeAttempt(revoked.BearerToken(), model.ChannelModelDetectionClaimedModelSol, "revoked")
	assert.ErrorIs(t, err, ErrChannelModelDetectorTokenRevoked)

	first := issueTestChannelModelDetectorCredentialForTarget(t, store, now, "run-1", 10)
	second := issueTestChannelModelDetectorCredentialForTarget(t, store, now, "run-1", 20)
	assert.Equal(t, 1, store.RevokeRunTarget("run-1", 10))
	_, err = store.AuthorizeAttempt(first.BearerToken(), model.ChannelModelDetectionClaimedModelSol, "first")
	assert.ErrorIs(t, err, ErrChannelModelDetectorTokenRevoked)
	_, err = store.AuthorizeAttempt(second.BearerToken(), model.ChannelModelDetectionClaimedModelSol, "second")
	assert.NoError(t, err)
}

func TestChannelModelDetectorTokenValidatesDedicatedKeyTTLAndRelayURL(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	_, err := newChannelModelDetectorTokenStore([]byte("short"), func() time.Time { return now })
	assert.Error(t, err)

	store := newTestChannelModelDetectorTokenStore(t, &now)
	spec := ChannelModelDetectorTokenSpec{
		RunID:           "run-1",
		TargetID:        1,
		ExecutionID:     1001,
		ChannelID:       2,
		RequestModel:    "model-a",
		ClaimedModel:    model.ChannelModelDetectionClaimedModelSol,
		Preset:          model.ChannelModelDetectionPresetLow,
		RelayBaseURL:    "https://user:password@internal.example/v1?channel=2",
		MaxHTTPAttempts: 1,
		ExpiresAt:       now.Add(time.Hour).Unix(),
	}
	_, err = store.Issue(spec)
	assert.Error(t, err)

	spec.RelayBaseURL = "https://internal.example/v1/"
	spec.ExpiresAt = now.Add(ChannelModelDetectorTokenMaxTTL + time.Second).Unix()
	_, err = store.Issue(spec)
	assert.Error(t, err)

	spec.ExpiresAt = now.Add(time.Hour).Unix()
	credential, err := store.Issue(spec)
	require.NoError(t, err)
	assert.Equal(t, "https://internal.example/v1", credential.Claims.RelayBaseURL)

	firstStore, err := NewChannelModelDetectorTokenStore()
	require.NoError(t, err)
	secondStore, err := NewChannelModelDetectorTokenStore()
	require.NoError(t, err)
	assert.NotEqual(t, firstStore.signingKey, secondStore.signingKey)
}

func TestChannelModelDetectorTokenValidatesAndFreezesLogicalMembers(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := newTestChannelModelDetectorTokenStore(t, &now)
	spec := ChannelModelDetectorTokenSpec{
		RunID: "logical-run", TargetID: 11, ExecutionID: 1011, ChannelID: 23,
		LogicalChannelID: 99, LogicalRevision: 3, RequestModel: "channel-alias",
		ClaimedModel: model.ChannelModelDetectionClaimedModelSol, Preset: model.ChannelModelDetectionPresetLow,
		RelayBaseURL: "http://127.0.0.1:3000/internal/model-detector", MaxHTTPAttempts: 2, ExpiresAt: now.Add(time.Hour).Unix(),
	}

	_, err := store.Issue(spec)
	assert.Error(t, err)

	spec.LogicalMembers = []model.ChannelModelDetectionMemberSnapshot{{ChannelID: 23, Weight: 1}, {ChannelID: 23, Weight: 2}}
	_, err = store.Issue(spec)
	assert.Error(t, err)

	spec.LogicalMembers = []model.ChannelModelDetectionMemberSnapshot{{ChannelID: 23, Weight: 1}, {ChannelID: 24, Weight: 2}}
	credential, err := store.Issue(spec)
	require.NoError(t, err)
	spec.LogicalMembers[0].ChannelID = 999
	assert.Equal(t, []model.ChannelModelDetectionMemberSnapshot{{ChannelID: 23, Weight: 1}, {ChannelID: 24, Weight: 2}}, credential.Claims.LogicalMembers)
	credential.Claims.LogicalMembers[0].ChannelID = 998
	authorization, err := store.AuthorizeAttempt(credential.BearerToken(), credential.Claims.RequestModel, "frozen-members")
	require.NoError(t, err)
	assert.Equal(t, []model.ChannelModelDetectionMemberSnapshot{{ChannelID: 23, Weight: 1}, {ChannelID: 24, Weight: 2}}, authorization.Claims.LogicalMembers)
}

func newTestChannelModelDetectorTokenStore(t *testing.T, now *time.Time) *ChannelModelDetectorTokenStore {
	t.Helper()
	store, err := newChannelModelDetectorTokenStore([]byte(strings.Repeat("a", 32)), func() time.Time { return *now })
	require.NoError(t, err)
	return store
}

func issueTestChannelModelDetectorCredential(t *testing.T, store *ChannelModelDetectorTokenStore, now time.Time, attempts int) ChannelModelDetectorCredential {
	t.Helper()
	return issueTestChannelModelDetectorCredentialForTargetWithAttempts(t, store, now, "run-1", 11, attempts)
}

func issueTestChannelModelDetectorCredentialForTarget(t *testing.T, store *ChannelModelDetectorTokenStore, now time.Time, runID string, targetID int64) ChannelModelDetectorCredential {
	t.Helper()
	return issueTestChannelModelDetectorCredentialForTargetWithAttempts(t, store, now, runID, targetID, 2)
}

func issueTestChannelModelDetectorCredentialForTargetWithAttempts(t *testing.T, store *ChannelModelDetectorTokenStore, now time.Time, runID string, targetID int64, attempts int) ChannelModelDetectorCredential {
	t.Helper()
	credential, err := store.Issue(ChannelModelDetectorTokenSpec{
		RunID:           runID,
		TargetID:        targetID,
		ExecutionID:     targetID + 1000,
		ChannelID:       23,
		RequestModel:    "channel-alias",
		ClaimedModel:    model.ChannelModelDetectionClaimedModelSol,
		Preset:          model.ChannelModelDetectionPresetLow,
		RelayBaseURL:    "http://127.0.0.1:3000/internal/model-detector",
		MaxHTTPAttempts: attempts,
		ExpiresAt:       now.Add(time.Hour).Unix(),
	})
	require.NoError(t, err)
	return credential
}
