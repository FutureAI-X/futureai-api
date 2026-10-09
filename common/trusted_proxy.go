package common

import (
	"fmt"
	"net"
	"strings"
)

// ParseTrustedProxies 解析 TRUSTED_PROXIES（逗号分隔的 IP 或 CIDR）。
//
// 返回空切片表示不信任任何代理头，此时 gin 使用 TCP 对端地址。
// 该值必须精确：填 0.0.0.0/0 等于信任一切，客户端自带的 X-Forwarded-For
// 重新变得可伪造，登录限流会彻底失效。
//
// 之所以从 main 挪到这里，是因为根包内嵌了 web/dist（见 webui 包），
// 未构建前端时无法编译，而 `go test ./...` 会连带整个根包一起编译失败。
// 根包因此被排除在测试之外（见 Makefile 的 test 目标），
// 与安全相关的解析逻辑必须待在能被测试覆盖的子包里。
func ParseTrustedProxies(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			result = append(result, p)
		}
	}
	return result
}

// ValidateTrustedProxies 拒绝等价于「信任一切」的网段配置。
//
// 填 0.0.0.0/0 时 gin 会采信任何客户端自带的 X-Forwarded-For，攻击者可以为每个
// 请求编造一个不同的来源 IP，登录限流随之彻底失效。危险之处在于它**看起来像是
// 配置已完成**：日志里能看到真实 IP、一切正常，只不过那个 IP 是攻击者自己填的。
// 因此这里与密钥校验保持同一个态度——fail-closed，宁可拒绝启动，也不接受一个
// 等于没配的配置。
//
// 按掩码位数判断而不是比对字面量，是为了让 0.0.0.0/00 这类等价写法也拦得住。
// 空值合法：表示不信任任何代理头，gin 直接使用 TCP 对端地址。
func ValidateTrustedProxies(proxies []string) error {
	for _, p := range proxies {
		_, ipnet, err := net.ParseCIDR(p)
		if err != nil {
			// 非法网段留给 gin 的 SetTrustedProxies 报错，这里只负责
			// 「语法合法但语义等于信任一切」这一种。
			continue
		}
		if ones, _ := ipnet.Mask.Size(); ones == 0 {
			return fmt.Errorf("TRUSTED_PROXIES 不能包含 %q：/0 等于信任一切，"+
				"客户端可伪造 X-Forwarded-For 绕过登录限流；请填反向代理的实际地址或网段", p)
		}
	}
	return nil
}
