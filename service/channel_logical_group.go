package service

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

var (
	ErrLogicalChannelGroupNotFound        = errors.New("逻辑渠道组不存在")
	ErrLogicalChannelGroupChannelMissing  = errors.New("逻辑渠道组成员渠道不存在")
	ErrLogicalChannelGroupAlreadyGrouped  = errors.New("渠道已属于其他逻辑组")
	ErrLogicalChannelGroupAddressMismatch = errors.New("逻辑渠道组成员请求地址不一致")
	ErrLogicalChannelGroupInvalidRevision = errors.New("逻辑渠道组 revision 无效")
)

// LogicalChannelGroupMemberInput is the write representation of one member.
// A pointer keeps an omitted weight distinguishable from an explicit zero.
type LogicalChannelGroupMemberInput struct {
	ChannelID int   `json:"channel_id"`
	Weight    *uint `json:"weight,omitempty"`
}

// LogicalChannelGroupMemberView is safe to return to administrators; it never
// contains a channel key or any credential material.
type LogicalChannelGroupMemberView struct {
	ID                 int64  `json:"id"`
	ChannelID          int    `json:"channel_id"`
	ChannelName        string `json:"channel_name,omitempty"`
	ChannelType        int    `json:"channel_type,omitempty"`
	ChannelStatus      int    `json:"channel_status,omitempty"`
	Weight             uint   `json:"weight"`
	NormalizedAddress  string `json:"normalized_address,omitempty"`
	AddressFingerprint string `json:"address_fingerprint,omitempty"`
	CreatedAt          int64  `json:"created_at"`
	UpdatedAt          int64  `json:"updated_at"`
}

type LogicalChannelGroupView struct {
	ID        int64                           `json:"id"`
	Name      string                          `json:"name"`
	Remark    string                          `json:"remark,omitempty"`
	Status    int                             `json:"status"`
	Revision  int64                           `json:"revision"`
	CreatedAt int64                           `json:"created_at"`
	UpdatedAt int64                           `json:"updated_at"`
	Members   []LogicalChannelGroupMemberView `json:"members"`
}

// CreateLogicalChannelGroup creates a group and its initial member set in one
// transaction. Every member must resolve to the same normalized request URL.
func CreateLogicalChannelGroup(name, remark string, status int, inputs []LogicalChannelGroupMemberInput) (*LogicalChannelGroupView, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, model.ErrChannelLogicalGroupInvalidName
	}
	if len([]rune(name)) > 255 {
		return nil, fmt.Errorf("%w: 名称长度不能超过 255 个字符", model.ErrChannelLogicalGroupInvalidName)
	}
	remark = strings.TrimSpace(remark)
	if len([]rune(remark)) > 1024 {
		return nil, fmt.Errorf("逻辑渠道组备注长度不能超过 1024 个字符")
	}
	if status == 0 {
		status = model.ChannelLogicalGroupStatusEnabled
	}
	if status != model.ChannelLogicalGroupStatusEnabled && status != model.ChannelLogicalGroupStatusDisabled {
		return nil, model.ErrChannelLogicalGroupInvalidStatus
	}
	if len(inputs) == 0 {
		return nil, model.ErrChannelLogicalGroupEmptyMembers
	}

	var result *LogicalChannelGroupView
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		channels, members, err := prepareLogicalChannelGroupMembers(tx, 0, inputs, nil)
		if err != nil {
			return err
		}
		group := &model.ChannelLogicalGroup{Name: name, Remark: remark, Status: status}
		if err := tx.Create(group).Error; err != nil {
			return err
		}
		for i := range members {
			members[i].LogicalGroupID = group.Id
			if err := tx.Create(&members[i]).Error; err != nil {
				return err
			}
		}
		memberIDs := make([]int, 0, len(channels))
		for _, channel := range channels {
			memberIDs = append(memberIDs, channel.Id)
		}
		if err := model.EnsureChannelModelDetectionLogicalConfigTx(tx, group.Id, memberIDs); err != nil {
			return err
		}
		if err := assignLogicalChannelIDs(tx, nil, channels, channels, group.Id); err != nil {
			return err
		}
		result = buildLogicalChannelGroupView(group, members, channels)
		return nil
	})
	if err != nil {
		return nil, err
	}
	InvalidateLogicalChannelRuntimeCache()
	return result, nil
}

// ListLogicalChannelGroups returns all configured groups and safe member
// metadata. Channel keys are deliberately never selected or serialized.
func ListLogicalChannelGroups() ([]LogicalChannelGroupView, error) {
	groups := make([]model.ChannelLogicalGroup, 0)
	if err := model.DB.Order("id asc").Find(&groups).Error; err != nil {
		return nil, err
	}
	views := make([]LogicalChannelGroupView, 0, len(groups))
	for i := range groups {
		view, err := loadLogicalChannelGroupView(model.DB, &groups[i])
		if err != nil {
			return nil, err
		}
		views = append(views, *view)
	}
	return views, nil
}

func GetLogicalChannelGroup(id int64) (*LogicalChannelGroupView, error) {
	if id <= 0 {
		return nil, ErrLogicalChannelGroupNotFound
	}
	var group model.ChannelLogicalGroup
	if err := model.DB.First(&group, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLogicalChannelGroupNotFound
		}
		return nil, err
	}
	return loadLogicalChannelGroupView(model.DB, &group)
}

// ReplaceLogicalChannelGroupMembers atomically replaces the complete member
// set using an optimistic revision check. A stale revision is rejected before
// any relation or channel ownership is changed.
func ReplaceLogicalChannelGroupMembers(id, expectedRevision int64, inputs []LogicalChannelGroupMemberInput) (*LogicalChannelGroupView, error) {
	if id <= 0 {
		return nil, ErrLogicalChannelGroupNotFound
	}
	if expectedRevision <= 0 {
		return nil, ErrLogicalChannelGroupInvalidRevision
	}
	if len(inputs) == 0 {
		return nil, model.ErrChannelLogicalGroupEmptyMembers
	}
	var result *LogicalChannelGroupView
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var group model.ChannelLogicalGroup
		if err := tx.First(&group, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrLogicalChannelGroupNotFound
			}
			return err
		}
		if err := model.CheckChannelLogicalGroupRevision(group.Revision, expectedRevision); err != nil {
			return err
		}
		newInputIDs, err := logicalChannelGroupMemberInputIDs(inputs)
		if err != nil {
			return err
		}
		var discoveredOldMembers []model.ChannelLogicalGroupMember
		if err := tx.Select("channel_id").Where("logical_group_id = ?", id).Find(&discoveredOldMembers).Error; err != nil {
			return err
		}
		lockIDSet := make(map[int]struct{}, len(newInputIDs)+len(discoveredOldMembers))
		lockIDs := make([]int, 0, len(newInputIDs)+len(discoveredOldMembers))
		for _, channelID := range newInputIDs {
			lockIDSet[channelID] = struct{}{}
			lockIDs = append(lockIDs, channelID)
		}
		for _, member := range discoveredOldMembers {
			if _, exists := lockIDSet[member.ChannelID]; exists {
				continue
			}
			lockIDSet[member.ChannelID] = struct{}{}
			lockIDs = append(lockIDs, member.ChannelID)
		}
		lockedChannels, err := model.LockChannelsForLogicalGroupMembership(tx, lockIDs)
		if err != nil {
			return err
		}
		channels, members, err := prepareLogicalChannelGroupMembers(tx, id, inputs, lockedChannels)
		if err != nil {
			return err
		}
		lockedGroup, err := model.LockLogicalChannelGroupForMembership(tx, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrLogicalChannelGroupNotFound
			}
			return err
		}
		group = *lockedGroup
		if err := model.CheckChannelLogicalGroupRevision(group.Revision, expectedRevision); err != nil {
			return err
		}
		oldMembers, err := model.LockLogicalChannelGroupMembers(tx, []int64{id})
		if err != nil {
			return err
		}
		oldIDs := make([]int, 0, len(oldMembers))
		for _, member := range oldMembers {
			oldIDs = append(oldIDs, member.ChannelID)
		}
		newIDs := make([]int, 0, len(channels))
		for _, channel := range channels {
			newIDs = append(newIDs, channel.Id)
		}
		if err := model.EnsureChannelModelDetectionLogicalConfigTx(tx, id, newIDs); err != nil {
			return err
		}
		now := common.GetTimestamp()
		newRevision := group.Revision + 1
		update := tx.Model(&model.ChannelLogicalGroup{}).
			Where("id = ? AND revision = ?", id, expectedRevision).
			Updates(map[string]interface{}{"revision": newRevision, "updated_at": now})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return model.ErrChannelLogicalGroupRevisionConflict
		}
		if err := tx.Where("logical_group_id = ?", id).Delete(&model.ChannelLogicalGroupMember{}).Error; err != nil {
			return err
		}
		for i := range members {
			members[i].LogicalGroupID = id
			if err := tx.Create(&members[i]).Error; err != nil {
				return err
			}
		}
		if err := assignLogicalChannelIDs(tx, oldIDs, channels, lockedChannels, id); err != nil {
			return err
		}
		group.Revision = newRevision
		group.UpdatedAt = now
		result = buildLogicalChannelGroupView(&group, members, channels)
		return nil
	})
	if err != nil {
		return nil, err
	}
	InvalidateLogicalChannelRuntimeCache()
	return result, nil
}

// UpdateLogicalChannelGroupStatus toggles shared behavior for one logical
// group with the same optimistic revision contract as member updates. Setting
// status to disabled keeps all relations and history intact; new scheduling,
// probe, and detection tasks resolve their physical channel identity instead.
func UpdateLogicalChannelGroupStatus(id, expectedRevision int64, status int) (*LogicalChannelGroupView, error) {
	if id <= 0 {
		return nil, ErrLogicalChannelGroupNotFound
	}
	if expectedRevision <= 0 {
		return nil, ErrLogicalChannelGroupInvalidRevision
	}
	if status != model.ChannelLogicalGroupStatusEnabled && status != model.ChannelLogicalGroupStatusDisabled {
		return nil, model.ErrChannelLogicalGroupInvalidStatus
	}
	var result *LogicalChannelGroupView
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var group model.ChannelLogicalGroup
		if err := tx.First(&group, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrLogicalChannelGroupNotFound
			}
			return err
		}
		if err := model.CheckChannelLogicalGroupRevision(group.Revision, expectedRevision); err != nil {
			return err
		}
		now := common.GetTimestamp()
		newRevision := expectedRevision + 1
		updated := tx.Model(&model.ChannelLogicalGroup{}).
			Where("id = ? AND revision = ?", id, expectedRevision).
			Updates(map[string]any{"status": status, "revision": newRevision, "updated_at": now})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return model.ErrChannelLogicalGroupRevisionConflict
		}
		group.Status = status
		group.Revision = newRevision
		group.UpdatedAt = now
		view, err := loadLogicalChannelGroupView(tx, &group)
		if err != nil {
			return err
		}
		result = view
		return nil
	})
	if err != nil {
		return nil, err
	}
	InvalidateLogicalChannelRuntimeCache()
	return result, nil
}

// DeleteLogicalChannelGroup removes only the logical relation and group row;
// physical channels and all their historical data remain untouched.
func DeleteLogicalChannelGroup(id, expectedRevision int64) error {
	if id <= 0 {
		return ErrLogicalChannelGroupNotFound
	}
	if expectedRevision <= 0 {
		return ErrLogicalChannelGroupInvalidRevision
	}
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var group model.ChannelLogicalGroup
		if err := tx.First(&group, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrLogicalChannelGroupNotFound
			}
			return err
		}
		if err := model.CheckChannelLogicalGroupRevision(group.Revision, expectedRevision); err != nil {
			return err
		}
		// Delete the parent with the revision predicate first. This closes the
		// read/check/delete race with a concurrent member replacement; all
		// relation cleanup below remains part of the same transaction.
		deleted := tx.Where("id = ? AND revision = ?", id, expectedRevision).Delete(&model.ChannelLogicalGroup{})
		if deleted.Error != nil {
			return deleted.Error
		}
		if deleted.RowsAffected != 1 {
			return model.ErrChannelLogicalGroupRevisionConflict
		}
		if err := tx.Where("logical_group_id = ?", id).Delete(&model.ChannelLogicalGroupMember{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.Channel{}).Where("logical_channel_id = ?", id).Update("logical_channel_id", nil).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	InvalidateLogicalChannelRuntimeCache()
	return nil
}

// PrecheckLogicalChannelGroup resolves channel ids to safe address results.
func PrecheckLogicalChannelGroup(channelIDs []int) LogicalChannelAddressPrecheckResult {
	if len(channelIDs) == 0 {
		return PrecheckLogicalChannelAddresses(nil)
	}
	seen := make(map[int]struct{}, len(channelIDs))
	ids := make([]int, 0, len(channelIDs))
	for _, id := range channelIDs {
		if id <= 0 {
			return LogicalChannelAddressPrecheckResult{Members: []LogicalChannelAddressPrecheckMember{{ChannelID: id, Error: model.ErrChannelLogicalGroupInvalidMember.Error()}}, Error: model.ErrChannelLogicalGroupInvalidMember.Error()}
		}
		if _, ok := seen[id]; ok {
			return LogicalChannelAddressPrecheckResult{Error: model.ErrChannelLogicalGroupDuplicateMember.Error()}
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	var channels []*model.Channel
	if err := model.DB.Where("id IN ?", ids).Find(&channels).Error; err != nil {
		return LogicalChannelAddressPrecheckResult{Error: err.Error()}
	}
	if len(channels) != len(ids) {
		return LogicalChannelAddressPrecheckResult{Error: ErrLogicalChannelGroupChannelMissing.Error()}
	}
	byID := make(map[int]*model.Channel, len(channels))
	for _, channel := range channels {
		byID[channel.Id] = channel
	}
	inputs := make([]LogicalChannelAddressInput, 0, len(ids))
	for _, id := range ids {
		inputs = append(inputs, LogicalChannelAddressInput{ChannelID: id, Address: byID[id].GetBaseURL()})
	}
	return PrecheckLogicalChannelAddresses(inputs)
}

func logicalChannelGroupMemberInputIDs(inputs []LogicalChannelGroupMemberInput) ([]int, error) {
	if len(inputs) == 0 {
		return nil, model.ErrChannelLogicalGroupEmptyMembers
	}
	ids := make([]int, 0, len(inputs))
	seen := make(map[int]struct{}, len(inputs))
	for _, input := range inputs {
		if input.ChannelID <= 0 {
			return nil, model.ErrChannelLogicalGroupInvalidMember
		}
		if _, exists := seen[input.ChannelID]; exists {
			return nil, model.ErrChannelLogicalGroupDuplicateMember
		}
		seen[input.ChannelID] = struct{}{}
		ids = append(ids, input.ChannelID)
	}
	return ids, nil
}

func prepareLogicalChannelGroupMembers(tx *gorm.DB, groupID int64, inputs []LogicalChannelGroupMemberInput, lockedChannels []*model.Channel) ([]*model.Channel, []model.ChannelLogicalGroupMember, error) {
	ids, err := logicalChannelGroupMemberInputIDs(inputs)
	if err != nil {
		return nil, nil, err
	}
	if lockedChannels == nil {
		lockedChannels, err = model.LockChannelsForLogicalGroupMembership(tx, ids)
		if err != nil {
			return nil, nil, err
		}
	}
	byID := make(map[int]*model.Channel, len(lockedChannels))
	for _, channel := range lockedChannels {
		byID[channel.Id] = channel
	}
	channels := make([]*model.Channel, 0, len(ids))
	addressInputs := make([]LogicalChannelAddressInput, 0, len(ids))
	for _, id := range ids {
		channel := byID[id]
		if channel == nil {
			return nil, nil, ErrLogicalChannelGroupChannelMissing
		}
		if channel.LogicalChannelID != nil && *channel.LogicalChannelID != groupID {
			return nil, nil, ErrLogicalChannelGroupAlreadyGrouped
		}
		channels = append(channels, channel)
		addressInputs = append(addressInputs, LogicalChannelAddressInput{ChannelID: id, Address: channel.GetBaseURL()})
	}
	precheck := PrecheckLogicalChannelAddresses(addressInputs)
	if !precheck.Compatible {
		if precheck.Error != "" {
			return nil, nil, fmt.Errorf("%w: %s", ErrLogicalChannelGroupAddressMismatch, precheck.Error)
		}
		return nil, nil, ErrLogicalChannelGroupAddressMismatch
	}
	var existing []model.ChannelLogicalGroupMember
	if err := tx.Where("channel_id IN ? AND logical_group_id <> ?", ids, groupID).Find(&existing).Error; err != nil {
		return nil, nil, err
	}
	if len(existing) > 0 {
		return nil, nil, ErrLogicalChannelGroupAlreadyGrouped
	}
	members := make([]model.ChannelLogicalGroupMember, 0, len(ids))
	for _, input := range inputs {
		weight, err := model.NormalizeChannelLogicalGroupMemberWeight(input.Weight)
		if err != nil {
			return nil, nil, err
		}
		members = append(members, model.ChannelLogicalGroupMember{
			ChannelID: input.ChannelID, Weight: weight,
			AddressFingerprint: LogicalChannelAddressFingerprint(precheck.NormalizedAddress),
		})
	}
	return channels, members, nil
}

func assignLogicalChannelIDs(tx *gorm.DB, oldIDs []int, channels, lockedChannels []*model.Channel, groupID int64) error {
	newIDs := make(map[int]struct{}, len(channels))
	newIDList := make([]int, 0, len(channels))
	assignIDs := make([]int, 0, len(channels))
	for _, channel := range channels {
		newIDs[channel.Id] = struct{}{}
		newIDList = append(newIDList, channel.Id)
		if channel.LogicalChannelID == nil {
			assignIDs = append(assignIDs, channel.Id)
			continue
		}
		if *channel.LogicalChannelID != groupID {
			return ErrLogicalChannelGroupAlreadyGrouped
		}
	}
	removedIDs := make([]int, 0, len(oldIDs))
	for _, id := range oldIDs {
		if _, keep := newIDs[id]; !keep {
			removedIDs = append(removedIDs, id)
		}
	}
	lockedByID := make(map[int]*model.Channel, len(lockedChannels))
	for _, channel := range lockedChannels {
		lockedByID[channel.Id] = channel
	}
	if len(removedIDs) > 0 {
		for _, channelID := range removedIDs {
			channel := lockedByID[channelID]
			if channel == nil {
				return ErrLogicalChannelGroupChannelMissing
			}
			if channel.LogicalChannelID == nil || *channel.LogicalChannelID != groupID {
				return model.ErrChannelLogicalGroupInvalidMember
			}
		}
		cleared := tx.Model(&model.Channel{}).
			Where("id IN ? AND logical_channel_id = ?", removedIDs, groupID).
			Update("logical_channel_id", nil)
		if cleared.Error != nil {
			return cleared.Error
		}
		if cleared.RowsAffected != int64(len(removedIDs)) {
			return ErrLogicalChannelGroupChannelMissing
		}
	}
	if len(assignIDs) > 0 {
		assigned := tx.Model(&model.Channel{}).
			Where("id IN ? AND logical_channel_id IS NULL", assignIDs).
			Update("logical_channel_id", groupID)
		if assigned.Error != nil {
			return assigned.Error
		}
		if assigned.RowsAffected != int64(len(assignIDs)) {
			return ErrLogicalChannelGroupChannelMissing
		}
	}
	lockedChannels, err := model.LockChannelsForLogicalGroupMembership(tx, newIDList)
	if err != nil {
		return err
	}
	if len(lockedChannels) != len(newIDList) {
		return ErrLogicalChannelGroupChannelMissing
	}
	for _, channel := range lockedChannels {
		if channel.LogicalChannelID == nil || *channel.LogicalChannelID != groupID {
			return model.ErrChannelLogicalGroupInvalidMember
		}
	}
	return nil
}

func loadLogicalChannelGroupView(db *gorm.DB, group *model.ChannelLogicalGroup) (*LogicalChannelGroupView, error) {
	var members []model.ChannelLogicalGroupMember
	if err := db.Where("logical_group_id = ?", group.Id).Order("id asc").Find(&members).Error; err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(members))
	for _, member := range members {
		ids = append(ids, member.ChannelID)
	}
	channels := make([]*model.Channel, 0, len(ids))
	if len(ids) > 0 {
		if err := db.Where("id IN ?", ids).Find(&channels).Error; err != nil {
			return nil, err
		}
	}
	return buildLogicalChannelGroupView(group, members, channels), nil
}

func buildLogicalChannelGroupView(group *model.ChannelLogicalGroup, members []model.ChannelLogicalGroupMember, channels []*model.Channel) *LogicalChannelGroupView {
	byID := make(map[int]*model.Channel, len(channels))
	for _, channel := range channels {
		byID[channel.Id] = channel
	}
	view := &LogicalChannelGroupView{ID: group.Id, Name: group.Name, Remark: group.Remark, Status: group.Status, Revision: group.Revision, CreatedAt: group.CreatedAt, UpdatedAt: group.UpdatedAt, Members: make([]LogicalChannelGroupMemberView, 0, len(members))}
	for _, member := range members {
		item := LogicalChannelGroupMemberView{ID: member.Id, ChannelID: member.ChannelID, Weight: member.Weight, AddressFingerprint: member.AddressFingerprint, CreatedAt: member.CreatedAt, UpdatedAt: member.UpdatedAt}
		if channel := byID[member.ChannelID]; channel != nil {
			item.ChannelName, item.ChannelType, item.ChannelStatus = channel.Name, channel.Type, channel.Status
			if address, err := NormalizeLogicalChannelAddressForChannel(channel); err == nil {
				item.NormalizedAddress = address
			}
		}
		view.Members = append(view.Members, item)
	}
	return view
}
