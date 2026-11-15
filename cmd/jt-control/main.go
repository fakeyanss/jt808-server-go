package main

import (
	"github.com/fakeyanss/jt808-server-go/internal/app"
	"github.com/fakeyanss/jt808-server-go/internal/config"
	// control 需要 SQL 后端：显式注册 sqlite/mysql，数据面二进制不导入。
	_ "github.com/fakeyanss/jt808-server-go/internal/repository/database/sqlstore"
)

// jt-control 固定承担 control 角色：业务 API、落库、命令编排与 outbox。
// 它通过 HTTP 调度 jt-media 与 jt-attachment 节点，不托管附件接入。
func main() {
	app.Main(app.Spec{
		Binary:        "jt-control",
		Mode:          config.ServerModeCluster,
		RequiredRoles: []string{config.RoleControl},
	})
}
