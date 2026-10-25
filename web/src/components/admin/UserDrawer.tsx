import { useState, useEffect } from 'react'
import { X, Loader2 } from 'lucide-react'
import type { AdminUser } from '../../api/admin'

// ── 新建用户的默认凭据 ──
//
// 用户名的格式是 U_{yyyyMMddhhmmss}，方便按创建时间辨认账号。
function generateUsername(): string {
  const now = new Date()
  const pad = (value: number) => String(value).padStart(2, '0')
  return `U_${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`
}

const PASSWORD_GROUPS = [
  'ABCDEFGHIJKLMNOPQRSTUVWXYZ',
  'abcdefghijklmnopqrstuvwxyz',
  '0123456789',
  '!@#$%^&*',
]

// 每类字符各取 4 个，共 16 位。
const PASSWORD_LENGTH_PER_GROUP = 4

interface PasswordChar {
  char: string
  type: number
}

// 用 crypto 而不是 Math.random：生成的是真实账号密码，不该是可预测序列。
function randomInt(max: number): number {
  const buf = new Uint32Array(1)
  crypto.getRandomValues(buf)
  return buf[0] % max
}

function shuffled<T>(items: readonly T[]): T[] {
  const result = items.slice()
  for (let i = result.length - 1; i > 0; i--) {
    const j = randomInt(i + 1)
    ;[result[i], result[j]] = [result[j], result[i]]
  }
  return result
}

// 生成 16 位密码：大写/小写/数字/特殊符号各 4 位，顺序打乱，
// 且相邻两位不能属于同一类型。
function generatePassword(): string {
  const build = (): PasswordChar[] =>
    shuffled(
      PASSWORD_GROUPS.flatMap((group, type) =>
        shuffled(group.split(''))
          .slice(0, PASSWORD_LENGTH_PER_GROUP)
          .map((char) => ({ char, type })),
      ),
    )

  const hasNoAdjacentSameType = (chars: PasswordChar[]) =>
    chars.every((item, i) => i === 0 || item.type !== chars[i - 1].type)

  // 随机打乱后重试。16 位、4 类各 4 个时，单次随机排列满足条件的概率
  // 约 3.6%，1000 次尝试全部失败的概率可以忽略；兜底分支只是理论上存在。
  for (let attempt = 0; attempt < 1000; attempt++) {
    const chars = build()
    if (hasNoAdjacentSameType(chars)) {
      return chars.map((item) => item.char).join('')
    }
  }

  // 兜底：四类按随机顺序轮转输出（A B C D A B C D …），天然不相邻。
  const groups = PASSWORD_GROUPS.map((group) =>
    shuffled(group.split('')).slice(0, PASSWORD_LENGTH_PER_GROUP),
  )
  return shuffled([0, 1, 2, 3])
    .flatMap((type) => groups[type])
    .join('')
}

// 复制文本。优先用异步 Clipboard API；非 HTTPS 环境下它不可用或会拒绝，
// 退回旧的 execCommand 方案（与重置密码弹窗的做法一致）。
async function copyText(text: string): Promise<void> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return
    }
  } catch {
    // 继续走兜底
  }

  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.style.position = 'fixed'
  textarea.style.opacity = '0'
  document.body.appendChild(textarea)
  textarea.select()
  const copied = document.execCommand('copy')
  document.body.removeChild(textarea)
  if (!copied) throw new Error('copy failed')
}

interface UserDrawerProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentRow?: AdminUser | null
  onSubmit: (data: {
    username: string
    password: string
    display_name: string
    role: number
    credits: number
  }) => Promise<void>
}

export function UserDrawer({ open, onOpenChange, currentRow, onSubmit }: UserDrawerProps) {
  const isEdit = !!currentRow
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [role, setRole] = useState(1)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (open) {
      if (isEdit && currentRow) {
        setUsername(currentRow.username)
        setPassword('')
        setDisplayName(currentRow.display_name)
        setRole(currentRow.role)
      } else {
        // 新建时直接生成一套可用凭据；两个输入框仍可手动修改。
        setUsername(generateUsername())
        setPassword(generatePassword())
        setDisplayName('')
        setRole(1)
      }
      setError('')
    }
  }, [open, isEdit, currentRow])

  useEffect(() => {
    if (open) {
      document.body.style.overflow = 'hidden'
    } else {
      document.body.style.overflow = ''
    }
    return () => { document.body.style.overflow = '' }
  }, [open])

  useEffect(() => {
    const handleEsc = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && open) onOpenChange(false)
    }
    document.addEventListener('keydown', handleEsc)
    return () => document.removeEventListener('keydown', handleEsc)
  }, [open, onOpenChange])

  const validate = (): boolean => {
    setError('')
    if (!username.trim()) { setError('用户名不能为空'); return false }
    if (!isEdit && !password.trim()) { setError('密码不能为空'); return false }
    if (!isEdit && password.length < 6) { setError('密码长度不能少于6位'); return false }
    return true
  }

  const save = async () => {
    setSaving(true)
    try {
      await onSubmit({
        username: username.trim(),
        password,
        display_name: displayName.trim(),
        role,
        credits: currentRow?.credits ?? 0,
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : '操作失败')
    } finally {
      setSaving(false)
    }
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (validate()) void save()
  }

  const handleSaveClick = () => {
    if (validate()) void save()
  }

  // 复制并保存：先复制输入框里的当前值（自动生成后可能已被修改），再走保存流程。
  // 复制失败时不保存——否则弹框一关，这套凭据就再也看不到了。
  const handleCopyAndSave = async () => {
    if (saving) return
    if (!validate()) return
    const credentials = `用户: ${username.trim()}\n密码: ${password}`
    try {
      await copyText(credentials)
    } catch {
      setError('复制到剪贴板失败，请检查浏览器权限后重试')
      return
    }
    await save()
  }

  if (!open) return null

  return (
    <div className='fixed inset-0 z-[100] flex items-center justify-center'>
      {/* Overlay */}
      <div
        className='bg-background/80 fixed inset-0 backdrop-blur-sm'
        onClick={() => onOpenChange(false)}
      />
      {/* Dialog */}
      <div className='bg-background border-border/60 relative z-10 w-full max-w-lg rounded-xl border shadow-lg'>
        {/* Header */}
        <div className='flex items-center justify-between border-b border-border/40 px-6 py-4'>
          <div>
            <h2 className='text-lg font-semibold'>{isEdit ? '编辑用户' : '添加用户'}</h2>
            <p className='text-muted-foreground mt-0.5 text-sm'>
              {isEdit ? '修改用户基本信息' : '填写信息创建新用户'}
            </p>
          </div>
          <button
            onClick={() => onOpenChange(false)}
            className='hover:bg-muted rounded-lg p-1.5 transition-colors'
          >
            <X className='size-4' />
          </button>
        </div>

        {/* Form */}
        {/*
          autoComplete='off'：这个弹框里的「用户名 + 密码」会被浏览器识别成登录表单，
          把管理员自己保存的登录凭据自动填进来。表单和用户名框都关掉自动填充，
          密码框用 new-password 明确表示「这是新建用户的密码」，浏览器才不会
          把它当成当前站点的登录凭证去填充。
        */}
        <form onSubmit={handleSubmit} autoComplete='off' className='p-6 space-y-4'>
          {error && (
            <div className='bg-destructive/10 text-destructive rounded-lg px-4 py-3 text-sm'>
              {error}
            </div>
          )}

          <div className='space-y-2'>
            <label className='text-sm font-medium'>用户名</label>
            <input
              type='text'
              autoComplete='off'
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder='请输入用户名'
              className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
            />
          </div>

          {!isEdit && (
            <div className='space-y-2'>
              <label className='text-sm font-medium'>密码</label>
              <input
                type='password'
                autoComplete='new-password'
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder='请输入密码（至少6位）'
                className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
              />
            </div>
          )}

          <div className='space-y-2'>
            <label className='text-sm font-medium'>显示名称</label>
            <input
              type='text'
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              placeholder='留空则使用用户名'
              className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
            />
          </div>

          {!isEdit && (
            <div className='space-y-2'>
              <label className='text-sm font-medium'>角色</label>
              <select
                value={role}
                onChange={(e) => setRole(Number(e.target.value))}
                className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
              >
                <option value={1}>普通用户</option>
                <option value={10}>管理员</option>
              </select>
              <p className='text-muted-foreground text-xs'>不能设为超级管理员</p>
            </div>
          )}
        </form>

        {/* Footer */}
        <div className='flex justify-end gap-3 border-t border-border/40 px-6 py-4'>
          <button
            type='button'
            onClick={() => onOpenChange(false)}
            disabled={saving}
            className='border-border/60 hover:bg-muted inline-flex h-9 items-center justify-center rounded-lg border px-4 text-sm font-medium transition-colors'
          >
            取消
          </button>
          <button
            onClick={isEdit ? handleSaveClick : handleCopyAndSave}
            disabled={saving}
            className='bg-primary hover:bg-primary/90 inline-flex h-9 items-center justify-center gap-2 rounded-lg px-4 text-sm font-medium text-white transition-colors disabled:opacity-50'
          >
            {saving && <Loader2 className='size-4 animate-spin' />}
            {saving ? '保存中...' : isEdit ? '保存' : '复制并保存'}
          </button>
        </div>
      </div>
    </div>
  )
}
