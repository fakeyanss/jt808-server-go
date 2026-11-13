package uplink

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/attachmentnode"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// FTPSessionStore 是 control 侧的 FTP 任务鉴权能力。
//
// 附件节点不连库：登录校验必须由 control 完成，节点只出站询问。
type FTPSessionStore interface {
	Authenticate(ctx context.Context, nodeID, username, password string) (*repository.AttachmentFTPTask, error)
	MaxFileSize() int64
}

// SetAttachmentFTPSessionStore 挂载 FTP 登录校验入口。
func (s *Server) SetAttachmentFTPSessionStore(store FTPSessionStore) {
	if s != nil {
		s.attachmentFTP = store
	}
}

// SetAttachmentNodeRepository 挂载附件节点注册接口。
func (s *Server) SetAttachmentNodeRepository(nodes repository.AttachmentNodeRepository) {
	if s != nil {
		s.attachmentNodes = nodes
	}
}

// handleAttachmentFTPLogin 校验任务级一次性账号并返回上传配置。
func (s *Server) handleAttachmentFTPLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing bearer token")
		return
	}
	s.mu.RLock()
	store := s.attachmentFTP
	s.mu.RUnlock()
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "attachment ftp session store is not configured")
		return
	}
	var req attachmentnode.FTPLoginRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(req.NodeID) == "" || strings.TrimSpace(req.Username) == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "nodeId, username and password are required")
		return
	}
	task, err := store.Authenticate(r.Context(), req.NodeID, req.Username, req.Password)
	if err != nil {
		if errors.Is(err, contracts.ErrFTPLoginRejected) {
			writeError(w, http.StatusUnauthorized, "login_rejected", "ftp credentials are invalid or revoked")
			return
		}
		log.Warn().Err(err).Str("node", req.NodeID).Msg("Attachment ftp login check failed")
		writeError(w, http.StatusServiceUnavailable, "auth_unavailable", "control cannot validate ftp credentials right now")
		return
	}
	writeJSON(w, http.StatusOK, attachmentnode.FTPLoginResponse{
		Phone: task.Phone, Serial: task.Serial, UploadPath: task.UploadPath, MaxBytes: store.MaxFileSize(),
	})
}

// handleAttachmentNodeRegister 登记附件节点可达地址。
func (s *Server) handleAttachmentNodeRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing bearer token")
		return
	}
	if s.attachmentNodes == nil {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "attachment node registry is not configured")
		return
	}
	var req attachmentnode.RegisterRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(req.NodeID) == "" || strings.TrimSpace(req.InstanceID) == "" || strings.TrimSpace(req.BaseURL) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "nodeId, instanceId and baseUrl are required")
		return
	}
	if err := s.attachmentNodes.Register(r.Context(), req.NodeID, req.InstanceID, req.BaseURL, s.clock.Now()); err != nil {
		log.Error().Err(err).Str("node", req.NodeID).Msg("Attachment node register failed")
		writeError(w, http.StatusInternalServerError, "register_failed", "failed to register attachment node")
		return
	}
	writeJSON(w, http.StatusOK, attachmentnode.StatusResponse{Status: "online"})
}

// handleAttachmentNodeHeartbeat 续期附件节点；实例被顶替时返回 409。
func (s *Server) handleAttachmentNodeHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing bearer token")
		return
	}
	if s.attachmentNodes == nil {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "attachment node registry is not configured")
		return
	}
	var req attachmentnode.HeartbeatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(req.NodeID) == "" || strings.TrimSpace(req.InstanceID) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "nodeId and instanceId are required")
		return
	}
	ok, err := s.attachmentNodes.Heartbeat(r.Context(), req.NodeID, req.InstanceID, s.clock.Now())
	if err != nil {
		log.Error().Err(err).Str("node", req.NodeID).Msg("Attachment node heartbeat failed")
		writeError(w, http.StatusInternalServerError, "heartbeat_failed", "failed to update attachment node")
		return
	}
	if !ok {
		writeJSON(w, http.StatusConflict, attachmentnode.StatusResponse{
			Status: "fenced", Code: "instance_stale", Message: "attachment instance is no longer current",
		})
		return
	}
	writeJSON(w, http.StatusOK, attachmentnode.StatusResponse{Status: "online"})
}

// handleAttachmentNodeDeregister 把附件节点标记为离线。
func (s *Server) handleAttachmentNodeDeregister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing bearer token")
		return
	}
	if s.attachmentNodes == nil {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "attachment node registry is not configured")
		return
	}
	var req attachmentnode.HeartbeatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(req.NodeID) == "" || strings.TrimSpace(req.InstanceID) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "nodeId and instanceId are required")
		return
	}
	if err := s.attachmentNodes.Deregister(r.Context(), req.NodeID, req.InstanceID, s.clock.Now()); err != nil {
		log.Error().Err(err).Str("node", req.NodeID).Msg("Attachment node deregister failed")
		writeError(w, http.StatusInternalServerError, "deregister_failed", "failed to deregister attachment node")
		return
	}
	writeJSON(w, http.StatusOK, attachmentnode.StatusResponse{Status: "offline"})
}
