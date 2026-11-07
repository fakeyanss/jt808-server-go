package alarmserver

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// bitmap 按块记录附件数据是否已完整接收，每块占 1 bit。
type bitmap []byte

func bitmapSize(totalBlocks int) int {
	if totalBlocks <= 0 {
		return 0
	}
	return (totalBlocks + 7) / 8
}

func (b bitmap) get(index int) bool {
	if index < 0 || index/8 >= len(b) {
		return false
	}
	return b[index/8]&(1<<uint(index%8)) != 0
}

// set 置位并返回本次是否由未置位变为置位，用于重复块不重复计数。
func (b bitmap) set(index int) bool {
	if index < 0 || index/8 >= len(b) {
		return false
	}
	mask := byte(1 << uint(index%8))
	if b[index/8]&mask != 0 {
		return false
	}
	b[index/8] |= mask
	return true
}

func (b bitmap) count() int {
	n := 0
	for _, v := range b {
		for v != 0 {
			n += int(v & 1)
			v >>= 1
		}
	}
	return n
}

// fileState 表示一个附件文件的接收进度。
// 同一文件的 WriteAt、位图修改与完成收尾必须在 st.mu 下串行执行。
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
	bitmap       bitmap
	totalBlocks  int
	received     int
	lastActivity time.Time

	completed bool
	removed   bool
	meta      Metadata
}

func newFileState(owner *Assembler, info AttachmentInfo, date, dir string, entry AttachmentEntry, fileType uint8) *fileState {
	size := int64(entry.FileSize)
	totalBlocks := 0
	if size > 0 {
		totalBlocks = int((size + DefaultBlockSize - 1) / DefaultBlockSize)
	}
	finalPath := filepath.Join(dir, entry.FileName)
	return &fileState{
		owner:       owner,
		fileName:    entry.FileName,
		fileType:    fileType,
		size:        size,
		phone:       info.Phone,
		terminalID:  info.TerminalID,
		alarmNo:     info.AlarmNo,
		alarmSign:   append([]byte(nil), info.AlarmSign...),
		date:        date,
		dir:         dir,
		partPath:    finalPath + partSuffix,
		finalPath:   finalPath,
		bitmap:      make(bitmap, bitmapSize(totalBlocks)),
		totalBlocks: totalBlocks,
	}
}

// adoptPersisted 在启动对账或断点续传时恢复位图。
// 仅当文件大小、位图长度与 .part 文件同时匹配时才采纳，避免脏状态导致误判完成。
func (st *fileState) adoptPersisted(p persistedState) bool {
	if p.Size != st.size || len(p.Bitmap) != len(st.bitmap) {
		return false
	}
	if _, err := os.Stat(st.partPath); err != nil {
		return false
	}
	copy(st.bitmap, p.Bitmap)
	st.received = st.bitmap.count()
	if st.fileType == 0 && p.FileType != 0 {
		st.fileType = p.FileType
	}
	if p.LastActivity > 0 {
		st.lastActivity = time.Unix(0, p.LastActivity)
	}
	return true
}

func (st *fileState) blockRange(index int) (offset, length int64) {
	start := int64(index) * DefaultBlockSize
	end := start + DefaultBlockSize
	if end > st.size {
		end = st.size
	}
	return start, end - start
}

// markRangeLocked 把被 [offset, offset+length) 完整覆盖的块置位。
// 非块对齐的碎片不会提前置位，宁可在 0x9212 中整块要求补传，也不误判完成。
func (st *fileState) markRangeLocked(offset, length int64) {
	if length <= 0 || st.size <= 0 {
		return
	}
	first := int(offset / DefaultBlockSize)
	last := int((offset + length - 1) / DefaultBlockSize)
	if last >= st.totalBlocks {
		last = st.totalBlocks - 1
	}
	for i := first; i <= last; i++ {
		blockOffset, blockLength := st.blockRange(i)
		if offset <= blockOffset && offset+length >= blockOffset+blockLength {
			if st.bitmap.set(i) {
				st.received++
			}
		}
	}
}

// missingLocked 按偏移升序返回未完成的连续块区间。
// 协议数量字段只有 1 字节，超过 255 段时先回报前 255 段，终端补传后再次 0x1212 收敛。
func (st *fileState) missingLocked() []MissingRange {
	if st.size <= 0 || st.received >= st.totalBlocks {
		return nil
	}
	var ranges []MissingRange
	start := -1
	for i := 0; i <= st.totalBlocks; i++ {
		missing := i < st.totalBlocks && !st.bitmap.get(i)
		switch {
		case missing && start < 0:
			start = i
		case !missing && start >= 0:
			firstOffset, _ := st.blockRange(start)
			lastOffset, lastLength := st.blockRange(i - 1)
			ranges = append(ranges, MissingRange{
				Offset: uint32(firstOffset),
				Length: uint32(lastOffset + lastLength - firstOffset),
			})
			start = -1
			if len(ranges) >= MaxRetransmitRanges {
				return ranges
			}
		}
	}
	return ranges
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
		Received:     int64(st.received),
		LastActivity: st.lastActivity.UnixNano(),
		Bitmap:       append([]byte(nil), st.bitmap...),
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
	st.lastActivity = time.Now()
	return st.persistLocked()
}

func (st *fileState) completeLocked() bool {
	if st.size == 0 {
		return true
	}
	return st.totalBlocks > 0 && st.received >= st.totalBlocks
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
		Phone:        st.phone,
		TerminalID:   st.terminalID,
		AlarmNo:      st.alarmNo,
		AlarmSignHex: hex.EncodeToString(st.alarmSign),
		FileName:     st.fileName,
		FileType:     st.fileType,
		Size:         st.size,
		MD5:          sum,
		Path:         st.finalPath,
		CompletedAt:  time.Now(),
	}
	if err := writeMetadata(st.finalPath, st.meta); err != nil {
		return fmt.Errorf("write attachment metadata: %w", err)
	}
	if err := os.Remove(statePath(st.partPath)); err != nil && !os.IsNotExist(err) {
		log.Warn().Err(err).Str("file", st.partPath).Msg("Fail to remove attachment state file")
	}
	st.completed = true
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
	st.lastActivity = time.Now()
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

func (st *fileState) hasProgress() bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.received > 0 || st.completed
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

// Assembler 按手机号 + 文件名管理所有进行中的附件文件。
type Assembler struct {
	cfg   Config
	mu    sync.Mutex
	files map[string]*fileState
	// active 统计当前持有文件句柄的附件数量，用原子操作避免与清理路径互相等待。
	active atomic.Int64
}

func NewAssembler(cfg Config) *Assembler {
	return &Assembler{
		cfg:   cfg.Normalize(),
		files: make(map[string]*fileState),
	}
}

func (a *Assembler) Config() Config { return a.cfg }

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
	for _, entry := range info.Attachments {
		if err := validateFileName(entry.FileName); err != nil {
			return err
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
				Phone:        info.Phone,
				TerminalID:   info.TerminalID,
				AlarmNo:      info.AlarmNo,
				AlarmSignHex: hex.EncodeToString(info.AlarmSign),
				FileName:     entry.FileName,
				FileType:     fileType,
				Size:         int64(entry.FileSize),
				Path:         finalPath,
				CompletedAt:  fi.ModTime(),
			}
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
	state.lastActivity = time.Now()

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

// HasProgress 报告文件是否已收到过数据，用于避免 0x1211 先于数据到达时误报整文件缺失。
func (a *Assembler) HasProgress(phone, fileName string) bool {
	st := a.lookup(phone, fileName)
	return st != nil && st.hasProgress()
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
func (a *Assembler) Select(phone string) []Metadata {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make([]Metadata, 0)
	for _, st := range a.files {
		st.mu.Lock()
		if st.completed && (phone == "" || st.phone == phone) {
			result = append(result, st.meta)
		}
		st.mu.Unlock()
	}
	return result
}

// FindMetadata 返回指定手机号与文件名的已完成附件元数据。
func (a *Assembler) FindMetadata(phone, fileName string) (Metadata, error) {
	st := a.lookup(phone, fileName)
	if st == nil {
		return Metadata{}, ErrFileNotRegistered
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.completed {
		return Metadata{}, ErrFileNotRegistered
	}
	return st.meta, nil
}
