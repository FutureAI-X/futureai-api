package supplier

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ── applyImageVersion：按调用方模型名补全 APIMart 的 version 字段 ──

func TestApplyImageVersionSetsVersionByRequestedModel(t *testing.T) {
	cases := []struct {
		name           string
		requestedModel string
		wantVersion    interface{}
		wantSet        bool
	}{
		{"flare 变体", "gpt-image-2.5-flare-ext", "flare", true},
		{"sunburst 变体", "gpt-image-2.5-sunburst-ext", "sunburst", true},
		{"未登记的调用方模型不写 version", "gpt-image-2.5-unknown-ext", nil, false},
		{"调用方模型名为空不写 version", "", nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]interface{}{
				"model":  apimartVersionedImageModel,
				"prompt": "a cat",
			}

			applyImageVersion(body, tc.requestedModel)

			got, set := body["version"]
			if set != tc.wantSet {
				t.Fatalf("version 是否写入 = %v, want %v (body=%v)", set, tc.wantSet, body)
			}
			if got != tc.wantVersion {
				t.Errorf("version = %v, want %v", got, tc.wantVersion)
			}
		})
	}
}

// 其他图像模型不带 version 参数，必须原样透传，不能被塞进一个多余的字段。
func TestApplyImageVersionLeavesOtherModelsUntouched(t *testing.T) {
	body := map[string]interface{}{
		"model":   "gpt-image-1",
		"prompt":  "a cat",
		"version": "caller-supplied",
	}

	applyImageVersion(body, "gpt-image-2.5-sunburst-ext")

	if got := body["version"]; got != "caller-supplied" {
		t.Errorf("非目标模型的 version 被改写为 %v, want 原样透传", got)
	}
	if len(body) != 3 {
		t.Errorf("请求体被改动了: %v", body)
	}
}

// 调用方自己塞 version 时以服务端推导为准：否则按 flare 计费的请求
// 可以用 sunburst 出图，绕过差异化计费。
func TestApplyImageVersionOverridesCallerSuppliedVersion(t *testing.T) {
	body := map[string]interface{}{
		"model":   apimartVersionedImageModel,
		"version": "sunburst",
	}

	applyImageVersion(body, "gpt-image-2.5-flare-ext")

	if got := body["version"]; got != "flare" {
		t.Errorf("version = %v, want flare（服务端推导优先）", got)
	}
}

// 端到端校验：version 要真的出现在发往 APIMart 的 JSON 里，
// 且不能写回调用方持有的 map（controller 之后还会用到它）。
func TestImageGenerateSendsVersionWithoutMutatingCallerBody(t *testing.T) {
	var received map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Errorf("请求路径 = %s, want /v1/images/generations", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("解析上游收到的请求体失败: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":200,"data":[{"status":"submitted","task_id":"vendor-task-1"}]}`)
	}))
	defer srv.Close()

	callerBody := map[string]interface{}{
		"model":  apimartVersionedImageModel,
		"prompt": "a cat",
	}

	// 用普通 client 绕过 SSRF 防护：SafeDialContext 会拒绝测试服务器的回环地址。
	a := &apimart{cfg: Config{BaseURL: srv.URL, APIKey: "test-key"}, client: srv.Client()}

	resp := a.ImageGenerate(ImageGenerateRequest{Model: "gpt-image-2.5-sunburst-ext", Body: callerBody})

	if resp.Code != "success" {
		t.Fatalf("resp.Code = %s, want success", resp.Code)
	}
	if resp.Data["taskId"] != "vendor-task-1" {
		t.Errorf("taskId = %v, want vendor-task-1", resp.Data["taskId"])
	}
	if got := received["version"]; got != "sunburst" {
		t.Errorf("上游收到的 version = %v, want sunburst（请求体=%v）", got, received)
	}
	if got := received["model"]; got != apimartVersionedImageModel {
		t.Errorf("上游收到的 model = %v, want %s", got, apimartVersionedImageModel)
	}
	if _, exists := callerBody["version"]; exists {
		t.Errorf("调用方的请求体被写脏了: %v", callerBody)
	}
}
