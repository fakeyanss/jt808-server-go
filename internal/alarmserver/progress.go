package alarmserver

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/pkg/routines"
)

const (
	progressQueueSize     = 256
	progressReportTimeout = 5 * time.Second
)

// startProgressReporter 启动单 worker 的进度上报协程。
//
// 单 worker 保证同一文件的快照按产生顺序到达 control，后到的快照自然覆盖先到的；
// 队列满时丢弃并将由 control 扫描 attachment/ 前缀补齐。
func (a *Assembler) startProgressReporter() {
	if a.cfg.Storage == nil || a.cfg.Storage.Progress == nil {
		return
	}
	a.reportCh = make(chan contracts.AttachmentProgress, progressQueueSize)
	a.reportDone = make(chan struct{})
	a.reportWG.Add(1)
	sink := a.cfg.Storage.Progress
	routines.GoSafe(func() {
		defer a.reportWG.Done()
		for {
			select {
			case <-a.reportDone:
				return
			case progress := <-a.reportCh:
				ctx, cancel := context.WithTimeout(context.Background(), progressReportTimeout)
				err := sink.Save(ctx, progress)
				cancel()
				if err != nil {
					log.Warn().Err(err).Str("phone", progress.Phone).Str("file", progress.FileName).
						Str("status", progress.Status).Msg("Fail to report attachment progress to control")
				}
			}
		}
	})
}

// enqueueProgress 非阻塞投递进度快照。
func (a *Assembler) enqueueProgress(progress contracts.AttachmentProgress) {
	if a.reportCh == nil || a.reportClosed.Load() {
		return
	}
	select {
	case a.reportCh <- progress:
	default:
		log.Warn().Str("phone", progress.Phone).Str("file", progress.FileName).
			Msg("Attachment progress queue is full; drop snapshot and rely on control backfill")
	}
}

// stopProgressReporter 停止上报协程；不关闭队列，避免并发发送 panic。
func (a *Assembler) stopProgressReporter() {
	if a.reportCh == nil || a.reportClosed.Swap(true) {
		return
	}
	close(a.reportDone)
	a.reportWG.Wait()
}
