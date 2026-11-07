// 从任务的 query_response 里提取结果地址。
//
// 落库的并不是上游整包响应，而是归一化后的 result.Data（见 controller/image.go），
// 正常情况下就是 {"url": "https://..."}。但历史数据、失败任务和不同供应商的
// 结构并不一致，因此按优先级兜底；实在没有 URL 时给出一句可展示的原因。

const HTTP_URL = /^https?:\/\//i
const URL_IN_TEXT = /https?:\/\/[^\s"'<>\\]+/i

export interface TaskResult {
  url: string | null
  // 没有 URL 时可展示的简短原因（上游错误、base64 结果等）
  note: string | null
}

// 常见的结果字段路径，按由新到旧排列。
// 数组下标用字符串表达，对象和数组都能按下标取值。
const URL_PATHS: string[][] = [
  ['url'],
  ['data', 'url'],
  ['result', 'url'],
  ['output', 'url'],
  ['video_url'],
  ['data', '0', 'url'],
  ['images', '0', 'url'],
  ['result', 'images', '0', 'url'],
]

function readPath(root: unknown, path: string[]): unknown {
  let current = root
  for (const key of path) {
    if (current === null || typeof current !== 'object') return undefined
    current = (current as Record<string, unknown>)[key]
  }
  return current
}

// 字符串本身、字符串数组、以及常见的 {url: ...} 包装都接受
function firstHttpUrl(value: unknown): string | null {
  if (typeof value === 'string') {
    const trimmed = value.trim()
    return HTTP_URL.test(trimmed) ? trimmed : null
  }
  if (Array.isArray(value)) {
    for (const item of value) {
      const found = firstHttpUrl(item)
      if (found) return found
    }
  }
  return null
}

// 兜底：在对象里找第一个 http(s) 字符串。
// 限制递归深度，避免异常数据造成过深遍历。
function scanForUrl(value: unknown, depth = 0): string | null {
  if (depth > 6 || value === null || value === undefined) return null
  if (typeof value === 'string') return firstHttpUrl(value)
  if (Array.isArray(value)) {
    for (const item of value) {
      const found = scanForUrl(item, depth + 1)
      if (found) return found
    }
    return null
  }
  if (typeof value === 'object') {
    for (const key of Object.keys(value as Record<string, unknown>)) {
      const found = scanForUrl((value as Record<string, unknown>)[key], depth + 1)
      if (found) return found
    }
  }
  return null
}

function firstStringAt(root: unknown, paths: string[][]): string | null {
  for (const path of paths) {
    const value = readPath(root, path)
    if (typeof value === 'string' && value.trim() !== '') return value.trim()
  }
  return null
}

// 从请求体里取出 prompt，供详情页一键复制。
// 平台标准字段集中在顶层；历史数据可能包了一层，因此也做嵌套兜底。
const PROMPT_PATHS: string[][] = [
  ['prompt'],
  ['data', 'prompt'],
  ['input', 'prompt'],
]

export function extractPrompt(requestBody?: string | null): string | null {
  const text = (requestBody ?? '').trim()
  if (!text) return null

  try {
    const parsed = JSON.parse(text)
    for (const path of PROMPT_PATHS) {
      const value = readPath(parsed, path)
      if (typeof value === 'string' && value.trim() !== '') return value
    }
    return null
  } catch {
    // 请求体超长被截断时整段不是合法 JSON，但 prompt 通常仍在前面，
    // 尝试按字段提取；转义交给 JSON.parse 还原。
    const match = text.match(/"prompt"\s*:\s*"((?:\\.|[^"\\])*)"/)
    if (!match) return null
    try {
      return JSON.parse(`"${match[1]}"`)
    } catch {
      return match[1]
    }
  }
}

export function extractTaskResult(queryResponse?: string | null): TaskResult {
  const text = (queryResponse ?? '').trim()
  if (!text) return { url: null, note: null }

  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    // 非 JSON（或写入时已被截断）：直接从文本里捞第一个 URL
    const match = text.match(URL_IN_TEXT)
    return { url: match ? match[0] : null, note: null }
  }

  for (const path of URL_PATHS) {
    const url = firstHttpUrl(readPath(parsed, path))
    if (url) return { url, note: null }
  }

  const scanned = scanForUrl(parsed)
  if (scanned) return { url: scanned, note: null }

  const errorText = firstStringAt(parsed, [
    ['error'],
    ['message'],
    ['error_message'],
    ['msg'],
    ['data', 'error'],
    ['data', 'message'],
  ])
  if (errorText) return { url: null, note: errorText }

  // APIMart 在只有 b64_json 时也会把 url 留空，不能把 base64 当地址展示
  const b64 = firstStringAt(parsed, [['b64_json'], ['data', 'b64_json']])
  if (b64) return { url: null, note: '上游返回的是 base64 数据，没有结果地址' }

  return { url: null, note: null }
}
