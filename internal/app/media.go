package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/control"
	"github.com/fakeyanss/jt808-server-go/internal/media"
	"github.com/fakeyanss/jt808-server-go/internal/media/playback"
	"github.com/fakeyanss/jt808-server-go/pkg/routines"
)

// startMedia 装配 1078 媒体节点与播放能力。
func (d *deps) startMedia() {
	hasMedia := d.hasMedia
	mediaEnabled := d.mediaEnabled
	mediaOptions := d.mediaOptions
	cfg := d.cfg
	controlBlob := d.controlBlob
	clusterOptions := d.settings.options
	remoteMedia := d.remoteMedia
	hasControl := d.hasControl

	var mediaServ *media.Server
	if hasMedia && mediaEnabled {
		// cluster 默认不录制（实时流只进内存帧环），standalone 保持旧行为；
		// 需要转存时显式配置 record.mode=segmented。
		recordMode := mediaOptions.RecordMode
		if !mediaOptions.RecordModeExplicit {
			if strings.EqualFold(cfg.Server.Mode, "cluster") {
				recordMode = string(media.RecordModeOff)
			} else {
				recordMode = string(media.RecordModeLegacy)
			}
		}
		if recordMode == string(media.RecordModeOff) {
			log.Info().Msg("Media recording is disabled (record.mode=off): live frames stay in the in-memory ring")
		}
		playbackOpts := mediaOptions.Playback
		advertiseIP := mediaOptions.AdvertiseIP
		if advertiseIP == "" {
			advertiseIP = "127.0.0.1"
		}
		playbackConfig := playback.Config{
			Output: playback.OutputMode(playbackOpts.Output),
			FLV: playback.FLVHTTPOptions{
				BaseURL: fmt.Sprintf("http://%s:%d", advertiseIP, mediaOptions.HTTPPort),
			},
			RTMP: playback.RTMPConfig{
				URL:           playbackOpts.RTMPURL,
				StreamPrefix:  playbackOpts.StreamPrefix,
				PlayURLPrefix: playbackOpts.PlayURLPrefix,
				DialTimeout:   playbackOpts.DialTimeout,
				WriteTimeout:  playbackOpts.WriteTimeout,
				QueueFrames:   playbackOpts.QueueFrames,
			},
		}
		if playbackConfig.Output != playback.OutputRTMP {
			log.Info().Str("output", string(playbackConfig.Output)).
				Str("flvBaseUrl", playbackConfig.FLV.BaseURL).Msg("Media playback output configured")
		}
		if playbackConfig.Output == playback.OutputRTMP || playbackConfig.Output == playback.OutputBoth {
			// 桥接模式下观众直连 SRS，平台无法替观众做 GOP 对齐；
			// 必须保持 SRS 的 gop_cache 开启（默认值），否则新观众会黑屏/卡住。
			log.Info().Str("rtmp", playbackOpts.RTMPURL).
				Msg("RTMP playback output enabled; keep SRS gop_cache ON (default) for new viewers")
		}
		// media 不连库：同进程 control 直接写索引，边缘节点走 HTTP 出站上报。
		var segmentSink media.SegmentSink
		if hasControl && d.storeSet != nil {
			segmentSink = control.NewSegmentRepoSink(d.storeSet.RecordingSegments)
		} else if report := mediaOptions.SegmentReport; report.URL != "" {
			sink, sinkErr := media.NewHTTPSegmentSink(report.URL, report.Token, report.Timeout)
			if sinkErr != nil {
				log.Error().Err(sinkErr).Msg("Fail to configure media segment report")
			} else if sink != nil {
				segmentSink = sink
				log.Info().Str("url", report.URL).Msg("Media segment index will be reported to control")
			}
		}
		mediaServ = media.NewServer(media.Config{
			Dir:             mediaOptions.Dir,
			MaxConcurrent:   mediaOptions.MaxConcurrent,
			MaxTCPConns:     mediaOptions.MaxTCPConns,
			Retention:       mediaOptions.Retention,
			RecordMode:      media.RecordMode(recordMode),
			SegmentDuration: mediaOptions.SegmentDuration,
			RingBytes:       mediaOptions.RingBytes,
			RingDuration:    mediaOptions.RingDuration,
			Blob:            controlBlob,
			SegmentSink:     segmentSink,
			Clock:           d.clock,
			AdvertiseIP:     advertiseIP,
			NodeID:          clusterOptions.Media.NodeID,
			Playback:        playbackConfig,
		})
		mediaAddr := fmt.Sprintf(":%d", mediaOptions.Port)
		if err := mediaServ.Listen(mediaAddr); err != nil {
			log.Error().Err(err).Str("addr", mediaAddr).Msg("Fail to listen media addr")
			os.Exit(1)
		}
		httpAddr := fmt.Sprintf(":%d", mediaOptions.HTTPPort)
		if err := mediaServ.ListenHTTP(httpAddr); err != nil {
			log.Error().Err(err).Str("addr", httpAddr).Msg("Fail to listen media http addr")
			os.Exit(1)
		}
		routines.GoSafe(func() { mediaServ.Start() })
	}

	// 播放能力：同进程有媒体节点时直接本地调用，否则使用跨进程的媒体调度器。
	var playbackLibrary contracts.PlaybackLibrary
	switch {
	case mediaServ != nil:
		playbackLibrary = mediaServ
		if hasControl {
			log.Info().Msg("Media playback uses the in-process media server")
		}
	case remoteMedia != nil:
		playbackLibrary = remoteMedia
		log.Info().Int("nodes", len(remoteMedia.Nodes())).Msg("Media playback uses remote media nodes")
	}

	d.mediaServ = mediaServ
	d.playbackLibrary = playbackLibrary
}
