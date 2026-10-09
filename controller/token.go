package controller

import (
	"net/http"
	"strconv"

	"github.com/FutureAI-X/futureai-api/common"
	"github.com/FutureAI-X/futureai-api/model"
	"github.com/gin-gonic/gin"
)

// GetTokens 获取当前用户的 Token 列表
func GetTokens(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "未登录"})
		return
	}

	tokens, err := model.GetTokensByUserID(userID.(int))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "获取 Token 列表失败"})
		return
	}

	type tokenResponse struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		// 只返回前后各几位。库里存的是哈希，完整明文已经不可恢复——
		// 连平台自己也拿不到，所以这里没有「传 data_key 就返回完整值」的分支。
		KeyPrefix string `json:"key_prefix"`
		KeySuffix string `json:"key_suffix"`
		// KeyLength 让前端能把中间的遮蔽渲染成与真实 key 等长
		KeyLength int    `json:"key_length"`
		Status    int    `json:"status"`
		CreatedAt string `json:"created_at"`
	}

	items := make([]tokenResponse, len(tokens))
	for i, t := range tokens {
		items[i] = tokenResponse{
			ID:        t.ID,
			Name:      t.Name,
			KeyPrefix: t.KeyPrefix,
			KeySuffix: t.KeySuffix,
			KeyLength: t.KeyLength,
			Status:    t.Status,
			CreatedAt: t.CreatedAt.Format("2006-01-02 15:04:05"),
		}
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}

// CreateTokenRequest 创建 Token 请求
type CreateTokenRequest struct {
	Name    string `json:"name" binding:"required"`
	DataKey string `json:"data_key" binding:"required"`
}

// CreateToken 创建 Token
func CreateToken(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "未登录"})
		return
	}

	var req CreateTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "名称不能为空"})
		return
	}

	key, hash, prefix, suffix, err := model.GenerateTokenKey()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "生成 Key 失败"})
		return
	}

	// 入库的只有哈希与前后缀：明文 key 不落库，只在这条响应里出现一次
	token := model.Token{
		UserID:      userID.(int),
		Name:        req.Name,
		KeyHash:     hash,
		KeyPrefix:   prefix,
		KeySuffix:   suffix,
		KeyLength:   len(key),
		Status:      1,
		ExpiredTime: -1,
	}

	if err := model.CreateToken(&token); err != nil {
		common.SysErrorf("[CreateToken] 创建 Token 失败: userID=%v, err=%v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "创建 Token 失败"})
		return
	}

	// 创建时是唯一一次能看到完整 key 的机会，用 data_key 加密后返回。
	// 之后列表接口只会给前后缀（库里也再没有明文可解）。
	encryptedKey, err := common.EncryptWithKey(key, req.DataKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Key 加密失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Token 创建成功",
		"data": gin.H{
			"id":         token.ID,
			"name":       token.Name,
			"key":        encryptedKey,
			"created_at": token.CreatedAt.Format("2006-01-02 15:04:05"),
		},
	})
}

// UpdateTokenRequest 更新 Token 请求
type UpdateTokenRequest struct {
	Name string `json:"name" binding:"required"`
}

// UpdateToken 更新 Token 名称
func UpdateToken(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "未登录"})
		return
	}

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的 Token ID"})
		return
	}

	token, err := model.GetTokenByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "Token 不存在"})
		return
	}

	if token.UserID != userID.(int) {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "无权操作此 Token"})
		return
	}

	var req UpdateTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "名称不能为空"})
		return
	}

	if err := model.UpdateTokenName(id, req.Name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "更新失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "更新成功"})
}

// DeleteToken 删除 Token
func DeleteToken(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "未登录"})
		return
	}

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的 Token ID"})
		return
	}

	token, err := model.GetTokenByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "Token 不存在"})
		return
	}

	if token.UserID != userID.(int) {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "无权操作此 Token"})
		return
	}

	if err := model.DeleteToken(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "删除失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Token 已删除"})
}
