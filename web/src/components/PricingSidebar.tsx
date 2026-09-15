import { useState } from 'react'
import { ChevronDown, RotateCcw } from 'lucide-react'
import { cn } from '../lib/utils'
import { modelTypeLabel } from '../lib/model-type'
import type { PricingModel } from '../types/pricing'

// ── 工具函数 ──
function parseTags(tags?: string): string[] {
  if (!tags) return []
  return tags
    .split(',')
    .map((t) => t.trim().toLowerCase())
    .filter(Boolean)
}

function countBy(
  models: PricingModel[],
  predicate: (model: PricingModel) => boolean
): number {
  return models.reduce((count, model) => count + (predicate(model) ? 1 : 0), 0)
}

// ── 类型 ──
type FilterOption = {
  value: string
  label: string
  count?: number
}

type FilterSectionProps = {
  title: string
  value: string
  options: FilterOption[]
  onChange: (value: string) => void
}

// ── 筛选芯片 ──
function FilterChip(props: {
  option: FilterOption
  active: boolean
  onClick: () => void
}) {
  return (
    <button
      type='button'
      onClick={props.onClick}
      className={cn(
        'group inline-flex max-w-full items-center gap-1.5 rounded-md border px-2 py-1 text-xs font-medium transition-all',
        props.active
          ? 'border-foreground/30 bg-foreground/5 text-foreground shadow-sm'
          : 'border-border/70 bg-background text-muted-foreground hover:border-border hover:bg-muted/50 hover:text-foreground'
      )}
      title={props.option.label}
    >
      <span className='truncate'>{props.option.label}</span>
      {props.option.count != null && (
        <span
          className={cn(
            'rounded-md px-1.5 py-0.5 text-[12px]',
            props.active
              ? 'bg-background text-foreground'
              : 'bg-muted text-muted-foreground'
          )}
        >
          {props.option.count}
        </span>
      )}
    </button>
  )
}

// ── 可折叠筛选区 ──
function FilterSection(props: FilterSectionProps) {
  const [open, setOpen] = useState(true)

  return (
    <div className='border-border/70 border-b pb-3 last:border-b-0'>
      <button
        type='button'
        onClick={() => setOpen(!open)}
        className='group flex w-full items-center justify-between py-2.5 text-left'
      >
        <span className='text-foreground text-sm font-semibold'>
          {props.title}
        </span>
        <ChevronDown
          className={cn(
            'text-muted-foreground size-4 transition-transform',
            open && 'rotate-180'
          )}
        />
      </button>
      {open && (
        <div className='flex flex-wrap gap-1.5'>
          {props.options.map((option) => (
            <FilterChip
              key={option.value}
              option={option}
              active={props.value === option.value}
              onClick={() => props.onChange(option.value)}
            />
          ))}
        </div>
      )}
    </div>
  )
}

// ── 常量 ──
const FILTER_ALL = '__all__'

// ── Props ──
export interface PricingSidebarProps {
  typeFilter: string
  onTypeChange: (value: string) => void
  tagFilter: string
  onTagChange: (value: string) => void
  models: PricingModel[]
  hasActiveFilters: boolean
  onClearFilters: () => void
  className?: string
}

// ── 主组件 ──
export function PricingSidebar(props: PricingSidebarProps) {
  // 标签选项（从所有模型中提取）
  const allTags = Array.from(
    new Set(props.models.flatMap((m) => parseTags(m.tags)))
  ).sort()

  const tagOptions: FilterOption[] = [
    {
      value: FILTER_ALL,
      label: '全部标签',
      count: props.models.length,
    },
    ...allTags.map((tag) => ({
      value: tag,
      label: tag,
      count: countBy(props.models, (model) =>
        parseTags(model.tags).includes(tag)
      ),
    })),
  ]

  // 类型选项：只列出实际存在的类型，避免出现点了必定为空的筛选项
  const presentTypes = Array.from(
    new Set(props.models.map((m) => m.type).filter((t): t is string => !!t))
  ).sort()

  const typeOptions: FilterOption[] = [
    {
      value: FILTER_ALL,
      label: '全部类型',
      count: props.models.length,
    },
    ...presentTypes.map((type) => ({
      value: type,
      label: modelTypeLabel(type),
      count: countBy(props.models, (model) => model.type === type),
    })),
  ]

  return (
    <aside className={cn('rounded-xl border p-3', props.className)}>
      <div className='mb-2.5 flex items-center justify-between gap-2'>
        <div>
          <h2 className='text-foreground text-sm font-bold'>筛选</h2>
          <p className='text-muted-foreground mt-1 text-xs'>
            按类型或标签筛选模型
          </p>
        </div>
        <button
          type='button'
          onClick={props.onClearFilters}
          disabled={!props.hasActiveFilters}
          className={cn(
            'inline-flex h-7 items-center gap-1.5 rounded-md px-2 text-xs transition-colors',
            props.hasActiveFilters
              ? 'text-muted-foreground hover:text-foreground hover:bg-muted'
              : 'text-muted-foreground/50 cursor-not-allowed'
          )}
        >
          <RotateCcw className='size-3.5' />
          重置
        </button>
      </div>

      {props.hasActiveFilters && (
        <span className='bg-primary/10 text-primary mb-3 inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium'>
          筛选已激活
        </span>
      )}

      <div className='space-y-1'>
        <FilterSection
          title='模型类型'
          value={props.typeFilter}
          options={typeOptions}
          onChange={props.onTypeChange}
        />
        <FilterSection
          title='模型标签'
          value={props.tagFilter}
          options={tagOptions}
          onChange={props.onTagChange}
        />
      </div>
    </aside>
  )
}
