package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseMediaServerDisabled(t *testing.T) {
	for _, cfg := range []*Config{
		nil,
		{},
		{MediaServer: &mediaServerConf{}},
		{MediaServer: &mediaServerConf{Port: "   "}},
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
	}, options)
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
	}, options)
}

func TestParseMediaServerInvalidValues(t *testing.T) {
	cases := map[string]*mediaServerConf{
		"non numeric port":     {Port: "udp"},
		"zero port":            {Port: "0"},
		"port too large":       {Port: "70000"},
		"negative concurrency": {Port: "7300", MaxConcurrent: -1},
	}
	for name, conf := range cases {
		t.Run(name, func(t *testing.T) {
			_, enabled, err := (&Config{MediaServer: conf}).ParseMediaServer()
			assert.Error(t, err)
			assert.False(t, enabled)
		})
	}
}
