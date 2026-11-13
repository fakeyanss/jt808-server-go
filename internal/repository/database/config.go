package database

import (
	"github.com/fakeyanss/jt808-server-go/internal/config"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// FromConfig 将配置文件的 database 段与 retention 段转换为数据库层配置。
// 主服务与独立迁移工具共用该转换，避免两处映射漂移。
func FromConfig(cfg *config.Config) Config {
	dbCfg := Config{}
	policy := cfg.RetentionPolicy()
	dbCfg.LocationRetention = repository.LocationRetention{
		MaxAge:       policy.Locations.MaxAge,
		MaxPerDevice: policy.Locations.MaxPerDevice,
	}
	dbCfg.AlarmRetention = repository.AlarmRetention{
		DedupWindow:   policy.Alarms.DedupWindow,
		ConfirmExpire: policy.Alarms.ConfirmExpire,
		MaxPerDevice:  policy.Alarms.MaxPerDevice,
	}
	if cfg == nil || cfg.Database == nil {
		return dbCfg
	}
	dbCfg.Type = cfg.Database.Type
	if cfg.Database.SQLite != nil {
		dbCfg.SQLitePath = cfg.Database.SQLite.Path
		dbCfg.SQLiteReadConns = cfg.Database.SQLite.ReadConns
	}
	if cfg.Database.MySQL != nil {
		dbCfg.MySQLDSN = cfg.Database.MySQL.DSN
	}
	return dbCfg
}
