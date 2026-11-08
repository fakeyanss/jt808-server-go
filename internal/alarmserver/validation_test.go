package alarmserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateAttachmentFileNameFormat(t *testing.T) {
	valid := []string{
		"00_64_6401_01_ALARM.jpg",
		"01_0_6400_9_A1.bin",
		"02_65_6501_0_X.png",
		"03_64_66AF_1_B.jpeg",
		"04_37_6712_123_ALARM-2026.mp4",
		"00_64_6401_01_ALARM.JPEG",
	}
	for _, name := range valid {
		require.NoError(t, validateAttachmentFileNameFormat(name), name)
	}

	invalid := []string{
		"dup.jpg",
		"00_64_6401_01_ALARM",
		"05_64_6401_01_ALARM.jpg",
		"00_38_6401_01_ALARM.jpg",
		"00_66_6401_01_ALARM.jpg",
		"00_64_6301_01_ALARM.jpg",
		"00_64_640_01_ALARM.jpg",
		"00_64_6401_A_ALARM.jpg",
		"00_64_6401_01_.jpg",
		"00_64_6401_01_ALARM.jpg_",
	}
	for _, name := range invalid {
		require.ErrorIs(t, validateAttachmentFileNameFormat(name), ErrInvalidFileNameFormat, name)
	}
}

func TestValidateAlarmSign(t *testing.T) {
	require.NoError(t, validateAlarmSign(testAlarmSign()))

	t.Run("terminal id padding is allowed", func(t *testing.T) {
		sign := testAlarmSign()
		copy(sign[:7], []byte{'A', 'B', 'C', 0, 0, 0, 0})
		require.NoError(t, validateAlarmSign(sign))
	})

	t.Run("reserved byte is ignored", func(t *testing.T) {
		sign := testAlarmSign()
		sign[15] = 0xFF
		require.NoError(t, validateAlarmSign(sign))
	})

	cases := []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{name: "wrong length", mutate: func(sign []byte) []byte { return sign[:15] }},
		{name: "lowercase terminal id", mutate: func(sign []byte) []byte {
			sign[0] = 'a'
			return sign
		}},
		{name: "empty terminal id", mutate: func(sign []byte) []byte {
			copy(sign[:7], make([]byte, 7))
			return sign
		}},
		{name: "non decimal bcd nibble", mutate: func(sign []byte) []byte {
			sign[7] = 0x1A
			return sign
		}},
		{name: "invalid calendar date", mutate: func(sign []byte) []byte {
			sign[8] = 0x02
			sign[9] = 0x30
			return sign
		}},
		{name: "invalid hour", mutate: func(sign []byte) []byte {
			sign[10] = 0x24
			return sign
		}},
		{name: "zero attachment count", mutate: func(sign []byte) []byte {
			sign[14] = 0
			return sign
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, validateAlarmSign(tt.mutate(testAlarmSign())), ErrInvalidAlarmSign)
		})
	}
}

func TestAlarmDateFallsBackForInvalidTime(t *testing.T) {
	sign := testAlarmSign()
	sign[8] = 0x13 // 13 月
	require.Equal(t, "20260101", alarmDate(sign, "20260101"))
	require.Equal(t, nowDate(), alarmDate(sign, ""))
}

func TestMetadataValidationFlags(t *testing.T) {
	assembler, dir := newTestAssembler(t)
	phone := "013800000001"

	t.Run("conformant file and alarm sign", func(t *testing.T) {
		fileName := "00_64_6401_01_ALARM.jpg"
		source := buildSource(100)
		registerTestFile(t, assembler, phone, fileName, len(source))
		require.NoError(t, assembler.WriteChunk(phone, fileName, 0, source))
		result, err := assembler.CompleteCheck(phone, fileName)
		require.NoError(t, err)
		require.True(t, result.Complete)

		meta, err := readMetadata(filepath.Join(dir, phone, "20260107", "ALARM-0001", fileName) + metaSuffix)
		require.NoError(t, err)
		require.True(t, meta.FileNameConformant)
		require.True(t, meta.AlarmSignValid)
	})

	t.Run("non conformant file name still accepted", func(t *testing.T) {
		fileName := "legacy.jpg"
		source := buildSource(100)
		registerTestFile(t, assembler, phone, fileName, len(source))
		require.NoError(t, assembler.WriteChunk(phone, fileName, 0, source))
		result, err := assembler.CompleteCheck(phone, fileName)
		require.NoError(t, err)
		require.True(t, result.Complete)

		meta, err := assembler.FindMetadata(phone, fileName)
		require.NoError(t, err)
		require.False(t, meta.FileNameConformant)
	})

	t.Run("non conformant alarm sign still accepted", func(t *testing.T) {
		fileName := "00_64_6401_02_ALARM.jpg"
		source := buildSource(100)
		sign := testAlarmSign()
		sign[14] = 0
		err := assembler.RegisterAlarm(AttachmentInfo{
			Phone:       phone,
			TerminalID:  "ABC1234",
			AlarmSign:   sign,
			AlarmNo:     "ALARM-0002",
			Attachments: []AttachmentEntry{{FileName: fileName, FileSize: uint32(len(source))}},
		})
		require.NoError(t, err)
		require.NoError(t, assembler.WriteChunk(phone, fileName, 0, source))
		result, err := assembler.CompleteCheck(phone, fileName)
		require.NoError(t, err)
		require.True(t, result.Complete)

		meta, err := assembler.FindMetadata(phone, fileName)
		require.NoError(t, err)
		require.True(t, meta.FileNameConformant)
		require.False(t, meta.AlarmSignValid)
	})
}

func TestMetadataFlagsRecomputedFromLegacyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "00_64_6401_01_ALARM.meta.json")
	legacy := `{"phone":"013800000001","alarmSignHex":"41424331323334260107120000000100","fileName":"00_64_6401_01_ALARM.jpg"}`
	require.NoError(t, os.WriteFile(path, []byte(legacy), 0o644))

	meta, err := readMetadata(path)
	require.NoError(t, err)
	require.True(t, meta.FileNameConformant)
	require.True(t, meta.AlarmSignValid)
}

func TestRegisterAlarmRejectsPathTraversal(t *testing.T) {
	assembler, _ := newTestAssembler(t)
	err := assembler.RegisterAlarm(AttachmentInfo{
		Phone:       "013800000001",
		AlarmSign:   testAlarmSign(),
		AlarmNo:     "ALARM-0001",
		Attachments: []AttachmentEntry{{FileName: "../evil.jpg", FileSize: 10}},
	})
	require.ErrorIs(t, err, ErrInvalidFileName)
}
