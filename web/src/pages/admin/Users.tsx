import { useState, useEffect, useCallback } from 'react'
import {
  Search,
  Plus,
  Pencil,
  Trash2,
  Power,
  PowerOff,
  Coins,
  KeyRound,
  Copy,
  Check,
  Loader2,
  ChevronLeft,
  ChevronRight,
  Shield,
  ShieldCheck,
  User as UserIcon,
} from 'lucide-react'
import { cn } from '../../lib/utils'
import { decryptWithKey } from '../../lib/crypto'
import {
  getUsers,
  createUser,
  updateUser,
  deleteUser,
  updateUserStatus,
  adjustUserCredits,
  resetUserPassword,
  type AdminUser,
} from '../../api/admin'
import { UserDrawer } from '../../components/admin/UserDrawer'
import { CreditDialog } from '../../components/admin/CreditDialog'
import { ConfirmDialog } from '../../components/admin/ConfirmDialog'
import { formatCredits } from '../../lib/credits'

// ── 角色配置 ──
const ROLE_CONFIG: Record<number, { label: string; icon: typeof Shield; className: string }> = {
  1: { label: '普通用户', icon: UserIcon, className: 'bg-muted text-muted-foreground' },
  10: { label: '管理员', icon: Shield, className: 'bg-blue-500/10 text-blue-600 dark:text-blue-400' },
  100: { label: '超级管理员', icon: ShieldCheck, className: 'bg-purple-500/10 text-purple-600 dark:text-purple-400' },
}

// ── 状态配置 ──
const STATUS_CONFIG: Record<number, { label: string; className: string }> = {
  1: { label: '正常', className: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400' },
  2: { label: '已禁用', className: 'bg-red-500/10 text-red-600 dark:text-red-400' },
  3: { label: '已删除', className: 'bg-gray-500/10 text-gray-500 dark:text-gray-400' },
}

const PAGE_SIZE_OPTIONS = [10, 20, 50]

export function AdminUsers() {
  const [users, setUsers] = useState<AdminUser[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [keyword, setKeyword] = useState('')
  const [searchInput, setSearchInput] = useState('')
  const [loading, setLoading] = useState(true)

  // Dialog states
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [editRow, setEditRow] = useState<AdminUser | null>(null)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [deleteRow, setDeleteRow] = useState<AdminUser | null>(null)
  const [creditDialogOpen, setCreditDialogOpen] = useState(false)
  const [creditRow, setCreditRow] = useState<AdminUser | null>(null)
  const [resetDialogOpen, setResetDialogOpen] = useState(false)
  const [resetRow, setResetRow] = useState<AdminUser | null>(null)
  const [resetResultOpen, setResetResultOpen] = useState(false)
  const [resetPassword, setResetPassword] = useState('')
  const [copied, setCopied] = useState(false)
  const [actionLoading, setActionLoading] = useState(false)

  const totalPages = Math.ceil(total / pageSize)

  // ── 数据加载 ──
  const loadUsers = useCallback(async () => {
    setLoading(true)
    try {
      const res = await getUsers({ p: page, page_size: pageSize, keyword })
      if (res.success) {
        setUsers(res.data.items || [])
        setTotal(res.data.total || 0)
      }
    } catch {
      // ignore
    } finally {
      setLoading(false)
    }
  }, [page, pageSize, keyword])

  useEffect(() => { loadUsers() }, [loadUsers])

  // ── 搜索 ──
  const handleSearch = () => {
    setKeyword(searchInput)
    setPage(1)
  }

  // ── 添加用户 ──
  const handleCreate = () => {
    setEditRow(null)
    setDrawerOpen(true)
  }

  // ── 编辑用户 ──
  const handleEdit = (user: AdminUser) => {
    setEditRow(user)
    setDrawerOpen(true)
  }

  // ── 提交（创建/编辑） ──
  const handleDrawerSubmit = async (data: {
    username: string
    password: string
    display_name: string
    role: number
    credits: number
  }) => {
    if (editRow) {
      const updateData: { username?: string; display_name?: string; password?: string } = {}
      if (data.username !== editRow.username) updateData.username = data.username
      if (data.display_name !== editRow.display_name) updateData.display_name = data.display_name
      if (data.password) updateData.password = data.password

      const res = await updateUser(editRow.id, updateData)
      if (res.success) {
        setDrawerOpen(false)
        loadUsers()
      } else {
        throw new Error(res.message || '更新失败')
      }
    } else {
      const res = await createUser(data)
      if (res.success) {
        setDrawerOpen(false)
        loadUsers()
      } else {
        throw new Error(res.message || '创建失败')
      }
    }
  }

  // ── 删除 ──
  const handleDeleteClick = (user: AdminUser) => {
    setDeleteRow(user)
    setDeleteDialogOpen(true)
  }

  const handleDeleteConfirm = async () => {
    if (!deleteRow) return
    setActionLoading(true)
    try {
      const res = await deleteUser(deleteRow.id)
      if (res.success) {
        setDeleteDialogOpen(false)
        setDeleteRow(null)
        loadUsers()
      }
    } catch {
      // ignore
    } finally {
      setActionLoading(false)
    }
  }

  // ── 启用/禁用 ──
  const handleToggleStatus = async (user: AdminUser) => {
    const newStatus = user.status === 1 ? 2 : 1
    setActionLoading(true)
    try {
      await updateUserStatus(user.id, newStatus)
      loadUsers()
    } catch {
      // ignore
    } finally {
      setActionLoading(false)
    }
  }

  // ── 积分调整 ──
  const handleCreditClick = (user: AdminUser) => {
    setCreditRow(user)
    setCreditDialogOpen(true)
  }

  const handleCreditConfirm = async (mode: string, value: number) => {
    if (!creditRow) return
    setActionLoading(true)
    try {
      await adjustUserCredits(creditRow.id, mode, value)
      setCreditDialogOpen(false)
      setCreditRow(null)
      loadUsers()
    } catch {
      // ignore
    } finally {
      setActionLoading(false)
    }
  }

  // ── 密码重置 ──
  const handleResetClick = (user: AdminUser) => {
    setResetRow(user)
    setResetDialogOpen(true)
  }

  const handleResetConfirm = async () => {
    if (!resetRow) return
    setActionLoading(true)
    try {
      const dataKey = localStorage.getItem('data_key') || ''
      const res = await resetUserPassword(resetRow.id, dataKey)
      if (res.success && res.data?.encrypted_password) {
        const password = await decryptWithKey(res.data.encrypted_password, dataKey)
        setResetPassword(password)
        setCopied(false)
        setResetDialogOpen(false)
        setResetResultOpen(true)
      }
    } catch {
      // ignore
    } finally {
      setActionLoading(false)
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

  return (
    <div className='space-y-6'>
      {/* 页面标题 */}
      <div className='flex items-center justify-between'>
        <div>
          <h2 className='text-2xl font-bold tracking-tight'>用户管理</h2>
          <p className='text-muted-foreground mt-1 text-sm'>
            管理系统中的所有用户账户
          </p>
        </div>
        <button
          onClick={handleCreate}
          className='bg-primary hover:bg-primary/90 inline-flex h-9 items-center justify-center gap-2 rounded-lg px-4 text-sm font-medium text-white transition-colors'
        >
          <Plus className='size-4' />
          添加用户
        </button>
      </div>

      {/* 搜索栏 */}
      <div className='flex items-center gap-3'>
        <div className='relative flex-1 max-w-sm'>
          <Search className='text-muted-foreground absolute left-3 top-1/2 size-4 -translate-y-1/2' />
          <input
            type='text'
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') handleSearch() }}
            placeholder='搜索用户名、显示名或邮箱...'
            className='border-border/60 bg-background focus-visible:ring-ring h-9 w-full rounded-lg border pl-9 pr-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
          />
        </div>
        <button
          onClick={handleSearch}
          className='border-border/60 hover:bg-muted inline-flex h-9 items-center justify-center rounded-lg border px-4 text-sm font-medium transition-colors'
        >
          搜索
        </button>
      </div>

      {/* 数据表格 */}
      <div className='border-border/60 overflow-hidden rounded-xl border'>
        <div className='overflow-x-auto'>
          <table className='w-full text-sm'>
            {/* 表头 */}
            <thead>
              <tr className='bg-muted/30 border-border/40 border-b'>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>ID</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>用户名</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>显示名称</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>角色</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>状态</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>积分</th>
                <th className='text-muted-foreground min-w-[200px] px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>邮箱</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>创建时间</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>操作</th>
              </tr>
            </thead>

            {/* 表体 */}
            <tbody className='divide-border/40 divide-y'>
              {loading ? (
                <tr>
                  <td colSpan={9} className='px-4 py-12 text-center'>
                    <Loader2 className='text-muted-foreground mx-auto size-6 animate-spin' />
                    <p className='text-muted-foreground mt-2 text-sm'>加载中...</p>
                  </td>
                </tr>
              ) : users.length === 0 ? (
                <tr>
                  <td colSpan={9} className='px-4 py-12 text-center'>
                    <p className='text-muted-foreground text-sm'>暂无用户数据</p>
                  </td>
                </tr>
              ) : (
                users.map((user) => {
                  const roleConf = ROLE_CONFIG[user.role] || ROLE_CONFIG[1]
                  const statusConf = STATUS_CONFIG[user.status] || STATUS_CONFIG[1]
                  const RoleIcon = roleConf.icon
                  const isRoot = user.role >= 100
                  const isDeleted = user.status === 3

                  return (
                    <tr
                      key={user.id}
                      className={cn(
                        'hover:bg-muted/20 transition-colors',
                        (user.status === 2 || isDeleted) && 'opacity-50'
                      )}
                    >
                      <td className='px-4 py-3'>
                        <span className='text-muted-foreground font-mono text-xs'>{user.id}</span>
                      </td>
                      <td className='px-4 py-3'>
                        <span className='font-medium'>{user.username}</span>
                      </td>
                      <td className='px-4 py-3'>
                        <span className='text-muted-foreground text-sm'>{user.display_name || '-'}</span>
                      </td>
                      <td className='px-4 py-3'>
                        <span className={cn('inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-medium', roleConf.className)}>
                          <RoleIcon className='size-3' />
                          {roleConf.label}
                        </span>
                      </td>
                      <td className='px-4 py-3'>
                        <span className={cn('inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-medium', statusConf.className)}>
                          <span className={cn('size-1.5 rounded-full', user.status === 1 ? 'bg-emerald-500' : user.status === 3 ? 'bg-gray-400' : 'bg-red-500')} />
                          {statusConf.label}
                        </span>
                      </td>
                      <td className='px-4 py-3'>
                        <div className='flex flex-col gap-0.5'>
                          <span className='font-mono text-sm'>{formatCredits(user.credits)}</span>
                          <span className='text-muted-foreground text-xs'>
                            已用 {formatCredits(user.used_credits)}
                          </span>
                        </div>
                      </td>
                      <td className='px-4 py-3'>
                        <span className='text-muted-foreground text-sm'>{user.email || '-'}</span>
                      </td>
                      <td className='px-4 py-3'>
                        <span className='text-muted-foreground text-sm'>{user.created_at}</span>
                      </td>
                      <td className='px-4 py-3'>
                        <div className='flex items-center gap-1'>
                          {/* 积分调整 */}
                          <button
                            onClick={() => handleCreditClick(user)}
                            disabled={isDeleted}
                            className='hover:bg-muted inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs font-medium transition-colors disabled:opacity-50'
                          >
                            <Coins className='size-3.5' />
                            积分
                          </button>

                          {/* 密码重置 */}
                          <button
                            onClick={() => handleResetClick(user)}
                            disabled={isRoot || isDeleted}
                            className='hover:bg-muted inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs font-medium transition-colors disabled:opacity-50'
                          >
                            <KeyRound className='size-3.5' />
                            密码重置
                          </button>

                          {/* 编辑 */}
                          <button
                            onClick={() => handleEdit(user)}
                            disabled={isDeleted}
                            className='hover:bg-muted inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs font-medium transition-colors disabled:opacity-50'
                          >
                            <Pencil className='size-3.5' />
                            编辑
                          </button>

                          {/* 启用/禁用 */}
                          <button
                            onClick={() => handleToggleStatus(user)}
                            disabled={isRoot || isDeleted}
                            className={cn(
                              'inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs font-medium transition-colors disabled:opacity-50',
                              user.status === 1
                                ? 'hover:bg-amber-500/10 text-amber-600 dark:text-amber-400'
                                : 'hover:bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
                            )}
                          >
                            {user.status === 1 ? (
                              <><PowerOff className='size-3.5' /> 禁用</>
                            ) : (
                              <><Power className='size-3.5' /> 启用</>
                            )}
                          </button>

                          {/* 删除 */}
                          <button
                            onClick={() => handleDeleteClick(user)}
                            disabled={isRoot || isDeleted}
                            className='text-destructive hover:bg-destructive/10 inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs font-medium transition-colors disabled:opacity-50'
                          >
                            <Trash2 className='size-3.5' />
                            删除
                          </button>
                        </div>
                      </td>
                    </tr>
                  )
                })
              )}
            </tbody>
          </table>
        </div>

        {/* 分页 */}
        {!loading && total > 0 && (
          <div className='border-border/40 flex items-center justify-between border-t px-4 py-3'>
            <div className='text-muted-foreground text-sm'>
              共 <span className='text-foreground font-medium'>{total}</span> 条记录
            </div>
            <div className='flex items-center gap-2'>
              <select
                value={pageSize}
                onChange={(e) => { setPageSize(Number(e.target.value)); setPage(1) }}
                className='border-border/60 bg-background h-8 rounded-lg border px-2 text-xs'
              >
                {PAGE_SIZE_OPTIONS.map((size) => (
                  <option key={size} value={size}>{size} 条/页</option>
                ))}
              </select>

              <button
                onClick={() => setPage(Math.max(1, page - 1))}
                disabled={page <= 1}
                className='border-border/60 hover:bg-muted inline-flex size-8 items-center justify-center rounded-lg border transition-colors disabled:opacity-50'
              >
                <ChevronLeft className='size-4' />
              </button>

              <div className='flex items-center gap-1'>
                {Array.from({ length: Math.min(5, totalPages) }, (_, i) => {
                  let pageNum: number
                  if (totalPages <= 5) {
                    pageNum = i + 1
                  } else if (page <= 3) {
                    pageNum = i + 1
                  } else if (page >= totalPages - 2) {
                    pageNum = totalPages - 4 + i
                  } else {
                    pageNum = page - 2 + i
                  }
                  return (
                    <button
                      key={pageNum}
                      onClick={() => setPage(pageNum)}
                      className={cn(
                        'inline-flex size-8 items-center justify-center rounded-lg text-sm font-medium transition-colors',
                        page === pageNum
                          ? 'bg-primary text-primary-foreground'
                          : 'hover:bg-muted'
                      )}
                    >
                      {pageNum}
                    </button>
                  )
                })}
              </div>

              <button
                onClick={() => setPage(Math.min(totalPages, page + 1))}
                disabled={page >= totalPages}
                className='border-border/60 hover:bg-muted inline-flex size-8 items-center justify-center rounded-lg border transition-colors disabled:opacity-50'
              >
                <ChevronRight className='size-4' />
              </button>
            </div>
          </div>
        )}
      </div>

      {/* 添加/编辑抽屉 */}
      <UserDrawer
        open={drawerOpen}
        onOpenChange={setDrawerOpen}
        currentRow={editRow}
        onSubmit={handleDrawerSubmit}
      />

      {/* 删除确认弹窗 */}
      <ConfirmDialog
        open={deleteDialogOpen}
        onOpenChange={setDeleteDialogOpen}
        title='确认删除'
        description={
          <>
            确定要删除用户 <span className='text-foreground font-semibold'>{deleteRow?.username}</span> 吗？此操作不可撤销。
          </>
        }
        confirmText='删除'
        destructive
        loading={actionLoading}
        onConfirm={handleDeleteConfirm}
      />

      {/* 积分调整弹窗 */}
      <CreditDialog
        open={creditDialogOpen}
        onOpenChange={setCreditDialogOpen}
        username={creditRow?.username || ''}
        currentCredits={creditRow?.credits || 0}
        loading={actionLoading}
        onConfirm={handleCreditConfirm}
      />

      {/* 密码重置确认弹窗 */}
      <ConfirmDialog
        open={resetDialogOpen}
        onOpenChange={setResetDialogOpen}
        title='重置密码'
        description={
          <>
            确定要重置用户 <span className='text-foreground font-semibold'>{resetRow?.username}</span> 的密码吗？系统将自动生成随机密码。
          </>
        }
        confirmText='重置密码'
        destructive
        loading={actionLoading}
        onConfirm={handleResetConfirm}
      />

      {/* 密码重置结果弹窗 */}
      {resetResultOpen && (
        <div className='fixed inset-0 z-[100] flex items-center justify-center'>
          <div className='bg-background/80 fixed inset-0 backdrop-blur-sm' onClick={() => setResetResultOpen(false)} />
          <div className='bg-background border-border/60 relative z-10 w-full max-w-md rounded-xl border p-6 shadow-lg'>
            <h2 className='text-lg font-semibold'>密码重置成功</h2>
            <p className='text-muted-foreground mt-2 text-sm'>
              用户 <span className='text-foreground font-medium'>{resetRow?.username}</span> 的新密码：
            </p>
            <div className='bg-muted/30 mt-4 flex items-center gap-2 rounded-lg px-4 py-3'>
              <code className='flex-1 font-mono text-sm break-all select-all'>{resetPassword}</code>
              <button
                onClick={handleCopyPassword}
                className='hover:bg-muted shrink-0 rounded-lg p-1.5 transition-colors'
                title='复制密码'
              >
                {copied ? (
                  <Check className='size-4 text-emerald-500' />
                ) : (
                  <Copy className='size-4 text-muted-foreground' />
                )}
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
