package common

import "strings"

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
