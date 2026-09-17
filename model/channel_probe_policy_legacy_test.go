package model

// Frozen pre-policy downstream schema from eccbf584e.
type channelProbePolicyLegacyMonitor struct {
	Id                          int      `json:"id"`
	ChannelId                   int      `json:"channel_id" gorm:"uniqueIndex;not null"`
	Ratio                       float64  `json:"ratio" gorm:"not null"`
	PreviousRatio               *float64 `json:"previous_ratio"`
	Remark                      string   `json:"remark" gorm:"type:varchar(255);default:''"`
	UpdatedTime                 int64    `json:"updated_time" gorm:"bigint;index"`
	UpdatedBy                   int      `json:"updated_by" gorm:"index"`
	UpdatedByUsername           string   `json:"updated_by_username" gorm:"type:varchar(64);default:''"`
	LastFetchStatus             string   `json:"last_fetch_status" gorm:"type:varchar(16);index"`
	LastFetchError              string   `json:"last_fetch_error" gorm:"type:varchar(255)"`
	LastFetchTime               int64    `json:"last_fetch_time" gorm:"bigint;index"`
	ConsecutiveFailures         int      `json:"consecutive_failures"`
	FetchFailureAlertNotified   bool     `json:"-"`
	UpstreamBalance             *float64 `json:"upstream_balance"`
	LastBalanceTime             int64    `json:"last_balance_time" gorm:"bigint"`
	LastBalanceError            string   `json:"last_balance_error" gorm:"type:varchar(255)"`
	BalanceConsecutiveFailures  int      `json:"balance_consecutive_failures"`
	BalanceFailureAlertNotified bool     `json:"-"`
	BalanceWarningThreshold     *float64 `json:"balance_warning_threshold"`
	BalanceAutoDisableThreshold *float64 `json:"balance_auto_disable_threshold"`
	BalanceAlertNotified        bool     `json:"balance_alert_notified"`
	UpstreamType                string   `json:"upstream_type" gorm:"type:varchar(32)"`
	UpstreamBaseURL             string   `json:"upstream_base_url" gorm:"type:text"`
	UpstreamGroup               string   `json:"upstream_group" gorm:"type:varchar(64)"`
	UpstreamAuthType            string   `json:"upstream_auth_type" gorm:"type:varchar(16)"`
	UpstreamUserId              int      `json:"upstream_user_id"`
	UpstreamAccessToken         string   `json:"-" gorm:"type:text"`
	UpstreamRefreshToken        string   `json:"-" gorm:"type:text"`
	UpstreamAccount             string   `json:"-" gorm:"type:varchar(320)"`
	UpstreamPassword            string   `json:"-" gorm:"type:text"`
	UpstreamRevision            int64    `json:"-" gorm:"bigint"`
	CostConversion              string   `json:"-" gorm:"type:text"`
	CustomUpstreamConfig        string   `json:"-" gorm:"type:text"`
	UpstreamRatioSyncDisabled   bool     `json:"-"`
	UpstreamBalanceSyncDisabled bool     `json:"-"`
	SingleChannelAction         string   `json:"single_channel_action" gorm:"type:varchar(32)"`
	MultipleChannelsAction      string   `json:"multiple_channels_action" gorm:"type:varchar(32)"`
	ConcurrencyLimit            int      `json:"concurrency_limit"`
	RPMLimit                    int      `json:"rpm_limit"`
	ConcurrencyRevision         int64    `json:"-" gorm:"bigint"`
}

func (channelProbePolicyLegacyMonitor) TableName() string { return "channel_ratio_monitors" }
