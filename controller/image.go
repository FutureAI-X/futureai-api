package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/FutureAI/token-hub/common"
	"github.com/FutureAI/token-hub/model"
	"github.com/FutureAI/token-hub/supplier"
	"github.com/gin-gonic/gin"
)

// ImageGenerate 图像生成端点
// POST /v1/images/generations
func ImageGenerate(c *gin.Context) {
	// 解析请求体
	var reqBody map[string]interface{}
	if err := c.ShouldBindJSON(&reqBody); err != nil {
		common.SysErrorf("[ImageGenerate] 请求体解析失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": "请求体格式错误"})
		return
	}

	// 提取 model 字段
	modelName, _ := reqBody["model"].(string)
	if modelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": "缺少 model 参数"})
		return
	}

	common.SysLogf("[ImageGenerate] 收到请求: model=%s", modelName)

	// 1. 根据端点路径查找端点
	endpoint, err := model.GetEndpointByPath("/v1/images/generations")
	if err != nil {
		common.SysErrorf("[ImageGenerate] 端点不存在: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"code": "fail", "message": "端点不存在"})
		return
	}

	// 2. 根据 model 名称查找模型
	m, err := model.GetModelByName(modelName)
	if err != nil {
		common.SysErrorf("[ImageGenerate] 模型不存在或已禁用: model=%s, err=%v", modelName, err)
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": "模型不存在或已禁用"})
		return
	}

	// 3. 验证模型是否支持此端点
	modelEndpoints, err := model.GetModelEndpoints(m.ID)
	if err != nil {
		common.SysErrorf("[ImageGenerate] 查询端点关联失败: modelID=%d, err=%v", m.ID, err)
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
		common.SysErrorf("[ImageGenerate] 模型不支持此端点: model=%s, endpoint=%s", modelName, endpoint.Path)
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": "该模型不支持图像生成端点"})
		return
	}

	// 4. 查找可用供应商（仅返回「关联启用 且 供应商本身也启用」的项）
	vendorModels, err := model.GetEnabledVendorModelsByModelID(m.ID)
	if err != nil || len(vendorModels) == 0 {
		common.SysErrorf("[ImageGenerate] 无可用供应商: model=%s, err=%v", modelName, err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "fail", "message": "无可用供应商"})
		return
	}

	// 取第一个可用供应商
	vendorModel := vendorModels[0]
	vendor, err := model.GetEnabledVendorByID(vendorModel.VendorID)
	if err != nil {
		common.SysErrorf("[ImageGenerate] 供应商不可用: vendorID=%d, err=%v", vendorModel.VendorID, err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "fail", "message": "无可用供应商"})
		return
	}

	common.SysLogf("[ImageGenerate] 选择供应商: %s (vendorID=%d), 供应商模型ID: %s", vendor.Name, vendor.ID, vendorModel.VendorModelID)

	// 5. 解密 API Key
	apiKey, err := common.DecryptSecret(vendor.APIKey)
	if err != nil {
		// 内部错误仅记日志，不向调用方泄露细节
		common.SysErrorf("[ImageGenerate] 供应商密钥解密失败: vendor=%s, err=%v", vendor.Name, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "服务暂时不可用，请稍后再试"})
		return
	}

	// 6. 确认调用方身份（由 APIAuth 中间件从 API Key 解析）
	userID := c.GetInt("user_id")
	if userID <= 0 {
		common.SysErrorf("[ImageGenerate] 缺少有效用户身份: userID=%d", userID)
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{"message": "无效的 API Key", "type": "authentication_error"},
		})
		return
	}

	// 7. 计算积分消耗。
	// 必须在覆盖 reqBody["model"] 之前计算：差异化计费规则可能以 model 作为条件，
	// 此处应匹配调用方传入的模型名，而不是供应商侧的模型 ID。
	credits, err := resolveCredits(m.ID, m.Type, reqBody, endpoint.Path)
	if err != nil {
		common.SysErrorf("[ImageGenerate] 计费规则解析失败: model=%s, err=%v", modelName, err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "fail", "message": "该模型未配置计费规则，暂不可用"})
		return
	}
	if credits.RefImageCount > 0 {
		common.SysLogf("[ImageGenerate] 含参考图附加计费: model=%s, 参考图=%d张, 基础=%.2f, 合计=%.2f",
			modelName, credits.RefImageCount, credits.Base, credits.Total)
	}

	// 8. 先扣费、后调用上游。
	// 顺序至关重要：若先调用供应商再扣费，零余额用户可以让平台先产生真实成本，
	// 随后扣费失败返回 402，形成「无限免费消耗上游额度」。
	task := model.Task{
		TaskID:     model.GenerateTaskID(),
		UserID:     userID,
		VendorID:   vendor.ID,
		ModelID:    m.ID,
		EndpointID: endpoint.ID,
		Status:     "pending", // 尚未提交上游
		Credits:    credits.Total,
		// 必须在下面覆盖 reqBody["model"] 之前取快照，否则落库的是供应商侧模型 ID，
		// 回溯时看不到调用方实际传入的模型名，也无法复现计费规则的匹配过程。
		RequestBody: marshalRequestBody(reqBody),
	}

	if err := model.CreateTaskAndDeduct(&task, credits.Total, credits.Remark()); err != nil {
		if errors.Is(err, model.ErrInsufficientCredits) {
			common.SysErrorf("[ImageGenerate] 积分不足: userID=%d, amount=%.6f", userID, credits.Total)
			c.JSON(http.StatusPaymentRequired, gin.H{"code": "fail", "message": "积分不足"})
		} else {
			common.SysErrorf("[ImageGenerate] 任务创建失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "任务创建失败"})
		}
		return
	}

	// 9. 构造供应商客户端
	cfg := supplier.Config{
		BaseURL: vendor.BaseURL,
		APIKey:  apiKey,
	}
	s := supplier.NewSupplier(vendor.Name, cfg)
	if s == nil {
		common.SysErrorf("[ImageGenerate] 不支持的供应商类型: %s", vendor.Name)
		// 未调用上游即失败，退还预扣积分
		model.UpdateTaskStatusWithRefund(task.TaskID, "call_fail", `{"error":"unsupported vendor"}`)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "不支持的供应商类型"})
		return
	}

	// 10. 调用供应商 API（将用户侧模型名替换为供应商侧模型 ID）
	reqBody["model"] = vendorModel.VendorModelID

	result := s.ImageGenerate(supplier.ImageGenerateRequest{
		// 一并带上用户侧模型名：多个模型映射到同一 vendor_model_id 时，
		// 供应商侧只能靠它区分调用方实际请求的变体。
		Model: modelName,
		Body:  reqBody,
	})

	// 11. 上游调用失败 → 退还积分
	if result.Code != "success" {
		common.SysErrorf("[ImageGenerate] 供应商调用失败: vendor=%s, model=%s", vendor.Name, modelName)
		if err := model.UpdateTaskStatusWithRefund(task.TaskID, "call_fail", `{"error":"vendor call failed"}`); err != nil {
			common.SysErrorf("[ImageGenerate] 退还积分失败: taskID=%s, err=%v", task.TaskID, err)
		}
		c.JSON(http.StatusOK, gin.H{"code": "fail", "message": "供应商调用失败"})
		return
	}

	// 12. 提交成功：记录供应商响应并转入轮询
	vendorRespJSON, _ := json.Marshal(result.Data)
	if err := model.SetTaskVendorResponse(task.TaskID, string(vendorRespJSON)); err != nil {
		common.SysErrorf("[ImageGenerate] 记录供应商响应失败: taskID=%s, err=%v", task.TaskID, err)
	}

	common.SysLogf("[ImageGenerate] 任务创建成功: taskId=%s, vendor=%s, model=%s", task.TaskID, vendor.Name, modelName)

	// 13. 启动后台轮询任务状态
	go pollTaskStatus(task.TaskID, string(vendorRespJSON), vendor.Name, cfg)

	// 14. 返回系统 taskId
	c.JSON(http.StatusOK, gin.H{
		"code":   "success",
		"taskId": task.TaskID,
	})
}

// maxStoredRequestBody 落库的请求体长度上限（字节）。
//
// 定在 1MB 而不是更小的值：调用方基本以 URL 传参考图，正常请求体只有几 KB，
// 这个上限在常规流量下永远不触发，截断只是兜底——万一有人改成 base64 内联，
// tasks 又是留存量最大的表，不封顶会把库撑爆。
const maxStoredRequestBody = 1 << 20

// requestBodyTruncatedMarker 附在截断后的请求体末尾，提示内容不完整
const requestBodyTruncatedMarker = "...[truncated]"

// marshalRequestBody 把请求体序列化成可落库的字符串。
//
// 只用于回溯，因此任何失败都必须降级为「记不下就不记」，绝不能让它影响一次已经扣过费的调用。
func marshalRequestBody(reqBody map[string]interface{}) string {
	data, err := json.Marshal(reqBody)
	if err != nil {
		common.SysErrorf("[ImageGenerate] 请求体序列化失败，本次不记录: %v", err)
		return ""
	}
	if len(data) <= maxStoredRequestBody {
		return string(data)
	}

	common.SysErrorf("[ImageGenerate] 请求体 %d 字节超过上限 %d，落库时截断", len(data), maxStoredRequestBody)

	// 直接切字节可能劈开多字节字符，落库后是无效 UTF-8；
	// 这个索引一定在范围内（len(data) > maxStoredRequestBody），回退必然终止。
	cut := maxStoredRequestBody
	for cut > 0 && !utf8.RuneStart(data[cut]) {
		cut--
	}
	return string(data[:cut]) + requestBodyTruncatedMarker
}

// maxRefImagesPerRequest 单次请求计入计费的参考图张数上限。
// 请求体目前没有大小限制，若不封顶，一个上万元素的数组会产生荒谬的扣费
// （并让调用方必然 402）。超出部分不再计费，仅记日志告警。
const maxRefImagesPerRequest = 100

// refImageEndpoints 支持参考图附加计费的端点白名单。
// 刻意用白名单而非「非对话端点」的反向判断：将来若有多模态对话端点复用
// resolveCredits，其请求体同样会带 images / image_urls 这类字段，白名单能防止误加价。
var refImageEndpoints = map[string]bool{
	"/v1/images/generations": true,
	"/v1/images/edits":       true,
}

// isRefImageEndpoint 判断端点是否支持参考图附加计费。
// 新增图片类端点时记得在上面的白名单里登记。
func isRefImageEndpoint(path string) bool {
	return refImageEndpoints[path]
}

// isRefImageModel 判断模型是否属于会为参考图计费的类型。
//
// 空类型也算图像模型：该列有 not null 约束但可存空串，若被直接 SQL 改成空串，
// 按「是图像模型」处理是 fail-closed 的方向——宁可多扣，不漏扣。
func isRefImageModel(t model.ModelType) bool {
	return t == model.ModelTypeImage || t == ""
}

// creditResolution 一次请求的计费结果
type creditResolution struct {
	// Total 本次应扣的总积分
	Total float64

	// Base 其中的基础部分（基础积分或命中的参数组合积分），不含参考图
	Base float64

	// RefImageCount 计入的参考图张数
	RefImageCount int
}

// Remark 生成扣费备注。CreditLog.Remark 是 varchar(255)，超长在 Postgres 上是
// 硬报错而非截断，所以格式固定且短；张数已封顶 100，长度可控。
func (r creditResolution) Remark() string {
	if r.RefImageCount <= 0 {
		return "图像生成任务"
	}
	return fmt.Sprintf("图像生成任务(基础%.2f+参考图%d张)", r.Base, r.RefImageCount)
}

// resolveCredits 按模型的计费规则计算本次请求应扣积分。
// 未配置规则时返回错误（fail-closed）：否则模型漏配规则会变成对所有人免费，
// 而这是运维上极易发生、且不会被察觉的资损。如需免费模型，请显式配置一条 0 积分的规则。
//
// 计费公式：基础积分（或命中的参数组合积分）+ 参考图张数 × 每张参考图积分。
// 参考图部分是叠加而非取代，且需要同时满足两个条件：模型类型是图像生成、
// 且 endpointPath 命中图片端点白名单。
func resolveCredits(modelID int, modelType model.ModelType, reqBody map[string]interface{}, endpointPath string) (creditResolution, error) {
	creditRule, err := model.GetCreditRuleByModelID(modelID)
	if err != nil || creditRule == nil {
		return creditResolution{}, errors.New("模型未配置计费规则")
	}

	creditsAmount := creditRule.BaseCredits

	// 参数组合差异化定价：某组合的所有条件都命中时使用该组合的积分。
	// 注意条件只支持字符串值的顶层参数，数组值（如参考图）永远匹配不上。
	for _, item := range creditRule.Items {
		matched := len(item.Conditions) > 0
		for _, cond := range item.Conditions {
			if paramVal, ok := reqBody[cond.ParamPath].(string); !ok || paramVal != cond.ParamValue {
				matched = false
				break
			}
		}
		if matched {
			creditsAmount = item.Credits
			break
		}
	}

	baseAmount := creditsAmount

	// 参考图附加计费。必须放在下面的负数守卫之前——否则一条被改成负数的单价
	// 会绕过校验，变成反向给用户送积分。
	//
	// 闸门是「类型」与「端点」两者的与：只有图像模型 + 图片端点才加价。
	// 保留端点条件不是冗余——它是纯放松的反面：若只按类型判定，一个配了参考图
	// 计费、类型却是 text 的模型会静默不加价（没有报错、没有日志，只是扣得少），
	// 而端点白名单同时挡住了「将来多模态对话端点复用本函数被误加价」。
	refImageCount := 0
	if creditRule.RefImageCredits > 0 && isRefImageModel(modelType) {
		if isRefImageEndpoint(endpointPath) {
			refImageCount = countRefImages(reqBody, creditRule.EffectiveRefImageParams())
			creditsAmount += float64(refImageCount) * creditRule.RefImageCredits
		} else {
			// 配了参考图计费却收不到钱：这是误配，必须可见
			common.SysErrorf("[resolveCredits] 模型类型为图像生成且已配置参考图计费，但端点 %s 不在白名单内，本次未计费: modelID=%d, 每张=%.2f",
				endpointPath, modelID, creditRule.RefImageCredits)
		}
	}

	if creditsAmount < 0 {
		return creditResolution{}, errors.New("计费规则中的积分为负数")
	}

	// 收敛到 2 位小数：0.01*3 这类累加会产出 0.030000000000000002。
	// 写库虽是 numeric(20,6) 会被四舍五入，但内存值会与实际扣费不一致。
	return creditResolution{
		Total:         math.Round(creditsAmount*100) / 100,
		Base:          math.Round(baseAmount*100) / 100,
		RefImageCount: refImageCount,
	}, nil
}

// countRefImages 统计请求体中携带的参考图张数。
// 按「去重后的图片值」计数：默认参数名里的 image / image_url / image_urls 是
// 同一张图的不同 SDK 写法，若按参数名累加，会把一张图扣上 2~3 次。
func countRefImages(reqBody map[string]interface{}, params []string) int {
	seen := make(map[string]bool)
	for _, name := range params {
		raw, ok := reqBody[name]
		if !ok || raw == nil {
			continue
		}
		for _, v := range refImageValues(raw, name) {
			seen[v] = true
		}
	}

	if len(seen) > maxRefImagesPerRequest {
		common.SysErrorf("[countRefImages] 参考图张数 %d 超过上限 %d，超出部分不计费", len(seen), maxRefImagesPerRequest)
		return maxRefImagesPerRequest
	}
	return len(seen)
}

// refImageValues 把某个参数名的取值归一化成图片值列表（用于去重计数）。
// 未知类型保守按 1 张计并告警：漏计费是资损，多计费只是一次可解释的争议。
func refImageValues(raw interface{}, name string) []string {
	switch v := raw.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []string{v}

	case []interface{}:
		values := make([]string, 0, len(v))
		for _, elem := range v {
			if s, ok := elem.(string); ok {
				// 空串是「占位但没填」，不算一张图
				if s != "" {
					values = append(values, s)
				}
				continue
			}
			// 元素非字符串（0 / false / {} / null）：只有非 nil 才当作一张图。
			// 朴素的 s != "" 判断会把 0 和 false 误判成有效图片。
			if elem != nil {
				common.SysErrorf("[countRefImages] 参数 %s 的数组元素类型为 %T，按 1 张计入", name, elem)
				values = append(values, fmt.Sprintf("%v", elem))
			}
		}
		return values

	case []string:
		values := make([]string, 0, len(v))
		for _, s := range v {
			if s != "" {
				values = append(values, s)
			}
		}
		return values

	default:
		common.SysErrorf("[countRefImages] 参数 %s 的类型为 %T，无法识别，按 1 张计入", name, raw)
		return []string{fmt.Sprintf("%v", raw)}
	}
}

// GetTask 查询任务状态
// GET /v1/tasks/:task_id
func GetTask(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"taskId": "", "status": "fail", "message": "缺少 task_id"})
		return
	}

	task, err := model.GetTaskByTaskID(taskID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"taskId": taskID, "status": "fail", "message": "任务不存在"})
		return
	}

	// 校验任务归属：仅允许查询本人的任务。
	// 刻意不保留 userID > 0 的「跳过校验」分支——历史数据中存在 user_id=0 的任务，
	// 一旦放行，任何持有 API Key 的人都能读取这些任务。
	userID := c.GetInt("user_id")
	if userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{"message": "无效的 API Key", "type": "authentication_error"},
		})
		return
	}
	if task.UserID != userID {
		c.JSON(http.StatusForbidden, gin.H{"taskId": taskID, "status": "fail", "message": "无权查看此任务"})
		return
	}

	// 解析 query_response 作为 data
	var data map[string]interface{}
	if task.QueryResponse != "" {
		json.Unmarshal([]byte(task.QueryResponse), &data)
	}

	c.JSON(http.StatusOK, gin.H{
		"taskId": task.TaskID,
		"status": task.Status,
		"data":   data,
	})
}

// maxConcurrentPolls 并发轮询上限。
// 每个轮询 goroutine 最长存活 10 分钟、每 5 秒发起一次上游调用；
// 若不加限制，请求量会被放大成任意数量的常驻 goroutine 与上游请求。
const maxConcurrentPolls = 500

// pollSem 轮询并发槽位
var pollSem = make(chan struct{}, maxConcurrentPolls)

// pollTaskStatus 后台轮询供应商任务状态
// 取不到并发槽位时立即终止任务并退还积分，避免用户为无人轮询的任务付费。
func pollTaskStatus(taskID string, vendorResponse string, vendorName string, cfg supplier.Config) {
	select {
	case pollSem <- struct{}{}:
		defer func() { <-pollSem }()
	default:
		common.SysErrorf("[TaskPoll] 轮询并发已达上限(%d)，终止任务并退款: taskID=%s", maxConcurrentPolls, taskID)
		model.UpdateTaskStatusWithRefund(taskID, "call_fail", `{"error":"poll concurrency limit reached"}`)
		return
	}

	s := supplier.NewSupplier(vendorName, cfg)
	if s == nil {
		common.SysErrorf("[TaskPoll] 不支持的供应商: %s, taskID=%s", vendorName, taskID)
		model.UpdateTaskStatusWithRefund(taskID, "call_fail", "")
		return
	}

	maxAttempts := 120 // 最多轮询120次
	interval := 5 * time.Second

	for i := 0; i < maxAttempts; i++ {
		time.Sleep(interval)

		result := s.TaskQuery(vendorResponse)
		common.SysLogf("[TaskPoll] 轮询 #%d: taskID=%s, vendorStatus=%s", i+1, taskID, result.Status)

		switch result.Status {
		case "completed":
			dataJSON, _ := json.Marshal(result.Data)
			if err := model.UpdateTaskStatus(taskID, "completed", string(dataJSON)); err != nil {
				common.SysErrorf("[TaskPoll] 更新任务状态失败: taskID=%s, err=%v", taskID, err)
			} else {
				common.SysLogf("[TaskPoll] 任务完成: taskID=%s", taskID)
			}
			return

		case "failed", "cancelled":
			if err := model.UpdateTaskStatusWithRefund(taskID, result.Status, ""); err != nil {
				common.SysErrorf("[TaskPoll] 更新任务状态失败: taskID=%s, err=%v", taskID, err)
			} else {
				common.SysLogf("[TaskPoll] 任务终止: taskID=%s, status=%s", taskID, result.Status)
			}
			return

		case "call_fail":
			common.SysErrorf("[TaskPoll] 查询调用失败，继续重试: taskID=%s", taskID)
			continue

		default:
			// pending / processing，继续轮询
			continue
		}
	}

	// 超过最大轮询次数
	common.SysErrorf("[TaskPoll] 轮询超时: taskID=%s, 已轮询%d次", taskID, maxAttempts)
	model.UpdateTaskStatusWithRefund(taskID, "call_fail", `{"error":"poll timeout"}`)
}

// RecoverPendingTasks 启动时恢复未完成任务的轮询
func RecoverPendingTasks() {
	tasks, err := model.GetPendingTasks()
	if err != nil {
		common.SysErrorf("[Recover] 查询未完成任务失败: %v", err)
		return
	}

	if len(tasks) == 0 {
		common.SysLogf("[Recover] 无未完成任务")
		return
	}

	common.SysLogf("[Recover] 发现 %d 个未完成任务，开始恢复轮询", len(tasks))

	for _, task := range tasks {
		if task.VendorResponse == "" {
			common.SysErrorf("[Recover] 任务缺少供应商响应，跳过: taskID=%s", task.TaskID)
			model.UpdateTaskStatusWithRefund(task.TaskID, "call_fail", `{"error":"missing vendor_response"}`)
			continue
		}

		// 获取供应商信息
		vendor, err := model.GetVendorByID(task.VendorID)
		if err != nil {
			common.SysErrorf("[Recover] 供应商不存在，跳过: taskID=%s, vendorID=%d, err=%v", task.TaskID, task.VendorID, err)
			model.UpdateTaskStatusWithRefund(task.TaskID, "call_fail", `{"error":"vendor not found"}`)
			continue
		}

		// 解密 API Key
		apiKey, err := common.DecryptSecret(vendor.APIKey)
		if err != nil {
			common.SysErrorf("[Recover] 密钥解密失败，跳过: taskID=%s, vendor=%s, err=%v", task.TaskID, vendor.Name, err)
			model.UpdateTaskStatusWithRefund(task.TaskID, "call_fail", `{"error":"decrypt failed"}`)
			continue
		}

		cfg := supplier.Config{
			BaseURL: vendor.BaseURL,
			APIKey:  apiKey,
		}

		common.SysLogf("[Recover] 恢复轮询: taskID=%s, vendor=%s", task.TaskID, vendor.Name)
		go pollTaskStatus(task.TaskID, task.VendorResponse, vendor.Name, cfg)
	}
}
