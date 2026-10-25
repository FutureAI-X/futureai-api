import { useState, useEffect, useMemo } from 'react'
import { Link, useLocation, Outlet, useNavigate } from 'react-router-dom'
import { User, Coins, PanelLeft, Users, Radio, Key, Box, Link2, Globe, FileText, History } from 'lucide-react'
import { cn } from '../../lib/utils'
import { Header } from '../../components/Header'

// ── 主布局 ──
export function DashboardLayout() {
  const location = useLocation()
  const navigate = useNavigate()
  const [sidebarOpen, setSidebarOpen] = useState(true)

  const isRoot = useMemo(() => {
    try {
      const stored = localStorage.getItem('user')
      if (!stored) return false
      const user = JSON.parse(stored)
      return user.role >= 100
    } catch {
      return false
    }
  }, [])

  const sidebarNav = useMemo(() => {
    const nav = [
      {
        group: '个人',
        items: [
          { title: '个人资料', href: '/dashboard/profile', icon: User },
          { title: '积分', href: '/dashboard/wallet', icon: Coins },
          { title: '积分日志', href: '/dashboard/credit-logs', icon: History },
          { title: 'API Keys', href: '/dashboard/api-keys', icon: Key },
          { title: '任务日志', href: '/dashboard/task-logs', icon: FileText },
        ],
      },
    ]
    if (isRoot) {
      nav.push({
        group: '管理员',
        items: [
          { title: '用户管理', href: '/dashboard/admin/users', icon: Users },
          { title: '端点管理', href: '/dashboard/admin/endpoints', icon: Globe },
          { title: '模型管理', href: '/dashboard/admin/models', icon: Box },
          { title: '供应商管理', href: '/dashboard/admin/vendors', icon: Radio },
          { title: '供应商模型', href: '/dashboard/admin/vendor-models', icon: Link2 },
        ],
      })
      nav.push({
        group: '运维',
        items: [
          { title: '积分日志', href: '/dashboard/admin/credit-logs', icon: History },
          { title: '任务日志', href: '/dashboard/admin/task-logs', icon: FileText },
        ],
      })
    }
    return nav
  }, [isRoot])

  useEffect(() => {
    if (!localStorage.getItem('token')) {
      navigate('/login', { replace: true })
    }
  }, [navigate])

  return (
    <div className='bg-background text-foreground relative min-h-svh overflow-x-clip'>
      <Header
        leftExtra={
          <button
            onClick={() => setSidebarOpen(!sidebarOpen)}
            className='hover:bg-muted size-8 rounded-lg transition-colors'
          >
            <PanelLeft className='mx-auto size-4' />
          </button>
        }
      />

      {/* ── 主体：侧边栏 + 内容 ── */}
      <div className='flex pt-16'>
        {/* 侧边栏 */}
        <aside
          className={cn(
            'border-border/40 sticky top-16 z-30 h-[calc(100svh-4rem)] shrink-0 border-r transition-all duration-300',
            sidebarOpen ? 'w-60' : 'w-12'
          )}
        >
          {/* 展开状态：完整菜单 */}
          {sidebarOpen && (
            <nav className='flex h-full flex-col space-y-6 overflow-y-auto p-4'>
              {sidebarNav.map((group) => (
                <div key={group.group}>
                  <h3 className='text-muted-foreground mb-2 px-2 text-[11px] font-semibold tracking-wider uppercase'>
                    {group.group}
                  </h3>
                  <div className='space-y-0.5'>
                    {group.items.map((item) => {
                      const Icon = item.icon
                      const isActive = location.pathname === item.href
                      return (
                        <Link
                          key={item.href}
                          to={item.href}
                          className={cn(
                            'flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors',
                            isActive
                              ? 'bg-accent text-accent-foreground'
                              : 'text-muted-foreground hover:bg-accent/50 hover:text-foreground'
                          )}
                        >
                          <Icon className='size-4' />
                          {item.title}
                        </Link>
                      )
                    })}
                  </div>
                </div>
              ))}
            </nav>
          )}

          {/* 收起状态：只显示图标 */}
          {!sidebarOpen && (
            <nav className='flex flex-col items-center gap-1 px-2 pt-2'>
              {sidebarNav.flatMap((group) =>
                group.items.map((item) => {
                  const Icon = item.icon
                  const isActive = location.pathname === item.href
                  return (
                    <Link
                      key={item.href}
                      to={item.href}
                      className={cn(
                        'flex size-8 items-center justify-center rounded-lg transition-colors',
                        isActive
                          ? 'bg-accent text-accent-foreground'
                          : 'text-muted-foreground hover:bg-accent/50 hover:text-foreground'
                      )}
                      title={item.title}
                    >
                      <Icon className='size-4' />
                    </Link>
                  )
                })
              )}
            </nav>
          )}
        </aside>

        {/* 内容区 */}
        <main className='min-w-0 flex-1'>
          {/* data-page-fill：页面自己接管高度，内部滚动，整页不再出现滚动条 */}
          <div className='has-[[data-page-fill]]:h-[calc(100svh-4rem)] has-[[data-page-fill]]:overflow-hidden w-full px-4 py-6 sm:px-6 lg:px-8'>
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  )
}
