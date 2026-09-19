package model

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"strconv"
	"time"

	"github.com/FutureAI/token-hub/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 用户角色常量
const (
	RoleCommonUser = 1   // 普通用户
	RoleAdminUser  = 10  // 管理员
	RoleRootUser   = 100 // 超级管理员
)

// 用户状态常量
const (
	UserStatusEnabled  = 1 // 启用
	UserStatusDisabled = 2 // 禁用
	UserStatusDeleted  = 3 // 已删除
)

// 错误定义
var (
	ErrInvalidCredentials   = errors.New("invalid username or password")
	ErrUserDisabled         = errors.New("user is disabled")
	ErrUserDeleted          = errors.New("user is deleted")
	ErrUserEmptyCredentials = errors.New("username or password is empty")
	ErrInsufficientCredits  = errors.New("insufficient credits")
	// ErrCreditPrecision 积分的小数位超过业务精度（model.CreditPrecision）。
	// 调用方应据此返回 400 而不是 500：这是输入问题，不是服务故障。
	ErrCreditPrecision = errors.New("积分的小数位超过允许的位数")
)

// User 用户模型
type User struct {
	// 唯一标识，自增主键
	ID int `json:"id" gorm:"primaryKey"`

	// 用户名，用于登录，全局唯一，长度限制32字符
	Username string `json:"username" gorm:"uniqueIndex;size:32;not null"`

	// 显示名称，用于界面展示，长度限制64字符
	DisplayName string `json:"display_name" gorm:"size:64"`

	// 角色：1=普通用户, 10=管理员, 100=root 超级管理员
	Role int `json:"role" gorm:"default:1"`

	// 状态：1=启用, 2=禁用, 3=已删除
	Status int `json:"status" gorm:"default:1"`

	// 当前积分，0 表示无积分。标度刻意宽于业务精度 CreditPrecision
	Credits float64 `json:"credits" gorm:"type:numeric(20,10);default:0"`

	// 已使用积分，标度同上
	UsedCredits float64 `json:"used_credits" gorm:"type:numeric(20,10);default:0"`

	// 邮箱，用于通知和找回密码，长度限制64字符
	Email string `json:"email" gorm:"size:64"`

	// 密码哈希值，使用 bcrypt 加密存储，JSON 序列化时忽略
	Password string `json:"-" gorm:"not null"`

	// 记录创建时间，自动设置
	CreatedAt time.Time `json:"created_at"`

	// 记录最后更新时间，自动更新
	UpdatedAt time.Time `json:"updated_at"`
}

// Token API Token 模型（对应 api_keys 表，存储用户 API 访问密钥）
type Token struct {
	// 唯一标识，自增主键
	ID int `json:"id" gorm:"primaryKey"`

	// 所属用户ID，关联 users 表
	UserID int `json:"user_id" gorm:"index"`

	// 密钥名称，便于用户识别
	Name string `json:"name" gorm:"size:64"`

	// 密钥，用于 API 认证，全局唯一
	Key string `json:"key" gorm:"uniqueIndex;size:64;not null"`

	// 状态：1=启用, 2=禁用, 3=已删除
	Status int `json:"status" gorm:"default:1"`

	// 过期时间戳，-1 表示永不过期
	ExpiredTime int64 `json:"expired_time" gorm:"default:-1"`

	// 记录创建时间
	CreatedAt time.Time `json:"created_at"`

	// 记录最后更新时间
	UpdatedAt time.Time `json:"updated_at"`
}

// Token 状态常量
const (
	TokenStatusEnabled  = 1 // 启用
	TokenStatusDisabled = 2 // 禁用
	TokenStatusDeleted  = 3 // 已删除
)

// TableName 指定表名（原为 tokens，已更名为 api_keys）
func (Token) TableName() string {
	return "api_keys"
}

// GetTokensByUserID 获取用户的 Token 列表（排除已删除）
func GetTokensByUserID(userID int) ([]Token, error) {
	var tokens []Token
	err := DB.Where("user_id = ? AND status != ?", userID, TokenStatusDeleted).Order("id ASC").Find(&tokens).Error
	return tokens, err
}

// GetTokenByID 根据 ID 获取 Token（排除已删除）
func GetTokenByID(id int) (*Token, error) {
	var token Token
	err := DB.Where("id = ? AND status != ?", id, TokenStatusDeleted).First(&token).Error
	if err != nil {
		return nil, err
	}
	return &token, nil
}

// GetTokenByKey 根据 Key 获取 Token
func GetTokenByKey(key string) (*Token, error) {
	var token Token
	err := DB.Where("key = ? AND status = ?", key, 1).First(&token).Error
	if err != nil {
		return nil, err
	}
	return &token, nil
}

// CreateToken 创建 Token
func CreateToken(token *Token) error {
	return DB.Create(token).Error
}

// UpdateTokenName 更新 Token 名称
func UpdateTokenName(id int, name string) error {
	return DB.Model(&Token{}).Where("id = ?", id).Update("name", name).Error
}

// DeleteToken 删除 Token（置为已删除状态，非物理删除）
func DeleteToken(id int) error {
	return DB.Model(&Token{}).Where("id = ?", id).Update("status", TokenStatusDeleted).Error
}

// IsTokenKeyExists 检查 Token Key 是否已存在（排除已删除）
func IsTokenKeyExists(key string) bool {
	var count int64
	DB.Model(&Token{}).Where("key = ? AND status != ?", key, TokenStatusDeleted).Count(&count)
	return count > 0
}

// GenerateUniqueTokenKey 生成唯一的 Token Key（sk- + 32位小写字母数字）
func GenerateUniqueTokenKey() (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	for i := 0; i < 10; i++ { // 最多重试10次
		result := make([]byte, 32)
		for j := range result {
			n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
			result[j] = chars[n.Int64()]
		}
		key := "sk-" + string(result)
		if !IsTokenKeyExists(key) {
			return key, nil
		}
	}
	return "", errors.New("failed to generate unique token key")
}

// ValidateAndFill 验证用户名密码并填充用户信息
func (user *User) ValidateAndFill() error {
	password := user.Password
	if user.Username == "" || password == "" {
		return ErrUserEmptyCredentials
	}

	// 根据用户名查询用户
	err := DB.Where("username = ?", user.Username).First(user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInvalidCredentials
		}
		return err
	}

	// 验证密码
	if !common.ValidatePasswordAndHash(password, user.Password) {
		return ErrInvalidCredentials
	}

	// 检查用户状态
	if user.Status == UserStatusDeleted {
		return ErrUserDeleted
	}
	if user.Status != UserStatusEnabled {
		return ErrUserDisabled
	}

	return nil
}

// GetUserByUsername 根据用户名获取用户（不含已删除）
func GetUserByUsername(username string) (*User, error) {
	var user User
	err := DB.Where("username = ? AND status != ?", username, UserStatusDeleted).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserByID 根据ID获取用户（不含已删除）
func GetUserByID(id int) (*User, error) {
	var user User
	err := DB.Where("id = ? AND status != ?", id, UserStatusDeleted).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUsernameMap 获取用户ID到用户名的映射
func GetUsernameMap() (map[int]string, error) {
	var users []User
	err := DB.Select("id", "username").Find(&users).Error
	if err != nil {
		return nil, err
	}
	m := make(map[int]string, len(users))
	for _, u := range users {
		m[u.ID] = u.Username
	}
	return m, nil
}

// GetUsers 分页查询用户列表，支持关键词搜索
func GetUsers(page, pageSize int, keyword string) ([]User, int64, error) {
	var users []User
	var total int64

	query := DB.Model(&User{}).Where("status != ?", UserStatusDeleted)
	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("username LIKE ? OR display_name LIKE ? OR email LIKE ?", like, like, like)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := query.Order("id ASC").Offset(offset).Limit(pageSize).Find(&users).Error; err != nil {
		return nil, 0, err
	}

	return users, total, nil
}

// AdminCreateUser 管理员创建用户
func AdminCreateUser(user *User) error {
	hashedPassword, err := common.Password2Hash(user.Password)
	if err != nil {
		return err
	}
	user.Password = hashedPassword
	return DB.Create(user).Error
}

// AdminDeleteUser 管理员删除用户（标记为已删除）
func AdminDeleteUser(id int) error {
	return UpdateUserStatus(id, UserStatusDeleted)
}

// UpdateUserStatus 更新用户状态
func UpdateUserStatus(id int, status int) error {
	return DB.Model(&User{}).Where("id = ?", id).Update("status", status).Error
}

// UpdateUser 更新用户信息
func UpdateUser(id int, updates map[string]interface{}) error {
	return DB.Model(&User{}).Where("id = ?", id).Updates(updates).Error
}

// AdjustUserCredits 调整用户积分。写 users.credits 的第三个（也是最后一个）入口，
// 与 deductCreditsTx / refundCreditsTx 一样在边界上守精度。
//
// 全程使用数据库端原子表达式（credits = credits ± ?），不再读-改-写：
// 并发调整时原来的实现会丢失更新（TOCTOU），导致余额与审计不一致。
// 同时写入一条 CreditLog，使管理员调整可追溯。
//
// 这里刻意是「拒绝」而不是像扣费那样「静默收敛」：金额是管理员手输的，
// 把他的 1.2345 悄悄改成 1.234 会让人以为系统记账不准；扣费金额是算出来的，
// 收敛掉浮点噪声才是正确行为。这个差异是有意的。
func AdjustUserCredits(id int, mode string, value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return errors.New("积分调整值必须是非负数")
	}
	// 位数守卫：这里曾是唯一的漏网点——不校验也不收敛，管理员可以直接往库里
	// 塞 6 位小数，于是「库里只有 CreditPrecision 位小数」这个前提不成立，
	// 往后每一次「业务精度放宽」都要先面对一批精度不明的存量数据。
	if !ValidCreditPrecision(value) {
		return ErrCreditPrecision
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		var expr clause.Expr
		var remark string

		// 备注按最短形式输出：值已收敛到业务精度，写死位数只会和实际入账对不上
		amount := strconv.FormatFloat(value, 'f', -1, 64)

		switch mode {
		case "add":
			expr = gorm.Expr("credits + ?", value)
			remark = "管理员增加积分 " + amount
		case "subtract":
			// GREATEST 保证余额不会变成负数
			expr = gorm.Expr("GREATEST(credits - ?, 0)", value)
			remark = "管理员扣减积分 " + amount
		case "override":
			expr = gorm.Expr("?", value)
			remark = "管理员覆盖积分为 " + amount
		default:
			return errors.New("invalid mode: must be add, subtract, or override")
		}

		result := tx.Model(&User{}).Where("id = ?", id).Update("credits", expr)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("用户不存在")
		}

		return tx.Create(&CreditLog{
			UserID:  id,
			Credits: value,
			Type:    CreditLogTypeAdjust,
			Remark:  remark,
		}).Error
	})
}

// rootInitialPasswordFile 初始密码落盘位置（仅首次创建 root 时写入）
const rootInitialPasswordFile = "root_initial_password.txt"

// CreateRootUserIfNeed 创建 root 用户（如果不存在）
func CreateRootUserIfNeed() error {
	var count int64
	DB.Model(&User{}).Count(&count)
	if count > 0 {
		return nil
	}

	// 优先采用环境变量提供的初始密码；未提供则随机生成，避免任何已知默认密码
	initialPassword := os.Getenv("INITIAL_ROOT_PASSWORD")
	generated := false
	if initialPassword == "" {
		initialPassword = common.GenerateRandomPassword(16)
		generated = true
	}

	hashedPassword, err := common.Password2Hash(initialPassword)
	if err != nil {
		return err
	}

	rootUser := User{
		Username:    "root",
		Password:    hashedPassword,
		DisplayName: "Root User",
		Role:        RoleRootUser,
		Status:      UserStatusEnabled,
		Credits:     100000000,
	}

	if err := DB.Create(&rootUser).Error; err != nil {
		return err
	}

	// 初始密码绝不写入日志：日志会流向容器日志、journald 或第三方聚合平台，
	// 可见范围远大于数据库本身。改为写入仅属主可读的文件（0600）。
	if !generated {
		common.SysErrorf("已创建 root 用户(用户名: root)，初始密码取自 INITIAL_ROOT_PASSWORD 环境变量，请立即登录修改密码")
		return nil
	}

	if err := os.WriteFile(rootInitialPasswordFile, []byte(initialPassword+"\n"), 0600); err != nil {
		return fmt.Errorf("root 用户已创建，但初始密码写入文件失败: %w（请删除该用户，设置 INITIAL_ROOT_PASSWORD 后重启）", err)
	}
	common.SysErrorf("已创建 root 用户(用户名: root)，初始密码已写入 %s（权限 0600）。请立即登录修改密码并删除该文件", rootInitialPasswordFile)
	return nil
}
