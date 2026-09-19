package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/FutureAI/token-hub/common"
	"github.com/FutureAI/token-hub/model"
	"github.com/gin-gonic/gin"
)

// UserAuth 用户认证中间件
// 除校验 JWT 签名外，还必须回查数据库中的用户状态与角色：
//   - 禁用/删除用户可立即失效，不必等待 24 小时 JWT 过期
//   - 角色降权立即生效（不信任令牌中携带的旧角色）
func UserAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := parseAndLoadUser(c)
		if !ok {
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("user_role", claims.Role)

		c.Next()
	}
}

// AdminAuth 管理员认证中间件（role >= 10，当前未挂载，保留供后续使用）
func AdminAuth() gin.HandlerFunc {
	return requireRole(model.RoleAdminUser)
}

// RootAuth 超级管理员认证中间件（仅 role=100）
func RootAuth() gin.HandlerFunc {
	return requireRole(model.RoleRootUser)
}

// requireRole 生成「要求至少 minRole 角色」的中间件。
// 角色取自数据库实时值，而非 JWT 中的旧声明。
func requireRole(minRole int) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := parseAndLoadUser(c)
		if !ok {
			return
		}

		if claims.Role < minRole {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "权限不足",
			})
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("user_role", claims.Role)

		c.Next()
	}
}

// parseAndLoadUser 解析 JWT 并回查用户，返回以数据库为准的 claims。
// 校验失败时已写入响应并 Abort，调用方直接 return 即可。
func parseAndLoadUser(c *gin.Context) (*common.Claims, bool) {
	token := extractToken(c)
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "未提供认证Token",
		})
		c.Abort()
		return nil, false
	}

	claims, err := common.ParseToken(token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "无效的Token",
		})
		c.Abort()
		return nil, false
	}

	// 回查数据库：确保用户仍然存在且处于启用状态
	user, err := model.GetUserByID(claims.UserID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "账号不存在或已被删除",
		})
		c.Abort()
		return nil, false
	}
	if user.Status != model.UserStatusEnabled {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "账号已被禁用",
		})
		c.Abort()
		return nil, false
	}

	// 令牌版本必须与库中一致。改密码会递增用户的 token_version，
	// 因此这一步让「改密」具备立即作废所有旧令牌的能力——
	// JWT 本身无状态、签发后无法撤回，没有这道校验时密码泄露后的
	// 应急改密对攻击者手上的旧令牌毫无作用。
	if claims.TokenVersion != user.TokenVersion {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "登录状态已失效，请重新登录",
		})
		c.Abort()
		return nil, false
	}

	// 以数据库中的当前角色/用户名为准，使降权即时生效
	claims.Role = user.Role
	claims.Username = user.Username

	return claims, true
}

// APIAuth API Key 认证中间件
// 校验 /v1 接口的 Authorization: Bearer <token>
// <token> 对应 api_keys 表中的 key 字段（即 API Key）
func APIAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := extractToken(c)
		if key == "" {
			abortAPIAuth(c, "未提供 API Key", "authentication_error")
			return
		}

		// 根据 Key 获取 Token（GetTokenByKey 已过滤 status=1 启用状态）
		token, err := model.GetTokenByKey(key)
		if err != nil {
			abortAPIAuth(c, "无效的 API Key", "authentication_error")
			return
		}

		// 检查是否过期（-1 表示永不过期）
		if token.ExpiredTime != -1 && token.ExpiredTime < time.Now().Unix() {
			abortAPIAuth(c, "API Key 已过期", "authentication_error")
			return
		}

		// 校验 Key 所属用户仍然存在且启用。
		// 缺少这一步会导致：管理员禁用/删除用户后，其 API Key 依然可以
		// 无限期消耗上游额度（Key 默认永不过期）。
		user, err := model.GetUserByID(token.UserID)
		if err != nil {
			abortAPIAuth(c, "API Key 无效", "authentication_error")
			return
		}
		if user.Status != model.UserStatusEnabled {
			abortAPIAuth(c, "账号已被禁用", "authentication_error")
			return
		}

		// 将身份信息存入上下文供后续处理使用
		c.Set("user_id", token.UserID)
		c.Set("token_id", token.ID)
		c.Set("api_key", token.Key)

		c.Next()
	}
}

// abortAPIAuth 以 OpenAI 兼容格式终止 API 请求
func abortAPIAuth(c *gin.Context, message, errorType string) {
	c.JSON(http.StatusUnauthorized, gin.H{
		"error": gin.H{
			"message": message,
			"type":    errorType,
		},
	})
	c.Abort()
}

// extractToken 从 Authorization 请求头提取 Token。
// 刻意不支持 ?token= 查询参数：查询串会进入访问日志、反向代理日志、
// 浏览器历史与 Referer 头，等同于把凭证写入多个持久化位置。
func extractToken(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return ""
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
		return strings.TrimSpace(parts[1])
	}
	return ""
}
