package model

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// Logical channel groups are an administrative identity shared only by the
// smart-scheduling, status-probe, and model-detection flows. Physical channel
// data (credentials, billing, monitoring, balance, ratio, and concurrency)
// remains keyed by Channel.Id.
const (
	ChannelLogicalGroupStatusEnabled  = 1
	ChannelLogicalGroupStatusDisabled = 2

	ChannelLogicalGroupDefaultMemberWeight uint = 1
	// A bounded weight keeps weighted selection arithmetic safe while leaving
	// enough range for an administrator to express highly uneven ratios.
	ChannelLogicalGroupMaxMemberWeight uint = 1_000_000

	channelLogicalGroupAddressFingerprintLength = 32 // SHA-256, in bytes
)

var (
	ErrChannelLogicalGroupInvalidName       = errors.New("逻辑渠道组名称不能为空")
	ErrChannelLogicalGroupInvalidRemark    = errors.New("逻辑渠道组备注过长")
	ErrChannelLogicalGroupInvalidStatus    = errors.New("逻辑渠道组状态无效")
	ErrChannelLogicalGroupInvalidRevision  = errors.New("逻辑渠道组 revision 无效")
	ErrChannelLogicalGroupInvalidWeight    = errors.New("逻辑渠道组成员 weight 超出范围")
	ErrChannelLogicalGroupInvalidMember    = errors.New("逻辑渠道组成员无效")
	ErrChannelLogicalGroupDuplicateMember  = errors.New("逻辑渠道组成员不能重复")
	ErrChannelLogicalGroupEmptyMembers     = errors.New("逻辑渠道组至少需要一个成员")
	ErrChannelLogicalGroupRevisionConflict  = errors.New("逻辑渠道组 revision 冲突")
	ErrChannelLogicalGroupInvalidFingerprint = errors.New("逻辑渠道组成员地址摘要无效")
)

// ChannelLogicalGroup is the durable identity for a set of physical channels
// that are explicitly configured to share selected background capabilities.
type ChannelLogicalGroup struct {
	Id        int64  `json:"id" gorm:"primaryKey"`
	Name      string `json:"name" gorm:"type:varchar(255);not null"`
	Remark    string `json:"remark" gorm:"type:varchar(1024)"`
	Status    int    `json:"status" gorm:"not null"`
	Revision  int64  `json:"revision" gorm:"bigint;not null"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt int64  `json:"updated_at" gorm:"bigint;not null"`

	Members []ChannelLogicalGroupMember `json:"members,omitempty" gorm:"-"`
}

// ChannelLogicalGroupMember records one physical channel and its group-local
// selection weight. ChannelId has a unique index, enforcing the invariant
// that a physical channel belongs to at most one logical group. We intentionally
// avoid database foreign-key constraints so SQLite, MySQL, and PostgreSQL
// migrations share identical semantics.
type ChannelLogicalGroupMember struct {
	Id                 int64  `json:"id" gorm:"primaryKey"`
	LogicalGroupID     int64  `json:"logical_group_id" gorm:"bigint;not null;index:idx_channel_logical_group_member_group"`
	ChannelID          int    `json:"channel_id" gorm:"not null;uniqueIndex:uk_channel_logical_group_member_channel"`
	Weight             uint   `json:"weight" gorm:"not null"`
	AddressFingerprint string `json:"address_fingerprint" gorm:"type:char(64);not null"`
	CreatedAt          int64  `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt          int64  `json:"updated_at" gorm:"bigint;not null"`
}

func (group *ChannelLogicalGroup) BeforeCreate(_ *gorm.DB) error {
	group.Name = strings.TrimSpace(group.Name)
	group.Remark = strings.TrimSpace(group.Remark)
	if group.Status == 0 {
		group.Status = ChannelLogicalGroupStatusEnabled
	}
	if group.Revision == 0 {
		group.Revision = 1
	}
	now := common.GetTimestamp()
	if group.CreatedAt == 0 {
		group.CreatedAt = now
	}
	if group.UpdatedAt == 0 {
		group.UpdatedAt = group.CreatedAt
	}
	return group.Validate()
}

func (group ChannelLogicalGroup) Validate() error {
	if strings.TrimSpace(group.Name) == "" {
		return ErrChannelLogicalGroupInvalidName
	}
	if len([]rune(group.Name)) > 255 {
		return ErrChannelLogicalGroupInvalidName
	}
	if len([]rune(group.Remark)) > 1024 {
		return ErrChannelLogicalGroupInvalidRemark
	}
	if group.Status != ChannelLogicalGroupStatusEnabled && group.Status != ChannelLogicalGroupStatusDisabled {
		return ErrChannelLogicalGroupInvalidStatus
	}
	if group.Revision <= 0 {
		return ErrChannelLogicalGroupInvalidRevision
	}
	if group.CreatedAt < 0 || group.UpdatedAt < 0 {
		return fmt.Errorf("%w: 时间戳无效", ErrChannelLogicalGroupInvalidRevision)
	}
	return nil
}

func (member *ChannelLogicalGroupMember) BeforeCreate(_ *gorm.DB) error {
	member.AddressFingerprint = strings.ToLower(strings.TrimSpace(member.AddressFingerprint))
	if member.CreatedAt == 0 {
		member.CreatedAt = common.GetTimestamp()
	}
	if member.UpdatedAt == 0 {
		member.UpdatedAt = member.CreatedAt
	}
	return member.Validate()
}

func (member ChannelLogicalGroupMember) Validate() error {
	if member.LogicalGroupID <= 0 || member.ChannelID <= 0 {
		return ErrChannelLogicalGroupInvalidMember
	}
	if err := ValidateChannelLogicalGroupMemberWeight(member.Weight); err != nil {
		return err
	}
	if !validChannelLogicalGroupAddressFingerprint(member.AddressFingerprint) {
		return ErrChannelLogicalGroupInvalidFingerprint
	}
	if member.CreatedAt < 0 || member.UpdatedAt < 0 {
		return ErrChannelLogicalGroupInvalidMember
	}
	return nil
}

func ValidateChannelLogicalGroupMemberWeight(weight uint) error {
	if weight > ChannelLogicalGroupMaxMemberWeight {
		return ErrChannelLogicalGroupInvalidWeight
	}
	return nil
}

// NormalizeChannelLogicalGroupMemberWeight applies the product default when a
// request omits weight. A pointer is used so an explicit zero (which means
// “participate only when all configured weights are zero”) is preserved.
func NormalizeChannelLogicalGroupMemberWeight(weight *uint) (uint, error) {
	if weight == nil {
		return ChannelLogicalGroupDefaultMemberWeight, nil
	}
	if err := ValidateChannelLogicalGroupMemberWeight(*weight); err != nil {
		return 0, err
	}
	return *weight, nil
}

func validChannelLogicalGroupAddressFingerprint(fingerprint string) bool {
	fingerprint = strings.TrimSpace(fingerprint)
	if len(fingerprint) != channelLogicalGroupAddressFingerprintLength*2 {
		return false
	}
	if _, err := hex.DecodeString(fingerprint); err != nil {
		return false
	}
	return true
}

// ValidateChannelLogicalGroupMembers validates a complete replacement set. It
// catches duplicate channels before a database write and leaves enforcement of
// the unique channel index as a second line of defense.
func ValidateChannelLogicalGroupMembers(members []ChannelLogicalGroupMember) error {
	if len(members) == 0 {
		return ErrChannelLogicalGroupEmptyMembers
	}
	seen := make(map[int]struct{}, len(members))
	for _, member := range members {
		if _, exists := seen[member.ChannelID]; exists {
			return ErrChannelLogicalGroupDuplicateMember
		}
		seen[member.ChannelID] = struct{}{}
		if err := member.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// BumpRevision advances the optimistic-concurrency token used when replacing
// group members or shared configuration. The operation is deliberately kept
// in-memory; callers should persist it with a revision-guarded UPDATE inside
// their transaction.
func (group *ChannelLogicalGroup) BumpRevision(now int64) error {
	if group == nil || group.Revision <= 0 || group.Revision == math.MaxInt64 {
		return ErrChannelLogicalGroupInvalidRevision
	}
	group.Revision++
	if now > 0 {
		group.UpdatedAt = now
	}
	return nil
}

// CheckChannelLogicalGroupRevision returns a stable conflict error for stale
// member/configuration replacements. Callers creating a new group should skip
// this check; a persisted group always has a positive revision.
func CheckChannelLogicalGroupRevision(actual, expected int64) error {
	if actual <= 0 || expected < 0 || actual != expected {
		return ErrChannelLogicalGroupRevisionConflict
	}
	return nil
}
