package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	clientconfigs "github.com/fakeyanss/jt808-server-go/client/configs"
	serverconfigs "github.com/fakeyanss/jt808-server-go/configs"
)

// 旧 storage 段必须启动失败，不能被 viper 静默忽略。
func TestRejectLegacyStorageSection(t *testing.T) {
	valid := viper.New()
	valid.SetConfigType("yaml")
	require.NoError(t, valid.ReadConfig(strings.NewReader("database:\n  type: memory\n")))
	require.NoError(t, rejectLegacyKeys(valid))

	legacy := viper.New()
	legacy.SetConfigType("yaml")
	require.NoError(t, legacy.ReadConfig(strings.NewReader("storage:\n  type: memory\n")))
	err := rejectLegacyKeys(legacy)
	require.Error(t, err)
	require.Contains(t, err.Error(), `"storage"`)
	require.Contains(t, err.Error(), `"database"`)

	legacyCluster := viper.New()
	legacyCluster.SetConfigType("yaml")
	require.NoError(t, legacyCluster.ReadConfig(strings.NewReader("cluster:\n  blob:\n    backend: local\n")))
	err = rejectLegacyKeys(legacyCluster)
	require.Error(t, err)
	require.Contains(t, err.Error(), `"cluster.blob"`)
	require.Contains(t, err.Error(), `"cluster.storage"`)
}

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

func TestEmbeddedDefaultsMatchCheckedInConfigs(t *testing.T) {
	server, err := serverconfigs.FS.ReadFile("default.yaml")
	assert.NoError(t, err)
	assert.True(t, strings.Contains(string(server), "ackTimeoutMs: 5000"))
	assert.True(t, strings.Contains(string(server), "udpPort: \"8081\""))

	client, err := clientconfigs.FS.ReadFile("default.yaml")
	assert.NoError(t, err)
	assert.True(t, strings.Contains(string(client), "remoteAddr: \"localhost:8080\""))
	assert.True(t, strings.Contains(string(client), "maxRetransmissions: 3"))
}
