const BASE = '/api/admin'

function authHeaders(): Record<string, string> {
  const token = localStorage.getItem('token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}

async function request<T>(url: string, options?: RequestInit): Promise<T> {
  // options 先展开、headers 最后合并：若反过来，任何传入 headers 的调用
  // 都会整体覆盖掉 Authorization，导致 401
  const res = await fetch(url, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...authHeaders(),
      ...((options?.headers as Record<string, string>) ?? {}),
    },
  })
  return res.json()
}

export interface AdminModel {
  id: number
  name: string
  owner: string
  description: string
  tags: string
  status: number
  /** 模型类型：image / video / text / music / other */
  type: string
  /** 图像模型的参数规格；未配置为 null */
  param_spec: Record<string, unknown> | null
  /** 规格是否已配置且通过元校验 */
  param_spec_valid: boolean
  /** 管理端手动排序值；null/ undefined 表示未排序 */
  sort_order?: number | null
  created_at: string
}

export function getModels() {
  return request<{ success: boolean; data: AdminModel[] }>(`${BASE}/models`)
}

export function createModel(data: {
  name: string
  owner: string
  /** 必填：后端 binding:"required"，且类型参与参考图加价判定 */
  type: string
  description?: string
  tags?: string
}) {
  return request<{ success: boolean; message: string }>(`${BASE}/models`, {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export function updateModel(id: number, data: {
  name?: string
  owner?: string
  description?: string
  tags?: string
  type?: string
}) {
  return request<{ success: boolean; message: string }>(`${BASE}/models/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

/** 保存模型展示顺序：ids 为从上到下的完整列表 */
export function reorderModels(ids: number[]) {
  return request<{ success: boolean; message: string }>(`${BASE}/model-order`, {
    method: 'PUT',
    body: JSON.stringify({ ids }),
  })
}

/** 恢复默认排序：清空所有手动排序值 */
export function resetModelOrder() {
  return request<{ success: boolean; message: string }>(`${BASE}/model-order`, {
    method: 'DELETE',
  })
}

export function updateModelStatus(id: number, status: number) {
  return request<{ success: boolean; message: string }>(`${BASE}/models/${id}/status`, {
    method: 'PUT',
    body: JSON.stringify({ status }),
  })
}

export function updateModelParamSpec(id: number, paramSpec: Record<string, unknown> | null) {
  return request<{ success: boolean; message: string }>(`${BASE}/models/${id}/param-spec`, {
    method: 'PUT',
    body: JSON.stringify({ param_spec: paramSpec }),
  })
}

export function deleteModel(id: number) {
  return request<{ success: boolean; message: string }>(`${BASE}/models/${id}`, {
    method: 'DELETE',
  })
}

// ── 模型端点关联 ──

export interface ModelEndpoint {
  id: number
  model_id: number
  endpoint_id: number
  endpoint_path: string
  endpoint_name: string
}

export function getModelEndpoints(modelId: number) {
  return request<{ success: boolean; data: ModelEndpoint[] }>(`${BASE}/models/${modelId}/endpoints`)
}

export function syncModelEndpoints(modelId: number, endpointIds: number[]) {
  return request<{ success: boolean; message: string }>(`${BASE}/models/${modelId}/endpoints`, {
    method: 'PUT',
    body: JSON.stringify({ endpoint_ids: endpointIds }),
  })
}

// ── 积分规则 ──

export interface CreditRuleCondition {
  id?: number
  item_id?: number
  param_path: string
  param_value: string
}

export interface CreditRuleItem {
  id?: number
  rule_id?: number
  credits: number
  conditions?: CreditRuleCondition[]
}

export interface CreditRule {
  id: number
  model_id: number
  rule_type: string
  base_credits: number
  description?: string
  status: number
  /** 每张参考图消耗的积分，0 = 不计费。叠加在基础积分之上 */
  ref_image_credits?: number
  /** per_token：文本输入每百万 token 的积分 */
  input_text_credits_per_mtok?: number
  /** per_token：图片输入每百万 token 的积分 */
  image_input_credits_per_mtok?: number
  /** per_token：图片输出每百万 token 的积分 */
  image_output_credits_per_mtok?: number
  /** per_token：预扣估算参数 */
  token_config?: Record<string, unknown> | null
  created_at: string
  updated_at: string
  items?: CreditRuleItem[]
}

export function getCreditRule(modelId: number) {
  return request<{ success: boolean; data: CreditRule | null }>(`${BASE}/models/${modelId}/credit-rule`)
}

export function saveCreditRule(modelId: number, data: {
  rule_type: string
  base_credits: number
  description?: string
  ref_image_credits?: number
  input_text_credits_per_mtok?: number
  image_input_credits_per_mtok?: number
  image_output_credits_per_mtok?: number
  token_config?: Record<string, unknown> | null
  items?: CreditRuleItem[]
}) {
  return request<{ success: boolean; message: string }>(`${BASE}/models/${modelId}/credit-rule`, {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export function updateCreditRuleStatus(id: number, status: number) {
  return request<{ success: boolean; message: string }>(`${BASE}/credit-rules/${id}/status`, {
    method: 'PUT',
    body: JSON.stringify({ status }),
  })
}

export function deleteCreditRule(id: number) {
  return request<{ success: boolean; message: string }>(`${BASE}/credit-rules/${id}`, {
    method: 'DELETE',
  })
}

export function deleteModelCreditRule(modelId: number) {
  return request<{ success: boolean; message: string }>(`${BASE}/models/${modelId}/credit-rule`, {
    method: 'DELETE',
  })
}

/** 视频计费的单个分辨率档位 */
export interface VideoCreditRate {
  id?: number
  rule_id?: number
  /** 输出分辨率档位，统一大写 */
  resolution: string
  /** 每个参考视频秒消耗的积分 */
  input_video_credits_per_second: number
  /** 不含参考视频时每秒输出消耗的积分 */
  output_credits_per_second_no_ref: number
  /** 含参考视频时每秒输出消耗的积分 */
  output_credits_per_second_with_ref: number
  created_at?: string
  updated_at?: string
}

/** 视频模型计费规则 */
export interface VideoCreditRule {
  id: number
  model_id: number
  description?: string
  status: number
  /** 每张参考图积分 */
  image_credits_per_item: number
  /** 每请求免费的前 N 张参考图 */
  image_free_items: number
  /** 每个参考音频积分 */
  audio_credits_per_item: number
  rates?: VideoCreditRate[]
  created_at: string
  updated_at: string
}

export function getVideoCreditRule(modelId: number) {
  return request<{ success: boolean; data: VideoCreditRule | null }>(
    `${BASE}/models/${modelId}/video-credit-rule`,
  )
}

export function saveVideoCreditRule(
  modelId: number,
  data: {
    description?: string
    status?: number
    image_credits_per_item: number
    image_free_items: number
    audio_credits_per_item: number
    rates: Array<{
      resolution: string
      input_video_credits_per_second: number
      output_credits_per_second_no_ref: number
      output_credits_per_second_with_ref: number
    }>
  },
) {
  return request<{ success: boolean; message: string }>(
    `${BASE}/models/${modelId}/video-credit-rule`,
    {
      method: 'PUT',
      body: JSON.stringify(data),
    },
  )
}
