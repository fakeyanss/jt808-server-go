package main

import (
	"github.com/fakeyanss/jt808-server-go/internal/app"
	"github.com/fakeyanss/jt808-server-go/internal/config"
)

// jt-media 固定承担 media 角色：1078 数据面、播放会话与录像切片。
// 管控通过 /internal/media/* 调度本节点。
func main() {
	app.Main(app.Spec{
		Binary:        "jt-media",
		Mode:          config.ServerModeCluster,
		RequiredRoles: []string{config.RoleMedia},
	})
}
