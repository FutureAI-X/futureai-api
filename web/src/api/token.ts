const BASE = '/api/user'

function authHeaders(): Record<string, string> {
  const token = localStorage.getItem('token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}

async function request<T>(url: string, options?: RequestInit): Promise<T> {
  // 注意顺序：options 先展开，headers 最后合并。
  // 若按原来的写法（headers 在前、...options 在后），任何传入 headers 的调用
  // 都会整体覆盖掉 Authorization，导致 401。
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

export interface ApiKey {
  id: number
  name: string
  /**
   * 只有前后各几位。库里存的是 API Key 的哈希，完整明文已不可恢复——
   * 连服务端也拿不到，因此列表不再有「传 data_key 就返回完整值」这回事。
   */
  key_prefix: string
  key_suffix: string
  status: number
  created_at: string
}

/** 创建接口的响应：唯一一次能看到完整 key 的地方 */
export interface CreatedApiKey {
  id: number
  name: string
  /** 用会话的 data_key 加密后的完整 key，需要在前端解密后展示 */
  key: string
  created_at: string
}

export function getTokens() {
  // 只返回前后缀，无需 data_key
  return request<{ success: boolean; data: ApiKey[] }>(`${BASE}/tokens`)
}

export function createToken(name: string, dataKey: string) {
  return request<{ success: boolean; message: string; data?: CreatedApiKey }>(`${BASE}/tokens`, {
    method: 'POST',
    body: JSON.stringify({ name, data_key: dataKey }),
  })
}

export function updateToken(id: number, name: string) {
  return request<{ success: boolean; message: string }>(`${BASE}/tokens/${id}`, {
    method: 'PUT',
    body: JSON.stringify({ name }),
  })
}

export function deleteToken(id: number) {
  return request<{ success: boolean; message: string }>(`${BASE}/tokens/${id}`, {
    method: 'DELETE',
  })
}
