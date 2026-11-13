package app

import (
	"os"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/config"
	"github.com/fakeyanss/jt808-server-go/pkg/logger"
)

// settings 是启动阶段解析并校验过的配置视图，构建依赖时只读。
type settings struct {
	outbox       config.CommandOutboxOptions
	uplink       config.UplinkOptions
	uplinkSink   config.UplinkSinkOptions
	options      config.ClusterOptions
	probeAddr    string
	probeEnabled bool
}

// loadSettings 配置日志并完成启动前的 fail-fast 校验。
func loadSettings(cfg *config.Config, spec Spec) settings {
	logConfig := cfg.ParseLogConf()
	log.Logger = *logger.Configure(logConfig).Logger

	if err := cfg.ValidateRetention(); err != nil {
		log.Error().Err(err).Msg("invalid retention configuration")
		os.Exit(1)
	}
	outboxOptions, outboxErr := cfg.ParseCommandOutbox()
	if outboxErr != nil {
		log.Error().Err(outboxErr).Msg("Invalid commandOutbox configuration")
		os.Exit(1)
	}
	uplinkOptions, uplinkErr := cfg.ParseUplink()
	if uplinkErr != nil {
		log.Error().Err(uplinkErr).Msg("Invalid uplink reliability configuration")
		os.Exit(1)
	}
	uplinkSinkOptions, uplinkSinkErr := cfg.ParseUplinkSink()
	if uplinkSinkErr != nil {
		log.Error().Err(uplinkSinkErr).Msg("Invalid uplink sink configuration")
		os.Exit(1)
	}

	// 运行形态必须在 database.Init 之前 fail-fast：配置矛盾时不要先连数据库。
	clusterOptions, clusterErr := cfg.ParseServerMode()
	if clusterErr != nil {
		log.Error().Err(clusterErr).Msg("Invalid server mode / cluster configuration")
		os.Exit(1)
	}
	if err := spec.validate(clusterOptions); err != nil {
		log.Error().Err(err).Msg("Server role mismatch")
		os.Exit(1)
	}
	// 集群网关必须把上行投给 control 才能完成会话 claim；local 出口会绕过 claim，
	// 等同于关闭 generation 围栏，启动时直接拒绝。
	if !clusterOptions.IsStandalone() && clusterOptions.Roles.Has(config.RoleGateway) &&
		uplinkSinkOptions.Mode != config.UplinkSinkHTTP {
		log.Error().
			Str("uplink_sink", uplinkSinkOptions.Mode).
			Msg("cluster gateway requires uplink.sink=http so session claims go through control")
		os.Exit(1)
	}

	probeAddr, probeEnabled, probeErr := cfg.ParseProbes()
	if probeErr != nil {
		log.Error().Err(probeErr).Msg("Invalid probes configuration")
		os.Exit(1)
	}
	if spec.Mode != "" && !probeEnabled {
		log.Error().Str("binary", spec.Binary).
			Msg("probes.addr is required for a role-specific binary; set probes.addr (for example 0.0.0.0:9105)")
		os.Exit(1)
	}

	return settings{
		outbox:       outboxOptions,
		uplink:       uplinkOptions,
		uplinkSink:   uplinkSinkOptions,
		options:      clusterOptions,
		probeAddr:    probeAddr,
		probeEnabled: probeEnabled,
	}
}
