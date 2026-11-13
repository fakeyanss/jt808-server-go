package main

import (
	"github.com/fakeyanss/jt808-server-go/internal/app"
	"github.com/fakeyanss/jt808-server-go/internal/config"
)

// jt-attachment 固定承担 attachment 角色：
// 独立监听附件 TCP/UDP、自行解析 808 帧，把文件切片写入全集群共享的 objectStore，
// 并把进度写入共享库供控制面查询。
//
// 附件与 control 分离的原因：附件连接按报警建立、传完才断，单个连接可能持续
// 数十分钟并占用上行带宽，容量与 control 的短请求负载无法合并调度。
func main() {
	app.Main(app.Spec{
		Binary:        "jt-attachment",
		Mode:          config.ServerModeCluster,
		RequiredRoles: []string{config.RoleAttachment},
	})
}
