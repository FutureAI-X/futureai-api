import { useEffect, useMemo, useRef } from 'react'
import {
  AlertCircle,
  CheckCircle2,
  ChevronDown,
  Clock,
  Loader2,
  X,
  XCircle,
} from 'lucide-react'
import { cn } from '../lib/utils'
import { formatCredits } from '../lib/credits'
import { formatDuration, formatTime } from '../lib/time'
import { extractPrompt, extractTaskResult } from '../lib/task-result'
import { CopyButton } from './CopyButton'
import { JsonViewer } from './JsonViewer'

// 两个任务日志页面共用的状态映射。含个人端历史状态值，避免旧记录显示空白。
const STATUS_CONFIG: Record<string, { label: string; className: string; icon: typeof Loader2 }> = {
  pending: { label: '待提交', className: 'bg-gray-500/10 text-gray-600 dark:text-gray-400', icon: Clock },
  submitted: { label: '已提交', className: 'bg-blue-500/10 text-blue-600 dark:text-blue-400', icon: Clock },
  processing: { label: '处理中', className: 'bg-amber-500/10 text-amber-600 dark:text-amber-400', icon: Loader2 },
  unknown: { label: '结果未确认', className: 'bg-amber-500/10 text-amber-600 dark:text-amber-400', icon: AlertCircle },
  completed: { label: '已完成', className: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400', icon: CheckCircle2 },
  failed: { label: '失败', className: 'bg-red-500/10 text-red-600 dark:text-red-400', icon: XCircle },
  cancelled: { label: '已取消', className: 'bg-gray-500/10 text-gray-600 dark:text-gray-400', icon: AlertCircle },
  call_fail: { label: '调用失败', className: 'bg-red-500/10 text-red-600 dark:text-red-400', icon: XCircle },
}

// 失败类终态：这些状态没有可交付的结果，结果地址统一显示「暂无」
const FAILED_STATUSES = new Set(['failed', 'cancelled', 'call_fail'])

export interface TaskDetailData {
  id: number
  task_id: string
  status: string
  credits: number
  credits_refunded: boolean
  duration_seconds?: number
  request_body?: string
  query_response?: string
  error_code?: string
  error_message?: string
  created_at: string
  updated_at: string
  username?: string
}

interface TaskDetailDialogProps {
  open: boolean
  loading: boolean
  task: TaskDetailData | null
  onClose: () => void
  // 管理员端展示任务归属用户
  showUsername?: boolean
  // 管理员端保留原始查询响应入口（默认收起）
  showRawQueryResponse?: boolean
  // 结果地址只对管理员展示。个人端不展示，避免把上游地址泄露给用户。
  showResultUrl?: boolean
}

function StatusBadge({ status }: { status: string }) {
  const conf = STATUS_CONFIG[status] ?? STATUS_CONFIG.processing
  const Icon = conf.icon
  return (
    <span className={cn('inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-medium', conf.className)}>
      <Icon className='size-3' />
      {conf.label}
    </span>
  )
}

// 字段区 + 请求体；结果地址按 showResultUrl 决定是否展示。
// 单独拆组件是为了让 extractTaskResult 的 useMemo 只在 task 存在时运行。
function TaskDetailContent({
  task,
  showUsername,
  showRawQueryResponse,
  showResultUrl,
}: {
  task: TaskDetailData
  showUsername: boolean
  showRawQueryResponse: boolean
  showResultUrl: boolean
}) {
  const result = useMemo(() => extractTaskResult(task.query_response), [task.query_response])
  const prompt = useMemo(() => extractPrompt(task.request_body), [task.request_body])
  const failed = FAILED_STATUSES.has(task.status)
  const resultNote = result.note && result.note.length > 300 ? `${result.note.slice(0, 300)}…` : result.note

  return (
    <div className='space-y-5'>
      <div className='grid grid-cols-1 gap-4 sm:grid-cols-2'>
        <div className='min-w-0'>
          <label className='text-muted-foreground text-xs font-medium'>任务ID</label>
          <div className='mt-1 flex min-w-0 items-start gap-1'>
            <p className='min-w-0 font-mono text-sm break-all'>{task.task_id}</p>
            <CopyButton text={task.task_id} />
          </div>
        </div>

        {showUsername && (
          <div className='min-w-0'>
            <label className='text-muted-foreground text-xs font-medium'>用户</label>
            <p className='mt-1 truncate text-sm'>{task.username || '-'}</p>
          </div>
        )}

        <div className='min-w-0'>
          <label className='text-muted-foreground text-xs font-medium'>状态</label>
          <div className='mt-1 flex flex-wrap items-center gap-x-2 gap-y-1'>
            <StatusBadge status={task.status} />
            {task.error_message && (
              <span className='text-xs break-words text-red-600 dark:text-red-400'>{task.error_message}</span>
            )}
          </div>
        </div>

        <div>
          <label className='text-muted-foreground text-xs font-medium'>积分消耗</label>
          <p className='mt-1 text-sm'>
            {formatCredits(task.credits)}
            {task.credits_refunded && (
              <span className='ml-2 text-xs text-emerald-600 dark:text-emerald-400'>已退还</span>
            )}
          </p>
        </div>

        <div>
          <label className='text-muted-foreground text-xs font-medium'>耗时</label>
          <p className='mt-1 text-sm'>{formatDuration(task.duration_seconds)}</p>
        </div>

        <div>
          <label className='text-muted-foreground text-xs font-medium'>创建时间</label>
          <p className='mt-1 text-sm'>{formatTime(task.created_at)}</p>
        </div>

        <div>
          <label className='text-muted-foreground text-xs font-medium'>更新时间</label>
          <p className='mt-1 text-sm'>{formatTime(task.updated_at)}</p>
        </div>
      </div>

      <div>
        <label className='text-muted-foreground text-xs font-medium'>请求体</label>
        <JsonViewer
          key={`${task.id}-request`}
          value={task.request_body}
          emptyText='暂无请求体记录'
          className='mt-1'
          bodyClassName='max-h-[45vh]'
          prompt={prompt}
        />
      </div>

      {showResultUrl && (
        <div>
          <label className='text-muted-foreground text-xs font-medium'>结果地址</label>
          {failed ? (
            <p className='text-muted-foreground mt-1 text-sm'>暂无</p>
          ) : result.url ? (
            <div className='mt-1 flex min-w-0 items-start gap-1'>
              <a
                href={result.url}
                target='_blank'
                rel='noreferrer noopener'
                className='text-primary min-w-0 font-mono text-xs break-all hover:underline'
              >
                {result.url}
              </a>
              <CopyButton text={result.url} />
            </div>
          ) : (
            <p className='text-muted-foreground mt-1 text-sm'>{resultNote || '暂无结果地址'}</p>
          )}
        </div>
      )}

      {showRawQueryResponse && task.query_response && (
        <details className='group border-border/50 rounded-lg border'>
          <summary className='text-muted-foreground hover:bg-muted/40 flex cursor-pointer list-none items-center justify-between rounded-lg px-3 py-2 text-xs font-medium select-none [&::-webkit-details-marker]:hidden'>
            <span>原始查询响应</span>
            <ChevronDown className='size-3.5 transition-transform group-open:rotate-180' />
          </summary>
          <div className='border-border/40 border-t p-3'>
            <JsonViewer
              key={`${task.id}-query`}
              value={task.query_response}
              emptyText='暂无查询响应'
              bodyClassName='max-h-[35vh]'
            />
          </div>
        </details>
      )}
    </div>
  )
}

export function TaskDetailDialog({
  open,
  loading,
  task,
  onClose,
  showUsername = false,
  showRawQueryResponse = false,
  showResultUrl = false,
}: TaskDetailDialogProps) {
  const panelRef = useRef<HTMLDivElement>(null)

  // 打开时把焦点移到面板，关闭时还原，避免 Tab 落到遮罩后面的列表上
  useEffect(() => {
    if (!open) return
    const previous = document.activeElement as HTMLElement | null
    panelRef.current?.focus()
    return () => previous?.focus?.()
  }, [open])

  // 锁定页面滚动。锁 documentElement 而不是 body：全局样式把滚动放在根元素上。
  useEffect(() => {
    if (!open) return
    const root = document.documentElement
    const scrollbarWidth = window.innerWidth - root.clientWidth
    const prevOverflow = root.style.overflow
    const prevPaddingRight = root.style.paddingRight

    root.style.overflow = 'hidden'
    if (scrollbarWidth > 0) root.style.paddingRight = `${scrollbarWidth}px`

    return () => {
      root.style.overflow = prevOverflow
      root.style.paddingRight = prevPaddingRight
    }
  }, [open])

  useEffect(() => {
    if (!open) return
    const handleEsc = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', handleEsc)
    return () => document.removeEventListener('keydown', handleEsc)
  }, [open, onClose])

  if (!open) return null

  return (
    <div className='fixed inset-0 z-[100] flex items-center justify-center p-4'>
      <div className='bg-background/80 fixed inset-0 backdrop-blur-sm' onClick={onClose} />

      <div
        ref={panelRef}
        tabIndex={-1}
        role='dialog'
        aria-modal='true'
        aria-labelledby='task-detail-title'
        className='bg-background border-border/60 relative z-10 flex max-h-[85dvh] w-[min(1100px,94vw)] flex-col rounded-xl border shadow-lg focus:outline-none'
      >
        <div className='border-border/40 flex shrink-0 items-center justify-between border-b px-5 py-4 sm:px-6'>
          <h2 id='task-detail-title' className='text-lg font-semibold'>任务详情</h2>
          <button
            type='button'
            onClick={onClose}
            className='text-muted-foreground hover:text-foreground hover:bg-muted rounded-lg p-1.5 transition-colors'
            title='关闭'
          >
            <X className='size-4' />
          </button>
        </div>

        <div className='min-h-0 flex-1 overflow-auto overscroll-contain px-5 py-5 sm:px-6'>
          {loading ? (
            <div className='flex items-center justify-center py-12'>
              <Loader2 className='text-muted-foreground size-6 animate-spin' />
            </div>
          ) : task ? (
            <TaskDetailContent
              task={task}
              showUsername={showUsername}
              showRawQueryResponse={showRawQueryResponse}
              showResultUrl={showResultUrl}
            />
          ) : (
            <p className='text-muted-foreground py-12 text-center text-sm'>加载失败</p>
          )}
        </div>
      </div>
    </div>
  )
}
