package aws

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSharedLimitAWSFailureDoesNotRetryInsideSDK(t *testing.T) {
	originalDB, originalRedis := model.DB, common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "shared.db")), &gorm.Config{})
	require.NoError(t, err)
	model.DB, common.RedisEnabled = db, false
	t.Cleanup(func() {
		model.DB, common.RedisEnabled = originalDB, originalRedis
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.ChannelRatioMonitor{}, &model.ChannelLimitGroup{}, &model.ChannelLimitGroupTier{}, &model.ChannelLimitGroupMember{}, &model.ChannelLimitGroupRevision{}))
	require.NoError(t, db.Create(&model.Channel{Id: 1, Name: "AWS", Key: "test", Status: 1}).Error)
	group := &model.ChannelLimitGroup{Name: "上游", ConcurrencyLimit: 2, RPMLimit: 10, Tiers: []model.ChannelLimitGroupTier{{Priority: 0}}, Members: []model.ChannelLimitGroupMember{{ChannelID: 1, Priority: 0}}}
	require.NoError(t, service.SaveChannelLimitGroup(t.Context(), group, false))
	require.NoError(t, db.Model(group).Update("updated_at", time.Now().Add(-2*time.Minute).UnixMilli()).Error)
	group.Enabled = true
	require.NoError(t, service.SaveChannelLimitGroup(t.Context(), group, false))
	lease, ok, _, err := service.AcquireChannelConcurrency(t.Context(), 1)
	require.NoError(t, err)
	require.True(t, ok)
	t.Cleanup(lease.Release)
	for _, secret := range []string{"token|us-east-1", "access|secret|us-east-1"} {
		info := newAwsTestRelayInfo()
		info.ApiKey = secret
		ctx := newAwsTestContext(httptest.NewRecorder(), lease.Context)
		client, err := newAwsClient(ctx, info)
		require.NoError(t, err)
		requests := 0
		_, err = client.InvokeModel(lease.Context, &bedrockruntime.InvokeModelInput{ModelId: aws.String(awsTestModel), Body: []byte(`{}`)}, func(options *bedrockruntime.Options) {
			options.BaseEndpoint = aws.String("https://bedrock.test")
			options.HTTPClient = awsHTTPClientFunc(func(request *http.Request) (*http.Response, error) {
				requests++
				return &http.Response{StatusCode: 500, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"message":"unavailable"}`)), Request: request}, nil
			})
		})
		require.Error(t, err)
		assert.Equal(t, 1, requests)
		plain, err := newAwsClient(newAwsTestContext(httptest.NewRecorder(), t.Context()), info)
		require.NoError(t, err)
		assert.Greater(t, plain.Options().Retryer.MaxAttempts(), 1)
	}
}
