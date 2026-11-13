package storage

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 对象 key 必须能独立还原终端、通道、起止时间与序号。
func TestMediaSegmentObjectKeyRoundTrip(t *testing.T) {
	start := time.Date(2026, 11, 14, 8, 30, 0, 0, time.FixedZone("CST", 8*3600))
	duration := 10 * time.Minute
	key := MediaSegmentObjectKey("13800138000", 3, "live", start, duration, 7)
	expected := fmt.Sprintf("media/segments/13800138000/ch3/live/20261114/%d_%d_%d_7.ts",
		start.UnixMilli(), duration.Milliseconds(), start.Add(duration).UnixMilli())
	require.Equal(t, expected, key)

	info, err := ParseMediaSegmentKey(key)
	require.NoError(t, err)
	require.Equal(t, "13800138000", info.Phone)
	require.Equal(t, uint8(3), info.Channel)
	require.Equal(t, "live", info.Kind)
	require.True(t, start.Equal(info.StartAt))
	require.Equal(t, duration, info.Duration)
	require.True(t, start.Add(duration).Equal(info.EndAt))
	require.Equal(t, 7, info.Sequence)
	require.Equal(t, key, info.ObjectKey)
}

func TestParseMediaSegmentKeyRejectsBrokenMetadata(t *testing.T) {
	cases := []string{
		"media/segments/13800138000/ch1/live/20261114/1_2_4_0.ts", // end 与 start+duration 不一致
		"media/segments/13800138000/chx/live/20261114/1_2_3_0.ts", // 通道非法
		"media/segments/13800138000/ch1/live/20261114/a_2_3_0.ts", // 起始时间非法
		"media/segments/13800138000/ch1/live/20261114/1_2_3.ts",   // 字段不足
		"attachment/segments/13800138000/ch1/live/20261114/1_2_3_0.ts",
	}
	for _, key := range cases {
		_, err := ParseMediaSegmentKey(key)
		require.Error(t, err, key)
	}
}

// 附件切片 key 必须能独立还原终端、报警号、文件名、偏移与总长，
// 使 control 在不依赖任何上报内容的情况下重建进度。
func TestAttachmentSegmentObjectKeyRoundTrip(t *testing.T) {
	key := AttachmentSegmentObjectKey(SourceSubia, "13800138000", "A1", "clip.bin", 8, 8, 24, 1)
	require.Equal(t, "attachment/subia/13800138000/A1/clip.bin/000000000008_000000000008_000000000024_00000001", key)

	info, err := ParseAttachmentSegmentKey(key)
	require.NoError(t, err)
	require.Equal(t, SourceSubia, info.Source)
	require.Equal(t, "13800138000", info.Phone)
	require.Equal(t, "A1", info.AlarmNo)
	require.Equal(t, "clip.bin", info.FileName)
	require.EqualValues(t, 8, info.Offset)
	require.EqualValues(t, 8, info.Size)
	require.EqualValues(t, 24, info.TotalSize)
	require.Equal(t, 1, info.Index)
	require.Equal(t, key, info.ObjectKey)
}

func TestParseAttachmentSegmentKeyRejectsBrokenMetadata(t *testing.T) {
	cases := []string{
		"attachment/subia/13800138000/A1/clip.bin/0_8_4_0",        // 段超出总长
		"attachment/subia/13800138000/A1/clip.bin/0_0_8_0",        // 段大小为 0
		"attachment/subia/13800138000/A1/clip.bin/0_8_8",          // 字段不足
		"attachment/unknown/13800138000/A1/clip.bin/0_8_8_0",      // 来源非法
		"media/segments/13800138000/ch1/live/20261114/1_2_3_0.ts", // 前缀不符
	}
	for _, key := range cases {
		_, err := ParseAttachmentSegmentKey(key)
		require.Error(t, err, key)
	}
}
