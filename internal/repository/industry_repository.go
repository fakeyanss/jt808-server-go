package repository

import (
	"sync"

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
	SetRecording(phone string, active bool)
	IsRecording(phone string) bool
}

// memoryIndustryRepository 是 IndustryRepository 的内存实现。
type memoryIndustryRepository struct {
	mu              sync.RWMutex
	drivers         map[string]*model.Msg0702
	cans            map[string]*model.Msg0705
	records         map[string]*model.Msg0700
	mediaEvents     map[string]*model.Msg0800
	mediaUploads    map[string][]*model.Msg0801
	captures        map[string]*model.Msg0805
	mediaSearches   map[string]*model.Msg0802
	mediaResources  map[string]*model.Msg1205
	events          map[string]map[uint8]model.EventItem
	eventReports    map[string]*model.Msg0301
	questions       map[string]*model.Msg8302
	questionResps   map[string]*model.Msg0302
	infoMenus       map[string]map[uint32]model.InfoMenuItem
	menuActions     map[string]*model.Msg0303
	infoServices    map[string]*model.Msg8304
	callbacks       map[string]*model.Msg8400
	phonebook       map[string]map[string]model.PhonebookContact
	upgrades        map[string]*model.Msg8108
	upgradeResults  map[string]*model.Msg0108
	transparentDown map[string]*model.Msg8900
	transparentUp   map[string]*model.Msg0900
	compressed      map[string]*model.Msg0901
	vehicleControl  map[string]uint16
	waybills        map[string]*model.Msg0701
	batchLocs       map[string]*model.Msg0704
	recording       map[string]bool
}

// NewIndustryRepository 创建独立的内存行业上报仓储。
func NewIndustryRepository() IndustryRepository { return newMemoryIndustryRepository() }

func newMemoryIndustryRepository() *memoryIndustryRepository {
	return &memoryIndustryRepository{drivers: map[string]*model.Msg0702{}, cans: map[string]*model.Msg0705{}, records: map[string]*model.Msg0700{}, mediaEvents: map[string]*model.Msg0800{}, mediaUploads: map[string][]*model.Msg0801{}, captures: map[string]*model.Msg0805{}, mediaSearches: map[string]*model.Msg0802{}, mediaResources: map[string]*model.Msg1205{}, events: map[string]map[uint8]model.EventItem{}, eventReports: map[string]*model.Msg0301{}, questions: map[string]*model.Msg8302{}, questionResps: map[string]*model.Msg0302{}, infoMenus: map[string]map[uint32]model.InfoMenuItem{}, menuActions: map[string]*model.Msg0303{}, infoServices: map[string]*model.Msg8304{}, callbacks: map[string]*model.Msg8400{}, phonebook: map[string]map[string]model.PhonebookContact{}, upgrades: map[string]*model.Msg8108{}, upgradeResults: map[string]*model.Msg0108{}, transparentDown: map[string]*model.Msg8900{}, transparentUp: map[string]*model.Msg0900{}, compressed: map[string]*model.Msg0901{}, vehicleControl: map[string]uint16{}, waybills: map[string]*model.Msg0701{}, batchLocs: map[string]*model.Msg0704{}, recording: map[string]bool{}}
}

func (c *memoryIndustryRepository) Record(msg model.JT808Msg) {
	if msg == nil || msg.GetHeader() == nil {
		return
	}
	phone := msg.GetHeader().PhoneNumber
	c.mu.Lock()
	defer c.mu.Unlock()
	switch m := msg.(type) {
	case *model.Msg0702:
		copy := *m
		c.drivers[phone] = &copy
	case *model.Msg0705:
		copy := *m
		copy.Items = append([]model.CANDataItem(nil), m.Items...)
		c.cans[phone] = &copy
	case *model.Msg0700:
		copy := *m
		copy.Data = append([]byte(nil), m.Data...)
		c.records[phone] = &copy
	case *model.Msg0800:
		copy := *m
		c.mediaEvents[phone] = &copy
	case *model.Msg0801:
		if len(m.FragmentData) > 1<<20 {
			return
		}
		copy := *m
		copy.GeoAlarmBody = append([]byte(nil), m.GeoAlarmBody...)
		copy.FragmentData = append([]byte(nil), m.FragmentData...)
		list := append(c.mediaUploads[phone], &copy)
		if len(list) > 100 {
			list = list[len(list)-100:]
		}
		c.mediaUploads[phone] = list
	case *model.Msg0805:
		copy := *m
		copy.MediaIDs = append([]uint32(nil), m.MediaIDs...)
		c.captures[phone] = &copy
	case *model.Msg0802:
		copy := *m
		copy.Items = append([]model.MediaSearchItem(nil), m.Items...)
		c.mediaSearches[phone] = &copy
	case *model.Msg1205:
		copy := *m
		copy.Items = append([]model.DeviceMedia(nil), m.Items...)
		c.mediaResources[phone] = &copy
	case *model.Msg0301:
		copy := *m
		c.eventReports[phone] = &copy
	case *model.Msg8302:
		copy := *m
		copy.Candidates = append([]model.QuestionCandidate(nil), m.Candidates...)
		c.questions[phone] = &copy
	case *model.Msg0302:
		copy := *m
		c.questionResps[phone] = &copy
	case *model.Msg0303:
		copy := *m
		c.menuActions[phone] = &copy
	case *model.Msg8304:
		copy := *m
		c.infoServices[phone] = &copy
	case *model.Msg8400:
		copy := *m
		c.callbacks[phone] = &copy
	case *model.Msg8108:
		copy := *m
		copy.Data = append([]byte(nil), m.Data...)
		c.upgrades[phone] = &copy
	case *model.Msg0108:
		copy := *m
		c.upgradeResults[phone] = &copy
	case *model.Msg8900:
		copy := *m
		copy.Data = append([]byte(nil), m.Data...)
		c.transparentDown[phone] = &copy
	case *model.Msg0900:
		copy := *m
		copy.Data = append([]byte(nil), m.Data...)
		c.transparentUp[phone] = &copy
	case *model.Msg0901:
		copy := *m
		copy.Data = append([]byte(nil), m.Data...)
		copy.Decompressed = append([]byte(nil), m.Decompressed...)
		c.compressed[phone] = &copy
	case *model.Msg0500:
		copy := *m
		c.vehicleControl[phone] = copy.ControlFlags
	case *model.Msg0701:
		copy := *m
		c.waybills[phone] = &copy
	case *model.Msg0704:
		copy := *m
		copy.Items = append([]model.BatchLocationItem(nil), m.Items...)
		c.batchLocs[phone] = &copy
	}
}

// Waybill returns the latest 0x0701 electronic waybill for a device.
func (c *memoryIndustryRepository) Waybill(phone string) *model.Msg0701 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := c.waybills[phone]
	if m == nil {
		return nil
	}
	copy := *m
	return &copy
}

// BatchLocation returns the latest 0x0704 batch location report for a device.
func (c *memoryIndustryRepository) BatchLocation(phone string) *model.Msg0704 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := c.batchLocs[phone]
	if m == nil {
		return nil
	}
	copy := *m
	copy.Items = append([]model.BatchLocationItem(nil), m.Items...)
	return &copy
}

// SetVehicleControl stores the latest 0x8500 control flags for a device.
func (c *memoryIndustryRepository) SetVehicleControl(phone string, flags uint16) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vehicleControl[phone] = flags
}

// VehicleControl returns the latest vehicle control flags for a device.
func (c *memoryIndustryRepository) VehicleControl(phone string) uint16 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vehicleControl[phone]
}

// ApplyEventSet updates the terminal simulator's event catalog according to the
// 0x8301 set type: 0=delete all, 1=replace, 2=append, 3=modify by ID.
func (c *memoryIndustryRepository) ApplyEventSet(phone string, setType uint8, items []model.EventItem) {
	c.mu.Lock()
	defer c.mu.Unlock()
	catalog, ok := c.events[phone]
	if !ok {
		catalog = map[uint8]model.EventItem{}
		c.events[phone] = catalog
	}
	switch setType {
	case 0:
		c.events[phone] = map[uint8]model.EventItem{}
	case 1:
		c.events[phone] = map[uint8]model.EventItem{}
		for _, item := range items {
			c.events[phone][item.ID] = item
		}
	case 2:
		for _, item := range items {
			catalog[item.ID] = item
		}
	case 3:
		for _, item := range items {
			catalog[item.ID] = item
		}
	}
}

// Events returns the configured event items for a device.
func (c *memoryIndustryRepository) Events(phone string) []model.EventItem {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]model.EventItem, 0, len(c.events[phone]))
	for _, item := range c.events[phone] {
		out = append(out, item)
	}
	return out
}

// EventReport returns the latest 0x0301 event report for a device.
func (c *memoryIndustryRepository) EventReport(phone string) *model.Msg0301 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := c.eventReports[phone]
	if m == nil {
		return nil
	}
	copy := *m
	return &copy
}

// Question returns the latest 0x8302 question for a device.
func (c *memoryIndustryRepository) Question(phone string) *model.Msg8302 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := c.questions[phone]
	if m == nil {
		return nil
	}
	copy := *m
	copy.Candidates = append([]model.QuestionCandidate(nil), m.Candidates...)
	return &copy
}

// QuestionResponse returns the latest 0x0302 question response for a device.
func (c *memoryIndustryRepository) QuestionResponse(phone string) *model.Msg0302 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := c.questionResps[phone]
	if m == nil {
		return nil
	}
	copy := *m
	return &copy
}

// ApplyInfoMenuSet updates the terminal simulator's information on-demand menu
// catalog according to the 0x8303 set type: 0=delete all, 1=replace,
// 2=append, 3=modify by ID.
func (c *memoryIndustryRepository) ApplyInfoMenuSet(phone string, setType uint8, items []model.InfoMenuItem) {
	c.mu.Lock()
	defer c.mu.Unlock()
	catalog, ok := c.infoMenus[phone]
	if !ok {
		catalog = map[uint32]model.InfoMenuItem{}
		c.infoMenus[phone] = catalog
	}
	switch setType {
	case 0:
		c.infoMenus[phone] = map[uint32]model.InfoMenuItem{}
	case 1:
		c.infoMenus[phone] = map[uint32]model.InfoMenuItem{}
		for _, item := range items {
			c.infoMenus[phone][item.ID] = item
		}
	case 2, 3:
		for _, item := range items {
			catalog[item.ID] = item
		}
	}
}

// InfoMenu returns the configured information on-demand menu items for a device.
func (c *memoryIndustryRepository) InfoMenu(phone string) []model.InfoMenuItem {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]model.InfoMenuItem, 0, len(c.infoMenus[phone]))
	for _, item := range c.infoMenus[phone] {
		out = append(out, item)
	}
	return out
}

// MenuAction returns the latest 0x0303 on-demand/cancel action for a device.
func (c *memoryIndustryRepository) MenuAction(phone string) *model.Msg0303 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := c.menuActions[phone]
	if m == nil {
		return nil
	}
	copy := *m
	return &copy
}

// InfoService returns the latest 0x8304 information-service message for a device.
func (c *memoryIndustryRepository) InfoService(phone string) *model.Msg8304 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := c.infoServices[phone]
	if m == nil {
		return nil
	}
	copy := *m
	return &copy
}

// CallBack returns the latest 0x8400 call-callback command for a device.
func (c *memoryIndustryRepository) CallBack(phone string) *model.Msg8400 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := c.callbacks[phone]
	if m == nil {
		return nil
	}
	copy := *m
	return &copy
}

// ApplyPhonebookSet updates the terminal simulator's phonebook according to the
// 0x8401 set type: 0=delete all, 1=replace, 2=append, 3=modify by phone number.
func (c *memoryIndustryRepository) ApplyPhonebookSet(phone string, setType uint8, items []model.PhonebookContact) {
	c.mu.Lock()
	defer c.mu.Unlock()
	catalog, ok := c.phonebook[phone]
	if !ok {
		catalog = map[string]model.PhonebookContact{}
		c.phonebook[phone] = catalog
	}
	switch setType {
	case 0:
		c.phonebook[phone] = map[string]model.PhonebookContact{}
	case 1:
		c.phonebook[phone] = map[string]model.PhonebookContact{}
		for _, item := range items {
			c.phonebook[phone][item.Phone] = item
		}
	case 2, 3:
		for _, item := range items {
			catalog[item.Phone] = item
		}
	}
}

// Phonebook returns the configured phonebook contacts for a device.
func (c *memoryIndustryRepository) Phonebook(phone string) []model.PhonebookContact {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]model.PhonebookContact, 0, len(c.phonebook[phone]))
	for _, item := range c.phonebook[phone] {
		out = append(out, item)
	}
	return out
}

// Upgrade returns the latest 0x8108 terminal-upgrade command for a device.
func (c *memoryIndustryRepository) Upgrade(phone string) *model.Msg8108 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := c.upgrades[phone]
	if m == nil {
		return nil
	}
	copy := *m
	copy.Data = append([]byte(nil), m.Data...)
	return &copy
}

// UpgradeResult returns the latest 0x0108 upgrade-result notification for a device.
func (c *memoryIndustryRepository) UpgradeResult(phone string) *model.Msg0108 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := c.upgradeResults[phone]
	if m == nil {
		return nil
	}
	copy := *m
	return &copy
}

// TransparentDown returns the latest 0x8900 downlink transparent message for a device.
func (c *memoryIndustryRepository) TransparentDown(phone string) *model.Msg8900 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := c.transparentDown[phone]
	if m == nil {
		return nil
	}
	copy := *m
	copy.Data = append([]byte(nil), m.Data...)
	return &copy
}

// TransparentUp returns the latest 0x0900 uplink transparent message for a device.
func (c *memoryIndustryRepository) TransparentUp(phone string) *model.Msg0900 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := c.transparentUp[phone]
	if m == nil {
		return nil
	}
	copy := *m
	copy.Data = append([]byte(nil), m.Data...)
	return &copy
}

// Compressed returns the latest 0x0901 compressed data report for a device.
func (c *memoryIndustryRepository) Compressed(phone string) *model.Msg0901 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := c.compressed[phone]
	if m == nil {
		return nil
	}
	copy := *m
	copy.Data = append([]byte(nil), m.Data...)
	copy.Decompressed = append([]byte(nil), m.Decompressed...)
	return &copy
}
func (c *memoryIndustryRepository) Driver(phone string) *model.Msg0702 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := c.drivers[phone]
	if m == nil {
		return nil
	}
	copy := *m
	return &copy
}
func (c *memoryIndustryRepository) CAN(phone string) *model.Msg0705 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := c.cans[phone]
	if m == nil {
		return nil
	}
	copy := *m
	copy.Items = append([]model.CANDataItem(nil), m.Items...)
	return &copy
}
func (c *memoryIndustryRepository) RecordData(phone string) *model.Msg0700 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := c.records[phone]
	if m == nil {
		return nil
	}
	copy := *m
	copy.Data = append([]byte(nil), m.Data...)
	return &copy
}
func (c *memoryIndustryRepository) MediaUploads(phone string) []model.Msg0801 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]model.Msg0801, 0, len(c.mediaUploads[phone]))
	for _, m := range c.mediaUploads[phone] {
		out = append(out, *m)
	}
	return out
}

func (c *memoryIndustryRepository) MediaEvent(phone string) *model.Msg0800 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := c.mediaEvents[phone]
	if m == nil {
		return nil
	}
	copy := *m
	return &copy
}

func (c *memoryIndustryRepository) Capture(phone string) *model.Msg0805 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := c.captures[phone]
	if m == nil {
		return nil
	}
	copy := *m
	copy.MediaIDs = append([]uint32(nil), m.MediaIDs...)
	return &copy
}

func (c *memoryIndustryRepository) MediaSearch(phone string) *model.Msg0802 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := c.mediaSearches[phone]
	if m == nil {
		return nil
	}
	copy := *m
	copy.Items = append([]model.MediaSearchItem(nil), m.Items...)
	return &copy
}

// MediaResourceList returns the latest 0x1205 resource list response for a
// device, or nil if the terminal has not answered a 0x9205 query.
func (c *memoryIndustryRepository) MediaResourceList(phone string) *model.Msg1205 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := c.mediaResources[phone]
	if m == nil {
		return nil
	}
	copy := *m
	copy.Items = append([]model.DeviceMedia(nil), m.Items...)
	return &copy
}

// SetRecording toggles the terminal simulator's recording flag for a device.
func (c *memoryIndustryRepository) SetRecording(phone string, active bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recording[phone] = active
}

// IsRecording reports whether the terminal simulator is currently recording.
func (c *memoryIndustryRepository) IsRecording(phone string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.recording[phone]
}

// 编译期确认内存实现满足接口。
var _ IndustryRepository = (*memoryIndustryRepository)(nil)
