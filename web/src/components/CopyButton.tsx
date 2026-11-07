import { useState } from 'react'
import { Copy, Check, type LucideIcon } from 'lucide-react'
import { cn } from '../lib/utils'

interface CopyButtonProps {
  text: string
  className?: string
  // 用不同图标区分复制对象，默认是通用复制图标
  icon?: LucideIcon
  // 无文字按钮通过 tooltip 说明复制对象
  title?: string
}

// 复制按钮（点击复制，短暂显示对勾）
export function CopyButton({ text, className, icon, title = '复制' }: CopyButtonProps) {
  const [copied, setCopied] = useState(false)
  const Icon = icon ?? Copy

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text)
    } catch {
      // 兼容非安全上下文（如 http）
      const input = document.createElement('input')
      input.value = text
      document.body.appendChild(input)
      input.select()
      document.execCommand('copy')
      document.body.removeChild(input)
    }
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }

  return (
    <button
      type='button'
      onClick={copy}
      title={title}
      className={cn('hover:bg-muted inline-flex shrink-0 items-center rounded p-1 transition-colors', className)}
    >
      {copied ? <Check className='text-emerald-500 size-3.5' /> : <Icon className='text-muted-foreground size-3.5' />}
    </button>
  )
}
