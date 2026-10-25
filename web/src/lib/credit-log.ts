import { Minus, Plus, SlidersHorizontal } from 'lucide-react'
import { formatCredits } from './credits'

export interface CreditLogLike {
  type: string
  credits: number
  remark?: string
}

// 积分日志类型的展示配置
const TYPE_CONFIG: Record<string, { label: string; className: string; icon: typeof Minus }> = {
  deduct: { label: '扣除', className: 'text-red-600 dark:text-red-400', icon: Minus },
  refund: { label: '退还', className: 'text-emerald-600 dark:text-emerald-400', icon: Plus },
  adjust: { label: '调整', className: 'text-amber-600 dark:text-amber-400', icon: SlidersHorizontal },
}

// 未知类型不能回退成「扣除」：那会把方向也一起说反
export function creditLogType(type: string) {
  return TYPE_CONFIG[type] ?? {
    label: type || '未知',
    className: 'text-muted-foreground',
    icon: SlidersHorizontal,
  }
}

// 变动方向：正数=增加，负数=减少，0=方向未知。
//
// credit_logs.credits 只存绝对值，方向由 type 与备注表达：
// deduct/refund 由 type 决定；adjust 从备注里的「增加/扣减/本次减少…」判断。
// 历史覆盖记录（「管理员覆盖积分为 N」）当时没有保存方向，只能返回 0，不能臆断正负。
export function creditLogDelta(log: CreditLogLike): number {
  const amount = Math.abs(log.credits)
  if (log.type === 'refund') return amount
  if (log.type === 'deduct') return -amount

  const remark = log.remark || ''
  if (remark.startsWith('管理员增加积分') || remark.includes('本次增加')) return amount
  if (remark.startsWith('管理员扣减积分') || remark.includes('本次减少')) return -amount
  return 0
}

// 变动数额的颜色跟随方向，而不是类型
export function creditLogDeltaClass(delta: number): string {
  if (delta > 0) return 'text-emerald-600 dark:text-emerald-400'
  if (delta < 0) return 'text-red-600 dark:text-red-400'
  return 'text-amber-600 dark:text-amber-400'
}

// 带符号的变动数额；方向未知时用 ± 明确标出「只记录了数额」
export function formatCreditLogDelta(log: CreditLogLike): string {
  const delta = creditLogDelta(log)
  const sign = delta > 0 ? '+' : delta < 0 ? '-' : '±'
  return `${sign}${formatCredits(Math.abs(log.credits))}`
}
