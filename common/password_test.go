package common

import (
	"strings"
	"testing"
	"unicode"
)

func passwordClasses(ch rune) int {
	switch {
	case unicode.IsUpper(ch):
		return 0
	case unicode.IsLower(ch):
		return 1
	case unicode.IsDigit(ch):
		return 2
	default:
		return 3
	}
}

// TestGenerateStrongPassword 校验统一生成规则：长度、每类数量、相邻不同类、
// 以及易混淆字符排除。多跑几轮以覆盖随机排列。
func TestGenerateStrongPassword(t *testing.T) {
	const ambiguous = "0O1lI"

	for round := 0; round < 100; round++ {
		pw, err := GenerateStrongPassword()
		if err != nil {
			t.Fatalf("生成密码失败: %v", err)
		}

		runes := []rune(pw)
		if len(runes) != StrongPasswordLength {
			t.Fatalf("长度错误: got %d want %d (%q)", len(runes), StrongPasswordLength, pw)
		}

		counts := [4]int{}
		for i, r := range runes {
			if strings.ContainsRune(ambiguous, r) {
				t.Fatalf("密码包含易混淆字符 %q: %q", r, pw)
			}
			counts[passwordClasses(r)]++
			if i > 0 && passwordClasses(r) == passwordClasses(runes[i-1]) {
				t.Fatalf("相邻两位属于同一类: %q", pw)
			}
		}
		for kind, n := range counts {
			if n != strongPasswordPerClass {
				t.Fatalf("第 %d 类字符数量错误: got %d want %d (%q)", kind, n, strongPasswordPerClass, pw)
			}
		}
	}
}

func TestGenerateStrongPasswordIsRandom(t *testing.T) {
	a, err := GenerateStrongPassword()
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateStrongPassword()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Errorf("连续两次生成了相同密码: %q", a)
	}
}

func TestValidatePasswordStrength(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  error
	}{
		{"满足三类且够长", "Abcdef12", nil},
		{"四类混合", "Abcdef1!", nil},
		{"太短", "Abc12!", ErrPasswordTooShort},
		{"只有小写一类", "abcdefgh", ErrPasswordTooWeak},
		{"只有大写一类但够长", "ABCDEFGHIJKLMN", ErrPasswordTooWeak},
		{"三类含符号", "abcdef1!", nil},
		{"中文算符号", "密码密码abcdABCD", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePasswordStrength(tt.password)
			if err != tt.wantErr {
				t.Errorf("ValidatePasswordStrength(%q) = %v, want %v", tt.password, err, tt.wantErr)
			}
		})
	}
}
