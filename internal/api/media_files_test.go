package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/fakeyanss/jt808-server-go/internal/mediaupload"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// apiGeoBody 构造 0x0801 的 28 字节位置体。
func apiGeoBody(alarmSign uint32, bcdTime string) []byte {
	body := hex.WriteDoubleWord(nil, alarmSign)
	body = hex.WriteDoubleWord(body, 0x00000001)
	body = hex.WriteDoubleWord(body, 30000000)
	body = hex.WriteDoubleWord(body, 120000000)
	body = hex.WriteWord(body, 100)
	body = hex.WriteWord(body, 600)
	body = hex.WriteWord(body, 90)
	return hex.WriteBCD(body, bcdTime)
}

// newMediaAPITestServer 组装内存仓储、临时媒体目录与只注册媒体路由的 gin 引擎。
func newMediaAPITestServer(t *testing.T) (*gin.Engine, repository.MediaFileRepository, *mediaupload.Store, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	const phone = "188765432113"
	devices := repository.NewDeviceRepository()
	require.NoError(t, devices.Upsert(context.Background(), &model.Device{Phone: phone, Plate: "京A12345"}))

	files := repository.NewMediaFileRepository()
	store, err := mediaupload.NewStore(t.TempDir())
	require.NoError(t, err)

	searches := repository.NewMediaSearchRepository()
	require.NoError(t, searches.Replace(context.Background(), phone, 41, []repository.MediaSearchRecord{{
		MediaID: 8001, MediaType: 0, ChannelID: 1, EventID: 1,
		Latitude: 30000000, Longitude: 120000000,
		LocationTime: time.Date(2026, 11, 12, 12, 0, 0, 0, hex.ProtocolTimeZone),
		ReceivedAt:   time.Date(2026, 11, 12, 12, 0, 1, 0, hex.ProtocolTimeZone),
	}}))

	router := gin.New()
	registerMediaRoutes(router.Group("/api"), devices, files, files, searches, store)

	saveMedia := func(mediaID uint32, channel uint8, alarmSign uint32, bcdTime string, data []byte) {
		file, saveErr := store.Save(&model.Msg0801{
			Header:              &model.MsgHeader{MsgID: 0x0801, PhoneNumber: phone, Attr: &model.MsgBodyAttr{}},
			MultiMediaID:        mediaID,
			MultiMediaType:      0,
			MultiMediaContainer: 0,
			EventID:             1,
			LogicChannelID:      channel,
			GeoAlarmBody:        apiGeoBody(alarmSign, bcdTime),
			FragmentData:        data,
		})
		require.NoError(t, saveErr)
		require.NoError(t, files.Save(context.Background(), file))
	}
	saveMedia(8001, 1, 0x00000002, "261112120000", []byte("jpeg-content"))
	saveMedia(8002, 2, 0, "261112130000", []byte("mp3-content"))

	require.NoError(t, files.SaveCapture(context.Background(), &repository.MediaCapture{
		Phone: phone, RecordID: "41", AnswerSerial: 41, Result: 0,
		ReceivedAt: time.Date(2026, 11, 12, 12, 0, 0, 0, time.Local),
		Items: []repository.MediaCaptureItem{
			{MediaID: 8001}, {MediaID: 8002}, {MediaID: 8003},
		},
	}))
	return router, files, store, phone
}

func TestMediaFileListFiltersAndOrder(t *testing.T) {
	router, _, _, phone := newMediaAPITestServer(t)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/device/"+phone+"/media/files?channel=2", nil))
	require.Equal(t, http.StatusOK, recorder.Code)

	var payload struct {
		Count int                    `json:"count"`
		Items []repository.MediaFile `json:"items"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, 1, payload.Count)
	require.Equal(t, uint32(8002), payload.Items[0].MediaID)
	require.Contains(t, recorder.Body.String(), `"alarmSign"`, "接口需显式暴露报警标志位")

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/device/"+phone+"/media/files?alarmOnly=true", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, 1, payload.Count)
	require.Equal(t, uint32(8001), payload.Items[0].MediaID)

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/device/"+phone+"/media/files?channel=abc", nil))
	require.Equal(t, http.StatusBadRequest, recorder.Code)

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/device/"+phone+"/media/files?start=2026-11-12%2012:30:00", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, 1, payload.Count)
	require.Equal(t, uint32(8002), payload.Items[0].MediaID)
}

func TestMediaFileDetailAndContentWithRange(t *testing.T) {
	router, _, _, phone := newMediaAPITestServer(t)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/device/"+phone+"/media/files/8001", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"mediaId":8001`)

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/device/"+phone+"/media/files/9999", nil))
	require.Equal(t, http.StatusNotFound, recorder.Code)

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/device/"+phone+"/media/files/not-a-number", nil))
	require.Equal(t, http.StatusBadRequest, recorder.Code)

	request := httptest.NewRequest(http.MethodGet, "/api/device/"+phone+"/media/files/8001/content", nil)
	request.Header.Set("Range", "bytes=0-3")
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusPartialContent, recorder.Code)
	require.Equal(t, "jpeg", recorder.Body.String())
	require.Equal(t, "image/jpeg", recorder.Header().Get("Content-Type"))

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/device/unknown/media/files", nil))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

// 查询侧时间参数固定按 GMT+8 解释，不能随服务器时区漂移。
func TestParseMediaTimeUsesFixedGMT8(t *testing.T) {
	original := time.Local
	t.Cleanup(func() { time.Local = original })

	expected := time.Date(2026, 11, 12, 10, 30, 0, 0, hex.ProtocolTimeZone)
	for _, location := range []*time.Location{
		time.UTC,
		time.FixedZone("UTC-5", -5*60*60),
		time.FixedZone("UTC+13", 13*60*60),
	} {
		time.Local = location
		got, err := parseMediaTime("start", "2026-11-12 10:30:00")
		require.NoError(t, err)
		require.NotNil(t, got)
		require.True(t, expected.Equal(*got), "time.Local=%s parsed %s, want %s", location, got, expected)
	}
}

// 0x0805 拍摄记录需要展示每个 mediaId 是否已有对应 0x0801 文件。
func TestMediaCapturesExposeArrivalState(t *testing.T) {
	router, _, _, phone := newMediaAPITestServer(t)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/device/"+phone+"/media/captures", nil))
	require.Equal(t, http.StatusOK, recorder.Code)

	var payload struct {
		Count int                       `json:"count"`
		Items []repository.MediaCapture `json:"items"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, 1, payload.Count)
	require.Len(t, payload.Items[0].Items, 3)
	require.True(t, payload.Items[0].Items[0].Arrived)
	require.True(t, payload.Items[0].Items[1].Arrived)
	require.False(t, payload.Items[0].Items[2].Arrived)
}

// 0x0802 检索结果查询入口返回最后一次应答的记录。
func TestMediaSearchResultsEndpoint(t *testing.T) {
	router, _, _, phone := newMediaAPITestServer(t)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/device/"+phone+"/media/search-results", nil))
	require.Equal(t, http.StatusOK, recorder.Code)

	var payload struct {
		Count int                            `json:"count"`
		Items []repository.MediaSearchRecord `json:"items"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, 1, payload.Count)
	require.Len(t, payload.Items, 1)
	require.Equal(t, uint16(41), payload.Items[0].AnswerSerial)
	require.Equal(t, uint32(8001), payload.Items[0].MediaID)
	require.Equal(t, uint32(30000000), payload.Items[0].Latitude)

	// 未注册设备与非法 limit 均返回 400。
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/device/188000000000/media/search-results", nil))
	require.Equal(t, http.StatusBadRequest, recorder.Code)

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/device/"+phone+"/media/search-results?limit=abc", nil))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}
