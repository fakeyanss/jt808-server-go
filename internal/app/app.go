// Package app 是服务端的共享装配层。
//
// 每个 cmd 只声明自己固定承担的角色；具体启动哪些组件、按什么顺序退出，
// 全部由本包按角色推导，避免多个二进制各复制一份 main。
package app

import (
	"flag"
	"fmt"

	"github.com/fakeyanss/jt808-server-go/internal/config"
	"github.com/fakeyanss/jt808-server-go/pkg/routines"
)

// Main 是五个角色二进制与通用入口共用的启动入口。
// 只解析命令行并加载配置，启动与关闭顺序见 lifecycle.go 的 run。
func Main(spec Spec) {
	routines.Recover()

	var cfgPath string
	flag.StringVar(&cfgPath, "c", config.DefaultServConfKey, "config file path")
	flag.Parse()
	fmt.Printf("Start with configuration %v\n", cfgPath)
	run(spec, config.Load(cfgPath))
}
