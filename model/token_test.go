package model

import (
	"strings"
	"testing"
)

// ── API Key 哈希 ──

// 哈希是认证时唯一的查找依据：必须稳定，且不同 key 不能撞在一起。
func TestHashAPIKeyIsStableAndDistinct(t *testing.T) {
	key := "sk-abcdefghijklmnopqrstuvwxyz012345"

	if HashAPIKey(key) != HashAPIKey(key) {
		t.Error("同一 key 两次哈希结果不一致")
	}
	if HashAPIKey(key) == HashAPIKey(key+"x") {
		t.Error("不同 key 得到相同哈希")
	}
	if got := len(HashAPIKey(key)); got != 64 {
		t.Errorf("SHA-256 十六进制长度应为 64，得到 %d", got)
	}
}

// ── 列表展示用的前后缀 ──

// 正常 key 切出前 8 后 4，且两者不得覆盖整个 key——那等于把明文又发了一遍。
func TestAPIKeyDisplayPartsOnFullKey(t *testing.T) {
	full := "sk-" + strings.Repeat("a", 32)

	prefix, suffix := APIKeyDisplayParts(full)
	if prefix != full[:8] {
		t.Errorf("prefix = %q, want %q", prefix, full[:8])
	}
	if suffix != full[len(full)-4:] {
		t.Errorf("suffix = %q, want %q", suffix, full[len(full)-4:])
	}
	if len(prefix)+len(suffix) >= len(full) {
		t.Errorf("前后缀合计 %d 位覆盖了整个 key（%d 位）", len(prefix)+len(suffix), len(full))
	}
}

// 短于前后缀之和的 key（历史遗留、人工插入）必须整体作为前缀返回：
// 既不越界 panic，也不让前后缀重叠出「看起来是完整 key」的结果。
func TestAPIKeyDisplayPartsOnShortKey(t *testing.T) {
	for _, short := range []string{"", "sk-", "sk-abc", "sk-abcdefgh"} {
		prefix, suffix := APIKeyDisplayParts(short)
		if prefix != short || suffix != "" {
			t.Errorf("短 key %q 被切成 prefix=%q suffix=%q，应整体作为前缀", short, prefix, suffix)
		}
	}
}

func TestMaskAPIKey(t *testing.T) {
	cases := []struct {
		prefix, suffix, want string
	}{
		{"sk-1hi2v", "kz9e", "sk-1hi2v…kz9e"},
		{"", "", ""},
		{"sk-short", "", "sk-short"}, // 没有后缀时不该留下一个孤零零的省略号
	}
	for _, tc := range cases {
		if got := MaskAPIKey(tc.prefix, tc.suffix); got != tc.want {
			t.Errorf("MaskAPIKey(%q, %q) = %q, want %q", tc.prefix, tc.suffix, got, tc.want)
		}
	}
}
