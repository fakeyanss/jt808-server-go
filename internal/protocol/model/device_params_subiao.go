package model

import (
	"encoding/json"
	"fmt"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// 苏标 T/JSATL 12-2017 4.3.1 参数项。
//
// 参数 ID 0xF364~0xF367 的参数值都是固定布局的复合结构，不能按整型或字符串处理。
// 解码结果以结构体返回，便于 0x0104 查询应答语义化展示；编码同时接受结构体和
// API 传入的 map[string]any（JSON 反序列化后的默认类型）。

const (
	subiaoADASParamMinLen = 52 // 52~55 为保留字段
	subiaoADASParamLen    = 56
	subiaoDSMParamMinLen  = 47 // 47~48 为保留字段
	subiaoDSMParamLen     = 49
	subiaoTPMSParamMinLen = 30 // 30~35 为保留字段
	subiaoTPMSParamLen    = 36
	subiaoBSDParamLen     = 2
)

// SubiaoADASParam 对应表 4-10 高级驾驶辅助系统参数（0xF364）。
type SubiaoADASParam struct {
	AlarmSpeedThreshold                 uint8  `json:"alarmSpeedThreshold"`                 // 报警判断速度阈值
	AlarmVolume                         uint8  `json:"alarmVolume"`                         // 报警提示音量
	ActivePhotoStrategy                 uint8  `json:"activePhotoStrategy"`                 // 主动拍照策略
	ActiveTimedPhotoInterval            uint16 `json:"activeTimedPhotoInterval"`            // 主动定时拍照时间间隔，秒
	ActiveDistancePhotoInterval         uint16 `json:"activeDistancePhotoInterval"`         // 主动定距拍照距离间隔，米
	SingleActivePhotoCount              uint8  `json:"singleActivePhotoCount"`              // 单次主动拍照张数
	SingleActivePhotoInterval           uint8  `json:"singleActivePhotoInterval"`           // 单次主动拍照间隔，100ms
	PhotoResolution                     uint8  `json:"photoResolution"`                     // 拍照分辨率
	VideoResolution                     uint8  `json:"videoResolution"`                     // 视频录制分辨率
	AlarmEnable                         uint32 `json:"alarmEnable"`                         // 报警使能
	EventEnable                         uint32 `json:"eventEnable"`                         // 事件使能
	Reserved                            uint8  `json:"reserved"`                            // 预留字段
	ObstacleAlarmDistanceThreshold      uint8  `json:"obstacleAlarmDistanceThreshold"`      // 障碍物报警距离阈值
	ObstacleAlarmSpeedThreshold         uint8  `json:"obstacleAlarmSpeedThreshold"`         // 障碍物报警分级速度阈值
	ObstacleAlarmVideoDuration          uint8  `json:"obstacleAlarmVideoDuration"`          // 障碍物报警前后视频录制时间，秒
	ObstacleAlarmPhotoCount             uint8  `json:"obstacleAlarmPhotoCount"`             // 障碍物报警拍照张数
	ObstacleAlarmPhotoInterval          uint8  `json:"obstacleAlarmPhotoInterval"`          // 障碍物报警拍照间隔，100ms
	FrequentLaneChangeDetectInterval    uint8  `json:"frequentLaneChangeDetectInterval"`    // 频繁变道报警判断时间段，秒
	FrequentLaneChangeDetectCount       uint8  `json:"frequentLaneChangeDetectCount"`       // 频繁变道报警判断次数
	FrequentLaneChangeSpeedThreshold    uint8  `json:"frequentLaneChangeSpeedThreshold"`    // 频繁变道报警分级速度阈值
	FrequentLaneChangeVideoDuration     uint8  `json:"frequentLaneChangeVideoDuration"`     // 频繁变道报警前后视频录制时间，秒
	FrequentLaneChangePhotoCount        uint8  `json:"frequentLaneChangePhotoCount"`        // 频繁变道报警拍照张数
	FrequentLaneChangePhotoInterval     uint8  `json:"frequentLaneChangePhotoInterval"`     // 频繁变道报警拍照间隔，100ms
	LaneDepartureSpeedThreshold         uint8  `json:"laneDepartureSpeedThreshold"`         // 车道偏离报警分级速度阈值
	LaneDepartureVideoDuration          uint8  `json:"laneDepartureVideoDuration"`          // 车道偏离报警前后视频录制时间，秒
	LaneDeparturePhotoCount             uint8  `json:"laneDeparturePhotoCount"`             // 车道偏离报警拍照张数
	LaneDeparturePhotoInterval          uint8  `json:"laneDeparturePhotoInterval"`          // 车道偏离报警拍照间隔，100ms
	ForwardCollisionTimeThreshold       uint8  `json:"forwardCollisionTimeThreshold"`       // 前向碰撞报警时间阈值，100ms
	ForwardCollisionSpeedThreshold      uint8  `json:"forwardCollisionSpeedThreshold"`      // 前向碰撞报警分级速度阈值
	ForwardCollisionVideoDuration       uint8  `json:"forwardCollisionVideoDuration"`       // 前向碰撞报警前后视频录制时间，秒
	ForwardCollisionPhotoCount          uint8  `json:"forwardCollisionPhotoCount"`          // 前向碰撞报警拍照张数
	ForwardCollisionPhotoInterval       uint8  `json:"forwardCollisionPhotoInterval"`       // 前向碰撞报警拍照间隔，100ms
	PedestrianCollisionTimeThreshold    uint8  `json:"pedestrianCollisionTimeThreshold"`    // 行人碰撞报警时间阈值，100ms
	PedestrianCollisionSpeedThreshold   uint8  `json:"pedestrianCollisionSpeedThreshold"`   // 行人碰撞报警使能速度阈值
	PedestrianCollisionVideoDuration    uint8  `json:"pedestrianCollisionVideoDuration"`    // 行人碰撞报警前后视频录制时间，秒
	PedestrianCollisionPhotoCount       uint8  `json:"pedestrianCollisionPhotoCount"`       // 行人碰撞报警拍照张数
	PedestrianCollisionPhotoInterval    uint8  `json:"pedestrianCollisionPhotoInterval"`    // 行人碰撞报警拍照间隔，100ms
	DistanceMonitoringDistanceThreshold uint8  `json:"distanceMonitoringDistanceThreshold"` // 车距监控报警距离阈值，100ms
	DistanceMonitoringSpeedThreshold    uint8  `json:"distanceMonitoringSpeedThreshold"`    // 车距监控报警分级速度阈值
	DistanceTooCloseVideoDuration       uint8  `json:"distanceTooCloseVideoDuration"`       // 车距过近报警前后视频录制时间，秒
	DistanceTooClosePhotoCount          uint8  `json:"distanceTooClosePhotoCount"`          // 车距过近报警拍照张数
	DistanceTooClosePhotoInterval       uint8  `json:"distanceTooClosePhotoInterval"`       // 车距过近报警拍照间隔，100ms
	RoadSignRecognitionPhotoCount       uint8  `json:"roadSignRecognitionPhotoCount"`       // 道路标志识别拍照张数
	RoadSignRecognitionPhotoInterval    uint8  `json:"roadSignRecognitionPhotoInterval"`    // 道路标志识别拍照间隔，100ms
	TailReserved                        []byte `json:"tailReserved,omitempty"`              // 保留字段 BYTE[4]
}

// SubiaoDSMParam 对应表 4-11 驾驶员状态监测系统参数（0xF365）。
type SubiaoDSMParam struct {
	AlarmSpeedThreshold              uint8  `json:"alarmSpeedThreshold"`              // 报警判断速度阈值
	AlarmVolume                      uint8  `json:"alarmVolume"`                      // 报警音量
	ActivePhotoStrategy              uint8  `json:"activePhotoStrategy"`              // 主动拍照策略
	ActiveTimedPhotoInterval         uint16 `json:"activeTimedPhotoInterval"`         // 主动定时拍照时间间隔，秒
	ActiveDistancePhotoInterval      uint16 `json:"activeDistancePhotoInterval"`      // 主动定距拍照距离间隔，米
	SingleActivePhotoCount           uint8  `json:"singleActivePhotoCount"`           // 单次主动拍照张数
	SingleActivePhotoInterval        uint8  `json:"singleActivePhotoInterval"`        // 单次主动拍照时间间隔，100ms
	PhotoResolution                  uint8  `json:"photoResolution"`                  // 拍照分辨率
	VideoResolution                  uint8  `json:"videoResolution"`                  // 视频录制分辨率
	AlarmEnable                      uint32 `json:"alarmEnable"`                      // 报警使能
	EventEnable                      uint32 `json:"eventEnable"`                      // 事件使能
	SmokingAlarmInterval             uint16 `json:"smokingAlarmInterval"`             // 吸烟报警判断时间间隔，秒
	PhoneCallAlarmInterval           uint16 `json:"phoneCallAlarmInterval"`           // 接打电话报警判断时间间隔，秒
	MiddleReserved                   []byte `json:"middleReserved,omitempty"`         // 预留字段 BYTE[3]
	FatigueSpeedThreshold            uint8  `json:"fatigueSpeedThreshold"`            // 疲劳驾驶报警分级速度阈值
	FatigueVideoDuration             uint8  `json:"fatigueVideoDuration"`             // 疲劳驾驶报警前后视频录制时间，秒
	FatiguePhotoCount                uint8  `json:"fatiguePhotoCount"`                // 疲劳驾驶报警拍照张数
	FatiguePhotoInterval             uint8  `json:"fatiguePhotoInterval"`             // 疲劳驾驶报警拍照间隔，100ms
	PhoneCallSpeedThreshold          uint8  `json:"phoneCallSpeedThreshold"`          // 接打电话报警分级速度阈值
	PhoneCallVideoDuration           uint8  `json:"phoneCallVideoDuration"`           // 接打电话报警前后视频录制时间，秒
	PhoneCallPhotoCount              uint8  `json:"phoneCallPhotoCount"`              // 接打电话报警拍驾驶员面部特征照片张数
	PhoneCallPhotoInterval           uint8  `json:"phoneCallPhotoInterval"`           // 接打电话报警拍驾驶员面部特征照片间隔，100ms
	SmokingSpeedThreshold            uint8  `json:"smokingSpeedThreshold"`            // 抽烟报警分级车速阈值
	SmokingVideoDuration             uint8  `json:"smokingVideoDuration"`             // 抽烟报警前后视频录制时间，秒
	SmokingPhotoCount                uint8  `json:"smokingPhotoCount"`                // 抽烟报警拍驾驶员面部特征照片张数
	SmokingPhotoInterval             uint8  `json:"smokingPhotoInterval"`             // 抽烟报警拍驾驶员面部特征照片间隔，100ms
	DistractedSpeedThreshold         uint8  `json:"distractedSpeedThreshold"`         // 分神驾驶报警分级车速阈值
	DistractedVideoDuration          uint8  `json:"distractedVideoDuration"`          // 分神驾驶报警前后视频录制时间，秒
	DistractedPhotoCount             uint8  `json:"distractedPhotoCount"`             // 分神驾驶报警拍照张数
	DistractedPhotoInterval          uint8  `json:"distractedPhotoInterval"`          // 分神驾驶报警拍照间隔，100ms
	AbnormalDrivingSpeedThreshold    uint8  `json:"abnormalDrivingSpeedThreshold"`    // 驾驶行为异常分级速度阈值
	AbnormalDrivingVideoDuration     uint8  `json:"abnormalDrivingVideoDuration"`     // 驾驶行为异常视频录制时间，秒
	AbnormalDrivingPhotoCount        uint8  `json:"abnormalDrivingPhotoCount"`        // 驾驶行为异常抓拍照片张数
	AbnormalDrivingPhotoInterval     uint8  `json:"abnormalDrivingPhotoInterval"`     // 驾驶行为异常拍照间隔，100ms
	DriverIdentityRecognitionTrigger uint8  `json:"driverIdentityRecognitionTrigger"` // 驾驶员身份识别触发
	TailReserved                     []byte `json:"tailReserved,omitempty"`           // 保留字段 BYTE[2]
}

// SubiaoTPMSParam 对应表 4-12 胎压监测系统参数（0xF366）。
type SubiaoTPMSParam struct {
	TireSpecification        string `json:"tireSpecification"`        // 轮胎规格型号 BYTE[12]，标准为 ASCII
	TirePressureUnit         uint16 `json:"tirePressureUnit"`         // 胎压单位
	NormalPressure           uint16 `json:"normalPressure"`           // 正常胎压值
	ImbalanceThreshold       uint16 `json:"imbalanceThreshold"`       // 胎压不平衡门限，百分比
	SlowLeakThreshold        uint16 `json:"slowLeakThreshold"`        // 慢漏气门限，百分比
	LowPressureThreshold     uint16 `json:"lowPressureThreshold"`     // 低压阈值
	HighPressureThreshold    uint16 `json:"highPressureThreshold"`    // 高压阈值
	HighTemperatureThreshold uint16 `json:"highTemperatureThreshold"` // 高温阈值，摄氏度
	VoltageThreshold         uint16 `json:"voltageThreshold"`         // 电压阈值，百分比
	TimedReportInterval      uint16 `json:"timedReportInterval"`      // 定时上报时间间隔，秒
	Reserved                 []byte `json:"reserved,omitempty"`       // 保留项 BYTE[6]
}

// SubiaoBSDParam 对应表 4-13 盲区监测系统参数（0xF367）。
type SubiaoBSDParam struct {
	RearApproachAlarmTimeThreshold     uint8 `json:"rearApproachAlarmTimeThreshold"`     // 后方接近报警时间阈值，秒
	SideRearApproachAlarmTimeThreshold uint8 `json:"sideRearApproachAlarmTimeThreshold"` // 侧后方接近报警时间阈值，秒
}

func decodeSubiaoADASParam(b []byte) (any, error) {
	if len(b) < subiaoADASParamMinLen {
		return nil, fmt.Errorf("%w: F364 requires at least %d bytes, got %d", ErrDecodeDeviceParams, subiaoADASParamMinLen, len(b))
	}
	idx := 0
	p := SubiaoADASParam{}
	p.AlarmSpeedThreshold = hex.ReadByte(b, &idx)
	p.AlarmVolume = hex.ReadByte(b, &idx)
	p.ActivePhotoStrategy = hex.ReadByte(b, &idx)
	p.ActiveTimedPhotoInterval = hex.ReadWord(b, &idx)
	p.ActiveDistancePhotoInterval = hex.ReadWord(b, &idx)
	p.SingleActivePhotoCount = hex.ReadByte(b, &idx)
	p.SingleActivePhotoInterval = hex.ReadByte(b, &idx)
	p.PhotoResolution = hex.ReadByte(b, &idx)
	p.VideoResolution = hex.ReadByte(b, &idx)
	p.AlarmEnable = hex.ReadDoubleWord(b, &idx)
	p.EventEnable = hex.ReadDoubleWord(b, &idx)
	p.Reserved = hex.ReadByte(b, &idx)
	p.ObstacleAlarmDistanceThreshold = hex.ReadByte(b, &idx)
	p.ObstacleAlarmSpeedThreshold = hex.ReadByte(b, &idx)
	p.ObstacleAlarmVideoDuration = hex.ReadByte(b, &idx)
	p.ObstacleAlarmPhotoCount = hex.ReadByte(b, &idx)
	p.ObstacleAlarmPhotoInterval = hex.ReadByte(b, &idx)
	p.FrequentLaneChangeDetectInterval = hex.ReadByte(b, &idx)
	p.FrequentLaneChangeDetectCount = hex.ReadByte(b, &idx)
	p.FrequentLaneChangeSpeedThreshold = hex.ReadByte(b, &idx)
	p.FrequentLaneChangeVideoDuration = hex.ReadByte(b, &idx)
	p.FrequentLaneChangePhotoCount = hex.ReadByte(b, &idx)
	p.FrequentLaneChangePhotoInterval = hex.ReadByte(b, &idx)
	p.LaneDepartureSpeedThreshold = hex.ReadByte(b, &idx)
	p.LaneDepartureVideoDuration = hex.ReadByte(b, &idx)
	p.LaneDeparturePhotoCount = hex.ReadByte(b, &idx)
	p.LaneDeparturePhotoInterval = hex.ReadByte(b, &idx)
	p.ForwardCollisionTimeThreshold = hex.ReadByte(b, &idx)
	p.ForwardCollisionSpeedThreshold = hex.ReadByte(b, &idx)
	p.ForwardCollisionVideoDuration = hex.ReadByte(b, &idx)
	p.ForwardCollisionPhotoCount = hex.ReadByte(b, &idx)
	p.ForwardCollisionPhotoInterval = hex.ReadByte(b, &idx)
	p.PedestrianCollisionTimeThreshold = hex.ReadByte(b, &idx)
	p.PedestrianCollisionSpeedThreshold = hex.ReadByte(b, &idx)
	p.PedestrianCollisionVideoDuration = hex.ReadByte(b, &idx)
	p.PedestrianCollisionPhotoCount = hex.ReadByte(b, &idx)
	p.PedestrianCollisionPhotoInterval = hex.ReadByte(b, &idx)
	p.DistanceMonitoringDistanceThreshold = hex.ReadByte(b, &idx)
	p.DistanceMonitoringSpeedThreshold = hex.ReadByte(b, &idx)
	p.DistanceTooCloseVideoDuration = hex.ReadByte(b, &idx)
	p.DistanceTooClosePhotoCount = hex.ReadByte(b, &idx)
	p.DistanceTooClosePhotoInterval = hex.ReadByte(b, &idx)
	p.RoadSignRecognitionPhotoCount = hex.ReadByte(b, &idx)
	p.RoadSignRecognitionPhotoInterval = hex.ReadByte(b, &idx)
	p.TailReserved = subiaoReservedBytes(b, idx, subiaoADASParamLen-subiaoADASParamMinLen)
	return p, nil
}

func encodeSubiaoADASParam(value any) ([]byte, error) {
	var p SubiaoADASParam
	if err := coerceSubiaoParam(value, &p); err != nil {
		return nil, err
	}
	if len(p.TailReserved) > subiaoADASParamLen-subiaoADASParamMinLen {
		return nil, fmt.Errorf("F364 reserved field must be at most %d bytes, got %d", subiaoADASParamLen-subiaoADASParamMinLen, len(p.TailReserved))
	}
	pkt := make([]byte, 0, subiaoADASParamLen)
	pkt = hex.WriteByte(pkt, p.AlarmSpeedThreshold)
	pkt = hex.WriteByte(pkt, p.AlarmVolume)
	pkt = hex.WriteByte(pkt, p.ActivePhotoStrategy)
	pkt = hex.WriteWord(pkt, p.ActiveTimedPhotoInterval)
	pkt = hex.WriteWord(pkt, p.ActiveDistancePhotoInterval)
	pkt = hex.WriteByte(pkt, p.SingleActivePhotoCount)
	pkt = hex.WriteByte(pkt, p.SingleActivePhotoInterval)
	pkt = hex.WriteByte(pkt, p.PhotoResolution)
	pkt = hex.WriteByte(pkt, p.VideoResolution)
	pkt = hex.WriteDoubleWord(pkt, p.AlarmEnable)
	pkt = hex.WriteDoubleWord(pkt, p.EventEnable)
	pkt = hex.WriteByte(pkt, p.Reserved)
	pkt = hex.WriteByte(pkt, p.ObstacleAlarmDistanceThreshold)
	pkt = hex.WriteByte(pkt, p.ObstacleAlarmSpeedThreshold)
	pkt = hex.WriteByte(pkt, p.ObstacleAlarmVideoDuration)
	pkt = hex.WriteByte(pkt, p.ObstacleAlarmPhotoCount)
	pkt = hex.WriteByte(pkt, p.ObstacleAlarmPhotoInterval)
	pkt = hex.WriteByte(pkt, p.FrequentLaneChangeDetectInterval)
	pkt = hex.WriteByte(pkt, p.FrequentLaneChangeDetectCount)
	pkt = hex.WriteByte(pkt, p.FrequentLaneChangeSpeedThreshold)
	pkt = hex.WriteByte(pkt, p.FrequentLaneChangeVideoDuration)
	pkt = hex.WriteByte(pkt, p.FrequentLaneChangePhotoCount)
	pkt = hex.WriteByte(pkt, p.FrequentLaneChangePhotoInterval)
	pkt = hex.WriteByte(pkt, p.LaneDepartureSpeedThreshold)
	pkt = hex.WriteByte(pkt, p.LaneDepartureVideoDuration)
	pkt = hex.WriteByte(pkt, p.LaneDeparturePhotoCount)
	pkt = hex.WriteByte(pkt, p.LaneDeparturePhotoInterval)
	pkt = hex.WriteByte(pkt, p.ForwardCollisionTimeThreshold)
	pkt = hex.WriteByte(pkt, p.ForwardCollisionSpeedThreshold)
	pkt = hex.WriteByte(pkt, p.ForwardCollisionVideoDuration)
	pkt = hex.WriteByte(pkt, p.ForwardCollisionPhotoCount)
	pkt = hex.WriteByte(pkt, p.ForwardCollisionPhotoInterval)
	pkt = hex.WriteByte(pkt, p.PedestrianCollisionTimeThreshold)
	pkt = hex.WriteByte(pkt, p.PedestrianCollisionSpeedThreshold)
	pkt = hex.WriteByte(pkt, p.PedestrianCollisionVideoDuration)
	pkt = hex.WriteByte(pkt, p.PedestrianCollisionPhotoCount)
	pkt = hex.WriteByte(pkt, p.PedestrianCollisionPhotoInterval)
	pkt = hex.WriteByte(pkt, p.DistanceMonitoringDistanceThreshold)
	pkt = hex.WriteByte(pkt, p.DistanceMonitoringSpeedThreshold)
	pkt = hex.WriteByte(pkt, p.DistanceTooCloseVideoDuration)
	pkt = hex.WriteByte(pkt, p.DistanceTooClosePhotoCount)
	pkt = hex.WriteByte(pkt, p.DistanceTooClosePhotoInterval)
	pkt = hex.WriteByte(pkt, p.RoadSignRecognitionPhotoCount)
	pkt = hex.WriteByte(pkt, p.RoadSignRecognitionPhotoInterval)
	pkt = append(pkt, p.TailReserved...)
	pkt = append(pkt, make([]byte, subiaoADASParamLen-subiaoADASParamMinLen-len(p.TailReserved))...)
	return pkt, nil
}

func decodeSubiaoDSMParam(b []byte) (any, error) {
	if len(b) < subiaoDSMParamMinLen {
		return nil, fmt.Errorf("%w: F365 requires at least %d bytes, got %d", ErrDecodeDeviceParams, subiaoDSMParamMinLen, len(b))
	}
	idx := 0
	p := SubiaoDSMParam{}
	p.AlarmSpeedThreshold = hex.ReadByte(b, &idx)
	p.AlarmVolume = hex.ReadByte(b, &idx)
	p.ActivePhotoStrategy = hex.ReadByte(b, &idx)
	p.ActiveTimedPhotoInterval = hex.ReadWord(b, &idx)
	p.ActiveDistancePhotoInterval = hex.ReadWord(b, &idx)
	p.SingleActivePhotoCount = hex.ReadByte(b, &idx)
	p.SingleActivePhotoInterval = hex.ReadByte(b, &idx)
	p.PhotoResolution = hex.ReadByte(b, &idx)
	p.VideoResolution = hex.ReadByte(b, &idx)
	p.AlarmEnable = hex.ReadDoubleWord(b, &idx)
	p.EventEnable = hex.ReadDoubleWord(b, &idx)
	p.SmokingAlarmInterval = hex.ReadWord(b, &idx)
	p.PhoneCallAlarmInterval = hex.ReadWord(b, &idx)
	p.MiddleReserved = subiaoReservedBytes(b, idx, 3)
	idx += 3
	p.FatigueSpeedThreshold = hex.ReadByte(b, &idx)
	p.FatigueVideoDuration = hex.ReadByte(b, &idx)
	p.FatiguePhotoCount = hex.ReadByte(b, &idx)
	p.FatiguePhotoInterval = hex.ReadByte(b, &idx)
	p.PhoneCallSpeedThreshold = hex.ReadByte(b, &idx)
	p.PhoneCallVideoDuration = hex.ReadByte(b, &idx)
	p.PhoneCallPhotoCount = hex.ReadByte(b, &idx)
	p.PhoneCallPhotoInterval = hex.ReadByte(b, &idx)
	p.SmokingSpeedThreshold = hex.ReadByte(b, &idx)
	p.SmokingVideoDuration = hex.ReadByte(b, &idx)
	p.SmokingPhotoCount = hex.ReadByte(b, &idx)
	p.SmokingPhotoInterval = hex.ReadByte(b, &idx)
	p.DistractedSpeedThreshold = hex.ReadByte(b, &idx)
	p.DistractedVideoDuration = hex.ReadByte(b, &idx)
	p.DistractedPhotoCount = hex.ReadByte(b, &idx)
	p.DistractedPhotoInterval = hex.ReadByte(b, &idx)
	p.AbnormalDrivingSpeedThreshold = hex.ReadByte(b, &idx)
	p.AbnormalDrivingVideoDuration = hex.ReadByte(b, &idx)
	p.AbnormalDrivingPhotoCount = hex.ReadByte(b, &idx)
	p.AbnormalDrivingPhotoInterval = hex.ReadByte(b, &idx)
	p.DriverIdentityRecognitionTrigger = hex.ReadByte(b, &idx)
	p.TailReserved = subiaoReservedBytes(b, idx, subiaoDSMParamLen-subiaoDSMParamMinLen)
	return p, nil
}

func encodeSubiaoDSMParam(value any) ([]byte, error) {
	var p SubiaoDSMParam
	if err := coerceSubiaoParam(value, &p); err != nil {
		return nil, err
	}
	if len(p.MiddleReserved) > 3 {
		return nil, fmt.Errorf("F365 reserved field at offset 23 must be at most 3 bytes, got %d", len(p.MiddleReserved))
	}
	if len(p.TailReserved) > subiaoDSMParamLen-subiaoDSMParamMinLen {
		return nil, fmt.Errorf("F365 tail reserved field must be at most %d bytes, got %d", subiaoDSMParamLen-subiaoDSMParamMinLen, len(p.TailReserved))
	}
	pkt := make([]byte, 0, subiaoDSMParamLen)
	pkt = hex.WriteByte(pkt, p.AlarmSpeedThreshold)
	pkt = hex.WriteByte(pkt, p.AlarmVolume)
	pkt = hex.WriteByte(pkt, p.ActivePhotoStrategy)
	pkt = hex.WriteWord(pkt, p.ActiveTimedPhotoInterval)
	pkt = hex.WriteWord(pkt, p.ActiveDistancePhotoInterval)
	pkt = hex.WriteByte(pkt, p.SingleActivePhotoCount)
	pkt = hex.WriteByte(pkt, p.SingleActivePhotoInterval)
	pkt = hex.WriteByte(pkt, p.PhotoResolution)
	pkt = hex.WriteByte(pkt, p.VideoResolution)
	pkt = hex.WriteDoubleWord(pkt, p.AlarmEnable)
	pkt = hex.WriteDoubleWord(pkt, p.EventEnable)
	pkt = hex.WriteWord(pkt, p.SmokingAlarmInterval)
	pkt = hex.WriteWord(pkt, p.PhoneCallAlarmInterval)
	pkt = append(pkt, p.MiddleReserved...)
	pkt = append(pkt, make([]byte, 3-len(p.MiddleReserved))...)
	pkt = hex.WriteByte(pkt, p.FatigueSpeedThreshold)
	pkt = hex.WriteByte(pkt, p.FatigueVideoDuration)
	pkt = hex.WriteByte(pkt, p.FatiguePhotoCount)
	pkt = hex.WriteByte(pkt, p.FatiguePhotoInterval)
	pkt = hex.WriteByte(pkt, p.PhoneCallSpeedThreshold)
	pkt = hex.WriteByte(pkt, p.PhoneCallVideoDuration)
	pkt = hex.WriteByte(pkt, p.PhoneCallPhotoCount)
	pkt = hex.WriteByte(pkt, p.PhoneCallPhotoInterval)
	pkt = hex.WriteByte(pkt, p.SmokingSpeedThreshold)
	pkt = hex.WriteByte(pkt, p.SmokingVideoDuration)
	pkt = hex.WriteByte(pkt, p.SmokingPhotoCount)
	pkt = hex.WriteByte(pkt, p.SmokingPhotoInterval)
	pkt = hex.WriteByte(pkt, p.DistractedSpeedThreshold)
	pkt = hex.WriteByte(pkt, p.DistractedVideoDuration)
	pkt = hex.WriteByte(pkt, p.DistractedPhotoCount)
	pkt = hex.WriteByte(pkt, p.DistractedPhotoInterval)
	pkt = hex.WriteByte(pkt, p.AbnormalDrivingSpeedThreshold)
	pkt = hex.WriteByte(pkt, p.AbnormalDrivingVideoDuration)
	pkt = hex.WriteByte(pkt, p.AbnormalDrivingPhotoCount)
	pkt = hex.WriteByte(pkt, p.AbnormalDrivingPhotoInterval)
	pkt = hex.WriteByte(pkt, p.DriverIdentityRecognitionTrigger)
	pkt = append(pkt, p.TailReserved...)
	pkt = append(pkt, make([]byte, subiaoDSMParamLen-subiaoDSMParamMinLen-len(p.TailReserved))...)
	return pkt, nil
}

func decodeSubiaoTPMSParam(b []byte) (any, error) {
	if len(b) < subiaoTPMSParamMinLen {
		return nil, fmt.Errorf("%w: F366 requires at least %d bytes, got %d", ErrDecodeDeviceParams, subiaoTPMSParamMinLen, len(b))
	}
	idx := 0
	p := SubiaoTPMSParam{}
	p.TireSpecification = string(hex.ReadBytes(b, &idx, 12))
	p.TirePressureUnit = hex.ReadWord(b, &idx)
	p.NormalPressure = hex.ReadWord(b, &idx)
	p.ImbalanceThreshold = hex.ReadWord(b, &idx)
	p.SlowLeakThreshold = hex.ReadWord(b, &idx)
	p.LowPressureThreshold = hex.ReadWord(b, &idx)
	p.HighPressureThreshold = hex.ReadWord(b, &idx)
	p.HighTemperatureThreshold = hex.ReadWord(b, &idx)
	p.VoltageThreshold = hex.ReadWord(b, &idx)
	p.TimedReportInterval = hex.ReadWord(b, &idx)
	p.Reserved = subiaoReservedBytes(b, idx, subiaoTPMSParamLen-subiaoTPMSParamMinLen)
	return p, nil
}

func encodeSubiaoTPMSParam(value any) ([]byte, error) {
	var p SubiaoTPMSParam
	if err := coerceSubiaoParam(value, &p); err != nil {
		return nil, err
	}
	spec := []byte(p.TireSpecification)
	if len(spec) > 12 {
		return nil, fmt.Errorf("F366 tire specification must be at most 12 bytes, got %d", len(spec))
	}
	if len(p.Reserved) > subiaoTPMSParamLen-subiaoTPMSParamMinLen {
		return nil, fmt.Errorf("F366 reserved field must be at most %d bytes, got %d", subiaoTPMSParamLen-subiaoTPMSParamMinLen, len(p.Reserved))
	}
	pkt := make([]byte, 0, subiaoTPMSParamLen)
	pkt = append(pkt, spec...)
	pkt = append(pkt, make([]byte, 12-len(spec))...)
	pkt = hex.WriteWord(pkt, p.TirePressureUnit)
	pkt = hex.WriteWord(pkt, p.NormalPressure)
	pkt = hex.WriteWord(pkt, p.ImbalanceThreshold)
	pkt = hex.WriteWord(pkt, p.SlowLeakThreshold)
	pkt = hex.WriteWord(pkt, p.LowPressureThreshold)
	pkt = hex.WriteWord(pkt, p.HighPressureThreshold)
	pkt = hex.WriteWord(pkt, p.HighTemperatureThreshold)
	pkt = hex.WriteWord(pkt, p.VoltageThreshold)
	pkt = hex.WriteWord(pkt, p.TimedReportInterval)
	pkt = append(pkt, p.Reserved...)
	pkt = append(pkt, make([]byte, subiaoTPMSParamLen-subiaoTPMSParamMinLen-len(p.Reserved))...)
	return pkt, nil
}

func decodeSubiaoBSDParam(b []byte) (any, error) {
	if len(b) < subiaoBSDParamLen {
		return nil, fmt.Errorf("%w: F367 requires at least %d bytes, got %d", ErrDecodeDeviceParams, subiaoBSDParamLen, len(b))
	}
	idx := 0
	return SubiaoBSDParam{
		RearApproachAlarmTimeThreshold:     hex.ReadByte(b, &idx),
		SideRearApproachAlarmTimeThreshold: hex.ReadByte(b, &idx),
	}, nil
}

func encodeSubiaoBSDParam(value any) ([]byte, error) {
	var p SubiaoBSDParam
	if err := coerceSubiaoParam(value, &p); err != nil {
		return nil, err
	}
	pkt := make([]byte, 0, subiaoBSDParamLen)
	pkt = hex.WriteByte(pkt, p.RearApproachAlarmTimeThreshold)
	pkt = hex.WriteByte(pkt, p.SideRearApproachAlarmTimeThreshold)
	return pkt, nil
}

// coerceSubiaoParam 把解码得到的结构体或 API 传入的 JSON map 统一转换为目标结构体。
func coerceSubiaoParam(value any, dst any) error {
	if value == nil {
		return fmt.Errorf("parameter value is nil")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cannot encode parameter value: %w", err)
	}
	if err := json.Unmarshal(encoded, dst); err != nil {
		return fmt.Errorf("cannot decode parameter value: %w", err)
	}
	return nil
}

// subiaoReservedBytes 复制保留字段原始字节，最多 maxLen 字节。
func subiaoReservedBytes(b []byte, offset, maxLen int) []byte {
	if offset >= len(b) || maxLen <= 0 {
		return nil
	}
	end := offset + maxLen
	if end > len(b) {
		end = len(b)
	}
	return append([]byte(nil), b[offset:end]...)
}
