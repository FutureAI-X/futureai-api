package videoparam

import (
	"strings"
	"testing"
)

func mustSpec(t *testing.T) *Spec {
	t.Helper()
	spec, err := ParseSpec(validSpecJSON)
	if err != nil {
		t.Fatalf("准备规格失败: %v", err)
	}
	return spec
}

func validValues() Values {
	return Values{
		"prompt":     "一只猫在窗边",
		"resolution": "2K",
		"duration":   5,
		"ratio":      "16:9",
	}
}

func TestValidateParams(t *testing.T) {
	spec := mustSpec(t)

	if err := spec.Validate(validValues()); err != nil {
		t.Fatalf("合法参数不应报错: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(Values)
		code   string
	}{
		{"缺少 prompt", func(v Values) { delete(v, "prompt") }, "PARAM_REQUIRED"},
		{"prompt 过长", func(v Values) { v["prompt"] = strings.Repeat("a", 2001) }, "PARAM_TOO_LONG"},
		{"分辨率不支持", func(v Values) { v["resolution"] = "1K" }, "PARAM_UNSUPPORTED_VALUE"},
		{"分辨率大小写归一", func(v Values) { v["resolution"] = "2k" }, ""},
		{"duration 小于下限", func(v Values) { v["duration"] = 3 }, "PARAM_TOO_SMALL"},
		{"duration 大于上限", func(v Values) { v["duration"] = 16 }, "PARAM_TOO_LARGE"},
		{"duration 非整数", func(v Values) { v["duration"] = 5.5 }, "PARAM_TYPE"},
		{"比例不支持", func(v Values) { v["ratio"] = "2:1" }, "PARAM_UNSUPPORTED_VALUE"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values := validValues()
			tc.mutate(values)
			err := spec.Validate(values)
			if tc.code == "" {
				if err != nil {
					t.Fatalf("不应报错: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("应当返回 %s", tc.code)
			}
			if err.Code != tc.code {
				t.Errorf("code = %s, want %s（%s）", err.Code, tc.code, err.Message)
			}
		})
	}
}

func validImageFile(id string) RefFile {
	return RefFile{
		ID:          id,
		Bytes:       1 << 20,
		ContentType: "image/jpeg",
		Width:       1080,
		Height:      1080,
	}
}

func validVideoFile(id string) RefFile {
	return RefFile{
		ID:          id,
		Bytes:       10 << 20,
		ContentType: "video/mp4",
		Width:       1920,
		Height:      1080,
		DurationMS:  5000,
		FPS:         30,
		VideoCodecs: []string{"h264"},
		AudioCodecs: []string{"aac"},
	}
}

func TestValidateRefMediaImage(t *testing.T) {
	param := mustSpec(t).ParamByPath("image_ids")

	if err := ValidateRefMedia(param, []RefFile{validImageFile("file_a")}); err != nil {
		t.Fatalf("合法图片不应报错: %v", err)
	}

	files := make([]RefFile, 10)
	for i := range files {
		files[i] = validImageFile("file_a")
	}
	if err := ValidateRefMedia(param, files); err == nil || err.Code != "PARAM_REF_TOO_MANY" {
		t.Errorf("超过张数应报 PARAM_REF_TOO_MANY，实际 %+v", err)
	}

	f := validImageFile("file_a")
	f.ContentType = "image/gif"
	if err := ValidateRefMedia(param, []RefFile{f}); err == nil || err.Code != "PARAM_REF_TYPE_UNSUPPORTED" {
		t.Errorf("错误格式应报 PARAM_REF_TYPE_UNSUPPORTED，实际 %+v", err)
	}

	f = validImageFile("file_a")
	f.Bytes = MaxImageBytes + 1
	if err := ValidateRefMedia(param, []RefFile{f}); err == nil || err.Code != "PARAM_REF_FILE_TOO_LARGE" {
		t.Errorf("超限大小应报 PARAM_REF_FILE_TOO_LARGE，实际 %+v", err)
	}

	f = validImageFile("file_a")
	f.Width, f.Height = 3000, 1000 // 3.0 > 2.5
	if err := ValidateRefMedia(param, []RefFile{f}); err == nil || err.Code != "PARAM_REF_ASPECT_OUT_OF_RANGE" {
		t.Errorf("宽高比超限应报 PARAM_REF_ASPECT_OUT_OF_RANGE，实际 %+v", err)
	}

	f = validImageFile("file_a")
	f.Width, f.Height = 128, 128 // 低于 256
	if err := ValidateRefMedia(param, []RefFile{f}); err == nil || err.Code != "PARAM_REF_DIMENSION_OUT_OF_RANGE" {
		t.Errorf("宽高超限应报 PARAM_REF_DIMENSION_OUT_OF_RANGE，实际 %+v", err)
	}
}

func TestValidateRefMediaAudioDuration(t *testing.T) {
	param := mustSpec(t).ParamByPath("audio_ids")

	audio := func(ms int64) RefFile {
		return RefFile{ID: "file_a", Bytes: 1 << 20, ContentType: "audio/mpeg", DurationMS: ms}
	}

	if err := ValidateRefMedia(param, []RefFile{audio(1500)}); err != nil {
		t.Fatalf("1.5 秒向上取整为 2 秒，应在范围内: %v", err)
	}
	if err := ValidateRefMedia(param, []RefFile{audio(1000)}); err == nil || err.Code != "PARAM_REF_DURATION_OUT_OF_RANGE" {
		t.Errorf("1 秒低于下限应报错，实际 %+v", err)
	}

	// 两条 8 秒音频：单条合法，但总时长 16 秒超过 15 秒。
	files := []RefFile{audio(8000), audio(8000)}
	if err := ValidateRefMedia(param, files); err == nil || err.Code != "PARAM_REF_TOTAL_DURATION_EXCEEDED" {
		t.Errorf("总时长超限应报错，实际 %+v", err)
	}
}

func TestValidateRefMediaVideoConstraints(t *testing.T) {
	param := mustSpec(t).ParamByPath("video_ids")

	if err := ValidateRefMedia(param, []RefFile{validVideoFile("file_v")}); err != nil {
		t.Fatalf("合法视频不应报错: %v", err)
	}

	f := validVideoFile("file_v")
	f.FPS = 120
	if err := ValidateRefMedia(param, []RefFile{f}); err == nil || err.Code != "PARAM_REF_FPS_OUT_OF_RANGE" {
		t.Errorf("帧率超限应报错，实际 %+v", err)
	}

	f = validVideoFile("file_v")
	f.VideoCodecs = []string{"av1"}
	if err := ValidateRefMedia(param, []RefFile{f}); err == nil || err.Code != "PARAM_REF_CODEC_UNSUPPORTED" {
		t.Errorf("视频编码不支持应报错，实际 %+v", err)
	}

	f = validVideoFile("file_v")
	f.AudioCodecs = []string{"opus"}
	if err := ValidateRefMedia(param, []RefFile{f}); err == nil || err.Code != "PARAM_REF_CODEC_UNSUPPORTED" {
		t.Errorf("音频编码不支持应报错，实际 %+v", err)
	}

	// 没有音频流是合法的；配置了 audio_codecs 不代表必须有音频。
	f = validVideoFile("file_v")
	f.AudioCodecs = nil
	if err := ValidateRefMedia(param, []RefFile{f}); err != nil {
		t.Errorf("无音频流不应报错: %v", err)
	}

	// 别名应被视为同一编码。
	f = validVideoFile("file_v")
	f.VideoCodecs = []string{"avc1"}
	f.AudioCodecs = []string{"aac"}
	if err := ValidateRefMedia(param, []RefFile{f}); err != nil {
		t.Errorf("编码别名应被接受: %v", err)
	}
}

func TestCountChars(t *testing.T) {
	cases := []struct {
		text string
		mode string
		want int
	}{
		{"你好abc", "codepoint", 5},
		{"你好abc", "utf16", 5},
		{"你好abc", "gbk_bytes", 7},
		{"你好abc", "cjk_weighted", 7},
		{"😀", "codepoint", 1},
		{"😀", "utf16", 2},
		{"😀", "cjk_weighted", 1},
		{"，a", "gbk_bytes", 3},
		{"，a", "cjk_weighted", 2},
	}
	for _, tc := range cases {
		if got := CountChars(tc.text, tc.mode); got != tc.want {
			t.Errorf("CountChars(%q, %s) = %d, want %d", tc.text, tc.mode, got, tc.want)
		}
	}
}

func TestCeilSeconds(t *testing.T) {
	cases := map[int64]int{0: 0, -1: 0, 1: 1, 999: 1, 1000: 1, 1001: 2, 2001: 3}
	for ms, want := range cases {
		if got := ceilSeconds(ms); got != want {
			t.Errorf("ceilSeconds(%d) = %d, want %d", ms, got, want)
		}
	}
}
