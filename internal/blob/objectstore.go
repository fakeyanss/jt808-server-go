// S3 兼容对象存储后端。
//
// 设计边界：
//   - 只依赖标准库，按 S3 REST API 直接实现 SigV4、multipart 与区间读取，
//     不绑定任何厂商 SDK；
//   - Put 写入 `_staging/<txid>/<key>`，Promote 用服务端复制把对象提级到
//     最终 key，并写 `x-amz-meta-jt808-state=pending`；
//   - MarkCommitted 只改对象元数据，不移动数据；
//   - 应用侧不做远端全量遍历，孤儿对象交给桶生命周期；这里只提供
//     `SweepPending` 作为生命周期未配置时的兜底。
package blob

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
)

const (
	// objectStoreStagingPrefix 是暂存对象前缀，与最终 key 隔离。
	objectStoreStagingPrefix = "_staging/"
	// objectStoreStateMeta 是对象状态元数据键。
	objectStoreStateMeta = "jt808-state"
	// objectStorePartSize 是 multipart 分片大小；超过单次 PUT 阈值才启用。
	objectStorePartSize = 16 * 1024 * 1024
	// objectStoreSinglePutLimit 是单次 PUT 的大小上限，超过改用 multipart。
	objectStoreSinglePutLimit = 64 * 1024 * 1024
	// objectStoreEmptySHA256 是空载荷的 SHA-256。
	objectStoreEmptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

var objectStoreTx atomic.Uint64

// ObjectStoreConfig 是 S3 兼容后端配置。
type ObjectStoreConfig struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	// PathStyle 为 true 时使用 https://endpoint/bucket/key，否则使用虚拟主机风格。
	PathStyle bool
	Prefix    string
	// Retries 是 5xx/429/网络错误的额外重试次数。
	Retries int
	// HTTPClient 可注入测试客户端；为空时使用带超时的默认客户端。
	HTTPClient *http.Client
}

// ObjectStore 是 contracts.BlobStore 的 S3 兼容实现。
type ObjectStore struct {
	cfg    ObjectStoreConfig
	base   *url.URL
	client *http.Client
}

// NewObjectStore 解析并校验配置。
func NewObjectStore(cfg ObjectStoreConfig) (*ObjectStore, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		return nil, errors.New("object store endpoint is empty")
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	base, err := url.Parse(endpoint)
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("invalid object store endpoint %q: %v", cfg.Endpoint, err)
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, errors.New("object store bucket is empty")
	}
	if strings.TrimSpace(cfg.Region) == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.Retries < 0 {
		cfg.Retries = 0
	}
	if cfg.Retries == 0 {
		cfg.Retries = 3
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	cfg.Endpoint = endpoint
	cfg.Prefix = strings.Trim(strings.TrimSpace(cfg.Prefix), "/")
	return &ObjectStore{cfg: cfg, base: base, client: cfg.HTTPClient}, nil
}

// Put 把内容写入暂存区并返回待提级对象。
func (s *ObjectStore) Put(ctx context.Context, key string, r io.Reader, opts contracts.BlobOptions) (contracts.BlobInfo, error) {
	clean, err := cleanKey(key)
	if err != nil {
		return contracts.BlobInfo{}, err
	}
	temp, err := os.CreateTemp("", "jt808-blob-*")
	if err != nil {
		return contracts.BlobInfo{}, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	hasher := md5.New()
	size, err := io.Copy(io.MultiWriter(temp, hasher), r)
	if err != nil {
		_ = temp.Close()
		return contracts.BlobInfo{}, err
	}
	if err := temp.Close(); err != nil {
		return contracts.BlobInfo{}, err
	}
	txid := fmt.Sprintf("%d-%d", time.Now().UnixNano(), objectStoreTx.Add(1))
	staging := objectStoreStagingPrefix + txid + "/" + clean
	contentMD5 := base64.StdEncoding.EncodeToString(hasher.Sum(nil))
	headers := http.Header{}
	if opts.ContentType != "" {
		headers.Set("Content-Type", opts.ContentType)
	}
	headers.Set("Content-MD5", contentMD5)
	for name, value := range opts.Metadata {
		headers.Set("x-amz-meta-"+strings.ToLower(name), value)
	}
	file, err := os.Open(tempPath)
	if err != nil {
		return contracts.BlobInfo{}, err
	}
	defer file.Close()

	var etag string
	if size > objectStoreSinglePutLimit {
		etag, err = s.putMultipart(ctx, staging, file, size, headers)
	} else {
		etag, err = s.putSingle(ctx, staging, file, size, headers)
	}
	if err != nil {
		return contracts.BlobInfo{}, err
	}
	sum := hex.EncodeToString(hasher.Sum(nil))
	return contracts.BlobInfo{
		Key:        clean,
		StagingKey: staging,
		Size:       size,
		MD5:        sum,
		ETag:       strings.Trim(etag, `"`),
		State:      contracts.BlobStatePending,
	}, nil
}

// Promote 用服务端复制把暂存对象提级到最终 key，并写入 pending 元数据。
func (s *ObjectStore) Promote(ctx context.Context, info contracts.BlobInfo) (contracts.BlobInfo, error) {
	key, err := cleanKey(info.Key)
	if err != nil {
		return contracts.BlobInfo{}, err
	}
	staging, err := cleanKey(info.StagingKey)
	if err != nil {
		return contracts.BlobInfo{}, err
	}
	if err := s.copyObject(ctx, staging, key, contracts.BlobStatePending, info.ETag); err != nil {
		return contracts.BlobInfo{}, err
	}
	info.Key = key
	info.State = contracts.BlobStatePending
	return info, nil
}

// MarkCommitted 只改写对象状态元数据。
func (s *ObjectStore) MarkCommitted(ctx context.Context, key string) error {
	clean, err := cleanKey(key)
	if err != nil {
		return err
	}
	return s.copyObject(ctx, clean, clean, contracts.BlobStateCommitted, "")
}

// Open 读取完整对象。
func (s *ObjectStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	clean, err := cleanKey(key)
	if err != nil {
		return nil, err
	}
	return s.get(ctx, clean, "")
}

// OpenRange 读取对象的 [offset, offset+length) 区间。
func (s *ObjectStore) OpenRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	if offset < 0 {
		return nil, fmt.Errorf("object range offset must be >= 0, got %d", offset)
	}
	clean, err := cleanKey(key)
	if err != nil {
		return nil, err
	}
	header := fmt.Sprintf("bytes=%d-", offset)
	if length > 0 {
		header = fmt.Sprintf("bytes=%d-%d", offset, offset+length-1)
	}
	return s.get(ctx, clean, header)
}

// Delete 删除对象。
func (s *ObjectStore) Delete(ctx context.Context, key string) error {
	clean, err := cleanKey(key)
	if err != nil {
		return err
	}
	resp, err := s.send(ctx, http.MethodDelete, clean, nil, nil, 0, objectStoreEmptySHA256, nil)
	if err != nil {
		return err
	}
	defer drainClose(resp)
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return s.responseError(resp)
	}
	return nil
}

// Stat 返回对象大小、ETag 与业务状态。
func (s *ObjectStore) Stat(ctx context.Context, key string) (contracts.BlobInfo, error) {
	clean, err := cleanKey(key)
	if err != nil {
		return contracts.BlobInfo{}, err
	}
	head, err := s.head(ctx, clean)
	if err != nil {
		return contracts.BlobInfo{}, err
	}
	state := contracts.BlobStateCommitted
	switch head.Header.Get("x-amz-meta-" + objectStoreStateMeta) {
	case string(contracts.BlobStatePending):
		state = contracts.BlobStatePending
	case string(contracts.BlobStateMissing):
		state = contracts.BlobStateMissing
	}
	return contracts.BlobInfo{
		Key:   clean,
		Size:  head.Size,
		ETag:  head.ETag,
		State: state,
	}, nil
}

type headResult struct {
	Size   int64
	ETag   string
	Header http.Header
}

func (s *ObjectStore) head(ctx context.Context, key string) (headResult, error) {
	resp, err := s.send(ctx, http.MethodHead, key, nil, nil, 0, objectStoreEmptySHA256, nil)
	if err != nil {
		return headResult{}, err
	}
	defer drainClose(resp)
	if resp.StatusCode == http.StatusNotFound {
		return headResult{}, fmt.Errorf("object %s not found", key)
	}
	if resp.StatusCode != http.StatusOK {
		return headResult{}, s.responseError(resp)
	}
	size, _ := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	return headResult{
		Size:   size,
		ETag:   strings.Trim(resp.Header.Get("ETag"), `"`),
		Header: resp.Header.Clone(),
	}, nil
}

// PendingBefore 返回暂存区中超过给定年龄的对象 key。
func (s *ObjectStore) PendingBefore(ctx context.Context, olderThan time.Duration) ([]string, error) {
	cutoff := time.Now().Add(-olderThan)
	objects, err := s.list(ctx, objectStoreStagingPrefix, 1000)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(objects))
	for _, object := range objects {
		if object.LastModified.Before(cutoff) {
			keys = append(keys, object.Key)
		}
	}
	return keys, nil
}

// SweepPending 删除超龄且未被业务引用的暂存对象；作为桶生命周期的兜底。
func (s *ObjectStore) SweepPending(ctx context.Context, olderThan time.Duration, referenced func(key string) bool) {
	keys, err := s.PendingBefore(ctx, olderThan)
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

func (s *ObjectStore) putSingle(ctx context.Context, key string, body io.ReadSeeker, size int64, headers http.Header) (string, error) {
	hash, err := hashFile(body)
	if err != nil {
		return "", err
	}
	if _, err := body.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	resp, err := s.send(ctx, http.MethodPut, key, nil, headers, size, hash, body)
	if err != nil {
		return "", err
	}
	defer drainClose(resp)
	if resp.StatusCode != http.StatusOK {
		return "", s.responseError(resp)
	}
	return resp.Header.Get("ETag"), nil
}

type multipartUpload struct {
	XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
	UploadID string   `xml:"UploadId"`
}

type multipartPart struct {
	PartNumber int    `xml:"PartNumber"`
	ETag       string `xml:"ETag"`
}

type multipartComplete struct {
	XMLName xml.Name        `xml:"CompleteMultipartUpload"`
	Parts   []multipartPart `xml:"Part"`
}

func (s *ObjectStore) putMultipart(ctx context.Context, key string, body io.ReadSeeker, size int64, headers http.Header) (string, error) {
	query := url.Values{"uploads": {""}}
	initHeaders := http.Header{}
	if contentType := headers.Get("Content-Type"); contentType != "" {
		initHeaders.Set("Content-Type", contentType)
	}
	for name, values := range headers {
		if strings.HasPrefix(strings.ToLower(name), "x-amz-meta-") {
			initHeaders[name] = values
		}
	}
	resp, err := s.send(ctx, http.MethodPost, key, query, initHeaders, 0, objectStoreEmptySHA256, nil)
	if err != nil {
		return "", err
	}
	var initiated multipartUpload
	xmlErr := xml.NewDecoder(resp.Body).Decode(&initiated)
	drainClose(resp)
	if xmlErr != nil {
		return "", fmt.Errorf("initiate multipart upload: %w", xmlErr)
	}
	if initiated.UploadID == "" {
		return "", errors.New("initiate multipart upload: empty upload id")
	}
	abort := func() {
		cleanup, err := s.send(ctx, http.MethodDelete, key, url.Values{"uploadId": {initiated.UploadID}}, nil, 0, objectStoreEmptySHA256, nil)
		if err == nil {
			drainClose(cleanup)
		}
	}

	parts := make([]multipartPart, 0, size/objectStorePartSize+1)
	buffer := make([]byte, objectStorePartSize)
	partNumber := 1
	for offset := int64(0); offset < size; offset += int64(len(buffer)) {
		if _, err := body.Seek(offset, io.SeekStart); err != nil {
			abort()
			return "", err
		}
		chunk := buffer
		remaining := size - offset
		if remaining < int64(len(buffer)) {
			chunk = buffer[:remaining]
		}
		n, err := io.ReadFull(body, chunk)
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			abort()
			return "", err
		}
		chunk = chunk[:n]
		sum := sha256.Sum256(chunk)
		partResp, err := s.send(ctx, http.MethodPut, key,
			url.Values{"partNumber": {strconv.Itoa(partNumber)}, "uploadId": {initiated.UploadID}},
			nil, int64(len(chunk)), hex.EncodeToString(sum[:]), bytes.NewReader(chunk))
		if err != nil {
			abort()
			return "", err
		}
		etag := partResp.Header.Get("ETag")
		status := partResp.StatusCode
		drainClose(partResp)
		if status != http.StatusOK {
			abort()
			return "", fmt.Errorf("upload part %d failed with status %d", partNumber, status)
		}
		parts = append(parts, multipartPart{PartNumber: partNumber, ETag: etag})
		partNumber++
	}

	payload, err := xml.Marshal(multipartComplete{Parts: parts})
	if err != nil {
		abort()
		return "", err
	}
	sum := sha256.Sum256(payload)
	completeResp, err := s.send(ctx, http.MethodPost, key, url.Values{"uploadId": {initiated.UploadID}},
		http.Header{"Content-Type": {"application/xml"}}, int64(len(payload)), hex.EncodeToString(sum[:]), bytes.NewReader(payload))
	if err != nil {
		abort()
		return "", err
	}
	defer drainClose(completeResp)
	if completeResp.StatusCode != http.StatusOK {
		abort()
		return "", s.responseError(completeResp)
	}
	var completed struct {
		ETag string `xml:"ETag"`
	}
	_ = xml.NewDecoder(completeResp.Body).Decode(&completed)
	return completed.ETag, nil
}

func (s *ObjectStore) copyObject(ctx context.Context, source, target string, state contracts.BlobState, etag string) error {
	sourcePath := "/" + s.cfg.Bucket + "/" + s.objectKey(source)
	headers := http.Header{}
	headers.Set("x-amz-copy-source", awsEscapePath(sourcePath))
	headers.Set("x-amz-metadata-directive", "REPLACE")
	headers.Set("x-amz-meta-"+objectStoreStateMeta, string(state))
	if head, err := s.head(ctx, source); err == nil {
		if head.ETag != "" {
			etag = head.ETag
		}
		if contentType := head.Header.Get("Content-Type"); contentType != "" {
			headers.Set("Content-Type", contentType)
		}
	}
	if etag != "" {
		headers.Set("x-amz-copy-source-if-match", `"`+strings.Trim(etag, `"`)+`"`)
	}
	resp, err := s.send(ctx, http.MethodPut, target, nil, headers, 0, objectStoreEmptySHA256, nil)
	if err != nil {
		return err
	}
	defer drainClose(resp)
	if resp.StatusCode != http.StatusOK {
		return s.responseError(resp)
	}
	// CopyObject 的 200 响应也可能是错误体，必须解析。
	var result struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if len(raw) > 0 && xml.Unmarshal(raw, &result) == nil && result.Code != "" {
		return fmt.Errorf("copy object: %s %s", result.Code, result.Message)
	}
	return nil
}

func (s *ObjectStore) get(ctx context.Context, key, rangeHeader string) (io.ReadCloser, error) {
	headers := http.Header{}
	if rangeHeader != "" {
		headers.Set("Range", rangeHeader)
	}
	resp, err := s.send(ctx, http.MethodGet, key, nil, headers, 0, objectStoreEmptySHA256, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		defer drainClose(resp)
		return nil, s.responseError(resp)
	}
	return resp.Body, nil
}

type listObject struct {
	Key          string    `xml:"Key"`
	LastModified time.Time `xml:"LastModified"`
}

type listResult struct {
	Contents              []listObject `xml:"Contents"`
	IsTruncated           bool         `xml:"IsTruncated"`
	NextContinuationToken string       `xml:"NextContinuationToken"`
}

// list 返回指定前缀下的对象；maxKeys<=0 使用 1000。
func (s *ObjectStore) list(ctx context.Context, prefix string, maxKeys int) ([]listObject, error) {
	if maxKeys <= 0 || maxKeys > 1000 {
		maxKeys = 1000
	}
	var objects []listObject
	token := ""
	for {
		query := url.Values{"list-type": {"2"}, "max-keys": {strconv.Itoa(maxKeys)}}
		if prefix != "" {
			query.Set("prefix", s.objectKey(prefix))
		}
		if token != "" {
			query.Set("continuation-token", token)
		}
		resp, err := s.send(ctx, http.MethodGet, "", query, nil, 0, objectStoreEmptySHA256, nil)
		if err != nil {
			return nil, err
		}
		var result listResult
		decodeErr := xml.NewDecoder(resp.Body).Decode(&result)
		status := resp.StatusCode
		drainClose(resp)
		if status != http.StatusOK {
			return nil, fmt.Errorf("list objects failed with status %d", status)
		}
		if decodeErr != nil {
			return nil, decodeErr
		}
		objects = append(objects, result.Contents...)
		if !result.IsTruncated || result.NextContinuationToken == "" {
			return objects, nil
		}
		token = result.NextContinuationToken
	}
}

func (s *ObjectStore) send(ctx context.Context, method, key string, query url.Values, headers http.Header, bodyLen int64, payloadHash string, body io.Reader) (*http.Response, error) {
	target, err := s.urlFor(key, query)
	if err != nil {
		return nil, err
	}
	attempts := s.cfg.Retries + 1
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		request, err := http.NewRequestWithContext(ctx, method, target.String(), body)
		if err != nil {
			return nil, err
		}
		if headers != nil {
			request.Header = headers.Clone()
		}
		if bodyLen > 0 {
			request.ContentLength = bodyLen
		}
		s.sign(request, payloadHash)
		response, err := s.client.Do(request)
		if err != nil {
			lastErr = err
			sleepBackoff(attempt)
			continue
		}
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
			lastErr = s.responseError(response)
			drainClose(response)
			sleepBackoff(attempt)
			continue
		}
		return response, nil
	}
	return nil, lastErr
}

func (s *ObjectStore) urlFor(key string, query url.Values) (*url.URL, error) {
	target := *s.base
	target.Path = strings.TrimSuffix(target.Path, "/")
	if s.cfg.PathStyle {
		target.Path += "/" + s.cfg.Bucket
		if key != "" {
			target.Path += "/" + s.objectKey(key)
		}
	} else {
		target.Host = s.cfg.Bucket + "." + target.Host
		if key != "" {
			target.Path += "/" + s.objectKey(key)
		}
	}
	if query != nil {
		target.RawQuery = canonicalQuery(query)
	}
	return &target, nil
}

func (s *ObjectStore) objectKey(key string) string {
	key = strings.TrimPrefix(key, "/")
	if s.cfg.Prefix == "" {
		return key
	}
	return path.Join(s.cfg.Prefix, key)
}

func (s *ObjectStore) sign(request *http.Request, payloadHash string) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	date := now.Format("20060102")
	request.Header.Set("x-amz-date", amzDate)
	request.Header.Set("x-amz-content-sha256", payloadHash)

	names := make([]string, 0, len(request.Header)+1)
	values := map[string]string{"host": request.URL.Host}
	for name := range request.Header {
		lower := strings.ToLower(name)
		if lower == "authorization" || lower == "user-agent" || lower == "content-length" {
			continue
		}
		names = append(names, lower)
		values[lower] = strings.TrimSpace(request.Header.Get(name))
	}
	names = append(names, "host")
	sort.Strings(names)
	names = uniqueStrings(names)

	var canonicalHeaders strings.Builder
	for _, name := range names {
		canonicalHeaders.WriteString(name)
		canonicalHeaders.WriteString(":")
		canonicalHeaders.WriteString(values[name])
		canonicalHeaders.WriteString("\n")
	}
	signedHeaders := strings.Join(names, ";")
	canonicalRequest := strings.Join([]string{
		request.Method,
		request.URL.EscapedPath(),
		canonicalQuery(request.URL.Query()),
		canonicalHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := date + "/" + s.cfg.Region + "/s3/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256Hex([]byte(canonicalRequest))
	signingKey := hmacSHA256([]byte("AWS4"+s.cfg.SecretKey), date)
	signingKey = hmacSHA256(signingKey, s.cfg.Region)
	signingKey = hmacSHA256(signingKey, "s3")
	signingKey = hmacSHA256(signingKey, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
	request.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.cfg.AccessKey, scope, signedHeaders, signature))
}

func (s *ObjectStore) responseError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	var payload struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	detail := strings.TrimSpace(string(raw))
	if xml.Unmarshal(raw, &payload) == nil && payload.Code != "" {
		detail = payload.Code + " " + payload.Message
	}
	return fmt.Errorf("object store %s %s failed: status=%d %s",
		resp.Request.Method, resp.Request.URL.Path, resp.StatusCode, detail)
}

func hashFile(file io.ReadSeeker) (string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}

// canonicalQuery 按 AWS 规则编码并按 key 排序。
func canonicalQuery(values url.Values) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		items := append([]string(nil), values[key]...)
		sort.Strings(items)
		if len(items) == 0 {
			items = []string{""}
		}
		for _, item := range items {
			pairs = append(pairs, awsEscape(key)+"="+awsEscape(item))
		}
	}
	return strings.Join(pairs, "&")
}

func awsEscapePath(value string) string {
	segments := strings.Split(value, "/")
	for i, segment := range segments {
		segments[i] = awsEscape(segment)
	}
	return strings.Join(segments, "/")
}

// awsEscape 按 RFC 3986 编码；保留 A-Z a-z 0-9 - _ . ~。
func awsEscape(value string) string {
	var builder strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			builder.WriteByte(c)
			continue
		}
		fmt.Fprintf(&builder, "%%%02X", c)
	}
	return builder.String()
}

func uniqueStrings(values []string) []string {
	out := values[:0]
	for i, value := range values {
		if i == 0 || value != values[i-1] {
			out = append(out, value)
		}
	}
	return out
}

func sleepBackoff(attempt int) {
	delay := time.Duration(1<<uint(attempt)) * 50 * time.Millisecond
	if delay > 2*time.Second {
		delay = 2 * time.Second
	}
	time.Sleep(delay)
}

func drainClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	_ = resp.Body.Close()
}

var _ contracts.BlobStore = (*ObjectStore)(nil)
