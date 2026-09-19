import { useState, useEffect, useMemo, useCallback } from 'react'
import { Search, LayoutGrid, List, ChevronRight } from 'lucide-react'
import { cn } from '../lib/utils'
import { modelTypeLabel } from '../lib/model-type'
import { Header } from '../components/Header'
import { PricingSidebar } from '../components/PricingSidebar'
import { CopyButton } from '../components/CopyButton'
import { ModelDetailDialog } from '../components/ModelDetailDialog'
import { OwnerAvatar } from '../components/OwnerAvatar'
import type { PricingModel, PricingData } from '../types/pricing'
import { formatCredits } from '../lib/credits'

// ── 筛选常量 ──
const FILTER_ALL = '__all__'

// ── 工具函数 ──
function parseTags(tags?: string): string[] {
  if (!tags) return []
  return tags.split(',').map((t) => t.trim()).filter(Boolean)
}

// ── 模型卡片 ──
function ModelCard({
  model,
  onOpen,
}: {
  model: PricingModel
  onOpen: (model: PricingModel) => void
}) {
  const tags = parseTags(model.tags)
  const creditRule = model.credit_rule

  // 卡片上标「起」，所以这里必须是真实下限：某个参数组合可能比基础积分更便宜，
  // 直接拿 base_credits 加「起」会说谎。items 为空时 Math.min 只有 base 一项。
  // Math.min() 不带参数会返回 Infinity，所以 base 必须始终参与比较。
  const minCredits = creditRule
    ? Math.min(creditRule.base_credits, ...(creditRule.items ?? []).map((i) => i.credits))
    : 0

  // 点击处理器挂在卡片本身，而不是标题按钮的拉伸伪元素上。
  //
  // 曾经的写法是给标题按钮加 `after:absolute after:inset-0` 把热区铺满整卡——
  // 真实点击会失效，因为 index.css 有一条全局规则
  // `button:not(:disabled):active { transform: scale(0.98) }`：
  // transform 会让按钮成为其绝对定位后代的包含块，于是鼠标按下的瞬间
  // `::after` 从「整张卡片」塌缩成「按钮自己那一小块」，mouseup 落到别的元素上，
  // click 最终派发给两者的共同祖先（即本卡片），按钮的 onClick 永远不触发，
  // 且没有任何报错。挂在卡片上就没有这个问题——click 无论落在卡片的哪个后代，
  // 都会冒泡到这里。
  const handleOpen = () => {
    // 划选卡片里的文字松手时同样会派发 click，不拦掉会误弹
    if (window.getSelection()?.toString()) return
    onOpen(model)
  }

  return (
    <div
      onClick={handleOpen}
      className='group relative flex cursor-pointer flex-col overflow-hidden rounded-xl border transition-all hover:border-foreground/20 hover:shadow-lg'
    >
      {/* 主内容区 */}
      <div className='flex flex-1 flex-col p-4 sm:p-5'>
        {/* 头部：图标 + 名称 + 操作 */}
        <div className='flex items-start justify-between gap-3'>
          <div className='flex min-w-0 items-start gap-3'>
            <OwnerAvatar owner={model.owner} fallbackName={model.name} />
            <div className='min-w-0'>
              <h3 className='text-foreground min-w-0 truncate font-mono text-sm font-bold sm:text-[15px]'>
                {model.name}
              </h3>
              <div className='mt-0.5 flex items-center gap-1.5'>
                <p className='text-muted-foreground truncate text-xs'>
                  {model.owner}
                </p>
                {model.type && (
                  <span className='bg-muted/70 text-muted-foreground shrink-0 rounded-md px-1.5 py-0.5 text-[11px] font-medium'>
                    {modelTypeLabel(model.type)}
                  </span>
                )}
              </div>
            </div>
          </div>

          {/* 点复制不应连带打开详情 */}
          <div onClick={(e) => e.stopPropagation()}>
            <CopyButton text={model.name} />
          </div>
        </div>

        {/* 描述 */}
        <p className='text-muted-foreground mt-3 line-clamp-2 flex-1 text-xs leading-relaxed sm:text-[13px]'>
          {model.description || '暂无描述'}
        </p>

        {/* 标签 */}
        {tags.length > 0 && (
          <div className='mt-3 flex flex-wrap gap-1.5'>
            {tags.slice(0, 3).map((tag) => (
              <span
                key={tag}
                className='bg-muted/70 text-muted-foreground rounded-md px-2 py-0.5 text-[11px] font-medium'
              >
                {tag}
              </span>
            ))}
            {tags.length > 3 && (
              <span className='text-muted-foreground text-[11px]'>+{tags.length - 3}</span>
            )}
          </div>
        )}
      </div>

      {/* 积分消耗：卡片上只给一个「起」价，完整规则点开详情看 */}
      {creditRule && (
        <div className='px-4 pb-3 sm:px-5 sm:pb-4'>
          <div className='flex items-center justify-between'>
            <div className='flex items-baseline gap-1'>
              <span className='text-xl font-bold tracking-tight sm:text-2xl'>
                {formatCredits(minCredits)}
              </span>
              <span className='text-muted-foreground text-xs'>积分起</span>
            </div>
            <span className='bg-muted text-muted-foreground rounded-md px-2 py-0.5 text-[11px] font-medium'>
              按次计费
            </span>
          </div>
        </div>
      )}

      {/* 明确的入口按钮：只靠整卡热区，用户根本看不出卡片可以点 */}
      <div className='px-4 pb-4 sm:px-5 sm:pb-5'>
        <button
          type='button'
          onClick={handleOpen}
          className='border-border/60 text-foreground hover:bg-muted focus-visible:ring-ring inline-flex h-9 w-full items-center justify-center gap-1 rounded-lg border text-xs font-medium transition-colors focus-visible:ring-2 focus-visible:outline-none'
        >
          查看计费详情
          <ChevronRight className='size-3.5' />
        </button>
      </div>
    </div>
  )
}

// ── 主页面 ──
export function Pricing() {
  const [data, setData] = useState<PricingData | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [search, setSearch] = useState('')
  const [tagFilter, setTagFilter] = useState(FILTER_ALL)
  const [typeFilter, setTypeFilter] = useState(FILTER_ALL)
  const [viewMode, setViewMode] = useState<'card' | 'list'>('card')
  const [detail, setDetail] = useState<PricingModel | null>(null)

  // 必须是稳定引用：ModelDetailDialog 的 Escape 监听以 onClose 为依赖
  const closeDetail = useCallback(() => setDetail(null), [])

  // 获取定价数据
  useEffect(() => {
    fetch('/api/pricing')
      .then((res) => res.json())
      .then((res) => {
        if (res.success) {
          setData(res.data)
        } else {
          setError(res.message || '获取数据失败')
        }
      })
      .catch(() => setError('网络错误'))
      .finally(() => setLoading(false))
  }, [])

  // 过滤模型
  const filteredModels = useMemo(() => {
    if (!data?.models) return []
    let models = data.models

    // 类型过滤
    if (typeFilter !== FILTER_ALL) {
      models = models.filter((m) => m.type === typeFilter)
    }

    // 标签过滤
    if (tagFilter !== FILTER_ALL) {
      models = models.filter((m) =>
        parseTags(m.tags)
          .map((t) => t.toLowerCase())
          .includes(tagFilter.toLowerCase())
      )
    }

    // 搜索过滤
    if (search) {
      const q = search.toLowerCase()
      models = models.filter(
        (m) =>
          m.name.toLowerCase().includes(q) ||
          m.owner?.toLowerCase().includes(q) ||
          m.tags?.toLowerCase().includes(q) ||
          m.description?.toLowerCase().includes(q)
      )
    }

    return models
  }, [data?.models, search, tagFilter, typeFilter])

  // 清除所有筛选
  const clearFilters = useCallback(() => {
    setSearch('')
    setTagFilter(FILTER_ALL)
    setTypeFilter(FILTER_ALL)
  }, [])

  const hasActiveFilters = search !== '' || tagFilter !== FILTER_ALL || typeFilter !== FILTER_ALL

  // 加载状态
  if (loading) {
    return (
      <div className='bg-background text-foreground relative min-h-svh'>
        <div className='mx-auto w-full max-w-[1400px] px-3 pt-20 pb-8 sm:px-6 sm:pt-24 sm:pb-10'>
          <div className='animate-pulse space-y-4'>
            <div className='bg-muted/30 mx-auto h-10 w-64 rounded-lg' />
            <div className='bg-muted/30 mx-auto h-6 w-96 rounded-lg' />
            <div className='mt-8 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3'>
              {Array.from({ length: 6 }).map((_, i) => (
                <div key={i} className='bg-muted/20 h-48 rounded-xl' />
              ))}
            </div>
          </div>
        </div>
      </div>
    )
  }

  // 错误状态
  if (error) {
    return (
      <div className='bg-background text-foreground flex min-h-svh items-center justify-center'>
        <div className='text-center'>
          <p className='text-destructive text-lg font-medium'>{error}</p>
          <button
            onClick={() => window.location.reload()}
            className='bg-primary text-primary-foreground mt-4 rounded-lg px-4 py-2 text-sm'
          >
            重试
          </button>
        </div>
      </div>
    )
  }

  // 注意根节点的 overflow-x-clip：overflow: clip 不会为 fixed 后代创建包含块，
  // 所以 ModelDetailDialog 能正常逃逸。但若今后给这个根节点或中间层加上
  // transform / filter / will-change / contain，弹框会被静默困在裁剪区域内。
  return (
    <div className='bg-background text-foreground relative min-h-svh overflow-x-clip'>
      <Header />

      {/* ── 主内容 ── */}
      <div className='relative'>
        {/* 背景渐变 */}
        <div
          aria-hidden
          className='pointer-events-none absolute inset-x-0 top-0 h-[600px] opacity-20 dark:opacity-[0.10]'
          style={{
            background: [
              'radial-gradient(ellipse 60% 50% at 20% 20%, oklch(0.72 0.18 250 / 80%) 0%, transparent 70%)',
              'radial-gradient(ellipse 50% 40% at 80% 15%, oklch(0.65 0.15 200 / 60%) 0%, transparent 70%)',
              'radial-gradient(ellipse 40% 35% at 50% 70%, oklch(0.70 0.12 280 / 40%) 0%, transparent 70%)',
            ].join(', '),
            maskImage: 'linear-gradient(to bottom, black 40%, transparent 100%)',
            WebkitMaskImage: 'linear-gradient(to bottom, black 40%, transparent 100%)',
          }}
        />

        <div className='relative mx-auto w-full max-w-[1800px] px-3 pt-16 pb-8 sm:px-6 sm:pt-20 sm:pb-10 xl:px-8'>
          {/* 页头 */}
          <header className='mx-auto mb-5 max-w-3xl pt-5 text-center sm:mb-10 sm:pt-10'>
            <h1 className='text-[clamp(2rem,5.5vw,3.5rem)] leading-[1.15] font-bold tracking-tight'>
              模型广场
            </h1>
            <p className='text-muted-foreground/80 mt-3 text-sm sm:mt-4 sm:text-base'>
              当前共有 <span className='text-foreground font-semibold'>{data?.models.length || 0}</span> 个模型可用
            </p>
            <p className='text-muted-foreground/60 mx-auto mt-2 max-w-2xl text-xs leading-relaxed sm:text-sm'>
              探索精选 AI 模型，为每个场景选择合适的模型。
            </p>

            {/* 搜索框 */}
            <div className='relative mx-auto mt-4 max-w-2xl sm:mt-6'>
              <Search className='text-muted-foreground absolute top-1/2 left-3.5 size-4 -translate-y-1/2' />
              <input
                type='text'
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder='搜索模型名称、开发者、标签...'
                className='bg-background border-border/60 ring-offset-background placeholder:text-muted-foreground focus-visible:ring-ring flex h-10 w-full rounded-xl border pr-4 pl-10 text-sm focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none'
              />
              {search && (
                <button
                  onClick={() => setSearch('')}
                  className='text-muted-foreground hover:text-foreground absolute top-1/2 right-3 -translate-y-1/2 text-xs'
                >
                  清除
                </button>
              )}
            </div>
          </header>

          {/* 侧边栏 + 内容区 */}
          <div className='grid gap-4 xl:grid-cols-[280px_minmax(0,1fr)]'>
            {/* 侧边栏（桌面端显示） */}
            <PricingSidebar
              typeFilter={typeFilter}
              onTypeChange={setTypeFilter}
              tagFilter={tagFilter}
              onTagChange={setTagFilter}
              models={data?.models || []}
              hasActiveFilters={hasActiveFilters}
              onClearFilters={clearFilters}
              className='hover-scrollbar sticky top-4 hidden max-h-[calc(100dvh-2rem)] self-start overflow-y-auto xl:block'
            />

            {/* 主内容区 */}
            <main className='min-w-0'>
              {/* 工具栏 */}
              <div className='mb-4 flex flex-wrap items-center justify-between gap-3'>
                <div className='flex items-center gap-2'>
                  <span className='text-muted-foreground text-sm'>
                    共 <span className='text-foreground font-medium'>{filteredModels.length}</span> 个模型
                    {filteredModels.length !== (data?.models.length || 0) && (
                      <span className='text-muted-foreground/60'> / {data?.models.length || 0}</span>
                    )}
                  </span>
                  {hasActiveFilters && (
                    <button
                      onClick={clearFilters}
                      className='text-muted-foreground hover:text-foreground text-xs underline underline-offset-2'
                    >
                      清除筛选
                    </button>
                  )}
                </div>

                <div className='flex items-center gap-2'>
                  {/* 视图切换 */}
                  <div className='bg-muted/50 flex items-center rounded-lg p-0.5'>
                    <button
                      onClick={() => setViewMode('card')}
                      className={cn(
                        'rounded-md p-1.5 transition-colors',
                        viewMode === 'card'
                          ? 'bg-background text-foreground shadow-sm'
                          : 'text-muted-foreground hover:text-foreground'
                      )}
                    >
                      <LayoutGrid className='size-3.5' />
                    </button>
                    <button
                      onClick={() => setViewMode('list')}
                      className={cn(
                        'rounded-md p-1.5 transition-colors',
                        viewMode === 'list'
                          ? 'bg-background text-foreground shadow-sm'
                          : 'text-muted-foreground hover:text-foreground'
                      )}
                    >
                      <List className='size-3.5' />
                    </button>
                  </div>
                </div>
              </div>

              {/* 模型列表 */}
              {filteredModels.length === 0 ? (
                <div className='flex flex-col items-center justify-center py-20'>
                  <p className='text-muted-foreground text-lg'>没有找到匹配的模型</p>
                  {hasActiveFilters && (
                    <button
                      onClick={clearFilters}
                      className='text-primary mt-2 text-sm underline underline-offset-2'
                    >
                      清除筛选条件
                    </button>
                  )}
                </div>
              ) : viewMode === 'card' ? (
                <div className='grid grid-cols-1 gap-3 sm:gap-4 md:grid-cols-2 lg:grid-cols-3'>
                  {filteredModels.map((model) => (
                    <ModelCard
                      key={model.id}
                      model={model}
                      onOpen={setDetail}
                    />
                  ))}
                </div>
              ) : (
                <div className='border-border/40 overflow-hidden rounded-xl border'>
                  <table className='w-full'>
                    <thead>
                      <tr className='bg-muted/30 border-border/40 border-b'>
                        <th className='px-4 py-3 text-left text-xs font-medium'>模型</th>
                        <th className='px-4 py-3 text-left text-xs font-medium'>类型</th>
                        <th className='px-4 py-3 text-left text-xs font-medium'>开发者</th>
                        <th className='px-4 py-3 text-left text-xs font-medium'>描述</th>
                        <th className='px-4 py-3 text-left text-xs font-medium'>基础积分</th>
                        <th className='px-4 py-3 text-left text-xs font-medium'>标签</th>
                        <th className='px-4 py-3 text-right text-xs font-medium'>操作</th>
                      </tr>
                    </thead>
                    <tbody>
                      {filteredModels.map((model) => (
                        <tr
                          key={model.id}
                          onClick={() => {
                            // 划选描述文字松手时浏览器同样会派发 click，
                            // 不拦掉的话每次复制表格里的文字都会弹出详情
                            if (window.getSelection()?.toString()) return
                            setDetail(model)
                          }}
                          className='border-border/20 hover:bg-muted/20 cursor-pointer border-b transition-colors'
                        >
                          <td className='px-4 py-3'>
                            <span className='font-mono text-sm font-medium'>{model.name}</span>
                          </td>
                          <td className='px-4 py-3'>
                            {model.type ? (
                              <span className='bg-muted/60 text-muted-foreground rounded-md px-2 py-0.5 text-xs font-medium'>
                                {modelTypeLabel(model.type)}
                              </span>
                            ) : (
                              // 老接口可能没有 type，占位避免列错位
                              <span className='text-muted-foreground'>-</span>
                            )}
                          </td>
                          <td className='text-muted-foreground px-4 py-3 text-sm'>
                            {model.owner || '-'}
                          </td>
                          <td className='text-muted-foreground px-4 py-3 text-sm max-w-xs truncate'>
                            {model.description || '-'}
                          </td>
                          <td className='px-4 py-3 text-sm'>
                            {model.credit_rule ? (
                              <>
                                <span className='font-medium'>{formatCredits(model.credit_rule.base_credits)}</span>
                                <span className='text-muted-foreground text-xs'> 积分/次</span>
                              </>
                            ) : (
                              // 无计费规则的模型也要占位，否则这一列会错位
                              <span className='text-muted-foreground'>-</span>
                            )}
                          </td>
                          <td className='px-4 py-3'>
                            <div className='flex flex-wrap gap-1'>
                              {parseTags(model.tags)
                                .slice(0, 2)
                                .map((tag) => (
                                  <span
                                    key={tag}
                                    className='bg-muted/50 rounded px-1.5 py-0.5 text-[11px]'
                                  >
                                    {tag}
                                  </span>
                                ))}
                            </div>
                          </td>
                          <td className='px-4 py-3 text-right'>
                            {/* 显式按钮 = 键盘可达入口；tr 上刻意不加 role，
                                否则会覆盖 role="row" 破坏表格语义 */}
                            <button
                              type='button'
                              onClick={(e) => {
                                e.stopPropagation()
                                setDetail(model)
                              }}
                              className='text-primary hover:bg-primary/10 inline-flex h-8 items-center rounded-lg px-2.5 text-xs font-medium transition-colors'
                            >
                              详情
                            </button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </main>
          </div>
        </div>
      </div>

      {/* ── Footer ── */}
      <footer className='border-border/40 relative z-10 border-t px-6 py-10'>
        <div className='mx-auto flex max-w-6xl flex-col items-center gap-4 text-center'>
          <div className='flex items-center gap-2'>
            <span>⚡</span>
            <span className='text-sm font-semibold'>Token Hub</span>
          </div>
          <p className='text-muted-foreground text-xs'>下一代 LLM 网关和 AI 资产管理系统</p>
          <p className='text-muted-foreground/60 text-xs'>© 2026 Token Hub. 基于 Go + Gin + React 构建</p>
        </div>
      </footer>

      <ModelDetailDialog model={detail} onClose={closeDetail} />
    </div>
  )
}
