import { useState, useEffect, useCallback } from 'react'
import {
  Loader2,
  ChevronLeft,
  ChevronRight,
  Clock,
  CheckCircle2,
  XCircle,
  AlertCircle,
  Eye,
} from 'lucide-react'
import { cn } from '../../lib/utils'
import { getAdminTaskLogs, getAdminTaskLogDetail, type AdminTaskLog } from '../../api/admin'
import { UserSelect } from '../../components/admin/UserSelect'
import { CopyButton } from '../../components/CopyButton'

const STATUS_CONFIG: Record<string, { label: string; className: string; icon: typeof Clock }> = {
  submitted: { label: '已提交', className: 'bg-blue-500/10 text-blue-600 dark:text-blue-400', icon: Clock },
  processing: { label: '处理中', className: 'bg-amber-500/10 text-amber-600 dark:text-amber-400', icon: Loader2 },
  completed: { label: '已完成', className: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400', icon: CheckCircle2 },
  failed: { label: '失败', className: 'bg-red-500/10 text-red-600 dark:text-red-400', icon: XCircle },
  cancelled: { label: '已取消', className: 'bg-gray-500/10 text-gray-600 dark:text-gray-400', icon: AlertCircle },
  call_fail: { label: '调用失败', className: 'bg-red-500/10 text-red-600 dark:text-red-400', icon: XCircle },
}

const STATUS_OPTIONS = [
  { value: '', label: '全部状态' },
  { value: 'submitted', label: '已提交' },
  { value: 'processing', label: '处理中' },
  { value: 'completed', label: '已完成' },
  { value: 'failed', label: '失败' },
  { value: 'cancelled', label: '已取消' },
  { value: 'call_fail', label: '调用失败' },
]

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

export function AdminTaskLogs() {
  const [tasks, setTasks] = useState<AdminTaskLog[]>([])
  const [loading, setLoading] = useState(true)
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize] = useState(20)
  const [userFilter, setUserFilter] = useState(0)
  const [statusFilter, setStatusFilter] = useState('')
  const [detailOpen, setDetailOpen] = useState(false)
  const [detailTask, setDetailTask] = useState<AdminTaskLog | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)

  const loadTasks = useCallback(async () => {
    setLoading(true)
    try {
      const res = await getAdminTaskLogs({
        page,
        page_size: pageSize,
        status: statusFilter || undefined,
        ...(userFilter ? { user_id: userFilter } : {}),
      })
      if (res.success) {
        setTasks(res.data.items || [])
        setTotal(res.data.total || 0)
      }
    } catch { /* ignore */ }
    finally { setLoading(false) }
  }, [page, pageSize, userFilter, statusFilter])

  useEffect(() => { loadTasks() }, [loadTasks])

  const handleViewDetail = async (task: AdminTaskLog) => {
    setDetailLoading(true)
    setDetailOpen(true)
    try {
      const res = await getAdminTaskLogDetail(task.id)
      if (res.success) {
        setDetailTask(res.data)
      }
    } catch { /* ignore */ }
    finally { setDetailLoading(false) }
  }

  const totalPages = Math.ceil(total / pageSize)

  return (
    <div className='space-y-6'>
      <div>
        <h2 className='text-2xl font-bold tracking-tight'>任务日志</h2>
        <p className='text-muted-foreground mt-1 text-sm'>查看全部用户的任务执行记录</p>
      </div>

      {/* 筛选 */}
      <div className='flex flex-wrap items-center gap-3'>
        <UserSelect value={userFilter} onChange={(id) => { setUserFilter(id); setPage(1) }} />
        <select
          value={statusFilter}
          onChange={(e) => { setStatusFilter(e.target.value); setPage(1) }}
          className='border-border/60 bg-background focus-visible:ring-ring flex h-9 rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
        >
          {STATUS_OPTIONS.map((opt) => (
            <option key={opt.value} value={opt.value}>{opt.label}</option>
          ))}
        </select>
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
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>用户</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>任务ID</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>状态</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>积分消耗</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>创建时间</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>更新时间</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>操作</th>
              </tr>
            </thead>
            <tbody className='divide-border/40 divide-y'>
              {loading ? (
                <tr><td colSpan={8} className='px-4 py-12 text-center'><Loader2 className='text-muted-foreground mx-auto size-6 animate-spin' /></td></tr>
              ) : tasks.length === 0 ? (
                <tr><td colSpan={8} className='px-4 py-12 text-center'><p className='text-muted-foreground text-sm'>暂无任务记录</p></td></tr>
              ) : tasks.map((task) => {
                const statusConf = STATUS_CONFIG[task.status] || STATUS_CONFIG.submitted
                const StatusIcon = statusConf.icon
                return (
                  <tr key={task.id} className='hover:bg-muted/20 transition-colors'>
                    <td className='px-4 py-3'><span className='text-muted-foreground font-mono text-xs'>{task.id}</span></td>
                    <td className='px-4 py-3'><span className='font-medium'>{task.username || '-'}</span></td>
                    <td className='px-4 py-3'>
                      <div className='flex items-center gap-1'>
                        <span className='font-mono text-xs break-all'>{task.task_id}</span>
                        <CopyButton text={task.task_id} />
                      </div>
                    </td>
                    <td className='px-4 py-3'>
                      <span className={cn('inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-medium', statusConf.className)}>
                        <StatusIcon className='size-3' />
                        {statusConf.label}
                      </span>
                    </td>
                    <td className='px-4 py-3'>
                      <div className='flex items-center gap-1'>
                        <span className='text-xs font-medium'>{task.credits}</span>
                        {task.credits_refunded && (
                          <span className='text-emerald-600 dark:text-emerald-400 text-[10px]'>已退还</span>
                        )}
                      </div>
                    </td>
                    <td className='px-4 py-3'><span className='text-muted-foreground text-xs'>{formatTime(task.created_at)}</span></td>
                    <td className='px-4 py-3'><span className='text-muted-foreground text-xs'>{formatTime(task.updated_at)}</span></td>
                    <td className='px-4 py-3'>
                      <button
                        onClick={() => handleViewDetail(task)}
                        className='hover:bg-muted inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs font-medium transition-colors'
                      >
                        <Eye className='size-3.5' />
                        查看
                      </button>
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

      {/* 详情弹框 */}
      {detailOpen && (
        <div className='fixed inset-0 z-[100] flex items-center justify-center p-4'>
          <div className='bg-background/80 fixed inset-0 backdrop-blur-sm' onClick={() => setDetailOpen(false)} />
          <div className='bg-background border-border/60 relative z-10 w-full max-w-2xl rounded-xl border shadow-lg'>
            <div className='flex items-center justify-between border-b border-border/40 px-6 py-4'>
              <h2 className='text-lg font-semibold'>任务详情</h2>
              <button onClick={() => setDetailOpen(false)} className='hover:bg-muted rounded-lg p-1.5 transition-colors'>
                <span className='text-muted-foreground text-lg'>×</span>
              </button>
            </div>
            <div className='max-h-[70vh] overflow-auto p-6'>
              {detailLoading ? (
                <div className='flex items-center justify-center py-12'>
                  <Loader2 className='text-muted-foreground size-6 animate-spin' />
                </div>
              ) : detailTask ? (
                <div className='space-y-4'>
                  <div className='grid grid-cols-2 gap-4'>
                    <div>
                      <label className='text-muted-foreground text-xs font-medium'>用户</label>
                      <p className='mt-1 text-sm'>{detailTask.username || '-'}</p>
                    </div>
                    <div>
                      <label className='text-muted-foreground text-xs font-medium'>任务ID</label>
                      <p className='mt-1 font-mono text-sm'>{detailTask.task_id}</p>
                    </div>
                    <div>
                      <label className='text-muted-foreground text-xs font-medium'>状态</label>
                      <div className='mt-1'>
                        {(() => {
                          const statusConf = STATUS_CONFIG[detailTask.status] || STATUS_CONFIG.submitted
                          const StatusIcon = statusConf.icon
                          return (
                            <span className={cn('inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-medium', statusConf.className)}>
                              <StatusIcon className='size-3' />
                              {statusConf.label}
                            </span>
                          )
                        })()}
                      </div>
                    </div>
                    <div>
                      <label className='text-muted-foreground text-xs font-medium'>积分消耗</label>
                      <p className='mt-1 text-sm'>
                        {detailTask.credits}
                        {detailTask.credits_refunded && (
                          <span className='text-emerald-600 dark:text-emerald-400 ml-2 text-xs'>已退还</span>
                        )}
                      </p>
                    </div>
                    <div>
                      <label className='text-muted-foreground text-xs font-medium'>创建时间</label>
                      <p className='mt-1 text-sm'>{formatTime(detailTask.created_at)}</p>
                    </div>
                    <div>
                      <label className='text-muted-foreground text-xs font-medium'>更新时间</label>
                      <p className='mt-1 text-sm'>{formatTime(detailTask.updated_at)}</p>
                    </div>
                  </div>

                  {detailTask.request_body && (
                    <div>
                      <label className='text-muted-foreground text-xs font-medium'>请求体</label>
                      <pre className='bg-muted/50 mt-1 max-h-40 overflow-auto rounded-lg p-3 text-xs break-all whitespace-pre-wrap'>
                        {detailTask.request_body}
                      </pre>
                    </div>
                  )}

                  {detailTask.query_response && (
                    <div>
                      <label className='text-muted-foreground text-xs font-medium'>查询响应</label>
                      <pre className='bg-muted/50 mt-1 max-h-40 overflow-auto rounded-lg p-3 text-xs'>
                        {detailTask.query_response}
                      </pre>
                    </div>
                  )}
                </div>
              ) : (
                <p className='text-muted-foreground text-center text-sm'>加载失败</p>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
