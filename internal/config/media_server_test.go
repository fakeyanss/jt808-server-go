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
	assert.Equal(t, MediaServerOptions{
		Port:          7300,
		Dir:           DefaultMediaServerDir,
		MaxConcurrent: DefaultMediaServerMaxConcurrent,
		MaxTCPConns:   DefaultMediaServerMaxTCPConns,
		Retention:     DefaultMediaServerRetentionDays * 24 * time.Hour,
	}, options)
}

func TestParseMediaServerTCPOnly(t *testing.T) {
	options, enabled, err := (&Config{MediaServer: &mediaServerConf{TCPPort: "7301"}}).ParseMediaServer()
	assert.NoError(t, err)
	assert.True(t, enabled)
	assert.Zero(t, options.Port)
	assert.Equal(t, uint16(7301), options.TCPPort)
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
	assert.Equal(t, uint16(7300), options.TCPPort)
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
	assert.Equal(t, MediaServerOptions{
		Port:          7611,
		Dir:           "/var/lib/jt808/media",
		MaxConcurrent: 3,
		MaxTCPConns:   DefaultMediaServerMaxTCPConns,
		Retention:     DefaultMediaServerRetentionDays * 24 * time.Hour,
	}, options)
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
