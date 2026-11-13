// Package storage 提供 media/附件文件的写入后端（sharedVolume 形态）。
//
// E2 只实现 sharedVolume：单实例控制面挂载共享卷，用普通文件系统承载
// 0x0801 的图片写入。对象状态（pending/committed/missing）放在隐藏目录的
// sidecar 文件里，不污染媒体目录，也不会被保留期扫描当成媒体文件。
package storage

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
)

const (
	// MarkerFile 是共享卷的标记文件，启动时校验，避免误写本地盘。
	MarkerFile = ".jt808-shared-volume"
	// StateDir 保存对象 sidecar 状态。
	StateDir = ".jt808-blob-state"
	// StagingDir 保存未提级的对象。
	StagingDir = ".jt808-staging"
)

var txCounter atomic.Uint64

// SharedVolume 是 contracts.BlobStore 的文件系统实现。
type SharedVolume struct {
	root string
}

type blobState struct {
	State     contracts.BlobState `json:"state"`
	UpdatedAt time.Time           `json:"updatedAt"`
}

// NewSharedVolume 打开共享卷；requireMarker 为真时必须存在标记文件。
func NewSharedVolume(root string, requireMarker bool) (*SharedVolume, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("blob shared volume root is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if requireMarker {
		if _, err := os.Stat(filepath.Join(abs, MarkerFile)); err != nil {
			return nil, fmt.Errorf("shared volume marker missing at %s: %w", filepath.Join(abs, MarkerFile), err)
		}
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, err
	}
	return &SharedVolume{root: abs}, nil
}

// Root 返回共享卷根目录。
func (s *SharedVolume) Root() string { return s.root }

// ListObjects 列举共享卷指定前缀下的对象，供 control 重建切片索引。
func (s *SharedVolume) ListObjects(ctx context.Context, prefix string) ([]contracts.BlobObject, error) {
	clean := strings.Trim(strings.TrimSpace(prefix), "/")
	if clean == "" {
		return nil, errors.New("blob list prefix is empty")
	}
	base := filepath.Join(s.root, filepath.FromSlash(clean))
	out := make([]contracts.BlobObject, 0)
	err := filepath.WalkDir(base, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		relative, err := filepath.Rel(s.root, path)
		if err != nil {
			return nil
		}
		out = append(out, contracts.BlobObject{
			Key:          filepath.ToSlash(relative),
			Size:         info.Size(),
			LastModified: info.ModTime(),
		})
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	return out, err
}

// Put 把内容写入暂存区，不使最终 key 可见。
func (s *SharedVolume) Put(ctx context.Context, key string, r io.Reader, opts contracts.BlobOptions) (contracts.BlobInfo, error) {
	clean, err := cleanKey(key)
	if err != nil {
		return contracts.BlobInfo{}, err
	}
	txid := fmt.Sprintf("%d-%d", time.Now().UnixNano(), txCounter.Add(1))
	staging := filepath.Join(StagingDir, txid, clean)
	target := filepath.Join(s.root, staging)
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return contracts.BlobInfo{}, err
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return contracts.BlobInfo{}, err
	}
	hasher := md5.New()
	size, copyErr := io.Copy(io.MultiWriter(file, hasher), r)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(target)
		return contracts.BlobInfo{}, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(target)
		return contracts.BlobInfo{}, closeErr
	}
	sum := hex.EncodeToString(hasher.Sum(nil))
	return contracts.BlobInfo{
		Key:        clean,
		StagingKey: filepath.ToSlash(staging),
		Size:       size,
		MD5:        sum,
		ETag:       sum,
		State:      contracts.BlobStatePending,
	}, nil
}

// Promote 把暂存对象原子提级到最终 key，并写 pending 状态。
func (s *SharedVolume) Promote(ctx context.Context, info contracts.BlobInfo) (contracts.BlobInfo, error) {
	key, err := cleanKey(info.Key)
	if err != nil {
		return contracts.BlobInfo{}, err
	}
	staging, err := cleanKey(info.StagingKey)
	if err != nil {
		return contracts.BlobInfo{}, err
	}
	source := filepath.Join(s.root, staging)
	final := filepath.Join(s.root, key)
	if err := os.MkdirAll(filepath.Dir(final), 0o750); err != nil {
		return contracts.BlobInfo{}, err
	}
	if err := os.Rename(source, final); err != nil {
		return contracts.BlobInfo{}, err
	}
	_ = os.Remove(filepath.Dir(source))
	if err := s.writeState(key, contracts.BlobStatePending); err != nil {
		return contracts.BlobInfo{}, err
	}
	info.Key = key
	info.State = contracts.BlobStatePending
	return info, nil
}

// MarkCommitted 只表达业务元数据已落库。
func (s *SharedVolume) MarkCommitted(ctx context.Context, key string) error {
	clean, err := cleanKey(key)
	if err != nil {
		return err
	}
	return s.writeState(clean, contracts.BlobStateCommitted)
}

// Open 打开已提级的对象。
func (s *SharedVolume) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	clean, err := cleanKey(key)
	if err != nil {
		return nil, err
	}
	return os.Open(filepath.Join(s.root, clean))
}

// OpenRange 打开对象的 [offset, offset+length) 区间。
// length<=0 表示读到对象末尾；offset 越界返回错误。
func (s *SharedVolume) OpenRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	if offset < 0 {
		return nil, fmt.Errorf("blob range offset must be >= 0, got %d", offset)
	}
	clean, err := cleanKey(key)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(filepath.Join(s.root, clean))
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if offset > info.Size() {
		_ = file.Close()
		return nil, fmt.Errorf("blob range offset %d exceeds object size %d", offset, info.Size())
	}
	available := info.Size() - offset
	if length <= 0 || length > available {
		length = available
	}
	return &sectionReadCloser{Reader: io.NewSectionReader(file, offset, length), closer: file}, nil
}

// sectionReadCloser 让区间读取与普通对象读取一样可关闭。
type sectionReadCloser struct {
	io.Reader
	closer io.Closer
}

func (r *sectionReadCloser) Close() error { return r.closer.Close() }

// Delete 删除对象与 sidecar；不存在时返回错误。
func (s *SharedVolume) Delete(ctx context.Context, key string) error {
	clean, err := cleanKey(key)
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(s.root, clean)); err != nil {
		return err
	}
	_ = os.Remove(s.statePath(clean))
	return nil
}

// Stat 返回对象大小、校验和与状态。
func (s *SharedVolume) Stat(ctx context.Context, key string) (contracts.BlobInfo, error) {
	clean, err := cleanKey(key)
	if err != nil {
		return contracts.BlobInfo{}, err
	}
	stat, err := os.Stat(filepath.Join(s.root, clean))
	if err != nil {
		return contracts.BlobInfo{}, err
	}
	state := contracts.BlobStateMissing
	if raw, readErr := os.ReadFile(s.statePath(clean)); readErr == nil {
		var meta blobState
		if json.Unmarshal(raw, &meta) == nil && meta.State != "" {
			state = meta.State
		}
	}
	return contracts.BlobInfo{Key: clean, Size: stat.Size(), State: state}, nil
}

// PendingBefore 返回超过 olderThan 仍处于 pending 的对象 key。
func (s *SharedVolume) PendingBefore(ctx context.Context, olderThan time.Duration) ([]string, error) {
	return s.PendingBeforePrefix(ctx, "", olderThan)
}

// PendingBeforePrefix 只返回指定来源前缀下的 pending 对象。
func (s *SharedVolume) PendingBeforePrefix(ctx context.Context, sourcePrefix string, olderThan time.Duration) ([]string, error) {
	cutoff := time.Now().Add(-olderThan)
	var keys []string
	err := filepath.WalkDir(filepath.Join(s.root, StateDir), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		var meta blobState
		if json.Unmarshal(raw, &meta) != nil || meta.State != contracts.BlobStatePending || meta.UpdatedAt.After(cutoff) {
			return nil
		}
		relative, relErr := filepath.Rel(filepath.Join(s.root, StateDir), path)
		if relErr != nil {
			return nil
		}
		key := filepath.ToSlash(strings.TrimSuffix(relative, ".json"))
		if sourcePrefix != "" && !strings.HasPrefix(key, sourcePrefix) {
			return nil
		}
		keys = append(keys, key)
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return keys, err
}

// SweepPending 删除 pending 超时且没有被业务表引用的对象。
// referenced 用于在删除前回查数据库，仍然被引用的对象一律保留。
func (s *SharedVolume) SweepPending(ctx context.Context, olderThan time.Duration, referenced func(key string) bool) {
	s.SweepPendingPrefix(ctx, "", olderThan, referenced)
}

// SweepPendingPrefix 只在指定来源前缀内回收 pending 对象。
func (s *SharedVolume) SweepPendingPrefix(ctx context.Context, sourcePrefix string, olderThan time.Duration, referenced func(key string) bool) {
	keys, err := s.PendingBeforePrefix(ctx, sourcePrefix, olderThan)
	if err != nil {
		return
	}
	for _, key := range keys {
		if referenced != nil && referenced(key) {
			continue
		}
		_ = s.Delete(ctx, key)
	}
}

func (s *SharedVolume) statePath(key string) string {
	return filepath.Join(s.root, StateDir, key+".json")
}

func (s *SharedVolume) writeState(key string, state contracts.BlobState) error {
	path := s.statePath(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	raw, err := json.Marshal(blobState{State: state, UpdatedAt: time.Now()})
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// cleanKey 拒绝绝对路径与目录穿越，并统一成 slash 分隔。
func cleanKey(key string) (string, error) {
	if key == "" {
		return "", errors.New("blob key is empty")
	}
	slashed := filepath.ToSlash(key)
	if strings.HasPrefix(slashed, "/") || strings.Contains(slashed, "../") || slashed == ".." {
		return "", fmt.Errorf("invalid blob key %q", key)
	}
	cleaned := filepath.ToSlash(filepath.Clean(slashed))
	if cleaned == "." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("invalid blob key %q", key)
	}
	return cleaned, nil
}

var _ contracts.BlobStore = (*SharedVolume)(nil)
