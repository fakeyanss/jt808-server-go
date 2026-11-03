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
