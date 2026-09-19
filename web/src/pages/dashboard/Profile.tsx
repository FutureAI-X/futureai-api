import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { User, Mail, AtSign, Pencil, Loader2, Coins, X, KeyRound, Copy, Check } from 'lucide-react'
import { getUserAvatarFallback, getUserAvatarStyle } from '../../lib/avatar'
import { decryptWithKey } from '../../lib/crypto'
import { formatCredits } from '../../lib/credits'

interface UserInfo {
  id: number
  username: string
  display_name?: string
  role: number
  status: number
  email?: string
  credits: number
  used_credits: number
}

export function Profile() {
  const navigate = useNavigate()
  const [user, setUser] = useState<UserInfo | null>(null)
  const [loading, setLoading] = useState(true)
  const [message, setMessage] = useState({ type: '', text: '' })

  // 弹框状态
  const [dialogOpen, setDialogOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const [formUsername, setFormUsername] = useState('')
  const [formDisplayName, setFormDisplayName] = useState('')
  const [formEmail, setFormEmail] = useState('')
  const [formError, setFormError] = useState('')

  // 密码重置状态
  const [resetConfirmOpen, setResetConfirmOpen] = useState(false)
  const [resetResultOpen, setResetResultOpen] = useState(false)
  const [resetPassword, setResetPassword] = useState('')
  const [copied, setCopied] = useState(false)
  const [resetLoading, setResetLoading] = useState(false)

  useEffect(() => {
    const token = localStorage.getItem('token')
    if (!token) {
      setLoading(false)
      return
    }

    fetch('/api/user/info', {
      headers: { Authorization: `Bearer ${token}` },
    })
      .then((res) => {
        if (res.status === 401) {
          localStorage.removeItem('token')
          localStorage.removeItem('user')
          navigate('/login')
          return null
        }
        return res.json()
      })
      .then((res) => {
        if (res && res.success) {
          setUser(res.data)
        }
      })
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [navigate])

  const openDialog = () => {
    if (!user) return
    setFormUsername(user.username)
    setFormDisplayName(user.display_name || '')
    setFormEmail(user.email || '')
    setFormError('')
    setDialogOpen(true)
  }

  const handleSave = async () => {
    const token = localStorage.getItem('token')
    if (!token || !user) return

    if (!formUsername.trim()) {
      setFormError('用户名不能为空')
      return
    }

    setSaving(true)
    setFormError('')

    try {
      const res = await fetch('/api/user/profile', {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({
          username: formUsername.trim(),
          display_name: formDisplayName.trim(),
          email: formEmail.trim(),
        }),
      })
      const data = await res.json()
      if (data.success) {
        setUser({
          ...user,
          username: formUsername.trim(),
          display_name: formDisplayName.trim(),
          email: formEmail.trim(),
        })
        const stored = localStorage.getItem('user')
        if (stored) {
          try {
            const u = JSON.parse(stored)
            u.username = formUsername.trim()
            u.display_name = formDisplayName.trim()
            localStorage.setItem('user', JSON.stringify(u))
          } catch { /* ignore */ }
        }
        setDialogOpen(false)
        setMessage({ type: 'success', text: '资料更新成功' })
        setTimeout(() => setMessage({ type: '', text: '' }), 3000)
      } else {
        setFormError(data.message || '更新失败')
      }
    } catch {
      setFormError('网络错误')
    } finally {
      setSaving(false)
    }
  }

  useEffect(() => {
    if (dialogOpen) {
      document.body.style.overflow = 'hidden'
    } else {
      document.body.style.overflow = ''
    }
    return () => { document.body.style.overflow = '' }
  }, [dialogOpen, resetConfirmOpen, resetResultOpen])

  useEffect(() => {
    const handleEsc = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        if (resetResultOpen) setResetResultOpen(false)
        else if (resetConfirmOpen) setResetConfirmOpen(false)
        else if (dialogOpen) setDialogOpen(false)
      }
    }
    document.addEventListener('keydown', handleEsc)
    return () => document.removeEventListener('keydown', handleEsc)
  }, [dialogOpen, resetConfirmOpen, resetResultOpen])

  // ── 密码重置 ──
  const handleResetConfirm = async () => {
    const token = localStorage.getItem('token')
    if (!token) return
    setResetLoading(true)
    try {
      const dataKey = localStorage.getItem('data_key') || ''
      const res = await fetch('/api/user/password', {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ data_key: dataKey }),
      })
      const data = await res.json()
      if (data.success && data.data?.encrypted_password) {
        const password = await decryptWithKey(data.data.encrypted_password, dataKey)
        setResetPassword(password)
        setCopied(false)
        setResetConfirmOpen(false)
        setResetResultOpen(true)
      }
    } catch {
      // ignore
    } finally {
      setResetLoading(false)
    }
  }

  const handleCopyPassword = async () => {
    try {
      await navigator.clipboard.writeText(resetPassword)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      const input = document.createElement('input')
      input.value = resetPassword
      document.body.appendChild(input)
      input.select()
      document.execCommand('copy')
      document.body.removeChild(input)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }

  if (loading) {
    return (
      <div className='space-y-6'>
        <div className='animate-pulse space-y-4'>
          <div className='bg-muted/30 h-32 rounded-xl' />
          <div className='bg-muted/30 h-48 rounded-xl' />
        </div>
      </div>
    )
  }

  if (!user) {
    return (
      <div className='flex flex-col items-center justify-center py-20'>
        <p className='text-muted-foreground text-lg'>请先登录</p>
      </div>
    )
  }

  const avatarFallback = getUserAvatarFallback(user.username)
  const avatarStyle = getUserAvatarStyle(user.username)

  return (
    <div className='space-y-6'>
      {/* 页面标题 */}
      <div className='flex items-center justify-between'>
        <div>
          <h2 className='text-2xl font-bold tracking-tight'>个人资料</h2>
          <p className='text-muted-foreground mt-1 text-sm'>管理您的账户信息</p>
        </div>
        <div className='flex gap-2'>
          <button
            onClick={openDialog}
            className='border-border/60 hover:bg-muted inline-flex h-9 items-center justify-center gap-2 rounded-lg border px-4 text-sm font-medium transition-colors'
          >
            <Pencil className='size-4' />
            修改资料
          </button>
          <button
            onClick={() => setResetConfirmOpen(true)}
            className='border-border/60 hover:bg-muted inline-flex h-9 items-center justify-center gap-2 rounded-lg border px-4 text-sm font-medium transition-colors'
          >
            <KeyRound className='size-4' />
            重置密码
          </button>
        </div>
      </div>

      {/* 消息提示 */}
      {message.text && (
        <div className={`rounded-lg px-4 py-3 text-sm ${
          message.type === 'success'
            ? 'bg-emerald-500/10 text-emerald-600 border border-emerald-500/20'
            : 'bg-destructive/10 text-destructive border border-destructive/20'
        }`}>
          {message.text}
        </div>
      )}

      {/* 用户信息卡片 */}
      <div className='border-border/60 rounded-xl border p-6'>
        <div className='flex flex-col items-start gap-6 sm:flex-row'>
          <div className='shrink-0'>
            <span className='flex size-20 items-center justify-center rounded-2xl text-2xl font-bold text-white' style={avatarStyle}>
              {avatarFallback}
            </span>
          </div>

          <div className='flex-1 space-y-6'>
            <div className='flex items-center gap-3'>
              <div className='bg-muted flex size-10 items-center justify-center rounded-lg'>
                <User className='text-muted-foreground size-5' />
              </div>
              <div>
                <p className='text-muted-foreground text-xs'>用户名</p>
                <p className='text-sm font-medium'>{user.username}</p>
              </div>
            </div>

            <div className='flex items-center gap-3'>
              <div className='bg-muted flex size-10 items-center justify-center rounded-lg'>
                <AtSign className='text-muted-foreground size-5' />
              </div>
              <div>
                <p className='text-muted-foreground text-xs'>显示名称</p>
                <p className='text-sm font-medium'>{user.display_name || '-'}</p>
              </div>
            </div>

            <div className='flex items-center gap-3'>
              <div className='bg-muted flex size-10 items-center justify-center rounded-lg'>
                <Mail className='text-muted-foreground size-5' />
              </div>
              <div>
                <p className='text-muted-foreground text-xs'>邮箱</p>
                <p className='text-sm font-medium'>{user.email || '-'}</p>
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* 积分信息 */}
      <div className='border-border/60 rounded-xl border p-6'>
        <div className='flex items-center gap-3'>
          <div className='bg-emerald-500/10 flex size-10 items-center justify-center rounded-lg'>
            <Coins className='size-5 text-emerald-600' />
          </div>
          <div>
            <p className='text-muted-foreground text-xs'>当前积分</p>
            <p className='text-2xl font-bold tabular-nums'>{formatCredits(user.credits)}</p>
          </div>
        </div>
        <div className='border-border/40 mt-4 flex items-center gap-3 border-t pt-4'>
          <div className='bg-muted flex size-10 items-center justify-center rounded-lg'>
            <Coins className='text-muted-foreground size-5' />
          </div>
          <div>
            <p className='text-muted-foreground text-xs'>已消耗积分</p>
            <p className='text-lg font-semibold tabular-nums'>{formatCredits(user.used_credits)}</p>
          </div>
        </div>
      </div>

      {/* 修改资料弹框 */}
      {dialogOpen && (
        <div className='fixed inset-0 z-[100] flex items-center justify-center'>
          <div className='bg-background/80 fixed inset-0 backdrop-blur-sm' onClick={() => setDialogOpen(false)} />
          <div className='bg-background border-border/60 relative z-10 w-full max-w-lg rounded-xl border shadow-lg'>
            {/* Header */}
            <div className='flex items-center justify-between border-b border-border/40 px-6 py-4'>
              <h2 className='text-lg font-semibold'>修改资料</h2>
              <button onClick={() => setDialogOpen(false)} className='hover:bg-muted rounded-lg p-1.5 transition-colors'>
                <X className='size-4' />
              </button>
            </div>

            {/* Body */}
            <div className='p-6 space-y-4'>
              {formError && (
                <div className='bg-destructive/10 text-destructive rounded-lg px-4 py-3 text-sm'>{formError}</div>
              )}

              <div className='space-y-2'>
                <label className='text-sm font-medium'>用户名</label>
                <input
                  type='text'
                  value={formUsername}
                  onChange={(e) => setFormUsername(e.target.value)}
                  className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                />
              </div>

              <div className='space-y-2'>
                <label className='text-sm font-medium'>显示名称</label>
                <input
                  type='text'
                  value={formDisplayName}
                  onChange={(e) => setFormDisplayName(e.target.value)}
                  placeholder='留空则使用用户名'
                  className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                />
              </div>

              <div className='space-y-2'>
                <label className='text-sm font-medium'>邮箱</label>
                <input
                  type='email'
                  value={formEmail}
                  onChange={(e) => setFormEmail(e.target.value)}
                  placeholder='请输入邮箱'
                  className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                />
              </div>
            </div>

            {/* Footer */}
            <div className='flex justify-end gap-3 border-t border-border/40 px-6 py-4'>
              <button
                onClick={() => setDialogOpen(false)}
                disabled={saving}
                className='border-border/60 hover:bg-muted inline-flex h-9 items-center justify-center rounded-lg border px-4 text-sm font-medium transition-colors'
              >
                取消
              </button>
              <button
                onClick={handleSave}
                disabled={saving}
                className='bg-primary hover:bg-primary/90 inline-flex h-9 items-center justify-center gap-2 rounded-lg px-4 text-sm font-medium text-white transition-colors disabled:opacity-50'
              >
                {saving && <Loader2 className='size-4 animate-spin' />}
                {saving ? '保存中...' : '保存'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 重置密码确认弹框 */}
      {resetConfirmOpen && (
        <div className='fixed inset-0 z-[100] flex items-center justify-center'>
          <div className='bg-background/80 fixed inset-0 backdrop-blur-sm' onClick={() => setResetConfirmOpen(false)} />
          <div className='bg-background border-border/60 relative z-10 w-full max-w-md rounded-xl border p-6 shadow-lg'>
            <h2 className='text-lg font-semibold'>重置密码</h2>
            <p className='text-muted-foreground mt-2 text-sm'>
              确定要重置密码吗？系统将自动生成一个随机密码。
            </p>
            <div className='mt-6 flex justify-end gap-3'>
              <button
                onClick={() => setResetConfirmOpen(false)}
                disabled={resetLoading}
                className='border-border/60 hover:bg-muted inline-flex h-9 items-center justify-center rounded-lg border px-4 text-sm font-medium transition-colors'
              >
                取消
              </button>
              <button
                onClick={handleResetConfirm}
                disabled={resetLoading}
                className='bg-destructive hover:bg-destructive/90 inline-flex h-9 items-center justify-center gap-2 rounded-lg px-4 text-sm font-medium text-white transition-colors disabled:opacity-50'
              >
                {resetLoading && <Loader2 className='size-4 animate-spin' />}
                {resetLoading ? '重置中...' : '确认重置'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 重置密码结果弹框 */}
      {resetResultOpen && (
        <div className='fixed inset-0 z-[100] flex items-center justify-center'>
          <div className='bg-background/80 fixed inset-0 backdrop-blur-sm' onClick={() => setResetResultOpen(false)} />
          <div className='bg-background border-border/60 relative z-10 w-full max-w-md rounded-xl border p-6 shadow-lg'>
            <h2 className='text-lg font-semibold'>密码重置成功</h2>
            <p className='text-muted-foreground mt-2 text-sm'>您的新密码如下，请妥善保管：</p>
            <div className='bg-muted/30 mt-4 flex items-center gap-2 rounded-lg px-4 py-3'>
              <code className='flex-1 font-mono text-sm break-all select-all'>{resetPassword}</code>
              <button
                onClick={handleCopyPassword}
                className='hover:bg-muted shrink-0 rounded-lg p-1.5 transition-colors'
                title='复制密码'
              >
                {copied ? <Check className='size-4 text-emerald-500' /> : <Copy className='size-4 text-muted-foreground' />}
              </button>
            </div>
            <div className='mt-6 flex justify-end'>
              <button
                onClick={() => setResetResultOpen(false)}
                className='bg-primary hover:bg-primary/90 inline-flex h-9 items-center justify-center rounded-lg px-4 text-sm font-medium text-white transition-colors'
              >
                我已保存，关闭
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

