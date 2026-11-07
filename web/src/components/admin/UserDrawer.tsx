import { useState, useEffect } from 'react'
import { X, Loader2 } from 'lucide-react'
import { decryptWithKey } from '../../lib/crypto'
import { generateUserPassword, type AdminUser } from '../../api/admin'
import { validatePasswordStrength } from '../../lib/password'

// ── 新建用户的默认用户名 ──
//
// 用户名的格式是 U_{yyyyMMddhhmmss}，方便按创建时间辨认账号。
function generateUsername(): string {
  const now = new Date()
  const pad = (value: number) => String(value).padStart(2, '0')
  return `U_${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`
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
  const [generatingPassword, setGeneratingPassword] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!open) return

    setError('')

    if (isEdit && currentRow) {
      setUsername(currentRow.username)
      setPassword('')
      setDisplayName(currentRow.display_name)
      setRole(currentRow.role)
      setGeneratingPassword(false)
      return
    }

    // 新建用户的用户名在前端生成；密码统一由后端生成，
    // 保证与「重置密码」共用同一套规则，不再维护两份实现。
    setUsername(generateUsername())
    setPassword('')
    setDisplayName('')
    setRole(1)

    let cancelled = false
    setGeneratingPassword(true)
    const dataKey = localStorage.getItem('data_key') || ''

    generateUserPassword(dataKey)
      .then(async (res) => {
        if (!res.success || !res.data?.encrypted_password) {
          throw new Error(res.message || '生成密码失败')
        }
        return decryptWithKey(res.data.encrypted_password, dataKey)
      })
      .then((generated) => {
        if (cancelled) return
        // 生成期间管理员可能已经手动输入了密码，不要覆盖。
        setPassword((current) => current || generated)
      })
      .catch((err) => {
        if (cancelled) return
        setError(err instanceof Error ? err.message : '生成密码失败，请手动输入')
      })
      .finally(() => {
        if (!cancelled) setGeneratingPassword(false)
      })

    return () => {
      cancelled = true
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
    if (!isEdit) {
      if (!password.trim()) { setError('密码不能为空'); return false }
      const passwordError = validatePasswordStrength(password)
      if (passwordError) { setError(passwordError); return false }
    }
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
                disabled={generatingPassword}
                placeholder={
                  generatingPassword ? '正在生成随机密码…' : '至少8位，且包含至少3类字符'
                }
                className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none disabled:opacity-60'
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
            disabled={saving || generatingPassword}
            className='bg-primary hover:bg-primary/90 inline-flex h-9 items-center justify-center gap-2 rounded-lg px-4 text-sm font-medium text-white transition-colors disabled:opacity-50'
          >
            {(saving || generatingPassword) && <Loader2 className='size-4 animate-spin' />}
            {generatingPassword ? '生成密码中...' : saving ? '保存中...' : isEdit ? '保存' : '复制并保存'}
          </button>
        </div>
      </div>
    </div>
  )
}
