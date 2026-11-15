package repository

import (
	"context"

	"github.com/fakeyanss/jt808-server-go/internal/domain"
)

// DeviceParamsRepository 终端参数仓储（B 类）。
type DeviceParamsRepository interface {
	Get(ctx context.Context, phone string) (*domain.DeviceParams, error)
	Replace(ctx context.Context, params *domain.DeviceParams) error
	Delete(ctx context.Context, phone string) error
}
