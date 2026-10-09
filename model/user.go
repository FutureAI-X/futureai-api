package model

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"strconv"
	"time"

	"github.com/FutureAI-X/futureai-api/common"
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

	// TokenVersion 令牌版本号，每次改密递增，用于立即作废已签发的 JWT。
	// 没有它时密码泄露后改密无效——旧令牌仍可用满 24 小时。
	TokenVersion int `json:"-" gorm:"default:0"`

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

	// KeyHash API Key 的 SHA-256（十六进制），用于认证时查找。**库里不存明文**。
	//
	// 为什么用户凭证要哈希、而供应商密钥是加密：供应商密钥是我们**需要**还原出来
	// 转发给上游的，只能可逆；用户 API Key 只在认证时比对一次，没有任何环节需要
	// 读回明文。明文入库意味着「拿到库或备份 = 拿到全部用户凭证」，而备份里本来
	// 就带着 .env（见 deploy/backup.sh），这一条把那个后果降到只剩上游密钥。
	//
	// 用 SHA-256 而不是 bcrypt：key 是 32 位随机字符（约 165 bit 熵），不存在
	// 字典攻击，慢哈希只会拖慢每个 /v1 请求；而且哈希后仍能建唯一索引直接查，
	// bcrypt 每行盐不同，根本没法按值查。
	//
	// 刻意可空（不带 not null）：唯一索引允许 NULL 并存，存量表在回填之前整列都是
	// NULL，写成 not null 会让 AutoMigrate 建索引时撞上重复的空值而失败。
	KeyHash string `json:"-" gorm:"uniqueIndex;size:64"`

	// KeyPrefix / KeySuffix 只用于列表展示（如 sk-a1b2c3…xy9z），让用户能分辨
	// 是哪一把 Key。代价是把 9 个随机字符暴露给「能读到库」的人，剩余熵仍远超
	// 可暴力破解的范围。
	KeyPrefix string `json:"key_prefix" gorm:"size:16"`
	KeySuffix string `json:"key_suffix" gorm:"size:8"`

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

// apiKeyPrefixLen / apiKeySuffixLen 是列表展示时保留的明文位数。
// 12 位（含固定的 "sk-" 前缀）足以让用户分辨是哪一把 Key。
const (
	apiKeyPrefixLen = 8
	apiKeySuffixLen = 4
)

// HashAPIKey 计算 API Key 的存储哈希（十六进制 SHA-256）。
func HashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// APIKeyDisplayParts 切出用于展示的前缀与后缀。
// 对短于 12 位的 key（历史遗留或人工插入）整体作为前缀返回，
// 避免切片越界，也避免前后缀重叠导致看起来像把整个 key 打印出来。
func APIKeyDisplayParts(key string) (prefix, suffix string) {
	if len(key) <= apiKeyPrefixLen+apiKeySuffixLen {
		return key, ""
	}
	return key[:apiKeyPrefixLen], key[len(key)-apiKeySuffixLen:]
}

// MaskAPIKey 把前缀与后缀拼成列表展示值，如 sk-a1b2c3…xy9z。
func MaskAPIKey(prefix, suffix string) string {
	if prefix == "" && suffix == "" {
		return ""
	}
	if suffix == "" {
		return prefix
	}
	return prefix + "…" + suffix
}

// GetTokenByKey 按明文 API Key 查找启用中的 Token。
// 库里只存哈希，因此这里先把明文哈希一次再查——不需要解密，也无从解密。
func GetTokenByKey(key string) (*Token, error) {
	var token Token
	err := DB.Where("key_hash = ? AND status = ?", HashAPIKey(key), TokenStatusEnabled).First(&token).Error
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

// IsTokenHashExists 检查该哈希是否已被占用（排除已删除）。
// 注意查的是哈希而不是明文：库里已经没有明文可比。
func IsTokenHashExists(hash string) bool {
	var count int64
	DB.Model(&Token{}).Where("key_hash = ? AND status != ?", hash, TokenStatusDeleted).Count(&count)
	return count > 0
}

// GenerateTokenKey 生成一个新的 API Key 及其派生的存储字段。
//
// 明文 key **只在这个返回值里出现一次**：调用方返回给用户之后即应丢弃，
// 之后再也无法从库里还原（这正是本次改造的目的）。
func GenerateTokenKey() (key, hash, prefix, suffix string, err error) {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	for i := 0; i < 10; i++ { // 最多重试10次
		result := make([]byte, 32)
		for j := range result {
			n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
			result[j] = chars[n.Int64()]
		}
		key = "sk-" + string(result)
		hash = HashAPIKey(key)
		if !IsTokenHashExists(hash) {
			prefix, suffix = APIKeyDisplayParts(key)
			return key, hash, prefix, suffix, nil
		}
	}
	return "", "", "", "", errors.New("failed to generate unique token key")
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
			// 用户不存在时也要付出一次 bcrypt 的代价：直接返回会比
			// 「用户存在但密码错」快上百毫秒，足以枚举出哪些用户名存在。
			common.CompareDummyPassword(password)
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

// UpdatePasswordAndInvalidateTokens 更新密码并作废该用户已签发的所有令牌。
//
// 两件事必须在同一条 UPDATE 里完成：分两步做的话，中间失败会留下
// 「密码已改、旧令牌仍然有效」的状态——恰恰是应急改密最不希望的结果。
//
// 用数据库端自增而非先读后写：并发改密时两种写法结果都是作废旧令牌，
// 但自增不会丢失更新。
func UpdatePasswordAndInvalidateTokens(id int, hashedPassword string) error {
	result := DB.Model(&User{}).Where("id = ?", id).Updates(map[string]interface{}{
		"password":      hashedPassword,
		"token_version": gorm.Expr("token_version + 1"),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("用户不存在")
	}
	return nil
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
		// 行级锁读取当前余额：日志里必须记**实际发生的**变动，而不是请求值。
		// subtract 在余额不足时会被夹到 0（扣不动那么多），过去日志仍记请求值，
		// 于是「余额只剩 1、管理员扣 100」会写成 -100 而实际只少了 1，
		// sum(credit_logs) 与余额变化从此对不上。
		var user User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", id).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("用户不存在")
			}
			return err
		}

		// actual 是本次调整真正改变的数额（add 取正值，subtract 取被扣掉的部分，
		// override 取差值的带符号数）。刻意按模式直接算，而不是事后用减法求差：
		// 浮点减法会引入 0.30000000000000004 这类噪声，写进账本又得再收敛一遍。
		//
		// 注意不要顺手把 user.Credits 也收敛一遍：库里允许存在早期残留的
		// 超精度余额（见 credit.go 的说明），重算会把它悄悄抹掉。
		var newCredits, actual float64
		switch mode {
		case "add":
			actual = value
			newCredits = user.Credits + value
		case "subtract":
			// 与原来的 GREATEST(credits - ?, 0) 等价：余额不会被扣成负数
			actual = math.Min(value, user.Credits)
			newCredits = user.Credits - actual
		case "override":
			actual = value - user.Credits
			newCredits = value
		default:
			return errors.New("invalid mode: must be add, subtract, or override")
		}

		if err := tx.Model(&User{}).Where("id = ?", id).
			Update("credits", newCredits).Error; err != nil {
			return err
		}

		// 没有实际变动（例如把余额覆盖成原值）不写日志：
		// 记一条「增加 0 积分」只会给对账添噪声。
		if actual == 0 {
			return nil
		}

		// 备注按最短形式输出：写死位数只会和实际入账对不上
		amount := strconv.FormatFloat(math.Abs(actual), 'f', -1, 64)
		var remark string
		switch mode {
		case "add":
			remark = "管理员增加积分 " + amount
		case "subtract":
			remark = "管理员扣减积分 " + amount
		case "override":
			// 覆盖记的是结果值：它才是这次操作想表达的信息，
			// Credits 列仍记实际差额，两者各司其职
			remark = "管理员覆盖积分为 " + strconv.FormatFloat(newCredits, 'f', -1, 64)
		}

		return tx.Create(&CreditLog{
			UserID:  id,
			Credits: math.Abs(actual),
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
