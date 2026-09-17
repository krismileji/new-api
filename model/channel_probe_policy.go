package model

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"gorm.io/gorm"
)

var ErrChannelProbePolicyConflict = errors.New("渠道探测策略已变更，请刷新后重试")

// ChannelProbePolicy belongs to a physical channel, never to its logical group.
type ChannelProbePolicy struct {
	AutoProbeDisabled         bool   `json:"auto_probe_disabled"`
	SmallInputResponseEnabled bool   `json:"small_input_response_enabled"`
	SmallInputThresholdTokens int    `json:"small_input_threshold_tokens"`
	SmallInputResponseText    string `json:"small_input_response_text"`
	ProbePolicyRevision       int64  `json:"probe_policy_revision"`
	ProbePolicyUpdatedAt      int64  `json:"probe_policy_updated_at"`
}

func (monitor ChannelRatioMonitor) ProbePolicy() ChannelProbePolicy {
	return ChannelProbePolicy{
		AutoProbeDisabled:         monitor.AutoProbeDisabled,
		SmallInputResponseEnabled: monitor.SmallInputResponseEnabled,
		SmallInputThresholdTokens: monitor.SmallInputThresholdTokens,
		SmallInputResponseText:    monitor.SmallInputResponseText,
		ProbePolicyRevision:       monitor.ProbePolicyRevision,
		ProbePolicyUpdatedAt:      monitor.ProbePolicyUpdatedAt,
	}
}

func (policy *ChannelProbePolicy) Normalize() error {
	if policy.ProbePolicyRevision < 0 {
		return errors.New("探测策略修订号不能为负数")
	}
	if !policy.AutoProbeDisabled {
		policy.SmallInputResponseEnabled = false
	}
	if policy.SmallInputThresholdTokens < 0 || policy.SmallInputThresholdTokens > constant.ChannelProbeMaxInputTokens {
		return fmt.Errorf("输入阈值必须在 0 到 %d tokens 之间", constant.ChannelProbeMaxInputTokens)
	}
	if !utf8.ValidString(policy.SmallInputResponseText) || utf8.RuneCountInString(policy.SmallInputResponseText) > constant.ChannelProbeMaxResponseTextLength {
		return fmt.Errorf("响应文本必须是有效文本且不能超过 %d 个字符", constant.ChannelProbeMaxResponseTextLength)
	}
	if policy.SmallInputResponseEnabled && (policy.SmallInputThresholdTokens == 0 || strings.TrimSpace(policy.SmallInputResponseText) == "") {
		return errors.New("开启小输入响应需要正数阈值和非空响应文本")
	}
	return nil
}

// Read the authoritative row. A stale cache must not permit automatic dispatch
// after an administrator disabled probes on another node.
func GetChannelProbePolicy(ctx context.Context, channelID int) (ChannelProbePolicy, error) {
	return GetChannelProbePolicyWithDB(ctx, DB, channelID)
}

func GetChannelProbePolicyWithDB(ctx context.Context, db *gorm.DB, channelID int) (ChannelProbePolicy, error) {
	var policy ChannelProbePolicy
	if db == nil {
		return policy, errors.New("渠道探测策略数据库不可用")
	}
	var channel Channel
	if err := db.WithContext(ctx).Select("id").First(&channel, channelID).Error; err != nil {
		return policy, err
	}
	err := db.WithContext(ctx).Model(&ChannelRatioMonitor{}).Where("channel_id = ?", channelID).Take(&policy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	return policy, err
}

func InsertChannelWithProbePolicyFrom(channel *Channel, sourceID int) error {
	return channel.InsertWithTransactionHook(func(tx *gorm.DB) error {
		if err := lockChannelForDependentWriteTx(tx, sourceID); err != nil {
			return err
		}
		policy, err := GetChannelProbePolicyWithDB(tx.Statement.Context, tx, sourceID)
		if err != nil {
			return err
		}
		if policy.ProbePolicyRevision == 0 {
			return nil
		}
		return tx.Create(&ChannelRatioMonitor{
			ChannelId: channel.Id, AutoProbeDisabled: policy.AutoProbeDisabled,
			SmallInputResponseEnabled: policy.SmallInputResponseEnabled,
			SmallInputThresholdTokens: policy.SmallInputThresholdTokens,
			SmallInputResponseText:    policy.SmallInputResponseText,
			ProbePolicyRevision:       1, ProbePolicyUpdatedAt: common.GetTimestamp(),
		}).Error
	})
}

func SaveChannelProbePolicy(ctx context.Context, channelID int, policy ChannelProbePolicy) (before ChannelProbePolicy, after ChannelProbePolicy, err error) {
	if err = policy.Normalize(); err != nil {
		return
	}
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	err = DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockChannelForDependentWriteTx(tx, channelID); err != nil {
			return err
		}
		var monitor ChannelRatioMonitor
		findErr := lockForUpdate(tx).Where("channel_id = ?", channelID).First(&monitor).Error
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		before = monitor.ProbePolicy()
		if before.ProbePolicyRevision != policy.ProbePolicyRevision {
			return ErrChannelProbePolicyConflict
		}
		if policy.ProbePolicyRevision == math.MaxInt64 {
			return errors.New("探测策略修订号已达上限")
		}
		policy.ProbePolicyRevision++
		policy.ProbePolicyUpdatedAt = common.GetTimestamp()
		after = policy
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			monitor.ChannelId = channelID
			if err := tx.Create(&monitor).Error; err != nil {
				return err
			}
		}
		return tx.Model(&ChannelRatioMonitor{}).Where("id = ?", monitor.Id).Updates(map[string]any{
			"auto_probe_disabled":          policy.AutoProbeDisabled,
			"small_input_response_enabled": policy.SmallInputResponseEnabled,
			"small_input_threshold_tokens": policy.SmallInputThresholdTokens,
			"small_input_response_text":    policy.SmallInputResponseText,
			"probe_policy_revision":        policy.ProbePolicyRevision,
			"probe_policy_updated_at":      policy.ProbePolicyUpdatedAt,
		}).Error
	})
	return
}
