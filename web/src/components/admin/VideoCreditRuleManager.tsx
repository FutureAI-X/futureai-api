import { useEffect, useState } from 'react'
import { Coins, Loader2, Plus, Trash2, X } from 'lucide-react'
import {
  getVideoCreditRule,
  saveVideoCreditRule,
  type VideoCreditRule,
} from '../../api/admin-model'
import { formatCredits, validCreditInput } from '../../lib/credits'

interface FormRate {
  resolution: string
  input: string
  noRef: string
  withRef: string
}

interface VideoCreditRuleManagerProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  modelId: number
  modelName: string
}

function emptyRate(): FormRate {
  return { resolution: '', input: '0', noRef: '0', withRef: '0' }
}

/** 把输入解析成积分；空串按 0 处理，格式不合法返回 null。 */
function parseCredit(raw: string): number | null {
  const text = raw.trim()
  if (text === '') return 0
  if (!validCreditInput(text)) return null
  const value = Number(text)
  return Number.isFinite(value) ? value : null
}

export function VideoCreditRuleManager({ open, onOpenChange, modelId, modelName }: VideoCreditRuleManagerProps) {
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const [formDescription, setFormDescription] = useState('')
  const [formStatus, setFormStatus] = useState(1)
  const [formImageCredits, setFormImageCredits] = useState('0')
  const [formImageFree, setFormImageFree] = useState('0')
  const [formAudioCredits, setFormAudioCredits] = useState('0')
  const [formRates, setFormRates] = useState<FormRate[]>([emptyRate()])

  useEffect(() => {
    if (!open) return
    let cancelled = false
    setLoading(true)
    setError('')

    getVideoCreditRule(modelId)
      .then((res) => {
        if (cancelled) return
        if (!res.success) {
          setError('获取视频计费规则失败')
          return
        }
        const rule: VideoCreditRule | null = res.data
        if (!rule) {
          setFormDescription('')
          setFormStatus(1)
          setFormImageCredits('0')
          setFormImageFree('0')
          setFormAudioCredits('0')
          setFormRates([emptyRate()])
          return
        }
        setFormDescription(rule.description ?? '')
        setFormStatus(rule.status ?? 1)
        setFormImageCredits(formatCredits(rule.image_credits_per_item ?? 0))
        setFormImageFree(String(rule.image_free_items ?? 0))
        setFormAudioCredits(formatCredits(rule.audio_credits_per_item ?? 0))
        const rates = (rule.rates ?? []).map((rate) => ({
          resolution: rate.resolution,
          input: formatCredits(rate.input_video_credits_per_second),
          noRef: formatCredits(rate.output_credits_per_second_no_ref),
          withRef: formatCredits(rate.output_credits_per_second_with_ref),
        }))
        setFormRates(rates.length > 0 ? rates : [emptyRate()])
      })
      .catch(() => {
        if (!cancelled) setError('获取视频计费规则失败')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [open, modelId])

  function updateRate(index: number, field: keyof FormRate, value: string) {
    setFormRates((prev) =>
      prev.map((rate, i) => {
        if (i !== index) return rate
        switch (field) {
          case 'resolution':
            return { ...rate, resolution: value }
          case 'input':
            return { ...rate, input: value }
          case 'noRef':
            return { ...rate, noRef: value }
          case 'withRef':
            return { ...rate, withRef: value }
        }
        return rate
      }),
    )
  }

  function addRate() {
    setFormRates((prev) => [...prev, emptyRate()])
  }

  function removeRate(index: number) {
    setFormRates((prev) => prev.filter((_, i) => i !== index))
  }

  async function handleSave() {
    setError('')

    const imageCredits = parseCredit(formImageCredits)
    const audioCredits = parseCredit(formAudioCredits)
    if (imageCredits === null) {
      setError('每张参考图积分格式不正确，最多 3 位小数')
      return
    }
    if (audioCredits === null) {
      setError('每个参考音频积分格式不正确，最多 3 位小数')
      return
    }
    if (!/^\d+$/.test(formImageFree.trim())) {
      setError('免费张数必须是非负整数')
      return
    }
    if (formRates.length === 0) {
      setError('至少配置一个分辨率档位')
      return
    }

    const seen = new Set<string>()
    const rates: Array<{
      resolution: string
      input_video_credits_per_second: number
      output_credits_per_second_no_ref: number
      output_credits_per_second_with_ref: number
    }> = []
    for (const rate of formRates) {
      const resolution = rate.resolution.trim().toUpperCase()
      if (!resolution) {
        setError('分辨率档位不能为空')
        return
      }
      if (seen.has(resolution)) {
        setError(`分辨率档位 ${resolution} 重复`)
        return
      }
      seen.add(resolution)

      const input = parseCredit(rate.input)
      const noRef = parseCredit(rate.noRef)
      const withRef = parseCredit(rate.withRef)
      if (input === null || noRef === null || withRef === null) {
        setError(`分辨率 ${resolution} 的费率格式不正确，最多 3 位小数`)
        return
      }
      rates.push({
        resolution,
        input_video_credits_per_second: input,
        output_credits_per_second_no_ref: noRef,
        output_credits_per_second_with_ref: withRef,
      })
    }

    setSaving(true)
    try {
      const res = await saveVideoCreditRule(modelId, {
        description: formDescription.trim(),
        status: formStatus,
        image_credits_per_item: imageCredits,
        image_free_items: Number(formImageFree.trim()),
        audio_credits_per_item: audioCredits,
        rates,
      })
      if (!res.success) {
        setError(res.message || '保存失败')
        return
      }
      onOpenChange(false)
    } catch {
      setError('保存失败')
    } finally {
      setSaving(false)
    }
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
              <h2 className='text-lg font-semibold'>视频计费规则</h2>
              <p className='text-muted-foreground text-sm'>
                配置模型 <span className='text-foreground font-medium'>{modelName}</span> 的图片、音频与视频计费
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
              {error && <div className='bg-destructive/10 text-destructive rounded-lg px-4 py-3 text-sm'>{error}</div>}

              <div className='border-border/60 rounded-lg border p-5'>
                <h3 className='mb-4 text-sm font-medium'>按数量计费</h3>
                <div className='grid grid-cols-2 gap-6'>
                  <div className='space-y-2'>
                    <label className='text-sm font-medium'>每张参考图积分</label>
                    <input
                      type='text'
                      inputMode='decimal'
                      value={formImageCredits}
                      onChange={(e) => setFormImageCredits(e.target.value)}
                      className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                    />
                  </div>
                  <div className='space-y-2'>
                    <label className='text-sm font-medium'>每请求免费张数</label>
                    <input
                      type='text'
                      inputMode='numeric'
                      value={formImageFree}
                      onChange={(e) => setFormImageFree(e.target.value)}
                      className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                    />
                  </div>
                  <div className='space-y-2'>
                    <label className='text-sm font-medium'>每个参考音频积分</label>
                    <input
                      type='text'
                      inputMode='decimal'
                      value={formAudioCredits}
                      onChange={(e) => setFormAudioCredits(e.target.value)}
                      className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                    />
                  </div>
                  <div className='space-y-2'>
                    <label className='text-sm font-medium'>规则状态</label>
                    <select
                      value={formStatus}
                      onChange={(e) => setFormStatus(Number(e.target.value))}
                      className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                    >
                      <option value={1}>启用</option>
                      <option value={2}>禁用</option>
                    </select>
                  </div>
                  <div className='col-span-2 space-y-2'>
                    <label className='text-sm font-medium'>描述</label>
                    <input
                      type='text'
                      value={formDescription}
                      onChange={(e) => setFormDescription(e.target.value)}
                      placeholder='可选'
                      className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                    />
                  </div>
                </div>
                <p className='text-muted-foreground mt-3 text-xs'>
                  0 表示该维度免费。参考视频秒数按每个文件向上取整后求和；输出按请求的 duration 计费。
                </p>
              </div>

              <div className='border-border/60 rounded-lg border p-5'>
                <div className='mb-4 flex items-center justify-between'>
                  <div>
                    <h3 className='text-sm font-medium'>分辨率价目</h3>
                    <p className='text-muted-foreground mt-1 text-xs'>
                      每个档位分别设置参考视频每秒、输出每秒（含/不含参考视频）的积分。
                    </p>
                  </div>
                  <button
                    onClick={addRate}
                    className='border-border/60 hover:bg-muted inline-flex h-8 items-center gap-1 rounded-lg border px-3 text-xs font-medium transition-colors'
                  >
                    <Plus className='size-3.5' /> 添加档位
                  </button>
                </div>

                <div className='space-y-3'>
                  {formRates.map((rate, index) => (
                    <div key={index} className='border-border/40 grid grid-cols-[1fr_1fr_1fr_1fr_40px] items-end gap-3 rounded-lg border p-3'>
                      <div className='space-y-1'>
                        <label className='text-muted-foreground text-xs'>分辨率</label>
                        <input
                          type='text'
                          value={rate.resolution}
                          onChange={(e) => updateRate(index, 'resolution', e.target.value)}
                          placeholder='1080P'
                          className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                        />
                      </div>
                      <div className='space-y-1'>
                        <label className='text-muted-foreground text-xs'>参考视频每秒</label>
                        <input
                          type='text'
                          inputMode='decimal'
                          value={rate.input}
                          onChange={(e) => updateRate(index, 'input', e.target.value)}
                          className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                        />
                      </div>
                      <div className='space-y-1'>
                        <label className='text-muted-foreground text-xs'>输出每秒（无参考视频）</label>
                        <input
                          type='text'
                          inputMode='decimal'
                          value={rate.noRef}
                          onChange={(e) => updateRate(index, 'noRef', e.target.value)}
                          className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                        />
                      </div>
                      <div className='space-y-1'>
                        <label className='text-muted-foreground text-xs'>输出每秒（含参考视频）</label>
                        <input
                          type='text'
                          inputMode='decimal'
                          value={rate.withRef}
                          onChange={(e) => updateRate(index, 'withRef', e.target.value)}
                          className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                        />
                      </div>
                      <button
                        onClick={() => removeRate(index)}
                        disabled={formRates.length <= 1}
                        className='hover:bg-destructive/10 text-destructive inline-flex h-9 w-9 items-center justify-center rounded-lg transition-colors disabled:opacity-40'
                      >
                        <Trash2 className='size-4' />
                      </button>
                    </div>
                  ))}
                </div>
              </div>
            </div>
          )}
        </div>

        <div className='flex items-center justify-end gap-3 border-t border-border/40 px-6 py-4'>
          <button
            onClick={() => onOpenChange(false)}
            disabled={saving}
            className='border-border/60 hover:bg-muted inline-flex h-9 items-center justify-center rounded-lg border px-4 text-sm font-medium transition-colors'
          >
            取消
          </button>
          <button
            onClick={handleSave}
            disabled={saving || loading}
            className='bg-primary hover:bg-primary/90 inline-flex h-9 items-center justify-center gap-2 rounded-lg px-4 text-sm font-medium text-white transition-colors disabled:opacity-50'
          >
            {saving && <Loader2 className='size-4 animate-spin' />}
            {saving ? '保存中...' : '保存'}
          </button>
        </div>
      </div>
    </div>
  )
}
