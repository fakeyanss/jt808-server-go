package config

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mix-go/xfmt"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"

	clientconfigs "github.com/fakeyanss/jt808-server-go/client/configs"
	serverconfigs "github.com/fakeyanss/jt808-server-go/configs"
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

const (
	DefaultServConfKey  string = "embedded-default-server-config"
	DefaultCliConfKey   string = "embedded-default-client-config"
	defaultServConfFile string = "default.yaml"
	defaultCliConfFile  string = "default.yaml"
)

type logConf struct {
	ConsoleEnable       bool         `yaml:"consoleEnable"`
	FileEnable          bool         `yaml:"fileEnable"`
	PrintAsJSON         bool         `yaml:"printAsJson"`
	LogLevel            LogLevelType `yaml:"logLevel"`
	LogDirectory        string       `yaml:"logDirectory"`
	LogFile             string       `yaml:"logFile"`
	MaxSizeOfRolling    int          `yaml:"maxSizeOfRolling"`
	MaxBackupsOfRolling int          `yaml:"maxBackupsOfRolling"`
	MaxAgeOfRolling     int          `yaml:"maxAgeOfRolling"`
}

type serverConf struct {
	Name               string      `yaml:"name"`
	Port               *servPort   `yaml:"port"`
	Banner             *servBanner `yaml:"banner"`
	Action             *actionConf `yaml:"action"`
	AckTimeoutMS       int         `yaml:"ackTimeoutMs"`
	MaxRetransmissions int         `yaml:"maxRetransmissions"`
}

// actionConf 配置业务数据回调。目前仅提供 HTTP 示例实现。
type actionConf struct {
	HTTP *httpActionConf `yaml:"http"`
}

type httpActionConf struct {
	URL       string `yaml:"url"`
	TimeoutMS int    `yaml:"timeoutMs"`
}

type servPort struct {
	TCPPort  string `yaml:"tcpPort"`
	UDPPort  string `yaml:"udpPort"`
	HTTPPort string `yaml:"httpPort"`
}

type servBanner struct {
	Enable     bool   `yaml:"enable"`
	BannerPath string `yaml:"bannerPath"`
}

type clientConf struct {
	Name               string            `yaml:"name"`
	Conn               *connection       `yaml:"conn"`
	Concurrency        int               `yaml:"concurrency"`
	Device             *DeviceConf       `yaml:"device"`
	DeviceGeo          *DeviceGeoConf    `yaml:"deviceGeo"`
	DeviceParams       *DeviceParamsConf `yaml:"deviceParams"`
	AckTimeoutMS       int               `yaml:"ackTimeoutMs"`
	MaxRetransmissions int               `yaml:"maxRetransmissions"`
}

type connection struct {
	RemoteAddr string `yaml:"remoteAddr"`
}

type DeviceConf struct {
	IDReg           string `yaml:"idReg"`
	IMEIReg         string `yaml:"imeiReg"`
	PhoneReg        string `yaml:"phoneReg"`
	PlateReg        string `yaml:"plateReg"`
	ProtocolVersion string `yaml:"protocolVersion"`
	TransProto      string `yaml:"transProto"`
	Keepalive       int    `yaml:"keepalive"`
	ProvinceIDReg   string `yaml:"provinceIdReg"`
	CityIDReg       string `yaml:"cityIdReg"`
	PlateColorReg   string `yaml:"plateColorReg"`
}

type DeviceGeoConf struct {
	LocationReportInterval int           `yaml:"locationReportInterval"`
	Geo                    *geoConf      `yaml:"geo"`
	Location               *locationConf `yaml:"location"`
	Drive                  *driveConf    `yaml:"drive"`
}

type geoConf struct {
	ACCStatusReg              string `yaml:"accStatusReg"`
	LocationStatusReg         string `yaml:"locationStatusReg"`
	LatitudeTypeReg           string `yaml:"latitudeTypeReg"`
	LongitudeTypeReg          string `yaml:"longitudeTypeReg"`
	OperatingStatusReg        string `yaml:"operatingStatusReg"`
	GeoEncryptionStatusReg    string `yaml:"geoEncryptionStatusReg"`
	LoadStatusReg             string `yaml:"loadStatusReg"`
	FuelSystemStatusReg       string `yaml:"fuelSystemStatusReg"`
	AlternatorSystemStatusReg string `yaml:"alternatorSystemStatusReg"`
	DoorLockedStatusReg       string `yaml:"doorLockedStatusReg"`
	FrontDoorStatusReg        string `yaml:"frontDoorStatusReg"`
	MidDoorStatusReg          string `yaml:"midDoorStatusReg"`
	BackDoorStatusReg         string `yaml:"backDoorStatusReg"`
	DriverDoorStatusReg       string `yaml:"driverDoorStatusReg"`
	CustomDoorStatusReg       string `yaml:"customDoorStatusReg"`
	GPSLocationStatusReg      string `yaml:"gpsLocationStatusReg"`
	BeidouLocationStatusReg   string `yaml:"beidouLocationStatusReg"`
	GLONASSLocationStatusReg  string `yaml:"glonassLocationStatusReg"`
	GalileoLocationStatusReg  string `yaml:"galileoLocationStatusReg"`
	DrivingStatusReg          string `yaml:"drivingStatusReg"`
}

type locationConf struct {
	LatitudeReg  string `yaml:"latitudeReg"`
	LongitudeReg string `yaml:"longitudeReg"`
	AltitudeReg  string `yaml:"altitudeReg"`
}

type driveConf struct {
	SpeedReg     string `yaml:"speedReg"`
	DirectionReg string `yaml:"directionReg"`
}

type DeviceParamsConf struct {
}

type Config struct {
	Log         *logConf         `yaml:"log"`
	Server      *serverConf      `yaml:"server"`
	Client      *clientConf      `yaml:"client"`
	Storage     *storageConf     `yaml:"storage"`
	Retention   *retentionConf   `yaml:"retention"`
	AlarmServer *alarmServerConf `yaml:"alarmServer"`
}

// 苏标附件服务器的工程默认值。未配置 alarmServer.port 时服务不启动。
const (
	DefaultAlarmServerDir           = "./data/alarms"
	DefaultAlarmServerMaxFileSize   = 100 * 1024 * 1024
	DefaultAlarmServerMaxConcurrent = 16
	DefaultAlarmServerCompletedTTL  = 20 * time.Minute
)

// alarmServerConf 配置苏标主动安全附件服务器。
// port 为空表示不启用；显式配置 port 后才监听独立 TCP 端口。
type alarmServerConf struct {
	Port          string `yaml:"port"`
	Dir           string `yaml:"dir"`
	MaxFileSize   int64  `yaml:"maxFileSize"`
	MaxConcurrent int    `yaml:"maxConcurrent"`
	// CompletedTTL 是完成态内存状态的保留时长（如 "20m"）；
	// 显式 "0" 表示不淘汰，留空使用默认值。
	CompletedTTL string `yaml:"completedTTL"`
	// AutoUpload 控制报警首次上报时是否自动下发 0x9208。
	// 默认 false，保持既有手动下发行为不变。
	AutoUpload bool `yaml:"autoUpload"`
	// UploadIP 是 0x9208 中携带的附件服务器 IP，autoUpload 开启时必填。
	UploadIP string `yaml:"uploadIP"`
	// UploadPort 是 0x9208 中携带的附件服务器 TCP 端口；
	// 0 表示复用 alarmServer.port。
	UploadPort int `yaml:"uploadPort"`
	// UploadUDPPort 是 0x9208 中携带的附件服务器 UDP 端口，0 表示不提供。
	UploadUDPPort int `yaml:"uploadUDPPort"`
}

// AlarmServerOptions 是附件服务器的归一化配置。
type AlarmServerOptions struct {
	Port          uint16
	Dir           string
	MaxFileSize   int64
	MaxConcurrent int
	CompletedTTL  time.Duration
	AutoUpload    bool
	UploadIP      string
	UploadPort    uint16
	UploadUDPPort uint16
}

// ParseAlarmServer 解析并校验 alarmServer 配置。
// 返回 enabled=false 表示未配置端口，附件服务器不启动，行为与旧版本一致。
// 即使 enabled=false，只要开启了 autoUpload，仍会返回自动下发所需的地址参数。
func (c *Config) ParseAlarmServer() (options AlarmServerOptions, enabled bool, err error) {
	if c == nil || c.AlarmServer == nil {
		return AlarmServerOptions{}, false, nil
	}
	conf := c.AlarmServer
	rawPort := strings.TrimSpace(conf.Port)
	// 未配置端口且未开启自动下发时，整段配置视为未启用，保持历史行为。
	if rawPort == "" && !conf.AutoUpload {
		return AlarmServerOptions{}, false, nil
	}
	port := 0
	if rawPort != "" {
		parsed, parseErr := strconv.Atoi(rawPort)
		if parseErr != nil || parsed < 1 || parsed > 65535 {
			return AlarmServerOptions{}, false, fmt.Errorf("alarmServer.port must be a TCP port between 1 and 65535, got %q", rawPort)
		}
		port = parsed
	}
	dir := strings.TrimSpace(conf.Dir)
	if dir == "" {
		dir = DefaultAlarmServerDir
	}
	if conf.MaxFileSize < 0 || conf.MaxFileSize > math.MaxUint32 {
		return AlarmServerOptions{}, false, fmt.Errorf("alarmServer.maxFileSize must be between 0 and %d, got %d", uint32(math.MaxUint32), conf.MaxFileSize)
	}
	maxFileSize := conf.MaxFileSize
	if maxFileSize == 0 {
		maxFileSize = DefaultAlarmServerMaxFileSize
	}
	if conf.MaxConcurrent < 0 {
		return AlarmServerOptions{}, false, fmt.Errorf("alarmServer.maxConcurrent must be >= 0, got %d", conf.MaxConcurrent)
	}
	maxConcurrent := conf.MaxConcurrent
	if maxConcurrent == 0 {
		maxConcurrent = DefaultAlarmServerMaxConcurrent
	}
	completedTTL := DefaultAlarmServerCompletedTTL
	if raw := strings.TrimSpace(conf.CompletedTTL); raw != "" {
		parsed, parseErr := time.ParseDuration(raw)
		if parseErr != nil || parsed < 0 {
			return AlarmServerOptions{}, false, fmt.Errorf("alarmServer.completedTTL must be a non-negative duration (e.g. 20m), got %q", raw)
		}
		completedTTL = parsed
	}
	if conf.UploadPort < 0 || conf.UploadPort > 65535 {
		return AlarmServerOptions{}, false, fmt.Errorf("alarmServer.uploadPort must be between 0 and 65535, got %d", conf.UploadPort)
	}
	if conf.UploadUDPPort < 0 || conf.UploadUDPPort > 65535 {
		return AlarmServerOptions{}, false, fmt.Errorf("alarmServer.uploadUDPPort must be between 0 and 65535, got %d", conf.UploadUDPPort)
	}
	uploadIP := strings.TrimSpace(conf.UploadIP)
	uploadPort := conf.UploadPort
	if conf.AutoUpload {
		if uploadIP == "" {
			return AlarmServerOptions{}, false, fmt.Errorf("alarmServer.uploadIP is required when alarmServer.autoUpload is enabled")
		}
		if len([]byte(uploadIP)) > 255 {
			return AlarmServerOptions{}, false, fmt.Errorf("alarmServer.uploadIP must contain at most 255 bytes")
		}
		if uploadPort == 0 {
			if port == 0 {
				return AlarmServerOptions{}, false, fmt.Errorf("alarmServer.uploadPort is required when alarmServer.port is not configured")
			}
			uploadPort = port
		}
	}
	return AlarmServerOptions{
		Port:          uint16(port),
		Dir:           dir,
		MaxFileSize:   maxFileSize,
		MaxConcurrent: maxConcurrent,
		CompletedTTL:  completedTTL,
		AutoUpload:    conf.AutoUpload,
		UploadIP:      uploadIP,
		UploadPort:    uint16(uploadPort),
		UploadUDPPort: uint16(conf.UploadUDPPort),
	}, rawPort != "", nil
}

// 保留期默认值。未配置 retention 段时使用，保证与已有的进程内窗口一致：
// 报警去重 30 分钟、确认关联 10 分钟、每设备 1000 条。定位历史的
// 7 天 / 10000 条是自行设定的工程默认值（JT/T 808 未规定平台侧保留期）。
const (
	DefaultLocationRetentionDays = 7
	DefaultLocationMaxPerDevice  = 10000
	DefaultAlarmDedupWindowMS    = 30 * 60 * 1000
	DefaultAlarmConfirmExpireMS  = 10 * 60 * 1000
	DefaultAlarmMaxPerDevice     = 1000
)

// retentionConf 配置持久化数据的保留策略。
// 字段使用指针以区分“未配置”（取默认值）与“显式 0”（关闭该策略）。
type retentionConf struct {
	Locations *locationRetentionConf `yaml:"locations"`
	Alarms    *alarmRetentionConf    `yaml:"alarms"`
}

type locationRetentionConf struct {
	RetentionDays *int `yaml:"retentionDays"`
	MaxPerDevice  *int `yaml:"maxPerDevice"`
}

type alarmRetentionConf struct {
	DedupWindowMS   *int `yaml:"dedupWindowMs"`
	ConfirmExpireMS *int `yaml:"confirmExpireMs"`
	MaxPerDevice    *int `yaml:"maxPerDevice"`
}

// LocationRetention 是定位历史保留策略的归一化结果。
// MaxAge 为 0 表示不做时间清理；MaxPerDevice 为 0 表示不限制条数。
type LocationRetention struct {
	MaxAge       time.Duration
	MaxPerDevice int
}

// AlarmRetention 是报警保留策略的归一化结果。
// DedupWindow 为 0 表示不去重；ConfirmExpire 为 0 表示确认关联立即失效；
// MaxPerDevice 为 0 表示不限制条数。
type AlarmRetention struct {
	DedupWindow   time.Duration
	ConfirmExpire time.Duration
	MaxPerDevice  int
}

// RetentionPolicy 汇总各仓储的保留策略。
type RetentionPolicy struct {
	Locations LocationRetention
	Alarms    AlarmRetention
}

// RetentionPolicy 返回应用默认值后的保留策略。
func (c *Config) RetentionPolicy() RetentionPolicy {
	policy := RetentionPolicy{
		Locations: LocationRetention{
			MaxAge:       time.Duration(DefaultLocationRetentionDays) * 24 * time.Hour,
			MaxPerDevice: DefaultLocationMaxPerDevice,
		},
		Alarms: AlarmRetention{
			DedupWindow:   time.Duration(DefaultAlarmDedupWindowMS) * time.Millisecond,
			ConfirmExpire: time.Duration(DefaultAlarmConfirmExpireMS) * time.Millisecond,
			MaxPerDevice:  DefaultAlarmMaxPerDevice,
		},
	}
	if c == nil || c.Retention == nil {
		return policy
	}
	if loc := c.Retention.Locations; loc != nil {
		if loc.RetentionDays != nil {
			policy.Locations.MaxAge = time.Duration(*loc.RetentionDays) * 24 * time.Hour
		}
		if loc.MaxPerDevice != nil {
			policy.Locations.MaxPerDevice = *loc.MaxPerDevice
		}
	}
	if alarm := c.Retention.Alarms; alarm != nil {
		if alarm.DedupWindowMS != nil {
			policy.Alarms.DedupWindow = time.Duration(*alarm.DedupWindowMS) * time.Millisecond
		}
		if alarm.ConfirmExpireMS != nil {
			policy.Alarms.ConfirmExpire = time.Duration(*alarm.ConfirmExpireMS) * time.Millisecond
		}
		if alarm.MaxPerDevice != nil {
			policy.Alarms.MaxPerDevice = *alarm.MaxPerDevice
		}
	}
	return policy
}

// ValidateRetention 校验保留策略配置，负数视为非法配置。
// 显式 0 合法，表示关闭对应的清理/去重/上限。
func (c *Config) ValidateRetention() error {
	if c == nil || c.Retention == nil {
		return nil
	}
	if loc := c.Retention.Locations; loc != nil {
		if loc.RetentionDays != nil && *loc.RetentionDays < 0 {
			return fmt.Errorf("retention.locations.retentionDays must be >= 0, got %d", *loc.RetentionDays)
		}
		if loc.MaxPerDevice != nil && *loc.MaxPerDevice < 0 {
			return fmt.Errorf("retention.locations.maxPerDevice must be >= 0, got %d", *loc.MaxPerDevice)
		}
	}
	if alarm := c.Retention.Alarms; alarm != nil {
		if alarm.DedupWindowMS != nil && *alarm.DedupWindowMS < 0 {
			return fmt.Errorf("retention.alarms.dedupWindowMs must be >= 0, got %d", *alarm.DedupWindowMS)
		}
		if alarm.ConfirmExpireMS != nil && *alarm.ConfirmExpireMS < 0 {
			return fmt.Errorf("retention.alarms.confirmExpireMs must be >= 0, got %d", *alarm.ConfirmExpireMS)
		}
		if alarm.MaxPerDevice != nil && *alarm.MaxPerDevice < 0 {
			return fmt.Errorf("retention.alarms.maxPerDevice must be >= 0, got %d", *alarm.MaxPerDevice)
		}
	}
	return nil
}

// storageConf 配置持久化后端；type 缺省为 memory。
type storageConf struct {
	Type   string      `yaml:"type"`
	SQLite *sqliteConf `yaml:"sqlite"`
	MySQL  *mysqlConf  `yaml:"mysql"`
}

type sqliteConf struct {
	Path string `yaml:"path"`
}

type mysqlConf struct {
	DSN string `yaml:"dsn"`
}

var (
	configOnce sync.Once
	config     *Config
)

func Load(confFilePath string) *Config {
	configOnce.Do(func() {
		config = &Config{}
		viper.SetConfigType("yaml")

		var err error
		if confFilePath == DefaultServConfKey || confFilePath == DefaultCliConfKey {
			var confContent []byte
			if confFilePath == DefaultServConfKey {
				confContent, err = serverconfigs.FS.ReadFile(defaultServConfFile)
			} else {
				confContent, err = clientconfigs.FS.ReadFile(defaultCliConfFile)
			}
			if err != nil {
				panic(errors.Wrap(err, "Fail to read embedded default config"))
			}
			err = viper.ReadConfig(bytes.NewBuffer(confContent))
		} else {
			viper.SetConfigFile(confFilePath)
			err = viper.ReadInConfig()
		}

		if err != nil {
			panic(errors.Wrap(err, "Fail to read config with viper"))
		}

		err = viper.Unmarshal(config)
		if err != nil {
			panic(errors.Wrap(err, "Fail to unmarshal config"))
		}
		fmt.Printf("Load configuration: %s\n", xfmt.Sprintf("%+v", config))
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
