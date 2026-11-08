package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseCommandOutboxMode(t *testing.T) {
	tests := []struct {
		name    string
		mode    string
		want    string
		wantErr bool
	}{
		{name: "缺省留空待归一化", mode: "", want: ""},
		{name: "off", mode: "off", want: CommandOutboxModeOff},
		{name: "memory", mode: "memory", want: CommandOutboxModeMemory},
		{name: "durable", mode: "durable", want: CommandOutboxModeDurable},
		{name: "大小写与空白归一化", mode: "  DURABLE ", want: CommandOutboxModeDurable},
		{name: "非法值", mode: "best-effort", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{CommandOutbox: &commandOutboxConf{Mode: tt.mode}}
			options, err := cfg.ParseCommandOutbox()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, options.Mode)
		})
	}
}

func TestResolveCommandOutboxMode(t *testing.T) {
	tests := []struct {
		name    string
		mode    string
		backend string
		want    string
	}{
		{name: "单机 memory 后端默认 memory", mode: "", backend: "memory", want: CommandOutboxModeMemory},
		{name: "SQL 后端默认 durable", mode: "", backend: "sqlite", want: CommandOutboxModeDurable},
		{name: "MySQL 后端默认 durable", mode: "", backend: "mysql", want: CommandOutboxModeDurable},
		{name: "显式 off 覆盖后端", mode: "off", backend: "sqlite", want: CommandOutboxModeOff},
		{name: "显式 memory 覆盖 SQL 后端", mode: "memory", backend: "sqlite", want: CommandOutboxModeMemory},
		{name: "显式 durable 覆盖 memory 后端", mode: "durable", backend: "memory", want: CommandOutboxModeDurable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ResolveCommandOutboxMode(tt.mode, tt.backend))
		})
	}
}
