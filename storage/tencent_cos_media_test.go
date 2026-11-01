package storage

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBuildPostPolicy(t *testing.T) {
	expiresAt := time.Date(2026, 11, 1, 12, 30, 0, 0, time.UTC)
	keyTime := "1762082400;1762084200"
	key := "staging/file_abc.mp4"

	policyJSON, policyB64, signature := buildPostPolicy(
		"bucket-1250000000", "secret-id", "secret-key",
		key, "video/mp4", 200<<20, keyTime, expiresAt,
	)

	var policy struct {
		Expiration string        `json:"expiration"`
		Conditions []interface{} `json:"conditions"`
	}
	if err := json.Unmarshal(policyJSON, &policy); err != nil {
		t.Fatalf("policy 不是合法 JSON: %v", err)
	}
	if policy.Expiration != "2026-11-01T12:30:00.000Z" {
		t.Errorf("expiration = %q", policy.Expiration)
	}
	if len(policy.Conditions) == 0 {
		t.Fatal("policy 缺少 conditions")
	}

	decoded, err := base64.StdEncoding.DecodeString(policyB64)
	if err != nil {
		t.Fatalf("policy base64 解码失败: %v", err)
	}
	if !bytes.Equal(decoded, policyJSON) {
		t.Errorf("policy base64 与 JSON 不一致")
	}

	// 独立复算签名，锁住算法：SignKey=hex(HMAC-SHA1(SK, KeyTime))，
	// StringToSign=hex(SHA1(Policy))，Signature=hex(HMAC-SHA1(SignKey, StringToSign))。
	signKey := hmacSHA1Hex("secret-key", keyTime)
	stringToSign := sha1Hex(policyJSON)
	wantSignature := hmacSHA1Hex(signKey, stringToSign)
	if signature != wantSignature {
		t.Errorf("signature = %q, want %q", signature, wantSignature)
	}
	if len(signature) != 40 {
		t.Errorf("signature 长度 = %d, want 40（SHA1 hex）", len(signature))
	}

	raw := string(policyJSON)
	for _, want := range []string{
		`"bucket":"bucket-1250000000"`,
		`"content-length-range"`,
		`"$key"`,
		`"$Content-Type"`,
		`"q-sign-time"`,
		`"q-ak":"secret-id"`,
		`"q-sign-algorithm":"sha1"`,
		`"success_action_status":"200"`,
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("policy 缺少 %s: %s", want, raw)
		}
	}
}

func TestPresignPost(t *testing.T) {
	s := &TencentCOS{
		bucketURL:  "https://bucket-1250000000.cos.ap-chengdu.myqcloud.com",
		bucketName: "bucket-1250000000",
		secretID:   "secret-id",
		secretKey:  "secret-key",
	}

	uploadURL, fields, expiresAt, err := s.PresignPost(
		context.Background(), "staging/file_abc.mp4", "video/mp4", 200<<20, 30*time.Minute,
	)
	if err != nil {
		t.Fatalf("PresignPost 失败: %v", err)
	}

	if uploadURL != "https://bucket-1250000000.cos.ap-chengdu.myqcloud.com/" {
		t.Errorf("uploadURL = %q", uploadURL)
	}
	for _, key := range []string{"key", "policy", "q-sign-algorithm", "q-ak", "q-key-time", "q-signature", "Content-Type", "success_action_status"} {
		if fields[key] == "" {
			t.Errorf("fields 缺少 %q", key)
		}
	}
	if fields["key"] != "staging/file_abc.mp4" {
		t.Errorf("key = %q", fields["key"])
	}
	if fields["Content-Type"] != "video/mp4" || fields["q-ak"] != "secret-id" {
		t.Errorf("字段值不正确: %v", fields)
	}
	if fields["q-sign-algorithm"] != "sha1" || fields["success_action_status"] != "200" {
		t.Errorf("签名/状态字段不正确: %v", fields)
	}
	if time.Until(expiresAt) < 20*time.Minute {
		t.Errorf("expiresAt 过早: %v", expiresAt)
	}
}

func TestParseFPS(t *testing.T) {
	cases := map[string]float64{
		"24.000000": 24,
		"30/1":      30,
		"":          0,
		"0/0":       0,
		"invalid":   0,
	}
	for raw, want := range cases {
		if got := parseFPS(raw); got != want {
			t.Errorf("parseFPS(%q) = %v, want %v", raw, got, want)
		}
	}

	if got := parseFPS("24000/1001"); got < 23.97 || got > 23.98 {
		t.Errorf("parseFPS(24000/1001) = %v, want 23.976", got)
	}
}

// 回归：CI imageInfo 实测返回顶层 JSON，且 width/height 是字符串。
func TestParseImageInfoBody(t *testing.T) {
	probe, err := parseImageInfoBody([]byte(`{
		"bit_depth": "8",
		"format": "png",
		"height": "300",
		"width": "300"
	}`))
	if err != nil {
		t.Fatalf("顶层 JSON 解析失败: %v", err)
	}
	if probe.Width != 300 || probe.Height != 300 || probe.Format != "png" {
		t.Errorf("probe = %+v", probe)
	}

	// 兼容 XML 形态。
	probe, err = parseImageInfoBody([]byte(`<Response><ImageInfo><Format>jpeg</Format><Width>800</Width><Height>600</Height></ImageInfo></Response>`))
	if err != nil {
		t.Fatalf("XML 解析失败: %v", err)
	}
	if probe.Width != 800 || probe.Height != 600 || probe.Format != "jpeg" {
		t.Errorf("probe = %+v", probe)
	}

	// 兼容包一层 imageInfo 的 JSON。
	probe, err = parseImageInfoBody([]byte(`{"imageInfo":{"format":"webp","width":640,"height":480}}`))
	if err != nil {
		t.Fatalf("嵌套 JSON 解析失败: %v", err)
	}
	if probe.Width != 640 || probe.Height != 480 || probe.Format != "webp" {
		t.Errorf("probe = %+v", probe)
	}

	if _, err := parseImageInfoBody([]byte(`not-an-image-info`)); err == nil {
		t.Error("非法响应应返回错误")
	}
}

func TestNormalizeImageFormat(t *testing.T) {
	cases := map[string]string{
		"jpg":  "jpeg",
		"JPEG": "jpeg",
		"png":  "png",
		"heic": "heic",
		"mif1": "heif",
		"webp": "webp",
		"":     "",
	}
	for raw, want := range cases {
		if got := normalizeImageFormat(raw); got != want {
			t.Errorf("normalizeImageFormat(%q) = %q, want %q", raw, got, want)
		}
	}
}
