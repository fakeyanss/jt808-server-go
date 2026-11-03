package storage

import (
	"github.com/fakeyanss/jt808-server-go/internal/config"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// FromConfig 将配置文件的 storage 段与 retention 段转换为存储层配置。
// 主服务与独立迁移工具共用该转换，避免两处映射漂移。
func FromConfig(cfg *config.Config) Config {
	storageCfg := Config{}
	policy := cfg.RetentionPolicy()
	storageCfg.LocationRetention = repository.LocationRetention{
		MaxAge:       policy.Locations.MaxAge,
		MaxPerDevice: policy.Locations.MaxPerDevice,
	}
	storageCfg.AlarmRetention = repository.AlarmRetention{
		DedupWindow:   policy.Alarms.DedupWindow,
		ConfirmExpire: policy.Alarms.ConfirmExpire,
		MaxPerDevice:  policy.Alarms.MaxPerDevice,
	}
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
