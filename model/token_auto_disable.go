package model

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TokenAutoDisableConfig is a versioned singleton. Rules use TEXT on all dialects.
type TokenAutoDisableConfig struct {
	Id       int    `json:"-" gorm:"primaryKey;autoIncrement:false"`
	Revision int64  `json:"revision"`
	Enabled  bool   `json:"enabled"`
	Rules    string `json:"-" gorm:"type:text"`
}

// Each incident is retained after release. The single-instance service serializes
// transitions for a token; Id also makes persistence retries idempotent.
type TokenAutoDisableRecord struct {
	Id               string `json:"id" gorm:"primaryKey;type:varchar(36)"`
	TokenId          int    `json:"token_id" gorm:"index:idx_token_auto_disable_active,priority:1"`
	UserId           int    `json:"user_id"`
	TokenName        string `json:"token_name" gorm:"type:varchar(128)"`
	RuleId           string `json:"rule_id" gorm:"type:varchar(64)"`
	RuleName         string `json:"rule_name" gorm:"type:varchar(128)"`
	RuleSnapshot     string `json:"rule_snapshot" gorm:"type:text"`
	ChannelId        int    `json:"channel_id"`
	RequestId        string `json:"request_id" gorm:"type:varchar(128)"`
	UpstreamStatus   int    `json:"upstream_status"`
	ErrorSummary     string `json:"error_summary" gorm:"type:text"`
	ResponseStatus   int    `json:"response_status"`
	ResponseMessage  string `json:"response_message" gorm:"type:text"`
	CanceledRequests int    `json:"canceled_requests"`
	CreatedAt        int64  `json:"created_at" gorm:"index"`
	ReleasedAt       int64  `json:"released_at" gorm:"index:idx_token_auto_disable_active,priority:2"`
	ReleasedBy       int    `json:"released_by"`
}

var ErrTokenAutoDisableConflict = errors.New("配置或禁用记录已变更，请刷新后重试")

func LoadTokenAutoDisableConfig(ctx context.Context) (TokenAutoDisableConfig, error) {
	config := TokenAutoDisableConfig{Id: 1, Rules: "[]"}
	err := DB.WithContext(ctx).First(&config, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return config, nil
	}
	return config, err
}

func SaveTokenAutoDisableConfig(ctx context.Context, config TokenAutoDisableConfig) error {
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&TokenAutoDisableConfig{Id: 1, Rules: "[]"}).Error; err != nil {
			return err
		}
		result := tx.Model(&TokenAutoDisableConfig{}).Where("id = ? AND revision = ?", 1, config.Revision).
			Updates(map[string]any{"enabled": config.Enabled, "rules": config.Rules, "revision": config.Revision + 1})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTokenAutoDisableConflict
		}
		return nil
	})
}

func LoadActiveTokenAutoDisables(ctx context.Context) ([]TokenAutoDisableRecord, error) {
	var records []TokenAutoDisableRecord
	err := DB.WithContext(ctx).Where("released_at = ?", 0).Find(&records).Error
	return records, err
}

func PersistTokenAutoDisable(ctx context.Context, record TokenAutoDisableRecord) error {
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var token Token
		if err := lockForUpdate(tx).Unscoped().First(&token, record.TokenId).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&record).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Model(&Token{}).Where("id = ?", record.TokenId).Update("status", common.TokenStatusDisabled).Error; err != nil {
			return err
		}
		// Cache failure cannot reopen the independent in-process deny gate.
		if err := invalidateTokenCacheForMutation(token.Key); err != nil {
			common.SysError("自动禁用 API Key 缓存失效失败: " + err.Error())
		}
		return nil
	})
}

func ReleaseTokenAutoDisable(ctx context.Context, id string, actor int) error {
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record TokenAutoDisableRecord
		if err := tx.First(&record, "id = ?", id).Error; err != nil {
			return err
		}
		if record.ReleasedAt != 0 {
			return ErrTokenAutoDisableConflict
		}
		var token Token
		if err := lockForUpdate(tx).Unscoped().First(&token, record.TokenId).Error; err != nil {
			return err
		}
		result := tx.Model(&TokenAutoDisableRecord{}).Where("id = ? AND released_at = ?", id, 0).
			Updates(map[string]any{"released_at": time.Now().Unix(), "released_by": actor})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTokenAutoDisableConflict
		}
		// Releasing this protection never restores quota, expiry, or deleted keys.
		status := common.TokenStatusEnabled
		if token.ExpiredTime != -1 && token.ExpiredTime < time.Now().Unix() {
			status = common.TokenStatusExpired
		} else if !token.UnlimitedQuota && token.RemainQuota <= 0 {
			status = common.TokenStatusExhausted
		}
		if err := tx.Unscoped().Model(&Token{}).Where("id = ? AND status = ?", record.TokenId, common.TokenStatusDisabled).Update("status", status).Error; err != nil {
			return err
		}
		if err := invalidateTokenCacheForMutation(token.Key); err != nil {
			return err // Keep the block until a release can safely refresh authentication.
		}
		return nil
	})
}

func ListTokenAutoDisables(ctx context.Context, offset, limit int) ([]TokenAutoDisableRecord, int64, error) {
	var records []TokenAutoDisableRecord
	var total int64
	query := DB.WithContext(ctx).Model(&TokenAutoDisableRecord{})
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("created_at DESC, id DESC").Offset(offset).Limit(limit).Find(&records).Error
	return records, total, err
}
