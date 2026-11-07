package common

import (
	"crypto/rand"
	"errors"
	"math/big"
	"strings"
	"unicode"
)

const (
	// StrongPasswordLength 统一生成密码的长度。
	StrongPasswordLength = 16
	// strongPasswordPerClass 每一类字符在密码中占的位数。
	strongPasswordPerClass = 4
	// PasswordMinLength 手动设置密码时的最小长度。
	PasswordMinLength = 8
	// passwordMinClasses 手动设置密码时至少需要覆盖的字符类别数。
	passwordMinClasses = 3
)

var (
	ErrPasswordTooShort = errors.New("密码长度不能少于8位")
	ErrPasswordTooWeak  = errors.New("密码需至少包含大写字母、小写字母、数字、符号中的3类")
)

// strongPasswordGroups 生成随机密码时使用的四类字符。
// 统一排除 0/O、1/l/I 等易混淆字符：生成后的密码会展示给用户抄写，
// 看错的字符会直接变成「密码不对」的工单。
var strongPasswordGroups = [][]rune{
	[]rune("ABCDEFGHJKLMNPQRSTUVWXYZ"), // 去掉 I、O
	[]rune("abcdefghijkmnpqrstuvwxyz"), // 去掉 l、o
	[]rune("23456789"),                 // 去掉 0、1
	[]rune("!@#$%^&*"),
}

type passwordChar struct {
	char rune
	kind int
}

func secureIntn(n int) (int, error) {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, err
	}
	return int(v.Int64()), nil
}

func shufflePasswordChars(chars []passwordChar) error {
	for i := len(chars) - 1; i > 0; i-- {
		j, err := secureIntn(i + 1)
		if err != nil {
			return err
		}
		chars[i], chars[j] = chars[j], chars[i]
	}
	return nil
}

func hasAdjacentSameKind(chars []passwordChar) bool {
	for i := 1; i < len(chars); i++ {
		if chars[i].kind == chars[i-1].kind {
			return true
		}
	}
	return false
}

// pickFromGroups 每类各取 strongPasswordPerClass 个字符，按类别连续排列。
func pickFromGroups() ([]passwordChar, error) {
	chars := make([]passwordChar, 0, StrongPasswordLength)
	for kind, group := range strongPasswordGroups {
		pool := make([]passwordChar, len(group))
		for i, ch := range group {
			pool[i] = passwordChar{char: ch, kind: kind}
		}
		if err := shufflePasswordChars(pool); err != nil {
			return nil, err
		}
		chars = append(chars, pool[:strongPasswordPerClass]...)
	}
	return chars, nil
}

func passwordCharsToString(chars []passwordChar) string {
	var b strings.Builder
	b.Grow(len(chars))
	for _, c := range chars {
		b.WriteRune(c.char)
	}
	return b.String()
}

// GenerateStrongPassword 生成符合统一规则的随机密码：
// 16 位，大写/小写/数字/符号各 4 位，顺序随机且相邻两位不属于同一类。
//
// 新增用户、重置密码、root 初始密码都走这一个函数，避免各处规则再次分叉。
func GenerateStrongPassword() (string, error) {
	// 四类各 4 位时，单次随机排列满足「相邻不同类」的概率约 3.6%，
	// 1000 次仍失败的概率可以忽略。
	for attempt := 0; attempt < 1000; attempt++ {
		chars, err := pickFromGroups()
		if err != nil {
			return "", err
		}
		if err := shufflePasswordChars(chars); err != nil {
			return "", err
		}
		if !hasAdjacentSameKind(chars) {
			return passwordCharsToString(chars), nil
		}
	}

	// 兜底：四类按随机顺序轮转（A B C D A B C D …），天然不相邻。
	// 这个分支只是理论上存在，仍然保证密码满足同一套规则。
	chars, err := pickFromGroups()
	if err != nil {
		return "", err
	}
	groups := make([][]passwordChar, len(strongPasswordGroups))
	for i := range groups {
		start := i * strongPasswordPerClass
		groups[i] = chars[start : start+strongPasswordPerClass]
	}
	order := make([]int, len(groups))
	for i := range order {
		order[i] = i
	}
	for i := len(order) - 1; i > 0; i-- {
		j, err := secureIntn(i + 1)
		if err != nil {
			return "", err
		}
		order[i], order[j] = order[j], order[i]
	}
	out := make([]passwordChar, 0, StrongPasswordLength)
	for pos := 0; pos < strongPasswordPerClass; pos++ {
		for _, kind := range order {
			out = append(out, groups[kind][pos])
		}
	}
	return passwordCharsToString(out), nil
}

// ValidatePasswordStrength 校验手动设置的密码是否满足最低强度。
//
// 注意：只用于「设置新密码」的入口（新增用户、管理员改密）。
// 登录时绝不能调用——否则已上线用户的旧密码会被这条新规则挡在门外。
func ValidatePasswordStrength(password string) error {
	if len([]rune(password)) < PasswordMinLength {
		return ErrPasswordTooShort
	}

	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		default:
			hasSpecial = true
		}
	}

	classes := 0
	for _, ok := range []bool{hasUpper, hasLower, hasDigit, hasSpecial} {
		if ok {
			classes++
		}
	}
	if classes < passwordMinClasses {
		return ErrPasswordTooWeak
	}
	return nil
}
