package openai

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relay/requestplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/tidwall/sjson"
)

func OpenaiRealtimeHandler(c *gin.Context, info *relaycommon.RelayInfo) (*types.NewAPIError, *dto.RealtimeUsage) {
	if info == nil || info.ClientWs == nil || info.TargetWs == nil {
		return types.NewError(fmt.Errorf("invalid websocket connection"), types.ErrorCodeBadResponse), nil
	}

	info.IsStream = true
	clientConn := info.ClientWs
	targetConn := info.TargetWs
	sessionCtx, stopSession := context.WithCancel(c.Request.Context())
	defer stopSession()
	var readers sync.WaitGroup
	readers.Add(2)
	interrupted := make(chan struct{})
	stopInterrupt := context.AfterFunc(sessionCtx, func() {
		defer close(interrupted)
		_ = targetConn.Close()
		_ = clientConn.UnderlyingConn().SetReadDeadline(time.Now())
		_ = clientConn.UnderlyingConn().SetWriteDeadline(time.Now())
	})
	defer func() {
		stopSession()
		if !stopInterrupt() {
			<-interrupted
		}
		readers.Wait()
		if cause := service.TokenAutoDisableFromContext(c.Request.Context()); cause != nil {
			c.Set("token_protection_ws_notified", true)
			_ = clientConn.SetWriteDeadline(time.Now().Add(time.Second))
			helper.WssError(c, clientConn, types.OpenAIError{Code: service.TokenAutoDisabledCode, Type: "permission_error", Message: cause.Record.ResponseMessage})
			_ = clientConn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "API Key 已被自动禁用"), time.Now().Add(time.Second))
		}
	}()

	clientClosed := make(chan struct{})
	targetClosed := make(chan struct{})
	sendChan := make(chan []byte, 100)
	receiveChan := make(chan []byte, 100)
	errChan := make(chan error, 2)

	usage := &dto.RealtimeUsage{}
	localUsage := &dto.RealtimeUsage{}
	sumUsage := &dto.RealtimeUsage{}
	drain, unregisterDrain := newRealtimeChannelDrain(info)
	defer unregisterDrain()

	// The request plugins answer the client and cancel upstream responses
	// from either reader, so each socket takes one writer at a time.
	var clientWriteMu, targetWriteMu sync.Mutex
	writeClient := func(message []byte) error {
		clientWriteMu.Lock()
		defer clientWriteMu.Unlock()
		return helper.WssString(c, clientConn, string(message))
	}
	writeTarget := func(message []byte) error {
		targetWriteMu.Lock()
		defer targetWriteMu.Unlock()
		return helper.WssString(c, targetConn, string(message))
	}
	plugins := requestplugin.NewRealtime(c, info, func(message []byte) {
		if err := writeClient(message); err != nil {
			select {
			case errChan <- fmt.Errorf("error writing to client: %v", err):
			default:
			}
		}
	}, func(message []byte) {
		if err := writeTarget(message); err != nil {
			select {
			case errChan <- fmt.Errorf("error writing to target: %v", err):
			default:
			}
		}
	})
	if plugins != nil {
		// The consume log reads what the plugins did once the session ends.
		defer plugins.Close(info)
	}

	gopool.Go(func() {
		defer readers.Done()
		defer func() {
			if r := recover(); r != nil {
				errChan <- fmt.Errorf("panic in client reader: %v", r)
			}
		}()
		for {
			select {
			case <-sessionCtx.Done():
				return
			default:
				_, message, err := clientConn.ReadMessage()
				if err != nil {
					if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
						errChan <- fmt.Errorf("error reading from client: %v", err)
					}
					close(clientClosed)
					return
				}

				realtimeEvent := &dto.RealtimeEvent{}
				err = common.Unmarshal(message, realtimeEvent)
				if err != nil {
					errChan <- fmt.Errorf("error unmarshalling message: %v", err)
					return
				}
				if plugins != nil {
					patched, dropped := plugins.ClientEvent(realtimeEvent.Type, message)
					if dropped {
						continue
					}
					if patched != nil {
						message, realtimeEvent = patched, &dto.RealtimeEvent{}
						if err = common.Unmarshal(message, realtimeEvent); err != nil {
							errChan <- fmt.Errorf("error unmarshalling message: %v", err)
							return
						}
					}
				}

				// A create rejection must be distinguishable from an unrelated
				// control error while the channel is draining.
				if realtimeEvent.Type == "response.create" && realtimeEvent.EventId == "" {
					realtimeEvent.EventId = common.GetUUID()
					message, err = sjson.SetBytes(message, "event_id", realtimeEvent.EventId)
					if err != nil {
						errChan <- err
						return
					}
				}
				if !drain.admit(realtimeEvent.Type, realtimeEvent.EventId) {
					payload, _ := common.Marshal(map[string]any{"type": "error", "error": map[string]any{
						"type": "invalid_request_error", "code": "channel_disabled", "event_id": realtimeEvent.EventId,
						"message": "渠道已自动禁用，不再接受新的生成请求",
					}})
					clientWriteMu.Lock()
					err = helper.WssString(c, clientConn, string(payload))
					clientWriteMu.Unlock()
					if err != nil {
						errChan <- err
						return
					}
					continue
				}
				if realtimeEvent.Type == dto.RealtimeEventTypeSessionUpdate {
					if realtimeEvent.Session != nil {
						if realtimeEvent.Session.Tools != nil {
							info.RealtimeTools = realtimeEvent.Session.Tools
						}
					}
				}

				textToken, audioToken, err := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
				if err != nil {
					errChan <- fmt.Errorf("error counting text token: %v", err)
					return
				}
				logger.LogInfo(c, common.LogText("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
				localUsage.TotalTokens += textToken + audioToken
				localUsage.InputTokens += textToken + audioToken
				localUsage.InputTokenDetails.TextTokens += textToken
				localUsage.InputTokenDetails.AudioTokens += audioToken

				err = writeTarget(message)
				if err != nil {
					errChan <- fmt.Errorf("error writing to target: %v", err)
					return
				}

				select {
				case sendChan <- message:
				default:
				}
			}
		}
	})

	gopool.Go(func() {
		defer readers.Done()
		defer func() {
			if r := recover(); r != nil {
				errChan <- fmt.Errorf("panic in target reader: %v", r)
			}
		}()
		for {
			select {
			case <-sessionCtx.Done():
				return
			default:
				_, message, err := targetConn.ReadMessage()
				if err != nil {
					if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
						errChan <- fmt.Errorf("error reading from target: %v", err)
					}
					close(targetClosed)
					return
				}
				info.SetFirstResponseTime()
				service.ObserveTokenAutoDisableJSON(c.Request.Context(), info.ChannelId, http.StatusSwitchingProtocols, message)
				if service.TokenAutoDisableFromContext(c.Request.Context()) != nil {
					return
				}
				realtimeEvent := &dto.RealtimeEvent{}
				err = common.Unmarshal(message, realtimeEvent)
				if err != nil {
					errChan <- fmt.Errorf("error unmarshalling message: %v", err)
					return
				}

				drain.observeStart(message)
				// quota is what a response.done was charged.
				quota := 0
				if realtimeEvent.Type == dto.RealtimeEventTypeResponseDone {
					realtimeUsage := realtimeEvent.Response.Usage
					if realtimeUsage != nil {
						usage.TotalTokens += realtimeUsage.TotalTokens
						usage.InputTokens += realtimeUsage.InputTokens
						usage.OutputTokens += realtimeUsage.OutputTokens
						usage.InputTokenDetails.AudioTokens += realtimeUsage.InputTokenDetails.AudioTokens
						usage.InputTokenDetails.CachedTokens += realtimeUsage.InputTokenDetails.CachedTokens
						usage.InputTokenDetails.TextTokens += realtimeUsage.InputTokenDetails.TextTokens
						usage.OutputTokenDetails.AudioTokens += realtimeUsage.OutputTokenDetails.AudioTokens
						usage.OutputTokenDetails.TextTokens += realtimeUsage.OutputTokenDetails.TextTokens
						quota, err = preConsumeUsage(c, info, usage, sumUsage)
						if err != nil {
							errChan <- fmt.Errorf("error consume usage: %v", err)
							return
						}
						// 本次计费完成，清除
						usage = &dto.RealtimeUsage{}

						localUsage = &dto.RealtimeUsage{}
					} else {
						textToken, audioToken, err := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
						if err != nil {
							errChan <- fmt.Errorf("error counting text token: %v", err)
							return
						}
						logger.LogInfo(c, common.LogText("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
						localUsage.TotalTokens += textToken + audioToken
						info.IsFirstRequest = false
						localUsage.InputTokens += textToken + audioToken
						localUsage.InputTokenDetails.TextTokens += textToken
						localUsage.InputTokenDetails.AudioTokens += audioToken
						quota, err = preConsumeUsage(c, info, localUsage, sumUsage)
						if err != nil {
							errChan <- fmt.Errorf("error consume usage: %v", err)
							return
						}
						// 本次计费完成，清除
						localUsage = &dto.RealtimeUsage{}
						// print now usage
					}
					logger.LogInfo(c, common.LogText("realtime streaming sumUsage: %v", sumUsage))
					logger.LogInfo(c, common.LogText("realtime streaming localUsage: %v", localUsage))
					logger.LogInfo(c, common.LogText("realtime streaming localUsage: %v", localUsage))

				} else if realtimeEvent.Type == dto.RealtimeEventTypeSessionUpdated || realtimeEvent.Type == dto.RealtimeEventTypeSessionCreated {
					realtimeSession := realtimeEvent.Session
					if realtimeSession != nil {
						// update audio format
						info.InputAudioFormat = common.GetStringIfEmpty(realtimeSession.InputAudioFormat, info.InputAudioFormat)
						info.OutputAudioFormat = common.GetStringIfEmpty(realtimeSession.OutputAudioFormat, info.OutputAudioFormat)
					}
				} else {
					textToken, audioToken, err := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
					if err != nil {
						errChan <- fmt.Errorf("error counting text token: %v", err)
						return
					}
					logger.LogInfo(c, common.LogText("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
					localUsage.TotalTokens += textToken + audioToken
					localUsage.OutputTokens += textToken + audioToken
					localUsage.OutputTokenDetails.TextTokens += textToken
					localUsage.OutputTokenDetails.AudioTokens += audioToken
				}

				if plugins != nil {
					var responseUsage *dto.RealtimeUsage
					if realtimeEvent.Response != nil {
						responseUsage = realtimeEvent.Response.Usage
					}
					plugins.ServerEvent(realtimeEvent.Type, message, responseUsage, quota)
				} else if err = writeClient(message); err != nil {
					errChan <- fmt.Errorf("error writing to client: %v", err)
					return
				}

				drain.observeFinished(message)
				select {
				case receiveChan <- message:
				default:
				}
			}
		}
	})

	select {
	case <-clientClosed:
	case <-targetClosed:
	case err := <-errChan:
		//return service.OpenAIErrorWrapper(err, "realtime_error", http.StatusInternalServerError), nil
		logger.LogError(c, common.LogText("realtime error: %s", err.Error()))
	case <-sessionCtx.Done():
	}
	stopSession()
	if !stopInterrupt() {
		<-interrupted
	}
	readers.Wait()

	if usage.TotalTokens != 0 {
		_, _ = preConsumeUsage(c, info, usage, sumUsage)
	}

	if localUsage.TotalTokens != 0 {
		_, _ = preConsumeUsage(c, info, localUsage, sumUsage)
	}

	// check usage total tokens, if 0, use local usage

	return nil, sumUsage
}

func preConsumeUsage(ctx *gin.Context, info *relaycommon.RelayInfo, usage *dto.RealtimeUsage, totalUsage *dto.RealtimeUsage) (int, error) {
	if usage == nil || totalUsage == nil {
		return 0, fmt.Errorf("invalid usage pointer")
	}

	totalUsage.TotalTokens += usage.TotalTokens
	totalUsage.InputTokens += usage.InputTokens
	totalUsage.OutputTokens += usage.OutputTokens
	totalUsage.InputTokenDetails.CachedTokens += usage.InputTokenDetails.CachedTokens
	totalUsage.InputTokenDetails.TextTokens += usage.InputTokenDetails.TextTokens
	totalUsage.InputTokenDetails.AudioTokens += usage.InputTokenDetails.AudioTokens
	totalUsage.OutputTokenDetails.TextTokens += usage.OutputTokenDetails.TextTokens
	totalUsage.OutputTokenDetails.AudioTokens += usage.OutputTokenDetails.AudioTokens
	// clear usage
	return service.PreWssConsumeQuota(ctx, info, usage)
}
