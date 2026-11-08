package adminapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
)

// sessionView 是管理面的会话展示模型。
// 运行时状态只存在于当前实例，因此这里展示的是本节点视角的在线会话。
type sessionView struct {
	Phone           string `json:"phone"`
	SessionID       string `json:"sessionId"`
	RemoteAddr      string `json:"remoteAddr"`
	Transport       string `json:"transport"`
	ProtocolVersion uint8  `json:"protocolVersion"`
	ConnectedAt     string `json:"connectedAt"`
	LastComTime     string `json:"lastComTime,omitempty"`
}

type sessionListResponse struct {
	Items    []sessionView `json:"items"`
	Total    int           `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"pageSize"`
}

// registerSessionRoutes 注册会话运维接口，全部位于已鉴权分组下。
func registerSessionRoutes(group *gin.RouterGroup, deps Dependencies) {
	group.GET("/sessions", func(c *gin.Context) {
		listSessions(c, deps)
	})
	group.GET("/sessions/:phone", func(c *gin.Context) {
		getSession(c, deps)
	})
	group.DELETE("/sessions/:phone", func(c *gin.Context) {
		kickSession(c, deps)
	})
}

func listSessions(c *gin.Context, deps Dependencies) {
	params, ok := parsePage(c)
	if !ok {
		return
	}
	if deps.Conn == nil {
		writeJSONError(c, http.StatusServiceUnavailable, codeUnavailable, "session registry is unavailable")
		return
	}
	bindings := deps.Conn.ListBindings(c.Request.Context())
	start, end := sliceBounds(len(bindings), params)
	items := make([]sessionView, 0, end-start)
	for _, binding := range bindings[start:end] {
		items = append(items, buildSessionView(c.Request.Context(), deps, binding))
	}
	writeJSONOK(c, sessionListResponse{Items: items, Total: len(bindings), Page: params.Page, PageSize: params.PageSize})
}

func getSession(c *gin.Context, deps Dependencies) {
	if deps.Conn == nil {
		writeJSONError(c, http.StatusServiceUnavailable, codeUnavailable, "session registry is unavailable")
		return
	}
	phone := c.Param("phone")
	session, err := deps.Conn.GetByPhone(c.Request.Context(), phone)
	if err != nil {
		writeSessionLookupError(c, phone, err)
		return
	}
	writeJSONOK(c, buildSessionView(c.Request.Context(), deps, runtime.SessionBinding{Phone: phone, Session: session}))
}

// kickSession 主动断开指定手机号的会话并清理注册表索引。
// 重复踢或手机号不存在都返回 404；踢线只影响当前实例。
func kickSession(c *gin.Context, deps Dependencies) {
	if deps.Conn == nil {
		writeJSONError(c, http.StatusServiceUnavailable, codeUnavailable, "session registry is unavailable")
		return
	}
	phone := c.Param("phone")
	session, err := deps.Conn.GetByPhone(c.Request.Context(), phone)
	if err != nil {
		writeSessionLookupError(c, phone, err)
		return
	}

	// 先关连接与等待项，再清理索引；Unbind 只在手机号仍指向本会话时删除，
	// 因此并发顶替发生时不会误删新会话的索引。
	session.ClosePending()
	if session.Conn != nil {
		_ = session.Conn.Close()
	}
	deps.Conn.Unbind(c.Request.Context(), session.ID)
	log.Info().Str("phone", phone).Str("session", session.ID).Msg("Session kicked by admin API")

	c.JSON(http.StatusOK, gin.H{
		"phone":        phone,
		"sessionId":    session.ID,
		"disconnected": true,
	})
}

func writeSessionLookupError(c *gin.Context, phone string, err error) {
	if errors.Is(err, runtime.ErrPhoneNotBound) || errors.Is(err, runtime.ErrSessionClosed) {
		writeJSONError(c, http.StatusNotFound, codeNotFound, "no active session for phone "+phone)
		return
	}
	log.Error().Err(err).Str("phone", phone).Msg("Lookup session by phone")
	writeJSONError(c, http.StatusInternalServerError, codeInternal, "internal server error")
}

// buildSessionView 组装单个会话视图。
// 最后通信时间取持久化最后交互时间与进程内心跳的较新值；两者缺失时留空。
func buildSessionView(ctx context.Context, deps Dependencies, binding runtime.SessionBinding) sessionView {
	session := binding.Session
	view := sessionView{
		Phone:      binding.Phone,
		SessionID:  session.ID,
		RemoteAddr: session.ID,
		Transport:  string(session.GetTransProto()),
	}
	if !session.ConnectedAt.IsZero() {
		view.ConnectedAt = session.ConnectedAt.Format(time.RFC3339)
	}
	last := lastCommunicationTime(ctx, deps, binding.Phone)
	if !last.IsZero() {
		view.LastComTime = last.Format(time.RFC3339)
	}
	if deps.Devices != nil {
		if device, err := deps.Devices.GetByPhone(ctx, binding.Phone); err == nil {
			view.ProtocolVersion = device.ProtocolVersion
		} else if !errors.Is(err, repository.ErrDeviceNotFound) {
			log.Warn().Err(err).Str("phone", binding.Phone).Msg("Load device for admin session view")
		}
	}
	return view
}

func lastCommunicationTime(ctx context.Context, deps Dependencies, phone string) time.Time {
	var last time.Time
	if deps.Devices != nil {
		if device, err := deps.Devices.GetByPhone(ctx, phone); err == nil {
			last = device.LastestComTime
		}
	}
	if deps.Heartbeat != nil {
		if at, ok := deps.Heartbeat.Last(ctx, phone); ok && at.After(last) {
			last = at
		}
	}
	return last
}
