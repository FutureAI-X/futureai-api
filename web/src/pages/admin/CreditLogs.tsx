import { useState, useEffect, useCallback } from 'react'
import { Loader2 } from 'lucide-react'
import { cn } from '../../lib/utils'
import { getAdminCreditLogs, type AdminCreditLog } from '../../api/admin'
import { UserSelect } from '../../components/admin/UserSelect'
import { CopyButton } from '../../components/CopyButton'
import { Pagination } from '../../components/Pagination'
import {
  creditLogDelta,
  creditLogDeltaClass,
  creditLogType,
  formatCreditLogDelta,
} from '../../lib/credit-log'

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

export function AdminCreditLogs() {
  const [logs, setLogs] = useState<AdminCreditLog[]>([])
  const [loading, setLoading] = useState(true)
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [userFilter, setUserFilter] = useState(0)

  const loadLogs = useCallback(async () => {
    setLoading(true)
    try {
      const res = await getAdminCreditLogs({
        page,
        page_size: pageSize,
        ...(userFilter ? { user_id: userFilter } : {}),
      })
      if (res.success) {
        setLogs(res.data.items || [])
        setTotal(res.data.total || 0)
      }
    } catch { /* ignore */ }
    finally { setLoading(false) }
  }, [page, pageSize, userFilter])

  useEffect(() => { loadLogs() }, [loadLogs])

  return (
    <div data-page-fill className='flex h-full min-h-0 flex-col gap-6'>
      <div className='shrink-0'>
        <h2 className='text-2xl font-bold tracking-tight'>积分日志</h2>
        <p className='text-muted-foreground mt-1 text-sm'>查看全部用户的积分消耗和退还记录</p>
      </div>

      {/* 筛选 */}
      <div className='flex shrink-0 flex-wrap items-center gap-3'>
        <UserSelect value={userFilter} onChange={(id) => { setUserFilter(id); setPage(1) }} />
      </div>

      {/* 表格 */}
      <div className='border-border/60 flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border'>
        <div className='min-h-0 flex-1 overflow-auto'>
          <table className='w-full text-sm'>
            <thead className='bg-muted sticky top-0 z-10'>
              <tr className='bg-muted/30 border-border/40 border-b'>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>ID</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>用户</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>类型</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>积分</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>任务ID</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>备注</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>时间</th>
              </tr>
            </thead>
            <tbody className='divide-border/40 divide-y'>
              {loading ? (
                <tr><td colSpan={7} className='px-4 py-12 text-center'><Loader2 className='text-muted-foreground mx-auto size-6 animate-spin' /></td></tr>
              ) : logs.length === 0 ? (
                <tr><td colSpan={7} className='px-4 py-12 text-center'><p className='text-muted-foreground text-sm'>暂无积分记录</p></td></tr>
              ) : logs.map((log) => {
                const typeConf = creditLogType(log.type)
                const TypeIcon = typeConf.icon
                const delta = creditLogDelta(log)
                return (
                  <tr key={log.id} className='hover:bg-muted/20 transition-colors'>
                    <td className='px-4 py-3'><span className='text-muted-foreground font-mono text-xs'>{log.id}</span></td>
                    <td className='px-4 py-3'><span className='font-medium'>{log.username || '-'}</span></td>
                    <td className='px-4 py-3'>
                      <span className={cn('inline-flex items-center gap-1 text-xs font-medium', typeConf.className)}>
                        <TypeIcon className='size-3' />
                        {typeConf.label}
                      </span>
                    </td>
                    <td className='px-4 py-3'>
                      <span
                        className={cn('text-sm font-medium', creditLogDeltaClass(delta))}
                        title={delta === 0 ? '历史覆盖记录未保存方向，± 表示仅知道变动数额' : undefined}
                      >
                        {formatCreditLogDelta(log)}
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
        <Pagination
          page={page}
          pageSize={pageSize}
          total={total}
          onPageChange={setPage}
          onPageSizeChange={setPageSize}
        />
      </div>
    </div>
  )
}
