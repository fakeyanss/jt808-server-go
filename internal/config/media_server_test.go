package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestParseMediaServerDisabled(t *testing.T) {
	for _, cfg := range []*Config{
		nil,
		{},
		{MediaServer: &mediaServerConf{}},
		{MediaServer: &mediaServerConf{Port: "   "}},
		{MediaServer: &mediaServerConf{TCPPort: "   "}},
		{MediaServer: &mediaServerConf{Port: "  ", TCPPort: "  "}},
	} {
		options, enabled, err := cfg.ParseMediaServer()
		assert.NoError(t, err)
		assert.False(t, enabled)
		assert.Equal(t, MediaServerOptions{}, options)
	}
}

func TestParseMediaServerDefaults(t *testing.T) {
	cfg := &Config{MediaServer: &mediaServerConf{Port: "7300"}}
	options, enabled, err := cfg.ParseMediaServer()
	assert.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, uint16(7300), options.Port)
	assert.Equal(t, uint16(7301), options.HTTPPort)
	assert.Equal(t, DefaultMediaServerDir, options.Dir)
	assert.Equal(t, DefaultMediaServerMaxConcurrent, options.MaxConcurrent)
	assert.Equal(t, DefaultMediaServerMaxTCPConns, options.MaxTCPConns)
	assert.Equal(t, DefaultMediaServerRetentionDays*24*time.Hour, options.Retention)
	assert.Equal(t, "flv", options.Playback.Output)
}

func TestParseMediaServerTCPPortAlias(t *testing.T) {
	// 单一端口模式下 tcpPort 只是 port 的别名，两者必须一致。
	options, enabled, err := (&Config{MediaServer: &mediaServerConf{TCPPort: "7301"}}).ParseMediaServer()
	assert.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, uint16(7301), options.Port)
	assert.Equal(t, uint16(7302), options.HTTPPort)
	assert.Equal(t, DefaultMediaServerMaxTCPConns, options.MaxTCPConns)
}

func TestParseMediaServerBothTransports(t *testing.T) {
	options, enabled, err := (&Config{MediaServer: &mediaServerConf{
		Port:        "7300",
		TCPPort:     "7300",
		MaxTCPConns: 8,
	}}).ParseMediaServer()
	assert.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, uint16(7300), options.Port)
	assert.Equal(t, 8, options.MaxTCPConns)
}

func TestParseMediaServerRetentionDays(t *testing.T) {
	days := 30
	options, enabled, err := (&Config{MediaServer: &mediaServerConf{Port: "7300", RetentionDays: &days}}).ParseMediaServer()
	assert.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, 30*24*time.Hour, options.Retention)

	disabled := 0
	options, enabled, err = (&Config{MediaServer: &mediaServerConf{Port: "7300", RetentionDays: &disabled}}).ParseMediaServer()
	assert.NoError(t, err)
	assert.True(t, enabled)
	assert.Zero(t, options.Retention)
}

func TestParseMediaServerExplicitValues(t *testing.T) {
	cfg := &Config{MediaServer: &mediaServerConf{
		Port:          "7611",
		Dir:           "/var/lib/jt808/media",
		MaxConcurrent: 3,
	}}
	options, enabled, err := cfg.ParseMediaServer()
	assert.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, uint16(7611), options.Port)
	assert.Equal(t, "/var/lib/jt808/media", options.Dir)
	assert.Equal(t, 3, options.MaxConcurrent)
	assert.Equal(t, DefaultMediaServerMaxTCPConns, options.MaxTCPConns)
	assert.Equal(t, DefaultMediaServerRetentionDays*24*time.Hour, options.Retention)
}

func TestParseMediaServerInvalidValues(t *testing.T) {
	negative := -1
	cases := map[string]*mediaServerConf{
		"non numeric port":     {Port: "udp"},
		"zero port":            {Port: "0"},
		"port too large":       {Port: "70000"},
		"negative concurrency": {Port: "7300", MaxConcurrent: -1},
		"negative tcp conns":   {Port: "7300", MaxTCPConns: -1},
		"non numeric tcp port": {TCPPort: "tcp"},
		"zero tcp port":        {TCPPort: "0"},
		"tcp port too large":   {TCPPort: "70000"},
		"negative retention":   {Port: "7300", RetentionDays: &negative},
	}
	for name, conf := range cases {
		t.Run(name, func(t *testing.T) {
			_, enabled, err := (&Config{MediaServer: conf}).ParseMediaServer()
			assert.Error(t, err)
			assert.False(t, enabled)
		})
	}
}

func TestParseMediaServerPlaybackOutput(t *testing.T) {
	cfg := &Config{MediaServer: &mediaServerConf{
		Port: "7300",
		Playback: &mediaPlaybackConf{
			Output: "both",
			RTMP: &mediaRTMPConf{
				URL:           "rtmp://127.0.0.1:1935/live",
				StreamPrefix:  "cam-",
				PlayURLPrefix: "http://127.0.0.1:8080/live",
				QueueFrames:   64,
			},
		},
	}}
	options, enabled, err := cfg.ParseMediaServer()
	assert.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, "both", options.Playback.Output)
	assert.Equal(t, "rtmp://127.0.0.1:1935/live", options.Playback.RTMPURL)
	assert.Equal(t, "cam-", options.Playback.StreamPrefix)
	assert.Equal(t, "http://127.0.0.1:8080/live", options.Playback.PlayURLPrefix)
	assert.Equal(t, 64, options.Playback.QueueFrames)
}

func TestParseMediaServerPlaybackInvalid(t *testing.T) {
	cases := map[string]*mediaPlaybackConf{
		"unknown output":    {Output: "hls"},
		"rtmp without url":  {Output: "rtmp"},
		"both without rtmp": {Output: "both"},
		"rtmp url scheme":   {Output: "rtmp", RTMP: &mediaRTMPConf{URL: "http://127.0.0.1/live"}},
		"negative queue":    {Output: "flv", RTMP: &mediaRTMPConf{QueueFrames: -1}},
	}
	for name, playback := range cases {
		t.Run(name, func(t *testing.T) {
			_, enabled, err := (&Config{MediaServer: &mediaServerConf{Port: "7300", Playback: playback}}).ParseMediaServer()
			assert.Error(t, err)
			assert.False(t, enabled)
		})
	}
}
