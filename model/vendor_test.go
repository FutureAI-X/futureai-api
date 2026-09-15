package model

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestVendorJSONNeverContainsAPIKey 是 C1 的回归测试。
// /api/pricing 是无需认证的公开接口，此前直接序列化整个 Vendor 模型，
// 把每个上游供应商 API Key 的密文连同 base_url 一并泄露给匿名调用方。
func TestVendorJSONNeverContainsAPIKey(t *testing.T) {
	v := Vendor{
		ID:          1,
		Name:        "APIMart",
		Description: "test vendor",
		BaseURL:     "https://api.example.com",
		APIKey:      "super-secret-ciphertext-do-not-leak",
		Status:      1,
	}

	encoded, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	out := string(encoded)

	if strings.Contains(out, "super-secret-ciphertext-do-not-leak") {
		t.Errorf("Vendor 序列化结果泄露了 APIKey: %s", out)
	}
	if strings.Contains(out, "api_key") {
		t.Errorf("Vendor 序列化结果包含 api_key 字段: %s", out)
	}
}

// 说明：随 /api/pricing 移除 vendors 字段，PublicVendor DTO 及其两个字段白名单测试
// 已一并删除。上面 TestVendorJSONNeverContainsAPIKey 才是 C1 的回归测试——它锁定的是
// Vendor 自身的 APIKey 带 json:"-"，与 DTO 存废无关，必须保留。
