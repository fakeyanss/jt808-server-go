package alarmserver

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/contracts/attachment"
)

// fakeProgressSink 是测试用的 control 替身：内存保存最新快照，可模拟 control 不可达。
type fakeProgressSink struct {
	mu       sync.Mutex
	items    map[attachment.AttachmentProgressKey]attachment.AttachmentProgress
	disabled bool
}

func newFakeProgressSink() *fakeProgressSink {
	return &fakeProgressSink{items: map[attachment.AttachmentProgressKey]attachment.AttachmentProgress{}}
}

func (s *fakeProgressSink) Save(_ context.Context, progress attachment.AttachmentProgress) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[progress.Key()] = progress
	return nil
}

func (s *fakeProgressSink) Load(_ context.Context, key attachment.AttachmentProgressKey) (*attachment.AttachmentProgress, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.disabled {
		return nil, context.DeadlineExceeded
	}
	progress, ok := s.items[key]
	if !ok {
		return nil, attachment.ErrAttachmentProgressNotFound
	}
	return &progress, nil
}

func (s *fakeProgressSink) get(key attachment.AttachmentProgressKey) (attachment.AttachmentProgress, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	progress, ok := s.items[key]
	return progress, ok
}

// waitStatus 等待异步上报到达目标状态。
func (s *fakeProgressSink) waitStatus(t *testing.T, key attachment.AttachmentProgressKey, status string) attachment.AttachmentProgress {
	t.Helper()
	var got attachment.AttachmentProgress
	require.Eventually(t, func() bool {
		progress, ok := s.get(key)
		if !ok || progress.Status != status {
			return false
		}
		got = progress
		return true
	}, 5*time.Second, 10*time.Millisecond)
	return got
}
