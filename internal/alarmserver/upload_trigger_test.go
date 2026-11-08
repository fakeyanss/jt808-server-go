package alarmserver

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	actionpkg "github.com/fakeyanss/jt808-server-go/internal/action"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

type recordingHooks struct {
	alarms []actionpkg.AlarmEvent
}

func (h *recordingHooks) OnRegister(context.Context, actionpkg.RegisterEvent) {}
func (h *recordingHooks) OnAuth(context.Context, actionpkg.AuthEvent)         {}
func (h *recordingHooks) OnLocation(context.Context, actionpkg.LocationEvent) {}
func (h *recordingHooks) OnAlarm(_ context.Context, event actionpkg.AlarmEvent) {
	h.alarms = append(h.alarms, event)
}
func (h *recordingHooks) OnDisconnect(context.Context, actionpkg.DisconnectEvent) {}

func testUploadAlarmID() []byte {
	id := make([]byte, 16)
	for i := range id {
		id[i] = byte(i + 1)
	}
	return id
}

func newTestUploadTrigger(t *testing.T, send UploadSender, dedup time.Duration) (*UploadTrigger, *recordingHooks, *[]*model.Msg9208) {
	t.Helper()
	next := &recordingHooks{}
	var sent []*model.Msg9208
	trigger := NewUploadTrigger(next, UploadTriggerOptions{
		IP:          "10.0.0.8",
		TCPPort:     7611,
		UDPPort:     7612,
		DedupWindow: dedup,
	}, func(phone string, msg *model.Msg9208) error {
		sent = append(sent, msg)
		if send != nil {
			return send(phone, msg)
		}
		return nil
	})
	return trigger, next, &sent
}

func TestUploadTriggerSkipsWhenAlarmIDUnavailable(t *testing.T) {
	trigger, next, sent := newTestUploadTrigger(t, nil, time.Minute)
	event := actionpkg.AlarmEvent{Phone: "13800138000", AlarmSign: 0x01}
	trigger.OnAlarm(context.Background(), event)

	require.Len(t, next.alarms, 1, "原始告警事件仍应转发给下游 Hooks")
	require.Empty(t, *sent)
}

func TestUploadTriggerSendsOncePerAlarm(t *testing.T) {
	trigger, _, sent := newTestUploadTrigger(t, nil, time.Minute)
	alarmID := testUploadAlarmID()
	event := actionpkg.AlarmEvent{Phone: "13800138000", AlarmSign: 0x01, AlarmID: alarmID}

	trigger.OnAlarm(context.Background(), event)
	trigger.OnAlarm(context.Background(), event)

	require.Len(t, *sent, 1)
	msg := (*sent)[0]
	require.Equal(t, "10.0.0.8", msg.IP)
	require.Equal(t, uint16(7611), msg.TCPPort)
	require.Equal(t, uint16(7612), msg.UDPPort)
	require.Equal(t, alarmID, msg.AlarmSign)
	require.Equal(t, strings.ToUpper(hex.EncodeToString(alarmID)), msg.AlarmNo)
	require.Nil(t, msg.Header, "消息头应由发送方按当前会话补齐")
}

func TestUploadTriggerDifferentAlarmsAreSentSeparately(t *testing.T) {
	trigger, _, sent := newTestUploadTrigger(t, nil, time.Minute)
	first := testUploadAlarmID()
	second := append([]byte(nil), first...)
	second[0] ^= 0xFF

	trigger.OnAlarm(context.Background(), actionpkg.AlarmEvent{Phone: "13800138000", AlarmID: first})
	trigger.OnAlarm(context.Background(), actionpkg.AlarmEvent{Phone: "13800138000", AlarmID: second})

	require.Len(t, *sent, 2)
}

func TestUploadTriggerDedupWindowExpires(t *testing.T) {
	trigger, _, sent := newTestUploadTrigger(t, nil, time.Minute)
	current := time.Unix(1_700_000_000, 0)
	trigger.now = func() time.Time { return current }
	event := actionpkg.AlarmEvent{Phone: "13800138000", AlarmID: testUploadAlarmID()}

	trigger.OnAlarm(context.Background(), event)
	current = current.Add(59 * time.Second)
	trigger.OnAlarm(context.Background(), event)
	require.Len(t, *sent, 1, "去重窗口内不重复下发")

	current = current.Add(2 * time.Second)
	trigger.OnAlarm(context.Background(), event)
	require.Len(t, *sent, 2, "窗口过期后允许再次下发")
}

func TestUploadTriggerSendFailureOnlyLogs(t *testing.T) {
	trigger, _, sent := newTestUploadTrigger(t, func(string, *model.Msg9208) error {
		return errors.New("device is offline")
	}, time.Minute)
	event := actionpkg.AlarmEvent{Phone: "13800138000", AlarmID: testUploadAlarmID()}

	require.NotPanics(t, func() {
		trigger.OnAlarm(context.Background(), event)
		trigger.OnAlarm(context.Background(), event)
	})
	require.Len(t, *sent, 1, "首次上报只尝试下发一次，失败不重试")
}

func TestUploadTriggerNilSenderIsNoop(t *testing.T) {
	trigger := NewUploadTrigger(nil, UploadTriggerOptions{}, nil)
	require.NotPanics(t, func() {
		trigger.OnAlarm(context.Background(), actionpkg.AlarmEvent{AlarmID: testUploadAlarmID()})
	})
}
