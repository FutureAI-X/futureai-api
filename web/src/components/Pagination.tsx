import { ChevronLeft, ChevronRight } from 'lucide-react'
import { cn } from '../lib/utils'

const PAGE_SIZE_OPTIONS = [10, 20, 50]

interface PaginationProps {
  page: number
  pageSize: number
  total: number
  onPageChange: (page: number) => void
  onPageSizeChange: (size: number) => void
}

// 列表页统一分页栏：固定在表格卡片底部，左侧总数、右侧页码
export function Pagination({
  page,
  pageSize,
  total,
  onPageChange,
  onPageSizeChange,
}: PaginationProps) {
  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  return (
    <div className='border-border/40 flex shrink-0 items-center justify-between border-t px-4 py-3'>
      <div className='text-muted-foreground text-sm'>
        共 <span className='text-foreground font-medium'>{total}</span> 条记录
      </div>
      <div className='flex items-center gap-2'>
        <select
          value={pageSize}
          onChange={(e) => { onPageSizeChange(Number(e.target.value)); onPageChange(1) }}
          className='border-border/60 bg-background h-8 rounded-lg border px-2 text-xs'
        >
          {PAGE_SIZE_OPTIONS.map((size) => (
            <option key={size} value={size}>{size} 条/页</option>
          ))}
        </select>

        <button
          onClick={() => onPageChange(Math.max(1, page - 1))}
          disabled={page <= 1}
          className='border-border/60 hover:bg-muted inline-flex size-8 items-center justify-center rounded-lg border transition-colors disabled:opacity-50'
        >
          <ChevronLeft className='size-4' />
        </button>

        <div className='flex items-center gap-1'>
          {Array.from({ length: Math.min(5, totalPages) }, (_, i) => {
            let pageNum: number
            if (totalPages <= 5) {
              pageNum = i + 1
            } else if (page <= 3) {
              pageNum = i + 1
            } else if (page >= totalPages - 2) {
              pageNum = totalPages - 4 + i
            } else {
              pageNum = page - 2 + i
            }
            return (
              <button
                key={pageNum}
                onClick={() => onPageChange(pageNum)}
                className={cn(
                  'inline-flex size-8 items-center justify-center rounded-lg text-sm font-medium transition-colors',
                  page === pageNum
                    ? 'bg-primary text-primary-foreground'
                    : 'hover:bg-muted'
                )}
              >
                {pageNum}
              </button>
            )
          })}
        </div>

        <button
          onClick={() => onPageChange(Math.min(totalPages, page + 1))}
          disabled={page >= totalPages}
          className='border-border/60 hover:bg-muted inline-flex size-8 items-center justify-center rounded-lg border transition-colors disabled:opacity-50'
        >
          <ChevronRight className='size-4' />
        </button>
      </div>
    </div>
  )
}
