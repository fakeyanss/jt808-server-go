package business

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	codechex "github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/fakeyanss/jt808-server-go/internal/domain"
	"github.com/fakeyanss/jt808-server-go/internal/protocol"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

func (p *Processor) processMsg0200(ctx context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg0200)

	devices := p.repos.Devices
	device, err := devices.GetByPhone(ctx, in.Header.PhoneNumber)
	// 缓存不存在，说明设备不合法，需要返回错误，让服务层处理关闭
	if errors.Is(err, repository.ErrDeviceNotFound) {
		return errors.Wrapf(err, "Fail to find device cache, phoneNumber=%s", in.Header.PhoneNumber)
	}
	if err := p.repos.Alarms.Record(ctx, in.Header.PhoneNumber, in.Header.SerialNumber, in.AlarmSign, in.Latitude, in.Longitude, in.Time); err != nil {
		// 报警落库失败需要上抛：at_least_once 分类下不应答，由终端重传。
		return errors.Wrapf(err, "record alarm, phoneNumber=%s", in.Header.PhoneNumber)
	}

	// 解析状态位编码
	dg := &model.DeviceGeo{}
	err = dg.Decode(device.Phone, in)
	if err != nil {
		return errors.Wrapf(err, "Fail to decode device geo, phoneNumber=%s", device.Phone)
	}

	// 与 Geo.Append 的单调守卫使用同一基准：旧位置不能把新在线态改回休眠。
	latestGeo, latestErr := p.repos.Geo.Latest(ctx, device.Phone)
	staleLocation := latestErr == nil && latestGeo != nil && dg.Time.Before(latestGeo.Time)
	if !staleLocation && dg.Geo.ACCStatus == 0 { // ACC关闭，设备休眠
		device.Status = domain.DeviceStatusSleeping
		device.LastestComTime = time.Now()
		_ = devices.Upsert(ctx, device)
	}

	_ = p.repos.Geo.Append(ctx, dg)
	return p.recordLocationHistory(ctx, 0x0200, []*model.Msg0200{in})
}

// processMsg0704 保存批量定位快照，并把每个数据项写入定位历史。
// 0x0704 数据项内没有独立消息流水号，无法与报警去重口径对齐，因此只记定位、不记报警。
func (p *Processor) processMsg0704(ctx context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg0704)
	p.repos.Industry.Record(in)

	locs := make([]*model.Msg0200, 0, len(in.Items))
	for i := range in.Items {
		locs = append(locs, &in.Items[i].Loc)
	}
	return p.recordLocationHistory(ctx, 0x0704, locs)
}

// recordLocationHistory 写入定位历史；memory 后端未启用 Locations 时跳过。
// 返回错误供 at_least_once 分类判定：落库失败时本次不应答，等待终端重传。
func (p *Processor) recordLocationHistory(ctx context.Context, msgID uint16, locs []*model.Msg0200) error {
	repo := p.repos.Locations
	if repo == nil || len(locs) == 0 {
		return nil
	}
	records := make([]repository.LocationRecord, 0, len(locs))
	event := protocol.UplinkEventFromContext(ctx)
	for index, loc := range locs {
		if loc.Header == nil {
			continue
		}
		records = append(records, repository.LocationRecord{
			Phone:      loc.Header.PhoneNumber,
			MsgID:      msgID,
			AlarmSign:  loc.AlarmSign,
			StatusSign: loc.StatusSign,
			Latitude:   loc.Latitude,
			Longitude:  loc.Longitude,
			Altitude:   loc.Altitude,
			Speed:      loc.Speed,
			Direction:  loc.Direction,
			ExtItems:   loc.ExtItems,
			OccurredAt: loc.Time,
			ReceivedAt: time.Now(),
			DedupKey:   locationDedupKey(event.EventID, index),
		})
	}
	if len(records) == 0 {
		return nil
	}
	if err := repo.Record(ctx, records); err != nil {
		return errors.Wrapf(err, "record location history, phoneNumber=%s, msgID=0x%04X", records[0].Phone, msgID)
	}
	return nil
}

// locationDedupKey 把事件 ID 与数据项下标压成短幂等键；没有事件上下文时返回空串，
// 写入 NULL 后不参与唯一约束。
func locationDedupKey(eventID string, itemIndex int) string {
	if eventID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d", eventID, itemIndex)))
	return hex.EncodeToString(sum[:])[:32]
}

// processIndustryReport 记录行业上报的内存快照；0x0108 升级结果额外落库。
func (p *Processor) processIndustryReport(ctx context.Context, data *model.ProcessData) error {
	p.repos.Industry.Record(data.Incoming)
	// 0x0108 升级结果需要跨重启保留，写入独立仓储而不是只留内存快照。
	if result, ok := data.Incoming.(*model.Msg0108); ok && result.Header != nil {
		return p.repos.UpgradeResults.Save(ctx, &repository.UpgradeResult{
			Phone:       result.Header.PhoneNumber,
			UpgradeType: result.UpgradeType,
			Result:      result.Result,
			Serial:      result.Header.SerialNumber,
			ReceivedAt:  time.Now(),
		})
	}
	return nil
}

// processMsg0801 把多媒体数据落盘并写入媒体文件仓储。
//
// 到达这里的 0x0801 都是完整消息：分包由 runtime 重组完成后再进入业务，
// 非分包单包即为完整文件，因此这里不需要再判断包序号。
// 行业内存快照保留原有行为，落盘失败时仍可通过 Industry 查到最近一条上报。
func (p *Processor) processMsg0801(ctx context.Context, data *model.ProcessData) error {
	upload, ok := data.Incoming.(*model.Msg0801)
	if !ok {
		return nil
	}
	p.repos.Industry.Record(upload)
	if p.mediaUpload == nil {
		log.Warn().Msg("media upload store not configured, skip 0x0801 persistence")
		return nil
	}
	file, err := p.mediaUpload.Save(upload)
	if err != nil {
		return err
	}
	return p.repos.MediaFiles.Save(ctx, file)
}

// processMsg0802 把 0x8802 检索命令的应答（0x0802）写入检索结果仓储。
// 行业内存快照保留原有行为；落库以 (phone, answerSerial) 为单位整体覆盖，
// 便于查询“终端最近一次返回了哪些本地媒体”。
func (p *Processor) processMsg0802(ctx context.Context, data *model.ProcessData) error {
	p.repos.Industry.Record(data.Incoming)
	result, ok := data.Incoming.(*model.Msg0802)
	if !ok || result.Header == nil {
		return nil
	}
	receivedAt := time.Now()
	records := make([]repository.MediaSearchRecord, 0, len(result.Items))
	for _, item := range result.Items {
		records = append(records, repository.MediaSearchRecord{
			MediaID:      item.MediaID,
			MediaType:    item.MediaType,
			ChannelID:    item.ChannelID,
			EventID:      item.EventID,
			AlarmSign:    item.Location.AlarmSign,
			StatusSign:   item.Location.StatusSign,
			Latitude:     item.Location.Latitude,
			Longitude:    item.Location.Longitude,
			Altitude:     item.Location.Altitude,
			Speed:        item.Location.Speed,
			Direction:    item.Location.Direction,
			LocationTime: parseMediaSearchLocationTime(item.Location.Time),
			ReceivedAt:   receivedAt,
		})
	}
	return p.repos.MediaSearches.Replace(ctx, result.Header.PhoneNumber, result.AnswerSerial, records)
}

// parseMediaSearchLocationTime 按固定 GMT+8 解析内嵌位置体的 BCD 时间；
// 缺失或非法时返回零值，避免把平台接收时间误当成终端时间。
func parseMediaSearchLocationTime(value string) time.Time {
	if len(value) != 12 {
		return time.Time{}
	}
	parsed, err := time.ParseInLocation("060102150405", value, codechex.ProtocolTimeZone)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

// processMsg0805 把摄像头立即拍摄命令应答写入拍摄记录，
// mediaId 与 0x0801 落盘的文件通过 (phone, mediaId) 关联，查询时再判断文件是否已到达。
func (p *Processor) processMsg0805(ctx context.Context, data *model.ProcessData) error {
	reply, ok := data.Incoming.(*model.Msg0805)
	if !ok || reply.Header == nil {
		return nil
	}
	p.repos.Industry.Record(reply)
	capture := &repository.MediaCapture{
		Phone:        reply.Header.PhoneNumber,
		RecordID:     strconv.FormatUint(uint64(reply.AnswerSerial), 10),
		AnswerSerial: reply.AnswerSerial,
		Result:       reply.Result,
		ReceivedAt:   time.Now(),
		Items:        make([]repository.MediaCaptureItem, 0, len(reply.MediaIDs)),
	}
	for _, mediaID := range reply.MediaIDs {
		capture.Items = append(capture.Items, repository.MediaCaptureItem{MediaID: mediaID})
	}
	return p.repos.MediaCaptures.SaveCapture(ctx, capture)
}

// processFileUploadNotice 记录 0x1206 文件上传完成通知，
// 供 API 查询与后续媒体接入使用，并落媒体元数据仓储。
func (p *Processor) processFileUploadNotice(ctx context.Context, data *model.ProcessData) error {
	p.repos.Industry.Record(data.Incoming)
	if notice, ok := data.Incoming.(*model.Msg1206); ok {
		return p.repos.Media.SaveUploadNotice(ctx, notice.Header.PhoneNumber, notice)
	}
	return nil
}

// processMediaResourceList 记录 0x1205 音视频资源列表，并整包覆盖媒体元数据仓储。
func (p *Processor) processMediaResourceList(ctx context.Context, data *model.ProcessData) error {
	p.repos.Industry.Record(data.Incoming)
	list, ok := data.Incoming.(*model.Msg1205)
	if !ok || list.Header == nil {
		return nil
	}
	return p.repos.Media.ReplaceResourceList(ctx, list.Header.PhoneNumber, list)
}

// processAttachmentSignaling 记录主链路上的苏标附件信令。
// 真正的附件接收、位图与 0x9212 应答在 internal/alarmserver 的独立端口完成。
func (p *Processor) processAttachmentSignaling(_ context.Context, data *model.ProcessData) error {
	if data == nil || data.Incoming == nil || data.Incoming.GetHeader() == nil {
		return nil
	}
	header := data.Incoming.GetHeader()
	log.Debug().Uint16("msg_id", header.MsgID).Str("phone", header.PhoneNumber).
		Msg("Attachment signaling received on main port, handled by alarm server")
	return nil
}
