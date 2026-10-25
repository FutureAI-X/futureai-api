import { useState, useEffect } from 'react'
import { X, Loader2 } from 'lucide-react'
import type { AdminUser } from '../../api/admin'

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
        setUsername('')
        setPassword('')
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

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')

    if (!username.trim()) { setError('用户名不能为空'); return }
    if (!isEdit && !password.trim()) { setError('密码不能为空'); return }
    if (!isEdit && password.length < 6) { setError('密码长度不能少于6位'); return }

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
            onClick={handleSubmit}
            disabled={saving}
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
