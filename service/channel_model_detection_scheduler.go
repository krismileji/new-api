package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const channelModelDetectionScheduleMaxCandidateRuns = 10000

var (
	ErrChannelModelDetectionScheduleNotConfigured = errors.New("模型检测统一定时配置不存在")
	ErrChannelModelDetectionScheduleInvalid       = errors.New("模型检测统一定时配置无效")
	ErrChannelModelDetectionRunAlreadyActive      = errors.New("渠道已有活动模型检测轮次")
	ErrChannelModelDetectionNoEnabledTargets      = errors.New("渠道没有已启用的模型检测目标")
	ErrChannelModelDetectionManualHighUnconfirmed = errors.New("手动高档模型检测需要确认本次成本风险")
)

// ChannelModelDetectionManualRunInput contains the command-only values used
// to freeze a manual run. High-cost confirmation is intentionally not stored.
type ChannelModelDetectionManualRunInput struct {
	ChannelID         int
	Preset            string
	ConfirmHighCost   bool
	CreatedByUserID   int
	CreatedByUsername string
}

// ChannelModelDetectionScheduleResult describes one scheduler pass. A due
// schedule may be skipped because an older batch is still active; that still
// advances NextBatchAt so historical periods are never replayed one by one.
type ChannelModelDetectionScheduleResult struct {
	Due               bool
	Created           bool
	SkippedForBacklog bool
	ScheduledFor      int64
	NextBatchAt       int64
	Batch             *model.ChannelModelDetectionBatch
	RunIDs            []string
}

// CalculateChannelModelDetectionScheduleAnchor returns the first configured
// wall-clock time strictly after now. Go resolves nonexistent local times to a
// valid instant in the selected IANA zone; the round-trip guard below then
// advances minute-by-minute when that resolution moves backward.
func CalculateChannelModelDetectionScheduleAnchor(now time.Time, scheduleTime, timezone string) (time.Time, error) {
	parsed, err := time.Parse("15:04", strings.TrimSpace(scheduleTime))
	if err != nil {
		return time.Time{}, ErrChannelModelDetectionScheduleInvalid
	}
	location, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		return time.Time{}, ErrChannelModelDetectionScheduleInvalid
	}
	localNow := now.In(location)
	candidate := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), parsed.Hour(), parsed.Minute(), 0, 0, location)
	if candidate.Hour() != parsed.Hour() || candidate.Minute() != parsed.Minute() {
		requestedMinutes := parsed.Hour()*60 + parsed.Minute()
		for step := 0; step <= 180; step++ {
			probe := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, requestedMinutes+step, 0, 0, location)
			if probe.Hour()*60+probe.Minute() >= requestedMinutes {
				candidate = probe
				break
			}
		}
	}
	if !candidate.After(now) {
		nextLocalDay := localNow.AddDate(0, 0, 1)
		candidate = time.Date(nextLocalDay.Year(), nextLocalDay.Month(), nextLocalDay.Day(), parsed.Hour(), parsed.Minute(), 0, 0, location)
		if candidate.Hour() != parsed.Hour() || candidate.Minute() != parsed.Minute() {
			requestedMinutes := parsed.Hour()*60 + parsed.Minute()
			for step := 0; step <= 180; step++ {
				probe := time.Date(nextLocalDay.Year(), nextLocalDay.Month(), nextLocalDay.Day(), 0, requestedMinutes+step, 0, 0, location)
				if probe.Hour()*60+probe.Minute() >= requestedMinutes {
					candidate = probe
					break
				}
			}
		}
	}
	return candidate.UTC(), nil
}

// NextChannelModelDetectionSchedule advances from the frozen UTC anchor. When
// several periods were missed, it returns only the most recent due instant and
// the first future instant, preventing an unbounded catch-up queue.
func NextChannelModelDetectionSchedule(anchor time.Time, intervalHours int, now time.Time) (scheduledFor time.Time, next time.Time, err error) {
	return NextChannelModelDetectionScheduleInTimezone(anchor, intervalHours, now, "UTC")
}

func nextChannelModelDetectionIntervalBoundary(after time.Time, intervalMinutes int) (time.Time, error) {
	if intervalMinutes < model.ChannelModelDetectionMinIntervalMinutes || intervalMinutes > model.ChannelModelDetectionMaxIntervalMinutes {
		return time.Time{}, ErrChannelModelDetectionScheduleInvalid
	}
	intervalSeconds := int64(intervalMinutes) * int64(time.Minute/time.Second)
	timestamp := after.UTC().Unix()
	if timestamp < 0 {
		return time.Time{}, ErrChannelModelDetectionScheduleInvalid
	}
	return time.Unix(timestamp-timestamp%intervalSeconds+intervalSeconds, 0).UTC(), nil
}

func NextChannelModelDetectionScheduleMinutes(nextRunAt time.Time, intervalMinutes int, now time.Time) (scheduledFor time.Time, next time.Time, err error) {
	if nextRunAt.IsZero() || intervalMinutes < model.ChannelModelDetectionMinIntervalMinutes || intervalMinutes > model.ChannelModelDetectionMaxIntervalMinutes {
		return time.Time{}, time.Time{}, ErrChannelModelDetectionScheduleInvalid
	}
	intervalSeconds := int64(intervalMinutes) * int64(time.Minute/time.Second)
	nextRunAt = nextRunAt.UTC().Truncate(time.Second)
	if nextRunAt.Unix()%intervalSeconds != 0 {
		nextRunAt, err = nextChannelModelDetectionIntervalBoundary(nextRunAt, intervalMinutes)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	now = now.UTC()
	if now.Before(nextRunAt) {
		return time.Time{}, nextRunAt, nil
	}
	interval := time.Duration(intervalMinutes) * time.Minute
	steps := int64(now.Sub(nextRunAt) / interval)
	scheduledFor = nextRunAt.Add(time.Duration(steps) * interval)
	next = scheduledFor.Add(interval)
	return scheduledFor, next, nil
}

// NextChannelModelDetectionScheduleInTimezone preserves the configured local
// wall-clock phase across daylight-saving transitions. A repeated local time
// maps to one instant, and a nonexistent local time is shifted to the first
// valid minute after the gap.
func NextChannelModelDetectionScheduleInTimezone(anchor time.Time, intervalHours int, now time.Time, timezone string) (scheduledFor time.Time, next time.Time, err error) {
	if anchor.IsZero() || intervalHours <= 0 {
		return time.Time{}, time.Time{}, ErrChannelModelDetectionScheduleInvalid
	}
	interval := time.Duration(intervalHours) * time.Hour
	if interval <= 0 {
		return time.Time{}, time.Time{}, ErrChannelModelDetectionScheduleInvalid
	}
	location, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		return time.Time{}, time.Time{}, ErrChannelModelDetectionScheduleInvalid
	}
	anchor = anchor.UTC()
	now = now.UTC()
	if now.Before(anchor) {
		return time.Time{}, anchor, nil
	}
	localAnchor := anchor.In(location)
	localNow := now.In(location)
	anchorDate := time.Date(localAnchor.Year(), localAnchor.Month(), localAnchor.Day(), 0, 0, 0, 0, time.UTC)
	nowDate := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.UTC)
	dayMinutes := int64(nowDate.Sub(anchorDate) / time.Minute)
	wallMinutes := dayMinutes + int64(localNow.Hour()*60+localNow.Minute()-localAnchor.Hour()*60-localAnchor.Minute())
	steps := wallMinutes / int64(intervalHours*60)
	if steps < 0 {
		steps = 0
	}
	scheduledFor = channelModelDetectionWallClockStep(anchor, steps, intervalHours, location)
	for scheduledFor.After(now) && steps > 0 {
		steps--
		scheduledFor = channelModelDetectionWallClockStep(anchor, steps, intervalHours, location)
	}
	next = channelModelDetectionWallClockStep(anchor, steps+1, intervalHours, location)
	for !next.After(now) {
		steps++
		scheduledFor = next
		next = channelModelDetectionWallClockStep(anchor, steps+1, intervalHours, location)
	}
	return scheduledFor, next, nil
}

func channelModelDetectionWallClockStep(anchor time.Time, steps int64, intervalHours int, location *time.Location) time.Time {
	localAnchor := anchor.In(location)
	totalMinutes := int64(localAnchor.Hour()*60+localAnchor.Minute()) + steps*int64(intervalHours)*60
	dayOffset := totalMinutes / (24 * 60)
	minuteOfDay := totalMinutes % (24 * 60)
	if minuteOfDay < 0 {
		minuteOfDay += 24 * 60
		dayOffset--
	}
	day := localAnchor.AddDate(0, 0, int(dayOffset))
	candidate := time.Date(day.Year(), day.Month(), day.Day(), 0, int(minuteOfDay), 0, 0, location)
	if candidate.Hour()*60+candidate.Minute() >= int(minuteOfDay) {
		return candidate.UTC()
	}
	for step := int64(1); step <= 180; step++ {
		probe := time.Date(day.Year(), day.Month(), day.Day(), 0, int(minuteOfDay+step), 0, 0, location)
		if probe.Hour()*60+probe.Minute() >= int(minuteOfDay) {
			return probe.UTC()
		}
	}
	return candidate.UTC()
}

// RunChannelModelDetectionScheduleOnce claims the single global row using its
// revision lease, creates at most one due batch, and advances the schedule in
// the same transaction. A unique scheduled_for index is the final multi-node
// idempotency guard.
func RunChannelModelDetectionScheduleOnce(ctx context.Context, db *gorm.DB, now time.Time) (ChannelModelDetectionScheduleResult, error) {
	if db == nil {
		db = model.DB
	}
	if db == nil {
		return ChannelModelDetectionScheduleResult{}, errors.New("模型检测调度数据库不可用")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now = now.UTC()
	nowUnix := now.Unix()
	result := ChannelModelDetectionScheduleResult{}

	var config model.ChannelModelDetectionGlobalConfig
	findErr := db.WithContext(ctx).Where("id = ?", model.ChannelModelDetectionConfigID).First(&config).Error
	if errors.Is(findErr, gorm.ErrRecordNotFound) {
		return result, ErrChannelModelDetectionScheduleNotConfigured
	}
	if findErr != nil {
		return result, findErr
	}
	if err := config.Validate(); err != nil {
		return result, err
	}
	if !config.ScheduleEnabled || config.NextBatchAt <= 0 || config.NextBatchAt > nowUnix {
		result.NextBatchAt = config.NextBatchAt
		return result, nil
	}
	if strings.TrimSpace(config.DetectorURL) == "" {
		result.NextBatchAt = config.NextBatchAt
		return result, nil
	}

	leaseToken := common.GetUUID()
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		claimed, claimErr := config.TryAcquireLease(tx, config.Revision, nowUnix, leaseToken)
		if claimErr != nil || !claimed {
			return claimErr
		}
		var current model.ChannelModelDetectionGlobalConfig
		if err := tx.Where("id = ? AND revision = ? AND lease_token = ? AND lease_until > ?", config.Id, config.Revision, leaseToken, nowUnix).First(&current).Error; err != nil {
			return err
		}
		if err := current.Validate(); err != nil {
			return err
		}
		if !current.ScheduleEnabled || current.NextBatchAt <= 0 || current.NextBatchAt > nowUnix {
			result.NextBatchAt = current.NextBatchAt
			return nil
		}
		scheduledFor, next, scheduleErr := NextChannelModelDetectionScheduleMinutes(
			time.Unix(current.NextBatchAt, 0).UTC(), current.EffectiveIntervalMinutes(), now,
		)
		if scheduleErr != nil {
			return scheduleErr
		}
		if scheduledFor.IsZero() {
			result.NextBatchAt = next.Unix()
			updated := tx.Model(&model.ChannelModelDetectionGlobalConfig{}).
				Where("id = ? AND revision = ? AND lease_token = ?", current.Id, current.Revision, leaseToken).
				Updates(map[string]any{"next_batch_at": result.NextBatchAt, "lease_token": "", "lease_until": int64(0), "updated_at": nowUnix})
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return gorm.ErrRecordNotFound
			}
			return nil
		}
		result.Due = true
		result.ScheduledFor = scheduledFor.Unix()
		result.NextBatchAt = next.Unix()

		activeStatuses := []string{
			model.ChannelModelDetectionRunStatusQueued,
			model.ChannelModelDetectionRunStatusWaitingDetector,
			model.ChannelModelDetectionRunStatusSubmitting,
			model.ChannelModelDetectionRunStatusRunning,
			model.ChannelModelDetectionRunStatusSubmissionUnknown,
			model.ChannelModelDetectionRunStatusCanceling,
		}
		var activeScheduledRuns int64
		if err := tx.Model(&model.ChannelModelDetectionRun{}).
			Where("trigger = ? AND status IN ?", model.ChannelModelDetectionTriggerScheduled, activeStatuses).
			Count(&activeScheduledRuns).Error; err != nil {
			return err
		}
		if activeScheduledRuns > 0 {
			result.SkippedForBacklog = true
			updated := tx.Model(&model.ChannelModelDetectionGlobalConfig{}).
				Where("id = ? AND revision = ? AND lease_token = ?", current.Id, current.Revision, leaseToken).
				Updates(map[string]any{"next_batch_at": result.NextBatchAt, "lease_token": "", "lease_until": int64(0), "updated_at": nowUnix})
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return gorm.ErrRecordNotFound
			}
			return nil
		}

		batch := model.ChannelModelDetectionBatch{
			GlobalConfigRevision: current.Revision,
			Preset:               current.ScheduledPreset,
			ScheduledFor:         result.ScheduledFor,
			Status:               model.ChannelModelDetectionBatchStatusQueued,
			CreatedAt:            nowUnix,
			UpdatedAt:            nowUnix,
		}
		inserted := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&batch)
		if inserted.Error != nil {
			return inserted.Error
		}
		if inserted.RowsAffected == 0 {
			if err := tx.Where("scheduled_for = ?", result.ScheduledFor).First(&batch).Error; err != nil {
				return err
			}
		} else {
			createdRuns, createErr := createChannelModelDetectionScheduledRuns(tx, current, &batch, nowUnix)
			if createErr != nil {
				return createErr
			}
			result.RunIDs = createdRuns
			batch.ChannelCount = len(createdRuns)
			batch.RunCount = len(createdRuns)
			if err := tx.Model(&batch).Updates(map[string]any{
				"channel_count": batch.ChannelCount,
				"run_count":     batch.RunCount,
				"updated_at":    nowUnix,
			}).Error; err != nil {
				return err
			}
			result.Created = true
		}
		result.Batch = &batch

		updated := tx.Model(&model.ChannelModelDetectionGlobalConfig{}).
			Where("id = ? AND revision = ? AND lease_token = ?", current.Id, current.Revision, leaseToken).
			Updates(map[string]any{"next_batch_at": result.NextBatchAt, "lease_token": "", "lease_until": int64(0), "updated_at": nowUnix})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		_, _ = config.ReleaseLease(db.WithContext(context.Background()), config.Revision, leaseToken, time.Now().UTC().Unix())
	}
	return result, err
}

func createChannelModelDetectionScheduledRuns(tx *gorm.DB, global model.ChannelModelDetectionGlobalConfig, batch *model.ChannelModelDetectionBatch, now int64) ([]string, error) {
	type scheduledConfig struct {
		model.ChannelModelDetectionConfig
		ChannelStatus int `gorm:"column:channel_status"`
	}
	targetTable := tx.NamingStrategy.TableName("ChannelModelDetectionTarget")
	channelTable := tx.NamingStrategy.TableName("Channel")
	var configs []scheduledConfig
	if err := tx.Table(tx.NamingStrategy.TableName("ChannelModelDetectionConfig")+" AS detection_config").
		Select("detection_config.*, channel.status AS channel_status").
		Joins("JOIN "+channelTable+" AS channel ON channel.id = detection_config.channel_id").
		Joins("JOIN "+targetTable+" AS detection_target ON detection_target.config_id = detection_config.id AND detection_target.channel_id = detection_config.channel_id AND detection_target.enabled = ?", true).
		Where("detection_config.schedule_enabled = ?", true).
		Where("detection_config.running_run_id = ?", "").
		Where("channel.status <> ?", common.ChannelStatusManuallyDisabled).
		Group("detection_config.id").
		Order("detection_config.channel_id ASC").
		Limit(channelModelDetectionScheduleMaxCandidateRuns).
		Scan(&configs).Error; err != nil {
		return nil, err
	}
	runIDs := make([]string, 0, len(configs))
	for _, candidate := range configs {
		var targets []model.ChannelModelDetectionTarget
		if err := tx.Where("config_id = ? AND channel_id = ? AND enabled = ?", candidate.Id, candidate.ChannelId, true).
			Order("position ASC, id ASC").Find(&targets).Error; err != nil {
			return nil, err
		}
		if len(targets) == 0 {
			continue
		}
		run := model.ChannelModelDetectionRun{
			BatchId:              &batch.BatchId,
			ChannelId:            candidate.ChannelId,
			ConfigRevision:       candidate.Revision,
			GlobalConfigRevision: global.Revision,
			Trigger:              model.ChannelModelDetectionTriggerScheduled,
			Preset:               global.ScheduledPreset,
			PresetSource:         model.ChannelModelDetectionPresetSourceScheduledDefault,
			Status:               model.ChannelModelDetectionRunStatusQueued,
			TargetCount:          len(targets),
			QueuedAt:             now,
			CreatedAt:            now,
			UpdatedAt:            now,
		}
		created, err := model.CreateChannelModelDetectionRun(tx, &run)
		if err != nil {
			return nil, err
		}
		if !created {
			continue
		}
		for _, target := range targets {
			execution := model.ChannelModelDetectionExecution{
				RunId: run.RunId, TargetKey: target.TargetKey, TargetId: target.Id, ChannelId: target.ChannelId,
				RequestModel: target.RequestModel, ClaimedModel: target.ClaimedModel, Preset: run.Preset,
				Status: model.ChannelModelDetectionExecutionStatusPending, CreatedAt: now, UpdatedAt: now,
			}
			if err := tx.Create(&execution).Error; err != nil {
				return nil, err
			}
		}
		runIDs = append(runIDs, run.RunId)
	}
	return runIDs, nil
}

// CreateChannelModelDetectionManualRun freezes the chosen preset and current
// target/config revisions without modifying the global next_batch_at.
func CreateChannelModelDetectionManualRun(ctx context.Context, db *gorm.DB, input ChannelModelDetectionManualRunInput, now time.Time) (model.ChannelModelDetectionRun, error) {
	if db == nil {
		db = model.DB
	}
	if db == nil {
		return model.ChannelModelDetectionRun{}, errors.New("模型检测调度数据库不可用")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if input.ChannelID <= 0 {
		return model.ChannelModelDetectionRun{}, ErrChannelModelDetectionNoEnabledTargets
	}
	now = now.UTC()
	var createdRun model.ChannelModelDetectionRun
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var global model.ChannelModelDetectionGlobalConfig
		if err := tx.Where("id = ?", model.ChannelModelDetectionConfigID).First(&global).Error; err != nil {
			return err
		}
		preset := strings.ToLower(strings.TrimSpace(input.Preset))
		if preset == "" {
			preset = global.ScheduledPreset
		}
		if !model.IsChannelModelDetectionPreset(preset) {
			return model.ErrChannelModelDetectionInvalidPreset
		}
		if preset == model.ChannelModelDetectionPresetHigh && !input.ConfirmHighCost {
			return ErrChannelModelDetectionManualHighUnconfirmed
		}
		var config model.ChannelModelDetectionConfig
		if err := tx.Where("channel_id = ?", input.ChannelID).First(&config).Error; err != nil {
			return err
		}
		var targets []model.ChannelModelDetectionTarget
		if err := tx.Where("config_id = ? AND channel_id = ? AND enabled = ?", config.Id, input.ChannelID, true).
			Order("position ASC, id ASC").Find(&targets).Error; err != nil {
			return err
		}
		if len(targets) == 0 {
			return ErrChannelModelDetectionNoEnabledTargets
		}
		createdRun = model.ChannelModelDetectionRun{
			ChannelId: input.ChannelID, ConfigRevision: config.Revision, GlobalConfigRevision: global.Revision,
			Trigger: model.ChannelModelDetectionTriggerManual, Preset: preset,
			PresetSource: model.ChannelModelDetectionPresetSourceManualSelected,
			Status:       model.ChannelModelDetectionRunStatusQueued, TargetCount: len(targets),
			QueuedAt: now.Unix(), CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
			CreatedByUserId: input.CreatedByUserID, CreatedByUsername: strings.TrimSpace(input.CreatedByUsername),
		}
		created, err := model.CreateChannelModelDetectionRun(tx, &createdRun)
		if err != nil {
			return err
		}
		if !created {
			return ErrChannelModelDetectionRunAlreadyActive
		}
		for _, target := range targets {
			execution := model.ChannelModelDetectionExecution{
				RunId: createdRun.RunId, TargetKey: target.TargetKey, TargetId: target.Id, ChannelId: target.ChannelId,
				RequestModel: target.RequestModel, ClaimedModel: target.ClaimedModel, Preset: preset,
				Status: model.ChannelModelDetectionExecutionStatusPending, CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
			}
			if err := tx.Create(&execution).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return createdRun, err
}
