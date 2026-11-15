// Package sqlbackend 把 sqlite 与 mysql 后端注册到 database 装配层。
//
// 只有需要 SQL 后端的二进制（jt-control、jt-server-standalone、jt808-migrate）
// 显式导入本包；gateway/media/attachment 等数据面二进制不导入，因此不会链接
// modernc.org/sqlite 与 go-sql-driver/mysql。
package sqlbackend

import (
	"github.com/fakeyanss/jt808-server-go/internal/repository/database"
	"github.com/fakeyanss/jt808-server-go/internal/repository/mysql"
	"github.com/fakeyanss/jt808-server-go/internal/repository/sqlite"
)

func init() {
	database.RegisterBackend(database.BackendSQLite, openSQLite, migrateSQLite)
	database.RegisterBackend(database.BackendMySQL, openMySQL, migrateMySQL)
}

func openSQLite(cfg database.Config) (*database.Repositories, error) {
	handle, err := sqlite.Open(cfg.SQLitePath, cfg.SQLiteReadConns)
	if err != nil {
		return nil, err
	}
	return database.NewSQLRepositories(database.BackendSQLite, cfg, handle)
}

func migrateSQLite(cfg database.Config) (int, error) {
	return sqlite.Migrate(cfg.SQLitePath)
}

func openMySQL(cfg database.Config) (*database.Repositories, error) {
	handle, err := mysql.Open(cfg.MySQLDSN)
	if err != nil {
		return nil, err
	}
	return database.NewSQLRepositories(database.BackendMySQL, cfg, handle)
}

func migrateMySQL(cfg database.Config) (int, error) {
	return mysql.Migrate(cfg.MySQLDSN)
}
