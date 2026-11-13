package sqlcore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/repository/memory"
)

// regionItem 是待写入 region_items 的一行。
type regionItem struct {
	id      uint32
	payload []byte
}

// regionRepository 在内存实现之上叠加区域/路线的持久化。
// Query 继续走内存目录，启动时从 region_items 回灌一次。
type regionRepository struct {
	repository.RegionRepository
	store *Store
	mu    sync.Mutex
}

func newRegionRepository(store *Store) (repository.RegionRepository, error) {
	repo := &regionRepository{
		RegionRepository: memory.NewRegionRepository(),
		store:            store,
	}
	if err := repo.load(context.Background()); err != nil {
		return nil, err
	}
	return repo, nil
}

// load 从 region_items 回灌内存目录。回灌使用“更新”属性，保证整族覆盖一致。
func (r *regionRepository) load(ctx context.Context) error {
	ctx, cancel := readContext(ctx)
	defer cancel()
	rows, err := r.store.readDB.QueryContext(ctx,
		`SELECT phone, region_type, payload_json FROM region_items`)
	if err != nil {
		return fmt.Errorf("load region items: %w", err)
	}
	defer rows.Close()

	type group struct {
		circles    []model.CircleArea
		rectangles []model.RectangleArea
		polygons   []model.PolygonArea
		routes     []model.Route
	}
	groups := map[string]*group{}
	for rows.Next() {
		var (
			phone   string
			typ     int
			payload []byte
		)
		if err := rows.Scan(&phone, &typ, &payload); err != nil {
			return fmt.Errorf("scan region item: %w", err)
		}
		g, ok := groups[phone]
		if !ok {
			g = &group{}
			groups[phone] = g
		}
		switch model.RegionRouteType(typ) {
		case model.RegionRouteCircle:
			var item model.CircleArea
			if err := json.Unmarshal(payload, &item); err != nil {
				return fmt.Errorf("decode circle area %s: %w", phone, err)
			}
			g.circles = append(g.circles, item)
		case model.RegionRouteRect:
			var item model.RectangleArea
			if err := json.Unmarshal(payload, &item); err != nil {
				return fmt.Errorf("decode rectangle area %s: %w", phone, err)
			}
			g.rectangles = append(g.rectangles, item)
		case model.RegionRoutePolygon:
			var item model.PolygonArea
			if err := json.Unmarshal(payload, &item); err != nil {
				return fmt.Errorf("decode polygon area %s: %w", phone, err)
			}
			g.polygons = append(g.polygons, item)
		case model.RegionRouteRoute:
			var item model.Route
			if err := json.Unmarshal(payload, &item); err != nil {
				return fmt.Errorf("decode route %s: %w", phone, err)
			}
			g.routes = append(g.routes, item)
		default:
			return fmt.Errorf("unsupported region type %d for %s", typ, phone)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate region items: %w", err)
	}

	for phone, g := range groups {
		if len(g.circles) > 0 {
			r.RegionRepository.ApplySet(phone, &model.MsgRegionRouteSet{
				SettingAttr: 1, Type: model.RegionRouteCircle, Circles: g.circles,
			})
		}
		if len(g.rectangles) > 0 {
			r.RegionRepository.ApplySet(phone, &model.MsgRegionRouteSet{
				SettingAttr: 1, Type: model.RegionRouteRect, Rectangles: g.rectangles,
			})
		}
		if len(g.polygons) > 0 {
			r.RegionRepository.ApplySet(phone, &model.MsgRegionRouteSet{
				SettingAttr: 1, Type: model.RegionRoutePolygon, Polygons: g.polygons,
			})
		}
		if len(g.routes) > 0 {
			r.RegionRepository.ApplySet(phone, &model.MsgRegionRouteSet{
				SettingAttr: 1, Type: model.RegionRouteRoute, Routes: g.routes,
			})
		}
	}
	return nil
}

// ApplySet 先更新内存目录，再把整族快照在同一事务内覆盖到 DB。
func (r *regionRepository) ApplySet(phone string, set *model.MsgRegionRouteSet) {
	if set == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.RegionRepository.ApplySet(phone, set)

	snapshot := r.RegionRepository.Query(phone, uint8(set.Type), nil)
	items, err := regionSnapshotItems(snapshot, set.Type)
	if err != nil {
		log.Error().Err(err).Str("phone", phone).Int("region_type", int(set.Type)).Msg("encode region items")
		return
	}
	r.replaceFamily(phone, set.Type, items)
}

// Delete 先更新内存目录，再删除 DB 中对应行。空 ID 列表表示删除整族。
func (r *regionRepository) Delete(phone string, typ model.RegionRouteType, ids []uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.RegionRepository.Delete(phone, typ, ids)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if len(ids) == 0 {
		if _, err := r.store.db.ExecContext(ctx,
			`DELETE FROM region_items WHERE phone = ? AND region_type = ?`, phone, int(typ)); err != nil {
			log.Error().Err(err).Str("phone", phone).Int("region_type", int(typ)).Msg("delete region items")
		}
		return
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, 0, len(ids)+2)
	args = append(args, phone, int(typ))
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, int64(id))
	}
	stmt := `DELETE FROM region_items WHERE phone = ? AND region_type = ? AND item_id IN (` +
		strings.Join(placeholders, ", ") + `)`
	if _, err := r.store.db.ExecContext(ctx, stmt, args...); err != nil {
		log.Error().Err(err).Str("phone", phone).Int("region_type", int(typ)).Msg("delete region items")
	}
}

// regionSnapshotItems 把内存快照转换为待落库的 JSON 行。
func regionSnapshotItems(snapshot *model.Msg0608, typ model.RegionRouteType) ([]regionItem, error) {
	switch typ {
	case model.RegionRouteCircle:
		items := make([]regionItem, 0, len(snapshot.Circles))
		for _, item := range snapshot.Circles {
			payload, err := json.Marshal(item)
			if err != nil {
				return nil, err
			}
			items = append(items, regionItem{id: item.ID, payload: payload})
		}
		return items, nil
	case model.RegionRouteRect:
		items := make([]regionItem, 0, len(snapshot.Rectangles))
		for _, item := range snapshot.Rectangles {
			payload, err := json.Marshal(item)
			if err != nil {
				return nil, err
			}
			items = append(items, regionItem{id: item.ID, payload: payload})
		}
		return items, nil
	case model.RegionRoutePolygon:
		items := make([]regionItem, 0, len(snapshot.Polygons))
		for _, item := range snapshot.Polygons {
			payload, err := json.Marshal(item)
			if err != nil {
				return nil, err
			}
			items = append(items, regionItem{id: item.ID, payload: payload})
		}
		return items, nil
	case model.RegionRouteRoute:
		items := make([]regionItem, 0, len(snapshot.Routes))
		for _, item := range snapshot.Routes {
			payload, err := json.Marshal(item)
			if err != nil {
				return nil, err
			}
			items = append(items, regionItem{id: item.ID, payload: payload})
		}
		return items, nil
	default:
		return nil, fmt.Errorf("unsupported region type %d", typ)
	}
}

// replaceFamily 在同一事务内删除整族旧记录并写入完整快照。
// “清空后重设”依赖该事务：中途失败会整体回滚，不会留下半套区域数据。
func (r *regionRepository) replaceFamily(phone string, typ model.RegionRouteType, items []regionItem) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		log.Error().Err(err).Str("phone", phone).Int("region_type", int(typ)).Msg("begin region transaction")
		return
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM region_items WHERE phone = ? AND region_type = ?`, phone, int(typ)); err != nil {
		log.Error().Err(err).Str("phone", phone).Int("region_type", int(typ)).Msg("delete region items")
		return
	}
	now := time.Now().UnixMilli()
	for _, item := range items {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO region_items (phone, region_type, item_id, payload_json, updated_at) VALUES (?, ?, ?, ?, ?)`,
			phone, int(typ), int64(item.id), string(item.payload), now); err != nil {
			log.Error().Err(err).Str("phone", phone).Int("region_type", int(typ)).Int64("item_id", int64(item.id)).Msg("insert region item")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		log.Error().Err(err).Str("phone", phone).Int("region_type", int(typ)).Msg("commit region transaction")
	}
}

var _ repository.RegionRepository = (*regionRepository)(nil)
