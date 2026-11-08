package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseAPIDefaults(t *testing.T) {
	for _, cfg := range []*Config{nil, {}, {API: &apiConf{}}} {
		options, err := cfg.ParseAPI()
		assert.NoError(t, err)
		assert.Empty(t, options.Token)
		assert.Equal(t, DefaultAPIRatePerSecond, options.RatePerSecond)
		assert.Equal(t, DefaultAPIBurst, options.Burst)
	}
}

func TestParseAPIExplicitValues(t *testing.T) {
	rate := 12.5
	burst := 25
	options, err := (&Config{API: &apiConf{
		Auth:      &apiAuthConf{Token: "  secret  "},
		RateLimit: &apiRateLimitConf{RatePerSecond: &rate, Burst: &burst},
	}}).ParseAPI()
	assert.NoError(t, err)
	assert.Equal(t, "secret", options.Token)
	assert.Equal(t, 12.5, options.RatePerSecond)
	assert.Equal(t, 25, options.Burst)
}

func TestParseAPIRejectsInvalidRateLimit(t *testing.T) {
	zeroRate := 0.0
	zeroBurst := 0
	cases := []*apiConf{
		{RateLimit: &apiRateLimitConf{RatePerSecond: &zeroRate}},
		{RateLimit: &apiRateLimitConf{Burst: &zeroBurst}},
	}
	for i, conf := range cases {
		_, err := (&Config{API: conf}).ParseAPI()
		assert.Error(t, err, "case %d", i)
	}
}
