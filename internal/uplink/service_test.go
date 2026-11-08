package uplink

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// recordingLocations 是用于断言重放结果的定位仓储替身。
type recordingLocations struct {
	records []repository.LocationRecord
	err     error
}

func (r *recordingLocations) Record(_ context.Context, records []repository.LocationRecord) error {
	if r.err != nil {
		return r.err
	}
	r.records = append(r.records, records...)
	return nil
}

func (r *recordingLocations) List(context.Context, string, int) ([]repository.LocationRecord, error) {
	return nil, nil
}

func (r *recordingLocations) ListPage(context.Context, repository.HistoryQuery) ([]repository.LocationRecord, int, error) {
	return nil, 0, nil
}

// recordingAlarms 是用于断言重放结果的报警仓储替身。
type recordingAlarms struct {
	events []repository.AlarmEvent
	err    error
}

func (r *recordingAlarms) Record(_ context.Context, phone string, serial uint16, alarmSign,
	latitude, longitude uint32, occurredAt string) error {
	if r.err != nil {
		return r.err
	}
	r.events = append(r.events, repository.AlarmEvent{
		Phone:         phone,
		MessageSerial: serial,
		AlarmSign:     alarmSign,
		Latitude:      latitude,
		Longitude:     longitude,
		OccurredAt:    occurredAt,
	})
	return nil
}

func (r *recordingAlarms) List(context.Context, string) ([]repository.AlarmEvent, error) {
	return nil, nil
}

func (r *recordingAlarms) ListPage(context.Context, repository.HistoryQuery) ([]repository.AlarmEvent, int, error) {
	return nil, 0, nil
}

func (r *recordingAlarms) RegisterConfirmation(context.Context, string, uint16, uint16, uint32) error {
	return nil
}

func (r *recordingAlarms) ConfirmByCommand(context.Context, string, uint16) (bool, error) {
	return false, nil
}

func newTestService(t *testing.T, locations repository.LocationRepository, alarms repository.AlarmRepository,
	defaultReliability, location, batchLocation, alarm string) (*Service, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "spool")
	service, err := NewService(NewPolicy(defaultReliability, location, batchLocation, alarm), dir, locations, alarms)
	require.NoError(t, err)
	return service, dir
}

// locationMessage 构造一条 0x0200 报警上报，用于驱动 at_least_once 分类。
func locationMessage(phone string, serial uint16, alarmSign uint32) *model.Msg0200 {
	return &model.Msg0200{
		Header:    &model.MsgHeader{MsgID: 0x0200, PhoneNumber: phone, SerialNumber: serial},
		AlarmSign: alarmSign,
		Latitude:  39543265,
		Longitude: 116235432,
		Time:      "241108120000",
	}
}

func TestBeforeAckPersistsAtLeastOnceRecords(t *testing.T) {
	service, _ := newTestService(t, &recordingLocations{}, &recordingAlarms{},
		"best_effort", "best_effort", "at_least_once", "at_least_once")

	message := locationMessage("18800001111", 21, 1)
	ids, err := service.BeforeAck(context.Background(), 0x0200, &model.ProcessData{Incoming: message})
	require.NoError(t, err)
	require.Len(t, ids, 1)

	// BeforeAck 返回时记录必须已经落盘。
	pending, err := service.spool.Pending()
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, "18800001111", pending[0].Record.Phone)
	require.Equal(t, uint32(1), pending[0].Record.AlarmSign)

	// 业务成功后清理。
	service.AfterBusiness(ids)
	pending, err = service.spool.Pending()
	require.NoError(t, err)
	require.Empty(t, pending)
}

func TestBeforeAckSkipsBestEffort(t *testing.T) {
	service, dir := newTestService(t, &recordingLocations{}, &recordingAlarms{},
		"best_effort", "best_effort", "at_least_once", "at_least_once")

	message := locationMessage("18800001112", 22, 0)
	ids, err := service.BeforeAck(context.Background(), 0x0200, &model.ProcessData{Incoming: message})
	require.NoError(t, err)
	require.Empty(t, ids)

	pending, err := service.spool.Pending()
	require.NoError(t, err)
	require.Empty(t, pending, "best_effort must not write spool records")
	require.Equal(t, dir, service.SpoolDir())
}

func TestBeforeAckPersistsBatchLocationItems(t *testing.T) {
	service, _ := newTestService(t, &recordingLocations{}, &recordingAlarms{},
		"best_effort", "best_effort", "at_least_once", "at_least_once")

	batch := &model.Msg0704{
		Header: &model.MsgHeader{MsgID: 0x0704, PhoneNumber: "18800001113", SerialNumber: 23},
		Items: []model.BatchLocationItem{
			{Loc: model.Msg0200{Header: &model.MsgHeader{PhoneNumber: "18800001113"}, Time: "241108120000"}},
			{Loc: model.Msg0200{Header: &model.MsgHeader{PhoneNumber: "18800001113"}, Time: "241108120001"}},
		},
	}
	ids, err := service.BeforeAck(context.Background(), 0x0704, &model.ProcessData{Incoming: batch})
	require.NoError(t, err)
	require.Len(t, ids, 2)

	pending, err := service.spool.Pending()
	require.NoError(t, err)
	require.Len(t, pending, 2)
}

func TestReplayWritesLocationsAndAlarmsThenClearsSpool(t *testing.T) {
	locations := &recordingLocations{}
	alarms := &recordingAlarms{}
	service, _ := newTestService(t, locations, alarms,
		"best_effort", "best_effort", "at_least_once", "at_least_once")

	// 一条带报警标志的定位（重放时要同时写定位与报警）。
	_, err := service.spool.Put(SpoolRecord{
		LocationRecord: repository.LocationRecord{
			Phone:      "18800001114",
			MsgID:      0x0200,
			AlarmSign:  3,
			OccurredAt: "241108120000",
		},
		Serial: 24,
	})
	require.NoError(t, err)
	// 一条普通批量定位（重放只写定位）。
	_, err = service.spool.Put(SpoolRecord{
		LocationRecord: repository.LocationRecord{
			Phone:      "18800001115",
			MsgID:      0x0704,
			OccurredAt: "241108120001",
		},
	})
	require.NoError(t, err)

	require.NoError(t, service.Replay(context.Background()))
	require.Len(t, locations.records, 2)
	require.Len(t, alarms.events, 1)
	require.Equal(t, "18800001114", alarms.events[0].Phone)
	require.Equal(t, uint16(24), alarms.events[0].MessageSerial)

	pending, err := service.spool.Pending()
	require.NoError(t, err)
	require.Empty(t, pending, "replayed records must be acknowledged")
}

func TestReplayKeepsSpoolWhenStorageFails(t *testing.T) {
	locations := &recordingLocations{err: errors.New("storage unavailable")}
	service, _ := newTestService(t, locations, &recordingAlarms{},
		"best_effort", "best_effort", "at_least_once", "at_least_once")

	_, err := service.spool.Put(SpoolRecord{
		LocationRecord: repository.LocationRecord{Phone: "18800001116", MsgID: 0x0704},
	})
	require.NoError(t, err)

	require.Error(t, service.Replay(context.Background()))
	// 写库失败时记录必须保留，供下次启动再次重放。
	pending, err := service.spool.Pending()
	require.NoError(t, err)
	require.Len(t, pending, 1)
}
