package repository

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAlarmRepositoryRecordsDeduplicatesAndConfirmsAfterCommandAck(t *testing.T) {
	cache := newMemoryAlarmRepository()
	cache.Record("phone", 12, 1, 100, 200, "261028204500")
	cache.Record("phone", 12, 1, 100, 200, "261028204500")
	events := cache.List("phone")
	require.Len(t, events, 1)
	require.False(t, events[0].Confirmed)
	cache.RegisterConfirmation("phone", 80, 12, 1)
	require.True(t, cache.ConfirmByCommand("phone", 80))
	require.True(t, cache.List("phone")[0].Confirmed)
	require.False(t, cache.ConfirmByCommand("phone", 80), "a confirmation command should be consumed once")
}
