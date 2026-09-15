import { useState, useEffect, useCallback } from 'react'
import {
  Plus,
  Trash2,
  Loader2,
  X,
  Coins,
  Settings,
  CheckCircle2,
  Image as ImageIcon,
} from 'lucide-react'
import {
  getCreditRule,
  saveCreditRule,
  deleteModelCreditRule,
  type CreditRule,
} from '../../api/admin-model'
import { ConfirmDialog } from './ConfirmDialog'

const RULE_TYPE_OPTIONS = [
  { value: 'per_request', label: '按次计费' },
]

// 与后端 model.DefaultRefImageParams 保持一致
const DEFAULT_REF_IMAGE_PARAMS = 'image,images,image_url,image_urls,ref_images'

// 表单项接口（credits 用字符串处理输入）
interface FormCreditCondition {
  param_path: string
  param_value: string
}

interface FormCreditItem {
  credits: string
  conditions: FormCreditCondition[]
}

interface CreditRuleManagerProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  modelId: number
  modelName: string
}

export function CreditRuleManager({ open, onOpenChange, modelId, modelName }: CreditRuleManagerProps) {
  const [rule, setRule] = useState<CreditRule | null>(null)
  const [loading, setLoading] = useState(true)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [actionLoading, setActionLoading] = useState(false)

  // 表单
  const [formRuleType, setFormRuleType] = useState('per_request')
  const [formBaseCredits, setFormBaseCredits] = useState('')
  const [formDesc, setFormDesc] = useState('')
  const [formRefImageCredits, setFormRefImageCredits] = useState('')
  const [formRefImageParams, setFormRefImageParams] = useState('')
  const [formItems, setFormItems] = useState<FormCreditItem[]>([])
  const [formError, setFormError] = useState('')
  const [formSaving, setFormSaving] = useState(false)
  const [showSuccess, setShowSuccess] = useState(false)

  // 格式化为两位小数
  const formatCredits = (credits: number): string => {
    return credits.toFixed(2)
  }

  // 清空表单。所有「加载失败 / 无规则 / 删除成功」的分支都必须走这里，
  // 否则上一个模型的价格会串到下一个模型上。
  const resetForm = useCallback(() => {
    setRule(null)
    setFormRuleType('per_request')
    setFormBaseCredits('')
    setFormDesc('')
    setFormRefImageCredits('')
    setFormRefImageParams('')
    setFormItems([])
  }, [])

  const loadRule = useCallback(async () => {
    setLoading(true)
    try {
      const res = await getCreditRule(modelId)
      if (res.success && res.data) {
        setRule(res.data)
        setFormRuleType(res.data.rule_type)
        setFormBaseCredits(formatCredits(res.data.base_credits))
        setFormDesc(res.data.description || '')
        // 老规则没有这两个字段，用 ?? 兜底避免 formatCredits(undefined) 崩溃
        setFormRefImageCredits(formatCredits(res.data.ref_image_credits ?? 0))
        setFormRefImageParams(res.data.ref_image_params || DEFAULT_REF_IMAGE_PARAMS)
        // 将后端数据转换为表单格式
        setFormItems((res.data.items || []).map(item => ({
          credits: formatCredits(item.credits),
          conditions: (item.conditions || []).map(c => ({
            param_path: c.param_path,
            param_value: c.param_value,
          })),
        })))
      } else {
        resetForm()
      }
    } catch {
      resetForm()
    } finally {
      setLoading(false)
    }
  }, [modelId, resetForm])

  useEffect(() => {
    if (open) loadRule()
  }, [open, loadRule])

  // 验证小数位数不超过2位
  const validateDecimalPlaces = (value: number): boolean => {
    const str = value.toString()
    const decimalPart = str.split('.')[1]
    return !decimalPart || decimalPart.length <= 2
  }

  // 添加一个参数组合映射项（默认带一个空条件）
  const addItem = () => {
    setFormItems([...formItems, { credits: '', conditions: [{ param_path: '', param_value: '' }] }])
  }

  // 删除一个映射项
  const removeItem = (index: number) => {
    setFormItems(formItems.filter((_, i) => i !== index))
  }

  // 更新某个映射项的积分
  const updateItemCredits = (index: number, value: string) => {
    const newItems = [...formItems]
    newItems[index] = { ...newItems[index], credits: value }
    setFormItems(newItems)
  }

  // 给某个映射项添加一个条件
  const addCondition = (itemIndex: number) => {
    const newItems = [...formItems]
    newItems[itemIndex] = {
      ...newItems[itemIndex],
      conditions: [...newItems[itemIndex].conditions, { param_path: '', param_value: '' }],
    }
    setFormItems(newItems)
  }

  // 删除某个映射项的一个条件
  const removeCondition = (itemIndex: number, condIndex: number) => {
    const newItems = [...formItems]
    newItems[itemIndex] = {
      ...newItems[itemIndex],
      conditions: newItems[itemIndex].conditions.filter((_, i) => i !== condIndex),
    }
    setFormItems(newItems)
  }

  // 更新某个映射项的某个条件字段
  const updateCondition = (itemIndex: number, condIndex: number, field: keyof FormCreditCondition, value: string) => {
    const newItems = [...formItems]
    const conditions = [...newItems[itemIndex].conditions]
    conditions[condIndex] = { ...conditions[condIndex], [field]: value }
    newItems[itemIndex] = { ...newItems[itemIndex], conditions }
    setFormItems(newItems)
  }

  const handleSave = async () => {
    if (!formBaseCredits || parseFloat(formBaseCredits) <= 0) {
      setFormError('基础积分必须大于 0')
      return
    }

    if (!validateDecimalPlaces(parseFloat(formBaseCredits))) {
      setFormError('基础积分最多支持2位小数')
      return
    }

    // 参考图积分允许为 0（表示不计费），空值按 0 处理
    const refImageCredits = formRefImageCredits.trim() === '' ? 0 : parseFloat(formRefImageCredits)
    if (isNaN(refImageCredits) || refImageCredits < 0) {
      setFormError('每张参考图积分不能为负数')
      return
    }
    if (!validateDecimalPlaces(refImageCredits)) {
      setFormError('每张参考图积分最多支持2位小数')
      return
    }

    // 验证参数组合映射
    for (let i = 0; i < formItems.length; i++) {
      const item = formItems[i]
      const credits = parseFloat(item.credits)
      if (isNaN(credits) || credits <= 0) {
        setFormError(`第 ${i + 1} 项的积分必须大于 0`)
        return
      }
      if (!validateDecimalPlaces(credits)) {
        setFormError(`第 ${i + 1} 项的积分最多支持2位小数`)
        return
      }
      if (item.conditions.length === 0) {
        setFormError(`第 ${i + 1} 项至少需要 1 个参数条件`)
        return
      }
      for (let j = 0; j < item.conditions.length; j++) {
        if (!item.conditions[j].param_path.trim() || !item.conditions[j].param_value.trim()) {
          setFormError(`第 ${i + 1} 项的第 ${j + 1} 个条件参数路径和参数值不能为空`)
          return
        }
      }
    }

    setFormSaving(true)
    setFormError('')
    try {
      const res = await saveCreditRule(modelId, {
        rule_type: formRuleType,
        base_credits: parseFloat(formBaseCredits),
        description: formDesc || undefined,
        ref_image_credits: refImageCredits,
        // 显式发送（可能为空串）：后端把空串规范化为内置默认值，
        // 这样「清空输入框」才能真的恢复默认，而不是变成一个静默的空操作
        ref_image_params: formRefImageParams.trim(),
        items: formItems.length > 0 ? formItems.map(item => ({
          credits: parseFloat(item.credits),
          conditions: item.conditions.map(c => ({
            param_path: c.param_path.trim(),
            param_value: c.param_value.trim(),
          })),
        })) : undefined,
      })
      if (!res.success) {
        setFormError(res.message || '保存失败')
        return
      }
      setShowSuccess(true)
      setTimeout(() => setShowSuccess(false), 2000)
      loadRule()
    } catch {
      setFormError('网络错误')
    } finally {
      setFormSaving(false)
    }
  }

  const handleDeleteConfirm = async () => {
    if (!rule) return
    setActionLoading(true)
    try {
      await deleteModelCreditRule(modelId)
      setDeleteOpen(false)
      resetForm()
    } catch { /* ignore */ }
    finally { setActionLoading(false) }
  }

  if (!open) return null

  return (
    <div className='fixed inset-0 z-[100] flex items-center justify-center p-4'>
      <div className='bg-background/80 fixed inset-0 backdrop-blur-sm' onClick={() => onOpenChange(false)} />
      <div className='bg-background border-border/60 relative z-10 flex h-[85vh] w-full max-w-4xl flex-col rounded-xl border shadow-lg'>
        <div className='flex items-center justify-between border-b border-border/40 px-6 py-4'>
          <div className='flex items-center gap-3'>
            <Coins className='size-5 text-amber-500' />
            <div>
              <h2 className='text-lg font-semibold'>积分规则配置</h2>
              <p className='text-muted-foreground text-sm'>
                配置模型 <span className='font-medium text-foreground'>{modelName}</span> 的积分扣除规则
              </p>
            </div>
          </div>
          <button onClick={() => onOpenChange(false)} className='hover:bg-muted rounded-lg p-1.5 transition-colors'>
            <X className='size-4' />
          </button>
        </div>

        <div className='flex-1 overflow-auto p-6'>
          {loading ? (
            <div className='flex h-full items-center justify-center'>
              <Loader2 className='text-muted-foreground size-6 animate-spin' />
            </div>
          ) : (
            <div className='space-y-6'>
              {/* 基本规则配置 */}
              <div className='rounded-lg border border-border/60 p-5'>
                <div className='mb-5 flex items-center gap-2'>
                  <Settings className='size-4 text-muted-foreground' />
                  <h3 className='text-sm font-medium'>基本规则</h3>
                </div>

                <div className='grid grid-cols-2 gap-6'>
                  <div className='space-y-2'>
                    <label className='text-sm font-medium'>规则类型</label>
                    <select
                      value={formRuleType}
                      onChange={(e) => setFormRuleType(e.target.value)}
                      className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                    >
                      {RULE_TYPE_OPTIONS.map((t) => (
                        <option key={t.value} value={t.value}>{t.label}</option>
                      ))}
                    </select>
                    <p className='text-muted-foreground text-xs'>目前仅支持按次计费</p>
                  </div>

                  <div className='space-y-2'>
                    <label className='text-sm font-medium'>基础积分</label>
                    <div className='flex items-center gap-2'>
                      <input
                        type='number'
                        value={formBaseCredits}
                        onChange={(e) => setFormBaseCredits(e.target.value)}
                        placeholder='例如: 10'
                        min='0'
                        step='0.01'
                        className='border-border/60 bg-background focus-visible:ring-ring flex h-9 flex-1 rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                      />
                      <span className='text-muted-foreground text-sm whitespace-nowrap'>积分/次</span>
                    </div>
                    <p className='text-muted-foreground text-xs'>每次 API 调用扣除的基础积分数量</p>
                  </div>
                </div>

                <div className='mt-5 space-y-2'>
                  <label className='text-sm font-medium'>描述 <span className='text-muted-foreground font-normal'>（可选）</span></label>
                  <input
                    type='text'
                    value={formDesc}
                    onChange={(e) => setFormDesc(e.target.value)}
                    placeholder='规则说明'
                    className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                  />
                </div>
              </div>

              {/* 参数组合积分映射 */}
              <div className='rounded-lg border border-border/60 p-5'>
                <div className='mb-4 flex items-center justify-between'>
                  <div className='flex items-center gap-2'>
                    <Coins className='size-4 text-muted-foreground' />
                    <h3 className='text-sm font-medium'>参数组合积分映射 <span className='text-muted-foreground font-normal'>（可选）</span></h3>
                  </div>
                  <button
                    type='button'
                    onClick={addItem}
                    className='text-primary hover:bg-primary/10 inline-flex h-8 items-center gap-1 rounded-lg px-3 text-xs font-medium transition-colors'
                  >
                    <Plus className='size-3.5' /> 添加组合
                  </button>
                </div>

                <p className='text-muted-foreground mb-4 text-xs'>
                  为不同的参数组合设置差异化积分，当请求的所有条件都命中时使用该组合的积分，否则使用基础积分。
                  条件仅支持顶层参数且值必须为<strong className='font-medium text-foreground'>字符串</strong>，
                  数组类型的参数（如参考图）不会命中——参考图请用下方的「参考图附加计费」。
                </p>

                {formItems.length === 0 ? (
                  <div className='rounded-lg border border-dashed border-border/60 p-6 text-center'>
                    <p className='text-muted-foreground text-sm'>暂无参数组合映射</p>
                    <p className='text-muted-foreground text-xs mt-1'>点击上方按钮添加差异化计费规则</p>
                  </div>
                ) : (
                  <div className='space-y-4'>
                    {formItems.map((item, index) => (
                      <div key={index} className='rounded-lg border border-border/60 p-4'>
                        {/* 积分 */}
                        <div className='flex items-center gap-3'>
                          <div className='flex items-center gap-2'>
                            <label className='text-sm font-medium shrink-0'>积分</label>
                            <input
                              type='text'
                              inputMode='decimal'
                              value={item.credits}
                              onChange={(e) => updateItemCredits(index, e.target.value)}
                              placeholder='例如: 0.01'
                              className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-28 rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                            />
                            <span className='text-muted-foreground text-xs whitespace-nowrap'>积分/次</span>
                          </div>
                          <div className='ml-auto flex items-center gap-1'>
                            <button
                              type='button'
                              onClick={() => addCondition(index)}
                              className='text-primary hover:bg-primary/10 inline-flex h-8 items-center gap-1 rounded-lg px-3 text-xs font-medium transition-colors'
                            >
                              <Plus className='size-3.5' /> 添加条件
                            </button>
                            <button
                              type='button'
                              onClick={() => removeItem(index)}
                              className='hover:bg-destructive/10 text-destructive inline-flex h-9 w-9 items-center justify-center rounded-lg transition-colors'
                              title='删除此组合'
                            >
                              <Trash2 className='size-4' />
                            </button>
                          </div>
                        </div>

                        {/* 条件列表 */}
                        <div className='mt-3 space-y-2'>
                          <div className='grid grid-cols-[1fr_1fr_40px] gap-3 text-xs font-medium text-muted-foreground'>
                            <div>参数路径</div>
                            <div>参数值</div>
                            <div></div>
                          </div>
                          {item.conditions.map((cond, condIndex) => (
                            <div key={condIndex} className='grid grid-cols-[1fr_1fr_40px] items-center gap-3'>
                              <input
                                type='text'
                                value={cond.param_path}
                                onChange={(e) => updateCondition(index, condIndex, 'param_path', e.target.value)}
                                placeholder='如: resolution'
                                className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                              />
                              <input
                                type='text'
                                value={cond.param_value}
                                onChange={(e) => updateCondition(index, condIndex, 'param_value', e.target.value)}
                                placeholder='如: 1k'
                                className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                              />
                              <div className='flex justify-center'>
                                <button
                                  type='button'
                                  onClick={() => removeCondition(index, condIndex)}
                                  className='hover:bg-destructive/10 text-destructive inline-flex h-9 w-9 items-center justify-center rounded-lg transition-colors'
                                  title='删除条件'
                                >
                                  <Trash2 className='size-4' />
                                </button>
                              </div>
                            </div>
                          ))}
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </div>

              {/* 参考图附加计费 */}
              <div className='rounded-lg border border-border/60 p-5'>
                <div className='mb-4 flex items-center gap-2'>
                  <ImageIcon className='size-4 text-muted-foreground' />
                  <h3 className='text-sm font-medium'>参考图附加计费 <span className='text-muted-foreground font-normal'>（可选）</span></h3>
                </div>

                <p className='text-muted-foreground mb-4 text-xs'>
                  在基础积分之上<strong className='font-medium text-foreground'>叠加</strong>：图片生成时按请求中携带的参考图张数计费，
                  留空或填 0 表示不计费。同一张图重复出现在多个参数名下只计一次。
                </p>

                {/* 这个依赖跨了两个界面（模型类型在「模型管理」里配），不写出来没人知道 */}
                <p className='text-muted-foreground mb-4 rounded-md bg-muted/40 px-3 py-2 text-xs'>
                  生效前提：该模型的<strong className='font-medium text-foreground'>类型必须是「图像生成」</strong>。
                  其它类型（文本/视频/音乐/其他）即使这里配了单价也不会加价——
                  如需修改，请到「模型管理」编辑该模型的类型。
                </p>

                <div className='grid grid-cols-2 gap-6'>
                  <div className='space-y-2'>
                    <label className='text-sm font-medium'>每张参考图积分</label>
                    <div className='flex items-center gap-2'>
                      <input
                        type='text'
                        inputMode='decimal'
                        value={formRefImageCredits}
                        onChange={(e) => setFormRefImageCredits(e.target.value)}
                        placeholder='0 表示不计费'
                        className='border-border/60 bg-background focus-visible:ring-ring flex h-9 flex-1 rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                      />
                      <span className='text-muted-foreground text-sm whitespace-nowrap'>积分/张</span>
                    </div>
                    <p className='text-muted-foreground text-xs'>
                      例如基础积分 2.00、此处填 1.00，则带 3 张参考图共扣 5.00
                    </p>
                  </div>

                  <div className='space-y-2'>
                    <label className='text-sm font-medium'>参考图参数名</label>
                    <input
                      type='text'
                      value={formRefImageParams}
                      onChange={(e) => setFormRefImageParams(e.target.value)}
                      placeholder={DEFAULT_REF_IMAGE_PARAMS}
                      className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                    />
                    <p className='text-muted-foreground text-xs'>
                      逗号分隔，留空使用默认值。只填实际会用到的参数名
                    </p>
                  </div>
                </div>
              </div>

              {/* 错误提示 */}
              {formError && (
                <div className='bg-destructive/10 text-destructive rounded-lg px-4 py-3 text-sm'>{formError}</div>
              )}

              {/* 成功提示 */}
              {showSuccess && (
                <div className='bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 flex items-center gap-2 rounded-lg px-4 py-3 text-sm'>
                  <CheckCircle2 className='size-4' />
                  积分规则保存成功
                </div>
              )}
            </div>
          )}
        </div>

        {/* 底部按钮 */}
        <div className='flex items-center justify-between border-t border-border/40 px-6 py-4'>
          <div>
            {rule && (
              <button
                onClick={() => setDeleteOpen(true)}
                className='text-destructive hover:bg-destructive/10 inline-flex h-9 items-center gap-1 rounded-lg px-4 text-sm font-medium transition-colors'
              >
                删除规则
              </button>
            )}
          </div>
          <div className='flex items-center gap-3'>
            <button
              onClick={() => onOpenChange(false)}
              disabled={formSaving}
              className='border-border/60 hover:bg-muted inline-flex h-9 items-center justify-center rounded-lg border px-4 text-sm font-medium transition-colors'
            >
              取消
            </button>
            <button
              onClick={handleSave}
              disabled={formSaving}
              className='bg-primary hover:bg-primary/90 inline-flex h-9 items-center justify-center gap-2 rounded-lg px-4 text-sm font-medium text-white transition-colors disabled:opacity-50'
            >
              {formSaving && <Loader2 className='size-4 animate-spin' />}
              {formSaving ? '保存中...' : '保存'}
            </button>
          </div>
        </div>

        <ConfirmDialog
          open={deleteOpen}
          onOpenChange={setDeleteOpen}
          title='确认删除'
          description={<>确定要删除该模型的积分规则吗？</>}
          confirmText='删除'
          destructive
          loading={actionLoading}
          onConfirm={handleDeleteConfirm}
        />
      </div>
    </div>
  )
}
