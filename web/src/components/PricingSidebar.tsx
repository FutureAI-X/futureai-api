import { useState, type ReactNode } from 'react'
import { ChevronDown, RotateCcw } from 'lucide-react'
import { cn } from '../lib/utils'
import { FILTER_ALL, type FilterOption } from '../lib/model-filters'

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

// ── 可折叠筛选区外壳 ──
function FilterSectionShell(props: {
  title: string
  children: ReactNode
}) {
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
      {open && <div className='flex flex-wrap gap-1.5'>{props.children}</div>}
    </div>
  )
}

// ── 单选筛选区 ──
function FilterSection(props: {
  title: string
  value: string
  options: FilterOption[]
  onChange: (value: string) => void
}) {
  return (
    <FilterSectionShell title={props.title}>
      {props.options.map((option) => (
        <FilterChip
          key={option.value}
          option={option}
          active={props.value === option.value}
          onClick={() => props.onChange(option.value)}
        />
      ))}
    </FilterSectionShell>
  )
}

// ── 多选筛选区 ──
function MultiFilterSection(props: {
  title: string
  options: FilterOption[]
  selected: string[]
  onToggle: (value: string) => void
  onClear: () => void
}) {
  const all = props.options.find((option) => option.value === FILTER_ALL)
  const items = props.options.filter((option) => option.value !== FILTER_ALL)

  return (
    <FilterSectionShell title={props.title}>
      {all && (
        <FilterChip
          option={all}
          active={props.selected.length === 0}
          onClick={props.onClear}
        />
      )}
      {items.map((option) => (
        <FilterChip
          key={option.value || '__unknown_owner__'}
          option={option}
          active={props.selected.includes(option.value)}
          onClick={() => props.onToggle(option.value)}
        />
      ))}
    </FilterSectionShell>
  )
}

// ── Props ──
export interface PricingSidebarProps {
  ownerOptions: FilterOption[]
  ownerFilters: string[]
  onToggleOwner: (value: string) => void
  onClearOwners: () => void
  typeOptions: FilterOption[]
  typeFilter: string
  onTypeChange: (value: string) => void
  tagOptions: FilterOption[]
  tagFilter: string
  onTagChange: (value: string) => void
  hasActiveFilters: boolean
  onClearFilters: () => void
  className?: string
}

// ── 主组件 ──
export function PricingSidebar(props: PricingSidebarProps) {
  return (
    <aside className={cn('rounded-xl border p-3', props.className)}>
      <div className='mb-2.5 flex items-center justify-between gap-2'>
        <div>
          <h2 className='text-foreground text-sm font-bold'>筛选</h2>
          <p className='text-muted-foreground mt-1 text-xs'>
            按开发者、类型或标签筛选模型
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
        <MultiFilterSection
          title='模型开发者'
          options={props.ownerOptions}
          selected={props.ownerFilters}
          onToggle={props.onToggleOwner}
          onClear={props.onClearOwners}
        />
        <FilterSection
          title='模型类型'
          value={props.typeFilter}
          options={props.typeOptions}
          onChange={props.onTypeChange}
        />
        <FilterSection
          title='模型标签'
          value={props.tagFilter}
          options={props.tagOptions}
          onChange={props.onTagChange}
        />
      </div>
    </aside>
  )
}
