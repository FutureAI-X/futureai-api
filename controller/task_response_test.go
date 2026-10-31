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

// 视频任务对外只返回 url；其余元数据不进入公开响应。
func TestVideoTaskDataOnlyExposesURL(t *testing.T) {
	queryResponse := `{
		"url": "https://files.metaso.cn/video.mp4",
		"resolution": "480P",
		"duration": 5,
		"ratio": "16:9",
		"usage": {"total_seconds": 5},
		"vendor_task_id": "2116517652677840896",
		"model": "MiniMax-H3"
	}`

	got := videoTaskData(queryResponse)

	data, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("data = %v, want map", got)
	}
	if len(data) != 1 {
		t.Errorf("data 只应包含 url，实际 %v", data)
	}
	if data["url"] != "https://files.metaso.cn/video.mp4" {
		t.Errorf("url = %v", data["url"])
	}
	for _, key := range []string{"resolution", "duration", "ratio", "usage", "vendor_task_id", "model"} {
		if _, exists := data[key]; exists {
			t.Errorf("不应对外返回字段 %q", key)
		}
	}
}

// 失败任务保留上游错误，便于调用方判断原因。
func TestVideoTaskDataFailedCarriesError(t *testing.T) {
	got := videoTaskData(`{"error":{"code":"1026","message":"sensitive content"}}`)

	data, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("data = %v, want map", got)
	}
	errInfo, ok := data["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error = %v, want map", data["error"])
	}
	if errInfo["code"] != "1026" || errInfo["message"] != "sensitive content" {
		t.Errorf("error = %v", errInfo)
	}
}

func TestVideoTaskDataEmptyOrInvalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"空串", ""},
		{"非法 JSON", "not-json"},
		{"url 为空", `{"url":"   "}`},
		{"只有元数据", `{"resolution":"480P","duration":5}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := videoTaskData(tc.input); got != nil {
				t.Errorf("videoTaskData(%q) = %v, want nil", tc.input, got)
			}
		})
	}
}

// 同时存在 url 与 error 时以 url 为准（正常情况下不会同时出现）。
func TestVideoTaskDataPrefersURL(t *testing.T) {
	got := videoTaskData(`{"url":"https://x/v.mp4","error":"old"}`)

	data, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("data = %v, want map", got)
	}
	if _, exists := data["error"]; exists {
		t.Errorf("有 url 时不应再返回 error: %v", data)
	}
}
