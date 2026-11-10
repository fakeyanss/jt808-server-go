package config

import (
	"bytes"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRetentionPolicyDefaults 验证未配置 retention 段时取内置默认值。
func TestRetentionPolicyDefaults(t *testing.T) {
	var cfg *Config
	policy := cfg.RetentionPolicy()

	assert.Equal(t, time.Duration(DefaultLocationRetentionDays)*24*time.Hour, policy.Locations.MaxAge)
	assert.Equal(t, DefaultLocationMaxPerDevice, policy.Locations.MaxPerDevice)
	assert.Equal(t, time.Duration(DefaultAlarmDedupWindowMS)*time.Millisecond, policy.Alarms.DedupWindow)
	assert.Equal(t, time.Duration(DefaultAlarmConfirmExpireMS)*time.Millisecond, policy.Alarms.ConfirmExpire)
	assert.Equal(t, DefaultAlarmMaxPerDevice, policy.Alarms.MaxPerDevice)
	assert.Zero(t, policy.Alarms.MaxAge)
	assert.Zero(t, policy.MediaFiles.MaxAge)
	assert.Zero(t, policy.MediaCaptures.MaxAge)
	assert.False(t, policy.Cleanup.Enabled)
	assert.Equal(t, DefaultRetentionCleanupInterval, policy.Cleanup.Interval)
	assert.Equal(t, DefaultRetentionCleanupBatchSize, policy.Cleanup.BatchSize)
}

// TestRetentionPolicyFromYAML 验证 YAML 键名映射与“显式 0 关闭策略”的语义。
func TestRetentionPolicyFromYAML(t *testing.T) {
	raw := []byte(`
retention:
  locations:
    retentionDays: 3
    maxPerDevice: 500
  alarms:
    dedupWindowMs: 0
    confirmExpireMs: 120000
    maxPerDevice: 0
`)

	v := viper.New()
	v.SetConfigType("yaml")
	require.NoError(t, v.ReadConfig(bytes.NewBuffer(raw)))

	var cfg Config
	require.NoError(t, v.Unmarshal(&cfg))

	policy := cfg.RetentionPolicy()
	assert.Equal(t, 3*24*time.Hour, policy.Locations.MaxAge)
	assert.Equal(t, 500, policy.Locations.MaxPerDevice)
	assert.Equal(t, time.Duration(0), policy.Alarms.DedupWindow)
	assert.Equal(t, 2*time.Minute, policy.Alarms.ConfirmExpire)
	assert.Equal(t, 0, policy.Alarms.MaxPerDevice)
}

// TestRetentionPolicyPartial 验证只配置部分字段时，未配置字段仍取默认值。
func TestRetentionPolicyPartial(t *testing.T) {
	days := 1
	cfg := &Config{Retention: &retentionConf{
		Locations: &locationRetentionConf{RetentionDays: &days},
	}}
	policy := cfg.RetentionPolicy()

	assert.Equal(t, 24*time.Hour, policy.Locations.MaxAge)
	assert.Equal(t, DefaultLocationMaxPerDevice, policy.Locations.MaxPerDevice)
	assert.Equal(t, time.Duration(DefaultAlarmDedupWindowMS)*time.Millisecond, policy.Alarms.DedupWindow)
	assert.Equal(t, time.Duration(DefaultAlarmConfirmExpireMS)*time.Millisecond, policy.Alarms.ConfirmExpire)
	assert.Equal(t, DefaultAlarmMaxPerDevice, policy.Alarms.MaxPerDevice)
}

// TestRetentionCleanupFromYAML 验证新增的定期清理配置映射与归一化。
func TestRetentionCleanupFromYAML(t *testing.T) {
	raw := []byte(`
retention:
  alarms:
    retentionDays: 30
  mediaFiles:
    retentionDays: 14
  mediaCaptures:
    retentionDays: 7
  cleanup:
    enabled: true
    interval: "30m"
    batchSize: 250
`)

	v := viper.New()
	v.SetConfigType("yaml")
	require.NoError(t, v.ReadConfig(bytes.NewBuffer(raw)))

	var cfg Config
	require.NoError(t, v.Unmarshal(&cfg))
	require.NoError(t, cfg.ValidateRetention())

	policy := cfg.RetentionPolicy()
	assert.Equal(t, 30*24*time.Hour, policy.Alarms.MaxAge)
	assert.Equal(t, 14*24*time.Hour, policy.MediaFiles.MaxAge)
	assert.Equal(t, 7*24*time.Hour, policy.MediaCaptures.MaxAge)
	assert.True(t, policy.Cleanup.Enabled)
	assert.Equal(t, 30*time.Minute, policy.Cleanup.Interval)
	assert.Equal(t, 250, policy.Cleanup.BatchSize)
}

// TestRetentionCleanupZeroBatchSize 验证 batchSize 显式为 0 时回落到默认值。
func TestRetentionCleanupZeroBatchSize(t *testing.T) {
	zero := 0
	enabled := true
	cfg := &Config{Retention: &retentionConf{Cleanup: &cleanupConf{
		Enabled:   &enabled,
		BatchSize: &zero,
	}}}
	require.NoError(t, cfg.ValidateRetention())

	policy := cfg.RetentionPolicy()
	assert.True(t, policy.Cleanup.Enabled)
	assert.Equal(t, DefaultRetentionCleanupBatchSize, policy.Cleanup.BatchSize)
}

// TestValidateRetentionCleanup 验证定期清理配置的非法值不会静默按 0 处理。
func TestValidateRetentionCleanup(t *testing.T) {
	neg := -1
	tooManyDays := MaxRetentionDays + 1
	tooManyRows := MaxRetentionCleanupBatchSize + 1

	tests := []struct {
		name    string
		cfg     *Config
		wantErr string
	}{
		{
			name:    "negative alarm retentionDays",
			cfg:     &Config{Retention: &retentionConf{Alarms: &alarmRetentionConf{RetentionDays: &neg}}},
			wantErr: "retention.alarms.retentionDays must be >= 0, got -1",
		},
		{
			name:    "negative mediaFiles retentionDays",
			cfg:     &Config{Retention: &retentionConf{MediaFiles: &timeRetentionConf{RetentionDays: &neg}}},
			wantErr: "retention.mediaFiles.retentionDays must be >= 0, got -1",
		},
		{
			name:    "negative mediaCaptures retentionDays",
			cfg:     &Config{Retention: &retentionConf{MediaCaptures: &timeRetentionConf{RetentionDays: &neg}}},
			wantErr: "retention.mediaCaptures.retentionDays must be >= 0, got -1",
		},
		{
			name:    "oversized location retentionDays",
			cfg:     &Config{Retention: &retentionConf{Locations: &locationRetentionConf{RetentionDays: &tooManyDays}}},
			wantErr: "retention.locations.retentionDays must be <= 36500, got 36501",
		},
		{
			name:    "oversized mediaFiles retentionDays",
			cfg:     &Config{Retention: &retentionConf{MediaFiles: &timeRetentionConf{RetentionDays: &tooManyDays}}},
			wantErr: "retention.mediaFiles.retentionDays must be <= 36500, got 36501",
		},
		{
			name:    "invalid cleanup interval",
			cfg:     &Config{Retention: &retentionConf{Cleanup: &cleanupConf{Interval: "soon"}}},
			wantErr: `retention.cleanup.interval must be a positive duration (e.g. 1h), got "soon"`,
		},
		{
			name:    "zero cleanup interval",
			cfg:     &Config{Retention: &retentionConf{Cleanup: &cleanupConf{Interval: "0s"}}},
			wantErr: `retention.cleanup.interval must be a positive duration (e.g. 1h), got "0s"`,
		},
		{
			name:    "negative cleanup batchSize",
			cfg:     &Config{Retention: &retentionConf{Cleanup: &cleanupConf{BatchSize: &neg}}},
			wantErr: "retention.cleanup.batchSize must be between 0 and 5000, got -1",
		},
		{
			name:    "oversized cleanup batchSize",
			cfg:     &Config{Retention: &retentionConf{Cleanup: &cleanupConf{BatchSize: &tooManyRows}}},
			wantErr: "retention.cleanup.batchSize must be between 0 and 5000, got 5001",
		},
		{
			name: "explicit zero disables everything",
			cfg: &Config{Retention: &retentionConf{
				Alarms:        &alarmRetentionConf{RetentionDays: new(int)},
				MediaFiles:    &timeRetentionConf{RetentionDays: new(int)},
				MediaCaptures: &timeRetentionConf{RetentionDays: new(int)},
				Cleanup:       &cleanupConf{},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.ValidateRetention()
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Equal(t, tt.wantErr, err.Error())
		})
	}
}

// TestValidateRetention 验证负数非法、显式 0 合法、缺省合法。
func TestValidateRetention(t *testing.T) {
	neg := -1
	zero := 0
	positive := 10

	tests := []struct {
		name    string
		cfg     *Config
		wantErr string
	}{
		{name: "nil config", cfg: nil},
		{name: "missing retention", cfg: &Config{}},
		{name: "explicit zero", cfg: &Config{Retention: &retentionConf{
			Locations: &locationRetentionConf{RetentionDays: &zero, MaxPerDevice: &zero},
			Alarms: &alarmRetentionConf{
				DedupWindowMS:   &zero,
				ConfirmExpireMS: &zero,
				MaxPerDevice:    &zero,
			},
		}}},
		{name: "positive", cfg: &Config{Retention: &retentionConf{
			Locations: &locationRetentionConf{RetentionDays: &positive},
		}}},
		{
			name:    "negative retentionDays",
			cfg:     &Config{Retention: &retentionConf{Locations: &locationRetentionConf{RetentionDays: &neg}}},
			wantErr: "retention.locations.retentionDays must be >= 0, got -1",
		},
		{
			name:    "negative location maxPerDevice",
			cfg:     &Config{Retention: &retentionConf{Locations: &locationRetentionConf{MaxPerDevice: &neg}}},
			wantErr: "retention.locations.maxPerDevice must be >= 0, got -1",
		},
		{
			name:    "negative dedupWindowMs",
			cfg:     &Config{Retention: &retentionConf{Alarms: &alarmRetentionConf{DedupWindowMS: &neg}}},
			wantErr: "retention.alarms.dedupWindowMs must be >= 0, got -1",
		},
		{
			name:    "negative confirmExpireMs",
			cfg:     &Config{Retention: &retentionConf{Alarms: &alarmRetentionConf{ConfirmExpireMS: &neg}}},
			wantErr: "retention.alarms.confirmExpireMs must be >= 0, got -1",
		},
		{
			name:    "negative alarm maxPerDevice",
			cfg:     &Config{Retention: &retentionConf{Alarms: &alarmRetentionConf{MaxPerDevice: &neg}}},
			wantErr: "retention.alarms.maxPerDevice must be >= 0, got -1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.ValidateRetention()
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Equal(t, tt.wantErr, err.Error())
		})
	}
}
