import { useState, useEffect, useCallback } from 'react'
import {
  Loader2,
  ChevronLeft,
  ChevronRight,
  Minus,
  Plus,
} from 'lucide-react'
import { cn } from '../../lib/utils'
import { CopyButton } from '../../components/CopyButton'
import { formatCredits } from '../../lib/credits'

const BASE = '/api/user'

function authHeaders(): Record<string, string> {
  const token = localStorage.getItem('token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}

interface CreditLog {
  id: number
  task_id: string
  credits: number
  type: string
  remark: string
  created_at: string
}

const TYPE_CONFIG: Record<string, { label: string; className: string; icon: typeof Minus }> = {
  deduct: { label: '扣除', className: 'text-red-600 dark:text-red-400', icon: Minus },
  refund: { label: '退还', className: 'text-emerald-600 dark:text-emerald-400', icon: Plus },
}

// 格式化时间
function formatTime(timeStr?: string): string {
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

export function CreditLogs() {
  const [logs, setLogs] = useState<CreditLog[]>([])
  const [loading, setLoading] = useState(true)
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize] = useState(20)

  const loadLogs = useCallback(async () => {
    setLoading(true)
    try {
      const res = await fetch(`${BASE}/credit-logs?page=${page}&page_size=${pageSize}`, {
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
      })
      const data = await res.json()
      if (data.success) {
        setLogs(data.data.items || [])
        setTotal(data.data.total || 0)
      }
    } catch { /* ignore */ }
    finally { setLoading(false) }
  }, [page, pageSize])

  useEffect(() => { loadLogs() }, [loadLogs])

  const totalPages = Math.ceil(total / pageSize)

  return (
    <div className='space-y-6'>
      <div>
        <h2 className='text-2xl font-bold tracking-tight'>积分日志</h2>
        <p className='text-muted-foreground mt-1 text-sm'>查看积分消耗和退还记录</p>
      </div>

      {/* 统计信息 */}
      <div className='flex items-center gap-3'>
        <span className='text-muted-foreground text-sm'>
          共 <span className='text-foreground font-medium'>{total}</span> 条记录
        </span>
      </div>

      {/* 表格 */}
      <div className='border-border/60 overflow-hidden rounded-xl border'>
        <div className='overflow-x-auto'>
          <table className='w-full text-sm'>
            <thead>
              <tr className='bg-muted/30 border-border/40 border-b'>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>ID</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>类型</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>积分</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>任务ID</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>备注</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>时间</th>
              </tr>
            </thead>
            <tbody className='divide-border/40 divide-y'>
              {loading ? (
                <tr><td colSpan={6} className='px-4 py-12 text-center'><Loader2 className='text-muted-foreground mx-auto size-6 animate-spin' /></td></tr>
              ) : logs.length === 0 ? (
                <tr><td colSpan={6} className='px-4 py-12 text-center'><p className='text-muted-foreground text-sm'>暂无积分记录</p></td></tr>
              ) : logs.map((log) => {
                const typeConf = TYPE_CONFIG[log.type] || TYPE_CONFIG.deduct
                const TypeIcon = typeConf.icon
                return (
                  <tr key={log.id} className='hover:bg-muted/20 transition-colors'>
                    <td className='px-4 py-3'><span className='text-muted-foreground font-mono text-xs'>{log.id}</span></td>
                    <td className='px-4 py-3'>
                      <span className={cn('inline-flex items-center gap-1 text-xs font-medium', typeConf.className)}>
                        <TypeIcon className='size-3' />
                        {typeConf.label}
                      </span>
                    </td>
                    <td className='px-4 py-3'>
                      <span className={cn('text-sm font-medium', typeConf.className)}>
                        {log.type === 'refund' ? '+' : '-'}{formatCredits(log.credits)}
                      </span>
                    </td>
                    <td className='px-4 py-3'>
                      {log.task_id ? (
                        <div className='flex items-center gap-1'>
                          <span className='font-mono text-xs break-all'>{log.task_id}</span>
                          <CopyButton text={log.task_id} />
                        </div>
                      ) : (
                        <span className='text-muted-foreground text-xs'>-</span>
                      )}
                    </td>
                    <td className='px-4 py-3'>
                      <span className='text-muted-foreground text-xs'>{log.remark || '-'}</span>
                    </td>
                    <td className='px-4 py-3'>
                      <span className='text-muted-foreground text-xs'>{formatTime(log.created_at)}</span>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      </div>

      {/* 分页 */}
      {totalPages > 1 && (
        <div className='flex items-center justify-between'>
          <span className='text-muted-foreground text-sm'>
            第 {page} / {totalPages} 页
          </span>
          <div className='flex items-center gap-2'>
            <button
              onClick={() => setPage(p => Math.max(1, p - 1))}
              disabled={page === 1}
              className='border-border/60 hover:bg-muted inline-flex h-9 items-center gap-1 rounded-lg border px-3 text-sm font-medium transition-colors disabled:opacity-50'
            >
              <ChevronLeft className='size-4' />
              上一页
            </button>
            <button
              onClick={() => setPage(p => Math.min(totalPages, p + 1))}
              disabled={page === totalPages}
              className='border-border/60 hover:bg-muted inline-flex h-9 items-center gap-1 rounded-lg border px-3 text-sm font-medium transition-colors disabled:opacity-50'
            >
              下一页
              <ChevronRight className='size-4' />
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
