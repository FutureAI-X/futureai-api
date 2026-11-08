// 列表和详情共用的时间格式化。
// 后端详情接口返回 RFC3339，列表接口返回已格式化字符串；
// 这里统一按本地时区展示成 "YYYY-MM-DD HH:mm:ss"。
export function formatTime(timeStr?: string): string {
  if (!timeStr) return '-'
  try {
    const date = new Date(timeStr)
    if (isNaN(date.getTime())) return timeStr
    const year = date.getFullYear()
    const month = String(date.getMonth() + 1).padStart(2, '0')
    const day = String(date.getDate()).padStart(2, '0')
    const hours = String(date.getHours()).padStart(2, '0')
    const minutes = String(date.getMinutes()).padStart(2, '0')
    const seconds = String(date.getSeconds()).padStart(2, '0')
    return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`
  } catch {
    return timeStr
  }
}

// 列表里的耗时展示。秒级精度足够，超过 1 分钟后不再显示秒。
export function formatDuration(seconds?: number): string {
  if (seconds === undefined || seconds === null || !Number.isFinite(seconds) || seconds < 0) return '-'

  const total = Math.floor(seconds)
  if (total < 1) return '<1 秒'
  if (total < 60) return `${total} 秒`

  const minutes = Math.floor(total / 60)
  if (minutes < 60) {
    const rest = total % 60
    return rest ? `${minutes} 分 ${rest} 秒` : `${minutes} 分`
  }

  const hours = Math.floor(minutes / 60)
  if (hours < 24) {
    const rest = minutes % 60
    return rest ? `${hours} 时 ${rest} 分` : `${hours} 时`
  }

  const days = Math.floor(hours / 24)
  const rest = hours % 24
  return rest ? `${days} 天 ${rest} 时` : `${days} 天`
}
