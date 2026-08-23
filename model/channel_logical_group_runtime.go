package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// LogicalChannelMemberSnapshot is a safe immutable-at-call-boundary view of a
// physical channel in a logical group. Credentials are never loaded here.
type LogicalChannelMemberSnapshot struct {
	ChannelID          int    `json:"channel_id"`
	Weight             uint   `json:"weight"`
	AddressFingerprint string `json:"address_fingerprint,omitempty"`
}

// LogicalChannelGroupSnapshot freezes the relation and revision used by one
// scheduling, probe, or detection task.
type LogicalChannelGroupSnapshot struct {
	LogicalChannelID int64                          `json:"logical_channel_id"`
	Revision         int64                          `json:"revision"`
	Status           int                            `json:"status"`
	Members          []LogicalChannelMemberSnapshot `json:"members"`
}

// LogicalChannelIdentity is the channel_id to logical identity projection.
// Ungrouped channels use their own id and revision zero.
type LogicalChannelIdentity struct {
	ChannelID        int   `json:"channel_id"`
	LogicalChannelID int64 `json:"logical_channel_id"`
	Revision         int64 `json:"revision"`
}

// LogicalChannelRuntimeSnapshot is atomically published and contains no keys.
type LogicalChannelRuntimeSnapshot struct {
	Channels map[int]LogicalChannelIdentity        `json:"channels"`
	Groups   map[int64]LogicalChannelGroupSnapshot `json:"groups"`
}

var (
	logicalChannelRuntimeCache *LogicalChannelRuntimeSnapshot
	logicalChannelRuntimeDirty bool
)

var (
	ErrLogicalChannelRuntimeChannelNotFound = errors.New("渠道不存在")
	ErrLogicalChannelRuntimeGroupNotFound   = errors.New("逻辑渠道组不存在")
	ErrLogicalChannelRuntimeUnavailable     = errors.New("逻辑渠道组运行时缓存不可用")
)

// RefreshLogicalChannelRuntimeCache builds a complete relation snapshot and
// publishes it only after all queries succeed. On failure, the previous
// complete snapshot remains available.
func RefreshLogicalChannelRuntimeCache() error {
	snapshot, err := buildLogicalChannelRuntimeSnapshot(DB)
	if err != nil {
		channelSyncLock.Lock()
		logicalChannelRuntimeDirty = true
		channelSyncLock.Unlock()
		return err
	}
	channelSyncLock.Lock()
	logicalChannelRuntimeCache = snapshot
	logicalChannelRuntimeDirty = false
	channelSyncLock.Unlock()
	return nil
}

// InvalidateLogicalChannelRuntimeCache marks the relation cache stale without
// discarding the last complete snapshot. Resolving a new task attempts refresh.
func InvalidateLogicalChannelRuntimeCache() {
	channelSyncLock.Lock()
	logicalChannelRuntimeDirty = true
	channelSyncLock.Unlock()
}

// ResolveChannelLogicalIdentity resolves one physical channel to its current
// logical identity and revision. Callers should retain this value in task
// snapshots so later membership changes cannot rewrite running tasks.
func ResolveChannelLogicalIdentity(channelID int) (LogicalChannelIdentity, error) {
	if channelID <= 0 {
		return LogicalChannelIdentity{}, ErrLogicalChannelRuntimeChannelNotFound
	}
	if !IsLogicalChannelGroupingEnabled() {
		// The process-wide kill switch is deliberately checked before the
		// runtime cache so disabling the rollout immediately restores the old
		// physical-channel identity for newly created tasks.
		return resolvePhysicalChannelIdentity(channelID)
	}
	if common.MemoryCacheEnabled {
		channelSyncLock.RLock()
		snapshot := logicalChannelRuntimeCache
		dirty := logicalChannelRuntimeDirty
		identity, ok := identityFromRuntimeSnapshot(snapshot, channelID)
		channelSyncLock.RUnlock()
		if !dirty && ok {
			return identity, nil
		}
		if err := RefreshLogicalChannelRuntimeCache(); err == nil {
			channelSyncLock.RLock()
			identity, ok = identityFromRuntimeSnapshot(logicalChannelRuntimeCache, channelID)
			channelSyncLock.RUnlock()
			if ok {
				return identity, nil
			}
		}
		// Keep the previous snapshot published for diagnostics, but never use
		// dirty membership for a new task. A direct database read either
		// resolves current ownership or fails closed.
	}
	return resolveLogicalIdentityFromDatabase(channelID)
}

// GetLogicalChannelGroupSnapshot returns a defensive copy of one group.
func GetLogicalChannelGroupSnapshot(logicalID int64) (LogicalChannelGroupSnapshot, error) {
	if logicalID <= 0 {
		return LogicalChannelGroupSnapshot{}, ErrLogicalChannelRuntimeGroupNotFound
	}
	if common.MemoryCacheEnabled {
		channelSyncLock.RLock()
		snapshot := logicalChannelRuntimeCache
		dirty := logicalChannelRuntimeDirty
		group, ok := groupFromRuntimeSnapshot(snapshot, logicalID)
		channelSyncLock.RUnlock()
		if !dirty && ok {
			return group, nil
		}
		if err := RefreshLogicalChannelRuntimeCache(); err == nil {
			channelSyncLock.RLock()
			group, ok = groupFromRuntimeSnapshot(logicalChannelRuntimeCache, logicalID)
			channelSyncLock.RUnlock()
			if ok {
				return group, nil
			}
		}
		// A dirty group snapshot may contain a removed key. Selection callers
		// must read the current complete relation or receive an error.
	}
	return loadLogicalChannelGroupSnapshotFromDatabase(logicalID)
}

// GetLogicalChannelRuntimeSnapshot returns a defensive copy suitable for a
// frozen execution snapshot or diagnostics.
func GetLogicalChannelRuntimeSnapshot() (*LogicalChannelRuntimeSnapshot, error) {
	if !common.MemoryCacheEnabled {
		return buildLogicalChannelRuntimeSnapshot(DB)
	}
	if common.MemoryCacheEnabled {
		channelSyncLock.RLock()
		snapshot := cloneLogicalChannelRuntimeSnapshot(logicalChannelRuntimeCache)
		dirty := logicalChannelRuntimeDirty
		channelSyncLock.RUnlock()
		if snapshot != nil && !dirty {
			return snapshot, nil
		}
		if err := RefreshLogicalChannelRuntimeCache(); err == nil {
			channelSyncLock.RLock()
			snapshot = cloneLogicalChannelRuntimeSnapshot(logicalChannelRuntimeCache)
			channelSyncLock.RUnlock()
			if snapshot != nil {
				return snapshot, nil
			}
		}
		if snapshot != nil {
			return snapshot, nil
		}
	}
	return nil, ErrLogicalChannelRuntimeUnavailable
}

func identityFromRuntimeSnapshot(snapshot *LogicalChannelRuntimeSnapshot, channelID int) (LogicalChannelIdentity, bool) {
	if snapshot == nil {
		return LogicalChannelIdentity{}, false
	}
	identity, ok := snapshot.Channels[channelID]
	return identity, ok
}

func groupFromRuntimeSnapshot(snapshot *LogicalChannelRuntimeSnapshot, logicalID int64) (LogicalChannelGroupSnapshot, bool) {
	if snapshot == nil {
		return LogicalChannelGroupSnapshot{}, false
	}
	group, ok := snapshot.Groups[logicalID]
	if !ok {
		return LogicalChannelGroupSnapshot{}, false
	}
	group.Members = append([]LogicalChannelMemberSnapshot(nil), group.Members...)
	return group, true
}

func cloneLogicalChannelRuntimeSnapshot(snapshot *LogicalChannelRuntimeSnapshot) *LogicalChannelRuntimeSnapshot {
	if snapshot == nil {
		return nil
	}
	clone := &LogicalChannelRuntimeSnapshot{
		Channels: make(map[int]LogicalChannelIdentity, len(snapshot.Channels)),
		Groups:   make(map[int64]LogicalChannelGroupSnapshot, len(snapshot.Groups)),
	}
	for channelID, identity := range snapshot.Channels {
		clone.Channels[channelID] = identity
	}
	for logicalID, group := range snapshot.Groups {
		group.Members = append([]LogicalChannelMemberSnapshot(nil), group.Members...)
		clone.Groups[logicalID] = group
	}
	return clone
}

func buildLogicalChannelRuntimeSnapshot(db *gorm.DB) (*LogicalChannelRuntimeSnapshot, error) {
	if db == nil {
		return nil, ErrLogicalChannelRuntimeUnavailable
	}
	// The relation schema is optional during rolling upgrades and on read-only
	// nodes that have not observed the master migration yet. Keep the runtime
	// projection physical-only until all three required pieces are present;
	// this preserves pre-feature channel resolution instead of turning a
	// missing optional table/column into a routing outage.
	if !logicalChannelGroupingSchemaAvailable(db) {
		return buildPhysicalChannelRuntimeSnapshot(db)
	}
	// Query only identity columns; Channel.Key and other credential fields are
	// deliberately not selected.
	type channelIdentityRow struct {
		ID               int
		LogicalChannelID *int64
	}
	var channels []channelIdentityRow
	if err := db.Model(&Channel{}).Select("id", "logical_channel_id").Order("id asc").Find(&channels).Error; err != nil {
		return nil, err
	}
	var groups []ChannelLogicalGroup
	if err := db.Model(&ChannelLogicalGroup{}).Select("id", "status", "revision").Order("id asc").Find(&groups).Error; err != nil {
		return nil, err
	}
	var members []ChannelLogicalGroupMember
	if err := db.Model(&ChannelLogicalGroupMember{}).Select("logical_group_id", "channel_id", "weight", "address_fingerprint").Order("logical_group_id asc, channel_id asc").Find(&members).Error; err != nil {
		return nil, err
	}

	snapshot := &LogicalChannelRuntimeSnapshot{
		Channels: make(map[int]LogicalChannelIdentity, len(channels)),
		Groups:   make(map[int64]LogicalChannelGroupSnapshot, len(groups)),
	}
	for _, group := range groups {
		if group.Id <= 0 || group.Revision <= 0 || (group.Status != ChannelLogicalGroupStatusEnabled && group.Status != ChannelLogicalGroupStatusDisabled) {
			return nil, fmt.Errorf("%w: 逻辑组 revision 无效", ErrLogicalChannelRuntimeUnavailable)
		}
		if !IsLogicalChannelGroupActive(group.Status) {
			// Keep the persisted group in the diagnostic snapshot, but mark it
			// disabled so all shared callers fall back to physical identities.
			group.Status = ChannelLogicalGroupStatusDisabled
		}
		snapshot.Groups[group.Id] = LogicalChannelGroupSnapshot{LogicalChannelID: group.Id, Revision: group.Revision, Status: group.Status, Members: make([]LogicalChannelMemberSnapshot, 0)}
	}
	for _, member := range members {
		group, ok := snapshot.Groups[member.LogicalGroupID]
		if !ok {
			return nil, fmt.Errorf("%w: 成员所属逻辑组不存在", ErrLogicalChannelRuntimeUnavailable)
		}
		if member.ChannelID <= 0 || ValidateChannelLogicalGroupMemberWeight(member.Weight) != nil || !validChannelLogicalGroupAddressFingerprint(member.AddressFingerprint) {
			return nil, fmt.Errorf("%w: 成员数据无效", ErrLogicalChannelRuntimeUnavailable)
		}
		group.Members = append(group.Members, LogicalChannelMemberSnapshot{ChannelID: member.ChannelID, Weight: member.Weight, AddressFingerprint: strings.ToLower(strings.TrimSpace(member.AddressFingerprint))})
		snapshot.Groups[member.LogicalGroupID] = group
	}
	for _, channel := range channels {
		logicalID := int64(channel.ID)
		revision := int64(0)
		if channel.LogicalChannelID != nil && *channel.LogicalChannelID > 0 {
			logicalID = *channel.LogicalChannelID
			group, ok := snapshot.Groups[logicalID]
			if !ok {
				return nil, fmt.Errorf("%w: 渠道引用的逻辑组不存在", ErrLogicalChannelRuntimeUnavailable)
			}
			if IsLogicalChannelGroupActive(group.Status) {
				revision = group.Revision
			} else {
				// A disabled rollout must not leave new tasks sharing a group.
				logicalID = int64(channel.ID)
			}
		}
		snapshot.Channels[channel.ID] = LogicalChannelIdentity{ChannelID: channel.ID, LogicalChannelID: logicalID, Revision: revision}
	}
	// A complete snapshot must not silently route to a missing physical key.
	for logicalID, group := range snapshot.Groups {
		if len(group.Members) == 0 {
			return nil, fmt.Errorf("%w: 逻辑组无成员", ErrLogicalChannelRuntimeUnavailable)
		}
		// Disabled groups remain in the diagnostic snapshot, but their members
		// intentionally resolve to physical identities during the rollout
		// kill-switch. Do not reject that deliberate fallback as a relation
		// inconsistency.
		if !IsLogicalChannelGroupActive(group.Status) {
			continue
		}
		for _, member := range group.Members {
			identity, ok := snapshot.Channels[member.ChannelID]
			if !ok || identity.LogicalChannelID != logicalID {
				return nil, fmt.Errorf("%w: 逻辑组成员渠道关系不一致", ErrLogicalChannelRuntimeUnavailable)
			}
		}
	}
	return snapshot, nil
}

func logicalChannelGroupingSchemaAvailable(db *gorm.DB) bool {
	if db == nil {
		return false
	}
	return db.Migrator().HasTable(&ChannelLogicalGroup{}) &&
		db.Migrator().HasTable(&ChannelLogicalGroupMember{}) &&
		db.Migrator().HasColumn(&Channel{}, "logical_channel_id")
}

func buildPhysicalChannelRuntimeSnapshot(db *gorm.DB) (*LogicalChannelRuntimeSnapshot, error) {
	var channels []struct {
		ID int
	}
	if err := db.Model(&Channel{}).Select("id").Order("id asc").Find(&channels).Error; err != nil {
		return nil, err
	}
	snapshot := &LogicalChannelRuntimeSnapshot{
		Channels: make(map[int]LogicalChannelIdentity, len(channels)),
		Groups:   make(map[int64]LogicalChannelGroupSnapshot),
	}
	for _, channel := range channels {
		snapshot.Channels[channel.ID] = LogicalChannelIdentity{
			ChannelID: channel.ID, LogicalChannelID: int64(channel.ID),
		}
	}
	return snapshot, nil
}

// GetLogicalChannelSelectionSnapshot returns the frozen member set for a
// previously resolved identity. Ungrouped identities receive an in-memory self
// member and never look up a persisted group id, avoiding numeric id collisions
// between physical channels and logical groups.
func GetLogicalChannelSelectionSnapshot(identity LogicalChannelIdentity) (LogicalChannelGroupSnapshot, error) {
	if identity.ChannelID <= 0 || identity.LogicalChannelID <= 0 {
		return LogicalChannelGroupSnapshot{}, ErrLogicalChannelRuntimeChannelNotFound
	}
	if !IsLogicalChannelGroupingEnabled() {
		return LogicalChannelGroupSnapshot{
			LogicalChannelID: int64(identity.ChannelID),
			Revision:         0,
			Status:           ChannelLogicalGroupStatusEnabled,
			Members:          []LogicalChannelMemberSnapshot{{ChannelID: identity.ChannelID, Weight: ChannelLogicalGroupDefaultMemberWeight}},
		}, nil
	}
	if identity.Revision == 0 && identity.LogicalChannelID == int64(identity.ChannelID) {
		return LogicalChannelGroupSnapshot{
			LogicalChannelID: identity.LogicalChannelID,
			Revision:         0,
			Status:           ChannelLogicalGroupStatusEnabled,
			Members:          []LogicalChannelMemberSnapshot{{ChannelID: identity.ChannelID, Weight: ChannelLogicalGroupDefaultMemberWeight}},
		}, nil
	}
	group, err := GetLogicalChannelGroupSnapshot(identity.LogicalChannelID)
	if err != nil {
		return LogicalChannelGroupSnapshot{}, err
	}
	if group.Revision != identity.Revision {
		return LogicalChannelGroupSnapshot{}, ErrChannelLogicalGroupRevisionConflict
	}
	return group, nil
}

// GetLogicalChannelMembers is retained as a convenience for callers that start
// from a physical channel rather than a frozen identity.
func GetLogicalChannelMembers(channelID int) (LogicalChannelIdentity, LogicalChannelGroupSnapshot, error) {
	identity, err := ResolveChannelLogicalIdentity(channelID)
	if err != nil {
		return LogicalChannelIdentity{}, LogicalChannelGroupSnapshot{}, err
	}
	group, err := GetLogicalChannelSelectionSnapshot(identity)
	if err != nil {
		return LogicalChannelIdentity{}, LogicalChannelGroupSnapshot{}, err
	}
	return identity, group, nil
}

func resolveLogicalIdentityFromDatabase(channelID int) (LogicalChannelIdentity, error) {
	if DB == nil {
		return LogicalChannelIdentity{}, ErrLogicalChannelRuntimeUnavailable
	}
	if !logicalChannelGroupingSchemaAvailable(DB) {
		return resolvePhysicalChannelIdentity(channelID)
	}
	type channelIdentityRow struct {
		ID               int
		LogicalChannelID *int64
	}
	var channel channelIdentityRow
	if err := DB.Model(&Channel{}).Select("id", "logical_channel_id").Where("id = ?", channelID).First(&channel).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return LogicalChannelIdentity{}, ErrLogicalChannelRuntimeChannelNotFound
		}
		return LogicalChannelIdentity{}, err
	}
	identity := LogicalChannelIdentity{ChannelID: channel.ID, LogicalChannelID: int64(channel.ID)}
	if channel.LogicalChannelID != nil && *channel.LogicalChannelID > 0 {
		identity.LogicalChannelID = *channel.LogicalChannelID
		var group ChannelLogicalGroup
		if err := DB.Model(&ChannelLogicalGroup{}).Select("id", "status", "revision").Where("id = ?", identity.LogicalChannelID).First(&group).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return LogicalChannelIdentity{}, ErrLogicalChannelRuntimeGroupNotFound
			}
			return LogicalChannelIdentity{}, err
		}
		if !IsLogicalChannelGroupActive(group.Status) {
			return LogicalChannelIdentity{ChannelID: channelID, LogicalChannelID: int64(channelID)}, nil
		}
		identity.Revision = group.Revision
	}
	return identity, nil
}

func resolvePhysicalChannelIdentity(channelID int) (LogicalChannelIdentity, error) {
	if DB == nil {
		return LogicalChannelIdentity{}, ErrLogicalChannelRuntimeUnavailable
	}
	var channel struct {
		ID int
	}
	if err := DB.Model(&Channel{}).Select("id").Where("id = ?", channelID).First(&channel).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return LogicalChannelIdentity{}, ErrLogicalChannelRuntimeChannelNotFound
		}
		return LogicalChannelIdentity{}, err
	}
	return LogicalChannelIdentity{ChannelID: channel.ID, LogicalChannelID: int64(channel.ID)}, nil
}

func loadLogicalChannelGroupSnapshotFromDatabase(logicalID int64) (LogicalChannelGroupSnapshot, error) {
	if DB == nil {
		return LogicalChannelGroupSnapshot{}, ErrLogicalChannelRuntimeUnavailable
	}
	if !logicalChannelGroupingSchemaAvailable(DB) {
		return LogicalChannelGroupSnapshot{}, ErrLogicalChannelRuntimeGroupNotFound
	}
	var group ChannelLogicalGroup
	if err := DB.Model(&ChannelLogicalGroup{}).Select("id", "status", "revision").Where("id = ?", logicalID).First(&group).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return LogicalChannelGroupSnapshot{}, ErrLogicalChannelRuntimeGroupNotFound
		}
		return LogicalChannelGroupSnapshot{}, err
	}
	if group.Id <= 0 || group.Revision <= 0 || (group.Status != ChannelLogicalGroupStatusEnabled && group.Status != ChannelLogicalGroupStatusDisabled) {
		return LogicalChannelGroupSnapshot{}, ErrLogicalChannelRuntimeUnavailable
	}
	if !IsLogicalChannelGroupActive(group.Status) {
		group.Status = ChannelLogicalGroupStatusDisabled
	}
	var members []ChannelLogicalGroupMember
	if err := DB.Model(&ChannelLogicalGroupMember{}).Select("logical_group_id", "channel_id", "weight", "address_fingerprint").Where("logical_group_id = ?", logicalID).Order("channel_id asc").Find(&members).Error; err != nil {
		return LogicalChannelGroupSnapshot{}, err
	}
	memberIDs := make([]int, 0, len(members))
	for _, member := range members {
		memberIDs = append(memberIDs, member.ChannelID)
	}
	type channelIdentityRow struct {
		ID               int
		LogicalChannelID *int64
	}
	var channels []channelIdentityRow
	if len(memberIDs) > 0 {
		if err := DB.Model(&Channel{}).Select("id", "logical_channel_id").Where("id IN ?", memberIDs).Find(&channels).Error; err != nil {
			return LogicalChannelGroupSnapshot{}, err
		}
	}
	channelByID := make(map[int]channelIdentityRow, len(channels))
	for _, channel := range channels {
		channelByID[channel.ID] = channel
	}
	result := LogicalChannelGroupSnapshot{LogicalChannelID: group.Id, Revision: group.Revision, Status: group.Status, Members: make([]LogicalChannelMemberSnapshot, 0, len(members))}
	for _, member := range members {
		channel, exists := channelByID[member.ChannelID]
		if !exists || channel.LogicalChannelID == nil || *channel.LogicalChannelID != logicalID || member.ChannelID <= 0 || ValidateChannelLogicalGroupMemberWeight(member.Weight) != nil || !validChannelLogicalGroupAddressFingerprint(member.AddressFingerprint) {
			return LogicalChannelGroupSnapshot{}, ErrLogicalChannelRuntimeUnavailable
		}
		result.Members = append(result.Members, LogicalChannelMemberSnapshot{ChannelID: member.ChannelID, Weight: member.Weight, AddressFingerprint: strings.ToLower(strings.TrimSpace(member.AddressFingerprint))})
	}
	if len(result.Members) == 0 {
		return LogicalChannelGroupSnapshot{}, ErrLogicalChannelRuntimeGroupNotFound
	}
	return result, nil
}
