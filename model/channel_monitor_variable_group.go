package model

import (
	"context"
	"errors"
	"math"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ChannelMonitorVariableGroup owns requests and their current credentials.
// Channels store only a variable_group_id in their custom configuration.
type ChannelMonitorVariableGroup struct {
	ID             int    `gorm:"primaryKey" json:"id"`
	Name           string `gorm:"size:80;not null" json:"name"`
	BaseURL        string `gorm:"type:text;not null" json:"base_url"`
	Proxy          string `gorm:"type:text" json:"-"`
	RequestTimeout int    `json:"request_timeout"`
	Config         string `gorm:"type:text;not null" json:"-"`
	Revision       int64  `gorm:"not null" json:"revision"`
}

var ErrChannelMonitorVariableGroupChanged = errors.New("共享请求与变量已变更，请重新打开配置后重试")

func GetChannelMonitorVariableGroup(ctx context.Context, id int) (ChannelMonitorVariableGroup, error) {
	var group ChannelMonitorVariableGroup
	err := DB.WithContext(ctx).First(&group, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return group, errors.New("共享请求与变量不存在，请重新选择")
	}
	return group, err
}

func ListChannelMonitorVariableGroups(ctx context.Context) ([]ChannelMonitorVariableGroup, error) {
	groups := make([]ChannelMonitorVariableGroup, 0)
	err := DB.WithContext(ctx).Order("id ASC").Find(&groups).Error
	return groups, err
}

func channelMonitorVariableGroupID(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	var config struct {
		VariableGroupID int `json:"variable_group_id"`
	}
	err := common.UnmarshalJsonStr(raw, &config)
	return config.VariableGroupID, err
}

// Lock the referenced group before channel rows, matching the edit/delete lock
// order so a channel cannot acquire a reference to a concurrently deleted group.
func lockChannelMonitorVariableGroupReference(tx *gorm.DB, upstreamType, raw string, revision int64) error {
	if upstreamType != "custom" {
		return nil
	}
	id, err := channelMonitorVariableGroupID(raw)
	if err != nil || id == 0 {
		return err
	}
	var group ChannelMonitorVariableGroup
	if err := lockForUpdate(tx).First(&group, id).Error; err != nil {
		return errors.New("共享请求与变量不存在，请重新选择")
	}
	if revision > 0 && group.Revision != revision {
		return ErrChannelMonitorVariableGroupChanged
	}
	return nil
}

// SaveChannelMonitorVariableGroup compares the full saved row, including
// refreshed values, so an editor cannot overwrite credentials refreshed since
// it read them. validate protects the templates of all referencing channels.
func SaveChannelMonitorVariableGroup(ctx context.Context, group *ChannelMonitorVariableGroup, expected *ChannelMonitorVariableGroup, validate func([]ChannelRatioMonitor) error) error {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	return DB.WithContext(ctx).Session(&gorm.Session{Logger: DB.Logger.LogMode(logger.Silent)}).Transaction(func(tx *gorm.DB) error {
		if group.ID == 0 {
			group.Revision = 1
			return tx.Create(group).Error
		}
		var current ChannelMonitorVariableGroup
		if err := lockForUpdate(tx).First(&current, group.ID).Error; err != nil {
			return err
		}
		if expected == nil || current != *expected || group.Revision != current.Revision || current.Revision == math.MaxInt64 {
			return ErrChannelMonitorVariableGroupChanged
		}
		monitors, err := channelMonitorVariableGroupReferences(tx, group.ID)
		if err != nil {
			return err
		}
		if err := validate(monitors); err != nil {
			return err
		}
		for _, monitor := range monitors {
			if monitor.UpstreamRevision == math.MaxInt64 {
				return ErrChannelRatioMonitorConfigChanged
			}
			if err := tx.Model(&ChannelRatioMonitor{}).Where("channel_id = ?", monitor.ChannelId).
				Update("upstream_revision", gorm.Expr("upstream_revision + ?", 1)).Error; err != nil {
				return err
			}
		}
		group.Revision++
		return tx.Save(group).Error
	})
}

func channelMonitorVariableGroupReferences(tx *gorm.DB, id int) ([]ChannelRatioMonitor, error) {
	var monitors []ChannelRatioMonitor
	if err := tx.Where("upstream_type = ?", "custom").Find(&monitors).Error; err != nil {
		return nil, err
	}
	references := make([]ChannelRatioMonitor, 0)
	for _, monitor := range monitors {
		groupID, err := channelMonitorVariableGroupID(monitor.CustomUpstreamConfig)
		if err != nil {
			return nil, errors.New("渠道自定义配置无效，无法检查共享变量引用")
		}
		if groupID == id {
			references = append(references, monitor)
		}
	}
	return references, nil
}

func DeleteChannelMonitorVariableGroup(ctx context.Context, id int, revision int64) error {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var group ChannelMonitorVariableGroup
		if err := lockForUpdate(tx).First(&group, id).Error; err != nil {
			return err
		}
		if group.Revision != revision {
			return ErrChannelMonitorVariableGroupChanged
		}
		references, err := channelMonitorVariableGroupReferences(tx, id)
		if err != nil {
			return err
		}
		if len(references) > 0 {
			return errors.New("共享配置仍被渠道引用，请先解除引用后再删除")
		}
		return tx.Delete(&group).Error
	})
}

func RefreshChannelMonitorVariableGroup(ctx context.Context, expected ChannelMonitorVariableGroup, config string) error {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	return DB.WithContext(ctx).Session(&gorm.Session{Logger: DB.Logger.LogMode(logger.Silent)}).Transaction(func(tx *gorm.DB) error {
		var current ChannelMonitorVariableGroup
		if err := lockForUpdate(tx).First(&current, expected.ID).Error; err != nil {
			return err
		}
		if current != expected {
			return ErrChannelMonitorVariableGroupChanged
		}
		return tx.Model(&current).Update("config", config).Error
	})
}
