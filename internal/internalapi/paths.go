// Package internalapi 定义角色之间内部 HTTP 的路径与上行事件信封。
//
// 边界：internal/api 是对外管理面 HTTP（/api/*），internalapi 只承载服务间
// 内部调用（/internal/v1/*），不对公网暴露，也不放请求处理逻辑。
//
// 这些定义只描述线上契约，不依赖任何实现包：网关侧出口与 control 侧入口
// 共用同一份常量与 JSON 结构，避免任一侧为了一个路径常量导入另一侧的实现。
package internalapi

const (
	// PathUplink 是网关上行事件的入口。
	PathUplink = "/internal/v1/uplink"
	// PathMediaSegment 是 media 节点上报切片索引的入口。
	PathMediaSegment = "/internal/v1/media/segments"
	// PathAttachmentProgress 是附件节点上报进度、查询恢复点的入口。
	PathAttachmentProgress = "/internal/v1/attachment/progress"
)
