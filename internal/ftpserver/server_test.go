package ftpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

type fakeBlob struct {
	mu        sync.Mutex
	staging   map[string][]byte
	objects   map[string][]byte
	committed map[string]bool
}

func newFakeBlob() *fakeBlob {
	return &fakeBlob{
		staging: map[string][]byte{}, objects: map[string][]byte{}, committed: map[string]bool{},
	}
}

func (b *fakeBlob) Put(_ context.Context, key string, r io.Reader, _ contracts.BlobOptions) (contracts.BlobInfo, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return contracts.BlobInfo{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.staging[key] = data
	return contracts.BlobInfo{Key: key, StagingKey: key, Size: int64(len(data))}, nil
}

func (b *fakeBlob) Promote(_ context.Context, info contracts.BlobInfo) (contracts.BlobInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, ok := b.staging[info.Key]
	if !ok {
		return contracts.BlobInfo{}, fmt.Errorf("staging object %s not found", info.Key)
	}
	b.objects[info.Key] = data
	delete(b.staging, info.Key)
	return info, nil
}

func (b *fakeBlob) MarkCommitted(_ context.Context, key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.committed[key] = true
	return nil
}

func (b *fakeBlob) Open(_ context.Context, key string) (io.ReadCloser, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, ok := b.objects[key]
	if !ok {
		return nil, fmt.Errorf("object %s not found", key)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (b *fakeBlob) OpenRange(ctx context.Context, key string, _, _ int64) (io.ReadCloser, error) {
	return b.Open(ctx, key)
}

func (b *fakeBlob) Delete(_ context.Context, key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.objects, key)
	delete(b.staging, key)
	return nil
}

func (b *fakeBlob) Stat(_ context.Context, key string) (contracts.BlobInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, ok := b.objects[key]
	if !ok {
		return contracts.BlobInfo{}, fmt.Errorf("object %s not found", key)
	}
	return contracts.BlobInfo{Key: key, Size: int64(len(data))}, nil
}

func (b *fakeBlob) ListObjects(_ context.Context, prefix string) ([]contracts.BlobObject, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]contracts.BlobObject, 0)
	for key, data := range b.objects {
		if strings.HasPrefix(key, prefix) {
			out = append(out, contracts.BlobObject{Key: key, Size: int64(len(data)), LastModified: time.Now()})
		}
	}
	return out, nil
}

type fakeControl struct {
	mu       sync.Mutex
	progress []contracts.AttachmentProgress
	reject   bool
	down     bool
}

func (c *fakeControl) Login(_ context.Context, _, _, _ string) (LoginResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.down {
		return LoginResult{}, fmt.Errorf("control unreachable")
	}
	if c.reject {
		return LoginResult{}, ErrLoginRejected
	}
	return LoginResult{Phone: "13800138000", Serial: 7, UploadPath: "/"}, nil
}

func (c *fakeControl) Report(_ context.Context, progress contracts.AttachmentProgress) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.progress = append(c.progress, progress)
	return nil
}

func (c *fakeControl) lastProgress() (contracts.AttachmentProgress, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.progress) == 0 {
		return contracts.AttachmentProgress{}, false
	}
	return c.progress[len(c.progress)-1], true
}

func startTestServer(t *testing.T) (*Server, *fakeBlob, *fakeControl, string) {
	t.Helper()
	blob := newFakeBlob()
	control := &fakeControl{}
	server, err := New(Config{
		Dir: t.TempDir(), ControlPort: 0, PassivePorts: freePorts(t, 4),
		AdvertiseIP: "127.0.0.1", NodeID: "node-1", SegmentSize: 4,
		Blob: blob, Control: control,
	})
	if err != nil {
		t.Fatalf("new ftp server: %v", err)
	}
	if err := server.Listen("127.0.0.1:0"); err != nil {
		t.Fatalf("listen: %v", err)
	}
	go server.Start()
	t.Cleanup(server.Stop)
	return server, blob, control, server.Addr()
}

func freePorts(t *testing.T, count int) []int {
	t.Helper()
	ports := make([]int, 0, count)
	for len(ports) < count {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("reserve port: %v", err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()
		ports = append(ports, port)
	}
	return ports
}

type ftpClient struct {
	conn net.Conn
	rd   *bufio.Reader
	t    *testing.T
}

func dialFTP(t *testing.T, addr string) *ftpClient {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial ftp: %v", err)
	}
	client := &ftpClient{conn: conn, rd: bufio.NewReader(conn), t: t}
	t.Cleanup(func() { _ = conn.Close() })
	if code := client.readCode(); code != "220" {
		t.Fatalf("unexpected greeting %q", code)
	}
	return client
}

func (c *ftpClient) send(line string) {
	c.t.Helper()
	if _, err := io.WriteString(c.conn, line+"\r\n"); err != nil {
		c.t.Fatalf("send %q: %v", line, err)
	}
}

func (c *ftpClient) readCode() string {
	c.t.Helper()
	line, err := c.rd.ReadString('\n')
	if err != nil {
		c.t.Fatalf("read reply: %v", err)
	}
	return strings.TrimSpace(line)[:3]
}

func (c *ftpClient) reply() string {
	c.t.Helper()
	line, err := c.rd.ReadString('\n')
	if err != nil {
		c.t.Fatalf("read reply: %v", err)
	}
	return strings.TrimSpace(line)
}

func (c *ftpClient) login() {
	c.t.Helper()
	c.send("USER ftp1078")
	if code := c.readCode(); code != "331" {
		c.t.Fatalf("USER got %s", code)
	}
	c.send("PASS secret")
	if code := c.readCode(); code != "230" {
		c.t.Fatalf("PASS got %s", code)
	}
}

func (c *ftpClient) pasv() net.Conn {
	c.t.Helper()
	c.send("PASV")
	line := c.reply()
	if !strings.HasPrefix(line, "227") {
		c.t.Fatalf("PASV got %q", line)
	}
	start := strings.Index(line, "(")
	end := strings.Index(line, ")")
	parts := strings.Split(line[start+1:end], ",")
	if len(parts) != 6 {
		c.t.Fatalf("PASV parse %q", line)
	}
	p1, _ := strconv.Atoi(parts[4])
	p2, _ := strconv.Atoi(parts[5])
	addr := fmt.Sprintf("%s.%s.%s.%s:%d", parts[0], parts[1], parts[2], parts[3], p1*256+p2)
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		c.t.Fatalf("dial data: %v", err)
	}
	return conn
}

func TestFTPUploadWritesSegmentsAndManifest(t *testing.T) {
	_, blob, control, addr := startTestServer(t)
	client := dialFTP(t, addr)
	client.login()
	data := client.pasv()
	client.send("STOR /nested/alarm.mp4")
	if code := client.readCode(); code != "150" {
		t.Fatalf("STOR got %s", code)
	}
	payload := []byte("0123456789")
	if _, err := data.Write(payload); err != nil {
		t.Fatalf("write data: %v", err)
	}
	_ = data.Close()
	if code := client.readCode(); code != "226" {
		t.Fatalf("upload completion got %s", code)
	}

	blob.mu.Lock()
	defer blob.mu.Unlock()
	for index, size := range []int64{4, 4, 2} {
		key := storage.AttachmentFTPSegmentObjectKey("13800138000", "7", "alarm.mp4",
			int64(index)*4, size, index)
		obj, ok := blob.objects[key]
		if !ok {
			t.Fatalf("segment %s missing", key)
		}
		if len(obj) != int(size) {
			t.Fatalf("segment %s size=%d want %d", key, len(obj), size)
		}
		if !blob.committed[key] {
			t.Fatalf("segment %s not marked committed", key)
		}
	}
	doneKey := storage.AttachmentDoneObjectKey(storage.SourceFTP1078, "13800138000", "7", "alarm.mp4")
	if _, ok := blob.objects[doneKey]; !ok {
		t.Fatalf("manifest %s missing", doneKey)
	}
	var manifest storage.AttachmentDoneManifest
	if err := json.Unmarshal(blob.objects[doneKey], &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.TotalSize != 10 || manifest.Segments != 3 {
		t.Fatalf("unexpected manifest %+v", manifest)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if progress, ok := control.lastProgress(); ok && progress.Status == contracts.AttachmentStatusCompleted {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("completed progress was not reported")
}

func TestFTPRejectsPORTAndLoginWhenControlDown(t *testing.T) {
	_, _, control, addr := startTestServer(t)
	client := dialFTP(t, addr)
	client.login()
	client.send("PORT 127,0,0,1,10,10")
	if code := client.readCode(); code != "502" {
		t.Fatalf("PORT got %s", code)
	}

	control.mu.Lock()
	control.down = true
	control.mu.Unlock()
	second := dialFTP(t, addr)
	second.send("USER ftp1078")
	second.readCode()
	second.send("PASS secret")
	if code := second.readCode(); code != "421" {
		t.Fatalf("login with control down got %s", code)
	}
}

func TestUploadPauseResumeAndCancel(t *testing.T) {
	blob := newFakeBlob()
	control := &fakeControl{}
	server, err := New(Config{Dir: t.TempDir(), NodeID: "n", SegmentSize: 4, Blob: blob, Control: control})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	t.Cleanup(server.Stop)
	up := server.newUpload(LoginResult{Phone: "13800138000", Serial: 9}, "a.bin")
	ctx := context.Background()
	if err := up.open(ctx, 0, true); err != nil {
		t.Fatalf("open: %v", err)
	}
	up.pause()
	done := make(chan error, 1)
	go func() { done <- up.Write(ctx, []byte("abcd")) }()
	select {
	case err := <-done:
		t.Fatalf("write returned while paused: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	up.resume()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("write after resume: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("write did not resume")
	}
	up.cancel()
	if err := up.waitIfPaused(); err != ErrUploadCancelled {
		t.Fatalf("cancel not visible: %v", err)
	}
	up.abort(ctx)
	if up.total != 4 {
		t.Fatalf("unexpected total %d", up.total)
	}
}

func TestApplyControlDeduplicatesSeq(t *testing.T) {
	blob := newFakeBlob()
	server, err := New(Config{Dir: t.TempDir(), NodeID: "n", SegmentSize: 4, Blob: blob, Control: &fakeControl{}})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	t.Cleanup(server.Stop)
	if !server.ApplyControl(ControlCommand{Phone: "1", Serial: 2, Action: ControlPause, Seq: 5}) {
		t.Fatal("first command should apply")
	}
	if server.ApplyControl(ControlCommand{Phone: "1", Serial: 2, Action: ControlResume, Seq: 5}) {
		t.Fatal("duplicate seq must be ignored")
	}
}
