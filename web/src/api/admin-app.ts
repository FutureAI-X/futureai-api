const BASE = '/api/admin'

function authHeaders(): Record<string, string> {
  const token = localStorage.getItem('token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}

async function request<T>(url: string, options?: RequestInit): Promise<T> {
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

// ── 应用管理 ──

export interface AdminApp {
  id: number
  /** 客户端上报的稳定标识，创建后不可修改 */
  key: string
  name: string
  description: string
  status: number
  created_at: string
}

export function getApps() {
  return request<{ success: boolean; data: AdminApp[] }>(`${BASE}/apps`)
}

export function createApp(data: { key: string; name: string; description?: string }) {
  return request<{ success: boolean; message: string }>(`${BASE}/apps`, {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export function updateApp(id: number, data: { name?: string; description?: string }) {
  return request<{ success: boolean; message: string }>(`${BASE}/apps/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export function updateAppStatus(id: number, status: number) {
  return request<{ success: boolean; message: string }>(`${BASE}/apps/${id}/status`, {
    method: 'PUT',
    body: JSON.stringify({ status }),
  })
}

export function deleteApp(id: number) {
  return request<{ success: boolean; message: string }>(`${BASE}/apps/${id}`, {
    method: 'DELETE',
  })
}

// ── 版本发布 ──

export interface AdminAppRelease {
  id: number
  app_id: number
  app_key: string
  app_name: string
  platform: string
  version: string
  update_url: string
  release_notes: string
  status: number
  created_at: string
}

export function getAppReleases(params?: { app_id?: number; platform?: string }) {
  const query = new URLSearchParams()
  if (params?.app_id) query.set('app_id', String(params.app_id))
  if (params?.platform) query.set('platform', params.platform)
  const suffix = query.toString() ? `?${query.toString()}` : ''

  return request<{ success: boolean; data: AdminAppRelease[] }>(`${BASE}/app-releases${suffix}`)
}

export function createAppRelease(data: {
  app_id: number
  platform: string
  version: string
  update_url: string
  release_notes?: string
}) {
  return request<{ success: boolean; message: string }>(`${BASE}/app-releases`, {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export function updateAppRelease(id: number, data: {
  app_id?: number
  platform?: string
  version?: string
  update_url?: string
  release_notes?: string
}) {
  return request<{ success: boolean; message: string }>(`${BASE}/app-releases/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export function updateAppReleaseStatus(id: number, status: number) {
  return request<{ success: boolean; message: string }>(`${BASE}/app-releases/${id}/status`, {
    method: 'PUT',
    body: JSON.stringify({ status }),
  })
}

export function deleteAppRelease(id: number) {
  return request<{ success: boolean; message: string }>(`${BASE}/app-releases/${id}`, {
    method: 'DELETE',
  })
}
