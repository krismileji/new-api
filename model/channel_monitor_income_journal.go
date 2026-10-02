package model

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// Every node must use the same persistent directory in a cluster. Files are
// immutable day/channel markers, so another node or a restarted process can
// read them even when the database write that triggered them never succeeded.
func channelMonitorIncomeGapDirectory() string {
	if path := os.Getenv("CHANNEL_MONITOR_INCOME_GAP_DIR"); path != "" {
		return path
	}
	return filepath.Join(*common.LogDir, "channel-monitor-income-gaps")
}

func persistChannelMonitorIncomeGap(gap ChannelMonitorIncomeGap) error {
	dir := channelMonitorIncomeGapDirectory()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	name := fmt.Sprintf("%d_%d.gap", gap.From, gap.ChannelID)
	if gap.CostEventID != "" {
		name = fmt.Sprintf("%d_%d_%s.gap", gap.From, gap.ChannelID, hex.EncodeToString([]byte(gap.CostEventID)))
	}
	file, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	// The filename is the complete record: no partially written JSON can
	// erase attribution, and simultaneous writers never overwrite content.
	err = file.Sync()
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	// POSIX also requires syncing the directory entry after creating a file.
	// Windows FlushFileBuffers on the file handles the supported flush path.
	if runtime.GOOS != "windows" {
		directory, err := os.Open(dir)
		if err != nil {
			return err
		}
		defer directory.Close()
		return directory.Sync()
	}
	return nil
}

func ReadChannelMonitorIncomeGapJournal() ([]ChannelMonitorIncomeGap, error) {
	dir, err := os.Open(channelMonitorIncomeGapDirectory())
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(10001)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > 10000 {
		return nil, fmt.Errorf("收入缺口日志超过读取上限")
	}
	var gaps []ChannelMonitorIncomeGap
	for _, entry := range entries {
		gap, err := parseChannelMonitorIncomeGapEntry(entry)
		if err != nil {
			return nil, err
		}
		gaps = append(gaps, gap)
	}
	return resolveChannelMonitorIncomeGapDates(gaps)
}

// The immutable journal keeps the event identity; its durable gap row keeps
// the final reporting day even after the cost outbox itself is retained away.
func resolveChannelMonitorIncomeGapDates(gaps []ChannelMonitorIncomeGap) ([]ChannelMonitorIncomeGap, error) {
	var keys []string
	for _, gap := range gaps {
		if gap.CostEventID != "" {
			keys = append(keys, gap.GapKey)
		}
	}
	if len(keys) == 0 || DB == nil {
		return gaps, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var persisted []ChannelMonitorIncomeGap
	if err := DB.WithContext(ctx).Where("gap_key IN ?", keys).Find(&persisted).Error; err != nil {
		return nil, err
	}
	byKey := make(map[string]ChannelMonitorIncomeGap, len(persisted))
	for _, gap := range persisted {
		byKey[gap.GapKey] = gap
	}
	for i := range gaps {
		if gap, exists := byKey[gaps[i].GapKey]; exists && gap.To < math.MaxInt64 {
			gaps[i].From, gaps[i].To = gap.From, gap.To
		}
	}
	return gaps, nil
}

func parseChannelMonitorIncomeGapEntry(entry os.DirEntry) (ChannelMonitorIncomeGap, error) {
	name, ok := strings.CutSuffix(entry.Name(), ".gap")
	if !ok || entry.IsDir() {
		return ChannelMonitorIncomeGap{}, fmt.Errorf("收入缺口日志格式无效")
	}
	dayText, channelText, ok := strings.Cut(name, "_")
	channelText, eventText, hasEvent := strings.Cut(channelText, "_")
	day, dayErr := strconv.ParseInt(dayText, 10, 64)
	channel, channelErr := strconv.Atoi(channelText)
	if !ok || dayErr != nil || channelErr != nil || day <= 0 || channel < 0 || ChannelDailyCostDayStart(day) != day {
		return ChannelMonitorIncomeGap{}, fmt.Errorf("收入缺口日志归属无效")
	}
	gap := ChannelMonitorIncomeGap{GapKey: fmt.Sprintf("%d:%d", day, channel), ChannelID: channel, From: day, To: day + 86400}
	if hasEvent {
		event, err := hex.DecodeString(eventText)
		if err != nil || len(event) == 0 || len(event) > 64 {
			return ChannelMonitorIncomeGap{}, fmt.Errorf("收入缺口成本事件无效")
		}
		gap.CostEventID, gap.To = string(event), math.MaxInt64
		gap.GapKey = ChannelMonitorIncomeKey(gap.CostEventID, "gap")
	}
	return gap, nil
}

// Cleanup must be able to reduce a journal that exceeds the query limit.
// Keep unrecognized entries as evidence; they still block profit confirmation,
// but do not prevent deleting unrelated expired markers or database history.
func cleanupChannelMonitorIncomeGapJournal(ctx context.Context, cutoff int64, budget ChannelMonitorCleanupBudget) (bool, error) {
	dir, err := os.Open(channelMonitorIncomeGapDirectory())
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	defer dir.Close()
	incomplete := false
	for {
		if err := ctx.Err(); err != nil {
			return true, err
		}
		if budget.Exhausted() {
			return true, nil
		}
		entries, err := dir.ReadDir(128)
		if err != nil && err != io.EOF {
			return true, err
		}
		if len(entries) == 0 {
			return incomplete, nil
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return true, err
			}
			if budget.Exhausted() {
				return true, nil
			}
			gap, parseErr := parseChannelMonitorIncomeGapEntry(entry)
			if parseErr != nil {
				incomplete = true
				continue
			}
			if gap.CostEventID != "" {
				resolved, err := resolveChannelMonitorIncomeGapDates([]ChannelMonitorIncomeGap{gap})
				if err != nil {
					return true, err
				}
				gap = resolved[0]
			}
			if gap.To <= cutoff {
				if err := os.Remove(filepath.Join(dir.Name(), entry.Name())); err != nil && !os.IsNotExist(err) {
					return true, err
				}
			}
		}
	}
}

func RestoreChannelMonitorIncomeGaps(ctx context.Context) error {
	gaps, err := ReadChannelMonitorIncomeGapJournal()
	if err != nil {
		return err
	}
	for _, gap := range gaps {
		channelMonitorPendingIncomeGaps.Store(gap.GapKey, gap)
	}
	FlushChannelMonitorIncomeGaps(ctx)
	return nil
}
