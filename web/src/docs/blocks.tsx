import { useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { AlertTriangle, ImageOff, Info, Sparkles } from 'lucide-react'
import { cn } from '../lib/utils'
import { CopyButton } from '../components/CopyButton'

// ── 文档正文的基础排版组件 ──
// 站点没有引入 @tailwindcss/typography，这里用一组小元件保证各篇文档的
// 字号、间距和暗色模式表现一致。

export function DocH2({ children }: { children: ReactNode }) {
  return (
    <h2 className='border-border/60 mt-10 mb-1 scroll-mt-24 border-b pb-2 text-lg font-semibold tracking-tight first:mt-0'>
      {children}
    </h2>
  )
}

export function DocH3({ children }: { children: ReactNode }) {
  return <h3 className='mt-6 text-sm font-semibold'>{children}</h3>
}

export function DocP({ children }: { children: ReactNode }) {
  return (
    <p className='text-muted-foreground mt-3 text-sm leading-7 first:mt-0'>
      {children}
    </p>
  )
}

export function DocUl({ children }: { children: ReactNode }) {
  return (
    <ul className='text-muted-foreground marker:text-muted-foreground/50 mt-3 list-disc space-y-1.5 pl-5 text-sm leading-7'>
      {children}
    </ul>
  )
}

export function DocLink({ to, children }: { to: string; children: ReactNode }) {
  return (
    <Link
      to={to}
      className='text-primary font-medium underline-offset-4 hover:underline'
    >
      {children}
    </Link>
  )
}

export function DocStrong({ children }: { children: ReactNode }) {
  return <strong className='text-foreground font-semibold'>{children}</strong>
}

// ── 步骤列表 ──
// 编号由调用方传入，和正文里的「第几步」保持一一对应。

export function Steps({ children }: { children: ReactNode }) {
  return <ol className='mt-5 space-y-5'>{children}</ol>
}

export function Step({
  n,
  title,
  children,
}: {
  n: number
  title: string
  children?: ReactNode
}) {
  return (
    <li className='flex gap-4'>
      <span className='border-border bg-muted/50 text-foreground flex size-7 shrink-0 items-center justify-center rounded-full border text-xs font-semibold tabular-nums'>
        {n}
      </span>
      <div className='min-w-0 flex-1 pt-0.5'>
        <h3 className='text-sm font-semibold'>{title}</h3>
        {children ? (
          <div className='text-muted-foreground mt-1.5 space-y-3 text-sm leading-7'>
            {children}
          </div>
        ) : null}
      </div>
    </li>
  )
}

// ── 提示块 ──

export function Callout({
  variant = 'info',
  children,
}: {
  variant?: 'info' | 'warning'
  children: ReactNode
}) {
  const Icon = variant === 'warning' ? AlertTriangle : Info
  return (
    <div
      className={cn(
        'mt-4 flex gap-3 rounded-xl border px-4 py-3 text-sm leading-6 first:mt-0',
        variant === 'warning'
          ? 'border-amber-500/25 bg-amber-500/5'
          : 'border-border/70 bg-muted/30'
      )}
    >
      <Icon
        className={cn(
          'mt-0.5 size-4 shrink-0',
          variant === 'warning' ? 'text-amber-500' : 'text-primary'
        )}
      />
      <div className='text-muted-foreground min-w-0'>{children}</div>
    </div>
  )
}

// ── 图片 ──
// 图片放在 web/public/docs/ 下。文件还不存在时（或路径写错）显示占位框，
// 不会在页面上留下破图；把截图按占位框里的文件名放进去即可自动生效。

export function DocImage({
  src,
  alt,
  hint,
}: {
  src: string
  alt: string
  hint?: string
}) {
  const [failed, setFailed] = useState(false)

  return (
    <figure className='border-border/70 bg-muted/20 mt-4 overflow-hidden rounded-xl border'>
      {failed ? (
        <div className='text-muted-foreground flex flex-col items-center justify-center gap-2 px-6 py-10 text-center'>
          <ImageOff className='size-5' />
          <p className='text-foreground text-xs font-medium'>截图待补充</p>
          {hint ? (
            <p className='max-w-sm text-[11px] leading-relaxed'>{hint}</p>
          ) : null}
          <code className='bg-muted/60 mt-1 rounded px-1.5 py-0.5 text-[10px]'>
            {src}
          </code>
        </div>
      ) : (
        <img
          src={src}
          alt={alt}
          loading='lazy'
          onError={() => setFailed(true)}
          className='block w-full'
        />
      )}
      <figcaption className='border-border/60 text-muted-foreground border-t px-4 py-2 text-[11px]'>
        {alt}
      </figcaption>
    </figure>
  )
}

// ── 即将上线的占位内容 ──

export function ComingSoon() {
  return (
    <div className='border-border/70 bg-muted/20 mt-6 flex flex-col items-center justify-center rounded-xl border px-6 py-20 text-center'>
      <Sparkles className='text-primary size-6' />
      <p className='mt-3 text-lg font-semibold'>即将上线！敬请期待！</p>
    </div>
  )
}

// ── 二维码 ──
// 联系方式统一用二维码展示。图片未放好时同样显示占位框。

export function QrCard({
  src,
  label,
  hint,
}: {
  src: string
  label: string
  hint?: string
}) {
  const [failed, setFailed] = useState(false)

  return (
    <figure className='border-border/70 bg-muted/20 rounded-xl border p-4'>
      <figcaption className='text-center text-sm font-semibold'>
        {label}
      </figcaption>
      <div className='bg-background border-border/60 mx-auto mt-3 aspect-square w-full max-w-56 overflow-hidden rounded-lg border'>
        {failed ? (
          <div className='text-muted-foreground flex size-full flex-col items-center justify-center gap-2 px-3 text-center'>
            <ImageOff className='size-5' />
            <span className='text-foreground text-xs font-medium'>
              二维码待补充
            </span>
            <code className='text-[9px] leading-tight break-all opacity-70'>
              {src}
            </code>
          </div>
        ) : (
          <img
            src={src}
            alt={`${label}二维码`}
            loading='lazy'
            onError={() => setFailed(true)}
            className='size-full object-contain'
          />
        )}
      </div>
      {hint ? (
        <p className='text-muted-foreground mt-3 text-center text-[11px] leading-relaxed'>
          {hint}
        </p>
      ) : null}
    </figure>
  )
}

// ── 可复制的单行代码 ──

export function CodeLine({ value }: { value: string }) {
  return (
    <div className='border-border/70 bg-muted/30 mt-3 flex items-center justify-between gap-3 rounded-lg border px-3 py-2.5'>
      <code className='font-mono text-xs sm:text-sm'>{value}</code>
      <CopyButton text={value} />
    </div>
  )
}
