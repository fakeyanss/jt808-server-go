package control

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/repository/memory"
)

func newFTPManager(t *testing.T, ttl time.Duration) (*FTPTaskManager, *memory.AttachmentFTPTaskRepository, *memory.AttachmentNodeRepository) {
	t.Helper()
	tasks := memory.NewAttachmentFTPTaskRepository()
	nodes := memory.NewAttachmentNodeRepository()
	manager := NewFTPTaskManager(FTPTaskOptions{Tasks: tasks, Nodes: nodes, TaskTTL: ttl})
	require.NotNil(t, manager)
	return manager, tasks, nodes
}

func TestFTPTaskOneTimeCredential(t *testing.T) {
	ctx := context.Background()
	manager, _, _ := newFTPManager(t, time.Hour)
	task, password, err := manager.Create(ctx, CreateFTPTaskInput{
		Phone: "13800138000", Serial: 12, UploadPath: "/a", StartTime: time.Now(),
		EndTime: time.Now().Add(time.Minute), AlarmSign: "0000000000000001",
	})
	require.NoError(t, err)
	require.NotEmpty(t, password)
	require.NotContains(t, task.SecretHash, password)

	_, err = manager.Authenticate(ctx, "node-1", task.Username, "wrong")
	require.ErrorIs(t, err, contracts.ErrFTPLoginRejected)

	authed, err := manager.Authenticate(ctx, "node-1", task.Username, password)
	require.NoError(t, err)
	require.Equal(t, "node-1", authed.NodeID)
	require.Equal(t, repository.FTPTaskStatusUploading, authed.Status)

	manager.CompleteFromNotice(ctx, "13800138000", 12, 0)
	_, err = manager.Authenticate(ctx, "node-1", task.Username, password)
	require.ErrorIs(t, err, contracts.ErrFTPLoginRejected)

	done, err := manager.Task(ctx, "13800138000", 12)
	require.NoError(t, err)
	require.Equal(t, repository.FTPTaskStatusCompleted, done.Status)
	require.False(t, done.RevokedAt.IsZero())
}

func TestFTPControlSequenceAndCancel(t *testing.T) {
	ctx := context.Background()
	manager, _, _ := newFTPManager(t, time.Hour)
	task, password, err := manager.Create(ctx, CreateFTPTaskInput{
		Phone: "13800138001", Serial: 3, StartTime: time.Now(), EndTime: time.Now().Add(time.Hour),
	})
	require.NoError(t, err)
	_, err = manager.Authenticate(ctx, "node-1", task.Username, password)
	require.NoError(t, err)

	paused, err := manager.ApplyControl(ctx, "13800138001", 3, repository.FTPControlPause)
	require.NoError(t, err)
	require.Equal(t, repository.FTPTaskStatusPaused, paused.Status)
	require.EqualValues(t, 1, paused.ControlSeq)

	resumed, err := manager.ApplyControl(ctx, "13800138001", 3, repository.FTPControlResume)
	require.NoError(t, err)
	require.Equal(t, repository.FTPTaskStatusUploading, resumed.Status)
	require.EqualValues(t, 2, resumed.ControlSeq)

	cancelled, err := manager.ApplyControl(ctx, "13800138001", 3, repository.FTPControlCancel)
	require.NoError(t, err)
	require.Equal(t, repository.FTPTaskStatusCancelled, cancelled.Status)
	require.False(t, cancelled.RevokedAt.IsZero())

	_, err = manager.Authenticate(ctx, "node-1", task.Username, password)
	require.ErrorIs(t, err, contracts.ErrFTPLoginRejected)
}

func TestFTPTaskExpiryRevokesCredentials(t *testing.T) {
	ctx := context.Background()
	manager, _, _ := newFTPManager(t, time.Millisecond)
	task, password, err := manager.Create(ctx, CreateFTPTaskInput{
		Phone: "13800138002", Serial: 4, StartTime: time.Now(), EndTime: time.Now().Add(time.Hour),
	})
	require.NoError(t, err)
	time.Sleep(5 * time.Millisecond)

	expired, err := manager.Expire(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, expired)

	_, err = manager.Authenticate(ctx, "node-1", task.Username, password)
	require.ErrorIs(t, err, contracts.ErrFTPLoginRejected)
}

func TestAttachmentNodeRegistrationFencing(t *testing.T) {
	ctx := context.Background()
	nodes := memory.NewAttachmentNodeRepository()
	now := time.Now()
	require.NoError(t, nodes.Register(ctx, "node-1", "instance-a", "http://10.0.0.1:7402", now))

	ok, err := nodes.Heartbeat(ctx, "node-1", "instance-a", now.Add(time.Second))
	require.NoError(t, err)
	require.True(t, ok)

	// 新实例覆盖旧记录后，旧实例心跳必须被 fence。
	require.NoError(t, nodes.Register(ctx, "node-1", "instance-b", "http://10.0.0.2:7402", now.Add(2*time.Second)))
	ok, err = nodes.Heartbeat(ctx, "node-1", "instance-a", now.Add(3*time.Second))
	require.NoError(t, err)
	require.False(t, ok)

	node, err := nodes.Get(ctx, "node-1")
	require.NoError(t, err)
	require.NotNil(t, node)
	require.Equal(t, "instance-b", node.InstanceID)
	require.Equal(t, "http://10.0.0.2:7402", node.BaseURL)

	// 过期节点不再出现在在线清单。
	online, err := nodes.ListOnline(ctx, now.Add(2*repository.AttachmentNodeTTL+3*time.Second))
	require.NoError(t, err)
	require.Empty(t, online)
}
