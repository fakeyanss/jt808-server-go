package adminapi

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// stubLocationRepository 只验证管理面是否正确传递查询条件与分页参数。
type stubLocationRepository struct {
	queries []repository.HistoryQuery
	items   []repository.LocationRecord
	total   int
}

func (s *stubLocationRepository) Record(context.Context, []repository.LocationRecord) error {
	return nil
}

func (s *stubLocationRepository) List(context.Context, string, int) ([]repository.LocationRecord, error) {
	return s.items, nil
}

func (s *stubLocationRepository) ListPage(_ context.Context, query repository.HistoryQuery) ([]repository.LocationRecord, int, error) {
	s.queries = append(s.queries, query)
	return s.items, s.total, nil
}

func historyURL(base string, phone string, from, to time.Time, page, pageSize int) string {
	values := url.Values{}
	if phone != "" {
		values.Set("phone", phone)
	}
	if !from.IsZero() {
		values.Set("from", from.Format(time.RFC3339))
	}
	if !to.IsZero() {
		values.Set("to", to.Format(time.RFC3339))
	}
	if page > 0 {
		values.Set("page", strconv.Itoa(page))
	}
	if pageSize > 0 {
		values.Set("pageSize", strconv.Itoa(pageSize))
	}
	return base + "?" + values.Encode()
}

func TestListAlarmsFiltersByPhoneAndTimeRange(t *testing.T) {
	alarms := repository.NewAlarmRepository(repository.AlarmRetention{})
	ctx := context.Background()
	for serial := uint16(1); serial <= 3; serial++ {
		require.NoError(t, alarms.Record(ctx, "18800000001", serial, 1, 0, 0, "261028204500"))
	}
	server := NewServer(testOptions("secret", 100, 200), Dependencies{Alarms: alarms})

	from := time.Now().Add(-time.Hour)
	to := time.Now().Add(time.Hour)
	recorder := doRequest(t, server.Handler(), http.MethodGet,
		historyURL("/alarms", "18800000001", from, to, 2, 2), "secret")
	require.Equal(t, http.StatusOK, recorder.Code)

	var response alarmHistoryResponse
	require.NoError(t, decodeJSON(recorder, &response))
	require.Equal(t, 3, response.Total)
	require.Equal(t, 2, response.Page)
	require.Equal(t, 2, response.PageSize)
	require.Len(t, response.Items, 1)
	require.Equal(t, uint16(1), response.Items[0].MessageSerial)
}

func TestListLocationsUsesRepositoryQuery(t *testing.T) {
	from := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	stub := &stubLocationRepository{
		items: []repository.LocationRecord{{
			Phone: "18800000001", MsgID: 0x0200, OccurredAt: "261108000000",
			ReceivedAt: from.Add(time.Minute),
		}},
		total: 3,
	}
	server := NewServer(testOptions("secret", 100, 200), Dependencies{Locations: stub})

	recorder := doRequest(t, server.Handler(), http.MethodGet,
		historyURL("/locations", "18800000001", from, to, 2, 2), "secret")
	require.Equal(t, http.StatusOK, recorder.Code)

	var response locationHistoryResponse
	require.NoError(t, decodeJSON(recorder, &response))
	require.Equal(t, 3, response.Total)
	require.Equal(t, 2, response.Page)
	require.Equal(t, 2, response.PageSize)
	require.Len(t, response.Items, 1)

	require.Len(t, stub.queries, 1)
	require.Equal(t, "18800000001", stub.queries[0].Phone)
	require.Equal(t, from, stub.queries[0].From)
	require.Equal(t, to, stub.queries[0].To)
	require.Equal(t, 2, stub.queries[0].Offset)
	require.Equal(t, 2, stub.queries[0].Limit)
}

func TestHistoryRequiresPhoneAndTimeRange(t *testing.T) {
	server := NewServer(testOptions("secret", 100, 200), Dependencies{
		Locations: &stubLocationRepository{},
		Alarms:    repository.NewAlarmRepository(repository.AlarmRetention{}),
	})
	from := time.Now().Add(-time.Hour)
	to := time.Now()

	cases := []string{
		historyURL("/alarms", "", from, to, 0, 0),
		historyURL("/alarms", "18800000001", time.Time{}, to, 0, 0),
		historyURL("/alarms", "18800000001", from, time.Time{}, 0, 0),
		historyURL("/alarms", "18800000001", to, from, 0, 0),
		"/alarms?phone=18800000001&from=not-a-time&to=" + url.QueryEscape(to.Format(time.RFC3339)),
		historyURL("/alarms", "18800000001", from, to, 0, 101),
	}
	for _, path := range cases {
		recorder := doRequest(t, server.Handler(), http.MethodGet, path, "secret")
		require.Equal(t, http.StatusBadRequest, recorder.Code, path)
		require.Equal(t, codeBadRequest, decodeError(t, recorder).Code, path)
	}
}

func TestListLocationsUnavailableWithoutSQLBackend(t *testing.T) {
	server := NewServer(testOptions("secret", 100, 200), Dependencies{})
	from := time.Now().Add(-time.Hour)
	to := time.Now()
	recorder := doRequest(t, server.Handler(), http.MethodGet,
		historyURL("/locations", "18800000001", from, to, 0, 0), "secret")
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Equal(t, codeUnavailable, decodeError(t, recorder).Code)
}
