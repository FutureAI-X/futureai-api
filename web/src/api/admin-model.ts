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

export function updateModelStatus(id: number, status: number) {
  return request<{ success: boolean; message: string }>(`${BASE}/models/${id}/status`, {
    method: 'PUT',
    body: JSON.stringify({ status }),
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
