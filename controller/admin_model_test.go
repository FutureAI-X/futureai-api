package controller

import "testing"

func TestValidateReorderIDs(t *testing.T) {
	cases := []struct {
		name    string
		ids     []int
		wantErr bool
	}{
		{"合法顺序", []int{3, 1, 2}, false},
		{"单个模型", []int{7}, false},
		{"空列表", nil, true},
		{"空数组", []int{}, true},
		{"包含 0", []int{1, 0}, true},
		{"包含负数", []int{-1, 2}, true},
		{"重复 ID", []int{1, 2, 1}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := validateReorderIDs(tc.ids)
			if (msg != "") != tc.wantErr {
				t.Fatalf("validateReorderIDs(%v) = %q, wantErr %v", tc.ids, msg, tc.wantErr)
			}
		})
	}
}
