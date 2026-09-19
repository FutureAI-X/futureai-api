import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { Coins } from 'lucide-react'
import { formatCredits } from '../../lib/credits'

interface UserInfo {
  id: number
  username: string
  credits: number
  used_credits: number
}

export function Wallet() {
  const navigate = useNavigate()
  const [user, setUser] = useState<UserInfo | null>(null)
  const [loading, setLoading] = useState(true)

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

  if (loading) {
    return (
      <div className='space-y-6'>
        <div className='animate-pulse space-y-4'>
          <div className='bg-muted/30 h-24 rounded-xl' />
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

  return (
    <div className='space-y-6'>
      {/* 页面标题 */}
      <div>
        <h2 className='text-2xl font-bold tracking-tight'>积分</h2>
        <p className='text-muted-foreground mt-1 text-sm'>
          查看您的积分余额
        </p>
      </div>

      {/* 积分卡片 */}
      <div className='border-border/60 rounded-xl border p-8'>
        <div className='flex items-center gap-6'>
          <div className='bg-emerald-500/10 flex size-14 items-center justify-center rounded-xl'>
            <Coins className='size-7 text-emerald-600' />
          </div>
          <div className='flex items-center gap-8'>
            <div>
              <p className='text-muted-foreground text-sm font-medium'>当前积分</p>
              <p className='text-4xl font-bold tabular-nums'>{formatCredits(user.credits)}</p>
            </div>
            <div className='border-border/40 h-12 border-l' />
            <div>
              <p className='text-muted-foreground text-sm font-medium'>已消耗积分</p>
              <p className='text-2xl font-bold tabular-nums text-muted-foreground'>{formatCredits(user.used_credits)}</p>
            </div>
          </div>
        </div>
      </div>

      {/* 提示信息 */}
      <div className='bg-muted/30 rounded-xl p-5'>
        <h4 className='mb-2 text-sm font-medium'>关于积分</h4>
        <ul className='text-muted-foreground space-y-1.5 text-xs leading-relaxed'>
          <li>• 积分用于 API 调用，不同模型消耗不同积分</li>
          <li>• 按量模型根据 token 数量消耗积分</li>
          <li>• 按次模型每次调用消耗固定积分</li>
          <li>• 具体消耗请参考模型广场</li>
        </ul>
      </div>
    </div>
  )
}

// ── 格式化积分 ──
