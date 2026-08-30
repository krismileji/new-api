package model

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"gorm.io/gorm"
)

// ChannelStatusProbeOverviewRelations is a request-local projection of the
// current channel and logical-group relationships. It is loaded directly from
// the database and is never retained in the process-wide runtime cache.
type ChannelStatusProbeOverviewRelations struct {
	logicalGrouping bool
	scopesByChannel map[int]channelStatusProbeScope
	scopesByLogical map[int64]channelStatusProbeScope
}

func channelStatusProbeOverviewDB(ctx context.Context, db *gorm.DB) (*gorm.DB, error) {
	if db == nil {
		db = DB
	}
	if db == nil {
		return nil, errors.New("渠道状态探测查询数据库不可用")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return db.WithContext(ctx), nil
}

// LoadChannelStatusProbeOverviewRelations loads all relationships with a
// fixed number of direct database queries so overview projection does not use
// a stale runtime snapshot or issue per-row relationship lookups.
func LoadChannelStatusProbeOverviewRelations(ctx context.Context, db *gorm.DB) (*ChannelStatusProbeOverviewRelations, error) {
	queryDB, err := channelStatusProbeOverviewDB(ctx, db)
	if err != nil {
		return nil, err
	}
	logicalGroupingAvailable := IsLogicalChannelGroupingEnabled() && logicalChannelGroupingSchemaAvailable(queryDB)
	type channelRelationRow struct {
		ID               int
		LogicalChannelID *int64
	}
	var channels []channelRelationRow
	channelQuery := queryDB.Model(&Channel{}).Select("id")
	if logicalGroupingAvailable {
		channelQuery = channelQuery.Select("id", "logical_channel_id")
	}
	if err := channelQuery.Order("id ASC").Find(&channels).Error; err != nil {
		return nil, err
	}
	relations := &ChannelStatusProbeOverviewRelations{
		logicalGrouping: logicalGroupingAvailable,
		scopesByChannel: make(map[int]channelStatusProbeScope, len(channels)),
		scopesByLogical: make(map[int64]channelStatusProbeScope),
	}
	for _, channel := range channels {
		relations.scopesByChannel[channel.ID] = channelStatusProbeScope{
			Identity:  LogicalChannelIdentity{ChannelID: channel.ID, LogicalChannelID: int64(channel.ID)},
			OwnerID:   channel.ID,
			MemberIDs: []int{channel.ID},
		}
	}
	if !logicalGroupingAvailable {
		return relations, nil
	}

	type logicalGroupRelationRow struct {
		ID       int64
		Status   int
		Revision int64
	}
	var groups []logicalGroupRelationRow
	if err := queryDB.Model(&ChannelLogicalGroup{}).Select("id", "status", "revision").Order("id ASC").Find(&groups).Error; err != nil {
		return nil, err
	}
	type logicalMemberRelationRow struct {
		LogicalGroupID int64
		ChannelID      int
	}
	var members []logicalMemberRelationRow
	if err := queryDB.Model(&ChannelLogicalGroupMember{}).
		Select("logical_group_id", "channel_id").
		Order("logical_group_id ASC, channel_id ASC").Find(&members).Error; err != nil {
		return nil, err
	}

	type logicalGroupRelation struct {
		status    int
		revision  int64
		memberIDs []int
	}
	groupsByID := make(map[int64]*logicalGroupRelation, len(groups))
	for _, group := range groups {
		if group.ID <= 0 || group.Revision <= 0 ||
			(group.Status != ChannelLogicalGroupStatusEnabled && group.Status != ChannelLogicalGroupStatusDisabled) {
			return nil, fmt.Errorf("%w: 逻辑组 revision 无效", ErrLogicalChannelRuntimeUnavailable)
		}
		groupsByID[group.ID] = &logicalGroupRelation{status: group.Status, revision: group.Revision}
	}
	for _, member := range members {
		group, exists := groupsByID[member.LogicalGroupID]
		if !exists {
			return nil, fmt.Errorf("%w: 成员所属逻辑组不存在", ErrLogicalChannelRuntimeUnavailable)
		}
		if member.ChannelID <= 0 {
			return nil, fmt.Errorf("%w: 成员数据无效", ErrLogicalChannelRuntimeUnavailable)
		}
		group.memberIDs = append(group.memberIDs, member.ChannelID)
	}

	channelIDsByLogical := make(map[int64][]int)
	for _, channel := range channels {
		if channel.LogicalChannelID == nil || *channel.LogicalChannelID <= 0 {
			continue
		}
		group, exists := groupsByID[*channel.LogicalChannelID]
		if !exists {
			return nil, fmt.Errorf("%w: 渠道引用的逻辑组不存在", ErrLogicalChannelRuntimeUnavailable)
		}
		if !IsLogicalChannelGroupActive(group.status) {
			continue
		}
		scope := relations.scopesByChannel[channel.ID]
		scope.Identity.LogicalChannelID = *channel.LogicalChannelID
		scope.Identity.Revision = group.revision
		relations.scopesByChannel[channel.ID] = scope
		channelIDsByLogical[*channel.LogicalChannelID] = append(channelIDsByLogical[*channel.LogicalChannelID], channel.ID)
	}
	for logicalID, group := range groupsByID {
		if !IsLogicalChannelGroupActive(group.status) {
			continue
		}
		if len(group.memberIDs) == 0 {
			return nil, fmt.Errorf("%w: 逻辑组无成员", ErrLogicalChannelRuntimeUnavailable)
		}
		sort.Ints(group.memberIDs)
		for _, channelID := range group.memberIDs {
			scope, exists := relations.scopesByChannel[channelID]
			if !exists || scope.Identity.LogicalChannelID != logicalID || scope.Identity.Revision != group.revision {
				return nil, fmt.Errorf("%w: 逻辑组成员渠道关系不一致", ErrLogicalChannelRuntimeUnavailable)
			}
		}
		ownerID := group.memberIDs[0]
		logicalScope := channelStatusProbeScope{
			Identity: LogicalChannelIdentity{
				ChannelID: ownerID, LogicalChannelID: logicalID, Revision: group.revision,
			},
			OwnerID: ownerID, MemberIDs: append([]int(nil), group.memberIDs...),
		}
		relations.scopesByLogical[logicalID] = logicalScope
		for _, channelID := range channelIDsByLogical[logicalID] {
			scope := relations.scopesByChannel[channelID]
			scope.OwnerID = ownerID
			scope.MemberIDs = append([]int(nil), group.memberIDs...)
			relations.scopesByChannel[channelID] = scope
		}
	}
	return relations, nil
}

func (relations *ChannelStatusProbeOverviewRelations) resolveChannelScope(channelID int) (channelStatusProbeScope, error) {
	if relations == nil {
		return channelStatusProbeScope{}, ErrLogicalChannelRuntimeUnavailable
	}
	scope, exists := relations.scopesByChannel[channelID]
	if !exists {
		return channelStatusProbeScope{}, ErrLogicalChannelRuntimeChannelNotFound
	}
	scope.MemberIDs = append([]int(nil), scope.MemberIDs...)
	return scope, nil
}

func (relations *ChannelStatusProbeOverviewRelations) resolvePersistedScope(channelID int, logicalChannelID int64, logicalRevision int64) (channelStatusProbeScope, error) {
	scope, err := relations.resolveChannelScope(channelID)
	if err != nil || !relations.logicalGrouping || logicalChannelID <= 0 || logicalRevision <= 0 ||
		(scope.Identity.Revision > 0 && scope.Identity.LogicalChannelID == logicalChannelID) {
		return scope, err
	}
	logicalScope, exists := relations.scopesByLogical[logicalChannelID]
	if !exists {
		return scope, nil
	}
	logicalScope.MemberIDs = append([]int(nil), logicalScope.MemberIDs...)
	return logicalScope, nil
}

func (relations *ChannelStatusProbeOverviewRelations) resolveLogicalScope(logicalChannelID int64) (channelStatusProbeScope, bool) {
	if relations == nil {
		return channelStatusProbeScope{}, false
	}
	scope, exists := relations.scopesByLogical[logicalChannelID]
	if !exists {
		return channelStatusProbeScope{}, false
	}
	scope.MemberIDs = append([]int(nil), scope.MemberIDs...)
	return scope, true
}

// GetChannelsForStatusProbeOverview loads only the channel fields used by the
// status probe overview. Large provider settings and runtime metadata stay out
// of the polling path.
func GetChannelsForStatusProbeOverview(ctx context.Context, db *gorm.DB, channelIDs []int) ([]*Channel, error) {
	queryDB, err := channelStatusProbeOverviewDB(ctx, db)
	if err != nil {
		return nil, err
	}
	if channelIDs != nil && len(channelIDs) == 0 {
		return []*Channel{}, nil
	}
	var channels []*Channel
	query := resolveChannelSortOptions(false, nil).Apply(queryDB).
		Select("id", "type", "status", "name", "models", commonGroupCol, "remark")
	if channelIDs != nil {
		query = query.Where("id IN ?", channelIDs)
	}
	err = query.Find(&channels).Error
	return channels, err
}

// GetChannelGroupsForStatusProbeOverview loads the lightweight channel/group
// projection used to keep filter options stable while a model filter is active.
func GetChannelGroupsForStatusProbeOverview(ctx context.Context, db *gorm.DB) ([]*Channel, error) {
	queryDB, err := channelStatusProbeOverviewDB(ctx, db)
	if err != nil {
		return nil, err
	}
	var channels []*Channel
	err = queryDB.Select("id", commonGroupCol).Order("id ASC").Find(&channels).Error
	return channels, err
}
