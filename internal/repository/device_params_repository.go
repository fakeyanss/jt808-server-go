package repository

import (
	"context"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// DeviceParamsRepository 终端参数仓储（B 类）。
type DeviceParamsRepository interface {
	Get(ctx context.Context, phone string) (*model.DeviceParams, error)
	Replace(ctx context.Context, params *model.DeviceParams) error
	Delete(ctx context.Context, phone string) error
}
