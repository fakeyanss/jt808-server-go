package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAlarmRepositoryRecordsDeduplicatesAndConfirmsAfterCommandAck(t *testing.T) {
	ctx := context.Background()
	cache := NewAlarmRepository()
	require.NoError(t, cache.Record(ctx, "phone", 12, 1, 100, 200, "261028204500"))
	require.NoError(t, cache.Record(ctx, "phone", 12, 1, 100, 200, "261028204500"))
	events, err := cache.List(ctx, "phone")
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.False(t, events[0].Confirmed)
	require.NoError(t, cache.RegisterConfirmation(ctx, "phone", 80, 12, 1))
	confirmed, err := cache.ConfirmByCommand(ctx, "phone", 80)
	require.NoError(t, err)
	require.True(t, confirmed)
	events, err = cache.List(ctx, "phone")
	require.NoError(t, err)
	require.True(t, events[0].Confirmed)
	confirmed, err = cache.ConfirmByCommand(ctx, "phone", 80)
	require.NoError(t, err)
	require.False(t, confirmed, "a confirmation command should be consumed once")
}

func TestAlarmRepositoryDefaultRetentionPolicy(t *testing.T) {
	ctx := context.Background()
	cache := NewAlarmRepository()
	require.NoError(t, cache.Record(ctx, "phone", 1, 1, 0, 0, "261028204500"))
	time.Sleep(time.Millisecond)
	// 同一流水号在默认 30 分钟去重窗口内应视为重复上报。
	require.NoError(t, cache.Record(ctx, "phone", 1, 2, 0, 0, "261028204500"))
	events, err := cache.List(ctx, "phone")
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, uint32(2), events[0].AlarmSign)
}

func TestAlarmRepositoryDisabledPolicy(t *testing.T) {
	ctx := context.Background()
	cache := NewAlarmRepository(AlarmRetention{})
	require.NoError(t, cache.Record(ctx, "phone", 1, 1, 0, 0, "261028204500"))
	require.NoError(t, cache.Record(ctx, "phone", 1, 1, 0, 0, "261028204500"))
	events, err := cache.List(ctx, "phone")
	require.NoError(t, err)
	require.Len(t, events, 2, "dedupWindow <= 0 should append every report")
	require.NoError(t, cache.RegisterConfirmation(ctx, "phone", 80, 1, 1))
	confirmed, err := cache.ConfirmByCommand(ctx, "phone", 80)
	require.NoError(t, err)
	require.False(t, confirmed, "confirmExpire <= 0 should expire the command link immediately")
}

func TestAlarmRepositoryMaxPerDevice(t *testing.T) {
	ctx := context.Background()
	cache := NewAlarmRepository(AlarmRetention{MaxPerDevice: 2})
	for serial := uint16(1); serial <= 3; serial++ {
		require.NoError(t, cache.Record(ctx, "phone", serial, 1, 0, 0, "261028204500"))
	}
	events, err := cache.List(ctx, "phone")
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, uint16(2), events[0].MessageSerial)
	require.Equal(t, uint16(3), events[1].MessageSerial)
}
