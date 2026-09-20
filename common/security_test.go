package common

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

// TestMain 为整个 common 测试包设置合法的密钥。
// getJWTSecret / getServerKey 使用 sync.Once 缓存，必须在任何测试调用它们之前设置好。
func TestMain(m *testing.M) {
	os.Setenv("JWT_SECRET", strings.Repeat("a", 64))
	os.Setenv("SECRET_KEY", strings.Repeat("b", 64))
	os.Exit(m.Run())
}

// ── RequireSecret：密钥缺失/弱值必须 fail-closed ──

func TestRequireSecretRejectsEmpty(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	if _, err := RequireSecret("JWT_SECRET"); err == nil {
		t.Fatal("密钥为空时应返回错误，否则服务会用可预测的默认值启动")
	}
}

func TestRequireSecretRejectsKnownDefaults(t *testing.T) {
	// 这两个值曾在仓库中作为默认值出现，等同公开
	for _, weak := range []string{
		"futureai-api-jwt-secret-change-me",
		"futureai-api-secret-change-me",
	} {
		t.Setenv("TEST_SECRET_KEY", weak)
		if _, err := RequireSecret("TEST_SECRET_KEY"); err == nil {
			t.Errorf("已知默认值 %q 应被拒绝", weak)
		}
	}
}

func TestRequireSecretRejectsShortValue(t *testing.T) {
	t.Setenv("TEST_SECRET_KEY", strings.Repeat("x", MinSecretLen-1))
	if _, err := RequireSecret("TEST_SECRET_KEY"); err == nil {
		t.Error("长度不足的密钥应被拒绝")
	}
}

func TestRequireSecretAcceptsStrongValue(t *testing.T) {
	strong := strings.Repeat("f", 64)
	t.Setenv("TEST_SECRET_KEY", strong)
	got, err := RequireSecret("TEST_SECRET_KEY")
	if err != nil {
		t.Fatalf("强随机值不应被拒绝: %v", err)
	}
	if got != strong {
		t.Errorf("返回值不一致: got %q want %q", got, strong)
	}
}

// ── 加解密 ──

func TestEncryptDecryptRoundTrip(t *testing.T) {
	secret := "sk-test-1234567890"
	encrypted, err := EncryptSecret(secret)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	if encrypted == secret {
		t.Fatal("密文与明文相同，说明没有真正加密")
	}

	decrypted, err := DecryptSecret(encrypted)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if decrypted != secret {
		t.Errorf("往返结果不一致: got %q want %q", decrypted, secret)
	}
}

func TestEncryptSecretUsesRandomNonce(t *testing.T) {
	a, _ := EncryptSecret("same-plaintext")
	b, _ := EncryptSecret("same-plaintext")
	if a == b {
		t.Error("相同明文产生了相同密文，nonce 未随机化")
	}
}

// TestDecryptWithKeyShortCiphertext 覆盖一个真实缺陷：
// 密文短于 GCM nonce 时，旧实现返回 ("", nil)，调用方只判 err 就会
// 把一个空字符串当作"解密成功的密钥"继续使用。
func TestDecryptWithKeyShortCiphertext(t *testing.T) {
	key, err := GenerateDataKey()
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}

	short := base64.StdEncoding.EncodeToString([]byte("abc")) // 3 字节 < 12 字节 nonce

	plaintext, err := DecryptWithKey(short, key)
	if err == nil {
		t.Fatalf("短密文必须返回错误，实际返回 nil（plaintext=%q）", plaintext)
	}
	if plaintext != "" {
		t.Errorf("出错时不应返回明文，实际为 %q", plaintext)
	}
}

func TestDecryptWithKeyWrongKeyFails(t *testing.T) {
	correctKey, _ := GenerateDataKey()
	wrongKey, _ := GenerateDataKey()

	encrypted, err := EncryptWithKey("secret", correctKey)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	if _, err := DecryptWithKey(encrypted, wrongKey); err == nil {
		t.Error("用错误的密钥解密必须失败（AEAD 认证）")
	}
}

func TestEncryptWithKeyRejectsBadKeyLength(t *testing.T) {
	badKey := base64.StdEncoding.EncodeToString([]byte("too-short"))
	if _, err := EncryptWithKey("data", badKey); err == nil {
		t.Error("非 32 字节的密钥必须被拒绝")
	}
}

// ── 脱敏 ──

func TestMaskSecret(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", "****"},
		{"abc", "****"},
		{"abcd", "****"},
		{"sk-1234567890", "****7890"},
	}
	for _, tc := range tests {
		if got := MaskSecret(tc.in); got != tc.want {
			t.Errorf("MaskSecret(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMaskSecretNeverLeaksFullValue(t *testing.T) {
	secret := "sk-abcdefghijklmnopqrstuvwxyz"
	masked := MaskSecret(secret)
	if strings.Contains(masked, secret) || len(masked) >= len(secret) {
		t.Errorf("脱敏结果泄露了完整密钥: %q", masked)
	}
}

// TestGenerateRandomPassword 校验随机密码的字符集与长度
func TestGenerateRandomPassword(t *testing.T) {
	const n = 100
	pw := GenerateRandomPassword(n)
	if len(pw) != n {
		t.Errorf("长度错误: got %d want %d", len(pw), n)
	}

	seen := map[byte]bool{}
	for i := 0; i < n; i++ {
		seen[pw[i]] = true
	}
	if len(seen) < 10 {
		t.Errorf("随机性不足，%d 个字符中只有 %d 种", n, len(seen))
	}
}
