package repository

import (
	"context"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// MediaMetadataRepository 媒体元数据仓储（B 类）。
// 承载 0x1205 音视频资源列表、0x9206 文件上传指令与 0x1206 上传完成通知，
// 与进程内热读缓存（IndustryRepository）分离；读取返回快照。
type MediaMetadataRepository interface {
	ReplaceResourceList(ctx context.Context, phone string, list *model.Msg1205) error
	ResourceList(ctx context.Context, phone string) (*model.Msg1205, error)
	SaveUploadTask(ctx context.Context, phone string, task *model.Msg9206) error
	UploadTask(ctx context.Context, phone string, serial uint16) (*model.Msg9206, error)
	SaveUploadNotice(ctx context.Context, phone string, notice *model.Msg1206) error
	UploadNotice(ctx context.Context, phone string, serial uint16) (*model.Msg1206, error)
}
