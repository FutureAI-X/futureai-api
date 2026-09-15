package model

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/FutureAI/token-hub/common"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB 全局数据库实例
var DB *gorm.DB

// newGormConfig 创建 GORM 配置
func newGormConfig() *gorm.Config {
	return &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		Logger:                                   logger.Default.LogMode(logger.Silent),
	}
}

// openPostgreSQL 打开 PostgreSQL 连接
func openPostgreSQL(dsn string) (*gorm.DB, error) {
	common.SysLog("using PostgreSQL as database")
	return gorm.Open(postgres.New(postgres.Config{
		DSN:                  dsn,
		PreferSimpleProtocol: true,
	}), newGormConfig())
}

// InitDB 初始化数据库
// 后续扩展 MySQL: 在此函数中根据 SQL_DSN 前缀选择驱动
func InitDB() error {
	dsn := os.Getenv("SQL_DSN")
	if dsn == "" {
		return fmt.Errorf("SQL_DSN environment variable is required")
	}

	db, err := openPostgreSQL(dsn)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	if common.DebugEnabled {
		db = db.Debug()
	}

	DB = db

	// 配置连接池
	sqlDB, err := DB.DB()
	if err != nil {
		return err
	}
	sqlDB.SetMaxIdleConns(common.GetEnvOrDefault("SQL_MAX_IDLE_CONNS", 10))
	sqlDB.SetMaxOpenConns(common.GetEnvOrDefault("SQL_MAX_OPEN_CONNS", 100))
	sqlDB.SetConnMaxLifetime(time.Second * time.Duration(common.GetEnvOrDefault("SQL_MAX_LIFETIME", 60)))

	// 数据库迁移
	common.SysLog("database migration started")
	err = migrateDB()
	if err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	// 更新表注释
	err = addTableComments()
	if err != nil {
		common.SysError("failed to add table comments: " + err.Error())
	}

	// 创建默认 root 用户（如果不存在）
	err = CreateRootUserIfNeed()
	if err != nil {
		common.SysError("failed to create root user: " + err.Error())
	}

	// 创建默认模型数据
	err = createDefaultModels()
	if err != nil {
		common.SysError("failed to create default models: " + err.Error())
	}

	// 计费采用 fail-closed：缺少规则的模型会拒绝服务而非静默免费。
	// 启动时列出这些模型，便于上线前补齐。
	warnModelsWithoutCreditRule()

	// 模型类型参与参考图加价判定，值不合法会让扣费静默偏离预期
	warnInvalidModelTypes()

	return nil
}

// warnInvalidModelTypes 启动时列出类型为空或不在枚举内的模型。
//
// 类型不只是展示字段——参考图附加计费要看它是不是「图像生成」，所以一个非法值
// 会直接改变扣费金额。列出来让运维能提前发现（例如有人直接用 SQL 改过）。
func warnInvalidModelTypes() {
	var models []Model
	if err := DB.Find(&models).Error; err != nil {
		common.SysError("failed to check model types: " + err.Error())
		return
	}

	var invalid []string
	for _, m := range models {
		if !IsValidModelType(m.Type) {
			invalid = append(invalid, fmt.Sprintf("%s(type=%q)", m.Name, m.Type))
		}
	}

	if len(invalid) == 0 {
		return
	}

	common.SysErrorf("[模型] 以下 %d 个模型的类型为空或不在枚举内，参考图计费判定可能异常: %s",
		len(invalid), strings.Join(invalid, ", "))
	common.SysErrorf("[模型] 合法类型: %s；请在管理后台重新选择模型类型", strings.Join(modelTypeKeys(), ", "))
}

// modelTypeKeys 把合法类型拼成可读列表，供告警文案使用
func modelTypeKeys() []string {
	keys := make([]string, len(AllModelTypes))
	for i, t := range AllModelTypes {
		keys[i] = string(t)
	}
	return keys
}

// warnModelsWithoutCreditRule 启动时列出缺少启用计费规则的启用模型。
// 计费是 fail-closed 的：未配置规则的模型会返回「暂不可用」，
// 因此这里必须提前把清单打出来，避免上线后才发现模型不可用。
func warnModelsWithoutCreditRule() {
	var models []Model
	if err := DB.Where("status = ?", 1).Find(&models).Error; err != nil {
		common.SysError("failed to check credit rules: " + err.Error())
		return
	}

	var missing []string
	for _, m := range models {
		var count int64
		if err := DB.Model(&CreditRule{}).
			Where("model_id = ? AND status = ?", m.ID, 1).
			Count(&count).Error; err != nil {
			continue
		}
		if count == 0 {
			missing = append(missing, m.Name)
		}
	}

	if len(missing) == 0 {
		common.SysLogf("[计费] 全部 %d 个启用模型均已配置计费规则", len(models))
		return
	}

	common.SysErrorf("[计费] 以下 %d 个启用模型缺少计费规则，调用将被拒绝: %s",
		len(missing), strings.Join(missing, ", "))
	common.SysErrorf("[计费] 请在管理后台为上述模型配置计费规则；如需免费，请显式配置一条 0 积分的规则")
}

// uniqueIndexPrechecks 需要在建立唯一索引前排查重复数据的表
// （table / columns 均为编译期常量，不来自外部输入）
var uniqueIndexPrechecks = []struct {
	table   string
	columns string
	label   string
}{
	{"vendors", "name", "供应商名称"},
	{"endpoints", "path", "端点路径"},
	{"vendor_models", "vendor_id, model_id", "供应商-模型关联"},
	{"model_endpoints", "model_id, endpoint_id", "模型-端点关联"},
	{"credit_rules", "model_id", "模型积分规则"},
}

// precheckUniqueIndexes 在建立唯一索引前检测重复数据。
// 若存在重复行，GORM 只会抛出底层唯一索引冲突错误，很难定位；
// 这里提前列出具体重复值，让运维可以按提示清理后再启动。
func precheckUniqueIndexes() error {
	var problems []string

	for _, c := range uniqueIndexPrechecks {
		query := fmt.Sprintf(
			"SELECT %s, COUNT(*) AS duplicate_count FROM %s GROUP BY %s HAVING COUNT(*) > 1 LIMIT 20",
			c.columns, c.table, c.columns,
		)

		var rows []map[string]interface{}
		if err := DB.Raw(query).Scan(&rows).Error; err != nil {
			// 表尚不存在（首次部署）时忽略
			continue
		}
		for _, row := range rows {
			problems = append(problems, fmt.Sprintf("  %s(%s): %v", c.label, c.columns, row))
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf(
			"检测到重复数据，无法建立唯一索引。请先清理以下记录后重新启动：\n%s",
			strings.Join(problems, "\n"),
		)
	}
	return nil
}

// migrateDB 数据库迁移
func migrateDB() error {
	// 清理 tasks 表中 user_id 为 NULL 的记录（AutoMigrate 加 NOT NULL 约束前需要先处理）
	if err := DB.Exec("UPDATE tasks SET user_id = 0 WHERE user_id IS NULL").Error; err != nil {
		common.SysLog("pre-migrate cleanup (tasks.user_id nulls): " + err.Error())
	} else {
		common.SysLog("pre-migrate cleanup: tasks.user_id nulls set to 0")
	}

	// 唯一索引前置检查：给出可操作的错误信息，而非底层数据库报错
	if err := precheckUniqueIndexes(); err != nil {
		return err
	}

	err := DB.AutoMigrate(
		&User{},
		&Token{},
		&Vendor{},
		&Model{},
		&VendorModel{},
		&Endpoint{},
		&ModelEndpoint{},
		&Task{},
		&CreditRule{},
		&CreditRuleItem{},
		&CreditRuleCondition{},
		&CreditLog{},
	)
	if err != nil {
		return err
	}

	// 修正历史库上建错的索引：必须放在 AutoMigrate 之后
	if err := ensureUniqueCreditRuleIndex(); err != nil {
		return err
	}

	// 添加表和字段注释
	return addTableComments()
}

// creditRuleModelIndex 是 credit_rules.model_id 上唯一索引的名字，
// 与 CreditRule.ModelID 的 gorm uniqueIndex tag 推导出的默认名一致
const creditRuleModelIndex = "idx_credit_rules_model_id"

// ensureUniqueCreditRuleIndex 把 credit_rules.model_id 上的索引升级为唯一索引。
//
// 为什么需要这个：GORM 的 AutoMigrate 只在索引「不存在」时才创建。历史库上该索引
// 是按早期的非唯一 tag 建出来的，之后加上 uniqueIndex tag 并不会让它变成唯一索引——
// AutoMigrate 看到同名索引已存在就直接跳过。于是 model_id 可以重复插入，
// 而 GetCreditRuleByModelID 用 First() 只取其中一条，产生「管理员改了 A 行、
// 实际生效的是 B 行」这类静默错配（DeleteCreditRuleByModelID 没有 status 过滤，
// 更会删错行）。
//
// 重复数据由 precheckUniqueIndexes 在 AutoMigrate 之前拦截，这里只处理索引本身。
func ensureUniqueCreditRuleIndex() error {
	var uniqueFlags []bool
	err := DB.Raw(`
		SELECT i.indisunique
		FROM pg_class c
		JOIN pg_index i ON i.indexrelid = c.oid
		WHERE c.relname = ?`, creditRuleModelIndex).Scan(&uniqueFlags).Error
	if err != nil {
		return err
	}

	// 索引不存在时 AutoMigrate 已按 uniqueIndex tag 建好，无需处理
	if len(uniqueFlags) == 0 || uniqueFlags[0] {
		return nil
	}

	common.SysLogf("[计费] %s 当前不是唯一索引，重建为唯一索引", creditRuleModelIndex)
	if err := DB.Exec("DROP INDEX IF EXISTS " + creditRuleModelIndex).Error; err != nil {
		return err
	}
	// 索引名来自常量，不涉及外部输入
	if err := DB.Exec(
		"CREATE UNIQUE INDEX " + creditRuleModelIndex + " ON credit_rules (model_id)",
	).Error; err != nil {
		return err
	}
	common.SysLogf("[计费] %s 已重建为唯一索引", creditRuleModelIndex)
	return nil
}

// addTableComments 为 PostgreSQL 表和字段添加注释
func addTableComments() error {
	comments := []string{
		// users 表注释
		`COMMENT ON TABLE users IS '用户表，存储系统用户信息'`,
		`COMMENT ON COLUMN users.id IS '用户唯一标识，自增主键'`,
		`COMMENT ON COLUMN users.username IS '用户名，用于登录，全局唯一'`,
		`COMMENT ON COLUMN users.password IS '密码哈希值，使用 bcrypt 加密存储'`,
		`COMMENT ON COLUMN users.display_name IS '显示名称，用于界面展示'`,
		`COMMENT ON COLUMN users.role IS '用户角色：1=普通用户, 10=管理员, 100=root'`,
		`COMMENT ON COLUMN users.status IS '用户状态：1=启用, 2=禁用, 3=已删除'`,
		`COMMENT ON COLUMN users.email IS '用户邮箱，用于通知和找回密码'`,
		`COMMENT ON COLUMN users.credits IS '用户当前积分'`,
		`COMMENT ON COLUMN users.used_credits IS '已使用积分'`,
		`COMMENT ON COLUMN users.created_at IS '记录创建时间'`,
		`COMMENT ON COLUMN users.updated_at IS '记录最后更新时间'`,

		// api_keys 表注释（原 tokens 表，已更名为 api_keys）
		`COMMENT ON TABLE api_keys IS 'API 密钥表，存储用户 API 访问密钥'`,
		`COMMENT ON COLUMN api_keys.id IS '密钥唯一标识，自增主键'`,
		`COMMENT ON COLUMN api_keys.user_id IS '所属用户ID，关联 users 表'`,
		`COMMENT ON COLUMN api_keys.key IS '密钥，用于 API 认证'`,
		`COMMENT ON COLUMN api_keys.name IS '密钥名称，便于用户识别'`,
		`COMMENT ON COLUMN api_keys.status IS '密钥状态：1=启用, 2=禁用, 3=已删除'`,
		`COMMENT ON COLUMN api_keys.expired_time IS '过期时间戳，-1 表示永不过期'`,
		`COMMENT ON COLUMN api_keys.created_at IS '记录创建时间'`,
		`COMMENT ON COLUMN api_keys.updated_at IS '记录最后更新时间'`,

		// vendors 表注释
		`COMMENT ON TABLE vendors IS '供应商表，存储 AI 模型供应商信息'`,
		`COMMENT ON COLUMN vendors.id IS '供应商唯一标识，自增主键'`,
		`COMMENT ON COLUMN vendors.name IS '供应商名称'`,
		`COMMENT ON COLUMN vendors.description IS '供应商描述'`,
		`COMMENT ON COLUMN vendors.status IS '供应商状态：1=启用, 2=禁用, 3=已删除'`,
		`COMMENT ON COLUMN vendors.created_at IS '记录创建时间'`,
		`COMMENT ON COLUMN vendors.updated_at IS '记录最后更新时间'`,

		// models 表注释
		`COMMENT ON TABLE models IS '模型表，存储可用的 AI 模型信息'`,
		`COMMENT ON COLUMN models.id IS '模型唯一标识，自增主键'`,
		`COMMENT ON COLUMN models.name IS '模型名称，全局唯一，用于 API 调用'`,
		`COMMENT ON COLUMN models.description IS '模型描述信息'`,
		`COMMENT ON COLUMN models.tags IS '模型标签，逗号分隔'`,
		`COMMENT ON COLUMN models.owner IS '模型所有者/提供商'`,
		`COMMENT ON COLUMN models.status IS '模型状态：1=启用, 2=禁用'`,
		`COMMENT ON COLUMN models.type IS '模型类型：image=图像生成, video=视频生成, text=文本生成, music=音乐生成, other=其他'`,
		`COMMENT ON COLUMN models.created_at IS '记录创建时间'`,
		`COMMENT ON COLUMN models.updated_at IS '记录最后更新时间'`,

		// credit_rules 表注释
		`COMMENT ON TABLE credit_rules IS '积分扣除规则表，存储模型的积分扣除算法'`,
		`COMMENT ON COLUMN credit_rules.id IS '规则唯一标识，自增主键'`,
		`COMMENT ON COLUMN credit_rules.model_id IS '关联的模型ID，关联 models 表，唯一'`,
		`COMMENT ON COLUMN credit_rules.rule_type IS '规则类型：per_request=按次计费'`,
		`COMMENT ON COLUMN credit_rules.base_credits IS '基础积分（每次请求扣除的积分数量）'`,
		`COMMENT ON COLUMN credit_rules.description IS '规则描述'`,
		`COMMENT ON COLUMN credit_rules.status IS '规则状态：1=启用, 2=禁用'`,
		`COMMENT ON COLUMN credit_rules.ref_image_credits IS '每张参考图消耗的积分，0=不计费'`,
		`COMMENT ON COLUMN credit_rules.ref_image_params IS '参考图参数名（逗号分隔），空串使用内置默认值'`,
		`COMMENT ON COLUMN credit_rules.created_at IS '记录创建时间'`,
		`COMMENT ON COLUMN credit_rules.updated_at IS '记录最后更新时间'`,

		// credit_rule_items 表注释
		`COMMENT ON TABLE credit_rule_items IS '积分规则参数组合映射表，存储差异化计费的参数组合'`,
		`COMMENT ON COLUMN credit_rule_items.id IS '项唯一标识，自增主键'`,
		`COMMENT ON COLUMN credit_rule_items.rule_id IS '关联的规则ID，关联 credit_rules 表'`,
		`COMMENT ON COLUMN credit_rule_items.credits IS '该参数组合命中部请求时对应的积分'`,
		`COMMENT ON COLUMN credit_rule_items.created_at IS '记录创建时间'`,
		`COMMENT ON COLUMN credit_rule_items.updated_at IS '记录最后更新时间'`,

		// credit_rule_conditions 表注释
		`COMMENT ON TABLE credit_rule_conditions IS '积分规则参数组合条件表，存储每个映射项的 AND 条件'`,
		`COMMENT ON COLUMN credit_rule_conditions.id IS '条件唯一标识，自增主键'`,
		`COMMENT ON COLUMN credit_rule_conditions.item_id IS '所属映射项ID，关联 credit_rule_items 表'`,
		`COMMENT ON COLUMN credit_rule_conditions.param_path IS '请求参数路径（如 resolution, quality, model）'`,
		`COMMENT ON COLUMN credit_rule_conditions.param_value IS '参数值（如 1k, low, gpt-4）'`,
		`COMMENT ON COLUMN credit_rule_conditions.created_at IS '记录创建时间'`,
		`COMMENT ON COLUMN credit_rule_conditions.updated_at IS '记录最后更新时间'`,

		// credit_logs 表注释
		`COMMENT ON TABLE credit_logs IS '积分日志表，记录积分扣除和退还'`,
		`COMMENT ON COLUMN credit_logs.id IS '日志唯一标识，自增主键'`,
		`COMMENT ON COLUMN credit_logs.user_id IS '用户ID，关联 users 表'`,
		`COMMENT ON COLUMN credit_logs.task_id IS '关联的任务ID'`,
		`COMMENT ON COLUMN credit_logs.credits IS '积分数量（正数）'`,
		`COMMENT ON COLUMN credit_logs.type IS '操作类型：deduct=扣除, refund=退还'`,
		`COMMENT ON COLUMN credit_logs.remark IS '备注说明'`,
		`COMMENT ON COLUMN credit_logs.created_at IS '记录创建时间'`,
	}

	for _, comment := range comments {
		if err := DB.Exec(comment).Error; err != nil {
			// 注释失败不影响正常使用，只记录警告
			common.SysError("failed to add comment: " + err.Error())
		}
	}

	return nil
}

// CloseDB 关闭数据库连接
func CloseDB() error {
	sqlDB, err := DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
