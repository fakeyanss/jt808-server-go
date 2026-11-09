package api

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/fakeyanss/jt808-server-go/internal/mediaupload"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// 媒体文件查询的默认与最大条数，避免单次请求拖垮进程。
const (
	defaultMediaAPILimit = 100
	maxMediaAPILimit     = 500
)

// mediaTimeLayout 与业务 API 其它时间参数保持一致。
const mediaTimeLayout = "2006-01-02 15:04:05"

// registerMediaRoutes 注册 0x0801 媒体文件与 0x0805 拍摄记录的查询/下载接口。
//
// 鉴权沿用业务 API 的 token 中间件；这里只做参数校验与仓储/文件读取。
// 注意：0x0801 位置体只有报警标志位、没有报警流水号，
// 列表的 alarmOnly 只能按报警类别过滤，无法按某一条报警精确关联。
func registerMediaRoutes(api *gin.RouterGroup, devices repository.DeviceRepository, files repository.MediaFileRepository, captures repository.MediaCaptureRepository, uploads *mediaupload.Store) {
	group := api.Group("/device/:phone/media")
	group.GET("/files", func(c *gin.Context) {
		phone, ok := requireMediaDevice(c, devices)
		if !ok {
			return
		}
		filter, err := parseMediaFileFilter(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		items, err := files.List(c.Request.Context(), phone, filter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"phone": phone, "count": len(items), "items": items})
	})

	group.GET("/files/:mediaId", func(c *gin.Context) {
		phone, ok := requireMediaDevice(c, devices)
		if !ok {
			return
		}
		mediaID, ok := parseMediaID(c)
		if !ok {
			return
		}
		file, err := files.Get(c.Request.Context(), phone, mediaID)
		if errors.Is(err, repository.ErrMediaFileNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"err": "media file not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		c.JSON(http.StatusOK, file)
	})

	group.GET("/files/:mediaId/content", func(c *gin.Context) {
		phone, ok := requireMediaDevice(c, devices)
		if !ok {
			return
		}
		mediaID, ok := parseMediaID(c)
		if !ok {
			return
		}
		file, err := files.Get(c.Request.Context(), phone, mediaID)
		if errors.Is(err, repository.ErrMediaFileNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"err": "media file not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		if uploads == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"err": "media upload store is not configured"})
			return
		}
		handle, err := uploads.Open(file.RelativePath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				c.JSON(http.StatusNotFound, gin.H{"err": "media file content missing on disk"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		defer handle.Close()
		info, err := handle.Stat()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		name := filepath.Base(file.RelativePath)
		contentType := mime.TypeByExtension(filepath.Ext(name))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		c.Header("Content-Type", contentType)
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		// ServeContent 负责 Range、If-Modified-Since 与 Content-Length。
		http.ServeContent(c.Writer, c.Request, name, info.ModTime(), handle)
	})

	group.GET("/captures", func(c *gin.Context) {
		phone, ok := requireMediaDevice(c, devices)
		if !ok {
			return
		}
		limit, err := parseMediaLimit(c.Query("limit"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		items, err := captures.ListCaptures(c.Request.Context(), phone, limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"phone": phone, "count": len(items), "items": items})
	})
}

// requireMediaDevice 校验设备已注册，并返回路径中的手机号。
func requireMediaDevice(c *gin.Context, devices repository.DeviceRepository) (string, bool) {
	phone := c.Param("phone")
	if phone == "" {
		c.JSON(http.StatusBadRequest, gin.H{"err": "phone is required"})
		return "", false
	}
	if _, err := devices.GetByPhone(c.Request.Context(), phone); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return "", false
	}
	return phone, true
}

// parseMediaID 解析路径中的媒体 ID。
func parseMediaID(c *gin.Context) (uint32, bool) {
	raw := c.Param("mediaId")
	mediaID, err := strconv.ParseUint(raw, 10, 32)
	if err != nil || mediaID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"err": "mediaId must be a positive 32-bit integer"})
		return 0, false
	}
	return uint32(mediaID), true
}

// parseMediaFileFilter 解析媒体文件列表的查询参数。
func parseMediaFileFilter(c *gin.Context) (repository.MediaFileFilter, error) {
	filter := repository.MediaFileFilter{}
	if raw := strings.TrimSpace(c.Query("channel")); raw != "" {
		value, err := parseUint8Query("channel", raw)
		if err != nil {
			return filter, err
		}
		filter.ChannelID = &value
	}
	if raw := strings.TrimSpace(c.Query("type")); raw != "" {
		value, err := parseUint8Query("type", raw)
		if err != nil {
			return filter, err
		}
		filter.MediaType = &value
	}
	start, err := parseMediaTime("start", c.Query("start"))
	if err != nil {
		return filter, err
	}
	filter.Start = start
	end, err := parseMediaTime("end", c.Query("end"))
	if err != nil {
		return filter, err
	}
	filter.End = end
	if filter.Start != nil && filter.End != nil && filter.End.Before(*filter.Start) {
		return filter, fmt.Errorf("end must not be earlier than start")
	}
	if raw := strings.TrimSpace(c.Query("alarmOnly")); raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			return filter, fmt.Errorf("alarmOnly must be true or false, got %q", raw)
		}
		filter.AlarmOnly = enabled
	}
	limit, err := parseMediaLimit(c.Query("limit"))
	if err != nil {
		return filter, err
	}
	filter.Limit = limit
	return filter, nil
}

// parseMediaTime 解析可选的时间参数，空字符串表示不限制。
func parseMediaTime(name, raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	// 与全项目统一口径：协议 BCD 时间固定为 GMT+8，不随服务器时区变化。
	value, err := time.ParseInLocation(mediaTimeLayout, raw, hex.ProtocolTimeZone)
	if err != nil {
		return nil, fmt.Errorf("%s must use format %q, got %q", name, mediaTimeLayout, raw)
	}
	return &value, nil
}

// parseMediaLimit 解析条数上限，缺省使用默认值，超过上限按上限截断。
func parseMediaLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultMediaAPILimit, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, fmt.Errorf("limit must be a positive integer, got %q", raw)
	}
	if value > maxMediaAPILimit {
		return maxMediaAPILimit, nil
	}
	return value, nil
}

// parseUint8Query 解析 0-255 的查询参数。
func parseUint8Query(name, raw string) (uint8, error) {
	value, err := strconv.ParseUint(raw, 10, 8)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer between 0 and 255, got %q", name, raw)
	}
	return uint8(value), nil
}
