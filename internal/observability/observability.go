// Package observability 提供进程级运行指标与就绪检查。
//
// 设计边界：
//   - 只做读侧观测与判定，不改变业务路径；
//   - 指标以 Prometheus 文本格式手工输出，不引入额外依赖；
//   - 计数型指标由本包持有，容量型指标由调用方在采集时从各组件读取。
package observability

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"sync/atomic"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// DefaultExpiredOutboxThreshold 是就绪检查允许的过期 pending 命令上限。
// 超过该值说明重投调度没有正常工作，继续接收新命令只会扩大堆积。
const DefaultExpiredOutboxThreshold = 100

// outboxScanLimit 是就绪检查与指标采集时单次扫描的 pending 命令上限。
// 只用于判断趋势，不需要全量统计。
const outboxScanLimit = 1000

// Metrics 持有进程内累计计数器。
// 只在明确的终态事件上自增，避免把中间重试也计入。
type Metrics struct {
	ackTimeouts atomic.Int64
}

// NewMetrics 创建进程级计数器。
func NewMetrics() *Metrics { return &Metrics{} }

// IncAckTimeout 记录一次应答超时（重传耗尽）。
func (m *Metrics) IncAckTimeout() {
	if m == nil {
		return
	}
	m.ackTimeouts.Add(1)
}

// AckTimeouts 返回累计应答超时次数。
func (m *Metrics) AckTimeouts() int64 {
	if m == nil {
		return 0
	}
	return m.ackTimeouts.Load()
}

// Snapshot 是一次指标采集的结果。
type Snapshot struct {
	ConnectedSessions    int
	AckPending           int
	OutboxPending        int
	OutboxExpired        int
	MediaStreams         int
	AttachmentsActive    int64
	AttachmentsCompleted int64
	AckTimeouts          int64
}

// Check 是一条带名称的就绪检查。
type Check struct {
	Name string
	Fn   func(context.Context) error
}

// Result 是单条检查的执行结果。
type Result struct {
	Name string
	Err  error
}

// OK 返回全部检查是否通过。
func (r Result) OK() bool { return r.Err == nil }

// Provider 组合计数器、就绪检查与指标采集函数。
// 所有字段都可能为空：未启用对应组件时跳过该项，而不是判定失败。
type Provider struct {
	Metrics *Metrics
	Checks  []Check
	Collect func(context.Context) Snapshot
}

// Ready 依次执行就绪检查；单条检查失败不阻断其余检查，便于一次性定位问题。
func (p *Provider) Ready(ctx context.Context) []Result {
	if p == nil {
		return nil
	}
	results := make([]Result, 0, len(p.Checks))
	for _, check := range p.Checks {
		if check.Fn == nil {
			continue
		}
		results = append(results, Result{Name: check.Name, Err: check.Fn(ctx)})
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	return results
}

// Snapshot 采集当前指标快照，并补上本包持有的计数器。
func (p *Provider) Snapshot(ctx context.Context) Snapshot {
	var snapshot Snapshot
	if p == nil {
		return snapshot
	}
	if p.Collect != nil {
		snapshot = p.Collect(ctx)
	}
	snapshot.AckTimeouts = p.Metrics.AckTimeouts()
	return snapshot
}

// WritePrometheus 以 Prometheus 文本 exposition 格式输出指标。
func WritePrometheus(w io.Writer, snapshot Snapshot) error {
	lines := []struct {
		name  string
		help  string
		kind  string
		value string
	}{
		{"jt808_sessions_connected", "Current terminal sessions holding a live connection.", "gauge", fmt.Sprint(snapshot.ConnectedSessions)},
		{"jt808_ack_pending", "Downlink messages waiting for a terminal acknowledgement.", "gauge", fmt.Sprint(snapshot.AckPending)},
		{"jt808_ack_timeouts_total", "Downlink messages whose acknowledgement retransmissions were exhausted.", "counter", fmt.Sprint(snapshot.AckTimeouts)},
		{"jt808_outbox_pending", "Commands still pending in the downlink outbox.", "gauge", fmt.Sprint(snapshot.OutboxPending)},
		{"jt808_outbox_expired", "Pending outbox commands past their expiry time.", "gauge", fmt.Sprint(snapshot.OutboxExpired)},
		{"jt808_media_streams", "Active 1078 media streams.", "gauge", fmt.Sprint(snapshot.MediaStreams)},
		{"jt808_attachments_active", "Attachment files currently being assembled.", "gauge", fmt.Sprint(snapshot.AttachmentsActive)},
		{"jt808_attachments_completed_total", "Attachment files completed since process start.", "counter", fmt.Sprint(snapshot.AttachmentsCompleted)},
	}
	for _, line := range lines {
		if _, err := fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n%s %s\n",
			line.name, line.help, line.name, line.kind, line.name, line.value); err != nil {
			return err
		}
	}
	return nil
}

// OutboxStats 返回 outbox 中 pending 与已过期待投的命令数量，用于就绪判定与指标。
func OutboxStats(ctx context.Context, commands repository.CommandRepository) (pending, expired int, err error) {
	if commands == nil {
		return 0, 0, nil
	}
	list, err := commands.ListPending(ctx, repository.PendingQuery{Limit: outboxScanLimit})
	if err != nil {
		return 0, 0, fmt.Errorf("list pending commands: %w", err)
	}
	now := time.Now()
	for _, command := range list {
		if !command.ExpiresAt.IsZero() && command.ExpiresAt.Before(now) {
			expired++
		}
	}
	return len(list), expired, nil
}

// CheckOutboxBacklog 在过期 pending 命令超过 threshold 时返回错误。
func CheckOutboxBacklog(ctx context.Context, commands repository.CommandRepository, threshold int) error {
	_, expired, err := OutboxStats(ctx, commands)
	if err != nil {
		return err
	}
	if threshold > 0 && expired >= threshold {
		return fmt.Errorf("%d pending commands are past their expiry", expired)
	}
	return nil
}

// CheckWritableDir 校验目录存在且可写入，用于发现磁盘只读或空间耗尽。
// 探针只创建并删除一个空文件，不保留任何状态。
func CheckWritableDir(dir string) error {
	if dir == "" {
		return nil
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("stat dir %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path %s is not a directory", dir)
	}
	probe, err := os.CreateTemp(dir, ".jt808-ready-*")
	if err != nil {
		return fmt.Errorf("create probe file in %s: %w", dir, err)
	}
	name := probe.Name()
	closeErr := probe.Close()
	removeErr := os.Remove(name)
	if closeErr != nil {
		return fmt.Errorf("close probe file %s: %w", name, closeErr)
	}
	if removeErr != nil {
		return fmt.Errorf("remove probe file %s: %w", name, removeErr)
	}
	return nil
}
