package app

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/fakeyanss/jt808-server-go/internal/config"
	"github.com/rs/zerolog/log"
)

// run 是唯一的启动到关闭顺序表：每一步只调用一个角色的装配函数。
func run(spec Spec, cfg *config.Config) {
	s := loadSettings(cfg, spec)
	d := buildDeps(spec, cfg, s)

	d.startRetention()
	d.setupDownlinkHTTP()
	d.setupControl()
	d.startOutbox()
	d.startDownlinkHTTP()
	d.startUplinkHTTP()
	d.startUplinkSink()
	d.installUploadHooks()
	d.startGatewayListeners()
	d.startAttachment()
	d.startMedia()
	d.buildProbes()
	d.startAdmin()
	d.startProbes()
	d.startBusinessAPI()

	// 运行时状态只存在于进程内，进程退出即自然归零；数据库无需、也不应被批量重置。
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Info().Str("signal", sig.String()).Msg("Shutting down")

	d.shutdown()
}
