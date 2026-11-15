package storage

import (
	"context"
	"io"
	"time"
)

// BlobState 是对象在“提级完成、业务元数据落库”之间的状态。
type BlobState string

const (
	// BlobStatePending 对象已可见但业务元数据尚未落库，只能等待对账或过期。
	BlobStatePending BlobState = "pending"
	// BlobStateCommitted 业务元数据已落库，对象可被引用。
	BlobStateCommitted BlobState = "committed"
	// BlobStateMissing 业务元数据存在但对象丢失，保留引用依据供排查。
	BlobStateMissing BlobState = "missing"
)

// BlobInfo 描述一次暂存或提级后的对象。
type BlobInfo struct {
	Key        string
	StagingKey string
	Size       int64
	MD5        string
	ETag       string
	State      BlobState
}

// BlobOptions 是写入对象的可选属性。
type BlobOptions struct {
	ContentType string
	MD5         string
	Metadata    map[string]string
}

// BlobStore 是媒体/附件文件的统一写入契约。
//
// Put 只写暂存区，不使最终 key 可见；Promote 负责原子提级；
// MarkCommitted 只表达业务引用状态，不参与提级。三者分开，
// 避免“对象已可见”与“业务已提交”两个事实被混为一个 Commit。
type BlobStore interface {
	Put(ctx context.Context, key string, r io.Reader, opts BlobOptions) (BlobInfo, error)
	Promote(ctx context.Context, info BlobInfo) (BlobInfo, error)
	MarkCommitted(ctx context.Context, key string) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	// OpenRange 读取 [offset, offset+length) 区间；length<=0 表示读到对象末尾。
	//
	// 这是录像回放的必需能力，不是可选优化：平台录像按片段索引 seek，
	// 退化成“读全量再丢弃前缀”会在长录像上放大成倍带宽与内存。
	OpenRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	Stat(ctx context.Context, key string) (BlobInfo, error)
}

// BlobObject 是列举结果中的一个对象。
type BlobObject struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// BlobLister 是可选能力：能列举指定前缀下的对象。
//
// control 依赖它从 media/segments/ 的对象 key 重建切片索引；
// 不要求所有 BlobStore 实现，测试替身缺失时扫描侧会显式跳过。
type BlobLister interface {
	ListObjects(ctx context.Context, prefix string) ([]BlobObject, error)
}
