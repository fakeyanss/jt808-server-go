package config

import (
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/rs/zerolog"

	"github.com/fakeyanss/jt808-server-go/pkg/logger"
)

type LogLevelType string

const (
	LogLevelFatal LogLevelType = "FATAl"
	LogLevelError LogLevelType = "ERROR"
	LogLevelWarn  LogLevelType = "WARN"
	LogLevelInfo  LogLevelType = "INFO"
	LogLevelDebug LogLevelType = "DEBUG"
)

type logConf struct {
	ConsoleEnable       bool         `toml:"console_enable"`
	FileEnable          bool         `toml:"file_enable"`
	PrintAsJSON         bool         `toml:"print_as_json"`
	LogLevel            LogLevelType `toml:"log_level"`
	LogDirectory        string       `toml:"log_directory"`
	LogFile             string       `toml:"log_file"`
	MaxSizeOfRolling    int          `toml:"max_size_of_rolling"`
	MaxBackupsOfRolling int          `toml:"max_backups_of_rolling"`
	MaxAgeOfRolling     int          `toml:"max_age_of_rolling"`
}

type serverConf struct {
	name   string     `toml:"name"`
	Port   servPort   `toml:"port"`
	Banner servBanner `toml:"banner"`
}

type servPort struct {
	TCPPort  string `toml:"tcp_port"`
	UDPPort  string `toml:"udp_port"`
	HTTPPort string `toml:"http_port"`
}

type servBanner struct {
	Enable     bool   `toml:"enable"`
	BannerPath string `toml:"banner_path"`
}

type clientConf struct {
	Name        string       `toml:"name"`
	Conn        connection   `toml:"conn"`
	Concurrency int          `toml:"concurrency"`
	Device      clientDevice `toml:"device"`
}

type connection struct {
	RemoteAddr string `toml:"remote_addr"`
}

type clientDevice struct {
	LocationReportInteval int    `toml:"localtion_report_inteval"`
	DeviceTpl             string `toml:"device_tpl"`
	Msg0100Tpl            string `toml:"msg_0100_tpl"`
}

type Config struct {
	Log    logConf    `toml:"log"`
	Server serverConf `toml:"server"`
	Client clientConf `toml:"client"`
}

var (
	configOnce sync.Once
	config     *Config
)

func Load(confFilePath string) *Config {
	configOnce.Do(func() {
		config = &Config{}
		if _, err := toml.DecodeFile(confFilePath, config); err != nil {
			panic(err)
		}
	})
	return config
}

func (c *Config) ParseLogConf() *logger.Config {
	logCfg := c.Log
	var logLevel int8
	switch logCfg.LogLevel {
	case "DEBUG":
		logLevel = int8(zerolog.DebugLevel)
	case "INFO":
		logLevel = int8(zerolog.InfoLevel)
	case "WARN":
		logLevel = int8(zerolog.WarnLevel)
	case "ERROR":
		logLevel = int8(zerolog.ErrorLevel)
	case "FATAl":
		logLevel = int8(zerolog.FatalLevel)
	}
	return &logger.Config{
		ConsoleLoggingEnabled: logCfg.ConsoleEnable,
		EncodeLogsAsJSON:      logCfg.PrintAsJSON,
		FileLoggingEnabled:    logCfg.FileEnable,
		LogLevel:              logLevel,
		Directory:             logCfg.LogDirectory,
		Filename:              logCfg.LogFile,
		MaxSize:               logCfg.MaxSizeOfRolling,
		MaxBackups:            logCfg.MaxBackupsOfRolling,
		MaxAge:                logCfg.MaxAgeOfRolling,
	}
}
