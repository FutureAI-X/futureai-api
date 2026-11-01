package controller

import (
	"strings"
	"testing"

	"github.com/FutureAI-X/futureai-api/model"
)

func validVideoGenerateRequest() videoGenerateRequest {
	return videoGenerateRequest{
		Model:      "MiniMax-H3",
		Prompt:     "a cat walking",
		Images:     []string{"file_image1.png"},
		Videos:     []string{"file_video1.mp4"},
		Audios:     []string{"file_audio1.mp3"},
		Resolution: "2K",
		Duration:   5,
		Ratio:      "adaptive",
	}
}

func TestVideoGenerateRequestValidate(t *testing.T) {
	valid := validVideoGenerateRequest()
	if err := valid.validate(); err != nil {
		t.Fatalf("合法请求不应报错: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*videoGenerateRequest)
		want   string
	}{
		{"缺少 model", func(r *videoGenerateRequest) { r.Model = "  " }, "model"},
		{"缺少 prompt", func(r *videoGenerateRequest) { r.Prompt = "  " }, "prompt"},
		{"缺少 resolution", func(r *videoGenerateRequest) { r.Resolution = "" }, "resolution"},
		{"duration 为 0", func(r *videoGenerateRequest) { r.Duration = 0 }, "duration"},
		{"duration 为负", func(r *videoGenerateRequest) { r.Duration = -1 }, "duration"},
		{"缺少 ratio", func(r *videoGenerateRequest) { r.Ratio = "" }, "ratio"},
		{"images 空 file_id", func(r *videoGenerateRequest) {
			r.Images = []string{"  "}
		}, "空的 file_id"},
		{"videos 传 URL", func(r *videoGenerateRequest) {
			r.Videos = []string{"https://example.com/a.mp4"}
		}, "不接受 URL"},
		{"audios 传非 file_id", func(r *videoGenerateRequest) {
			r.Audios = []string{"abc"}
		}, "无效的 file_id"},
		{"file_id 过长", func(r *videoGenerateRequest) {
			r.Images = []string{"file_" + strings.Repeat("a", model.MaxFileIDLen)}
		}, "过长"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validVideoGenerateRequest()
			tc.mutate(&req)
			err := req.validate()
			if err == nil {
				t.Fatalf("应当返回错误，实际通过")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("错误信息 = %q，应包含 %q", err.Error(), tc.want)
			}
		})
	}
}

// prompt 必填，三个媒体数组可以为空（纯文生视频）。
func TestVideoGenerateRequestValidateAllowsTextOnly(t *testing.T) {
	req := validVideoGenerateRequest()
	req.Images = nil
	req.Videos = nil
	req.Audios = nil

	if err := req.validate(); err != nil {
		t.Fatalf("纯文生视频不应报错: %v", err)
	}
}

func TestDedupeVideoFileIDs(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil", nil, nil},
		{"保持顺序", []string{"file_a", "file_b"}, []string{"file_a", "file_b"}},
		{"去重", []string{"file_a", "file_b", "file_a", "file_b"}, []string{"file_a", "file_b"}},
		{"去空白", []string{" file_a ", "file_a"}, []string{"file_a"}},
		{"跳过空串", []string{"", "file_a", "  "}, []string{"file_a"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := dedupeVideoFileIDs(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("dedupe = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("dedupe[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}
