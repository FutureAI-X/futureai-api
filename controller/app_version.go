package controller

import (
	"net/http"
	"strings"

	"github.com/FutureAI-X/futureai-api/common"
	"github.com/FutureAI-X/futureai-api/model"
	"github.com/gin-gonic/gin"
)

// GetAppVersion 客户端启动时检查更新。
//
// 公开接口：客户端可能在用户登录前就发起检查，因此不要求任何凭证，
// 由路由层按来源 IP 限流。只返回「是否有更新」和最新版本信息，
// 更新地址仅在确实有更新时下发，避免客户端误把下载页当成常驻入口。
func GetAppVersion(c *gin.Context) {
	appKey := strings.TrimSpace(c.Param("app_key"))
	platform := strings.TrimSpace(c.Query("platform"))
	currentVersion := strings.TrimSpace(c.Query("current_version"))

	if !model.IsValidAppPlatform(platform) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "platform 必须为 windows 或 macos",
		})
		return
	}
	if _, err := model.ParseSemver(currentVersion); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "current_version 必须为 x.y.z 格式（如 1.2.3）",
		})
		return
	}

	app, err := model.GetEnabledAppByKey(appKey)
	if err != nil {
		// 禁用应用与不存在的应用给出同样的响应：停用即让客户端不再收到更新提示
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "应用不存在",
		})
		return
	}

	release, err := model.GetLatestPublishedAppRelease(app.ID, platform)
	if err != nil {
		common.SysErrorf("[GetAppVersion] 获取最新版本失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "获取版本信息失败",
		})
		return
	}

	data := gin.H{
		"app_key":        app.Key,
		"platform":       platform,
		"latest_version": "",
		"has_update":     false,
	}

	if release == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
		return
	}

	cmp, err := model.CompareSemver(release.Version, currentVersion)
	if err != nil {
		// currentVersion 已在上面校验过，这里只可能出现在版本数据本身非法的场景
		common.SysErrorf("[GetAppVersion] 已发布版本号非法（release_id=%d）: %v", release.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "获取版本信息失败",
		})
		return
	}

	data["latest_version"] = release.Version
	data["published_at"] = common.FormatDisplayTime(release.CreatedAt)
	data["has_update"] = cmp > 0

	if cmp > 0 {
		data["update_url"] = release.UpdateURL
		data["release_notes"] = release.ReleaseNotes
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}
