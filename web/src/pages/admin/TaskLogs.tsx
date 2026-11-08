import { useState, useEffect, useCallback } from 'react'
import {
  Loader2,
  CheckCircle2,
  XCircle,
  Eye,
} from 'lucide-react'
import { cn } from '../../lib/utils'
import { getAdminTaskLogs, getAdminTaskLogDetail, type AdminTaskLog } from '../../api/admin'
import { UserSelect } from '../../components/admin/UserSelect'
import { CopyButton } from '../../components/CopyButton'
import { TaskDetailDialog } from '../../components/TaskDetailDialog'
import { formatCredits } from '../../lib/credits'
import { formatDuration, formatTime } from '../../lib/time'
import { Pagination } from '../../components/Pagination'

const STATUS_CONFIG: Record<string, { label: string; className: string; icon: typeof Loader2 }> = {
  processing: { label: '处理中', className: 'bg-amber-500/10 text-amber-600 dark:text-amber-400', icon: Loader2 },
  completed: { label: '已完成', className: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400', icon: CheckCircle2 },
  failed: { label: '失败', className: 'bg-red-500/10 text-red-600 dark:text-red-400', icon: XCircle },
}

const STATUS_OPTIONS = [
  { value: '', label: '全部状态' },
  { value: 'processing', label: '处理中' },
  { value: 'completed', label: '已完成' },
  { value: 'failed', label: '失败' },
]

export function AdminTaskLogs() {
  const [tasks, setTasks] = useState<AdminTaskLog[]>([])
  const [loading, setLoading] = useState(true)
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
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
    setDetailTask(null)
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

  return (
    <div data-page-fill className='flex h-full min-h-0 flex-col gap-6'>
      <div className='shrink-0'>
        <h2 className='text-2xl font-bold tracking-tight'>任务日志</h2>
        <p className='text-muted-foreground mt-1 text-sm'>查看全部用户的任务执行记录</p>
      </div>

      {/* 筛选 */}
      <div className='flex shrink-0 flex-wrap items-center gap-3'>
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
      </div>

      {/* 表格 */}
      <div className='border-border/60 flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border'>
        <div className='min-h-0 flex-1 overflow-auto'>
          <table className='w-full text-sm'>
            <thead className='bg-muted sticky top-0 z-10'>
              <tr className='bg-muted/30 border-border/40 border-b'>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>ID</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>用户</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>任务ID</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>模型</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>状态</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>积分消耗</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>耗时</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>创建时间</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>更新时间</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>操作</th>
              </tr>
            </thead>
            <tbody className='divide-border/40 divide-y'>
              {loading ? (
                <tr><td colSpan={10} className='px-4 py-12 text-center'><Loader2 className='text-muted-foreground mx-auto size-6 animate-spin' /></td></tr>
              ) : tasks.length === 0 ? (
                <tr><td colSpan={10} className='px-4 py-12 text-center'><p className='text-muted-foreground text-sm'>暂无任务记录</p></td></tr>
              ) : tasks.map((task) => {
                const statusConf = STATUS_CONFIG[task.status] || STATUS_CONFIG.processing
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
                    <td className='px-4 py-3'><span className='text-xs'>{task.model_name || '-'}</span></td>
                    <td className='px-4 py-3'>
                      <span className={cn('inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-medium', statusConf.className)}>
                        <StatusIcon className='size-3' />
                        {statusConf.label}
                      </span>
                    </td>
                    <td className='px-4 py-3'>
                      <div className='flex items-center gap-1'>
                        <span className='text-xs font-medium'>{formatCredits(task.credits)}</span>
                        {task.credits_refunded && (
                          <span className='text-emerald-600 dark:text-emerald-400 text-[10px]'>已退还</span>
                        )}
                      </div>
                    </td>
                    <td className='px-4 py-3'><span className='text-muted-foreground text-xs'>{formatDuration(task.duration_seconds)}</span></td>
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
        <Pagination
          page={page}
          pageSize={pageSize}
          total={total}
          onPageChange={setPage}
          onPageSizeChange={setPageSize}
        />
      </div>

      {/* 详情弹框 */}
      <TaskDetailDialog
        open={detailOpen}
        loading={detailLoading}
        task={detailTask}
        onClose={() => setDetailOpen(false)}
        showUsername
        showRawQueryResponse
      />
    </div>
  )
}
