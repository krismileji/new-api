package service

import (
	"fmt"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const channelModelDetectionTransportContextKey = "channel_model_detection_transport"

type channelModelDetectionTransportState struct {
	mu          sync.Mutex
	db          *gorm.DB
	costEventId string
	dispatched  bool
	dispatchErr error
}

// BeginChannelModelDetectionTransport binds one prepared cost event to the
// exact upstream transport boundary carried by this Gin context.
func BeginChannelModelDetectionTransport(ctx *gin.Context, db *gorm.DB, costEventId string) {
	if ctx == nil || costEventId == "" {
		return
	}
	ctx.Set(channelModelDetectionTransportContextKey, &channelModelDetectionTransportState{
		db:          db,
		costEventId: costEventId,
	})
}

func markChannelModelDetectionRequestDispatched(ctx *gin.Context) error {
	state := channelModelDetectionTransportStateFromContext(ctx)
	if state == nil {
		return nil
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.dispatched {
		return nil
	}
	_, err := MarkChannelModelDetectionCostEventDispatched(ctx.Request.Context(), state.db, state.costEventId, common.GetTimestamp())
	if err != nil {
		state.dispatchErr = err
		return err
	}
	state.dispatched = true
	state.dispatchErr = nil
	return nil
}

// ChannelModelDetectionTransportStatus reports whether the current attempt
// crossed the real upstream transport boundary. A dispatch persistence error
// is retained so the caller never misclassifies a potentially sent request as
// not_started.
func ChannelModelDetectionTransportStatus(ctx *gin.Context) (bool, error) {
	state := channelModelDetectionTransportStateFromContext(ctx)
	if state == nil {
		return false, nil
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.dispatched, state.dispatchErr
}

func channelModelDetectionTransportStateFromContext(ctx *gin.Context) *channelModelDetectionTransportState {
	if ctx == nil {
		return nil
	}
	value, exists := ctx.Get(channelModelDetectionTransportContextKey)
	if !exists {
		return nil
	}
	state, _ := value.(*channelModelDetectionTransportState)
	return state
}

// MarkChannelModelDetectionTransportUnresolved is used when the dispatch
// marker itself could not be persisted. The request may already be on the
// wire, so the prepared event must never be downgraded to not_started.
func MarkChannelModelDetectionTransportUnresolved(ctx *gin.Context, input ChannelModelDetectionCostUnresolvedInput) error {
	state := channelModelDetectionTransportStateFromContext(ctx)
	if state == nil {
		return nil
	}
	state.mu.Lock()
	db := state.db
	costEventId := state.costEventId
	state.mu.Unlock()

	if _, err := MarkChannelModelDetectionCostEventDispatched(ctx.Request.Context(), db, costEventId, common.GetTimestamp()); err != nil {
		return err
	}
	input.CostEventId = costEventId
	_, err := MarkChannelModelDetectionCostEventUnresolved(ctx.Request.Context(), db, input)
	return err
}

func logChannelModelDetectionDispatchError(ctx *gin.Context, err error) {
	if err == nil {
		return
	}
	logger.LogError(ctx, fmt.Sprintf("模型检测成本事件发送标记失败: %s", common.MaskSensitiveInfo(err.Error())))
}
