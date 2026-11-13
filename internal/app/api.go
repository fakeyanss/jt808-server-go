package app

import (
	"context"

	"github.com/fakeyanss/jt808-server-go/internal/api"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/control"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/pkg/routines"
)

// startBusinessAPI 启动业务 API。
func (d *deps) startBusinessAPI() {
	hasGateway := d.hasGateway
	hasControl := d.hasControl
	serv := d.serv
	cfg := d.cfg
	apiOptions := d.apiOptions
	alarmServ := d.alarmServ
	storeSet := d.storeSet
	controlBlob := d.controlBlob
	outbox := d.outbox
	controlDownlink := d.controlDownlink
	mediaUploadStore := d.mediaUploadStore
	playbackLibrary := d.playbackLibrary
	standalone := d.standalone
	remoteAttachment := d.remoteAttachment

	apiCtx, cancelAPI := context.WithCancel(context.Background())
	apiDone := make(chan struct{})

	if hasControl {
		// control-only 没有本地会话，查询 broker 通过下行契约交给网关等待终端应答；
		// gateway+control 同进程继续使用进程内 broker。
		var queryer api.Queryer
		if !hasGateway && controlDownlink != nil {
			queryer = &control.DownlinkQueryer{Sender: controlDownlink}
		}
		routines.GoSafe(func() {
			defer close(apiDone)
			var attachments repository.AttachmentFileRepository
			if !standalone && storeSet != nil {
				attachments = storeSet.AttachmentFiles
			}
			var allocator contracts.AttachmentAllocator
			if remoteAttachment != nil {
				allocator = remoteAttachment
			}
			api.Run(apiCtx, serv, cfg, apiOptions, alarmServ, attachments, allocator, controlBlob,
				outbox, controlDownlink, mediaUploadStore, queryer, playbackLibrary,
				d.ftpTasks, d.ftpPort, d.ftpAdvertiseIP)
		})
	} else {
		close(apiDone)
	}

	d.apiCtx = apiCtx
	d.cancelAPI = cancelAPI
	d.apiDone = apiDone
}
