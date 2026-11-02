package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/FutureAI-X/futureai-api/common"
	"github.com/FutureAI-X/futureai-api/model"
	"github.com/gin-gonic/gin"
)

// validateUpdateURL 校验更新地址：必须是 http/https 绝对地址。
// 该地址由客户端用系统浏览器打开，服务端不会请求它，因此无需做 SSRF 校验；
// 但限制协议可以挡住 javascript: 之类的取值被误配进后台。
func validateUpdateURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("更新地址不能为空")
	}
	if len(raw) > 1024 {
		return fmt.Errorf("更新地址不能超过 1024 个字符")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("更新地址不合法")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("更新地址必须以 http:// 或 https:// 开头")
	}
	if u.Host == "" {
		return fmt.Errorf("更新地址缺少域名")
	}
	return nil
}

// ── 应用管理 ──

// AdminGetApps 管理员获取应用列表
func AdminGetApps(c *gin.Context) {
	apps, err := model.AdminGetApps()
	if err != nil {
		common.SysErrorf("[AdminGetApps] 获取应用列表失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "获取应用列表失败"})
		return
	}

	type appResponse struct {
		ID          int    `json:"id"`
		Key         string `json:"key"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Status      int    `json:"status"`
		CreatedAt   string `json:"created_at"`
	}

	items := make([]appResponse, len(apps))
	for i, app := range apps {
		items[i] = appResponse{
			ID:          app.ID,
			Key:         app.Key,
			Name:        app.Name,
			Description: app.Description,
			Status:      app.Status,
			CreatedAt:   common.FormatDisplayTime(app.CreatedAt),
		}
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}

// AdminCreateAppRequest 创建应用请求
type AdminCreateAppRequest struct {
	Key         string `json:"key" binding:"required"`
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

// AdminCreateApp 创建应用
func AdminCreateApp(c *gin.Context) {
	var req AdminCreateAppRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请填写应用标识和名称"})
		return
	}

	req.Key = strings.TrimSpace(req.Key)
	req.Name = strings.TrimSpace(req.Name)
	if err := model.ValidateAppKey(req.Key); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "应用名称不能为空"})
		return
	}

	// 提前给出「标识已存在」的明确提示；并发下的极端重复由唯一索引兜底
	if _, err := model.GetAppByKey(req.Key); err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "应用标识已存在"})
		return
	}

	app := model.App{
		Key:         req.Key,
		Name:        req.Name,
		Description: req.Description,
		Status:      model.AppStatusEnabled,
	}

	if err := model.CreateApp(&app); err != nil {
		common.SysErrorf("[AdminCreateApp] 创建应用失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "创建应用失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "应用创建成功"})
}

// AdminUpdateAppRequest 更新应用请求（key 不允许修改）。
// 两个字段都用指针：允许显式传空串来清空备注，而不是把「空」当成「不修改」。
type AdminUpdateAppRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

// AdminUpdateApp 更新应用
func AdminUpdateApp(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的应用ID"})
		return
	}

	var req AdminUpdateAppRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求参数无效"})
		return
	}

	updates := map[string]interface{}{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "应用名称不能为空"})
			return
		}
		updates["name"] = name
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "没有需要更新的字段"})
		return
	}

	if err := model.UpdateApp(id, updates); err != nil {
		common.SysErrorf("[AdminUpdateApp] 更新应用失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "更新应用失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "应用更新成功"})
}

// AdminUpdateAppStatus 更新应用状态
func AdminUpdateAppStatus(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的应用ID"})
		return
	}

	var req struct {
		Status int `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "状态值无效"})
		return
	}

	if req.Status != model.AppStatusEnabled && req.Status != model.AppStatusDisabled {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "状态值必须为 1(启用) 或 2(禁用)"})
		return
	}

	if err := model.UpdateAppStatus(id, req.Status); err != nil {
		common.SysErrorf("[AdminUpdateAppStatus] 更新应用状态失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "更新状态失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "状态已更新"})
}

// AdminDeleteApp 删除应用（应用下仍有版本记录时拒绝）
func AdminDeleteApp(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的应用ID"})
		return
	}

	if err := model.DeleteApp(id); err != nil {
		if errors.Is(err, model.ErrAppHasReleases) {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
		common.SysErrorf("[AdminDeleteApp] 删除应用失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "删除应用失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "应用已删除"})
}

// ── 版本发布 ──

// AdminGetAppReleases 管理员获取版本列表，可按应用与平台筛选
func AdminGetAppReleases(c *gin.Context) {
	appID := 0
	if raw := strings.TrimSpace(c.Query("app_id")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的应用ID"})
			return
		}
		appID = parsed
	}

	platform := strings.TrimSpace(c.Query("platform"))
	if platform != "" && !model.IsValidAppPlatform(platform) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "平台值必须为 windows 或 macos"})
		return
	}

	releases, err := model.AdminGetAppReleases(appID, platform)
	if err != nil {
		common.SysErrorf("[AdminGetAppReleases] 获取版本列表失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "获取版本列表失败"})
		return
	}

	appMap, err := model.GetAppMapByID()
	if err != nil {
		common.SysErrorf("[AdminGetAppReleases] 获取应用映射失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "获取版本列表失败"})
		return
	}

	type releaseResponse struct {
		ID           int    `json:"id"`
		AppID        int    `json:"app_id"`
		AppKey       string `json:"app_key"`
		AppName      string `json:"app_name"`
		Platform     string `json:"platform"`
		Version      string `json:"version"`
		UpdateURL    string `json:"update_url"`
		ReleaseNotes string `json:"release_notes"`
		Status       int    `json:"status"`
		CreatedAt    string `json:"created_at"`
	}

	items := make([]releaseResponse, len(releases))
	for i, release := range releases {
		app := appMap[release.AppID]
		items[i] = releaseResponse{
			ID:           release.ID,
			AppID:        release.AppID,
			AppKey:       app.Key,
			AppName:      app.Name,
			Platform:     release.Platform,
			Version:      release.Version,
			UpdateURL:    release.UpdateURL,
			ReleaseNotes: release.ReleaseNotes,
			Status:       release.Status,
			CreatedAt:    common.FormatDisplayTime(release.CreatedAt),
		}
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}

// AdminCreateAppReleaseRequest 创建版本请求
type AdminCreateAppReleaseRequest struct {
	AppID        int    `json:"app_id" binding:"required"`
	Platform     string `json:"platform" binding:"required"`
	Version      string `json:"version" binding:"required"`
	UpdateURL    string `json:"update_url" binding:"required"`
	ReleaseNotes string `json:"release_notes"`
}

// AdminCreateAppRelease 创建版本
func AdminCreateAppRelease(c *gin.Context) {
	var req AdminCreateAppReleaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请填写应用、平台、版本号和更新地址"})
		return
	}

	req.Platform = strings.TrimSpace(req.Platform)
	req.Version = strings.TrimSpace(req.Version)
	req.UpdateURL = strings.TrimSpace(req.UpdateURL)

	if _, err := model.GetAppByID(req.AppID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "应用不存在"})
		return
	}
	if !model.IsValidAppPlatform(req.Platform) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "平台值必须为 windows 或 macos"})
		return
	}
	if _, err := model.ParseSemver(req.Version); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := validateUpdateURL(req.UpdateURL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	exists, err := model.AppReleaseExists(req.AppID, req.Platform, req.Version, 0)
	if err != nil {
		common.SysErrorf("[AdminCreateAppRelease] 检查版本重复失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "创建版本失败"})
		return
	}
	if exists {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "该应用在此平台的此版本已存在"})
		return
	}

	release := model.AppRelease{
		AppID:        req.AppID,
		Platform:     req.Platform,
		Version:      req.Version,
		UpdateURL:    req.UpdateURL,
		ReleaseNotes: req.ReleaseNotes,
		Status:       model.AppReleaseStatusPublished,
	}

	if err := model.CreateAppRelease(&release); err != nil {
		common.SysErrorf("[AdminCreateAppRelease] 创建版本失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "创建版本失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "版本发布成功"})
}

// AdminUpdateAppReleaseRequest 更新版本请求。
// release_notes 用指针：允许显式传空串来清空说明，而不是把「空」当成「不修改」。
type AdminUpdateAppReleaseRequest struct {
	AppID        int     `json:"app_id"`
	Platform     string  `json:"platform"`
	Version      string  `json:"version"`
	UpdateURL    string  `json:"update_url"`
	ReleaseNotes *string `json:"release_notes"`
}

// AdminUpdateAppRelease 更新版本
func AdminUpdateAppRelease(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的版本ID"})
		return
	}

	var req AdminUpdateAppReleaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求参数无效"})
		return
	}

	existing, err := model.GetAppReleaseByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "版本记录不存在"})
		return
	}

	// 用「现值 + 本次改动」拼出最终值统一校验，避免只校验改动字段时，
	// 组合后的应用/平台/版本变成非法或重复。
	appID := existing.AppID
	if req.AppID != 0 {
		appID = req.AppID
	}
	platform := existing.Platform
	if req.Platform != "" {
		platform = strings.TrimSpace(req.Platform)
	}
	version := existing.Version
	if req.Version != "" {
		version = strings.TrimSpace(req.Version)
	}
	updateURL := existing.UpdateURL
	if req.UpdateURL != "" {
		updateURL = strings.TrimSpace(req.UpdateURL)
	}

	if _, err := model.GetAppByID(appID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "应用不存在"})
		return
	}
	if !model.IsValidAppPlatform(platform) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "平台值必须为 windows 或 macos"})
		return
	}
	if _, err := model.ParseSemver(version); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := validateUpdateURL(updateURL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	exists, err := model.AppReleaseExists(appID, platform, version, id)
	if err != nil {
		common.SysErrorf("[AdminUpdateAppRelease] 检查版本重复失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "更新版本失败"})
		return
	}
	if exists {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "该应用在此平台的此版本已存在"})
		return
	}

	updates := map[string]interface{}{}
	if req.AppID != 0 {
		updates["app_id"] = req.AppID
	}
	if req.Platform != "" {
		updates["platform"] = platform
	}
	if req.Version != "" {
		updates["version"] = version
	}
	if req.UpdateURL != "" {
		updates["update_url"] = updateURL
	}
	if req.ReleaseNotes != nil {
		updates["release_notes"] = *req.ReleaseNotes
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "没有需要更新的字段"})
		return
	}

	if err := model.UpdateAppRelease(id, updates); err != nil {
		common.SysErrorf("[AdminUpdateAppRelease] 更新版本失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "更新版本失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "版本更新成功"})
}

// AdminUpdateAppReleaseStatus 上架/下架版本
func AdminUpdateAppReleaseStatus(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的版本ID"})
		return
	}

	var req struct {
		Status int `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "状态值无效"})
		return
	}

	if req.Status != model.AppReleaseStatusPublished && req.Status != model.AppReleaseStatusUnpublished {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "状态值必须为 1(上架) 或 2(下架)"})
		return
	}

	if err := model.UpdateAppReleaseStatus(id, req.Status); err != nil {
		common.SysErrorf("[AdminUpdateAppReleaseStatus] 更新版本状态失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "更新状态失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "状态已更新"})
}

// AdminDeleteAppRelease 删除版本
func AdminDeleteAppRelease(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的版本ID"})
		return
	}

	if err := model.DeleteAppRelease(id); err != nil {
		common.SysErrorf("[AdminDeleteAppRelease] 删除版本失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "删除版本失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "版本已删除"})
}
