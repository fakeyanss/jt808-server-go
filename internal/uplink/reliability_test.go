package uplink

import (
	"testing"

	"github.com/stretchr/testify/require"
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
