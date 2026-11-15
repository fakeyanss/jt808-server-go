package repository

import (
	"context"

	"github.com/fakeyanss/jt808-server-go/internal/domain"
)

// TerminalPropertiesRepository 终端属性档案仓储（B 类）。
// 数据只来自 0x0107 查询应答，与 Device 的静态字段独立存储。
type TerminalPropertiesRepository interface {
	Get(ctx context.Context, phone string) (*domain.TerminalProperties, error)
	Replace(ctx context.Context, properties *domain.TerminalProperties) error
	Delete(ctx context.Context, phone string) error
}
