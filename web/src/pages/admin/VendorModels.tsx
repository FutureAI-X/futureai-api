import { useState, useEffect, useCallback } from 'react'
import {
  Plus,
  Pencil,
  Trash2,
  Power,
  PowerOff,
  Loader2,
  X,
  Link2,
} from 'lucide-react'
import { cn } from '../../lib/utils'
import {
  getVendorModels,
  createVendorModel,
  updateVendorModel,
  updateVendorModelStatus,
  deleteVendorModel,
  type VendorModel,
} from '../../api/vendor-model'
import { getVendors, type Vendor } from '../../api/vendor'
import { getModels, type AdminModel } from '../../api/admin-model'
import { ConfirmDialog } from '../../components/admin/ConfirmDialog'

const STATUS_CONFIG: Record<number, { label: string; className: string }> = {
  1: { label: '正常', className: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400' },
  2: { label: '已禁用', className: 'bg-red-500/10 text-red-600 dark:text-red-400' },
}

export function AdminVendorModels() {
  const [items, setItems] = useState<VendorModel[]>([])
  const [vendors, setVendors] = useState<Vendor[]>([])
  const [models, setModels] = useState<AdminModel[]>([])
  const [loading, setLoading] = useState(true)

  const [drawerOpen, setDrawerOpen] = useState(false)
  const [editRow, setEditRow] = useState<VendorModel | null>(null)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteRow, setDeleteRow] = useState<VendorModel | null>(null)
  const [actionLoading, setActionLoading] = useState(false)

  const [formVendor, setFormVendor] = useState('')
  const [formModel, setFormModel] = useState('')
  const [formVendorModelId, setFormVendorModelId] = useState('')
  const [formError, setFormError] = useState('')
  const [formSaving, setFormSaving] = useState(false)

  const loadAll = useCallback(async () => {
    setLoading(true)
    try {
      const [vmRes, vRes, mRes] = await Promise.all([getVendorModels(), getVendors(), getModels()])
      if (vmRes.success) setItems(vmRes.data || [])
      if (vRes.success) setVendors(vRes.data || [])
      if (mRes.success) setModels(mRes.data || [])
    } catch { /* ignore */ }
    finally { setLoading(false) }
  }, [])

  useEffect(() => { loadAll() }, [loadAll])

  const openCreate = () => {
    setEditRow(null)
    setFormVendor(''); setFormModel(''); setFormVendorModelId(''); setFormError('')
    setDrawerOpen(true)
  }

  const openEdit = (vm: VendorModel) => {
    setEditRow(vm)
    setFormVendor(String(vm.vendor_id)); setFormModel(String(vm.model_id))
    setFormVendorModelId(vm.vendor_model_id); setFormError('')
    setDrawerOpen(true)
  }

  const handleSave = async () => {
    if (!formVendor) { setFormError('请选择供应商'); return }
    if (!formModel) { setFormError('请选择模型'); return }
    if (!formVendorModelId.trim()) { setFormError('供应商模型ID不能为空'); return }

    setFormSaving(true); setFormError('')
    try {
      if (editRow) {
        const data: Record<string, string | number> = {}
        if (Number(formVendor) !== editRow.vendor_id) data.vendor_id = Number(formVendor)
        if (Number(formModel) !== editRow.model_id) data.model_id = Number(formModel)
        if (formVendorModelId !== editRow.vendor_model_id) data.vendor_model_id = formVendorModelId
        const res = await updateVendorModel(editRow.id, data)
        if (!res.success) { setFormError(res.message || '更新失败'); return }
      } else {
        const res = await createVendorModel({
          vendor_id: Number(formVendor), model_id: Number(formModel),
          vendor_model_id: formVendorModelId,
        })
        if (!res.success) { setFormError(res.message || '创建失败'); return }
      }
      setDrawerOpen(false); loadAll()
    } catch { setFormError('网络错误') }
    finally { setFormSaving(false) }
  }

  const handleToggle = async (vm: VendorModel) => {
    setActionLoading(true)
    try { await updateVendorModelStatus(vm.id, vm.status === 1 ? 2 : 1); loadAll() }
    catch { /* ignore */ }
    finally { setActionLoading(false) }
  }

  const handleDeleteConfirm = async () => {
    if (!deleteRow) return
    setActionLoading(true)
    try { await deleteVendorModel(deleteRow.id); setDeleteOpen(false); setDeleteRow(null); loadAll() }
    catch { /* ignore */ }
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
          <h2 className='text-2xl font-bold tracking-tight'>供应商模型</h2>
          <p className='text-muted-foreground mt-1 text-sm'>管理供应商与模型的关联关系</p>
        </div>
        <button onClick={openCreate}
          className='bg-primary hover:bg-primary/90 inline-flex h-9 items-center justify-center gap-2 rounded-lg px-4 text-sm font-medium text-white transition-colors'>
          <Plus className='size-4' /> 添加关联
        </button>
      </div>

      <div className='border-border/60 flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border'>
        <div className='min-h-0 flex-1 overflow-auto'>
          <table className='w-full text-sm'>
            <thead className='bg-muted sticky top-0 z-10'>
              <tr className='bg-muted/30 border-border/40 border-b'>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>ID</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>供应商</th>
                <th className='text-muted-foreground min-w-[200px] px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>供应商模型ID</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>模型</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>状态</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>操作</th>
              </tr>
            </thead>
            <tbody className='divide-border/40 divide-y'>
              {loading ? (
                <tr><td colSpan={6} className='px-4 py-12 text-center'><Loader2 className='text-muted-foreground mx-auto size-6 animate-spin' /></td></tr>
              ) : items.length === 0 ? (
                <tr><td colSpan={6} className='px-4 py-12 text-center'><p className='text-muted-foreground text-sm'>暂无关联</p></td></tr>
              ) : items.map((vm) => {
                const statusConf = STATUS_CONFIG[vm.status] || STATUS_CONFIG[1]
                return (
                  <tr key={vm.id} className={cn('hover:bg-muted/20 transition-colors', vm.status === 2 && 'opacity-50')}>
                    <td className='px-4 py-3'><span className='text-muted-foreground font-mono text-xs'>{vm.id}</span></td>
                    <td className='px-4 py-3'>
                      <span className='inline-flex items-center gap-1.5 rounded-md bg-blue-500/10 px-2 py-0.5 text-xs font-medium text-blue-600 dark:text-blue-400'>
                        <Link2 className='size-3' />
                        {vm.vendor_name || `#${vm.vendor_id}`}
                      </span>
                    </td>
                    <td className='px-4 py-3'>
                      <code className='font-mono text-xs break-all'>{vm.vendor_model_id}</code>
                    </td>
                    <td className='px-4 py-3'>
                      <span className='font-medium'>{vm.model_name || `#${vm.model_id}`}</span>
                    </td>
                    <td className='px-4 py-3'>
                      <span className={cn('inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-medium', statusConf.className)}>
                        <span className={cn('size-1.5 rounded-full', vm.status === 1 ? 'bg-emerald-500' : 'bg-red-500')} />
                        {statusConf.label}
                      </span>
                    </td>
                    <td className='px-4 py-3'>
                      <div className='flex items-center gap-1'>
                        <button onClick={() => openEdit(vm)} className='hover:bg-muted inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs font-medium transition-colors'>
                          <Pencil className='size-3.5' /> 编辑
                        </button>
                        <button onClick={() => handleToggle(vm)}
                          className={cn('inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs font-medium transition-colors',
                            vm.status === 1 ? 'hover:bg-amber-500/10 text-amber-600 dark:text-amber-400' : 'hover:bg-emerald-500/10 text-emerald-600 dark:text-emerald-400')}>
                          {vm.status === 1 ? <><PowerOff className='size-3.5' /> 禁用</> : <><Power className='size-3.5' /> 启用</>}
                        </button>
                        <button onClick={() => { setDeleteRow(vm); setDeleteOpen(true) }}
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
              <h2 className='text-lg font-semibold'>{editRow ? '编辑关联' : '添加关联'}</h2>
              <button onClick={() => setDrawerOpen(false)} className='hover:bg-muted rounded-lg p-1.5 transition-colors'><X className='size-4' /></button>
            </div>
            <div className='p-6 space-y-4'>
              {formError && <div className='bg-destructive/10 text-destructive rounded-lg px-4 py-3 text-sm'>{formError}</div>}

              <div className='space-y-2'>
                <label className='text-sm font-medium'>供应商</label>
                <select value={formVendor} onChange={(e) => setFormVendor(e.target.value)}
                  className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'>
                  <option value=''>请选择供应商</option>
                  {vendors.filter(v => v.status === 1).map(v => (
                    <option key={v.id} value={v.id}>{v.name}</option>
                  ))}
                </select>
              </div>

              <div className='space-y-2'>
                <label className='text-sm font-medium'>供应商模型ID</label>
                <input type='text' value={formVendorModelId} onChange={(e) => setFormVendorModelId(e.target.value)} placeholder='供应商侧的模型标识'
                  className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none' />
              </div>

              <div className='space-y-2'>
                <label className='text-sm font-medium'>模型</label>
                <select value={formModel} onChange={(e) => setFormModel(e.target.value)}
                  className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'>
                  <option value=''>请选择模型</option>
                  {models.filter(m => m.status === 1).map(m => (
                    <option key={m.id} value={m.id}>{m.name} ({m.owner})</option>
                  ))}
                </select>
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
        description={<>确定要删除此关联吗？</>}
        confirmText='删除' destructive loading={actionLoading} onConfirm={handleDeleteConfirm} />
    </div>
  )
}
