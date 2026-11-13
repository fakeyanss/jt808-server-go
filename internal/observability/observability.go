// Package observability 提供进程级运行指标与就绪检查。
//
// 设计边界：
//   - 只做读侧观测与判定，不改变业务路径；
//   - 指标以 Prometheus 文本格式手工输出，不引入额外依赖；
//   - 计数型指标由本包持有，容量型指标由调用方在采集时从各组件读取。
package observability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// DefaultExpiredOutboxThreshold 是就绪检查允许的过期 pending 命令上限。
// 超过该值说明重投调度没有正常工作，继续接收新命令只会扩大堆积。
const DefaultExpiredOutboxThreshold = 100

// 保留期清理的指标类别，与清理任务的分表策略一一对应。
const (
	RetentionCategoryLocations          = "locations"
	RetentionCategoryAlarms             = "alarms"
	RetentionCategoryMediaFiles         = "mediaFiles"
	RetentionCategoryMediaCaptures      = "mediaCaptures"
	RetentionCategoryMediaSearchResults = "mediaSearchResults"
)

// outboxScanLimit 是就绪检查与指标采集时单次扫描的 pending 命令上限。
// 只用于判断趋势，不需要全量统计。
const outboxScanLimit = 1000

// retentionCounter 是单个类别的累计删除与失败计数。
type retentionCounter struct {
	deleted  atomic.Int64
	failures atomic.Int64
}

// Metrics 持有进程内累计计数器。
// 只在明确的终态事件上自增，避免把中间重试也计入。
type Metrics struct {
	ackTimeouts atomic.Int64
	retentionMu sync.Mutex
	retention   map[string]*retentionCounter
	// retentionLastRun 与 retentionLastDuration 记录最近一次清理任务的时间与耗时。
	retentionLastRun      atomic.Int64
	retentionLastDuration atomic.Int64
	// 孤儿文件扫描的累计计数与最近一次运行信息。
	orphanScanned      atomic.Int64
	orphanDeleted      atomic.Int64
	orphanSkipped      atomic.Int64
	orphanFailures     atomic.Int64
	orphanLastRun      atomic.Int64
	orphanLastDuration atomic.Int64
	// 终端应答处理结果，按 outcome 分别计数。
	terminalReplyAcked          atomic.Int64
	terminalReplyFailed         atomic.Int64
	terminalReplyRetryScheduled atomic.Int64
	terminalReplyDuplicate      atomic.Int64
	terminalReplyLate           atomic.Int64
	terminalReplyUnmatched      atomic.Int64
	terminalReplyIgnored        atomic.Int64
	// 上行事件在控制面的终态计数：409 丢弃、超限丢弃、非图片 0x0801、提交标记失败。
	uplinkUnknownDropped atomic.Int64
	uplinkOversized      atomic.Int64
	mediaTypeRejected    atomic.Int64
	storageCommitFailed  atomic.Int64
	// 0x9208 自动触发因角色拆分未装配的次数；>0 表示附件上行需要人工触发。
	autoUploadSkipped atomic.Int64
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

// TerminalReplySnapshot 是终端应答处理结果的累计计数。
type TerminalReplySnapshot struct {
	Acked          int64
	Failed         int64
	RetryScheduled int64
	Duplicate      int64
	Late           int64
	Unmatched      int64
	Ignored        int64
}

// IncTerminalReply 记录一次终端应答处理结果。
// 只有真正推进状态或明确分类的终态才计数，仓储错误不计入。
func (m *Metrics) IncTerminalReply(outcome string) {
	if m == nil {
		return
	}
	switch outcome {
	case "acked":
		m.terminalReplyAcked.Add(1)
	case "failed":
		m.terminalReplyFailed.Add(1)
	case "retry_scheduled":
		m.terminalReplyRetryScheduled.Add(1)
	case "duplicate":
		m.terminalReplyDuplicate.Add(1)
	case "late":
		m.terminalReplyLate.Add(1)
	case "unmatched":
		m.terminalReplyUnmatched.Add(1)
	case "ignored":
		m.terminalReplyIgnored.Add(1)
	}
}

// TerminalReplySnapshot 返回终端应答指标副本。
func (m *Metrics) TerminalReplySnapshot() TerminalReplySnapshot {
	if m == nil {
		return TerminalReplySnapshot{}
	}
	return TerminalReplySnapshot{
		Acked:          m.terminalReplyAcked.Load(),
		Failed:         m.terminalReplyFailed.Load(),
		RetryScheduled: m.terminalReplyRetryScheduled.Load(),
		Duplicate:      m.terminalReplyDuplicate.Load(),
		Late:           m.terminalReplyLate.Load(),
		Unmatched:      m.terminalReplyUnmatched.Load(),
		Ignored:        m.terminalReplyIgnored.Load(),
	}
}

// UplinkEventSnapshot 是上行事件在控制面的终态计数。
type UplinkEventSnapshot struct {
	UnknownDeviceDropped int64
	Oversized            int64
	MediaTypeRejected    int64
	StorageCommitFailed  int64
}

// IncUplinkUnknownDropped 记录一次 409 unknown_device 重投后仍失败的丢弃。
func (m *Metrics) IncUplinkUnknownDropped() {
	if m == nil {
		return
	}
	m.uplinkUnknownDropped.Add(1)
}

// IncUplinkOversized 记录一次超过 maxBodyBytes 的消息体丢弃（仍会应答）。
func (m *Metrics) IncUplinkOversized() {
	if m == nil {
		return
	}
	m.uplinkOversized.Add(1)
}

// IncMediaTypeRejected 记录一次 0x0801 携带非图片类型的拒收（仍会应答）。
func (m *Metrics) IncMediaTypeRejected() {
	if m == nil {
		return
	}
	m.mediaTypeRejected.Add(1)
}

// IncStorageCommitFailed 记录一次文件已落库但提交标记失败的告警。
func (m *Metrics) IncStorageCommitFailed() {
	if m == nil {
		return
	}
	m.storageCommitFailed.Add(1)
}

// IncAutoUploadSkipped 记录一次 0x9208 自动触发因角色拆分而不可用的告警。
func (m *Metrics) IncAutoUploadSkipped() {
	if m == nil {
		return
	}
	m.autoUploadSkipped.Add(1)
}

// AutoUploadSkipped 返回 0x9208 自动触发被跳过的累计次数。
func (m *Metrics) AutoUploadSkipped() int64 {
	if m == nil {
		return 0
	}
	return m.autoUploadSkipped.Load()
}

// UplinkEventSnapshot 返回上行事件终态计数副本。
func (m *Metrics) UplinkEventSnapshot() UplinkEventSnapshot {
	if m == nil {
		return UplinkEventSnapshot{}
	}
	return UplinkEventSnapshot{
		UnknownDeviceDropped: m.uplinkUnknownDropped.Load(),
		Oversized:            m.uplinkOversized.Load(),
		MediaTypeRejected:    m.mediaTypeRejected.Load(),
		StorageCommitFailed:  m.storageCommitFailed.Load(),
	}
}

// retentionCounterFor 返回类别对应的计数器，调用方需持有 retentionMu。
func (m *Metrics) retentionCounterFor(category string) *retentionCounter {
	if m.retention == nil {
		m.retention = make(map[string]*retentionCounter)
	}
	counter := m.retention[category]
	if counter == nil {
		counter = &retentionCounter{}
		m.retention[category] = counter
	}
	return counter
}

// AddRetentionDeleted 累加某类别本次清理删除的行数。
func (m *Metrics) AddRetentionDeleted(category string, rows int64) {
	if m == nil || rows <= 0 {
		return
	}
	m.retentionMu.Lock()
	counter := m.retentionCounterFor(category)
	m.retentionMu.Unlock()
	counter.deleted.Add(rows)
}

// IncRetentionFailure 累加某类别本次清理的失败次数。
func (m *Metrics) IncRetentionFailure(category string) {
	if m == nil {
		return
	}
	m.retentionMu.Lock()
	counter := m.retentionCounterFor(category)
	m.retentionMu.Unlock()
	counter.failures.Add(1)
}

// RecordRetentionRun 记录一次清理任务的完成时间与总耗时。
func (m *Metrics) RecordRetentionRun(at time.Time, duration time.Duration) {
	if m == nil {
		return
	}
	m.retentionLastRun.Store(at.UnixMilli())
	m.retentionLastDuration.Store(duration.Milliseconds())
}

// AddOrphanScan 累加一次孤儿文件扫描的统计量。
func (m *Metrics) AddOrphanScan(scanned, deleted, skipped, failures int64) {
	if m == nil {
		return
	}
	m.orphanScanned.Add(scanned)
	m.orphanDeleted.Add(deleted)
	m.orphanSkipped.Add(skipped)
	m.orphanFailures.Add(failures)
}

// RecordOrphanScan 记录一次孤儿文件扫描的完成时间与总耗时。
func (m *Metrics) RecordOrphanScan(at time.Time, duration time.Duration) {
	if m == nil {
		return
	}
	m.orphanLastRun.Store(at.UnixMilli())
	m.orphanLastDuration.Store(duration.Milliseconds())
}

// OrphanSnapshot 是孤儿文件扫描的累计指标副本。
type OrphanSnapshot struct {
	Scanned          int64
	Deleted          int64
	Skipped          int64
	Failures         int64
	LastRunUnixMilli int64
	LastRunDuration  time.Duration
}

// OrphanSnapshot 返回孤儿文件扫描指标的副本；未执行过时各计数为 0。
func (m *Metrics) OrphanSnapshot() OrphanSnapshot {
	if m == nil {
		return OrphanSnapshot{}
	}
	return OrphanSnapshot{
		Scanned:          m.orphanScanned.Load(),
		Deleted:          m.orphanDeleted.Load(),
		Skipped:          m.orphanSkipped.Load(),
		Failures:         m.orphanFailures.Load(),
		LastRunUnixMilli: m.orphanLastRun.Load(),
		LastRunDuration:  time.Duration(m.orphanLastDuration.Load()) * time.Millisecond,
	}
}

// RetentionSnapshot 是清理任务累计指标的副本。
type RetentionSnapshot struct {
	Deleted  map[string]int64
	Failures map[string]int64
	// LastRunUnixMilli 为 0 表示尚未执行过清理。
	LastRunUnixMilli int64
	LastRunDuration  time.Duration
}

// RetentionSnapshot 返回清理任务指标的副本；未执行过时各计数为 0。
func (m *Metrics) RetentionSnapshot() RetentionSnapshot {
	snapshot := RetentionSnapshot{
		Deleted:  make(map[string]int64),
		Failures: make(map[string]int64),
	}
	if m == nil {
		return snapshot
	}
	m.retentionMu.Lock()
	for category, counter := range m.retention {
		snapshot.Deleted[category] = counter.deleted.Load()
		snapshot.Failures[category] = counter.failures.Load()
	}
	m.retentionMu.Unlock()
	snapshot.LastRunUnixMilli = m.retentionLastRun.Load()
	snapshot.LastRunDuration = time.Duration(m.retentionLastDuration.Load()) * time.Millisecond
	return snapshot
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
	// MediaFiles 与 MediaFileBytes 是 0x0801 落盘文件的存量，
	// 用于发现媒体目录在生产环境无声膨胀。
	MediaFiles     int64
	MediaFileBytes int64
	AckTimeouts    int64
	// Retention 是历史数据清理任务的累计指标。
	Retention RetentionSnapshot
	// Orphan 是媒体目录孤儿文件扫描的累计指标。
	Orphan OrphanSnapshot
	// Uplink 是上行出口的累计投递指标；未启用外部出口时全为 0。
	Uplink UplinkSnapshot
	// TerminalReply 是终端应答处理结果的累计指标。
	TerminalReply TerminalReplySnapshot
	// UplinkEvent 是上行事件在控制面被丢弃/拒收的累计指标。
	UplinkEvent UplinkEventSnapshot
	// AutoUploadSkipped 是 0x9208 自动触发因角色拆分被跳过的累计次数。
	AutoUploadSkipped int64
}

// UplinkSnapshot 是上行出口的累计计数与当前水位。
type UplinkSnapshot struct {
	Delivered int64
	Failed    int64
	Timeouts  int64
	Dropped   int64
	Rejected  int64
	InFlight  int64
	// ShardQueues 是每个 phone 分片队列的当前深度，下标即分片号。
	ShardQueues []int
}

// Check 是一条带名称的就绪检查。
type Check struct {
	Name string
	Fn   func(context.Context) error
}

// Degradation 表示检查通过但环境处于降级状态。
//
// 典型场景：集群模式使用 cluster.allowEmbeddedStorage 的 SQLite。
// 该配置只允许本地联调，进程仍可服务，但 /readyz 必须暴露 degraded，
// 避免被误当成可以投入生产的部署形态。
type Degradation struct {
	Reason string
}

// Error 实现 error；降级不是失败，调用方需用 IsDegraded 判定。
func (d *Degradation) Error() string { return d.Reason }

// Degraded 构造一条降级原因。
func Degraded(reason string) error { return &Degradation{Reason: reason} }

// Result 是单条检查的执行结果。
type Result struct {
	Name string
	Err  error
	// Degraded 为 true 表示检查通过但环境处于降级状态。
	Degraded bool
}

// OK 返回检查是否未失败；降级仍算通过。
func (r Result) OK() bool { return r.Err == nil || r.Degraded }

// IsDegraded 返回该检查是否处于降级状态。
func (r Result) IsDegraded() bool { return r.Degraded }

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
		err := check.Fn(ctx)
		result := Result{Name: check.Name, Err: err}
		if err != nil {
			var degradation *Degradation
			result.Degraded = errors.As(err, &degradation)
		}
		results = append(results, result)
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
	snapshot.Retention = p.Metrics.RetentionSnapshot()
	snapshot.Orphan = p.Metrics.OrphanSnapshot()
	snapshot.TerminalReply = p.Metrics.TerminalReplySnapshot()
	snapshot.UplinkEvent = p.Metrics.UplinkEventSnapshot()
	snapshot.AutoUploadSkipped = p.Metrics.AutoUploadSkipped()
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
		{"jt808_media_files", "Media files persisted from 0x0801 uploads.", "gauge", fmt.Sprint(snapshot.MediaFiles)},
		{"jt808_media_file_bytes", "Total bytes occupied by persisted 0x0801 media files.", "gauge", fmt.Sprint(snapshot.MediaFileBytes)},
		{"jt808_uplink_delivered_total", "Uplink events accepted by the configured sink.", "counter", fmt.Sprint(snapshot.Uplink.Delivered)},
		{"jt808_uplink_failed_total", "Uplink events whose sink delivery failed.", "counter", fmt.Sprint(snapshot.Uplink.Failed)},
		{"jt808_uplink_timeouts_total", "Uplink events whose sink delivery timed out.", "counter", fmt.Sprint(snapshot.Uplink.Timeouts)},
		{"jt808_uplink_dropped_total", "Uplink events dropped by the sink queue.", "counter", fmt.Sprint(snapshot.Uplink.Dropped)},
		{"jt808_uplink_rejected_total", "Confirm-before-ack events rejected due to in-flight limits.", "counter", fmt.Sprint(snapshot.Uplink.Rejected)},
		{"jt808_uplink_in_flight", "Confirm-before-ack uplink deliveries in flight.", "gauge", fmt.Sprint(snapshot.Uplink.InFlight)},
		{"jt808_uplink_unknown_device_dropped_total", "Uplink events dropped after a 409 unknown_device redelivery.", "counter", fmt.Sprint(snapshot.UplinkEvent.UnknownDeviceDropped)},
		{"jt808_uplink_oversized_total", "Uplink events dropped because the message body exceeded maxBodyBytes.", "counter", fmt.Sprint(snapshot.UplinkEvent.Oversized)},
		{"jt808_media_type_rejected_total", "0x0801 uploads rejected because the media type is not an image.", "counter", fmt.Sprint(snapshot.UplinkEvent.MediaTypeRejected)},
		{"jt808_storage_commit_failed_total", "Media files written but not confirmed as committed.", "counter", fmt.Sprint(snapshot.UplinkEvent.StorageCommitFailed)},
		{"jt808_attachment_trigger_skipped_total", "0x9208 automatic upload triggers skipped because gateway and control are in separate processes.", "counter", fmt.Sprint(snapshot.AutoUploadSkipped)},
	}
	for _, line := range lines {
		if _, err := fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n%s %s\n",
			line.name, line.help, line.name, line.kind, line.name, line.value); err != nil {
			return err
		}
	}
	if err := writeUplinkQueuePrometheus(w, snapshot.Uplink.ShardQueues); err != nil {
		return err
	}
	if err := writeRetentionPrometheus(w, snapshot.Retention); err != nil {
		return err
	}
	if err := writeTerminalReplyPrometheus(w, snapshot.TerminalReply); err != nil {
		return err
	}
	return writeOrphanPrometheus(w, snapshot.Orphan)
}

// writeUplinkQueuePrometheus 输出每个 phone 分片的队列深度。
// 出口按 phone 哈希分片，单一 queue_depth 无法反映实际拓扑，因此按分片打标签。
func writeUplinkQueuePrometheus(w io.Writer, depths []int) error {
	if len(depths) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(w,
		"# HELP jt808_uplink_queue_depth Uplink events waiting in each sink shard queue.\n"+
			"# TYPE jt808_uplink_queue_depth gauge\n"); err != nil {
		return err
	}
	for shard, depth := range depths {
		if _, err := fmt.Fprintf(w, "jt808_uplink_queue_depth{shard=\"%d\"} %d\n", shard, depth); err != nil {
			return err
		}
	}
	return nil
}

// writeTerminalReplyPrometheus 输出终端应答处理结果的累计计数。
// duplicate 表示与终态一致的重复应答；late 表示迟到或与终态矛盾的应答。
func writeTerminalReplyPrometheus(w io.Writer, snapshot TerminalReplySnapshot) error {
	lines := []struct {
		outcome string
		value   int64
	}{
		{"acked", snapshot.Acked},
		{"failed", snapshot.Failed},
		{"retry_scheduled", snapshot.RetryScheduled},
		{"duplicate", snapshot.Duplicate},
		{"late", snapshot.Late},
		{"unmatched", snapshot.Unmatched},
		{"ignored", snapshot.Ignored},
	}
	if _, err := fmt.Fprintf(w,
		"# HELP jt808_terminal_reply_total Terminal replies handled by outcome.\n"+
			"# TYPE jt808_terminal_reply_total counter\n"); err != nil {
		return err
	}
	for _, line := range lines {
		if _, err := fmt.Fprintf(w, "jt808_terminal_reply_total{outcome=%q} %d\n", line.outcome, line.value); err != nil {
			return err
		}
	}
	return nil
}

// writeOrphanPrometheus 输出孤儿文件扫描的累计指标与最近一次运行信息。
func writeOrphanPrometheus(w io.Writer, snapshot OrphanSnapshot) error {
	for _, line := range []struct {
		name  string
		help  string
		value int64
	}{
		{"jt808_orphan_files_scanned_total", "Files examined by the orphan media scan.", snapshot.Scanned},
		{"jt808_orphan_files_deleted_total", "Orphan media files deleted by the scan.", snapshot.Deleted},
		{"jt808_orphan_files_skipped_total", "Files skipped by the orphan scan (recent, temporary or still referenced).", snapshot.Skipped},
		{"jt808_orphan_scan_failures_total", "Errors encountered by the orphan media scan.", snapshot.Failures},
	} {
		if _, err := fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n%s %d\n",
			line.name, line.help, line.name, line.name, line.value); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w,
		"# HELP jt808_orphan_scan_last_run_timestamp_seconds Unix timestamp of the last orphan media scan.\n"+
			"# TYPE jt808_orphan_scan_last_run_timestamp_seconds gauge\n"+
			"jt808_orphan_scan_last_run_timestamp_seconds %s\n",
		strconv.FormatFloat(float64(snapshot.LastRunUnixMilli)/1000, 'f', -1, 64)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w,
		"# HELP jt808_orphan_scan_last_run_duration_seconds Duration of the last orphan media scan.\n"+
			"# TYPE jt808_orphan_scan_last_run_duration_seconds gauge\n"+
			"jt808_orphan_scan_last_run_duration_seconds %s\n",
		strconv.FormatFloat(snapshot.LastRunDuration.Seconds(), 'f', -1, 64)); err != nil {
		return err
	}
	return nil
}

// retentionMetricCategories 是清理指标的固定输出顺序，保证 exposition 稳定。
var retentionMetricCategories = []string{
	RetentionCategoryLocations,
	RetentionCategoryAlarms,
	RetentionCategoryMediaFiles,
	RetentionCategoryMediaCaptures,
	RetentionCategoryMediaSearchResults,
}

// writeRetentionPrometheus 输出历史数据清理任务的累计指标。
func writeRetentionPrometheus(w io.Writer, snapshot RetentionSnapshot) error {
	if _, err := fmt.Fprintf(w,
		"# HELP jt808_retention_deleted_total Rows deleted by the historical retention cleanup.\n"+
			"# TYPE jt808_retention_deleted_total counter\n"); err != nil {
		return err
	}
	for _, category := range retentionMetricCategories {
		if _, err := fmt.Fprintf(w, "jt808_retention_deleted_total{category=%q} %d\n",
			category, snapshot.Deleted[category]); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w,
		"# HELP jt808_retention_failures_total Failed batches by the historical retention cleanup.\n"+
			"# TYPE jt808_retention_failures_total counter\n"); err != nil {
		return err
	}
	for _, category := range retentionMetricCategories {
		if _, err := fmt.Fprintf(w, "jt808_retention_failures_total{category=%q} %d\n",
			category, snapshot.Failures[category]); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w,
		"# HELP jt808_retention_last_run_timestamp_seconds Unix timestamp of the last retention cleanup run.\n"+
			"# TYPE jt808_retention_last_run_timestamp_seconds gauge\n"+
			"jt808_retention_last_run_timestamp_seconds %s\n",
		strconv.FormatFloat(float64(snapshot.LastRunUnixMilli)/1000, 'f', -1, 64)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w,
		"# HELP jt808_retention_last_run_duration_seconds Duration of the last retention cleanup run.\n"+
			"# TYPE jt808_retention_last_run_duration_seconds gauge\n"+
			"jt808_retention_last_run_duration_seconds %s\n",
		strconv.FormatFloat(snapshot.LastRunDuration.Seconds(), 'f', -1, 64)); err != nil {
		return err
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
