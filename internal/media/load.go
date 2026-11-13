package media

import (
	"runtime"
	"strings"

	"github.com/fakeyanss/jt808-server-go/internal/medianode"
)

// Load 采样本节点当前负载，供心跳上报 control 作为分配依据。
//
// 口径：
//   - 活跃通道数 = 当前 (phone, channel) 会话数；
//   - 入流码率取两次采样之间的字节增量折算 kbps；
//   - CPU 用进程累计 CPU 时间差分；平台取不到时该字段为 nil（少报，不失败）；
//   - 内存取 runtime.MemStats.Sys。
//
// 这里只产出心跳需要的小指标；每路流、每个端口的明细不得塞进心跳。
func (s *Server) Load() medianode.Load {
	if s == nil {
		return medianode.Load{}
	}
	now := s.cfg.Clock.Now()
	load := medianode.Load{
		ActiveChannels: int64(s.Streams()),
		ActiveTCPConns: int64(s.activeTCPConns()),
	}

	bytes := s.ingressBytes.Load()
	cpu, cpuOK := readProcessCPU()

	s.loadMu.Lock()
	if !s.lastIngressAt.IsZero() {
		if elapsed := now.Sub(s.lastIngressAt); elapsed > 0 {
			load.InboundBitrateKbps = int64(float64(bytes-s.lastIngress) * 8 / elapsed.Seconds() / 1000)
			if load.InboundBitrateKbps < 0 {
				load.InboundBitrateKbps = 0
			}
		}
	}
	s.lastIngress = bytes
	s.lastIngressAt = now
	if cpuOK {
		if !s.lastCPUAt.IsZero() {
			if elapsed := now.Sub(s.lastCPUAt); elapsed > 0 {
				percent := float64(cpu-s.lastCPU) / float64(elapsed) * 100
				if percent < 0 {
					percent = 0
				}
				load.CPUPercent = &percent
			}
		}
		s.lastCPU = cpu
		s.lastCPUAt = now
	}
	s.loadMu.Unlock()

	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	memory := stats.Sys
	load.MemoryBytes = &memory
	return load
}

// PushIP 返回 0x9101/0x9201 信令里通告给终端的推流地址。
func (s *Server) PushIP() string {
	if s == nil {
		return ""
	}
	if ip := strings.TrimSpace(s.cfg.AdvertiseIP); ip != "" {
		return ip
	}
	return "127.0.0.1"
}

// PushPorts 返回 1078 实际监听端口。单端口模式下 TCP 与 UDP 相同。
func (s *Server) PushPorts() (tcp, udp uint16) {
	if s == nil {
		return 0, 0
	}
	return s.tcpPort, s.udpPort
}

// PlaybackBaseURL 返回播放出口基址；未启用 flv 时为空。
func (s *Server) PlaybackBaseURL() string {
	if s == nil {
		return ""
	}
	return s.cfg.Playback.FLV.BaseURL
}

// activeTCPConns 返回当前 1078 TCP 连接数。
func (s *Server) activeTCPConns() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}
