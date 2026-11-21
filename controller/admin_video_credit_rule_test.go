package controller

import (
	"strings"
	"testing"
)

func TestCompareVideoResolutionSets(t *testing.T) {
	if err := compareVideoResolutionSets([]string{"480P", "2K"}, []string{"480p", "2k"}); err != nil {
		t.Fatalf("大小写不同不应视为不一致: %v", err)
	}

	err := compareVideoResolutionSets([]string{"480P", "768P"}, []string{"480P"})
	if err == nil {
		t.Fatalf("缺少计费档位应当报错")
	}
	if !strings.Contains(err.Error(), "768P") {
		t.Errorf("错误信息应列出缺少的档位: %v", err)
	}

	err = compareVideoResolutionSets([]string{"480P"}, []string{"480P", "1080P"})
	if err == nil {
		t.Fatalf("参数规格缺少档位应当报错")
	}
	if !strings.Contains(err.Error(), "1080P") {
		t.Errorf("错误信息应列出多余的档位: %v", err)
	}
}
