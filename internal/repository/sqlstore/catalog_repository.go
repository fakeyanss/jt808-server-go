package sqlstore

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// 终端配置目录类型，对应 terminal_catalogs.catalog_type。
const (
	catalogTypeEvent           = "event"
	catalogTypeInfoMenu        = "info_menu"
	catalogTypePhonebook       = "phonebook"
	catalogTypeUpgrade         = "upgrade"
	catalogTypeTransparentDown = "transparent_down"
)

// catalogEntry 是待写入 terminal_catalogs 的一行。
type catalogEntry struct {
	key     string
	payload []byte
}

// upgradePayload 是 0x8108 落库时保存的字段；不序列化消息头，避免跨进程残留 session 信息。
type upgradePayload struct {
	UpgradeType uint8  `json:"upgradeType"`
	MakerID     string `json:"makerId"`
	Version     string `json:"version"`
	Data        []byte `json:"data"`
}

// transparentDownPayload 是 0x8900 落库时保存的字段。
type transparentDownPayload struct {
	Type uint32 `json:"type"`
	Data []byte `json:"data"`
}

// industryRepository 在内存实现之上叠加配置类数据的整包落库。
// 读取路径仍由内存实现承担：启动时从 DB 回灌一次，进程内查询不访问 DB。
type industryRepository struct {
	repository.IndustryRepository
	store *Store
	mu    sync.Mutex
}

func newIndustryRepository(store *Store) (repository.IndustryRepository, error) {
	repo := &industryRepository{
		IndustryRepository: repository.NewIndustryRepository(),
		store:              store,
	}
	if err := repo.load(context.Background()); err != nil {
		return nil, err
	}
	return repo, nil
}

// load 从 terminal_catalogs 回灌内存目录。单条快照（升级/透传）直接重建消息。
func (r *industryRepository) load(ctx context.Context) error {
	rows, err := r.store.db.QueryContext(ctx,
		`SELECT phone, catalog_type, item_key, payload_json FROM terminal_catalogs`)
	if err != nil {
		return fmt.Errorf("load terminal catalogs: %w", err)
	}
	defer rows.Close()

	type row struct {
		phone       string
		catalogType string
		key         string
		payload     []byte
	}
	var all []row
	for rows.Next() {
		var item row
		if err := rows.Scan(&item.phone, &item.catalogType, &item.key, &item.payload); err != nil {
			return fmt.Errorf("scan terminal catalog: %w", err)
		}
		all = append(all, item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate terminal catalogs: %w", err)
	}

	type phoneCatalog struct {
		events    []model.EventItem
		menus     []model.InfoMenuItem
		phonebook []model.PhonebookContact
	}
	catalogs := map[string]*phoneCatalog{}
	get := func(phone string) *phoneCatalog {
		c, ok := catalogs[phone]
		if !ok {
			c = &phoneCatalog{}
			catalogs[phone] = c
		}
		return c
	}

	for _, item := range all {
		switch item.catalogType {
		case catalogTypeEvent:
			var event model.EventItem
			if err := json.Unmarshal(item.payload, &event); err != nil {
				return fmt.Errorf("decode event catalog %s/%s: %w", item.phone, item.key, err)
			}
			c := get(item.phone)
			c.events = append(c.events, event)
		case catalogTypeInfoMenu:
			var menu model.InfoMenuItem
			if err := json.Unmarshal(item.payload, &menu); err != nil {
				return fmt.Errorf("decode info menu catalog %s/%s: %w", item.phone, item.key, err)
			}
			c := get(item.phone)
			c.menus = append(c.menus, menu)
		case catalogTypePhonebook:
			var contact model.PhonebookContact
			if err := json.Unmarshal(item.payload, &contact); err != nil {
				return fmt.Errorf("decode phonebook catalog %s/%s: %w", item.phone, item.key, err)
			}
			c := get(item.phone)
			c.phonebook = append(c.phonebook, contact)
		case catalogTypeUpgrade:
			var payload upgradePayload
			if err := json.Unmarshal(item.payload, &payload); err != nil {
				return fmt.Errorf("decode upgrade catalog %s: %w", item.phone, err)
			}
			r.IndustryRepository.Record(&model.Msg8108{
				Header:      &model.MsgHeader{PhoneNumber: item.phone},
				UpgradeType: payload.UpgradeType,
				MakerID:     payload.MakerID,
				Version:     payload.Version,
				Data:        payload.Data,
			})
		case catalogTypeTransparentDown:
			var payload transparentDownPayload
			if err := json.Unmarshal(item.payload, &payload); err != nil {
				return fmt.Errorf("decode transparent downlink catalog %s: %w", item.phone, err)
			}
			r.IndustryRepository.Record(&model.Msg8900{
				Header: &model.MsgHeader{PhoneNumber: item.phone},
				Type:   payload.Type,
				Data:   payload.Data,
			})
		}
	}

	for phone, c := range catalogs {
		if len(c.events) > 0 {
			r.IndustryRepository.ApplyEventSet(phone, 1, c.events)
		}
		if len(c.menus) > 0 {
			r.IndustryRepository.ApplyInfoMenuSet(phone, 1, c.menus)
		}
		if len(c.phonebook) > 0 {
			r.IndustryRepository.ApplyPhonebookSet(phone, 1, c.phonebook)
		}
	}
	return nil
}

// Record 先走内存实现，再对 0x8108 / 0x8900 做单条快照覆盖。
func (r *industryRepository) Record(msg model.JT808Msg) {
	r.IndustryRepository.Record(msg)
	if msg == nil || msg.GetHeader() == nil {
		return
	}
	phone := msg.GetHeader().PhoneNumber
	switch m := msg.(type) {
	case *model.Msg8108:
		payload, err := json.Marshal(upgradePayload{
			UpgradeType: m.UpgradeType,
			MakerID:     m.MakerID,
			Version:     m.Version,
			Data:        m.Data,
		})
		if err != nil {
			log.Error().Err(err).Str("phone", phone).Msg("encode terminal upgrade catalog")
			return
		}
		r.persistSnapshot(phone, catalogTypeUpgrade, payload)
	case *model.Msg8900:
		payload, err := json.Marshal(transparentDownPayload{Type: m.Type, Data: m.Data})
		if err != nil {
			log.Error().Err(err).Str("phone", phone).Msg("encode transparent downlink catalog")
			return
		}
		r.persistSnapshot(phone, catalogTypeTransparentDown, payload)
	}
}

func (r *industryRepository) ApplyEventSet(phone string, setType uint8, items []model.EventItem) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.IndustryRepository.ApplyEventSet(phone, setType, items)
	entries := make([]catalogEntry, 0, len(items))
	for _, item := range r.IndustryRepository.Events(phone) {
		payload, err := json.Marshal(item)
		if err != nil {
			log.Error().Err(err).Str("phone", phone).Msg("encode event catalog")
			return
		}
		entries = append(entries, catalogEntry{key: strconv.Itoa(int(item.ID)), payload: payload})
	}
	r.replaceCatalog(phone, catalogTypeEvent, entries)
}

func (r *industryRepository) ApplyInfoMenuSet(phone string, setType uint8, items []model.InfoMenuItem) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.IndustryRepository.ApplyInfoMenuSet(phone, setType, items)
	entries := make([]catalogEntry, 0, len(items))
	for _, item := range r.IndustryRepository.InfoMenu(phone) {
		payload, err := json.Marshal(item)
		if err != nil {
			log.Error().Err(err).Str("phone", phone).Msg("encode info menu catalog")
			return
		}
		entries = append(entries, catalogEntry{key: strconv.FormatUint(uint64(item.ID), 10), payload: payload})
	}
	r.replaceCatalog(phone, catalogTypeInfoMenu, entries)
}

func (r *industryRepository) ApplyPhonebookSet(phone string, setType uint8, items []model.PhonebookContact) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.IndustryRepository.ApplyPhonebookSet(phone, setType, items)
	entries := make([]catalogEntry, 0, len(items))
	for _, item := range r.IndustryRepository.Phonebook(phone) {
		payload, err := json.Marshal(item)
		if err != nil {
			log.Error().Err(err).Str("phone", phone).Msg("encode phonebook catalog")
			return
		}
		entries = append(entries, catalogEntry{key: item.Phone, payload: payload})
	}
	r.replaceCatalog(phone, catalogTypePhonebook, entries)
}

// persistSnapshot 覆盖单条快照（升级包或透传命令）。
func (r *industryRepository) persistSnapshot(phone, catalogType string, payload []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replaceCatalog(phone, catalogType, []catalogEntry{{key: "latest", payload: payload}})
}

// replaceCatalog 在同一事务内删除旧目录并写入完整快照，保证整包覆盖的原子性。
func (r *industryRepository) replaceCatalog(phone, catalogType string, entries []catalogEntry) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		log.Error().Err(err).Str("phone", phone).Str("catalog", catalogType).Msg("begin terminal catalog transaction")
		return
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM terminal_catalogs WHERE phone = ? AND catalog_type = ?`, phone, catalogType); err != nil {
		log.Error().Err(err).Str("phone", phone).Str("catalog", catalogType).Msg("delete terminal catalog")
		return
	}
	now := time.Now().UnixMilli()
	for _, entry := range entries {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO terminal_catalogs (phone, catalog_type, item_key, payload_json, updated_at) VALUES (?, ?, ?, ?, ?)`,
			phone, catalogType, entry.key, string(entry.payload), now); err != nil {
			log.Error().Err(err).Str("phone", phone).Str("catalog", catalogType).Str("key", entry.key).Msg("insert terminal catalog")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		log.Error().Err(err).Str("phone", phone).Str("catalog", catalogType).Msg("commit terminal catalog transaction")
	}
}

var _ repository.IndustryRepository = (*industryRepository)(nil)
