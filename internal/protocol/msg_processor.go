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
	options[0x8104] = &action{ // 查询终端参数
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8104{}, Outgoing: &model.Msg0104{}}
		},
		process: processMsg8104,
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

// 收到心跳，应刷新终端缓存有效期
func processMsg0002(ctx context.Context, data *model.ProcessData) error {
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
func processMsg0003(ctx context.Context, data *model.ProcessData) error {
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
func processMsg0102(ctx context.Context, data *model.ProcessData) error {
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

// 收到位置信息汇报，回复通用应答
func processMsg0200(ctx context.Context, data *model.ProcessData) error {
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

func processMsg8001(ctx context.Context, data *model.ProcessData) error {
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
func processMsg8100(ctx context.Context, data *model.ProcessData) error {
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

// 收到查询终端参数请求，回复终端参数(此时是作为client进程)
func processMsg8104(ctx context.Context, data *model.ProcessData) error {
	out := data.Outgoing.(*model.Msg0104)
	// 模拟一个固定的参数
	// todo: generate by config
	params := []*model.ParamData{
		{
			ParamID:    0x0001,
			ParamLen:   4,
			ParamValue: uint32(0x49454252),
		},
	}
	out.Parameters = &model.DeviceParams{
		DevicePhone: out.Header.PhoneNumber,
		ParamCnt:    uint8(len(params)),
		Params:      params,
	}
	out.AnswerParamCnt = out.Parameters.ParamCnt
	return nil
}

// 收到查询终端参数应答，无需回复，可以在这里做一个一个channel write，由其他地方阻塞式read来完成hook功能。
func processMsg0104(ctx context.Context, data *model.ProcessData) error {
	// todo: write channel
	return nil
}
