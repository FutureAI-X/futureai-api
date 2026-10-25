import { useState, useEffect, useCallback } from 'react'
import {
  Plus,
  Pencil,
  Trash2,
  Power,
  PowerOff,
  Loader2,
  X,
  Globe,
} from 'lucide-react'
import { cn } from '../../lib/utils'
import {
  getEndpoints,
  createEndpoint,
  updateEndpoint,
  updateEndpointStatus,
  deleteEndpoint,
  type Endpoint,
} from '../../api/endpoint'
import { ConfirmDialog } from '../../components/admin/ConfirmDialog'

const STATUS_CONFIG: Record<number, { label: string; className: string }> = {
  1: { label: '正常', className: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400' },
  2: { label: '已禁用', className: 'bg-red-500/10 text-red-600 dark:text-red-400' },
}

export function AdminEndpoints() {
  const [endpoints, setEndpoints] = useState<Endpoint[]>([])
  const [loading, setLoading] = useState(true)

  const [drawerOpen, setDrawerOpen] = useState(false)
  const [editRow, setEditRow] = useState<Endpoint | null>(null)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteRow, setDeleteRow] = useState<Endpoint | null>(null)
  const [deleteError, setDeleteError] = useState('')
  const [actionLoading, setActionLoading] = useState(false)

  const [formPath, setFormPath] = useState('')
  const [formName, setFormName] = useState('')
  const [formDesc, setFormDesc] = useState('')
  const [formError, setFormError] = useState('')
  const [formSaving, setFormSaving] = useState(false)

  const loadEndpoints = useCallback(async () => {
    setLoading(true)
    try {
      const res = await getEndpoints()
      if (res.success) setEndpoints(res.data || [])
    } catch { /* ignore */ }
    finally { setLoading(false) }
  }, [])

  useEffect(() => { loadEndpoints() }, [loadEndpoints])

  const openCreate = () => {
    setEditRow(null)
    setFormPath(''); setFormName(''); setFormDesc(''); setFormError('')
    setDrawerOpen(true)
  }

  const openEdit = (ep: Endpoint) => {
    setEditRow(ep)
    setFormPath(ep.path); setFormName(ep.name); setFormDesc(ep.description); setFormError('')
    setDrawerOpen(true)
  }

  const handleSave = async () => {
    if (!formPath.trim()) { setFormError('端点路径不能为空'); return }
    if (!formName.trim()) { setFormError('端点名称不能为空'); return }
    setFormSaving(true); setFormError('')
    try {
      if (editRow) {
        const data: Record<string, string> = {}
        if (formPath !== editRow.path) data.path = formPath
        if (formName !== editRow.name) data.name = formName
        if (formDesc !== editRow.description) data.description = formDesc
        const res = await updateEndpoint(editRow.id, data)
        if (!res.success) { setFormError(res.message || '更新失败'); return }
      } else {
        const res = await createEndpoint({ path: formPath, name: formName, description: formDesc })
        if (!res.success) { setFormError(res.message || '创建失败'); return }
      }
      setDrawerOpen(false); loadEndpoints()
    } catch { setFormError('网络错误') }
    finally { setFormSaving(false) }
  }

  const handleToggle = async (ep: Endpoint) => {
    setActionLoading(true)
    try { await updateEndpointStatus(ep.id, ep.status === 1 ? 2 : 1); loadEndpoints() }
    catch { /* ignore */ }
    finally { setActionLoading(false) }
  }

  const handleDeleteConfirm = async () => {
    if (!deleteRow) return
    setActionLoading(true)
    setDeleteError('')
    try {
      const res = await deleteEndpoint(deleteRow.id)
      if (!res.success) { setDeleteError(res.message || '删除失败'); return }
      setDeleteOpen(false); setDeleteRow(null); loadEndpoints()
    } catch { setDeleteError('网络错误') }
    finally { setActionLoading(false) }
  }

  useEffect(() => {
    if (drawerOpen || deleteOpen) document.body.style.overflow = 'hidden'
    else document.body.style.overflow = ''
    return () => { document.body.style.overflow = '' }
  }, [drawerOpen, deleteOpen])

  useEffect(() => {
    const handleEsc = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      if (deleteOpen) setDeleteOpen(false)
      else if (drawerOpen) setDrawerOpen(false)
    }
    document.addEventListener('keydown', handleEsc)
    return () => document.removeEventListener('keydown', handleEsc)
  }, [drawerOpen, deleteOpen])

  return (
    <div data-page-fill className='flex h-full min-h-0 flex-col gap-6'>
      <div className='flex shrink-0 items-center justify-between'>
        <div>
          <h2 className='text-2xl font-bold tracking-tight'>端点管理</h2>
          <p className='text-muted-foreground mt-1 text-sm'>管理 API 端点</p>
        </div>
        <button onClick={openCreate}
          className='bg-primary hover:bg-primary/90 inline-flex h-9 items-center justify-center gap-2 rounded-lg px-4 text-sm font-medium text-white transition-colors'>
          <Plus className='size-4' /> 添加端点
        </button>
      </div>

      <div className='border-border/60 flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border'>
        <div className='min-h-0 flex-1 overflow-auto'>
          <table className='w-full text-sm'>
            <thead className='bg-muted sticky top-0 z-10'>
              <tr className='bg-muted/30 border-border/40 border-b'>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>ID</th>
                <th className='text-muted-foreground min-w-[200px] px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>端点路径</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>端点名称</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>描述</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>状态</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>操作</th>
              </tr>
            </thead>
            <tbody className='divide-border/40 divide-y'>
              {loading ? (
                <tr><td colSpan={6} className='px-4 py-12 text-center'><Loader2 className='text-muted-foreground mx-auto size-6 animate-spin' /></td></tr>
              ) : endpoints.length === 0 ? (
                <tr><td colSpan={6} className='px-4 py-12 text-center'><p className='text-muted-foreground text-sm'>暂无端点</p></td></tr>
              ) : endpoints.map((ep) => {
                const statusConf = STATUS_CONFIG[ep.status] || STATUS_CONFIG[1]
                return (
                  <tr key={ep.id} className={cn('hover:bg-muted/20 transition-colors', ep.status === 2 && 'opacity-50')}>
                    <td className='px-4 py-3'><span className='text-muted-foreground font-mono text-xs'>{ep.id}</span></td>
                    <td className='px-4 py-3'>
                      <div className='flex items-center gap-2'>
                        <Globe className='text-muted-foreground size-4 shrink-0' />
                        <code className='font-mono text-xs break-all'>{ep.path}</code>
                      </div>
                    </td>
                    <td className='px-4 py-3'><span className='font-medium'>{ep.name}</span></td>
                    <td className='px-4 py-3'><span className='text-muted-foreground text-sm'>{ep.description || '-'}</span></td>
                    <td className='px-4 py-3'>
                      <span className={cn('inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-medium', statusConf.className)}>
                        <span className={cn('size-1.5 rounded-full', ep.status === 1 ? 'bg-emerald-500' : 'bg-red-500')} />
                        {statusConf.label}
                      </span>
                    </td>
                    <td className='px-4 py-3'>
                      <div className='flex items-center gap-1'>
                        <button onClick={() => openEdit(ep)} className='hover:bg-muted inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs font-medium transition-colors'>
                          <Pencil className='size-3.5' /> 编辑
                        </button>
                        <button onClick={() => handleToggle(ep)}
                          className={cn('inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs font-medium transition-colors',
                            ep.status === 1 ? 'hover:bg-amber-500/10 text-amber-600 dark:text-amber-400' : 'hover:bg-emerald-500/10 text-emerald-600 dark:text-emerald-400')}>
                          {ep.status === 1 ? <><PowerOff className='size-3.5' /> 禁用</> : <><Power className='size-3.5' /> 启用</>}
                        </button>
                        <button onClick={() => { setDeleteRow(ep); setDeleteError(''); setDeleteOpen(true) }}
                          className='text-destructive hover:bg-destructive/10 inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs font-medium transition-colors'>
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

      {/* 新增/编辑弹框 */}
      {drawerOpen && (
        <div className='fixed inset-0 z-[100] flex items-center justify-center'>
          <div className='bg-background/80 fixed inset-0 backdrop-blur-sm' onClick={() => setDrawerOpen(false)} />
          <div className='bg-background border-border/60 relative z-10 w-full max-w-lg rounded-xl border shadow-lg'>
            <div className='flex items-center justify-between border-b border-border/40 px-6 py-4'>
              <h2 className='text-lg font-semibold'>{editRow ? '编辑端点' : '添加端点'}</h2>
              <button onClick={() => setDrawerOpen(false)} className='hover:bg-muted rounded-lg p-1.5 transition-colors'><X className='size-4' /></button>
            </div>
            <div className='p-6 space-y-4'>
              {formError && <div className='bg-destructive/10 text-destructive rounded-lg px-4 py-3 text-sm'>{formError}</div>}
              <div className='space-y-2'>
                <label className='text-sm font-medium'>端点路径</label>
                <input type='text' value={formPath} onChange={(e) => setFormPath(e.target.value)} placeholder='例如: /v1/chat/completions'
                  className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none' />
              </div>
              <div className='space-y-2'>
                <label className='text-sm font-medium'>端点名称</label>
                <input type='text' value={formName} onChange={(e) => setFormName(e.target.value)} placeholder='例如: Chat Completions'
                  className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none' />
              </div>
              <div className='space-y-2'>
                <label className='text-sm font-medium'>描述</label>
                <input type='text' value={formDesc} onChange={(e) => setFormDesc(e.target.value)} placeholder='可选'
                  className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none' />
              </div>
            </div>
            <div className='flex justify-end gap-3 border-t border-border/40 px-6 py-4'>
              <button onClick={() => setDrawerOpen(false)} disabled={formSaving}
                className='border-border/60 hover:bg-muted inline-flex h-9 items-center justify-center rounded-lg border px-4 text-sm font-medium transition-colors'>取消</button>
              <button onClick={handleSave} disabled={formSaving}
                className='bg-primary hover:bg-primary/90 inline-flex h-9 items-center justify-center gap-2 rounded-lg px-4 text-sm font-medium text-white transition-colors disabled:opacity-50'>
                {formSaving && <Loader2 className='size-4 animate-spin' />}
                {formSaving ? '保存中...' : '保存'}
              </button>
            </div>
          </div>
        </div>
      )}

      <ConfirmDialog open={deleteOpen} onOpenChange={setDeleteOpen} title='确认删除'
        description={<>
          确定要删除端点 <span className='text-foreground font-semibold'>{deleteRow?.name}</span> 吗？
          {deleteError && <span className='text-destructive mt-2 block text-sm'>{deleteError}</span>}
        </>}
        confirmText='删除' destructive loading={actionLoading} onConfirm={handleDeleteConfirm} />
    </div>
  )
}
