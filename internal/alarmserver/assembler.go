package alarmserver

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// byteRange 表示附件文件中已连续覆盖的字节区间 [Offset, Offset+Length)。
// 补传记账按字节粒度进行，不要求终端按 64KB 块对齐。
type byteRange struct {
	Offset int64 `json:"offset"`
	Length int64 `json:"length"`
}

func (r byteRange) end() int64 { return r.Offset + r.Length }

// normalizeRanges 将任意区间集合裁剪到 [0, size) 内，按偏移排序并合并相邻/重叠区间。
func normalizeRanges(ranges []byteRange, size int64) []byteRange {
	if size <= 0 || len(ranges) == 0 {
		return nil
	}
	clipped := make([]byteRange, 0, len(ranges))
	for _, item := range ranges {
		start, end := item.Offset, item.end()
		if start < 0 {
			start = 0
		}
		if end > size {
			end = size
		}
		if start >= end {
			continue
		}
		clipped = append(clipped, byteRange{Offset: start, Length: end - start})
	}
	if len(clipped) == 0 {
		return nil
	}
	sort.Slice(clipped, func(i, j int) bool { return clipped[i].Offset < clipped[j].Offset })
	merged := clipped[:1]
	for _, item := range clipped[1:] {
		last := &merged[len(merged)-1]
		if item.Offset <= last.end() {
			if item.end() > last.end() {
				last.Length = item.end() - last.Offset
			}
			continue
		}
		merged = append(merged, item)
	}
	return merged
}

func coveredBytes(ranges []byteRange) int64 {
	var total int64
	for _, item := range ranges {
		total += item.Length
	}
	return total
}

// fileState 表示一个附件文件的接收进度。
// 同一文件的 WriteAt、区间合并与完成收尾必须在 st.mu 下串行执行。
type fileState struct {
	mu sync.Mutex

	owner *Assembler

	fileName   string
	fileType   uint8
	size       int64
	phone      string
	terminalID string
	alarmNo    string
	alarmSign  []byte
	date       string

	dir       string
	partPath  string
	finalPath string

	file         *os.File
	ranges       []byteRange
	covered      int64
	lastActivity time.Time
	completedAt  time.Time

	completed bool
	removed   bool
	meta      Metadata
}

func newFileState(owner *Assembler, info AttachmentInfo, date, dir string, entry AttachmentEntry, fileType uint8) *fileState {
	finalPath := filepath.Join(dir, entry.FileName)
	return &fileState{
		owner:      owner,
		fileName:   entry.FileName,
		fileType:   fileType,
		size:       int64(entry.FileSize),
		phone:      info.Phone,
		terminalID: info.TerminalID,
		alarmNo:    info.AlarmNo,
		alarmSign:  append([]byte(nil), info.AlarmSign...),
		date:       date,
		dir:        dir,
		partPath:   finalPath + partSuffix,
		finalPath:  finalPath,
	}
}

// adoptPersisted 在启动对账或断点续传时恢复已覆盖区间。
// 仅当文件大小与 .part 文件同时匹配时才采纳，避免脏状态导致误判完成。
func (st *fileState) adoptPersisted(p persistedState) bool {
	if p.Size != st.size {
		return false
	}
	if _, err := os.Stat(st.partPath); err != nil {
		return false
	}
	st.ranges = normalizeRanges(p.Ranges, st.size)
	st.covered = coveredBytes(st.ranges)
	if st.fileType == 0 && p.FileType != 0 {
		st.fileType = p.FileType
	}
	if p.LastActivity > 0 {
		st.lastActivity = time.Unix(0, p.LastActivity)
	}
	return true
}

// markRangeLocked 将 [offset, offset+length) 并入已覆盖区间集合。
// 任意字节粒度的碎片帧都会立即生效；重叠、重复、相邻区间自动合并。
func (st *fileState) markRangeLocked(offset, length int64) {
	if length <= 0 || st.size <= 0 {
		return
	}
	start, end := offset, offset+length
	if start < 0 {
		start = 0
	}
	if end > st.size {
		end = st.size
	}
	if start >= end {
		return
	}

	index := sort.Search(len(st.ranges), func(i int) bool { return st.ranges[i].end() >= start })
	last := index
	replaced := int64(0)
	for last < len(st.ranges) && st.ranges[last].Offset <= end {
		if st.ranges[last].Offset < start {
			start = st.ranges[last].Offset
		}
		if st.ranges[last].end() > end {
			end = st.ranges[last].end()
		}
		replaced += st.ranges[last].Length
		last++
	}
	st.covered += (end - start) - replaced
	merged := byteRange{Offset: start, Length: end - start}
	st.ranges = append(st.ranges[:index], append([]byteRange{merged}, st.ranges[last:]...)...)
}

// missingLocked 以 [0, fileSize) 为全集，减去已覆盖区间后返回按 offset 升序的缺口。
// 首段缺口与尾段缺口都会返回；协议数量字段只有 1 字节，超过 255 段时先回报前 255 段，
// 终端补传后再次 0x1212 收敛。
func (st *fileState) missingLocked() []MissingRange {
	if st.size <= 0 || st.covered >= st.size {
		return nil
	}
	result := make([]MissingRange, 0)
	cursor := int64(0)
	for _, item := range st.ranges {
		if item.Offset > cursor {
			result = append(result, MissingRange{
				Offset: uint32(cursor),
				Length: uint32(item.Offset - cursor),
			})
			if len(result) >= MaxRetransmitRanges {
				return result
			}
		}
		if item.end() > cursor {
			cursor = item.end()
		}
	}
	if cursor < st.size {
		result = append(result, MissingRange{
			Offset: uint32(cursor),
			Length: uint32(st.size - cursor),
		})
	}
	return result
}

func (st *fileState) ensureOpenLocked() error {
	if st.file != nil {
		return nil
	}
	if err := st.owner.acquire(); err != nil {
		return err
	}
	file, err := os.OpenFile(st.partPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		st.owner.release()
		return err
	}
	if err := file.Truncate(st.size); err != nil {
		_ = file.Close()
		st.owner.release()
		return err
	}
	st.file = file
	return nil
}

func (st *fileState) closeFileLocked() {
	if st.file == nil {
		return
	}
	if err := st.file.Close(); err != nil {
		log.Warn().Err(err).Str("file", st.partPath).Msg("Fail to close attachment file")
	}
	st.file = nil
	st.owner.release()
}

func (st *fileState) persistLocked() error {
	return writePersistedState(st.partPath, persistedState{
		FileName:     st.fileName,
		FileType:     st.fileType,
		Size:         st.size,
		Phone:        st.phone,
		TerminalID:   st.terminalID,
		AlarmNo:      st.alarmNo,
		AlarmSign:    st.alarmSign,
		Date:         st.date,
		LastActivity: st.lastActivity.UnixNano(),
		Ranges:       append([]byteRange(nil), st.ranges...),
	})
}

func (st *fileState) writeAt(offset int64, data []byte) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	if st.removed {
		return ErrFileNotRegistered
	}
	if st.completed {
		return nil
	}
	if len(data) == 0 || len(data) > FramePayloadLimit {
		return ErrInvalidChunkLength
	}
	if offset < 0 || offset+int64(len(data)) > st.size {
		return ErrOffsetOutOfRange
	}
	if err := st.ensureOpenLocked(); err != nil {
		return err
	}
	if _, err := st.file.WriteAt(data, offset); err != nil {
		return err
	}
	st.markRangeLocked(offset, int64(len(data)))
	st.lastActivity = st.owner.nowTime()
	return st.persistLocked()
}

func (st *fileState) completeLocked() bool {
	if st.size == 0 {
		return true
	}
	return st.covered >= st.size
}

func (st *fileState) finishLocked() error {
	if st.completed {
		return nil
	}
	if !st.completeLocked() {
		return fmt.Errorf("attachment file is incomplete")
	}
	st.closeFileLocked()
	if st.size == 0 {
		// 空文件不会经过数据帧，需要显式创建占位，保证 rename 有源文件。
		if _, err := os.Stat(st.partPath); os.IsNotExist(err) {
			if err := os.WriteFile(st.partPath, nil, 0o644); err != nil {
				return fmt.Errorf("create empty attachment file: %w", err)
			}
		}
	}
	if err := os.Rename(st.partPath, st.finalPath); err != nil {
		return fmt.Errorf("rename attachment file: %w", err)
	}
	sum, err := fileMD5(st.finalPath)
	if err != nil {
		return err
	}
	st.meta = Metadata{
		Phone:              st.phone,
		TerminalID:         st.terminalID,
		AlarmNo:            st.alarmNo,
		AlarmSignHex:       hex.EncodeToString(st.alarmSign),
		FileName:           st.fileName,
		FileType:           st.fileType,
		Size:               st.size,
		MD5:                sum,
		Path:               st.finalPath,
		CompletedAt:        st.owner.nowTime(),
		FileNameConformant: validateAttachmentFileNameFormat(st.fileName) == nil,
		AlarmSignValid:     validateAlarmSign(st.alarmSign) == nil,
	}
	if err := writeMetadata(st.finalPath, st.meta); err != nil {
		return fmt.Errorf("write attachment metadata: %w", err)
	}
	if err := os.Remove(statePath(st.partPath)); err != nil && !os.IsNotExist(err) {
		log.Warn().Err(err).Str("file", st.partPath).Msg("Fail to remove attachment state file")
	}
	st.completed = true
	st.completedAt = st.meta.CompletedAt
	st.owner.completed.Add(1)
	log.Info().Str("phone", st.phone).Str("file", st.fileName).Int64("size", st.size).
		Str("md5", sum).Msg("Attachment file completed")
	return nil
}

// check 检查文件完整性；到齐后落成正式文件并返回完成结果。
func (st *fileState) check() (CompletionResult, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.removed {
		return CompletionResult{}, ErrFileNotRegistered
	}
	st.lastActivity = st.owner.nowTime()
	if st.completed {
		return CompletionResult{Complete: true}, nil
	}
	if st.completeLocked() {
		if err := st.finishLocked(); err != nil {
			return CompletionResult{}, err
		}
		return CompletionResult{Complete: true}, nil
	}
	return CompletionResult{Complete: false, Missing: st.missingLocked()}, nil
}

func (st *fileState) updateFileType(fileType uint8) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.fileType == 0 && fileType != 0 {
		st.fileType = fileType
	}
}

// expire 在无进展超时后清理半成品文件与状态，防止目录膨胀。
func (st *fileState) expire(now time.Time, timeout time.Duration) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.removed || st.completed {
		return false
	}
	if now.Sub(st.lastActivity) < timeout {
		return false
	}
	st.closeFileLocked()
	if err := os.Remove(st.partPath); err != nil && !os.IsNotExist(err) {
		log.Warn().Err(err).Str("file", st.partPath).Msg("Fail to remove stale attachment part")
	}
	if err := os.Remove(statePath(st.partPath)); err != nil && !os.IsNotExist(err) {
		log.Warn().Err(err).Str("file", st.partPath).Msg("Fail to remove stale attachment state")
	}
	st.removed = true
	log.Warn().Str("phone", st.phone).Str("file", st.fileName).Int64("size", st.size).
		Msg("Discard attachment file without progress")
	return true
}

// expireCompleted 在完成态 TTL 到期后只释放内存状态，保留正式文件与 .meta.json。
// ttl <= 0 表示不淘汰；未完成文件不在此路径处理。
func (st *fileState) expireCompleted(now time.Time, ttl time.Duration) bool {
	if ttl <= 0 {
		return false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.removed || !st.completed {
		return false
	}
	if st.completedAt.IsZero() || now.Sub(st.completedAt) < ttl {
		return false
	}
	st.removed = true
	log.Debug().Str("phone", st.phone).Str("file", st.fileName).Dur("ttl", ttl).
		Msg("Evict completed attachment memory state")
	return true
}

// Assembler 按手机号 + 文件名管理所有进行中的附件文件。
type Assembler struct {
	cfg   Config
	mu    sync.Mutex
	files map[string]*fileState
	// active 统计当前持有文件句柄的附件数量，用原子操作避免与清理路径互相等待。
	active atomic.Int64
	// completed 统计进程启动后完成落盘的附件数量，供指标采集。
	completed atomic.Int64
	// now 可注入时钟，测试据此验证完成态 TTL，无需等待真实时间。
	now func() time.Time
}

func NewAssembler(cfg Config) *Assembler {
	return &Assembler{
		cfg:   cfg.Normalize(),
		files: make(map[string]*fileState),
		now:   time.Now,
	}
}

func (a *Assembler) nowTime() time.Time { return a.now() }

func (a *Assembler) Config() Config { return a.cfg }

// Stats 返回当前正在组装的附件数与进程启动后完成落盘的附件数。
func (a *Assembler) Stats() (active, completed int64) {
	if a == nil {
		return 0, 0
	}
	return a.active.Load(), a.completed.Load()
}

func fileKey(phone, fileName string) string { return phone + "\x00" + fileName }

func (a *Assembler) acquire() error {
	for {
		current := a.active.Load()
		if current >= int64(a.cfg.MaxConcurrent) {
			return ErrTooManyFiles
		}
		if a.active.CompareAndSwap(current, current+1) {
			return nil
		}
	}
}

func (a *Assembler) release() {
	for {
		current := a.active.Load()
		if current <= 0 {
			return
		}
		if a.active.CompareAndSwap(current, current-1) {
			return
		}
	}
}

func (a *Assembler) lookup(phone, fileName string) *fileState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.files[fileKey(phone, fileName)]
}

// RegisterAlarm 处理 0x1210 的附件清单登记。
// 只登记元数据，文件句柄与位图在首个数据帧到达时才真正启用。
func (a *Assembler) RegisterAlarm(info AttachmentInfo) error {
	if err := validateSegment(info.Phone); err != nil {
		return err
	}
	if len(info.Attachments) == 0 {
		return nil
	}
	date := alarmDate(info.AlarmSign, info.Date)
	dir := filepath.Join(a.cfg.Dir, info.Phone, date, sanitizeLabel(info.AlarmNo))
	if err := validateAlarmSign(info.AlarmSign); err != nil {
		// 降级接收：报警标识号不合规只影响元数据标记与目录日期推导，
		// 拒绝整条 0x1210 会让终端无法补传任何附件。
		log.Warn().Err(err).Str("phone", info.Phone).Str("alarm", info.AlarmNo).
			Msg("Accept 0x1210 with non-conformant alarm sign")
	}
	for _, entry := range info.Attachments {
		if err := validateFileName(entry.FileName); err != nil {
			return err
		}
		if err := validateAttachmentFileNameFormat(entry.FileName); err != nil {
			log.Warn().Err(err).Str("phone", info.Phone).Str("file", entry.FileName).
				Msg("Accept attachment with non-conformant file name")
		}
		if int64(entry.FileSize) > a.cfg.MaxFileSize {
			return ErrFileTooLarge
		}
		if _, err := a.registerFile(info, date, dir, entry, 0); err != nil {
			return err
		}
	}
	return nil
}

// RegisterFile 处理 0x1211 的文件信息上传；未在 0x1210 清单中的文件按当前连接信息补登记。
func (a *Assembler) RegisterFile(info AttachmentInfo, fileName string, fileType uint8, size uint32) (*fileState, error) {
	if err := validateFileName(fileName); err != nil {
		return nil, err
	}
	if int64(size) > a.cfg.MaxFileSize {
		return nil, ErrFileTooLarge
	}
	if st := a.lookup(info.Phone, fileName); st != nil {
		st.updateFileType(fileType)
		return st, nil
	}
	if err := validateAttachmentFileNameFormat(fileName); err != nil {
		log.Warn().Err(err).Str("phone", info.Phone).Str("file", fileName).
			Msg("Accept attachment with non-conformant file name")
	}
	if err := validateAlarmSign(info.AlarmSign); err != nil {
		log.Warn().Err(err).Str("phone", info.Phone).Str("alarm", info.AlarmNo).
			Msg("Accept 0x1211 with non-conformant alarm sign")
	}
	date := alarmDate(info.AlarmSign, info.Date)
	dir := filepath.Join(a.cfg.Dir, info.Phone, date, sanitizeLabel(info.AlarmNo))
	return a.registerFile(info, date, dir, AttachmentEntry{FileName: fileName, FileSize: size}, fileType)
}

func (a *Assembler) registerFile(info AttachmentInfo, date, dir string, entry AttachmentEntry, fileType uint8) (*fileState, error) {
	key := fileKey(info.Phone, entry.FileName)
	a.mu.Lock()
	if existing, ok := a.files[key]; ok {
		a.mu.Unlock()
		if fileType != 0 {
			existing.updateFileType(fileType)
		}
		return existing, nil
	}
	a.mu.Unlock()

	finalPath := filepath.Join(dir, entry.FileName)
	if fi, err := os.Stat(finalPath); err == nil && fi.Size() == int64(entry.FileSize) {
		state := newFileState(a, info, date, dir, entry, fileType)
		state.completed = true
		if meta, readErr := readMetadata(finalPath + metaSuffix); readErr == nil {
			state.meta = meta
		} else {
			state.meta = Metadata{
				Phone:              info.Phone,
				TerminalID:         info.TerminalID,
				AlarmNo:            info.AlarmNo,
				AlarmSignHex:       hex.EncodeToString(info.AlarmSign),
				FileName:           entry.FileName,
				FileType:           fileType,
				Size:               int64(entry.FileSize),
				Path:               finalPath,
				CompletedAt:        fi.ModTime(),
				FileNameConformant: validateAttachmentFileNameFormat(entry.FileName) == nil,
				AlarmSignValid:     validateAlarmSign(info.AlarmSign) == nil,
			}
		}
		state.completedAt = state.meta.CompletedAt
		if state.completedAt.IsZero() {
			state.completedAt = fi.ModTime()
		}
		a.mu.Lock()
		if existing, ok := a.files[key]; ok {
			a.mu.Unlock()
			return existing, nil
		}
		a.files[key] = state
		a.mu.Unlock()
		log.Info().Str("phone", info.Phone).Str("file", entry.FileName).Msg("Attachment file already completed, reuse existing file")
		return state, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create attachment directory: %w", err)
	}
	state := newFileState(a, info, date, dir, entry, fileType)
	if persisted, err := readPersistedState(statePath(state.partPath)); err == nil {
		state.adoptPersisted(persisted)
	}
	state.lastActivity = a.nowTime()

	a.mu.Lock()
	if existing, ok := a.files[key]; ok {
		a.mu.Unlock()
		return existing, nil
	}
	a.files[key] = state
	a.mu.Unlock()

	state.mu.Lock()
	defer state.mu.Unlock()
	if err := state.persistLocked(); err != nil {
		log.Warn().Err(err).Str("file", state.partPath).Msg("Fail to persist attachment state")
	}
	return state, nil
}

// WriteChunk 按字节偏移随机写入附件数据。
func (a *Assembler) WriteChunk(phone, fileName string, offset uint32, data []byte) error {
	st := a.lookup(phone, fileName)
	if st == nil {
		return ErrFileNotRegistered
	}
	return st.writeAt(int64(offset), data)
}

// CompleteCheck 检查文件完整性并返回缺口；文件到齐时在此完成落盘收尾。
func (a *Assembler) CompleteCheck(phone, fileName string) (CompletionResult, error) {
	st := a.lookup(phone, fileName)
	if st == nil {
		return CompletionResult{}, ErrFileNotRegistered
	}
	return st.check()
}

// CleanupStale 清理超过 timeout 无进展的文件，返回清理数量。
func (a *Assembler) CleanupStale(now time.Time, timeout time.Duration) int {
	a.mu.Lock()
	states := make([]*fileState, 0, len(a.files))
	for _, st := range a.files {
		states = append(states, st)
	}
	a.mu.Unlock()

	removed := 0
	for _, st := range states {
		if st.expire(now, timeout) {
			removed++
		}
	}
	if removed == 0 {
		return 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for key, st := range a.files {
		st.mu.Lock()
		removedNow := st.removed
		st.mu.Unlock()
		if removedNow {
			delete(a.files, key)
		}
	}
	return removed
}

// CleanupCompleted 淘汰完成态超过 ttl 的内存状态，返回淘汰数量。
// 只释放内存，正式文件与 .meta.json 保留，Select/FindMetadata 仍可查询；
// ttl <= 0 表示不淘汰。未完成文件不受影响，仍由 CleanupStale 按 SessionTimeout 清理。
func (a *Assembler) CleanupCompleted(now time.Time, ttl time.Duration) int {
	if ttl <= 0 {
		return 0
	}
	a.mu.Lock()
	states := make([]*fileState, 0, len(a.files))
	for _, st := range a.files {
		states = append(states, st)
	}
	a.mu.Unlock()

	removed := 0
	for _, st := range states {
		if st.expireCompleted(now, ttl) {
			removed++
		}
	}
	if removed == 0 {
		return 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for key, st := range a.files {
		st.mu.Lock()
		removedNow := st.removed
		st.mu.Unlock()
		if removedNow {
			delete(a.files, key)
		}
	}
	return removed
}

// Close 关闭全部文件句柄，供服务停机与测试使用。
func (a *Assembler) Close() {
	a.mu.Lock()
	states := make([]*fileState, 0, len(a.files))
	for _, st := range a.files {
		states = append(states, st)
	}
	a.mu.Unlock()
	for _, st := range states {
		st.mu.Lock()
		st.closeFileLocked()
		st.mu.Unlock()
	}
}

// Reconcile 扫描落盘目录并与位图状态对账：
//   - 有状态无 .part：删除残留状态；
//   - 位图已完整：补做 rename 与元数据生成；
//   - 有 .part 无状态：无法恢复块信息，删除并告警。
func (a *Assembler) Reconcile() error {
	if err := os.MkdirAll(a.cfg.Dir, 0o755); err != nil {
		return fmt.Errorf("create attachment root: %w", err)
	}
	type candidate struct {
		statePath string
		partPath  string
	}
	var candidates []candidate
	parts := make(map[string]struct{})

	err := filepath.WalkDir(a.cfg.Dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		switch {
		case strings.HasSuffix(path, stateSuffix):
			candidates = append(candidates, candidate{statePath: path, partPath: strings.TrimSuffix(path, stateSuffix)})
		case strings.HasSuffix(path, partSuffix):
			parts[path] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return err
	}

	for _, item := range candidates {
		delete(parts, item.partPath)
		persisted, readErr := readPersistedState(item.statePath)
		if readErr != nil {
			log.Warn().Err(readErr).Str("file", item.statePath).Msg("Discard unreadable attachment state")
			_ = os.Remove(item.statePath)
			continue
		}
		if _, statErr := os.Stat(item.partPath); statErr != nil {
			log.Warn().Str("file", item.partPath).Msg("Discard attachment state without part file")
			_ = os.Remove(item.statePath)
			continue
		}
		date := persisted.Date
		if _, err := parseDate(date); err != nil {
			date = nowDate()
		}
		info := AttachmentInfo{
			Phone:      persisted.Phone,
			TerminalID: persisted.TerminalID,
			AlarmNo:    persisted.AlarmNo,
			AlarmSign:  persisted.AlarmSign,
			Date:       date,
		}
		dir := filepath.Dir(item.partPath)
		st := newFileState(a, info, date, dir, AttachmentEntry{FileName: persisted.FileName, FileSize: uint32(persisted.Size)}, persisted.FileType)
		st.partPath = item.partPath
		st.finalPath = strings.TrimSuffix(item.partPath, partSuffix)
		if !st.adoptPersisted(persisted) {
			log.Warn().Str("file", item.partPath).Msg("Discard attachment state mismatching part file")
			_ = os.Remove(item.statePath)
			_ = os.Remove(item.partPath)
			continue
		}
		key := fileKey(st.phone, st.fileName)
		a.mu.Lock()
		if _, ok := a.files[key]; !ok {
			a.files[key] = st
		}
		a.mu.Unlock()
		st.mu.Lock()
		if st.completeLocked() {
			if err := st.finishLocked(); err != nil {
				log.Warn().Err(err).Str("file", item.partPath).Msg("Fail to finish reconciled attachment")
			}
		}
		st.mu.Unlock()
	}

	for orphan := range parts {
		log.Warn().Str("file", orphan).Msg("Remove orphan attachment part without state")
		_ = os.Remove(orphan)
	}
	return nil
}

// Select 返回已完成的附件元数据，phone 为空时返回全部。
// 内存态之外还会扫描落盘元数据，保证进程重启后历史附件仍可查询。
func (a *Assembler) Select(phone string) []Metadata {
	merged := make(map[string]Metadata)
	a.mu.Lock()
	for _, st := range a.files {
		st.mu.Lock()
		if st.completed && (phone == "" || st.phone == phone) {
			merged[st.meta.Path] = st.meta
		}
		st.mu.Unlock()
	}
	a.mu.Unlock()

	if phone == "" || validateSegment(phone) == nil {
		root := a.cfg.Dir
		if phone != "" {
			root = filepath.Join(root, phone)
		}
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, metaSuffix) {
				return nil
			}
			meta, readErr := readMetadata(path)
			if readErr != nil {
				return nil
			}
			if phone == "" || meta.Phone == phone {
				merged[meta.Path] = meta
			}
			return nil
		})
	}

	result := make([]Metadata, 0, len(merged))
	for _, meta := range merged {
		result = append(result, meta)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CompletedAt.After(result[j].CompletedAt)
	})
	return result
}

// FindMetadata 返回指定手机号与文件名的已完成附件元数据。
func (a *Assembler) FindMetadata(phone, fileName string) (Metadata, error) {
	if st := a.lookup(phone, fileName); st != nil {
		st.mu.Lock()
		completed := st.completed
		meta := st.meta
		st.mu.Unlock()
		if completed {
			return meta, nil
		}
	}
	for _, meta := range a.Select(phone) {
		if meta.FileName == fileName {
			return meta, nil
		}
	}
	return Metadata{}, ErrFileNotRegistered
}
