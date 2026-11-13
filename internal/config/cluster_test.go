package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gatewayControlBase() *Config {
	return &Config{
		Server:        &serverConf{Mode: ServerModeCluster, AckTimeoutMS: 5000, MaxRetransmissions: 3},
		Database:      &databaseConf{Type: "mysql"},
		CommandOutbox: &commandOutboxConf{Mode: CommandOutboxModeDurable},
		Uplink:        &uplinkConf{Sink: UplinkSinkLocal},
		Cluster: &clusterConf{
			Roles:   []string{RoleGateway, RoleControl},
			Gateway: &clusterGatewayConf{Addr: "127.0.0.1:9101", Routing: GatewayRoutingFixed, URL: "http://127.0.0.1:9101", Token: "gateway-token"},
			Control: &clusterControlConf{Addr: "127.0.0.1:9102", Token: "control-token"},
		},
	}
}

func gatewayOnlyBase() *Config {
	cfg := gatewayControlBase()
	cfg.Cluster.Roles = []string{RoleGateway}
	cfg.Uplink = &uplinkConf{
		Sink: UplinkSinkHTTP,
		HTTP: &uplinkHTTPConf{URL: "http://control:9102/internal/v1/uplink", Token: "control-token"},
	}
	return cfg
}

func controlOnlyBase() *Config {
	cfg := gatewayControlBase()
	cfg.Cluster.Roles = []string{RoleControl}
	return cfg
}

// TestParseServerModeMatrix 覆盖 server.mode 与 cluster.roles 的启动矩阵。
func TestParseServerModeMatrix(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*testing.T, *Config)
		wantErr string
	}{
		{
			name: "standalone 缺省",
			mutate: func(t *testing.T, cfg *Config) {
				cfg.Server.Mode = ""
				cfg.Cluster = nil
			},
		},
		{
			name: "standalone 不允许角色",
			mutate: func(t *testing.T, cfg *Config) {
				cfg.Server.Mode = ServerModeStandalone
			},
			wantErr: "cluster.roles must be empty",
		},
		{
			name: "gateway+control 同进程 local 通过",
		},
		{
			name: "cluster 缺角色",
			mutate: func(t *testing.T, cfg *Config) {
				cfg.Cluster.Roles = nil
			},
			wantErr: "cluster.roles must not be empty",
		},
		{
			name: "未知角色",
			mutate: func(t *testing.T, cfg *Config) {
				cfg.Cluster.Roles = []string{"gateway", "worker"}
			},
			wantErr: "unknown role",
		},
		{
			name: "gateway 缺 token",
			mutate: func(t *testing.T, cfg *Config) {
				cfg.Cluster.Gateway.Token = ""
			},
			wantErr: "cluster.gateway.token is required",
		},
		{
			name: "control 缺地址",
			mutate: func(t *testing.T, cfg *Config) {
				cfg.Cluster.Control.Addr = ""
			},
			wantErr: "cluster.control.addr is required",
		},
		{
			name: "gateway-only 必须 http 上行",
			mutate: func(t *testing.T, cfg *Config) {
				*cfg = *gatewayOnlyBase()
				cfg.Uplink = &uplinkConf{Sink: UplinkSinkLocal}
			},
			wantErr: "uplink.sink must be http",
		},
		{
			name: "gateway-only http 通过",
			mutate: func(t *testing.T, cfg *Config) {
				*cfg = *gatewayOnlyBase()
			},
		},
		{
			name: "control-only 通过",
			mutate: func(t *testing.T, cfg *Config) {
				*cfg = *controlOnlyBase()
			},
		},
		{
			name: "control 缺少下行模式",
			mutate: func(t *testing.T, cfg *Config) {
				*cfg = *controlOnlyBase()
				cfg.Cluster.Gateway.Routing = ""
			},
			wantErr: "cluster.gateway.routing is required",
		},
		{
			name: "fixed 缺少 url",
			mutate: func(t *testing.T, cfg *Config) {
				*cfg = *controlOnlyBase()
				cfg.Cluster.Gateway.URL = ""
			},
			wantErr: "cluster.gateway.url is required",
		},
		{
			name: "directory 不得配置 url",
			mutate: func(t *testing.T, cfg *Config) {
				*cfg = *controlOnlyBase()
				cfg.Cluster.Gateway.Routing = GatewayRoutingDirectory
			},
			wantErr: "cluster.gateway.url must be empty",
		},
		{
			name: "directory 通过",
			mutate: func(t *testing.T, cfg *Config) {
				*cfg = *controlOnlyBase()
				cfg.Cluster.Gateway.Routing = GatewayRoutingDirectory
				cfg.Cluster.Gateway.URL = ""
			},
		},
		{
			name: "未知下行模式",
			mutate: func(t *testing.T, cfg *Config) {
				*cfg = *controlOnlyBase()
				cfg.Cluster.Gateway.Routing = "roundrobin"
				cfg.Cluster.Gateway.URL = ""
			},
			wantErr: "cluster.gateway.routing must be",
		},
		{
			name: "uplink token 必须等于 control token",
			mutate: func(t *testing.T, cfg *Config) {
				*cfg = *gatewayOnlyBase()
				cfg.Uplink.HTTP.Token = "other"
			},
			wantErr: "uplink.http.token must equal cluster.control.token",
		},
		{
			name: "同进程 http 指向自身视为回环",
			mutate: func(t *testing.T, cfg *Config) {
				cfg.Uplink = &uplinkConf{
					Sink: UplinkSinkHTTP,
					HTTP: &uplinkHTTPConf{URL: "http://127.0.0.1:9102/internal/v1/uplink", Token: "control-token"},
				}
			},
			wantErr: "must not point to the local control addr",
		},
		{
			name: "gateway/control 必须 mysql",
			mutate: func(t *testing.T, cfg *Config) {
				cfg.Database.Type = "memory"
			},
			wantErr: "database.type must be mysql",
		},
		{
			name: "control 必须 durable outbox",
			mutate: func(t *testing.T, cfg *Config) {
				cfg.CommandOutbox.Mode = CommandOutboxModeOff
			},
			wantErr: "commandOutbox.mode must be durable",
		},
		{
			name: "下行超时不得超过重传窗口",
			mutate: func(t *testing.T, cfg *Config) {
				cfg.Cluster.Downlink = &clusterDownlinkConf{TimeoutMS: 30000}
			},
			wantErr: "must not exceed server.ackTimeoutMs",
		},
		{
			name: "media 使用 local 拒绝",
			mutate: func(t *testing.T, cfg *Config) {
				*cfg = *gatewayOnlyBase()
				cfg.Cluster.Roles = []string{RoleGateway, RoleMedia}
				cfg.Cluster.Storage = &clusterStorageConf{Backend: BlobBackendLocal}
				cfg.MediaServer = &mediaServerConf{Port: "7100"}
			},
			wantErr: "cluster.storage.backend must be sharedVolume or objectStore",
		},
		{
			name: "objectStore 在 E1 拒绝",
			mutate: func(t *testing.T, cfg *Config) {
				*cfg = *gatewayOnlyBase()
				cfg.Cluster.Roles = []string{RoleGateway, RoleMedia}
				cfg.Cluster.Storage = &clusterStorageConf{Backend: BlobBackendObjectStore}
				cfg.MediaServer = &mediaServerConf{Port: "7100"}
			},
			wantErr: "cluster.storage.objectStore.endpoint is required",
		},
		{
			name: "sharedVolume 缺标记拒绝",
			mutate: func(t *testing.T, cfg *Config) {
				*cfg = *gatewayOnlyBase()
				cfg.Cluster.Roles = []string{RoleGateway, RoleMedia}
				cfg.Cluster.Storage = &clusterStorageConf{
					Backend:      BlobBackendSharedVolume,
					SharedVolume: true,
					Root:         t.TempDir(),
				}
				cfg.MediaServer = &mediaServerConf{Port: "7100"}
			},
			wantErr: "cluster.storage marker",
		},
		{
			name: "sharedVolume 带标记通过",
			mutate: func(t *testing.T, cfg *Config) {
				*cfg = *gatewayOnlyBase()
				cfg.Cluster.Roles = []string{RoleGateway, RoleMedia}
				root := t.TempDir()
				if err := os.WriteFile(filepath.Join(root, DefaultClusterStorageMarker), []byte{}, 0o644); err != nil {
					t.Fatalf("write marker: %v", err)
				}
				cfg.Cluster.Storage = &clusterStorageConf{
					Backend:      BlobBackendSharedVolume,
					SharedVolume: true,
					Root:         root,
				}
				cfg.MediaServer = &mediaServerConf{Port: "7100"}
			},
		},
		{
			name: "media 缺端口拒绝",
			mutate: func(t *testing.T, cfg *Config) {
				*cfg = *gatewayOnlyBase()
				cfg.Cluster.Roles = []string{RoleGateway, RoleMedia}
				root := t.TempDir()
				if err := os.WriteFile(filepath.Join(root, DefaultClusterStorageMarker), []byte{}, 0o644); err != nil {
					t.Fatalf("write marker: %v", err)
				}
				cfg.Cluster.Storage = &clusterStorageConf{
					Backend:      BlobBackendSharedVolume,
					SharedVolume: true,
					Root:         root,
				}
			},
			wantErr: "mediaServer.port is required",
		},
		{
			name: "control sharedVolume 根目录必须对齐 mediaUpload",
			mutate: func(t *testing.T, cfg *Config) {
				*cfg = *controlOnlyBase()
				cfg.MediaUpload = &mediaUploadConf{Dir: t.TempDir()}
				root := t.TempDir()
				if err := os.WriteFile(filepath.Join(root, DefaultClusterStorageMarker), []byte{}, 0o644); err != nil {
					t.Fatalf("write marker: %v", err)
				}
				cfg.Cluster.Storage = &clusterStorageConf{
					Backend:      BlobBackendSharedVolume,
					SharedVolume: true,
					Root:         root,
				}
			},
			wantErr: "must equal mediaUpload.dir",
		},
		{
			name: "control sharedVolume 缺标记拒绝",
			mutate: func(t *testing.T, cfg *Config) {
				*cfg = *controlOnlyBase()
				root := t.TempDir()
				cfg.MediaUpload = &mediaUploadConf{Dir: root}
				cfg.Cluster.Storage = &clusterStorageConf{
					Backend:      BlobBackendSharedVolume,
					SharedVolume: true,
					Root:         root,
				}
			},
			wantErr: "cluster.storage marker",
		},
		{
			name: "control sharedVolume 对齐且带标记通过",
			mutate: func(t *testing.T, cfg *Config) {
				*cfg = *controlOnlyBase()
				root := t.TempDir()
				if err := os.WriteFile(filepath.Join(root, DefaultClusterStorageMarker), []byte{}, 0o644); err != nil {
					t.Fatalf("write marker: %v", err)
				}
				cfg.MediaUpload = &mediaUploadConf{Dir: root}
				cfg.Cluster.Storage = &clusterStorageConf{
					Backend:      BlobBackendSharedVolume,
					SharedVolume: true,
					Root:         root,
				}
			},
		},
		{
			name: "maxRequestBytes 必须覆盖 base64 膨胀",
			mutate: func(t *testing.T, cfg *Config) {
				cfg.Cluster.Uplink = &clusterUplinkConf{
					MaxBodyBytes:    8 << 20,
					MaxRequestBytes: 8 << 20,
				}
			},
			wantErr: "maxRequestBytes",
		},
		{
			name: "maxDecodedBodyBytes 不得小于 maxBodyBytes",
			mutate: func(t *testing.T, cfg *Config) {
				cfg.Cluster.Uplink = &clusterUplinkConf{
					MaxBodyBytes:        8 << 20,
					MaxDecodedBodyBytes: 4 << 20,
				}
			},
			wantErr: "maxDecodedBodyBytes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := gatewayControlBase()
			if tt.mutate != nil {
				tt.mutate(t, cfg)
			}
			_, err := cfg.ParseServerMode()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

// TestParseServerModeStandaloneDoesNotEnableRoles 明确 standalone 不启用任何 cluster 角色。
func TestParseServerModeStandaloneDoesNotEnableRoles(t *testing.T) {
	cfg := gatewayControlBase()
	cfg.Server.Mode = ""
	cfg.Cluster = nil
	options, err := cfg.ParseServerMode()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !options.IsStandalone() {
		t.Fatalf("expected standalone, got %q", options.Mode)
	}
	if len(options.Roles.List()) != 0 {
		t.Fatalf("expected no roles, got %v", options.Roles.List())
	}
	if options.LeaseTolerance != DefaultClusterLeaseTolerance {
		t.Fatalf("expected default lease tolerance %v, got %v", DefaultClusterLeaseTolerance, options.LeaseTolerance)
	}
}
