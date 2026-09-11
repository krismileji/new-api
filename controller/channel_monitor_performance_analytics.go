package controller

import (
	"errors"
	"math"
)

// Recalculate averages from measurement totals after every merge. Averaging
// per-key or per-model TPS would give short requests disproportionate weight.
func mergeChannelMonitorPerformanceMeasurements(target, source map[string]any) error {
	total, _ := target["first_token_total_ms"].(float64)
	delta, _ := source["first_token_total_ms"].(float64)
	total += delta
	if total < 0 || math.IsNaN(total) || math.IsInf(total, 0) {
		return errors.New("首字延迟汇总无效")
	}
	target["first_token_total_ms"] = total
	target["average_first_token_ms"], target["average_tps"] = nil, nil
	if samples, _ := target["first_token_sample_count"].(int64); samples > 0 {
		target["average_first_token_ms"] = total / float64(samples)
	}
	samples, _ := target["tps_sample_count"].(int64)
	tokens, _ := target["tps_output_tokens"].(int64)
	duration, _ := target["tps_generation_duration_ms"].(int64)
	if samples > 0 && tokens > 0 && duration > 0 {
		target["average_tps"] = float64(tokens) / (float64(duration) / 1000)
	}
	return nil
}
