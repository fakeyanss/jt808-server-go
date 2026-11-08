package alarmserver

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testAlarmSign() []byte {
	// 终端ID(7) + 时间 BCD(6) 2026-01-07 12:00:00 + 序号 + 附件数量 + 预留
	sign := []byte{'A', 'B', 'C', '1', '2', '3', '4'}
	sign = append(sign, 0x26, 0x01, 0x07, 0x12, 0x00, 0x00)
	return append(sign, 0x00, 0x01, 0x00)
}

func newTestAssembler(t *testing.T) (*Assembler, string) {
	t.Helper()
	dir := t.TempDir()
	assembler := NewAssembler(Config{
		Dir:            dir,
		MaxFileSize:    16 * 1024 * 1024,
		MaxConcurrent:  4,
		SessionTimeout: time.Minute,
	})
	t.Cleanup(assembler.Close)
	return assembler, dir
}

func registerTestFile(t *testing.T, assembler *Assembler, phone, fileName string, size int) {
	t.Helper()
	err := assembler.RegisterAlarm(AttachmentInfo{
		Phone:      phone,
		TerminalID: "ABC1234",
		AlarmSign:  testAlarmSign(),
		AlarmNo:    "ALARM-0001",
		Attachments: []AttachmentEntry{
			{FileName: fileName, FileSize: uint32(size)},
		},
	})
	require.NoError(t, err)
}

func buildSource(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i*31 + 7)
	}
	return data
}

func writeBlock(t *testing.T, assembler *Assembler, phone, fileName string, source []byte, index int) {
	t.Helper()
	offset := index * DefaultBlockSize
	end := offset + DefaultBlockSize
	if end > len(source) {
		end = len(source)
	}
	require.NoError(t, assembler.WriteChunk(phone, fileName, uint32(offset), source[offset:end]))
}

func TestAssemblerOutOfOrderMissingRangesAndMD5(t *testing.T) {
	assembler, dir := newTestAssembler(t)
	phone := "013800000001"
	fileName := "00_64_6401_01_ALARM.jpg"
	size := 9*DefaultBlockSize + 1000 // 10 块，最后一块 1000 字节
	source := buildSource(size)
	registerTestFile(t, assembler, phone, fileName, size)

	dropped := map[int]bool{1: true, 4: true, 9: true}
	order := make([]int, 0, 10)
	for i := 0; i < 10; i++ {
		if !dropped[i] {
			order = append(order, i)
		}
	}
	rand.New(rand.NewSource(42)).Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	for _, index := range order {
		writeBlock(t, assembler, phone, fileName, source, index)
	}

	result, err := assembler.CompleteCheck(phone, fileName)
	require.NoError(t, err)
	require.False(t, result.Complete)
	require.Equal(t, []MissingRange{
		{Offset: 1 * DefaultBlockSize, Length: DefaultBlockSize},
		{Offset: 4 * DefaultBlockSize, Length: DefaultBlockSize},
		{Offset: 9 * DefaultBlockSize, Length: 1000},
	}, result.Missing)

	for index := range dropped {
		writeBlock(t, assembler, phone, fileName, source, index)
	}
	result, err = assembler.CompleteCheck(phone, fileName)
	require.NoError(t, err)
	require.True(t, result.Complete)

	finalPath := filepath.Join(dir, phone, "20260107", "ALARM-0001", fileName)
	onDisk, err := os.ReadFile(finalPath)
	require.NoError(t, err)
	require.Equal(t, source, onDisk)
	require.Equal(t, fmt.Sprintf("%x", md5.Sum(source)), fmt.Sprintf("%x", md5.Sum(onDisk)))

	_, statErr := os.Stat(filepath.Join(finalPath + ".part"))
	require.True(t, os.IsNotExist(statErr))
	_, stateErr := os.Stat(finalPath + stateSuffix)
	require.True(t, os.IsNotExist(stateErr))
	meta, err := readMetadata(finalPath + metaSuffix)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("%x", md5.Sum(source)), meta.MD5)
	require.Equal(t, uint8(0), meta.FileType)
}

func TestAssemblerDuplicateChunkNotCountedTwice(t *testing.T) {
	assembler, _ := newTestAssembler(t)
	phone := "013800000002"
	fileName := "dup.jpg"
	size := 3 * DefaultBlockSize
	source := buildSource(size)
	registerTestFile(t, assembler, phone, fileName, size)

	writeBlock(t, assembler, phone, fileName, source, 0)
	writeBlock(t, assembler, phone, fileName, source, 0)
	state := assembler.lookup(phone, fileName)
	require.NotNil(t, state)
	state.mu.Lock()
	require.Equal(t, int64(DefaultBlockSize), state.covered)
	state.mu.Unlock()

	result, err := assembler.CompleteCheck(phone, fileName)
	require.NoError(t, err)
	require.False(t, result.Complete)
	require.Equal(t, []MissingRange{{Offset: DefaultBlockSize, Length: 2 * DefaultBlockSize}}, result.Missing)
}

func TestAssemblerRejectsOutOfRangeOffset(t *testing.T) {
	assembler, _ := newTestAssembler(t)
	phone := "013800000003"
	fileName := "range.jpg"
	size := 2 * DefaultBlockSize
	registerTestFile(t, assembler, phone, fileName, size)

	require.ErrorIs(t, assembler.WriteChunk(phone, fileName, uint32(size-10), make([]byte, 20)), ErrOffsetOutOfRange)
	require.ErrorIs(t, assembler.WriteChunk(phone, fileName, uint32(size), []byte{1}), ErrOffsetOutOfRange)
	require.ErrorIs(t, assembler.WriteChunk(phone, fileName, 0, nil), ErrInvalidChunkLength)
	require.ErrorIs(t, assembler.WriteChunk(phone, "unknown.jpg", 0, []byte{1}), ErrFileNotRegistered)
}

func TestValidateFileNameRejectsTraversal(t *testing.T) {
	for _, name := range []string{"", "../x.jpg", "/etc/passwd", "..", ".", "a/b.jpg", `a\b.jpg`, "a\x00b.jpg", "文件名.jpg"} {
		require.ErrorIs(t, validateFileName(name), ErrInvalidFileName, name)
	}
	for _, name := range []string{"00_64_6401_01_ALARM.jpg", "a-b_c.d"} {
		require.NoError(t, validateFileName(name))
	}
}

func TestAssemblerPersistAndReconcile(t *testing.T) {
	assembler, dir := newTestAssembler(t)
	phone := "013800000004"
	fileName := "resume.h264"
	size := 4 * DefaultBlockSize
	source := buildSource(size)
	registerTestFile(t, assembler, phone, fileName, size)

	for _, index := range []int{0, 2} {
		writeBlock(t, assembler, phone, fileName, source, index)
	}
	assembler.Close()

	restored := NewAssembler(assembler.Config())
	t.Cleanup(restored.Close)
	require.NoError(t, restored.Reconcile())
	state := restored.lookup(phone, fileName)
	require.NotNil(t, state)
	state.mu.Lock()
	require.Equal(t, int64(2*DefaultBlockSize), state.covered)
	state.mu.Unlock()

	for _, index := range []int{1, 3} {
		writeBlock(t, restored, phone, fileName, source, index)
	}
	result, err := restored.CompleteCheck(phone, fileName)
	require.NoError(t, err)
	require.True(t, result.Complete)

	onDisk, err := os.ReadFile(filepath.Join(dir, phone, "20260107", "ALARM-0001", fileName))
	require.NoError(t, err)
	require.Equal(t, source, onDisk)
}

func TestAssemblerConcurrentFilesDoNotInterfere(t *testing.T) {
	assembler, dir := newTestAssembler(t)
	phone := "013800000005"
	size := 6*DefaultBlockSize + 321

	type fileCase struct {
		name   string
		source []byte
		drop   int
	}
	cases := []fileCase{
		{name: "one.jpg", source: buildSource(size), drop: 2},
		{name: "two.wav", source: buildSource(size + 17), drop: 5},
	}
	for _, item := range cases {
		require.NoError(t, assembler.RegisterAlarm(AttachmentInfo{
			Phone:      phone,
			TerminalID: "ABC1234",
			AlarmSign:  testAlarmSign(),
			AlarmNo:    "ALARM-0001",
			Attachments: []AttachmentEntry{
				{FileName: item.name, FileSize: uint32(len(item.source))},
			},
		}))
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(cases))
	for _, item := range cases {
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			total := (len(item.source) + DefaultBlockSize - 1) / DefaultBlockSize
			for index := 0; index < total; index++ {
				if index == item.drop {
					continue
				}
				offset := index * DefaultBlockSize
				end := offset + DefaultBlockSize
				if end > len(item.source) {
					end = len(item.source)
				}
				if err := assembler.WriteChunk(phone, item.name, uint32(offset), item.source[offset:end]); err != nil {
					errCh <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}

	for _, item := range cases {
		result, err := assembler.CompleteCheck(phone, item.name)
		require.NoError(t, err)
		require.False(t, result.Complete)
		require.Len(t, result.Missing, 1)
		offset := item.drop * DefaultBlockSize
		length := DefaultBlockSize
		if offset+length > len(item.source) {
			length = len(item.source) - offset
		}
		require.Equal(t, uint32(offset), result.Missing[0].Offset)
		require.Equal(t, uint32(length), result.Missing[0].Length)
		require.NoError(t, assembler.WriteChunk(phone, item.name, uint32(offset), item.source[offset:offset+length]))
		result, err = assembler.CompleteCheck(phone, item.name)
		require.NoError(t, err)
		require.True(t, result.Complete)
		onDisk, err := os.ReadFile(filepath.Join(dir, phone, "20260107", "ALARM-0001", item.name))
		require.NoError(t, err)
		require.Equal(t, item.source, onDisk)
	}
}

func TestAssemblerCleanupStale(t *testing.T) {
	assembler, dir := newTestAssembler(t)
	phone := "013800000006"
	fileName := "stale.jpg"
	size := DefaultBlockSize
	source := buildSource(size)
	registerTestFile(t, assembler, phone, fileName, size)
	writeBlock(t, assembler, phone, fileName, source, 0)

	partPath := filepath.Join(dir, phone, "20260107", "ALARM-0001", fileName+partSuffix)
	_, err := os.Stat(partPath)
	require.NoError(t, err)

	removed := assembler.CleanupStale(time.Now().Add(2*time.Minute), time.Minute)
	require.Equal(t, 1, removed)
	_, partErr := os.Stat(partPath)
	require.True(t, os.IsNotExist(partErr))
	_, stateErr := os.Stat(partPath + stateSuffix)
	require.True(t, os.IsNotExist(stateErr))
	require.Nil(t, assembler.lookup(phone, fileName))
}

func TestAssemblerZeroSizeFileCompletes(t *testing.T) {
	assembler, dir := newTestAssembler(t)
	phone := "013800000007"
	fileName := "empty.txt"
	registerTestFile(t, assembler, phone, fileName, 0)

	result, err := assembler.CompleteCheck(phone, fileName)
	require.NoError(t, err)
	require.True(t, result.Complete)
	_, statErr := os.Stat(filepath.Join(dir, phone, "20260107", "ALARM-0001", fileName))
	require.NoError(t, statErr)
}

func TestAssemblerRejectsOversizeAndTraversal(t *testing.T) {
	assembler, _ := newTestAssembler(t)
	err := assembler.RegisterAlarm(AttachmentInfo{
		Phone:       "013800000008",
		AlarmSign:   testAlarmSign(),
		AlarmNo:     "ALARM-0001",
		Attachments: []AttachmentEntry{{FileName: "../evil.jpg", FileSize: 10}},
	})
	require.ErrorIs(t, err, ErrInvalidFileName)

	err = assembler.RegisterAlarm(AttachmentInfo{
		Phone:       "013800000008",
		AlarmSign:   testAlarmSign(),
		AlarmNo:     "ALARM-0001",
		Attachments: []AttachmentEntry{{FileName: "big.jpg", FileSize: uint32(assembler.Config().MaxFileSize + 1)}},
	})
	require.ErrorIs(t, err, ErrFileTooLarge)

	require.True(t, bytes.Equal(FrameMagic[:], []byte{'0', '1', 'c', 'd'}))
}

// 非 64KB 对齐、比块更小的碎片帧应立即计入覆盖区间。
func TestAssemblerFragmentRangesCoverImmediately(t *testing.T) {
	assembler, _ := newTestAssembler(t)
	phone := "013800000009"
	fileName := "fragment.jpg"
	size := 10000
	registerTestFile(t, assembler, phone, fileName, size)

	require.NoError(t, assembler.WriteChunk(phone, fileName, 0, make([]byte, 300)))
	require.NoError(t, assembler.WriteChunk(phone, fileName, 8000, make([]byte, 200)))
	require.NoError(t, assembler.WriteChunk(phone, fileName, 300, make([]byte, 200)))

	result, err := assembler.CompleteCheck(phone, fileName)
	require.NoError(t, err)
	require.False(t, result.Complete)
	require.Equal(t, []MissingRange{
		{Offset: 500, Length: 7500},
		{Offset: 8200, Length: 1800},
	}, result.Missing)

	// 重叠区间与重复区间只扩展并集，不重复计数。
	require.NoError(t, assembler.WriteChunk(phone, fileName, 400, make([]byte, 500)))
	state := assembler.lookup(phone, fileName)
	require.NotNil(t, state)
	state.mu.Lock()
	require.Equal(t, int64(900+200), state.covered)
	require.Len(t, state.ranges, 2)
	state.mu.Unlock()

	result, err = assembler.CompleteCheck(phone, fileName)
	require.NoError(t, err)
	require.Equal(t, []MissingRange{
		{Offset: 900, Length: 7100},
		{Offset: 8200, Length: 1800},
	}, result.Missing)
}

// 终端只按自己的小碎片补传时，多轮 0x1212 应逐步收敛到完全覆盖。
func TestAssemblerMultiRoundFragmentsConverge(t *testing.T) {
	assembler, dir := newTestAssembler(t)
	phone := "013800000010"
	fileName := "multi-round.bin"
	size := 9973
	source := buildSource(size)
	registerTestFile(t, assembler, phone, fileName, size)

	initial := []struct {
		offset int
		length int
	}{
		{offset: 0, length: 500},
		{offset: 1000, length: 700},
		{offset: 2500, length: 300},
		{offset: 8000, length: 400},
	}
	for _, item := range initial {
		require.NoError(t, assembler.WriteChunk(phone, fileName, uint32(item.offset), source[item.offset:item.offset+item.length]))
	}

	rounds := 0
	for {
		rounds++
		require.Less(t, rounds, 50, "retransmission did not converge")
		result, err := assembler.CompleteCheck(phone, fileName)
		require.NoError(t, err)
		if result.Complete {
			break
		}
		// 每轮只补每个缺口的前 333 字节，模拟终端坚持使用更小的自有帧边界。
		for _, missing := range result.Missing {
			length := int64(333)
			if length > int64(missing.Length) {
				length = int64(missing.Length)
			}
			offset := int(missing.Offset)
			require.NoError(t, assembler.WriteChunk(phone, fileName, missing.Offset, source[offset:offset+int(length)]))
		}
	}
	require.Greater(t, rounds, 1)

	onDisk, err := os.ReadFile(filepath.Join(dir, phone, "20260107", "ALARM-0001", fileName))
	require.NoError(t, err)
	require.Equal(t, source, onDisk)
}

// 首段缺口、尾段缺口、整文件单帧与越界帧的边界行为。
func TestAssemblerRangeBoundaries(t *testing.T) {
	assembler, _ := newTestAssembler(t)
	phone := "013800000011"
	fileName := "boundary.bin"
	size := 1000
	registerTestFile(t, assembler, phone, fileName, size)

	require.NoError(t, assembler.WriteChunk(phone, fileName, 400, make([]byte, 200)))
	result, err := assembler.CompleteCheck(phone, fileName)
	require.NoError(t, err)
	require.Equal(t, []MissingRange{
		{Offset: 0, Length: 400},
		{Offset: 600, Length: 400},
	}, result.Missing)

	require.NoError(t, assembler.WriteChunk(phone, fileName, 0, make([]byte, 400)))
	result, err = assembler.CompleteCheck(phone, fileName)
	require.NoError(t, err)
	require.Equal(t, []MissingRange{{Offset: 600, Length: 400}}, result.Missing)

	// 越界帧被拒绝，不改变已覆盖区间。
	require.ErrorIs(t, assembler.WriteChunk(phone, fileName, 900, make([]byte, 200)), ErrOffsetOutOfRange)
	result, err = assembler.CompleteCheck(phone, fileName)
	require.NoError(t, err)
	require.Equal(t, []MissingRange{{Offset: 600, Length: 400}}, result.Missing)

	require.NoError(t, assembler.WriteChunk(phone, fileName, 600, make([]byte, 400)))
	result, err = assembler.CompleteCheck(phone, fileName)
	require.NoError(t, err)
	require.True(t, result.Complete)

	// 单帧小文件。
	smallName := "small.bin"
	registerTestFile(t, assembler, phone, smallName, 10)
	require.NoError(t, assembler.WriteChunk(phone, smallName, 0, make([]byte, 10)))
	result, err = assembler.CompleteCheck(phone, smallName)
	require.NoError(t, err)
	require.True(t, result.Complete)
}

// completeTestFile 写入完整文件并触发完成收尾。
func completeTestFile(t *testing.T, assembler *Assembler, phone, fileName string, size int) {
	t.Helper()
	source := buildSource(size)
	registerTestFile(t, assembler, phone, fileName, size)
	for index := 0; index*DefaultBlockSize < size; index++ {
		writeBlock(t, assembler, phone, fileName, source, index)
	}
	result, err := assembler.CompleteCheck(phone, fileName)
	require.NoError(t, err)
	require.True(t, result.Complete)
}

// 完成态 TTL 到期只释放内存状态，落盘文件与 .meta.json 仍可查询。
func TestAssemblerCompletedTTLReleasesMemoryAndKeepsMetadata(t *testing.T) {
	assembler, _ := newTestAssembler(t)
	clock := time.Date(2026, 1, 7, 12, 0, 0, 0, time.UTC)
	assembler.now = func() time.Time { return clock }
	phone := "013800000012"
	fileName := "ttl.jpg"
	size := 2 * DefaultBlockSize
	completeTestFile(t, assembler, phone, fileName, size)

	// 未到 TTL 不淘汰。
	require.Equal(t, 0, assembler.CleanupCompleted(clock.Add(19*time.Minute), 20*time.Minute))
	require.NotNil(t, assembler.lookup(phone, fileName))

	// 到期释放内存。
	require.Equal(t, 1, assembler.CleanupCompleted(clock.Add(20*time.Minute), 20*time.Minute))
	require.Nil(t, assembler.lookup(phone, fileName))

	// GET /api/device/:phone/attachments 与单文件查询都走 Select/FindMetadata，
	// 淘汰后仍由 .meta.json 扫描兜底，行为保持一致。
	list := assembler.Select(phone)
	require.Len(t, list, 1)
	require.Equal(t, fileName, list[0].FileName)
	meta, err := assembler.FindMetadata(phone, fileName)
	require.NoError(t, err)
	require.Equal(t, list[0].Path, meta.Path)
	_, statErr := os.Stat(meta.Path)
	require.NoError(t, statErr)

	// 淘汰后再次收到 0x1212：RegisterFile 会从落盘文件重建完成态，结果仍为完成。
	info := AttachmentInfo{Phone: phone, TerminalID: "ABC1234", AlarmSign: testAlarmSign(), AlarmNo: "ALARM-0001"}
	_, err = assembler.RegisterFile(info, fileName, 0, uint32(size))
	require.NoError(t, err)
	result, err := assembler.CompleteCheck(phone, fileName)
	require.NoError(t, err)
	require.True(t, result.Complete)
}

// TTL 配置为 0 时不淘汰。
func TestAssemblerCompletedTTLDisabled(t *testing.T) {
	assembler, _ := newTestAssembler(t)
	clock := time.Date(2026, 1, 7, 12, 0, 0, 0, time.UTC)
	assembler.now = func() time.Time { return clock }
	phone := "013800000013"

	completeTestFile(t, assembler, phone, "keep.jpg", DefaultBlockSize)
	require.Equal(t, 0, assembler.CleanupCompleted(clock.Add(24*time.Hour), 0))
	require.NotNil(t, assembler.lookup(phone, "keep.jpg"))
}

// 未完成文件不受完成态 TTL 影响。
func TestAssemblerCompletedTTLDoesNotEvictIncomplete(t *testing.T) {
	assembler, _ := newTestAssembler(t)
	clock := time.Date(2026, 1, 7, 12, 0, 0, 0, time.UTC)
	assembler.now = func() time.Time { return clock }
	phone := "013800000014"

	incomplete := "pending.jpg"
	size := 2 * DefaultBlockSize
	source := buildSource(size)
	registerTestFile(t, assembler, phone, incomplete, size)
	writeBlock(t, assembler, phone, incomplete, source, 0)
	require.Equal(t, 0, assembler.CleanupCompleted(clock.Add(24*time.Hour), 20*time.Minute))
	require.NotNil(t, assembler.lookup(phone, incomplete))
}
