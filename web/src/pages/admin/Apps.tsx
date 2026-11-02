import { useState, useEffect, useCallback } from 'react'
import {
  Plus,
  Pencil,
  Trash2,
  Power,
  PowerOff,
  Loader2,
  X,
} from 'lucide-react'
import { cn } from '../../lib/utils'
import {
  getApps,
  createApp,
  updateApp,
  updateAppStatus,
  deleteApp,
  type AdminApp,
} from '../../api/admin-app'
import { ConfirmDialog } from '../../components/admin/ConfirmDialog'

const STATUS_CONFIG: Record<number, { label: string; className: string }> = {
  1: { label: '启用', className: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400' },
  2: { label: '已禁用', className: 'bg-red-500/10 text-red-600 dark:text-red-400' },
}

// 与后端 ValidateAppKey 保持一致：客户端用这个标识查询更新，错一个字符就查不到
const APP_KEY_PATTERN = /^[a-z0-9][a-z0-9-]{0,63}$/

export function AdminApps() {
  const [apps, setApps] = useState<AdminApp[]>([])
  const [loading, setLoading] = useState(true)
  const [pageError, setPageError] = useState('')

  // 弹框状态
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [editRow, setEditRow] = useState<AdminApp | null>(null)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [deleteRow, setDeleteRow] = useState<AdminApp | null>(null)
  const [actionLoading, setActionLoading] = useState(false)

  // 表单状态
  const [formKey, setFormKey] = useState('')
  const [formName, setFormName] = useState('')
  const [formDesc, setFormDesc] = useState('')
  const [formError, setFormError] = useState('')
  const [formSaving, setFormSaving] = useState(false)

  const loadApps = useCallback(async () => {
    setLoading(true)
    try {
      const res = await getApps()
      if (res.success) setApps(res.data || [])
    } catch { /* ignore */ }
    finally { setLoading(false) }
  }, [])

  useEffect(() => { loadApps() }, [loadApps])

  const handleCreate = () => {
    setEditRow(null)
    setFormKey('')
    setFormName('')
    setFormDesc('')
    setFormError('')
    setDrawerOpen(true)
  }

  const handleEdit = (app: AdminApp) => {
    setEditRow(app)
    setFormKey(app.key)
    setFormName(app.name)
    setFormDesc(app.description || '')
    setFormError('')
    setDrawerOpen(true)
  }

  const handleSave = async () => {
    const key = formKey.trim()
    const name = formName.trim()

    if (!editRow) {
      if (!APP_KEY_PATTERN.test(key)) {
        setFormError('应用标识只能包含小写字母、数字和连字符，且不能超过 64 个字符')
        return
      }
    }
    if (!name) { setFormError('应用名称不能为空'); return }

    setFormSaving(true)
    setFormError('')

    try {
      if (editRow) {
        const data: Record<string, string> = {}
        if (name !== editRow.name) data.name = name
        if (formDesc !== (editRow.description || '')) data.description = formDesc
        if (Object.keys(data).length === 0) { setDrawerOpen(false); return }

        const res = await updateApp(editRow.id, data)
        if (!res.success) { setFormError(res.message || '更新失败'); return }
      } else {
        const res = await createApp({ key, name, description: formDesc })
        if (!res.success) { setFormError(res.message || '创建失败'); return }
      }
      setDrawerOpen(false)
      loadApps()
    } catch {
      setFormError('网络错误')
    } finally {
      setFormSaving(false)
    }
  }

  const handleToggle = async (app: AdminApp) => {
    setActionLoading(true)
    setPageError('')
    try {
      const res = await updateAppStatus(app.id, app.status === 1 ? 2 : 1)
      if (!res.success) setPageError(res.message || '更新状态失败')
      loadApps()
    } catch {
      setPageError('网络错误')
    } finally {
      setActionLoading(false)
    }
  }

  const handleDeleteConfirm = async () => {
    if (!deleteRow) return
    setActionLoading(true)
    setPageError('')
    try {
      const res = await deleteApp(deleteRow.id)
      if (!res.success) {
        setPageError(res.message || '删除失败')
      }
      setDeleteDialogOpen(false)
      setDeleteRow(null)
      loadApps()
    } catch {
      setPageError('网络错误')
    } finally {
      setActionLoading(false)
    }
  }

  useEffect(() => {
    if (drawerOpen || deleteDialogOpen) {
      document.body.style.overflow = 'hidden'
    } else {
      document.body.style.overflow = ''
    }
    return () => { document.body.style.overflow = '' }
  }, [drawerOpen, deleteDialogOpen])

  useEffect(() => {
    const handleEsc = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        if (deleteDialogOpen) setDeleteDialogOpen(false)
        else if (drawerOpen) setDrawerOpen(false)
      }
    }
    document.addEventListener('keydown', handleEsc)
    return () => document.removeEventListener('keydown', handleEsc)
  }, [drawerOpen, deleteDialogOpen])

  return (
    <div data-page-fill className='flex h-full min-h-0 flex-col gap-6'>
      <div className='flex shrink-0 items-center justify-between'>
        <div>
          <h2 className='text-2xl font-bold tracking-tight'>应用管理</h2>
          <p className='text-muted-foreground mt-1 text-sm'>
            登记需要检查更新的客户端。应用标识由客户端硬编码，创建后不可修改
          </p>
        </div>
        <button
          onClick={handleCreate}
          className='bg-primary hover:bg-primary/90 inline-flex h-9 items-center justify-center gap-2 rounded-lg px-4 text-sm font-medium text-white transition-colors'
        >
          <Plus className='size-4' />
          添加应用
        </button>
      </div>

      {pageError && (
        <div className='bg-destructive/10 text-destructive shrink-0 rounded-lg px-4 py-3 text-sm'>{pageError}</div>
      )}

      <div className='border-border/60 flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border'>
        <div className='min-h-0 flex-1 overflow-auto'>
          <table className='w-full text-sm'>
            <thead className='bg-muted sticky top-0 z-10'>
              <tr className='bg-muted/30 border-border/40 border-b'>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>ID</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>应用标识</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>名称</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>状态</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>创建时间</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>操作</th>
              </tr>
            </thead>
            <tbody className='divide-border/40 divide-y'>
              {loading ? (
                <tr>
                  <td colSpan={6} className='px-4 py-12 text-center'>
                    <Loader2 className='text-muted-foreground mx-auto size-6 animate-spin' />
                  </td>
                </tr>
              ) : apps.length === 0 ? (
                <tr>
                  <td colSpan={6} className='px-4 py-12 text-center'>
                    <p className='text-muted-foreground text-sm'>暂无应用，先添加一个再发布版本</p>
                  </td>
                </tr>
              ) : apps.map((app) => {
                const statusConf = STATUS_CONFIG[app.status] || STATUS_CONFIG[1]
                return (
                  <tr key={app.id} className={cn('hover:bg-muted/20 transition-colors', app.status === 2 && 'opacity-50')}>
                    <td className='px-4 py-3'>
                      <span className='text-muted-foreground font-mono text-xs'>{app.id}</span>
                    </td>
                    <td className='px-4 py-3'>
                      <code className='font-mono text-xs'>{app.key}</code>
                    </td>
                    <td className='px-4 py-3'>
                      <div className='flex flex-col gap-0.5'>
                        <span className='font-medium'>{app.name}</span>
                        {app.description && (
                          <span className='text-muted-foreground text-xs'>{app.description}</span>
                        )}
                      </div>
                    </td>
                    <td className='px-4 py-3'>
                      <span className={cn('inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-medium', statusConf.className)}>
                        <span className={cn('size-1.5 rounded-full', app.status === 1 ? 'bg-emerald-500' : 'bg-red-500')} />
                        {statusConf.label}
                      </span>
                    </td>
                    <td className='px-4 py-3'>
                      <span className='text-muted-foreground text-sm'>{app.created_at}</span>
                    </td>
                    <td className='px-4 py-3'>
                      <div className='flex items-center gap-1'>
                        <button onClick={() => handleEdit(app)} className='hover:bg-muted inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs font-medium transition-colors'>
                          <Pencil className='size-3.5' /> 编辑
                        </button>
                        <button
                          onClick={() => handleToggle(app)}
                          className={cn(
                            'inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs font-medium transition-colors',
                            app.status === 1
                              ? 'hover:bg-amber-500/10 text-amber-600 dark:text-amber-400'
                              : 'hover:bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
                          )}
                        >
                          {app.status === 1 ? <><PowerOff className='size-3.5' /> 禁用</> : <><Power className='size-3.5' /> 启用</>}
                        </button>
                        <button
                          onClick={() => { setDeleteRow(app); setDeleteDialogOpen(true) }}
                          className='text-destructive hover:bg-destructive/10 inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs font-medium transition-colors'
                        >
                          <Trash2 className='size-3.5' /> 删除
                        </button>
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      </div>

      {drawerOpen && (
        <div className='fixed inset-0 z-[100] flex items-center justify-center'>
          <div className='bg-background/80 fixed inset-0 backdrop-blur-sm' onClick={() => setDrawerOpen(false)} />
          <div className='bg-background border-border/60 relative z-10 w-full max-w-lg rounded-xl border shadow-lg'>
            <div className='flex items-center justify-between border-b border-border/40 px-6 py-4'>
              <h2 className='text-lg font-semibold'>{editRow ? '编辑应用' : '添加应用'}</h2>
              <button onClick={() => setDrawerOpen(false)} className='hover:bg-muted rounded-lg p-1.5 transition-colors'>
                <X className='size-4' />
              </button>
            </div>

            <div className='p-6 space-y-4'>
              {formError && (
                <div className='bg-destructive/10 text-destructive rounded-lg px-4 py-3 text-sm'>{formError}</div>
              )}

              <div className='space-y-2'>
                <label className='text-sm font-medium'>应用标识</label>
                <input
                  type='text'
                  value={formKey}
                  onChange={(e) => setFormKey(e.target.value)}
                  placeholder='例如: futureai-image'
                  disabled={!!editRow}
                  className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 font-mono text-sm focus-visible:ring-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60'
                />
                <p className='text-muted-foreground text-xs'>
                  {editRow
                    ? '标识创建后不可修改：改了等于让已安装的客户端查不到更新'
                    : '仅小写字母、数字和连字符。必须与客户端代码中写死的标识完全一致'}
                </p>
              </div>

              <div className='space-y-2'>
                <label className='text-sm font-medium'>名称</label>
                <input
                  type='text'
                  value={formName}
                  onChange={(e) => setFormName(e.target.value)}
                  placeholder='例如: FutureAI 生图'
                  className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                />
              </div>

              <div className='space-y-2'>
                <label className='text-sm font-medium'>备注</label>
                <input
                  type='text'
                  value={formDesc}
                  onChange={(e) => setFormDesc(e.target.value)}
                  placeholder='可选'
                  className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                />
              </div>
            </div>

            <div className='flex justify-end gap-3 border-t border-border/40 px-6 py-4'>
              <button onClick={() => setDrawerOpen(false)} disabled={formSaving}
                className='border-border/60 hover:bg-muted inline-flex h-9 items-center justify-center rounded-lg border px-4 text-sm font-medium transition-colors'>
                取消
              </button>
              <button onClick={handleSave} disabled={formSaving}
                className='bg-primary hover:bg-primary/90 inline-flex h-9 items-center justify-center gap-2 rounded-lg px-4 text-sm font-medium text-white transition-colors disabled:opacity-50'>
                {formSaving && <Loader2 className='size-4 animate-spin' />}
                {formSaving ? '保存中...' : '保存'}
              </button>
            </div>
          </div>
        </div>
      )}

      <ConfirmDialog
        open={deleteDialogOpen}
        onOpenChange={setDeleteDialogOpen}
        title='确认删除'
        description={<>确定要删除应用 <span className='text-foreground font-semibold'>{deleteRow?.name}</span> 吗？<br />应用下仍有版本记录时无法删除，可改为禁用。</>}
        confirmText='删除'
        destructive
        loading={actionLoading}
        onConfirm={handleDeleteConfirm}
      />
    </div>
  )
}
