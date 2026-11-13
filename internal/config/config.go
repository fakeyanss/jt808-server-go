package config

import (
	"bytes"
	"fmt"
	"math"
	"net"
	neturl "net/url"
	"os"
	"path/filepath"
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
	Name string `yaml:"name"`
	// Mode 选择运行形态：standalone（缺省，四角色同进程）或 cluster（按 cluster.roles 拆进程）。
	// cluster 下必须配合 cluster 段使用；显式配置 cluster.roles 而 mode=standalone 视为矛盾配置。
	Mode               string      `yaml:"mode"`
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
	Database      *databaseConf      `yaml:"database"`
	Retention     *retentionConf     `yaml:"retention"`
	AlarmServer   *alarmServerConf   `yaml:"alarmServer"`
	MediaServer   *mediaServerConf   `yaml:"mediaServer"`
	MediaUpload   *mediaUploadConf   `yaml:"mediaUpload"`
	Admin         *adminConf         `yaml:"admin"`
	API           *apiConf           `yaml:"api"`
	CommandOutbox *commandOutboxConf `yaml:"commandOutbox"`
	Uplink        *uplinkConf        `yaml:"uplink"`
	Cluster       *clusterConf       `yaml:"cluster"`
	Probes        *probesConf        `yaml:"probes"`
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
// sink 选择上行出口：local（默认，直连内置处理）或 http（HTTP hook）。
type uplinkConf struct {
	Sink        string                 `yaml:"sink"`
	HTTP        *uplinkHTTPConf        `yaml:"http"`
	Reliability *uplinkReliabilityConf `yaml:"reliability"`
}

// uplinkHTTPConf 配置 HTTP 上行出口。仅当 sink=http 时生效。
type uplinkHTTPConf struct {
	URL         string `yaml:"url"`
	Token       string `yaml:"token"`
	TimeoutMS   int    `yaml:"timeoutMs"`
	QueueSize   int    `yaml:"queueSize"`
	Workers     int    `yaml:"workers"`
	MaxAttempts int    `yaml:"maxAttempts"`
	GatewayID   string `yaml:"gatewayId"`
	// AuthGraceMS 是网关只读鉴权的宽限期（毫秒）：缓存过期后 control 不可达时，
	// 在该窗口内继续按缓存档案放行；0 表示使用默认值。
	AuthGraceMS int `yaml:"authGraceMs"`
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

	fields := []struct {
		name  string
		value *string
	}{
		{"default", &options.Default},
		{"location", &options.Location},
		{"batchLocation", &options.BatchLocation},
		{"alarm", &options.Alarm},
	}
	for _, field := range fields {
		normalized, ok := normalizeUplinkReliability(*field.value)
		if !ok {
			return UplinkOptions{}, fmt.Errorf(
				"uplink.reliability.%s must be best_effort or at_least_once, got %q", field.name, *field.value)
		}
		*field.value = normalized
	}

	return options, nil
}

// normalizeUplinkReliability 统一可靠性取值。
// confirm_before_ack 与 at_least_once 等价，前者是出口契约里的语义名。
func normalizeUplinkReliability(value string) (string, bool) {
	switch normalized := strings.ToLower(strings.TrimSpace(value)); normalized {
	case UplinkReliabilityBestEffort:
		return UplinkReliabilityBestEffort, true
	case UplinkReliabilityAtLeastOnce, "confirm_before_ack", "confirm-before-ack":
		return UplinkReliabilityAtLeastOnce, true
	default:
		return "", false
	}
}

// 上行出口模式与 HTTP hook 的工程默认值。
const (
	UplinkSinkLocal = "local"
	UplinkSinkHTTP  = "http"

	DefaultUplinkSinkMode    = UplinkSinkLocal
	DefaultUplinkHTTPTimeout = 3 * time.Second
	DefaultUplinkHTTPQueue   = 1024
	DefaultUplinkHTTPWorkers = 4
	// DefaultUplinkHTTPAttempts 为单条事件的投递次数上限（含首次）。
	// 默认不重试：confirm-before-ack 的重试会成倍放大应答时延，
	// 必须严格小于终端重传窗口。
	DefaultUplinkHTTPAttempts = 1
	// DefaultUplinkAuthGrace 是网关只读鉴权的默认宽限期：
	// 缓存过期后 control 不可达时，最多再按缓存档案放行这么久。
	DefaultUplinkAuthGrace = 30 * time.Minute
)

// UplinkSinkOptions 是上行出口的归一化配置。
type UplinkSinkOptions struct {
	Mode string
	HTTP UplinkHTTPOptions
}

// UplinkHTTPOptions 是 HTTP 上行出口的归一化配置。
type UplinkHTTPOptions struct {
	URL       string
	Token     string
	Timeout   time.Duration
	QueueSize int
	// Workers 是 phone 分片数：每个分片单 goroutine 顺序消费，
	// 同一 phone 的事件固定落在同一分片。
	Workers     int
	MaxAttempts int
	GatewayID   string
	// AuthGrace 是缓存过期后 control 不可达时的有限放行窗口。
	AuthGrace time.Duration
}

// DefaultUplinkSinkOptions 返回默认上行出口：本地直连内置处理，不产生外部依赖。
func DefaultUplinkSinkOptions() UplinkSinkOptions {
	return UplinkSinkOptions{
		Mode: DefaultUplinkSinkMode,
		HTTP: UplinkHTTPOptions{
			Timeout:     DefaultUplinkHTTPTimeout,
			QueueSize:   DefaultUplinkHTTPQueue,
			Workers:     DefaultUplinkHTTPWorkers,
			MaxAttempts: DefaultUplinkHTTPAttempts,
			AuthGrace:   DefaultUplinkAuthGrace,
		},
	}
}

// ParseUplinkSink 归一化上行出口配置。
// 未配置时返回 local；非法配置直接报错，不做静默降级。
func (c *Config) ParseUplinkSink() (UplinkSinkOptions, error) {
	options := DefaultUplinkSinkOptions()
	if c == nil || c.Uplink == nil {
		return options, nil
	}

	mode := strings.ToLower(strings.TrimSpace(c.Uplink.Sink))
	if mode == "" {
		mode = DefaultUplinkSinkMode
	}
	if mode != UplinkSinkLocal && mode != UplinkSinkHTTP {
		return UplinkSinkOptions{}, fmt.Errorf(
			"uplink.sink must be %s or %s, got %q", UplinkSinkLocal, UplinkSinkHTTP, c.Uplink.Sink)
	}
	options.Mode = mode
	if mode == UplinkSinkLocal {
		return options, nil
	}

	conf := c.Uplink.HTTP
	if conf == nil {
		return UplinkSinkOptions{}, fmt.Errorf("uplink.http is required when uplink.sink=%s", UplinkSinkHTTP)
	}

	url := strings.TrimSpace(conf.URL)
	parsed, err := neturl.Parse(url)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return UplinkSinkOptions{}, fmt.Errorf("uplink.http.url must be an absolute http(s) URL, got %q", conf.URL)
	}
	options.HTTP.URL = url

	token := strings.TrimSpace(conf.Token)
	if token == "" {
		return UplinkSinkOptions{}, fmt.Errorf("uplink.http.token is required when uplink.sink=%s", UplinkSinkHTTP)
	}
	options.HTTP.Token = token

	if conf.TimeoutMS != 0 {
		if conf.TimeoutMS < 1 || conf.TimeoutMS > 60000 {
			return UplinkSinkOptions{}, fmt.Errorf("uplink.http.timeoutMs must be in [1, 60000], got %d", conf.TimeoutMS)
		}
		options.HTTP.Timeout = time.Duration(conf.TimeoutMS) * time.Millisecond
	}
	if conf.QueueSize != 0 {
		if conf.QueueSize < 1 || conf.QueueSize > 1000000 {
			return UplinkSinkOptions{}, fmt.Errorf("uplink.http.queueSize must be in [1, 1000000], got %d", conf.QueueSize)
		}
		options.HTTP.QueueSize = conf.QueueSize
	}
	if conf.Workers != 0 {
		if conf.Workers < 1 || conf.Workers > 64 {
			return UplinkSinkOptions{}, fmt.Errorf("uplink.http.workers must be in [1, 64], got %d", conf.Workers)
		}
		options.HTTP.Workers = conf.Workers
	}
	if conf.MaxAttempts != 0 {
		if conf.MaxAttempts < 1 || conf.MaxAttempts > 3 {
			return UplinkSinkOptions{}, fmt.Errorf("uplink.http.maxAttempts must be in [1, 3], got %d", conf.MaxAttempts)
		}
		options.HTTP.MaxAttempts = conf.MaxAttempts
	}
	if conf.AuthGraceMS != 0 {
		if conf.AuthGraceMS < 1 || conf.AuthGraceMS > int((24*time.Hour).Milliseconds()) {
			return UplinkSinkOptions{}, fmt.Errorf("uplink.http.authGraceMs must be in [1, 86400000], got %d", conf.AuthGraceMS)
		}
		options.HTTP.AuthGrace = time.Duration(conf.AuthGraceMS) * time.Millisecond
	}
	options.HTTP.GatewayID = strings.TrimSpace(conf.GatewayID)
	if options.HTTP.GatewayID == "" {
		options.HTTP.GatewayID = defaultGatewayID()
	}
	return options, nil
}

// defaultGatewayID 生成进程级网关标识：主机名 + 进程号。
func defaultGatewayID() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "jt808-gateway"
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}

// defaultGatewayNodeID 生成稳定的网关节点标识：仅主机名。
// 进程重启时 nodeId 不变，由 instanceId 表达实例更替；同机多网关需显式配置 nodeId。
func defaultGatewayNodeID() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		return "jt808-gateway"
	}
	return host
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
	// DefaultAlarmServerSegmentSize 是集群附件单个对象切片大小（8 MiB）。
	DefaultAlarmServerSegmentSize = 8 * 1024 * 1024
	// DefaultAlarmServerStagingMax 是附件节点本地暂存区总量上限（2 GiB）。
	DefaultAlarmServerStagingMax = 2 * 1024 * 1024 * 1024
)

// alarmServerConf 配置苏标主动安全附件服务器。
// port 为 TCP 监听端口，udpPort 为 UDP 监听端口；两者都为空表示不启用。
// 0x9208 通告的端口直接取自这两个监听端口，未配置的一侧以 0 表示不可用，
// 避免通告地址与实际监听不一致。
type alarmServerConf struct {
	Port    string `yaml:"port"`
	UDPPort string `yaml:"udpPort"`
	// HTTPPort 是附件节点内部状态接口端口；集群 role attachment 必填。
	HTTPPort      string `yaml:"httpPort"`
	Dir           string `yaml:"dir"`
	MaxFileSize   int64  `yaml:"maxFileSize"`
	MaxConcurrent int    `yaml:"maxConcurrent"`
	// CompletedTTL 是完成态内存状态的保留时长（如 "20m"）；
	// 显式 "0" 表示不淘汰，留空使用默认值。
	CompletedTTL string `yaml:"completedTTL"`
	// RetentionDays 是已完成附件文件的落盘保留天数；显式 0 表示不清理。
	RetentionDays *int `yaml:"retentionDays"`
	// SegmentSizeBytes 是集群附件切片的单个对象大小；默认 8 MiB。
	// standalone 本地模式忽略该字段。
	SegmentSizeBytes int64 `yaml:"segmentSizeBytes"`
	// StagingMaxBytes 是附件节点本地暂存区总量上限；默认 2 GiB。
	// 到顶后拒绝新的文件登记（返回失败应答），不排队、不写满磁盘。
	StagingMaxBytes int64 `yaml:"stagingMaxBytes"`
	// AutoUpload 控制报警首次上报时是否自动下发 0x9208。
	// 默认 false，保持既有手动下发行为不变。
	AutoUpload bool `yaml:"autoUpload"`
	// UploadIP 是 0x9208 中携带的附件服务器 IP，autoUpload 开启时必填。
	// 端口不单独配置，始终使用 port / udpPort 的实际监听端口。
	UploadIP string `yaml:"uploadIP"`
	// ProgressReport 是集群附件节点向 control 上报进度、查询恢复点的地址。
	// 附件节点不连库，未配置时只能靠对象存储扫描恢复。
	ProgressReport *mediaSegmentReportConf `yaml:"progressReport"`
}

// AlarmServerOptions 是附件服务器的归一化配置。
type AlarmServerOptions struct {
	Port          uint16 // TCP 监听与通告端口，0 表示未启用
	UDPPort       uint16 // UDP 监听与通告端口，0 表示未启用
	HTTPPort      uint16 // 内部状态接口端口，0 表示未启用
	Dir           string
	MaxFileSize   int64
	MaxConcurrent int
	CompletedTTL  time.Duration
	Retention     time.Duration
	AutoUpload    bool
	UploadIP      string
	// SegmentSizeBytes / StagingMaxBytes 只在集群对象存储模式使用。
	SegmentSizeBytes int64
	StagingMaxBytes  int64
	// ProgressReport 是附件进度上报 control 的出站配置；URL 为空表示不上报。
	ProgressReport MediaSegmentReportOptions
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
	httpPort, err := parsePort("httpPort", strings.TrimSpace(conf.HTTPPort))
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
	if conf.SegmentSizeBytes < 0 {
		return AlarmServerOptions{}, false, fmt.Errorf("alarmServer.segmentSizeBytes must be >= 0, got %d", conf.SegmentSizeBytes)
	}
	segmentSize := conf.SegmentSizeBytes
	if segmentSize == 0 {
		segmentSize = DefaultAlarmServerSegmentSize
	}
	if conf.StagingMaxBytes < 0 {
		return AlarmServerOptions{}, false, fmt.Errorf("alarmServer.stagingMaxBytes must be >= 0, got %d", conf.StagingMaxBytes)
	}
	stagingMax := conf.StagingMaxBytes
	if stagingMax == 0 {
		stagingMax = DefaultAlarmServerStagingMax
	}
	retentionDays := DefaultAlarmServerRetentionDays
	if conf.RetentionDays != nil {
		if *conf.RetentionDays < 0 {
			return AlarmServerOptions{}, false, fmt.Errorf("alarmServer.retentionDays must be >= 0, got %d", *conf.RetentionDays)
		}
		retentionDays = *conf.RetentionDays
	}
	uploadIP := strings.TrimSpace(conf.UploadIP)
	progressReport := MediaSegmentReportOptions{Timeout: 3 * time.Second}
	if conf.ProgressReport != nil {
		progressReport.URL = strings.TrimSpace(conf.ProgressReport.URL)
		progressReport.Token = strings.TrimSpace(conf.ProgressReport.Token)
		if progressReport.URL != "" && !strings.HasPrefix(progressReport.URL, "http://") && !strings.HasPrefix(progressReport.URL, "https://") {
			return AlarmServerOptions{}, false, fmt.Errorf("alarmServer.progressReport.url must start with http:// or https://")
		}
		if conf.ProgressReport.TimeoutMS < 0 {
			return AlarmServerOptions{}, false, fmt.Errorf("alarmServer.progressReport.timeoutMs must be >= 0, got %d", conf.ProgressReport.TimeoutMS)
		}
		if conf.ProgressReport.TimeoutMS > 0 {
			progressReport.Timeout = time.Duration(conf.ProgressReport.TimeoutMS) * time.Millisecond
		}
	}
	if conf.AutoUpload {
		if port == 0 && udpPort == 0 {
			// control-only 的自动触发从 cluster.attachment.nodes 取附件节点地址，
			// 不需要本进程监听附件端口，也不要求 uploadIP。
			return AlarmServerOptions{
				Dir:              dir,
				MaxFileSize:      maxFileSize,
				MaxConcurrent:    maxConcurrent,
				CompletedTTL:     completedTTL,
				Retention:        time.Duration(retentionDays) * 24 * time.Hour,
				AutoUpload:       true,
				UploadIP:         uploadIP,
				SegmentSizeBytes: segmentSize,
				StagingMaxBytes:  stagingMax,
				ProgressReport:   progressReport,
			}, true, nil
		}
		if uploadIP == "" {
			return AlarmServerOptions{}, false, fmt.Errorf("alarmServer.uploadIP is required when alarmServer.autoUpload is enabled with a local attachment listener")
		}
		if len([]byte(uploadIP)) > 255 {
			return AlarmServerOptions{}, false, fmt.Errorf("alarmServer.uploadIP must contain at most 255 bytes")
		}
	}
	return AlarmServerOptions{
		Port:             uint16(port),
		UDPPort:          uint16(udpPort),
		HTTPPort:         uint16(httpPort),
		Dir:              dir,
		MaxFileSize:      maxFileSize,
		MaxConcurrent:    maxConcurrent,
		CompletedTTL:     completedTTL,
		Retention:        time.Duration(retentionDays) * 24 * time.Hour,
		AutoUpload:       conf.AutoUpload,
		UploadIP:         uploadIP,
		SegmentSizeBytes: segmentSize,
		StagingMaxBytes:  stagingMax,
		ProgressReport:   progressReport,
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
	Port    string `yaml:"port"`
	TCPPort string `yaml:"tcpPort"`
	// HTTPPort 是媒体节点内部 HTTP（会话管理 + FLV 播放）端口；
	// 未配置时取 UDP 端口 +1。
	HTTPPort string `yaml:"httpPort"`
	// AdvertiseIP 是下行信令与播放地址里填写的媒体节点地址；为空时用 127.0.0.1。
	AdvertiseIP   string `yaml:"advertiseIp"`
	Dir           string `yaml:"dir"`
	MaxConcurrent int    `yaml:"maxConcurrent"`
	MaxTCPConns   int    `yaml:"maxTCPConns"`
	// RetentionDays 是音视频文件的落盘保留天数；显式 0 表示不清理。
	RetentionDays *int               `yaml:"retentionDays"`
	Record        *mediaRecordConf   `yaml:"record"`
	Playback      *mediaPlaybackConf `yaml:"playback"`
	RingBytes     int64              `yaml:"ringBytes"`
	RingSeconds   int                `yaml:"ringSeconds"`
	// MaxBitrateKbps 是节点入流码率容量，心跳上报后 control 据此判断容量；
	// 0 表示不按码率限制。
	MaxBitrateKbps int64 `yaml:"maxBitrateKbps"`
	// SegmentReport 是切片完成后向 control 出站上报的地址；为空时不上报。
	SegmentReport *mediaSegmentReportConf `yaml:"segmentReport"`
}

type mediaSegmentReportConf struct {
	URL       string `yaml:"url"`
	Token     string `yaml:"token"`
	TimeoutMS int    `yaml:"timeoutMs"`
}

// mediaPlaybackConf 配置播放出口。
//
// output=flv 使用进程内 HTTP-FLV（轻量默认）；rtmp 转推 SRS/lal 由其分发；
// both 两种出口并联，互不阻塞。桥接模式下必须保持 SRS 的 gop_cache 开启。
type mediaPlaybackConf struct {
	Output string         `yaml:"output"`
	RTMP   *mediaRTMPConf `yaml:"rtmp"`
}

type mediaRTMPConf struct {
	URL           string `yaml:"url"`
	StreamPrefix  string `yaml:"streamPrefix"`
	PlayURLPrefix string `yaml:"playUrlPrefix"`
	DialTimeout   int    `yaml:"dialTimeoutSeconds"`
	WriteTimeout  int    `yaml:"writeTimeoutSeconds"`
	QueueFrames   int    `yaml:"queueFrames"`
}

// mediaRecordConf 配置录制行为。
//
// 直播本身不落盘；record.mode=off 关闭录制，legacy 保持旧的持续落盘，
// segmented 按周期切片后上传对象存储。
type mediaRecordConf struct {
	Mode           string `yaml:"mode"`
	SegmentMinutes int    `yaml:"segmentMinutes"`
}

// MediaServerOptions 是媒体接入服务器的归一化配置。
type MediaServerOptions struct {
	Port          uint16 // UDP/TCP 端口，0 表示不启用
	HTTPPort      uint16 // 媒体节点 HTTP 端口
	AdvertiseIP   string
	Dir           string
	MaxConcurrent int
	MaxTCPConns   int
	Retention     time.Duration
	RecordMode    string
	// RecordModeExplicit 表示配置里显式写了 record.mode，用于 cluster/standalone
	// 默认值差异的告警。
	RecordModeExplicit bool
	SegmentDuration    time.Duration
	RingBytes          int64
	RingDuration       time.Duration
	// MaxBitrateKbps 是入流码率容量；0 表示不限。
	MaxBitrateKbps int64
	// Playback 是播放出口配置；Output 默认 flv。
	Playback PlaybackOptions
	// SegmentReport 是切片索引出站上报配置。
	SegmentReport MediaSegmentReportOptions
}

// MediaSegmentReportOptions 是切片上报 control 的归一化配置。
type MediaSegmentReportOptions struct {
	URL     string
	Token   string
	Timeout time.Duration
}

// PlaybackOptions 是播放输出的归一化配置。
type PlaybackOptions struct {
	Output        string // flv | rtmp | both
	RTMPURL       string
	StreamPrefix  string
	PlayURLPrefix string
	DialTimeout   time.Duration
	WriteTimeout  time.Duration
	QueueFrames   int
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
	if udpPort != 0 && tcpPort != 0 && udpPort != tcpPort {
		return MediaServerOptions{}, false, fmt.Errorf(
			"mediaServer.port and mediaServer.tcpPort must be the same value (single-port mode), got %d and %d",
			udpPort, tcpPort)
	}
	port := udpPort
	if port == 0 {
		port = tcpPort
	}
	httpPort, err := parseMediaPort("mediaServer.httpPort", conf.HTTPPort, "HTTP")
	if err != nil {
		return MediaServerOptions{}, false, err
	}
	if httpPort == 0 {
		if port < 65535 {
			httpPort = port + 1
		}
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
	recordMode := "legacy"
	explicit := false
	segmentMinutes := 10
	if conf.Record != nil {
		if mode := strings.ToLower(strings.TrimSpace(conf.Record.Mode)); mode != "" {
			switch mode {
			case "off", "legacy", "segmented":
				recordMode = mode
				explicit = true
			default:
				return MediaServerOptions{}, false, fmt.Errorf("mediaServer.record.mode must be off, legacy or segmented, got %q", mode)
			}
		}
		if conf.Record.SegmentMinutes < 0 {
			return MediaServerOptions{}, false, fmt.Errorf("mediaServer.record.segmentMinutes must be >= 0, got %d", conf.Record.SegmentMinutes)
		}
		if conf.Record.SegmentMinutes > 0 {
			segmentMinutes = conf.Record.SegmentMinutes
		}
	}
	if conf.RingBytes < 0 {
		return MediaServerOptions{}, false, fmt.Errorf("mediaServer.ringBytes must be >= 0, got %d", conf.RingBytes)
	}
	if conf.RingSeconds < 0 {
		return MediaServerOptions{}, false, fmt.Errorf("mediaServer.ringSeconds must be >= 0, got %d", conf.RingSeconds)
	}
	ringBytes := conf.RingBytes
	if ringBytes == 0 {
		ringBytes = 32 * 1024 * 1024
	}
	ringSeconds := conf.RingSeconds
	if ringSeconds == 0 {
		ringSeconds = 60
	}
	playback, err := parsePlaybackOptions(conf.Playback)
	if err != nil {
		return MediaServerOptions{}, false, err
	}
	report := MediaSegmentReportOptions{Timeout: 3 * time.Second}
	if conf.SegmentReport != nil {
		report.URL = strings.TrimSpace(conf.SegmentReport.URL)
		report.Token = strings.TrimSpace(conf.SegmentReport.Token)
		if report.URL != "" && !strings.HasPrefix(report.URL, "http://") && !strings.HasPrefix(report.URL, "https://") {
			return MediaServerOptions{}, false, fmt.Errorf("mediaServer.segmentReport.url must start with http:// or https://")
		}
		if conf.SegmentReport.TimeoutMS < 0 {
			return MediaServerOptions{}, false, fmt.Errorf("mediaServer.segmentReport.timeoutMs must be >= 0, got %d", conf.SegmentReport.TimeoutMS)
		}
		if conf.SegmentReport.TimeoutMS > 0 {
			report.Timeout = time.Duration(conf.SegmentReport.TimeoutMS) * time.Millisecond
		}
	}
	if conf.MaxBitrateKbps < 0 {
		return MediaServerOptions{}, false, fmt.Errorf("mediaServer.maxBitrateKbps must be >= 0, got %d", conf.MaxBitrateKbps)
	}
	return MediaServerOptions{
		Port:               port,
		HTTPPort:           httpPort,
		AdvertiseIP:        strings.TrimSpace(conf.AdvertiseIP),
		Dir:                dir,
		MaxConcurrent:      maxConcurrent,
		MaxTCPConns:        maxTCPConns,
		Retention:          time.Duration(retentionDays) * 24 * time.Hour,
		RecordMode:         recordMode,
		RecordModeExplicit: explicit,
		SegmentDuration:    time.Duration(segmentMinutes) * time.Minute,
		RingBytes:          ringBytes,
		RingDuration:       time.Duration(ringSeconds) * time.Second,
		MaxBitrateKbps:     conf.MaxBitrateKbps,
		Playback:           playback,
		SegmentReport:      report,
	}, true, nil
}

// parsePlaybackOptions 解析播放输出配置并做组合校验。
func parsePlaybackOptions(conf *mediaPlaybackConf) (PlaybackOptions, error) {
	options := PlaybackOptions{Output: "flv"}
	if conf == nil {
		return options, nil
	}
	if mode := strings.ToLower(strings.TrimSpace(conf.Output)); mode != "" {
		switch mode {
		case "flv", "rtmp", "both":
			options.Output = mode
		default:
			return options, fmt.Errorf("mediaServer.playback.output must be flv, rtmp or both, got %q", mode)
		}
	}
	if conf.RTMP == nil {
		if options.Output == "rtmp" || options.Output == "both" {
			return options, fmt.Errorf("mediaServer.playback.rtmp is required when playback.output=%s", options.Output)
		}
		return options, nil
	}
	rtmp := conf.RTMP
	options.RTMPURL = strings.TrimSpace(rtmp.URL)
	options.StreamPrefix = strings.TrimSpace(rtmp.StreamPrefix)
	options.PlayURLPrefix = strings.TrimRight(strings.TrimSpace(rtmp.PlayURLPrefix), "/")
	if options.Output == "rtmp" || options.Output == "both" {
		if options.RTMPURL == "" {
			return options, fmt.Errorf("mediaServer.playback.rtmp.url is required when playback.output=%s", options.Output)
		}
		if !strings.HasPrefix(options.RTMPURL, "rtmp://") {
			return options, fmt.Errorf("mediaServer.playback.rtmp.url must start with rtmp://, got %q", options.RTMPURL)
		}
	}
	if rtmp.DialTimeout < 0 || rtmp.WriteTimeout < 0 {
		return options, fmt.Errorf("mediaServer.playback.rtmp timeouts must be >= 0")
	}
	options.DialTimeout = time.Duration(rtmp.DialTimeout) * time.Second
	options.WriteTimeout = time.Duration(rtmp.WriteTimeout) * time.Second
	if rtmp.QueueFrames < 0 {
		return options, fmt.Errorf("mediaServer.playback.rtmp.queueFrames must be >= 0, got %d", rtmp.QueueFrames)
	}
	options.QueueFrames = rtmp.QueueFrames
	return options, nil
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

// ParseProbes 解析独立的存活/就绪探针监听地址。
// 未配置时返回 enabled=false：通用入口保持既有行为，不新增监听。
func (c *Config) ParseProbes() (addr string, enabled bool, err error) {
	if c == nil || c.Probes == nil {
		return "", false, nil
	}
	addr = strings.TrimSpace(c.Probes.Addr)
	if addr == "" {
		return "", false, nil
	}
	if err := validateListenAddr("probes.addr", addr); err != nil {
		return "", false, err
	}
	return addr, true, nil
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

	// 孤儿文件扫描的工程默认值。显式开启后才执行，默认关闭。
	// 宽限期 7 天远大于正常补传/重传窗口，避免把正在补传的旧目录误判为孤儿。
	DefaultOrphanGraceDays      = 7
	DefaultOrphanScanInterval   = 24 * time.Hour
	DefaultOrphanMaxFilesPerRun = 10000
	DefaultOrphanBatchSize      = 500
	// MaxOrphanFilesPerRun 限制单次扫描的文件数上限，避免一次遍历打满 IO。
	MaxOrphanFilesPerRun = 1000000
)

// retentionConf 配置持久化数据的保留策略。
// 字段使用指针以区分“未配置”（取默认值）与“显式 0”（关闭该策略）。
type retentionConf struct {
	Locations          *locationRetentionConf `yaml:"locations"`
	Alarms             *alarmRetentionConf    `yaml:"alarms"`
	MediaFiles         *timeRetentionConf     `yaml:"mediaFiles"`
	MediaSegments      *timeRetentionConf     `yaml:"mediaSegments"`
	MediaCaptures      *timeRetentionConf     `yaml:"mediaCaptures"`
	MediaSearchResults *timeRetentionConf     `yaml:"mediaSearchResults"`
	OrphanFiles        *orphanFilesConf       `yaml:"orphanFiles"`
	Cleanup            *cleanupConf           `yaml:"cleanup"`
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

// orphanFilesConf 配置媒体目录孤儿文件扫描。
// 默认关闭；只有 enabled 显式为 true 才启动。判定依据是文件自身 mtime，
// 不使用 <YYYYMMDD> 目录名，避免把补传到旧目录的新文件误删。
type orphanFilesConf struct {
	Enabled        *bool  `yaml:"enabled"`
	GraceDays      *int   `yaml:"graceDays"`
	Interval       string `yaml:"interval"`
	MaxFilesPerRun *int   `yaml:"maxFilesPerRun"`
	BatchSize      *int   `yaml:"batchSize"`
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
	Locations          LocationRetention
	Alarms             AlarmRetention
	MediaFiles         TimeRetention
	MediaSegments      TimeRetention
	MediaCaptures      TimeRetention
	MediaSearchResults TimeRetention
	OrphanFiles        OrphanFilesPolicy
	Cleanup            RetentionCleanupPolicy
}

// OrphanFilesPolicy 是孤儿文件扫描的归一化配置。
// Grace 是文件 mtime 的最小年龄，只有早于 now-Grace 的文件才可能被删除。
type OrphanFilesPolicy struct {
	Enabled        bool
	Grace          time.Duration
	Interval       time.Duration
	MaxFilesPerRun int
	BatchSize      int
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
		OrphanFiles: OrphanFilesPolicy{
			Grace:          time.Duration(DefaultOrphanGraceDays) * 24 * time.Hour,
			Interval:       DefaultOrphanScanInterval,
			MaxFilesPerRun: DefaultOrphanMaxFilesPerRun,
			BatchSize:      DefaultOrphanBatchSize,
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
	// 录像切片默认跟随多媒体文件保留期；显式配置 mediaSegments 时以它为准。
	policy.MediaSegments.MaxAge = policy.MediaFiles.MaxAge
	if segment := c.Retention.MediaSegments; segment != nil && segment.RetentionDays != nil {
		policy.MediaSegments.MaxAge = time.Duration(*segment.RetentionDays) * 24 * time.Hour
	}
	if capture := c.Retention.MediaCaptures; capture != nil && capture.RetentionDays != nil {
		policy.MediaCaptures.MaxAge = time.Duration(*capture.RetentionDays) * 24 * time.Hour
	}
	if search := c.Retention.MediaSearchResults; search != nil && search.RetentionDays != nil {
		policy.MediaSearchResults.MaxAge = time.Duration(*search.RetentionDays) * 24 * time.Hour
	}
	if orphan := c.Retention.OrphanFiles; orphan != nil {
		if orphan.Enabled != nil {
			policy.OrphanFiles.Enabled = *orphan.Enabled
		}
		if orphan.GraceDays != nil {
			policy.OrphanFiles.Grace = time.Duration(*orphan.GraceDays) * 24 * time.Hour
		}
		if raw := strings.TrimSpace(orphan.Interval); raw != "" {
			if interval, err := time.ParseDuration(raw); err == nil {
				policy.OrphanFiles.Interval = interval
			}
		}
		if orphan.MaxFilesPerRun != nil && *orphan.MaxFilesPerRun > 0 {
			policy.OrphanFiles.MaxFilesPerRun = *orphan.MaxFilesPerRun
		}
		if orphan.BatchSize != nil && *orphan.BatchSize > 0 {
			policy.OrphanFiles.BatchSize = *orphan.BatchSize
		}
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
	if search := c.Retention.MediaSearchResults; search != nil {
		if err := validateRetentionDays("retention.mediaSearchResults.retentionDays", search.RetentionDays); err != nil {
			return err
		}
	}
	if orphan := c.Retention.OrphanFiles; orphan != nil {
		if orphan.GraceDays != nil {
			if *orphan.GraceDays < 1 {
				return fmt.Errorf("retention.orphanFiles.graceDays must be >= 1, got %d", *orphan.GraceDays)
			}
			if *orphan.GraceDays > MaxRetentionDays {
				return fmt.Errorf("retention.orphanFiles.graceDays must be <= %d, got %d",
					MaxRetentionDays, *orphan.GraceDays)
			}
		}
		if raw := strings.TrimSpace(orphan.Interval); raw != "" {
			interval, err := time.ParseDuration(raw)
			if err != nil || interval <= 0 {
				return fmt.Errorf("retention.orphanFiles.interval must be a positive duration (e.g. 24h), got %q", raw)
			}
		}
		if orphan.MaxFilesPerRun != nil &&
			(*orphan.MaxFilesPerRun < 1 || *orphan.MaxFilesPerRun > MaxOrphanFilesPerRun) {
			return fmt.Errorf("retention.orphanFiles.maxFilesPerRun must be between 1 and %d, got %d",
				MaxOrphanFilesPerRun, *orphan.MaxFilesPerRun)
		}
		if orphan.BatchSize != nil && (*orphan.BatchSize < 0 || *orphan.BatchSize > MaxRetentionCleanupBatchSize) {
			return fmt.Errorf("retention.orphanFiles.batchSize must be between 0 and %d, got %d",
				MaxRetentionCleanupBatchSize, *orphan.BatchSize)
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
	DefaultCommandOutboxOfflineRetry  = 15 * time.Second
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
	// OfflineRetry 是设备离线时的短退避，避免每个 poll tick 空转。
	OfflineRetry string `yaml:"offlineRetry"`
	ExpireAfter  string `yaml:"expireAfter"`
	InitialWait  string `yaml:"initialWait"`
	// RetryOnTerminalFailure 决定终端明确执行失败（Result=1）后是否重投。
	// 默认 false：终端已执行过，重投可能重复副作用；Result=2/3 始终不重投。
	RetryOnTerminalFailure bool `yaml:"retryOnTerminalFailure"`
}

// CommandOutboxOptions 是重投调度的归一化配置。
type CommandOutboxOptions struct {
	Mode                   string
	PollInterval           time.Duration
	BatchSize              int
	MaxAttempts            int
	LeaseDuration          time.Duration
	RetryBase              time.Duration
	RetryMax               time.Duration
	OfflineRetry           time.Duration
	ExpireAfter            time.Duration
	InitialWait            time.Duration
	RetryOnTerminalFailure bool
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
		OfflineRetry:  DefaultCommandOutboxOfflineRetry,
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
	if options.OfflineRetry, err = parseDuration("offlineRetry", conf.OfflineRetry, options.OfflineRetry); err != nil {
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
	options.RetryOnTerminalFailure = conf.RetryOnTerminalFailure
	return options, nil
}

// 运行形态与角色。standalone 是缺省且零外部依赖；cluster 按 roles 拆进程。
const (
	ServerModeStandalone = "standalone"
	ServerModeCluster    = "cluster"

	RoleGateway    = "gateway"
	RoleControl    = "control"
	RoleMedia      = "media"
	RoleAttachment = "attachment"
)

// control 的下行路由模式。directory 按会话目录定位归属网关；
// fixed 固定发往 cluster.gateway.url，只用于单节点部署。
const (
	GatewayRoutingDirectory = "directory"
	GatewayRoutingFixed     = "fixed"
)

// 文件存储后端。local 只用于 standalone；sharedVolume 是过渡形态；objectStore 是正式形态。
// 配置值大小写不敏感，内部统一为小写规范值。
const (
	BlobBackendLocal        = "local"
	BlobBackendSharedVolume = "sharedvolume"
	BlobBackendObjectStore  = "objectstore"
)

const (
	DefaultClusterDownlinkTimeout  = 8 * time.Second
	DefaultClusterDownlinkIDTTL    = 2 * time.Minute
	DefaultClusterDownlinkMaxInFly = 256
	DefaultClusterUplinkDedupTTL   = 10 * time.Minute
	DefaultClusterUplinkDedupMax   = 100000
	DefaultClusterUplinkMaxBody    = 8 * 1024 * 1024
	DefaultClusterUplinkMaxRequest = 12 * 1024 * 1024
	DefaultClusterUplinkMaxDecoded = 8 * 1024 * 1024
	DefaultClusterStorageRoot      = "./data/blobs"
	DefaultClusterStorageMarker    = ".jt808-shared-volume"
	// DefaultClusterLeaseTolerance 是跨副本抢租约的保守容差：
	// 接管条件为 expires_at + tolerance < now，宁可晚夺不可早夺。
	DefaultClusterLeaseTolerance = 2 * time.Second
)

// clusterConf 配置角色拆分与两个内部 HTTP 契约。
type clusterConf struct {
	Roles   []string            `yaml:"roles"`
	Gateway *clusterGatewayConf `yaml:"gateway"`
	Control *clusterControlConf `yaml:"control"`
	Media   *clusterMediaConf   `yaml:"media"`
	// Attachment 是控制面可选的附件节点清单；附件角色自己监听 TCP/UDP。
	Attachment *clusterAttachmentConf `yaml:"attachment"`
	Downlink   *clusterDownlinkConf   `yaml:"downlink"`
	Uplink     *clusterUplinkConf     `yaml:"uplink"`
	Storage    *clusterStorageConf    `yaml:"storage"`
	// AllowEmbeddedStorage 是仅用于本地开发联调的开关：允许集群角色共用
	// 同一个嵌入式 SQLite 文件代替共享 MySQL。
	// 生产环境禁止开启；开启时启动打印 WARN，/readyz 返回 degraded。
	AllowEmbeddedStorage bool `yaml:"allowEmbeddedStorage"`
	// LeaseTolerance 是跨副本抢租约的容差，缺省 2s。
	LeaseTolerance string `yaml:"leaseTolerance"`
}

type clusterGatewayConf struct {
	// Addr 是网关侧下行 HTTP 入口的监听地址。
	Addr string `yaml:"addr"`
	// Routing 是 control 的下行模式：directory 按会话目录路由，fixed 固定发往 URL。
	Routing string `yaml:"routing"`
	// URL 是 fixed 模式下唯一的下行入口基址；directory 模式禁止配置。
	URL string `yaml:"url"`
	// NodeID 是网关节点标识，必须全局唯一；为空时默认主机名。
	NodeID string `yaml:"nodeId"`
	// AdvertiseURL 是网关告诉 control 的下行入口地址；为空时回退到 URL。
	AdvertiseURL string `yaml:"advertiseUrl"`
	// Token 是 control -> gateway 的共享密钥；gateway 校验、control 发送。
	Token string `yaml:"token"`
}

type clusterControlConf struct {
	// Addr 是管控侧上行 HTTP 入口的监听地址。
	Addr string `yaml:"addr"`
	// Token 是 gateway -> control 的共享密钥；control 校验、gateway 发送。
	// 同一方向上 gateway 的 uplink.http.token 必须与本值一致。
	Token string `yaml:"token"`
}

// clusterMediaConf 配置媒体节点侧标识与管控侧的节点清单。
//
// nodeId 由媒体节点在 /internal/media/status 中回报；nodes 是管控挑选媒体节点的
// 唯一依据，地址必须是媒体节点内部 HTTP 的基址（即 mediaServer.httpPort）。
type clusterMediaConf struct {
	NodeID string                 `yaml:"nodeId"`
	Nodes  []clusterMediaNodeConf `yaml:"nodes"`
	// ControlURL 是媒体节点注册与心跳的 control 基址；
	// 为空时从 mediaServer.segmentReport.url 反推。
	ControlURL string `yaml:"controlUrl"`
	// Token 是 media -> control 的内部令牌；为空时回退 segmentReport.token。
	Token string `yaml:"token"`
	// AdvertiseURL 是 control 可达的本节点内部 HTTP 基址；
	// 为空时按 mediaServer.advertiseIp + mediaServer.httpPort 推导。
	AdvertiseURL string `yaml:"advertiseUrl"`
}

type clusterMediaNodeConf struct {
	// Name 是节点在日志与错误信息里的可读名字；为空时按序号生成。
	Name string `yaml:"name"`
	// URL 是媒体节点内部 HTTP 基址，例如 http://10.0.0.12:7201。
	URL string `yaml:"url"`
}

// clusterAttachmentConf 配置附件节点清单。
//
// nodes 是控制面选择附件节点的唯一依据；地址必须是附件节点内部 HTTP 基址
// （默认 7402），节点通过 /internal/attachment/status 通告 TCP/UDP 端口。
type clusterAttachmentConf struct {
	// NodeID 是附件节点在状态接口中回报的稳定标识；为空时按通告地址生成。
	NodeID string                      `yaml:"nodeId"`
	Nodes  []clusterAttachmentNodeConf `yaml:"nodes"`
	// AdvertiseURL 是本节点登记到 control 的内部 HTTP 基址，用于接收 0x9207 推送；
	// 为空时按 ftp1078.advertiseIp + alarmServer.httpPort 推导。
	AdvertiseURL string `yaml:"advertiseUrl"`
	// FTP 配置 1078 文件上传 FTP 服务；缺省关闭。
	FTP *clusterFTPConf `yaml:"ftp1078"`
}

type clusterFTPConf struct {
	// Port 是 FTP 控制连接端口，默认 7500。
	Port uint16 `yaml:"port"`
	// PassivePorts 是被动数据连接端口段，例如 "7501-7510"。
	PassivePorts string `yaml:"passivePorts"`
	// AdvertiseIP 是 0x9206/PASV 通告给终端的地址；为空时取控制连接本地地址。
	AdvertiseIP string `yaml:"advertiseIp"`
}

type clusterAttachmentNodeConf struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}

// probesConf 配置独立的存活/就绪探针监听。
// 只暴露 /healthz 与 /readyz，不承载任何业务接口或鉴权。
type probesConf struct {
	Addr string `yaml:"addr"`
}

type clusterDownlinkConf struct {
	TimeoutMS int    `yaml:"timeoutMs"`
	IDTTL     string `yaml:"idempotencyTTL"`
	MaxInFly  int    `yaml:"maxInFlight"`
}

type clusterUplinkConf struct {
	DedupTTL        string `yaml:"dedupTTL"`
	DedupMaxEntries int    `yaml:"dedupMaxEntries"`
	// MaxBodyBytes 限制解码后的原始消息体（网关投递前、管控解码后各校验一次）。
	MaxBodyBytes int `yaml:"maxBodyBytes"`
	// MaxRequestBytes 限制 HTTP 请求体（含 bodyBase64 的 4/3 膨胀），是 413 的判定依据。
	MaxRequestBytes int `yaml:"maxRequestBytes"`
	// MaxDecodedBodyBytes 是管控解码后的二次校验上限。
	MaxDecodedBodyBytes int `yaml:"maxDecodedBodyBytes"`
}

type clusterStorageConf struct {
	Backend string `yaml:"backend"`
	// SharedVolume 必须显式为 true，避免把本机独占目录误当成共享卷。
	SharedVolume bool                    `yaml:"sharedVolume"`
	Marker       string                  `yaml:"marker"`
	Root         string                  `yaml:"root"`
	ObjectStore  *clusterObjectStoreConf `yaml:"objectStore"`
}

// clusterObjectStoreConf 配置 S3 兼容对象存储；不绑定厂商。
type clusterObjectStoreConf struct {
	Endpoint  string `yaml:"endpoint"`
	Region    string `yaml:"region"`
	Bucket    string `yaml:"bucket"`
	AccessKey string `yaml:"accessKey"`
	SecretKey string `yaml:"secretKey"`
	// PathStyle 缺省 true，兼容 MinIO 等自建存储。
	PathStyle *bool  `yaml:"pathStyle"`
	Prefix    string `yaml:"prefix"`
	Retries   int    `yaml:"retries"`
}

// RoleSet 是角色集合。
type RoleSet map[string]bool

// Has 判断角色是否启用。
func (r RoleSet) Has(role string) bool { return r != nil && r[role] }

// List 按固定顺序返回已启用角色，保证日志稳定。
func (r RoleSet) List() []string {
	order := []string{RoleGateway, RoleControl, RoleMedia, RoleAttachment}
	roles := make([]string, 0, len(r))
	for _, role := range order {
		if r.Has(role) {
			roles = append(roles, role)
		}
	}
	return roles
}

// ClusterOptions 是 server.mode 与 cluster.* 的归一化配置。
type ClusterOptions struct {
	Mode    string
	Roles   RoleSet
	Gateway struct {
		Addr         string
		Routing      string
		URL          string
		NodeID       string
		AdvertiseURL string
		Token        string
	}
	Control struct {
		Addr  string
		Token string
	}
	Media struct {
		NodeID       string
		Nodes        []MediaNodeOptions
		ControlURL   string
		Token        string
		AdvertiseURL string
	}
	Attachment struct {
		NodeID       string
		Nodes        []AttachmentNodeOptions
		AdvertiseURL string
		FTP          FTPOptions
	}
	Downlink struct {
		Timeout     time.Duration
		IDTTL       time.Duration
		MaxInFlight int
	}
	Uplink struct {
		DedupTTL            time.Duration
		DedupMaxEntries     int
		MaxBodyBytes        int64
		MaxRequestBytes     int64
		MaxDecodedBodyBytes int64
	}
	Storage struct {
		Backend      string
		SharedVolume bool
		Marker       string
		Root         string
		ObjectStore  ObjectStoreOptions
	}
	// AllowEmbeddedStorage 表示集群角色使用嵌入式 SQLite 的本地联调模式，
	// 生产禁止开启。
	AllowEmbeddedStorage bool
	// LeaseTolerance 是跨副本抢租约的保守容差。
	LeaseTolerance time.Duration
}

// ObjectStoreOptions 是 S3 兼容后端的归一化配置。
type ObjectStoreOptions struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	PathStyle bool
	Prefix    string
	Retries   int
}

// MediaNodeOptions 是一个可被管控调度的媒体节点。
type MediaNodeOptions struct {
	Name string
	URL  string
}

// AttachmentNodeOptions 是一个可被控制面调度的附件节点。
type AttachmentNodeOptions struct {
	Name string
	URL  string
}

// FTPOptions 是 1078 FTP 上传服务端配置。
type FTPOptions struct {
	Enabled      bool
	Port         uint16
	PassivePorts []int
	AdvertiseIP  string
}

// DefaultFTPControlPort / DefaultFTPPassiveFrom / DefaultFTPPassiveTo 与 ftpserver 默认值保持一致，
// 配置层不反引 ftpserver，避免角色包之间形成依赖。
const (
	DefaultFTPControlPort = 7500
	DefaultFTPPassiveFrom = 7501
	DefaultFTPPassiveTo   = 7510
)

// IsStandalone 表示进程是否运行在 standalone 形态。
func (o ClusterOptions) IsStandalone() bool { return o.Mode == ServerModeStandalone }

// ParseServerMode 归一化并校验 server.mode 与 cluster.*。
// 返回的错误一次性列出全部问题，便于启动时一次修完。
func (c *Config) ParseServerMode() (ClusterOptions, error) {
	options := ClusterOptions{
		Mode:  ServerModeStandalone,
		Roles: RoleSet{},
	}
	options.Downlink.Timeout = DefaultClusterDownlinkTimeout
	options.Downlink.IDTTL = DefaultClusterDownlinkIDTTL
	options.Downlink.MaxInFlight = DefaultClusterDownlinkMaxInFly
	options.Uplink.DedupTTL = DefaultClusterUplinkDedupTTL
	options.Uplink.DedupMaxEntries = DefaultClusterUplinkDedupMax
	options.Uplink.MaxBodyBytes = DefaultClusterUplinkMaxBody
	options.Uplink.MaxRequestBytes = DefaultClusterUplinkMaxRequest
	options.Uplink.MaxDecodedBodyBytes = DefaultClusterUplinkMaxDecoded
	options.Storage.Root = DefaultClusterStorageRoot
	options.Storage.Marker = DefaultClusterStorageMarker
	options.LeaseTolerance = DefaultClusterLeaseTolerance

	if c == nil {
		return options, nil
	}
	mode := strings.ToLower(strings.TrimSpace(c.Server.Mode))
	if mode == "" {
		mode = ServerModeStandalone
	}
	if mode != ServerModeStandalone && mode != ServerModeCluster {
		return options, fmt.Errorf("server.mode must be %s or %s, got %q",
			ServerModeStandalone, ServerModeCluster, c.Server.Mode)
	}
	options.Mode = mode

	conf := c.Cluster
	if conf == nil {
		if mode == ServerModeCluster {
			return options, fmt.Errorf("cluster section is required when server.mode=%s", ServerModeCluster)
		}
		return options, nil
	}

	var problems []string
	roles := RoleSet{}
	seen := map[string]bool{}
	for _, raw := range conf.Roles {
		role := strings.ToLower(strings.TrimSpace(raw))
		if role == "" {
			problems = append(problems, "cluster.roles must not contain empty entries")
			continue
		}
		switch role {
		case RoleGateway, RoleControl, RoleMedia, RoleAttachment:
		default:
			problems = append(problems, fmt.Sprintf("cluster.roles contains unknown role %q", raw))
			continue
		}
		if seen[role] {
			problems = append(problems, fmt.Sprintf("cluster.roles contains duplicate role %q", role))
			continue
		}
		seen[role] = true
		roles[role] = true
	}
	options.Roles = roles
	options.AllowEmbeddedStorage = conf.AllowEmbeddedStorage
	if raw := strings.TrimSpace(conf.LeaseTolerance); raw != "" {
		parsed, parseErr := time.ParseDuration(raw)
		if parseErr != nil || parsed < 0 {
			problems = append(problems, fmt.Sprintf("cluster.leaseTolerance must be a non-negative duration, got %q", conf.LeaseTolerance))
		} else {
			options.LeaseTolerance = parsed
		}
	}

	if mode == ServerModeStandalone {
		if len(conf.Roles) > 0 {
			problems = append(problems, "cluster.roles must be empty when server.mode=standalone")
		}
		if len(problems) > 0 {
			return options, fmt.Errorf("%s", strings.Join(problems, "; "))
		}
		return options, nil
	}

	if len(roles) == 0 {
		problems = append(problems, "cluster.roles must not be empty when server.mode=cluster")
	}

	// gateway / control 的监听与凭据。
	if conf.Gateway != nil {
		options.Gateway.Addr = strings.TrimSpace(conf.Gateway.Addr)
		options.Gateway.Routing = strings.ToLower(strings.TrimSpace(conf.Gateway.Routing))
		options.Gateway.URL = strings.TrimSpace(conf.Gateway.URL)
		options.Gateway.NodeID = strings.TrimSpace(conf.Gateway.NodeID)
		options.Gateway.AdvertiseURL = strings.TrimSpace(conf.Gateway.AdvertiseURL)
		options.Gateway.Token = strings.TrimSpace(conf.Gateway.Token)
		if options.Gateway.NodeID == "" {
			options.Gateway.NodeID = defaultGatewayNodeID()
		}
		if options.Gateway.AdvertiseURL == "" {
			options.Gateway.AdvertiseURL = options.Gateway.URL
		}
	}
	if conf.Control != nil {
		options.Control.Addr = strings.TrimSpace(conf.Control.Addr)
		options.Control.Token = strings.TrimSpace(conf.Control.Token)
	}
	if conf.Media != nil {
		options.Media.NodeID = strings.TrimSpace(conf.Media.NodeID)
		options.Media.ControlURL = strings.TrimSpace(conf.Media.ControlURL)
		options.Media.Token = strings.TrimSpace(conf.Media.Token)
		options.Media.AdvertiseURL = strings.TrimSpace(conf.Media.AdvertiseURL)
		if options.Media.ControlURL != "" {
			if err := validateHTTPURL("cluster.media.controlUrl", options.Media.ControlURL); err != nil {
				problems = append(problems, err.Error())
			}
		}
		if options.Media.AdvertiseURL != "" {
			if err := validateHTTPURL("cluster.media.advertiseUrl", options.Media.AdvertiseURL); err != nil {
				problems = append(problems, err.Error())
			}
		}
		seenNodeURL := map[string]bool{}
		for index, raw := range conf.Media.Nodes {
			url := strings.TrimSpace(raw.URL)
			name := strings.TrimSpace(raw.Name)
			if url == "" {
				problems = append(problems, fmt.Sprintf("cluster.media.nodes[%d].url is required", index))
				continue
			}
			if err := validateHTTPURL(fmt.Sprintf("cluster.media.nodes[%d].url", index), url); err != nil {
				problems = append(problems, err.Error())
				continue
			}
			if seenNodeURL[url] {
				problems = append(problems, fmt.Sprintf("cluster.media.nodes[%d].url duplicates %q", index, url))
				continue
			}
			seenNodeURL[url] = true
			if name == "" {
				name = fmt.Sprintf("media-%d", index+1)
			}
			options.Media.Nodes = append(options.Media.Nodes, MediaNodeOptions{Name: name, URL: url})
		}
	}
	if conf.Attachment != nil {
		options.Attachment.NodeID = strings.TrimSpace(conf.Attachment.NodeID)
		options.Attachment.AdvertiseURL = strings.TrimSpace(conf.Attachment.AdvertiseURL)
		if options.Attachment.AdvertiseURL != "" {
			if err := validateHTTPURL("cluster.attachment.advertiseUrl", options.Attachment.AdvertiseURL); err != nil {
				problems = append(problems, err.Error())
			}
		}
		seenNodeURL := map[string]bool{}
		for index, raw := range conf.Attachment.Nodes {
			url := strings.TrimSpace(raw.URL)
			name := strings.TrimSpace(raw.Name)
			if url == "" {
				problems = append(problems, fmt.Sprintf("cluster.attachment.nodes[%d].url is required", index))
				continue
			}
			if err := validateHTTPURL(fmt.Sprintf("cluster.attachment.nodes[%d].url", index), url); err != nil {
				problems = append(problems, err.Error())
				continue
			}
			if seenNodeURL[url] {
				problems = append(problems, fmt.Sprintf("cluster.attachment.nodes[%d].url duplicates %q", index, url))
				continue
			}
			seenNodeURL[url] = true
			if name == "" {
				name = fmt.Sprintf("attachment-%d", index+1)
			}
			options.Attachment.Nodes = append(options.Attachment.Nodes, AttachmentNodeOptions{Name: name, URL: url})
		}
		if conf.Attachment.FTP != nil {
			ftp := conf.Attachment.FTP
			options.Attachment.FTP.Enabled = true
			options.Attachment.FTP.Port = ftp.Port
			if options.Attachment.FTP.Port == 0 {
				options.Attachment.FTP.Port = DefaultFTPControlPort
			}
			options.Attachment.FTP.AdvertiseIP = strings.TrimSpace(ftp.AdvertiseIP)
			ports, err := parsePortRange(ftp.PassivePorts)
			if err != nil {
				problems = append(problems, fmt.Sprintf("cluster.attachment.ftp1078.passivePorts: %v", err))
			} else if len(ports) == 0 {
				for port := DefaultFTPPassiveFrom; port <= DefaultFTPPassiveTo; port++ {
					ports = append(ports, port)
				}
			}
			options.Attachment.FTP.PassivePorts = ports
		}
	}
	// 下行模式必须显式且互斥：fixed 依赖 url，directory 不允许再配 url。
	switch options.Gateway.Routing {
	case "":
		if roles.Has(RoleControl) {
			problems = append(problems, "cluster.gateway.routing is required for role control (directory or fixed)")
		}
	case GatewayRoutingDirectory:
		if options.Gateway.URL != "" {
			problems = append(problems, "cluster.gateway.url must be empty when cluster.gateway.routing=directory")
		}
	case GatewayRoutingFixed:
		if options.Gateway.URL == "" {
			problems = append(problems, "cluster.gateway.url is required when cluster.gateway.routing=fixed")
		} else if err := validateHTTPURL("cluster.gateway.url", options.Gateway.URL); err != nil {
			problems = append(problems, err.Error())
		}
	default:
		problems = append(problems, fmt.Sprintf(
			"cluster.gateway.routing must be %q or %q, got %q",
			GatewayRoutingDirectory, GatewayRoutingFixed, options.Gateway.Routing))
	}

	if roles.Has(RoleGateway) {
		if options.Gateway.Addr == "" {
			problems = append(problems, "cluster.gateway.addr is required for role gateway")
		} else if err := validateListenAddr("cluster.gateway.addr", options.Gateway.Addr); err != nil {
			problems = append(problems, err.Error())
		}
		if options.Gateway.Token == "" {
			problems = append(problems, "cluster.gateway.token is required for role gateway")
		}
		if options.Gateway.AdvertiseURL == "" {
			problems = append(problems, "cluster.gateway.advertiseUrl is required for role gateway")
		} else if err := validateHTTPURL("cluster.gateway.advertiseUrl", options.Gateway.AdvertiseURL); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if roles.Has(RoleControl) {
		if options.Control.Addr == "" {
			problems = append(problems, "cluster.control.addr is required for role control")
		} else if err := validateListenAddr("cluster.control.addr", options.Control.Addr); err != nil {
			problems = append(problems, err.Error())
		}
		if options.Control.Token == "" {
			problems = append(problems, "cluster.control.token is required for role control")
		}
	}

	// 下行参数。
	if dl := conf.Downlink; dl != nil {
		if dl.TimeoutMS != 0 {
			if dl.TimeoutMS < 1 {
				problems = append(problems, fmt.Sprintf("cluster.downlink.timeoutMs must be > 0, got %d", dl.TimeoutMS))
			}
			options.Downlink.Timeout = time.Duration(dl.TimeoutMS) * time.Millisecond
		}
		if raw := strings.TrimSpace(dl.IDTTL); raw != "" {
			parsed, err := time.ParseDuration(raw)
			if err != nil || parsed <= 0 {
				problems = append(problems, fmt.Sprintf("cluster.downlink.idempotencyTTL must be a positive duration, got %q", raw))
			} else {
				options.Downlink.IDTTL = parsed
			}
		}
		if dl.MaxInFly != 0 {
			if dl.MaxInFly < 1 {
				problems = append(problems, fmt.Sprintf("cluster.downlink.maxInFlight must be >= 1, got %d", dl.MaxInFly))
			}
			options.Downlink.MaxInFlight = dl.MaxInFly
		}
	}
	// 下行超时上限：覆盖一次下发到会话重传耗尽的窗口。
	if c.Server.AckTimeoutMS > 0 && c.Server.MaxRetransmissions >= 0 {
		capWindow := time.Duration(c.Server.AckTimeoutMS) * time.Millisecond * time.Duration(c.Server.MaxRetransmissions+1)
		if capWindow > 0 && options.Downlink.Timeout > capWindow {
			problems = append(problems, fmt.Sprintf(
				"cluster.downlink.timeoutMs must not exceed server.ackTimeoutMs × (maxRetransmissions+1) = %s, got %s",
				capWindow, options.Downlink.Timeout))
		}
	}

	// 上行入口参数。
	if up := conf.Uplink; up != nil {
		if raw := strings.TrimSpace(up.DedupTTL); raw != "" {
			parsed, err := time.ParseDuration(raw)
			if err != nil || parsed <= 0 {
				problems = append(problems, fmt.Sprintf("cluster.uplink.dedupTTL must be a positive duration, got %q", raw))
			} else {
				options.Uplink.DedupTTL = parsed
			}
		}
		if up.DedupMaxEntries != 0 {
			if up.DedupMaxEntries < 1 {
				problems = append(problems, fmt.Sprintf("cluster.uplink.dedupMaxEntries must be >= 1, got %d", up.DedupMaxEntries))
			}
			options.Uplink.DedupMaxEntries = up.DedupMaxEntries
		}
		if up.MaxBodyBytes != 0 {
			if up.MaxBodyBytes < 1024 || up.MaxBodyBytes > 64*1024*1024 {
				problems = append(problems, fmt.Sprintf("cluster.uplink.maxBodyBytes must be in [1024, 67108864], got %d", up.MaxBodyBytes))
			}
			options.Uplink.MaxBodyBytes = int64(up.MaxBodyBytes)
		}
		if up.MaxRequestBytes != 0 {
			if up.MaxRequestBytes < 1024 || up.MaxRequestBytes > 96*1024*1024 {
				problems = append(problems, fmt.Sprintf("cluster.uplink.maxRequestBytes must be in [1024, 100663296], got %d", up.MaxRequestBytes))
			}
			options.Uplink.MaxRequestBytes = int64(up.MaxRequestBytes)
		}
		if up.MaxDecodedBodyBytes != 0 {
			if up.MaxDecodedBodyBytes < 1024 || up.MaxDecodedBodyBytes > 64*1024*1024 {
				problems = append(problems, fmt.Sprintf("cluster.uplink.maxDecodedBodyBytes must be in [1024, 67108864], got %d", up.MaxDecodedBodyBytes))
			}
			options.Uplink.MaxDecodedBodyBytes = int64(up.MaxDecodedBodyBytes)
		}
		// base64 膨胀 4/3，再加 64 KiB 给信封字段留余量；请求体上限必须容纳最大消息体。
		minRequestBytes := (options.Uplink.MaxBodyBytes*4+2)/3 + 64*1024
		if options.Uplink.MaxRequestBytes < minRequestBytes {
			problems = append(problems, fmt.Sprintf(
				"cluster.uplink.maxRequestBytes (%d) must be >= ceil(maxBodyBytes*4/3)+64KiB (%d)",
				options.Uplink.MaxRequestBytes, minRequestBytes))
		}
		if options.Uplink.MaxDecodedBodyBytes < options.Uplink.MaxBodyBytes {
			problems = append(problems, fmt.Sprintf(
				"cluster.uplink.maxDecodedBodyBytes (%d) must be >= maxBodyBytes (%d)",
				options.Uplink.MaxDecodedBodyBytes, options.Uplink.MaxBodyBytes))
		}
	}

	// 文件存储。
	if storageConf := conf.Storage; storageConf != nil {
		options.Storage.Backend = strings.ToLower(strings.TrimSpace(storageConf.Backend))
		options.Storage.SharedVolume = storageConf.SharedVolume
		if marker := strings.TrimSpace(storageConf.Marker); marker != "" {
			options.Storage.Marker = marker
		}
		if root := strings.TrimSpace(storageConf.Root); root != "" {
			options.Storage.Root = root
		}
		if store := storageConf.ObjectStore; store != nil {
			options.Storage.ObjectStore = ObjectStoreOptions{
				Endpoint:  strings.TrimSpace(store.Endpoint),
				Region:    strings.TrimSpace(store.Region),
				Bucket:    strings.TrimSpace(store.Bucket),
				AccessKey: strings.TrimSpace(store.AccessKey),
				SecretKey: store.SecretKey,
				PathStyle: true,
				Prefix:    strings.Trim(strings.TrimSpace(store.Prefix), "/"),
				Retries:   store.Retries,
			}
			if store.PathStyle != nil {
				options.Storage.ObjectStore.PathStyle = *store.PathStyle
			}
			if options.Storage.ObjectStore.Region == "" {
				options.Storage.ObjectStore.Region = "us-east-1"
			}
		}
	}
	needsBlob := roles.Has(RoleMedia) || roles.Has(RoleAttachment) ||
		(roles.Has(RoleControl) && options.Storage.Backend == BlobBackendObjectStore)
	if needsBlob {
		switch options.Storage.Backend {
		case BlobBackendSharedVolume:
			if !options.Storage.SharedVolume {
				problems = append(problems, "cluster.storage.sharedVolume must be true when cluster.storage.backend=sharedVolume")
			}
			markerPath := filepath.Join(options.Storage.Root, options.Storage.Marker)
			if _, err := os.Stat(markerPath); err != nil {
				problems = append(problems, fmt.Sprintf(
					"cluster.storage marker %q is required for sharedVolume backend: %v", markerPath, err))
			}
		case BlobBackendObjectStore:
			store := options.Storage.ObjectStore
			if store.Endpoint == "" {
				problems = append(problems, "cluster.storage.objectStore.endpoint is required when backend=objectStore")
			}
			if store.Bucket == "" {
				problems = append(problems, "cluster.storage.objectStore.bucket is required when backend=objectStore")
			}
			if store.AccessKey == "" || store.SecretKey == "" {
				problems = append(problems, "cluster.storage.objectStore.accessKey and secretKey are required when backend=objectStore")
			}
			if store.Retries < 0 || store.Retries > 10 {
				problems = append(problems, fmt.Sprintf("cluster.storage.objectStore.retries must be in [0,10], got %d", store.Retries))
			}
		default:
			problems = append(problems, "cluster.storage.backend must be sharedVolume or objectStore for roles media/attachment")
		}
	}
	if options.Storage.Backend == BlobBackendObjectStore {
		if policy := c.RetentionPolicy(); policy.OrphanFiles.Enabled {
			problems = append(problems, "retention.orphanFiles.enabled must be false when cluster.storage.backend=objectStore (use bucket lifecycle)")
		}
	}

	// control 在 E2 用 sharedVolume 承接 0x0801 落盘。共享卷根必须等于媒体下载目录，
	// 下载接口才能按同一相对路径打开文件；标记文件必须已存在且可写。
	if roles.Has(RoleControl) && options.Storage.Backend == BlobBackendSharedVolume {
		mediaUploadOptions, _ := c.ParseMediaUpload()
		uploadRoot, err := filepath.Abs(mediaUploadOptions.Dir)
		if err != nil {
			problems = append(problems, fmt.Sprintf("mediaUpload.dir is invalid for role control: %v", err))
		}
		if conf.Storage == nil || strings.TrimSpace(conf.Storage.Root) == "" {
			options.Storage.Root = mediaUploadOptions.Dir
		}
		storageRoot, err := filepath.Abs(options.Storage.Root)
		if err != nil {
			problems = append(problems, fmt.Sprintf("cluster.storage.root is invalid for role control: %v", err))
		} else if uploadRoot != storageRoot {
			problems = append(problems, fmt.Sprintf(
				"cluster.storage.root (%s) must equal mediaUpload.dir (%s) for role control", storageRoot, uploadRoot))
		}
		if !options.Storage.SharedVolume {
			problems = append(problems, "cluster.storage.sharedVolume must be true when cluster.storage.backend=sharedVolume")
		}
		markerPath := filepath.Join(options.Storage.Root, options.Storage.Marker)
		file, err := os.OpenFile(markerPath, os.O_RDWR, 0)
		if err != nil {
			problems = append(problems, fmt.Sprintf(
				"cluster.storage marker %q must exist and be writable for role control: %v", markerPath, err))
		} else {
			_ = file.Close()
		}
	}

	// 元数据面不变量：只有 control 连接并写入元数据库。
	// gateway/media/attachment 是数据面节点，配置里不得出现数据库入口。
	if !roles.Has(RoleControl) {
		backend := ""
		if c.Database != nil {
			backend = strings.ToLower(strings.TrimSpace(c.Database.Type))
		}
		if backend != "" && backend != "memory" {
			problems = append(problems, fmt.Sprintf(
				"database must not be configured for data-plane roles (gateway/media/attachment), got %q: "+
					"only control connects to the metadata database", backend))
		}
	}
	if roles.Has(RoleControl) {
		backend := strings.ToLower(strings.TrimSpace(c.Database.Type))
		if backend == "" {
			backend = "memory"
		}
		if backend != "mysql" {
			allowEmbedded := c.Cluster != nil && c.Cluster.AllowEmbeddedStorage && backend == "sqlite"
			if !allowEmbedded {
				problems = append(problems, fmt.Sprintf(
					"database.type must be mysql when roles include control, got %q "+
						"(cluster.allowEmbeddedStorage with sqlite is dev-only; production must use mysql)", backend))
			}
		}
	}

	// outbox：control 必须 durable。
	if roles.Has(RoleControl) {
		outboxOptions, err := c.ParseCommandOutbox()
		if err != nil {
			problems = append(problems, err.Error())
		} else {
			backend := strings.ToLower(strings.TrimSpace(c.Database.Type))
			if backend == "" {
				backend = "memory"
			}
			resolved := ResolveCommandOutboxMode(outboxOptions.Mode, backend)
			if resolved != CommandOutboxModeDurable {
				problems = append(problems, fmt.Sprintf(
					"commandOutbox.mode must be durable for role control, got %q", resolved))
			}
		}
	}

	// 上行出口：gateway-only 必须 http；同进程走 local，http 指向自身视为回环。
	sinkOptions, sinkErr := c.ParseUplinkSink()
	if sinkErr != nil {
		problems = append(problems, sinkErr.Error())
	} else if roles.Has(RoleGateway) {
		if roles.Has(RoleControl) {
			if sinkOptions.Mode == UplinkSinkHTTP && loopbackToAddr(sinkOptions.HTTP.URL, options.Control.Addr) {
				problems = append(problems, "uplink.http.url must not point to the local control addr when gateway and control share a process")
			}
		} else if sinkOptions.Mode != UplinkSinkHTTP {
			problems = append(problems, fmt.Sprintf(
				"uplink.sink must be %s for gateway-only cluster, got %q", UplinkSinkHTTP, sinkOptions.Mode))
		}
	}
	// gateway 发送上行、control 校验：同一方向必须使用同一密钥。
	if roles.Has(RoleGateway) && sinkOptions.Mode == UplinkSinkHTTP && options.Control.Token != "" {
		if sinkOptions.HTTP.Token != options.Control.Token {
			problems = append(problems, "uplink.http.token must equal cluster.control.token")
		}
	}

	// media / attachment 端口。
	if roles.Has(RoleMedia) {
		mediaOptions, enabled, err := c.ParseMediaServer()
		if err != nil {
			problems = append(problems, err.Error())
		} else if !enabled || mediaOptions.Port == 0 {
			problems = append(problems, "mediaServer.port is required for role media")
		}
	}
	if roles.Has(RoleAttachment) {
		alarmOptions, enabled, err := c.ParseAlarmServer()
		if err != nil {
			problems = append(problems, err.Error())
		} else if !enabled || (alarmOptions.Port == 0 && alarmOptions.UDPPort == 0) {
			problems = append(problems, "alarmServer.port or alarmServer.udpPort is required for role attachment")
		} else if alarmOptions.HTTPPort == 0 {
			problems = append(problems, "alarmServer.httpPort is required for role attachment (internal status API)")
		}
		if options.Attachment.FTP.Enabled {
			// FTP 登录校验与进度上报都是节点到 control 的出站请求，必须有 control 入口。
			if strings.TrimSpace(alarmOptions.ProgressReport.URL) == "" {
				problems = append(problems,
					"alarmServer.progressReport.url is required when cluster.attachment.ftp1078 is enabled (control login/progress endpoint)")
			}
			if options.Attachment.FTP.AdvertiseIP == "" && strings.TrimSpace(alarmOptions.UploadIP) == "" {
				problems = append(problems,
					"cluster.attachment.ftp1078.advertiseIp or alarmServer.uploadIp is required so terminals can reach the FTP server")
			}
		}
	}

	if len(problems) > 0 {
		return options, fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return options, nil
}

// ValidateServerMode 在 database.Init 之前做 fail-fast 校验。
func (c *Config) ValidateServerMode() error {
	_, err := c.ParseServerMode()
	return err
}

// parsePortRange 解析 FTP 被动端口段：支持 "a-b"、单端口与逗号分隔组合。
func parsePortRange(raw string) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	seen := map[int]bool{}
	var ports []int
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lowText, highText, hasRange := strings.Cut(part, "-")
		low, err := strconv.Atoi(strings.TrimSpace(lowText))
		if err != nil || low < 1 || low > 65535 {
			return nil, fmt.Errorf("invalid port %q", part)
		}
		high := low
		if hasRange {
			high, err = strconv.Atoi(strings.TrimSpace(highText))
			if err != nil || high < low || high > 65535 {
				return nil, fmt.Errorf("invalid port range %q", part)
			}
		}
		for port := low; port <= high; port++ {
			if !seen[port] {
				seen[port] = true
				ports = append(ports, port)
			}
		}
	}
	return ports, nil
}

// validateListenAddr 校验监听地址 host:port；允许空 host（表示全部网卡）。
func validateListenAddr(name, addr string) error {
	_, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%s must be a host:port address, got %q", name, addr)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("%s must use a port between 1 and 65535, got %q", name, addr)
	}
	return nil
}

// validateHTTPURL 校验绝对 http(s) 地址。
func validateHTTPURL(name, raw string) error {
	parsed, err := neturl.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%s must be an absolute http(s) URL, got %q", name, raw)
	}
	return nil
}

// loopbackToAddr 判断 url 是否指向本进程的监听地址。
// 监听 host 为空、0.0.0.0、:: 时按本机通配处理，只比较端口。
func loopbackToAddr(rawURL, listenAddr string) bool {
	parsed, err := neturl.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return false
	}
	urlHost, urlPort, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		return false
	}
	listenHost, listenPort, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return false
	}
	portEqual := urlPort == listenPort || (urlPort == "80" && listenPort == "80") || (urlPort == "443" && listenPort == "443")
	if !portEqual {
		return false
	}
	if listenHost == "" || listenHost == "0.0.0.0" || listenHost == "::" {
		return true
	}
	return strings.EqualFold(urlHost, listenHost)
}

// databaseConf 配置持久化数据库后端；type 缺省为 memory。
type databaseConf struct {
	Type   string      `yaml:"type"`
	SQLite *sqliteConf `yaml:"sqlite"`
	MySQL  *mysqlConf  `yaml:"mysql"`
}

type sqliteConf struct {
	Path string `yaml:"path"`
	// ReadConns 是只读连接池大小；0 表示按 CPU 数。
	// 写池固定为 1 个连接，不可配置。
	ReadConns int `yaml:"readConns"`
}

type mysqlConf struct {
	DSN string `yaml:"dsn"`
}

var (
	configOnce sync.Once
	config     *Config
)

// rejectLegacyKeys 拒绝已改名的旧配置段。
//
// viper 默认忽略未知字段；若不显式检查，旧配置里的 storage 段会被静默丢弃，
// 服务仍按默认后端启动，改名根本没有生效。
func rejectLegacyKeys(v *viper.Viper) error {
	if v.InConfig("storage") {
		return errors.New(`config section "storage" has been renamed to "database": ` +
			`move storage.type, storage.sqlite.* and storage.mysql.dsn under the top-level "database" section`)
	}
	if v.InConfig("cluster.blob") {
		return errors.New(`config key "cluster.blob" has been renamed to "cluster.storage": ` +
			`move cluster.blob.backend, cluster.blob.sharedVolume, cluster.blob.root, ` +
			`cluster.blob.marker and cluster.blob.objectStore.* under the "cluster.storage" section`)
	}
	return nil
}

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

		// 旧的 storage 段已整体改名为 database；必须显式报错，
		// 不能让未知字段被静默忽略后继续用默认后端启动。
		if err := rejectLegacyKeys(viper.GetViper()); err != nil {
			panic(err)
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
