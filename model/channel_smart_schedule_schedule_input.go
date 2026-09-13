package model

import "context"

// GetChannelSmartScheduleRoutesForScheduling reads current persisted inputs for
// a complete scheduling run. Dashboard snapshots can lag behind runtime writes
// and must not supply revisions for a new guarded update.
func GetChannelSmartScheduleRoutesForScheduling(ctx context.Context) ([]ChannelSmartScheduleRoute, error) {
	return getChannelSmartScheduleRoutes(ctx, true)
}
