package common

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWT 密钥，从环境变量获取（延迟加载，确保 .env 已先被加载）
var (
	jwtOnce   sync.Once
	jwtSecret []byte
)

// getJWTSecret 获取 JWT 密钥（首次调用时加载 .env）
// fail-closed：密钥缺失/为默认值时直接终止进程，绝不回落到可预测的默认值。
func getJWTSecret() []byte {
	jwtOnce.Do(func() {
		secret, err := RequireSecret("JWT_SECRET")
		if err != nil {
			log.Fatalf("[FUTUREAI-API] [安全] %v", err)
		}
		jwtSecret = []byte(secret)
	})
	return jwtSecret
}

// Claims JWT 声明
type Claims struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
	Role     int    `json:"role"`

	// TokenVersion 签发时用户的令牌版本号。校验时与库中的当前值比对，
	// 不一致即视为失效——改密码时递增用户的版本号，就能立刻作废
	// 所有已签发的令牌。
	//
	// 为什么需要它：JWT 是无状态的，签发后到过期前无法撤回。
	// 没有版本号时，密码泄露后用户改密码这个最常用的应急动作**完全无效**，
	// 攻击者仍能用旧令牌操纵账户最长 24 小时。
	TokenVersion int `json:"tv"`

	jwt.RegisteredClaims
}

// GenerateToken 生成 JWT Token
func GenerateToken(userID int, username string, role int, tokenVersion int) (string, error) {
	// Token 过期时间：24小时
	expireTime := time.Now().Add(24 * time.Hour)

	claims := Claims{
		UserID:       userID,
		Username:     username,
		Role:         role,
		TokenVersion: tokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expireTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "futureai-api",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(getJWTSecret())
}

// ParseToken 解析并校验 JWT Token。
//
// 显式要求 exp 与 iss：exp 在 jwt/v5 里默认是可选的，缺少 exp 的令牌
// 会被判为有效。当前签发路径恒设 exp，因此这是纵深防御而非在修漏洞——
// 但它把「永不过期的令牌」这类问题从可能变成不可能。
func ParseToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{},
		func(token *jwt.Token) (interface{}, error) {
			// 校验签名算法，仅接受 HMAC，防止算法混淆攻击
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return getJWTSecret(), nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithIssuer("futureai-api"),
	)

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, jwt.ErrSignatureInvalid
}
