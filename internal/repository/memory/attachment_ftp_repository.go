package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// AttachmentFTPTaskRepository 是进程内 FTP 上传任务表，仅用于单机与测试。
type AttachmentFTPTaskRepository struct {
	mu    sync.Mutex
	tasks map[repository.AttachmentFTPTaskKey]repository.AttachmentFTPTask
}

// NewAttachmentFTPTaskRepository 创建内存任务表。
func NewAttachmentFTPTaskRepository() *AttachmentFTPTaskRepository {
	return &AttachmentFTPTaskRepository{tasks: make(map[repository.AttachmentFTPTaskKey]repository.AttachmentFTPTask)}
}

func (r *AttachmentFTPTaskRepository) Save(_ context.Context, task *repository.AttachmentFTPTask) (*repository.AttachmentFTPTask, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if task.Status == "" {
		task.Status = repository.FTPTaskStatusIssued
	}
	now := time.Now()
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	task.UpdatedAt = now
	stored := *task
	r.tasks[task.Key()] = stored
	copy := stored
	return &copy, nil
}

func (r *AttachmentFTPTaskRepository) Get(_ context.Context, key repository.AttachmentFTPTaskKey) (*repository.AttachmentFTPTask, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[key]
	if !ok {
		return nil, repository.ErrAttachmentFTPTaskNotFound
	}
	copy := task
	return &copy, nil
}

func (r *AttachmentFTPTaskRepository) GetByUsername(_ context.Context, username string) (*repository.AttachmentFTPTask, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, task := range r.tasks {
		if task.Username == username {
			copy := task
			return &copy, nil
		}
	}
	return nil, repository.ErrAttachmentFTPTaskNotFound
}

func (r *AttachmentFTPTaskRepository) ListByStatus(_ context.Context, status string, updatedBefore time.Time, limit int) ([]*repository.AttachmentFTPTask, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*repository.AttachmentFTPTask, 0)
	for _, task := range r.tasks {
		if status != "" && task.Status != status {
			continue
		}
		if !updatedBefore.IsZero() && !task.UpdatedAt.Before(updatedBefore) {
			continue
		}
		copy := task
		out = append(out, &copy)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.Before(out[j].UpdatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *AttachmentFTPTaskRepository) Delete(_ context.Context, key repository.AttachmentFTPTaskKey) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tasks, key)
	return nil
}

var _ repository.AttachmentFTPTaskRepository = (*AttachmentFTPTaskRepository)(nil)

// AttachmentNodeRepository 是进程内附件节点注册表。
type AttachmentNodeRepository struct {
	mu    sync.Mutex
	nodes map[string]repository.AttachmentNode
}

// NewAttachmentNodeRepository 创建内存附件节点表。
func NewAttachmentNodeRepository() *AttachmentNodeRepository {
	return &AttachmentNodeRepository{nodes: make(map[string]repository.AttachmentNode)}
}

func (r *AttachmentNodeRepository) Register(_ context.Context, nodeID, instanceID, baseURL string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodes[nodeID] = repository.AttachmentNode{
		NodeID:     nodeID,
		InstanceID: instanceID,
		BaseURL:    baseURL,
		Status:     "online",
		StartedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  now.Add(repository.AttachmentNodeTTL),
	}
	return nil
}

func (r *AttachmentNodeRepository) Heartbeat(_ context.Context, nodeID, instanceID string, now time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	node, ok := r.nodes[nodeID]
	if !ok || node.InstanceID != instanceID {
		return false, nil
	}
	node.Status = "online"
	node.LastSeenAt = now
	node.ExpiresAt = now.Add(repository.AttachmentNodeTTL)
	r.nodes[nodeID] = node
	return true, nil
}

func (r *AttachmentNodeRepository) Deregister(_ context.Context, nodeID, instanceID string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	node, ok := r.nodes[nodeID]
	if !ok || node.InstanceID != instanceID {
		return nil
	}
	node.Status = "offline"
	node.LastSeenAt = now
	node.ExpiresAt = now
	r.nodes[nodeID] = node
	return nil
}

func (r *AttachmentNodeRepository) Get(_ context.Context, nodeID string) (*repository.AttachmentNode, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	node, ok := r.nodes[nodeID]
	if !ok {
		return nil, nil
	}
	copy := node
	return &copy, nil
}

func (r *AttachmentNodeRepository) ListOnline(_ context.Context, now time.Time) ([]*repository.AttachmentNode, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*repository.AttachmentNode, 0, len(r.nodes))
	for _, node := range r.nodes {
		if node.Status != "online" || !node.ExpiresAt.After(now) {
			continue
		}
		copy := node
		out = append(out, &copy)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	return out, nil
}

func (r *AttachmentNodeRepository) Stats(_ context.Context, now time.Time) (repository.AttachmentNodeStats, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var stats repository.AttachmentNodeStats
	for _, node := range r.nodes {
		switch {
		case node.Status == "offline":
			stats.Offline++
		case node.ExpiresAt.After(now):
			stats.Online++
		default:
			stats.Expired++
		}
	}
	return stats, nil
}

var _ repository.AttachmentNodeRepository = (*AttachmentNodeRepository)(nil)
