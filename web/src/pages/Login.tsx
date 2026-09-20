import { useState } from 'react'
import { useNavigate } from 'react-router-dom'

export function Login() {
  const navigate = useNavigate()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState('')

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setIsLoading(true)

    try {
      const res = await fetch('/api/auth/login', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ username, password }),
      })

      const data = await res.json()

      if (data.success) {
        // 保存认证信息到 localStorage
        localStorage.setItem('token', data.data.token)
        localStorage.setItem('data_key', data.data.data_key)
        localStorage.setItem('user', JSON.stringify(data.data.user))
        // 跳转到主页
        navigate('/')
      } else {
        setError(data.message || '登录失败')
      }
    } catch {
      setError('网络错误，请稍后重试')
    } finally {
      setIsLoading(false)
    }
  }

  return (
    <div className='bg-background flex min-h-svh items-center justify-center px-4'>
      {/* 背景渐变 */}
      <div
        aria-hidden
        className='pointer-events-none fixed inset-0 -z-10 opacity-25 dark:opacity-[0.12]'
        style={{
          background: [
            'radial-gradient(ellipse 60% 50% at 20% 20%, oklch(0.72 0.18 250 / 80%) 0%, transparent 70%)',
            'radial-gradient(ellipse 50% 40% at 80% 15%, oklch(0.65 0.15 200 / 60%) 0%, transparent 70%)',
          ].join(', '),
        }}
      />

      <div className='w-full max-w-md'>
        {/* Logo */}
        <div className='mb-8 text-center'>
          <a href='/' className='inline-flex items-center gap-2.5'>
            <span className='text-2xl'>⚡</span>
            <span className='text-xl font-bold tracking-tight'>FutureAI API</span>
          </a>
        </div>

        {/* 登录卡片 */}
        <div className='bg-card border-border/60 rounded-2xl border p-8 shadow-lg'>
          <div className='mb-6'>
            <h2 className='text-2xl font-semibold tracking-tight'>登录</h2>
            <p className='text-muted-foreground mt-2 text-sm'>
              使用您的账号登录 FutureAI API
            </p>
          </div>

          <form onSubmit={handleSubmit} className='space-y-4'>
            {/* 错误提示 */}
            {error && (
              <div className='bg-destructive/10 text-destructive rounded-lg px-4 py-3 text-sm'>
                {error}
              </div>
            )}

            {/* 用户名 */}
            <div className='space-y-2'>
              <label
                htmlFor='username'
                className='text-sm font-medium leading-none'
              >
                用户名
              </label>
              <input
                id='username'
                type='text'
                placeholder='请输入用户名'
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                required
                className='border-input bg-background ring-offset-background placeholder:text-muted-foreground focus-visible:ring-ring flex h-10 w-full rounded-lg border px-3 py-2 text-sm focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50'
              />
            </div>

            {/* 密码 */}
            <div className='space-y-2'>
              <label
                htmlFor='password'
                className='text-sm font-medium leading-none'
              >
                密码
              </label>
              <input
                id='password'
                type='password'
                placeholder='请输入密码'
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
                className='border-input bg-background ring-offset-background placeholder:text-muted-foreground focus-visible:ring-ring flex h-10 w-full rounded-lg border px-3 py-2 text-sm focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50'
              />
            </div>

            {/* 登录按钮 */}
            <button
              type='submit'
              disabled={isLoading}
              className='bg-primary text-primary-foreground hover:bg-primary/90 focus-visible:ring-ring inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg px-4 py-2 text-sm font-medium transition-colors focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none disabled:pointer-events-none disabled:opacity-50'
            >
              {isLoading ? (
                <svg
                  className='h-4 w-4 animate-spin'
                  xmlns='http://www.w3.org/2000/svg'
                  fill='none'
                  viewBox='0 0 24 24'
                >
                  <circle
                    className='opacity-25'
                    cx='12'
                    cy='12'
                    r='10'
                    stroke='currentColor'
                    strokeWidth='4'
                  />
                  <path
                    className='opacity-75'
                    fill='currentColor'
                    d='M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z'
                  />
                </svg>
              ) : (
                <svg
                  className='h-4 w-4'
                  xmlns='http://www.w3.org/2000/svg'
                  viewBox='0 0 24 24'
                  fill='none'
                  stroke='currentColor'
                  strokeWidth='2'
                  strokeLinecap='round'
                  strokeLinejoin='round'
                >
                  <path d='M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4' />
                  <polyline points='10 17 15 12 10 7' />
                  <line x1='15' y1='12' x2='3' y2='12' />
                </svg>
              )}
              {isLoading ? '登录中...' : '登录'}
            </button>
          </form>
        </div>

        {/* 底部链接 */}
        <p className='text-muted-foreground mt-6 text-center text-xs'>
          <a href='/' className='hover:text-foreground underline underline-offset-4'>
            返回首页
          </a>
        </p>
      </div>
    </div>
  )
}
