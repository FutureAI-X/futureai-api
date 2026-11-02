package model

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"gorm.io/gorm"
)

// ErrAppHasReleases 应用下仍有版本记录，不允许删除
var ErrAppHasReleases = errors.New("应用下仍有版本记录")

// App 客户端应用。
//
// key 是客户端在升级检查接口里上报的稳定标识（如 futureai-image），
// 一旦发布就不能修改——改了等于让所有老客户端查不到自己的更新。
type App struct {
	// 唯一标识，自增主键
	ID int `json:"id" gorm:"primaryKey"`

	// 客户端标识，全局唯一，仅小写字母/数字/连字符
	Key string `json:"key" gorm:"size:64;not null;uniqueIndex"`

	// 展示名称
	Name string `json:"name" gorm:"size:128;not null"`

	// 备注
	Description string `json:"description,omitempty" gorm:"type:text"`

	// 状态：1=启用, 2=禁用。禁用后公开接口按「应用不存在」处理。
	Status int `json:"status" gorm:"default:1"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// App 状态常量
const (
	AppStatusEnabled  = 1
	AppStatusDisabled = 2
)

// AppRelease 某个应用在某个平台上的一个已发布版本。
//
// 版本记录只增不删也能当历史用；公开接口取「已上架 + 该平台 + semver 最大」的一条，
// 因此下架最新版即可回退到上一版。
type AppRelease struct {
	// 唯一标识，自增主键
	ID int `json:"id" gorm:"primaryKey"`

	// 关联应用
	AppID int `json:"app_id" gorm:"not null;uniqueIndex:idx_app_releases_app_platform_version"`

	// 平台：windows / macos
	Platform string `json:"platform" gorm:"size:16;not null;uniqueIndex:idx_app_releases_app_platform_version"`

	// 严格三段 semver，如 1.3.0
	Version string `json:"version" gorm:"size:32;not null;uniqueIndex:idx_app_releases_app_platform_version"`

	// 更新地址：网盘链接或文档页面，客户端用系统浏览器打开
	UpdateURL string `json:"update_url" gorm:"size:1024;not null"`

	// 更新说明，展示在客户端弹窗中
	ReleaseNotes string `json:"release_notes,omitempty" gorm:"type:text"`

	// 状态：1=上架, 2=下架
	Status int `json:"status" gorm:"default:1"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AppRelease 状态常量
const (
	AppReleaseStatusPublished   = 1 // 上架
	AppReleaseStatusUnpublished = 2 // 下架
)

// 平台取值
const (
	AppPlatformWindows = "windows"
	AppPlatformMacOS   = "macos"
)

// AllAppPlatforms 全部合法平台，供管理端与公开接口校验
var AllAppPlatforms = []string{
	AppPlatformWindows,
	AppPlatformMacOS,
}

// IsValidAppPlatform 判断平台是否在枚举内
func IsValidAppPlatform(platform string) bool {
	for _, valid := range AllAppPlatforms {
		if platform == valid {
			return true
		}
	}
	return false
}

// appKeyPattern 应用标识：小写字母开头，只含小写字母/数字/连字符，最长 64。
// 不限制结尾不能是连字符——那只会让标识难看，不影响客户端定位。
var appKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ValidateAppKey 校验应用标识格式
func ValidateAppKey(key string) error {
	if !appKeyPattern.MatchString(key) {
		return fmt.Errorf("应用标识只能包含小写字母、数字和连字符，且不能超过 64 个字符")
	}
	return nil
}

// semverPattern 严格三段版本号：1.2.3。
//
// 不允许前缀 v、两段版本、预发布后缀或前导零：客户端用同一套规则校验上报值，
// 放宽解析只会让「服务端认为没更新、客户端认为有更新」这类分歧变得难查。
var semverPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// ParseSemver 把严格三段版本号解析成三个整数
func ParseSemver(version string) ([3]int, error) {
	var parts [3]int

	m := semverPattern.FindStringSubmatch(version)
	if m == nil {
		return parts, fmt.Errorf("版本号必须为 x.y.z 格式（如 1.2.3）")
	}

	for i := 0; i < 3; i++ {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return parts, fmt.Errorf("版本号段 %q 超出范围", m[i+1])
		}
		parts[i] = n
	}

	return parts, nil
}

// CompareSemver 比较两个版本号：a < b 返回 -1，相等返回 0，a > b 返回 1。
// 任一方格式非法时返回错误。
func CompareSemver(a, b string) (int, error) {
	pa, err := ParseSemver(a)
	if err != nil {
		return 0, err
	}
	pb, err := ParseSemver(b)
	if err != nil {
		return 0, err
	}

	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1, nil
			}
			return 1, nil
		}
	}
	return 0, nil
}

// AdminGetApps 管理员获取全部应用（含禁用）
func AdminGetApps() ([]App, error) {
	var apps []App
	err := DB.Order("id ASC").Find(&apps).Error
	return apps, err
}

// GetAppByID 根据 ID 获取应用（含禁用）
func GetAppByID(id int) (*App, error) {
	var app App
	if err := DB.Where("id = ?", id).First(&app).Error; err != nil {
		return nil, err
	}
	return &app, nil
}

// GetAppByKey 根据标识获取应用（含禁用），用于创建时查重
func GetAppByKey(key string) (*App, error) {
	var app App
	if err := DB.Where("key = ?", key).First(&app).Error; err != nil {
		return nil, err
	}
	return &app, nil
}

// GetEnabledAppByKey 根据标识获取「启用」的应用。
// 禁用应用对公开接口等同于不存在，避免停用后客户端仍收到更新提示。
func GetEnabledAppByKey(key string) (*App, error) {
	var app App
	if err := DB.Where("key = ? AND status = ?", key, AppStatusEnabled).First(&app).Error; err != nil {
		return nil, err
	}
	return &app, nil
}

// GetAppMapByID 获取应用 ID 到应用的映射，用于版本列表拼接展示信息
func GetAppMapByID() (map[int]App, error) {
	var apps []App
	if err := DB.Find(&apps).Error; err != nil {
		return nil, err
	}
	m := make(map[int]App, len(apps))
	for _, app := range apps {
		m[app.ID] = app
	}
	return m, nil
}

// CreateApp 创建应用
func CreateApp(app *App) error {
	return DB.Create(app).Error
}

// UpdateApp 更新应用（key 不可修改）
func UpdateApp(id int, updates map[string]interface{}) error {
	return DB.Model(&App{}).Where("id = ?", id).Updates(updates).Error
}

// UpdateAppStatus 更新应用状态
func UpdateAppStatus(id int, status int) error {
	return DB.Model(&App{}).Where("id = ?", id).Update("status", status).Error
}

// DeleteApp 删除应用。应用下仍有版本记录时拒绝删除，
// 否则版本会变成无主数据，管理页再也看不到、删不掉。
func DeleteApp(id int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&AppRelease{}).Where("app_id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("%w（%d 条），请先删除版本或改为禁用", ErrAppHasReleases, count)
		}
		return tx.Delete(&App{}, id).Error
	})
}

// AdminGetAppReleases 管理员获取版本列表，可按应用与平台筛选（空值不筛）
func AdminGetAppReleases(appID int, platform string) ([]AppRelease, error) {
	query := DB.Model(&AppRelease{})
	if appID > 0 {
		query = query.Where("app_id = ?", appID)
	}
	if platform != "" {
		query = query.Where("platform = ?", platform)
	}

	var releases []AppRelease
	err := query.Order("id DESC").Find(&releases).Error
	return releases, err
}

// GetAppReleaseByID 根据 ID 获取版本记录
func GetAppReleaseByID(id int) (*AppRelease, error) {
	var release AppRelease
	if err := DB.Where("id = ?", id).First(&release).Error; err != nil {
		return nil, err
	}
	return &release, nil
}

// GetLatestPublishedAppRelease 取指定应用、平台下已上架的最高版本。
// 没有已上架版本时返回 (nil, nil)。
//
// 不用 SQL 排序：version 是字符串，字典序会把 1.10.0 排到 1.9.0 前面。
// 版本记录量很小（每个应用每个平台几十条），全部取出在 Go 里比较即可。
func GetLatestPublishedAppRelease(appID int, platform string) (*AppRelease, error) {
	var releases []AppRelease
	if err := DB.Where(
		"app_id = ? AND platform = ? AND status = ?",
		appID, platform, AppReleaseStatusPublished,
	).Find(&releases).Error; err != nil {
		return nil, err
	}

	var latest *AppRelease
	for i := range releases {
		if latest == nil {
			latest = &releases[i]
			continue
		}
		cmp, err := CompareSemver(releases[i].Version, latest.Version)
		if err == nil && cmp > 0 {
			latest = &releases[i]
		}
	}
	return latest, nil
}

// AppReleaseExists 判断同一应用/平台/版本是否已存在（排除 excludeID 自身）
func AppReleaseExists(appID int, platform, version string, excludeID int) (bool, error) {
	query := DB.Model(&AppRelease{}).Where(
		"app_id = ? AND platform = ? AND version = ?", appID, platform, version,
	)
	if excludeID > 0 {
		query = query.Where("id <> ?", excludeID)
	}

	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// CreateAppRelease 创建版本记录
func CreateAppRelease(release *AppRelease) error {
	return DB.Create(release).Error
}

// UpdateAppRelease 更新版本记录
func UpdateAppRelease(id int, updates map[string]interface{}) error {
	return DB.Model(&AppRelease{}).Where("id = ?", id).Updates(updates).Error
}

// UpdateAppReleaseStatus 更新版本上架状态
func UpdateAppReleaseStatus(id int, status int) error {
	return DB.Model(&AppRelease{}).Where("id = ?", id).Update("status", status).Error
}

// DeleteAppRelease 删除版本记录
func DeleteAppRelease(id int) error {
	return DB.Delete(&AppRelease{}, id).Error
}
