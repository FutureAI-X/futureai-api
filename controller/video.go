package controller

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/FutureAI-X/futureai-api/common"
	"github.com/FutureAI-X/futureai-api/model"
	"github.com/FutureAI-X/futureai-api/supplier"
	"github.com/gin-gonic/gin"
)

// videoGenerationsPath 视频生成端点路径。
// 必须与 endpoints 表里手动创建的记录一致，否则 GetEndpointByPath 会返回 404。
const videoGenerationsPath = "/v1/videos/generations"

// VideoGenerate 视频生成端点
// POST /v1/videos/generations
//
// 流转逻辑与图像生成一致：校验 → 端点/模型/供应商路由 → 建任务 → 提交上游
// → 记录供应商响应 → 对账循环轮询。差异只有两点：
//   - 视频计费尚未启用，任务按 0 积分创建，不查计费规则、不扣费；
//   - 结果不转存到对象存储，任务完成时直接把上游 URL 返回给调用方。
func VideoGenerate(c *gin.Context) {
	// 只认标准字段，请求体里的其他字段由 encoding/json 直接忽略。
	var req supplier.VideoGenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		if common.IsBodyTooLarge(err) {
			common.SysErrorf("[VideoGenerate] 请求体超过上限: userID=%d", c.GetInt("user_id"))
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": "fail", "message": "请求体过大"})
			return
		}
		common.SysErrorf("[VideoGenerate] 请求体解析失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": "请求体格式错误"})
		return
	}

	// 本地先校验必填字段与内容项类型：参数错误不应留下任务行，
	// 也不需要为一次笔误付出一次上游往返。
	if err := req.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": err.Error()})
		return
	}

	modelName := req.Model
	common.SysLogf("[VideoGenerate] 收到请求: model=%s", modelName)

	// 1. 根据端点路径查找端点
	endpoint, err := model.GetEndpointByPath(videoGenerationsPath)
	if err != nil {
		common.SysErrorf("[VideoGenerate] 端点不存在: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"code": "fail", "message": "端点不存在"})
		return
	}

	// 2. 根据 model 名称查找模型
	m, err := model.GetModelByName(modelName)
	if err != nil {
		common.SysErrorf("[VideoGenerate] 模型不存在或已禁用: model=%s, err=%v", modelName, err)
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": "模型不存在或已禁用"})
		return
	}

	// 类型不参与路由判断（端点绑定才是闸门），但配错会让 /v1/models 的 type
	// 与实际能力不符，留一条日志便于发现。
	if m.Type != model.ModelTypeVideo {
		common.SysErrorf("[VideoGenerate] 模型类型不是 video，请检查管理端配置: model=%s, type=%s", modelName, m.Type)
	}

	// 3. 验证模型是否支持此端点
	modelEndpoints, err := model.GetModelEndpoints(m.ID)
	if err != nil {
		common.SysErrorf("[VideoGenerate] 查询端点关联失败: modelID=%d, err=%v", m.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "查询端点关联失败"})
		return
	}
	supported := false
	for _, me := range modelEndpoints {
		if me.EndpointID == endpoint.ID {
			supported = true
			break
		}
	}
	if !supported {
		common.SysErrorf("[VideoGenerate] 模型不支持此端点: model=%s, endpoint=%s", modelName, endpoint.Path)
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": "该模型不支持视频生成端点"})
		return
	}

	// 4. 查找可用供应商（关联启用且供应商本身也启用）
	vendorModels, err := model.GetEnabledVendorModelsByModelID(m.ID)
	if err != nil || len(vendorModels) == 0 {
		common.SysErrorf("[VideoGenerate] 无可用供应商: model=%s, err=%v", modelName, err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "fail", "message": "无可用供应商"})
		return
	}

	// 取第一个可用供应商
	vendorModel := vendorModels[0]
	vendor, err := model.GetEnabledVendorByID(vendorModel.VendorID)
	if err != nil {
		common.SysErrorf("[VideoGenerate] 供应商不可用: vendorID=%d, err=%v", vendorModel.VendorID, err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "fail", "message": "无可用供应商"})
		return
	}

	common.SysLogf("[VideoGenerate] 选择供应商: %s (vendorID=%d), 供应商模型ID: %s",
		vendor.Name, vendor.ID, vendorModel.VendorModelID)

	// 5. 解密 API Key
	apiKey, err := common.DecryptSecret(vendor.APIKey)
	if err != nil {
		common.SysErrorf("[VideoGenerate] 供应商密钥解密失败: vendor=%s, err=%v", vendor.Name, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "服务暂时不可用，请稍后再试"})
		return
	}

	// 6. 确认调用方身份（由 APIAuth 中间件从 API Key 解析）
	userID := c.GetInt("user_id")
	if userID <= 0 {
		common.SysErrorf("[VideoGenerate] 缺少有效用户身份: userID=%d", userID)
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{"message": "无效的 API Key", "type": "authentication_error"},
		})
		return
	}

	// 7. 视频计费尚未启用：任务按 0 积分创建，不查计费规则、不扣费。
	// 任务行与状态机照常建立，后续接入计费时只需在这里补计费逻辑。
	common.SysLogf("[VideoGenerate] 视频计费尚未启用，本次任务按 0 积分创建: userID=%d, model=%s",
		userID, modelName)

	// 8. 幂等键。语义与图像生成一致：窗口内重复提交复用已有任务。
	idemKey, ok := parseIdempotencyKey(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": "Idempotency-Key 过长"})
		return
	}
	if idemKey != nil {
		if existing, err := model.GetTaskByUserAndIdempotencyKey(userID, *idemKey); err == nil {
			common.SysLogf("[VideoGenerate] 命中幂等键，复用已有任务: userID=%d, taskID=%s", userID, existing.TaskID)
			c.JSON(http.StatusOK, gin.H{"code": "success", "taskId": existing.TaskID})
			return
		}
	}

	// 9. 创建任务。没有扣费，因此不需要事务；幂等键冲突的处理与图像生成一致。
	task := model.Task{
		TaskID:     model.GenerateTaskID(),
		UserID:     userID,
		VendorID:   vendor.ID,
		ModelID:    m.ID,
		EndpointID: endpoint.ID,
		Status:     model.StatusPending, // 尚未提交上游
		Credits:    0,                   // 计费尚未启用
		// 落库的是标准字段快照；供应商侧模型 ID 被 json:"-" 排除。
		RequestBody:    marshalRequestBodyFor("VideoGenerate", req),
		IdempotencyKey: idemKey,
	}

	if err := model.CreateTask(&task); err != nil {
		// 并发同键：两个请求同时没查到记录、各自去插入，其中一个必然撞唯一索引。
		// 这不是故障，而是幂等机制正在生效——回查已有任务返回即可。
		if idemKey != nil && model.IsDuplicateKeyError(err) {
			if existing, lookupErr := model.GetTaskByUserAndIdempotencyKey(userID, *idemKey); lookupErr == nil {
				common.SysLogf("[VideoGenerate] 幂等键并发冲突，复用已有任务: userID=%d, taskID=%s", userID, existing.TaskID)
				c.JSON(http.StatusOK, gin.H{"code": "success", "taskId": existing.TaskID})
				return
			}
		}

		common.SysErrorf("[VideoGenerate] 任务创建失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "任务创建失败"})
		return
	}

	// 10. 构造供应商客户端
	s := supplier.NewVideoSupplier(vendor.Name, supplier.Config{
		BaseURL: vendor.BaseURL,
		APIKey:  apiKey,
	})
	if s == nil {
		common.SysErrorf("[VideoGenerate] 不支持的视频供应商类型: %s", vendor.Name)
		// 未调用上游即失败，任务直接转终态（积分为 0，无需退款）
		model.UpdateTaskStatusWithRefund(task.TaskID, model.StatusCallFail, `{"error":"unsupported vendor"}`)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "不支持的供应商类型"})
		return
	}

	// 11. 调用供应商 API。标准字段原样传下去，供应商侧模型 ID 单独给出。
	req.VendorModelID = vendorModel.VendorModelID

	// 提交用独立带超时的 context：客户端断开时若直接取消上游调用，
	// 我们就再也不知道它有没有建单（task_id 只存在于响应里）。
	submitCtx, cancelSubmit := context.WithTimeout(context.Background(), supplier.SubmitTimeout)
	defer cancelSubmit()

	result := s.VideoGenerate(submitCtx, req)

	// 12. 上游明确拒绝受理（4xx、业务失败码、请求没发出去）→ 转终态
	if result.Code != "success" && result.FailureKind != supplier.FailureUnknown {
		common.SysErrorf("[VideoGenerate] 供应商明确拒绝: vendor=%s, model=%s, kind=%s",
			vendor.Name, modelName, result.FailureKind)
		if err := model.UpdateTaskStatusWithRefund(task.TaskID, model.StatusCallFail, `{"error":"vendor rejected"}`); err != nil {
			common.SysErrorf("[VideoGenerate] 更新任务状态失败: taskID=%s, err=%v", task.TaskID, err)
		}
		c.JSON(http.StatusOK, gin.H{"code": "fail", "message": "供应商调用失败"})
		return
	}

	// 13. 结果不确定（超时、连接中断、响应读不完整）：不能当失败处理，
	// 上游很可能已经建单并计费。置为非终态的 unknown，交给人工核对窗口。
	if result.FailureKind == supplier.FailureUnknown {
		common.SysErrorf("[VideoGenerate] [SUBMIT_UNKNOWN] 提交结果不确定，转入人工核对（暂不转终态）: taskID=%s, userID=%d, vendor=%s, model=%s",
			task.TaskID, userID, vendor.Name, modelName)
		if err := model.UpdateTaskStatus(task.TaskID, model.StatusUnknown, `{"pending_manual_review":"submit result unknown"}`); err != nil {
			common.SysErrorf("[VideoGenerate] 标记任务为 unknown 失败: taskID=%s, err=%v", task.TaskID, err)
		}
		c.JSON(http.StatusOK, gin.H{"code": "fail", "message": "上游调用结果不确定，任务已转入人工核对，请稍后用 taskId 查询状态"})
		return
	}

	// 14. 提交成功：记录供应商响应并交给对账循环轮询。
	// vendor_response 是重启后找回上游任务的唯一线索，写失败要重试并在日志里标出来。
	vendorRespJSON, err := json.Marshal(result.Data)
	if err != nil {
		common.SysErrorf("[VideoGenerate] 供应商响应序列化失败: taskID=%s, err=%v", task.TaskID, err)
	}
	if err := setVendorResponseWithRetry("VideoGenerate", task.TaskID, string(vendorRespJSON)); err != nil {
		common.SysErrorf("[VideoGenerate] [SUBMIT_UNKNOWN] 记录供应商响应失败，重启后该任务将无法对账: taskID=%s, err=%v",
			task.TaskID, err)
	}

	common.SysLogf("[VideoGenerate] 任务创建成功: taskId=%s, vendor=%s, model=%s", task.TaskID, vendor.Name, modelName)

	// 15. 立即安排一次轮询；抢不到并发位也无妨，下一轮对账循环会接手。
	ScheduleTaskPoll(task.TaskID)

	// 16. 返回系统 taskId
	c.JSON(http.StatusOK, gin.H{
		"code":   "success",
		"taskId": task.TaskID,
	})
}
