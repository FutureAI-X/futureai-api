import { cn } from '../../lib/utils'
import { CREDIT_DECIMALS, validCreditInput } from '../../lib/credits'

interface CreditAmountInputProps {
  value: string
  onChange: (value: string) => void
  placeholder?: string
  /** 外层容器类名（宽度、flex 等布局由调用方决定） */
  className?: string
  onKeyDown?: (e: React.KeyboardEvent<HTMLInputElement>) => void
}

/**
 * 积分数量输入框。
 *
 * 存在的理由：全站有 5 个积分输入框（基础积分、每张参考图积分、每个参数组合的积分、
 * 管理员调整积分），逐个写校验必然飘——规则配置页曾经是「随便敲、点保存才在表单底部
 * 报错」，而调整对话框是即时标红，同一个约束两种表现。约束只在这里写一遍。
 *
 * 用 text + inputMode 而不是 type=number：number 输入框会把非法内容直接吞成空串，
 * 校验拿不到用户实际输入了什么（1.2.3 会变成 ""），移动端也弹不出小数键盘。
 */
export function CreditAmountInput({
  value,
  onChange,
  placeholder,
  className,
  onKeyDown,
}: CreditAmountInputProps) {
  // 只对「填了内容但格式不对」报错：空值不算格式错误，
  // 是否必填、是否必须大于 0 由提交时的校验统一判断。
  const malformed = value.trim() !== '' && !validCreditInput(value)

  return (
    <div className={cn('flex flex-col gap-1', className)}>
      <input
        type='text'
        inputMode='decimal'
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={onKeyDown}
        placeholder={placeholder ?? `最多 ${CREDIT_DECIMALS} 位小数`}
        aria-invalid={malformed}
        className={cn(
          'border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none',
          malformed && 'border-destructive focus-visible:ring-destructive'
        )}
      />
      {malformed && (
        <p className='text-destructive text-xs'>最多 {CREDIT_DECIMALS} 位小数</p>
      )}
    </div>
  )
}
