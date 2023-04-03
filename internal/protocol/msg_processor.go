package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hash"
	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

var (
	ErrMsgIDNotSupportted = errors.New("Msg id is not supportted") // 消息ID无法处理，应忽略
	ErrNotAuthorized      = errors.New("Not authorized")           // server校验鉴权不通过
	ErrActiveClose        = errors.New("Active close")             // client无法继续处理，应主动关闭连接
)

// 处理消息的Handler接口
type MsgProcessor interface {
	Process(ctx context.Context, pkt *model.PacketData) (*model.ProcessData, error)
}

// 消息处理方法调用表, <msgId, action>
type processOptions map[uint16]*action

type action struct {
	genData func() *model.ProcessData                       // 定义生成消息的类型。由于go不支持type作为参数，所以这里直接初始化结构体
	process func(context.Context, *model.ProcessData) error // 处理消息的逻辑。可以设置消息字段、根据消息做相应处理逻辑
}

// 表驱动，初始化消息处理方法组
func initProcessOption() processOptions {
	options := make(processOptions)
	options[0x0001] = &action{ // 通用应答
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0001{}} // 无需回复
		},
	}
	options[0x0002] = &action{ // 心跳
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0002{}, Outgoing: &model.Msg8001{}}
		},
		process: processMsg0002,
	}
	options[0x0003] = &action{ // 注销
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0003{}, Outgoing: &model.Msg8001{}}
		},
		process: processMsg0003,
	}
	options[0x0100] = &action{ // 注册
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0100{}, Outgoing: &model.Msg8100{}}
		},
		process: processMsg0100,
	}
	options[0x0102] = &action{ // 鉴权
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0102{}, Outgoing: &model.Msg8001{}}
		},
		process: processMsg0102,
	}
	options[0x0104] = &action{ // 查询终端参数应答
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0104{}} // 无需回复
		},
	}
	options[0x0200] = &action{ // 位置信息上报
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0200{}, Outgoing: &model.Msg8001{}}
		},
		process: processMsg0200,
	}
	options[0x1205] = &action{ // 终端上传音视频资源列表
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg1205{}} // 无需回复
		},
	}
	options[0x8001] = &action{ // 通用应答
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8001{}}
		},
		process: processMsg8001,
	}
	options[0x8100] = &action{ // 注册应答
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8100{}, Outgoing: &model.Msg0102{}}
		},
		process: processMsg8100,
	}
	options[0x8103] = &action{ // 查询终端参数
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8103{}, Outgoing: &model.Msg0001{}}
		},
		process: processMsg8103,
	}
	options[0x8104] = &action{ // 查询终端参数
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8104{}, Outgoing: &model.Msg0104{}}
		},
		process: processMsg8104,
	}
	options[0x9205] = &action{ // 查询终端音视频资源列表
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg9205{}, Outgoing: &model.Msg1205{}}
		},
		process: processMsg9205,
	}

	return options
}

// 处理jt808消息的Handler方法
type JT808MsgProcessor struct {
	options processOptions
}

// processor单例
var jt808MsgProcessorSingleton *JT808MsgProcessor
var processorInitOnce sync.Once

func NewJT808MsgProcessor() *JT808MsgProcessor {
	processorInitOnce.Do(func() {
		jt808MsgProcessorSingleton = &JT808MsgProcessor{
			options: initProcessOption(),
		}
	})
	return jt808MsgProcessorSingleton
}

func (mp *JT808MsgProcessor) Process(ctx context.Context, pkt *model.PacketData) (*model.ProcessData, error) {
	msgID := pkt.Header.MsgID
	if _, ok := mp.options[msgID]; !ok {
		return nil, ErrMsgIDNotSupportted
	}

	// process segment packet
	if !pkt.SegCompleted {
	}

	act := mp.options[msgID]
	genDataFn := act.genData
	if genDataFn == nil {
		return nil, ErrMsgIDNotSupportted
	}
	data := genDataFn()

	in := data.Incoming
	err := in.Decode(pkt)
	if err != nil {
		return nil, errors.Wrap(err, "Fail to decode packet to jtmsg")
	}

	if log.Logger.GetLevel() == zerolog.DebugLevel {
		// print log of msg content
		session := ctx.Value(model.SessionCtxKey{}).(*model.Session)
		inJSON, err := json.Marshal(in)
		if err != nil {
			return nil, errors.Wrap(err, "Fail to serialize incoming msg to json")
		}
		// for debug
		log.Debug().Str("id", session.ID).Str("RawMsgID", fmt.Sprintf("0x%04x", in.GetHeader().MsgID)).RawJSON("incoming", inJSON).Msg("Received jt808 msg.")
	}

	// 生成待回复的消息
	if data.Outgoing != nil {
		out := data.Outgoing
		err = out.GenOutgoing(in)
		if err != nil {
			return data, errors.Wrap(err, "Fail to generate outgoing msg")
		}

		// print log of outgoing content
		defer func() {
			if out == nil || log.Logger.GetLevel() != zerolog.DebugLevel {
				return
			}

			outJSON, _ := json.Marshal(out)
			session := ctx.Value(model.SessionCtxKey{}).(*model.Session)
			// for debug
			log.Debug().Str("id", session.ID).Str("RawMsgID", fmt.Sprintf("0x%04x", out.GetHeader().MsgID)).RawJSON("outgoing", outJSON).
				Msg("Generating jt808 outgoing msg.")
		}()
	}

	// 对消息按类别做特殊处理
	processFunc := act.process
	if processFunc == nil {
		return data, nil
	}
	err = processFunc(ctx, data)
	if err != nil {
		return data, errors.Wrap(err, "Fail to process data")
	}
	if data.Outgoing == nil {
		return nil, nil // 此类型msg不需要回复
	}
	return data, nil
}

func processSegmentPacket() {}

// 收到心跳，应刷新终端缓存有效期
func processMsg0002(_ context.Context, data *model.ProcessData) error {
	cache := storage.GetDeviceCache()
	device, err := cache.GetDeviceByPhone(data.Incoming.GetHeader().PhoneNumber)

	// 缓存不存在，说明设备不合法，需要返回错误，让服务层处理关闭
	if errors.Is(err, storage.ErrDeviceNotFound) {
		return errors.Wrapf(err, "Fail to find device cache, phoneNumber=%s", data.Incoming.GetHeader().PhoneNumber)
	}

	device.LastestComTime = time.Now()
	cache.CacheDevice(device)

	return nil
}

// 收到注销，应清除缓存，断开连接。
func processMsg0003(_ context.Context, data *model.ProcessData) error {
	cache := storage.GetDeviceCache()
	device, err := cache.GetDeviceByPhone(data.Incoming.GetHeader().PhoneNumber)
	// 缓存不存在，说明设备不合法，需要返回错误，让服务层处理关闭
	if errors.Is(err, storage.ErrDeviceNotFound) {
		return errors.Wrapf(err, "Fail to find device cache, phoneNumber=%s", data.Incoming.GetHeader().PhoneNumber)
	}
	// 取消定时任务
	timer := NewKeepaliveTimer()
	timer.Cancel(device.Phone)
	// 清楚缓存
	cache.DelDeviceByPhone(device.Phone)
	// 为避免连接TIMEWAIT，应等待对方主动关闭
	return nil
}

// 收到注册，应校验设备ID，如果可注册，则缓存设备信息并返回鉴权码
func processMsg0100(ctx context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg0100)

	cache := storage.GetDeviceCache()
	// 校验注册逻辑
	out := data.Outgoing.(*model.Msg8100)
	// 车辆已被注册
	if cache.HasPlate(in.PlateNumber) {
		out.Result = model.ResCarAlreadyRegister
		return nil
	}
	// 终端已被注册
	if cache.HasPhone(in.Header.PhoneNumber) {
		out.Result = model.ResDeviceAlreadyRegister
		return nil
	}

	session := ctx.Value(model.SessionCtxKey{}).(*model.Session)
	device := model.NewDevice(in, session)
	out.AuthCode = genAuthCode(device) // 设置鉴权码

	cache.CacheDevice(device)

	timer := NewKeepaliveTimer()
	timer.Register(device.Phone)
	return nil
}

// 收到鉴权，应校验鉴权token
func processMsg0102(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg0102)

	cache := storage.GetDeviceCache()
	device, err := cache.GetDeviceByPhone(in.Header.PhoneNumber)
	// 缓存不存在，说明设备不合法，需要返回错误，让服务层处理关闭
	if errors.Is(err, storage.ErrDeviceNotFound) {
		return errors.Wrapf(err, "Fail to find device cache, phoneNumber=%s", in.Header.PhoneNumber)
	}

	out := data.Outgoing.(*model.Msg8001)
	// 校验鉴权逻辑
	if in.AuthCode != genAuthCode(device) {
		out.Result = model.ResultFail
		// 取消定时任务
		timer := NewKeepaliveTimer()
		timer.Cancel(device.Phone)
		// 删除设备缓存
		cache.DelDeviceByPhone(device.Phone)
	} else {
		// 鉴权通过
		device.Status = model.DeviceStatusOnline
		device.LastestComTime = time.Now()
		device.AuthCode = in.AuthCode
		device.IMEI = in.IMEI
		device.SoftwareVersion = in.SoftwareVersion
		cache.CacheDevice(device)
	}

	return nil
}

func genAuthCode(d *model.Device) string {
	var splitByte byte = '_'
	codeBuilder := new(strings.Builder)
	codeBuilder.WriteString(d.ID)
	codeBuilder.WriteByte(splitByte)
	codeBuilder.WriteString(d.Plate)
	codeBuilder.WriteByte(splitByte)
	codeBuilder.WriteString(d.Phone)
	return strconv.Itoa(int(hash.FNV32(codeBuilder.String())))
}

// 收到查询终端参数应答，无需回复，可以在这里做一个一个channel write，由其他地方阻塞式read来完成hook功能。
func processMsg0104(_ context.Context, _ *model.ProcessData) error {
	// todo: write channel
	return nil
}

// 收到位置信息汇报，回复通用应答
func processMsg0200(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg0200)

	cache := storage.GetDeviceCache()
	device, err := cache.GetDeviceByPhone(in.Header.PhoneNumber)
	// 缓存不存在，说明设备不合法，需要返回错误，让服务层处理关闭
	if errors.Is(err, storage.ErrDeviceNotFound) {
		return errors.Wrapf(err, "Fail to find device cache, phoneNumber=%s", in.Header.PhoneNumber)
	}

	// 解析状态位编码
	dg := &model.DeviceGeo{}
	err = dg.Decode(device.Phone, in)
	if err != nil {
		return errors.Wrapf(err, "Fail to decode device geo, phoneNumber=%s", device.Phone)
	}

	if dg.Geo.ACCStatus == 0 { // ACC关闭，设备休眠
		device.Status = model.DeviceStatusSleeping
		device.LastestComTime = time.Now()
		cache.CacheDevice(device)
	}

	geoCache := storage.GetGeoCache()
	rb := geoCache.GetGeoRingByPhone(device.Phone)
	rb.Write(dg)

	return nil
}

func processMsg8001(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg8001)
	// 收到8001消息，说明此时是作为终端设备
	if in.Result == model.ResultSuccess {
		// 回复成功，说明之前注册成功，为方便后续处理，将设备状态改为Online并缓存
		cache := storage.GetDeviceCache()
		device, err := cache.GetDeviceByPhone(in.Header.PhoneNumber)
		if errors.Is(err, storage.ErrDeviceNotFound) {
			return ErrActiveClose
		}
		if device.Status != model.DeviceStatusOffline {
			return nil
		}

		// 仅在未注册成功时执行一次
		device.Status = model.DeviceStatusOnline
		device.LastestComTime = time.Now()
		cache.CacheDevice(device)
	}

	return nil
}

// 收到注册应答，回复鉴权
func processMsg8100(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg8100)
	out := data.Outgoing.(*model.Msg0102)

	cache := storage.GetDeviceCache()
	device, err := cache.GetDeviceByPhone(in.Header.PhoneNumber)
	if errors.Is(err, storage.ErrDeviceNotFound) {
		return ErrActiveClose
	}

	out.IMEI = device.IMEI
	out.SoftwareVersion = device.SoftwareVersion

	return nil
}

// 收到设置终端参数请求，回复通用应答
func processMsg8103(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg8103)
	paramCache := storage.GetDeviceParamsCache()
	params, err := paramCache.GetDeviceParamsByPhone(in.GetHeader().PhoneNumber)
	if errors.Is(err, storage.ErrDeviceParamsNotFound) {
		params = in.Parameters
	} else {
		params.Update(in.Parameters)
	}
	paramCache.CacheDeviceParams(params)

	return nil
}

// 收到查询终端参数请求，回复终端参数(此时是作为client进程)
func processMsg8104(_ context.Context, data *model.ProcessData) error {
	// todo: generate by config
	out := data.Outgoing.(*model.Msg0104)
	paramCache := storage.GetDeviceParamsCache()
	params, err := paramCache.GetDeviceParamsByPhone(out.GetHeader().PhoneNumber)
	if errors.Is(err, storage.ErrDeviceParamsNotFound) {
		out.Parameters = &model.DeviceParams{}
		// 模拟一个固定的参数
		paramCnt := 39
		paramByteStr := "00000001044B687673" +
			"00000002046E764E65" +
			"000000030449716B57" +
			"000000040452704A36" +
			"0000000504524D6D52" +
			"00000006043535774E" +
			"00000007044743525A" +
			"000000101058676767375A3558584B44376E625661" +
			"000000111035625067564C37537A4C616672774E36" +
			"000000121048684E7669576777494D6E493555624D" +
			"000000131064527432655967746479316A46485745" +
			"0000001410676554657949565F7A537362694A6E54" +
			"0000001510556F6868585171533565773575385562" +
			"000000161068336D7550796A6E584D387933587173" +
			"000000171075397A57597452444C4863614E6A5278" +
			"00000018044E586A4F000000190438646C69" +
			"0000001A104E30775635786D4A664D6F6338616650" +
			"0000001B04626C3676" +
			"0000001C0438366537" +
			"0000001D104F344C42704B737445687A4F4C564635" +
			"0000002004386C5255" +
			"00000021045041666E" +
			"000000220457456B39" +
			"00000023106138334C43765737546D5051736E3152" +
			"0000002410427559785F4937584E694F63364E4D49" +
			"0000002510653756707A595579763576596433346C" +
			"0000002610544E376C476643386A4B4D496E4C3967" +
			"00000027045057586E0000002804674C636C" +
			"0000002904474F6A64" +
			"0000002C0445493157" +
			"0000002D04634E507A" +
			"0000002E0453445442" +
			"0000002F04525F647A" +
			"00000030045A4A6830" +
			"00000031026C64" +
			"000000320409302130" +
			"0000007623030000010100010202000103030001"
		_ = out.Parameters.Decode(out.Header.PhoneNumber, uint8(paramCnt), hex.Str2Byte(paramByteStr))
		paramCache.CacheDeviceParams(out.Parameters)
	} else {
		out.Parameters = params
	}

	return nil
}

func processMsg9205(ctx context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg9205)
	out := data.Outgoing.(*model.Msg1205)
	out.MediaCount = 1
	out.LogicChannelID = in.LogicChannelID
	// todo, generate several start-end time pair by input time range
	out.StartTime = in.StartTime
	out.EndTime = in.EndTime
	out.AlarmSign = 0
	out.AlarmSignExt = 0
	out.MediaType = 0
	out.StreamType = 0
	out.StorageType = 0

	return nil
}
