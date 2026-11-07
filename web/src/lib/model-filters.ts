import type { PricingModel } from '../types/pricing'
import { normalizeOwner } from './owner'

/** 筛选区里表示「全部」的哨兵值。 */
export const FILTER_ALL = '__all__'

export interface FilterOption {
  value: string
  label: string
  count?: number
}

/** 标签拆分：保留原始大小写，筛选时再统一转小写比较。 */
export function parseTags(tags?: string): string[] {
  if (!tags) return []
  return tags.split(',').map((tag) => tag.trim()).filter(Boolean)
}

export function tagKey(tag: string): string {
  return tag.trim().toLowerCase()
}

export interface ModelFilterState {
  search: string
  /** FILTER_ALL 或模型类型 */
  type: string
  /** FILTER_ALL 或小写标签键 */
  tag: string
  /** 归一化后的 owner 键；空数组表示不筛选 */
  owners: string[]
}

export interface ModelFilterIgnore {
  search?: boolean
  type?: boolean
  tag?: boolean
  owners?: boolean
}

function matchesSearch(model: PricingModel, raw: string): boolean {
  const query = raw.trim().toLowerCase()
  if (!query) return true
  return (
    model.name.toLowerCase().includes(query) ||
    (model.owner || '').toLowerCase().includes(query) ||
    (model.tags || '').toLowerCase().includes(query) ||
    (model.description || '').toLowerCase().includes(query)
  )
}

/**
 * 判断模型是否命中筛选条件。
 *
 * ignore 用于动态计数：计算某一区的选项数量时忽略该区自身的选择，
 * 只应用其它区的条件，避免出现「显示有 N 个、实际筛出来 0 个」。
 */
export function matchesModelFilters(
  model: PricingModel,
  filters: ModelFilterState,
  ignore: ModelFilterIgnore = {},
): boolean {
  if (!ignore.search && !matchesSearch(model, filters.search)) return false

  if (!ignore.type && filters.type !== FILTER_ALL && model.type !== filters.type) {
    return false
  }

  if (!ignore.tag && filters.tag !== FILTER_ALL) {
    const wanted = filters.tag.toLowerCase()
    if (!parseTags(model.tags).some((tag) => tagKey(tag) === wanted)) return false
  }

  if (!ignore.owners && filters.owners.length > 0) {
    if (!filters.owners.includes(normalizeOwner(model.owner))) return false
  }

  return true
}
