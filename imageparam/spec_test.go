package imageparam

import (
	"fmt"
	"strings"
	"testing"
)

func mustSpec(t *testing.T, modelName string) *Spec {
	t.Helper()
	raw, err := MustSeedSpec(modelName)
	if err != nil {
		t.Fatalf("MustSeedSpec(%s): %v", modelName, err)
	}
	spec, err := ParseSpec(raw)
	if err != nil {
		t.Fatalf("ParseSpec(%s): %v", modelName, err)
	}
	return spec
}

func TestSeedSpecsParse(t *testing.T) {
	for name, raw := range SeedSpecs() {
		if strings.TrimSpace(raw) == "" {
			t.Fatalf("%s 的种子规格为空", name)
		}
		spec, err := ParseSpec(raw)
		if err != nil {
			t.Fatalf("ParseSpec(%s): %v", name, err)
		}
		if len(spec.Params) == 0 {
			t.Fatalf("%s 没有参数", name)
		}
	}
}

func TestGptImageDefaultsAndValidation(t *testing.T) {
	spec := mustSpec(t, "gpt-image-2")
	values := Values{"prompt": "a cat"}
	if err := spec.ApplyDefaults(values); err != nil {
		t.Fatalf("ApplyDefaults: %v", err)
	}
	if values["size"] != "16:9" {
		t.Fatalf("size default = %v, want 16:9", values["size"])
	}
	if values["resolution"] != "1K" {
		t.Fatalf("resolution default = %v, want 1K", values["resolution"])
	}
	if err := spec.Validate(values); err != nil {
		t.Fatalf("Validate defaults: %v", err)
	}

	values["size"] = "1024x1024"
	if err := spec.Validate(values); err != nil {
		t.Fatalf("Validate pixel size: %v", err)
	}

	values["size"] = "2:4"
	err := spec.Validate(values)
	if err == nil || err.Code != "PARAM_SIZE_RATIO_UNSUPPORTED" {
		t.Fatalf("Validate unsupported ratio = %v, want PARAM_SIZE_RATIO_UNSUPPORTED", err)
	}

	values["size"] = "16:9"
	values["prompt"] = strings.Repeat("a", 2001)
	err = spec.Validate(values)
	if err == nil || err.Code != "PARAM_TOO_LONG" {
		t.Fatalf("Validate long prompt = %v, want PARAM_TOO_LONG", err)
	}
}

func TestGptImage26DefaultsAndValidation(t *testing.T) {
	spec := mustSpec(t, "gpt-image-2.6-flare-ext")
	values := Values{"prompt": "a cat"}
	if err := spec.ApplyDefaults(values); err != nil {
		t.Fatalf("ApplyDefaults: %v", err)
	}
	if values["size"] != "16:9" || values["resolution"] != "1K" {
		t.Fatalf("尺寸默认值不符: size=%v resolution=%v", values["size"], values["resolution"])
	}
	if values["quality"] != "auto" || values["output_format"] != "png" {
		t.Fatalf("可选参数默认值不符: quality=%v output_format=%v", values["quality"], values["output_format"])
	}
	// complexity 不设默认：用户不选就不发送，由上游决定。
	if _, ok := values["complexity"]; ok {
		t.Fatalf("complexity 不该有默认值: %v", values["complexity"])
	}
	if err := spec.Validate(values); err != nil {
		t.Fatalf("Validate defaults: %v", err)
	}

	values["quality"] = "xhigh"
	values["output_format"] = "webp"
	values["complexity"] = "high"
	if err := spec.Validate(values); err != nil {
		t.Fatalf("Validate optional params: %v", err)
	}

	values["quality"] = "ultra"
	err := spec.Validate(values)
	if err == nil || err.Code != "PARAM_UNSUPPORTED_VALUE" {
		t.Fatalf("Validate unsupported quality = %v, want PARAM_UNSUPPORTED_VALUE", err)
	}

	values["quality"] = "auto"
	values["output_format"] = "svg"
	err = spec.Validate(values)
	if err == nil || err.Code != "PARAM_UNSUPPORTED_VALUE" {
		t.Fatalf("Validate svg = %v, want PARAM_UNSUPPORTED_VALUE", err)
	}

	values["output_format"] = "gif"
	err = spec.Validate(values)
	if err == nil || err.Code != "PARAM_UNSUPPORTED_VALUE" {
		t.Fatalf("Validate gif = %v, want PARAM_UNSUPPORTED_VALUE", err)
	}

	values["output_format"] = "png"
	values["size"] = "21:9"
	if err := spec.Validate(values); err != nil {
		t.Fatalf("Validate 21:9: %v", err)
	}

	values["size"] = "3:1"
	err = spec.Validate(values)
	if err == nil || err.Code != "PARAM_SIZE_RATIO_UNSUPPORTED" {
		t.Fatalf("Validate 3:1 = %v, want PARAM_SIZE_RATIO_UNSUPPORTED", err)
	}
}

func TestSeedream55DefaultsAndValidation(t *testing.T) {
	spec := mustSpec(t, "seedream-5-5")
	values := Values{"prompt": "a cat"}
	if err := spec.ApplyDefaults(values); err != nil {
		t.Fatalf("ApplyDefaults: %v", err)
	}
	if values["size"] != "16:9" || values["resolution"] != "1K" {
		t.Fatalf("默认值不符: size=%v resolution=%v", values["size"], values["resolution"])
	}
	if err := spec.Validate(values); err != nil {
		t.Fatalf("Validate defaults: %v", err)
	}

	// 1.5K 是本模型独有档位。
	values["resolution"] = "1.5K"
	if err := spec.Validate(values); err != nil {
		t.Fatalf("Validate 1.5K: %v", err)
	}

	// 不支持 4K。
	values["resolution"] = "4K"
	err := spec.Validate(values)
	if err == nil || err.Code != "PARAM_UNSUPPORTED_VALUE" {
		t.Fatalf("Validate 4K = %v, want PARAM_UNSUPPORTED_VALUE", err)
	}

	// 比例集合与上游一致：2:1 / 1:2 可用，5:4 不可用。
	values["resolution"] = "1K"
	values["size"] = "2:1"
	if err := spec.Validate(values); err != nil {
		t.Fatalf("Validate 2:1: %v", err)
	}
	values["size"] = "5:4"
	err = spec.Validate(values)
	if err == nil || err.Code != "PARAM_SIZE_RATIO_UNSUPPORTED" {
		t.Fatalf("Validate 5:4 = %v, want PARAM_SIZE_RATIO_UNSUPPORTED", err)
	}

	// 只开放比例，不接受像素尺寸。
	values["size"] = "1024x1024"
	err = spec.Validate(values)
	if err == nil || err.Code != "PARAM_SIZE_MODE_UNSUPPORTED" {
		t.Fatalf("Validate pixel = %v, want PARAM_SIZE_MODE_UNSUPPORTED", err)
	}

	// 参考图上限 10 张。
	ids := make([]string, 11)
	for i := range ids {
		ids[i] = fmt.Sprintf("file_%d", i)
	}
	values = Values{"prompt": "a cat", "image_ids": ids}
	if err := spec.ApplyDefaults(values); err != nil {
		t.Fatalf("ApplyDefaults: %v", err)
	}
	err = spec.Validate(values)
	if err == nil || err.Code != "PARAM_REF_TOO_MANY" {
		t.Fatalf("Validate 11 refs = %v, want PARAM_REF_TOO_MANY", err)
	}
}

func TestSeedream55RefFileLimits(t *testing.T) {
	spec := mustSpec(t, "seedream-5-5")
	rule := spec.RefImagesRule()
	if rule == nil {
		t.Fatal("缺少 ref_images 规则")
	}
	if rule.MaxCount != 10 {
		t.Errorf("MaxCount = %d, want 10", rule.MaxCount)
	}
	if rule.MaxFileBytes != int64(30)<<20 {
		t.Errorf("MaxFileBytes = %d, want %d", rule.MaxFileBytes, int64(30)<<20)
	}
	if rule.MaxTotalBytes != int64(300)<<20 {
		t.Errorf("MaxTotalBytes = %d, want %d", rule.MaxTotalBytes, int64(300)<<20)
	}
}

func TestMossPixelSize(t *testing.T) {
	spec := mustSpec(t, "moss-image-base-1.0")
	values := Values{"prompt": "a cat", "size": "1440x1440"}
	if err := spec.ApplyDefaults(values); err != nil {
		t.Fatalf("ApplyDefaults: %v", err)
	}
	if err := spec.Validate(values); err != nil {
		t.Fatalf("Validate 1440x1440: %v", err)
	}
	if spec.HasKey("resolution") {
		t.Fatal("MOSS 不应包含 resolution")
	}
	if spec.RefImagesRule() != nil {
		t.Fatal("MOSS 不应包含 ref_images")
	}

	values["size"] = "1920x1080"
	err := spec.Validate(values)
	if err == nil || err.Code != "PARAM_SIZE_PIXEL_STEP" {
		t.Fatalf("Validate 1920x1080 = %v, want PARAM_SIZE_PIXEL_STEP", err)
	}

	values["size"] = ""
	if err := spec.ApplyDefaults(values); err == nil || err.Code != "PARAM_REQUIRED" {
		t.Fatalf("ApplyDefaults empty size = %v, want PARAM_REQUIRED", err)
	}
}

func TestRefImageCount(t *testing.T) {
	spec := mustSpec(t, "gpt-image-2")
	ids := make([]string, 16)
	for i := range ids {
		ids[i] = fmt.Sprintf("file_%d", i)
	}
	values := Values{"prompt": "a cat", "image_ids": ids}
	if err := spec.ApplyDefaults(values); err != nil {
		t.Fatalf("ApplyDefaults: %v", err)
	}
	err := spec.Validate(values)
	if err == nil || err.Code != "PARAM_REF_TOO_MANY" {
		t.Fatalf("Validate 16 refs = %v, want PARAM_REF_TOO_MANY", err)
	}
}

func TestRefFileSize(t *testing.T) {
	spec := mustSpec(t, "gpt-image-2")
	rule := spec.RefImagesRule()
	if rule == nil {
		t.Fatal("缺少 ref_images 规则")
	}
	err := ValidateRefFiles(rule, []RefFile{
		{ID: "a", Bytes: int64(11) << 20, ContentType: "image/png"},
	})
	if err == nil || err.Code != "PARAM_REF_FILE_TOO_LARGE" {
		t.Fatalf("ValidateRefFiles = %v, want PARAM_REF_FILE_TOO_LARGE", err)
	}
}

// 请求字段是否存在按 path 判断：key 可以与 path 不同，
// 控制器清空不支持字段时必须用 HasPath，不能用 HasKey。
func TestHasPathUsesRequestPath(t *testing.T) {
	spec, err := ParseSpec(`{"params":[{"key":"image_quality","path":"quality","widget":"segmented","options":["auto"]}]}`)
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}
	if !spec.HasPath("quality") {
		t.Error("HasPath(quality) = false, want true")
	}
	if spec.HasKey("quality") {
		t.Error("HasKey(quality) = true, 但规格的 key 是 image_quality")
	}
	if !spec.HasKey("image_quality") {
		t.Error("HasKey(image_quality) = false, want true")
	}
}

func TestParseSpecRejectsUnsupported(t *testing.T) {
	cases := []string{
		`{"params":[{"key":"prompt","widget":"file"}]}`,
		`{"params":[{"key":"prompt","widget":"textarea","unknown":true}]}`,
		`{"params":[{"key":"size","widget":"ratio_size","ratios":["1:1"],"default":{"by":"x"}}]}`,
		`{"params":[{"key":"prompt","widget":"textarea","visible_when":{"field":"x","op":"eq","value":1}}]}`,
		// segmented 只能映射到枚举型字段，不能借 path 修改 size 等控件语义
		`{"params":[{"key":"quality","path":"size","widget":"segmented","options":["high"]}]}`,
	}
	for _, raw := range cases {
		if _, err := ParseSpec(raw); err == nil {
			t.Errorf("ParseSpec(%s) 应该失败", raw)
		}
	}
}
