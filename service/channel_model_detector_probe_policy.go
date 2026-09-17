package service

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/model"
)

// CheckProbePolicy runs before reserving an HTTP attempt. Credentials and
// expiry are checked again by AuthorizeAttempt; no store lock is held during DB IO.
func (store *ChannelModelDetectorTokenStore) CheckProbePolicy(ctx context.Context, token string) error {
	if store == nil {
		return ErrChannelModelDetectorTokenInvalid
	}
	nonce, err := store.authenticate(token)
	if err != nil {
		return err
	}
	store.mu.Lock()
	record, ok := store.records[nonce]
	if !ok {
		store.mu.Unlock()
		return ErrChannelModelDetectorTokenInvalid
	}
	claims := cloneChannelModelDetectorTokenClaims(record.claims)
	revoked, expired := record.revoked, store.now().Unix() >= claims.ExpiresAt
	store.mu.Unlock()
	if expired {
		return ErrChannelModelDetectorTokenExpired
	}
	if revoked {
		return ErrChannelModelDetectorTokenRevoked
	}
	if claims.Trigger != model.ChannelStatusProbeTriggerScheduled {
		return nil
	}
	ctx = WithChannelProbeTrigger(ctx, claims.Trigger)
	if claims.LogicalRevision <= 0 {
		return CheckChannelProbeAllowed(ctx, claims.ChannelID)
	}
	for _, member := range claims.LogicalMembers {
		err := CheckChannelProbeAllowed(ctx, member.ChannelID)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrChannelAutoProbeDisabled) {
			return err
		}
	}
	return ErrChannelAutoProbeDisabled
}
