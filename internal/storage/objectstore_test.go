package storage

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
)

const fakeS3Bucket = "jt808-test"

// fakeS3Request 记录假服务器收到的原始请求，供断言请求路径/头/体。
type fakeS3Request struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   []byte
}

type fakeS3Object struct {
	Data         []byte
	ETag         string
	Meta         map[string]string
	ContentType  string
	LastModified time.Time
}

// fakeS3 是最小 S3 兼容服务，按方法 + 查询参数严格分派；未知请求返回 400。
type fakeS3 struct {
	bucket   string
	server   *httptest.Server
	mu       sync.Mutex
	objects  map[string]*fakeS3Object
	parts    map[string]map[int][]byte
	uploads  int
	requests []fakeS3Request
}

func newFakeS3(t *testing.T) *fakeS3 {
	t.Helper()
	fake := &fakeS3{
		bucket:  fakeS3Bucket,
		objects: make(map[string]*fakeS3Object),
		parts:   make(map[string]map[int][]byte),
	}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handle))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeS3) config() ObjectStoreConfig {
	return ObjectStoreConfig{
		Endpoint:   f.server.URL,
		Region:     "us-east-1",
		Bucket:     f.bucket,
		AccessKey:  "test-access",
		SecretKey:  "test-secret",
		PathStyle:  true,
		HTTPClient: f.server.Client(),
	}
}

func (f *fakeS3) newStore(t *testing.T) *ObjectStore {
	t.Helper()
	store, err := NewObjectStore(f.config())
	require.NoError(t, err)
	return store
}

func (f *fakeS3) handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		f.reject(w, "read body: %v", err)
		return
	}
	f.mu.Lock()
	f.requests = append(f.requests, fakeS3Request{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.Query(),
		Header: r.Header.Clone(),
		Body:   append([]byte(nil), body...),
	})
	f.mu.Unlock()

	key, ok := strings.CutPrefix(r.URL.Path, "/"+f.bucket)
	if !ok {
		f.reject(w, "unexpected path %q", r.URL.Path)
		return
	}
	key = strings.TrimPrefix(key, "/")
	if r.Method == http.MethodGet && key == "" && r.URL.Query().Has("list-type") {
		f.handleList(w, r)
		return
	}
	if key == "" {
		f.reject(w, "unexpected path %q", r.URL.Path)
		return
	}
	switch r.Method {
	case http.MethodPut:
		f.handlePut(w, r, key, body)
	case http.MethodPost:
		f.handlePost(w, r, key, body)
	case http.MethodGet:
		f.handleGet(w, r, key)
	case http.MethodHead:
		f.handleHead(w, key)
	case http.MethodDelete:
		f.handleDelete(w, key)
	default:
		f.reject(w, "unexpected method %s", r.Method)
	}
}

func (f *fakeS3) handlePut(w http.ResponseWriter, r *http.Request, key string, body []byte) {
	query := r.URL.Query()
	if _, ok := query["partNumber"]; ok {
		f.handleUploadPart(w, query, body)
		return
	}
	if source := r.Header.Get("x-amz-copy-source"); source != "" {
		f.handleCopy(w, r, source, key)
		return
	}
	etag := `"` + fakeHexMD5(body) + `"`
	f.mu.Lock()
	f.objects[key] = &fakeS3Object{
		Data:         append([]byte(nil), body...),
		ETag:         etag,
		Meta:         fakeMetaFromHeader(r.Header),
		ContentType:  r.Header.Get("Content-Type"),
		LastModified: time.Now().UTC(),
	}
	f.mu.Unlock()
	w.Header().Set("ETag", etag)
	w.WriteHeader(http.StatusOK)
}

func (f *fakeS3) handleUploadPart(w http.ResponseWriter, query url.Values, body []byte) {
	uploadID := query.Get("uploadId")
	number, err := strconv.Atoi(query.Get("partNumber"))
	if uploadID == "" || err != nil || number <= 0 {
		f.reject(w, "bad upload part query %q", query.Encode())
		return
	}
	f.mu.Lock()
	parts, ok := f.parts[uploadID]
	if !ok {
		f.mu.Unlock()
		f.reject(w, "unknown upload id %q", uploadID)
		return
	}
	parts[number] = append([]byte(nil), body...)
	f.mu.Unlock()
	w.Header().Set("ETag", `"part-`+strconv.Itoa(number)+`"`)
	w.WriteHeader(http.StatusOK)
}

func (f *fakeS3) handlePost(w http.ResponseWriter, r *http.Request, key string, body []byte) {
	query := r.URL.Query()
	if _, ok := query["uploads"]; ok {
		f.mu.Lock()
		f.uploads++
		uploadID := fmt.Sprintf("upload-%d", f.uploads)
		f.parts[uploadID] = make(map[int][]byte)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, `<InitiateMultipartUploadResult><Bucket>%s</Bucket><Key>%s</Key><UploadId>%s</UploadId></InitiateMultipartUploadResult>`,
			f.bucket, key, uploadID)
		return
	}
	if uploadID := query.Get("uploadId"); uploadID != "" {
		f.handleCompleteUpload(w, key, uploadID, body)
		return
	}
	f.reject(w, "unexpected POST query %q", query.Encode())
}

func (f *fakeS3) handleCompleteUpload(w http.ResponseWriter, key, uploadID string, body []byte) {
	var request struct {
		Parts []struct {
			PartNumber int    `xml:"PartNumber"`
			ETag       string `xml:"ETag"`
		} `xml:"Part"`
	}
	if err := xml.Unmarshal(body, &request); err != nil {
		f.reject(w, "decode complete request: %v", err)
		return
	}
	f.mu.Lock()
	parts, ok := f.parts[uploadID]
	if !ok {
		f.mu.Unlock()
		f.reject(w, "unknown upload id %q", uploadID)
		return
	}
	var assembled []byte
	for _, part := range request.Parts {
		assembled = append(assembled, parts[part.PartNumber]...)
	}
	delete(f.parts, uploadID)
	etag := `"` + fakeHexMD5(assembled) + `"`
	f.objects[key] = &fakeS3Object{Data: assembled, ETag: etag, Meta: map[string]string{}, LastModified: time.Now().UTC()}
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/xml")
	fmt.Fprintf(w, `<CompleteMultipartUploadResult><ETag>%s</ETag></CompleteMultipartUploadResult>`, etag)
}

func (f *fakeS3) handleCopy(w http.ResponseWriter, r *http.Request, source, target string) {
	decoded, err := url.PathUnescape(source)
	if err != nil {
		f.reject(w, "decode copy source %q: %v", source, err)
		return
	}
	sourceKey, ok := strings.CutPrefix(decoded, "/"+f.bucket+"/")
	if !ok {
		f.reject(w, "unexpected copy source %q", source)
		return
	}
	f.mu.Lock()
	sourceObject, ok := f.objects[sourceKey]
	if !ok {
		f.mu.Unlock()
		f.writeError(w, http.StatusNotFound, "NoSuchKey")
		return
	}
	copied := &fakeS3Object{
		Data:         append([]byte(nil), sourceObject.Data...),
		ETag:         sourceObject.ETag,
		Meta:         make(map[string]string),
		ContentType:  sourceObject.ContentType,
		LastModified: time.Now().UTC(),
	}
	if strings.EqualFold(r.Header.Get("x-amz-metadata-directive"), "REPLACE") {
		copied.Meta = fakeMetaFromHeader(r.Header)
		if contentType := r.Header.Get("Content-Type"); contentType != "" {
			copied.ContentType = contentType
		}
	} else {
		for name, value := range sourceObject.Meta {
			copied.Meta[name] = value
		}
	}
	f.objects[target] = copied
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/xml")
	fmt.Fprintf(w, `<CopyObjectResult><ETag>%s</ETag></CopyObjectResult>`, copied.ETag)
}

func (f *fakeS3) handleGet(w http.ResponseWriter, r *http.Request, key string) {
	f.mu.Lock()
	object, ok := f.objects[key]
	f.mu.Unlock()
	if !ok {
		f.writeError(w, http.StatusNotFound, "NoSuchKey")
		return
	}
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		start, end, err := fakeParseRange(rangeHeader, int64(len(object.Data)))
		if err != nil {
			f.reject(w, "%v", err)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end-1, len(object.Data)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(object.Data[start:end])
		return
	}
	w.Header().Set("ETag", object.ETag)
	w.Header().Set("Content-Length", strconv.Itoa(len(object.Data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(object.Data)
}

func (f *fakeS3) handleHead(w http.ResponseWriter, key string) {
	f.mu.Lock()
	object, ok := f.objects[key]
	f.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("ETag", object.ETag)
	w.Header().Set("Content-Length", strconv.Itoa(len(object.Data)))
	if object.ContentType != "" {
		w.Header().Set("Content-Type", object.ContentType)
	}
	for name, value := range object.Meta {
		w.Header().Set("x-amz-meta-"+name, value)
	}
	w.WriteHeader(http.StatusOK)
}

func (f *fakeS3) handleDelete(w http.ResponseWriter, key string) {
	f.mu.Lock()
	delete(f.objects, key)
	f.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeS3) reject(w http.ResponseWriter, format string, args ...any) {
	f.writeError(w, http.StatusBadRequest, "FakeS3UnexpectedRequest: "+fmt.Sprintf(format, args...))
}

func (f *fakeS3) writeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<Error><Code>%s</Code></Error>`, code)
}

func (f *fakeS3) seed(key string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = &fakeS3Object{
		Data:         append([]byte(nil), data...),
		ETag:         `"` + fakeHexMD5(data) + `"`,
		Meta:         map[string]string{"jt808-state": "committed"},
		LastModified: time.Now().UTC(),
	}
}

func (f *fakeS3) recorded() []fakeS3Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeS3Request(nil), f.requests...)
}

func (f *fakeS3) objectData(t *testing.T, key string) []byte {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	object, ok := f.objects[key]
	require.True(t, ok, "object %q was not stored", key)
	return append([]byte(nil), object.Data...)
}

// copyRequests 返回打到指定 key 的 CopyObject 请求。
func (f *fakeS3) copyRequests(key string) []fakeS3Request {
	var copies []fakeS3Request
	for _, request := range f.recorded() {
		if request.Method == http.MethodPut && request.Path == "/"+f.bucket+"/"+key &&
			request.Header.Get("x-amz-copy-source") != "" {
			copies = append(copies, request)
		}
	}
	return copies
}

// getRequests 返回打到指定 key 的 GET 请求。
func (f *fakeS3) getRequests(key string) []fakeS3Request {
	var gets []fakeS3Request
	for _, request := range f.recorded() {
		if request.Method == http.MethodGet && request.Path == "/"+f.bucket+"/"+key {
			gets = append(gets, request)
		}
	}
	return gets
}

// handleList 实现 ListObjectsV2，仅返回前缀匹配的 Contents。
func (f *fakeS3) handleList(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("prefix")
	type content struct {
		Key          string `xml:"Key"`
		Size         int64  `xml:"Size"`
		LastModified string `xml:"LastModified"`
	}
	type result struct {
		XMLName     xml.Name  `xml:"ListBucketResult"`
		IsTruncated bool      `xml:"IsTruncated"`
		Contents    []content `xml:"Contents"`
	}
	f.mu.Lock()
	keys := make([]string, 0, len(f.objects))
	for key := range f.objects {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	listed := result{IsTruncated: false}
	for _, key := range keys {
		object := f.objects[key]
		listed.Contents = append(listed.Contents, content{
			Key:          key,
			Size:         int64(len(object.Data)),
			LastModified: object.LastModified.Format("2006-01-02T15:04:05.000Z"),
		})
	}
	f.mu.Unlock()
	payload, err := xml.Marshal(listed)
	if err != nil {
		f.reject(w, "marshal list: %v", err)
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func (f *fakeS3) hasObject(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.objects[key]
	return ok
}

func (f *fakeS3) setLastModified(t *testing.T, key string, modified time.Time) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	object, ok := f.objects[key]
	require.True(t, ok, "object %q was not stored", key)
	object.LastModified = modified.UTC()
}

func fakeMetaFromHeader(header http.Header) map[string]string {
	meta := make(map[string]string)
	for name, values := range header {
		lower := strings.ToLower(name)
		if !strings.HasPrefix(lower, "x-amz-meta-") || len(values) == 0 {
			continue
		}
		meta[strings.TrimPrefix(lower, "x-amz-meta-")] = values[0]
	}
	return meta
}

// fakeParseRange 解析 "bytes=start-end"/"bytes=start-"，返回左闭右开区间。
func fakeParseRange(header string, size int64) (int64, int64, error) {
	value, ok := strings.CutPrefix(header, "bytes=")
	if !ok {
		return 0, 0, fmt.Errorf("unexpected range header %q", header)
	}
	startText, endText, ok := strings.Cut(value, "-")
	if !ok || startText == "" {
		return 0, 0, fmt.Errorf("unexpected range header %q", header)
	}
	start, err := strconv.ParseInt(startText, 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, fmt.Errorf("unsupported range header %q", header)
	}
	end := size
	if endText != "" {
		parsed, err := strconv.ParseInt(endText, 10, 64)
		if err != nil || parsed < start {
			return 0, 0, fmt.Errorf("unsupported range header %q", header)
		}
		if parsed+1 < end {
			end = parsed + 1
		}
	}
	return start, end, nil
}

func fakeHexMD5(data []byte) string {
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])
}

// Put 只写 _staging/<txid>/<key>，返回 pending 状态与正确的 MD5/大小。
func TestObjectStorePutWritesStagingObject(t *testing.T) {
	fake := newFakeS3(t)
	store := fake.newStore(t)
	ctx := context.Background()

	content := []byte("jpeg-bytes-for-staging")
	info, err := store.Put(ctx, "188765432105/20261112/1.jpg", bytes.NewReader(content), contracts.BlobOptions{
		ContentType: "image/jpeg",
		Metadata:    map[string]string{"Device": "188765432105"},
	})
	require.NoError(t, err)

	requests := fake.recorded()
	require.Len(t, requests, 2, "Put must upload staging object then pending marker")
	put := requests[0]
	require.Equal(t, http.MethodPut, put.Method)

	marker := requests[1]
	require.Equal(t, http.MethodPut, marker.Method)
	require.Equal(t, "/"+fakeS3Bucket+"/_pending/188765432105/20261112/1.jpg", marker.Path,
		"pending marker must live under the listable _pending/ prefix")
	require.Equal(t, "application/json", marker.Header.Get("Content-Type"))
	require.Contains(t, string(marker.Body), `"key":"188765432105/20261112/1.jpg"`)
	require.Contains(t, string(marker.Body), `"stagingKey":"`+info.StagingKey+`"`)

	sum := md5.Sum(content)
	require.True(t, strings.HasPrefix(info.StagingKey, objectStoreStagingPrefix),
		"staging key %q must use the staging prefix", info.StagingKey)
	require.Equal(t, "_staging/", info.StagingKey[:len("_staging/")])
	require.True(t, strings.HasSuffix(info.StagingKey, "/188765432105/20261112/1.jpg"))
	require.Equal(t, "/"+fakeS3Bucket+"/"+info.StagingKey, put.Path)
	require.Equal(t, "188765432105/20261112/1.jpg", info.Key)
	require.Equal(t, contracts.BlobStatePending, info.State)
	require.Equal(t, int64(len(content)), info.Size)
	require.Equal(t, hex.EncodeToString(sum[:]), info.MD5)
	require.Equal(t, hex.EncodeToString(sum[:]), info.ETag)
	require.Equal(t, content, put.Body)
	require.Equal(t, base64.StdEncoding.EncodeToString(sum[:]), put.Header.Get("Content-MD5"))
	require.Equal(t, "image/jpeg", put.Header.Get("Content-Type"))
	require.Equal(t, "188765432105", put.Header.Get("x-amz-meta-device"))
}

// Promote 用服务端复制把暂存对象搬到最终 key 并写 pending；MarkCommitted 再复制一次改为 committed。
func TestObjectStorePromoteAndMarkCommitted(t *testing.T) {
	fake := newFakeS3(t)
	store := fake.newStore(t)
	ctx := context.Background()

	key := "188765432105/20261112/clip.mp4"
	content := []byte("video-bytes")
	info, err := store.Put(ctx, key, bytes.NewReader(content), contracts.BlobOptions{ContentType: "video/mp4"})
	require.NoError(t, err)

	promoted, err := store.Promote(ctx, info)
	require.NoError(t, err)
	require.Equal(t, key, promoted.Key)
	require.Equal(t, contracts.BlobStatePending, promoted.State)

	copies := fake.copyRequests(key)
	require.Len(t, copies, 1)
	promoteCopy := copies[0]
	require.Equal(t, "/"+fakeS3Bucket+"/"+info.StagingKey, promoteCopy.Header.Get("x-amz-copy-source"))
	require.Equal(t, "pending", promoteCopy.Header.Get("x-amz-meta-jt808-state"))
	require.Equal(t, content, fake.objectData(t, key), "final key must carry the uploaded bytes")
	require.True(t, fake.hasObject("_pending/"+key), "promote must leave a listable pending marker")

	require.NoError(t, store.MarkCommitted(ctx, key))
	copies = fake.copyRequests(key)
	require.Len(t, copies, 2)
	commitCopy := copies[1]
	require.Equal(t, "/"+fakeS3Bucket+"/"+key, commitCopy.Header.Get("x-amz-copy-source"))
	require.Equal(t, "committed", commitCopy.Header.Get("x-amz-meta-jt808-state"))
	require.False(t, fake.hasObject("_pending/"+key), "committed object must not keep a pending marker")

	stat, err := store.Stat(ctx, key)
	require.NoError(t, err)
	require.Equal(t, key, stat.Key)
	require.Equal(t, contracts.BlobStateCommitted, stat.State)
	require.Equal(t, int64(len(content)), stat.Size)
	require.Equal(t, promoted.ETag, stat.ETag)
}

// OpenRange 按 offset/length 生成 Range 头，且只返回该区间。
func TestObjectStoreOpenRangeSendsRangeHeader(t *testing.T) {
	fake := newFakeS3(t)
	store := fake.newStore(t)
	ctx := context.Background()

	key := "media/clip.bin"
	content := make([]byte, 64)
	for i := range content {
		content[i] = byte('a' + i%26)
	}
	fake.seed(key, content)

	reader, err := store.OpenRange(ctx, key, 10, 10)
	require.NoError(t, err)
	chunk, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.Equal(t, content[10:20], chunk)

	gets := fake.getRequests(key)
	require.Len(t, gets, 1)
	require.Equal(t, "bytes=10-19", gets[0].Header.Get("Range"))

	reader, err = store.OpenRange(ctx, key, 10, 0)
	require.NoError(t, err)
	tail, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.Equal(t, content[10:], tail)

	gets = fake.getRequests(key)
	require.Len(t, gets, 2)
	require.Equal(t, "bytes=10-", gets[1].Header.Get("Range"))
}

// putMultipart 依次发起 initiate、upload part、complete，并返回 complete 的 ETag。
func TestObjectStorePutMultipart(t *testing.T) {
	fake := newFakeS3(t)
	store := fake.newStore(t)
	ctx := context.Background()

	key := "media/multi.bin"
	content := bytes.Repeat([]byte("0123456789abcdef"), 64) // 1024 字节，单分片
	sum := md5.Sum(content)
	headers := http.Header{}
	headers.Set("Content-Type", "application/octet-stream")
	headers.Set("Content-MD5", base64.StdEncoding.EncodeToString(sum[:]))

	etag, err := store.putMultipart(ctx, key, bytes.NewReader(content), int64(len(content)), headers)
	require.NoError(t, err)
	require.Equal(t, `"`+fakeHexMD5(content)+`"`, etag, "ETag must come from the complete response")

	requests := fake.recorded()
	require.Len(t, requests, 3, "expected initiate + upload part + complete exactly")

	initiate := requests[0]
	require.Equal(t, http.MethodPost, initiate.Method)
	require.Equal(t, "/"+fakeS3Bucket+"/"+key, initiate.Path)
	require.True(t, initiate.Query.Has("uploads"), "initiate must use ?uploads, got %q", initiate.Query.Encode())
	require.False(t, initiate.Query.Has("uploadId"))

	part := requests[1]
	require.Equal(t, http.MethodPut, part.Method)
	require.Equal(t, "/"+fakeS3Bucket+"/"+key, part.Path)
	require.Equal(t, "1", part.Query.Get("partNumber"))
	require.Equal(t, "upload-1", part.Query.Get("uploadId"))
	require.Equal(t, content, part.Body)

	complete := requests[2]
	require.Equal(t, http.MethodPost, complete.Method)
	require.Equal(t, "/"+fakeS3Bucket+"/"+key, complete.Path)
	require.False(t, complete.Query.Has("partNumber"))
	require.Equal(t, "upload-1", complete.Query.Get("uploadId"))
	require.Contains(t, string(complete.Body), "<PartNumber>1</PartNumber>")

	require.Equal(t, content, fake.objectData(t, key), "completed object must equal uploaded bytes")
}

// Promote 成功但业务表未写入：宽限期内不回收，超期后回收正式对象、暂存对象与标记。
func TestObjectStoreSweepReclaimsExpiredPendingWithoutBusinessRow(t *testing.T) {
	fake := newFakeS3(t)
	store := fake.newStore(t)
	ctx := context.Background()

	key := "recordings/13800138000/1_1700000000000.ts"
	content := []byte("segment-bytes")
	info, err := store.Put(ctx, key, bytes.NewReader(content), contracts.BlobOptions{ContentType: "video/mp2t"})
	require.NoError(t, err)
	_, err = store.Promote(ctx, info)
	require.NoError(t, err)
	require.True(t, fake.hasObject(key))
	require.True(t, fake.hasObject("_pending/"+key))

	// 标记只有 30 分钟，宽限期 1 小时：不回收。
	fake.setLastModified(t, "_pending/"+key, time.Now().Add(-30*time.Minute))
	store.SweepPending(ctx, time.Hour, func(string) bool { return false })
	require.True(t, fake.hasObject(key), "fresh pending object must not be reclaimed")
	require.True(t, fake.hasObject("_pending/"+key))

	// 标记超过 2 小时且业务侧查不到：对象、暂存对象、标记都应清掉。
	fake.setLastModified(t, "_pending/"+key, time.Now().Add(-2*time.Hour))
	store.SweepPending(ctx, time.Hour, func(string) bool { return false })
	require.False(t, fake.hasObject(key), "expired pending object must be reclaimed")
	require.False(t, fake.hasObject(info.StagingKey), "staging object must be reclaimed")
	require.False(t, fake.hasObject("_pending/"+key), "marker must be removed after reclaim")
}

// 业务行存在时只清标记，不删对象。
func TestObjectStoreSweepRemovesStaleMarkerButKeepsReferencedObject(t *testing.T) {
	fake := newFakeS3(t)
	store := fake.newStore(t)
	ctx := context.Background()

	key := "media/188765432105/20261112/1.jpg"
	info, err := store.Put(ctx, key, bytes.NewReader([]byte("jpeg")), contracts.BlobOptions{ContentType: "image/jpeg"})
	require.NoError(t, err)
	_, err = store.Promote(ctx, info)
	require.NoError(t, err)
	fake.setLastModified(t, "_pending/"+key, time.Now().Add(-2*time.Hour))

	store.SweepPending(ctx, time.Hour, func(candidate string) bool { return candidate == key })
	require.True(t, fake.hasObject(key), "referenced object must be kept")
	require.False(t, fake.hasObject("_pending/"+key), "stale marker for referenced object must be removed")
}

// 标记误留但对象已 committed：不能当孤儿删除。
func TestObjectStoreSweepKeepsCommittedObjectWithStaleMarker(t *testing.T) {
	fake := newFakeS3(t)
	store := fake.newStore(t)
	ctx := context.Background()

	key := "recordings/13800138000/1_1700000000002.ts"
	info, err := store.Put(ctx, key, bytes.NewReader([]byte("committed-segment")), contracts.BlobOptions{})
	require.NoError(t, err)
	_, err = store.Promote(ctx, info)
	require.NoError(t, err)
	require.NoError(t, store.MarkCommitted(ctx, key))

	// 模拟 MarkCommitted 成功后标记删除失败。
	require.NoError(t, store.writePendingMarker(ctx, info))
	fake.setLastModified(t, "_pending/"+key, time.Now().Add(-2*time.Hour))

	store.SweepPending(ctx, time.Hour, func(string) bool { return false })
	require.True(t, fake.hasObject(key), "committed object must never be reclaimed by pending sweep")
	require.False(t, fake.hasObject("_pending/"+key), "stale marker must still be cleaned")
}

// Delete 同步删除标记，不留悬空标记。
func TestObjectStoreDeleteRemovesPendingMarker(t *testing.T) {
	fake := newFakeS3(t)
	store := fake.newStore(t)
	ctx := context.Background()

	key := "recordings/13800138000/1_1700000000001.ts"
	info, err := store.Put(ctx, key, bytes.NewReader([]byte("segment")), contracts.BlobOptions{})
	require.NoError(t, err)
	_, err = store.Promote(ctx, info)
	require.NoError(t, err)

	require.NoError(t, store.Delete(ctx, key))
	require.False(t, fake.hasObject(key))
	require.False(t, fake.hasObject("_pending/"+key))

	// 标记已删，扫描不应再报出该 key。
	markers, err := store.PendingBefore(ctx, 0)
	require.NoError(t, err)
	require.NotContains(t, markers, key)
}
