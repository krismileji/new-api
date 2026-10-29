package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/go-redis/redis/v8"
)

const (
	contentCaptureConfigKey    = "channelMonitor:contentCapture:v1:config"
	contentCaptureIdsKey       = "channelMonitor:contentCapture:v1:ids"
	contentCaptureSummariesKey = "channelMonitor:contentCapture:v1:summaries"
	contentCaptureBodiesKey    = "channelMonitor:contentCapture:v1:bodies"

	ContentCaptureMaxLimit       = 1000
	ContentCaptureMaxBodyKB      = 256
	ContentCaptureMaxHours       = 72
	contentCapturePreviewBytes   = 300
	contentCaptureConfigCacheTTL = 2 * time.Second
)

var ErrContentCaptureRedisRequired = errors.New("请求内容抓取需要启用 Redis")

// ContentCaptureConfig is the active capture session. Session changes on every
// enable so writes started under an older session are rejected by Redis.
type ContentCaptureConfig struct {
	Enabled   bool   `json:"enabled"`
	Session   string `json:"-"`
	Limit     int    `json:"limit"`
	MaxBodyKB int    `json:"max_body_kb"`
	StartedAt int64  `json:"started_at"`
	ExpiresAt int64  `json:"expires_at"`
}

type ContentCaptureSummary struct {
	Id                string `json:"id"`
	CreatedAt         int64  `json:"created_at"`
	UserId            int    `json:"user_id"`
	Username          string `json:"username"`
	TokenId           int    `json:"token_id"`
	TokenName         string `json:"token_name"`
	Group             string `json:"group"`
	Model             string `json:"model"`
	ChannelId         int    `json:"channel_id"`
	Path              string `json:"path"`
	Ip                string `json:"ip"`
	StatusCode        int    `json:"status_code"`
	IsStream          bool   `json:"is_stream"`
	DurationMs        int64  `json:"duration_ms"`
	RequestSize       int64  `json:"request_size"`
	ResponseSize      int64  `json:"response_size"`
	RequestTruncated  bool   `json:"request_truncated"`
	ResponseTruncated bool   `json:"response_truncated"`
	RequestPreview    string `json:"request_preview"`
}

type ContentCaptureRecord struct {
	ContentCaptureSummary
	RequestBody  string `json:"request_body"`
	ResponseBody string `json:"response_body"`
}

var contentCaptureCache struct {
	sync.Mutex
	config    ContentCaptureConfig
	refreshed time.Time
}

// CachedContentCaptureConfig is read on every relay request, so each instance
// asks Redis at most once per cache window. Errors read as disabled.
func CachedContentCaptureConfig() ContentCaptureConfig {
	if !common.RedisEnabled {
		return ContentCaptureConfig{}
	}
	contentCaptureCache.Lock()
	defer contentCaptureCache.Unlock()
	now := time.Now()
	if now.Sub(contentCaptureCache.refreshed) < contentCaptureConfigCacheTTL {
		return contentCaptureCache.config
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	config, err := GetContentCaptureConfig(ctx)
	if err != nil {
		config = ContentCaptureConfig{}
	}
	contentCaptureCache.config = config
	contentCaptureCache.refreshed = now
	return config
}

func resetContentCaptureCache() {
	contentCaptureCache.Lock()
	contentCaptureCache.refreshed = time.Time{}
	contentCaptureCache.Unlock()
}

func GetContentCaptureConfig(ctx context.Context) (ContentCaptureConfig, error) {
	if !common.RedisEnabled {
		return ContentCaptureConfig{}, ErrContentCaptureRedisRequired
	}
	values, err := common.RedisMonitorReadClient().HGetAll(ctx, contentCaptureConfigKey).Result()
	if err != nil {
		return ContentCaptureConfig{}, err
	}
	if values["session"] == "" {
		return ContentCaptureConfig{}, nil
	}
	config := ContentCaptureConfig{Enabled: true, Session: values["session"]}
	config.Limit, _ = strconv.Atoi(values["limit"])
	config.MaxBodyKB, _ = strconv.Atoi(values["max_body_kb"])
	config.StartedAt, _ = strconv.ParseInt(values["started_at"], 10, 64)
	config.ExpiresAt, _ = strconv.ParseInt(values["expires_at"], 10, 64)
	return config, nil
}

// KEYS: config, ids, summaries, bodies. ARGV: new session, limit, body KB,
// started at, expires at, ttl seconds. An active session keeps its records.
var contentCaptureEnableScript = redis.NewScript(`
local session = redis.call('HGET', KEYS[1], 'session')
if not session then
  redis.call('DEL', KEYS[2], KEYS[3], KEYS[4])
  session = ARGV[1]
  redis.call('HSET', KEYS[1], 'session', session, 'started_at', ARGV[4])
end
redis.call('HSET', KEYS[1], 'limit', ARGV[2], 'max_body_kb', ARGV[3], 'expires_at', ARGV[5])
local limit = tonumber(ARGV[2])
while redis.call('LLEN', KEYS[2]) > limit do
  local id = redis.call('RPOP', KEYS[2])
  redis.call('HDEL', KEYS[3], id)
  redis.call('HDEL', KEYS[4], id)
end
for i = 1, 4 do
  redis.call('EXPIRE', KEYS[i], ARGV[6])
end
return session
`)

// KEYS: config, ids, summaries, bodies. ARGV: session, id, summary, body.
// Writes from a closed or replaced session are dropped.
var contentCapturePushScript = redis.NewScript(`
if redis.call('HGET', KEYS[1], 'session') ~= ARGV[1] then
  return 0
end
local limit = tonumber(redis.call('HGET', KEYS[1], 'limit'))
redis.call('LPUSH', KEYS[2], ARGV[2])
redis.call('HSET', KEYS[3], ARGV[2], ARGV[3])
redis.call('HSET', KEYS[4], ARGV[2], ARGV[4])
while redis.call('LLEN', KEYS[2]) > limit do
  local id = redis.call('RPOP', KEYS[2])
  redis.call('HDEL', KEYS[3], id)
  redis.call('HDEL', KEYS[4], id)
end
local ttl = redis.call('PTTL', KEYS[1])
if ttl > 0 then
  for i = 2, 4 do
    redis.call('PEXPIRE', KEYS[i], ttl)
  end
end
return 1
`)

func contentCaptureKeys() []string {
	return []string{contentCaptureConfigKey, contentCaptureIdsKey, contentCaptureSummariesKey, contentCaptureBodiesKey}
}

// EnableContentCapture starts a session, or updates the limits of the active
// one. The whole capture expires after hours even if nobody turns it off.
func EnableContentCapture(ctx context.Context, limit, maxBodyKB, hours int) (ContentCaptureConfig, error) {
	if !common.RedisEnabled {
		return ContentCaptureConfig{}, ErrContentCaptureRedisRequired
	}
	now := time.Now()
	ttl := time.Duration(hours) * time.Hour
	_, err := contentCaptureEnableScript.Run(ctx, common.RedisMonitorWriteClient(), contentCaptureKeys(),
		common.GetUUID(), limit, maxBodyKB, now.Unix(), now.Add(ttl).Unix(), int64(ttl.Seconds())).Result()
	resetContentCaptureCache()
	if err != nil {
		return ContentCaptureConfig{}, err
	}
	return GetContentCaptureConfig(ctx)
}

// DisableContentCapture removes the session and every captured record.
func DisableContentCapture(ctx context.Context) error {
	if !common.RedisEnabled {
		return ErrContentCaptureRedisRequired
	}
	err := common.RedisMonitorWriteClient().Del(ctx, contentCaptureKeys()...).Err()
	resetContentCaptureCache()
	return err
}

func SaveContentCaptureRecord(ctx context.Context, session string, record ContentCaptureRecord) error {
	preview := record.RequestBody
	if len(preview) > contentCapturePreviewBytes {
		preview = strings.ToValidUTF8(preview[:contentCapturePreviewBytes], "")
	}
	record.RequestPreview = preview
	summary, err := common.Marshal(record.ContentCaptureSummary)
	if err != nil {
		return err
	}
	body, err := common.Marshal(map[string]string{"request_body": record.RequestBody, "response_body": record.ResponseBody})
	if err != nil {
		return err
	}
	return contentCapturePushScript.Run(ctx, common.RedisMonitorWriteClient(), contentCaptureKeys(),
		session, record.Id, summary, body).Err()
}

// ListContentCaptureSummaries returns summaries newest first.
func ListContentCaptureSummaries(ctx context.Context) ([]ContentCaptureSummary, error) {
	if !common.RedisEnabled {
		return nil, ErrContentCaptureRedisRequired
	}
	client := common.RedisMonitorReadClient()
	ids, err := client.LRange(ctx, contentCaptureIdsKey, 0, -1).Result()
	if err != nil || len(ids) == 0 {
		return []ContentCaptureSummary{}, err
	}
	values, err := client.HMGet(ctx, contentCaptureSummariesKey, ids...).Result()
	if err != nil {
		return nil, err
	}
	summaries := make([]ContentCaptureSummary, 0, len(values))
	for _, value := range values {
		raw, ok := value.(string)
		if !ok {
			continue
		}
		var summary ContentCaptureSummary
		if common.UnmarshalJsonStr(raw, &summary) == nil {
			summaries = append(summaries, summary)
		}
	}
	return summaries, nil
}

// GetContentCaptureRecord returns nil when the record was trimmed or cleared.
func GetContentCaptureRecord(ctx context.Context, id string) (*ContentCaptureRecord, error) {
	if !common.RedisEnabled {
		return nil, ErrContentCaptureRedisRequired
	}
	client := common.RedisMonitorReadClient()
	values, err := client.HMGet(ctx, contentCaptureSummariesKey, id).Result()
	if err != nil {
		return nil, err
	}
	summary, ok := values[0].(string)
	if !ok {
		return nil, nil
	}
	body, err := client.HGet(ctx, contentCaptureBodiesKey, id).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var record ContentCaptureRecord
	if err := common.UnmarshalJsonStr(summary, &record.ContentCaptureSummary); err != nil {
		return nil, err
	}
	if err := common.UnmarshalJsonStr(body, &record); err != nil {
		return nil, err
	}
	return &record, nil
}
