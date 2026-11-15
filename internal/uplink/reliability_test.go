package uplink

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

func TestPolicyClassification(t *testing.T) {
	policy := NewPolicy("best_effort", "best_effort", "at_least_once", "at_least_once")

	tests := []struct {
		name      string
		msgID     uint16
		alarmSign uint32
		wantClass Class
		want      Reliability
	}{
		{name: "实时定位 best-effort", msgID: 0x0200, alarmSign: 0, wantClass: ClassLocation, want: BestEffort},
		{name: "带报警位的定位走报警", msgID: 0x0200, alarmSign: 1, wantClass: ClassAlarm, want: AtLeastOnce},
		{name: "批量定位至少一次", msgID: 0x0704, wantClass: ClassBatchLocation, want: AtLeastOnce},
		{name: "其他消息走 default", msgID: 0x0002, wantClass: ClassDefault, want: BestEffort},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			class := policy.Classify(tt.msgID, tt.alarmSign)
			require.Equal(t, tt.wantClass, class)
			require.Equal(t, tt.want, policy.For(class))
		})
	}
}

func TestParseReliabilityOnlyUpgradesOnExplicitValue(t *testing.T) {
	require.Equal(t, AtLeastOnce, ParseReliability("AT_LEAST_ONCE"))
	require.Equal(t, AtLeastOnce, ParseReliability(" at_least_once "))
	require.Equal(t, BestEffort, ParseReliability("best_effort"))
	require.Equal(t, BestEffort, ParseReliability(""))
	require.Equal(t, BestEffort, ParseReliability("unknown"))
}

// 分类覆盖只作用于显式配置的分类，其余回落到全局次数。
func TestAttemptPolicyFallsBackToGlobal(t *testing.T) {
	policy := NewAttemptPolicy(2, AttemptOverrides{SessionClosed: 3, Alarm: 1})

	require.Equal(t, 3, policy.For(ClassSessionClosed))
	require.Equal(t, 1, policy.For(ClassAlarm))
	require.Equal(t, 2, policy.For(ClassLocation))
	require.Equal(t, 2, policy.For(ClassBatchLocation))
	require.Equal(t, 2, policy.For(ClassDefault))

	// session.closed 未显式配置时使用代码层默认 2 次，而不是回落到全局值。
	require.Equal(t, 2, NewAttemptPolicy(1, AttemptOverrides{}).For(ClassSessionClosed))
	require.Equal(t, 3, NewAttemptPolicy(1, AttemptOverrides{SessionClosed: 3}).For(ClassSessionClosed))

	// 非法全局值按 1 次处理，保证至少投递一次。
	require.Equal(t, 1, NewAttemptPolicy(0, AttemptOverrides{}).For(ClassDefault))
}

func TestClassifyEvent(t *testing.T) {
	require.Equal(t, ClassSessionClosed, ClassifyEvent(contracts.UplinkEvent{Event: "session.closed"}))
	require.Equal(t, ClassAlarm, ClassifyEvent(contracts.UplinkEvent{
		MsgID: 0x0200, Payload: &model.Msg0200{AlarmSign: 1},
	}))
	require.Equal(t, ClassLocation, ClassifyEvent(contracts.UplinkEvent{
		MsgID: 0x0200, Payload: &model.Msg0200{},
	}))
	require.Equal(t, ClassBatchLocation, ClassifyEvent(contracts.UplinkEvent{MsgID: 0x0704}))
	require.Equal(t, ClassDefault, ClassifyEvent(contracts.UplinkEvent{MsgID: 0x0100}))
}
