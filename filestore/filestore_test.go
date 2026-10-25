package filestore

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/FutureAI-X/futureai-api/model"
	"github.com/FutureAI-X/futureai-api/storage"
)

var testNow = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

type uploadedObject struct {
	key         string
	data        []byte
	contentType string
}

type fakeStore struct {
	serviceAddress string
	uploads        []uploadedObject
	uploadErr      error
}

func (s *fakeStore) ServiceType() string { return storage.ServiceTencentCOS }

func (s *fakeStore) ServiceAddress() string { return s.serviceAddress }

func (s *fakeStore) Upload(ctx context.Context, key string, data []byte, contentType string) error {
	if s.uploadErr != nil {
		return s.uploadErr
	}
	s.uploads = append(s.uploads, uploadedObject{
		key:         key,
		data:        append([]byte(nil), data...),
		contentType: contentType,
	})
	return nil
}

func (s *fakeStore) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	return s.ObjectURL(key), nil
}

func (s *fakeStore) ObjectURL(key string) string {
	return strings.TrimRight(s.serviceAddress, "/") + "/" + strings.TrimLeft(key, "/")
}

func pngBytes() []byte {
	return []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 1, 2, 3, 4}
}

func newTestPersister(t *testing.T, handler http.HandlerFunc) (*Persister, *fakeStore, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	store := &fakeStore{serviceAddress: "https://bucket.example.com"}
	persister := &Persister{
		cfg: Config{
			ServiceType: storage.ServiceTencentCOS,
			TTL:         24 * time.Hour,
			MaxBytes:    1 << 20,
			Timeout:     5 * time.Second,
		},
		newStore:   func(string, string) (storage.Store, error) { return store, nil },
		createFile: func(*model.File) error { return nil },
		httpClient: srv.Client(),
		now:        func() time.Time { return testNow },
		newFileID:  generateFileID,
	}
	return persister, store, srv
}

func TestPersistURLSuccess(t *testing.T) {
	png := pngBytes()
	persister, store, srv := newTestPersister(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	})

	file, err := persister.PersistURL(context.Background(), srv.URL+"/source")
	if err != nil {
		t.Fatalf("转存失败: %v", err)
	}

	if !regexp.MustCompile(`^file_[0-9a-f]{24}\.png$`).MatchString(file.FileID) {
		t.Errorf("file_id 形态异常: %q", file.FileID)
	}
	wantKey := "png/" + file.FileID
	if file.FileKey != wantKey {
		t.Errorf("FileKey = %q, want %q", file.FileKey, wantKey)
	}
	if file.ServiceType != storage.ServiceTencentCOS {
		t.Errorf("ServiceType = %q", file.ServiceType)
	}
	if file.ServiceAddress != "https://bucket.example.com" {
		t.Errorf("ServiceAddress = %q", file.ServiceAddress)
	}
	if want := "https://bucket.example.com/" + wantKey; file.URL != want {
		t.Errorf("URL = %q, want %q", file.URL, want)
	}
	if file.TTLSeconds != 86400 {
		t.Errorf("TTLSeconds = %d, want 86400", file.TTLSeconds)
	}
	if !file.CreatedAt.Equal(testNow) {
		t.Errorf("CreatedAt = %s, want %s", file.CreatedAt, testNow)
	}
	if file.ExpiresAt == nil || !file.ExpiresAt.Equal(testNow.Add(24*time.Hour)) {
		t.Errorf("ExpiresAt = %v, want %v", file.ExpiresAt, testNow.Add(24*time.Hour))
	}

	if len(store.uploads) != 1 {
		t.Fatalf("上传次数 = %d, want 1", len(store.uploads))
	}
	up := store.uploads[0]
	if up.key != wantKey {
		t.Errorf("上传 key = %q, want %q", up.key, wantKey)
	}
	if up.contentType != "image/png" {
		t.Errorf("上传 Content-Type = %q, want image/png", up.contentType)
	}
	if string(up.data) != string(png) {
		t.Errorf("上传内容与源数据不一致")
	}
}

func TestPersistURLUsesConfiguredTTL(t *testing.T) {
	persister, _, srv := newTestPersister(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pngBytes())
	})
	persister.cfg.TTL = 2 * time.Hour

	file, err := persister.PersistURL(context.Background(), srv.URL+"/source")
	if err != nil {
		t.Fatalf("转存失败: %v", err)
	}
	if file.TTLSeconds != 7200 {
		t.Errorf("TTLSeconds = %d, want 7200", file.TTLSeconds)
	}
	if file.ExpiresAt == nil || !file.ExpiresAt.Equal(testNow.Add(2*time.Hour)) {
		t.Errorf("ExpiresAt = %v, want %v", file.ExpiresAt, testNow.Add(2*time.Hour))
	}
}

func TestPersistURLRejectsNonImage(t *testing.T) {
	persister, store, srv := newTestPersister(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>not an image</html>"))
	})

	_, err := persister.PersistURL(context.Background(), srv.URL+"/source")
	if err == nil {
		t.Fatal("非图片内容应返回错误")
	}
	if len(store.uploads) != 0 {
		t.Error("非图片内容不应上传")
	}
}

func TestPersistURLRejectsOversizedFile(t *testing.T) {
	body := append(pngBytes(), 1, 2, 3) // 超过下面设置的 10 字节上限
	persister, store, srv := newTestPersister(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	})
	persister.cfg.MaxBytes = 10

	_, err := persister.PersistURL(context.Background(), srv.URL+"/source")
	if err == nil {
		t.Fatal("超过体积上限应返回错误")
	}
	if len(store.uploads) != 0 {
		t.Error("超限文件不应上传")
	}
}

func TestPersistURLPropagatesHTTPError(t *testing.T) {
	persister, store, srv := newTestPersister(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	_, err := persister.PersistURL(context.Background(), srv.URL+"/missing")
	if err == nil {
		t.Fatal("上游 404 应返回错误")
	}
	if len(store.uploads) != 0 {
		t.Error("下载失败不应上传")
	}
}

func TestPersistURLRejectsInvalidURL(t *testing.T) {
	persister, _, _ := newTestPersister(t, func(w http.ResponseWriter, r *http.Request) {})

	cases := []string{"ftp://example.com/a.png", "file:///etc/passwd", "/relative/a.png", "", "https://user:pass@example.com/a.png"}
	for _, raw := range cases {
		if _, err := persister.PersistURL(context.Background(), raw); err == nil {
			t.Errorf("%q 应被拒绝", raw)
		}
	}
}

func TestPersistURLRetriesOnDuplicateFileID(t *testing.T) {
	persister, store, srv := newTestPersister(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pngBytes())
	})

	ids := []string{
		"file_aaaaaaaaaaaaaaaaaaaaaaaa.png",
		"file_bbbbbbbbbbbbbbbbbbbbbbbb.png",
	}
	next := 0
	persister.newFileID = func(ext string) (string, error) {
		id := ids[next]
		next++
		return id, nil
	}

	attempts := 0
	persister.createFile = func(*model.File) error {
		attempts++
		if attempts == 1 {
			return errors.New("duplicate key value violates unique constraint")
		}
		return nil
	}

	file, err := persister.PersistURL(context.Background(), srv.URL+"/source")
	if err != nil {
		t.Fatalf("冲突后应重试成功: %v", err)
	}
	if file.FileID != ids[1] {
		t.Errorf("FileID = %q, want %q", file.FileID, ids[1])
	}
	if len(store.uploads) != 2 {
		t.Errorf("上传次数 = %d, want 2", len(store.uploads))
	}
}

func TestPersistURLDoesNotRetryOtherDBErrors(t *testing.T) {
	persister, store, srv := newTestPersister(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pngBytes())
	})

	dbErr := errors.New("connection refused")
	persister.createFile = func(*model.File) error { return dbErr }

	_, err := persister.PersistURL(context.Background(), srv.URL+"/source")
	if !errors.Is(err, dbErr) {
		t.Errorf("err = %v, want %v", err, dbErr)
	}
	if len(store.uploads) != 1 {
		t.Errorf("非冲突错误不应重试，上传次数 = %d", len(store.uploads))
	}
}

func TestPersistURLPropagatesUploadError(t *testing.T) {
	persister, store, srv := newTestPersister(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pngBytes())
	})
	store.uploadErr = errors.New("COS 上传失败")

	if _, err := persister.PersistURL(context.Background(), srv.URL+"/source"); err == nil {
		t.Fatal("上传失败应返回错误")
	}
}

func TestDetectImageExt(t *testing.T) {
	cases := []struct {
		name        string
		data        []byte
		wantExt     string
		wantContent string
		wantOK      bool
	}{
		{"JPEG", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0, 0, 0, 0, 0}, "jpg", "image/jpeg", true},
		{"PNG", pngBytes(), "png", "image/png", true},
		{"WebP", []byte("RIFF\x00\x00\x00\x00WEBPVP8 "), "webp", "image/webp", true},
		{"GIF", []byte("GIF89a\x00\x00\x00\x00\x00\x00"), "gif", "image/gif", true},
		{"HTML", []byte("<!DOCTYPE html>"), "", "", false},
		{"空内容", nil, "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ext, contentType, ok := detectImageExt(tc.data)
			if ok != tc.wantOK || ext != tc.wantExt || contentType != tc.wantContent {
				t.Errorf("detectImageExt() = (%q, %q, %v), want (%q, %q, %v)",
					ext, contentType, ok, tc.wantExt, tc.wantContent, tc.wantOK)
			}
		})
	}
}

func TestGenerateFileID(t *testing.T) {
	pattern := regexp.MustCompile(`^file_[0-9a-f]{24}\.(jpg|png|webp|gif)$`)

	seen := make(map[string]struct{})
	for i := 0; i < 1000; i++ {
		id, err := generateFileID("png")
		if err != nil {
			t.Fatalf("生成 file_id 失败: %v", err)
		}
		if !pattern.MatchString(id) {
			t.Fatalf("file_id 形态异常: %q", id)
		}
		if !strings.HasSuffix(id, ".png") {
			t.Fatalf("扩展名丢失: %q", id)
		}
		if _, ok := seen[id]; ok {
			t.Fatalf("file_id 重复: %s", id)
		}
		seen[id] = struct{}{}
	}
}

func TestLoadConfig(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want time.Duration
	}{
		{"未配置", "", DefaultTTL},
		{"合法值", "3600", time.Hour},
		{"非法值", "abc", DefaultTTL},
		{"零", "0", DefaultTTL},
		{"负数", "-10", DefaultTTL},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(ttlEnv, tc.raw)
			if got := LoadConfig().TTL; got != tc.want {
				t.Errorf("TTL = %s, want %s", got, tc.want)
			}
		})
	}
}
