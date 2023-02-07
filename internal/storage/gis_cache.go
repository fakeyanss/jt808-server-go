package storage

import "github.com/fakeYanss/jt808-server-go/internal/protocol/model"

type GisCache struct {
	cacheByID map[string]*model.GISMeta
}
