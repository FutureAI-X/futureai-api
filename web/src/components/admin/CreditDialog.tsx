import { useState, useEffect, useRef } from 'react'
import { Loader2 } from 'lucide-react'
import { cn } from '../../lib/utils'
import { formatCredits, validCreditInput } from '../../lib/credits'
import { CreditAmountInput } from './CreditAmountInput'

type CreditMode = 'add' | 'subtract' | 'override'

interface CreditDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  username: string
  currentCredits: number
  loading?: boolean
  onConfirm: (mode: CreditMode, value: number) => void
}

const MODES: { key: CreditMode; label: string }[] = [
  { key: 'add', label: '添加' },
  { key: 'subtract', label: '减少' },
  { key: 'override', label: '覆盖' },
]

export function CreditDialog({
  open,
  onOpenChange,
  username,
  currentCredits,
  loading = false,
  onConfirm,
}: CreditDialogProps) {
  const [mode, setMode] = useState<CreditMode>('add')
  const [amount, setAmount] = useState('')
  const overlayRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (open) {
      setMode('add')
      setAmount('')
    }
  }, [open])

  useEffect(() => {
    if (open) {
      document.body.style.overflow = 'hidden'
    } else {
      document.body.style.overflow = ''
    }
    return () => { document.body.style.overflow = '' }
  }, [open])

  useEffect(() => {
    const handleEsc = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && open) onOpenChange(false)
    }
    document.addEventListener('keydown', handleEsc)
    return () => document.removeEventListener('keydown', handleEsc)
  }, [open, onOpenChange])

  const amountValue = parseFloat(amount) || 0

  // 位数与后端对齐：格式错误的输入由 CreditAmountInput 即时标红，
  // 这里只是提交闸门。覆盖模式允许填 0（表示清零），添加/减少必须为正。
  const canConfirm = validCreditInput(amount) && (mode === 'override' || amountValue > 0)

  const getPreview = () => {
    switch (mode) {
      case 'add':
        return `当前积分: ${formatCredits(currentCredits)}  + ${formatCredits(amountValue)} = ${formatCredits(currentCredits + amountValue)}`
      case 'subtract':
        return `当前积分: ${formatCredits(currentCredits)}  - ${formatCredits(amountValue)} = ${formatCredits(Math.max(0, currentCredits - amountValue))}`
      case 'override':
        return `当前积分: ${formatCredits(currentCredits)} → ${formatCredits(amountValue)}`
    }
  }

  const handleConfirm = () => {
    if (!canConfirm) return
    onConfirm(mode, amountValue)
  }

  if (!open) return null

  return (
    <div className='fixed inset-0 z-[100] flex items-center justify-center'>
      {/* Overlay */}
      <div
        ref={overlayRef}
        className='bg-background/80 fixed inset-0 backdrop-blur-sm'
        onClick={() => onOpenChange(false)}
      />
      {/* Dialog */}
      <div className='bg-background border-border/60 relative z-10 w-full max-w-md rounded-xl border p-6 shadow-lg'>
        <h2 className='text-lg font-semibold'>积分调整</h2>
        <p className='text-muted-foreground mt-1 text-sm'>
          为用户 <span className='text-foreground font-medium'>{username}</span> 调整积分
        </p>

        {/* 预览 */}
        <div className='bg-muted/30 mt-4 rounded-lg px-4 py-3 text-sm'>
          {getPreview()}
        </div>

        {/* 模式选择 */}
        <div className='mt-4 space-y-2'>
          <label className='text-sm font-medium'>调整模式</label>
          <div className='flex gap-2'>
            {MODES.map((m) => (
              <button
                key={m.key}
                type='button'
                onClick={() => { setMode(m.key); setAmount('') }}
                className={cn(
                  'inline-flex h-8 items-center justify-center rounded-lg border px-3 text-sm font-medium transition-colors',
                  mode === m.key
                    ? 'bg-primary text-primary-foreground border-primary'
                    : 'border-border/60 hover:bg-muted'
                )}
              >
                {m.label}
              </button>
            ))}
          </div>
        </div>

        {/* 数值输入 */}
        <div className='mt-4 space-y-2'>
          <label className='text-sm font-medium'>积分数量</label>
          <CreditAmountInput
            value={amount}
            onChange={setAmount}
            onKeyDown={(e) => { if (e.key === 'Enter') handleConfirm() }}
          />
        </div>

        {/* 按钮 */}
        <div className='mt-6 flex justify-end gap-3'>
          <button
            onClick={() => onOpenChange(false)}
            disabled={loading}
            className='border-border/60 hover:bg-muted inline-flex h-9 items-center justify-center rounded-lg border px-4 text-sm font-medium transition-colors'
          >
            取消
          </button>
          <button
            onClick={handleConfirm}
            disabled={loading || !canConfirm}
            className='bg-primary hover:bg-primary/90 inline-flex h-9 items-center justify-center gap-2 rounded-lg px-4 text-sm font-medium text-white transition-colors disabled:opacity-50'
          >
            {loading && <Loader2 className='size-4 animate-spin' />}
            {loading ? '处理中...' : '确认'}
          </button>
        </div>
      </div>
    </div>
  )
}
