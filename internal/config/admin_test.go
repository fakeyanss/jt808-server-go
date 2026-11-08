package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func adminFloat(v float64) *float64 { return &v }
func adminInt(v int) *int           { return &v }

func TestParseAdminDisabledByDefault(t *testing.T) {
	cfg := &Config{}
	opts, enabled, err := cfg.ParseAdmin()
	require.NoError(t, err)
	require.False(t, enabled)
	require.Empty(t, opts)

	var nilCfg *Config
	_, enabled, err = nilCfg.ParseAdmin()
	require.NoError(t, err)
	require.False(t, enabled)
}

func TestParseAdminRequiresTokenWhenEnabled(t *testing.T) {
	cfg := &Config{Admin: &adminConf{Enable: true}}
	_, enabled, err := cfg.ParseAdmin()
	require.Error(t, err)
	require.False(t, enabled)

	cfg.Admin.Token = "   "
	_, _, err = cfg.ParseAdmin()
	require.Error(t, err)
}

func TestParseAdminDefaultsAndOverrides(t *testing.T) {
	cfg := &Config{Admin: &adminConf{Enable: true, Token: "secret"}}
	opts, enabled, err := cfg.ParseAdmin()
	require.NoError(t, err)
	require.True(t, enabled)
	require.Equal(t, DefaultAdminAddr, opts.Addr)
	require.Equal(t, "secret", opts.Token)
	require.Equal(t, DefaultAdminRatePerSecond, opts.RatePerSecond)
	require.Equal(t, DefaultAdminBurst, opts.Burst)
	require.Equal(t, AdminRateKeyByCredential, opts.KeyBy)

	cfg.Admin = &adminConf{
		Enable: true,
		Addr:   "0.0.0.0:9100",
		Token:  "secret",
		RateLimit: &adminRateLimitConf{
			RatePerSecond: adminFloat(2.5),
			Burst:         adminInt(5),
			KeyBy:         AdminRateKeyBySource,
		},
	}
	opts, enabled, err = cfg.ParseAdmin()
	require.NoError(t, err)
	require.True(t, enabled)
	require.Equal(t, "0.0.0.0:9100", opts.Addr)
	require.Equal(t, 2.5, opts.RatePerSecond)
	require.Equal(t, 5, opts.Burst)
	require.Equal(t, AdminRateKeyBySource, opts.KeyBy)
}

func TestParseAdminRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name string
		conf *adminConf
	}{
		{name: "missing host", conf: &adminConf{Enable: true, Token: "secret", Addr: ":9000"}},
		{name: "missing port", conf: &adminConf{Enable: true, Token: "secret", Addr: "127.0.0.1"}},
		{name: "port out of range", conf: &adminConf{Enable: true, Token: "secret", Addr: "127.0.0.1:70000"}},
		{
			name: "non-positive rate",
			conf: &adminConf{Enable: true, Token: "secret", RateLimit: &adminRateLimitConf{RatePerSecond: adminFloat(0)}},
		},
		{
			name: "non-positive burst",
			conf: &adminConf{Enable: true, Token: "secret", RateLimit: &adminRateLimitConf{Burst: adminInt(0)}},
		},
		{
			name: "unknown keyBy",
			conf: &adminConf{Enable: true, Token: "secret", RateLimit: &adminRateLimitConf{KeyBy: "tenant"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{Admin: tc.conf}
			_, enabled, err := cfg.ParseAdmin()
			require.Error(t, err)
			require.False(t, enabled)
		})
	}
}
