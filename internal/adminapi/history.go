package adminapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// 历史查询要求显式提供 phone、from、to：
// 强制带时间范围可避免管理面退化为按手机号拉取全表。
const historyTimeLayout = time.RFC3339

type locationHistoryResponse struct {
	Items    []repository.LocationRecord `json:"items"`
	Total    int                         `json:"total"`
	Page     int                         `json:"page"`
	PageSize int                         `json:"pageSize"`
}

type alarmHistoryResponse struct {
	Items    []repository.AlarmEvent `json:"items"`
	Total    int                     `json:"total"`
	Page     int                     `json:"page"`
	PageSize int                     `json:"pageSize"`
}

// registerHistoryRoutes 注册定位与报警历史查询接口，全部位于已鉴权分组下。
func registerHistoryRoutes(group *gin.RouterGroup, deps Dependencies) {
	group.GET("/locations", func(c *gin.Context) {
		listLocations(c, deps)
	})
	group.GET("/alarms", func(c *gin.Context) {
		listAlarms(c, deps)
	})
}

func listLocations(c *gin.Context, deps Dependencies) {
	if deps.Locations == nil {
		// memory 后端只保留热读环形缓冲，不保存定位历史。
		writeJSONError(c, http.StatusServiceUnavailable, codeUnavailable,
			"location history is not available for the current storage backend")
		return
	}
	query, ok := parseHistoryQuery(c)
	if !ok {
		return
	}
	items, total, err := deps.Locations.ListPage(c.Request.Context(), query)
	if err != nil {
		log.Error().Err(err).Str("phone", query.Phone).Msg("Query location history")
		writeJSONError(c, http.StatusInternalServerError, codeInternal, "internal server error")
		return
	}
	if items == nil {
		items = []repository.LocationRecord{}
	}
	writeJSONOK(c, locationHistoryResponse{
		Items: items, Total: total,
		Page: (query.Offset / query.Limit) + 1, PageSize: query.Limit,
	})
}

func listAlarms(c *gin.Context, deps Dependencies) {
	if deps.Alarms == nil {
		writeJSONError(c, http.StatusServiceUnavailable, codeUnavailable, "alarm history is unavailable")
		return
	}
	query, ok := parseHistoryQuery(c)
	if !ok {
		return
	}
	items, total, err := deps.Alarms.ListPage(c.Request.Context(), query)
	if err != nil {
		log.Error().Err(err).Str("phone", query.Phone).Msg("Query alarm history")
		writeJSONError(c, http.StatusInternalServerError, codeInternal, "internal server error")
		return
	}
	if items == nil {
		items = []repository.AlarmEvent{}
	}
	writeJSONOK(c, alarmHistoryResponse{
		Items: items, Total: total,
		Page: (query.Offset / query.Limit) + 1, PageSize: query.Limit,
	})
}

// parseHistoryQuery 解析 phone、from、to 与分页参数。
// from/to 使用 RFC3339（可带时区偏移），时间区间为半开区间 [from, to)。
func parseHistoryQuery(c *gin.Context) (repository.HistoryQuery, bool) {
	phone := strings.TrimSpace(c.Query("phone"))
	if phone == "" {
		writeJSONError(c, http.StatusBadRequest, codeBadRequest, "phone is required")
		return repository.HistoryQuery{}, false
	}
	fromRaw := strings.TrimSpace(c.Query("from"))
	if fromRaw == "" {
		writeJSONError(c, http.StatusBadRequest, codeBadRequest, "from is required and must be RFC3339")
		return repository.HistoryQuery{}, false
	}
	toRaw := strings.TrimSpace(c.Query("to"))
	if toRaw == "" {
		writeJSONError(c, http.StatusBadRequest, codeBadRequest, "to is required and must be RFC3339")
		return repository.HistoryQuery{}, false
	}
	from, err := time.Parse(historyTimeLayout, fromRaw)
	if err != nil {
		writeJSONError(c, http.StatusBadRequest, codeBadRequest, "from must be RFC3339")
		return repository.HistoryQuery{}, false
	}
	to, err := time.Parse(historyTimeLayout, toRaw)
	if err != nil {
		writeJSONError(c, http.StatusBadRequest, codeBadRequest, "to must be RFC3339")
		return repository.HistoryQuery{}, false
	}
	if !from.Before(to) {
		writeJSONError(c, http.StatusBadRequest, codeBadRequest, "from must be earlier than to")
		return repository.HistoryQuery{}, false
	}
	params, ok := parsePage(c)
	if !ok {
		return repository.HistoryQuery{}, false
	}
	return repository.HistoryQuery{
		Phone:  phone,
		From:   from,
		To:     to,
		Offset: (params.Page - 1) * params.PageSize,
		Limit:  params.PageSize,
	}, true
}
