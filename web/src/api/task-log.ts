const BASE = '/api/user'

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

export interface TaskLog {
  id: number
  task_id: string
  status: string
  credits: number
  credits_refunded: boolean
  request_body?: string
  query_response?: string
  created_at: string
  updated_at: string
}

export interface TaskLogListResponse {
  items: TaskLog[]
  total: number
  page: number
  page_size: number
}

export function getTaskLogs(params: {
  page?: number
  page_size?: number
  status?: string
}) {
  const searchParams = new URLSearchParams()
  if (params.page) searchParams.set('page', params.page.toString())
  if (params.page_size) searchParams.set('page_size', params.page_size.toString())
  if (params.status) searchParams.set('status', params.status)

  return request<{ success: boolean; data: TaskLogListResponse }>(
    `${BASE}/task-logs?${searchParams.toString()}`
  )
}

export function getTaskLogDetail(id: number) {
  return request<{ success: boolean; data: TaskLog }>(`${BASE}/task-logs/${id}`)
}
