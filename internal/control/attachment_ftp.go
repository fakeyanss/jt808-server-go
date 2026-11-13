package control

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/attachmentnode"
	"github.com/fakeyanss/jt808-server-go/internal/clock"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/observability"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// ErrFTPLoginRejected 表示账号无效、已吊销或任务已终态。
var ErrFTPLoginRejected = contracts.ErrFTPLoginRejected

// DefaultFTPTaskTTL 是任务的绝对存活时间：超时后吊销账号并标记 expired。
const DefaultFTPTaskTTL = 24 * time.Hour

// FTPTaskOptions 是 FTP 上传任务管理器的构造参数。
type FTPTaskOptions struct {
	Tasks       repository.AttachmentFTPTaskRepository
	Nodes       repository.AttachmentNodeRepository
	Pusher      *FTPControlPusher
	Clock       clock.Clock
	Metrics     *observability.Metrics
	TaskTTL     time.Duration
	MaxFileSize int64
}

// CreateFTPTaskInput 是平台下发 0x9206 时登记的任务参数。
type CreateFTPTaskInput struct {
	Phone         string
	Serial        uint16
	UploadPath    string
	Channel       uint8
	StartTime     time.Time
	EndTime       time.Time
	AlarmSign     string
	MediaType     uint8
	StreamType    uint8
	StorageType   uint8
	TaskCondition uint8
}

// FTPTaskManager 维护任务生命周期：一次性账号、节点绑定、控制指令与终态收尾。
//
// 它是元数据面的唯一持有者：附件节点只通过 HTTP 调用 Authenticate/Progress。
type FTPTaskManager struct {
	tasks   repository.AttachmentFTPTaskRepository
	nodes   repository.AttachmentNodeRepository
	pusher  *FTPControlPusher
	clock   clock.Clock
	metrics *observability.Metrics
	ttl     time.Duration
	maxSize int64
}

// NewFTPTaskManager 构造管理器；tasks 为空时返回 nil。
func NewFTPTaskManager(opts FTPTaskOptions) *FTPTaskManager {
	if opts.Tasks == nil {
		return nil
	}
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}
	if opts.TaskTTL <= 0 {
		opts.TaskTTL = DefaultFTPTaskTTL
	}
	return &FTPTaskManager{
		tasks: opts.Tasks, nodes: opts.Nodes, pusher: opts.Pusher,
		clock: opts.Clock, metrics: opts.Metrics, ttl: opts.TaskTTL, maxSize: opts.MaxFileSize,
	}
}

// Create 生成任务级一次性账号并落库，返回明文密码交给 0x9206 下发。
//
// 明文只在这里出现一次：库中只有 sha256 散列，control 侧也不保留可回读的密码。
func (m *FTPTaskManager) Create(ctx context.Context, in CreateFTPTaskInput) (*repository.AttachmentFTPTask, string, error) {
	if m == nil {
		return nil, "", fmt.Errorf("ftp task manager is not configured")
	}
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		return nil, "", fmt.Errorf("generate ftp password: %w", err)
	}
	password := hex.EncodeToString(suffix)
	task := &repository.AttachmentFTPTask{
		Phone:         strings.TrimSpace(in.Phone),
		Serial:        in.Serial,
		Username:      fmt.Sprintf("ftp1078_%s_%d", strings.TrimSpace(in.Phone), in.Serial),
		SecretHash:    hashFTPSecret(password),
		UploadPath:    strings.TrimSpace(in.UploadPath),
		Channel:       in.Channel,
		StartTime:     in.StartTime,
		EndTime:       in.EndTime,
		AlarmSign:     in.AlarmSign,
		MediaType:     in.MediaType,
		StreamType:    in.StreamType,
		StorageType:   in.StorageType,
		TaskCondition: in.TaskCondition,
		Status:        repository.FTPTaskStatusIssued,
	}
	saved, err := m.tasks.Save(ctx, task)
	if err != nil {
		return nil, "", err
	}
	return saved, password, nil
}

// MaxFileSize 返回任务允许的单个文件上限，0 表示不限制。
func (m *FTPTaskManager) MaxFileSize() int64 {
	if m == nil {
		return 0
	}
	return m.maxSize
}

// Authenticate 校验任务级账号，并把任务绑定到发起登录的附件节点。
//
// control 不可达时附件节点必须拒绝新登录：这里没有本地凭据可自校验，
// 因此鉴权失败与不可达都返回错误，由节点侧区分 530 与 421。
func (m *FTPTaskManager) Authenticate(ctx context.Context, nodeID, username, password string) (*repository.AttachmentFTPTask, error) {
	if m == nil {
		return nil, fmt.Errorf("ftp task manager is not configured")
	}
	task, err := m.tasks.GetByUsername(ctx, strings.TrimSpace(username))
	if err != nil {
		if errors.Is(err, repository.ErrAttachmentFTPTaskNotFound) {
			return nil, ErrFTPLoginRejected
		}
		return nil, err
	}
	if !task.RevokedAt.IsZero() || isFTPTaskTerminal(task.Status) {
		return nil, ErrFTPLoginRejected
	}
	if subtle.ConstantTimeCompare([]byte(task.SecretHash), []byte(hashFTPSecret(password))) != 1 {
		return nil, ErrFTPLoginRejected
	}
	if nodeID = strings.TrimSpace(nodeID); nodeID != "" && task.NodeID != nodeID {
		task.NodeID = nodeID
	}
	if task.Status == repository.FTPTaskStatusIssued {
		task.Status = repository.FTPTaskStatusUploading
	}
	if _, err := m.tasks.Save(ctx, task); err != nil {
		return nil, err
	}
	return task, nil
}

// ApplyControl 处理一次 0x9207：推进任务状态并推送到绑定节点。
//
// 重复动作不会重复执行：控制序号单调递增，节点按序号去重。
func (m *FTPTaskManager) ApplyControl(ctx context.Context, phone string, serial uint16, action uint8) (*repository.AttachmentFTPTask, error) {
	if m == nil {
		return nil, fmt.Errorf("ftp task manager is not configured")
	}
	task, err := m.tasks.Get(ctx, repository.AttachmentFTPTaskKey{Phone: phone, Serial: serial})
	if err != nil {
		return nil, err
	}
	switch action {
	case repository.FTPControlPause:
		if task.Status == repository.FTPTaskStatusUploading || task.Status == repository.FTPTaskStatusIssued {
			task.Status = repository.FTPTaskStatusPaused
		}
	case repository.FTPControlResume:
		if task.Status == repository.FTPTaskStatusPaused {
			task.Status = repository.FTPTaskStatusUploading
		}
	case repository.FTPControlCancel:
		if !isFTPTaskTerminal(task.Status) {
			task.Status = repository.FTPTaskStatusCancelled
			task.RevokedAt = m.clock.Now()
		}
	default:
		return nil, fmt.Errorf("unknown ftp control action %d", action)
	}
	task.ControlSeq++
	if _, err := m.tasks.Save(ctx, task); err != nil {
		return nil, err
	}
	if m.pusher != nil && task.NodeID != "" {
		if err := m.pusher.Push(ctx, task.NodeID, attachmentnode.FTPControlRequest{
			Phone: task.Phone, Serial: task.Serial, Action: action, Seq: task.ControlSeq,
		}); err != nil {
			// 推送失败不回滚任务状态：节点会在下一次登录/重连时看到终态。
			log.Warn().Err(err).Str("phone", phone).Uint16("serial", serial).
				Uint8("action", action).Msg("Fail to push ftp control to attachment node")
		}
	}
	return task, nil
}

// CompleteFromNotice 处理 0x1206：任务到达终态并吊销一次性账号。
func (m *FTPTaskManager) CompleteFromNotice(ctx context.Context, phone string, serial uint16, result uint8) {
	if m == nil || phone == "" {
		return
	}
	task, err := m.tasks.Get(ctx, repository.AttachmentFTPTaskKey{Phone: phone, Serial: serial})
	if err != nil {
		return
	}
	if isFTPTaskTerminal(task.Status) {
		return
	}
	if result == 0 {
		task.Status = repository.FTPTaskStatusCompleted
		task.CompletedAt = m.clock.Now()
	} else {
		task.Status = repository.FTPTaskStatusFailed
	}
	task.RevokedAt = m.clock.Now()
	if _, err := m.tasks.Save(ctx, task); err != nil {
		log.Warn().Err(err).Str("phone", phone).Uint16("serial", serial).
			Msg("Fail to persist ftp task notice")
	}
}

// Expire 把超过绝对存活时间的非终态任务标记 expired 并吊销账号。
func (m *FTPTaskManager) Expire(ctx context.Context) (int, error) {
	if m == nil {
		return 0, nil
	}
	cutoff := m.clock.Now().Add(-m.ttl)
	expired := 0
	for _, status := range []string{
		repository.FTPTaskStatusIssued, repository.FTPTaskStatusUploading, repository.FTPTaskStatusPaused,
	} {
		tasks, err := m.tasks.ListByStatus(ctx, status, cutoff, 200)
		if err != nil {
			return expired, err
		}
		for _, task := range tasks {
			task.Status = repository.FTPTaskStatusExpired
			task.RevokedAt = m.clock.Now()
			if _, err := m.tasks.Save(ctx, task); err != nil {
				log.Warn().Err(err).Str("phone", task.Phone).Uint16("serial", task.Serial).
					Msg("Fail to expire ftp task")
				continue
			}
			expired++
		}
	}
	return expired, nil
}

// Task 返回任务快照，供 API 查询。
func (m *FTPTaskManager) Task(ctx context.Context, phone string, serial uint16) (*repository.AttachmentFTPTask, error) {
	if m == nil {
		return nil, repository.ErrAttachmentFTPTaskNotFound
	}
	return m.tasks.Get(ctx, repository.AttachmentFTPTaskKey{Phone: phone, Serial: serial})
}

func hashFTPSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func isFTPTaskTerminal(status string) bool {
	switch status {
	case repository.FTPTaskStatusCompleted, repository.FTPTaskStatusCancelled,
		repository.FTPTaskStatusExpired, repository.FTPTaskStatusFailed:
		return true
	default:
		return false
	}
}
