package common

import (
	"fmt"
	"os"
	"strconv"
	"sync"

	"github.com/joho/godotenv"
)

var envOnce sync.Once

// ensureEnvLoaded 加载 .env 环境变量（幂等）。
// 必须在读取任何密钥前调用——package init 早于 main 的 godotenv.Load()，
// 因此密钥获取应改为延迟调用本函数，确保 .env 已先被加载。
func ensureEnvLoaded() {
	envOnce.Do(func() {
		_ = godotenv.Load()
	})
}

// insecureDefaultSecrets 仓库中出现过的示例/历史默认密钥。
// 任何一项仍在生效都意味着该密钥实际是公开的，必须视为未配置。
var insecureDefaultSecrets = map[string]struct{}{
	"futureai-api-jwt-secret-change-me": {},
	"futureai-api-secret-change-me":     {},
}

// MinSecretLen 密钥最小长度。32 字符 ≈ 128 位熵（十六进制编码时）。
const MinSecretLen = 32

// RequireSecret 读取必填密钥；未设置、仍为示例默认值、或强度不足时返回错误。
// 采用 fail-closed：配置错误时拒绝启动，而不是回落到一个公开可预测的默认值。
func RequireSecret(name string) (string, error) {
	ensureEnvLoaded()
	value := os.Getenv(name)
	if value == "" {
		return "", fmt.Errorf("%s 未设置，请在 .env 中配置强随机值（如 openssl rand -hex 32）", name)
	}
	if _, isDefault := insecureDefaultSecrets[value]; isDefault {
		return "", fmt.Errorf("%s 仍为示例默认值，该值已公开，攻击者可据此伪造凭证，请改为强随机值", name)
	}
	if len(value) < MinSecretLen {
		return "", fmt.Errorf("%s 仅 %d 字符，强度不足（至少需要 %d 字符）", name, len(value), MinSecretLen)
	}
	return value, nil
}

// GetEnvOrDefault 获取环境变量，如果不存在则返回默认值
func GetEnvOrDefault(env string, defaultValue int) int {
	if env == "" || os.Getenv(env) == "" {
		return defaultValue
	}
	num, err := strconv.Atoi(os.Getenv(env))
	if err != nil {
		SysError(fmt.Sprintf("failed to parse %s: %s, using default value: %d", env, err.Error(), defaultValue))
		return defaultValue
	}
	return num
}

// GetEnvOrDefaultString 获取字符串环境变量
func GetEnvOrDefaultString(env string, defaultValue string) string {
	if env == "" || os.Getenv(env) == "" {
		return defaultValue
	}
	return os.Getenv(env)
}

// GetEnvOrDefaultBool 获取布尔环境变量
func GetEnvOrDefaultBool(env string, defaultValue bool) bool {
	if env == "" || os.Getenv(env) == "" {
		return defaultValue
	}
	b, err := strconv.ParseBool(os.Getenv(env))
	if err != nil {
		SysError(fmt.Sprintf("failed to parse %s: %s, using default value: %t", env, err.Error(), defaultValue))
		return defaultValue
	}
	return b
}
