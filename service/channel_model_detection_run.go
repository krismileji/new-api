package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

var ErrChannelModelDetectionDetectorURLMissing = errors.New("尚未配置官方检测器地址")

type ChannelModelDetectionManualRunRequest struct {
	ChannelID         int
	Preset            string
	ConfirmHighCost   bool
	CreatedByUserID   int
	CreatedByUsername string
}

type ChannelModelDetectionManualRunResponse struct {
	RunID        string `json:"run_id"`
	Status       string `json:"status"`
	Preset       string `json:"preset"`
	PresetSource string `json:"preset_source"`
}

type ChannelModelDetectionCancelResponse struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
}

type ChannelModelDetectionRunCanceler interface {
	CancelRun(context.Context, string) error
}

type ChannelModelDetectionRunCancelerFactory func(*gorm.DB) (ChannelModelDetectionRunCanceler, error)

var channelModelDetectionRunCancelerState = struct {
	sync.RWMutex
	factory ChannelModelDetectionRunCancelerFactory
}{}

func SetChannelModelDetectionRunCancelerFactory(factory ChannelModelDetectionRunCancelerFactory) func() {
	channelModelDetectionRunCancelerState.Lock()
	previous := channelModelDetectionRunCancelerState.factory
	channelModelDetectionRunCancelerState.factory = factory
	channelModelDetectionRunCancelerState.Unlock()
	return func() {
		channelModelDetectionRunCancelerState.Lock()
		channelModelDetectionRunCancelerState.factory = previous
		channelModelDetectionRunCancelerState.Unlock()
	}
}

func StartChannelModelDetectionManualRun(ctx context.Context, db *gorm.DB, input ChannelModelDetectionManualRunRequest, now time.Time) (ChannelModelDetectionManualRunResponse, error) {
	if db == nil {
		db = model.DB
	}
	if db == nil {
		return ChannelModelDetectionManualRunResponse{}, errors.New("模型检测数据库不可用")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var channel model.Channel
	if err := db.WithContext(ctx).Select("id").Where("id = ?", input.ChannelID).First(&channel).Error; err != nil {
		return ChannelModelDetectionManualRunResponse{}, err
	}
	var global model.ChannelModelDetectionGlobalConfig
	if err := db.WithContext(ctx).Where("id = ?", model.ChannelModelDetectionConfigID).First(&global).Error; err != nil {
		return ChannelModelDetectionManualRunResponse{}, err
	}
	if strings.TrimSpace(global.DetectorURL) == "" {
		return ChannelModelDetectionManualRunResponse{}, ErrChannelModelDetectionDetectorURLMissing
	}
	if _, err := ValidateChannelModelDetectorTarget(ctx, global.DetectorURL); err != nil {
		return ChannelModelDetectionManualRunResponse{}, err
	}
	run, err := CreateChannelModelDetectionManualRun(ctx, db, ChannelModelDetectionManualRunInput{
		ChannelID: input.ChannelID, Preset: input.Preset, ConfirmHighCost: input.ConfirmHighCost,
		CreatedByUserID: input.CreatedByUserID, CreatedByUsername: input.CreatedByUsername,
	}, now)
	if err != nil {
		return ChannelModelDetectionManualRunResponse{}, err
	}
	return ChannelModelDetectionManualRunResponse{
		RunID: run.RunId, Status: run.Status, Preset: run.Preset, PresetSource: run.PresetSource,
	}, nil
}

func CancelChannelModelDetectionRun(ctx context.Context, db *gorm.DB, runID string) (ChannelModelDetectionCancelResponse, error) {
	if db == nil {
		db = model.DB
	}
	if db == nil {
		return ChannelModelDetectionCancelResponse{}, errors.New("模型检测数据库不可用")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return ChannelModelDetectionCancelResponse{}, gorm.ErrRecordNotFound
	}
	var run model.ChannelModelDetectionRun
	if err := db.WithContext(ctx).Where("run_id = ?", runID).First(&run).Error; err != nil {
		return ChannelModelDetectionCancelResponse{}, err
	}
	if !model.IsChannelModelDetectionActiveRunStatus(run.Status) {
		return ChannelModelDetectionCancelResponse{RunID: run.RunId, Status: run.Status}, nil
	}
	channelModelDetectionRunCancelerState.RLock()
	factory := channelModelDetectionRunCancelerState.factory
	channelModelDetectionRunCancelerState.RUnlock()
	if factory == nil {
		factory = func(database *gorm.DB) (ChannelModelDetectionRunCanceler, error) {
			worker := NewChannelModelDetectionWorker(database, func(detectorURL string) (ChannelModelDetectionDetector, error) {
				return NewChannelModelDetectorClient(detectorURL)
			}, nil)
			return worker, nil
		}
	}
	canceler, err := factory(db)
	if err != nil {
		return ChannelModelDetectionCancelResponse{}, err
	}
	if canceler == nil {
		return ChannelModelDetectionCancelResponse{}, errors.New("模型检测取消服务不可用")
	}
	if err := canceler.CancelRun(ctx, run.RunId); err != nil {
		return ChannelModelDetectionCancelResponse{}, err
	}
	if err := db.WithContext(ctx).Select("run_id", "status").Where("run_id = ?", run.RunId).First(&run).Error; err != nil {
		return ChannelModelDetectionCancelResponse{}, err
	}
	return ChannelModelDetectionCancelResponse{RunID: run.RunId, Status: run.Status}, nil
}
