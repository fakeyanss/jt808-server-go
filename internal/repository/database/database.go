// Package database 负责持久化仓储的装配与初始化，不承载具体实现。
//
// 职责边界：
//   - 聚合 repository 契约的内存实现，或按配置装配 sqlite/mysql 后端；
//   - 提供进程默认实例（Init/Default），供装配层显式下传；
//   - 不保存运行时状态，运行时状态由 runtime.Stores 独立组装。
//
// 依赖方向：database → repository / repository/sqlite / repository/mysql；
// SQL 实现位于 internal/repository 下，objectStore 位于 internal/storage。
package database

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/fakeyanss/jt808-server-go/internal/coordination"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/repository/memory"
	"github.com/fakeyanss/jt808-server-go/internal/repository/mysql"
	"github.com/fakeyanss/jt808-server-go/internal/repository/sqlite"
)

// 支持的数据库后端类型；空值与 memory 等价，默认 memory 保证既有行为不变。
const (
	BackendMemory = "memory"
	BackendSQLite = "sqlite"
	BackendMySQL  = "mysql"
)

// Config 描述持久化后端的选择、连接参数与保留策略。
type Config struct {
	Type       string // memory | sqlite | mysql
	SQLitePath string // type=sqlite 时必填
	// SQLiteReadConns 是 SQLite 只读池连接数；0 表示按 CPU 数。
	// 写池连接数固定为 1，不提供配置入口。
	SQLiteReadConns int
	MySQLDSN        string // type=mysql 时必填

	// 保留策略仅 SQL 后端消费；memory 后端保持既有进程内行为不变。
	LocationRetention repository.LocationRetention
	AlarmRetention    repository.AlarmRetention
}

var (
	defaultMu           sync.RWMutex
	defaultRepositories *Repositories
)

// Repositories 聚合持久化仓储，作为业务组件的存储依赖入口。
type Repositories struct {
	Devices        repository.DeviceRepository
	Geo            repository.GeoRepository
	Params         repository.DeviceParamsRepository
	Properties     repository.TerminalPropertiesRepository
	Alarms         repository.AlarmRepository
	Regions        repository.RegionRepository
	Industry       repository.IndustryRepository
	Media          repository.MediaMetadataRepository
	MediaFiles     repository.MediaFileRepository
	MediaCaptures  repository.MediaCaptureRepository
	MediaSearches  repository.MediaSearchRepository
	UpgradeResults repository.UpgradeResultRepository
	Commands       repository.CommandRepository
	// AttachmentFiles 是附件接收元数据；集群下由附件节点写入、控制面读取。
	AttachmentFiles repository.AttachmentFileRepository
	// AttachmentFTPTasks 是 1078 FTP 上传任务与一次性账号，仅 control 使用。
	AttachmentFTPTasks repository.AttachmentFTPTaskRepository
	// AttachmentNodes 是附件节点注册表，control 据此推送 0x9207。
	AttachmentNodes repository.AttachmentNodeRepository
	// RecordingSegments 是平台录像切片索引，由媒体节点写入、控制面查询与清理。
	RecordingSegments repository.RecordingSegmentRepository
	// Locations 为定位历史仓储；memory 后端保持既有热读环形缓冲，本字段为 nil。
	Locations repository.LocationRepository
	// Retention 为历史数据分批清理能力，仅 SQL 后端提供；memory 后端为 nil。
	Retention repository.RetentionStore
	// Coordination 提供任务租约与共享 TTL 去重；memory 后端为进程内实现。
	Coordination coordination.Store
	// GatewayNodes 是网关注册与健康状态，仅 E4.1 使用。
	GatewayNodes repository.GatewayNodeRepository
	// SessionDirectory 是 phone 归属索引，仅 E4.2 使用。
	SessionDirectory repository.SessionDirectoryRepository

	// Backend 记录实际启用的后端，供启动日志与运维排查使用。
	Backend string
	close   func() error
	// check 校验后端连通性；memory 后端为空操作，nil 表示始终可用。
	check func(ctx context.Context) error
	// dbTime 读取数据库当前时间（Unix 毫秒），仅供应时钟偏差探针观测；
	// memory 后端为 nil，探针跳过。
	dbTime func(ctx context.Context) (int64, error)
}

// sqlBackend 是两个 SQL 后端句柄的公共装配面。
// sqlite.Backend 与 mysql.Backend 都嵌入 *sqlcore.Store，方法集一致。
type sqlBackend interface {
	Backend() string
	Ping(ctx context.Context) error
	Close() error
	DeviceRepository() repository.DeviceRepository
	DeviceParamsRepository() repository.DeviceParamsRepository
	TerminalPropertiesRepository() repository.TerminalPropertiesRepository
	IndustryRepository() (repository.IndustryRepository, error)
	RegionRepository() (repository.RegionRepository, error)
	MediaMetadataRepository() repository.MediaMetadataRepository
	MediaFileRepository() repository.MediaFileRepository
	MediaCaptureRepository() repository.MediaCaptureRepository
	MediaSearchRepository() repository.MediaSearchRepository
	UpgradeResultRepository() repository.UpgradeResultRepository
	CommandRepository() repository.CommandRepository
	AttachmentFileRepository() repository.AttachmentFileRepository
	AttachmentFTPTaskRepository() repository.AttachmentFTPTaskRepository
	AttachmentNodeRepository() repository.AttachmentNodeRepository
	RecordingSegmentRepository() repository.RecordingSegmentRepository
	AlarmRepository(policy repository.AlarmRetention) repository.AlarmRepository
	LocationRepository(policy repository.LocationRetention) repository.LocationRepository
	RetentionStore() repository.RetentionStore
	CoordinationStore() coordination.Store
	GatewayNodeRepository() repository.GatewayNodeRepository
	SessionDirectoryRepository() repository.SessionDirectoryRepository
	CurrentTimeMillis(ctx context.Context) (int64, error)
}

// NewMemory 创建一组互相独立的内存仓储，供测试与新代码注入。
func NewMemory() *Repositories {
	return newRepositories(BackendMemory)
}

func newRepositories(backend string) *Repositories {
	// 文件与拍摄应答共用同一实现，才能在同一次查询里判断 mediaId 是否已落盘。
	mediaFiles := memory.NewMediaFileRepository()
	return &Repositories{
		Devices:            memory.NewDeviceRepository(),
		Geo:                memory.NewGeoRepository(),
		Params:             memory.NewDeviceParamsRepository(),
		Properties:         memory.NewTerminalPropertiesRepository(),
		Alarms:             memory.NewAlarmRepository(),
		Regions:            memory.NewRegionRepository(),
		Industry:           memory.NewIndustryRepository(),
		Media:              memory.NewMediaMetadataRepository(),
		MediaFiles:         mediaFiles,
		MediaCaptures:      mediaFiles,
		MediaSearches:      memory.NewMediaSearchRepository(),
		UpgradeResults:     memory.NewUpgradeResultRepository(),
		Commands:           memory.NewCommandRepository(),
		AttachmentFiles:    memory.NewAttachmentFileRepository(),
		AttachmentFTPTasks: memory.NewAttachmentFTPTaskRepository(),
		AttachmentNodes:    memory.NewAttachmentNodeRepository(),
		RecordingSegments:  memory.NewRecordingSegmentRepository(),
		Coordination:       coordination.NewMemoryStore(),
		GatewayNodes:       memory.NewGatewayNodeRepository(),
		SessionDirectory:   memory.NewSessionDirectoryRepository(),
		Backend:            backend,
	}
}

// Open 按配置装配数据库仓储。
func Open(cfg Config) (*Repositories, error) {
	backend := normalizeBackend(cfg.Type)
	switch backend {
	case BackendMemory:
		return newRepositories(BackendMemory), nil
	case BackendSQLite:
		handle, err := sqlite.Open(cfg.SQLitePath, cfg.SQLiteReadConns)
		if err != nil {
			return nil, err
		}
		return openSQL(handle, backend, cfg)
	case BackendMySQL:
		handle, err := mysql.Open(cfg.MySQLDSN)
		if err != nil {
			return nil, err
		}
		return openSQL(handle, backend, cfg)
	default:
		return nil, fmt.Errorf("unsupported database backend %q, expected memory|sqlite|mysql", cfg.Type)
	}
}

// openSQL 把 SQL 后端句柄装配为统一 Repositories。
func openSQL(handle sqlBackend, backend string, cfg Config) (*Repositories, error) {
	industry, err := handle.IndustryRepository()
	if err != nil {
		_ = handle.Close()
		return nil, err
	}
	regions, err := handle.RegionRepository()
	if err != nil {
		_ = handle.Close()
		return nil, err
	}
	repos := newRepositories(backend)
	repos.Devices = handle.DeviceRepository()
	repos.Params = handle.DeviceParamsRepository()
	repos.Properties = handle.TerminalPropertiesRepository()
	repos.Industry = industry
	repos.Regions = regions
	repos.Media = handle.MediaMetadataRepository()
	repos.MediaFiles = handle.MediaFileRepository()
	repos.MediaCaptures = handle.MediaCaptureRepository()
	repos.MediaSearches = handle.MediaSearchRepository()
	repos.UpgradeResults = handle.UpgradeResultRepository()
	repos.Commands = handle.CommandRepository()
	repos.AttachmentFiles = handle.AttachmentFileRepository()
	repos.AttachmentFTPTasks = handle.AttachmentFTPTaskRepository()
	repos.AttachmentNodes = handle.AttachmentNodeRepository()
	repos.RecordingSegments = handle.RecordingSegmentRepository()
	repos.Alarms = handle.AlarmRepository(cfg.AlarmRetention)
	repos.Locations = handle.LocationRepository(cfg.LocationRetention)
	repos.Retention = handle.RetentionStore()
	repos.Coordination = handle.CoordinationStore()
	repos.GatewayNodes = handle.GatewayNodeRepository()
	repos.SessionDirectory = handle.SessionDirectoryRepository()
	repos.close = handle.Close
	repos.check = handle.Ping
	repos.dbTime = handle.CurrentTimeMillis
	return repos, nil
}

// Migrate 按配置显式执行迁移并返回本次应用的版本数，不初始化进程默认存储。
// MySQL 场景也可由 DBA 按文件名顺序直接执行 internal/repository/mysql/migrations 下的 SQL。
func Migrate(cfg Config) (int, error) {
	backend := normalizeBackend(cfg.Type)
	switch backend {
	case BackendMemory:
		return 0, fmt.Errorf("memory database has no schema to migrate")
	case BackendSQLite:
		return sqlite.Migrate(cfg.SQLitePath)
	case BackendMySQL:
		return mysql.Migrate(cfg.MySQLDSN)
	default:
		return 0, fmt.Errorf("unsupported database backend %q, expected memory|sqlite|mysql", cfg.Type)
	}
}

// normalizeBackend 归一化后端名称；空值与 memory 等价。
func normalizeBackend(name string) string {
	backend := strings.ToLower(strings.TrimSpace(name))
	if backend == "" {
		return BackendMemory
	}
	return backend
}

// Init 按配置初始化进程默认仓储，并返回实际实例供装配层显式下传。
// 必须在任何 database.Default() 调用之前执行；非法配置返回错误，由调用方终止启动。
func Init(cfg Config) (*Repositories, error) {
	instance, err := Open(cfg)
	if err != nil {
		return nil, err
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultRepositories = instance
	return instance, nil
}

// Default 返回进程默认仓储实例；未调用 Init 时懒加载内存后端。
// 业务处理器已改为通过 ProcessorDeps 显式注入，Default 仅作为
// 未注入时的回退入口与装配层之外的兼容取用点。
func Default() *Repositories {
	defaultMu.RLock()
	instance := defaultRepositories
	defaultMu.RUnlock()
	if instance != nil {
		return instance
	}

	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultRepositories == nil {
		defaultRepositories = newRepositories(BackendMemory)
	}
	return defaultRepositories
}

// Close 释放该 Repositories 持有的后端资源（内存后端为空操作）。
func (s *Repositories) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

// Check 校验后端当前可用；memory 后端始终返回 nil。
func (s *Repositories) Check(ctx context.Context) error {
	if s == nil || s.check == nil {
		return nil
	}
	return s.check(ctx)
}

// CurrentTimeMillis 读取数据库当前时间，供时钟偏差探针观测。
// 返回 false 表示当前后端没有数据库时钟（memory）。
func (s *Repositories) CurrentTimeMillis(ctx context.Context) (int64, bool, error) {
	if s == nil || s.dbTime == nil {
		return 0, false, nil
	}
	millis, err := s.dbTime(ctx)
	return millis, true, err
}

// Close 关闭默认仓储持有的资源（当前仅 SQL 后端需要）。
func Close() error {
	defaultMu.RLock()
	instance := defaultRepositories
	defaultMu.RUnlock()
	return instance.Close()
}
