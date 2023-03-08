package model

import (
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

var (
	ErrDecodeDeviceParams = errors.New("Fail to decode device params")
)

type DeviceParams struct {
	DevicePhone string       `json:"-"`        // 关联device phone
	ParamCnt    uint8        `json:"paramCnt"` // 参数项个数
	Params      []*ParamData `json:"params"`   // 参数项列表
}

func (a *DeviceParams) Decode(phone string, cnt uint8, pkt []byte) error {
	a.DevicePhone = phone
	a.ParamCnt = cnt
	for i := 0; i < int(cnt); i++ {
		param := &ParamData{}
		err := param.Decode(pkt)
		if err != nil {
			return err
		}

		a.Params = append(a.Params, param)
	}
	return nil
}

func (a *DeviceParams) Encode() (pkt []byte, err error) {
	pkt = hex.WriteByte(pkt, a.ParamCnt)
	for _, arg := range a.Params {
		paramBytes, err := arg.Encode()
		if err != nil {
			// skip this err
			log.Error().Err(err).Str("device", a.DevicePhone).Msg("Fail to encode device param")
			continue
		}
		pkt = hex.WriteBytes(pkt, paramBytes)
	}
	return nil, nil
}

type ParamData struct {
	ParamID    uint32 `json:"paramId"`    // 参数ID
	ParamLen   uint8  `json:"paramLen"`   // 参数长度
	ParamValue any    `json:"paramValue"` // 参数值
}

func (a *ParamData) Decode(pkt []byte) error {
	idx := 0
	a.ParamID = hex.ReadDoubleWord(pkt, &idx)
	a.ParamLen = hex.ReadByte(pkt, &idx)
	if fn, ok := argTable[a.ParamID]; ok {
		a.ParamValue = fn(pkt, &idx, int(a.ParamLen))
	}
	return nil
}

func (a *ParamData) Encode() (pkt []byte, err error) {
	return nil, nil
}

type paramFn func([]byte, *int, int) any

var argTable = map[uint32]paramFn{
	// 终端心跳发送间隔,单位为秒(s)
	0x0001: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// TCP消息应答超时时间,单位为秒(s)
	0x0002: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// TCP消息重传次数
	0x0003: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// UDP消息应答超时时间,单位为秒(s)
	0x0004: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// UDP消息重传次数
	0x0005: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// SMS消息应答超时时间,单位为秒(s)
	0x0006: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// SMS消息重传次数
	0x0007: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 主服务器APN,无线通信拨号访问点.若网络制式为CDMA,则该处为PPP拨号号码
	0x0010: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadGBK(b, idx, paramLen) }),
	// 主服务器无线通信拨号用户名
	0x0011: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadGBK(b, idx, paramLen) }),
	// 主服务器无线通信拨号密码
	0x0012: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadGBK(b, idx, paramLen) }),
	// 主服务器地址,IP或域名
	0x0013: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadGBK(b, idx, paramLen) }),
	// 备份服务器APN,无线通信拨号访问点
	0x0014: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadGBK(b, idx, paramLen) }),
	// 备份服务器无线通信拨号用户名
	0x0015: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadGBK(b, idx, paramLen) }),
	// 备份服务器无线通信拨号密码
	0x0016: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadGBK(b, idx, paramLen) }),
	// 备份服务器地址,IP或域名(2019版以冒号分割主机和端口,多个服务器使用分号分隔)
	0x0017: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadGBK(b, idx, paramLen) }),
	// (JTT2013)服务器TCP端口
	0x0018: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadGBK(b, idx, paramLen) }),
	// (JTT2013)服务器UDP端口
	0x0019: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadGBK(b, idx, paramLen) }),
	// 道路运输证IC卡认证主服务器IP地址或域名
	0x001A: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadGBK(b, idx, paramLen) }),
	// 道路运输证IC卡认证主服务器TCP端口
	0x001B: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 道路运输证IC卡认证主服务器UDP端口
	0x001C: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 道路运输证IC卡认证主服务器IP地址或域名,端口同主服务器
	0x001D: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadGBK(b, idx, paramLen) }),
	// 位置汇报策略：0.定时汇报 1.定距汇报 2.定时和定距汇报
	0x0020: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 位置汇报方案：0.根据ACC状态 1.根据登录状态和ACC状态,先判断登录状态,若登录再根据ACC状态
	0x0021: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 驾驶员未登录汇报时间间隔,单位为秒(s),>0
	0x0022: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// (JTT2019)从服务器APN.该值为空时,终端应使用主服务器相同配置
	0x0023: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadGBK(b, idx, paramLen) }),
	// (JTT2019)从服务器无线通信拨号用户名.该值为空时,终端应使用主服务器相同配置
	0x0024: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadGBK(b, idx, paramLen) }),
	// (JTT2019)从服务器无线通信拨号密码.该值为空时,终端应使用主服务器相同配置
	0x0025: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadGBK(b, idx, paramLen) }),
	// (JTT2019)从服务器备份地址、IP或域名.主服务器IP地址或域名,端口同主服务器
	0x0026: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadGBK(b, idx, paramLen) }),
	// 休眠时汇报时间间隔,单位为秒(s),>0
	0x0027: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 紧急报警时汇报时间间隔,单位为秒(s),>0
	0x0028: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 缺省时间汇报间隔,单位为秒(s),>0
	0x0029: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 缺省距离汇报间隔,单位为米(m),>0
	0x002C: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 驾驶员未登录汇报距离间隔,单位为米(m),>0
	0x002D: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 休眠时汇报距离间隔,单位为米(m),>0
	0x002E: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 紧急报警时汇报距离间隔,单位为米(m),>0
	0x002F: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 拐点补传角度,<180°
	0x0030: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 电子围栏半径,单位为米
	0x0031: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadWord(b, idx) }),
	// (JTT2019)违规行驶时段范围,精确到分。
	//   byte1：违规行驶开始时间的小时部分；
	//   byte2：违规行驶开始的分钟部分；
	//   byte3：违规行驶结束时间的小时部分；
	//   byte4：违规行驶结束时间的分钟部分。
	0x0032: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadString(b, idx, paramLen) }),
	// 监控平台电话号码
	0x0040: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadGBK(b, idx, paramLen) }),
	// 复位电话号码,可采用此电话号码拨打终端电话让终端复位
	0x0041: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadGBK(b, idx, paramLen) }),
	// 恢复出厂设置电话号码,可采用此电话号码拨打终端电话让终端恢复出厂设置
	0x0042: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadGBK(b, idx, paramLen) }),
	// 监控平台SMS电话号码
	0x0043: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadGBK(b, idx, paramLen) }),
	// 接收终端SMS文本报警号码
	0x0044: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadGBK(b, idx, paramLen) }),
	// 终端电话接听策略,0.自动接听 1.ACC ON时自动接听,OFF时手动接听
	0x0045: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 每次最长通话时间,单位为秒(s),0为不允许通话,0xFFFFFFFF为不限制
	0x0046: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 当月最长通话时间,单位为秒(s),0为不允许通话,0xFFFFFFFF为不限制
	0x0047: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 监听电话号码
	0x0048: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadGBK(b, idx, paramLen) }),
	// 监管平台特权短信号码
	0x0049: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadGBK(b, idx, paramLen) }),
	// 报警屏蔽字.与位置信息汇报消息中的报警标志相对应,相应位为1则相应报警被屏蔽
	0x0050: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 报警发送文本SMS开关,与位置信息汇报消息中的报警标志相对应,相应位为1则相应报警时发送文本SMS
	0x0051: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 报警拍摄开关,与位置信息汇报消息中的报警标志相对应,相应位为1则相应报警时摄像头拍摄
	0x0052: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 报警拍摄存储标志,与位置信息汇报消息中的报警标志相对应,相应位为1则对相应报警时牌的照片进行存储,否则实时长传
	0x0053: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 关键标志,与位置信息汇报消息中的报警标志相对应,相应位为1则对相应报警为关键报警
	0x0054: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 最高速度，单位为千米每小时(km/h)
	0x0055: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 超速持续时间,单位为秒(s)
	0x0056: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 连续驾驶时间门限,单位为秒(s)
	0x0057: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 当天累计驾驶时间门限,单位为秒(s)
	0x0058: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 最小休息时间,单位为秒(s)
	0x0059: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 最长停车时间,单位为秒(s)
	0x005A: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 超速预警差值
	0x005B: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadWord(b, idx) }),
	// 疲劳驾驶预警插值
	0x005C: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadWord(b, idx) }),
	// 碰撞报警参数
	0x005D: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadWord(b, idx) }),
	// 侧翻报警参数
	0x005E: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadWord(b, idx) }),
	// 定时拍照参数
	0x0064: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 定距拍照参数
	0x0065: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 视频质量,1~10,1最好
	0x0070: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 亮度,0~255
	0x0071: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 对比度,0~127
	0x0072: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 饱和度,0~127
	0x0073: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 色度,0~255
	0x0074: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 车辆里程表读数，1/10km
	0x0080: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// 车辆所在的省域ID
	0x0081: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadWord(b, idx) }),
	// 车辆所在的省域ID
	0x0082: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadWord(b, idx) }),
	// 公安交通管理部门颁发的机动车号牌
	0x0083: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadGBK(b, idx, paramLen) }),
	// 车牌颜色，按照JT/T415-2006的5.4.12
	0x0084: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadByte(b, idx) }),
	// GNSS定位模式，定义如下：
	//   bit0，0:禁用GPS定位，1:启用 GPS 定位;
	//   bit1，0:禁用北斗定位，1:启用北斗定位;
	//   bit2，0:禁用GLONASS 定位，1:启用GLONASS定位;
	//   bit3，0:禁用Galileo定位，1:启用Galileo定位
	0x0090: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadByte(b, idx) }),
	// GNSS波特率，定义如下：
	//   0x00:4800;
	//   0x01:9600;
	//   0x02:19200;
	//   0x03:38400;
	//   0x04:57600;
	//   0x05:115200
	0x0091: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadByte(b, idx) }),
	// GNSS模块详细定位数据输出频率，定义如下：
	//   0x00:500ms;
	//   0x01:1000ms(默认值);
	//   0x02:2000ms;
	//   0x03:3000ms;
	//   0x04:4000ms
	0x0092: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadByte(b, idx) }),
	// GNSS模块详细定位数据采集频率，单位为秒，默认为 1。
	0x0093: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// GNSS模块详细定位数据上传方式:
	//   0x00，本地存储，不上传(默认值);
	//   0x01，按时间间隔上传;
	//   0x02，按距离间隔上传;
	//   0x0B，按累计时间上传，达到传输时间后自动停止上传;
	//   0x0C，按累计距离上传，达到距离后自动停止上传;
	//   0x0D，按累计条数上传，达到上传条数后自动停止上传。
	0x0094: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadByte(b, idx) }),
	// GNSS模块详细定位数据上传设置, 关联0x0094:
	// 上传方式为 0x01 时，单位为秒;
	// 上传方式为 0x02 时，单位为米;
	// 上传方式为 0x0B 时，单位为秒;
	// 上传方式为 0x0C 时，单位为米;
	// 上传方式为 0x0D 时，单位为条。
	0x0095: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// CAN总线通道1采集时间间隔(ms)，0表示不采集
	0x0100: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// CAN总线通道1上传时间间隔(s)，0表示不上传
	0x0101: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadWord(b, idx) }),
	// CAN总线通道2采集时间间隔(ms)，0表示不采集
	0x0102: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadDoubleWord(b, idx) }),
	// CAN总线通道2上传时间间隔(s)，0表示不上传
	0x0103: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadWord(b, idx) }),
	// CAN总线ID单独采集设置:
	//   bit63-bit32 表示此 ID 采集时间间隔(ms)，0 表示不采集;
	//   bit31 表示 CAN 通道号，0:CAN1，1:CAN2;
	//   bit30 表示帧类型，0:标准帧，1:扩展帧;
	//   bit29 表示数据采集方式，0:原始数据，1:采集区间的计算值;
	//   bit28-bit0 表示 CAN 总线 ID。
	0x0110: paramFn(func(b []byte, idx *int, paramLen int) any { return hex.ReadString(b, idx, paramLen) }),
}
