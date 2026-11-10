package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/blob"
	"github.com/fakeyanss/jt808-server-go/internal/codec/hash"
	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/protocol"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
	"github.com/fakeyanss/jt808-server-go/internal/uplink"
)

func locationBody() []byte {
	body := hex.WriteDoubleWord(nil, 0x00000002)
	body = hex.WriteDoubleWord(body, 0x00000001)
	body = hex.WriteDoubleWord(body, 30000000)
	body = hex.WriteDoubleWord(body, 120000000)
	body = hex.WriteWord(body, 100)
	body = hex.WriteWord(body, 600)
	body = hex.WriteWord(body, 90)
	return hex.WriteBCD(body, "261112103000")
}

func mediaBody(mediaID uint32, mediaType uint8, payload []byte) []byte {
	body := hex.WriteDoubleWord(nil, mediaID)
	body = append(body, mediaType, 0, 1, 2)
	body = append(body, locationBody()...)
	return append(body, payload...)
}

func newTestBlob(t *testing.T) *blob.SharedVolume {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, blob.MarkerFile), nil, 0o640))
	volume, err := blob.NewSharedVolume(root, true)
	require.NoError(t, err)
	return volume
}

func registerTestDevice(t *testing.T, phone string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, storage.Default().Devices.Upsert(ctx, &model.Device{Phone: phone, AuthCode: "AUTH"}))
	t.Cleanup(func() { _ = storage.Default().Devices.DeleteByPhone(ctx, phone) })
}

// 档案不存在时返回 409 unknown_device，供网关做有界重投。
func TestBusinessExecutorRejectsUnknownDevice(t *testing.T) {
	var unknown int
	executor := NewBusinessExecutor(BusinessOptions{
		Devices: storage.Default().Devices,
		Files:   storage.Default().MediaFiles,
		OnUnknownDevice: func(string) {
			unknown++
		},
	})
	_, err := executor.ConsumeEvent(context.Background(), contracts.UplinkEvent{
		EventID: "evt-unknown",
		MsgID:   0x0200,
		Phone:   "188765400001",
		Body:    locationBody(),
	})
	var statusErr *uplink.StatusError
	require.ErrorAs(t, err, &statusErr)
	require.Equal(t, 409, statusErr.Status)
	require.Equal(t, "unknown_device", statusErr.Code)
	require.Equal(t, 1, unknown)
}

// 0x0801 非图片类型仍应答，但不写文件、不落库，并上报指标。
func TestBusinessExecutorAcksNonImageMedia(t *testing.T) {
	const phone = "188765400002"
	registerTestDevice(t, phone)
	var rejected int
	volume := newTestBlob(t)
	executor := NewBusinessExecutor(BusinessOptions{
		Devices: storage.Default().Devices,
		Files:   storage.Default().MediaFiles,
		Blob:    volume,
		OnMediaTypeRejected: func(string, uint8) {
			rejected++
		},
	})

	result, err := executor.ConsumeEvent(context.Background(), contracts.UplinkEvent{
		EventID: "evt-audio",
		MsgID:   0x0801,
		Phone:   phone,
		Body:    mediaBody(9101, 1, []byte("audio")),
	})
	require.NoError(t, err)
	require.True(t, result.Accepted)
	require.Equal(t, 1, rejected)

	_, err = storage.Default().MediaFiles.Get(context.Background(), phone, 9101)
	require.ErrorIs(t, err, repository.ErrMediaFileNotFound)
}

// 图片按“写文件 → 写库 → 标记已提交”的顺序落库，最终状态为 committed。
func TestBusinessExecutorIngestsImageMedia(t *testing.T) {
	const phone = "188765400003"
	registerTestDevice(t, phone)
	volume := newTestBlob(t)
	executor := NewBusinessExecutor(BusinessOptions{
		Devices: storage.Default().Devices,
		Files:   storage.Default().MediaFiles,
		Blob:    volume,
	})

	result, err := executor.ConsumeEvent(context.Background(), contracts.UplinkEvent{
		EventID: "evt-image",
		MsgID:   0x0801,
		Phone:   phone,
		Body:    mediaBody(9102, 0, []byte("jpeg-bytes")),
	})
	require.NoError(t, err)
	require.True(t, result.Accepted)

	file, err := storage.Default().MediaFiles.Get(context.Background(), phone, 9102)
	require.NoError(t, err)
	require.Equal(t, int64(len("jpeg-bytes")), file.FileSize)

	info, err := volume.Stat(context.Background(), file.RelativePath)
	require.NoError(t, err)
	require.Equal(t, contracts.BlobStateCommitted, info.State)
}

// 写库失败时返回错误（网关不应答），文件停留在 pending 交给巡检。
func TestBusinessExecutorKeepsPendingWhenSaveFails(t *testing.T) {
	const phone = "188765400004"
	registerTestDevice(t, phone)
	volume := newTestBlob(t)
	// 先建文件再 mock 写库，确保 Save 失败发生在 Promote 之后。
	executor := NewBusinessExecutor(BusinessOptions{
		Devices: storage.Default().Devices,
		Files:   failingMediaFiles{},
		Blob:    volume,
	})

	_, err := executor.ConsumeEvent(context.Background(), contracts.UplinkEvent{
		EventID: "evt-save-fail",
		MsgID:   0x0801,
		Phone:   phone,
		Body:    mediaBody(9103, 0, []byte("jpeg-bytes")),
	})
	require.Error(t, err)

	info, statErr := volume.Stat(context.Background(), phone+"/20261112/9103.jpg")
	require.NoError(t, statErr)
	require.Equal(t, contracts.BlobStatePending, info.State)
}

// 状态标记失败不影响业务成功：引用已建立，由巡检补 committed。
func TestBusinessExecutorAcceptsWhenCommitMarkerFails(t *testing.T) {
	const phone = "188765400005"
	registerTestDevice(t, phone)
	volume := &failingCommitBlob{SharedVolume: newTestBlob(t)}
	var commitFailed int
	executor := NewBusinessExecutor(BusinessOptions{
		Devices: storage.Default().Devices,
		Files:   storage.Default().MediaFiles,
		Blob:    volume,
		OnBlobCommitFailed: func(string, uint32) {
			commitFailed++
		},
	})

	result, err := executor.ConsumeEvent(context.Background(), contracts.UplinkEvent{
		EventID: "evt-commit-fail",
		MsgID:   0x0801,
		Phone:   phone,
		Body:    mediaBody(9104, 0, []byte("jpeg-bytes")),
	})
	require.NoError(t, err)
	require.True(t, result.Accepted)
	require.Equal(t, 1, commitFailed)
}

// 网关归属的消息被投到管控时直接接受，不制造重投。
func TestBusinessExecutorIgnoresGatewayOwnedEvents(t *testing.T) {
	executor := NewBusinessExecutor(BusinessOptions{Devices: storage.Default().Devices})
	result, err := executor.ConsumeEvent(context.Background(), contracts.UplinkEvent{
		EventID: "evt-gateway",
		MsgID:   0x0002,
		Phone:   "188765400006",
	})
	require.NoError(t, err)
	require.True(t, result.Accepted)
}

// 0x0001 归属改为 OwnerBoth 后，控制面业务半边是 no-op：应答由 uplink
// 入口直接交给 outbox，不在业务执行器里重复处理。
func TestBusinessExecutorAcceptsTerminalReplyHalf(t *testing.T) {
	ctx := context.Background()
	devices := repository.NewDeviceRepository()
	require.NoError(t, devices.Upsert(ctx, &model.Device{Phone: "188765400006"}))
	executor := NewBusinessExecutor(BusinessOptions{Devices: devices})
	result, err := executor.ConsumeEvent(ctx, contracts.UplinkEvent{
		EventID: "evt-reply",
		MsgID:   0x0001,
		Phone:   "188765400006",
		Body:    []byte{0x81, 0x05, 0x00, 0x01, 0x00},
	})
	require.NoError(t, err)
	require.True(t, result.Accepted)
}

// TestRegistrationAndAuthStateAreOrderIndependent 覆盖乱序自愈：
//  1. 先 0x0102（档案不存在）→ 409，注册到达后重投成功；
//  2. 先 0x0100 再 0x0102。
//
// 两种顺序的终态必须一致：档案字段完整，在线状态、会话与最后通信时间不被注册覆盖。
func TestRegistrationAndAuthStateAreOrderIndependent(t *testing.T) {
	const phone = "188765410001"
	registration := func() contracts.UplinkEvent {
		return contracts.UplinkEvent{
			EventID:         "evt-reg",
			MsgID:           0x0100,
			Phone:           phone,
			ProtocolVersion: "2019",
			Body:            registrationBody2019(),
		}
	}
	auth := func() contracts.UplinkEvent {
		return contracts.UplinkEvent{
			EventID:         "evt-auth",
			MsgID:           0x0102,
			Phone:           phone,
			SessionID:       "sess-order",
			ProtocolVersion: "2019",
			Body:            authBody2019(authCode2019(phone), "123456789012345", "V1.0.0"),
		}
	}

	run := func(t *testing.T, authFirst bool) *model.Device {
		t.Helper()
		ctx := context.Background()
		stores := storage.NewMemory()
		devices := stores.Devices
		executor := NewBusinessExecutor(BusinessOptions{
			Devices:       devices,
			ProcessorDeps: protocol.ProcessorDeps{Storage: stores},
		})
		if authFirst {
			_, err := executor.ConsumeEvent(ctx, auth())
			require.Error(t, err, "鉴权先到时应返回 409 等待注册")
			_, err = executor.ConsumeEvent(ctx, registration())
			require.NoError(t, err)
			_, err = executor.ConsumeEvent(ctx, auth())
			require.NoError(t, err)
		} else {
			_, err := executor.ConsumeEvent(ctx, registration())
			require.NoError(t, err)
			_, err = executor.ConsumeEvent(ctx, auth())
			require.NoError(t, err)
		}
		device, err := devices.GetByPhone(ctx, phone)
		require.NoError(t, err)
		return device
	}

	authFirst := run(t, true)
	registerFirst := run(t, false)

	require.Equal(t, registerFirst.ID, authFirst.ID)
	require.Equal(t, registerFirst.Plate, authFirst.Plate)
	require.Equal(t, registerFirst.AuthCode, authFirst.AuthCode)
	require.Equal(t, registerFirst.VersionDesc, authFirst.VersionDesc)
	require.Equal(t, registerFirst.IMEI, authFirst.IMEI)
	require.Equal(t, registerFirst.SoftwareVersion, authFirst.SoftwareVersion)
	require.Equal(t, model.DeviceStatusOnline, authFirst.Status)
	require.Equal(t, "sess-order", authFirst.SessionID)
	require.False(t, authFirst.LastestComTime.IsZero())
	require.Equal(t, "京A12345", authFirst.Plate)
}

// registrationBody2019 构造 2019 版本 0x0100 报文体（厂商 11、型号 30、终端 ID 30）。
func registrationBody2019() []byte {
	var body []byte
	body = hex.WriteWord(body, 11)
	body = hex.WriteWord(body, 1101)
	body = hex.WriteString(body, "MANUFACTUR1")
	body = hex.WriteString(body, strings.Repeat("M", 30))
	body = hex.WriteString(body, strings.Repeat("D", 30))
	body = hex.WriteByte(body, 1)
	return hex.WriteGBK(body, "京A12345")
}

// authBody2019 构造 2019 版本 0x0102 报文体。
func authBody2019(authCode, imei, software string) []byte {
	var body []byte
	body = hex.WriteByte(body, uint8(len(authCode)))
	body = hex.WriteString(body, authCode)
	imeiBytes := make([]byte, 15)
	copy(imeiBytes, imei)
	body = append(body, imeiBytes...)
	softwareBytes := make([]byte, 20)
	copy(softwareBytes, software)
	return append(body, softwareBytes...)
}

// authCode2019 复算注册鉴权码，与被测实现保持同一算法。
func authCode2019(phone string) string {
	source := strings.Repeat("D", 30) + "_" + "京A12345" + "_" + phone
	return strconv.Itoa(int(hash.FNV32(source)))
}

type failingMediaFiles struct{}

func (failingMediaFiles) Save(context.Context, *repository.MediaFile) error {
	return errors.New("db down")
}

func (failingMediaFiles) Get(context.Context, string, uint32) (*repository.MediaFile, error) {
	return nil, repository.ErrMediaFileNotFound
}

func (failingMediaFiles) List(context.Context, string, repository.MediaFileFilter) ([]*repository.MediaFile, error) {
	return nil, nil
}

func (failingMediaFiles) Stats(context.Context) (int64, int64, error) { return 0, 0, nil }

type failingCommitBlob struct {
	*blob.SharedVolume
}

func (b *failingCommitBlob) MarkCommitted(context.Context, string) error {
	return errors.New("sidecar write failed")
}
