package main

import (
	"github.com/fakeyanss/jt808-server-go/internal/app"
	"github.com/fakeyanss/jt808-server-go/internal/config"
)

// jt-server-standalone 固定以 standalone 形态运行：一个进程承担全部角色，
// 零外部依赖。配置里出现 cluster 角色时启动失败。
func main() {
	app.Main(app.Spec{
		Binary: "jt-server-standalone",
		Mode:   config.ServerModeStandalone,
	})
}
