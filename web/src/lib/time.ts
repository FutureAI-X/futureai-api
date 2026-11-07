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
