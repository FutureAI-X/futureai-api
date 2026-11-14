package model

import (
	"testing"
)

func TestParseTokenBillingConfigDefaults(t *testing.T) {
	cfg, err := ParseTokenBillingConfig("")
	if err != nil {
		t.Fatalf("ParseTokenBillingConfig: %v", err)
	}
	if cfg.OutputPixelsPerToken != 1024 {
		t.Errorf("OutputPixelsPerToken = %d", cfg.OutputPixelsPerToken)
	}
	if cfg.PreauthMargin != 1.2 {
		t.Errorf("PreauthMargin = %v", cfg.PreauthMargin)
	}
	if got := cfg.OutputTokensFor("2720x1536", ""); got != 4080 {
		t.Errorf("OutputTokensFor = %d, want 4080", got)
	}
	if got := cfg.OutputTokensFor("", "2K"); got != 4096 {
		t.Errorf("OutputTokensFor 2K = %d, want 4096", got)
	}
}

func TestParseTokenBillingConfigOverrides(t *testing.T) {
	cfg, err := ParseTokenBillingConfig(JSONText(`{
		"output_pixels_per_token": 512,
		"ref_image_tokens": 8000,
		"preauth_margin": 1.5
	}`))
	if err != nil {
		t.Fatalf("ParseTokenBillingConfig: %v", err)
	}
	if cfg.OutputPixelsPerToken != 512 {
		t.Errorf("OutputPixelsPerToken = %d", cfg.OutputPixelsPerToken)
	}
	if cfg.RefImageTokens != 8000 {
		t.Errorf("RefImageTokens = %d", cfg.RefImageTokens)
	}
	if cfg.PreauthMargin != 1.5 {
		t.Errorf("PreauthMargin = %v", cfg.PreauthMargin)
	}
	// 未提供的字段保持默认值
	if cfg.TextTokenOverhead != 200 {
		t.Errorf("TextTokenOverhead = %d", cfg.TextTokenOverhead)
	}
}

func TestParseTokenBillingConfigRejectsInvalid(t *testing.T) {
	for _, raw := range []string{
		`{"output_pixels_per_token": 0}`,
		`{"preauth_margin": 0.9}`,
		`{"ref_image_tokens": -1}`,
		`{"unknown_field": 1}`,
	} {
		if _, err := ParseTokenBillingConfig(JSONText(raw)); err == nil {
			t.Errorf("ParseTokenBillingConfig(%s) 应该失败", raw)
		}
	}
}

func TestTokenBillingSnapshotCreditsFor(t *testing.T) {
	snapshot := TokenBillingSnapshot{
		InputTextCreditsPerMTok:   1000,
		ImageInputCreditsPerMTok:  2000,
		ImageOutputCreditsPerMTok: 3000,
	}
	usage := BillingUsage{
		TextInputTokens:   1000,
		ImageInputTokens:  2000,
		ImageOutputTokens: 3000,
	}

	// 1000*1000/1e6 + 2000*2000/1e6 + 3000*3000/1e6
	// = 1 + 4 + 9 = 14
	if got := snapshot.CreditsFor(usage); got != 14 {
		t.Errorf("CreditsFor = %v, want 14", got)
	}
}

func TestParseTokenBillingSnapshot(t *testing.T) {
	raw, err := (TokenBillingSnapshot{PreauthCredits: 5.2}).MarshalJSONText()
	if err != nil {
		t.Fatalf("MarshalJSONText: %v", err)
	}
	snapshot, ok, err := ParseTokenBillingSnapshot(raw)
	if err != nil || !ok {
		t.Fatalf("ParseTokenBillingSnapshot: ok=%v err=%v", ok, err)
	}
	if snapshot.PreauthCredits != 5.2 {
		t.Errorf("PreauthCredits = %v", snapshot.PreauthCredits)
	}

	if _, ok, err := ParseTokenBillingSnapshot(""); err != nil || ok {
		t.Errorf("空快照应返回 ok=false: ok=%v err=%v", ok, err)
	}
	if _, _, err := ParseTokenBillingSnapshot("{bad"); err == nil {
		t.Error("非法快照应返回错误")
	}
}
