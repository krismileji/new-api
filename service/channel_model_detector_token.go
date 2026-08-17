package service

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	channelModelDetectorTokenPrefix       = "gpt56md1"
	channelModelDetectorTokenNonceBytes   = 32
	channelModelDetectorSigningKeyMinSize = 32

	// ChannelModelDetectorTokenMaxTTL is the absolute lifetime limit for an
	// in-memory detector credential. A worker may choose a shorter preset-based
	// lifetime, but it cannot issue a credential beyond this boundary.
	ChannelModelDetectorTokenMaxTTL = 24 * time.Hour
)

var (
	ErrChannelModelDetectorTokenInvalid        = errors.New("模型检测任务凭证无效")
	ErrChannelModelDetectorTokenExpired        = errors.New("模型检测任务凭证已过期")
	ErrChannelModelDetectorTokenRevoked        = errors.New("模型检测任务凭证已撤销")
	ErrChannelModelDetectorTokenBudgetExceeded = errors.New("模型检测任务凭证请求次数已用尽")
	ErrChannelModelDetectorTokenModelMismatch  = errors.New("模型检测请求型号与任务凭证不匹配")
	ErrChannelModelDetectorTokenReplay         = errors.New("模型检测请求已处理，不允许重复发送")
)

// ChannelModelDetectorTokenSpec is the immutable binding requested by a
// worker. The store always generates the nonce itself.
type ChannelModelDetectorTokenSpec struct {
	RunID           string
	TargetID        int64
	ExecutionID     int64
	ChannelID       int
	RequestModel    string
	ClaimedModel    string
	Preset          string
	RelayBaseURL    string
	MaxHTTPAttempts int
	ExpiresAt       int64
}

// ChannelModelDetectorTokenClaims are kept only in process memory. They are
// not embedded in the bearer token, so a holder cannot inspect or alter the
// selected channel, target, or upstream model.
type ChannelModelDetectorTokenClaims struct {
	RunID           string `json:"run_id"`
	TargetID        int64  `json:"target_id"`
	ExecutionID     int64  `json:"execution_id"`
	ChannelID       int    `json:"channel_id"`
	RequestModel    string `json:"request_model"`
	ClaimedModel    string `json:"claimed_model"`
	Preset          string `json:"preset"`
	RelayBaseURL    string `json:"relay_base_url"`
	MaxHTTPAttempts int    `json:"max_http_attempts"`
	ExpiresAt       int64  `json:"expires_at"`
	Nonce           string `json:"nonce"`
}

func (claims ChannelModelDetectorTokenClaims) MarshalJSON() ([]byte, error) {
	return common.Marshal(struct{}{})
}

func (claims ChannelModelDetectorTokenClaims) String() string {
	return "[模型检测任务凭证声明已隐藏]"
}

func (claims ChannelModelDetectorTokenClaims) GoString() string {
	return claims.String()
}

// ChannelModelDetectorCredential deliberately keeps the bearer value private
// and out of JSON. The worker passes BearerToken() only to the official
// detector's in-memory /start request.
type ChannelModelDetectorCredential struct {
	bearerToken string
	Claims      ChannelModelDetectorTokenClaims `json:"-"`
}

func (credential ChannelModelDetectorCredential) BearerToken() string {
	return credential.bearerToken
}

func (credential ChannelModelDetectorCredential) String() string {
	return "[模型检测任务凭证已隐藏]"
}

func (credential ChannelModelDetectorCredential) GoString() string {
	return credential.String()
}

func (credential ChannelModelDetectorCredential) MarshalJSON() ([]byte, error) {
	return common.Marshal(struct{}{})
}

// ChannelModelDetectorAttemptAuthorization is returned after signature,
// expiry, revocation, model, and atomic request-budget checks have succeeded.
// Replay is true when the same detector request ID was already reserved.
type ChannelModelDetectorAttemptAuthorization struct {
	Claims            ChannelModelDetectorTokenClaims
	DetectorRequestID string
	AttemptNo         int
	RemainingAttempts int
	Replay            bool
}

type channelModelDetectorTokenRecord struct {
	claims   ChannelModelDetectorTokenClaims
	revoked  bool
	used     int
	attempts map[[sha256.Size]byte]int
}

// ChannelModelDetectorTokenStore owns a dedicated signing key and all live
// detector credentials. Restarting the process invalidates the entire store;
// recovery must issue a fresh credential before resuming the official session.
type ChannelModelDetectorTokenStore struct {
	mu         sync.Mutex
	signingKey []byte
	now        func() time.Time
	records    map[string]*channelModelDetectorTokenRecord
}

func NewChannelModelDetectorTokenStore() (*ChannelModelDetectorTokenStore, error) {
	signingKey := make([]byte, channelModelDetectorSigningKeyMinSize)
	if _, err := rand.Read(signingKey); err != nil {
		return nil, fmt.Errorf("生成模型检测专用签名密钥失败: %w", err)
	}
	return newChannelModelDetectorTokenStore(signingKey, time.Now)
}

func newChannelModelDetectorTokenStore(signingKey []byte, now func() time.Time) (*ChannelModelDetectorTokenStore, error) {
	if len(signingKey) < channelModelDetectorSigningKeyMinSize {
		return nil, fmt.Errorf("模型检测任务凭证签名密钥至少需要 %d 字节", channelModelDetectorSigningKeyMinSize)
	}
	if now == nil {
		return nil, errors.New("模型检测任务凭证时钟无效")
	}
	keyCopy := append([]byte(nil), signingKey...)
	return &ChannelModelDetectorTokenStore{
		signingKey: keyCopy,
		now:        now,
		records:    make(map[string]*channelModelDetectorTokenRecord),
	}, nil
}

func (store *ChannelModelDetectorTokenStore) Issue(spec ChannelModelDetectorTokenSpec) (ChannelModelDetectorCredential, error) {
	if store == nil {
		return ChannelModelDetectorCredential{}, ErrChannelModelDetectorTokenInvalid
	}
	now := store.now().UTC()
	normalizedSpec, err := validateChannelModelDetectorTokenSpec(spec, now)
	if err != nil {
		return ChannelModelDetectorCredential{}, err
	}

	nonceBytes := make([]byte, channelModelDetectorTokenNonceBytes)
	if _, err = rand.Read(nonceBytes); err != nil {
		return ChannelModelDetectorCredential{}, fmt.Errorf("生成模型检测任务凭证失败: %w", err)
	}
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	claims := ChannelModelDetectorTokenClaims{
		RunID:           normalizedSpec.RunID,
		TargetID:        normalizedSpec.TargetID,
		ExecutionID:     normalizedSpec.ExecutionID,
		ChannelID:       normalizedSpec.ChannelID,
		RequestModel:    normalizedSpec.RequestModel,
		ClaimedModel:    normalizedSpec.ClaimedModel,
		Preset:          normalizedSpec.Preset,
		RelayBaseURL:    normalizedSpec.RelayBaseURL,
		MaxHTTPAttempts: normalizedSpec.MaxHTTPAttempts,
		ExpiresAt:       normalizedSpec.ExpiresAt,
		Nonce:           nonce,
	}

	store.mu.Lock()
	store.purgeExpiredLocked(now.Unix())
	if _, exists := store.records[nonce]; exists {
		store.mu.Unlock()
		return ChannelModelDetectorCredential{}, errors.New("模型检测任务凭证随机标识冲突")
	}
	store.records[nonce] = &channelModelDetectorTokenRecord{
		claims:   claims,
		attempts: make(map[[sha256.Size]byte]int),
	}
	store.mu.Unlock()

	message := channelModelDetectorTokenPrefix + "." + nonce
	signature := store.sign(message)
	return ChannelModelDetectorCredential{
		bearerToken: message + "." + base64.RawURLEncoding.EncodeToString(signature),
		Claims:      claims,
	}, nil
}

// AuthorizeAttempt atomically reserves one HTTP-attempt slot. Reusing the
// same detectorRequestID returns the original attempt number with Replay=true
// and does not consume another slot. The Relay coordinator rejects that replay
// before any upstream transport is invoked.
func (store *ChannelModelDetectorTokenStore) AuthorizeAttempt(token, requestedModel, detectorRequestID string) (ChannelModelDetectorAttemptAuthorization, error) {
	if store == nil {
		return ChannelModelDetectorAttemptAuthorization{}, ErrChannelModelDetectorTokenInvalid
	}
	nonce, err := store.authenticate(token)
	if err != nil {
		return ChannelModelDetectorAttemptAuthorization{}, err
	}
	detectorRequestID = strings.TrimSpace(detectorRequestID)
	if detectorRequestID == "" || len(detectorRequestID) > 256 {
		return ChannelModelDetectorAttemptAuthorization{}, errors.New("模型检测请求标识无效")
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.records[nonce]
	if !ok {
		return ChannelModelDetectorAttemptAuthorization{}, ErrChannelModelDetectorTokenInvalid
	}
	now := store.now().UTC().Unix()
	if now >= record.claims.ExpiresAt {
		delete(store.records, nonce)
		return ChannelModelDetectorAttemptAuthorization{}, ErrChannelModelDetectorTokenExpired
	}
	if record.revoked {
		return ChannelModelDetectorAttemptAuthorization{}, ErrChannelModelDetectorTokenRevoked
	}
	if !channelModelDetectorSecureStringEqual(requestedModel, record.claims.RequestModel) &&
		!channelModelDetectorSecureStringEqual(requestedModel, record.claims.ClaimedModel) {
		return ChannelModelDetectorAttemptAuthorization{}, ErrChannelModelDetectorTokenModelMismatch
	}

	attemptKey := sha256.Sum256([]byte(detectorRequestID))
	if attemptNo, exists := record.attempts[attemptKey]; exists {
		return ChannelModelDetectorAttemptAuthorization{
			Claims:            record.claims,
			DetectorRequestID: detectorRequestID,
			AttemptNo:         attemptNo,
			RemainingAttempts: record.claims.MaxHTTPAttempts - record.used,
			Replay:            true,
		}, nil
	}
	if record.used >= record.claims.MaxHTTPAttempts {
		return ChannelModelDetectorAttemptAuthorization{}, ErrChannelModelDetectorTokenBudgetExceeded
	}

	record.used++
	record.attempts[attemptKey] = record.used
	return ChannelModelDetectorAttemptAuthorization{
		Claims:            record.claims,
		DetectorRequestID: detectorRequestID,
		AttemptNo:         record.used,
		RemainingAttempts: record.claims.MaxHTTPAttempts - record.used,
	}, nil
}

func (store *ChannelModelDetectorTokenStore) Revoke(token string) error {
	if store == nil {
		return ErrChannelModelDetectorTokenInvalid
	}
	nonce, err := store.authenticate(token)
	if err != nil {
		return err
	}
	if !store.RevokeNonce(nonce) {
		return ErrChannelModelDetectorTokenInvalid
	}
	return nil
}

// RevokeNonce is intended for the worker that retained the issued claims. It
// never accepts a nonce originating from an HTTP request.
func (store *ChannelModelDetectorTokenStore) RevokeNonce(nonce string) bool {
	if store == nil || nonce == "" {
		return false
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.records[nonce]
	if !ok {
		return false
	}
	record.revoked = true
	return true
}

func (store *ChannelModelDetectorTokenStore) RevokeRunTarget(runID string, targetID int64) int {
	if store == nil || strings.TrimSpace(runID) == "" || targetID <= 0 {
		return 0
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	revoked := 0
	for _, record := range store.records {
		if record.claims.TargetID == targetID && channelModelDetectorSecureStringEqual(record.claims.RunID, runID) && !record.revoked {
			record.revoked = true
			revoked++
		}
	}
	return revoked
}

func (store *ChannelModelDetectorTokenStore) PurgeExpired() int {
	if store == nil {
		return 0
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.purgeExpiredLocked(store.now().UTC().Unix())
}

func (store *ChannelModelDetectorTokenStore) authenticate(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != channelModelDetectorTokenPrefix || parts[1] == "" || parts[2] == "" {
		return "", ErrChannelModelDetectorTokenInvalid
	}
	nonceBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(nonceBytes) != channelModelDetectorTokenNonceBytes || base64.RawURLEncoding.EncodeToString(nonceBytes) != parts[1] {
		return "", ErrChannelModelDetectorTokenInvalid
	}
	providedSignature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(providedSignature) != sha256.Size || base64.RawURLEncoding.EncodeToString(providedSignature) != parts[2] {
		return "", ErrChannelModelDetectorTokenInvalid
	}
	expectedSignature := store.sign(parts[0] + "." + parts[1])
	if !hmac.Equal(providedSignature, expectedSignature) {
		return "", ErrChannelModelDetectorTokenInvalid
	}
	return parts[1], nil
}

func (store *ChannelModelDetectorTokenStore) sign(message string) []byte {
	mac := hmac.New(sha256.New, store.signingKey)
	_, _ = mac.Write([]byte(message))
	return mac.Sum(nil)
}

func (store *ChannelModelDetectorTokenStore) purgeExpiredLocked(now int64) int {
	purged := 0
	for nonce, record := range store.records {
		if now >= record.claims.ExpiresAt {
			delete(store.records, nonce)
			purged++
		}
	}
	return purged
}

func validateChannelModelDetectorTokenSpec(spec ChannelModelDetectorTokenSpec, now time.Time) (ChannelModelDetectorTokenSpec, error) {
	spec.RunID = strings.TrimSpace(spec.RunID)
	spec.RequestModel = strings.TrimSpace(spec.RequestModel)
	spec.ClaimedModel = strings.TrimSpace(spec.ClaimedModel)
	spec.Preset = strings.ToLower(strings.TrimSpace(spec.Preset))
	if spec.RunID == "" || len(spec.RunID) > 128 || spec.TargetID <= 0 || spec.ExecutionID <= 0 || spec.ChannelID <= 0 {
		return ChannelModelDetectorTokenSpec{}, errors.New("模型检测任务凭证归属无效")
	}
	if spec.RequestModel == "" || len(spec.RequestModel) > 255 {
		return ChannelModelDetectorTokenSpec{}, errors.New("模型检测任务凭证请求模型无效")
	}
	if !model.IsChannelModelDetectionClaimedModel(spec.ClaimedModel) {
		return ChannelModelDetectorTokenSpec{}, model.ErrChannelModelDetectionInvalidClaimedModel
	}
	if !model.IsChannelModelDetectionPreset(spec.Preset) {
		return ChannelModelDetectorTokenSpec{}, model.ErrChannelModelDetectionInvalidPreset
	}
	if spec.MaxHTTPAttempts <= 0 || int64(spec.MaxHTTPAttempts) > math.MaxInt32 {
		return ChannelModelDetectorTokenSpec{}, errors.New("模型检测任务凭证请求次数无效")
	}
	nowUnix := now.UTC().Unix()
	if spec.ExpiresAt <= nowUnix || spec.ExpiresAt > now.Add(ChannelModelDetectorTokenMaxTTL).Unix() {
		return ChannelModelDetectorTokenSpec{}, errors.New("模型检测任务凭证过期时间无效")
	}
	normalizedBaseURL, err := normalizeChannelModelDetectorRelayBaseURL(spec.RelayBaseURL)
	if err != nil {
		return ChannelModelDetectorTokenSpec{}, err
	}
	spec.RelayBaseURL = normalizedBaseURL
	return spec, nil
}

func normalizeChannelModelDetectorRelayBaseURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 2048 {
		return "", errors.New("模型检测内部 Relay 地址无效")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("模型检测内部 Relay 地址无效")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}

func channelModelDetectorSecureStringEqual(left, right string) bool {
	leftHash := sha256.Sum256([]byte(left))
	rightHash := sha256.Sum256([]byte(right))
	return subtle.ConstantTimeCompare(leftHash[:], rightHash[:]) == 1
}
