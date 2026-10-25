package storage

import (
	"context"
	"errors"
	"hash/crc64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func newTestCOS(t *testing.T, bucketURL string, transport http.RoundTripper) *TencentCOS {
	t.Helper()
	store, err := NewTencentCOS(TencentCOSConfig{
		BucketURL: bucketURL,
		SecretID:  "test-secret-id",
		SecretKey: "test-secret-key",
		Transport: transport,
	})
	if err != nil {
		t.Fatalf("创建 COS 客户端失败: %v", err)
	}
	return store
}

func TestTencentCOSUpload(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 1, 2, 3}

	var (
		gotMethod string
		gotPath   string
		gotType   string
		gotBody   []byte
		gotAuth   string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotType = r.Header.Get("Content-Type")
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)

		// SDK 会在 2xx 响应上校验 x-cos-hash-crc64ecma，
		// 假服务端必须返回与上传内容一致的 CRC64-ECMA。
		hash := crc64.New(crc64.MakeTable(crc64.ECMA))
		_, _ = hash.Write(gotBody)
		w.Header().Set("x-cos-hash-crc64ecma", strconv.FormatUint(hash.Sum64(), 10))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := newTestCOS(t, srv.URL, srv.Client().Transport)
	if err := store.Upload(context.Background(), "png/file_abc.png", png, "image/png"); err != nil {
		t.Fatalf("上传失败: %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Errorf("方法 = %s, want PUT", gotMethod)
	}
	if gotPath != "/png/file_abc.png" {
		t.Errorf("路径 = %s, want /png/file_abc.png", gotPath)
	}
	if gotType != "image/png" {
		t.Errorf("Content-Type = %s, want image/png", gotType)
	}
	if string(gotBody) != string(png) {
		t.Errorf("上传内容与源数据不一致: %q", gotBody)
	}
	if gotAuth == "" {
		t.Error("请求缺少 Authorization，签名层未生效")
	}
}

func TestTencentCOSUploadPropagatesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<Error><Code>AccessDenied</Code></Error>`))
	}))
	defer srv.Close()

	store := newTestCOS(t, srv.URL, srv.Client().Transport)
	err := store.Upload(context.Background(), "png/file_abc.png", []byte{1}, "image/png")
	if err == nil {
		t.Fatal("上游返回 403 时应返回错误")
	}
}

func TestTencentCOSPresignGet(t *testing.T) {
	store := newTestCOS(t, "https://examplebucket-1250000000.cos.ap-guangzhou.myqcloud.com", nil)

	raw, err := store.PresignGet(context.Background(), "png/file_abc.png", 5*time.Minute)
	if err != nil {
		t.Fatalf("预签名失败: %v", err)
	}

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("预签名地址无法解析: %v", err)
	}
	if u.Scheme != "https" || u.Host != "examplebucket-1250000000.cos.ap-guangzhou.myqcloud.com" {
		t.Errorf("预签名地址主机异常: %s", raw)
	}
	if u.Path != "/png/file_abc.png" {
		t.Errorf("预签名路径 = %s, want /png/file_abc.png", u.Path)
	}

	q := u.Query()
	for _, key := range []string{"q-sign-algorithm", "q-signature", "q-key-time"} {
		if q.Get(key) == "" {
			t.Errorf("预签名参数缺少 %s: %s", key, raw)
		}
	}
}

func TestTencentCOSPresignGetRejectsEmptyKey(t *testing.T) {
	store := newTestCOS(t, "https://examplebucket-1250000000.cos.ap-guangzhou.myqcloud.com", nil)
	if _, err := store.PresignGet(context.Background(), "  ", time.Minute); err == nil {
		t.Error("空 file_key 应返回错误")
	}
}

func TestTencentCOSObjectURL(t *testing.T) {
	store := newTestCOS(t, "https://examplebucket-1250000000.cos.ap-guangzhou.myqcloud.com/", nil)

	want := "https://examplebucket-1250000000.cos.ap-guangzhou.myqcloud.com/png/file_abc.png"
	if got := store.ObjectURL("/png/file_abc.png"); got != want {
		t.Errorf("ObjectURL() = %q, want %q", got, want)
	}
	if store.ServiceType() != ServiceTencentCOS {
		t.Errorf("ServiceType() = %q", store.ServiceType())
	}
}

func TestNewTencentCOSValidation(t *testing.T) {
	cases := map[string]TencentCOSConfig{
		"缺少桶地址":        {SecretID: "id", SecretKey: "key"},
		"非法桶地址":        {BucketURL: "ftp://example.com", SecretID: "id", SecretKey: "key"},
		"缺少 SecretID":  {BucketURL: "https://example.com", SecretKey: "key"},
		"缺少 SecretKey": {BucketURL: "https://example.com", SecretID: "id"},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewTencentCOS(cfg); err == nil {
				t.Error("非法配置应返回错误")
			}
		})
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv(tencentCOSBucketURLEnv, "https://examplebucket-1250000000.cos.ap-guangzhou.myqcloud.com")
	t.Setenv(tencentCOSSecretIDEnv, "id")
	t.Setenv(tencentCOSSecretKeyEnv, "key")

	store, err := FromEnv(ServiceTencentCOS, "")
	if err != nil {
		t.Fatalf("FromEnv 失败: %v", err)
	}
	if store.ServiceType() != ServiceTencentCOS {
		t.Errorf("ServiceType() = %q", store.ServiceType())
	}

	// serviceAddress 优先于环境变量
	store, err = FromEnv(ServiceTencentCOS, "https://other-1250000000.cos.ap-beijing.myqcloud.com")
	if err != nil {
		t.Fatalf("FromEnv 失败: %v", err)
	}
	if store.ServiceAddress() != "https://other-1250000000.cos.ap-beijing.myqcloud.com" {
		t.Errorf("ServiceAddress() = %q", store.ServiceAddress())
	}
}

func TestFromEnvMissingSecret(t *testing.T) {
	t.Setenv(tencentCOSBucketURLEnv, "https://examplebucket-1250000000.cos.ap-guangzhou.myqcloud.com")
	t.Setenv(tencentCOSSecretIDEnv, "id")
	t.Setenv(tencentCOSSecretKeyEnv, "")

	_, err := FromEnv(ServiceTencentCOS, "")
	if !errors.Is(err, ErrNotConfigured) {
		t.Errorf("err = %v, want ErrNotConfigured", err)
	}
}

func TestFromEnvUnsupportedService(t *testing.T) {
	_, err := FromEnv("SOME_OTHER_STORAGE", "")
	if !errors.Is(err, ErrNotSupported) {
		t.Errorf("err = %v, want ErrNotSupported", err)
	}
}

func TestFromEnvServiceTypeIsCaseInsensitive(t *testing.T) {
	t.Setenv(tencentCOSSecretIDEnv, "id")
	t.Setenv(tencentCOSSecretKeyEnv, "key")

	store, err := FromEnv(strings.ToLower(ServiceTencentCOS), "https://examplebucket-1250000000.cos.ap-guangzhou.myqcloud.com")
	if err != nil {
		t.Fatalf("小写服务类型应被接受: %v", err)
	}
	if store.ServiceType() != ServiceTencentCOS {
		t.Errorf("ServiceType() = %q", store.ServiceType())
	}
}
