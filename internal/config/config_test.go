package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	serverconfigs "github.com/fakeyanss/jt808-server-go/configs"
	clientconfigs "github.com/fakeyanss/jt808-server-go/test/client/configs"
)

func TestLoad(t *testing.T) {
	type args struct {
		confFilePath string
	}
	tests := []struct {
		name string
		args args
		want *Config
	}{
		{
			name: "case1: test load server config",
			args: args{
				confFilePath: "./testdata/test.yaml",
			},
			want: &Config{
				Log: &logConf{
					ConsoleEnable:       true,
					FileEnable:          true,
					PrintAsJSON:         false,
					LogLevel:            "DEBUG",
					LogDirectory:        "./logs/",
					LogFile:             "jt808-server-go.log",
					MaxSizeOfRolling:    50,
					MaxBackupsOfRolling: 128,
					MaxAgeOfRolling:     7,
				},
				Server: &serverConf{
					Name: "jt808-server-go",
					Mode: ServerModeStandalone,
					Port: &servPort{
						TCPPort:  "8080",
						UDPPort:  "8081",
						HTTPPort: "8008",
					},
					Banner: &servBanner{
						Enable:     true,
						BannerPath: "./configs/banner.txt",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Load(tt.args.confFilePath)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseServerMode(t *testing.T) {
	tests := []struct {
		raw     string
		want    ServerMode
		wantErr bool
	}{
		{"", ServerModeStandalone, false},
		{"standalone", ServerModeStandalone, false},
		{"gateway", ServerModeGateway, false},
		{" GATEWAY ", ServerModeGateway, false},
		{"cluster", "", true},
	}
	for _, tt := range tests {
		got, err := ParseServerMode(tt.raw)
		if tt.wantErr {
			assert.Error(t, err)
			continue
		}
		assert.NoError(t, err)
		assert.Equal(t, tt.want, got)
	}
}

func TestEmbeddedDefaultsMatchCheckedInConfigs(t *testing.T) {
	server, err := serverconfigs.FS.ReadFile("default.yaml")
	assert.NoError(t, err)
	assert.True(t, strings.Contains(string(server), "ackTimeoutMs: 5000"))
	assert.True(t, strings.Contains(string(server), "udpPort: \"8081\""))
	assert.True(t, strings.Contains(string(server), "mode: \"standalone\""))

	client, err := clientconfigs.FS.ReadFile("default.yaml")
	assert.NoError(t, err)
	assert.True(t, strings.Contains(string(client), "remoteAddr: \"localhost:8080\""))
	assert.True(t, strings.Contains(string(client), "maxRetransmissions: 3"))
}
