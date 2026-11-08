package config

import (
	"testing"

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
		SpoolDir: " ./data/custom-spool ",
	}}).ParseUplink()
	require.NoError(t, err)
	require.Equal(t, DefaultUplinkOptions().Default, options.Default)
	require.Equal(t, UplinkReliabilityAtLeastOnce, options.Location)
	require.Equal(t, DefaultUplinkOptions().BatchLocation, options.BatchLocation)
	require.Equal(t, UplinkReliabilityBestEffort, options.Alarm)
	require.Equal(t, "./data/custom-spool", options.SpoolDir)
}

func TestParseUplinkRejectsUnknownReliability(t *testing.T) {
	_, err := (&Config{Uplink: &uplinkConf{
		Reliability: &uplinkReliabilityConf{Alarm: "at_most_once"},
	}}).ParseUplink()
	require.Error(t, err)
	require.Contains(t, err.Error(), "uplink.reliability.alarm")
}

func TestParseUplinkKeepsDefaultSpoolDirWhenUnset(t *testing.T) {
	options, err := (&Config{Uplink: &uplinkConf{}}).ParseUplink()
	require.NoError(t, err)
	require.Equal(t, DefaultUplinkSpoolDir, options.SpoolDir)
}
