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

// ── 用户列表 ──
export interface AdminUser {
  id: number
  username: string
  display_name: string
  role: number
  status: number
  email: string
  credits: number
  used_credits: number
  created_at: string
}

export interface UserListResponse {
  success: boolean
  data: { items: AdminUser[]; total: number }
  message?: string
}

export function getUsers(params: { p?: number; page_size?: number; keyword?: string }) {
  const query = new URLSearchParams()
  if (params.p) query.set('p', String(params.p))
  if (params.page_size) query.set('page_size', String(params.page_size))
  if (params.keyword) query.set('keyword', params.keyword)
  return request<UserListResponse>(`${BASE}/users?${query}`)
}

// ── 创建用户 ──
export function createUser(data: {
  username: string
  password: string
  display_name?: string
  role?: number
  credits?: number
}) {
  return request<{ success: boolean; message: string }>(`${BASE}/users`, {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

// ── 更新用户 ──
export function updateUser(id: number, data: { username?: string; display_name?: string; password?: string }) {
  return request<{ success: boolean; message: string }>(`${BASE}/users/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

// ── 删除用户 ──
export function deleteUser(id: number) {
  return request<{ success: boolean; message: string }>(`${BASE}/users/${id}`, {
    method: 'DELETE',
  })
}

// ── 更新用户状态 ──
export function updateUserStatus(id: number, status: number) {
  return request<{ success: boolean; message: string }>(`${BASE}/users/${id}/status`, {
    method: 'PUT',
    body: JSON.stringify({ status }),
  })
}

// ── 积分调整 ──
export function adjustUserCredits(id: number, mode: string, value: number) {
  return request<{ success: boolean; message: string }>(`${BASE}/users/${id}/credits`, {
    method: 'PUT',
    body: JSON.stringify({ mode, value }),
  })
}

// ── 重置密码 ──
export function resetUserPassword(id: number, dataKey: string) {
  return request<{ success: boolean; message: string; data?: { encrypted_password: string } }>(`${BASE}/users/${id}/password`, {
    method: 'PUT',
    body: JSON.stringify({ data_key: dataKey }),
  })
}

// ── 积分日志（全部用户） ──
export interface AdminCreditLog {
  id: number
  user_id: number
  username?: string
  task_id: string
  credits: number
  type: string
  remark: string
  created_at: string
}

export function getAdminCreditLogs(params: { page?: number; page_size?: number; user_id?: number }) {
  const query = new URLSearchParams()
  if (params.page) query.set('page', String(params.page))
  if (params.page_size) query.set('page_size', String(params.page_size))
  if (params.user_id) query.set('user_id', String(params.user_id))
  return request<{ success: boolean; data: { items: AdminCreditLog[]; total: number; page: number; page_size: number } }>(
    `${BASE}/credit-logs?${query}`
  )
}

// ── 任务日志（全部用户） ──
export interface AdminTaskLog {
  id: number
  task_id: string
  user_id: number
  username?: string
  status: string
  credits: number
  credits_refunded: boolean
  request_body?: string
  query_response?: string
  created_at: string
  updated_at: string
}

export function getAdminTaskLogs(params: { page?: number; page_size?: number; status?: string; user_id?: number }) {
  const query = new URLSearchParams()
  if (params.page) query.set('page', String(params.page))
  if (params.page_size) query.set('page_size', String(params.page_size))
  if (params.status) query.set('status', params.status)
  if (params.user_id) query.set('user_id', String(params.user_id))
  return request<{ success: boolean; data: { items: AdminTaskLog[]; total: number; page: number; page_size: number } }>(
    `${BASE}/task-logs?${query}`
  )
}

export function getAdminTaskLogDetail(id: number) {
  return request<{ success: boolean; data: AdminTaskLog }>(`${BASE}/task-logs/${id}`)
}
