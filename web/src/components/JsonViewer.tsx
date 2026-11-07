import { useMemo, useState, type ReactNode } from 'react'
import { ClipboardCopy, WrapText } from 'lucide-react'
import { cn } from '../lib/utils'
import { CopyButton } from './CopyButton'

// 单次最多渲染的字符数。request_body 上限 1MB，全量格式化后直接塞进 DOM
// 会卡住页面；展示截断，复制仍然拿完整内容。
const MAX_DISPLAY_CHARS = 200 * 1024
// 超过这个长度的字符串默认折叠（base64、超长签名 URL 等）
const LONG_STRING_CHARS = 400
const STORED_TRUNCATED_MARKER = '...[truncated]'

const TOKEN_PATTERN =
  /("(?:\\.|[^"\\])*")(\s*:)?|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)|\b(true|false|null)\b/g

const KEY_CLASS = 'text-sky-600 dark:text-sky-400'
const STRING_CLASS = 'text-emerald-600 dark:text-emerald-400'
const NUMBER_CLASS = 'text-amber-600 dark:text-amber-400'
const KEYWORD_CLASS = 'text-purple-600 dark:text-purple-400'

// 超长字符串默认只显示摘要，点击后展开。
// 展开是用户主动行为，因此允许渲染完整内容。
function LongString({ value }: { value: string }) {
  const [expanded, setExpanded] = useState(false)

  if (expanded || value.length <= LONG_STRING_CHARS) {
    return <span className={STRING_CLASS}>{value}</span>
  }

  return (
    <button
      type='button'
      onClick={() => setExpanded(true)}
      title='点击展开完整字符串'
      className={cn(
        STRING_CLASS,
        'cursor-pointer text-left underline decoration-dotted underline-offset-2 hover:decoration-solid'
      )}
    >
      {value.slice(0, 120)}…（共 {value.length} 个字符，点击展开）
    </button>
  )
}

// 极简 JSON 着色：按行做 token 拆分，不引入高亮库。
// pretty print 后字符串内部不会有真实换行，因此按行处理是安全的。
function renderLine(line: string): ReactNode[] {
  const nodes: ReactNode[] = []
  TOKEN_PATTERN.lastIndex = 0

  let cursor = 0
  let key = 0
  let match: RegExpExecArray | null
  while ((match = TOKEN_PATTERN.exec(line)) !== null) {
    if (match.index > cursor) nodes.push(line.slice(cursor, match.index))

    const [token, str, colon, num, keyword] = match
    if (str !== undefined) {
      if (colon !== undefined) {
        nodes.push(<span key={key++} className={KEY_CLASS}>{str}</span>)
        nodes.push(colon)
      } else if (str.length > LONG_STRING_CHARS) {
        nodes.push(<LongString key={key++} value={str} />)
      } else {
        nodes.push(<span key={key++} className={STRING_CLASS}>{str}</span>)
      }
    } else if (num !== undefined) {
      nodes.push(<span key={key++} className={NUMBER_CLASS}>{num}</span>)
    } else {
      nodes.push(<span key={key++} className={KEYWORD_CLASS}>{keyword}</span>)
    }

    cursor = match.index + token.length
  }
  if (cursor < line.length) nodes.push(line.slice(cursor))

  return nodes
}

interface JsonViewerProps {
  value?: string
  emptyText?: string
  // 外层间距
  className?: string
  // 内容滚动区的尺寸约束（如 max-h-[45vh]）
  bodyClassName?: string
  // 提供时在右上角按钮组里显示「复制 Prompt」
  prompt?: string | null
}

export function JsonViewer({
  value,
  emptyText = '暂无记录',
  className,
  bodyClassName,
  prompt,
}: JsonViewerProps) {
  const [wrap, setWrap] = useState(false)
  const raw = value ?? ''

  const formatted = useMemo(() => {
    const text = raw.trim()
    if (!text) return { text: '', isJson: false }
    try {
      return { text: JSON.stringify(JSON.parse(text), null, 2), isJson: true }
    } catch {
      // 非 JSON / 写入时被截断：按原文展示，不能让整个详情弹框报错
      return { text: raw, isJson: false }
    }
  }, [raw])

  const storedTruncated = raw.includes(STORED_TRUNCATED_MARKER)

  if (raw.trim() === '') {
    return (
      <div
        className={cn(
          'border-border/50 bg-muted/30 text-muted-foreground rounded-lg border px-3 py-6 text-center text-xs',
          className
        )}
      >
        {emptyText}
      </div>
    )
  }

  const displayTruncated = formatted.text.length > MAX_DISPLAY_CHARS
  const shown = displayTruncated ? formatted.text.slice(0, MAX_DISPLAY_CHARS) : formatted.text
  const lines = shown.split('\n')
  const copyText = formatted.isJson ? formatted.text : raw

  const showStatus = storedTruncated || !formatted.isJson || displayTruncated

  return (
    <div className={cn('relative', className)}>
      <div className={cn('border-border/50 bg-muted/30 overflow-auto rounded-lg border', bodyClassName)}>
        <div className={cn('p-3 pt-10 font-mono text-xs leading-relaxed', wrap ? 'w-full' : 'w-max min-w-full')}>
          {lines.map((line, index) => (
            <div key={index} className='flex'>
              <span className='text-muted-foreground/60 w-10 shrink-0 pr-3 text-right select-none'>
                {index + 1}
              </span>
              <span
                className={cn(
                  'flex-1',
                  wrap ? 'whitespace-pre-wrap break-all' : 'min-w-max whitespace-pre'
                )}
              >
                {renderLine(line)}
              </span>
            </div>
          ))}
        </div>
      </div>

      {/* 按钮浮在内容区右上角，不再单独占一行，避免标签和 JSON 之间出现空行 */}
      <div className='bg-background/90 border-border/50 absolute top-1.5 right-1.5 flex items-center gap-1 rounded-md border px-1 py-0.5 shadow-sm backdrop-blur'>
        <button
          type='button'
          onClick={() => setWrap((v) => !v)}
          title={wrap ? '关闭自动换行' : '开启自动换行'}
          className={cn(
            'hover:bg-muted rounded p-1 transition-colors',
            wrap ? 'text-foreground' : 'text-muted-foreground'
          )}
        >
          <WrapText className='size-3.5' />
        </button>
        <CopyButton text={copyText} title='复制 JSON' />
        {prompt && <CopyButton text={prompt} icon={ClipboardCopy} title='复制 Prompt' />}
      </div>

      {showStatus && (
        <div className='text-muted-foreground mt-1 flex flex-wrap items-center gap-2 text-[11px]'>
          {storedTruncated ? (
            <span className='rounded bg-amber-500/10 px-1.5 py-0.5 text-amber-600 dark:text-amber-400'>
              写入时已截断
            </span>
          ) : !formatted.isJson ? (
            <span className='bg-muted rounded px-1.5 py-0.5'>非 JSON</span>
          ) : null}
          {displayTruncated && <span className='text-amber-600 dark:text-amber-400'>仅展示前 20 万字符</span>}
        </div>
      )}

      {storedTruncated && (
        <p className='mt-1 text-[11px] text-amber-600 dark:text-amber-400'>
          原始内容在写入时已被截断，无法完整解析；复制得到的是已落库的部分。
        </p>
      )}
    </div>
  )
}
