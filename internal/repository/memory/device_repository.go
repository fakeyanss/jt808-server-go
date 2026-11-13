package memory

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"golang.org/x/exp/maps"
)

// memoryDeviceRepository 是 DeviceRepository 的内存实现。
type memoryDeviceRepository struct {
	cacheByPlate map[string]*model.Device
	cacheByPhone map[string]*model.Device
	mutex        *sync.Mutex
}

// newMemoryDeviceRepository 创建独立的内存设备仓储。
func newMemoryDeviceRepository() *memoryDeviceRepository {
	return &memoryDeviceRepository{
		cacheByPlate: make(map[string]*model.Device),
		cacheByPhone: make(map[string]*model.Device),
		mutex:        &sync.Mutex{},
	}
}

// List 返回全量设备快照。
func (cache *memoryDeviceRepository) List(_ context.Context) ([]*model.Device, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	devices := maps.Values(cache.cacheByPhone)
	for i, device := range devices {
		devices[i] = cloneDevice(device)
	}
	return devices, nil
}

// GetByPlate 按车牌查询设备快照。
func (cache *memoryDeviceRepository) GetByPlate(_ context.Context, carPlate string) (*model.Device, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if d, ok := cache.cacheByPlate[carPlate]; ok {
		return cloneDevice(d), nil
	}
	return nil, repository.ErrDeviceNotFound
}

// GetByPhone 按手机号查询设备快照。
func (cache *memoryDeviceRepository) GetByPhone(_ context.Context, phone string) (*model.Device, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if d, ok := cache.cacheByPhone[phone]; ok {
		return cloneDevice(d), nil
	}
	return nil, repository.ErrDeviceNotFound
}

// Upsert 写入设备，同时维护车牌与手机号索引。
func (cache *memoryDeviceRepository) Upsert(_ context.Context, d *model.Device) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	copy := cloneDevice(d)
	cache.cacheByPlate[copy.Plate] = copy
	cache.cacheByPhone[copy.Phone] = copy
	return nil
}

// SaveRegistration 只更新 0x0100 拥有的静态字段。
//
// 多副本下 0x0100 与 0x0102 可能乱序到达：注册不得整行覆盖，否则会把鉴权
// 刚写入的在线状态、会话与最后通信时间冲掉；在线状态只由鉴权侧维护。
func (cache *memoryDeviceRepository) SaveRegistration(_ context.Context, d *model.Device) error {
	if d == nil || d.Phone == "" {
		return errors.New("save registration: empty phone")
	}
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	existing, ok := cache.cacheByPhone[d.Phone]
	if !ok {
		copy := cloneDevice(d)
		if copy.Keepalive <= 0 {
			copy.Keepalive = time.Minute
		}
		// 注册只建立档案，不建立在线会话与在线状态；二者由 0x0102 鉴权后写入。
		copy.Status = model.DeviceStatusOffline
		copy.SessionID = ""
		if copy.Plate != "" {
			cache.cacheByPlate[copy.Plate] = copy
		}
		cache.cacheByPhone[copy.Phone] = copy
		return nil
	}
	if existing.Plate != d.Plate {
		delete(cache.cacheByPlate, existing.Plate)
		existing.Plate = d.Plate
		if d.Plate != "" {
			cache.cacheByPlate[d.Plate] = existing
		}
	}
	existing.ID = d.ID
	existing.VersionDesc = d.VersionDesc
	existing.ProtocolVersion = d.ProtocolVersion
	existing.AuthCode = d.AuthCode
	return nil
}

// SaveAuthState 只更新 0x0102 拥有的在线状态与终端信息。
// 档案不存在时返回 ErrDeviceNotFound，由管控 409 让网关有界重投注册事件。
func (cache *memoryDeviceRepository) SaveAuthState(_ context.Context, d *model.Device) error {
	if d == nil || d.Phone == "" {
		return repository.ErrDeviceNotFound
	}
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	existing, ok := cache.cacheByPhone[d.Phone]
	if !ok {
		return repository.ErrDeviceNotFound
	}
	existing.IMEI = d.IMEI
	existing.SoftwareVersion = d.SoftwareVersion
	existing.Status = d.Status
	existing.SessionID = d.SessionID
	existing.LastestComTime = d.LastestComTime
	return nil
}

// DeleteByPhone 按手机号删除设备及其车牌索引。
func (cache *memoryDeviceRepository) DeleteByPhone(_ context.Context, phone string) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	d, ok := cache.cacheByPhone[phone]
	if !ok {
		return nil
	}
	delete(cache.cacheByPlate, d.Plate)
	delete(cache.cacheByPhone, d.Phone)
	return nil
}

// DeleteByPlate 按车牌删除设备及其手机号索引。
func (cache *memoryDeviceRepository) DeleteByPlate(_ context.Context, carPlate string) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	d, ok := cache.cacheByPlate[carPlate]
	if !ok {
		return nil
	}
	delete(cache.cacheByPlate, d.Plate)
	delete(cache.cacheByPhone, d.Phone)
	return nil
}

// cloneDevice keeps callers from mutating cache-owned state after a lookup.
// Device fields are scalar values except Conn, which intentionally remains a
// shared transport handle.
func cloneDevice(device *model.Device) *model.Device {
	if device == nil {
		return nil
	}
	copy := *device
	return &copy
}

// NewDeviceRepository 创建独立的内存设备仓储，便于测试与新代码注入。
func NewDeviceRepository() repository.DeviceRepository { return newMemoryDeviceRepository() }
