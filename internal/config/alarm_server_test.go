package config

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestParseAlarmServerDisabled(t *testing.T) {
	for _, cfg := range []*Config{
		nil,
		{},
		{AlarmServer: &alarmServerConf{}},
		{AlarmServer: &alarmServerConf{Port: "   "}},
	} {
		options, enabled, err := cfg.ParseAlarmServer()
		assert.NoError(t, err)
		assert.False(t, enabled)
		assert.Equal(t, AlarmServerOptions{}, options)
	}
}

func TestParseAlarmServerDefaults(t *testing.T) {
	cfg := &Config{AlarmServer: &alarmServerConf{Port: "7200"}}
	options, enabled, err := cfg.ParseAlarmServer()
	assert.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, AlarmServerOptions{
		Port:             7200,
		Dir:              DefaultAlarmServerDir,
		MaxFileSize:      DefaultAlarmServerMaxFileSize,
		MaxConcurrent:    DefaultAlarmServerMaxConcurrent,
		CompletedTTL:     DefaultAlarmServerCompletedTTL,
		Retention:        DefaultAlarmServerRetentionDays * 24 * time.Hour,
		SegmentSizeBytes: DefaultAlarmServerSegmentSize,
		StagingMaxBytes:  DefaultAlarmServerStagingMax,
		ProgressReport:   MediaSegmentReportOptions{Timeout: 3 * time.Second},
	}, options)
}

func TestParseAlarmServerRetentionDays(t *testing.T) {
	days := 30
	options, enabled, err := (&Config{AlarmServer: &alarmServerConf{Port: "7200", RetentionDays: &days}}).ParseAlarmServer()
	assert.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, 30*24*time.Hour, options.Retention)

	disabled := 0
	options, enabled, err = (&Config{AlarmServer: &alarmServerConf{Port: "7200", RetentionDays: &disabled}}).ParseAlarmServer()
	assert.NoError(t, err)
	assert.True(t, enabled)
	assert.Zero(t, options.Retention)

	negative := -1
	_, enabled, err = (&Config{AlarmServer: &alarmServerConf{Port: "7200", RetentionDays: &negative}}).ParseAlarmServer()
	assert.Error(t, err)
	assert.False(t, enabled)
}

func TestParseAlarmServerCompletedTTL(t *testing.T) {
	cases := []struct {
		raw      string
		expected time.Duration
	}{
		{raw: "", expected: DefaultAlarmServerCompletedTTL},
		{raw: "5m", expected: 5 * time.Minute},
		{raw: "0", expected: 0},
	}
	for _, item := range cases {
		cfg := &Config{AlarmServer: &alarmServerConf{Port: "7200", CompletedTTL: item.raw}}
		options, enabled, err := cfg.ParseAlarmServer()
		assert.NoError(t, err)
		assert.True(t, enabled)
		assert.Equal(t, item.expected, options.CompletedTTL, item.raw)
	}

	for _, raw := range []string{"-1m", "abc"} {
		cfg := &Config{AlarmServer: &alarmServerConf{Port: "7200", CompletedTTL: raw}}
		_, enabled, err := cfg.ParseAlarmServer()
		assert.Error(t, err, raw)
		assert.False(t, enabled)
	}
}

func TestParseAlarmServerExplicitValues(t *testing.T) {
	cfg := &Config{AlarmServer: &alarmServerConf{
		Port:          "7611",
		Dir:           "/var/lib/jt808/alarms",
		MaxFileSize:   1024,
		MaxConcurrent: 3,
		CompletedTTL:  "7m",
	}}
	options, enabled, err := cfg.ParseAlarmServer()
	assert.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, AlarmServerOptions{
		Port:             7611,
		Dir:              "/var/lib/jt808/alarms",
		MaxFileSize:      1024,
		MaxConcurrent:    3,
		CompletedTTL:     7 * time.Minute,
		Retention:        DefaultAlarmServerRetentionDays * 24 * time.Hour,
		SegmentSizeBytes: DefaultAlarmServerSegmentSize,
		StagingMaxBytes:  DefaultAlarmServerStagingMax,
		ProgressReport:   MediaSegmentReportOptions{Timeout: 3 * time.Second},
	}, options)
}

func TestParseAlarmServerUDPOnly(t *testing.T) {
	cfg := &Config{AlarmServer: &alarmServerConf{UDPPort: "7201"}}
	options, enabled, err := cfg.ParseAlarmServer()
	assert.NoError(t, err)
	assert.True(t, enabled)
	assert.Zero(t, options.Port)
	assert.Equal(t, uint16(7201), options.UDPPort)
}

func TestParseAlarmServerDualListeners(t *testing.T) {
	cfg := &Config{AlarmServer: &alarmServerConf{Port: "7200", UDPPort: "7201"}}
	options, enabled, err := cfg.ParseAlarmServer()
	assert.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, uint16(7200), options.Port)
	assert.Equal(t, uint16(7201), options.UDPPort)
}

func TestParseAlarmServerInvalidValues(t *testing.T) {
	cases := []struct {
		name string
		conf *alarmServerConf
	}{
		{name: "non numeric port", conf: &alarmServerConf{Port: "tcp"}},
		{name: "zero port", conf: &alarmServerConf{Port: "0"}},
		{name: "port too large", conf: &alarmServerConf{Port: "70000"}},
		{name: "non numeric udp port", conf: &alarmServerConf{UDPPort: "udp"}},
		{name: "zero udp port", conf: &alarmServerConf{UDPPort: "0"}},
		{name: "udp port too large", conf: &alarmServerConf{UDPPort: "70000"}},
		{name: "negative max file size", conf: &alarmServerConf{Port: "7200", MaxFileSize: -1}},
		{name: "max file size over uint32", conf: &alarmServerConf{Port: "7200", MaxFileSize: math.MaxUint32 + 1}},
		{name: "negative max concurrent", conf: &alarmServerConf{Port: "7200", MaxConcurrent: -1}},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			_, enabled, err := (&Config{AlarmServer: item.conf}).ParseAlarmServer()
			assert.Error(t, err)
			assert.False(t, enabled)
		})
	}
}

func TestParseAlarmServerAutoUpload(t *testing.T) {
	t.Run("disabled by default", func(t *testing.T) {
		options, enabled, err := (&Config{AlarmServer: &alarmServerConf{Port: "7200"}}).ParseAlarmServer()
		assert.NoError(t, err)
		assert.True(t, enabled)
		assert.False(t, options.AutoUpload)
		assert.Empty(t, options.UploadIP)
	})

	t.Run("advertises listen ports", func(t *testing.T) {
		options, enabled, err := (&Config{AlarmServer: &alarmServerConf{
			Port:       "7200",
			UDPPort:    "7201",
			AutoUpload: true,
			UploadIP:   "10.0.0.8",
		}}).ParseAlarmServer()
		assert.NoError(t, err)
		assert.True(t, enabled)
		assert.True(t, options.AutoUpload)
		assert.Equal(t, "10.0.0.8", options.UploadIP)
		assert.Equal(t, uint16(7200), options.Port)
		assert.Equal(t, uint16(7201), options.UDPPort)
	})

	t.Run("udp only listener", func(t *testing.T) {
		options, enabled, err := (&Config{AlarmServer: &alarmServerConf{
			UDPPort:    "7201",
			AutoUpload: true,
			UploadIP:   "10.0.0.8",
		}}).ParseAlarmServer()
		assert.NoError(t, err)
		assert.True(t, enabled)
		assert.Zero(t, options.Port)
		assert.Equal(t, uint16(7201), options.UDPPort)
	})

	t.Run("invalid auto upload values", func(t *testing.T) {
		cases := []*alarmServerConf{
			{Port: "7200", AutoUpload: true},
			{Port: "7200", AutoUpload: true, UploadIP: "10.0.0.8", UDPPort: "0"},
		}
		for i, conf := range cases {
			_, _, err := (&Config{AlarmServer: conf}).ParseAlarmServer()
			assert.Error(t, err, "case %d", i)
		}
	})
}
