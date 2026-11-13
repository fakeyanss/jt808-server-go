package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseUplinkDefaultsWhenSectionMissing(t *testing.T) {
	options, err := (&Config{}).ParseUplink()
	require.NoError(t, err)
	require.Equal(t, DefaultUplinkOptions(), options)

	// 实时定位 best-effort，批量定位与报警 at-least-once。
	require.Equal(t, UplinkReliabilityBestEffort, options.Location)
	require.Equal(t, UplinkReliabilityAtLeastOnce, options.BatchLocation)
	require.Equal(t, UplinkReliabilityAtLeastOnce, options.Alarm)
}

func TestParseUplinkOverridesAndNormalizes(t *testing.T) {
	options, err := (&Config{Uplink: &uplinkConf{
		Reliability: &uplinkReliabilityConf{
			Default:       "",
			Location:      " AT_LEAST_ONCE ",
			BatchLocation: "",
			Alarm:         "best_effort",
		},
	}}).ParseUplink()
	require.NoError(t, err)
	require.Equal(t, DefaultUplinkOptions().Default, options.Default)
	require.Equal(t, UplinkReliabilityAtLeastOnce, options.Location)
	require.Equal(t, DefaultUplinkOptions().BatchLocation, options.BatchLocation)
	require.Equal(t, UplinkReliabilityBestEffort, options.Alarm)
}

func TestParseUplinkRejectsUnknownReliability(t *testing.T) {
	_, err := (&Config{Uplink: &uplinkConf{
		Reliability: &uplinkReliabilityConf{Alarm: "at_most_once"},
	}}).ParseUplink()
	require.Error(t, err)
	require.Contains(t, err.Error(), "uplink.reliability.alarm")
}

func TestParseUplinkMatchesDefaultsWhenSectionEmpty(t *testing.T) {
	options, err := (&Config{Uplink: &uplinkConf{}}).ParseUplink()
	require.NoError(t, err)
	require.Equal(t, DefaultUplinkOptions(), options)
}

func TestParseUplinkAcceptsConfirmBeforeAckAlias(t *testing.T) {
	options, err := (&Config{Uplink: &uplinkConf{Reliability: &uplinkReliabilityConf{
		Alarm: "confirm-before-ack",
	}}}).ParseUplink()
	require.NoError(t, err)
	require.Equal(t, UplinkReliabilityAtLeastOnce, options.Alarm)
}

func TestParseUplinkSinkDefaultsToLocal(t *testing.T) {
	options, err := (&Config{}).ParseUplinkSink()
	require.NoError(t, err)
	require.Equal(t, UplinkSinkLocal, options.Mode)
	// local 不读取 http 配置，不产生外部依赖。
	require.Empty(t, options.HTTP.URL)
	require.Empty(t, options.HTTP.Token)
}

func TestParseUplinkSinkHTTPNormalizes(t *testing.T) {
	options, err := (&Config{Uplink: &uplinkConf{
		Sink: " HTTP ",
		HTTP: &uplinkHTTPConf{
			URL:         " https://control.example.com/uplink ",
			Token:       " shared-token ",
			TimeoutMS:   1500,
			QueueSize:   64,
			Workers:     2,
			MaxAttempts: 1,
			GatewayID:   " gw-test ",
			AuthGraceMS: 60000,
		},
	}}).ParseUplinkSink()
	require.NoError(t, err)
	require.Equal(t, UplinkSinkHTTP, options.Mode)
	require.Equal(t, "https://control.example.com/uplink", options.HTTP.URL)
	require.Equal(t, "shared-token", options.HTTP.Token)
	require.Equal(t, 1500*time.Millisecond, options.HTTP.Timeout)
	require.Equal(t, 64, options.HTTP.QueueSize)
	require.Equal(t, 2, options.HTTP.Workers)
	require.Equal(t, 1, options.HTTP.MaxAttempts)
	require.Equal(t, "gw-test", options.HTTP.GatewayID)
	require.Equal(t, time.Minute, options.HTTP.AuthGrace)
}

func TestParseUplinkSinkDefaultsAuthGrace(t *testing.T) {
	options, err := (&Config{Uplink: &uplinkConf{
		Sink: UplinkSinkHTTP,
		HTTP: &uplinkHTTPConf{URL: "http://127.0.0.1:9090/uplink", Token: "token"},
	}}).ParseUplinkSink()
	require.NoError(t, err)
	require.Equal(t, DefaultUplinkAuthGrace, options.HTTP.AuthGrace)
	require.Equal(t, 30*time.Minute, options.HTTP.AuthGrace)
}

func TestParseUplinkSinkDefaultsHTTPGatewayID(t *testing.T) {
	options, err := (&Config{Uplink: &uplinkConf{
		Sink: UplinkSinkHTTP,
		HTTP: &uplinkHTTPConf{URL: "http://127.0.0.1:9090/uplink", Token: "token"},
	}}).ParseUplinkSink()
	require.NoError(t, err)
	require.NotEmpty(t, options.HTTP.GatewayID)
	require.Contains(t, options.HTTP.GatewayID, "-")
}

func TestParseUplinkSinkRejectsInvalidConfig(t *testing.T) {
	cases := []struct {
		name   string
		conf   *uplinkConf
		errKey string
	}{
		{
			name:   "unknown mode",
			conf:   &uplinkConf{Sink: "mq"},
			errKey: "uplink.sink",
		},
		{
			name:   "missing http section",
			conf:   &uplinkConf{Sink: UplinkSinkHTTP},
			errKey: "uplink.http is required",
		},
		{
			name:   "missing url",
			conf:   &uplinkConf{Sink: UplinkSinkHTTP, HTTP: &uplinkHTTPConf{Token: "token"}},
			errKey: "uplink.http.url",
		},
		{
			name:   "relative url",
			conf:   &uplinkConf{Sink: UplinkSinkHTTP, HTTP: &uplinkHTTPConf{URL: "/uplink", Token: "token"}},
			errKey: "uplink.http.url",
		},
		{
			name:   "missing token",
			conf:   &uplinkConf{Sink: UplinkSinkHTTP, HTTP: &uplinkHTTPConf{URL: "http://127.0.0.1/uplink"}},
			errKey: "uplink.http.token",
		},
		{
			name:   "invalid timeout",
			conf:   &uplinkConf{Sink: UplinkSinkHTTP, HTTP: &uplinkHTTPConf{URL: "http://127.0.0.1/uplink", Token: "token", TimeoutMS: -1}},
			errKey: "uplink.http.timeoutMs",
		},
		{
			name:   "invalid queue size",
			conf:   &uplinkConf{Sink: UplinkSinkHTTP, HTTP: &uplinkHTTPConf{URL: "http://127.0.0.1/uplink", Token: "token", QueueSize: -1}},
			errKey: "uplink.http.queueSize",
		},
		{
			name:   "invalid workers",
			conf:   &uplinkConf{Sink: UplinkSinkHTTP, HTTP: &uplinkHTTPConf{URL: "http://127.0.0.1/uplink", Token: "token", Workers: 100}},
			errKey: "uplink.http.workers",
		},
		{
			name:   "invalid attempts",
			conf:   &uplinkConf{Sink: UplinkSinkHTTP, HTTP: &uplinkHTTPConf{URL: "http://127.0.0.1/uplink", Token: "token", MaxAttempts: 4}},
			errKey: "uplink.http.maxAttempts",
		},
		{
			name:   "invalid auth grace",
			conf:   &uplinkConf{Sink: UplinkSinkHTTP, HTTP: &uplinkHTTPConf{URL: "http://127.0.0.1/uplink", Token: "token", AuthGraceMS: 86400001}},
			errKey: "uplink.http.authGraceMs",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := (&Config{Uplink: tc.conf}).ParseUplinkSink()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.errKey)
		})
	}
}
