package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
