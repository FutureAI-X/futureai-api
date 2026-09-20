import { useState, useEffect } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { cn } from '../lib/utils'
import { ProfileDropdown } from './ProfileDropdown'

interface HeaderProps {
  leftExtra?: React.ReactNode
}

export function Header({ leftExtra }: HeaderProps) {
  const location = useLocation()
  const [user, setUser] = useState<{ username: string; display_name?: string; role?: string } | null>(null)

  useEffect(() => {
    const token = localStorage.getItem('token')
    const stored = localStorage.getItem('user')
    if (token && stored) {
      try {
        setUser(JSON.parse(stored))
      } catch {
        localStorage.removeItem('token')
        localStorage.removeItem('user')
        setUser(null)
      }
    } else {
      setUser(null)
    }
  }, [location.pathname])

  return (
    <header className='pointer-events-none fixed inset-x-0 top-0 z-50'>
      <div className='pointer-events-auto mx-auto max-w-7xl px-4 pt-0 md:px-6'>
        <nav className='flex h-16 items-center justify-between px-2'>
          {/* 左侧 */}
          <div className='flex items-center gap-1.5'>
            <div className='flex size-8 items-center justify-center'>
              {leftExtra}
            </div>
            <a href='/' className='group flex shrink-0 items-center gap-2.5'>
              <div className='flex size-7 shrink-0 items-center justify-center transition-all duration-300 group-hover:scale-105'>
                <span className='text-lg'>⚡</span>
              </div>
              <span className='text-sm font-semibold tracking-tight'>FutureAI API</span>
            </a>
          </div>

          {/* 右侧导航 */}
          <div className='hidden items-center gap-0.5 sm:flex'>
            {[
              { title: '主页', href: '/' },
              { title: '控制台', href: '/dashboard/profile' },
              { title: '模型广场', href: '/pricing' },
            ].map((link) => {
              const isActive =
                link.href === '/'
                  ? location.pathname === '/'
                  : location.pathname.startsWith(link.href)
              return (
                <Link
                  key={link.href}
                  to={link.href}
                  className={cn(
                    'rounded-lg px-3 py-1.5 text-sm font-medium transition-colors',
                    isActive
                      ? 'text-foreground'
                      : 'text-muted-foreground hover:text-foreground'
                  )}
                >
                  {link.title}
                </Link>
              )
            })}
            <a
              href='https://future-ai.feishu.cn/wiki/Gs3Ow9fSfinMpwkCNuschCcYnab'
              target='_blank'
              rel='noopener noreferrer'
              className='text-muted-foreground hover:text-foreground rounded-lg px-3 py-1.5 text-sm font-medium transition-colors'
            >
              文档
            </a>
            <div className='bg-border/40 mx-2 h-4 w-px' />
            {user ? (
              <ProfileDropdown user={user} />
            ) : (
              <Link
                to='/login'
                className='bg-foreground text-background inline-flex h-8 items-center justify-center rounded-lg px-3.5 text-xs font-medium transition-opacity hover:opacity-90'
              >
                登录
              </Link>
            )}
          </div>
        </nav>
      </div>
    </header>
  )
}
