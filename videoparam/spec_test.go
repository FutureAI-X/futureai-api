package videoparam

import (
	"strings"
	"testing"
)

const validSpecJSON = `{
  "version": 1,
  "params": [
    {"key":"prompt","widget":"textarea","required":true,"max_length":2000},
    {"key":"resolution","widget":"segmented","required":true,"options":["480p","768P","2k"]},
    {"key":"duration","widget":"number","required":true,"min":4,"max":15,"step":1,"integer":true},
    {"key":"ratio","widget":"segmented","required":true,"options":["21:9","16:9","4:3","1:1","3:4","9:16","adaptive"]},
    {"key":"images","path":"image_ids","widget":"ref_images","group":"reference",
     "max_count":9,"allowed_mime_types":["image/jpeg","image/png","image/heic"],
     "max_file_bytes":31457280,"aspect_ratio":{"min":0.4,"max":2.5},"dimensions":{"min":256,"max":5760}},
    {"key":"audios","path":"audio_ids","widget":"ref_audios","group":"reference",
     "max_count":3,"allowed_mime_types":["audio/wav","audio/mpeg"],
     "max_file_bytes":15728640,"duration_seconds":{"min":2,"max":15},"max_total_duration_seconds":15},
    {"key":"videos","path":"video_ids","widget":"ref_videos","group":"reference",
     "max_count":3,"allowed_mime_types":["video/mp4","video/quicktime"],
     "max_file_bytes":52428800,"duration_seconds":{"min":2,"max":15},"max_total_duration_seconds":15,
     "aspect_ratio":{"min":0.4,"max":2.5},"dimensions":{"min":256,"max":5760},
     "fps":{"min":23.976,"max":60},"video_codecs":["h264","hevc"],"audio_codecs":["aac","mp3"]}
  ]
}`

func TestParseSpecValid(t *testing.T) {
	spec, err := ParseSpec(validSpecJSON)
	if err != nil {
		t.Fatalf("合法规格不应报错: %v", err)
	}
	got := strings.Join(spec.ResolutionOptions(), ",")
	if got != "480P,768P,2K" {
		t.Errorf("分辨率未统一大写: %s", got)
	}
	ratio := spec.ParamByPath("ratio")
	if ratio == nil || ratio.Options[6] != "adaptive" {
		t.Errorf("adaptive 应保持小写: %+v", ratio)
	}
}

func TestParseSpecMissing(t *testing.T) {
	if _, err := ParseSpec(""); err != ErrSpecMissing {
		t.Errorf("空规格应返回 ErrSpecMissing，实际 %v", err)
	}
	if _, err := ParseSpec("null"); err != ErrSpecMissing {
		t.Errorf("null 规格应返回 ErrSpecMissing，实际 %v", err)
	}
}

func TestParseSpecRejectsUnknownField(t *testing.T) {
	raw := `{"params":[{"key":"prompt","widget":"textarea","required":true,"wat":1}]}`
	if _, err := ParseSpec(raw); err == nil {
		t.Fatalf("未知字段应当报错")
	}
}

func TestValidateSpecRejectsInvalid(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Spec)
		want   string
	}{
		{"缺少 prompt", func(s *Spec) { s.Params = s.Params[1:] }, "缺少必填参数 prompt"},
		{"prompt 非必填", func(s *Spec) { s.ParamByPath("prompt").Required = false }, "必须设置为必填"},
		{"duration 非整数", func(s *Spec) { s.ParamByPath("duration").Integer = false }, "integer"},
		{"duration 超出全局", func(s *Spec) {
			p := s.ParamByPath("duration")
			max := 500.0
			p.Max = &max
		}, "duration 范围"},
		{"duration min 大于 max", func(s *Spec) {
			p := s.ParamByPath("duration")
			min := 20.0
			p.Min = &min
		}, "min 不能大于 max"},
		{"参考媒体缺少 max_count", func(s *Spec) { s.ParamByPath("image_ids").MaxCount = nil }, "max_count"},
		{"参考媒体缺少 MIME", func(s *Spec) { s.ParamByPath("image_ids").AllowedMimeTypes = nil }, "allowed_mime_types"},
		{"图片不支持时长", func(s *Spec) {
			s.ParamByPath("image_ids").DurationSeconds = &RangeInt{Min: 1, Max: 2}
		}, "ref_images 只支持"},
		{"音频不支持宽高比", func(s *Spec) {
			s.ParamByPath("audio_ids").AspectRatio = &RangeFloat{Min: 0.5, Max: 2}
		}, "ref_audios 只支持"},
		{"文件大小超过全局", func(s *Spec) { s.ParamByPath("video_ids").MaxFileBytes = MaxVideoBytes + 1 }, "max_file_bytes"},
		{"宽高比超出全局", func(s *Spec) {
			s.ParamByPath("video_ids").AspectRatio = &RangeFloat{Min: 0.01, Max: 2}
		}, "aspect_ratio"},
		{"不支持的编码", func(s *Spec) { s.ParamByPath("video_ids").VideoCodecs = []string{"av1"} }, "不支持编码"},
		{"不支持的 MIME", func(s *Spec) { s.ParamByPath("image_ids").AllowedMimeTypes = []string{"image/gif"} }, "不支持的 MIME"},
		{"重复 path", func(s *Spec) {
			dup := *s.ParamByPath("prompt")
			dup.Key = "prompt2"
			dup.Path = "prompt"
			s.Params = append(s.Params, dup)
		}, "重复"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := ParseSpec(validSpecJSON)
			if err != nil {
				t.Fatalf("准备规格失败: %v", err)
			}
			tc.mutate(spec)
			err = spec.ValidateSpec()
			if err == nil {
				t.Fatalf("应当返回错误，实际通过")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("错误信息 = %q，应包含 %q", err.Error(), tc.want)
			}
		})
	}
}

func TestValidateSpecAllowsZeroMaxCount(t *testing.T) {
	spec, err := ParseSpec(validSpecJSON)
	if err != nil {
		t.Fatalf("准备规格失败: %v", err)
	}
	zero := 0
	spec.ParamByPath("audio_ids").MaxCount = &zero
	spec.ParamByPath("audio_ids").AllowedMimeTypes = nil
	if err := spec.ValidateSpec(); err != nil {
		t.Fatalf("max_count=0 表示不支持，不应要求 MIME: %v", err)
	}
}
