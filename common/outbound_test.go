package common

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// ── IsBlockedOutboundIP：SSRF 目标拦截 ──

func TestIsBlockedOutboundIPBlocksInternalTargets(t *testing.T) {
	blocked := []string{
		"127.0.0.1",       // 回环
		"127.0.0.53",      // 回环网段
		"::1",             // IPv6 回环
		"10.0.0.1",        // 私网
		"172.16.5.5",      // 私网
		"192.168.1.1",     // 私网
		"169.254.169.254", // 云元数据（AWS/GCP/Azure/阿里云）
		"169.254.170.2",   // AWS ECS 元数据
		"100.100.100.200", // 阿里云元数据
		"100.64.0.1",      // CGNAT
		"0.0.0.0",         // 未指定
		"fc00::1",         // IPv6 ULA
		"fe80::1",         // IPv6 链路本地
		"198.18.0.1",      // 基准测试网段
	}
	for _, s := range blocked {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("测试用例 IP 无法解析: %s", s)
		}
		if !IsBlockedOutboundIP(ip) {
			t.Errorf("%s 应被拦截（SSRF 高危目标）", s)
		}
	}
}

func TestIsBlockedOutboundIPAllowsPublicTargets(t *testing.T) {
	allowed := []string{
		"8.8.8.8",
		"1.1.1.1",
		"93.184.216.34", // example.com 历史地址
		"2606:4700:4700::1111",
	}
	for _, s := range allowed {
		ip := net.ParseIP(s)
		if IsBlockedOutboundIP(ip) {
			t.Errorf("%s 是公网地址，不应被拦截", s)
		}
	}
}

func TestIsBlockedOutboundIPHandlesNil(t *testing.T) {
	if !IsBlockedOutboundIP(nil) {
		t.Error("nil IP 应视为不可用（fail-closed）")
	}
}

// TestTrustedEndpointHTTPClientAllowsLoopback 验证可信端点客户端不做内网拦截。
// 腾讯云 COS 在云主机内会把桶域名解析到 169.254.0.0/16，通用客户端会因此拒绝上传；
// 用回环地址模拟同一类"正常但非公网"的目标。
func TestTrustedEndpointHTTPClientAllowsLoopback(t *testing.T) {
	t.Setenv(OutboundProxyEnv, "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	resp, err := TrustedEndpointHTTPClient().Get(srv.URL)
	if err != nil {
		t.Fatalf("可信端点客户端不应拒绝回环地址: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// ── ValidateOutboundBaseURL：配置入口校验 ──
// 全部使用 IP 字面量，避免测试依赖 DNS 解析。

func TestValidateOutboundBaseURLRejectsDangerousTargets(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"回环地址", "http://127.0.0.1:3001"},
		{"云元数据地址", "http://169.254.169.254/latest/meta-data/"},
		{"私网地址", "http://10.0.0.5"},
		{"私网地址 192.168", "https://192.168.0.10"},
		{"IPv6 回环", "http://[::1]:8080"},
		{"file 协议", "file:///etc/passwd"},
		{"gopher 协议", "gopher://127.0.0.1:11211/"},
		{"缺少主机名", "http:///path"},
		{"空地址", ""},
		{"携带用户凭据", "https://user:pass@8.8.8.8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateOutboundBaseURL(tc.url); err == nil {
				t.Errorf("%q 应被拒绝", tc.url)
			}
		})
	}
}

func TestValidateOutboundBaseURLAllowsPublicHTTPS(t *testing.T) {
	// 使用公网 IP 字面量，避免 DNS 依赖
	if err := ValidateOutboundBaseURL("https://8.8.8.8/v1"); err != nil {
		t.Errorf("合法公网地址不应被拒绝: %v", err)
	}
}

func TestValidateOutboundBaseURLTrimsWhitespace(t *testing.T) {
	if err := ValidateOutboundBaseURL("  https://8.8.8.8/v1  "); err != nil {
		t.Errorf("首尾空白应被容忍: %v", err)
	}
}

// ── 安全 HTTP 客户端 ──

func TestOutboundHTTPClientRejectsCrossHostRedirect(t *testing.T) {
	c := OutboundHTTPClient()
	if c.CheckRedirect == nil {
		t.Fatal("必须设置 CheckRedirect，否则跨主机重定向会带着 API Key 跳走")
	}
	if c.Transport == nil {
		t.Fatal("必须设置自定义 Transport 以启用拨号期 IP 校验")
	}
}

// 出站客户端必须是共享单例：每个请求新建 Transport 等于连接池归零，
// keep-alive 完全失效，且未设 IdleConnTimeout 时空闲连接永不回收。
func TestOutboundHTTPClientIsShared(t *testing.T) {
	a := OutboundHTTPClient()
	b := OutboundHTTPClient()
	if a != b {
		t.Fatal("OutboundHTTPClient 必须返回同一个实例，否则连接池无法复用")
	}

	tr, ok := a.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport 类型 = %T, want *http.Transport", a.Transport)
	}
	if tr.IdleConnTimeout <= 0 {
		t.Error("IdleConnTimeout 必须大于 0：Go 只在它大于 0 时才回收空闲连接")
	}
	if tr.MaxIdleConnsPerHost <= 2 {
		t.Errorf("MaxIdleConnsPerHost = %d，等于默认值 2，高并发下会频繁重建连接", tr.MaxIdleConnsPerHost)
	}
}

// ── FileDownloadHTTPClient：公开文件下载客户端 ──

func TestFileDownloadHTTPClientIsShared(t *testing.T) {
	a := FileDownloadHTTPClient()
	b := FileDownloadHTTPClient()
	if a != b {
		t.Fatal("FileDownloadHTTPClient 必须返回同一个实例，否则连接池无法复用")
	}
	if _, ok := a.Transport.(*http.Transport); !ok {
		t.Fatalf("Transport 类型 = %T, want *http.Transport", a.Transport)
	}
}

// TestFileDownloadHTTPClientRedirectPolicy 验证：
// 不带凭证的下载客户端允许跨主机重定向（CDN 常见），但仍拒绝非 http(s)。
func TestFileDownloadHTTPClientRedirectPolicy(t *testing.T) {
	c := FileDownloadHTTPClient()
	if c.CheckRedirect == nil {
		t.Fatal("必须设置 CheckRedirect")
	}

	mustParse := func(raw string) *url.URL {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("解析 %q 失败: %v", raw, err)
		}
		return u
	}

	via := []*http.Request{{URL: mustParse("https://a.example.com/file.png")}}

	// 跨主机重定向应放行
	crossHost := &http.Request{URL: mustParse("https://b.example.com/file.png")}
	if err := c.CheckRedirect(crossHost, via); err != nil {
		t.Errorf("跨主机重定向被拒绝: %v", err)
	}

	// 非 http(s) 协议应拒绝
	badScheme := &http.Request{URL: mustParse("file:///etc/passwd")}
	if err := c.CheckRedirect(badScheme, via); err == nil {
		t.Error("重定向到 file 协议应被拒绝")
	}

	// 重定向次数应设上限
	tooMany := make([]*http.Request, 3)
	for i := range tooMany {
		tooMany[i] = &http.Request{URL: mustParse("https://a.example.com/file.png")}
	}
	if err := c.CheckRedirect(crossHost, tooMany); err == nil {
		t.Error("超过 3 次重定向应被拒绝")
	}
}
