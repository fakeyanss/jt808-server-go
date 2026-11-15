package memory

import "github.com/fakeyanss/jt808-server-go/internal/repository"

// NewRepositories 构造一组互相独立的内存仓储，供测试与不连库的角色注入。
//
// 需要跨表共享的实现（媒体文件与拍摄应答）在这里复用同一实例，语义与
// repository/database 的 memory 后端保持一致；Locations、Retention 仅 SQL
// 后端提供，这里保持 nil。
func NewRepositories() repository.Repositories {
	// 文件与拍摄应答共用同一实现，才能在同一次查询里判断 mediaId 是否已落盘。
	mediaFiles := NewMediaFileRepository()
	return repository.Repositories{
		Devices:            NewDeviceRepository(),
		Geo:                NewGeoRepository(),
		Params:             NewDeviceParamsRepository(),
		Properties:         NewTerminalPropertiesRepository(),
		Alarms:             NewAlarmRepository(),
		Regions:            NewRegionRepository(),
		Industry:           NewIndustryRepository(),
		Media:              NewMediaMetadataRepository(),
		MediaFiles:         mediaFiles,
		MediaCaptures:      mediaFiles,
		MediaSearches:      NewMediaSearchRepository(),
		UpgradeResults:     NewUpgradeResultRepository(),
		Commands:           NewCommandRepository(),
		AttachmentFiles:    NewAttachmentFileRepository(),
		AttachmentFTPTasks: NewAttachmentFTPTaskRepository(),
		AttachmentNodes:    NewAttachmentNodeRepository(),
		MediaNodes:         NewMediaNodeRepository(),
		RecordingSegments:  NewRecordingSegmentRepository(),
		GatewayNodes:       NewGatewayNodeRepository(),
		SessionDirectory:   NewSessionDirectoryRepository(),
	}
}
