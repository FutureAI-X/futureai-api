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

func TestParseSpecRejectsUnsupported(t *testing.T) {
	cases := []string{
		`{"params":[{"key":"prompt","widget":"file"}]}`,
		`{"params":[{"key":"prompt","widget":"textarea","unknown":true}]}`,
		`{"params":[{"key":"size","widget":"ratio_size","ratios":["1:1"],"default":{"by":"x"}}]}`,
		`{"params":[{"key":"prompt","widget":"textarea","visible_when":{"field":"x","op":"eq","value":1}}]}`,
	}
	for _, raw := range cases {
		if _, err := ParseSpec(raw); err == nil {
			t.Errorf("ParseSpec(%s) 应该失败", raw)
		}
	}
}
