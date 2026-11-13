package repository

import (
	"context"
	"errors"
	"time"
)

// ErrAttachmentFTPTaskNotFound 表示 FTP 上传任务不存在。
var ErrAttachmentFTPTaskNotFound = errors.New("attachment ftp task not found")

// FTP 上传任务状态。
const (
	FTPTaskStatusIssued    = "issued"
	FTPTaskStatusUploading = "uploading"
	FTPTaskStatusPaused    = "paused"
	FTPTaskStatusCompleted = "completed"
	FTPTaskStatusCancelled = "cancelled"
	FTPTaskStatusExpired   = "expired"
	FTPTaskStatusFailed    = "failed"
)

// FTP 上传控制动作，与 0x9207 的控制类型同值。
const (
	FTPControlPause  uint8 = 0
	FTPControlResume uint8 = 1
	FTPControlCancel uint8 = 2
)

// AttachmentFTPTask 是平台下发的一次 1078 FTP 上传任务。
//
// 任务以 (Phone, Serial) 为唯一键，Serial 就是 0x9206 报文头流水号；
// 账号是任务级一次性凭据，终态后吊销，不从属于终端长期账号体系。
type AttachmentFTPTask struct {
	Phone  string `json:"phone"`
	Serial uint16 `json:"serial"`

	Username string `json:"username"`
	// SecretHash 是密码散列，任何查询接口都不得返回。
	SecretHash string `json:"-"`
	UploadPath string `json:"uploadPath"`

	Channel       uint8     `json:"channel"`
	StartTime     time.Time `json:"startTime"`
	EndTime       time.Time `json:"endTime"`
	AlarmSign     string    `json:"alarmSign"`
	MediaType     uint8     `json:"mediaType"`
	StreamType    uint8     `json:"streamType"`
	StorageType   uint8     `json:"storageType"`
	TaskCondition uint8     `json:"taskCondition"`

	Status string `json:"status"`
	// NodeID 是任务绑定的附件节点；0x9207 只推送到这个节点。
	NodeID string `json:"nodeId,omitempty"`
	// ControlSeq 是已下发的控制动作序号，节点据此去重。
	ControlSeq uint32 `json:"controlSeq"`
	TotalBytes int64  `json:"totalBytes"`

	RevokedAt   time.Time `json:"revokedAt"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	CompletedAt time.Time `json:"completedAt"`
}

// Key 返回任务唯一键。
func (t *AttachmentFTPTask) Key() AttachmentFTPTaskKey {
	return AttachmentFTPTaskKey{Phone: t.Phone, Serial: t.Serial}
}

// AttachmentFTPTaskKey 是 FTP 任务唯一键。
type AttachmentFTPTaskKey struct {
	Phone  string
	Serial uint16
}

// AttachmentFTPTaskRepository 是 FTP 上传任务仓储，仅 control 使用。
type AttachmentFTPTaskRepository interface {
	Save(ctx context.Context, task *AttachmentFTPTask) (*AttachmentFTPTask, error)
	Get(ctx context.Context, key AttachmentFTPTaskKey) (*AttachmentFTPTask, error)
	GetByUsername(ctx context.Context, username string) (*AttachmentFTPTask, error)
	// ListByStatus 按状态与更新时间返回任务，供过期回收与运维查询。
	ListByStatus(ctx context.Context, status string, updatedBefore time.Time, limit int) ([]*AttachmentFTPTask, error)
	Delete(ctx context.Context, key AttachmentFTPTaskKey) error
}

// AttachmentNodeTTL 是附件节点心跳的有效期。
// 与网关节点同一口径：超时即判失效，0x9207 不再推送到该地址。
const AttachmentNodeTTL = 30 * time.Second

// AttachmentNode 是附件节点注册到 control 的可达地址。
type AttachmentNode struct {
	NodeID     string
	InstanceID string
	BaseURL    string
	Status     string
	StartedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
}

// AttachmentNodeStats 是附件节点存活水位。
type AttachmentNodeStats struct {
	Online  int64
	Offline int64
	Expired int64
}

// AttachmentNodeRepository 维护附件节点的注册、心跳与注销。
//
// Register 会覆盖同 node_id 的旧实例；Heartbeat 返回 false 表示 instance_id
// 已被新实例顶替或节点不存在，调用方应重新注册而不是继续使用旧地址。
type AttachmentNodeRepository interface {
	Register(ctx context.Context, nodeID, instanceID, baseURL string, now time.Time) error
	Heartbeat(ctx context.Context, nodeID, instanceID string, now time.Time) (bool, error)
	Deregister(ctx context.Context, nodeID, instanceID string, now time.Time) error
	Get(ctx context.Context, nodeID string) (*AttachmentNode, error)
	ListOnline(ctx context.Context, now time.Time) ([]*AttachmentNode, error)
	Stats(ctx context.Context, now time.Time) (AttachmentNodeStats, error)
}
