package uplink

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/medianode"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// SetMediaNodeRepository 挂载媒体节点注册接口。
//
// 设置后暴露 media 节点出站注册/心跳入口；control 用注册表而不是静态清单
// 作为 0x9101/0x9201 的分配依据。
func (s *Server) SetMediaNodeRepository(nodes repository.MediaNodeRepository) {
	if s != nil {
		s.mediaNodes = nodes
	}
}

// handleMediaNodeRegister 登记媒体节点地址、容量与初始状态。
func (s *Server) handleMediaNodeRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing bearer token")
		return
	}
	if s.mediaNodes == nil {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "media node registry is not configured")
		return
	}
	var req medianode.RegisterRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(req.NodeID) == "" || strings.TrimSpace(req.InstanceID) == "" ||
		strings.TrimSpace(req.InternalURL) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "nodeId, instanceId and internalUrl are required")
		return
	}
	if req.EndpointCapacity < 0 || req.MaxBitrateKbps < 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "endpointCapacity and maxBitrateKbps must be >= 0")
		return
	}
	node := &repository.MediaNode{
		NodeID:           req.NodeID,
		InstanceID:       req.InstanceID,
		InternalURL:      req.InternalURL,
		PushIP:           req.PushIP,
		PushTCPPort:      req.PushTCPPort,
		PushUDPPort:      req.PushUDPPort,
		PlaybackBaseURL:  req.PlaybackBaseURL,
		EndpointCapacity: req.EndpointCapacity,
		MaxBitrateKbps:   req.MaxBitrateKbps,
	}
	if err := s.mediaNodes.Register(r.Context(), node, s.clock.Now()); err != nil {
		log.Error().Err(err).Str("node", req.NodeID).Msg("Media node register failed")
		writeError(w, http.StatusInternalServerError, "register_failed", "failed to register media node")
		return
	}
	writeJSON(w, http.StatusOK, medianode.StatusResponse{Status: "online"})
}

// handleMediaNodeHeartbeat 续期媒体节点并接收小指标负载；实例被顶替时返回 409。
func (s *Server) handleMediaNodeHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing bearer token")
		return
	}
	if s.mediaNodes == nil {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "media node registry is not configured")
		return
	}
	var req medianode.HeartbeatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(req.NodeID) == "" || strings.TrimSpace(req.InstanceID) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "nodeId and instanceId are required")
		return
	}
	load := repository.MediaNodeLoad{
		ActiveChannels:     req.Load.ActiveChannels,
		ActiveTCPConns:     req.Load.ActiveTCPConns,
		InboundBitrateKbps: req.Load.InboundBitrateKbps,
		CPUPercent:         req.Load.CPUPercent,
		MemoryBytes:        req.Load.MemoryBytes,
	}
	ok, err := s.mediaNodes.Heartbeat(r.Context(), req.NodeID, req.InstanceID, load, s.clock.Now())
	if err != nil {
		log.Error().Err(err).Str("node", req.NodeID).Msg("Media node heartbeat failed")
		writeError(w, http.StatusInternalServerError, "heartbeat_failed", "failed to update media node")
		return
	}
	if !ok {
		writeJSON(w, http.StatusConflict, medianode.StatusResponse{
			Status: "fenced", Code: "instance_stale", Message: "media instance is no longer current",
		})
		return
	}
	writeJSON(w, http.StatusOK, medianode.StatusResponse{Status: "online"})
}

// handleMediaNodeDeregister 把媒体节点标记为离线。
func (s *Server) handleMediaNodeDeregister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing bearer token")
		return
	}
	if s.mediaNodes == nil {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "media node registry is not configured")
		return
	}
	var req medianode.HeartbeatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(req.NodeID) == "" || strings.TrimSpace(req.InstanceID) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "nodeId and instanceId are required")
		return
	}
	if err := s.mediaNodes.Deregister(r.Context(), req.NodeID, req.InstanceID, s.clock.Now()); err != nil {
		log.Error().Err(err).Str("node", req.NodeID).Msg("Media node deregister failed")
		writeError(w, http.StatusInternalServerError, "deregister_failed", "failed to deregister media node")
		return
	}
	writeJSON(w, http.StatusOK, medianode.StatusResponse{Status: "offline"})
}
