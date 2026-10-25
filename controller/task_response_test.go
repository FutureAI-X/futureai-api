package controller

import (
	"testing"
)

func TestApplyPublicFileURLWithFileID(t *testing.T) {
	t.Setenv(publicBaseURLEnv, "https://futureaiapi.com")
	data := map[string]interface{}{
		"url":      "https://vendor.example.com/result.png",
		"b64_json": "AAAA",
	}

	got := applyPublicFileURL(data, "file_abc.png")

	wantURL := "https://futureaiapi.com/v1/images/get/file_abc.png"
	if got["url"] != wantURL {
		t.Errorf("url = %v, want %s", got["url"], wantURL)
	}
	if got["url_vendor"] != "https://vendor.example.com/result.png" {
		t.Errorf("url_vendor = %v, 未保留上游原始地址", got["url_vendor"])
	}
	if got["b64_json"] != "AAAA" {
		t.Errorf("b64_json 被意外修改: %v", got["b64_json"])
	}
}

func TestApplyPublicFileURLWithoutFileID(t *testing.T) {
	t.Setenv(publicBaseURLEnv, "https://futureaiapi.com")
	vendorURL := "https://vendor.example.com/result.png"
	data := map[string]interface{}{"url": vendorURL}

	got := applyPublicFileURL(data, "")

	if got["url"] != vendorURL {
		t.Errorf("没有 file_id 时 url 应保持原值, got %v", got["url"])
	}
	if got["url_vendor"] != vendorURL {
		t.Errorf("url_vendor = %v, want %s", got["url_vendor"], vendorURL)
	}
}

func TestApplyPublicFileURLWithoutBaseURL(t *testing.T) {
	t.Setenv(publicBaseURLEnv, "")
	vendorURL := "https://vendor.example.com/result.png"
	data := map[string]interface{}{"url": vendorURL}

	got := applyPublicFileURL(data, "file_abc.png")

	if got["url"] != vendorURL {
		t.Errorf("未配置对外地址时 url 应回落到上游原始地址, got %v", got["url"])
	}
	if got["url_vendor"] != vendorURL {
		t.Errorf("url_vendor = %v, want %s", got["url_vendor"], vendorURL)
	}
}

func TestApplyPublicFileURLNilData(t *testing.T) {
	t.Setenv(publicBaseURLEnv, "")

	if got := applyPublicFileURL(nil, ""); got != nil {
		t.Errorf("无 file_id 且无 data 时应保持 nil, got %v", got)
	}
	if got := applyPublicFileURL(nil, "file_abc.png"); got != nil {
		t.Errorf("无法构造公开地址时应保持 nil, got %v", got)
	}

	t.Setenv(publicBaseURLEnv, "https://futureaiapi.com")
	got := applyPublicFileURL(nil, "file_abc.png")
	if got == nil {
		t.Fatal("有 file_id 且有对外地址时应创建 data")
	}
	want := "https://futureaiapi.com/v1/images/get/file_abc.png"
	if got["url"] != want {
		t.Errorf("url = %v, want %s", got["url"], want)
	}
	if _, ok := got["url_vendor"]; ok {
		t.Error("原本没有 url 时不应写入 url_vendor")
	}
}

func TestPublicFileURL(t *testing.T) {
	cases := []struct {
		name string
		base string
		want string
	}{
		{"完整 origin", "https://futureaiapi.com", "https://futureaiapi.com/v1/images/get/file_abc.png"},
		{"末尾斜杠被去掉", "https://futureaiapi.com/", "https://futureaiapi.com/v1/images/get/file_abc.png"},
		{"裸域名按 https 处理", "futureaiapi.com", "https://futureaiapi.com/v1/images/get/file_abc.png"},
		{"本地 http", "http://localhost:3001", "http://localhost:3001/v1/images/get/file_abc.png"},
		{"多余路径被忽略", "https://futureaiapi.com/gateway", "https://futureaiapi.com/v1/images/get/file_abc.png"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(publicBaseURLEnv, tc.base)
			if got := publicFileURL("file_abc.png"); got != tc.want {
				t.Errorf("publicFileURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPublicFileURLInvalidConfig(t *testing.T) {
	for _, raw := range []string{"", "   ", "ftp://example.com", "://missing-host"} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv(publicBaseURLEnv, raw)
			if got := publicFileURL("file_abc.png"); got != "" {
				t.Errorf("非法配置 %q 应返回空串, got %q", raw, got)
			}
		})
	}
}

func TestPublicFileURLEscapesFileID(t *testing.T) {
	t.Setenv(publicBaseURLEnv, "https://futureaiapi.com")

	want := "https://futureaiapi.com/v1/images/get/a%20b%2Fc"
	if got := publicFileURL("a b/c"); got != want {
		t.Errorf("publicFileURL() = %q, want %q", got, want)
	}
}
