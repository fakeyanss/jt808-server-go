package repository

import (
	"context"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// GeoRepository 定位热读仓储（C 类，派生最新值）。
type GeoRepository interface {
	Append(ctx context.Context, geo *model.DeviceGeo) error
	Latest(ctx context.Context, phone string) (*model.DeviceGeo, error)
	Delete(ctx context.Context, phone string) error
}
