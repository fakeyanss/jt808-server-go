package config

import (
	"bytes"
	"fmt"
	"math"
	"net"
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
	Log           *logConf           `yaml:"log"`
	Server        *serverConf        `yaml:"server"`
	Client        *clientConf        `yaml:"client"`
	Storage       *storageConf       `yaml:"storage"`
	Retention     *retentionConf     `yaml:"retention"`
	AlarmServer   *alarmServerConf   `yaml:"alarmServer"`
	MediaServer   *mediaServerConf   `yaml:"mediaServer"`
	MediaUpload   *mediaUploadConf   `yaml:"mediaUpload"`
	Admin         *adminConf         `yaml:"admin"`
	API           *apiConf           `yaml:"api"`
	CommandOutbox *commandOutboxConf `yaml:"commandOutbox"`
	Uplink        *uplinkConf        `yaml:"uplink"`
}

// 业务 API 的默认限流参数；token 为空表示未开启鉴权，启动时会打警告。
const (
	DefaultAPIRatePerSecond = 50.0
	DefaultAPIBurst         = 100
)

// apiConf 配置业务 API 的鉴权与限流。
// token 为空表示鉴权关闭，仅建议在可信内网临时使用。
type apiConf struct {
	Auth      *apiAuthConf      `yaml:"auth"`
	RateLimit *apiRateLimitConf `yaml:"rateLimit"`
}

type apiAuthConf struct {
	Token string `yaml:"token"`
}

type apiRateLimitConf struct {
	RatePerSecond *float64 `yaml:"ratePerSecond"`
	Burst         *int     `yaml:"burst"`
}

// APIOptions 是业务 API 鉴权与限流的归一化配置。
type APIOptions struct {
	Token         string
	RatePerSecond float64
	Burst         int
}

// ParseAPI 解析业务 API 配置；未配置时返回默认限流与空 token（鉴权关闭）。
func (c *Config) ParseAPI() (APIOptions, error) {
	options := APIOptions{
		RatePerSecond: DefaultAPIRatePerSecond,
		Burst:         DefaultAPIBurst,
	}
	if c == nil || c.API == nil {
		return options, nil
	}
	conf := c.API
	if conf.Auth != nil {
		options.Token = strings.TrimSpace(conf.Auth.Token)
	}
	if limit := conf.RateLimit; limit != nil {
		if limit.RatePerSecond != nil {
			if *limit.RatePerSecond <= 0 {
				return APIOptions{}, fmt.Errorf("api.rateLimit.ratePerSecond must be > 0, got %v", *limit.RatePerSecond)
			}
			options.RatePerSecond = *limit.RatePerSecond
		}
		if limit.Burst != nil {
			if *limit.Burst < 1 {
				return APIOptions{}, fmt.Errorf("api.rateLimit.burst must be >= 1, got %d", *limit.Burst)
			}
			options.Burst = *limit.Burst
		}
	}
	return options, nil
}

// uplinkConf 配置上行可靠性。
// reliability 按消息分类选择 best_effort / at_least_once；
// at_least_once 先完成落库再应答，落库失败由终端按协议重传。
type uplinkConf struct {
	Reliability *uplinkReliabilityConf `yaml:"reliability"`
}

type uplinkReliabilityConf struct {
	Default       string `yaml:"default"`
	Location      string `yaml:"location"`
	BatchLocation string `yaml:"batchLocation"`
	Alarm         string `yaml:"alarm"`
}

// UplinkOptions 是上行可靠性的归一化配置。
type UplinkOptions struct {
	Default       string
	Location      string
	BatchLocation string
	Alarm         string
}

const (
	UplinkReliabilityBestEffort  = "best_effort"
	UplinkReliabilityAtLeastOnce = "at_least_once"
)

// DefaultUplinkOptions 返回默认上行可靠性配置：
// 实时定位 best-effort（几十秒后就有下一条），批量定位与报警 at-least-once。
func DefaultUplinkOptions() UplinkOptions {
	return UplinkOptions{
		Default:       UplinkReliabilityBestEffort,
		Location:      UplinkReliabilityBestEffort,
		BatchLocation: UplinkReliabilityAtLeastOnce,
		Alarm:         UplinkReliabilityAtLeastOnce,
	}
}

// ParseUplink 归一化上行可靠性配置。
func (c *Config) ParseUplink() (UplinkOptions, error) {
	options := DefaultUplinkOptions()
	if c == nil || c.Uplink == nil {
		return options, nil
	}
	if conf := c.Uplink.Reliability; conf != nil {
		options.Default = firstNonEmpty(conf.Default, options.Default)
		options.Location = firstNonEmpty(conf.Location, options.Location)
		options.BatchLocation = firstNonEmpty(conf.BatchLocation, options.BatchLocation)
		options.Alarm = firstNonEmpty(conf.Alarm, options.Alarm)
	}

	for name, value := range map[string]string{
		"default":       options.Default,
		"location":      options.Location,
		"batchLocation": options.BatchLocation,
		"alarm":         options.Alarm,
	} {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized != UplinkReliabilityBestEffort && normalized != UplinkReliabilityAtLeastOnce {
			return UplinkOptions{}, fmt.Errorf(
				"uplink.reliability.%s must be best_effort or at_least_once, got %q", name, value)
		}
	}
	options.Default = strings.ToLower(strings.TrimSpace(options.Default))
	options.Location = strings.ToLower(strings.TrimSpace(options.Location))
	options.BatchLocation = strings.ToLower(strings.TrimSpace(options.BatchLocation))
	options.Alarm = strings.ToLower(strings.TrimSpace(options.Alarm))

	return options, nil
}

func firstNonEmpty(value, fallback string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return fallback
}

// 苏标附件服务器的工程默认值。未配置 alarmServer.port 时服务不启动。
const (
	DefaultAlarmServerDir           = "./data/alarms"
	DefaultAlarmServerMaxFileSize   = 100 * 1024 * 1024
	DefaultAlarmServerMaxConcurrent = 16
	// DefaultAlarmServerRetentionDays 是已完成附件文件的默认落盘保留天数。
	DefaultAlarmServerRetentionDays = 7
	DefaultAlarmServerCompletedTTL  = 20 * time.Minute
)

// alarmServerConf 配置苏标主动安全附件服务器。
// port 为 TCP 监听端口，udpPort 为 UDP 监听端口；两者都为空表示不启用。
// 0x9208 通告的端口直接取自这两个监听端口，未配置的一侧以 0 表示不可用，
// 避免通告地址与实际监听不一致。
type alarmServerConf struct {
	Port          string `yaml:"port"`
	UDPPort       string `yaml:"udpPort"`
	Dir           string `yaml:"dir"`
	MaxFileSize   int64  `yaml:"maxFileSize"`
	MaxConcurrent int    `yaml:"maxConcurrent"`
	// CompletedTTL 是完成态内存状态的保留时长（如 "20m"）；
	// 显式 "0" 表示不淘汰，留空使用默认值。
	CompletedTTL string `yaml:"completedTTL"`
	// RetentionDays 是已完成附件文件的落盘保留天数；显式 0 表示不清理。
	RetentionDays *int `yaml:"retentionDays"`
	// AutoUpload 控制报警首次上报时是否自动下发 0x9208。
	// 默认 false，保持既有手动下发行为不变。
	AutoUpload bool `yaml:"autoUpload"`
	// UploadIP 是 0x9208 中携带的附件服务器 IP，autoUpload 开启时必填。
	// 端口不单独配置，始终使用 port / udpPort 的实际监听端口。
	UploadIP string `yaml:"uploadIP"`
}

// AlarmServerOptions 是附件服务器的归一化配置。
type AlarmServerOptions struct {
	Port          uint16 // TCP 监听与通告端口，0 表示未启用
	UDPPort       uint16 // UDP 监听与通告端口，0 表示未启用
	Dir           string
	MaxFileSize   int64
	MaxConcurrent int
	CompletedTTL  time.Duration
	Retention     time.Duration
	AutoUpload    bool
	UploadIP      string
}

// ParseAlarmServer 解析并校验 alarmServer 配置。
// 返回 enabled=false 表示未配置任何监听端口，附件服务器不启动，行为与旧版本一致。
// port / udpPort 分别对应 TCP / UDP 的实际监听与 0x9208 通告端口，未配置的一侧通告为 0。
func (c *Config) ParseAlarmServer() (options AlarmServerOptions, enabled bool, err error) {
	if c == nil || c.AlarmServer == nil {
		return AlarmServerOptions{}, false, nil
	}
	conf := c.AlarmServer
	rawPort := strings.TrimSpace(conf.Port)
	rawUDPPort := strings.TrimSpace(conf.UDPPort)
	// 未配置任何端口且未开启自动下发时，整段配置视为未启用，保持历史行为。
	if rawPort == "" && rawUDPPort == "" && !conf.AutoUpload {
		return AlarmServerOptions{}, false, nil
	}
	parsePort := func(name, raw string) (int, error) {
		if raw == "" {
			return 0, nil
		}
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed < 1 || parsed > 65535 {
			return 0, fmt.Errorf("alarmServer.%s must be a port between 1 and 65535, got %q", name, raw)
		}
		return parsed, nil
	}
	port, err := parsePort("port", rawPort)
	if err != nil {
		return AlarmServerOptions{}, false, err
	}
	udpPort, err := parsePort("udpPort", rawUDPPort)
	if err != nil {
		return AlarmServerOptions{}, false, err
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
	retentionDays := DefaultAlarmServerRetentionDays
	if conf.RetentionDays != nil {
		if *conf.RetentionDays < 0 {
			return AlarmServerOptions{}, false, fmt.Errorf("alarmServer.retentionDays must be >= 0, got %d", *conf.RetentionDays)
		}
		retentionDays = *conf.RetentionDays
	}
	uploadIP := strings.TrimSpace(conf.UploadIP)
	if conf.AutoUpload {
		if uploadIP == "" {
			return AlarmServerOptions{}, false, fmt.Errorf("alarmServer.uploadIP is required when alarmServer.autoUpload is enabled")
		}
		if len([]byte(uploadIP)) > 255 {
			return AlarmServerOptions{}, false, fmt.Errorf("alarmServer.uploadIP must contain at most 255 bytes")
		}
		if port == 0 && udpPort == 0 {
			return AlarmServerOptions{}, false, fmt.Errorf("alarmServer.port or alarmServer.udpPort is required when alarmServer.autoUpload is enabled")
		}
	}
	return AlarmServerOptions{
		Port:          uint16(port),
		UDPPort:       uint16(udpPort),
		Dir:           dir,
		MaxFileSize:   maxFileSize,
		MaxConcurrent: maxConcurrent,
		CompletedTTL:  completedTTL,
		Retention:     time.Duration(retentionDays) * 24 * time.Hour,
		AutoUpload:    conf.AutoUpload,
		UploadIP:      uploadIP,
	}, true, nil
}

// 媒体 RTP 接入服务器的工程默认值。未配置 mediaServer.port 与 mediaServer.tcpPort 时服务不启动。
const (
	DefaultMediaServerDir           = "./data/media"
	DefaultMediaServerMaxConcurrent = 16
	DefaultMediaServerMaxTCPConns   = 64
	DefaultMediaServerRetentionDays = 7
)

// mediaServerConf 配置 1078 音视频接入服务器。
// port 为 UDP 端口，tcpPort 为 TCP 端口；任一非空即启用，可同时监听。
type mediaServerConf struct {
	Port          string `yaml:"port"`
	TCPPort       string `yaml:"tcpPort"`
	Dir           string `yaml:"dir"`
	MaxConcurrent int    `yaml:"maxConcurrent"`
	MaxTCPConns   int    `yaml:"maxTCPConns"`
	// RetentionDays 是音视频文件的落盘保留天数；显式 0 表示不清理。
	RetentionDays *int `yaml:"retentionDays"`
}

// MediaServerOptions 是媒体接入服务器的归一化配置。
type MediaServerOptions struct {
	Port          uint16 // UDP 端口，0 表示不启用
	TCPPort       uint16 // TCP 端口，0 表示不启用
	Dir           string
	MaxConcurrent int
	MaxTCPConns   int
	Retention     time.Duration
}

// ParseMediaServer 解析并校验 mediaServer 配置。
// 返回 enabled=false 表示未配置任何端口，媒体接入不启动，行为与旧版本一致。
func (c *Config) ParseMediaServer() (options MediaServerOptions, enabled bool, err error) {
	if c == nil || c.MediaServer == nil {
		return MediaServerOptions{}, false, nil
	}
	conf := c.MediaServer
	udpPort, err := parseMediaPort("mediaServer.port", conf.Port, "UDP")
	if err != nil {
		return MediaServerOptions{}, false, err
	}
	tcpPort, err := parseMediaPort("mediaServer.tcpPort", conf.TCPPort, "TCP")
	if err != nil {
		return MediaServerOptions{}, false, err
	}
	if udpPort == 0 && tcpPort == 0 {
		return MediaServerOptions{}, false, nil
	}
	dir := strings.TrimSpace(conf.Dir)
	if dir == "" {
		dir = DefaultMediaServerDir
	}
	if conf.MaxConcurrent < 0 {
		return MediaServerOptions{}, false, fmt.Errorf("mediaServer.maxConcurrent must be >= 0, got %d", conf.MaxConcurrent)
	}
	maxConcurrent := conf.MaxConcurrent
	if maxConcurrent == 0 {
		maxConcurrent = DefaultMediaServerMaxConcurrent
	}
	if conf.MaxTCPConns < 0 {
		return MediaServerOptions{}, false, fmt.Errorf("mediaServer.maxTCPConns must be >= 0, got %d", conf.MaxTCPConns)
	}
	maxTCPConns := conf.MaxTCPConns
	if maxTCPConns == 0 {
		maxTCPConns = DefaultMediaServerMaxTCPConns
	}
	retentionDays := DefaultMediaServerRetentionDays
	if conf.RetentionDays != nil {
		if *conf.RetentionDays < 0 {
			return MediaServerOptions{}, false, fmt.Errorf("mediaServer.retentionDays must be >= 0, got %d", *conf.RetentionDays)
		}
		retentionDays = *conf.RetentionDays
	}
	return MediaServerOptions{
		Port:          udpPort,
		TCPPort:       tcpPort,
		Dir:           dir,
		MaxConcurrent: maxConcurrent,
		MaxTCPConns:   maxTCPConns,
		Retention:     time.Duration(retentionDays) * 24 * time.Hour,
	}, true, nil
}

// parseMediaPort 解析可选的监听端口；空值返回 0。
func parseMediaPort(key, raw, transport string) (uint16, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("%s must be a %s port between 1 and 65535, got %q", key, transport, raw)
	}
	return uint16(port), nil
}

// DefaultMediaUploadDir 是 0x0801 多媒体文件的默认落盘目录。
const DefaultMediaUploadDir = "./data/media-uploads"

// mediaUploadConf 配置 0x0801 多媒体文件落盘目录。
// 与 mediaServer 不同，0x0801 落盘始终启用，本段只决定文件存放位置。
type mediaUploadConf struct {
	Dir string `yaml:"dir"`
}

// MediaUploadOptions 是 0x0801 多媒体文件落盘的归一化配置。
type MediaUploadOptions struct {
	Dir string
}

// ParseMediaUpload 解析媒体上传落盘配置；未配置时使用默认目录。
func (c *Config) ParseMediaUpload() (MediaUploadOptions, error) {
	options := MediaUploadOptions{Dir: DefaultMediaUploadDir}
	if c == nil || c.MediaUpload == nil {
		return options, nil
	}
	if dir := strings.TrimSpace(c.MediaUpload.Dir); dir != "" {
		options.Dir = dir
	}
	return options, nil
}

// 管理面默认值。未配置 admin 段或 enable=false 时不启动管理监听，
// 现有终端接入端口、HTTP API 与苏标/媒体服务行为不变。
const (
	DefaultAdminAddr           = "127.0.0.1:9000"
	DefaultAdminRatePerSecond  = 10.0
	DefaultAdminBurst          = 20
	AdminRateKeyByCredential   = "credential"
	AdminRateKeyBySource       = "source"
	defaultAdminRateLimitKeyBy = AdminRateKeyByCredential
)

// adminConf 配置独立的管理面 HTTP 监听。
// enable 缺省 false；开启时必须显式配置非空 token，避免管理面裸奔。
type adminConf struct {
	Enable    bool                `yaml:"enable"`
	Addr      string              `yaml:"addr"`
	Token     string              `yaml:"token"`
	RateLimit *adminRateLimitConf `yaml:"rateLimit"`
}

type adminRateLimitConf struct {
	RatePerSecond *float64 `yaml:"ratePerSecond"`
	Burst         *int     `yaml:"burst"`
	KeyBy         string   `yaml:"keyBy"`
}

// AdminOptions 是管理面监听与鉴权限流的归一化配置。
type AdminOptions struct {
	Addr          string
	Token         string
	RatePerSecond float64
	Burst         int
	KeyBy         string
}

// ParseAdmin 解析并校验 admin 配置。
// 返回 enabled=false 表示管理面未开启，进程不监听管理端口。
// 开启时 token 必须非空、addr 必须是合法 host:port、限流参数必须为正数。
func (c *Config) ParseAdmin() (options AdminOptions, enabled bool, err error) {
	if c == nil || c.Admin == nil || !c.Admin.Enable {
		return AdminOptions{}, false, nil
	}
	conf := c.Admin
	token := strings.TrimSpace(conf.Token)
	if token == "" {
		return AdminOptions{}, false, fmt.Errorf("admin.token is required when admin.enable is true")
	}
	addr := strings.TrimSpace(conf.Addr)
	if addr == "" {
		addr = DefaultAdminAddr
	}
	host, portText, splitErr := net.SplitHostPort(addr)
	if splitErr != nil || host == "" {
		return AdminOptions{}, false, fmt.Errorf("admin.addr must be a host:port address, got %q", addr)
	}
	port, parseErr := strconv.Atoi(portText)
	if parseErr != nil || port < 1 || port > 65535 {
		return AdminOptions{}, false, fmt.Errorf("admin.addr must use a port between 1 and 65535, got %q", addr)
	}

	ratePerSecond := DefaultAdminRatePerSecond
	burst := DefaultAdminBurst
	keyBy := defaultAdminRateLimitKeyBy
	if limit := conf.RateLimit; limit != nil {
		if limit.RatePerSecond != nil {
			if *limit.RatePerSecond <= 0 {
				return AdminOptions{}, false, fmt.Errorf("admin.rateLimit.ratePerSecond must be > 0, got %v", *limit.RatePerSecond)
			}
			ratePerSecond = *limit.RatePerSecond
		}
		if limit.Burst != nil {
			if *limit.Burst < 1 {
				return AdminOptions{}, false, fmt.Errorf("admin.rateLimit.burst must be >= 1, got %d", *limit.Burst)
			}
			burst = *limit.Burst
		}
		if raw := strings.TrimSpace(limit.KeyBy); raw != "" {
			switch raw {
			case AdminRateKeyByCredential, AdminRateKeyBySource:
				keyBy = raw
			default:
				return AdminOptions{}, false, fmt.Errorf("admin.rateLimit.keyBy must be %q or %q, got %q",
					AdminRateKeyByCredential, AdminRateKeyBySource, raw)
			}
		}
	}
	return AdminOptions{
		Addr:          addr,
		Token:         token,
		RatePerSecond: ratePerSecond,
		Burst:         burst,
		KeyBy:         keyBy,
	}, true, nil
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

	// 历史数据定期清理的工程默认值。默认关闭，未显式开启时不改变现有部署的保留情况。
	DefaultRetentionCleanupInterval  = time.Hour
	DefaultRetentionCleanupBatchSize = 1000
	// MaxRetentionDays 限制保留天数上限（100 年），避免换算 time.Duration 时溢出。
	MaxRetentionDays = 36500
	// MaxRetentionCleanupBatchSize 限制单批删除量，避免单条 SQL 参数过多。
	MaxRetentionCleanupBatchSize = 5000
)

// retentionConf 配置持久化数据的保留策略。
// 字段使用指针以区分“未配置”（取默认值）与“显式 0”（关闭该策略）。
type retentionConf struct {
	Locations     *locationRetentionConf `yaml:"locations"`
	Alarms        *alarmRetentionConf    `yaml:"alarms"`
	MediaFiles    *timeRetentionConf     `yaml:"mediaFiles"`
	MediaCaptures *timeRetentionConf     `yaml:"mediaCaptures"`
	Cleanup       *cleanupConf           `yaml:"cleanup"`
}

type locationRetentionConf struct {
	RetentionDays *int `yaml:"retentionDays"`
	MaxPerDevice  *int `yaml:"maxPerDevice"`
}

type alarmRetentionConf struct {
	DedupWindowMS   *int `yaml:"dedupWindowMs"`
	ConfirmExpireMS *int `yaml:"confirmExpireMs"`
	MaxPerDevice    *int `yaml:"maxPerDevice"`
	// RetentionDays 是按接收时间的定期清理保留天数；未配置或 0 表示不按时间清理。
	RetentionDays *int `yaml:"retentionDays"`
}

// timeRetentionConf 是按时间保留的单表配置。
type timeRetentionConf struct {
	RetentionDays *int `yaml:"retentionDays"`
}

// cleanupConf 配置历史数据定期清理任务。
// enabled 缺省为 false；interval / batchSize 仅在开启后生效。
type cleanupConf struct {
	Enabled   *bool  `yaml:"enabled"`
	Interval  string `yaml:"interval"`
	BatchSize *int   `yaml:"batchSize"`
}

// LocationRetention 是定位历史保留策略的归一化结果。
// MaxAge 为 0 表示不做时间清理；MaxPerDevice 为 0 表示不限制条数。
type LocationRetention struct {
	MaxAge       time.Duration
	MaxPerDevice int
}

// AlarmRetention 是报警保留策略的归一化结果。
// DedupWindow 为 0 表示不去重；ConfirmExpire 为 0 表示确认关联立即失效；
// MaxPerDevice 为 0 表示不限制条数；MaxAge 为 0 表示不做时间清理。
type AlarmRetention struct {
	DedupWindow   time.Duration
	ConfirmExpire time.Duration
	MaxPerDevice  int
	MaxAge        time.Duration
}

// TimeRetention 是按时间保留策略的归一化结果；MaxAge 为 0 表示不清理。
type TimeRetention struct {
	MaxAge time.Duration
}

// RetentionCleanupPolicy 是定期清理任务的归一化配置。
type RetentionCleanupPolicy struct {
	Enabled   bool
	Interval  time.Duration
	BatchSize int
}

// RetentionPolicy 汇总各仓储的保留策略。
type RetentionPolicy struct {
	Locations     LocationRetention
	Alarms        AlarmRetention
	MediaFiles    TimeRetention
	MediaCaptures TimeRetention
	Cleanup       RetentionCleanupPolicy
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
		Cleanup: RetentionCleanupPolicy{
			Interval:  DefaultRetentionCleanupInterval,
			BatchSize: DefaultRetentionCleanupBatchSize,
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
		if alarm.RetentionDays != nil {
			policy.Alarms.MaxAge = time.Duration(*alarm.RetentionDays) * 24 * time.Hour
		}
	}
	if media := c.Retention.MediaFiles; media != nil && media.RetentionDays != nil {
		policy.MediaFiles.MaxAge = time.Duration(*media.RetentionDays) * 24 * time.Hour
	}
	if capture := c.Retention.MediaCaptures; capture != nil && capture.RetentionDays != nil {
		policy.MediaCaptures.MaxAge = time.Duration(*capture.RetentionDays) * 24 * time.Hour
	}
	if cleanup := c.Retention.Cleanup; cleanup != nil {
		if cleanup.Enabled != nil {
			policy.Cleanup.Enabled = *cleanup.Enabled
		}
		if raw := strings.TrimSpace(cleanup.Interval); raw != "" {
			if interval, err := time.ParseDuration(raw); err == nil {
				policy.Cleanup.Interval = interval
			}
		}
		if cleanup.BatchSize != nil && *cleanup.BatchSize > 0 {
			policy.Cleanup.BatchSize = *cleanup.BatchSize
		}
	}
	return policy
}

// ValidateRetention 校验保留策略配置，负数视为非法配置。
// 显式 0 合法，表示关闭对应的清理/去重/上限；保留天数上限为 100 年，
// 避免换算 time.Duration 时溢出。
func (c *Config) ValidateRetention() error {
	if c == nil || c.Retention == nil {
		return nil
	}
	if loc := c.Retention.Locations; loc != nil {
		if err := validateRetentionDays("retention.locations.retentionDays", loc.RetentionDays); err != nil {
			return err
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
		if err := validateRetentionDays("retention.alarms.retentionDays", alarm.RetentionDays); err != nil {
			return err
		}
	}
	if media := c.Retention.MediaFiles; media != nil {
		if err := validateRetentionDays("retention.mediaFiles.retentionDays", media.RetentionDays); err != nil {
			return err
		}
	}
	if capture := c.Retention.MediaCaptures; capture != nil {
		if err := validateRetentionDays("retention.mediaCaptures.retentionDays", capture.RetentionDays); err != nil {
			return err
		}
	}
	if cleanup := c.Retention.Cleanup; cleanup != nil {
		if raw := strings.TrimSpace(cleanup.Interval); raw != "" {
			interval, err := time.ParseDuration(raw)
			if err != nil || interval <= 0 {
				return fmt.Errorf("retention.cleanup.interval must be a positive duration (e.g. 1h), got %q", raw)
			}
		}
		if cleanup.BatchSize != nil && (*cleanup.BatchSize < 0 || *cleanup.BatchSize > MaxRetentionCleanupBatchSize) {
			return fmt.Errorf("retention.cleanup.batchSize must be between 0 and %d, got %d",
				MaxRetentionCleanupBatchSize, *cleanup.BatchSize)
		}
	}
	return nil
}

// validateRetentionDays 校验保留天数：未配置合法，0 表示关闭，上限 100 年。
func validateRetentionDays(name string, days *int) error {
	if days == nil {
		return nil
	}
	if *days < 0 {
		return fmt.Errorf("%s must be >= 0, got %d", name, *days)
	}
	if *days > MaxRetentionDays {
		return fmt.Errorf("%s must be <= %d, got %d", name, MaxRetentionDays, *days)
	}
	return nil
}

// 下行命令 outbox 重投的工程默认值。
// initialWait 必须覆盖一次下发到会话重传耗尽的窗口（ackTimeout × (maxRetransmissions+1)），
// 否则重启后的重投会在会话仍在重传时重复下发。
const (
	DefaultCommandOutboxPollInterval  = 10 * time.Second
	DefaultCommandOutboxBatchSize     = 50
	DefaultCommandOutboxMaxAttempts   = 5
	DefaultCommandOutboxLeaseDuration = time.Minute
	DefaultCommandOutboxRetryBase     = 5 * time.Second
	DefaultCommandOutboxRetryMax      = 5 * time.Minute
	DefaultCommandOutboxExpireAfter   = 24 * time.Hour
	DefaultCommandOutboxInitialWait   = time.Minute
)

// command_outbox 的三种运行模式。
//   - off：不写 outbox，下行直接调用 Serv.Send，失败不可查、不可重投。
//   - memory：outbox 只存在进程内，保留幂等键与状态查询，进程重启即丢。
//   - durable：outbox 落在当前存储后端，支持进程重启后继续重投。
const (
	CommandOutboxModeOff     = "off"
	CommandOutboxModeMemory  = "memory"
	CommandOutboxModeDurable = "durable"
)

// commandOutboxConf 配置 pending 命令的重投调度。
// mode 缺省时按存储后端归一化：memory 后端默认 memory，SQL 后端默认 durable；
// 调度参数仅对 memory/durable 生效，off 模式忽略。
type commandOutboxConf struct {
	Mode          string `yaml:"mode"`
	PollInterval  string `yaml:"pollInterval"`
	BatchSize     int    `yaml:"batchSize"`
	MaxAttempts   int    `yaml:"maxAttempts"`
	LeaseDuration string `yaml:"leaseDuration"`
	RetryBase     string `yaml:"retryBase"`
	RetryMax      string `yaml:"retryMax"`
	ExpireAfter   string `yaml:"expireAfter"`
	InitialWait   string `yaml:"initialWait"`
}

// CommandOutboxOptions 是重投调度的归一化配置。
type CommandOutboxOptions struct {
	Mode          string
	PollInterval  time.Duration
	BatchSize     int
	MaxAttempts   int
	LeaseDuration time.Duration
	RetryBase     time.Duration
	RetryMax      time.Duration
	ExpireAfter   time.Duration
	InitialWait   time.Duration
}

// ResolveCommandOutboxMode 把未显式配置的 mode 按存储后端归一化。
// memory 后端无法跨重启恢复，默认 memory；SQL 后端具备持久化能力，默认 durable。
// 显式配置优先，调用方应保证值已通过 ParseCommandOutbox 校验。
func ResolveCommandOutboxMode(mode, backend string) string {
	if normalized := strings.ToLower(strings.TrimSpace(mode)); normalized != "" {
		return normalized
	}
	if strings.EqualFold(strings.TrimSpace(backend), "memory") {
		return CommandOutboxModeMemory
	}
	return CommandOutboxModeDurable
}

// DefaultCommandOutboxOptions 返回未配置 commandOutbox 时的默认重投参数。
func DefaultCommandOutboxOptions() CommandOutboxOptions {
	return CommandOutboxOptions{
		PollInterval:  DefaultCommandOutboxPollInterval,
		BatchSize:     DefaultCommandOutboxBatchSize,
		MaxAttempts:   DefaultCommandOutboxMaxAttempts,
		LeaseDuration: DefaultCommandOutboxLeaseDuration,
		RetryBase:     DefaultCommandOutboxRetryBase,
		RetryMax:      DefaultCommandOutboxRetryMax,
		ExpireAfter:   DefaultCommandOutboxExpireAfter,
		InitialWait:   DefaultCommandOutboxInitialWait,
	}
}

// ParseCommandOutbox 解析并校验 commandOutbox 配置。
// 空字符串视为未配置，取默认值；显式 0 或负数视为非法。
func (c *Config) ParseCommandOutbox() (CommandOutboxOptions, error) {
	options := DefaultCommandOutboxOptions()
	if c == nil || c.CommandOutbox == nil {
		return options, nil
	}
	conf := c.CommandOutbox
	switch mode := strings.ToLower(strings.TrimSpace(conf.Mode)); mode {
	case "", CommandOutboxModeOff, CommandOutboxModeMemory, CommandOutboxModeDurable:
		options.Mode = mode
	default:
		return CommandOutboxOptions{}, fmt.Errorf(
			"commandOutbox.mode must be one of off, memory, durable, got %q", conf.Mode)
	}
	parseDuration := func(name, raw string, fallback time.Duration) (time.Duration, error) {
		if strings.TrimSpace(raw) == "" {
			return fallback, nil
		}
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return 0, fmt.Errorf("commandOutbox.%s must be a positive duration (e.g. 10s, 5m), got %q", name, raw)
		}
		return parsed, nil
	}
	var err error
	if options.PollInterval, err = parseDuration("pollInterval", conf.PollInterval, options.PollInterval); err != nil {
		return CommandOutboxOptions{}, err
	}
	if options.LeaseDuration, err = parseDuration("leaseDuration", conf.LeaseDuration, options.LeaseDuration); err != nil {
		return CommandOutboxOptions{}, err
	}
	if options.RetryBase, err = parseDuration("retryBase", conf.RetryBase, options.RetryBase); err != nil {
		return CommandOutboxOptions{}, err
	}
	if options.RetryMax, err = parseDuration("retryMax", conf.RetryMax, options.RetryMax); err != nil {
		return CommandOutboxOptions{}, err
	}
	if options.ExpireAfter, err = parseDuration("expireAfter", conf.ExpireAfter, options.ExpireAfter); err != nil {
		return CommandOutboxOptions{}, err
	}
	if options.InitialWait, err = parseDuration("initialWait", conf.InitialWait, options.InitialWait); err != nil {
		return CommandOutboxOptions{}, err
	}
	if conf.BatchSize < 0 {
		return CommandOutboxOptions{}, fmt.Errorf("commandOutbox.batchSize must be >= 0, got %d", conf.BatchSize)
	}
	if conf.BatchSize > 0 {
		options.BatchSize = conf.BatchSize
	}
	if conf.MaxAttempts < 0 {
		return CommandOutboxOptions{}, fmt.Errorf("commandOutbox.maxAttempts must be >= 0, got %d", conf.MaxAttempts)
	}
	if conf.MaxAttempts > 0 {
		options.MaxAttempts = conf.MaxAttempts
	}
	if options.RetryMax < options.RetryBase {
		return CommandOutboxOptions{}, fmt.Errorf("commandOutbox.retryMax must be >= retryBase, got %q < %q",
			options.RetryMax, options.RetryBase)
	}
	return options, nil
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
