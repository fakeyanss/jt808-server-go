package storage

// 本文件是 M2 分批迁移期间的临时兼容层：保留 M1 的旧 API 表面，
// 全部一行转发到 Default() 聚合仓储，保证未迁移调用点在中间批次仍可编译。
// 所有调用点迁移完毕后（批次 5）删除本文件，旧名随之下线。

import (
	"context"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/pkg/container"
)

// ---- 设备仓储旧名 ----

type DeviceCache struct {
	repo DeviceRepository
}

func GetDeviceCache() *DeviceCache {
	return &DeviceCache{repo: Default().Devices}
}

func (cache *DeviceCache) ListDevice() []*model.Device {
	devices, _ := cache.repo.List(context.Background())
	return devices
}

func (cache *DeviceCache) GetDeviceByPlate(carPlate string) (*model.Device, error) {
	return cache.repo.GetByPlate(context.Background(), carPlate)
}

func (cache *DeviceCache) HasPlate(carPlate string) bool {
	d, err := cache.GetDeviceByPlate(carPlate)
	return d != nil && err == nil
}

func (cache *DeviceCache) GetDeviceByPhone(phone string) (*model.Device, error) {
	return cache.repo.GetByPhone(context.Background(), phone)
}

func (cache *DeviceCache) HasPhone(phone string) bool {
	d, err := cache.GetDeviceByPhone(phone)
	return d != nil && err == nil
}

func (cache *DeviceCache) CacheDevice(d *model.Device) {
	_ = cache.repo.Upsert(context.Background(), d)
}

func (cache *DeviceCache) DelDeviceByCarPlate(carPlate string) {
	_ = cache.repo.DeleteByPlate(context.Background(), carPlate)
}

func (cache *DeviceCache) DelDeviceByPhone(phone string) {
	_ = cache.repo.DeleteByPhone(context.Background(), phone)
}

// ---- 定位仓储旧名 ----

type GeoCache struct{}

func GetGeoCache() *GeoCache {
	return &GeoCache{}
}

func (cache *GeoCache) repo() *memoryGeoRepository {
	return Default().Geo.(*memoryGeoRepository)
}

func (cache *GeoCache) GetGeoRingByPhone(phone string) *container.RingBuffer {
	return cache.repo().ring(phone)
}

func (cache *GeoCache) GetGeoLatestByPhone(phone string) (*model.DeviceGeo, error) {
	return cache.repo().Latest(context.Background(), phone)
}

func (cache *GeoCache) DelGeoByPhone(phone string) {
	_ = cache.repo().Delete(context.Background(), phone)
}

// ---- 会话注册表旧名（包级函数） ----

// GetSession 通过 id 获取 session。不存在时返回 ErrSessionClosed。
func GetSession(id string) (*model.Session, error) {
	return Default().Conn.Get(context.Background(), id)
}

// StoreSession 缓存 session。
func StoreSession(s *model.Session) {
	_ = Default().Conn.Bind(context.Background(), s)
}

// ClearSession 清理 session。
func ClearSession(id string) {
	Default().Conn.Unbind(context.Background(), id)
}

// ---- 分包重组旧名（包级函数） ----

// CacheSegment 缓存上行分包，完整时返回 true 并回填 seg.Data。
func CacheSegment(seg *model.Segment, serialNumber uint16) bool {
	return Default().Fragment.CacheSegment(context.Background(), seg, serialNumber)
}

// MissingSegmentIndexes 返回尚未到达的分包序号，用于 0x8003 请求。
func MissingSegmentIndexes(seg *model.Segment, serialNumber uint16) []uint16 {
	return Default().Fragment.MissingIndexes(context.Background(), seg, serialNumber)
}

// ---- 下行重传跟踪旧名（包级函数） ----

// CacheMediaSegment 关联多媒体 ID 与分片消息流水号。
func CacheMediaSegment(phone string, mediaID uint32, serial uint16) {
	Default().Retrans.LinkMedia(context.Background(), phone, mediaID, serial)
}

// GetMediaSegmentSerial 查询多媒体 ID 关联的消息流水号。
func GetMediaSegmentSerial(phone string, mediaID uint32) (uint16, bool) {
	return Default().Retrans.MediaSerial(context.Background(), phone, mediaID)
}

// CacheSentSegmentFrame 保留已发送的分片帧，供 0x8003 原样重传。
func CacheSentSegmentFrame(phone string, originalSerial, index, total uint16, frame []byte) {
	Default().Retrans.CacheFrame(context.Background(), phone, originalSerial, index, total, frame)
}

// GetSentSegmentFrames 按请求序号返回分片帧副本。
func GetSentSegmentFrames(phone string, originalSerial uint16, packetIDs []uint16) [][]byte {
	return Default().Retrans.Frames(context.Background(), phone, originalSerial, packetIDs)
}

// ---- 终端参数仓储旧名 ----

type DeviceParamsCache struct {
	repo DeviceParamsRepository
}

func GetDeviceParamsCache() *DeviceParamsCache {
	return &DeviceParamsCache{repo: Default().Params}
}

func (cache *DeviceParamsCache) GetDeviceParamsByPhone(phone string) (*model.DeviceParams, error) {
	return cache.repo.Get(context.Background(), phone)
}

func (cache *DeviceParamsCache) CacheDeviceParams(d *model.DeviceParams) {
	_ = cache.repo.Replace(context.Background(), d)
}

func (cache *DeviceParamsCache) DelDeviceParamsByPhone(phone string) {
	_ = cache.repo.Delete(context.Background(), phone)
}
