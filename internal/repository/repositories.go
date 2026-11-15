package repository

// Repositories 聚合仓储接口，是业务组件唯一的存储依赖入口。
//
// 它与具体后端无关：内存实现由 repository/memory 提供，SQL 实现由
// repository/sqlite、repository/mysql 提供，装配层（repository/database）
// 负责选择后端并填充字段。协议内核与业务层只依赖本类型或其中的单个接口，
// 不依赖任何后端实现，也不触发数据库驱动的链接。
type Repositories struct {
	Devices        DeviceRepository
	Geo            GeoRepository
	Params         DeviceParamsRepository
	Properties     TerminalPropertiesRepository
	Alarms         AlarmRepository
	Regions        RegionRepository
	Industry       IndustryRepository
	Media          MediaMetadataRepository
	MediaFiles     MediaFileRepository
	MediaCaptures  MediaCaptureRepository
	MediaSearches  MediaSearchRepository
	UpgradeResults UpgradeResultRepository
	Commands       CommandRepository
	// AttachmentFiles 是附件接收元数据；集群下由附件节点写入、控制面读取。
	AttachmentFiles AttachmentFileRepository
	// AttachmentFTPTasks 是 1078 FTP 上传任务与一次性账号，仅 control 使用。
	AttachmentFTPTasks AttachmentFTPTaskRepository
	// AttachmentNodes 是附件节点注册表，control 据此推送 0x9207。
	AttachmentNodes AttachmentNodeRepository
	// MediaNodes 是媒体节点注册表与负载，control 据此分配 0x9101/0x9201 端点。
	MediaNodes MediaNodeRepository
	// RecordingSegments 是平台录像切片索引，由媒体节点写入、控制面查询与清理。
	RecordingSegments RecordingSegmentRepository
	// Locations 为定位历史仓储；memory 后端保持既有热读环形缓冲，本字段为 nil。
	Locations LocationRepository
	// Retention 为历史数据分批清理能力，仅 SQL 后端提供；memory 后端为 nil。
	Retention RetentionStore
	// GatewayNodes 是网关注册与健康状态，仅 E4.1 使用。
	GatewayNodes GatewayNodeRepository
	// SessionDirectory 是 phone 归属索引，仅 E4.2 使用。
	SessionDirectory SessionDirectoryRepository
}
