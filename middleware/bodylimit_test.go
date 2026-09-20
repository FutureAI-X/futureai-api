package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/FutureAI-X/futureai-api/common"
	"github.com/gin-gonic/gin"
)

// newBodyLimitEngine 起一个挂了体积限制的引擎，handler 只回报是否读到了 body。
func newBodyLimitEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(BodyLimit())

	read := func(c *gin.Context) {
		data, err := io.ReadAll(c.Request.Body)
		if err != nil {
			// 第二道防线（MaxBytesReader）在读取过程中才触发，
			// 处理函数必须自己把它翻译成 413
			if common.IsBodyTooLarge(err) {
				c.String(http.StatusRequestEntityTooLarge, "too large")
				return
			}
			c.String(http.StatusBadRequest, "read error")
			return
		}
		c.String(http.StatusOK, "read "+strconv.Itoa(len(data)))
	}

	e.POST("/v1/images/generations", read)
	e.POST(common.UploadPath, read)
	return e
}

// 超出上限的请求必须在读 body 之前就被拒掉，且返回 413 而不是 400。
func TestBodyLimitRejectsOversizedJSON(t *testing.T) {
	e := newBodyLimitEngine()

	body := bytes.Repeat([]byte("a"), int(common.DefaultBodyLimit)+1024)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("状态码 = %d, want 413（超大请求体必须被拒）", w.Code)
	}
}

// 正常大小的请求不受影响。
func TestBodyLimitAllowsNormalJSON(t *testing.T) {
	e := newBodyLimitEngine()

	body := []byte(`{"model":"m","prompt":"a cat"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200（正常请求不该被拦）", w.Code)
	}
	if want := "read " + strconv.Itoa(len(body)); w.Body.String() != want {
		t.Errorf("body = %q, want %q", w.Body.String(), want)
	}
}

// 上传端点必须放宽到「文件上限 + multipart 余量」，
// 否则一个刚好 10MB 的文件会因为边框开销被整体拒掉。
func TestBodyLimitAllowsUploadUpToItsLimit(t *testing.T) {
	e := newBodyLimitEngine()

	justUnder := bytes.Repeat([]byte("a"), int(common.MaxUploadFileSize)+1024)
	req := httptest.NewRequest(http.MethodPost, common.UploadPath, bytes.NewReader(justUnder))

	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code == http.StatusRequestEntityTooLarge {
		t.Errorf("上传端点不应拒绝 %d 字节（上限 %d）",
			len(justUnder), common.MaxUploadFileSize+common.MultipartOverhead)
	}

	// 明显超过上限的仍要被拒
	over := bytes.Repeat([]byte("a"), int(common.MaxUploadFileSize+common.MultipartOverhead)+1024)
	req = httptest.NewRequest(http.MethodPost, common.UploadPath, bytes.NewReader(over))
	w = httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("上传端点对超限请求的状态码 = %d, want 413", w.Code)
	}
}

// 分块传输（无 Content-Length）也要被拦住。
// 只做 Content-Length 预检的话，chunked 请求可以完全绕过限制。
func TestBodyLimitCatchesChunkedWithoutContentLength(t *testing.T) {
	e := newBodyLimitEngine()

	body := bytes.Repeat([]byte("a"), int(common.DefaultBodyLimit)+4096)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.ContentLength = -1 // 模拟 chunked

	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("chunked 超限请求状态码 = %d, want 413", w.Code)
	}
}
