import { useState, useEffect, useCallback } from 'react'
import {
  Plus,
  Pencil,
  Trash2,
  Copy,
  Check,
  Loader2,
  Key,
  X,
} from 'lucide-react'
import { decryptWithKey } from '../../lib/crypto'
import {
  getTokens,
  createToken,
  updateToken,
  deleteToken,
  type ApiKey,
} from '../../api/token'
import { ConfirmDialog } from '../../components/admin/ConfirmDialog'

export function ApiKeys() {
  const [keys, setKeys] = useState<ApiKey[]>([])
  const [loading, setLoading] = useState(true)

  // 创建
  const [createOpen, setCreateOpen] = useState(false)
  const [createName, setCreateName] = useState('')
  const [createSaving, setCreateSaving] = useState(false)
  const [createError, setCreateError] = useState('')

  // 创建结果
  const [resultOpen, setResultOpen] = useState(false)
  const [resultKey, setResultKey] = useState('')
  const [copied, setCopied] = useState(false)

  // 编辑
  const [editOpen, setEditOpen] = useState(false)
  const [editRow, setEditRow] = useState<ApiKey | null>(null)
  const [editName, setEditName] = useState('')
  const [editSaving, setEditSaving] = useState(false)

  // 删除
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteRow, setDeleteRow] = useState<ApiKey | null>(null)
  const [actionLoading, setActionLoading] = useState(false)

  const loadKeys = useCallback(async () => {
    setLoading(true)
    try {
      // 列表只返回前后几位：完整 key 存的是哈希，服务端自己也还原不出来
      const res = await getTokens()
      if (res.success) setKeys(res.data || [])
    } catch { /* ignore */ }
    finally { setLoading(false) }
  }, [])

  useEffect(() => { loadKeys() }, [loadKeys])

  // ── 创建 ──
  const handleCreate = () => {
    setCreateName('')
    setCreateError('')
    setCreateOpen(true)
  }

  const handleCreateConfirm = async () => {
    if (!createName.trim()) { setCreateError('名称不能为空'); return }
    setCreateSaving(true)
    setCreateError('')
    try {
      const dataKey = localStorage.getItem('data_key') || ''
      const res = await createToken(createName.trim(), dataKey)
      if (res.success && res.data) {
        const plain = await decryptWithKey(res.data.key, dataKey)
        setResultKey(plain)
        setCopied(false)
        setCreateOpen(false)
        setResultOpen(true)
        loadKeys()
      } else {
        setCreateError(res.message || '创建失败')
      }
    } catch {
      setCreateError('网络错误')
    } finally {
      setCreateSaving(false)
    }
  }

  // ── 编辑 ──
  const handleEdit = (k: ApiKey) => {
    setEditRow(k)
    setEditName(k.name)
    setEditOpen(true)
  }

  const handleEditConfirm = async () => {
    if (!editRow || !editName.trim()) return
    setEditSaving(true)
    try {
      await updateToken(editRow.id, editName.trim())
      setEditOpen(false)
      loadKeys()
    } catch { /* ignore */ }
    finally { setEditSaving(false) }
  }

  // ── 删除 ──
  const handleDeleteConfirm = async () => {
    if (!deleteRow) return
    setActionLoading(true)
    try {
      await deleteToken(deleteRow.id)
      setDeleteOpen(false)
      setDeleteRow(null)
      loadKeys()
    } catch { /* ignore */ }
    finally { setActionLoading(false) }
  }

  // ── 复制 ──
  const handleCopy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch { /* ignore */ }
  }

  // ── ESC ──
  useEffect(() => {
    const anyOpen = createOpen || editOpen || deleteOpen || resultOpen
    if (anyOpen) document.body.style.overflow = 'hidden'
    else document.body.style.overflow = ''
    return () => { document.body.style.overflow = '' }
  }, [createOpen, editOpen, deleteOpen, resultOpen])

  useEffect(() => {
    const handleEsc = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      if (resultOpen) setResultOpen(false)
      else if (createOpen) setCreateOpen(false)
      else if (editOpen) setEditOpen(false)
      else if (deleteOpen) setDeleteOpen(false)
    }
    document.addEventListener('keydown', handleEsc)
    return () => document.removeEventListener('keydown', handleEsc)
  }, [createOpen, editOpen, deleteOpen, resultOpen])

  // 列表展示：服务端只给前后各几位，中间无法还原
  const formatKey = (k: ApiKey) => {
    if (!k.key_prefix && !k.key_suffix) return '—'
    return k.key_suffix ? `${k.key_prefix}…${k.key_suffix}` : k.key_prefix
  }

  return (
    <div className='space-y-6'>
      {/* 标题 */}
      <div className='flex items-center justify-between'>
        <div>
          <h2 className='text-2xl font-bold tracking-tight'>API Keys</h2>
          <p className='text-muted-foreground mt-1 text-sm'>管理您的 API 访问密钥</p>
        </div>
        <button
          onClick={handleCreate}
          className='bg-primary hover:bg-primary/90 inline-flex h-9 items-center justify-center gap-2 rounded-lg px-4 text-sm font-medium text-white transition-colors'
        >
          <Plus className='size-4' />
          新建 Key
        </button>
      </div>

      {/* 表格 */}
      <div className='border-border/60 overflow-hidden rounded-xl border'>
        <div className='overflow-x-auto'>
          <table className='w-full text-sm'>
            <thead>
              <tr className='bg-muted/30 border-border/40 border-b'>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>名称</th>
                <th className='text-muted-foreground min-w-[300px] px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>Key</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>创建时间</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>操作</th>
              </tr>
            </thead>
            <tbody className='divide-border/40 divide-y'>
              {loading ? (
                <tr>
                  <td colSpan={4} className='px-4 py-12 text-center'>
                    <Loader2 className='text-muted-foreground mx-auto size-6 animate-spin' />
                  </td>
                </tr>
              ) : keys.length === 0 ? (
                <tr>
                  <td colSpan={4} className='px-4 py-12 text-center'>
                    <p className='text-muted-foreground text-sm'>暂无 API Key</p>
                  </td>
                </tr>
              ) : keys.map((k) => (
                <tr key={k.id} className='hover:bg-muted/20 transition-colors'>
                  <td className='px-4 py-3'>
                    <div className='flex items-center gap-2'>
                      <Key className='text-muted-foreground size-4' />
                      <span className='font-medium'>{k.name}</span>
                    </div>
                  </td>
                  <td className='px-4 py-3'>
                    <code className='font-mono text-xs break-all'>{formatKey(k)}</code>
                  </td>
                  <td className='px-4 py-3'>
                    <span className='text-muted-foreground text-sm'>{k.created_at}</span>
                  </td>
                  <td className='px-4 py-3'>
                    <div className='flex items-center gap-1'>
                      <button
                        onClick={() => handleEdit(k)}
                        className='hover:bg-muted inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs font-medium transition-colors'
                      >
                        <Pencil className='size-3.5' /> 编辑
                      </button>
                      <button
                        onClick={() => { setDeleteRow(k); setDeleteOpen(true) }}
                        className='text-destructive hover:bg-destructive/10 inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs font-medium transition-colors'
                      >
                        <Trash2 className='size-3.5' /> 删除
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      {/* 新建弹框 */}
      {createOpen && (
        <div className='fixed inset-0 z-[100] flex items-center justify-center'>
          <div className='bg-background/80 fixed inset-0 backdrop-blur-sm' onClick={() => setCreateOpen(false)} />
          <div className='bg-background border-border/60 relative z-10 w-full max-w-md rounded-xl border shadow-lg'>
            <div className='flex items-center justify-between border-b border-border/40 px-6 py-4'>
              <h2 className='text-lg font-semibold'>新建 API Key</h2>
              <button onClick={() => setCreateOpen(false)} className='hover:bg-muted rounded-lg p-1.5 transition-colors'>
                <X className='size-4' />
              </button>
            </div>
            <div className='p-6 space-y-4'>
              {createError && (
                <div className='bg-destructive/10 text-destructive rounded-lg px-4 py-3 text-sm'>{createError}</div>
              )}
              <div className='space-y-2'>
                <label className='text-sm font-medium'>名称</label>
                <input
                  type='text'
                  value={createName}
                  onChange={(e) => setCreateName(e.target.value)}
                  placeholder='例如: 我的应用'
                  autoFocus
                  onKeyDown={(e) => { if (e.key === 'Enter') handleCreateConfirm() }}
                  className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                />
              </div>
            </div>
            <div className='flex justify-end gap-3 border-t border-border/40 px-6 py-4'>
              <button onClick={() => setCreateOpen(false)} disabled={createSaving}
                className='border-border/60 hover:bg-muted inline-flex h-9 items-center justify-center rounded-lg border px-4 text-sm font-medium transition-colors'>
                取消
              </button>
              <button onClick={handleCreateConfirm} disabled={createSaving}
                className='bg-primary hover:bg-primary/90 inline-flex h-9 items-center justify-center gap-2 rounded-lg px-4 text-sm font-medium text-white transition-colors disabled:opacity-50'>
                {createSaving && <Loader2 className='size-4 animate-spin' />}
                {createSaving ? '创建中...' : '创建'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 创建结果弹框 */}
      {resultOpen && (
        <div className='fixed inset-0 z-[100] flex items-center justify-center'>
          <div className='bg-background/80 fixed inset-0 backdrop-blur-sm' onClick={() => setResultOpen(false)} />
          <div className='bg-background border-border/60 relative z-10 w-full max-w-lg rounded-xl border p-6 shadow-lg'>
            <h2 className='text-lg font-semibold'>API Key 已创建</h2>
            <p className='text-muted-foreground mt-2 text-sm'>请立即复制保存，此密钥不会再次显示：</p>
            <div className='bg-muted/30 mt-4 flex items-center gap-2 rounded-lg px-4 py-3'>
              <code className='flex-1 font-mono text-sm break-all select-all'>{resultKey}</code>
              <button
                onClick={() => handleCopy(resultKey)}
                className='hover:bg-muted shrink-0 rounded-lg p-1.5 transition-colors'
                title='复制'
              >
                {copied ? <Check className='size-4 text-emerald-500' /> : <Copy className='size-4 text-muted-foreground' />}
              </button>
            </div>
            <div className='mt-6 flex justify-end'>
              <button
                onClick={() => setResultOpen(false)}
                className='bg-primary hover:bg-primary/90 inline-flex h-9 items-center justify-center rounded-lg px-4 text-sm font-medium text-white transition-colors'
              >
                我已保存，关闭
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 编辑弹框 */}
      {editOpen && (
        <div className='fixed inset-0 z-[100] flex items-center justify-center'>
          <div className='bg-background/80 fixed inset-0 backdrop-blur-sm' onClick={() => setEditOpen(false)} />
          <div className='bg-background border-border/60 relative z-10 w-full max-w-md rounded-xl border shadow-lg'>
            <div className='flex items-center justify-between border-b border-border/40 px-6 py-4'>
              <h2 className='text-lg font-semibold'>编辑名称</h2>
              <button onClick={() => setEditOpen(false)} className='hover:bg-muted rounded-lg p-1.5 transition-colors'>
                <X className='size-4' />
              </button>
            </div>
            <div className='p-6 space-y-4'>
              <div className='space-y-2'>
                <label className='text-sm font-medium'>名称</label>
                <input
                  type='text'
                  value={editName}
                  onChange={(e) => setEditName(e.target.value)}
                  onKeyDown={(e) => { if (e.key === 'Enter') handleEditConfirm() }}
                  className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                />
              </div>
            </div>
            <div className='flex justify-end gap-3 border-t border-border/40 px-6 py-4'>
              <button onClick={() => setEditOpen(false)} disabled={editSaving}
                className='border-border/60 hover:bg-muted inline-flex h-9 items-center justify-center rounded-lg border px-4 text-sm font-medium transition-colors'>
                取消
              </button>
              <button onClick={handleEditConfirm} disabled={editSaving}
                className='bg-primary hover:bg-primary/90 inline-flex h-9 items-center justify-center gap-2 rounded-lg px-4 text-sm font-medium text-white transition-colors disabled:opacity-50'>
                {editSaving && <Loader2 className='size-4 animate-spin' />}
                {editSaving ? '保存中...' : '保存'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 删除确认 */}
      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title='确认删除'
        description={<>确定要删除 API Key <span className='text-foreground font-semibold'>{deleteRow?.name}</span> 吗？删除后使用此 Key 的应用将无法访问。</>}
        confirmText='删除'
        destructive
        loading={actionLoading}
        onConfirm={handleDeleteConfirm}
      />
    </div>
  )
}
