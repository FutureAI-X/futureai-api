package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/FutureAI/token-hub/common"
	"github.com/FutureAI/token-hub/model"
	"github.com/gin-gonic/gin"
)

// AdminGetUsers 管理员获取用户列表
func AdminGetUsers(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("p", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	keyword := c.Query("keyword")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	users, total, err := model.GetUsers(page, pageSize, keyword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "获取用户列表失败",
		})
		return
	}

	// 构建响应，隐藏密码等敏感信息
	type userResponse struct {
		ID          int    `json:"id"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Role        int    `json:"role"`
		Status      int    `json:"status"`
		Email       string `json:"email"`
		Credits     float64 `json:"credits"`
		UsedCredits float64 `json:"used_credits"`
		CreatedAt   string `json:"created_at"`
	}

	items := make([]userResponse, len(users))
	for i, u := range users {
		items[i] = userResponse{
			ID:          u.ID,
			Username:    u.Username,
			DisplayName: u.DisplayName,
			Role:        u.Role,
			Status:      u.Status,
			Email:       u.Email,
			Credits:     u.Credits,
			UsedCredits: u.UsedCredits,
			CreatedAt:   u.CreatedAt.Format("2006-01-02 15:04:05"),
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"items": items,
			"total": total,
		},
	})
}

// AdminCreateUserRequest 创建用户请求
type AdminCreateUserRequest struct {
	Username    string `json:"username" binding:"required"`
	Password    string `json:"password" binding:"required"`
	DisplayName string `json:"display_name"`
	Role        int    `json:"role"`
	Credits     float64 `json:"credits"`
}

// AdminCreateUser 管理员创建用户
func AdminCreateUser(c *gin.Context) {
	var req AdminCreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "用户名和密码不能为空",
		})
		return
	}

	// 验证角色：不允许创建 root 用户
	if req.Role >= model.RoleRootUser {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "不允许创建超级管理员用户",
		})
		return
	}

	// 默认角色为普通用户
	if req.Role == 0 {
		req.Role = model.RoleCommonUser
	}

	user := model.User{
		Username:    req.Username,
		Password:    req.Password,
		DisplayName: req.DisplayName,
		Role:        req.Role,
		Status:      model.UserStatusEnabled,
		Credits:     req.Credits,
	}

	if err := model.AdminCreateUser(&user); err != nil {
		common.SysErrorf("[AdminCreateUser] 创建用户失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "创建用户失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "用户创建成功",
	})
}

// AdminUpdateUserRequest 更新用户请求
type AdminUpdateUserRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
}

// AdminUpdateUser 管理员更新用户信息
func AdminUpdateUser(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的用户ID"})
		return
	}

	var req AdminUpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求参数无效"})
		return
	}

	user, err := model.GetUserByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "用户不存在"})
		return
	}

	if user.Role >= model.RoleRootUser {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "不允许修改超级管理员"})
		return
	}

	updates := map[string]interface{}{}
	if req.Username != "" {
		updates["username"] = req.Username
	}
	if req.DisplayName != "" {
		updates["display_name"] = req.DisplayName
	}
	if req.Password != "" {
		hashed, err := common.Password2Hash(req.Password)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "密码加密失败"})
			return
		}
		updates["password"] = hashed
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "没有需要更新的字段"})
		return
	}

	if err := model.UpdateUser(id, updates); err != nil {
		common.SysErrorf("[AdminUpdateUser] 更新用户失败: id=%d, err=%v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "更新用户失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "用户更新成功"})
}

// AdminDeleteUser 管理员删除用户
func AdminDeleteUser(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的用户ID",
		})
		return
	}

	// 不允许删除 root 用户
	user, err := model.GetUserByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "用户不存在",
		})
		return
	}

	if user.Role >= model.RoleRootUser {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "不允许删除超级管理员",
		})
		return
	}

	if err := model.AdminDeleteUser(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "删除用户失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "用户已删除",
	})
}

// AdminUpdateUserStatusRequest 更新用户状态请求
type AdminUpdateUserStatusRequest struct {
	Status int `json:"status" binding:"required"`
}

// AdminUpdateUserStatus 管理员更新用户状态
func AdminUpdateUserStatus(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的用户ID",
		})
		return
	}

	var req AdminUpdateUserStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "状态值无效",
		})
		return
	}

	if req.Status != model.UserStatusEnabled && req.Status != model.UserStatusDisabled {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "状态值无效，必须为 1(启用) 或 2(禁用)",
		})
		return
	}

	// 不允许禁用 root 用户
	user, err := model.GetUserByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "用户不存在",
		})
		return
	}

	// 不允许通过状态接口恢复已删除的用户
	if user.Status == model.UserStatusDeleted {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "已删除的用户无法修改状态",
		})
		return
	}

	if user.Role >= model.RoleRootUser {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "不允许修改超级管理员状态",
		})
		return
	}

	if err := model.UpdateUserStatus(id, req.Status); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "更新用户状态失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "用户状态已更新",
	})
}

// AdminAdjustUserCreditsRequest 积分调整请求
type AdminAdjustUserCreditsRequest struct {
	Mode  string  `json:"mode" binding:"required"`
	Value float64 `json:"value" binding:"required"`
}

// AdminAdjustUserCredits 管理员调整用户积分
func AdminAdjustUserCredits(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的用户ID",
		})
		return
	}

	var req AdminAdjustUserCreditsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "请提供调整模式和数值",
		})
		return
	}

	if req.Mode != "add" && req.Mode != "subtract" && req.Mode != "override" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "调整模式无效，必须为 add, subtract 或 override",
		})
		return
	}

	if req.Value < 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "数值不能为负数",
		})
		return
	}

	// 位数必须与业务精度一致：这个口子以前不校验，管理员可以往库里塞任意位数的小数，
	// 之后每一次放宽精度都要先面对一批精度不明的存量余额。
	if !model.ValidCreditPrecision(req.Value) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "积分最多支持 " + strconv.Itoa(model.CreditPrecision) + " 位小数",
		})
		return
	}

	if err := model.AdjustUserCredits(id, req.Mode, req.Value); err != nil {
		common.SysErrorf("[AdminAdjustUserCredits] 积分调整失败: id=%d, mode=%s, err=%v", id, req.Mode, err)
		// 位数问题属于输入错误（上面的校验是同一条规则的重复防线，模型侧也可能先拦下）
		if errors.Is(err, model.ErrCreditPrecision) {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "积分最多支持 " + strconv.Itoa(model.CreditPrecision) + " 位小数",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "积分调整失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "积分调整成功",
	})
}

// AdminResetPasswordRequest 重置密码请求
type AdminResetPasswordRequest struct {
	DataKey string `json:"data_key" binding:"required"`
}

// AdminResetPassword 管理员重置用户密码（随机生成，AES-GCM 加密返回）
func AdminResetPassword(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的用户ID"})
		return
	}

	var req AdminResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请提供 data_key"})
		return
	}

	user, err := model.GetUserByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "用户不存在"})
		return
	}

	if user.Role >= model.RoleRootUser {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "不允许重置超级管理员密码"})
		return
	}

	// 随机生成密码
	newPassword := common.GenerateRandomPassword(12)

	// 存储 bcrypt 哈希
	hashed, err := common.Password2Hash(newPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "密码加密失败"})
		return
	}

	if err := model.UpdateUser(id, map[string]interface{}{"password": hashed}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "重置密码失败"})
		return
	}

	// AES-GCM 加密明文密码后返回
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
