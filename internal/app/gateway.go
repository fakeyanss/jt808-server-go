package app

import (
	"context"
	"os"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/downlinkhttp"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/server"
	"github.com/fakeyanss/jt808-server-go/pkg/routines"
)

// setupDownlinkHTTP 构造网关侧下行 HTTP 入口（仅 cluster 形态）。
func (d *deps) setupDownlinkHTTP() {
	hasGateway := d.hasGateway
	standalone := d.standalone
	clusterOptions := d.settings.options
	serv := d.serv
	processorDeps := d.processorDeps

	var downlinkServer *downlinkhttp.Server
	if hasGateway && !standalone {
		srv, srvErr := downlinkhttp.NewServer(downlinkhttp.Dependencies{
			Token:       clusterOptions.Gateway.Token,
			Timeout:     clusterOptions.Downlink.Timeout,
			IDTTL:       clusterOptions.Downlink.IDTTL,
			MaxInFlight: clusterOptions.Downlink.MaxInFlight,
			Conn:        processorDeps.Runtime.Conn,
			Send: func(ctx context.Context, sessionID string, msg model.JT808Msg, serial uint16) error {
				return serv.Send(sessionID, msg, server.WithSerial(serial))
			},
		})
		if srvErr != nil {
			log.Error().Err(srvErr).Msg("Fail to create downlink HTTP server")
			os.Exit(1)
		}
		if err := srv.Listen(clusterOptions.Gateway.Addr); err != nil {
			log.Error().Err(err).Str("addr", clusterOptions.Gateway.Addr).Msg("Fail to listen downlink HTTP addr")
			os.Exit(1)
		}
		downlinkServer = srv
		// 查询应答（0x0104/0x0107/0x0201/0x0608）在网关侧回填等待表，
		// 由控制面通过下行契约取回消息体。
		processorDeps.QueryResponse = srv.HandleQueryResponse
		if serv != nil {
			serv.SetDeps(processorDeps)
		}
		log.Info().Str("addr", srv.Addr()).Msg("Downlink HTTP server listening")
	}

	d.processorDeps = processorDeps
	d.downlinkServer = downlinkServer
}

// startDownlinkHTTP 在后台启动下行 HTTP 入口。
func (d *deps) startDownlinkHTTP() {
	if d.downlinkServer != nil {
		srv := d.downlinkServer
		routines.GoSafe(func() { _ = srv.Start() })
	}
}

// startUplinkSink 在后台消费上行 best-effort 队列。
func (d *deps) startUplinkSink() {
	uplinkSink := d.uplinkSink

	cancelUplinkSink := func() {}
	var uplinkSinkDone <-chan struct{}
	if uplinkSink != nil {
		sinkCtx, cancel := context.WithCancel(context.Background())
		cancelUplinkSink = cancel
		done := make(chan struct{})
		uplinkSinkDone = done
		routines.GoSafe(func() {
			defer close(done)
			uplinkSink.Run(sinkCtx)
		})
	}

	d.cancelUplinkSink = cancelUplinkSink
	d.uplinkSinkDone = uplinkSinkDone
}

// startNodeReporter 启动网关心跳上报：注册节点、周期心跳，并在退出时注销。
func (d *deps) startNodeReporter() {
	reporter := d.nodeReporter
	if reporter == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.cancelNodeReport = cancel
	routines.GoSafe(func() { reporter.Run(ctx) })
}

// startGatewayListeners 启动终端 TCP/UDP 接入。
func (d *deps) startGatewayListeners() {
	hasGateway := d.hasGateway
	cfg := d.cfg
	serv := d.serv
	processorDeps := d.processorDeps
	hooks := d.hooks
	metrics := d.metrics

	if hasGateway {
		addr := ":" + cfg.Server.Port.TCPPort
		if err := serv.Listen(addr); err != nil {
			log.Error().Err(err).Str("addr", addr).Msg("Fail to listen tcp addr")
			os.Exit(1)
		}
		routines.GoSafe(func() { serv.Start() })
	}
	var udpServ *server.UDPServer
	if hasGateway && cfg.Server.Port.UDPPort != "" {
		udpServ = server.NewUDPServer()
		udpServ.SetDeps(processorDeps)
		udpServ.SetHooks(hooks)
		udpServ.SetMetrics(metrics)
		udpServ.SetReliability(time.Duration(cfg.Server.AckTimeoutMS)*time.Millisecond, cfg.Server.MaxRetransmissions)
		udpAddr := ":" + cfg.Server.Port.UDPPort
		if err := udpServ.Listen(udpAddr); err != nil {
			log.Error().Err(err).Str("addr", udpAddr).Msg("Fail to listen udp addr")
			os.Exit(1)
		}
		routines.GoSafe(func() { udpServ.Start() })
	}

	d.udpServ = udpServ
}
