package controller

import (
	"strings"
	"testing"

	"github.com/FutureAI-X/futureai-api/model"
	"github.com/FutureAI-X/futureai-api/storage"
)

func TestResolveMediaSpec(t *testing.T) {
	cases := []struct {
		mediaType   string
		contentType string
		wantType    string
		wantExt     string
		wantErr     bool
	}{
		{"image", "image/jpeg", "image/jpeg", "jpg", false},
		{"image", "image/jpg", "image/jpeg", "jpg", false},
		{"image", "image/png", "image/png", "png", false},
		{"image", "image/webp", "image/webp", "webp", false},
		{"image", "image/heic", "image/heic", "heic", false},
		{"image", "image/heif", "image/heif", "heif", false},
		{"audio", "audio/wav", "audio/wav", "wav", false},
		{"audio", "audio/x-wav", "audio/wav", "wav", false},
		{"audio", "audio/mpeg", "audio/mpeg", "mp3", false},
		{"audio", "audio/mp3", "audio/mpeg", "mp3", false},
		{"video", "video/mp4", "video/mp4", "mp4", false},
		{"video", "video/quicktime", "video/quicktime", "mov", false},
		{"video", "video/mov", "video/quicktime", "mov", false},
		{"image", "video/mp4", "", "", true},
		{"audio", "image/png", "", "", true},
		{"video", "audio/mpeg", "", "", true},
		{"image", "image/gif", "", "", true},
		{"other", "image/png", "", "", true},
		{"image", "not-a-type", "", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.mediaType+"/"+tc.contentType, func(t *testing.T) {
			gotType, spec, err := resolveMediaSpec(tc.mediaType, tc.contentType)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("应当返回错误")
				}
				return
			}
			if err != nil {
				t.Fatalf("不应返回错误: %v", err)
			}
			if gotType != tc.wantType || spec.Ext != tc.wantExt {
				t.Errorf("got (%q, %q), want (%q, %q)", gotType, spec.Ext, tc.wantType, tc.wantExt)
			}
		})
	}
}

func TestDetectMediaFromBytes(t *testing.T) {
	ftyp := func(brand string) []byte {
		b := make([]byte, 16)
		copy(b[4:8], "ftyp")
		copy(b[8:12], brand)
		return b
	}
	riff := func(form string) []byte {
		b := make([]byte, 16)
		copy(b[0:4], "RIFF")
		copy(b[8:12], form)
		return b
	}

	cases := []struct {
		name        string
		data        []byte
		wantType    string
		wantContent string
		wantExt     string
		wantErr     bool
	}{
		{"JPEG", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0, 0, 0, 0, 0}, model.FileMediaTypeImage, "image/jpeg", "jpg", false},
		{"PNG", append(append([]byte{}, pngSignature...), 0, 0, 0, 0), model.FileMediaTypeImage, "image/png", "png", false},
		{"WebP", riff("WEBP"), model.FileMediaTypeImage, "image/webp", "webp", false},
		{"HEIC", ftyp("heic"), model.FileMediaTypeImage, "image/heic", "heic", false},
		{"HEIF", ftyp("mif1"), model.FileMediaTypeImage, "image/heif", "heif", false},
		{"HEIF brand", ftyp("heif"), model.FileMediaTypeImage, "image/heif", "heif", false},
		{"WAV", riff("WAVE"), model.FileMediaTypeAudio, "audio/wav", "wav", false},
		{"MP3 ID3", append([]byte("ID3"), 0, 0, 0, 0, 0, 0, 0, 0, 0), model.FileMediaTypeAudio, "audio/mpeg", "mp3", false},
		{"MP3 frame", []byte{0xFF, 0xFB, 0x90, 0x00, 0, 0, 0, 0, 0, 0, 0, 0}, model.FileMediaTypeAudio, "audio/mpeg", "mp3", false},
		{"MP4", ftyp("isom"), model.FileMediaTypeVideo, "video/mp4", "mp4", false},
		{"MOV", ftyp("qt  "), model.FileMediaTypeVideo, "video/quicktime", "mov", false},
		{"GIF", []byte("GIF89a000000"), "", "", "", true},
		{"AVIF", ftyp("avif"), "", "", "", true},
		{"M4A", ftyp("M4A "), "", "", "", true},
		{"unknown", []byte("not-a-media-file"), "", "", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := detectMediaFromBytes(tc.data, "")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("应当返回错误，实际 got=%+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("不应返回错误: %v", err)
			}
			if got.MediaType != tc.wantType || got.ContentType != tc.wantContent || got.Ext != tc.wantExt {
				t.Errorf("got %+v, want (%s,%s,%s)", got, tc.wantType, tc.wantContent, tc.wantExt)
			}
		})
	}
}

func TestValidateImageProbe(t *testing.T) {
	cases := []struct {
		name    string
		probe   *storage.ImageProbe
		wantErr bool
	}{
		{"正常", &storage.ImageProbe{Width: 1000, Height: 1000, Format: "jpeg"}, false},
		{"下限", &storage.ImageProbe{Width: 300, Height: 300, Format: "png"}, false},
		{"上限", &storage.ImageProbe{Width: 5500, Height: 5500, Format: "webp"}, false},
		{"HEIC", &storage.ImageProbe{Width: 1080, Height: 1920, Format: "heic"}, false},
		{"宽度过小", &storage.ImageProbe{Width: 299, Height: 1000, Format: "jpeg"}, true},
		{"高度过大", &storage.ImageProbe{Width: 1000, Height: 5501, Format: "jpeg"}, true},
		{"宽高比过小", &storage.ImageProbe{Width: 300, Height: 1000, Format: "jpeg"}, true},
		{"宽高比过大", &storage.ImageProbe{Width: 1000, Height: 380, Format: "jpeg"}, true},
		{"格式不支持", &storage.ImageProbe{Width: 1000, Height: 1000, Format: "gif"}, true},
		{"nil", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateImageProbe(tc.probe)
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestValidateAudioProbe(t *testing.T) {
	valid := &storage.MediaProbe{HasAudio: true, DurationMS: 5000, Format: "mp3"}
	if err := validateAudioProbe(valid); err != nil {
		t.Fatalf("合法音频不应报错: %v", err)
	}

	cases := []struct {
		name    string
		probe   *storage.MediaProbe
		wantErr bool
	}{
		{"无音频流", &storage.MediaProbe{DurationMS: 5000, Format: "mp3"}, true},
		{"时长过短", &storage.MediaProbe{HasAudio: true, DurationMS: 1999, Format: "wav"}, true},
		{"时长过长", &storage.MediaProbe{HasAudio: true, DurationMS: 30001, Format: "wav"}, true},
		{"格式不支持", &storage.MediaProbe{HasAudio: true, DurationMS: 5000, Format: "mov,mp4,m4a"}, true},
		{"nil", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAudioProbe(tc.probe)
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestValidateVideoProbe(t *testing.T) {
	valid := &storage.MediaProbe{
		HasVideo:    true,
		HasAudio:    true,
		Width:       1000,
		Height:      1000,
		DurationMS:  5000,
		FPS:         24,
		VideoCodecs: []string{"h264"},
		AudioCodecs: []string{"aac"},
		Format:      "mov,mp4,m4a,3gp,3g2,mj2",
	}
	if err := validateVideoProbe(valid); err != nil {
		t.Fatalf("合法视频不应报错: %v", err)
	}

	clone := func() *storage.MediaProbe {
		p := *valid
		p.VideoCodecs = append([]string{}, valid.VideoCodecs...)
		p.AudioCodecs = append([]string{}, valid.AudioCodecs...)
		return &p
	}

	cases := []struct {
		name    string
		mutate  func(*storage.MediaProbe)
		wantErr bool
	}{
		{"23.976 帧率接受", func(p *storage.MediaProbe) { p.FPS = 24000.0 / 1001.0 }, false},
		{"60 帧率接受", func(p *storage.MediaProbe) { p.FPS = 60 }, false},
		{"静音视频允许", func(p *storage.MediaProbe) { p.AudioCodecs = nil; p.HasAudio = false }, false},
		{"H.265 编码接受", func(p *storage.MediaProbe) { p.VideoCodecs = []string{"hevc"} }, false},
		{"MP3 音频接受", func(p *storage.MediaProbe) { p.AudioCodecs = []string{"mp3"} }, false},
		{"帧率过低", func(p *storage.MediaProbe) { p.FPS = 23.8 }, true},
		{"帧率过高", func(p *storage.MediaProbe) { p.FPS = 60.1 }, true},
		{"无视频流", func(p *storage.MediaProbe) { p.HasVideo = false }, true},
		{"时长过短", func(p *storage.MediaProbe) { p.DurationMS = 1999 }, true},
		{"时长过长", func(p *storage.MediaProbe) { p.DurationMS = 30001 }, true},
		{"宽度过小", func(p *storage.MediaProbe) { p.Width = 299 }, true},
		{"宽高比过大", func(p *storage.MediaProbe) { p.Width = 1000; p.Height = 380 }, true},
		{"视频编码不支持", func(p *storage.MediaProbe) { p.VideoCodecs = []string{"vp9"} }, true},
		{"音频编码不支持", func(p *storage.MediaProbe) { p.AudioCodecs = []string{"opus"} }, true},
		{"容器格式不支持", func(p *storage.MediaProbe) { p.Format = "matroska,webm" }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := clone()
			tc.mutate(p)
			err := validateVideoProbe(p)
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestFilePubliclyReadable(t *testing.T) {
	cases := []struct {
		name string
		file *model.File
		want bool
	}{
		{"存量空值按公开处理", &model.File{}, true},
		{"public + ready", &model.File{Visibility: model.FileVisibilityPublic, Status: model.FileStatusReady}, true},
		{"private", &model.File{Visibility: model.FileVisibilityPrivate, Status: model.FileStatusReady}, false},
		{"pending", &model.File{Visibility: model.FileVisibilityPublic, Status: model.FileStatusPending}, false},
		{"failed", &model.File{Visibility: model.FileVisibilityPublic, Status: model.FileStatusFailed}, false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := filePubliclyReadable(tc.file); got != tc.want {
				t.Errorf("filePubliclyReadable() = %v, want %v", got, tc.want)
			}
		})
	}
}

// 确保错误信息里包含关键提示，便于调用方排查。
func TestValidateVideoProbeErrorMessages(t *testing.T) {
	p := &storage.MediaProbe{
		HasVideo:    true,
		Width:       1000,
		Height:      1000,
		DurationMS:  5000,
		FPS:         30,
		VideoCodecs: []string{"vp9"},
		Format:      "mov,mp4",
	}
	err := validateVideoProbe(p)
	if err == nil || !strings.Contains(err.Error(), "H.264") {
		t.Errorf("错误信息应提示支持的编码: %v", err)
	}
}
