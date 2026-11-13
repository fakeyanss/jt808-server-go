package sqlcore

import (
	"context"
	stdsql "database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// deviceParamsRepository 是 repository.DeviceParamsRepository 的 SQL 实现。
// 参数集合按设计决策 D1 整体序列化为 JSON 单字段；读取时重建 ParamData 切片
// 并回填 DevicePhone（该字段不参与 JSON 序列化）。
type deviceParamsRepository struct {
	store *Store
}

func (r *deviceParamsRepository) Get(ctx context.Context, phone string) (*model.DeviceParams, error) {
	ctx, cancel := readContext(ctx)
	defer cancel()
	var (
		paramCnt   int16
		paramsJSON []byte
	)
	err := r.store.readDB.QueryRowContext(ctx,
		`SELECT param_cnt, params_json FROM device_params WHERE phone = ?`, phone,
	).Scan(&paramCnt, &paramsJSON)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil, repository.ErrDeviceParamsNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get device params %s: %w", phone, err)
	}

	params := &model.DeviceParams{DevicePhone: phone, ParamCnt: uint8(paramCnt)}
	if err := json.Unmarshal(paramsJSON, &params.Params); err != nil {
		return nil, fmt.Errorf("decode device params %s: %w", phone, err)
	}
	return params, nil
}

func (r *deviceParamsRepository) Replace(ctx context.Context, params *model.DeviceParams) error {
	payload, err := json.Marshal(params.Params)
	if err != nil {
		return fmt.Errorf("encode device params %s: %w", params.DevicePhone, err)
	}
	stmt := r.store.stmts.UpsertDeviceParams()
	if _, err := r.store.db.ExecContext(ctx, stmt,
		params.DevicePhone, int16(params.ParamCnt), payload, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("replace device params %s: %w", params.DevicePhone, err)
	}
	return nil
}

func (r *deviceParamsRepository) Delete(ctx context.Context, phone string) error {
	if _, err := r.store.db.ExecContext(ctx, `DELETE FROM device_params WHERE phone = ?`, phone); err != nil {
		return fmt.Errorf("delete device params %s: %w", phone, err)
	}
	return nil
}

var _ repository.DeviceParamsRepository = (*deviceParamsRepository)(nil)
