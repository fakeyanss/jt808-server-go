package sqlstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

func catalogHeader(phone string) *model.MsgHeader {
	return &model.MsgHeader{PhoneNumber: phone}
}

// SQLite 写入终端配置目录后关闭重开，事件/菜单/电话本/升级/透传全部仍在。
func TestSQLitePersistsTerminalCatalogsAcrossReopen(t *testing.T) {
	const phone = "188765500001"
	dbPath := filepath.Join(t.TempDir(), "jt808.db")
	migrateSQLite(t, dbPath)

	first, err := Open(Config{Backend: BackendSQLite, Path: dbPath})
	require.NoError(t, err)

	industry, err := first.IndustryRepository()
	require.NoError(t, err)
	industry.ApplyEventSet(phone, 1, []model.EventItem{
		{ID: 1, Content: "超速"},
		{ID: 2, Content: "疲劳驾驶"},
	})
	industry.ApplyEventSet(phone, 2, []model.EventItem{{ID: 3, Content: "碰撞"}})
	industry.ApplyInfoMenuSet(phone, 1, []model.InfoMenuItem{
		{ID: 100, Content: "新闻"},
		{ID: 200, Content: "天气"},
	})
	industry.ApplyPhonebookSet(phone, 1, []model.PhonebookContact{
		{Flag: 1, Phone: "13800138000", Name: "调度"},
	})
	upgradeData := []byte{0x01, 0x02, 0x03}
	industry.Record(&model.Msg8108{
		Header:      catalogHeader(phone),
		UpgradeType: 0,
		MakerID:     "JT808",
		Version:     "v1.2.3",
		Data:        upgradeData,
	})
	transparentIDs := []uint8{0x64, 0x65}
	industry.Record(&model.Msg8900{
		Header: catalogHeader(phone),
		Type:   0xF7,
		IDs:    transparentIDs,
	})

	// 进程内立即可见。
	require.Len(t, industry.Events(phone), 3)
	require.Len(t, industry.InfoMenu(phone), 2)
	require.Len(t, industry.Phonebook(phone), 1)

	require.NoError(t, first.Close())

	second, err := Open(Config{Backend: BackendSQLite, Path: dbPath})
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })

	reloaded, err := second.IndustryRepository()
	require.NoError(t, err)

	events := map[uint8]string{}
	for _, item := range reloaded.Events(phone) {
		events[item.ID] = item.Content
	}
	require.Equal(t, map[uint8]string{1: "超速", 2: "疲劳驾驶", 3: "碰撞"}, events)

	menus := map[uint32]string{}
	for _, item := range reloaded.InfoMenu(phone) {
		menus[item.ID] = item.Content
	}
	require.Equal(t, map[uint32]string{100: "新闻", 200: "天气"}, menus)

	require.Len(t, reloaded.Phonebook(phone), 1)
	require.Equal(t, "13800138000", reloaded.Phonebook(phone)[0].Phone)

	up := reloaded.Upgrade(phone)
	require.NotNil(t, up)
	require.Equal(t, "JT808", up.MakerID)
	require.Equal(t, "v1.2.3", up.Version)
	require.Equal(t, upgradeData, up.Data)

	down := reloaded.TransparentDown(phone)
	require.NotNil(t, down)
	require.Equal(t, uint8(0xF7), down.Type)
	require.Equal(t, transparentIDs, down.IDs)

	// 删除全部后重开仍为空，验证整包覆盖不是简单追加。
	reloaded.ApplyEventSet(phone, 0, nil)
	require.Len(t, reloaded.Events(phone), 0)
	require.NoError(t, second.Close())

	third, err := Open(Config{Backend: BackendSQLite, Path: dbPath})
	require.NoError(t, err)
	t.Cleanup(func() { _ = third.Close() })
	cleared, err := third.IndustryRepository()
	require.NoError(t, err)
	require.Len(t, cleared.Events(phone), 0)
}

// SQLite 区域/路线：整族覆盖、按 ID 删除、清空全部三类语义在重开后保持一致。
func TestSQLitePersistsRegionItemsAcrossReopen(t *testing.T) {
	const phone = "188765500002"
	dbPath := filepath.Join(t.TempDir(), "jt808.db")
	migrateSQLite(t, dbPath)

	first, err := Open(Config{Backend: BackendSQLite, Path: dbPath})
	require.NoError(t, err)
	regions, err := first.RegionRepository()
	require.NoError(t, err)

	regions.ApplySet(phone, &model.MsgRegionRouteSet{
		SettingAttr: 0,
		Type:        model.RegionRouteCircle,
		Circles: []model.CircleArea{
			{ID: 1, Latitude: 100, Longitude: 200, Radius: 300, Name: "一号区"},
			{ID: 2, Latitude: 110, Longitude: 210, Radius: 310, Name: "二号区"},
		},
	})
	// 更新属性：清空后重设，只保留 ID=3。
	regions.ApplySet(phone, &model.MsgRegionRouteSet{
		SettingAttr: 1,
		Type:        model.RegionRouteCircle,
		Circles:     []model.CircleArea{{ID: 3, Latitude: 120, Longitude: 220, Radius: 320, Name: "三号区"}},
	})
	regions.ApplySet(phone, &model.MsgRegionRouteSet{
		SettingAttr: 1,
		Type:        model.RegionRouteRoute,
		Routes: []model.Route{{
			ID:          9,
			Attr:        model.AreaAttrSpeed,
			Inflections: []model.RouteInflection{{ID: 1, SectionID: 2, Width: 10}},
			Name:        "路线九",
		}},
	})

	require.Len(t, regions.Query(phone, 1, nil).Circles, 1)
	require.Len(t, regions.Query(phone, 4, nil).Routes, 1)
	require.NoError(t, first.Close())

	second, err := Open(Config{Backend: BackendSQLite, Path: dbPath})
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })
	reloaded, err := second.RegionRepository()
	require.NoError(t, err)

	circles := reloaded.Query(phone, 1, nil).Circles
	require.Len(t, circles, 1)
	require.Equal(t, uint32(3), circles[0].ID)
	require.Equal(t, "三号区", circles[0].Name)

	routes := reloaded.Query(phone, 4, nil).Routes
	require.Len(t, routes, 1)
	require.Equal(t, uint32(9), routes[0].ID)
	require.Equal(t, "路线九", routes[0].Name)
	require.Len(t, routes[0].Inflections, 1)

	// 按 ID 删除后重开：只剩另一条（此处先补一条再删）。
	reloaded.ApplySet(phone, &model.MsgRegionRouteSet{
		SettingAttr: 2,
		Type:        model.RegionRouteCircle,
		Circles:     []model.CircleArea{{ID: 4, Latitude: 130, Longitude: 230, Radius: 330, Name: "四号区"}},
	})
	reloaded.Delete(phone, model.RegionRouteCircle, []uint32{3})
	require.Len(t, reloaded.Query(phone, 1, nil).Circles, 1)

	third, err := Open(Config{Backend: BackendSQLite, Path: dbPath})
	require.NoError(t, err)
	t.Cleanup(func() { _ = third.Close() })
	again, err := third.RegionRepository()
	require.NoError(t, err)
	circles = again.Query(phone, 1, nil).Circles
	require.Len(t, circles, 1)
	require.Equal(t, uint32(4), circles[0].ID)

	// 空 ID 列表删除整族。
	again.Delete(phone, model.RegionRouteCircle, nil)
	require.Len(t, again.Query(phone, 1, nil).Circles, 0)
}

// MySQL 需要外部实例；未设置 JT808_TEST_MYSQL_DSN 时跳过。
func TestMySQLCatalogAndRegionPersistence(t *testing.T) {
	dsn := os.Getenv("JT808_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("JT808_TEST_MYSQL_DSN is not set; skipping MySQL integration test")
	}
	ctx := context.Background()
	store, err := Open(Config{Backend: BackendMySQL, DSN: dsn})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	phone := fmt.Sprintf("188%08d", time.Now().UnixNano()%100000000)
	_, err = store.db.ExecContext(ctx, `DELETE FROM terminal_catalogs WHERE phone = ?`, phone)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `DELETE FROM region_items WHERE phone = ?`, phone)
	require.NoError(t, err)

	industry, err := store.IndustryRepository()
	require.NoError(t, err)
	industry.ApplyEventSet(phone, 1, []model.EventItem{{ID: 7, Content: "冒烟"}})
	industry.Record(&model.Msg8108{
		Header:      catalogHeader(phone),
		UpgradeType: 1,
		MakerID:     "MYSQL",
		Version:     "v9",
		Data:        []byte{9},
	})

	regions, err := store.RegionRepository()
	require.NoError(t, err)
	regions.ApplySet(phone, &model.MsgRegionRouteSet{
		SettingAttr: 1,
		Type:        model.RegionRoutePolygon,
		Polygons: []model.PolygonArea{{
			ID:       5,
			Attr:     model.AreaAttrSpeed,
			Vertices: []model.PolygonVertex{{Latitude: 1, Longitude: 2}},
			Name:     "多边形",
		}},
	})

	// 重新装配仓储模拟进程重启，确认 MySQL 侧数据可回灌。
	reloadedIndustry, err := store.IndustryRepository()
	require.NoError(t, err)
	require.Len(t, reloadedIndustry.Events(phone), 1)
	require.Equal(t, "MYSQL", reloadedIndustry.Upgrade(phone).MakerID)

	reloadedRegions, err := store.RegionRepository()
	require.NoError(t, err)
	require.Len(t, reloadedRegions.Query(phone, 3, nil).Polygons, 1)

	_, err = store.db.ExecContext(ctx, `DELETE FROM terminal_catalogs WHERE phone = ?`, phone)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `DELETE FROM region_items WHERE phone = ?`, phone)
	require.NoError(t, err)
}
