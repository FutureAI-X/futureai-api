package common

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MaxOutboundBodySize 出站响应体最大读取字节数（8MB）。
// 仅靠 http.Client.Timeout 只能限制时长、不能限制体积：
// 恶意或被控的上游可以持续高速输出把进程内存打爆。
const MaxOutboundBodySize = 8 << 20

// outboundDialTimeout 建立出站连接的超时
const outboundDialTimeout = 10 * time.Second

// blockedRanges 额外需要拦截的网段（IsPrivate/IsLoopback 等未覆盖的部分）
var blockedRanges = func() []*net.IPNet {
	cidrs := []string{
		"0.0.0.0/8",       // 本网络
		"100.64.0.0/10",   // 运营商级 NAT (CGNAT)
		"192.0.0.0/24",    // IETF 协议保留
		"192.0.2.0/24",    // TEST-NET-1
		"198.18.0.0/15",   // 基准测试
		"198.51.100.0/24", // TEST-NET-2
		"203.0.113.0/24",  // TEST-NET-3
		"240.0.0.0/4",     // 保留
		"::/128",          // IPv6 未指定
		"64:ff9b::/96",    // IPv4/IPv6 转换
		"2001:db8::/32",   // IPv6 文档用
		"2002::/16",       // 6to4
	}
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		if _, n, err := net.ParseCIDR(c); err == nil {
			nets = append(nets, n)
		}
	}
	return nets
}()

// metadataIPs 云厂商元数据服务地址，SSRF 的经典首要目标
var metadataIPs = []net.IP{
	net.ParseIP("169.254.169.254"), // AWS/GCP/Azure/阿里云
	net.ParseIP("169.254.170.2"),   // AWS ECS
	net.ParseIP("100.100.100.200"), // 阿里云
	net.ParseIP("fd00:ec2::254"),   // AWS IPv6
}

// IsBlockedOutboundIP 判断目标 IP 是否禁止出站访问（内网/回环/保留/元数据地址）
func IsBlockedOutboundIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return true
	}
	for _, meta := range metadataIPs {
		if ip.Equal(meta) {
			return true
		}
	}
	for _, n := range blockedRanges {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ValidateOutboundBaseURL 校验供应商 BaseURL 是否可用于出站请求。
// 该地址会承载解密后的供应商 API Key，因此必须阻断 SSRF：
// 禁止非 http(s) 协议、禁止解析到内网/回环/链路本地/元数据地址。
// 注意：此处校验发生在保存配置时，实际连接时还会有 SafeDialContext 二次校验
// （防 DNS rebinding——保存时解析到公网、调用时解析到内网）。
func ValidateOutboundBaseURL(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return errors.New("地址不能为空")
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("地址格式无效: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("仅支持 http/https 协议，当前为 %q", u.Scheme)
	}
	if u.User != nil {
		return errors.New("地址中不允许包含用户名/密码")
	}

	host := u.Hostname()
	if host == "" {
		return errors.New("地址缺少主机名")
	}

	// 主机名本身是 IP 时可直接判断
	if ip := net.ParseIP(host); ip != nil {
		if IsBlockedOutboundIP(ip) {
			return fmt.Errorf("目标地址 %s 属于内网或保留地址，禁止使用", ip)
		}
		return nil
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("主机名无法解析: %w", err)
	}
	if len(ips) == 0 {
		return errors.New("主机名未解析到任何地址")
	}
	for _, ip := range ips {
		if IsBlockedOutboundIP(ip) {
			return fmt.Errorf("主机 %s 解析到内网或保留地址 %s，禁止使用", host, ip)
		}
	}
	return nil
}

// SafeDialContext 在真正建立 TCP 连接前校验解析出的 IP，
// 阻断 DNS rebinding（配置时解析到公网、请求时解析到内网）。
func SafeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return safeDial(ctx, network, addr, nil)
}

// safeDial 是 SafeDialContext 的实现。allowedIPs 非空时额外放行其中的地址——
// 仅用于出站代理：代理通常部署在内网，此时拨号的终点是代理本身而非目标站点，
// 用目标站点的规则去判定代理地址会把所有出站流量误杀。
func safeDial(ctx context.Context, network, addr string, allowedIPs map[string]bool) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, ipAddr := range ips {
		if IsBlockedOutboundIP(ipAddr.IP) && !allowedIPs[ipAddr.IP.String()] {
			lastErr = fmt.Errorf("拒绝连接内网或保留地址 %s", ipAddr.IP)
			continue
		}
		dialer := &net.Dialer{Timeout: outboundDialTimeout}
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ipAddr.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("目标主机无可用地址")
	}
	return nil, lastErr
}

// outboundResponseHeaderTimeout 等待上游响应头的超时。
// 客户端级不设 Timeout：整次调用的时限由调用方通过 context 控制，
// 这样同一份连接池能同时服务耗时不同的调用（提交任务 / 查询状态 / 上传图片）。
const outboundResponseHeaderTimeout = 30 * time.Second

// OutboundProxyEnv 出站代理的环境变量名。
//
// 刻意不复用 HTTP_PROXY / HTTPS_PROXY：环境里意外存在的代理变量会让**所有**
// 出站流量（含随请求发出的供应商 API Key）静默改道，生产上极难定位。
// 需要代理时必须显式设置本变量，启动日志会打印实际生效的代理。
const OutboundProxyEnv = "OUTBOUND_PROXY"

var (
	outboundClientOnce sync.Once
	outboundClient     *http.Client

	fileDownloadClientOnce sync.Once
	fileDownloadClient     *http.Client

	trustedEndpointClientOnce sync.Once
	trustedEndpointClient     *http.Client
)

// outboundClientOverride 测试注入用，生产运行期间始终为 nil。
var outboundClientOverride atomic.Pointer[http.Client]

// fileDownloadClientOverride 测试注入用，生产运行期间始终为 nil。
var fileDownloadClientOverride atomic.Pointer[http.Client]

// SetOutboundClientForTest 替换出站客户端，**仅供测试使用**，传 nil 恢复默认。
//
// 存在的理由：SSRF 防护会拒绝回环地址，而 httptest 起的假上游恰恰跑在回环上，
// 测试因此无法走真实拨号路径。生产代码不得调用本函数。
func SetOutboundClientForTest(c *http.Client) {
	outboundClientOverride.Store(c)
}

// SetFileDownloadClientForTest 替换公开文件下载客户端，**仅供测试使用**，
// 传 nil 恢复默认。理由同 SetOutboundClientForTest。
func SetFileDownloadClientForTest(c *http.Client) {
	fileDownloadClientOverride.Store(c)
}

// newOutboundTransport 构造带 SSRF 拨号校验与连接池配置的 Transport。
// 两个出站客户端共用同一套配置，避免其中一份漏掉 IdleConnTimeout 之类的
// 关键项（见 OutboundHTTPClient 注释）。
func newOutboundTransport(dial func(context.Context, string, string) (net.Conn, error), proxyURL *url.URL) *http.Transport {
	return &http.Transport{
		DialContext:           dial,
		Proxy:                 http.ProxyURL(proxyURL),
		TLSHandshakeTimeout:   outboundDialTimeout,
		ResponseHeaderTimeout: outboundResponseHeaderTimeout,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   16,
		ExpectContinueTimeout: time.Second,
		ForceAttemptHTTP2:     true,
	}
}

// outboundDialer 返回拨号函数与解析后的代理配置
func outboundDialer() (func(context.Context, string, string) (net.Conn, error), *url.URL) {
	proxyURL := parseOutboundProxy()
	if proxyURL != nil {
		return proxyDialContext(proxyURL), proxyURL
	}
	return SafeDialContext, nil
}

// OutboundHTTPClient 返回进程级共享的出站 HTTP 客户端。
//
// 必须是共享的单例：http.Transport 内部就是连接池，每次调用新建一个 Transport
// 等于每次请求都重新做 TCP + TLS 握手，keep-alive 完全失效。更隐蔽的是，
// 手工构造的 Transport 若未设 IdleConnTimeout，其值为 0，而 Go 只在
// IdleConnTimeout > 0 时才启动空闲回收定时器——于是每条用完的连接都要等上游
// 主动关闭才释放，读写 goroutine 与缓冲区在此期间一起挂着，高并发下会耗尽
// 文件描述符与临时端口，届时所有上游调用失败。
//
// 返回的客户端带 SSRF 防护：
//   - 拨号前校验目标 IP（含 DNS rebinding 防护）
//   - 拒绝跨主机重定向（API Key 会随请求发出，跳转即泄露）
//   - 拒绝重定向降级到非 https
func OutboundHTTPClient() *http.Client {
	if c := outboundClientOverride.Load(); c != nil {
		return c
	}

	outboundClientOnce.Do(func() {
		dial, proxyURL := outboundDialer()

		outboundClient = &http.Client{
			Transport: newOutboundTransport(dial, proxyURL),
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return errors.New("重定向次数过多")
				}
				origin := via[0].URL
				if req.URL.Host != origin.Host {
					return fmt.Errorf("拒绝跨主机重定向: %s -> %s", origin.Host, req.URL.Host)
				}
				if origin.Scheme == "https" && req.URL.Scheme != "https" {
					return errors.New("拒绝重定向降级到非 HTTPS")
				}
				return nil
			},
		}
	})
	return outboundClient
}

// FileDownloadHTTPClient 返回拉取公开文件内容用的出站客户端。
//
// 与 OutboundHTTPClient 的差别只有重定向策略：这里不带任何凭证，
// 而对象存储/CDN 经常 302 到另一个域名，禁止跨主机重定向会让正常的
// 文件读取失败。SSRF 防线不变——拨号仍然经过 SafeDialContext，
// 且重定向只允许 http(s)、最多 3 次。
func FileDownloadHTTPClient() *http.Client {
	if c := fileDownloadClientOverride.Load(); c != nil {
		return c
	}

	fileDownloadClientOnce.Do(func() {
		dial, proxyURL := outboundDialer()

		fileDownloadClient = &http.Client{
			Transport: newOutboundTransport(dial, proxyURL),
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return errors.New("重定向次数过多")
				}
				if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
					return fmt.Errorf("拒绝重定向到非 http(s) 协议: %s", req.URL.Scheme)
				}
				return nil
			},
		}
	})
	return fileDownloadClient
}

// TrustedEndpointHTTPClient 返回访问「运维配置的可信端点」用的出站客户端。
//
// 与 FileDownloadHTTPClient 的唯一区别是**不做目标 IP 的内网/保留地址拦截**。
// 原因：腾讯云主机访问 COS 时，桶域名会被解析到 169.254.0.0/16（链路本地，
// 走云内网直连，免流量费），而通用 SSRF 规则会把这种正常解析当成内网访问拒绝，
// 表现为上传报「拒绝连接内网或保留地址 169.254.0.x」。
//
// 使用边界：只能用于地址来自运维配置或本服务自己写入的记录的场景——
// TENCENT_COS_BUCKET_URL、files.service_address 以及据此签发的预签名地址。
// 用户可控的 URL（上游成图地址、历史外部地址）必须继续走 FileDownloadHTTPClient，
// 否则 SSRF 防护就被绕过了。
func TrustedEndpointHTTPClient() *http.Client {
	trustedEndpointClientOnce.Do(func() {
		proxyURL := parseOutboundProxy()

		var dial func(context.Context, string, string) (net.Conn, error)
		if proxyURL != nil {
			dial = proxyDialContext(proxyURL)
		} else {
			dialer := &net.Dialer{Timeout: outboundDialTimeout, KeepAlive: 30 * time.Second}
			dial = dialer.DialContext
		}

		trustedEndpointClient = &http.Client{
			Transport: newOutboundTransport(dial, proxyURL),
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return errors.New("重定向次数过多")
				}
				if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
					return fmt.Errorf("拒绝重定向到非 http(s) 协议: %s", req.URL.Scheme)
				}
				// 对象存储请求可能携带签名头，跨主机重定向等于把凭证交给另一个主机。
				if req.URL.Host != via[0].URL.Host {
					return fmt.Errorf("拒绝跨主机重定向: %s -> %s", via[0].URL.Host, req.URL.Host)
				}
				return nil
			},
		}
	})
	return trustedEndpointClient
}

// parseOutboundProxy 解析 OUTBOUND_PROXY。
// 配置了却解析不了属于配置错误，直接拒绝启动优于静默直连——
// 静默直连会表现为「所有上游调用超时」，而日志里看不出任何原因。
func parseOutboundProxy() *url.URL {
	raw := strings.TrimSpace(os.Getenv(OutboundProxyEnv))
	if raw == "" {
		return nil
	}

	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		FatalLog(fmt.Sprintf("%s 配置无效: %q（应形如 http://10.0.0.1:3128）", OutboundProxyEnv, raw))
		return nil
	}
	if u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" {
		FatalLog(fmt.Sprintf("%s 仅支持 http/https/socks5，当前为 %q", OutboundProxyEnv, u.Scheme))
		return nil
	}

	SysLogf("[安全] 出站流量将经代理转发: %s（目标地址不再逐 IP 校验，请确保代理侧有出网管控）", u.Redacted())
	return u
}

// proxyDialContext 返回「拨向代理」的拨号函数。
// 走代理时拨号的终点是代理本身，因此只放行代理的地址，其余一律按原有规则拦截；
// 目标站点的合法性此时无法在校验，这正是启用代理必须显式配置的原因。
func proxyDialContext(proxyURL *url.URL) func(context.Context, string, string) (net.Conn, error) {
	allowed := map[string]bool{}
	host := proxyURL.Hostname()

	if ip := net.ParseIP(host); ip != nil {
		allowed[ip.String()] = true
	} else if ips, err := net.LookupIP(host); err == nil {
		for _, ip := range ips {
			allowed[ip.String()] = true
		}
	} else {
		SysErrorf("[安全] 代理主机 %s 解析失败，将按通用规则校验: %v", host, err)
	}

	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return safeDial(ctx, network, addr, allowed)
	}
}
