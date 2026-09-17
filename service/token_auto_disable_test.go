package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupTokenProtectionTest(t *testing.T) (*gorm.DB, *model.Token) {
	t.Helper()
	t.Setenv("TOKEN_AUTO_DISABLE_JOURNAL_DIR", t.TempDir())
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/tokens.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	oldDB, oldManager, redis := model.DB, tokenProtection.Load(), common.RedisEnabled
	model.DB, common.RedisEnabled = db, false
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.TokenAutoDisableConfig{}, &model.TokenAutoDisableRecord{}))
	token := &model.Token{Id: 9001, UserId: 42, Key: "auto-disable-test-key", Name: "测试 Key", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 500}
	require.NoError(t, db.Create(token).Error)
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, InitTokenAutoDisable(ctx))
	t.Cleanup(func() {
		cancel()
		tokenProtection.Store(oldManager)
		model.DB, common.RedisEnabled = oldDB, redis
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	return db, token
}

func tokenProtectionTestSettings() TokenAutoDisableSettings {
	return TokenAutoDisableSettings{Enabled: true, Rules: []TokenAutoDisableRule{{Id: "policy", Name: "内容拒绝", Enabled: true, StatusCodes: []int{403}, Keywords: []string{"policy violation"}, ResponseStatus: 451, ResponseMessage: "此 Key 已被禁用"}}}
}

func TestTokenProtectionRequiresStatusChannelAndError(t *testing.T) {
	_, token := setupTokenProtectionTest(t)
	settings := tokenProtectionTestSettings()
	settings.Rules[0].ChannelIds = []int{7}
	_, err := SaveTokenAutoDisableSettings(t.Context(), settings)
	require.NoError(t, err)
	ctx, done, denied := RegisterTokenProtection(t.Context(), token, "request-a")
	defer done()
	require.Nil(t, denied)
	ObserveTokenAutoDisableError(ctx, 8, 403, "policy violation")
	ObserveTokenAutoDisableError(ctx, 7, 429, "policy violation")
	ObserveTokenAutoDisableError(ctx, 7, 403, "rate limited")
	assert.NoError(t, ctx.Err())
	ObserveTokenAutoDisableError(ctx, 7, 403, "POLICY VIOLATION for this request")
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	assert.Equal(t, 451, TokenAutoDisableError(ctx).StatusCode)
}

func TestTokenProtectionCancelsOnlyThisKeyAndPersistsUntilAdminRelease(t *testing.T) {
	db, token := setupTokenProtectionTest(t)
	_, err := SaveTokenAutoDisableSettings(t.Context(), tokenProtectionTestSettings())
	require.NoError(t, err)
	first, firstDone, _ := RegisterTokenProtection(t.Context(), token, "first")
	defer firstDone()
	second, secondDone, _ := RegisterTokenProtection(t.Context(), token, "second")
	defer secondDone()
	other, otherDone, _ := RegisterTokenProtection(t.Context(), &model.Token{Id: 9002, UserId: token.UserId}, "other")
	defer otherDone()
	ObserveTokenAutoDisableError(first, 7, 403, "policy violation")
	require.NotNil(t, TokenAutoDisableFromContext(first))
	assert.Same(t, TokenAutoDisableFromContext(first), TokenAutoDisableFromContext(second))
	assert.NoError(t, other.Err())
	_, done, denied := RegisterTokenProtection(t.Context(), token, "new")
	done()
	require.NotNil(t, denied)
	assert.Equal(t, 2, denied.Record.CanceledRequests)
	var saved model.Token
	require.NoError(t, db.First(&saved, token.Id).Error)
	assert.Equal(t, common.TokenStatusDisabled, saved.Status)
	assert.Equal(t, 500, saved.RemainQuota)
	saved.Status = common.TokenStatusEnabled
	require.ErrorContains(t, UpdateTokenWithProtection(&saved), "仅管理员")
	settings, err := TokenAutoDisableSettingsSnapshot()
	require.NoError(t, err)
	settings.Enabled = false
	_, err = SaveTokenAutoDisableSettings(t.Context(), settings)
	require.NoError(t, err)
	_, done, denied = RegisterTokenProtection(t.Context(), token, "disabled-switch")
	done()
	require.NotNil(t, denied)
	require.NoError(t, ReleaseTokenProtection(t.Context(), denied.Record.Id, 1))
	next, nextDone, denied := RegisterTokenProtection(t.Context(), token, "released")
	defer nextDone()
	require.Nil(t, denied)
	ObserveTokenAutoDisableError(first, 7, 403, "policy violation")
	assert.NoError(t, next.Err(), "an old request cannot disable a released key")
	assert.ErrorIs(t, second.Err(), context.Canceled, "release does not revive old requests")
	records, total, err := model.ListTokenAutoDisables(t.Context(), 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, records, 1)
	assert.Equal(t, 1, records[0].ReleasedBy)
	assert.Positive(t, records[0].ReleasedAt)
	require.NoError(t, db.First(&saved, token.Id).Error)
	assert.Equal(t, common.TokenStatusEnabled, saved.Status)
}

func TestTokenProtectionRestoresBlockAfterRestart(t *testing.T) {
	_, token := setupTokenProtectionTest(t)
	_, err := SaveTokenAutoDisableSettings(t.Context(), tokenProtectionTestSettings())
	require.NoError(t, err)
	ctx, done, _ := RegisterTokenProtection(t.Context(), token, "trigger")
	defer done()
	ObserveTokenAutoDisableError(ctx, 7, 403, "policy violation")
	restartCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, InitTokenAutoDisable(restartCtx))
	_, unregister, denied := RegisterTokenProtection(t.Context(), token, "restart")
	defer unregister()
	require.NotNil(t, denied)
	assert.Equal(t, "此 Key 已被禁用", denied.Record.ResponseMessage)
}

func TestTokenProtectionUsesRawUpstreamErrorsWithoutMatchingModelOutput(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		stream  bool
		body    string
		blocked bool
	}{
		{"raw error", 403, false, "policy violation", true},
		{"structured error", 403, false, `{"error":{"message":"policy violation"}}`, true},
		{"wrong status", 500, false, "policy violation", false},
		{"ordinary output", 200, false, `{"choices":[{"message":{"content":"policy violation"}}]}`, false},
		{"successful JSON error", 200, false, `{"error":{"message":"policy violation"}}`, true},
		{"ordinary stream text", 200, true, "data: {\"choices\":[{\"delta\":{\"content\":\"policy violation\"}}]}\n\n", false},
		{"stream error", 200, true, "event: error\ndata: {\"type\":\"error\",\"error\":{\"message\":\"policy violation\"}}\n\n", true},
		{"responses failed", 200, true, "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"message\":\"policy violation\"}}}\n\n", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, token := setupTokenProtectionTest(t)
			settings := tokenProtectionTestSettings()
			settings.Rules[0].StatusCodes = []int{403, 200}
			_, err := SaveTokenAutoDisableSettings(t.Context(), settings)
			require.NoError(t, err)
			ctx, done, _ := RegisterTokenProtection(t.Context(), token, "error")
			defer done()
			resp := &http.Response{StatusCode: test.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(test.body))}
			if test.stream {
				resp.Header.Set("Content-Type", "text/event-stream")
			}
			ProtectTokenUpstreamResponse(ctx, 7, resp)
			// Split at byte boundaries to exercise partial JSON and SSE frames.
			var output strings.Builder
			_, err = io.CopyBuffer(&output, resp.Body, make([]byte, 1))
			require.NoError(t, err)
			assert.Equal(t, test.body, output.String(), "observation must preserve the upstream body")
			assert.Equal(t, test.blocked, TokenAutoDisableFromContext(ctx) != nil)
		})
	}
}

func TestTokenProtectionRuleValidationAndPriority(t *testing.T) {
	settings := tokenProtectionTestSettings()
	settings.Rules[0].Keywords = []string{" "}
	require.Error(t, settings.Validate())
	settings = tokenProtectionTestSettings()
	settings.Rules[0].ResponseStatus = 200
	require.Error(t, settings.Validate())
	rule := tokenProtectionTestSettings().Rules[0]
	rule.Keywords = []string{"policy", "violation"}
	rule.MatchAll = true
	assert.False(t, rule.matches(7, 403, "policy"))
	assert.True(t, rule.matches(7, 403, "POLICY VIOLATION"))
	rule.CaseSensitive = true
	assert.False(t, rule.matches(7, 403, "POLICY VIOLATION"))
	_, token := setupTokenProtectionTest(t)
	settings = tokenProtectionTestSettings()
	secondRule := settings.Rules[0]
	secondRule.Id = "second"
	secondRule.ResponseStatus = 403
	settings.Rules = append(settings.Rules, secondRule)
	_, err := SaveTokenAutoDisableSettings(t.Context(), settings)
	require.NoError(t, err)
	_, err = SaveTokenAutoDisableSettings(t.Context(), settings)
	require.ErrorIs(t, err, model.ErrTokenAutoDisableConflict)
	ctx, done, _ := RegisterTokenProtection(t.Context(), token, "priority")
	defer done()
	ObserveTokenAutoDisableError(ctx, 7, 403, "policy violation")
	assert.Equal(t, "policy", TokenAutoDisableFromContext(ctx).Record.RuleId)
}

func TestTokenProtectionRejectsRulesTooLargeForAllDatabases(t *testing.T) {
	settings := tokenProtectionTestSettings()
	settings.Rules[0].ResponseMessage = strings.Repeat("x", 4096)
	for index := 0; index < 32; index++ {
		rule := settings.Rules[0]
		rule.Id = strings.Repeat("x", index+1)
		settings.Rules = append(settings.Rules, rule)
	}
	require.ErrorContains(t, settings.Validate(), "总大小")
}

func TestTokenProtectionRecoversPendingDisableAfterDatabaseFailure(t *testing.T) {
	db, token := setupTokenProtectionTest(t)
	_, err := SaveTokenAutoDisableSettings(t.Context(), tokenProtectionTestSettings())
	require.NoError(t, err)
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:database-unavailable", func(tx *gorm.DB) { tx.AddError(errors.New("database unavailable")) }))
	ctx, done, _ := RegisterTokenProtection(t.Context(), token, "database-failure")
	defer done()
	ObserveTokenAutoDisableError(ctx, 7, 403, "policy violation")
	require.Len(t, PendingTokenAutoDisables(), 1)
	_, unregister, denied := RegisterTokenProtection(t.Context(), token, "denied-during-outage")
	unregister()
	require.NotNil(t, denied)
	require.NoError(t, db.Callback().Create().Remove("test:database-unavailable"))
	restart, stop := context.WithCancel(context.Background())
	defer stop()
	require.NoError(t, InitTokenAutoDisable(restart))
	_, unregister, denied = RegisterTokenProtection(t.Context(), token, "recovered")
	unregister()
	require.NotNil(t, denied)
	tokenProtection.Load().persist(token.Id)
	assert.Empty(t, PendingTokenAutoDisables())
	records, total, err := model.ListTokenAutoDisables(t.Context(), 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, records, 1)
	assert.Equal(t, denied.Record.Id, records[0].Id)
}

func TestTokenProtectionConcurrentAdmissionCannotMissRevocation(t *testing.T) {
	_, token := setupTokenProtectionTest(t)
	_, err := SaveTokenAutoDisableSettings(t.Context(), tokenProtectionTestSettings())
	require.NoError(t, err)
	trigger, finish, _ := RegisterTokenProtection(t.Context(), token, "trigger")
	defer finish()
	start := make(chan struct{})
	var workers sync.WaitGroup
	var admitted context.Context
	var unregister func()
	var denied *TokenAutoDisableCause
	workers.Add(2)
	go func() {
		defer workers.Done()
		<-start
		admitted, unregister, denied = RegisterTokenProtection(t.Context(), token, "concurrent")
	}()
	go func() {
		defer workers.Done()
		<-start
		ObserveTokenAutoDisableError(trigger, 7, 403, "policy violation")
	}()
	close(start)
	workers.Wait()
	defer unregister()
	if denied == nil {
		require.NotNil(t, TokenAutoDisableFromContext(admitted), "a registered request must receive the cancellation even when admission races the trigger")
	}
	_, unregister, denied = RegisterTokenProtection(t.Context(), token, "after")
	unregister()
	require.NotNil(t, denied)
}
