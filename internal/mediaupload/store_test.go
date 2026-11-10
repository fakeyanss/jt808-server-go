package mediaupload

import (
	"crypto/md5"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// testGeoBody 构造 28 字节位置体：报警标志、状态、经纬度、高程、速度、方向与 BCD 时间。
func testGeoBody(alarmSign uint32, bcdTime string) []byte {
	body := hex.WriteDoubleWord(nil, alarmSign)
	body = hex.WriteDoubleWord(body, 0x00000001)
	body = hex.WriteDoubleWord(body, 30000000)
	body = hex.WriteDoubleWord(body, 120000000)
	body = hex.WriteWord(body, 100)
	body = hex.WriteWord(body, 600)
	body = hex.WriteWord(body, 90)
	return hex.WriteBCD(body, bcdTime)
}

func testUpload(phone string, mediaID uint32) *model.Msg0801 {
	return &model.Msg0801{
		Header:              &model.MsgHeader{MsgID: 0x0801, PhoneNumber: phone, Attr: &model.MsgBodyAttr{}},
		MultiMediaID:        mediaID,
		MultiMediaType:      0,
		MultiMediaContainer: 0,
		EventID:             1,
		LogicChannelID:      2,
		GeoAlarmBody:        testGeoBody(0x00000002, "261112103000"),
		FragmentData:        []byte("jpeg-bytes"),
	}
}

// 非分包的单条 0x0801 同样落盘，并按设备/日期/mediaId 分层。
func TestStoreSaveWritesFileAndMetadata(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)

	upload := testUpload("188765432105", 9001)
	file, err := store.Save(upload)
	require.NoError(t, err)

	require.Equal(t, "188765432105/20261112/9001.jpg", filepath.ToSlash(file.RelativePath))
	require.Equal(t, int64(len("jpeg-bytes")), file.FileSize)
	require.Equal(t, fmt.Sprintf("%x", md5.Sum([]byte("jpeg-bytes"))), file.MD5)
	require.Equal(t, uint32(0x00000002), file.AlarmSign)
	require.Equal(t, uint8(2), file.ChannelID)
	require.Equal(t, uint8(1), file.EventID)
	require.True(t, time.Date(2026, 11, 12, 10, 30, 0, 0, hex.ProtocolTimeZone).Equal(file.LocationTime))

	content, err := os.ReadFile(filepath.Join(store.Root(), file.RelativePath))
	require.NoError(t, err)
	require.Equal(t, []byte("jpeg-bytes"), content)

	handle, err := store.Open(file.RelativePath)
	require.NoError(t, err)
	defer handle.Close()
	info, err := handle.Stat()
	require.NoError(t, err)
	require.Equal(t, file.FileSize, info.Size())
}

// 同一 mediaId 重复上报应幂等覆盖，不产生额外文件。
func TestStoreSaveOverwritesSameMediaID(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	require.NoError(t, err)

	upload := testUpload("188765432106", 9002)
	_, err = store.Save(upload)
	require.NoError(t, err)
	upload.FragmentData = []byte("second-version")
	file, err := store.Save(upload)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(root, file.RelativePath))
	require.NoError(t, err)
	require.Equal(t, []byte("second-version"), content)
	require.Equal(t, int64(len("second-version")), file.FileSize)

	// 临时文件不残留。
	entries, err := os.ReadDir(filepath.Dir(filepath.Join(root, file.RelativePath)))
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

// 未知格式回退为 bin，缺失位置体时不阻断落盘。
func TestStoreSaveFallsBackForUnknownFormat(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	upload := testUpload("188765432107", 9003)
	upload.MultiMediaContainer = 9
	upload.GeoAlarmBody = nil

	file, err := store.Save(upload)
	require.NoError(t, err)
	require.Equal(t, ".bin", filepath.Ext(file.RelativePath))
	require.Equal(t, "188765432107/00000000/9003.bin", filepath.ToSlash(file.RelativePath))
	require.True(t, file.LocationTime.IsZero())
	require.Equal(t, uint32(0), file.AlarmSign)
}

// 路径校验必须阻断目录穿越与绝对路径。
func TestStoreRejectsPathTraversal(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)

	bad := testUpload("../evil", 9004)
	_, err = store.Save(bad)
	require.ErrorIs(t, err, ErrInvalidMediaPath)

	for _, path := range []string{"", ".", "..", "../escape", "a/../../escape", "/etc/passwd"} {
		_, err := store.Open(path)
		require.ErrorIs(t, err, ErrInvalidMediaPath, "path %q must be rejected", path)
	}
}

// BCD 时间必须固定按 GMT+8 解析，改变 time.Local 不能影响解析结果。
func TestDecodeLocationIsTimezoneIndependent(t *testing.T) {
	original := time.Local
	t.Cleanup(func() { time.Local = original })

	body := testGeoBody(0x00000002, "261112103000")
	expected := time.Date(2026, 11, 12, 10, 30, 0, 0, hex.ProtocolTimeZone)
	for _, location := range []*time.Location{
		time.UTC,
		time.FixedZone("UTC-5", -5*60*60),
		time.FixedZone("UTC+13", 13*60*60),
	} {
		time.Local = location
		got := decodeLocation(body).locationTime
		require.True(t, expected.Equal(got), "time.Local=%s parsed %s, want %s", location, got, expected)
		require.Equal(t, expected.Unix(), got.Unix(), "time.Local=%s produced different instant", location)
	}
}

// 文件缺失时错误应可被 errors.Is(err, os.ErrNotExist) 识别。
func TestStoreOpenMissingFile(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	_, err = store.Open("188765432108/20261112/9999.jpg")
	require.True(t, errors.Is(err, os.ErrNotExist))
}
