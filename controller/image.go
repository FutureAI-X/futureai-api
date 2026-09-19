package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/FutureAI/token-hub/common"
	"github.com/FutureAI/token-hub/model"
	"github.com/FutureAI/token-hub/supplier"
	"github.com/gin-gonic/gin"
)

// maxIdempotencyKeyLen 幂等键长度上限（与 tasks.idempotency_key 列宽一致）
const maxIdempotencyKeyLen = 128

// ImageGenerate 图像生成端点
// POST /v1/images/generations
func ImageGenerate(c *gin.Context) {
	// 解析请求体。只认标准字段，请求体里的其他字段被 encoding/json 直接忽略，
	// 因此后续流程（计费、快照、供应商调用）都只可能看到标准字段。
	var req supplier.ImageGenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 体积超限与格式错误要分开报：前者是调用方该改请求大小，
		// 后者是该改请求内容，混成 400 会让人往错的方向排查。
		if common.IsBodyTooLarge(err) {
			common.SysErrorf("[ImageGenerate] 请求体超过上限: userID=%d", c.GetInt("user_id"))
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": "fail", "message": "请求体过大"})
			return
		}
		common.SysErrorf("[ImageGenerate] 请求体解析失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": "请求体格式错误"})
		return
	}

	modelName := req.Model
	if modelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": "缺少 model 参数"})
		return
	}

	// 补齐标准默认值，且必须在计费之前：计费条件匹配的是补齐后的值，
	// 否则省略 resolution 的请求会计基础价、实际按 1k 出图，价格与生成结果不符。
	req.ApplyDefaults()

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

	// 7. 计算积分消耗。条件按标准字段匹配：差异化计费规则可能以 model 作为条件，
	// 此处匹配的是调用方传入的模型名，供应商侧模型 ID 不作为计费条件。
	credits, err := resolveCredits(m.ID, m.Type, req, endpoint.Path)
	if err != nil {
		common.SysErrorf("[ImageGenerate] 计费规则解析失败: model=%s, err=%v", modelName, err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "fail", "message": "该模型未配置计费规则，暂不可用"})
		return
	}
	if credits.RefImageCount > 0 {
		common.SysLogf("[ImageGenerate] 含参考图附加计费: model=%s, 参考图=%d张, 基础=%.2f, 合计=%.2f",
			modelName, credits.RefImageCount, credits.Base, credits.Total)
	}

	// 8. 幂等键。带了这个头的请求在窗口内重复提交只会扣一次费：
	// 上游提交是同步语义（最长 30s），而 OpenAI 兼容 SDK 默认会重试，
	// 没有这个机制时客户端超时重发就会重复扣费、重复占用上游额度。
	idemKey, ok := parseIdempotencyKey(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": "fail", "message": "Idempotency-Key 过长"})
		return
	}
	if idemKey != nil {
		if existing, err := model.GetTaskByUserAndIdempotencyKey(userID, *idemKey); err == nil {
			common.SysLogf("[ImageGenerate] 命中幂等键，复用已有任务: userID=%d, taskID=%s", userID, existing.TaskID)
			c.JSON(http.StatusOK, gin.H{"code": "success", "taskId": existing.TaskID})
			return
		}
	}

	// 9. 先扣费、后调用上游。
	// 顺序至关重要：若先调用供应商再扣费，零余额用户可以让平台先产生真实成本，
	// 随后扣费失败返回 402，形成「无限免费消耗上游额度」。
	task := model.Task{
		TaskID:     model.GenerateTaskID(),
		UserID:     userID,
		VendorID:   vendor.ID,
		ModelID:    m.ID,
		EndpointID: endpoint.ID,
		Status:     model.StatusPending, // 尚未提交上游
		Credits:    credits.Total,
		// 落库的是标准字段快照（已补齐默认值），即计费实际依据的那份数据：
		// 供应商侧模型 ID 被 json:"-" 排除，回溯时可据此复现条件匹配过程。
		RequestBody:    marshalRequestBody(req),
		IdempotencyKey: idemKey,
	}

	if err := model.CreateTaskAndDeduct(&task, credits.Total, credits.Remark()); err != nil {
		// 并发同键：两个请求同时没查到记录、各自去插入，其中一个必然撞唯一索引。
		// 这不是故障，而是幂等机制正在生效——回查已有任务返回即可，
		// 绝不能报错（那会让调用方以为失败而重试，反而制造重复任务）。
		if idemKey != nil && model.IsDuplicateKeyError(err) {
			if existing, lookupErr := model.GetTaskByUserAndIdempotencyKey(userID, *idemKey); lookupErr == nil {
				common.SysLogf("[ImageGenerate] 幂等键并发冲突，复用已有任务: userID=%d, taskID=%s", userID, existing.TaskID)
				c.JSON(http.StatusOK, gin.H{"code": "success", "taskId": existing.TaskID})
				return
			}
		}

		if errors.Is(err, model.ErrInsufficientCredits) {
			common.SysErrorf("[ImageGenerate] 积分不足: userID=%d, amount=%.6f", userID, credits.Total)
			c.JSON(http.StatusPaymentRequired, gin.H{"code": "fail", "message": "积分不足"})
		} else {
			common.SysErrorf("[ImageGenerate] 任务创建失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "任务创建失败"})
		}
		return
	}

	// 10. 构造供应商客户端
	cfg := supplier.Config{
		BaseURL: vendor.BaseURL,
		APIKey:  apiKey,
	}
	s := supplier.NewSupplier(vendor.Name, cfg)
	if s == nil {
		common.SysErrorf("[ImageGenerate] 不支持的供应商类型: %s", vendor.Name)
		// 未调用上游即失败，退还预扣积分
		model.UpdateTaskStatusWithRefund(task.TaskID, model.StatusCallFail, `{"error":"unsupported vendor"}`)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "fail", "message": "不支持的供应商类型"})
		return
	}

	// 11. 调用供应商 API。标准字段原样传下去，供应商侧模型 ID 单独给出：
	// 供应商需要同时知道「调用方请求的是哪个模型」（多个模型可能映射到同一个
	// vendor_model_id，要靠它区分变体）和「该往上游发哪个模型 ID」。
	req.VendorModelID = vendorModel.VendorModelID

	// 提交用独立带超时的 context，而不是请求的 context：
	// 客户端断开时若直接取消上游调用，我们就再也不知道它有没有建单
	// （task_id 只存在于响应里），会掉进「上游已受理却无从查证」的窗口。
	// 提交最多 30s，让它跑完并把结果落库，状态才是自洽的。
	submitCtx, cancelSubmit := context.WithTimeout(context.Background(), supplier.SubmitTimeout)
	defer cancelSubmit()

	result := s.ImageGenerate(submitCtx, req)

	// 12. 上游明确拒绝受理（4xx、业务失败码、请求没发出去）→ 退还积分
	if result.Code != "success" && result.FailureKind != supplier.FailureUnknown {
		common.SysErrorf("[ImageGenerate] 供应商明确拒绝: vendor=%s, model=%s, kind=%s",
			vendor.Name, modelName, result.FailureKind)
		if err := model.UpdateTaskStatusWithRefund(task.TaskID, model.StatusCallFail, `{"error":"vendor rejected"}`); err != nil {
			common.SysErrorf("[ImageGenerate] 退还积分失败: taskID=%s, err=%v", task.TaskID, err)
		}
		c.JSON(http.StatusOK, gin.H{"code": "fail", "message": "供应商调用失败"})
		return
	}

	// 13. 结果不确定（超时、连接中断、响应读不完整）。
	//
	// 这里是个两难：上游可能已经建单并计费，但我们没拿到 task_id，无从查证。
	// 选择退款，理由是用户确实没有拿到任何东西，收钱不发货比平台承担损失更糟；
	// 且这条路径被限流约束，敞口有限。[SUBMIT_UNKNOWN] 是留给对账的检索标记——
	// 若这类日志频繁出现，说明上游不稳定或提交超时设置不合理，需要调参。
	if result.FailureKind == supplier.FailureUnknown {
		common.SysErrorf("[ImageGenerate] [SUBMIT_UNKNOWN] 提交结果不确定，已退还积分待人工核对: taskID=%s, userID=%d, vendor=%s, model=%s",
			task.TaskID, userID, vendor.Name, modelName)
		if err := model.UpdateTaskStatusWithRefund(task.TaskID, model.StatusCallFail, `{"error":"submit result unknown"}`); err != nil {
			common.SysErrorf("[ImageGenerate] 退还积分失败: taskID=%s, err=%v", task.TaskID, err)
		}
		c.JSON(http.StatusOK, gin.H{"code": "fail", "message": "上游调用结果不确定，积分已退还"})
		return
	}

	// 14. 提交成功：记录供应商响应并交给对账循环轮询。
	//
	// 这一步必须在把 taskId 返回给调用方之前成功：vendor_response 是重启后
	// 找回上游任务的唯一线索，写失败而进程随后退出，就再也查不到这个任务了。
	vendorRespJSON, err := json.Marshal(result.Data)
	if err != nil {
		common.SysErrorf("[ImageGenerate] 供应商响应序列化失败: taskID=%s, err=%v", task.TaskID, err)
	}
	if err := setVendorResponseWithRetry(task.TaskID, string(vendorRespJSON)); err != nil {
		common.SysErrorf("[ImageGenerate] [SUBMIT_UNKNOWN] 记录供应商响应失败，重启后该任务将无法对账: taskID=%s, err=%v",
			task.TaskID, err)
	}

	common.SysLogf("[ImageGenerate] 任务创建成功: taskId=%s, vendor=%s, model=%s", task.TaskID, vendor.Name, modelName)

	// 15. 立即安排一次轮询；抢不到并发位也无妨——任务保持非终态，
	// 下一轮对账循环会接手。这里**绝不**因为「排不上队」而退款。
	ScheduleTaskPoll(task.TaskID)

	// 16. 返回系统 taskId
	c.JSON(http.StatusOK, gin.H{
		"code":   "success",
		"taskId": task.TaskID,
	})
}

// parseIdempotencyKey 读取 Idempotency-Key 请求头。
// 返回 (nil, true) 表示调用方没带这个头——这是绝大多数请求的情况，不做去重。
func parseIdempotencyKey(c *gin.Context) (*string, bool) {
	raw := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if raw == "" {
		return nil, true
	}
	if len(raw) > maxIdempotencyKeyLen {
		return nil, false
	}
	return &raw, true
}

// setVendorResponseWithRetry 记录供应商响应，失败时重试几次。
//
// 值得重试的原因：写失败意味着上游 task_id 只存在于内存里，
// 一旦进程随后退出，这个任务就永远无法对账（上游照常出图计费）。
// 数据库抖动是短暂的，重试能把绝大多数情况救回来。
func setVendorResponseWithRetry(taskID, vendorResponse string) error {
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		if err = model.SetTaskVendorResponse(taskID, vendorResponse); err == nil {
			return nil
		}
		common.SysErrorf("[ImageGenerate] 记录供应商响应失败(第%d次): taskID=%s, err=%v", attempt, taskID, err)
		time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
	}
	return err
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
func marshalRequestBody(v interface{}) string {
	data, err := json.Marshal(v)
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
//
// 基础积分按最短形式输出（1 → "1"，0.001 → "0.001"）：位数写死会与实际扣费对不上，
// 而这是给人看的审计备注，写了多少就该扣了多少。
func (r creditResolution) Remark() string {
	if r.RefImageCount <= 0 {
		return "图像生成任务"
	}
	return fmt.Sprintf("图像生成任务(基础%s+参考图%d张)", formatCredits(r.Base), r.RefImageCount)
}

// formatCredits 以最短形式输出积分值（去掉末尾的 0）。
// 输入已由 model.RoundCredits 收敛到业务精度，因此不会打出浮点噪声。
func formatCredits(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// resolveCredits 按模型的计费规则计算本次请求应扣积分。
// 未配置规则时返回错误（fail-closed）：否则模型漏配规则会变成对所有人免费，
// 而这是运维上极易发生、且不会被察觉的资损。如需免费模型，请显式配置一条 0 积分的规则。
//
// 计费公式：基础积分（或命中的参数组合积分）+ 参考图张数 × 每张参考图积分。
// 参考图部分是叠加而非取代，且需要同时满足两个条件：模型类型是图像生成、
// 且 endpointPath 命中图片端点白名单。
//
// 计费口径只认标准字段：调用方发的非标准字段进不了 req，因此既不会命中条件，
// 也不会被算成参考图——价格永远对得上实际发往上游的请求。
func resolveCredits(modelID int, modelType model.ModelType, req supplier.ImageGenerateRequest, endpointPath string) (creditResolution, error) {
	creditRule, err := model.GetCreditRuleByModelID(modelID)
	if err != nil || creditRule == nil {
		return creditResolution{}, errors.New("模型未配置计费规则")
	}

	creditsAmount := creditRule.BaseCredits

	// 参数组合差异化定价：某组合的所有条件都命中时使用该组合的积分。
	// 注意条件只支持字符串值的标准字段，数组值（如参考图）永远匹配不上。
	params := req.CreditParams()
	for _, item := range creditRule.Items {
		matched := len(item.Conditions) > 0
		for _, cond := range item.Conditions {
			if params[cond.ParamPath] != cond.ParamValue {
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
			refImageCount = countRefImages(req.ImageURLs)
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

	// 收敛到业务精度：0.001*3 这类累加会产出 0.0030000000000000005。
	// 不收敛的话内存值与落库值不一致，对账时会被当成差异。
	return creditResolution{
		Total:         model.RoundCredits(creditsAmount),
		Base:          model.RoundCredits(baseAmount),
		RefImageCount: refImageCount,
	}, nil
}

// countRefImages 统计参考图张数。
//
// 按「去重后的 URL」计数：同一张图在数组里出现多次只算一张，
// 否则调用方复制一遍 URL 就会被多扣一次。
// 类型由标准结构体保证是 []string，不再需要按取值形态分支判断。
func countRefImages(imageURLs []string) int {
	seen := make(map[string]bool, len(imageURLs))
	for _, u := range imageURLs {
		// 空串是「占位但没填」，不算一张图
		if u != "" {
			seen[u] = true
		}
	}

	if len(seen) > maxRefImagesPerRequest {
		common.SysErrorf("[countRefImages] 参考图张数 %d 超过上限 %d，超出部分不计费", len(seen), maxRefImagesPerRequest)
		return maxRefImagesPerRequest
	}
	return len(seen)
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

// ── 任务对账（轮询上游状态）──
//
// 为什么是「一个定时对账循环」而不是「每个任务一个常驻 goroutine」：
//
// 后者的每一步都要靠并发上限兜底，而上限一旦占满就面临两难——要么阻塞调用方，
// 要么放弃这个任务。原实现选择了后者：直接退款并把任务置为失败。但那时上游
// 往往已经受理并计费，于是变成「平台付钱、用户免费」，且单个用户就能靠持续
// 提交慢任务占满全部槽位，把整个平台的图像生成变成「永远秒退」。
//
// 对定时循环而言这个两难根本不存在：这一轮排不上就等下一轮，任务始终留在
// 非终态，既不退款也不丢弃。goroutine 数量也不再随任务数增长。

const (
	// reconcileInterval 对账间隔，同时也是同一任务两次上游查询之间的间隔
	reconcileInterval = 5 * time.Second

	// reconcileBatchSize 单轮最多处理的任务数（防止异常堆积时一次性读满内存）
	reconcileBatchSize = 500

	// maxConcurrentQueries 同时在飞的上游查询数
	maxConcurrentQueries = 50

	// taskMaxPollAge 任务超过这个年龄仍无法确认结果，则退款并告警交人工对账。
	// 上游异步生成正常在分钟级完成，6 小时仍未终结基本可以断定是异常。
	taskMaxPollAge = 6 * time.Hour

	// stalePendingGrace 判定「从未提交成功」所需的静默期。
	// 必须显著大于提交超时：一条**正在同步提交**的任务同样是 pending 且
	// vendor_response 为空，若把它也捞出来退款，用户就会既拿图又退款。
	// 只有确定超出提交窗口仍未落库的，才是真的没提交成功。
	stalePendingGrace = supplier.SubmitTimeout + 60*time.Second
)

// querySem 限制同时在飞的上游查询数
var querySem = make(chan struct{}, maxConcurrentQueries)

// inFlightPolls 本进程正在查询的任务，避免同一任务被并发查询
var inFlightPolls sync.Map

// claimPoll 尝试认领一个任务的查询权；已被认领则返回 false
func claimPoll(taskID string) bool {
	_, loaded := inFlightPolls.LoadOrStore(taskID, struct{}{})
	return !loaded
}

// releasePoll 释放查询权
func releasePoll(taskID string) {
	inFlightPolls.Delete(taskID)
}

// recoverPollPanic 兜住轮询 goroutine 里的 panic。
//
// 没有它，后台 goroutine 里任何一个 panic 都会直接终止整个进程：
// gin 的 CustomRecovery 只包住 HTTP handler，对 goroutine 完全无效。
// 一个后台任务的 bug 不该让全站用户的服务中断。
func recoverPollPanic(taskID string) {
	if r := recover(); r != nil {
		common.SysErrorf("[Reconcile] 轮询 panic 已捕获，任务保持非终态待下一轮: taskID=%s, err=%v", taskID, r)
	}
}

// ScheduleTaskPoll 立即安排一次上游查询。
//
// 抢不到并发位时**什么都不做**：任务保持非终态，下一轮对账循环会接手。
// 这里绝不能因为「排不上队」而退款——排不上队是我们自己的容量问题，
// 不是上游没有受理。
func ScheduleTaskPoll(taskID string) {
	task, err := model.GetTaskByTaskID(taskID)
	if err != nil {
		common.SysErrorf("[Reconcile] 读取任务失败，交由下一轮对账: taskID=%s, err=%v", taskID, err)
		return
	}
	if model.IsTerminalStatus(task.Status) || task.VendorResponse == "" {
		return
	}

	select {
	case querySem <- struct{}{}:
	default:
		return // 并发位已满，交给对账循环
	}

	if !claimPoll(taskID) {
		<-querySem
		return
	}

	go func() {
		defer func() { <-querySem }()
		defer releasePoll(taskID)
		defer recoverPollPanic(taskID)
		pollOnce(*task, newVendorCredentialCache())
	}()
}

// RunTaskReconciler 启动任务对账循环，直到 ctx 被取消。
//
// 它同时承担了原来「启动时恢复未完成任务」的职责，且做得更完整：
// 原实现只在进程启动时扫一次，处理函数在扣费与安排轮询之间 panic，
// 任务就会永远停在非终态、积分挂着不退；定时循环没有这个盲区。
func RunTaskReconciler(ctx context.Context) {
	common.SysLogf("[Reconcile] 任务对账循环已启动（间隔 %s，并发 %d，兜底时限 %s）",
		reconcileInterval, maxConcurrentQueries, taskMaxPollAge)

	// 启动时先跑一轮，把上个进程遗留的非终态任务接过来
	reconcileOnce()

	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			common.SysLogf("[Reconcile] 任务对账循环已停止")
			return
		case <-ticker.C:
			reconcileOnce()
		}
	}
}

// reconcileOnce 执行一轮对账。
func reconcileOnce() {
	now := time.Now()

	// 1) 停在 pending 且从未记下供应商响应：确定没有提交成功，退款。
	//    有静默期保护，正在提交中的任务不会被误伤。
	stale, err := model.GetStalePendingTasks(now.Add(-stalePendingGrace), reconcileBatchSize)
	if err != nil {
		common.SysErrorf("[Reconcile] 查询滞留任务失败: %v", err)
	} else {
		for _, t := range stale {
			common.SysErrorf("[Reconcile] 任务停留 pending 且无供应商响应，判定提交未成功并退款: taskID=%s", t.TaskID)
			if err := model.UpdateTaskStatusWithRefund(t.TaskID, model.StatusCallFail, `{"error":"submit never recorded"}`); err != nil {
				common.SysErrorf("[Reconcile] 退款失败: taskID=%s, err=%v", t.TaskID, err)
			}
		}
	}

	// 2) 超过兜底时限仍未能确认结果：退款 + 告警。这类任务已无法通过对账确认，
	//    只能人工介入——日志里明确标出来，便于事后核对上游账单。
	expired, err := model.GetExpiredTasks(now.Add(-taskMaxPollAge), reconcileBatchSize)
	if err != nil {
		common.SysErrorf("[Reconcile] 查询超龄任务失败: %v", err)
	} else {
		for _, t := range expired {
			common.SysErrorf("[Reconcile] [需人工对账] 任务超过 %s 仍未能确认结果，退款并终结: taskID=%s, status=%s, 创建于=%s",
				taskMaxPollAge, t.TaskID, t.Status, t.CreatedAt.Format(time.RFC3339))
			if err := model.UpdateTaskStatusWithRefund(t.TaskID, model.StatusCallFail, `{"error":"unresolved beyond max age"}`); err != nil {
				common.SysErrorf("[Reconcile] 退款失败: taskID=%s, err=%v", t.TaskID, err)
			}
		}
	}

	// 3) 其余非终态任务：查一次上游
	tasks, err := model.GetTasksAwaitingPoll(reconcileBatchSize)
	if err != nil {
		common.SysErrorf("[Reconcile] 查询待轮询任务失败: %v", err)
		return
	}
	if len(tasks) == 0 {
		return
	}

	creds := newVendorCredentialCache()
	var wg sync.WaitGroup

	for _, t := range tasks {
		if !claimPoll(t.TaskID) {
			continue // 已在本进程的查询中
		}

		select {
		case querySem <- struct{}{}:
		default:
			// 并发位已满：本轮到此为止，剩余任务下一轮继续。
			// 它们保持非终态，不受任何影响。
			releasePoll(t.TaskID)
			wg.Wait()
			return
		}

		wg.Add(1)
		go func(t model.Task) {
			defer wg.Done()
			defer func() { <-querySem }()
			defer releasePoll(t.TaskID)
			defer recoverPollPanic(t.TaskID)
			pollOnce(t, creds)
		}(t)
	}

	wg.Wait()
}

// pollOnce 查询一次上游任务状态并据此推进状态机。
func pollOnce(task model.Task, creds *vendorCredentialCache) {
	s, ok := creds.supplierFor(task.VendorID)
	if !ok {
		// 供应商不可用（被删、密钥解不开）不是任务失败，保持非终态等超龄兜底
		common.SysErrorf("[Reconcile] 供应商不可用，本轮跳过: taskID=%s, vendorID=%d", task.TaskID, task.VendorID)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), supplier.QueryTimeout)
	defer cancel()

	result := s.TaskQuery(ctx, task.VendorResponse)

	switch result.Status {
	case model.StatusCompleted:
		dataJSON, err := json.Marshal(result.Data)
		if err != nil {
			common.SysErrorf("[Reconcile] 结果序列化失败: taskID=%s, err=%v", task.TaskID, err)
		}
		if err := model.UpdateTaskStatus(task.TaskID, model.StatusCompleted, string(dataJSON)); err != nil {
			common.SysErrorf("[Reconcile] 更新任务状态失败: taskID=%s, err=%v", task.TaskID, err)
		} else {
			common.SysLogf("[Reconcile] 任务完成: taskID=%s", task.TaskID)
		}

	case model.StatusFailed, model.StatusCancelled:
		// 上游明确告诉我们任务失败/被取消 → 这是可退款的终态
		if err := model.UpdateTaskStatusWithRefund(task.TaskID, result.Status, ""); err != nil {
			common.SysErrorf("[Reconcile] 更新任务状态失败: taskID=%s, err=%v", task.TaskID, err)
		} else {
			common.SysLogf("[Reconcile] 任务终止并退款: taskID=%s, status=%s", task.TaskID, result.Status)
		}

	case model.StatusCallFail:
		// 查询本身失败，任务状态仍然未知——**不得退款**，等下一轮重试
		common.SysErrorf("[Reconcile] 查询失败，任务保持非终态待下一轮: taskID=%s", task.TaskID)

	default:
		// pending / processing：上游仍在生成
	}
}

// ── 供应商凭据缓存 ──

// vendorCredentialCache 单轮对账内的供应商凭据缓存。
// 一轮里几十个任务常常属于同一个供应商，逐个查库并解密密钥纯属浪费。
type vendorCredentialCache struct {
	mu      sync.Mutex
	entries map[int]*cachedSupplier
}

type cachedSupplier struct {
	s  supplier.Supplier
	ok bool
}

func newVendorCredentialCache() *vendorCredentialCache {
	return &vendorCredentialCache{entries: make(map[int]*cachedSupplier)}
}

func (c *vendorCredentialCache) supplierFor(vendorID int) (supplier.Supplier, bool) {
	c.mu.Lock()
	if e, found := c.entries[vendorID]; found {
		c.mu.Unlock()
		return e.s, e.ok
	}
	c.mu.Unlock()

	s, ok := loadSupplier(vendorID)

	c.mu.Lock()
	c.entries[vendorID] = &cachedSupplier{s: s, ok: ok}
	c.mu.Unlock()

	return s, ok
}

// loadSupplier 查库并构造供应商客户端。
// 供应商实现只持有不可变的配置，共用同一个进程级 HTTP 客户端，因此并发安全。
func loadSupplier(vendorID int) (supplier.Supplier, bool) {
	vendor, err := model.GetVendorByID(vendorID)
	if err != nil {
		common.SysErrorf("[Reconcile] 供应商不存在: vendorID=%d, err=%v", vendorID, err)
		return nil, false
	}

	apiKey, err := common.DecryptSecret(vendor.APIKey)
	if err != nil {
		common.SysErrorf("[Reconcile] 供应商密钥解密失败: vendor=%s, err=%v", vendor.Name, err)
		return nil, false
	}

	s := supplier.NewSupplier(vendor.Name, supplier.Config{BaseURL: vendor.BaseURL, APIKey: apiKey})
	if s == nil {
		common.SysErrorf("[Reconcile] 不支持的供应商类型: %s", vendor.Name)
		return nil, false
	}
	return s, true
}
