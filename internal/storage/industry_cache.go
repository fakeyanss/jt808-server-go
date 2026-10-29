package storage

import (
	"sync"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// IndustryCache retains the latest decoded industry report per device. The
// cache is intentionally in-memory like the current alarm and location caches.
type IndustryCache struct {
	mu            sync.RWMutex
	drivers       map[string]*model.Msg0702
	cans          map[string]*model.Msg0705
	records       map[string]*model.Msg0700
	mediaEvents   map[string]*model.Msg0800
	mediaUploads  map[string][]*model.Msg0801
	captures      map[string]*model.Msg0805
	mediaSearches map[string]*model.Msg0802
}

var industryCache = &IndustryCache{drivers: map[string]*model.Msg0702{}, cans: map[string]*model.Msg0705{}, records: map[string]*model.Msg0700{}, mediaEvents: map[string]*model.Msg0800{}, mediaUploads: map[string][]*model.Msg0801{}, captures: map[string]*model.Msg0805{}, mediaSearches: map[string]*model.Msg0802{}}

func GetIndustryCache() *IndustryCache { return industryCache }
func (c *IndustryCache) Record(msg model.JT808Msg) {
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
	}
}
func (c *IndustryCache) Driver(phone string) *model.Msg0702 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := c.drivers[phone]
	if m == nil {
		return nil
	}
	copy := *m
	return &copy
}
func (c *IndustryCache) CAN(phone string) *model.Msg0705 {
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
func (c *IndustryCache) RecordData(phone string) *model.Msg0700 {
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
func (c *IndustryCache) MediaUploads(phone string) []model.Msg0801 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]model.Msg0801, 0, len(c.mediaUploads[phone]))
	for _, m := range c.mediaUploads[phone] {
		out = append(out, *m)
	}
	return out
}

func (c *IndustryCache) MediaEvent(phone string) *model.Msg0800 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := c.mediaEvents[phone]
	if m == nil {
		return nil
	}
	copy := *m
	return &copy
}

func (c *IndustryCache) Capture(phone string) *model.Msg0805 {
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

func (c *IndustryCache) MediaSearch(phone string) *model.Msg0802 {
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
