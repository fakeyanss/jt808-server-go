package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 未配置 mediaUpload 时使用默认目录，保证 0x0801 落盘始终可用。
func TestParseMediaUploadDefaults(t *testing.T) {
	var cfg *Config
	options, err := cfg.ParseMediaUpload()
	require.NoError(t, err)
	require.Equal(t, DefaultMediaUploadDir, options.Dir)

	options, err = (&Config{}).ParseMediaUpload()
	require.NoError(t, err)
	require.Equal(t, DefaultMediaUploadDir, options.Dir)
}

// 显式目录会覆盖默认值，空白值仍回退到默认目录。
func TestParseMediaUploadOverridesDir(t *testing.T) {
	cfg := &Config{MediaUpload: &mediaUploadConf{Dir: "/var/lib/jt808/media"}}
	options, err := cfg.ParseMediaUpload()
	require.NoError(t, err)
	require.Equal(t, "/var/lib/jt808/media", options.Dir)

	cfg = &Config{MediaUpload: &mediaUploadConf{Dir: "  "}}
	options, err = cfg.ParseMediaUpload()
	require.NoError(t, err)
	require.Equal(t, DefaultMediaUploadDir, options.Dir)
}
