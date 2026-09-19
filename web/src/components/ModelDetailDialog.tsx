import { useEffect, useRef } from 'react'
import { X, Image as ImageIcon, Coins } from 'lucide-react'
import type { PricingModel } from '../types/pricing'
import { modelTypeLabel } from '../lib/model-type'
import { CopyButton } from './CopyButton'
import { OwnerAvatar } from './OwnerAvatar'
import { formatCredits } from '../lib/credits'

// 规则类型标签。目前后端只有 per_request 一个值，回落到原始字符串以免新增类型时显示空白。
// 用常量映射而非 enum：tsconfig 开了 erasableSyntaxOnly。
const RULE_TYPE_LABELS: Record<string, string> = {
  per_request: '按次计费',
}

interface ModelDetailDialogProps {
  // model 为 null 表示关闭。刻意不额外提供 open prop：两个 prop 各说各话时
  // 会出现「页面被锁住却什么都不渲染」的状态（滚动锁 effect 只看 open）。
  model: PricingModel | null
  onClose: () => void
}

// 条件组合的可读形式，如 "resolution=1k & quality=high"
function formatConditions(conditions?: { param_path: string; param_value: string }[]): string {
  const parts = (conditions || []).map((c) => `${c.param_path}=${c.param_value}`)
  return parts.length > 0 ? parts.join(' & ') : '无'
}

export function ModelDetailDialog({ model, onClose }: ModelDetailDialogProps) {
  const panelRef = useRef<HTMLDivElement>(null)

  // 打开时把焦点移到面板上，关闭时还原。
  // 不做这步的话，Tab 会先在遮罩后面几十上百个卡片里走一圈才轮到关闭按钮；
  // 关闭后焦点会掉回 <body>，用户被丢回页面顶部。
  useEffect(() => {
    if (!model) return
    const previous = document.activeElement as HTMLElement | null
    panelRef.current?.focus()
    return () => previous?.focus?.()
  }, [model])

  // 锁定页面滚动。
  // 注意锁的是 documentElement 而不是 body：index.css 里 html 设了 overflow-y: scroll，
  // 此时视口的滚动由根元素决定，body 的 overflow 不会传播到视口——
  // 只设 body.style.overflow='hidden' 会让 body 变成一个没有内容可裁的滚动容器，
  // 页面照常滚动（ConfirmDialog 等手写弹框沿用的正是这个无效写法）。
  useEffect(() => {
    if (!model) return
    const root = document.documentElement
    // 隐藏根元素的滚动条会让内容区变宽，先记下槽宽再补等宽内边距，避免布局跳动
    const scrollbarWidth = window.innerWidth - root.clientWidth
    const prevOverflow = root.style.overflow
    const prevPaddingRight = root.style.paddingRight

    root.style.overflow = 'hidden'
    if (scrollbarWidth > 0) root.style.paddingRight = `${scrollbarWidth}px`

    return () => {
      root.style.overflow = prevOverflow
      root.style.paddingRight = prevPaddingRight
    }
  }, [model])

  useEffect(() => {
    if (!model) return
    const handleEsc = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', handleEsc)
    return () => document.removeEventListener('keydown', handleEsc)
  }, [model, onClose])

  if (!model) return null

  const rule = model.credit_rule
  const refCredits = rule?.ref_image_credits ?? 0
  // 加价要求模型类型是「图像生成」（后端同时还要端点命中白名单）。
  // 只看 ref_image_credits > 0 的话，会给一个文本模型描述一笔永远不会发生的扣费。
  // 老接口可能不带 type，此时保守地按不展示处理。
  const hasRefPricing = refCredits > 0 && model.type === 'image'
  const items = rule?.items ?? []

  const refExampleTotal = rule ? rule.base_credits + 2 * refCredits : 0

  const tags = (model.tags || '')
    .split(',')
    .map((t) => t.trim())
    .filter(Boolean)

  return (
    <div className='fixed inset-0 z-[100] flex items-center justify-center p-4'>
      <div className='bg-background/80 fixed inset-0 backdrop-blur-sm' onClick={onClose} />

      <div
        ref={panelRef}
        tabIndex={-1}
        role='dialog'
        aria-modal='true'
        aria-labelledby='model-detail-title'
        className='bg-background border-border/60 relative z-10 flex max-h-[85dvh] w-full max-w-2xl flex-col rounded-xl border shadow-lg focus:outline-none'
      >
        {/* 头部 */}
        <div className='flex items-start justify-between gap-3 border-b border-border/40 px-5 py-4 sm:px-6'>
          <div className='flex min-w-0 items-start gap-3'>
            <OwnerAvatar owner={model.owner} fallbackName={model.name} />
            <div className='min-w-0'>
              <h2 id='model-detail-title' className='text-foreground truncate font-mono text-base font-bold'>
                {model.name}
              </h2>
              <div className='mt-0.5 flex items-center gap-1.5'>
                <p className='text-muted-foreground truncate text-xs'>{model.owner}</p>
                {model.type && (
                  <span className='bg-muted/70 text-muted-foreground shrink-0 rounded-md px-1.5 py-0.5 text-[11px] font-medium'>
                    {modelTypeLabel(model.type)}
                  </span>
                )}
              </div>
            </div>
          </div>
          <div className='flex shrink-0 items-center gap-1'>
            <CopyButton text={model.name} />
            <button
              type='button'
              onClick={onClose}
              className='text-muted-foreground hover:text-foreground hover:bg-muted rounded-lg p-1.5 transition-colors'
              title='关闭'
            >
              <X className='size-4' />
            </button>
          </div>
        </div>

        {/* 内容 */}
        <div className='flex-1 overflow-auto overscroll-contain px-5 py-5 sm:px-6'>
          <div className='space-y-5'>
            {/* 描述 */}
            <p className='text-muted-foreground text-sm leading-relaxed whitespace-pre-wrap'>
              {model.description || '暂无描述'}
            </p>

            {/* 标签 */}
            {tags.length > 0 && (
              <div className='flex flex-wrap gap-1.5'>
                {tags.map((tag) => (
                  <span key={tag} className='bg-muted/70 text-muted-foreground rounded-md px-2 py-0.5 text-[11px] font-medium'>
                    {tag}
                  </span>
                ))}
              </div>
            )}

            {rule ? (
              <>
                {/* 基础积分 */}
                <div className='rounded-lg border border-border/60 p-4'>
                  <div className='flex items-center justify-between gap-3'>
                    <div className='flex items-center gap-2'>
                      <Coins className='text-muted-foreground size-4' />
                      <span className='text-sm font-medium'>基础积分</span>
                    </div>
                    <span className='bg-muted text-muted-foreground rounded-md px-2 py-0.5 text-[11px] font-medium'>
                      {RULE_TYPE_LABELS[rule.rule_type] || rule.rule_type}
                    </span>
                  </div>
                  <div className='mt-3 flex items-baseline gap-1'>
                    <span className='text-3xl font-bold tracking-tight'>{formatCredits(rule.base_credits)}</span>
                    <span className='text-muted-foreground text-sm'>积分/次</span>
                  </div>
                  <p className='text-muted-foreground mt-2 text-xs leading-relaxed'>
                    每次 API 调用扣除的基础积分。命中下方参数组合时，改用该组合的积分。
                  </p>
                </div>

                {/* 参考图附加计费 */}
                {hasRefPricing && (
                  <div className='rounded-lg border border-border/60 p-4'>
                    <div className='flex items-center justify-between gap-3'>
                      <div className='flex items-center gap-2'>
                        <ImageIcon className='text-muted-foreground size-4' />
                        <span className='text-sm font-medium'>参考图附加计费</span>
                      </div>
                      <div className='flex items-baseline gap-1'>
                        <span className='text-xl font-bold tracking-tight'>{formatCredits(refCredits)}</span>
                        <span className='text-muted-foreground text-xs'>积分/张</span>
                      </div>
                    </div>
                    <p className='text-muted-foreground mt-3 text-xs leading-relaxed'>
                      图片生成时，按请求里携带的参考图<strong className='font-medium text-foreground'>张数</strong>叠加在基础积分之上，
                      而不是取代它。
                    </p>
                  </div>
                )}

                {/* 参数组合积分映射 */}
                {items.length > 0 && (
                  <div className='rounded-lg border border-border/60 p-4'>
                    <h3 className='text-sm font-medium'>参数组合积分映射</h3>
                    <p className='text-muted-foreground mt-1 text-xs'>
                      命中组合时使用该组合的积分，全部未命中则使用基础积分。系统按顺序取第一个全部命中的组合。
                    </p>
                    <div className='mt-3 overflow-hidden rounded-lg border border-border/40'>
                      <table className='w-full'>
                        <thead>
                          <tr className='bg-muted/30 border-b border-border/40'>
                            <th className='px-3 py-2 text-left text-xs font-medium'>条件组合</th>
                            <th className='px-3 py-2 text-right text-xs font-medium'>积分</th>
                          </tr>
                        </thead>
                        <tbody>
                          {items.map((item, i) => (
                            <tr key={item.id ?? i} className='border-b border-border/20 last:border-b-0'>
                              <td className='text-muted-foreground px-3 py-2 font-mono text-xs'>
                                {formatConditions(item.conditions)}
                              </td>
                              <td className='px-3 py-2 text-right text-xs font-medium'>
                                {formatCredits(item.credits)}
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  </div>
                )}

                {/* 计费说明 */}
                <div className='rounded-lg border border-border/60 bg-muted/20 p-4'>
                  <h3 className='text-sm font-medium'>计费说明</h3>
                  <div className='text-muted-foreground mt-2 font-mono text-xs leading-relaxed'>
                    本次扣费 = 命中组合的积分（全部未命中则取基础积分）
                    <br />
                    <span className='pl-[5.5rem]'>+ 参考图张数 × 每张参考图积分</span>
                  </div>
                  {hasRefPricing && (
                    <p className='text-muted-foreground mt-3 text-xs leading-relaxed'>
                      举例：基础 {formatCredits(rule.base_credits)}，图片生成时携带 2 张参考图 →{' '}
                      {formatCredits(rule.base_credits)} + 2 × {formatCredits(refCredits)} ={' '}
                      <strong className='font-medium text-foreground'>{formatCredits(refExampleTotal)} 积分</strong>
                    </p>
                  )}
                </div>
              </>
            ) : (
              <div className='rounded-lg border border-dashed border-border/60 p-5 text-center'>
                <p className='text-sm font-medium'>该模型暂未配置计费规则</p>
                <p className='text-muted-foreground mt-1 text-xs leading-relaxed'>
                  未配置计费规则的模型当前不可调用。请联系管理员在后台补充规则。
                </p>
              </div>
            )}
          </div>
        </div>

        {/* 底部 */}
        <div className='flex items-center justify-end border-t border-border/40 px-5 py-3 sm:px-6'>
          <button
            type='button'
            onClick={onClose}
            className='border-border/60 hover:bg-muted inline-flex h-9 items-center justify-center rounded-lg border px-4 text-sm font-medium transition-colors'
          >
            关闭
          </button>
        </div>
      </div>
    </div>
  )
}
