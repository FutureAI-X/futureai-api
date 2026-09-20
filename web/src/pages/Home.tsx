import { useState, useEffect, useRef, useCallback, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { cn } from '../lib/utils'
import { Header } from '../components/Header'

/* ── Counter (from New API Stats) ── */
interface CounterProps {
  end: number
  suffix?: string
  prefix?: string
  duration?: number
  decimals?: number
}

function Counter(props: CounterProps) {
  const { end, suffix = '', prefix = '', duration = 1600, decimals = 0 } = props
  const ref = useRef<HTMLSpanElement>(null)
  const startedRef = useRef(false)

  const formatValue = useCallback(
    (v: number) =>
      decimals > 0 ? v.toFixed(decimals) : Math.round(v).toLocaleString(),
    [decimals]
  )

  const animate = useCallback(() => {
    const el = ref.current
    if (!el) return
    const start = performance.now()
    const step = (now: number) => {
      const progress = Math.min((now - start) / duration, 1)
      const eased = 1 - Math.pow(1 - progress, 3)
      el.textContent = `${prefix}${formatValue(eased * end)}${suffix}`
      if (progress < 1) requestAnimationFrame(step)
    }
    requestAnimationFrame(step)
  }, [end, duration, prefix, suffix, formatValue])

  useEffect(() => {
    const el = ref.current
    if (!el) return

    const mq = window.matchMedia('(prefers-reduced-motion: reduce)')
    if (mq.matches) {
      el.textContent = `${prefix}${formatValue(end)}${suffix}`
      return
    }

    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting && !startedRef.current) {
          startedRef.current = true
          animate()
          observer.unobserve(el)
        }
      },
      { threshold: 0.5 }
    )

    observer.observe(el)
    return () => observer.disconnect()
  }, [animate, end, prefix, suffix, formatValue])

  return (
    <span ref={ref} className='tabular-nums'>
      {prefix}0{suffix}
    </span>
  )
}

/* ── AnimateInView ── */
interface AnimateInViewProps {
  children: ReactNode
  className?: string
  delay?: number
  animation?: 'fade-up' | 'scale-in' | 'fade-in'
}

function AnimateInView(props: AnimateInViewProps) {
  const { children, className, delay = 0, animation = 'fade-up' } = props
  const ref = useRef<HTMLDivElement>(null)
  const [visible, setVisible] = useState(false)

  useEffect(() => {
    const el = ref.current
    if (!el) return

    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          setVisible(true)
          observer.unobserve(el)
        }
      },
      { threshold: 0.1 }
    )

    observer.observe(el)
    return () => observer.disconnect()
  }, [])

  const animClass =
    animation === 'scale-in'
      ? 'landing-animate-scale-in'
      : animation === 'fade-in'
        ? 'landing-animate-fade-in'
        : 'landing-animate-fade-up'

  return (
    <div
      ref={ref}
      className={cn(
        visible ? animClass : 'opacity-0',
        className
      )}
      style={visible ? { animationDelay: `${delay}ms` } : undefined}
    >
      {children}
    </div>
  )
}

/* ── HeroTerminalDemo (from New API) ── */
type AccentTone = 'emerald' | 'amber' | 'blue' | 'violet'

interface ApiDemoConfig {
  id: string
  label: string
  method: 'POST' | 'GET'
  endpoint: string
  headers: string[]
  request: string[]
  response: string[]
  responseHighlights: string[]
  tokens: number
  latency: number
  accent: AccentTone
}

const ACCENT_CLASSES: Record<AccentTone, { activeText: string; activeBorder: string; badge: string }> = {
  emerald: {
    activeText: 'text-emerald-600 dark:text-emerald-400',
    activeBorder: 'border-emerald-500 dark:border-emerald-400',
    badge: 'bg-emerald-500/10 text-emerald-600 dark:bg-emerald-400/10 dark:text-emerald-400',
  },
  amber: {
    activeText: 'text-amber-600 dark:text-amber-400',
    activeBorder: 'border-amber-500 dark:border-amber-400',
    badge: 'bg-amber-500/10 text-amber-600 dark:bg-amber-400/10 dark:text-amber-400',
  },
  blue: {
    activeText: 'text-blue-600 dark:text-blue-400',
    activeBorder: 'border-blue-500 dark:border-blue-400',
    badge: 'bg-blue-500/10 text-blue-600 dark:bg-blue-400/10 dark:text-blue-400',
  },
  violet: {
    activeText: 'text-violet-600 dark:text-violet-400',
    activeBorder: 'border-violet-500 dark:border-violet-400',
    badge: 'bg-violet-500/10 text-violet-600 dark:bg-violet-400/10 dark:text-violet-400',
  },
}

const API_DEMOS: ApiDemoConfig[] = [
  {
    id: 'gpt-chat',
    label: 'Chat',
    method: 'POST',
    endpoint: '/v1/chat/completions',
    headers: ['"Authorization: Bearer sk-••••"'],
    request: ['"model": "your-model",', '"messages": [', '  { "role": "user", "content": "..." }', ']'],
    response: ['{', '  "choices": [{ "message": { "content": <text> } }],', '  "usage": { "total_tokens": <tokens> }', '}'],
    responseHighlights: ['<text>', '<tokens>'],
    tokens: 27,
    latency: 142,
    accent: 'emerald',
  },
  {
    id: 'responses',
    label: 'Responses',
    method: 'POST',
    endpoint: '/v1/responses',
    headers: ['"Authorization: Bearer sk-••••"'],
    request: ['"model": "your-model",', '"input": "..."'],
    response: ['{', '  "output": [{ "type": "output_text", "text": <text> }],', '  "usage": { "total_tokens": <tokens> }', '}'],
    responseHighlights: ['<text>', '<tokens>'],
    tokens: 31,
    latency: 168,
    accent: 'amber',
  },
  {
    id: 'claude',
    label: 'Claude',
    method: 'POST',
    endpoint: '/v1/messages',
    headers: ['"x-api-key: sk-••••"', '"anthropic-version: 2023-06-01"'],
    request: ['"model": "your-model",', '"max_tokens": 1024,', '"messages": [', '  { "role": "user", "content": "..." }', ']'],
    response: ['{', '  "content": [{ "type": "text", "text": <text> }],', '  "usage": { "input_tokens": <in>, "output_tokens": <out> }', '}'],
    responseHighlights: ['<text>', '<in>', '<out>'],
    tokens: 29,
    latency: 156,
    accent: 'blue',
  },
  {
    id: 'gemini',
    label: 'Gemini',
    method: 'POST',
    endpoint: '/v1beta/models/{model}:generateContent',
    headers: ['"x-goog-api-key: sk-••••"'],
    request: ['"contents": [', '  { "role": "user",', '    "parts": [{ "text": "..." }] }', ']'],
    response: ['{', '  "candidates": [{ "content": { "parts": [{ "text": <text> }] } }],', '  "usageMetadata": { "totalTokenCount": <tokens> }', '}'],
    responseHighlights: ['<text>', '<tokens>'],
    tokens: 25,
    latency: 93,
    accent: 'violet',
  },
]

const CYCLE_INTERVAL = 4500
const TRANSITION_MS = 220
const STRING_RE = /"[^"]*"/g
const PLACEHOLDER_RE = /<[a-z]+>/gi

function truncateResponse(demo: ApiDemoConfig): string {
  const map: Record<string, string> = {
    'gpt-chat': 'Chat request routed.',
    responses: 'Response workflow ready.',
    claude: 'Claude message routed.',
    gemini: 'Gemini request served.',
  }
  return map[demo.id] ?? '...'
}

function tokenize(input: string): ReactNode {
  const segments: ReactNode[] = []
  let cursor = 0
  const matches = [...input.matchAll(STRING_RE)]

  matches.forEach((match, idx) => {
    const start = match.index ?? 0
    if (start > cursor) {
      segments.push(<Muted key={`m-${idx}`}>{input.slice(cursor, start)}</Muted>)
    }
    const text = match[0]
    const after = input.slice(start + text.length).trimStart()
    const isKey = after.startsWith(':')
    if (isKey) {
      segments.push(<Key key={`k-${idx}`}>{text}</Key>)
    } else {
      segments.push(<StringText key={`s-${idx}`}>{text}</StringText>)
    }
    cursor = start + text.length
  })

  if (cursor < input.length) {
    segments.push(<Muted key='tail'>{input.slice(cursor)}</Muted>)
  }

  return segments
}

function CodeLine(props: { children: ReactNode; indent?: number }) {
  return (
    <div className='break-words whitespace-pre-wrap'>
      {props.indent ? (
        <span aria-hidden className='inline-block' style={{ width: `${props.indent}ch` }} />
      ) : null}
      {props.children}
    </div>
  )
}

function Command(props: { children: ReactNode }) {
  return <span className='font-medium text-emerald-600 dark:text-emerald-400'>{props.children}</span>
}

function Flag(props: { children: ReactNode }) {
  return <span className='text-blue-600 dark:text-blue-400'>{props.children}</span>
}

function Key(props: { children: ReactNode }) {
  return <span className='text-sky-700 dark:text-sky-300'>{props.children}</span>
}

function StringText(props: { children: ReactNode }) {
  return <span className='text-amber-700 dark:text-amber-300'>{props.children}</span>
}

function NumberText(props: { children: ReactNode }) {
  return <span className='font-medium text-violet-600 dark:text-violet-300'>{props.children}</span>
}

function Muted(props: { children: ReactNode }) {
  return <span className='text-foreground/55'>{props.children}</span>
}

function Accent(props: { children: ReactNode; accent: AccentTone }) {
  const tone = ACCENT_CLASSES[props.accent]
  return <span className={cn('font-medium', tone.activeText)}>{props.children}</span>
}

function SectionLabel(props: { children: ReactNode }) {
  return (
    <span className='text-foreground/30 font-sans text-[10px] font-semibold tracking-[0.18em] uppercase'>
      {props.children}
    </span>
  )
}

function renderJsonLine(line: string): ReactNode {
  if (!line.trim()) return <Muted> </Muted>
  return tokenize(line)
}

function renderResponseLine(line: string, demo: ApiDemoConfig): ReactNode {
  if (!line.trim()) return <Muted> </Muted>

  const segments: ReactNode[] = []
  let cursor = 0
  const matches = [...line.matchAll(PLACEHOLDER_RE)]

  if (matches.length === 0) return tokenize(line)

  matches.forEach((match, idx) => {
    const start = match.index ?? 0
    if (start > cursor) {
      segments.push(<span key={`pre-${idx}`}>{tokenize(line.slice(cursor, start))}</span>)
    }
    const placeholder = match[0]
    if (placeholder === '<text>') {
      segments.push(<Accent key={`ph-${idx}`} accent={demo.accent}>{`"${truncateResponse(demo)}"`}</Accent>)
    } else if (placeholder === '<tokens>') {
      segments.push(<NumberText key={`ph-${idx}`}>{demo.tokens}</NumberText>)
    } else if (placeholder === '<in>') {
      segments.push(<NumberText key={`ph-${idx}`}>{Math.floor(demo.tokens * 0.4)}</NumberText>)
    } else if (placeholder === '<out>') {
      segments.push(<NumberText key={`ph-${idx}`}>{Math.ceil(demo.tokens * 0.6)}</NumberText>)
    } else {
      segments.push(<Muted key={`ph-${idx}`}>{placeholder}</Muted>)
    }
    cursor = start + placeholder.length
  })

  if (cursor < line.length) {
    segments.push(<span key='tail'>{tokenize(line.slice(cursor))}</span>)
  }

  return segments
}

function RequestBlock(props: { demo: ApiDemoConfig; transitioning: boolean }) {
  const { demo, transitioning } = props
  return (
    <div className='relative px-5 py-4'>
      <SectionLabel>Request</SectionLabel>
      <div className={cn('mt-2 transition-opacity duration-200', transitioning ? 'opacity-0' : 'opacity-100')}>
        <CodeLine>
          <Command>curl</Command> <Flag>-X</Flag> <Flag>POST</Flag>{' '}
          <StringText>&quot;{demo.endpoint}&quot;</StringText> <Muted>{'\\'}</Muted>
        </CodeLine>
        {demo.headers.map((header) => (
          <CodeLine key={header} indent={2}>
            <Flag>-H</Flag> <StringText>{header}</StringText> <Muted>{'\\'}</Muted>
          </CodeLine>
        ))}
        <CodeLine indent={2}>
          <Flag>-d</Flag> <StringText>&apos;{'{'}</StringText>
        </CodeLine>
        {demo.request.map((line, i) => (
          <CodeLine key={i} indent={4}>{renderJsonLine(line)}</CodeLine>
        ))}
        <CodeLine indent={2}>
          <StringText>{'}'}&apos;</StringText>
        </CodeLine>
      </div>
    </div>
  )
}

function ResponseBlock(props: { demo: ApiDemoConfig; transitioning: boolean }) {
  const { demo, transitioning } = props
  return (
    <div className={cn('relative border-t px-5 py-4', 'border-border/40 bg-muted/20 dark:border-white/[0.05] dark:bg-white/[0.015]')}>
      <SectionLabel>Response</SectionLabel>
      <div className={cn('mt-2 transition-opacity duration-200', transitioning ? 'opacity-0' : 'opacity-100')}>
        {demo.response.map((line, i) => (
          <CodeLine key={i}>{renderResponseLine(line, demo)}</CodeLine>
        ))}
      </div>
    </div>
  )
}

function HeroTerminalDemo() {
  const [activeIndex, setActiveIndex] = useState(0)
  const [transitioning, setTransitioning] = useState(false)
  const intervalRef = useRef<ReturnType<typeof setInterval>>(undefined)
  const timeoutRef = useRef<ReturnType<typeof setTimeout>>(undefined)

  useEffect(() => {
    const mq = window.matchMedia('(prefers-reduced-motion: reduce)')
    if (mq.matches) return

    intervalRef.current = setInterval(() => {
      setTransitioning(true)
      timeoutRef.current = setTimeout(() => {
        setActiveIndex((prev) => (prev + 1) % API_DEMOS.length)
        setTransitioning(false)
      }, TRANSITION_MS)
    }, CYCLE_INTERVAL)

    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current)
      if (timeoutRef.current) clearTimeout(timeoutRef.current)
    }
  }, [])

  const handleSelect = (index: number) => {
    if (index === activeIndex) return
    if (intervalRef.current) clearInterval(intervalRef.current)
    if (timeoutRef.current) clearTimeout(timeoutRef.current)
    setTransitioning(true)
    timeoutRef.current = setTimeout(() => {
      setActiveIndex(index)
      setTransitioning(false)
    }, TRANSITION_MS)
  }

  const demo = API_DEMOS[activeIndex]
  const accent = ACCENT_CLASSES[demo.accent]

  return (
    <div className='mx-auto w-full max-w-2xl'>
      <div className={cn(
        'overflow-hidden rounded-2xl border backdrop-blur-sm',
        'border-border/60 bg-white/95 shadow-[0_20px_50px_-25px_rgba(15,23,42,0.18)]',
        'dark:border-white/[0.06] dark:bg-[#0b0f17]/95 dark:shadow-[0_20px_60px_-25px_rgba(0,0,0,0.7)]'
      )}>
        {/* Tab strip */}
        <div className={cn('flex items-center gap-1 border-b px-2 sm:gap-1.5 sm:px-3', 'border-border/50 dark:border-white/[0.05]')}>
          {API_DEMOS.map((item, index) => {
            const tone = ACCENT_CLASSES[item.accent]
            const isActive = index === activeIndex
            return (
              <button
                key={item.id}
                onClick={() => handleSelect(index)}
                className={cn(
                  'relative -mb-px flex items-center gap-1.5 border-b-2 px-2.5 py-2.5 text-[11px] font-medium tracking-wide transition-colors sm:px-3 sm:text-xs',
                  isActive
                    ? `${tone.activeBorder} ${tone.activeText}`
                    : 'text-foreground/40 hover:text-foreground/70 border-transparent'
                )}
              >
                {item.label}
              </button>
            )
          })}
          <div className='ml-auto flex items-center gap-2 pr-2 sm:pr-3'>
            <span className='inline-block size-1.5 rounded-full bg-emerald-500 shadow-[0_0_8px_rgba(16,185,129,0.45)]' />
            <span className='text-foreground/40 font-mono text-[10px] tracking-wider uppercase'>200 ok</span>
          </div>
        </div>

        {/* Endpoint row */}
        <div className={cn('flex items-center gap-2.5 border-b px-5 py-3', 'border-border/40 dark:border-white/[0.04]')}>
          <span className={cn('rounded-md px-1.5 py-0.5 font-mono text-[10px] font-semibold tracking-wider', accent.badge)}>
            {demo.method}
          </span>
          <code className={cn('text-foreground/75 truncate font-mono text-[12.5px] transition-opacity duration-200', transitioning ? 'opacity-0' : 'opacity-100')}>
            {demo.endpoint}
          </code>
        </div>

        {/* Body */}
        <div className='grid h-[400px] grid-rows-[235px_minmax(0,1fr)] font-mono text-[12.5px] leading-[1.55]'>
          <RequestBlock demo={demo} transitioning={transitioning} />
          <ResponseBlock demo={demo} transitioning={transitioning} />
        </div>

        {/* Footer metrics */}
        <div className={cn('flex items-center justify-between border-t px-5 py-2.5', 'border-border/40 bg-muted/30 dark:border-white/[0.05] dark:bg-white/[0.02]')}>
          <div className='text-foreground/40 flex items-center gap-3 text-[10px] tabular-nums'>
            <span className='flex items-center gap-1'>
              <span className='font-mono'>{demo.latency}</span>
              <span className='tracking-wider uppercase'>ms</span>
            </span>
            <span className='bg-foreground/15 size-1 rounded-full' />
            <span className='flex items-center gap-1'>
              <span className='font-mono'>{demo.tokens}</span>
              <span className='tracking-wider uppercase'>tokens</span>
            </span>
            <span className='bg-foreground/15 size-1 rounded-full' />
            <span className='flex items-center gap-1'>
              <span className='tracking-wider uppercase'>cost</span>
              <span className='font-mono'>${(demo.tokens * 0.00003).toFixed(5)}</span>
            </span>
          </div>
          <span className='text-foreground/30 font-mono text-[10px] tracking-wider uppercase'>stream · sse</span>
        </div>
      </div>
    </div>
  )
}

/* ── Main Home ── */
export function Home() {
  const stats = [
    { end: 50, suffix: '+', label: '上游服务接入' },
    { end: 100, suffix: '+', label: '模型计费支持' },
    { end: 50, suffix: '+', label: '兼容 API 路由' },
    { end: 10, suffix: '+', label: '调度控制能力' },
  ]

  const features = [
    {
      id: 'fast',
      num: '01',
      title: '极速响应',
      desc: '优化的网络架构确保毫秒级响应时间',
      span: 'md:col-span-2',
      icon: '⚡',
      visual: (
        <div className='mt-4 grid grid-cols-3 gap-2'>
          {['OpenAI', 'Claude', 'Gemini', 'DeepSeek', 'Qwen', 'Llama'].map((name) => (
            <div key={name} className='border-border/30 bg-muted/20 text-muted-foreground flex items-center justify-center rounded-lg border px-3 py-2 text-xs transition-colors duration-300 hover:border-blue-500/30 hover:bg-blue-500/5'>
              {name}
            </div>
          ))}
        </div>
      ),
    },
    {
      id: 'secure',
      num: '02',
      title: '安全可靠',
      desc: '企业级安全保障，全面的权限管理',
      span: 'md:col-span-1',
      icon: '🛡️',
      visual: (
        <div className='mt-4 flex items-center justify-center'>
          <div className='relative'>
            <div className='flex size-16 items-center justify-center rounded-2xl border border-emerald-500/20 bg-emerald-500/5'>
              <span className='text-2xl'>🛡️</span>
            </div>
            <div className='absolute -top-1 -right-1 flex size-4 items-center justify-center rounded-full bg-emerald-500'>
              <svg className='size-2.5 text-white' fill='none' viewBox='0 0 24 24' stroke='currentColor' strokeWidth={3}>
                <path strokeLinecap='round' strokeLinejoin='round' d='m4.5 12.75 6 6 9-13.5' />
              </svg>
            </div>
          </div>
        </div>
      ),
    },
    {
      id: 'global',
      num: '03',
      title: '全球覆盖',
      desc: '多区域部署，全球稳定访问',
      span: 'md:col-span-1',
      icon: '🌍',
      visual: (
        <div className='mt-4 space-y-2'>
          {['负载均衡', '速率限制', '成本追踪'].map((step, i) => (
            <div key={step} className='flex items-center gap-2'>
              <div className={cn(
                'flex size-6 items-center justify-center rounded-full text-[10px] font-bold',
                i === 1
                  ? 'border border-blue-500/30 bg-blue-500/20 text-blue-500'
                  : 'border-border/40 bg-muted text-muted-foreground border'
              )}>
                {i + 1}
              </div>
              <div className='bg-border/40 h-px flex-1' />
              <span className='text-muted-foreground text-xs'>{step}</span>
            </div>
          ))}
        </div>
      ),
    },
    {
      id: 'developer',
      num: '04',
      title: '开发者友好',
      desc: '兼容常见 AI 应用工作流的 API 路由',
      span: 'md:col-span-2',
      icon: '💻',
      visual: (
        <div className='mt-4 flex items-center gap-3'>
          <div className='flex -space-x-2'>
            {['API', 'SDK', 'CLI', 'Docs'].map((n) => (
              <div key={n} className='border-background from-muted to-muted/60 text-muted-foreground flex size-8 items-center justify-center rounded-full border-2 bg-gradient-to-br text-[9px] font-bold'>
                {n}
              </div>
            ))}
          </div>
          <div className='text-muted-foreground flex items-center gap-1.5 text-xs'>
            <span className='text-blue-500'>💻</span>
            多协议兼容
          </div>
        </div>
      ),
    },
  ]

  const additionalFeatures = [
    { icon: '📊', title: '高性能', desc: '支持高并发，自动负载均衡' },
    { icon: '💰', title: '透明计费', desc: '按使用量付费，实时监控' },
    { icon: '👥', title: '团队协作', desc: '多用户管理，灵活权限分配' },
    { icon: '🔓', title: '开源开放', desc: '社区驱动，可自托管，可扩展' },
  ]

  const steps = [
    { num: '1', title: '配置', desc: '添加 API Key，设置渠道和访问权限', icon: '⚙️' },
    { num: '2', title: '连接', desc: '通过 OpenAI、Claude、Gemini 等兼容 API 路由接入', icon: '⚡' },
    { num: '3', title: '监控', desc: '通过实时分析追踪使用量、成本和性能', icon: '📊' },
  ]

  return (
    <div className='bg-background text-foreground relative min-h-svh overflow-x-clip'>
      <Header />

      {/* ── Hero ── */}
      <section className='relative z-10 overflow-hidden px-6 pt-24 pb-16 md:pt-32 md:pb-24 lg:pt-36 lg:pb-28'>
        {/* Radial gradient background */}
        <div
          aria-hidden
          className='pointer-events-none absolute inset-0 -z-10 opacity-25 dark:opacity-[0.12]'
          style={{
            background: [
              'radial-gradient(ellipse 60% 50% at 20% 20%, oklch(0.72 0.18 250 / 80%) 0%, transparent 70%)',
              'radial-gradient(ellipse 50% 40% at 80% 15%, oklch(0.65 0.15 200 / 60%) 0%, transparent 70%)',
              'radial-gradient(ellipse 40% 35% at 40% 80%, oklch(0.70 0.12 280 / 40%) 0%, transparent 70%)',
            ].join(', '),
          }}
        />
        {/* Grid pattern */}
        <div
          aria-hidden
          className='absolute inset-0 -z-10 bg-[linear-gradient(to_right,var(--border)_1px,transparent_1px),linear-gradient(to_bottom,var(--border)_1px,transparent_1px)] [mask-image:radial-gradient(ellipse_60%_50%_at_50%_30%,black_20%,transparent_100%)] bg-[size:4rem_4rem] opacity-[0.08]'
        />

        <div className='mx-auto grid max-w-6xl grid-cols-1 items-start gap-12 lg:grid-cols-12 lg:gap-8'>
          {/* Left Column */}
          <div className='flex flex-col items-start text-left lg:col-span-6'>
            {/* Top Pill Badge */}
            <div
              className='landing-animate-fade-up mb-5 inline-flex items-center gap-1.5 rounded-full border border-blue-500/20 bg-blue-500/5 px-3 py-1.5 text-[11px] font-medium text-blue-600 opacity-0 shadow-xs dark:border-blue-400/20 dark:bg-blue-400/5 dark:text-blue-400'
              style={{ animationDelay: '0ms' }}
            >
              <span className='relative flex size-1.5'>
                <span className='absolute inline-flex h-full w-full animate-ping rounded-full bg-blue-400 opacity-75' />
                <span className='relative inline-flex size-1.5 rounded-full bg-blue-500 dark:bg-blue-400' />
              </span>
              <span>AI 应用基础设施</span>
            </div>

            <h1
              className='landing-animate-fade-up text-[clamp(2.25rem,4.5vw,3.25rem)] leading-[1.15] font-bold tracking-tight'
              style={{ animationDelay: '60ms' }}
            >
              统一的 API 网关
              <br />
              <span className='bg-gradient-to-r from-blue-400 via-violet-400 to-purple-500 bg-clip-text text-transparent'>
                适用于多种 AI 模型
              </span>
            </h1>
            <p
              className='landing-animate-fade-up text-muted-foreground/80 mt-5 max-w-xl text-base leading-relaxed opacity-0 md:text-[15px]'
              style={{ animationDelay: '120ms' }}
            >
              通过标准统一的 API 协议访问海量模型选择。为 AI 应用提供动力，管理数字资产，连接未来。
            </p>

            <div
              className='landing-animate-fade-up mt-8 flex flex-wrap items-center gap-3 opacity-0'
              style={{ animationDelay: '180ms' }}
            >
              <Link
                to='/login'
                className='btn btn-primary btn-lg group'
              >
                开始使用
                <span className='ml-1.5 inline-block transition-transform duration-200 group-hover:translate-x-0.5'>→</span>
              </Link>
              <button className='btn btn-outline btn-lg border-border/50 hover:border-border hover:bg-muted/50'>
                查看文档
              </button>
            </div>

            {/* Supported Apps */}
            <div
              className='landing-animate-fade-up mt-10 w-full max-w-xl opacity-0'
              style={{ animationDelay: '240ms' }}
            >
              <div className='mb-4 flex flex-col gap-1'>
                <span className='text-muted-foreground/50 text-[10px] font-bold tracking-[0.15em] uppercase'>
                  支持的应用
                </span>
                <p className='text-muted-foreground/60 text-xs leading-relaxed'>
                  支持一键配置，完美适配多协议配置。
                </p>
              </div>
              <div className='flex flex-wrap items-center gap-3'>
                {['OpenAI', 'Claude', 'Gemini', 'DeepSeek'].map((name) => (
                  <div key={name} className='group border-border/40 bg-muted/15 text-foreground/80 hover:border-border hover:bg-muted/30 hover:text-foreground flex items-center gap-3 rounded-full border px-5 py-2.5 text-sm font-medium shadow-[0_1px_2.5px_rgba(0,0,0,0.01)] backdrop-blur-xs transition-all duration-300 hover:scale-[1.02]'>
                    <span>{name}</span>
                  </div>
                ))}
              </div>
            </div>
          </div>

          {/* Right Column: Terminal Demo */}
          <div
            className='landing-animate-fade-up flex w-full justify-center opacity-0 lg:col-span-6'
            style={{ animationDelay: '320ms' }}
          >
            <HeroTerminalDemo />
          </div>
        </div>
      </section>

      {/* ── Stats ── */}
      <div className='border-border/40 bg-muted/10 relative z-10 border-y'>
        <div className='mx-auto max-w-6xl px-6 py-10 md:py-12'>
          <div className='grid grid-cols-2 gap-8 md:grid-cols-4 md:gap-12'>
            {stats.map((s) => (
              <div key={s.label} className='flex flex-col items-center text-center'>
                <span className='text-2xl font-bold tracking-tight md:text-3xl'>
                  <Counter end={s.end} suffix={s.suffix} />
                </span>
                <span className='text-muted-foreground mt-1.5 text-xs'>{s.label}</span>
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* ── Features ── */}
      <section id='features' className='relative z-10 px-6 py-24 md:py-32'>
        <div className='mx-auto max-w-6xl'>
          <AnimateInView className='mb-16 max-w-lg'>
            <p className='text-muted-foreground mb-3 text-xs font-medium tracking-widest uppercase'>核心功能</p>
            <h2 className='text-2xl leading-tight font-bold tracking-tight md:text-3xl'>
              为开发者构建
              <br />
              为规模设计
            </h2>
          </AnimateInView>

          {/* Bento grid */}
          <div className='border-border/40 bg-border/40 grid gap-px overflow-hidden rounded-xl border md:grid-cols-3'>
            {features.map((f, i) => (
              <AnimateInView
                key={f.id}
                delay={i * 100}
                animation='scale-in'
                className={`bg-background group hover:bg-muted/20 p-7 transition-colors duration-300 md:p-8 ${f.span}`}
              >
                <div className='mb-3 flex items-center gap-3'>
                  <span className='border-border/40 bg-muted text-muted-foreground flex size-7 items-center justify-center rounded-md border text-[10px] font-semibold tabular-nums'>
                    {f.num}
                  </span>
                  <h3 className='text-sm font-semibold'>{f.title}</h3>
                </div>
                <p className='text-muted-foreground text-sm leading-relaxed'>{f.desc}</p>
                {f.visual}
              </AnimateInView>
            ))}
          </div>

          {/* Additional features row */}
          <div className='mt-12 grid grid-cols-2 gap-8 md:grid-cols-4 md:gap-12'>
            {additionalFeatures.map((f, i) => (
              <AnimateInView key={f.title} delay={i * 100} animation='fade-up' className='flex flex-col items-center text-center'>
                <div className='text-muted-foreground border-border/50 bg-muted/30 group-hover:text-foreground mb-3 flex size-12 items-center justify-center rounded-xl border transition-colors'>
                  <span className='text-xl'>{f.icon}</span>
                </div>
                <h3 className='mb-1.5 text-sm font-semibold'>{f.title}</h3>
                <p className='text-muted-foreground max-w-[200px] text-xs leading-relaxed'>{f.desc}</p>
              </AnimateInView>
            ))}
          </div>
        </div>
      </section>

      {/* ── How It Works ── */}
      <section id='how-it-works' className='border-border/40 relative z-10 border-t px-6 py-24 md:py-32'>
        <div className='mx-auto max-w-6xl'>
          <AnimateInView className='mb-16 text-center md:mb-20'>
            <p className='text-muted-foreground mb-3 text-xs font-medium tracking-widest uppercase'>使用方法</p>
            <h2 className='text-2xl font-bold tracking-tight md:text-3xl'>三步开始使用</h2>
          </AnimateInView>

          <div className='grid gap-8 md:grid-cols-3 md:gap-12'>
            {steps.map((step, i) => (
              <AnimateInView key={step.num} delay={i * 150} animation='fade-up' className='relative flex flex-col items-center text-center'>
                <div className='relative mb-6'>
                  <div className='text-muted-foreground border-border/50 bg-muted/30 flex size-16 items-center justify-center rounded-2xl border transition-colors'>
                    <span className='text-2xl'>{step.icon}</span>
                  </div>
                  <div className='bg-foreground text-background absolute -top-2 -right-2 flex size-6 items-center justify-center rounded-full text-xs font-bold'>
                    {step.num}
                  </div>
                </div>
                <h3 className='mb-2 text-base font-semibold'>{step.title}</h3>
                <p className='text-muted-foreground max-w-[240px] text-sm leading-relaxed'>{step.desc}</p>
              </AnimateInView>
            ))}
          </div>
        </div>
      </section>

      {/* ── CTA ── */}
      <section className='relative z-10 overflow-hidden px-6 py-24 md:py-32'>
        <div
          aria-hidden
          className='absolute inset-0 -z-10 opacity-20 dark:opacity-[0.08]'
          style={{
            background: [
              'radial-gradient(ellipse 50% 50% at 30% 50%, oklch(0.7 0.15 250 / 70%) 0%, transparent 70%)',
              'radial-gradient(ellipse 40% 40% at 70% 40%, oklch(0.65 0.12 200 / 50%) 0%, transparent 70%)',
            ].join(', '),
          }}
        />

        <AnimateInView className='mx-auto max-w-2xl text-center' animation='scale-in'>
          <h2 className='text-2xl leading-tight font-bold tracking-tight md:text-4xl'>
            准备好简化
            <br />
            <span className='bg-gradient-to-r from-blue-400 via-violet-400 to-purple-500 bg-clip-text text-transparent'>
              你的 AI 集成了吗？
            </span>
          </h2>
          <p className='text-muted-foreground/80 mx-auto mt-5 max-w-md text-sm leading-relaxed md:text-base'>
            部署你自己的网关，开始通过配置的上游服务路由请求。
          </p>
          <div className='mt-8 flex items-center justify-center gap-3'>
            <Link
              to='/login'
              className='btn btn-primary btn-lg group'
            >
              开始使用
              <span className='ml-1 inline-block transition-transform duration-200 group-hover:translate-x-0.5'>→</span>
            </Link>
            <button className='btn btn-outline btn-lg border-border/50 hover:border-border hover:bg-muted/50'>
              查看定价
            </button>
          </div>
        </AnimateInView>
      </section>

      {/* ── Footer ── */}
      <footer className='border-border/40 relative z-10 border-t px-6 py-10'>
        <div className='mx-auto flex max-w-6xl flex-col items-center gap-4 text-center'>
          <div className='flex items-center gap-2'>
            <span>⚡</span>
            <span className='text-sm font-semibold'>FutureAI API</span>
          </div>
          <p className='text-muted-foreground text-xs'>下一代 LLM 网关和 AI 资产管理系统</p>
          <p className='text-muted-foreground/60 text-xs'>© 2026 FutureAI API. 基于 Go + Gin + React 构建</p>
        </div>
      </footer>
    </div>
  )
}
