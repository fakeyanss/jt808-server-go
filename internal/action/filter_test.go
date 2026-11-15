package action

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type recordingHooks struct {
	Noop
	calls []string
}

func (h *recordingHooks) OnAlarm(context.Context, AlarmEvent) {
	h.calls = append(h.calls, "alarm")
}

func (h *recordingHooks) OnWaybill(context.Context, WaybillEvent) {
	h.calls = append(h.calls, "waybill")
}

func (h *recordingHooks) OnLocation(context.Context, LocationEvent) {
	h.calls = append(h.calls, "location")
}

// 不配置范围时保持原有全量派发。
func TestFilterForwardsAllEventsWhenUnscoped(t *testing.T) {
	next := &recordingHooks{}
	hooks, err := NewFilter(next, nil)
	require.NoError(t, err)
	require.Same(t, next, hooks)

	ctx := context.Background()
	hooks.OnAlarm(ctx, AlarmEvent{})
	hooks.OnWaybill(ctx, WaybillEvent{})
	hooks.OnLocation(ctx, LocationEvent{})
	require.Equal(t, []string{"alarm", "waybill", "location"}, next.calls)
}

// 配置范围后只转发命中项，其余事件被丢弃。
func TestFilterForwardsOnlyScopedEvents(t *testing.T) {
	next := &recordingHooks{}
	hooks, err := NewFilter(next, []string{"alarm", "Waybill"})
	require.NoError(t, err)

	ctx := context.Background()
	hooks.OnAlarm(ctx, AlarmEvent{})
	hooks.OnWaybill(ctx, WaybillEvent{})
	hooks.OnLocation(ctx, LocationEvent{})
	require.Equal(t, []string{"alarm", "waybill"}, next.calls)
}

// 非法事件名必须在构造阶段报错并列出合法取值。
func TestFilterRejectsUnknownEvent(t *testing.T) {
	_, err := NewFilter(Noop{}, []string{"alarm", "alarms"})
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown action event "alarms"`)
	require.Contains(t, err.Error(), "attachmentInfo")
}
