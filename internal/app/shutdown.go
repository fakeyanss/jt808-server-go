package app

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
)

// shutdown 集中表达退出顺序：业务入口 → 内部 HTTP → 接入 → 投递/出口 → 清理 → 落盘 → 管理面 → 数据库。
//
// 不是简单的“启动逆序”，以下顺序是显式约束：
//   - 业务 API 与两个内部 HTTP 入口先停，避免关机过程中再受理新的下发/上行；
//   - 终端接入（TCP/UDP）早于 outbox/出口停止，先切断新上行再排空投递；
//   - 清理任务晚于 outbox/出口停止、早于存储关闭，避免与关闭中的库竞争；
//   - 媒体/附件写入在清理之后停止，保证文件句柄与已收数据落盘；
//   - 管理面与探针尽量后停，保持关机窗口内可观测；
//   - 数据库最后关闭，供上述所有组件完成收尾。
func (d *deps) shutdown() {
	cancelAPI := d.cancelAPI
	apiDone := d.apiDone
	downlinkServer := d.downlinkServer
	uplinkServer := d.uplinkServer
	serv := d.serv
	udpServ := d.udpServ
	cancelSweep := d.cancelSweep
	cancelOutbox := d.cancelOutbox
	outboxDone := d.outboxDone
	cancelUplinkSink := d.cancelUplinkSink
	uplinkSinkDone := d.uplinkSinkDone
	cancelRetention := d.cancelRetention
	retentionDones := d.retentionDones
	alarmServ := d.alarmServ
	attachmentServ := d.attachmentServ
	mediaServ := d.mediaServ
	adminServ := d.adminServ
	probeServ := d.probeServ
	storeSet := d.storeSet

	// 关机顺序：先停业务入口，再停终端接入，最后排空投递与落盘。
	// 1. 业务 API 不再受理新的下发请求，并等待在途 HTTP 请求完成。
	cancelAPI()
	select {
	case <-apiDone:
	case <-time.After(10 * time.Second):
		log.Warn().Msg("Timed out waiting for business API to shut down")
	}

	// 2. 停止两个内部 HTTP 入口，避免关机过程中继续受理下发/上行。
	if downlinkServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := downlinkServer.Stop(ctx); err != nil {
			log.Warn().Err(err).Msg("Fail to stop downlink HTTP server")
		}
		cancel()
	}
	if uplinkServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := uplinkServer.Stop(ctx); err != nil {
			log.Warn().Err(err).Msg("Fail to stop uplink HTTP server")
		}
		cancel()
	}

	// 3. 停止接入新终端并关闭存量会话，后续不再产生新的上行写入。
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	if serv != nil {
		if err := serv.Shutdown(shutdownCtx); err != nil {
			log.Warn().Err(err).Msg("Fail to shutdown TCP server")
		}
	}
	cancelShutdown()
	if udpServ != nil {
		udpServ.Stop()
	}

	// 3. 取消重投循环并等待当前 tick 收尾；未投递的命令留在 outbox 等下次启动。
	cancelSweep()
	cancelOutbox()
	if outboxDone != nil {
		select {
		case <-outboxDone:
		case <-time.After(5 * time.Second):
			log.Warn().Msg("Timed out waiting for command outbox to drain")
		}
	}

	// 4. 停止上行出口，给在途投递一个收尾窗口；未完成事件随进程退出丢弃。
	cancelUplinkSink()
	if uplinkSinkDone != nil {
		select {
		case <-uplinkSinkDone:
		case <-time.After(5 * time.Second):
			log.Warn().Msg("Timed out waiting for uplink sink to drain")
		}
	}

	// 5. 停止清理与扫描任务，等待当前批次收尾，避免与存储关闭竞争。
	cancelRetention()
	if len(retentionDones) > 0 {
		timer := time.NewTimer(5 * time.Second)
	waitRetention:
		for _, done := range retentionDones {
			select {
			case <-done:
			case <-timer.C:
				log.Warn().Msg("Timed out waiting for retention cleanup to stop")
				break waitRetention
			}
		}
		timer.Stop()
	}

	// 6. 关闭媒体与附件写入，确保文件句柄与已收数据落盘。
	if alarmServ != nil {
		alarmServ.Stop()
	}
	if attachmentServ != nil {
		attachmentServ.Stop()
	}
	if mediaServ != nil {
		mediaServ.Stop()
	}

	// 7. 管理面最后停止，尽量延长探针与指标的可用窗口。
	if adminServ != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := adminServ.Stop(ctx); err != nil {
			log.Warn().Err(err).Msg("Fail to stop admin API server")
		}
		cancel()
	}
	if probeServ != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := probeServ.Stop(ctx); err != nil {
			log.Warn().Err(err).Msg("Fail to stop probe server")
		}
		cancel()
	}
	if storeSet != nil {
		if err := storeSet.Close(); err != nil {
			log.Warn().Err(err).Msg("Fail to close database backend")
		}
	}
}
