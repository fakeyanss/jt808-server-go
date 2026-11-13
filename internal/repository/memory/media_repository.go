package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// memoryMediaMetadataRepository 是 MediaMetadataRepository 的内存实现。
type memoryMediaMetadataRepository struct {
	mu      sync.RWMutex
	lists   map[string]*model.Msg1205
	tasks   map[mediaUploadKey]*model.Msg9206
	notices map[mediaUploadKey]*model.Msg1206
}

type mediaUploadKey struct {
	phone  string
	serial uint16
}

// NewMediaMetadataRepository 创建独立的内存媒体元数据仓储。
func NewMediaMetadataRepository() repository.MediaMetadataRepository {
	return &memoryMediaMetadataRepository{
		lists:   map[string]*model.Msg1205{},
		tasks:   map[mediaUploadKey]*model.Msg9206{},
		notices: map[mediaUploadKey]*model.Msg1206{},
	}
}

func (r *memoryMediaMetadataRepository) ReplaceResourceList(_ context.Context, phone string, list *model.Msg1205) error {
	if list == nil {
		return fmt.Errorf("replace media resource list: nil message")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := *list
	copy.Items = append([]model.DeviceMedia(nil), list.Items...)
	r.lists[phone] = &copy
	return nil
}

func (r *memoryMediaMetadataRepository) ResourceList(_ context.Context, phone string) (*model.Msg1205, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := r.lists[phone]
	if list == nil {
		return nil, nil
	}
	copy := *list
	copy.Items = append([]model.DeviceMedia(nil), list.Items...)
	return &copy, nil
}

func (r *memoryMediaMetadataRepository) SaveUploadTask(_ context.Context, phone string, task *model.Msg9206) error {
	if task == nil || task.Header == nil {
		return fmt.Errorf("save media upload task: nil message or header")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := *task
	r.tasks[mediaUploadKey{phone: phone, serial: task.Header.SerialNumber}] = &copy
	return nil
}

func (r *memoryMediaMetadataRepository) UploadTask(_ context.Context, phone string, serial uint16) (*model.Msg9206, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	task := r.tasks[mediaUploadKey{phone: phone, serial: serial}]
	if task == nil {
		return nil, nil
	}
	copy := *task
	return &copy, nil
}

func (r *memoryMediaMetadataRepository) SaveUploadNotice(_ context.Context, phone string, notice *model.Msg1206) error {
	if notice == nil {
		return fmt.Errorf("save media upload notice: nil message")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := *notice
	r.notices[mediaUploadKey{phone: phone, serial: notice.AnswerSerialNumber}] = &copy
	return nil
}

func (r *memoryMediaMetadataRepository) UploadNotice(_ context.Context, phone string, serial uint16) (*model.Msg1206, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	notice := r.notices[mediaUploadKey{phone: phone, serial: serial}]
	if notice == nil {
		return nil, nil
	}
	copy := *notice
	return &copy, nil
}

var _ repository.MediaMetadataRepository = (*memoryMediaMetadataRepository)(nil)
