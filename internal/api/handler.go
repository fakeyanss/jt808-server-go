package api

import (
	"github.com/fakeyanss/jt808-server-go/internal/alarmserver"
	"github.com/fakeyanss/jt808-server-go/internal/config"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/contracts/attachment"
	"github.com/fakeyanss/jt808-server-go/internal/control"
	"github.com/fakeyanss/jt808-server-go/internal/media/playback"
	"github.com/fakeyanss/jt808-server-go/internal/mediaupload"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
	"github.com/fakeyanss/jt808-server-go/internal/server"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

// handler 持有对外管理面 HTTP 的依赖；请求处理按资源拆分到同包文件。
type handler struct {
	serv                *server.TCPServer
	cfg                 *config.Config
	repos               repository.Repositories
	runtimeState        *runtime.State
	apiOptions          config.APIOptions
	alarmServ           *alarmserver.Server
	attachments         repository.AttachmentFileRepository
	attachmentAllocator attachment.AttachmentAllocator
	blob                storage.BlobStore
	downlink            contracts.Downlink
	sender              contracts.DownlinkSender
	mediaUpload         *mediaupload.Store
	queryer             Queryer
	playback            playback.PlaybackLibrary
	ftpTasks            *control.FTPTaskManager
	ftpPort             uint16
	ftpAdvertiseIP      string

	devices       repository.DeviceRepository
	geo           repository.GeoRepository
	connRegistry  runtime.ConnectionRegistry
	outboxEnabled bool
}

func newHandler(serv *server.TCPServer, cfg *config.Config, repos repository.Repositories, runtimeState *runtime.State, apiOptions config.APIOptions, alarmServ *alarmserver.Server, attachments repository.AttachmentFileRepository, attachmentAllocator attachment.AttachmentAllocator, blob storage.BlobStore, downlink contracts.Downlink, sender contracts.DownlinkSender, mediaUpload *mediaupload.Store, queryer Queryer, playback playback.PlaybackLibrary, ftpTasks *control.FTPTaskManager, ftpPort uint16, ftpAdvertiseIP string) *handler {
	return &handler{
		serv: serv, cfg: cfg, repos: repos, runtimeState: runtimeState, apiOptions: apiOptions,
		alarmServ: alarmServ, attachments: attachments, attachmentAllocator: attachmentAllocator,
		blob: blob, downlink: downlink, sender: sender, mediaUpload: mediaUpload, queryer: queryer,
		playback: playback, ftpTasks: ftpTasks, ftpPort: ftpPort, ftpAdvertiseIP: ftpAdvertiseIP,
		devices: repos.Devices, geo: repos.Geo, connRegistry: runtimeState.Conn,
		outboxEnabled: downlink != nil && downlink.Enabled(),
	}
}
