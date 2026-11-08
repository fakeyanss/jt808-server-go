package adminapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// 分页默认值与硬上限。上限用于防止一次拉取全表，调用方必须显式翻页。
const (
	defaultPageSize = 20
	maxPageSize     = 100
)

type pageParams struct {
	Page     int
	PageSize int
}

// parsePage 解析 page / pageSize；缺失时使用默认值，非法或超限返回 400。
func parsePage(c *gin.Context) (pageParams, bool) {
	params := pageParams{Page: 1, PageSize: defaultPageSize}
	if raw := c.Query("page"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 {
			writeJSONError(c, http.StatusBadRequest, codeBadRequest, "page must be a positive integer")
			return params, false
		}
		params.Page = value
	}
	if raw := c.Query("pageSize"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 {
			writeJSONError(c, http.StatusBadRequest, codeBadRequest, "pageSize must be a positive integer")
			return params, false
		}
		if value > maxPageSize {
			writeJSONError(c, http.StatusBadRequest, codeBadRequest, "pageSize must not exceed 100")
			return params, false
		}
		params.PageSize = value
	}
	return params, true
}

// sliceBounds 计算 [start, end) 切片边界；页码超出范围时返回空区间。
func sliceBounds(total int, params pageParams) (int, int) {
	start := (params.Page - 1) * params.PageSize
	if start > total {
		start = total
	}
	end := start + params.PageSize
	if end > total {
		end = total
	}
	return start, end
}
