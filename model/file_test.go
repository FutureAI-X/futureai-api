package model

import (
	"strings"
	"testing"
)

// TestGenerateFileIDFormat 验证 ID 前缀与长度稳定，便于写入方与文档对齐
func TestGenerateFileIDFormat(t *testing.T) {
	id := GenerateFileID()

	const wantPrefix = "file_"
	if !strings.HasPrefix(id, wantPrefix) {
		t.Errorf("ID = %q, 期望以 %q 开头", id, wantPrefix)
	}
	// file_ + 12 字节的十六进制（24 字符）
	if len(id) != len(wantPrefix)+24 {
		t.Errorf("ID 长度 = %d, want %d: %q", len(id), len(wantPrefix)+24, id)
	}
	if strings.ContainsAny(id, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		t.Errorf("ID 应只含小写十六进制字符: %q", id)
	}
}

// TestGenerateFileIDIsUnique 验证随机性。
// 公开读取接口以 ID 作为唯一凭证，可枚举即等于所有文件公开。
func TestGenerateFileIDIsUnique(t *testing.T) {
	const n = 1000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := GenerateFileID()
		if _, ok := seen[id]; ok {
			t.Fatalf("生成第 %d 个 ID 时发生重复: %s", i+1, id)
		}
		seen[id] = struct{}{}
	}
}

func TestFileTableName(t *testing.T) {
	if got := (File{}).TableName(); got != "files" {
		t.Errorf("TableName() = %q, want files", got)
	}
}
