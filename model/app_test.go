package model

import "testing"

func TestParseSemverValid(t *testing.T) {
	cases := []struct {
		version string
		want    [3]int
	}{
		{"0.0.0", [3]int{0, 0, 0}},
		{"1.2.3", [3]int{1, 2, 3}},
		{"1.10.0", [3]int{1, 10, 0}},
	}

	for _, tc := range cases {
		got, err := ParseSemver(tc.version)
		if err != nil {
			t.Errorf("ParseSemver(%q) 返回错误: %v", tc.version, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseSemver(%q) = %v, want %v", tc.version, got, tc.want)
		}
	}
}

func TestParseSemverInvalid(t *testing.T) {
	// 客户端与服务端必须对「什么是合法版本号」保持一致，
	// 否则会出现一边认为有更新、另一边解析失败的分歧。
	cases := []string{
		"",
		"1.2",
		"1.2.3.4",
		"v1.2.3",
		"1.2.3-beta",
		"01.2.3",
		"1.02.3",
		"1.2.03",
		" 1.2.3",
		"1.2.3 ",
		"a.b.c",
	}

	for _, version := range cases {
		if _, err := ParseSemver(version); err == nil {
			t.Errorf("ParseSemver(%q) 期望报错，但通过了", version)
		}
	}
}

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"1.2.2", "1.2.3", -1},
		{"1.2.4", "1.2.3", 1},
		{"1.9.0", "1.10.0", -1}, // 字符串字典序会判反，必须按数字比较
		{"2.0.0", "1.99.99", 1},
		{"0.0.1", "0.0.0", 1},
	}

	for _, tc := range cases {
		got, err := CompareSemver(tc.a, tc.b)
		if err != nil {
			t.Errorf("CompareSemver(%q, %q) 返回错误: %v", tc.a, tc.b, err)
			continue
		}
		if got != tc.want {
			t.Errorf("CompareSemver(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestCompareSemverInvalid(t *testing.T) {
	if _, err := CompareSemver("1.2.3", "1.2"); err == nil {
		t.Error("任一方非法时应返回错误")
	}
}

func TestValidateAppKey(t *testing.T) {
	valid := []string{
		"futureai-image",
		"futureai-video",
		"app1",
		"a",
		"a-b-c",
	}
	for _, key := range valid {
		if err := ValidateAppKey(key); err != nil {
			t.Errorf("ValidateAppKey(%q) 期望通过，却返回: %v", key, err)
		}
	}

	invalid := []string{
		"",
		"FutureAI-Image",
		"futureai_image",
		"futureai image",
		"-futureai",
		"_futureai",
		"应用",
	}
	for _, key := range invalid {
		if err := ValidateAppKey(key); err == nil {
			t.Errorf("ValidateAppKey(%q) 期望报错，但通过了", key)
		}
	}
}

func TestIsValidAppPlatform(t *testing.T) {
	for _, platform := range AllAppPlatforms {
		if !IsValidAppPlatform(platform) {
			t.Errorf("IsValidAppPlatform(%q) = false, want true", platform)
		}
	}

	for _, platform := range []string{"", "linux", "Windows", "darwin"} {
		if IsValidAppPlatform(platform) {
			t.Errorf("IsValidAppPlatform(%q) = true, want false", platform)
		}
	}
}
