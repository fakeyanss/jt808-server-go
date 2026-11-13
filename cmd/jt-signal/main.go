package main

import (
	"github.com/fakeyanss/jt808-server-go/internal/app"
	"github.com/fakeyanss/jt808-server-go/internal/config"
)

// jt-signal 固定承担 gateway 角色：808 接入、协议、上行出口客户端与下行契约服务端。
func main() {
	app.Main(app.Spec{
		Binary:        "jt-signal",
		Mode:          config.ServerModeCluster,
		RequiredRoles: []string{config.RoleGateway},
	})
}
