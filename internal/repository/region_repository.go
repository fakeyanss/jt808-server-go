package repository

import (
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// RegionRepository 区域/路线仓储（B 类）：保存终端已接受的圆形、矩形、多边形
// 与路线对象，支撑 0x0608 查询。内存实现按设备隔离。
type RegionRepository interface {
	ApplySet(phone string, set *model.MsgRegionRouteSet)
	Delete(phone string, typ model.RegionRouteType, ids []uint32)
	Query(phone string, queryType uint8, ids []uint32) *model.Msg0608
}
