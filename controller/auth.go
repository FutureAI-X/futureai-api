package controller

import (
	"net/http"
	"sync"
	"time"

	"github.com/FutureAI-X/futureai-api/common"
	"github.com/FutureAI-X/futureai-api/model"
	"github.com/gin-gonic/gin"
)

// ── 登录频次限制（防暴力破解）──
// 仅统计「失败」尝试：成功登录会清空计数，避免 NAT 后的正常用户被误伤。
// 表中条目会被定期清理，并设有硬上限，防止伪造来源 IP 撑爆内存。
var (
	loginAttempts  = make(map[string][]time.Time)
	loginMu        sync.Mutex
	lastLoginSweep time.Time
)

const (
	loginMaxAttempts = 5               // 窗口内最大失败次数
	loginWindow      = 5 * time.Minute // 时间窗口
	maxTrackedIPs    = 100000          // 同时跟踪的来源 IP 上限
)

// sweepLoginAttemptsLocked 清理所有过期条目（调用方须持有 loginMu）。
// 按 loginWindow 节流，避免每个请求都做一次全表遍历。
func sweepLoginAttemptsLocked(now time.Time) {
	if !lastLoginSweep.IsZero() && now.Sub(lastLoginSweep) < loginWindow {
		return
	}
	lastLoginSweep = now

	for ip, times := range loginAttempts {
		kept := times[:0]
		for _, t := range times {
			if now.Sub(t) < loginWindow {
				kept = append(kept, t)
			}
		}
		if len(kept) == 0 {
			delete(loginAttempts, ip)
		} else {
			loginAttempts[ip] = kept
		}
	}
}

// checkLoginRateLimit 判断该来源是否已被限流（只读，不计数）
func checkLoginRateLimit(clientIP string) bool {
	now := time.Now()
	loginMu.Lock()
	defer loginMu.Unlock()

	sweepLoginAttemptsLocked(now)

	recent := 0
	for _, t := range loginAttempts[clientIP] {
		if now.Sub(t) < loginWindow {
			recent++
		}
	}
	return recent < loginMaxAttempts
}

// recordLoginFailure 记录一次登录失败
func recordLoginFailure(clientIP string) {
	now := time.Now()
	loginMu.Lock()
	defer loginMu.Unlock()

	// 内存保护：达到上限时强制清理一次；若仍满载则放弃记录新来源，
	// 宁可降低对新 IP 的计数精度，也不让 map 无界增长。
	if len(loginAttempts) >= maxTrackedIPs {
		if _, exists := loginAttempts[clientIP]; !exists {
			lastLoginSweep = time.Time{}
			sweepLoginAttemptsLocked(now)
			if len(loginAttempts) >= maxTrackedIPs {
				return
			}
		}
	}

	loginAttempts[clientIP] = append(loginAttempts[clientIP], now)
}

// resetLoginAttempts 登录成功后清空该来源的失败记录
func resetLoginAttempts(clientIP string) {
	loginMu.Lock()
	defer loginMu.Unlock()
	delete(loginAttempts, clientIP)
}

// LoginRequest 登录请求
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Login 用户登录
func Login(c *gin.Context) {
	// 登录前限流
	if !checkLoginRateLimit(c.ClientIP()) {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"success": false,
			"message": "尝试过于频繁，请稍后再试",
		})
		return
	}

	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 解析失败同样计入失败次数。
		//
		// 原实现只在凭证错误时计数，于是「必然解析失败」的请求（缺字段、
		// 体积超限）走的是不计数、不受限的分支——登录接口因此成了一个
		// 无需任何凭证、不受速率约束的入口，配合超大请求体即可稳定打满内存。
		// 计数口径必须覆盖所有失败，而不只是「密码错」这一种。
		recordLoginFailure(c.ClientIP())

		if common.IsBodyTooLarge(err) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{
				"success": false,
				"message": "请求体过大",
			})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "用户名和密码不能为空",
		})
		return
	}

	// 验证用户名密码
	user := model.User{
		Username: req.Username,
		Password: req.Password,
	}

	if err := user.ValidateAndFill(); err != nil {
		// 仅失败计入限流，避免正常用户被自己的成功登录挤出配额
		recordLoginFailure(c.ClientIP())
		// 统一错误文案，避免账号枚举
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "用户名或密码错误",
		})
		return
	}

	// 登录成功，清空失败计数
	resetLoginAttempts(c.ClientIP())

	// 生成 JWT Token（带上当前令牌版本，改密后旧令牌即失效）
	token, err := common.GenerateToken(user.ID, user.Username, user.Role, user.TokenVersion)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "生成Token失败",
		})
		return
	}

	// 生成数据加密密钥
	dataKey, err := common.GenerateDataKey()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "生成密钥失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "登录成功",
		"data": gin.H{
			"token":    token,
			"data_key": dataKey,
			"user": gin.H{
				"id":           user.ID,
				"username":     user.Username,
				"display_name": user.DisplayName,
				"role":         user.Role,
				"status":       user.Status,
				"email":        user.Email,
				"credits":      user.Credits,
				"used_credits": user.UsedCredits,
			},
		},
	})
}

// UpdateProfileRequest 更新个人资料请求
type UpdateProfileRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

// UpdateProfile 更新当前登录用户资料
func UpdateProfile(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "未登录",
		})
		return
	}

	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "请求参数无效",
		})
		return
	}

	updates := map[string]interface{}{}
	if req.Username != "" {
		updates["username"] = req.Username
	}
	if req.DisplayName != "" {
		updates["display_name"] = req.DisplayName
	}
	if req.Email != "" {
		updates["email"] = req.Email
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "没有需要更新的字段",
		})
		return
	}

	err := model.DB.Model(&model.User{}).Where("id = ?", userID).Updates(updates).Error
	if err != nil {
		common.SysErrorf("[UpdateProfile] 更新资料失败: userID=%v, err=%v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "更新失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "资料更新成功",
	})
}

// ResetMyPasswordRequest 用户重置自己密码请求
type ResetMyPasswordRequest struct {
	DataKey string `json:"data_key" binding:"required"`
}

// ResetMyPassword 用户重置自己的密码（随机生成，AES-GCM 加密返回）
func ResetMyPassword(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "未登录"})
		return
	}

	var req ResetMyPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请提供 data_key"})
		return
	}

	newPassword := common.GenerateRandomPassword(12)

	hashed, err := common.Password2Hash(newPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "密码加密失败"})
		return
	}

	// 同时递增令牌版本：重置密码的常见动机是「账号可能被盗」，
	// 此时旧令牌必须立刻作废，否则应急处理形同虚设。
	if err := model.UpdatePasswordAndInvalidateTokens(userID.(int), hashed); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "重置密码失败"})
		return
	}

	encrypted, err := common.EncryptWithKey(newPassword, req.DataKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "密码加密传输失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "密码重置成功",
		"data": gin.H{
			"encrypted_password": encrypted,
		},
	})
}

// GetUserInfo 获取当前登录用户信息
func GetUserInfo(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "未登录",
		})
		return
	}

	user, err := model.GetUserByID(userID.(int))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "获取用户信息失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"id":           user.ID,
			"username":     user.Username,
			"display_name": user.DisplayName,
			"role":         user.Role,
			"status":       user.Status,
			"email":        user.Email,
			"credits":      user.Credits,
			"used_credits": user.UsedCredits,
		},
	})
}
