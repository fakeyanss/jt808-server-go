// jt808-migrate 是独立于主服务的数据库迁移工具。
//
// 与主服务解耦的原因：迁移是低频、需要 DDL 权限的运维动作，不应随服务启动自动执行，
// 也不应让服务二进制承担该职责。主服务启动时只校验 schema 版本，版本不符直接拒绝启动。
//
// 用法：jt808-migrate -c <config file>
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/config"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
	"github.com/fakeyanss/jt808-server-go/pkg/logger"
)

func main() {
	var cfgPath string
	flag.StringVar(&cfgPath, "c", config.DefaultServConfKey, "config file path")
	flag.Parse()

	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "unexpected argument: %s\n", flag.Arg(0))
		flag.Usage()
		os.Exit(2)
	}

	fmt.Printf("Migrate with configuration %v\n", cfgPath)
	cfg := config.Load(cfgPath)
	log.Logger = *logger.Configure(cfg.ParseLogConf()).Logger

	applied, err := storage.Migrate(storage.FromConfig(cfg))
	if err != nil {
		log.Error().Err(err).Msg("Fail to migrate storage schema")
		os.Exit(1)
	}
	log.Info().Int("applied", applied).Msg("Storage schema migration completed")
}
