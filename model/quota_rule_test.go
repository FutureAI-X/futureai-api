package model

import (
	"strings"
	"testing"
)

// ── ParseRefImageParams：参数名配置清洗 ──

func TestParseRefImageParamsCleansInput(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{"常规逗号串", "image,images", []string{"image", "images"}},
		{"去空白丢空项", " image , , images ", []string{"image", "images"}},
		{"去重保留首次出现顺序", "b,a,b,c,a", []string{"b", "a", "c"}},
		{"单个参数名", "image", []string{"image"}},
		{"末尾多余逗号", "image,", []string{"image"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseRefImageParams(tc.raw)
			if len(got) != len(tc.want) {
				t.Fatalf("ParseRefImageParams(%q) = %v, want %v", tc.raw, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("ParseRefImageParams(%q) = %v, want %v", tc.raw, got, tc.want)
					break
				}
			}
		})
	}
}

// 空配置必须回落到内置默认值：存量规则升级后该字段为空串，
// 若不回落，参考图将永远识别不到、附加计费静默失效。
func TestParseRefImageParamsFallsBackToDefault(t *testing.T) {
	for _, raw := range []string{"", "   ", ",", " , "} {
		got := ParseRefImageParams(raw)
		if len(got) == 0 {
			t.Fatalf("ParseRefImageParams(%q) 返回空列表，应回落到默认值", raw)
		}
	}

	got := ParseRefImageParams("")
	want := strings.Split(DefaultRefImageParams, ",")
	if len(got) != len(want) {
		t.Fatalf("回落到默认值时应有 %d 项，实际 %v", len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("默认值第 %d 项 = %q, want %q", i, got[i], want[i])
		}
	}
}

// JoinRefImageParams 与 ParseRefImageParams 必须互为逆运算，
// 否则「读取→清洗→回写」会把配置越改越乱。
func TestRefImageParamsRoundTrip(t *testing.T) {
	joined := JoinRefImageParams([]string{"image", "image_urls"})
	if joined != "image,image_urls" {
		t.Fatalf("JoinRefImageParams = %q", joined)
	}

	back := ParseRefImageParams(joined)
	if len(back) != 2 || back[0] != "image" || back[1] != "image_urls" {
		t.Errorf("往返后 = %v, want [image image_urls]", back)
	}
}

// ── EffectiveRefImageParams ──

func TestEffectiveRefImageParams(t *testing.T) {
	rule := &CreditRule{RefImageParams: "ref_images"}
	got := rule.EffectiveRefImageParams()
	if len(got) != 1 || got[0] != "ref_images" {
		t.Errorf("已配置时应使用配置值，实际 %v", got)
	}

	empty := &CreditRule{}
	if len(empty.EffectiveRefImageParams()) == 0 {
		t.Error("未配置时应回落到默认值")
	}

	// nil 接收者不应 panic：GetCreditRuleByModelID 在无规则时返回 nil
	var nilRule *CreditRule
	if len(nilRule.EffectiveRefImageParams()) == 0 {
		t.Error("nil 规则应回落到默认值且不 panic")
	}
}
