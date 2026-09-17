package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

// The local outbox preserves a revocation across a restart while SQL is down.
// Deployments with external SQL should persist TOKEN_AUTO_DISABLE_JOURNAL_DIR.
func (m *tokenProtectionManager) openJournal(ctx context.Context) error {
	m.journalDir = os.Getenv("TOKEN_AUTO_DISABLE_JOURNAL_DIR")
	if m.journalDir == "" {
		m.journalDir = filepath.Join(filepath.Dir(common.SQLitePath), "token-auto-disable-journal")
	}
	if err := os.MkdirAll(m.journalDir, 0700); err != nil {
		return err
	}
	entries, err := os.ReadDir(m.journalDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.journalDir, entry.Name()))
		if err != nil {
			return err
		}
		var record model.TokenAutoDisableRecord
		if err := common.Unmarshal(data, &record); err != nil {
			return fmt.Errorf("禁用恢复记录无效: %w", err)
		}
		if record.Id+".json" != entry.Name() || record.TokenId <= 0 || record.ResponseStatus < 400 || record.ResponseStatus > 599 {
			return errors.New("禁用恢复记录字段无效")
		}
		var saved model.TokenAutoDisableRecord
		err = model.DB.WithContext(ctx).First(&saved, "id = ?", record.Id).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil {
			// A stale file must never undo an administrator's committed release.
			if err := os.Remove(filepath.Join(m.journalDir, entry.Name())); err != nil {
				return err
			}
			continue
		}
		if _, newer := m.blocks[record.TokenId]; !newer {
			m.blocks[record.TokenId] = &tokenProtectionBlock{cause: &TokenAutoDisableCause{Record: record}}
		}
	}
	return nil
}

func (m *tokenProtectionManager) journal(record model.TokenAutoDisableRecord) error {
	data, err := common.Marshal(record)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(m.journalDir, ".pending-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temporary, filepath.Join(m.journalDir, record.Id+".json"))
}
