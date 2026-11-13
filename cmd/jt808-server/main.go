package main

import "github.com/fakeyanss/jt808-server-go/internal/app"

// jt808-server 是保留的通用入口：完全按配置文件里的 server.mode 与
// cluster.roles 运行，行为与拆分前一致。按角色分别部署时请使用
// jt-server-standalone / jt-signal / jt-media / jt-control / jt-attachment。
// 保留原因是已有部署与本地调试仍以单入口启动，且它适合同机跑任意角色组合；
// 角色拆分后的生产部署应使用固定角色的二进制。
func main() {
	app.Main(app.Spec{Binary: "jt808-server"})
}
