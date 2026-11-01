package repository

import (
	"sync"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// RegionCatalog stores the area/route objects that the terminal simulator has
// accepted through 0x8600/0x8602/0x8604/0x8606 set messages. It backs the
// 0x0608 query response so that set → query round trips return typed records.
type RegionCatalog struct {
	mu         sync.RWMutex
	circles    map[string]map[uint32]model.CircleArea
	rectangles map[string]map[uint32]model.RectangleArea
	polygons   map[string]map[uint32]model.PolygonArea
	routes     map[string]map[uint32]model.Route
}

var regionCatalog = &RegionCatalog{
	circles:    map[string]map[uint32]model.CircleArea{},
	rectangles: map[string]map[uint32]model.RectangleArea{},
	polygons:   map[string]map[uint32]model.PolygonArea{},
	routes:     map[string]map[uint32]model.Route{},
}

func GetRegionCatalog() *RegionCatalog { return regionCatalog }

func (c *RegionCatalog) ensureMaps(phone string) {
	if c.circles[phone] == nil {
		c.circles[phone] = map[uint32]model.CircleArea{}
	}
	if c.rectangles[phone] == nil {
		c.rectangles[phone] = map[uint32]model.RectangleArea{}
	}
	if c.polygons[phone] == nil {
		c.polygons[phone] = map[uint32]model.PolygonArea{}
	}
	if c.routes[phone] == nil {
		c.routes[phone] = map[uint32]model.Route{}
	}
}

// ApplySet updates the catalog according to the setting attribute semantics.
// setting attr bit0=update (replace all), bit1=append, bit2=modify.
func (c *RegionCatalog) ApplySet(phone string, set *model.MsgRegionRouteSet) {
	if set == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ensureMaps(phone)

	update := set.SettingAttr&1 != 0

	switch set.Type {
	case model.RegionRouteCircle:
		if update {
			c.circles[phone] = map[uint32]model.CircleArea{}
		}
		for _, item := range set.Circles {
			c.circles[phone][item.ID] = item
		}
	case model.RegionRouteRect:
		if update {
			c.rectangles[phone] = map[uint32]model.RectangleArea{}
		}
		for _, item := range set.Rectangles {
			c.rectangles[phone][item.ID] = item
		}
	case model.RegionRoutePolygon:
		if update {
			c.polygons[phone] = map[uint32]model.PolygonArea{}
		}
		for _, item := range set.Polygons {
			c.polygons[phone][item.ID] = item
		}
	case model.RegionRouteRoute:
		if update {
			c.routes[phone] = map[uint32]model.Route{}
		}
		for _, item := range set.Routes {
			c.routes[phone][item.ID] = item
		}
	}
}

// Delete removes the given IDs for the selected family. An empty id list
// deletes all objects of that family.
func (c *RegionCatalog) Delete(phone string, typ model.RegionRouteType, ids []uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ensureMaps(phone)
	deleteAll := len(ids) == 0
	idSet := map[uint32]bool{}
	for _, id := range ids {
		idSet[id] = true
	}
	switch typ {
	case model.RegionRouteCircle:
		m := c.circles[phone]
		if deleteAll {
			for k := range m {
				delete(m, k)
			}
		} else {
			for id := range idSet {
				delete(m, id)
			}
		}
	case model.RegionRouteRect:
		m := c.rectangles[phone]
		if deleteAll {
			for k := range m {
				delete(m, k)
			}
		} else {
			for id := range idSet {
				delete(m, id)
			}
		}
	case model.RegionRoutePolygon:
		m := c.polygons[phone]
		if deleteAll {
			for k := range m {
				delete(m, k)
			}
		} else {
			for id := range idSet {
				delete(m, id)
			}
		}
	case model.RegionRouteRoute:
		m := c.routes[phone]
		if deleteAll {
			for k := range m {
				delete(m, k)
			}
		} else {
			for id := range idSet {
				delete(m, id)
			}
		}
	}
}

// Query returns the stored records for a query type, optionally filtered by IDs.
func (c *RegionCatalog) Query(phone string, queryType uint8, ids []uint32) *model.Msg0608 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	c.ensureMaps(phone)
	resp := &model.Msg0608{QueryType: queryType}
	idSet := map[uint32]bool{}
	for _, id := range ids {
		idSet[id] = true
	}
	filter := func(id uint32) bool { return len(ids) == 0 || idSet[id] }
	switch queryType {
	case 1:
		for id, item := range c.circles[phone] {
			if filter(id) {
				resp.Circles = append(resp.Circles, item)
			}
		}
	case 2:
		for id, item := range c.rectangles[phone] {
			if filter(id) {
				resp.Rectangles = append(resp.Rectangles, item)
			}
		}
	case 3:
		for id, item := range c.polygons[phone] {
			if filter(id) {
				resp.Polygons = append(resp.Polygons, item)
			}
		}
	case 4:
		for id, item := range c.routes[phone] {
			if filter(id) {
				resp.Routes = append(resp.Routes, item)
			}
		}
	}
	return resp
}
