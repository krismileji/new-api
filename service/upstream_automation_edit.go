package service

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

var errUpstreamAutomationDraftChanged = errors.New("暂存配置已变化")

// Saved drafts survive restarts. Execution state and action counters remain
// owned by the current run until its lease ends.
func ApplyPendingUpstreamAutomationEdits(ctx context.Context) error {
	rows, err := model.ListUpstreamAutomations(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Result == "" {
			continue
		}
		_, state, err := decodeUpstreamAutomation(row)
		if err != nil {
			return err
		}
		if state.LeaseUntil > common.GetTimestamp() {
			continue
		}
		var draft UpstreamAutomationConfig
		applyErr := common.UnmarshalJsonStr(row.Result, &draft)
		if applyErr == nil {
			_, applyErr = saveUpstreamAutomation(ctx, draft, row.Result)
		}
		if applyErr == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err = model.MutateUpstreamAutomation(ctx, row.TaskID, false, 0, 0, func(current *model.SystemTask) error {
			if current.Result != row.Result {
				return nil
			}
			_, state, err := decodeUpstreamAutomation(*current)
			if err != nil {
				return err
			}
			state.PendingSave = false
			state.Message = "暂存配置应用失败，请重新打开配置后保存"
			encoded, err := common.Marshal(state)
			current.Result, current.State = "", string(encoded)
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}
