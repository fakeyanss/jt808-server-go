package repository

import (
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// IndustryRepository 行业协议上报数据仓储（B/C 类）。
// 保留设备最新一次解码结果、下发目录与模拟器接收状态；内存实现按设备隔离，
// 读取返回深拷贝，避免调用方修改仓储内部状态。后续按设计文档 §6.4 拆分
// TerminalCatalogRepository / IndustrySnapshotHotView 时保持方法语义不变。
type IndustryRepository interface {
	Record(msg model.JT808Msg)
	Waybill(phone string) *model.Msg0701
	BatchLocation(phone string) *model.Msg0704
	SetVehicleControl(phone string, flags uint16)
	VehicleControl(phone string) uint16
	ApplyEventSet(phone string, setType uint8, items []model.EventItem)
	Events(phone string) []model.EventItem
	EventReport(phone string) *model.Msg0301
	Question(phone string) *model.Msg8302
	QuestionResponse(phone string) *model.Msg0302
	ApplyInfoMenuSet(phone string, setType uint8, items []model.InfoMenuItem)
	InfoMenu(phone string) []model.InfoMenuItem
	MenuAction(phone string) *model.Msg0303
	InfoService(phone string) *model.Msg8304
	CallBack(phone string) *model.Msg8400
	ApplyPhonebookSet(phone string, setType uint8, items []model.PhonebookContact)
	Phonebook(phone string) []model.PhonebookContact
	Upgrade(phone string) *model.Msg8108
	UpgradeResult(phone string) *model.Msg0108
	TransparentDown(phone string) *model.Msg8900
	TransparentUp(phone string) *model.Msg0900
	Compressed(phone string) *model.Msg0901
	Driver(phone string) *model.Msg0702
	CAN(phone string) *model.Msg0705
	RecordData(phone string) *model.Msg0700
	MediaUploads(phone string) []model.Msg0801
	MediaEvent(phone string) *model.Msg0800
	Capture(phone string) *model.Msg0805
	MediaSearch(phone string) *model.Msg0802
	MediaResourceList(phone string) *model.Msg1205
	MediaStatus(phone string) *model.Msg9105
	FileUploadNotice(phone string) *model.Msg1206
	SetRecording(phone string, active bool)
	IsRecording(phone string) bool
}
