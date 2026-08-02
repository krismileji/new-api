package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/joho/godotenv"
	"gorm.io/gorm"
)

const (
	seedConfirmEnvironment = "CHANNEL_SMART_SCHEDULE_SEED_CONFIRM"
	seedConfirmToken       = "write-channel-smart-schedule-v1"
	seedChannelTag         = "智能调度验收数据"
	seedChannelNamePrefix  = "[智能调度验收]"
)

var (
	seedGroups = []string{"调度验收-低成本", "调度验收-高可靠"}
	seedModels = []string{"smart-seed-chat-fast", "smart-seed-chat-balanced"}
)

type seedProfile struct {
	name             string
	costRatio        float64
	sampleCount      int
	failureCount     int
	firstTokenMs     float64
	tokensPerSecond  float64
	jitterEvery      int
	jitterFirstToken float64
	failureDurations []float64
}

type seedSample struct {
	Time              int64    `json:"time"`
	Success           bool     `json:"success"`
	Source            string   `json:"source,omitempty"`
	SampleID          string   `json:"sample_id,omitempty"`
	FailureDurationMs *float64 `json:"failure_duration_ms,omitempty"`
	FirstTokenMs      *float64 `json:"first_token_ms,omitempty"`
	TPS               *float64 `json:"tps,omitempty"`
}

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "写入智能调度开发验收数据失败："+err.Error())
		os.Exit(1)
	}
}

func run() error {
	_ = godotenv.Load(".env")
	if err := validateSeedEnvironment(os.Getenv); err != nil {
		return err
	}

	if sqlitePath := strings.TrimSpace(os.Getenv("SQLITE_PATH")); sqlitePath != "" {
		common.SQLitePath = sqlitePath
	}
	common.IsMasterNode = true
	if err := model.InitDB(); err != nil {
		return fmt.Errorf("初始化数据库失败: %w", err)
	}
	sqlDB, err := model.DB.DB()
	if err != nil {
		return fmt.Errorf("读取数据库连接失败: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	channelIDs, err := seedAcceptanceData(model.DB, common.GetTimestamp())
	if err != nil {
		return err
	}
	fmt.Printf(
		"已写入 %d 个带 %s 前缀的渠道、%d 个分组和 %d 个模型的智能调度验收数据。\n",
		len(channelIDs), seedChannelNamePrefix, len(seedGroups), len(seedModels),
	)
	fmt.Printf("渠道 ID：%v\n", channelIDs)
	fmt.Printf("分组：%s；模型：%s。\n", strings.Join(seedGroups, "、"), strings.Join(seedModels, "、"))
	return nil
}

func validateSeedEnvironment(getenv func(string) string) error {
	if !strings.EqualFold(strings.TrimSpace(getenv("APP_ENV")), "development") {
		return errors.New("仅允许在 APP_ENV=development 的开发环境执行")
	}
	if strings.TrimSpace(getenv(seedConfirmEnvironment)) != seedConfirmToken {
		return fmt.Errorf("必须设置 %s=%s 进行明确确认", seedConfirmEnvironment, seedConfirmToken)
	}
	if strings.EqualFold(strings.TrimSpace(getenv("NODE_TYPE")), "slave") {
		return errors.New("不能在只读从节点写入验收数据")
	}
	sqlDSN := strings.TrimSpace(getenv("SQL_DSN"))
	sqlitePath := strings.TrimSpace(getenv("SQLITE_PATH"))
	if sqlDSN == "" && sqlitePath == "" {
		return errors.New("必须通过 SQL_DSN 或 SQLITE_PATH 明确指定开发数据库")
	}
	if sqlDSN != "" && sqlitePath != "" {
		return errors.New("SQL_DSN 和 SQLITE_PATH 只能设置一个，避免写入错误的开发数据库")
	}
	return nil
}

func seedAcceptanceData(db *gorm.DB, now int64) ([]int, error) {
	if db == nil {
		return nil, errors.New("数据库未初始化")
	}
	if now <= 0 {
		now = time.Now().Unix()
	}
	profiles := []seedProfile{
		{name: "稳定高速", costRatio: 0.72, sampleCount: 24, firstTokenMs: 240, tokensPerSecond: 58},
		{name: "稳定低成本", costRatio: 0.68, sampleCount: 24, firstTokenMs: 330, tokensPerSecond: 50},
		{name: "均衡轻微失败", costRatio: 0.82, sampleCount: 24, failureCount: 1, firstTokenMs: 430, tokensPerSecond: 44, failureDurations: []float64{650}},
		{name: "高速高成本", costRatio: 1.18, sampleCount: 24, failureCount: 2, firstTokenMs: 280, tokensPerSecond: 54, failureDurations: []float64{800, 4_500}},
		{name: "低成本中速", costRatio: 0.76, sampleCount: 24, failureCount: 3, firstTokenMs: 620, tokensPerSecond: 37, failureDurations: []float64{500, 2_200, 8_500}},
		{name: "稳定慢响应", costRatio: 0.91, sampleCount: 24, firstTokenMs: 1_050, tokensPerSecond: 26},
		{name: "首字抖动", costRatio: 0.88, sampleCount: 24, failureCount: 1, firstTokenMs: 360, tokensPerSecond: 41, jitterEvery: 7, jitterFirstToken: 18_000, failureDurations: []float64{1_200}},
		{name: "近期不稳定", costRatio: 0.80, sampleCount: 24, failureCount: 8, firstTokenMs: 410, tokensPerSecond: 40, failureDurations: []float64{450, 900, 12_000}},
		{name: "慢失败偏多", costRatio: 0.86, sampleCount: 24, failureCount: 5, firstTokenMs: 720, tokensPerSecond: 31, failureDurations: []float64{11_000, 25_000, 55_000}},
		{name: "样本不足", costRatio: 0.74, sampleCount: 2, firstTokenMs: 300, tokensPerSecond: 47},
	}

	channelIDs := make([]int, 0, len(profiles))
	err := db.Transaction(func(tx *gorm.DB) error {
		for channelIndex, profile := range profiles {
			channelName := fmt.Sprintf("%s 渠道 %02d - %s", seedChannelNamePrefix, channelIndex+1, profile.name)
			var matchingChannels []model.Channel
			if err := tx.Where("name = ?", channelName).Limit(2).Find(&matchingChannels).Error; err != nil {
				return err
			}
			if len(matchingChannels) > 1 {
				return fmt.Errorf("发现多个同名验收渠道 %q，请先人工确认数据", channelName)
			}

			channelPriority := int64(0)
			channelWeight := uint(1000)
			testModel := seedModels[0]
			baseURL := "http://127.0.0.1:9"
			tag := seedChannelTag
			remark := "仅用于开发环境智能调度验收，不要发送真实业务请求"
			channel := model.Channel{
				Type: constant.ChannelTypeOpenAI, Key: "sk-smart-schedule-seed-not-for-relay",
				Status: common.ChannelStatusEnabled, Name: channelName, Weight: &channelWeight,
				CreatedTime: now, BaseURL: &baseURL, Models: strings.Join(seedModels, ","),
				Group: strings.Join(seedGroups, ","), Priority: &channelPriority,
				TestModel: &testModel, Tag: &tag, Remark: &remark,
			}
			if len(matchingChannels) == 0 {
				if err := tx.Create(&channel).Error; err != nil {
					return fmt.Errorf("创建验收渠道 %q 失败: %w", channelName, err)
				}
			} else {
				channel = matchingChannels[0]
				if err := tx.Model(&channel).Updates(map[string]any{
					"type": constant.ChannelTypeOpenAI, "key": "sk-smart-schedule-seed-not-for-relay",
					"status": common.ChannelStatusEnabled, "weight": channelWeight,
					"base_url": baseURL, "models": strings.Join(seedModels, ","),
					"group": strings.Join(seedGroups, ","), "priority": channelPriority,
					"test_model": testModel, "tag": tag, "remark": remark,
				}).Error; err != nil {
					return fmt.Errorf("更新验收渠道 %q 失败: %w", channelName, err)
				}
			}
			channelIDs = append(channelIDs, channel.Id)

			var monitor model.ChannelRatioMonitor
			monitorErr := tx.Where("channel_id = ?", channel.Id).First(&monitor).Error
			monitorValues := map[string]any{
				"ratio": profile.costRatio, "remark": "智能调度开发验收成本倍率",
				"updated_time": now, "updated_by_username": "开发验收数据工具",
				"last_fetch_status": model.ChannelRatioFetchStatusSucceeded, "last_fetch_time": now,
				"upstream_ratio_sync_disabled": true, "upstream_balance_sync_disabled": true,
			}
			switch {
			case errors.Is(monitorErr, gorm.ErrRecordNotFound):
				monitor = model.ChannelRatioMonitor{ChannelId: channel.Id}
				if err := tx.Create(&monitor).Error; err != nil {
					return err
				}
				if err := tx.Model(&monitor).Updates(monitorValues).Error; err != nil {
					return err
				}
			case monitorErr != nil:
				return monitorErr
			default:
				if err := tx.Model(&monitor).Updates(monitorValues).Error; err != nil {
					return err
				}
			}

			for modelIndex, modelName := range seedModels {
				modelProfile := profile
				if modelIndex == 1 {
					modelProfile.sampleCount = max(8, profile.sampleCount/2)
					modelProfile.failureCount = channelIndex % 4
					modelProfile.firstTokenMs = 380 + float64(len(profiles)-1-channelIndex)*105
					modelProfile.tokensPerSecond = 24 + float64(channelIndex)*2.5
					modelProfile.jitterEvery = 0
					modelProfile.jitterFirstToken = 0
					modelProfile.failureDurations = []float64{700, 6_500, 22_000}
				}
				if err := upsertSharedSampleState(tx, channel.Id, modelName, modelProfile, now); err != nil {
					return err
				}

				for groupIndex, groupName := range seedGroups {
					rankIndex := channelIndex
					if modelIndex == 1 {
						rankIndex = len(profiles) - 1 - rankIndex
					}
					if groupIndex == 1 {
						rankIndex = (rankIndex + 2) % len(profiles)
					}
					priority := int64(len(profiles) - rankIndex)
					weight := uint(1000)
					abilityConditions := model.Ability{ChannelId: channel.Id, Group: groupName, Model: modelName}
					var ability model.Ability
					abilityErr := tx.Where(&abilityConditions).First(&ability).Error
					switch {
					case errors.Is(abilityErr, gorm.ErrRecordNotFound):
						ability = abilityConditions
						ability.Enabled = true
						ability.Priority = &priority
						ability.Weight = weight
						if err := tx.Create(&ability).Error; err != nil {
							return err
						}
					case abilityErr != nil:
						return abilityErr
					default:
						if err := tx.Model(&ability).Updates(map[string]any{
							"enabled": true, "priority": priority, "weight": weight,
						}).Error; err != nil {
							return err
						}
					}

					routeConditions := model.ChannelSmartScheduleRouteState{
						ChannelId: channel.Id, GroupName: groupName, ModelName: modelName,
					}
					var routeState model.ChannelSmartScheduleRouteState
					routeErr := tx.Where(&routeConditions).First(&routeState).Error
					switch {
					case errors.Is(routeErr, gorm.ErrRecordNotFound):
						routeState = routeConditions
						routeState.ParticipationSet = true
						routeState.Excluded = false
						routeState.Revision = 1
						if err := tx.Create(&routeState).Error; err != nil {
							return err
						}
					case routeErr != nil:
						return routeErr
					case !routeState.ParticipationSet || routeState.Excluded:
						if routeState.Revision == math.MaxInt64 {
							return fmt.Errorf("验收路由修订号已达上限: channel_id=%d group=%s model=%s", channel.Id, groupName, modelName)
						}
						if err := tx.Model(&routeState).Updates(map[string]any{
							"participation_set": true, "excluded": false, "revision": routeState.Revision + 1,
						}).Error; err != nil {
							return err
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("事务写入验收数据失败: %w", err)
	}
	return channelIDs, nil
}

func upsertSharedSampleState(
	tx *gorm.DB,
	channelID int,
	modelName string,
	profile seedProfile,
	now int64,
) error {
	state, err := buildSharedSampleState(channelID, modelName, profile, now)
	if err != nil {
		return err
	}
	var existing model.ChannelSmartScheduleModelSampleState
	findErr := tx.Where("channel_id = ? AND model_name = ?", channelID, modelName).First(&existing).Error
	if errors.Is(findErr, gorm.ErrRecordNotFound) {
		return tx.Create(&state).Error
	}
	if findErr != nil {
		return findErr
	}
	return tx.Model(&existing).Updates(map[string]any{
		"window_start": state.WindowStart, "last_time": state.LastTime,
		"last_success": state.LastSuccess, "last_error": state.LastError,
		"sample_count": state.SampleCount, "success_count": state.SuccessCount,
		"failure_duration_sample_count": state.FailureDurationSampleCount,
		"average_failure_duration_ms":   state.AverageFailureDurationMs,
		"first_token_sample_count":      state.FirstTokenSampleCount,
		"average_first_token_ms":        state.AverageFirstTokenMs,
		"tps_sample_count":              state.TPSSampleCount, "average_tps": state.AverageTPS,
		"samples_json": state.SamplesJSON,
	}).Error
}

func buildSharedSampleState(
	channelID int,
	modelName string,
	profile seedProfile,
	now int64,
) (model.ChannelSmartScheduleModelSampleState, error) {
	if profile.sampleCount <= 0 || profile.failureCount < 0 || profile.failureCount > profile.sampleCount {
		return model.ChannelSmartScheduleModelSampleState{}, errors.New("验收样本配置无效")
	}
	samples := make([]seedSample, 0, profile.sampleCount)
	var successCount int64
	var failureDurationCount int64
	var failureDurationTotal float64
	var firstTokenCount int64
	var firstTokenTotal float64
	var tpsCount int64
	var tpsTotal float64
	failureIndex := 0
	for index := 0; index < profile.sampleCount; index++ {
		sampleTime := now - int64(profile.sampleCount-1-index)*90
		source := model.ChannelSmartScheduleSampleSourceScheduledProbe
		if index%2 == 1 {
			source = model.ChannelSmartScheduleSampleSourceManualTest
		}
		failedBefore := index * profile.failureCount / profile.sampleCount
		failedThroughCurrent := (index + 1) * profile.failureCount / profile.sampleCount
		failed := failedThroughCurrent > failedBefore
		sample := seedSample{
			Time: sampleTime, Success: !failed, Source: source,
			SampleID: fmt.Sprintf("smart-seed-%d-%s-%02d", channelID, modelName, index+1),
		}
		if failed {
			duration := 1_000.0
			if len(profile.failureDurations) > 0 {
				duration = profile.failureDurations[failureIndex%len(profile.failureDurations)]
			}
			failureIndex++
			sample.FailureDurationMs = &duration
			failureDurationCount++
			failureDurationTotal += duration
		} else {
			firstToken := profile.firstTokenMs * (0.9 + float64(index%5)*0.05)
			if profile.jitterEvery > 0 && (index+1)%profile.jitterEvery == 0 {
				firstToken = profile.jitterFirstToken
			}
			tps := profile.tokensPerSecond * (0.94 + float64(index%4)*0.04)
			sample.FirstTokenMs = &firstToken
			sample.TPS = &tps
			successCount++
			firstTokenCount++
			firstTokenTotal += firstToken
			tpsCount++
			tpsTotal += tps
		}
		samples = append(samples, sample)
	}
	rawSamples, err := common.Marshal(samples)
	if err != nil {
		return model.ChannelSmartScheduleModelSampleState{}, fmt.Errorf("编码验收共享样本失败: %w", err)
	}
	state := model.ChannelSmartScheduleModelSampleState{
		ChannelId: channelID, ModelName: modelName,
		WindowStart: samples[0].Time, LastTime: samples[len(samples)-1].Time,
		LastSuccess: samples[len(samples)-1].Success,
		SampleCount: int64(len(samples)), SuccessCount: successCount,
		FailureDurationSampleCount: failureDurationCount,
		FirstTokenSampleCount:      firstTokenCount, TPSSampleCount: tpsCount,
		SamplesJSON: model.ChannelSmartScheduleSamplesJSON(rawSamples),
	}
	if !state.LastSuccess {
		state.LastError = "开发验收：模拟上游请求失败"
	}
	if failureDurationCount > 0 {
		value := failureDurationTotal / float64(failureDurationCount)
		state.AverageFailureDurationMs = &value
	}
	if firstTokenCount > 0 {
		value := firstTokenTotal / float64(firstTokenCount)
		state.AverageFirstTokenMs = &value
	}
	if tpsCount > 0 {
		value := tpsTotal / float64(tpsCount)
		state.AverageTPS = &value
	}
	return state, nil
}
