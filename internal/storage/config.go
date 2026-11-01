package storage

import "github.com/fakeyanss/jt808-server-go/internal/config"

// FromConfig 将配置文件的 storage 段转换为存储层配置。
// 主服务与独立迁移工具共用该转换，避免两处映射漂移。
func FromConfig(cfg *config.Config) Config {
	storageCfg := Config{}
	if cfg == nil || cfg.Storage == nil {
		return storageCfg
	}
	storageCfg.Type = cfg.Storage.Type
	if cfg.Storage.SQLite != nil {
		storageCfg.SQLitePath = cfg.Storage.SQLite.Path
	}
	if cfg.Storage.MySQL != nil {
		storageCfg.MySQLDSN = cfg.Storage.MySQL.DSN
	}
	return storageCfg
}
